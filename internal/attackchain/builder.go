package attackchain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/openai"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Builder is an attack chain builder.
type Builder struct {
	db           *database.DB
	logger       *zap.Logger
	openAIClient *openai.Client
	openAIConfig *config.OpenAIConfig
	tokenCounter agent.TokenCounter
	maxTokens    int // max token limit, default 100000
}

// Node is an attack chain node (uses types from the database package).
type Node = database.AttackChainNode

// Edge is an attack chain edge (uses types from the database package).
type Edge = database.AttackChainEdge

// Chain 完整的攻击链
type Chain struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// NewBuilder creates a new attack chain builder.
func NewBuilder(db *database.DB, openAIConfig *config.OpenAIConfig, logger *zap.Logger) *Builder {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}
	httpClient := &http.Client{Timeout: 5 * time.Minute, Transport: transport}

	// Prefer the unified token cap from the config file (config.yaml -> openai.max_total_tokens)
	maxTokens := 0
	if openAIConfig != nil && openAIConfig.MaxTotalTokens > 0 {
		maxTokens = openAIConfig.MaxTotalTokens
	} else if openAIConfig != nil {
	} else if openAIConfig != nil {
		model := strings.ToLower(openAIConfig.Model)
		if strings.Contains(model, "gpt-4") {
			maxTokens = 128000 // gpt-4 typically supports 128k
		} else if strings.Contains(model, "gpt-3.5") {
			maxTokens = 16000 // gpt-3.5-turbo typically supports 16k
		} else if strings.Contains(model, "deepseek") {
			maxTokens = 131072 // deepseek-chat typically supports 131k
		} else {
			maxTokens = 100000 // fallback default
		}
	} else {
		// No OpenAI config; use fallback value to avoid zero
		maxTokens = 100000
	}

	return &Builder{
		db:           db,
		logger:       logger,
		openAIClient: openai.NewClient(openAIConfig, httpClient, logger),
		openAIConfig: openAIConfig,
		tokenCounter: agent.NewTikTokenCounter(),
		maxTokens:    maxTokens,
	}
}

