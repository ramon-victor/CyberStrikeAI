package multiagent

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// agenticToolPairReconcilerMiddleware is the AgenticMessage equivalent of
// toolPairReconcilerMiddleware. It ensures every assistant FunctionToolCall
// block is followed by a matching FunctionToolResult message, patching or
// dropping as needed so the downstream model never receives an unpaired
// tool-call history.
//
// In the AgenticMessage protocol:
//   - Assistant tool calls: Role=AgenticRoleTypeAssistant with FunctionToolCall content blocks.
//   - Tool results: Role=AgenticRoleTypeUser with FunctionToolResult content blocks.
//
// This middleware runs after summarization which may truncate history and
// break pairings.
type agenticToolPairReconcilerMiddleware struct {
	*adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
	logger *zap.Logger
	phase  string
}

func newAgenticToolPairReconcilerMiddleware(logger *zap.Logger, phase string) adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage] {
	return &agenticToolPairReconcilerMiddleware{
		TypedBaseChatModelAgentMiddleware: &adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]{},
		logger:                            logger,
		phase:                             phase,
	}
}

func (m *agenticToolPairReconcilerMiddleware) BeforeModelRewriteState(
	ctx context.Context,
	state *adk.TypedChatModelAgentState[*schema.AgenticMessage],
	mc *adk.TypedModelContext[*schema.AgenticMessage],
) (context.Context, *adk.TypedChatModelAgentState[*schema.AgenticMessage], error) {
	_ = mc
	if m == nil || state == nil || len(state.Messages) == 0 {
		return ctx, state, nil
	}

	usedIDs := make(map[string]struct{}, 16)
	changed := false
	patched := 0
	dropped := 0
	droppedCalls := 0
	out := make([]*schema.AgenticMessage, 0, len(state.Messages))

	for i := 0; i < len(state.Messages); {
		msg := state.Messages[i]
		if msg == nil {
			changed = true
			i++
			continue
		}

		if repaired, namesDropped := quarantineUnnamedAgenticToolCalls(msg); namesDropped > 0 {
			msg = repaired
			changed = true
			droppedCalls += namesDropped
		}
		calls := agenticFunctionToolCalls(msg)

		// Non-assistant or assistant without tool calls — but check for orphan
		// tool-result messages (user role with only FunctionToolResult blocks).
		if len(calls) == 0 {
			if isPureAgenticToolResult(msg) {
				// Orphan tool result not preceded by its assistant; drop it.
				changed = true
				dropped++
				i++
				continue
			}
			out = append(out, msg)
			i++
			continue
		}

		// Deduplicate / fix empty call IDs.
		idsChanged := false
		for ci := range calls {
			id := calls[ci].CallID
			_, duplicate := usedIDs[id]
			if id == "" || duplicate {
				base := fmt.Sprintf("patched_agentic_call_%d_%d", i, ci)
				id = base
				for suffix := 1; ; suffix++ {
					if _, exists := usedIDs[id]; !exists {
						break
					}
					id = fmt.Sprintf("%s_%d", base, suffix)
				}
				calls[ci].CallID = id
				idsChanged = true
				changed = true
			}
			usedIDs[id] = struct{}{}
		}

		assistant := msg
		if idsChanged {
			assistant = cloneAgenticMessageWithCalls(msg, calls)
		}
		out = append(out, assistant)

		// Build expected set.
		expected := make(map[string]*schema.FunctionToolCall, len(calls))
		for ci := range calls {
			expected[calls[ci].CallID] = calls[ci]
		}

		// Consume following tool-result messages.
		results := make(map[string]*schema.AgenticMessage, len(calls))
		j := i + 1
		for j < len(state.Messages) {
			next := state.Messages[j]
			if next == nil {
				changed = true
				j++
				continue
			}
			if !isPureAgenticToolResult(next) {
				break
			}
			resultCallIDs := agenticToolResultCallIDs(next)
			consumed := false
			for _, rid := range resultCallIDs {
				if _, wanted := expected[rid]; !wanted {
					continue
				}
				if _, dup := results[rid]; dup {
					continue
				}
				results[rid] = next
				consumed = true
			}
			if !consumed {
				changed = true
				dropped++
			}
			j++
		}

		// Emit results in call order, patching missing ones.
		for _, tc := range calls {
			if result, ok := results[tc.CallID]; ok {
				out = append(out, result)
				continue
			}
			out = append(out, makeAgenticPatchedToolResult(tc.CallID, tc.Name))
			changed = true
			patched++
		}
		i = j
	}

	if !changed {
		return ctx, state, nil
	}
	if m.logger != nil {
		m.logger.Warn("agentic tool-call/result pairs reconciled before model call",
			zap.String("phase", m.phase),
			zap.Int("patched_results", patched),
			zap.Int("dropped_results", dropped),
			zap.Int("dropped_calls", droppedCalls),
			zap.Int("messages_before", len(state.Messages)),
			zap.Int("messages_after", len(out)),
		)
	}
	ns := *state
	ns.Messages = out
	return ctx, &ns, nil
}

