package openai

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func readNativeToolStream(input string) ([]byte, error) {
	body := newEinoSSESanitizingBody(io.NopCloser(strings.NewReader(input)))
	body.toolCalls = &einoToolCallStream{}
	return io.ReadAll(body)
}
func toolCallsFromStream(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range bytes.Split(body, []byte("\n")) {
		raw := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(raw) == 0 || bytes.Equal(raw, []byte("[DONE]")) {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal(raw, &event); err != nil {
			t.Fatal(err)
		}
		choices, _ := event["choices"].([]any)
		for _, c := range choices {
			choice := c.(map[string]any)
			delta, _ := choice["delta"].(map[string]any)
			calls, _ := delta["tool_calls"].([]any)
			for _, v := range calls {
				out = append(out, v.(map[string]any))
			}
		}
	}
	return out
}

const nativeToolFrame = `data: {"id":"test","model":"test-model","choices":[{"index":0,"delta":{"role":"assistant","content":"thinking","reasoning_details":[{"type":"reasoning.text","text":"reason"}],"tool_calls":[{"index":1,"id":"b","type":"function","function":{"name":"skill","arguments":"{\"name\":"},"extra_content":{"signature":"keep"}},{"index":0,"id":"a","type":"function","function":{"name":"exec","arguments":"{\"command\":"}}]}}]}` + "\n"

func TestNativeToolStreamPreservesTextReasoningUsageAndMetadata(t *testing.T) {
	input := nativeToolFrame + `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"pwd\"}"}},{"index":1,"function":{"arguments":"\"recon\"}"}}]}}]}` + "\n" + `data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}` + "\ndata: [DONE]\n"
	out, err := readNativeToolStream(input)
	if err != nil {
		t.Fatal(err)
	}
	calls := toolCallsFromStream(t, out)
	if len(calls) != 2 {
		t.Fatalf("calls=%v", calls)
	}
	for i, want := range []string{`{"command":"pwd"}`, `{"name":"recon"}`} {
		if calls[i]["index"] != float64(i) || calls[i]["function"].(map[string]any)["arguments"] != want {
			t.Fatalf("incorrect call: %v", calls[i])
		}
	}
	if calls[1]["extra_content"].(map[string]any)["signature"] != "keep" {
		t.Fatal("lost tool metadata")
	}
	for _, value := range []string{`"content":"thinking"`, `"reasoning_details"`, `"finish_reason":"tool_calls"`, `"prompt_tokens":10`, `data: [DONE]`} {
		if !bytes.Contains(out, []byte(value)) {
			t.Errorf("lost %s in %s", value, out)
		}
	}
	if bytes.Count(out, []byte(`"usage"`)) != 1 {
		t.Fatal("duplicated token usage")
	}
}

func TestNativeToolStreamFlushesOnDoneOrCleanEOF(t *testing.T) {
	for name, suffix := range map[string]string{"done": "\ndata: [DONE]\n", "eof": "\n", "eof_without_newline": ""} {
		t.Run(name, func(t *testing.T) {
			out, err := readNativeToolStream(strings.TrimSuffix(nativeToolFrame, "\n") + suffix)
			if err != nil {
				t.Fatal(err)
			}
			calls := toolCallsFromStream(t, out)
			if len(calls) != 2 {
				t.Fatalf("calls=%v", calls)
			}
			if calls[0]["function"].(map[string]any)["arguments"] != `{"command":` {
				t.Fatal("incomplete arguments were modified")
			}
		})
	}
}
func TestNativeToolStreamPassesOrdinaryFramesThrough(t *testing.T) {
	input := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\ndata: [DONE]\n"
	out, err := readNativeToolStream(input)
	if err != nil || string(out) != input {
		t.Fatalf("out=%q error=%v", out, err)
	}
}
func TestNativeToolStreamDoesNotFlushCallsAfterProviderError(t *testing.T) {
	out, err := readNativeToolStream(nativeToolFrame + "data: {\"error\":{\"message\":\"interrupted\"}}\ndata: [DONE]\n")
	if err != nil || len(toolCallsFromStream(t, out)) != 0 || !bytes.Contains(out, []byte("interrupted")) {
		t.Fatalf("out=%s error=%v", out, err)
	}
}

type brokenNativeStream struct {
	sent   bool
	closed bool
}

func (b *brokenNativeStream) Read(p []byte) (int, error) {
	if !b.sent {
		b.sent = true
		return copy(p, nativeToolFrame), io.ErrUnexpectedEOF
	}
	return 0, io.ErrUnexpectedEOF
}
func (b *brokenNativeStream) Close() error { b.closed = true; return nil }
func TestNativeToolStreamPropagatesReadErrorAndClose(t *testing.T) {
	source := &brokenNativeStream{}
	body := newEinoSSESanitizingBody(source)
	body.toolCalls = &einoToolCallStream{}
	out, err := io.ReadAll(body)
	if !errors.Is(err, io.ErrUnexpectedEOF) || len(toolCallsFromStream(t, out)) != 0 {
		t.Fatalf("out=%s error=%v", out, err)
	}
	_ = body.Close()
	if !source.closed {
		t.Fatal("upstream was not closed")
	}
}