// BuildChainFromConversation builds an attack chain from a conversation (single LLM call; input is the last_react trace of the current task turn, aligned with the continue-conversation scope).
func (b *Builder) BuildChainFromConversation(ctx context.Context, conversationID string) (*Chain, error) {
	b.logger.Info("Starting attack chain construction (simplified version)", zap.String("conversationId", conversationID))

	// 0. First check if there are actual tool execution records
	messages, err := b.db.GetMessages(conversationID)
	if err != nil {
		return nil, fmt.Errorf("failed to get conversation messages: %w", err)
	}

	if len(messages) == 0 {
		b.logger.Info("No data in conversation", zap.String("conversationId", conversationID))
		return &Chain{Nodes: []Node{}, Edges: []Edge{}}, nil
	}

	// Check if there are actual tool executions: assistant's mcp_execution_ids, or tool_call/tool_result in process details
	// (in multi-agent mode, MCP may not return execution_id so IDs may be empty, but tools were executed via Eino and written to process_details)
	hasToolExecutions := false
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(messages[i].Role, "assistant") {
			if len(messages[i].MCPExecutionIDs) > 0 {
				hasToolExecutions = true
				break
			}
		}
	}
	if !hasToolExecutions {
		if pdOK, err := b.db.ConversationHasToolProcessDetails(conversationID); err != nil {
			b.logger.Warn("failed to query process details to determine tool execution", zap.Error(err))
		} else if pdOK {
			hasToolExecutions = true
		}
	}

	// Check if the task was cancelled (by checking the last assistant message content or process_details)
	taskCancelled := false
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(messages[i].Role, "assistant") {
			content := strings.ToLower(messages[i].Content)
		if strings.Contains(content, "取消") || strings.Contains(content, "cancelled") {
				taskCancelled = true
			}
			break
		}
	}

	// If the task was cancelled and there were no actual tool executions, return an empty attack chain
	if taskCancelled && !hasToolExecutions {
		b.logger.Info("task cancelled with no actual tool executions, returning empty attack chain",
			zap.String("conversationId", conversationID),
			zap.Bool("taskCancelled", taskCancelled),
			zap.Bool("hasToolExecutions", hasToolExecutions))
		return &Chain{Nodes: []Node{}, Edges: []Edge{}}, nil
	}

	// If there are no actual tool executions, also return an empty attack chain (avoid AI fabrication)
	if !hasToolExecutions {
		b.logger.Info("no actual tool execution records, returning empty attack chain",
			zap.String("conversationId", conversationID))
		return &Chain{Nodes: []Node{}, Edges: []Edge{}}, nil
	}

	// 1. First try to get saved last-round ReAct input and output from database
	reactInputJSON, modelOutput, err := b.db.GetAgentTrace(conversationID)
	if err != nil {
		b.logger.Warn("failed to get saved ReAct data, will use message history instead", zap.Error(err))
		// Continue with original logic
		reactInputJSON = ""
		modelOutput = ""
	}

	// var userInput string
	var reactInputFinal string
	var dataSource string // track data source

	// Prefer the stored agent trace (same source as continue-conversation loadHistoryFromAgentTrace), trimmed to "current task turn"
	if reactInputJSON != "" {
		trimmedJSON := agent.ExtractLastUserTurnTraceJSON(reactInputJSON)
		hash := sha256.Sum256([]byte(trimmedJSON))
		reactInputHash := hex.EncodeToString(hash[:])[:16]

		var messageCount int
		if msgs, parseErr := agent.ParseTraceMessages(trimmedJSON); parseErr == nil {
			messageCount = len(msgs)
			msgs = agent.MergeAssistantTraceOutput(msgs, modelOutput)
			reactInputFinal = b.formatAgentTraceFromChatMessages(msgs)
		} else {
			b.logger.Warn("failed to parse agent trace, falling back to raw JSON formatting", zap.Error(parseErr))
			reactInputFinal = b.formatAgentTraceInputFromJSON(trimmedJSON)
			if strings.TrimSpace(modelOutput) != "" {
				reactInputFinal += "\n\n## Assistant Conclusion (last_react_output)\n\n" + modelOutput
			}
		}

		dataSource = "last_user_turn_agent_trace"
		dataSource = "last_user_turn_agent_trace"
		b.logger.Info("building attack chain using current task turn agent trace (aligned with continue-conversation context scope)",
			zap.String("conversationId", conversationID),
			zap.String("dataSource", dataSource),
			zap.Int("traceInputSizeBeforeTrim", len(reactInputJSON)),
			zap.Int("traceInputSizeAfterTrim", len(trimmedJSON)),
			zap.Int("messageCount", messageCount),
			zap.String("reactInputHash", reactInputHash),
			zap.Int("modelOutputSize", len(modelOutput)))
	} else {
		// 2. If there is no saved ReAct data, build from conversation messages
		dataSource = "messages_table"
		b.logger.Info("building ReAct data from message history",
			zap.String("conversationId", conversationID),
			zap.String("dataSource", dataSource),
			zap.Int("messageCount", len(messages)))

		// Extract user input (last user message)
		for i := len(messages) - 1; i >= 0; i-- {
			if strings.EqualFold(messages[i].Role, "user") {
				// userInput = messages[i].Content
				break
			}
		}

		// Extract the last round of ReAct input (history messages + current user input)
		reactInputFinal = b.buildAgentTraceInput(messages)

		// Extract the model's last output (last assistant message)
		for i := len(messages) - 1; i >= 0; i-- {
			if strings.EqualFold(messages[i].Role, "assistant") {
				modelOutput = messages[i].Content
				break
			}
		}
	}

	// Multi-agent: the saved trace column may only contain the first-round user message without tool traces; supplement with the last assistant turn's process details (align with single-agent complete trace)
	hasMCPOnAssistant := false
	var lastAssistantID string
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(messages[i].Role, "assistant") {
			lastAssistantID = messages[i].ID
			if len(messages[i].MCPExecutionIDs) > 0 {
				hasMCPOnAssistant = true
			}
			break
		}
	}
	if lastAssistantID != "" {
		pdHasTools, _ := b.db.ConversationHasToolProcessDetails(conversationID)
		if pdHasTools && !(hasMCPOnAssistant && reactInputContainsToolTrace(reactInputJSON)) {
			detailsMap, err := b.db.GetProcessDetailsByConversation(conversationID)
			if err != nil {
			b.logger.Warn("failed to load process details for attack chain", zap.Error(err))
			} else if dets := detailsMap[lastAssistantID]; len(dets) > 0 {
				extra := b.formatProcessDetailsForAttackChain(dets)
				if strings.TrimSpace(extra) != "" {
					reactInputFinal = reactInputFinal + "\n\n## Execution Process and Tool Records (including multi-agent orchestration and subtasks)\n\n" + extra
				b.logger.Info("attack chain input supplemented with process details",
					zap.String("conversationId", conversationID),
					zap.String("messageId", lastAssistantID),
					zap.Int("detailEvents", len(dets)))
				}
			}
		}
	}

	// 3. Compress input by token budget, then build prompt (avoid exceeding model context)
	reactInputFinal, modelOutput, _ = b.fitAttackChainPayload(reactInputFinal, modelOutput)

	// 4. Build prompt and call LLM once (assistant conclusion already merged into trace — don't pass it again)
	promptAssistantOut := modelOutput
	if reactInputJSON != "" {
		promptAssistantOut = ""
	}
	prompt := b.buildSimplePrompt(reactInputFinal, promptAssistantOut)
	// fmt.Println(prompt)
	// 6. Call AI to generate attack chain (one-shot, no post-processing)
	chainJSON, err := b.callAIForChainGeneration(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("AI generation failed: %w", err)
	}

	// 7. Parse JSON and generate node/edge IDs (frontend requires valid IDs)
	chainData, err := b.parseChainJSON(chainJSON)
	if err != nil {
		// If parsing fails, return empty chain — let the frontend handle the error
		b.logger.Warn("failed to parse attack chain JSON", zap.Error(err), zap.String("raw_json", chainJSON))
		return &Chain{
			Nodes: []Node{},
			Edges: []Edge{},
		}, nil
	}

	b.logger.Info("attack chain construction completed",
		zap.String("conversationId", conversationID),
		zap.String("dataSource", dataSource),
		zap.Int("nodes", len(chainData.Nodes)),
		zap.Int("edges", len(chainData.Edges)))

	// Save to database (for subsequent loading)
	if err := b.saveChain(conversationID, chainData.Nodes, chainData.Edges); err != nil {
		b.logger.Warn("failed to save attack chain to database", zap.Error(err))
		// Even if saving fails, return data to the frontend
	}

	// Return directly without any post-processing or validation
	return chainData, nil
}

