package multiagent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	copenai "cyberstrike-ai/internal/openai"
	"cyberstrike-ai/internal/project"

	"github.com/bytedance/sonic"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"go.uber.org/zap"
)

// einoSummarizeUserInstruction: when compressing history, preserve the key information from penetration testing and user constraints.
// Aligned with Eino best practices (no tools, <analysis>+<summary>, <all_user_messages>); sections are tailored to security testing.
const einoSummarizeUserInstruction = `Key: Respond in plain text only. Do not call any tools (read_file, exec, grep, glob, write, edit, etc.).
The conversation above already contains the full context to be compressed; do not ask the user to paste history, do not output placeholder/meta replies like "please provide the conversation history to be compressed".
Tool calls will be rejected and will waste the one summary chance.

Your task: Compress the conversation history while keeping all key security testing information complete, so that subsequent agents can seamlessly continue the same authorized testing task.

Compression principles:
- Must preserve: confirmed vulnerabilities and attack paths, core findings from tool outputs, credentials and authentication details, architecture and weaknesses, current progress, failed attempts and dead ends, strategy decisions
- Preserve precise technical details (URLs, paths, parameters, payloads, version numbers; error messages can be summarized but key points must not be lost)
- Summarize verbose scan outputs into conclusions; merge repeated findings
- Already-enumerated assets must preserve an inheritable summary: main domain, brief list of key subdomains/hosts (or count + representative samples), high-value targets, key identified services/ports

Output format (strictly follow, only one reply round):
1. First output the <analysis> block: review the conversation in chronological order and check whether the following sections are covered; the analysis is for self-check only, keep it concise (suggested ≤ 400 words)
2. Then output the <summary> block: write the inheritable compression report by the following sections (write "none" where there is no information, do not leave empty template placeholders)

<summary>
## 1. Authorization Scope and Constraints
- Target / scope / prohibited items (domains, paths, IPs, environment)
- Credentials / authentication info (accounts, tokens, cookies; preserve sensitive values verbatim)
- User-specified methods, tools, priorities, and todos
- Negative constraints (what not to test, what techniques not to use)

## 2. Assets and Service Enumeration Summary
- Main domain / core assets, brief list of key subdomains or hosts (or count + representative samples)
- High-value targets, key identified services/ports
- Asset status (alive / attackable / ruled out / pending verification)

## 3. Architecture and Known Weaknesses
- Tech stack / deployment topology / trust boundaries
- List of identified weaknesses

## 4. Confirmed Vulnerabilities and Attack Paths
- Vulnerability name / CVE, URL / path, parameters / payload, PoC key points, impact level
- Attack chain / exploitation path (step-by-step)

## 5. Tool Core Findings and Scan Conclusions
- Each tool's conclusion (summarize core output, not verbose logs)
- Merge repeated findings

## 6. All User Messages
<all_user_messages>
- [List the key points of each non-tool user message; preserve sensitive constraints and original wording as much as possible]
</all_user_messages>

## 7. Current Progress, Strategy Decisions, and Next Steps
- Current position (completed / in progress / blocked)
- Failed attempts and dead ends (method, phenomenon / error summary, conclusion)
- Strategy decisions and concrete next-step operations (must be consistent with the most recent user request and unfinished tasks)
</summary>

Reminder: Do not call any tools; you must directly output <analysis> and <summary> based on the existing conversation above, and do not output any body text outside of the analysis.`

