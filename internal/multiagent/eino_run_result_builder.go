package multiagent

import (
	"encoding/json"
	"strings"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/einomcp"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type einoRunResultBuilderConfig struct {
	OrchMode         string
	EmptyHint        string
	RunMessages      *einoRunMessageAccumulator
	AssistantOutput  *einoAssistantOutputAccumulator
	SnapshotMCPIDs   func() []string
	ModelFacingTrace func() []adk.Message
}

type einoRunResultBuilder struct {
	cfg einoRunResultBuilderConfig
}

func newEinoRunResultBuilder(cfg einoRunResultBuilderConfig) *einoRunResultBuilder {
	return &einoRunResultBuilder{cfg: cfg}
}

func (b *einoRunResultBuilder) BuildPartial(runErr error) (*RunResult, error) {
	if b == nil || b.cfg.RunMessages == nil || !b.cfg.RunMessages.HasNewMessages() {
		return nil, runErr
	}
	return b.build(true), runErr
}

func (b *einoRunResultBuilder) BuildFinal() *RunResult {
	if b == nil {
		return &RunResult{}
	}
	return b.build(false)
}

func (b *einoRunResultBuilder) build(partial bool) *RunResult {
	var runMsgs []adk.Message
	if b.cfg.RunMessages != nil {
		runMsgs = b.cfg.RunMessages.NewMessages()
	}
	var lastAssistant string
	var lastPlanExecuteExecutor string
	if b.cfg.AssistantOutput != nil {
		lastAssistant = b.cfg.AssistantOutput.LastAssistant()
		lastPlanExecuteExecutor = b.cfg.AssistantOutput.LastPlanExecuteExecutor()
	}
	var modelFacing []adk.Message
	if b.cfg.ModelFacingTrace != nil {
		modelFacing = b.cfg.ModelFacingTrace()
	}
	var ids []string
	if b.cfg.SnapshotMCPIDs != nil {
		ids = b.cfg.SnapshotMCPIDs()
	}
	return buildEinoRunResultFromAccumulated(
		b.cfg.OrchMode,
		runMsgs,
		modelFacing,
		lastAssistant,
		lastPlanExecuteExecutor,
		b.cfg.EmptyHint,
		ids,
		partial,
	)
}

func einoPartialRunLastOutputHint() string {
	return "[Run did not finish normally (user stop, timeout, or error). When resuming, continue from the tools and results already produced above; do not repeat completed steps.]\n" +
		"[Run ended abnormally; continue from the trace above without repeating completed steps.]"
}

func buildEinoRunResultFromAccumulated(
	orchMode string,
	runAccumulatedMsgs []adk.Message,
	persistMsgs []adk.Message,
	lastAssistant string,
	lastPlanExecuteExecutor string,
	emptyHint string,
	mcpIDs []string,
	partial bool,
) *RunResult {
	traceForJSON := persistMsgs
	traceJSON := ""
	if len(traceForJSON) > 0 {
		traceForJSON = markModelFacingTraceForPersistence(traceForJSON)
		if histJSON, err := json.Marshal(traceForJSON); err == nil {
			traceJSON = string(histJSON)
		}
	}
	cleaned := strings.TrimSpace(lastAssistant)
	if orchMode == "plan_execute" {
		if e := strings.TrimSpace(lastPlanExecuteExecutor); e != "" {
			cleaned = e
		} else {
			cleaned = UnwrapPlanExecuteUserText(cleaned)
		}
	}
	// exit.final_result is the formal deliverable: even when the assistant body already has transitional
	// wording (e.g. "final report:"), the exit content must win, so supervisor-style modes never show only an empty intro.
	if exitFinal := strings.TrimSpace(einoExtractExitDeliverableFromMsgs(runAccumulatedMsgs)); exitFinal != "" {
		cleaned = einoMergeAssistantIntroWithExitFinal(cleaned, exitFinal)
	} else if cleaned == "" {
		if fb := strings.TrimSpace(einoExtractFallbackAssistantFromMsgs(runAccumulatedMsgs)); fb != "" {
			cleaned = fb
			if orchMode == "plan_execute" {
				cleaned = UnwrapPlanExecuteUserText(cleaned)
			}
		}
	}
	cleaned = dedupeRepeatedParagraphs(cleaned, 80)
	cleaned = dedupeParagraphsByLineFingerprint(cleaned, 100)
	const maxResponseRunes = 100000
	if rs := []rune(cleaned); len(rs) > maxResponseRunes {
		cleaned = string(rs[:maxResponseRunes]) + "\n\n... (response truncated)"
	}
	lastOut := cleaned
	resp := cleaned
	if partial && cleaned == "" {
		lastOut = einoPartialRunLastOutputHint()
		resp = emptyHint
	}
	out := &RunResult{
		Response:             resp,
		MCPExecutionIDs:      mcpIDs,
		LastAgentTraceInput:  traceJSON,
		LastAgentTraceOutput: lastOut,
	}
	if !partial && out.Response == "" {
		out.Response = emptyHint
		out.LastAgentTraceOutput = out.Response
	}
	return out
}

