package multiagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/einomcp"
	"cyberstrike-ai/internal/einoobserve"
	"cyberstrike-ai/internal/openai"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// normalizeStreamingDelta normalizes chunks that may contain accumulated text into pure deltas.
// Some models or bridge layers resend already emitted prefixes while streaming; if the frontend
// directly appends each chunk, duplicated text appears.
//
// Note: keep this consistent with internal/openai.normalizeStreamingDelta.
func normalizeStreamingDelta(current, incoming string) (next, delta string) {
	if incoming == "" {
		return current, ""
	}
	if current == "" {
		return incoming, incoming
	}
	if strings.HasPrefix(incoming, current) && len(incoming) > len(current) {
		return incoming, incoming[len(current):]
	}
	if incoming == current && utf8.RuneCountInString(current) > 1 {
		return current, ""
	}
	return current + incoming, incoming
}

func isInterruptContinue(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	return errors.Is(context.Cause(ctx), ErrInterruptContinue)
}

func isEinoIterationLimitError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if msg == "" {
		return false
	}
	return strings.Contains(msg, "max iteration") ||
		strings.Contains(msg, "maximum iteration") ||
		strings.Contains(msg, "maximum iterations") ||
		strings.Contains(msg, "iteration limit") ||
		strings.Contains(msg, "\u8fbe\u5230\u6700\u5927\u8fed\u4ee3")
}

// einoADKRunLoopArgs factors the Eino adk.Runner event loop out of RunDeepAgent / RunEinoSingleChatModelAgent for reuse.
type einoADKRunLoopArgs struct {
	OrchMode             string
	OrchestratorName     string
	ConversationID       string
	Progress             func(eventType, message string, data interface{})
	Logger               *zap.Logger
	SnapshotMCPIDs       func() []string
	StreamsMainAssistant func(agent string) bool
	EinoRoleTag          func(agent string) string
	CheckpointDir        string
	// RunRetryMaxAttempts / RunRetryMaxBackoffSec: exponential-backoff resume for 429, 5xx, and network jitter (0 = default 10 attempts / 30s cap).
	RunRetryMaxAttempts   int
	RunRetryMaxBackoffSec int

	McpIDsMu *sync.Mutex
	McpIDs   *[]string

	// When FilesystemMonitorAgent / FilesystemMonitorRecord are non-nil, record completed Eino ADK filesystem middleware tools
	// (ls/read_file/write_file/edit_file/glob/grep) in MCP monitoring; execute is already recorded by eino_execute_monitor, so skip it here.
	FilesystemMonitorAgent  *agent.Agent
	FilesystemMonitorRecord einomcp.ExecutionRecorder

	// ToolInvokeNotify is shared with einomcp.ToolsFromDefinitions: the run loop sets it before iteration, and the MCP bridge fires it to complete tool_result.
	ToolInvokeNotify *einomcp.ToolInvokeNotifyHolder

	DA adk.Agent

	// EmptyResponseMessage is the placeholder used when no assistant body is captured; multi-agent and single-agent text differs.
	EmptyResponseMessage string

	// ModelFacingTrace is optional: middleware at the end of each ChatModelAgent Handlers chain writes the message snapshot about to be sent to the model.
	// When non-empty, it is preferred for LastAgentTraceInput serialization so resumed runs match context after summarization/reduction.
	ModelFacingTrace *modelFacingTraceHolder

	// EinoCallbacks optionally injects Eino callbacks into ADK Runner for full-path observability; see internal/einoobserve.
	EinoCallbacks *config.MultiAgentEinoCallbacksConfig
}