// newEinoSummarizationMiddleware 使用 Eino ADK Summarization 中间件（见 https://www.cloudwego.io/zh/docs/eino/core_modules/eino_adk/eino_adk_chatmodelagentmiddleware/middleware_summarization/）。
// 触发阈值：估算 token 超过 openai.max_total_tokens * summarization_trigger_ratio（默认 0.8）时摘要。
func newEinoSummarizationMiddleware(
	ctx context.Context,
	summaryModel model.BaseChatModel,
	appCfg *config.Config,
	mwCfg *config.MultiAgentEinoMiddlewareConfig,
	conversationID string,
	db *database.DB,
	projectID string,
	logger *zap.Logger,
) (adk.ChatModelAgentMiddleware, error) {
	if summaryModel == nil || appCfg == nil {
		return nil, fmt.Errorf("multiagent: summarization 需要 model 与配置")
	}
	maxTotal := appCfg.OpenAI.MaxTotalTokens
	if maxTotal <= 0 {
		maxTotal = 120000
	}
	triggerRatio := 0.8
	emitInternalEvents := true
	if mwCfg != nil {
		triggerRatio = mwCfg.SummarizationTriggerRatioEffective()
		emitInternalEvents = mwCfg.SummarizationEmitInternalEventsEffective()
	}
	// Keep enough safety margin for tokenizer/model-side accounting mismatch.
	trigger := int(float64(maxTotal) * triggerRatio)
	if trigger < 4096 {
		trigger = maxTotal
		if trigger < 4096 {
			trigger = 4096
		}
	}
	preserveMax := trigger / 3
	if preserveMax < 2048 {
		preserveMax = 2048
	}

	modelName := strings.TrimSpace(appCfg.OpenAI.Model)
	if modelName == "" {
		modelName = "gpt-4o"
	}
	tokenCounter := einoSummarizationTokenCounter(modelName)
	recentTrailMax := trigger / 4
	if recentTrailMax < 2048 {
		recentTrailMax = 2048
	}
	if recentTrailMax > trigger/2 {
		recentTrailMax = trigger / 2
	}
	transcriptPath := ""
	if conv := strings.TrimSpace(conversationID); conv != "" {
		baseRoot := filepath.Join(os.TempDir(), "cyberstrike-summarization")
		if dbPath := strings.TrimSpace(appCfg.Database.Path); dbPath != "" {
			// Persist with the same lifecycle as local conversation storage.
			baseRoot = filepath.Join(filepath.Dir(dbPath), "conversation_artifacts", sanitizeEinoPathSegment(conv), "summarization")
		}
		base := baseRoot
		if mkErr := os.MkdirAll(base, 0o755); mkErr == nil {
			transcriptPath = filepath.Join(base, "transcript.txt")
		}
	}

	retryPolicy := einoTransientRunRetryPolicyFromMW(mwCfg)
	retryMax := retryPolicy.maxAttempts

	// ModelOptions apply only to summarization Generate (same ChatModel instance as the agent).
	// Strip thinking/reasoning on this call path; mark requests for empty-choices diagnostics.
	summaryModelOpts := []model.Option{
		einoopenai.WithExtraHeader(map[string]string{
			copenai.SummarizationRequestHeader: "1",
		}),
		einoopenai.WithRequestPayloadModifier(func(_ context.Context, in []*schema.Message, rawBody []byte) ([]byte, error) {
			if logger != nil {
				logger.Info("eino summarization generate request",
					zap.Int("input_messages", len(in)),
					zap.Int("payload_bytes", len(rawBody)),
					zap.String("model", modelName),
				)
			}
			return stripReasoningFromSummarizationPayload(rawBody)
		}),
	}

	mw, err := summarization.New(ctx, &summarization.Config{
		Model:        summaryModel,
		ModelOptions: summaryModelOpts,
		Trigger: &summarization.TriggerCondition{
			ContextTokens: trigger,
		},
		TokenCounter:       tokenCounter,
		UserInstruction:    einoSummarizeUserInstruction,
		EmitInternalEvents: emitInternalEvents,
		TranscriptFilePath: transcriptPath,
		PreserveUserMessages: &summarization.PreserveUserMessages{
			Enabled:   true,
			MaxTokens: preserveMax,
		},
		Retry: &summarization.RetryConfig{
			MaxRetries: &retryMax,
			ShouldRetry: func(_ context.Context, _ adk.Message, err error) bool {
				retry := isEinoTransientRunError(err)
				if retry && logger != nil {
					logger.Warn("eino summarization generate transient error, will retry if attempts remain",
						zap.Error(err),
						zap.Int("max_retries", retryMax),
					)
				}
				return retry
			},
		},
		Finalize: func(ctx context.Context, originalMessages []adk.Message, summary adk.Message) ([]adk.Message, error) {
			summary = stripAnalysisFromSummarizationMessage(summary)
			out, ferr := summarizeFinalizeWithRecentAssistantToolTrail(ctx, originalMessages, summary, tokenCounter, recentTrailMax)
			if ferr != nil {
				return nil, ferr
			}
			if appCfg != nil {
				out = refreshFactIndexInMessages(out, db, projectID, appCfg.Project, logger)
			}
			return out, nil
		},
		Callback: func(ctx context.Context, before, after adk.ChatModelAgentState) error {
			if transcriptPath != "" && len(before.Messages) > 0 {
				if werr := writeSummarizationTranscript(transcriptPath, before.Messages); werr != nil && logger != nil {
					logger.Warn("eino summarization transcript 写入失败",
						zap.String("path", transcriptPath),
						zap.Error(werr),
					)
				}
			}
			if logger != nil {
				beforeTokens, _ := tokenCounter(ctx, &summarization.TokenCounterInput{Messages: before.Messages})
				afterTokens, _ := tokenCounter(ctx, &summarization.TokenCounterInput{Messages: after.Messages})
				logger.Info("eino summarization 已压缩上下文",
					zap.Int("messages_before", len(before.Messages)),
					zap.Int("messages_after", len(after.Messages)),
					zap.Int("tokens_before_estimated", beforeTokens),
					zap.Int("tokens_after_estimated", afterTokens),
					zap.Int("max_total_tokens", maxTotal),
					zap.Int("trigger_context_tokens", trigger),
					zap.String("transcript_file", transcriptPath),
				)
			}
			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("summarization.New: %w", err)
	}
	return mw, nil
}

// refreshFactIndexInMessages 在 summarization 压缩后，用 DB 最新索引替换 system 中已有的项目黑板索引段。
func refreshFactIndexInMessages(msgs []adk.Message, db *database.DB, projectID string, cfg config.ProjectConfig, logger *zap.Logger) []adk.Message {
	if db == nil || !cfg.Enabled {
		return msgs
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return msgs
	}
	freshIndex, err := project.BuildFactIndexBlock(db, projectID, cfg)
	if err != nil {
		if logger != nil {
			logger.Warn("summarization: 刷新项目黑板索引失败", zap.String("projectId", projectID), zap.Error(err))
		}
		return msgs
	}
	freshIndex = strings.TrimSpace(freshIndex)
	if freshIndex == "" {
		return msgs
	}

	changed := false
	out := make([]adk.Message, len(msgs))
	for i, msg := range msgs {
		if msg == nil || msg.Role != schema.System {
			out[i] = msg
			continue
		}
		newContent, ok := project.ReplaceFactIndexSection(msg.Content, freshIndex)
		if !ok {
			out[i] = msg
			continue
		}
		cloned := *msg
		cloned.Content = newContent
		out[i] = &cloned
		changed = true
	}
	if changed && logger != nil {
		logger.Info("summarization: 已刷新项目黑板索引", zap.String("projectId", projectID))
	}
	return out
}

// summarizeFinalizeWithRecentAssistantToolTrail 在摘要消息后保留最近 assistant/tool 轨迹，避免压缩后执行链断裂。
//
// 关键不变量：tool_call ↔ tool_result 的 pair 必须整体保留或整体丢弃。
// 把消息切成 round（回合）为原子单位：
//   - user(...) 单条为一个 round；
//   - assistant(tool_calls=[...]) 及其后连续的 role=tool 消息合成一个 round；
//   - 其它 assistant(reply, 无 tool_calls) 单条为一个 round。
//
// 倒序挑 round（预算不够即放弃该 round），保证 tool 消息不会跨 round 被孤立。
func summarizeFinalizeWithRecentAssistantToolTrail(
	ctx context.Context,
	originalMessages []adk.Message,
	summary adk.Message,
	tokenCounter summarization.TokenCounterFunc,
	recentTrailTokenBudget int,
) ([]adk.Message, error) {
	systemMsgs := make([]adk.Message, 0, len(originalMessages))
	nonSystem := make([]adk.Message, 0, len(originalMessages))
	for _, msg := range originalMessages {
		if msg == nil {
			continue
		}
		if msg.Role == schema.System {
			systemMsgs = append(systemMsgs, msg)
			continue
		}
		nonSystem = append(nonSystem, msg)
	}

	mergedSystem := mergeCollectedSystemMessages(systemMsgs)

	if recentTrailTokenBudget <= 0 || len(nonSystem) == 0 {
		out := make([]adk.Message, 0, len(mergedSystem)+1)
		out = append(out, mergedSystem...)
		out = append(out, summary)
		return out, nil
	}

	rounds := splitMessagesIntoRounds(nonSystem)
	if len(rounds) == 0 {
		out := make([]adk.Message, 0, len(mergedSystem)+1)
		out = append(out, mergedSystem...)
		out = append(out, summary)
		return out, nil
	}

	// 目标：至少保留 minRounds 个 round 的执行轨迹；在预算允许时尽量多保留。
	// 优先确保最后一个 round（通常是最新的 tool 往返或 assistant 回复）存在。
	const minRounds = 2

	selectedRoundsReverse := make([]messageRound, 0, 8)
	selectedCount := 0
	totalTokens := 0

	tokensOfRound := func(r messageRound) (int, error) {
		if len(r.messages) == 0 {
			return 0, nil
		}
		n, err := tokenCounter(ctx, &summarization.TokenCounterInput{Messages: r.messages})
		if err != nil {
			return 0, err
		}
		if n <= 0 {
			n = len(r.messages)
		}
		return n, nil
	}

	for i := len(rounds) - 1; i >= 0; i-- {
		r := rounds[i]
		n, err := tokensOfRound(r)
		if err != nil {
			return nil, err
		}
		// 预算不够：已经保留了足够 round 则停，否则跳过该 round 继续往前找
		// （避免一个超大 round 挤占全部预算，至少保证有轨迹）。
		if totalTokens+n > recentTrailTokenBudget {
			if selectedCount >= minRounds {
				break
			}
			continue
		}
		totalTokens += n
		selectedRoundsReverse = append(selectedRoundsReverse, r)
		selectedCount++
	}

	// 还原时间顺序。round 内为原始 *schema.Message 指针，保留 ReasoningContent（DeepSeek 工具续跑所必需）。
	selectedMsgs := make([]adk.Message, 0, 8)
	for i := len(selectedRoundsReverse) - 1; i >= 0; i-- {
		selectedMsgs = append(selectedMsgs, selectedRoundsReverse[i].messages...)
	}

	out := make([]adk.Message, 0, len(mergedSystem)+1+len(selectedMsgs))
	out = append(out, mergedSystem...)
	out = append(out, summary)
	out = append(out, selectedMsgs...)
	return out, nil
}

// messageRound 表示一个"不可分割"的消息回合。
//   - 对 assistant(tool_calls) + 随后若干 tool 消息的组合，round 内全部 call_id 成对完整；
//   - 对独立的 user / assistant(reply) 消息，round 仅包含该条消息。
type messageRound struct {
	messages []adk.Message
}

// splitMessagesIntoRounds 将非 system 消息切分为若干 round，保证：
//   - 每个 assistant(tool_calls) 与其对应的 role=tool 响应消息在同一个 round；
//   - 孤立（无对应 assistant(tool_calls)）的 role=tool 消息不会单独成为 round，
//     而是被丢弃（这些消息在 pair 完整性层面已属孤儿，保留反而会触发 LLM 400）。
func splitMessagesIntoRounds(msgs []adk.Message) []messageRound {
	if len(msgs) == 0 {
		return nil
	}
	rounds := make([]messageRound, 0, len(msgs))
	i := 0
	for i < len(msgs) {
		msg := msgs[i]
		if msg == nil {
			i++
			continue
		}
		switch {
		case msg.Role == schema.Assistant && len(msg.ToolCalls) > 0:
			// 收集该 assistant 提供的 call_id 集合。
			provided := make(map[string]struct{}, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				if tc.ID != "" {
					provided[tc.ID] = struct{}{}
				}
			}
			round := messageRound{messages: []adk.Message{msg}}
			j := i + 1
			for j < len(msgs) {
				next := msgs[j]
				if next == nil {
					j++
					continue
				}
				if next.Role != schema.Tool {
					break
				}
				if next.ToolCallID != "" {
					if _, ok := provided[next.ToolCallID]; !ok {
						// 下一条 tool 不属于当前 assistant，认为当前 round 结束。
						break
					}
				}
				round.messages = append(round.messages, next)
				j++
			}
			rounds = append(rounds, round)
			i = j
		case msg.Role == schema.Tool:
			// 孤儿 tool 消息：既不跟随在一个 assistant(tool_calls) 后，
			// 说明它对应的 assistant 已被上游裁剪；直接丢弃，下一步到 orphan pruner
			// 兜底也不会出错，但在 round 切分这里就剔除更干净。
			i++
		default:
			// user / assistant(reply) / 其它：单条成 round。
			rounds = append(rounds, messageRound{messages: []adk.Message{msg}})
			i++
		}
	}
	return rounds
}

// writeSummarizationTranscript persists pre-compaction history for read_file after summarization.
// Eino TranscriptFilePath only embeds the path in summary text; the file must be written by the host app.
func writeSummarizationTranscript(path string, msgs []adk.Message) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	body := formatSummarizationTranscript(msgs)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir transcript dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return fmt.Errorf("write transcript: %w", err)
	}
	return nil
}