func markModelFacingTraceForPersistence(msgs []adk.Message) []adk.Message {
	out := cloneADKMessagesForTrace(msgs)
	if len(out) == 0 || out[0] == nil {
		return out
	}
	if out[0].Extra == nil {
		out[0].Extra = make(map[string]any, 1)
	}
	out[0].Extra[agent.ModelFacingTraceVersionKey] = 1
	return out
}

// einoExtractExitDeliverableFromMsgs extracts the formal exit deliverable of the
// current turn from the message trace (the exit tool output, or
// arguments.final_result when the assistant calls exit).
// If a non-exit tool result appears closer to the end, the exit is not treated as the final deliverable.
func einoExtractExitDeliverableFromMsgs(msgs []adk.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m == nil {
			continue
		}
		switch m.Role {
		case schema.Tool:
			if strings.EqualFold(strings.TrimSpace(m.ToolName), adk.ToolInfoExit.Name) {
				content := strings.TrimSpace(m.Content)
				if content != "" && !strings.HasPrefix(content, einomcp.ToolErrorPrefix) {
					return content
				}
				// When the exit tool output is empty, keep scanning back to the assistant arguments.
				continue
			}
			return ""
		case schema.Assistant:
			if s := einoExtractExitFinalFromAssistantToolCalls(m); s != "" {
				return s
			}
			if einoAssistantHasNonExitToolCall(m) {
				return ""
			}
		}
	}
	return ""
}

func einoAssistantHasNonExitToolCall(msg *schema.Message) bool {
	if msg == nil {
		return false
	}
	for _, tc := range msg.ToolCalls {
		if !strings.EqualFold(strings.TrimSpace(tc.Function.Name), adk.ToolInfoExit.Name) {
			return true
		}
	}
	return false
}

// einoMergeAssistantIntroWithExitFinal merges the assistant's transitional wording with the exit deliverable body.
// Exit content wins; if the assistant body is just an intro and is not contained in the exit text, it is kept as a prefix.
func einoMergeAssistantIntroWithExitFinal(assistant, exitFinal string) string {
	assistant = strings.TrimSpace(assistant)
	exitFinal = strings.TrimSpace(exitFinal)
	if exitFinal == "" {
		return assistant
	}
	if assistant == "" || assistant == exitFinal {
		return exitFinal
	}
	if strings.Contains(exitFinal, assistant) {
		return exitFinal
	}
	if strings.Contains(assistant, exitFinal) {
		return assistant
	}
	return assistant + "\n\n" + exitFinal
}

// einoExtractFallbackAssistantFromMsgs backfills a user-visible reply from the Eino ADK
// native message trace when the main channel produced no assistant body. Stay conservative:
// only accept the most recent deliverable final state in reverse order, so transitional text
// before tool calls or subtask progress is never promoted into the final reply.
//
// Deliverable final states:
// - exit tool output;
// - arguments.final_result when the assistant calls exit;
// - a plain assistant body not cut off by a later ordinary tool result.
func einoExtractFallbackAssistantFromMsgs(msgs []adk.Message) string {
	if s := einoExtractExitDeliverableFromMsgs(msgs); s != "" {
		return s
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m == nil {
			continue
		}
		switch m.Role {
		case schema.Tool:
			// The latest message is an ordinary tool result: the assistant has not produced the
			// final body yet; do not fall back to earlier progress text.
			return ""
		case schema.Assistant:
			if len(m.ToolCalls) == 0 {
				if content := strings.TrimSpace(m.Content); content != "" {
					return content
				}
			}
		}
	}
	return ""
}

func einoExtractExitFinalFromAssistantToolCalls(msg *schema.Message) string {
	if msg == nil || len(msg.ToolCalls) == 0 {
		return ""
	}
	for i := len(msg.ToolCalls) - 1; i >= 0; i-- {
		tc := msg.ToolCalls[i]
		if !strings.EqualFold(strings.TrimSpace(tc.Function.Name), adk.ToolInfoExit.Name) {
			continue
		}
		if s := einoParseExitFinalResultArguments(tc.Function.Arguments); s != "" {
			return s
		}
	}
	return ""
}

func einoParseExitFinalResultArguments(arguments string) string {
	arguments = strings.TrimSpace(arguments)
	if arguments == "" {
		return ""
	}
	var wrap struct {
		FinalResult json.RawMessage `json:"final_result"`
	}
	if err := json.Unmarshal([]byte(arguments), &wrap); err != nil || len(wrap.FinalResult) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(wrap.FinalResult, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var anyVal interface{}
	if err := json.Unmarshal(wrap.FinalResult, &anyVal); err != nil {
		return ""
	}
	b, err := json.Marshal(anyVal)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
