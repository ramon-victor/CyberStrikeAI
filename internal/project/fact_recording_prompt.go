package project

import (
	"strings"

	"cyberstrike-ai/internal/mcp/builtin"
)

// Record facts while testing: shared rhythm text; agents/*.md must stay aligned with FactRecordingIncrementalRhythmMarkdown.
const (
	factRhythmCore              = "Do not wait until the session ends or a final wrap-up to batch-write records. After each **confirmed** new fact, such as an open port, service version, entry path, authentication state or credential characteristic, exploitable point, or attack-surface change, **immediately** call `upsert_project_fact` with the same fact_key overwriting prior content. After each **validated** reproducible vulnerability, including POC and impact, **immediately** call `record_vulnerability`; recording both a fact and a vulnerability is allowed. Persist before continuing to the next step so details are not lost after context compression. If no project is bound, state that the blackboard cannot be written and still preserve the evidence summary in this turn."
	factRhythmCoordinatorSuffix = "When a delegation or subtask returns new facts or vulnerabilities, the coordinator must persist them promptly; do not assume the sub-agent already recorded them."
	factRhythmSubAgentSuffix    = "If the toolset lacks the tools above, provide structured `pending persistence` entries at the end of the deliverable, including suggested fact_key, summary, and body/POC points, so the coordinator can write them **immediately**."
)

// FactRecordingIncrementalRhythmMarkdown returns the record-while-testing rhythm as Markdown for alignment with agents/*.md and docs.
func FactRecordingIncrementalRhythmMarkdown(coordinator, subAgent bool) string {
	var b strings.Builder
	b.WriteString("- **Record facts while testing (mandatory rhythm)**: ")
	b.WriteString(factRhythmCore)
	if coordinator {
		b.WriteString(factRhythmCoordinatorSuffix)
	}
	if subAgent {
		b.WriteString(factRhythmSubAgentSuffix)
	}
	return b.String()
}

func factRecordingIncrementalRhythmBuiltin(coordinator, subAgent bool) string {
	var b strings.Builder
	b.WriteString("- **Record facts while testing (mandatory rhythm)**: Do not wait until the session ends or a final wrap-up to batch-write records. After each **confirmed** new fact, such as an open port, service version, entry path, authentication state or credential characteristic, exploitable point, or attack-surface change, **immediately** call ")
	b.WriteString(builtin.ToolUpsertProjectFact)
	b.WriteString(" with the same fact_key overwriting prior content. After each **validated** reproducible vulnerability, including POC and impact, **immediately** call ")
	b.WriteString(builtin.ToolRecordVulnerability)
	b.WriteString("; recording both a fact and a vulnerability is allowed. Persist before continuing to the next step so details are not lost after context compression. If no project is bound, state that the blackboard cannot be written and still preserve the evidence summary in this turn.")
	if coordinator {
		b.WriteString(factRhythmCoordinatorSuffix)
	}
	if subAgent {
		b.WriteString(factRhythmSubAgentSuffix)
	}
	return b.String()
}