func einoSummarizationTokenCounter(openAIModel string) summarization.TokenCounterFunc {
	tc := agent.NewTikTokenCounter()
	return func(ctx context.Context, input *summarization.TokenCounterInput) (int, error) {
		var sb strings.Builder
		for _, msg := range input.Messages {
			if msg == nil {
				continue
			}
			sb.WriteString(string(msg.Role))
			sb.WriteByte('\n')
			if msg.Content != "" {
				sb.WriteString(msg.Content)
				sb.WriteByte('\n')
			}
			if msg.ReasoningContent != "" {
				sb.WriteString(msg.ReasoningContent)
				sb.WriteByte('\n')
			}
			if len(msg.ToolCalls) > 0 {
				if b, err := sonic.Marshal(msg.ToolCalls); err == nil {
					sb.Write(b)
					sb.WriteByte('\n')
				}
			}
			for _, part := range msg.UserInputMultiContent {
				if part.Type == schema.ChatMessagePartTypeText && part.Text != "" {
					sb.WriteString(part.Text)
					sb.WriteByte('\n')
				}
			}
		}
		for _, tl := range input.Tools {
			if tl == nil {
				continue
			}
			cp := *tl
			cp.Extra = nil
			if text, err := sonic.MarshalString(cp); err == nil {
				sb.WriteString(text)
				sb.WriteByte('\n')
			}
		}
		text := sb.String()
		n, err := tc.Count(openAIModel, text)
		if err != nil {
			return (len(text) + 3) / 4, nil
		}
		return n, nil
	}
}
