package handler

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"cyberstrike-ai/internal/config"

	"go.uber.org/zap"
)

// TestCreateProgressCallback_ConcurrentToolEvents 回归 issue #142：并行 tool 回调不得 concurrent map panic。
func TestCreateProgressCallback_ConcurrentToolEvents(t *testing.T) {
	logger := zap.NewNop()
	h := &AgentHandler{
		logger: logger,
		config: &config.Config{},
	}
	cb := h.createProgressCallback(context.Background(), nil, "conv-race-test", "", nil)

	const workers = 64
	var wg sync.WaitGroup
	wg.Add(workers * 2)
	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			toolCallID := fmt.Sprintf("tc-%d", i)
			cb("tool_call", "calling skill", map[string]interface{}{
				"toolCallId":   toolCallID,
				"toolName":     "skill",
				"argumentsObj": map[string]interface{}{"skill_name": "demo-skill"},
			})
		}()
		go func() {
			defer wg.Done()
			toolCallID := fmt.Sprintf("tc-%d", i)
			cb("tool_result", "skill done", map[string]interface{}{
				"toolCallId": toolCallID,
				"toolName":   "skill",
				"success":    true,
			})
		}()
	}
	wg.Wait()
}