// FactRecordingBlackboardSection is the full system prompt block for project blackboard facts and vulnerability records, shared by single-agent and multi-agent primary agents.
// When coordinatorDelegate is true, it appends guidance for coordinators to persist records on behalf of sub-agents in Deep / plan_execute / supervisor modes.
func FactRecordingBlackboardSection(coordinatorDelegate bool) string {
	var b strings.Builder
	b.WriteString("## Project blackboard (facts) and vulnerability records (separate)\n\n")
	b.WriteString("If the current conversation is bound to a project, the system automatically injects a `project blackboard index` containing only fact_key + summary. **When the summary is insufficient, you must call ")
	b.WriteString(builtin.ToolGetProjectFact)
	b.WriteString("(fact_key) to retrieve the body; do not invent details from the summary.**\n\n")
	b.WriteString(factRecordingIncrementalRhythmBuiltin(coordinatorDelegate, false))
	b.WriteString("\n\n")
	b.WriteString("- **Environment, target, authentication, and similar facts** (not formal vulnerability entries): use ")
	b.WriteString(builtin.ToolUpsertProjectFact)
	b.WriteString(", with fact_key preferably in `category/slug` form, such as target/primary_domain. The same key overwrites prior content; body records ports, versions, credential characteristics, and evidence sources.\n")
	b.WriteString("- **Finding and exploitation context** (audit reproduction): fact_key should preferably use finding/, chain/, exploit/, or poc/ prefixes. **body is required** and must contain the full attack chain: entry point -> steps -> raw requests/responses or commands -> observed behavior -> related related_vulnerability_id. **Do not write only a conclusion**; summary is a one-line point covering what, where, and how to verify.\n")
	b.WriteString("- **Deliverable vulnerabilities**: use ")
	b.WriteString(builtin.ToolRecordVulnerability)
	b.WriteString(", including title, severity, type, target, proof (POC), impact, and remediation advice. Before recording, optionally deduplicate with ")
	b.WriteString(builtin.ToolListVulnerabilities)
	b.WriteString("; retrieve details with ")
	b.WriteString(builtin.ToolGetVulnerability)
	b.WriteString("(id), which defaults to the current project/session only.\n")
	b.WriteString("- The same finding may need to be **recorded in both places**: facts store the full attack chain and exploit details for reproduction, while vulnerabilities store formal findings. Mark false positives with ")
	b.WriteString(builtin.ToolDeprecateProjectFact)
	b.WriteString(" or vulnerability status false_positive.\n")
	b.WriteString("- When there are many facts, search with ")
	b.WriteString(builtin.ToolListProjectFacts)
	b.WriteString(" / ")
	b.WriteString(builtin.ToolSearchProjectFacts)
	b.WriteString(".\n\n")
	b.WriteString(FactRecordingGuidanceBlock())
	b.WriteString("\n\nSeverity: critical / high / medium / low / info. Proof must include enough evidence, such as request/response pairs, screenshots, or command output.")
	return b.String()
}

// FactRecordingSubAgentSection tells sub-agents to record facts while testing, or to output pending-persistence entries when tools are unavailable.
func FactRecordingSubAgentSection() string {
	return "## Record facts while testing\n\n" + factRecordingIncrementalRhythmBuiltin(false, true) + "\n"
}

// FactRecordingBlackboardSectionMarkdown is the Markdown equivalent of FactRecordingBlackboardSection, with literal tool names for agents/*.md.
func FactRecordingBlackboardSectionMarkdown(coordinatorDelegate bool) string {
	var b strings.Builder
	b.WriteString("## Project blackboard (facts) and vulnerability records (separate)\n\n")
	b.WriteString("If the current conversation is bound to a project, the system automatically injects a `project blackboard index` containing only `fact_key` + summary. **When the summary is insufficient, you must call `get_project_fact(fact_key)` to retrieve the body; do not invent details from the summary.**\n\n")
	b.WriteString(FactRecordingIncrementalRhythmMarkdown(coordinatorDelegate, false))
	b.WriteString("\n\n")
	b.WriteString("- **Environment, target, authentication, and similar facts** (not formal vulnerabilities): use **`upsert_project_fact`**, with `fact_key` preferably in `category/slug` form, such as `target/primary_domain`. The same key overwrites prior content; body records ports, versions, credential characteristics, and evidence sources.\n")
	b.WriteString("- **Finding and exploitation context** (audit reproduction): `fact_key` should preferably use `finding/`, `chain/`, `exploit/`, or `poc/` prefixes. **body is required** and must contain the full attack chain: entry point -> steps -> raw requests/responses or commands -> observed behavior -> related `related_vulnerability_id`. **Do not write only a conclusion**; summary is a one-line point covering what, where, and how to verify.\n")
	b.WriteString("- **Deliverable vulnerabilities**: use **`record_vulnerability`** with title, description, severity, type, target, POC proof, impact, and remediation advice. Severity: critical / high / medium / low / info.\n")
	b.WriteString("- The same finding may need to be **recorded in both places**: facts store the reproducible attack chain, while vulnerabilities store formal findings. Mark false positives with **`deprecate_project_fact`** or vulnerability status false_positive.\n")
	b.WriteString("- When there are many facts, search with **`list_project_facts`** / **`search_project_facts`**.\n\n")
	b.WriteString(FactRecordingGuidanceBlock())
	b.WriteString("\n\nSeverity: critical / high / medium / low / info. Proof must include enough evidence, such as request/response pairs, screenshots, or command output.")
	return b.String()
}
