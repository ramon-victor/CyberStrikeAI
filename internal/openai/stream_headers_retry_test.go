package openai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cyberstrike-ai/internal/config"
	"go.uber.org/zap"
)

func TestStreamingRetryRetainsCustomHeaders(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test-Credential") != "test-value" {
			t.Error("custom header missing from request")
		}
		if requests.Add(1) == 1 {
			http.Error(w, "temporary failure", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	c := NewClient(&config.OpenAIConfig{BaseURL: server.URL, APIKey: "test-only", Headers: map[string]string{"X-Test-Credential": "test-value"}}, server.Client(), zap.NewNop())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var deltas []string
	result, err := c.ChatCompletionStream(ctx, map[string]any{"model": "test"}, func(delta string) error { deltas = append(deltas, delta); return nil })
	if err != nil || result != "hello" || strings.Join(deltas, "") != "hello" || requests.Load() != 2 {
		t.Fatalf("result=%q deltas=%v requests=%d error=%v", result, deltas, requests.Load(), err)
	}
}

func TestStreamingDoesNotReplayContentAfterAnError(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\ndata: {\"error\":{\"message\":\"temporary failure\"}}\n\n")
	}))
	defer server.Close()
	c := NewClient(&config.OpenAIConfig{BaseURL: server.URL, APIKey: "test-only"}, server.Client(), zap.NewNop())
	result, err := c.ChatCompletionStream(context.Background(), map[string]any{"model": "test"}, func(string) error { return nil })
	if err == nil || result != "partial" || requests.Load() != 1 {
		t.Fatalf("result=%q requests=%d error=%v", result, requests.Load(), err)
	}
}
