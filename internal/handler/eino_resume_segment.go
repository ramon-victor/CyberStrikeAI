package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/multiagent"
)

func (h *AgentHandler) einoRunRetryMaxAttempts() int {
	if h.config != nil {
		return multiagent.RunRetryMaxAttemptsFromConfig(&h.config.MultiAgent.EinoMiddleware)
	}
	return multiagent.RunRetryMaxAttemptsFromConfig(nil)
}

func (h *AgentHandler) einoRunRetryMaxBackoffSec() int {
	if h.config != nil && h.config.MultiAgent.EinoMiddleware.RunRetryMaxBackoffSec > 0 {
		return h.config.MultiAgent.EinoMiddleware.RunRetryMaxBackoffSec
	}
	return 0
}

// applyEinoTraceResumeSegment 中断并继续：persist last_react_* → loadHistory，可选替换下一段 user 文案。
func (h *AgentHandler) applyEinoTraceResumeSegment(
	conversationID string,
	result *multiagent.RunResult,
	curHistory *[]agent.ChatMessage,
	curFinalMessage *string,
	segmentUserMessage string,
) {
	if shouldPersistEinoAgentTraceAfterRunError(context.Background()) {
		h.persistEinoAgentTraceForResume(conversationID, result)
	}
	if hist, err := h.loadHistoryFromAgentTrace(conversationID); err == nil && len(hist) > 0 {
		*curHistory = hist
	}
	if segmentUserMessage != "" {
		*curFinalMessage = segmentUserMessage
	}
}

// applyEinoTransientRetrySegment 临时错误重试：恢复轨迹并保留本请求原始 user 文案（不注入续跑说明）。
// segmentUserMessage 为本轮 HTTP 请求开始时用户发送的内容，避免因清空 finalMessage 而丢失「你好」等短句。
func (h *AgentHandler) applyEinoTransientRetrySegment(
	conversationID string,
	result *multiagent.RunResult,
	curHistory *[]agent.ChatMessage,
	curFinalMessage *string,
	segmentUserMessage string,
) {
	if shouldPersistEinoAgentTraceAfterRunError(context.Background()) {
		h.persistEinoAgentTraceForResume(conversationID, result)
	}
	if hist, err := h.loadHistoryFromAgentTrace(conversationID); err == nil && len(hist) > 0 {
		*curHistory = hist
	}
	if s := strings.TrimSpace(segmentUserMessage); s != "" {
		*curFinalMessage = segmentUserMessage
	}
}

// handleEinoTransientRetryContinue 在 SSE 任务循环内处理临时错误重试；返回 true 表示外层 for 应 continue。
func (h *AgentHandler) handleEinoTransientRetryContinue(
	baseCtx context.Context,
	conversationID string,
	result *multiagent.RunResult,
	runErr error,
	transientAttempts *int,
	curHistory *[]agent.ChatMessage,
	curFinalMessage *string,
	segmentUserMessage string,
	progressCallback func(eventType, message string, data interface{}),
	sendProgress func(msg string, extra map[string]interface{}),
) (handled bool, fatal error) {
	if !errors.Is(runErr, multiagent.ErrTransientRetryContinue) {
		return false, nil
	}
	maxAttempts := h.einoRunRetryMaxAttempts()
	*transientAttempts++
	if *transientAttempts > maxAttempts {
		if shouldPersistEinoAgentTraceAfterRunError(baseCtx) {
			h.persistEinoAgentTraceForResume(conversationID, result)
		}
		return false, errors.New("transient retry exhausted: " + runErr.Error())
	}
	attemptNo := *transientAttempts
	backoff := multiagent.TransientRetryBackoff(attemptNo-1, h.einoRunRetryMaxBackoffSec())
	if progressCallback != nil {
		progressCallback("eino_run_retry", fmt.Sprintf("遇到临时错误，%d 秒后第 %d/%d 次重试…", int(backoff.Seconds()), attemptNo, maxAttempts), map[string]interface{}{
			"conversationId": conversationID,
			"source":         "eino",
			"attempt":        attemptNo,
			"maxAttempts":    maxAttempts,
			"backoffSec":     int(backoff.Seconds()),
		})
	}
	select {
	case <-baseCtx.Done():
		return false, context.Cause(baseCtx)
	case <-time.After(backoff):
	}
	h.applyEinoTransientRetrySegment(conversationID, result, curHistory, curFinalMessage, segmentUserMessage)
	if progressCallback != nil {
		progressCallback("eino_run_retry", "已恢复上下文，正在重试…", map[string]interface{}{
			"conversationId": conversationID,
			"source":         "eino",
			"attempt":        attemptNo,
		})
	}
	if sendProgress != nil {
		sendProgress("正在重试…", map[string]interface{}{
			"conversationId": conversationID,
			"source":         "transient_retry",
		})
	}
	return true, nil
}