func runEinoADKAgentLoop(ctx context.Context, args *einoADKRunLoopArgs, baseMsgs []adk.Message) (*RunResult, error) {
	if args == nil || args.DA == nil {
		return nil, fmt.Errorf("eino run loop: args or Agent is nil")
	}
	if args.McpIDs == nil {
		s := []string{}
		args.McpIDs = &s
	}
	if args.McpIDsMu == nil {
		args.McpIDsMu = &sync.Mutex{}
	}

	orchMode := args.OrchMode
	orchestratorName := args.OrchestratorName
	conversationID := args.ConversationID
	progress := args.Progress
	logger := args.Logger
	snapshotMCPIDs := args.SnapshotMCPIDs
	if snapshotMCPIDs == nil {
		snapshotMCPIDs = func() []string { return nil }
	}
	streamsMainAssistant := args.StreamsMainAssistant
	if streamsMainAssistant == nil {
		streamsMainAssistant = func(agent string) bool {
			return agent == "" || agent == orchestratorName
		}
	}
	einoRoleTag := args.EinoRoleTag
	if einoRoleTag == nil {
		einoRoleTag = func(agent string) string {
			if streamsMainAssistant(agent) {
				return "orchestrator"
			}
			return "sub"
		}
	}
	da := args.DA
	mcpIDsMu := args.McpIDsMu
	mcpIDs := args.McpIDs

	// Panic recovery prevents an internal Eino framework panic from crashing the goroutine and leaving the connection open.
	defer func() {
		if r := recover(); r != nil {
			if logger != nil {
				logger.Error("eino runner panic recovered", zap.Any("recover", r), zap.Stack("stack"))
			}
			if progress != nil {
				progress("error", fmt.Sprintf("Internal error: %v", r), map[string]interface{}{
					"conversationId": conversationID,
					"source":         "eino",
				})
			}
		}
	}()

	var lastAssistant string
	var lastPlanExecuteExecutor string
	msgs := append([]adk.Message(nil), baseMsgs...)
	runAccumulatedMsgs := append([]adk.Message(nil), msgs...)
	baseAccumulatedCount := len(runAccumulatedMsgs)

	emptyHint := strings.TrimSpace(args.EmptyResponseMessage)
	if emptyHint == "" {
		emptyHint = "(Eino session completed but no assistant text was captured. Check process details or logs.)"
	}

	lastAssistant = ""
	lastPlanExecuteExecutor = ""
	var reasoningStreamSeq int64
	var einoSubReplyStreamSeq int64
	var mainResponseStreamSeq int64
	toolEmitSeen := make(map[string]struct{})
	var einoMainRound int
	var einoLastAgent string
	subAgentToolStep := make(map[string]int)
	// mainAgentToolStep increments on each main-agent tool-call batch for the UI round display; without sub-agent switches, single-agent runs used to stay at round 1.
	mainAgentToolStep := make(map[string]int)
	pendingByID := make(map[string]toolCallPendingInfo)
	pendingQueueByAgent := make(map[string][]string)
	var pendingMu sync.Mutex
	markPending := func(tc toolCallPendingInfo) {
		if tc.ToolCallID == "" {
			return
		}
		pendingMu.Lock()
		defer pendingMu.Unlock()
		pendingByID[tc.ToolCallID] = tc
		pendingQueueByAgent[tc.EinoAgent] = append(pendingQueueByAgent[tc.EinoAgent], tc.ToolCallID)
	}
	popNextPendingForAgent := func(agentName string) (toolCallPendingInfo, bool) {
		pendingMu.Lock()
		defer pendingMu.Unlock()
		q := pendingQueueByAgent[agentName]
		for len(q) > 0 {
			id := q[0]
			q = q[1:]
			pendingQueueByAgent[agentName] = q
			if tc, ok := pendingByID[id]; ok {
				delete(pendingByID, id)
				return tc, true
			}
		}
		return toolCallPendingInfo{}, false
	}
	removePendingByID := func(toolCallID string) {
		if toolCallID == "" {
			return
		}
		pendingMu.Lock()
		defer pendingMu.Unlock()
		delete(pendingByID, toolCallID)
	}
	popAnyPending := func() (toolCallPendingInfo, bool) {
		pendingMu.Lock()
		defer pendingMu.Unlock()
		for id, tc := range pendingByID {
			delete(pendingByID, id)
			return tc, true
		}
		return toolCallPendingInfo{}, false
	}
	pendingCount := func() int {
		pendingMu.Lock()
		defer pendingMu.Unlock()
		return len(pendingByID)
	}
	flushAllPendingAsFailed := func(err error) {
		pendingMu.Lock()
		pendingSnapshot := make([]toolCallPendingInfo, 0, len(pendingByID))
		for _, tc := range pendingByID {
			pendingSnapshot = append(pendingSnapshot, tc)
		}
		pendingByID = make(map[string]toolCallPendingInfo)
		pendingQueueByAgent = make(map[string][]string)
		pendingMu.Unlock()

		if progress == nil {
			return
		}
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		for _, tc := range pendingSnapshot {
			toolName := tc.ToolName
			if strings.TrimSpace(toolName) == "" {
				toolName = "unknown"
			}
			progress("tool_result", fmt.Sprintf("Tool result (%s)", toolName), map[string]interface{}{
				"toolName":       toolName,
				"success":        false,
				"isError":        true,
				"result":         msg,
				"resultPreview":  msg,
				"toolCallId":     tc.ToolCallID,
				"conversationId": conversationID,
				"einoAgent":      tc.EinoAgent,
				"einoRole":       tc.EinoRole,
				"source":         "eino",
			})
		}
	}

	// Trimmed stdout from the most recent successful Eino filesystem execute, used to suppress duplicate assistant timeline output when the model immediately repeats it.
	var executeStdoutDupMu sync.Mutex
	var pendingExecuteStdoutDup string
	recordPendingExecuteStdoutDup := func(toolName, stdout string, isErr bool) {
		if isErr || !strings.EqualFold(strings.TrimSpace(toolName), "execute") {
			return
		}
		t := strings.TrimSpace(stdout)
		if t == "" {
			return
		}
		executeStdoutDupMu.Lock()
		pendingExecuteStdoutDup = t
		executeStdoutDupMu.Unlock()
	}

	var toolResultSent sync.Map // toolCallID -> struct{}; deduplicates ADK Tool messages so bridge and event stream do not both push them
	if args.ToolInvokeNotify != nil {
		args.ToolInvokeNotify.Set(func(toolCallID, toolName, einoAgent string, success bool, content string, invokeErr error) {
			tid := strings.TrimSpace(toolCallID)
			removePendingByID(tid)
			if tid == "" || progress == nil {
				return
			}
			if _, loaded := toolResultSent.LoadOrStore(tid, struct{}{}); loaded {
				return
			}
			isErr := !success || invokeErr != nil
			body := content
			if invokeErr != nil {
				// Preserve already-streamed stdout, such as partial execute output before a timeout, so tool_result does not contain only the error string and lose context for the model/UI.
				tail := friendlyEinoExecuteInvokeTail(invokeErr)
				// The execute streaming wrapper may already have written the timeout sentence into content for ADK tool messages and streaming deltas; do not append it twice.
				if tail != "" && strings.Contains(content, tail) {
					body = content
				} else if strings.TrimSpace(content) != "" {
					body = strings.TrimRight(content, "\n") + "\n\n" + tail
				} else {
					body = tail
				}
				isErr = true
			}
			recordPendingExecuteStdoutDup(toolName, body, isErr)
			preview := body
			if len(preview) > 200 {
				preview = preview[:200] + "..."
			}
			agentTag := strings.TrimSpace(einoAgent)
			if agentTag == "" {
				agentTag = orchestratorName
			}
			progress("tool_result", fmt.Sprintf("Tool result (%s)", toolName), map[string]interface{}{
				"toolName":       toolName,
				"success":        !isErr,
				"isError":        isErr,
				"result":         body,
				"resultPreview":  preview,
				"toolCallId":     tid,
				"conversationId": conversationID,
				"einoAgent":      agentTag,
				"einoRole":       einoRoleTag(agentTag),
				"source":         "eino",
			})
		})
	}

	if args.EinoCallbacks != nil {
		ctx = einoobserve.AttachAgentRunCallbacks(ctx, args.EinoCallbacks, einoobserve.Params{
			Logger:           logger,
			Progress:         progress,
			ConversationID:   conversationID,
			OrchMode:         orchMode,
			OrchestratorName: orchestratorName,
		})
	}

	runnerCfg := adk.RunnerConfig{
		Agent: da,
		// Enable ADK streaming events so plan_execute also emits reasoning/response streams,
		// matching the frontend experience of deep/supervisor/eino_single.
		EnableStreaming: true,
	}
	var cpStore *fileCheckPointStore
	var checkPointID string
	if cp := strings.TrimSpace(args.CheckpointDir); cp != "" {
		cpDir := filepath.Join(cp, sanitizeEinoPathSegment(conversationID))
		st, stErr := newFileCheckPointStore(cpDir)
		if stErr != nil {
			if logger != nil {
				logger.Warn("eino checkpoint store disabled", zap.String("dir", cpDir), zap.Error(stErr))
			}
		} else {
			cpStore = st
			checkPointID = buildEinoCheckpointID(orchMode)
			runnerCfg.CheckPointStore = st
			if logger != nil {
				logger.Info("eino runner: checkpoint store enabled",
					zap.String("dir", cpDir),
					zap.String("checkPointID", checkPointID))
			}
		}
	}
	runner := adk.NewRunner(ctx, runnerCfg)
	var iter *adk.AsyncIterator[*adk.AgentEvent]
	if cpStore != nil && checkPointID != "" {
		if _, existed, getErr := cpStore.Get(ctx, checkPointID); getErr != nil {
			if logger != nil {
				logger.Warn("eino checkpoint preflight get failed", zap.String("checkPointID", checkPointID), zap.Error(getErr))
			}
		} else if existed {
			if progress != nil {
				progress("progress", "Checkpoint detected; resuming execution from the interrupted node...", map[string]interface{}{
					"conversationId": conversationID,
					"source":         "eino",
					"orchestration":  orchMode,
					"checkPointID":   checkPointID,
				})
			}
			if logger != nil {
				logger.Info("eino runner: resume from checkpoint", zap.String("checkPointID", checkPointID))
			}
			resumeIter, resumeErr := runner.Resume(ctx, checkPointID)
			if resumeErr == nil {
				iter = resumeIter
			} else {
				if logger != nil {
					logger.Warn("eino runner: resume failed, fallback to fresh run",
						zap.String("checkPointID", checkPointID),
						zap.Error(resumeErr))
				}
				if progress != nil {
					progress("progress", "Checkpoint resume failed; falling back to a fresh run.", map[string]interface{}{
						"conversationId": conversationID,
						"source":         "eino",
						"orchestration":  orchMode,
						"checkPointID":   checkPointID,
					})
				}
			}
		}
	}
	if iter == nil {
		if checkPointID != "" {
			iter = runner.Run(ctx, msgs, adk.WithCheckPointID(checkPointID))
		} else {
			iter = runner.Run(ctx, msgs)
		}
	}
	handleRunErr := func(runErr error) error {
		if runErr == nil {
			return nil
		}
		if errors.Is(runErr, context.DeadlineExceeded) {
			flushAllPendingAsFailed(runErr)
			if progress != nil {
				progress("error", runErr.Error(), map[string]interface{}{
					"conversationId": conversationID,
					"source":         "eino",
					"errorKind":      "timeout",
				})
			}
			return runErr
		}
		// context.Canceled is the only error that should directly terminate orchestration, such as closing the page or explicit stop.
		if errors.Is(runErr, context.Canceled) {
			flushAllPendingAsFailed(runErr)
			if progress != nil {
				progress("error", runErr.Error(), map[string]interface{}{
					"conversationId": conversationID,
					"source":         "eino",
				})
			}
			return runErr
		}
		if isEinoIterationLimitError(runErr) {
			flushAllPendingAsFailed(runErr)
			if progress != nil {
				progress("iteration_limit_reached", runErr.Error(), map[string]interface{}{
					"conversationId": conversationID,
					"source":         "eino",
					"orchestration":  orchMode,
				})
				progress("error", runErr.Error(), map[string]interface{}{
					"conversationId": conversationID,
					"source":         "eino",
					"errorKind":      "iteration_limit",
				})
			}
			return runErr
		}
		flushAllPendingAsFailed(runErr)
		if progress != nil {
			progress("error", runErr.Error(), map[string]interface{}{
				"conversationId": conversationID,
				"source":         "eino",
			})
		}
		return runErr
	}

	// maybeRetryTransientRun does not call runner.Run/Resume here; the handler persists state and resumes via loadHistoryFromAgentTrace in a segmented run, matching interrupt-and-continue.
	maybeRetryTransientRun := func(runErr error) (retry bool, fatal error) {
		if runErr == nil || !isEinoTransientRunError(runErr) {
			return false, handleRunErr(runErr)
		}
		if logger != nil {
			logger.Warn("eino transient error, ending run segment for handler resume",
				zap.Error(runErr),
				zap.String("orchestration", orchMode))
		}
		if progress != nil {
			progress("eino_run_retry", "Temporary error detected (rate limiting or network instability); saving context and retrying...", map[string]interface{}{
				"conversationId": conversationID,
				"source":         "eino",
				"orchestration":  orchMode,
				"error":          runErr.Error(),
				"resumeKind":     "trace_segment",
			})
		}
		return false, ErrTransientRetryContinue
	}

	takePartial := func(runErr error) (*RunResult, error) {
		if len(runAccumulatedMsgs) <= baseAccumulatedCount {
			return nil, runErr
		}
		ids := snapshotMCPIDs()
		return buildEinoRunResultFromAccumulated(
			orchMode, runAccumulatedMsgs, persistTraceSource(args, runAccumulatedMsgs),
			lastAssistant, lastPlanExecuteExecutor, emptyHint, ids, true,
		), runErr
	}

	for {
		// Detect context cancellation, such as browser close or request timeout, and flush pending tool state so the UI does not remain stuck on "running".
		select {
		case <-ctx.Done():
			flushAllPendingAsFailed(ctx.Err())
			if progress != nil {
				if isInterruptContinue(ctx) {
					progress("progress", "Paused current output; merging the user supplement and continuing...", map[string]interface{}{
						"conversationId": conversationID,
						"source":         "eino",
						"kind":           "interrupt_continue",
					})
				} else {
					progress("error", "Request cancelled", map[string]interface{}{
						"conversationId": conversationID,
						"source":         "eino",
					})
				}
			}
			return takePartial(ctx.Err())
		default:
		}

		ev, ok := iter.Next()
		if !ok {
			// Iterator end does not always mean normal completion.
			// If cancellation or timeout happens while iter.Next() is blocked, it may return !ok directly.
			// Keep the checkpoint so a later resume is not misclassified as "no checkpoint" and rerun from scratch.
			if ctxErr := ctx.Err(); ctxErr != nil {
				flushAllPendingAsFailed(ctxErr)
				if progress != nil {
					if isInterruptContinue(ctx) {
						progress("progress", "Paused current output; merging the user supplement and continuing...", map[string]interface{}{
							"conversationId": conversationID,
							"source":         "eino",
							"kind":           "interrupt_continue",
						})
					} else {
						progress("error", ctxErr.Error(), map[string]interface{}{
							"conversationId": conversationID,
							"source":         "eino",
						})
					}
				}
				return takePartial(ctxErr)
			}
			if orphanCount := pendingCount(); orphanCount > 0 {
				flushAllPendingAsFailed(errors.New("pending tool call missing result before run completion"))
				if progress != nil {
					progress("eino_pending_orphaned", "pending tool calls were force-closed at run end", map[string]interface{}{
						"conversationId": conversationID,
						"source":         "eino",
						"orchestration":  orchMode,
						"pendingCount":   orphanCount,
					})
				}
			}
			if cpStore != nil && checkPointID != "" {
				if p, pErr := cpStore.path(checkPointID); pErr == nil {
					if rmErr := os.Remove(p); rmErr != nil && !os.IsNotExist(rmErr) && logger != nil {
						logger.Warn("eino checkpoint cleanup failed", zap.String("path", p), zap.Error(rmErr))
					}
				}
			}
			break
		}
		if ev == nil {
			continue
		}
		if ev.Err != nil {
			if _, retErr := maybeRetryTransientRun(ev.Err); retErr != nil {
				return takePartial(retErr)
			}
		}
		if ev.AgentName != "" && progress != nil {
			iterEinoAgent := orchestratorName
			if orchMode == "plan_execute" {
				if a := strings.TrimSpace(ev.AgentName); a != "" {
					iterEinoAgent = a
				}
			}
			if streamsMainAssistant(ev.AgentName) {
				mainIterKey := einoMainIterationKey(iterEinoAgent, orchestratorName)
				if einoMainRound == 0 {
					einoMainRound = 1
					mainAgentToolStep[mainIterKey] = 1
					progress("iteration", "", map[string]interface{}{
						"iteration":      1,
						"einoScope":      "main",
						"einoRole":       "orchestrator",
						"einoAgent":      iterEinoAgent,
						"orchestration":  orchMode,
						"conversationId": conversationID,
						"source":         "eino",
					})
				} else if einoLastAgent != "" {
					needBump := false
					if !streamsMainAssistant(einoLastAgent) {
						needBump = true // sub-agent -> main agent
					} else if einoLastAgent != ev.AgentName {
						needBump = true // plan_execute: main-agent switch such as planner <-> executor
					}
					if needBump {
						einoMainRound++
						mainAgentToolStep[mainIterKey] = einoMainRound
						progress("iteration", "", map[string]interface{}{
							"iteration":      einoMainRound,
							"einoScope":      "main",
							"einoRole":       "orchestrator",
							"einoAgent":      iterEinoAgent,
							"orchestration":  orchMode,
							"conversationId": conversationID,
							"source":         "eino",
						})
					}
				}
			}
			einoLastAgent = ev.AgentName
			progress("progress", fmt.Sprintf("[Eino] %s", ev.AgentName), map[string]interface{}{
				"conversationId": conversationID,
				"einoAgent":      ev.AgentName,
				"einoRole":       einoRoleTag(ev.AgentName),
				"orchestration":  orchMode,
			})
		}
		if ev.Output == nil || ev.Output.MessageOutput == nil {
			continue
		}
		mv := ev.Output.MessageOutput

		if mv.IsStreaming && mv.MessageStream != nil {
			mainStreamID := fmt.Sprintf("eino-main-%s-%d", conversationID, atomic.AddInt64(&mainResponseStreamSeq, 1))
			streamHeaderSent := false
			var reasoningStreamID string
			var toolStreamFragments []schema.ToolCall
			var subAssistantBuf string
			var subReplyStreamID string
			var mainAssistantBuf string
			// Body already sent to the frontend through response_delta, accumulated consistently with monitor.js normalizeStreamingDeltaJs.
			var mainAssistWireAccum string
			var mainAssistDupTarget string // non-empty means buffer this main-assistant segment until EOF, then deduplicate against execute output
			var reasoningBuf string
			var prevReasoningDisplay string // UI display: accumulated reasoning after stripping Claude internal signature suffixes
			var streamRecvErr error
			type streamMsg struct {
				chunk *schema.Message
				err   error
			}
			recvCh := make(chan streamMsg, 8)
			go func() {
				defer close(recvCh)
				for {
					ch, rerr := mv.MessageStream.Recv()
					recvCh <- streamMsg{chunk: ch, err: rerr}
					if rerr != nil {
						return
					}
				}
			}()
		streamRecvLoop:
			for {
				select {
				case <-ctx.Done():
					streamRecvErr = ctx.Err()
					break streamRecvLoop
				case sm, ok := <-recvCh:
					if !ok {
						break streamRecvLoop
					}
					chunk, rerr := sm.chunk, sm.err
					if rerr != nil {
						if errors.Is(rerr, io.EOF) {
							break streamRecvLoop
						}
						if logger != nil {
							logger.Warn("eino stream recv error, flushing incomplete stream",
								zap.Error(rerr),
								zap.String("agent", ev.AgentName),
								zap.Int("toolFragments", len(toolStreamFragments)))
						}
						streamRecvErr = rerr
						break streamRecvLoop
					}
					if chunk == nil {
						continue
					}
					if progress != nil && strings.TrimSpace(chunk.ReasoningContent) != "" {
						var reasoningDelta string
						reasoningBuf, reasoningDelta = normalizeStreamingDelta(reasoningBuf, chunk.ReasoningContent)
						if reasoningDelta != "" {
							fullDisplay := openai.DisplayReasoningContent(reasoningBuf)
							var displayDelta string
							if strings.HasPrefix(fullDisplay, prevReasoningDisplay) {
								displayDelta = fullDisplay[len(prevReasoningDisplay):]
							} else {
								displayDelta = fullDisplay
							}
							prevReasoningDisplay = fullDisplay
							if displayDelta != "" {
								if reasoningStreamID == "" {
									reasoningStreamID = fmt.Sprintf("eino-reasoning-%s-%d", conversationID, atomic.AddInt64(&reasoningStreamSeq, 1))
									progress("reasoning_chain_stream_start", " ", map[string]interface{}{
										"streamId":      reasoningStreamID,
										"source":        "eino",
										"einoAgent":     ev.AgentName,
										"einoRole":      einoRoleTag(ev.AgentName),
										"orchestration": orchMode,
									})
								}
								progress("reasoning_chain_stream_delta", displayDelta, openai.WithSSEAccumulated(map[string]interface{}{
									"streamId": reasoningStreamID,
								}, fullDisplay))
							}
						}
					}
					if chunk.Content != "" {
						if progress != nil && streamsMainAssistant(ev.AgentName) {
							var contentDelta string
							mainAssistantBuf, contentDelta = normalizeStreamingDelta(mainAssistantBuf, chunk.Content)
							if contentDelta != "" {
								if mainAssistDupTarget == "" {
									executeStdoutDupMu.Lock()
									if pendingExecuteStdoutDup != "" {
										mainAssistDupTarget = pendingExecuteStdoutDup
									}
									executeStdoutDupMu.Unlock()
								}
								if mainAssistDupTarget != "" {
									// tool_result has already been shown; buffer the full text and suppress the assistant stream at EOF if it matches execute output.
								} else {
									if !streamHeaderSent {
										progress("response_start", "", map[string]interface{}{
											"conversationId":     conversationID,
											"mcpExecutionIds":    snapshotMCPIDs(),
											"messageGeneratedBy": "eino:" + ev.AgentName,
											"einoRole":           "orchestrator",
											"einoAgent":          ev.AgentName,
											"orchestration":      orchMode,
											"iteration":          einoMainRound,
											"streamId":           mainStreamID,
										})
										streamHeaderSent = true
									}
									progress("response_delta", contentDelta, openai.WithSSEAccumulated(map[string]interface{}{
										"conversationId":  conversationID,
										"mcpExecutionIds": snapshotMCPIDs(),
										"einoRole":        "orchestrator",
										"einoAgent":       ev.AgentName,
										"orchestration":   orchMode,
										"iteration":     einoMainRound,
										"streamId":        mainStreamID,
									}, mainAssistantBuf))
									mainAssistWireAccum, _ = normalizeStreamingDelta(mainAssistWireAccum, contentDelta)
								}
							}
						} else if !streamsMainAssistant(ev.AgentName) {
							var subDelta string
							subAssistantBuf, subDelta = normalizeStreamingDelta(subAssistantBuf, chunk.Content)
							if subDelta != "" {
								if progress != nil {
									if subReplyStreamID == "" {
										subReplyStreamID = fmt.Sprintf("eino-sub-reply-%s-%d", conversationID, atomic.AddInt64(&einoSubReplyStreamSeq, 1))
										progress("eino_agent_reply_stream_start", "", map[string]interface{}{
											"streamId":       subReplyStreamID,
											"einoAgent":      ev.AgentName,
											"einoRole":       "sub",
											"conversationId": conversationID,
											"source":         "eino",
										})
									}
									progress("eino_agent_reply_stream_delta", subDelta, openai.WithSSEAccumulated(map[string]interface{}{
										"streamId":       subReplyStreamID,
										"conversationId": conversationID,
									}, subAssistantBuf))
								}
							}
						}
					}
					if len(chunk.ToolCalls) > 0 {
						toolStreamFragments = append(toolStreamFragments, chunk.ToolCalls...)
					}
				}
			}
			if streamsMainAssistant(ev.AgentName) {
				s := strings.TrimSpace(mainAssistantBuf)
				if mainAssistDupTarget != "" {
					executeStdoutDupMu.Lock()
					pendingExecuteStdoutDup = ""
					executeStdoutDupMu.Unlock()
					if s != "" && s == mainAssistDupTarget {
						// Exactly matches the execute result just shown: do not emit assistant stream events, but still write trace and final reply fields.
						lastAssistant = s
						runAccumulatedMsgs = append(runAccumulatedMsgs, schema.AssistantMessage(s, nil))
						if orchMode == "plan_execute" && strings.EqualFold(strings.TrimSpace(ev.AgentName), "executor") {
							lastPlanExecuteExecutor = UnwrapPlanExecuteUserText(s)
						}
					} else if s != "" {
						if progress != nil {
							// Compare with execute using TrimSpace only; the UI must receive mainAssistantBuf,
							// otherwise trailing whitespace/newline mismatches can make frontend normalize append duplicated text.
							_, eofTail := normalizeStreamingDelta(mainAssistWireAccum, mainAssistantBuf)
							if eofTail != "" {
								if !streamHeaderSent {
									progress("response_start", "", map[string]interface{}{
										"conversationId":     conversationID,
										"mcpExecutionIds":    snapshotMCPIDs(),
										"messageGeneratedBy": "eino:" + ev.AgentName,
										"einoRole":           "orchestrator",
										"einoAgent":          ev.AgentName,
										"orchestration":      orchMode,
										"iteration":          einoMainRound,
										"streamId":           mainStreamID,
									})
								}
								progress("response_delta", eofTail, openai.WithSSEAccumulated(map[string]interface{}{
									"conversationId":  conversationID,
									"mcpExecutionIds": snapshotMCPIDs(),
									"einoRole":        "orchestrator",
									"einoAgent":       ev.AgentName,
									"orchestration":   orchMode,
									"iteration":       einoMainRound,
									"streamId":        mainStreamID,
								}, mainAssistantBuf))
								mainAssistWireAccum, _ = normalizeStreamingDelta(mainAssistWireAccum, eofTail)
							}
						}
						lastAssistant = s
						runAccumulatedMsgs = append(runAccumulatedMsgs, schema.AssistantMessage(s, nil))
						if orchMode == "plan_execute" && strings.EqualFold(strings.TrimSpace(ev.AgentName), "executor") {
							lastPlanExecuteExecutor = UnwrapPlanExecuteUserText(s)
						}
					}
				} else if s != "" {
					lastAssistant = s
					runAccumulatedMsgs = append(runAccumulatedMsgs, schema.AssistantMessage(s, nil))
					if orchMode == "plan_execute" && strings.EqualFold(strings.TrimSpace(ev.AgentName), "executor") {
						lastPlanExecuteExecutor = UnwrapPlanExecuteUserText(s)
					}
				}
			}
			if strings.TrimSpace(subAssistantBuf) != "" && progress != nil {
				if s := strings.TrimSpace(subAssistantBuf); s != "" {
					if subReplyStreamID != "" {
						progress("eino_agent_reply_stream_end", s, map[string]interface{}{
							"streamId":       subReplyStreamID,
							"einoAgent":      ev.AgentName,
							"einoRole":       "sub",
							"conversationId": conversationID,
							"source":         "eino",
						})
					} else {
						progress("eino_agent_reply", s, map[string]interface{}{
							"conversationId": conversationID,
							"einoAgent":      ev.AgentName,
							"einoRole":       "sub",
							"source":         "eino",
						})
					}
				}
			}
			var lastToolChunk *schema.Message
			if merged := mergeStreamingToolCallFragments(toolStreamFragments); len(merged) > 0 {
				lastToolChunk = mergeMessageToolCalls(&schema.Message{ToolCalls: merged})
			}
			tryEmitToolCallsOnce(lastToolChunk, ev.AgentName, orchestratorName, conversationID, orchMode, progress, toolEmitSeen, subAgentToolStep, mainAgentToolStep, markPending)
			// The streaming path previously sent tool_calls only to the progress UI and did not append them to runAccumulatedMsgs; after persistence, loadHistory -> RepairOrphan deleted all tool results, causing resumed/next rounds to forget them.
			if lastToolChunk != nil && len(lastToolChunk.ToolCalls) > 0 {
				runAccumulatedMsgs = append(runAccumulatedMsgs, schema.AssistantMessage("", lastToolChunk.ToolCalls))
			}
			if streamRecvErr != nil {
				if isInterruptContinue(ctx) {
					return takePartial(streamRecvErr)
				}
				if progress != nil {
					progress("eino_stream_error", streamRecvErr.Error(), map[string]interface{}{
						"conversationId": conversationID,
						"source":         "eino",
						"einoAgent":      ev.AgentName,
						"einoRole":       einoRoleTag(ev.AgentName),
					})
				}
				if _, retErr := maybeRetryTransientRun(streamRecvErr); retErr != nil {
					return takePartial(retErr)
				}
			}
			continue
		}

		msg, gerr := mv.GetMessage()
		if gerr != nil || msg == nil {
			continue
		}
		runAccumulatedMsgs = append(runAccumulatedMsgs, msg)
		tryEmitToolCallsOnce(mergeMessageToolCalls(msg), ev.AgentName, orchestratorName, conversationID, orchMode, progress, toolEmitSeen, subAgentToolStep, mainAgentToolStep, markPending)

		if mv.Role == schema.Assistant {
			if progress != nil && strings.TrimSpace(msg.ReasoningContent) != "" {
				progress("reasoning_chain", openai.DisplayReasoningContent(strings.TrimSpace(msg.ReasoningContent)), map[string]interface{}{
					"conversationId": conversationID,
					"source":         "eino",
					"einoAgent":      ev.AgentName,
					"einoRole":       einoRoleTag(ev.AgentName),
					"orchestration":  orchMode,
				})
			}
			body := strings.TrimSpace(msg.Content)
			if body != "" {
				if streamsMainAssistant(ev.AgentName) {
					executeStdoutDupMu.Lock()
					dup := pendingExecuteStdoutDup
					if dup != "" && body == dup {
						pendingExecuteStdoutDup = ""
						executeStdoutDupMu.Unlock()
						lastAssistant = body
						if orchMode == "plan_execute" && strings.EqualFold(strings.TrimSpace(ev.AgentName), "executor") {
							lastPlanExecuteExecutor = UnwrapPlanExecuteUserText(body)
						}
						// Non-streaming: skip assistant-channel display when it matches execute output; msg was already written to runAccumulatedMsgs above.
					} else {
						if dup != "" {
							pendingExecuteStdoutDup = ""
						}
						executeStdoutDupMu.Unlock()
						if progress != nil {
							nonStreamID := fmt.Sprintf("eino-main-%s-%d", conversationID, atomic.AddInt64(&mainResponseStreamSeq, 1))
							progress("response_start", "", map[string]interface{}{
								"conversationId":     conversationID,
								"mcpExecutionIds":    snapshotMCPIDs(),
								"messageGeneratedBy": "eino:" + ev.AgentName,
								"einoRole":           "orchestrator",
								"einoAgent":          ev.AgentName,
								"orchestration":      orchMode,
								"iteration":          einoMainRound,
								"streamId":           nonStreamID,
							})
							progress("response_delta", body, openai.WithSSEAccumulated(map[string]interface{}{
								"conversationId":  conversationID,
								"mcpExecutionIds": snapshotMCPIDs(),
								"einoRole":        "orchestrator",
								"einoAgent":       ev.AgentName,
								"orchestration":   orchMode,
								"iteration":       einoMainRound,
								"streamId":        nonStreamID,
							}, body))
						}
						lastAssistant = body
						if orchMode == "plan_execute" && strings.EqualFold(strings.TrimSpace(ev.AgentName), "executor") {
							lastPlanExecuteExecutor = UnwrapPlanExecuteUserText(body)
						}
					}
				} else if progress != nil {
					progress("eino_agent_reply", body, map[string]interface{}{
						"conversationId": conversationID,
						"einoAgent":      ev.AgentName,
						"einoRole":       "sub",
						"source":         "eino",
					})
				}
			}
		}

		if mv.Role == schema.Tool && progress != nil {
			toolName := msg.ToolName
			if toolName == "" {
				toolName = mv.ToolName
			}

			content := msg.Content
			isErr := false
			if strings.HasPrefix(content, einomcp.ToolErrorPrefix) {
				isErr = true
				content = strings.TrimPrefix(content, einomcp.ToolErrorPrefix)
			}

			preview := content
			if len(preview) > 200 {
				preview = preview[:200] + "..."
			}
			data := map[string]interface{}{
				"toolName":       toolName,
				"success":        !isErr,
				"isError":        isErr,
				"result":         content,
				"resultPreview":  preview,
				"conversationId": conversationID,
				"einoAgent":      ev.AgentName,
				"einoRole":       einoRoleTag(ev.AgentName),
				"source":         "eino",
			}
			toolCallID := strings.TrimSpace(msg.ToolCallID)
			if toolCallID == "" {
				if inferred, ok := popNextPendingForAgent(ev.AgentName); ok {
					toolCallID = inferred.ToolCallID
				} else if inferred, ok := popNextPendingForAgent(orchestratorName); ok {
					toolCallID = inferred.ToolCallID
				} else if inferred, ok := popNextPendingForAgent(""); ok {
					toolCallID = inferred.ToolCallID
				} else if inferred, ok := popAnyPending(); ok {
					toolCallID = inferred.ToolCallID
				}
			}
			if toolCallID != "" {
				removePendingByID(toolCallID)
				if _, loaded := toolResultSent.LoadOrStore(toolCallID, struct{}{}); loaded {
					// ToolInvokeNotify may have already pushed tool_result, such as when the execute streaming wrapper fires with truncated stdout only.
					// Still use the full content from the ADK Tool message to refresh the dedupe baseline, avoiding duplicate assistant output when the model repeats the full text and the truncated string would not match.
					recordPendingExecuteStdoutDup(toolName, content, isErr)
					continue
				}
				data["toolCallId"] = toolCallID
			}
			recordPendingExecuteStdoutDup(toolName, content, isErr)
			recordEinoADKFilesystemToolMonitor(args.FilesystemMonitorAgent, args.FilesystemMonitorRecord, toolName, toolCallID, runAccumulatedMsgs, content, isErr)
			progress("tool_result", fmt.Sprintf("Tool result (%s)", toolName), data)
		}
	}

	mcpIDsMu.Lock()
	ids := append([]string(nil), *mcpIDs...)
	mcpIDsMu.Unlock()

	out := buildEinoRunResultFromAccumulated(
		orchMode, runAccumulatedMsgs, persistTraceSource(args, runAccumulatedMsgs),
		lastAssistant, lastPlanExecuteExecutor, emptyHint, ids, false,
	)
	if shouldEinoEmptyResponseContinue(out, emptyHint, len(runAccumulatedMsgs), baseAccumulatedCount) {
		if logger != nil {
			logger.Info("eino empty response, ending run segment for handler resume",
				zap.String("conversationId", conversationID),
				zap.String("orchestration", orchMode),
				zap.Int("traceMessages", len(runAccumulatedMsgs)))
		}
		if progress != nil {
			progress("eino_empty_response_continue", "会话已结束但未产生助手正文，正在基于轨迹自动续跑…", map[string]interface{}{
				"conversationId": conversationID,
				"source":         "eino",
				"resumeKind":     "trace_segment",
			})
		}
		return out, ErrEmptyResponseContinue
	}
	return out, nil
}

