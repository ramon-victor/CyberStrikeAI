package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/multiagent"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// EinoSingleAgentLoopStream Eino ADK 单代理（ChatModelAgent + Runner）流式对话；不依赖 multi_agent.enabled。
func (h *AgentHandler) EinoSingleAgentLoopStream(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ev := StreamEvent{Type: "error", Message: "Invalid request parameters: " + err.Error()}
		b, _ := json.Marshal(ev)
		fmt.Fprintf(c.Writer, "data: %s\n\n", b)
		done := StreamEvent{Type: "done", Message: ""}
		db, _ := json.Marshal(done)
		fmt.Fprintf(c.Writer, "data: %s\n\n", db)
		if flusher, ok := c.Writer.(http.Flusher); ok {
			flusher.Flush()
		}
		return
	}

	c.Header("X-Accel-Buffering", "no")

	var baseCtx context.Context
	clientDisconnected := false
	var sseWriteMu sync.Mutex
	var ssePublishConversationID string
	sendEvent := func(eventType, message string, data interface{}) {
		if eventType == "error" && baseCtx != nil {
			cause := context.Cause(baseCtx)
			if errors.Is(cause, ErrTaskCancelled) || errors.Is(cause, multiagent.ErrInterruptContinue) {
				return
			}
		}
		ev := StreamEvent{Type: eventType, Message: message, Data: data}
		b, errMarshal := json.Marshal(ev)
		if errMarshal != nil {
			b = []byte(`{"type":"error","message":"marshal failed"}`)
		}
		sseLine := make([]byte, 0, len(b)+8)
		sseLine = append(sseLine, []byte("data: ")...)
		sseLine = append(sseLine, b...)
		sseLine = append(sseLine, '\n', '\n')
		if ssePublishConversationID != "" && h.taskEventBus != nil {
			h.taskEventBus.Publish(ssePublishConversationID, sseLine)
		}
		if clientDisconnected {
			return
		}
		select {
		case <-c.Request.Context().Done():
			clientDisconnected = true
			return
		default:
		}
		sseWriteMu.Lock()
		_, err := c.Writer.Write(sseLine)
		if err != nil {
			sseWriteMu.Unlock()
			clientDisconnected = true
			return
		}
		if flusher, ok := c.Writer.(http.Flusher); ok {
			flusher.Flush()
		} else {
			c.Writer.Flush()
		}
		sseWriteMu.Unlock()
	}

	h.logger.Info("Received Eino ADK single agent stream request",
		zap.String("conversationId", req.ConversationID),
	)

	prep, err := h.prepareMultiAgentSession(&req, c, "eino_agent_stream")
	if err != nil {
		sendEvent("error", err.Error(), nil)
		sendEvent("done", "", nil)
		return
	}
	ssePublishConversationID = prep.ConversationID
	if prep.CreatedNew {
		sendEvent("conversation", "Conversation created", map[string]interface{}{
			"conversationId": prep.ConversationID,
		})
	}

	conversationID := prep.ConversationID
	assistantMessageID := prep.AssistantMessageID
	h.activateHITLForConversation(conversationID, req.Hitl)
	if h.hitlManager != nil {
		defer h.hitlManager.DeactivateConversation(conversationID)
	}

	if prep.UserMessageID != "" {
		sendEvent("message_saved", "", map[string]interface{}{
			"conversationId": conversationID,
			"userMessageId":  prep.UserMessageID,
		})
	}

	var cancelWithCause context.CancelCauseFunc
	curFinalMessage := prep.FinalMessage
	curHistory := prep.History
	roleTools := prep.RoleTools

	taskStatus := "completed"
	// 仅在成功 StartTask 后再 FinishTask。若 StartTask 因 ErrTaskAlreadyRunning 失败仍 defer FinishTask，
	// 会误删其他连接上正在运行的同会话任务，导致「第一次拦截、第二次却放行」。
	taskOwned := false
	defer func() {
		if taskOwned {
			h.tasks.FinishTask(conversationID, taskStatus)
		}
	}()

	sendEvent("progress", "Starting Eino ADK single agent (ChatModelAgent)...", map[string]interface{}{
		"conversationId": conversationID,
	})

	stopKeepalive := make(chan struct{})
	go sseKeepalive(c, stopKeepalive, &sseWriteMu)
	defer close(stopKeepalive)

	if h.config == nil {
		taskStatus = "failed"
		h.tasks.UpdateTaskStatus(conversationID, taskStatus)
		sendEvent("error", "Server config not loaded", nil)
		sendEvent("done", "", map[string]interface{}{"conversationId": conversationID})
		return
	}

	var result *multiagent.RunResult
	var runErr error

	baseCtx, cancelWithCause = context.WithCancelCause(context.Background())
	taskCtx, timeoutCancel := context.WithTimeout(baseCtx, 600*time.Minute)

	if _, err := h.tasks.StartTask(conversationID, req.Message, cancelWithCause); err != nil {
		var errorMsg string
		if errors.Is(err, ErrTaskAlreadyRunning) {
			errorMsg = "A task is already running in this conversation. Please wait for it to complete or click \"Stop Task\" before retrying."
			sendEvent("error", errorMsg, map[string]interface{}{
				"conversationId": conversationID,
				"errorType":      "task_already_running",
			})
		} else {
			errorMsg = "Failed to start task: " + err.Error()
			sendEvent("error", errorMsg, nil)
		}
		if assistantMessageID != "" {
			_, _ = h.db.Exec("UPDATE messages SET content = ?, updated_at = ? WHERE id = ?", errorMsg, time.Now(), assistantMessageID)
		}
		sendEvent("done", "", map[string]interface{}{"conversationId": conversationID})
		timeoutCancel()
		return
	}
	taskOwned = true

	var cumulativeMCPExecutionIDs []string

	for {
		progressCallback := h.createProgressCallback(taskCtx, cancelWithCause, conversationID, assistantMessageID, sendEvent)
		taskCtxLoop := mcp.WithMCPConversationID(taskCtx, conversationID)
		taskCtxLoop = mcp.WithToolRunRegistry(taskCtxLoop, h.tasks)
		taskCtxLoop = multiagent.WithHITLToolInterceptor(taskCtxLoop, func(ctx context.Context, toolName, arguments string) (string, error) {
			return h.interceptHITLForEinoTool(ctx, cancelWithCause, conversationID, assistantMessageID, sendEvent, toolName, arguments)
		})

		result, runErr = multiagent.RunEinoSingleChatModelAgent(
			taskCtxLoop,
			h.config,
			&h.config.MultiAgent,
			h.agent,
			h.logger,
			conversationID,
			curFinalMessage,
			curHistory,
			roleTools,
			progressCallback,
			chatReasoningToClientIntent(req.Reasoning),
		)
		timeoutCancel()

		if result != nil && len(result.MCPExecutionIDs) > 0 {
			cumulativeMCPExecutionIDs = mergeMCPExecutionIDLists(cumulativeMCPExecutionIDs, result.MCPExecutionIDs)
		}

		if runErr == nil {
			break
		}

		cause := context.Cause(baseCtx)
		if errors.Is(cause, multiagent.ErrInterruptContinue) {
			if shouldPersistEinoAgentTraceAfterRunError(baseCtx) {
				h.persistEinoAgentTraceForResume(conversationID, result)
			}
			note := h.tasks.TakeInterruptContinueNote(conversationID)
			icSummary := interruptContinueTimelineSummary(note)
			progressCallback("user_interrupt_continue", icSummary, map[string]interface{}{
				"conversationId": conversationID,
				"rawReason":      strings.TrimSpace(note),
				"emptyReason":    strings.TrimSpace(note) == "",
				"kind":           "no_active_mcp_tool",
			})
			inject := formatInterruptContinueUserMessage(note)
			// 不写入 messages 表为 user 气泡：避免主对话流出现大段模板；说明已由 user_interrupt_continue 记入助手 process_details（迭代详情）。
			if hist, err := h.loadHistoryFromAgentTrace(conversationID); err == nil && len(hist) > 0 {
				curHistory = hist
			}
			curFinalMessage = inject
			sendEvent("progress", "Merged user supplement with latest trace, continuing reasoning...", map[string]interface{}{
				"conversationId": conversationID,
				"source":         "interrupt_continue",
			})
			h.tasks.UpdateTaskStatus(conversationID, "running")
			baseCtx, cancelWithCause = context.WithCancelCause(context.Background())
			h.tasks.BindTaskCancel(conversationID, cancelWithCause)
			taskCtx, timeoutCancel = context.WithTimeout(baseCtx, 600*time.Minute)
			continue
		}

		if shouldPersistEinoAgentTraceAfterRunError(baseCtx) {
			h.persistEinoAgentTraceForResume(conversationID, result)
		}
		if errors.Is(cause, ErrTaskCancelled) {
			taskStatus = "cancelled"
			h.tasks.UpdateTaskStatus(conversationID, taskStatus)
			cancelMsg := "Task cancelled by user, operations stopped."
			if assistantMessageID != "" {
				if result != nil {
					if err := h.mergeAssistantMessagePartialOnCancel(assistantMessageID, result.Response); err != nil {
						h.logger.Warn("Failed to merge partial response before cancel", zap.Error(err))
					}
				}
				if err := h.appendAssistantMessageNotice(assistantMessageID, cancelMsg); err != nil {
					h.logger.Warn("Failed to update assistant message after cancel", zap.Error(err))
				}
				_ = h.db.AddProcessDetail(assistantMessageID, conversationID, "cancelled", cancelMsg, nil)
			}
			sendEvent("cancelled", cancelMsg, map[string]interface{}{
				"conversationId": conversationID,
				"messageId":      assistantMessageID,
			})
			sendEvent("done", "", map[string]interface{}{"conversationId": conversationID})
			return
		}

		if errors.Is(runErr, context.DeadlineExceeded) || errors.Is(context.Cause(taskCtx), context.DeadlineExceeded) {
			taskStatus = "timeout"
			h.tasks.UpdateTaskStatus(conversationID, taskStatus)
			timeoutMsg := "Task execution timed out, auto-terminated."
			if assistantMessageID != "" {
				_, _ = h.db.Exec("UPDATE messages SET content = ?, updated_at = ? WHERE id = ?", timeoutMsg, time.Now(), assistantMessageID)
				_ = h.db.AddProcessDetail(assistantMessageID, conversationID, "timeout", timeoutMsg, nil)
			}
			sendEvent("error", timeoutMsg, map[string]interface{}{
				"conversationId": conversationID,
				"messageId":      assistantMessageID,
				"errorType":      "timeout",
			})
			sendEvent("done", "", map[string]interface{}{"conversationId": conversationID})
			return
		}

		h.logger.Error("Eino ADK single agent execution failed", zap.Error(runErr))
		taskStatus = "failed"
		h.tasks.UpdateTaskStatus(conversationID, taskStatus)
		errMsg := "Execution failed: " + runErr.Error()
		if assistantMessageID != "" {
			_, _ = h.db.Exec("UPDATE messages SET content = ?, updated_at = ? WHERE id = ?", errMsg, time.Now(), assistantMessageID)
			_ = h.db.AddProcessDetail(assistantMessageID, conversationID, "error", errMsg, nil)
		}
		sendEvent("error", errMsg, map[string]interface{}{
			"conversationId": conversationID,
			"messageId":      assistantMessageID,
		})
		sendEvent("done", "", map[string]interface{}{"conversationId": conversationID})
		return
	}

	if assistantMessageID != "" {
		_ = h.db.UpdateAssistantMessageFinalize(assistantMessageID, result.Response, cumulativeMCPExecutionIDs, multiagent.AggregatedReasoningFromTraceJSON(result.LastAgentTraceInput))
	}

	if result.LastAgentTraceInput != "" || result.LastAgentTraceOutput != "" {
		if err := h.db.SaveAgentTrace(conversationID, result.LastAgentTraceInput, result.LastAgentTraceOutput); err != nil {
			h.logger.Warn("Failed to save agent trace", zap.Error(err))
		}
	}

	sendEvent("response", result.Response, map[string]interface{}{
		"mcpExecutionIds": cumulativeMCPExecutionIDs,
		"conversationId":  conversationID,
		"messageId":       assistantMessageID,
		"agentMode":       "eino_single",
	})
	sendEvent("done", "", map[string]interface{}{"conversationId": conversationID})
}

