package openai

// retry.go – reusable retry infrastructure for all AI API calls.
//
// Design:
//   - isTransientError classifies errors as retryable (HTTP 429/500/502/503/504, network errors)
//     versus non-retryable (auth, bad-request, permission, etc.).
//   - withRetry wraps any non-streaming API call with up to 5 retries using exponential backoff
//     (10 s, 20 s, 40 s, 80 s, 160 s) with ±20 % jitter.
//   - If a Retry-After header was captured in the *APIError, its value is used as a floor for
//     the delay so we always honour rate-limit hints from the provider.
//   - Streaming helpers must handle the deltasSent guard themselves (see openai.go /
//     claude_bridge.go) before calling the shared waitForRetry utility.

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

// maxRetries is the maximum number of retry attempts after the initial call fails.
const maxRetries = 5

// retryBaseDelays holds the base wait time for each successive retry attempt (1-indexed).
var retryBaseDelays = []time.Duration{
	10 * time.Second,
	20 * time.Second,
	40 * time.Second,
	80 * time.Second,
	160 * time.Second,
}

// isTransientError reports whether err should be retried.
//
// Transient: HTTP 429, 500, 502, 503, 504 and common network/transport errors.
// Non-transient: HTTP 400, 401, 403, 404, 422, … (bad request, auth failure, etc.)
func isTransientError(err error) bool {
	if err == nil {
		return false
	}

	// HTTP status-based check.
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusTooManyRequests, // 429 – rate limit
			http.StatusInternalServerError, // 500
			http.StatusBadGateway,          // 502
			http.StatusServiceUnavailable,  // 503
			http.StatusGatewayTimeout:      // 504
			return true
		default:
			// 400, 401, 403, 404, 422, … are not transient.
			return false
		}
	}

	// Network / transport error keywords.
	msg := strings.ToLower(err.Error())
	for _, kw := range []string{
		"connection reset",
		"connection refused",
		"connection timed out",
		"timeout",
		"i/o timeout",
		"no such host",
		"network is unreachable",
		"broken pipe",
		" eof",
		"read tcp",
		"write tcp",
		"dial tcp",
	} {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}

// retryAfterFromResponse extracts a Retry-After duration from an HTTP response header.
// Returns 0 if the header is absent or cannot be parsed as a non-negative integer of seconds.
func retryAfterFromResponse(resp *http.Response) time.Duration {
	if resp == nil {
		return 0
	}
	ra := resp.Header.Get("Retry-After")
	if ra == "" {
		return 0
	}
	// The most common form is an integer number of seconds.
	if secs, err := strconv.ParseFloat(ra, 64); err == nil && secs > 0 {
		return time.Duration(secs * float64(time.Second))
	}
	return 0
}

// computeRetryDelay returns the wait duration before the nth retry attempt (1-indexed).
// A ±20 % jitter is applied to the base delay to spread concurrent retries.
// If err is an *APIError with a non-zero RetryAfter, that value is used as a floor.
func computeRetryDelay(attempt int, err error) time.Duration {
	idx := attempt - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(retryBaseDelays) {
		idx = len(retryBaseDelays) - 1
	}

	base := retryBaseDelays[idx]

	// ±20 % jitter
	jitterRange := float64(base) * 0.20
	jitter := time.Duration((rand.Float64()*2 - 1) * jitterRange)
	delay := base + jitter

	// Honor Retry-After from the provider if it's longer than our computed delay.
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > delay {
		delay = apiErr.RetryAfter
	}

	return delay
}

// logRetryAttempt emits a standardised warning log for each failed attempt.
func logRetryAttempt(logger *zap.Logger, op string, attempt int, err error, delay time.Duration) {
	fields := []zap.Field{
		zap.String("op", op),
		zap.Int("attempt", attempt),
		zap.Int("maxRetries", maxRetries),
		zap.Duration("retryIn", delay),
		zap.Error(err),
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		fields = append(fields, zap.Int("httpStatus", apiErr.StatusCode))
	}
	logger.Warn("API call failed, will retry", fields...)
}

// waitForRetry sleeps for the computed backoff duration (with Retry-After awareness).
// It returns ctx.Err() immediately if the context is cancelled during the wait.
func waitForRetry(ctx context.Context, attempt int, err error) error {
	delay := computeRetryDelay(attempt, err)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

// withRetry executes fn, retrying on transient errors up to maxRetries times.
//
// fn must return (retryAfterHint, error).  retryAfterHint is the value extracted from
// the Retry-After response header at the call site; pass 0 when not available.
// fn is expected to create a fresh *http.Request on each invocation so the request body
// is not exhausted after the first attempt.
func withRetry(ctx context.Context, logger *zap.Logger, op string, fn func() (retryAfterHint time.Duration, err error)) error {
	var lastErr error

	for attempt := 1; attempt <= maxRetries+1; attempt++ {
		if attempt > 1 {
			// Only retry transient errors.
			if !isTransientError(lastErr) {
				return lastErr
			}
			delay := computeRetryDelay(attempt-1, lastErr)
			logRetryAttempt(logger, op, attempt-1, lastErr, delay)

			select {
			case <-ctx.Done():
				return fmt.Errorf("%s: context cancelled while waiting to retry: %w", op, ctx.Err())
			case <-time.After(delay):
			}
		}

		retryAfterHint, err := fn()
		if err == nil {
			if attempt > 1 {
				logger.Info("API call succeeded after retry",
					zap.String("op", op),
					zap.Int("attempt", attempt),
				)
			}
			return nil
		}

		// Attach the Retry-After hint to APIError so computeRetryDelay can use it next round.
		var apiErr *APIError
		if errors.As(err, &apiErr) && retryAfterHint > 0 {
			apiErr.RetryAfter = retryAfterHint
		}

		lastErr = err

		if !isTransientError(err) {
			return err
		}
	}

	return fmt.Errorf("%s failed after %d retries: %w", op, maxRetries, lastErr)
}
