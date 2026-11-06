package multiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"cyberstrike-ai/internal/config"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// OpenRouter can stream parallel calls as 0,1,0,1. The native Eino converter
// used to assign a fresh content-block index every time the source index changed.
func TestNativeModelPreservesInterleavedParallelToolCallsAcrossTurns(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestNumber := requests.Add(1)
		var body struct {
			Messages []struct {
				Role      string `json:"role"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if requestNumber == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			frames := []string{
				`{"id":"parallel","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"skill","arguments":""}},{"index":1,"id":"call_b","type":"function","function":{"name":"skill","arguments":""}}]}}]}`,
				`{"id":"parallel","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"name\":"}},{"index":1,"function":{"arguments":"{\"name\":"}}]}}]}`,
				`{"id":"parallel","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"recon\"}"}},{"index":1,"function":{"arguments":"\"web\"}"}}]}}]}`,
				`{"id":"parallel","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":6,"total_tokens":16}}`,
			}
			for _, f := range frames {
				fmt.Fprintf(w, "data: %s\n\n", f)
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		calls := 0
		for _, m := range body.Messages {
			for _, c := range m.ToolCalls {
				calls++
				if c.Function.Name == "" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(400)
					fmt.Fprint(w, `{"error":{"message":"tool_calls[2].function.name must be a non-empty string (got undefined)","type":"invalid_request_error"}}`)
					return
				}
				want := map[string]string{"call_a": `{"name":"recon"}`, "call_b": `{"name":"web"}`}
				if c.Function.Name != "skill" || c.Function.Arguments != want[c.ID] {
					t.Errorf("corrupted tool history: %+v", c)
				}
			}
		}
		if calls != 2 {
			t.Errorf("calls=%d, want exactly 2", calls)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"final","choices":[{"index":0,"message":{"role":"assistant","content":"continued"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	ctx := context.Background()
	model, err := newEinoAgenticChatModelFactory(server.Client(), nil, nil)(ctx, config.OpenAIConfig{Provider: "openai_compatible", APIKey: "test-only", BaseURL: server.URL, Model: "openrouter-test"}, einoModelModeNormal)
	if err != nil {
		t.Fatal(err)
	}
	messages := []*schema.AgenticMessage{schema.UserAgenticMessage("load both skills")}
	stream, err := model.Stream(ctx, messages)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var chunks []*schema.AgenticMessage
	for {
		m, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		chunks = append(chunks, m)
	}
	merged, err := schema.ConcatAgenticMessages(chunks)
	if err != nil {
		t.Fatal(err)
	}
	// Pass through the real tail middleware: this is how the missing names used
	// to survive into the next request after malformed arguments became {}.
	messages = append(messages, merged)
	for _, id := range []string{"call_a", "call_b"} {
		messages = append(messages, makeAgenticPatchedToolResult(id, "skill"))
	}
	state := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{Messages: messages}
	for _, mw := range appendEinoAgenticChatModelTailMiddlewares(nil, einoChatModelTailConfig{phase: "test"}) {
		_, state, err = mw.BeforeModelRewriteState(ctx, state, nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = model.Generate(ctx, state.Messages); err != nil {
		t.Fatalf("tool continuation rejected: %v", err)
	}
	if len(agenticFunctionToolCalls(merged)) != 2 {
		t.Fatalf("stream split parallel calls into %d blocks", len(agenticFunctionToolCalls(merged)))
	}
}
