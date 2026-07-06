package multiagent

import (
	"strings"
	"time"

	"cyberstrike-ai/internal/config"
)

const defaultEmptyResponseContinueMaxAttempts = 5

// IsEinoEmptyResponseResult 判断 Run 是否以「未捕获助手正文」占位结束（非真实用户可见回复）。
func IsEinoEmptyResponseResult(result *RunResult) bool {
	if result == nil {
		return false
	}
	return isEinoEmptyResponseText(result.Response)
}

func isEinoEmptyResponseText(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	return 		strings.Contains(s, "no assistant text was captured") ||
		strings.Contains(s, "no assistant text output captured")
}

// HasEinoResumeTrace 轨迹非空，续跑才有上下文可恢复。
func HasEinoResumeTrace(result *RunResult) bool {
	if result == nil {
		return false
	}
	s := strings.TrimSpace(result.LastAgentTraceInput)
	return s != "" && s != "[]" && s != "null"
}

// EmptyResponseContinueMaxAttemptsFromConfig 无助手正文时 Handler 层退避续跑上限；0=默认 5。
func EmptyResponseContinueMaxAttemptsFromConfig(mw *config.MultiAgentEinoMiddlewareConfig) int {
	if mw != nil && mw.EmptyResponseContinueMaxAttempts > 0 {
		return mw.EmptyResponseContinueMaxAttempts
	}
	return defaultEmptyResponseContinueMaxAttempts
}

// EmptyResponseContinueBackoff 与 run_retry 相同指数退避（2s, 4s, 8s… capped）。
func EmptyResponseContinueBackoff(attempt int, mw *config.MultiAgentEinoMiddlewareConfig) time.Duration {
	maxBackoff := defaultEinoRunRetryMaxBackoff
	if mw != nil && mw.RunRetryMaxBackoffSec > 0 {
		maxBackoff = time.Duration(mw.RunRetryMaxBackoffSec) * time.Second
	}
	return einoTransientRetryBackoff(attempt, maxBackoff)
}

// FormatEmptyResponseContinueUserMessage is the user turn injected on system auto-resume (not written to the messages table bubble).
func FormatEmptyResponseContinueUserMessage() string {
	return strings.TrimSpace(`[System auto-resume / Auto resume]
The previous Eino session did not produce visible assistant text (possibly interrupted during streaming, or only completed tool calls). Please continue advancing based on the existing trajectory and tool results, and provide a short summary; do not repeat already-completed steps.`)
}