func shouldEinoEmptyResponseContinue(out *RunResult, emptyHint string, accumulatedLen, baseCount int) bool {
	if out == nil || accumulatedLen <= baseCount {
		return false
	}
	return strings.TrimSpace(out.Response) == strings.TrimSpace(emptyHint)
}

func persistTraceSource(args *einoADKRunLoopArgs, fallback []adk.Message) []adk.Message {
	if args != nil && args.ModelFacingTrace != nil {
		if snap := args.ModelFacingTrace.Snapshot(); len(snap) > 0 {
			return snap
		}
	}
	return fallback
}

func einoPartialRunLastOutputHint() string {
	return "[Run ended abnormally due to user stop, timeout, or error. Continue from the tools and results already produced above; do not repeat completed steps.]\n" +
		"[Run ended abnormally; continue from the trace above without repeating completed steps.]"
}

// friendlyEinoExecuteInvokeTail converts trailing errors from non-MCP paths such as Eino execute into short hints; other cases keep the original error text.
func friendlyEinoExecuteInvokeTail(invokeErr error) string {
	if invokeErr == nil {
		return ""
	}
	if errors.Is(invokeErr, context.DeadlineExceeded) {
		return einoExecuteTimeoutUserHint()
	}
	return "[Run ended abnormally] " + invokeErr.Error()
}