// reactInputContainsToolTrace checks whether the saved ReAct JSON contains parseable tool-call traces (true when single-agent saves the full trace).
func reactInputContainsToolTrace(reactInputJSON string) bool {
	s := strings.TrimSpace(reactInputJSON)
	if s == "" {
		return false
	}
	return strings.Contains(s, "tool_calls") ||
		strings.Contains(s, "tool_call_id") ||
		strings.Contains(s, `"role":"tool"`) ||
		strings.Contains(s, `"role": "tool"`)
}

// formatProcessDetailsForAttackChain formats the last assistant turn's process details as input for attack chain analysis (covers incomplete last_react_input in multi-agent scenarios).
func (b *Builder) formatProcessDetailsForAttackChain(details []database.ProcessDetail) string {
	if len(details) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, d := range details {
		// Goal: output the full iteration from the perspective of the main agent (orchestrator)
		// - Keep: orchestrator tool calls/results, subtask dispatches to sub-agents, sub-agent final replies (excluding reasoning)
		// - Drop: noise such as thinking/planning/progress, sub-agent tool details and reasoning process
		if d.EventType == "progress" || d.EventType == "thinking" || d.EventType == "reasoning_chain" || d.EventType == "planning" {
			continue
		}

		// Parse data (JSON string) to identify einoRole / toolName, etc.
		var dataMap map[string]interface{}
		if strings.TrimSpace(d.Data) != "" {
			_ = json.Unmarshal([]byte(d.Data), &dataMap)
		}
		einoRole := ""
		if v, ok := dataMap["einoRole"]; ok {
			einoRole = strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
		}
		toolName := ""
		if v, ok := dataMap["toolName"]; ok {
			toolName = strings.TrimSpace(fmt.Sprint(v))
		}

		// 1) Orchestrator tool calls/results: keep (these are "what tools the main agent called")
		if (d.EventType == "tool_call" || d.EventType == "tool_result" || d.EventType == "tool_calls_detected" || d.EventType == "iteration") && einoRole == "orchestrator" {
			sb.WriteString("[")
			sb.WriteString(d.EventType)
			sb.WriteString("] ")
			sb.WriteString(strings.TrimSpace(d.Message))
			sb.WriteString("\n")
			if strings.TrimSpace(d.Data) != "" {
				sb.WriteString(d.Data)
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
			continue
		}

		// 2) Sub-agent dispatch: tool_call(toolName=="task") represents the orchestrator dispatching a subtask; keep (only the task, not sub-agent reasoning)
		if d.EventType == "tool_call" && strings.EqualFold(toolName, "task") {
			sb.WriteString("[dispatch_subagent_task] ")
			sb.WriteString(strings.TrimSpace(d.Message))
			sb.WriteString("\n")
			if strings.TrimSpace(d.Data) != "" {
				sb.WriteString(d.Data)
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
			continue
		}

		// 3) Sub-agent final reply: keep (only the final output, not the analysis process)
		if d.EventType == "eino_agent_reply" && einoRole == "sub" {
			sb.WriteString("[subagent_final_reply] ")
			sb.WriteString(strings.TrimSpace(d.Message))
			sb.WriteString("\n")
			// data contains metadata like einoAgent — keeping it helps trace "which sub-agent said this"
			if strings.TrimSpace(d.Data) != "" {
				sb.WriteString(d.Data)
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
			continue
		}

		// Other events are dropped by default to avoid stuffing sub-agent tool details/reasoning into the prompt, deviating from the "main agent one-round iteration" perspective.
	}
	return strings.TrimSpace(sb.String())
}

// buildAgentTraceInput builds the last round of ReAct input (starting from the last user message, excluding earlier turns).
func (b *Builder) buildAgentTraceInput(messages []database.Message) string {
	start := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(messages[i].Role, "user") {
			start = i
			break
		}
	}
	var builder strings.Builder
	for _, msg := range messages[start:] {
		builder.WriteString(fmt.Sprintf("[%s]: %s\n\n", msg.Role, msg.Content))
	}
	return builder.String()
}

// extractUserInputFromReActInput extracts the last user input from saved ReAct input (JSON-formatted messages array)
// func (b *Builder) extractUserInputFromReActInput(reactInputJSON string) string {
// 	// reactInputJSON is a JSON-formatted ChatMessage array that needs parsing
// 	var messages []map[string]interface{}
// 	if err := json.Unmarshal([]byte(reactInputJSON), &messages); err != nil {
// 		b.logger.Warn("failed to parse ReAct input JSON", zap.Error(err))
// 		return ""
// 	}

// 	// Iterate backward to find the last user message
// 	for i := len(messages) - 1; i >= 0; i-- {
// 		if role, ok := messages[i]["role"].(string); ok && strings.EqualFold(role, "user") {
// 			if content, ok := messages[i]["content"].(string); ok {
// 				return content
// 			}
// 		}
// 	}

// 	return ""
// }

// formatAgentTraceInputFromJSON converts JSON trace to readable text (trims to the current task turn first).
func (b *Builder) formatAgentTraceInputFromJSON(reactInputJSON string) string {
	trimmed := agent.ExtractLastUserTurnTraceJSON(reactInputJSON)
	msgs, err := agent.ParseTraceMessages(trimmed)
	if err != nil {
		b.logger.Warn("failed to parse ReAct input JSON", zap.Error(err))
		return trimmed
	}
	return b.formatAgentTraceFromChatMessages(msgs)
}

// formatAgentTraceFromChatMessages formats agent messages as attack chain analysis input (aligned with continue-conversation trace fields).
func (b *Builder) formatAgentTraceFromChatMessages(msgs []agent.ChatMessage) string {
	var builder strings.Builder
	for _, msg := range msgs {
		role := msg.Role
		content := msg.Content

		if strings.EqualFold(role, "assistant") && len(msg.ToolCalls) > 0 {
			if content != "" {
				builder.WriteString(fmt.Sprintf("[%s]: %s\n", role, content))
			}
			builder.WriteString(fmt.Sprintf("[%s] tool calls (%d):\n", role, len(msg.ToolCalls)))
			for i, tc := range msg.ToolCalls {
				args := ""
				if tc.Function.Arguments != nil {
					if b, err := json.Marshal(tc.Function.Arguments); err == nil {
						args = string(b)
					}
				}
				builder.WriteString(fmt.Sprintf("  [tool call %d]\n", i+1))
				builder.WriteString(fmt.Sprintf("    ID: %s\n", tc.ID))
				builder.WriteString(fmt.Sprintf("    tool name: %s\n", tc.Function.Name))
				builder.WriteString(fmt.Sprintf("    arguments: %s\n", args))
			}
			builder.WriteString("\n")
			continue
		}

		if strings.EqualFold(role, "tool") {
			if msg.ToolCallID != "" {
				builder.WriteString(fmt.Sprintf("[%s] (tool_call_id: %s):\n%s\n\n", role, msg.ToolCallID, content))
			} else {
				builder.WriteString(fmt.Sprintf("[%s]: %s\n\n", role, content))
			}
			continue
		}

		builder.WriteString(fmt.Sprintf("[%s]: %s\n\n", role, content))
	}
	return builder.String()
}

// buildSimplePrompt builds a simplified prompt.
func (b *Builder) buildSimplePrompt(reactInput, modelOutput string) string {
	return fmt.Sprintf(`你是专业的安全测试分析师和攻击链构建专家。你的任务是根据**当前任务轮次**的对话记录和工具执行结果，一次性输出攻击链 JSON（不要分多轮追问）。

## 输入范围（与「继续对话」续跑一致）
- 下方「ReAct 轨迹」仅包含**最后一次用户提问之后**的消息与工具结果（last_react 当前任务轮次），不含更早的用户提问轮次。
- 「助手结论」为同轮任务的最终输出摘要（last_react_output）；节点须与轨迹中的实际工具执行一致，严禁编造。

## 核心目标

构建一个能够讲述完整攻击故事的攻击链让学习者能够：
1. 理解渗透测试的完整流程和思维逻辑（从目标识别到漏洞发现的每一步）
2. 学习如何从失败中获取线索并调整策略
3. 掌握工具使用的实际效果和局限性
4. 理解漏洞发现和利用的因果关系

**关键原则**：完整性优先。必须包含所有有意义的工具执行和关键步骤，不要为了控制节点数量而遗漏重要信息。

## 构建流程（按此顺序思考）

### 第一步：理解上下文
仔细分析ReAct输入中的工具调用序列和大模型输出，识别：
- 测试目标（IP、域名、URL等）
- 实际执行的工具和参数
- 工具返回的关键信息（成功结果、错误信息、超时等）
- AI的分析和决策过程

### 第二步：提取关键节点
从工具执行记录中提取有意义的节点，**确保不遗漏任何关键步骤**：
- **target节点**：每个独立的测试目标创建一个target节点
- **action节点**：每个有意义的工具执行创建一个action节点（包括提供线索的失败、成功的信息收集、漏洞验证等）
- **vulnerability节点**：每个真实确认的漏洞创建一个vulnerability节点
- **完整性检查**：对照ReAct输入中的工具调用序列，确保每个有意义的工具执行都被包含在攻击链中

### 第三步：构建逻辑关系（树状结构）
**重要：必须构建树状结构，而不是简单的线性链。**
按照因果关系连接节点，形成树状图（因为是单agent执行，所以可以不按照时间顺序）：
- **分支结构**：一个节点可以有多个后续节点（例如：端口扫描发现多个端口后，可以同时进行多个不同的测试）
- **汇聚结构**：多个节点可以指向同一个节点（例如：多个不同的测试都发现了同一个漏洞）
- 识别哪些action是基于前面action的结果而执行的
- 识别哪些vulnerability是由哪些action发现的
- 识别失败节点如何为后续成功提供线索
- **避免线性链**：不要将所有节点连成一条线，应该根据实际的并行测试和分支探索构建树状结构

### 第四步：优化和精简
- **完整性检查**：确保所有有意义的工具执行都被包含，不要遗漏关键步骤
- **合并规则**：只合并真正相似或重复的action节点（如多次相同工具的相似调用）
- **删除规则**：只删除完全无价值的失败节点（完全无输出、纯系统错误、重复的相同失败）
- **重要提醒**：宁可保留更多节点，也不要遗漏关键步骤。攻击链必须完整展现渗透测试过程
- 确保攻击链逻辑连贯，能够讲述完整故事

## 节点类型详解

### target（目标节点）
- **用途**：标识测试目标
- **创建规则**：每个独立目标（不同IP/域名）创建一个target节点
- **多目标处理**：不同目标的节点不相互连接，各自形成独立的子图
- **metadata.target**：精确记录目标标识（IP地址、域名、URL等）

### action（行动节点）
- **用途**：记录工具执行和AI分析结果
- **标签规则**：
  * 15-25个汉字，动宾结构
  * 成功节点：描述执行结果（如"扫描端口发现80/443/8080"、"目录扫描发现/admin路径"）
  * 失败节点：描述失败原因（如"尝试SQL注入（被WAF拦截）"、"端口扫描超时（目标不可达）"）
- **ai_analysis要求**：
  * 成功节点：总结工具执行的关键发现，说明这些发现的意义
  * 失败节点：必须说明失败原因、获得的线索、这些线索如何指引后续行动
  * 不超过150字，要具体、有信息量
- **findings要求**：
  * 提取工具返回结果中的关键信息点
  * 每个finding应该是独立的、有价值的信息片段
  * 成功节点：列出关键发现（如["80端口开放", "443端口开放", "HTTP服务为Apache 2.4"]）
  * 失败节点：列出失败线索（如["WAF拦截", "返回403", "检测到Cloudflare"]）
- **status标记**：
  * 成功节点：不设置或设为"success"
  * 提供线索的失败节点：必须设为"failed_insight"
- **risk_score**：始终为0（action节点不评估风险）

### vulnerability（漏洞节点）
- **用途**：记录真实确认的安全漏洞
- **创建规则**：
  * 必须是真实确认的漏洞，不是所有发现都是漏洞
  * 需要明确的漏洞证据（如SQL注入返回数据库错误、XSS成功执行等）
- **risk_score规则**：
  * critical（90-100）：可导致系统完全沦陷（RCE、SQL注入导致数据泄露等）
  * high（80-89）：可导致敏感信息泄露或权限提升
  * medium（60-79）：存在安全风险但影响有限
  * low（40-59）：轻微安全问题
- **metadata要求**：
  * vulnerability_type：漏洞类型（SQL注入、XSS、RCE等）
  * description：详细描述漏洞位置、原理、影响
  * severity：critical/high/medium/low
  * location：精确的漏洞位置（URL、参数、文件路径等）

## 节点过滤和合并规则

### 必须保留的失败节点
以下失败情况必须创建节点，因为它们提供了有价值的线索：
- 工具返回明确的错误信息（权限错误、连接拒绝、认证失败等）
- 超时或连接失败（可能表明防火墙、网络隔离等）
- WAF/防火墙拦截（返回403、406等，表明存在防护机制）
- 工具未安装或配置错误（但执行了调用）
- 目标不可达（DNS解析失败、网络不通等）

### 应该删除的失败节点
以下情况不应创建节点：
- 完全无输出的工具调用
- 纯系统错误（与目标无关，如本地环境问题）
- 重复的相同失败（多次相同错误只保留第一次）

### 节点合并规则
以下情况应合并节点：
- 同一工具的多次相似调用（如多次nmap扫描不同端口范围，合并为一个"端口扫描"节点）
- 同一目标的多个相似探测（如多个目录扫描工具，合并为一个"目录扫描"节点）

### 节点数量控制
- **完整性优先**：必须包含所有有意义的工具执行和关键步骤，不要为了控制数量而删除重要节点
- **建议范围**：单目标通常8-15个节点，但如果实际执行步骤较多，可以适当增加（最多20个节点）
- **优先保留**：关键成功步骤、提供线索的失败、发现的漏洞、重要的信息收集步骤
- **可以合并**：同一工具的多次相似调用（如多次nmap扫描不同端口范围，合并为一个"端口扫描"节点）
- **可以删除**：完全无输出的工具调用、纯系统错误、重复的相同失败（多次相同错误只保留第一次）
- **重要原则**：宁可节点稍多，也不要遗漏关键步骤。攻击链必须能够完整展现渗透测试的完整过程

## 边的类型和权重

### 边的类型
- **leads_to**：表示"导致"或"引导到"，用于action→action、target→action
  * 例如：端口扫描 → 目录扫描（因为发现了80端口，所以进行目录扫描）
- **discovers**：表示"发现"，**专门用于action→vulnerability**
  * 例如：SQL注入测试 → SQL注入漏洞
  * **重要**：所有action→vulnerability的边都必须使用discovers类型，即使多个action都指向同一个vulnerability，也应该统一使用discovers
- **enables**：表示"使能"或"促成"，**仅用于vulnerability→vulnerability、action→action（当后续行动依赖前面结果时）**
  * 例如：信息泄露漏洞 → 权限提升漏洞（通过信息泄露获得的信息促成了权限提升）
  * **重要**：enables不能用于action→vulnerability，action→vulnerability必须使用discovers

### 边的权重
- **权重1-2**：弱关联（如初步探测到进一步探测）
- **权重3-4**：中等关联（如发现端口到服务识别）
- **权重5-7**：强关联（如发现漏洞、关键信息泄露）
- **权重8-10**：极强关联（如漏洞利用成功、权限提升）

### DAG结构要求（有向无环图）
**关键：必须确保生成的是真正的DAG（有向无环图），不能有任何循环。**

- **节点编号规则**：节点id从"node_1"开始递增（node_1, node_2, node_3...）
- **边的方向规则**：所有边的source节点id必须严格小于target节点id（source < target），这是确保无环的关键
  * 例如：node_1 → node_2 ✓（正确）
  * 例如：node_2 → node_1 ✗（错误，会形成环）
  * 例如：node_3 → node_5 ✓（正确）
- **无环验证**：在输出JSON前，必须检查所有边，确保没有任何一条边的source >= target
- **无孤立节点**：确保每个节点至少有一条边连接（除了可能的根节点）
- **DAG结构特点**：
  * 一个节点可以有多个后续节点（分支），例如：node_2（端口扫描）可以同时连接到node_3、node_4、node_5等多个节点
  * 多个节点可以汇聚到一个节点（汇聚），例如：node_3、node_4、node_5都指向node_6（漏洞节点）
  * 避免将所有节点连成一条线，应该根据实际的并行测试和分支探索构建DAG结构
- **拓扑排序验证**：如果按照节点id从小到大排序，所有边都应该从左指向右（从上指向下），这样就能保证无环

## 攻击链逻辑连贯性要求

构建的攻击链应该能够回答以下问题：
1. **起点**：测试从哪里开始？（target节点）
2. **探索过程**：如何逐步收集信息？（action节点序列）
3. **失败与调整**：遇到障碍时如何调整策略？（failed_insight节点）
4. **关键发现**：发现了哪些重要信息？（action的findings）
5. **漏洞确认**：如何确认漏洞存在？（action→vulnerability）
6. **攻击路径**：完整的攻击路径是什么？（从target到vulnerability的路径）

## 当前任务 ReAct 轨迹（含工具执行；助手结论见轨迹末尾 assistant）

%s
%s

## 输出格式

严格按照以下JSON格式输出，不要添加任何其他文字：

**重要：示例展示的是树状结构，注意node_2（端口扫描）同时连接到多个后续节点（node_3、node_4），形成分支结构。**

{
   "nodes": [
     {
       "id": "node_1",
       "type": "target",
       "label": "测试目标: example.com",
       "risk_score": 40,
       "metadata": {
         "target": "example.com"
       }
     },
     {
       "id": "node_2",
       "type": "action",
       "label": "扫描端口发现80/443/8080",
       "risk_score": 0,
       "metadata": {
         "tool_name": "nmap",
         "tool_intent": "端口扫描",
         "ai_analysis": "使用nmap对目标进行端口扫描，发现80、443、8080端口开放。80端口运行HTTP服务，443端口运行HTTPS服务，8080端口可能为管理后台。这些开放端口为后续Web应用测试提供了入口。",
         "findings": ["80端口开放", "443端口开放", "8080端口开放", "HTTP服务为Apache 2.4"]
       }
     },
     {
       "id": "node_3",
       "type": "action",
       "label": "目录扫描发现/admin后台",
       "risk_score": 0,
       "metadata": {
         "tool_name": "dirsearch",
         "tool_intent": "目录扫描",
         "ai_analysis": "使用dirsearch对目标进行目录扫描，发现/admin目录存在且可访问。该目录可能为管理后台，是重要的测试目标。",
         "findings": ["/admin目录存在", "返回200状态码", "疑似管理后台"]
       }
     },
     {
       "id": "node_4",
       "type": "action",
       "label": "识别Web服务为Apache 2.4",
       "risk_score": 0,
       "metadata": {
         "tool_name": "whatweb",
         "tool_intent": "Web服务识别",
         "ai_analysis": "识别出目标运行Apache 2.4服务器，这为后续的漏洞测试提供了重要信息。",
         "findings": ["Apache 2.4", "PHP版本信息"]
       }
     },
     {
       "id": "node_5",
       "type": "action",
       "label": "尝试SQL注入（被WAF拦截）",
       "risk_score": 0,
       "metadata": {
         "tool_name": "sqlmap",
         "tool_intent": "SQL注入检测",
         "ai_analysis": "对/login.php进行SQL注入测试时被WAF拦截，返回403错误。错误信息显示检测到Cloudflare防护。这表明目标部署了WAF，需要调整测试策略。",
         "findings": ["WAF拦截", "返回403", "检测到Cloudflare", "目标部署WAF"],
         "status": "failed_insight"
       }
     },
     {
       "id": "node_6",
       "type": "vulnerability",
       "label": "SQL注入漏洞",
       "risk_score": 85,
       "metadata": {
         "vulnerability_type": "SQL注入",
         "description": "在/admin/login.php的username参数发现SQL注入漏洞，可通过注入payload绕过登录验证，直接获取管理员权限。漏洞返回数据库错误信息，确认存在注入点。",
         "severity": "high",
         "location": "/admin/login.php?username="
       }
     }
   ],
   "edges": [
     {
       "source": "node_1",
       "target": "node_2",
       "type": "leads_to",
       "weight": 3
     },
     {
       "source": "node_2",
       "target": "node_3",
       "type": "leads_to",
       "weight": 4
     },
     {
       "source": "node_2",
       "target": "node_4",
       "type": "leads_to",
       "weight": 3
     },
     {
       "source": "node_3",
       "target": "node_5",
       "type": "leads_to",
       "weight": 4
     },
     {
       "source": "node_5",
       "target": "node_6",
       "type": "discovers",
       "weight": 7
     }
   ]
}

## 重要提醒

1. **严禁杜撰**：只使用ReAct输入中实际执行的工具和实际返回的结果。如无实际数据，返回空的nodes和edges数组。
2. **DAG结构必须**：必须构建真正的DAG（有向无环图），不能有任何循环。所有边的source节点id必须严格小于target节点id（source < target）。
3. **拓扑顺序**：节点应该按照逻辑顺序编号，target节点通常是node_1，后续的action节点按执行顺序递增，vulnerability节点在最后。
4. **完整性优先**：必须包含所有有意义的工具执行和关键步骤，不要为了控制节点数量而删除重要节点。攻击链必须能够完整展现从目标识别到漏洞发现的完整过程。
5. **逻辑连贯**：确保攻击链能够讲述一个完整、连贯的渗透测试故事，包括所有关键步骤和决策点。
6. **教育价值**：优先保留有教育意义的节点，帮助学习者理解渗透测试思维和完整流程。
7. **准确性**：所有节点信息必须基于实际数据，不要推测或假设。
8. **完整性检查**：确保每个节点都有必要的metadata字段，每条边都有正确的source和target，没有孤立节点，没有循环。
9. **不要过度精简**：如果实际执行步骤较多，可以适当增加节点数量（最多20个），确保不遗漏关键步骤。
10. **输出前验证**：在输出JSON前，必须验证所有边都满足source < target的条件，确保DAG结构正确。

现在开始分析并构建攻击链：`, reactInput, assistantOutSection(modelOutput))
}

func assistantOutSection(modelOutput string) string {
	modelOutput = strings.TrimSpace(modelOutput)
	if modelOutput == "" {
		return ""
	}
	return "\n## Assistant Conclusion (supplementary)\n\n" + modelOutput + "\n"
}

// saveChain saves the attack chain to the database.
func (b *Builder) saveChain(conversationID string, nodes []Node, edges []Edge) error {
	// Delete old attack chain data first
	if err := b.db.DeleteAttackChain(conversationID); err != nil {
		b.logger.Warn("failed to delete old attack chain", zap.Error(err))
	}

	for _, node := range nodes {
		metadataJSON, _ := json.Marshal(node.Metadata)
		if err := b.db.SaveAttackChainNode(conversationID, node.ID, node.Type, node.Label, "", string(metadataJSON), node.RiskScore); err != nil {
		b.logger.Warn("failed to save attack chain node", zap.String("nodeId", node.ID), zap.Error(err))
		}
	}

	// Save edges
	for _, edge := range edges {
		if err := b.db.SaveAttackChainEdge(conversationID, edge.ID, edge.Source, edge.Target, edge.Type, edge.Weight); err != nil {
		b.logger.Warn("failed to save attack chain edge", zap.String("edgeId", edge.ID), zap.Error(err))
		}
	}

	return nil
}

// LoadChainFromDatabase loads an attack chain from the database.
func (b *Builder) LoadChainFromDatabase(conversationID string) (*Chain, error) {
	nodes, err := b.db.LoadAttackChainNodes(conversationID)
	if err != nil {
		return nil, fmt.Errorf("failed to load attack chain nodes: %w", err)
	}

	edges, err := b.db.LoadAttackChainEdges(conversationID)
	if err != nil {
		return nil, fmt.Errorf("failed to load attack chain edges: %w", err)
	}

	return &Chain{
		Nodes: nodes,
		Edges: edges,
	}, nil
}

// callAIForChainGeneration calls AI to generate an attack chain.
func (b *Builder) callAIForChainGeneration(ctx context.Context, prompt string) (string, error) {
	requestBody := map[string]interface{}{
		"model": b.openAIConfig.Model,
		"messages": []map[string]interface{}{
			{
				"role":    "system",
				"content": "You are a professional security testing analyst skilled in building attack chain graphs. Please return attack chain data strictly in JSON format.",
			},
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"temperature":           0.3,
		"max_completion_tokens": attackChainMaxCompletionTokens(b.maxTokens),
	}

	var apiResponse struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if b.openAIClient == nil {
		return "", fmt.Errorf("OpenAI client not initialized")
	}
	if err := b.openAIClient.ChatCompletion(ctx, requestBody, &apiResponse); err != nil {
		var apiErr *openai.APIError
		if errors.As(err, &apiErr) {
			bodyStr := strings.ToLower(apiErr.Body)
			if strings.Contains(bodyStr, "context") || strings.Contains(bodyStr, "length") || strings.Contains(bodyStr, "too long") {
				return "", fmt.Errorf("context length exceeded")
			}
		} else if strings.Contains(strings.ToLower(err.Error()), "context") || strings.Contains(strings.ToLower(err.Error()), "length") {
			return "", fmt.Errorf("context length exceeded")
		}
		return "", fmt.Errorf("request failed: %w", err)
	}

	if len(apiResponse.Choices) == 0 {
		return "", fmt.Errorf("API returned no valid response")
	}

	content := strings.TrimSpace(apiResponse.Choices[0].Message.Content)
	// Try to extract JSON (may be wrapped in markdown code blocks)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	return content, nil
}

// ChainJSON is the attack chain JSON structure.
type ChainJSON struct {
	Nodes []struct {
		ID        string                 `json:"id"`
		Type      string                 `json:"type"`
		Label     string                 `json:"label"`
		RiskScore int                    `json:"risk_score"`
		Metadata  map[string]interface{} `json:"metadata"`
	} `json:"nodes"`
	Edges []struct {
		Source string `json:"source"`
		Target string `json:"target"`
		Type   string `json:"type"`
		Weight int    `json:"weight"`
	} `json:"edges"`
}

// parseChainJSON parses attack chain JSON.
func (b *Builder) parseChainJSON(chainJSON string) (*Chain, error) {
	var chainData ChainJSON
	if err := json.Unmarshal([]byte(chainJSON), &chainData); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Create node ID mapping (AI-returned ID -> new UUID)
	nodeIDMap := make(map[string]string)

	// Convert to Chain structure
	nodes := make([]Node, 0, len(chainData.Nodes))
	for _, n := range chainData.Nodes {
		// Generate new UUID node ID
		newNodeID := fmt.Sprintf("node_%s", uuid.New().String())
		nodeIDMap[n.ID] = newNodeID

		node := Node{
			ID:        newNodeID,
			Type:      n.Type,
			Label:     n.Label,
			RiskScore: n.RiskScore,
			Metadata:  n.Metadata,
		}
		if node.Metadata == nil {
			node.Metadata = make(map[string]interface{})
		}
		nodes = append(nodes, node)
	}

	// Convert edges
	edges := make([]Edge, 0, len(chainData.Edges))
	for _, e := range chainData.Edges {
		sourceID, ok := nodeIDMap[e.Source]
		if !ok {
			continue
		}
		targetID, ok := nodeIDMap[e.Target]
		if !ok {
			continue
		}

		// Generate edge ID (required by frontend)
		edgeID := fmt.Sprintf("edge_%s", uuid.New().String())

		edges = append(edges, Edge{
			ID:     edgeID,
			Source: sourceID,
			Target: targetID,
			Type:   e.Type,
			Weight: e.Weight,
		})
	}

	return &Chain{
		Nodes: nodes,
		Edges: edges,
	}, nil
}

// All methods below are no longer used and have been removed to simplify the code.