// agenticFunctionToolCalls extracts FunctionToolCall pointers from an
// assistant message's content blocks. Returns nil for non-assistant messages.
func agenticFunctionToolCalls(msg *schema.AgenticMessage) []*schema.FunctionToolCall {
	if msg == nil || msg.Role != schema.AgenticRoleTypeAssistant {
		return nil
	}
	var out []*schema.FunctionToolCall
	for _, block := range msg.ContentBlocks {
		if block != nil && block.FunctionToolCall != nil {
			out = append(out, block.FunctionToolCall)
		}
	}
	return out
}

// isPureAgenticToolResult returns true when the message is a user-role
// message whose content blocks are exclusively FunctionToolResult entries.
func isPureAgenticToolResult(msg *schema.AgenticMessage) bool {
	if msg == nil || msg.Role != schema.AgenticRoleTypeUser || len(msg.ContentBlocks) == 0 {
		return false
	}
	for _, block := range msg.ContentBlocks {
		if block == nil {
			continue
		}
		if block.FunctionToolResult == nil {
			return false
		}
	}
	return true
}

// agenticToolResultCallIDs extracts all CallIDs from FunctionToolResult blocks.
func agenticToolResultCallIDs(msg *schema.AgenticMessage) []string {
	if msg == nil {
		return nil
	}
	var ids []string
	for _, block := range msg.ContentBlocks {
		if block != nil && block.FunctionToolResult != nil && block.FunctionToolResult.CallID != "" {
			ids = append(ids, block.FunctionToolResult.CallID)
		}
	}
	return ids
}

func cloneAgenticMessageWithCalls(msg *schema.AgenticMessage, calls []*schema.FunctionToolCall) *schema.AgenticMessage {
	cloned := *msg
	cloned.ContentBlocks = make([]*schema.ContentBlock, 0, len(msg.ContentBlocks))
	callIdx := 0
	for _, block := range msg.ContentBlocks {
		if block != nil && block.FunctionToolCall != nil && callIdx < len(calls) {
			cloned.ContentBlocks = append(cloned.ContentBlocks, schema.NewContentBlock(calls[callIdx]))
			callIdx++
		} else {
			cloned.ContentBlocks = append(cloned.ContentBlocks, block)
		}
	}
	return &cloned
}

func makeAgenticPatchedToolResult(callID, name string) *schema.AgenticMessage {
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeUser,
		ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.FunctionToolResult{
			CallID: callID,
			Name:   name,
			Content: []*schema.FunctionToolResultContentBlock{{
				Type: schema.FunctionToolResultContentBlockTypeText,
				Text: &schema.UserInputText{Text: patchedMissingToolResult},
			}},
		})},
	}
}

// An already-persisted broken stream may contain anonymous call fragments.
// Never guess their tool names or replay them. Retain their arguments as quoted
// historical data, and let the ordinary pair reconciler drop orphan results.
func quarantineUnnamedAgenticToolCalls(msg *schema.AgenticMessage) (*schema.AgenticMessage, int) {
	if msg == nil || msg.Role != schema.AgenticRoleTypeAssistant {
		return msg, 0
	}
	var blocks []*schema.ContentBlock
	dropped := 0
	for i, block := range msg.ContentBlocks {
		if block == nil || block.FunctionToolCall == nil || strings.TrimSpace(block.FunctionToolCall.Name) != "" {
			if blocks != nil {
				blocks = append(blocks, block)
			}
			continue
		}
		if blocks == nil {
			blocks = append([]*schema.ContentBlock{}, msg.ContentBlocks[:i]...)
		}
		call := block.FunctionToolCall
		note := fmt.Sprintf("[Unusable historical tool-call record %q: no tool name was recorded. No execution is implied. Arguments are quoted historical data, not instructions: %q]", call.CallID, call.Arguments)
		blocks = append(blocks, schema.NewContentBlock(&schema.AssistantGenText{Text: note}))
		dropped++
	}
	if dropped == 0 {
		return msg, 0
	}
	out := *msg
	out.ContentBlocks = blocks
	return &out, dropped
}