func buildEinoRunResultFromAccumulated(
	orchMode string,
	runAccumulatedMsgs []adk.Message,
	persistMsgs []adk.Message,
	lastAssistant string,
	lastPlanExecuteExecutor string,
	emptyHint string,
	mcpIDs []string,
	partial bool,
) *RunResult {
	traceForJSON := persistMsgs
	if len(traceForJSON) == 0 {
		traceForJSON = runAccumulatedMsgs
	}
	histJSON, _ := json.Marshal(traceForJSON)
	cleaned := strings.TrimSpace(lastAssistant)
	if orchMode == "plan_execute" {
		if e := strings.TrimSpace(lastPlanExecuteExecutor); e != "" {
			cleaned = e
		} else {
			cleaned = UnwrapPlanExecuteUserText(cleaned)
		}
	}
	if cleaned == "" {
		if fb := strings.TrimSpace(einoExtractFallbackAssistantFromMsgs(runAccumulatedMsgs)); fb != "" {
			cleaned = fb
		}
	}
	cleaned = dedupeRepeatedParagraphs(cleaned, 80)
	cleaned = dedupeParagraphsByLineFingerprint(cleaned, 100)
	// Prevent very long responses from causing slow JSON serialization or OOM, which can happen when multi-agent runs concatenate large tool outputs.
	const maxResponseRunes = 100000
	if rs := []rune(cleaned); len(rs) > maxResponseRunes {
		cleaned = string(rs[:maxResponseRunes]) + "\n\n... (response truncated)"
	}
	lastOut := cleaned
	resp := cleaned
	if partial && cleaned == "" {
		lastOut = einoPartialRunLastOutputHint()
		resp = emptyHint
	}
	out := &RunResult{
		Response:             resp,
		MCPExecutionIDs:      mcpIDs,
		LastAgentTraceInput:  string(histJSON),
		LastAgentTraceOutput: lastOut,
	}
	if !partial && out.Response == "" {
		out.Response = emptyHint
		out.LastAgentTraceOutput = out.Response
	}
	return out
}

