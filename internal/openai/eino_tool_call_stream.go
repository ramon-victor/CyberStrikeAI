package openai

import (
	"bytes"
	"encoding/json"
	"sort"
)

// Eino's native chat-completions converter allocates content-block indices by
// adjacency, not by tool-call index. Interleaved deltas (0,1,0,1) therefore split
// names from arguments. Only on the native model path, hold tool deltas until
// their choice finishes, then emit each complete call once in index order.
// Text, reasoning, usage and provider metadata continue streaming unchanged.
// Arguments are concatenated verbatim; incomplete JSON is never repaired here.
type einoToolCallStream struct {
	choices map[int]*einoPendingToolChoice
}
type einoPendingToolChoice struct {
	envelope map[string]json.RawMessage
	calls    map[int]map[string]json.RawMessage
}

func (s *einoToolCallStream) rewrite(line []byte) []byte {
	payload := bytes.TrimSpace(bytes.TrimLeft(line, " \t")[5:])
	if bytes.Equal(payload, []byte("[DONE]")) {
		return append(s.flushAll(), line...)
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(payload, &envelope) != nil {
		return line
	}
	if raw := envelope["error"]; len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
		s.choices = nil
		return line
	}
	var choices []map[string]json.RawMessage
	if json.Unmarshal(envelope["choices"], &choices) != nil {
		return line
	}
	changed := false
	var before []byte
	for _, choice := range choices {
		var index int
		if raw := choice["index"]; len(raw) > 0 {
			if json.Unmarshal(raw, &index) != nil {
				return line
			}
		}
		var delta map[string]json.RawMessage
		if json.Unmarshal(choice["delta"], &delta) == nil {
			var calls []map[string]json.RawMessage
			if json.Unmarshal(delta["tool_calls"], &calls) == nil && len(calls) > 0 {
				if s.choices == nil {
					s.choices = make(map[int]*einoPendingToolChoice)
				}
				pending := s.choices[index]
				if pending == nil {
					metadata := make(map[string]json.RawMessage, len(envelope))
					for k, v := range envelope {
						if k != "choices" && k != "usage" {
							metadata[k] = v
						}
					}
					pending = &einoPendingToolChoice{envelope: metadata, calls: make(map[int]map[string]json.RawMessage)}
					s.choices[index] = pending
				}
				for _, call := range calls {
					var toolIndex int
					if raw := call["index"]; len(raw) > 0 {
						if json.Unmarshal(raw, &toolIndex) != nil {
							return line
						}
					}
					target := pending.calls[toolIndex]
					if target == nil {
						target = make(map[string]json.RawMessage)
						pending.calls[toolIndex] = target
					}
					mergeEinoToolCallDelta(target, call)
				}
				delete(delta, "tool_calls")
				choice["delta"], _ = json.Marshal(delta)
				changed = true
			}
		}
		var finish string
		if json.Unmarshal(choice["finish_reason"], &finish) == nil && finish != "" {
			before = append(before, s.flushChoice(index)...)
		}
	}
	if !changed {
		return append(before, line...)
	}
	envelope["choices"], _ = json.Marshal(choices)
	normalized, err := json.Marshal(envelope)
	if err != nil {
		return append(before, line...)
	}
	return append(before, append(append([]byte("data: "), normalized...), '\n')...)
}

func mergeEinoToolCallDelta(target, delta map[string]json.RawMessage) {
	for key, value := range delta {
		if key == "function" {
			var dst, src map[string]json.RawMessage
			_ = json.Unmarshal(target[key], &dst)
			if json.Unmarshal(value, &src) != nil {
				target[key] = value
				continue
			}
			if dst == nil {
				dst = make(map[string]json.RawMessage)
			}
			for k, v := range src {
				if k == "arguments" || k == "name" {
					dst[k] = concatEinoToolField(dst[k], v, k == "arguments")
				} else {
					dst[k] = v
				}
			}
			target[key], _ = json.Marshal(dst)
		} else if key == "id" || key == "type" {
			target[key] = concatEinoToolField(target[key], value, false)
		} else {
			target[key] = value
		}
	}
}

func concatEinoToolField(previous, next json.RawMessage, alwaysAppend bool) json.RawMessage {
	var a, b string
	if json.Unmarshal(next, &b) != nil {
		return next
	}
	if json.Unmarshal(previous, &a) != nil {
		a = ""
	}
	if b != "" && (alwaysAppend || a != b) {
		a += b
	}
	out, _ := json.Marshal(a)
	return out
}

func (s *einoToolCallStream) flushChoice(index int) []byte {
	pending := s.choices[index]
	if pending == nil {
		return nil
	}
	delete(s.choices, index)
	indices := make([]int, 0, len(pending.calls))
	for i := range pending.calls {
		indices = append(indices, i)
	}
	sort.Ints(indices)
	calls := make([]map[string]json.RawMessage, 0, len(indices))
	for _, i := range indices {
		calls = append(calls, pending.calls[i])
	}
	choice := map[string]any{"index": index, "delta": map[string]any{"tool_calls": calls}}
	pending.envelope["choices"], _ = json.Marshal([]any{choice})
	payload, _ := json.Marshal(pending.envelope)
	return append(append([]byte("data: "), payload...), '\n')
}

func (s *einoToolCallStream) flushAll() []byte {
	indices := make([]int, 0, len(s.choices))
	for i := range s.choices {
		indices = append(indices, i)
	}
	sort.Ints(indices)
	var out []byte
	for _, i := range indices {
		out = append(out, s.flushChoice(i)...)
	}
	return out
}