// EinoSingleAgentLoop Eino ADK 单代理非流式对话。
func (h *AgentHandler) EinoSingleAgentLoop(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.logger.Info("Received Eino ADK single agent non-stream request", zap.String("conversationId", req.ConversationID))

	prep, err := h.prepareMultiAgentSession(&req, c, "eino_agent")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.activateHITLForConversation(prep.ConversationID, req.Hitl)
	if h.hitlManager != nil {
		defer h.hitlManager.DeactivateConversation(prep.ConversationID)
	}

	var progressBuf strings.Builder
	progressCallbackRaw := func(eventType, message string, data interface{}) {
		progressBuf.WriteString(eventType)
		progressBuf.WriteByte('\n')
	}
	baseCtx, cancelWithCause := context.WithCancelCause(c.Request.Context())
	defer cancelWithCause(nil)
	taskCtx, timeoutCancel := context.WithTimeout(baseCtx, 600*time.Minute)
	defer timeoutCancel()
	progressCallback := h.createProgressCallback(taskCtx, cancelWithCause, prep.ConversationID, prep.AssistantMessageID, progressCallbackRaw)
	taskCtx = multiagent.WithHITLToolInterceptor(taskCtx, func(ctx context.Context, toolName, arguments string) (string, error) {
		return h.interceptHITLForEinoTool(ctx, cancelWithCause, prep.ConversationID, prep.AssistantMessageID, nil, toolName, arguments)
	})

	if h.config == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Server configuration not loaded"})
		return
	}

	result, runErr := multiagent.RunEinoSingleChatModelAgent(
		taskCtx,
		h.config,
		&h.config.MultiAgent,
		h.agent,
		h.logger,
		prep.ConversationID,
		prep.FinalMessage,
		prep.History,
		prep.RoleTools,
		progressCallback,
		chatReasoningToClientIntent(req.Reasoning),
	)
	if runErr != nil {
		if shouldPersistEinoAgentTraceAfterRunError(baseCtx) {
			h.persistEinoAgentTraceForResume(prep.ConversationID, result)
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": runErr.Error()})
		return
	}

	if prep.AssistantMessageID != "" {
		_ = h.db.UpdateAssistantMessageFinalize(prep.AssistantMessageID, result.Response, result.MCPExecutionIDs, multiagent.AggregatedReasoningFromTraceJSON(result.LastAgentTraceInput))
	}
	if result.LastAgentTraceInput != "" || result.LastAgentTraceOutput != "" {
		_ = h.db.SaveAgentTrace(prep.ConversationID, result.LastAgentTraceInput, result.LastAgentTraceOutput)
	}

	c.JSON(http.StatusOK, gin.H{
		"response":           result.Response,
		"conversationId":     prep.ConversationID,
		"mcpExecutionIds":    result.MCPExecutionIDs,
		"assistantMessageId": prep.AssistantMessageID,
		"agentMode":          "eino_single",
	})
}