// einoExtractFallbackAssistantFromMsgs backfills the user-visible reply from the Eino ADK trace when the main channel produced no assistant body.
// Typical cases: the supervisor only calls exit with final_result in a Tool message, or tool results were written to history but lastAssistant was not updated.
//
// Priority: last exit tool output -> final_result from the last assistant tool_calls argument containing exit.
func einoExtractFallbackAssistantFromMsgs(msgs []adk.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m == nil || m.Role != schema.Tool {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(m.ToolName), adk.ToolInfoExit.Name) {
			continue
		}
		content := strings.TrimSpace(m.Content)
		if content == "" || strings.HasPrefix(content, einomcp.ToolErrorPrefix) {
			continue
		}
		return content
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m == nil || m.Role != schema.Assistant {
			continue
		}
		if s := einoExtractExitFinalFromAssistantToolCalls(m); s != "" {
			return s
		}
	}
	return ""
}

func einoExtractExitFinalFromAssistantToolCalls(msg *schema.Message) string {
	if msg == nil || len(msg.ToolCalls) == 0 {
		return ""
	}
	for i := len(msg.ToolCalls) - 1; i >= 0; i-- {
		tc := msg.ToolCalls[i]
		if !strings.EqualFold(strings.TrimSpace(tc.Function.Name), adk.ToolInfoExit.Name) {
			continue
		}
		if s := einoParseExitFinalResultArguments(tc.Function.Arguments); s != "" {
			return s
		}
	}
	return ""
}

func einoParseExitFinalResultArguments(arguments string) string {
	arguments = strings.TrimSpace(arguments)
	if arguments == "" {
		return ""
	}
	var wrap struct {
		FinalResult json.RawMessage `json:"final_result"`
	}
	if err := json.Unmarshal([]byte(arguments), &wrap); err != nil || len(wrap.FinalResult) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(wrap.FinalResult, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var anyVal interface{}
	if err := json.Unmarshal(wrap.FinalResult, &anyVal); err != nil {
		return ""
	}
	b, err := json.Marshal(anyVal)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func buildEinoCheckpointID(orchMode string) string {
	mode := sanitizeEinoPathSegment(strings.TrimSpace(orchMode))
	if mode == "" {
		mode = "default"
	}
	return "runner-" + mode
}
