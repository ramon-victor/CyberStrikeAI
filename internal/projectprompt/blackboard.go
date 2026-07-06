// Package projectprompt provides system prompt text related to the project blackboard
// (pure strings, no database dependency). It is referenced by packages such as
// agent / multiagent to avoid the agent -> project import cycle that would
// otherwise break gopls metadata.
package projectprompt

import (
	"strings"

	"cyberstrike-ai/internal/mcp/builtin"
)

const (
	factRhythmCore = "Do not wait until the end of the session to batch-write. Every time you **confirm** a new insight (open port / service version, entry path, authentication state or credential characteristics, exploitable point, or attack surface change), **immediately** call upsert_project_fact (overwrite-update with the same fact_key). Every time you **verify** a reproducible vulnerability (with POC / impact), **immediately** call record_vulnerability; facts and vulnerabilities can each be recorded once. Prefer persisting to the blackboard before continuing to the next step to avoid losing details after context compression. When no project is bound, explain that the blackboard cannot be written, but still retain an evidence summary in the current turn."
	factRhythmCoordinatorSuffix = "When a delegation/subtask returns a new insight or vulnerability, the coordinator should write it to the blackboard promptly; do not assume the sub-agent has already recorded it."
	factRhythmSubAgentSuffix    = "If the tool set does not include the above tools, include a 'to-be-recorded' structured entry at the end of the deliverable (suggested fact_key, summary, body/POC key points) so the coordinator can write it **immediately**."
)

// FactRecordingIncrementalRhythmMarkdown returns the record-while-penetrating cadence (Markdown, aligned with agents/*.md and docs).
func FactRecordingIncrementalRhythmMarkdown(coordinator, subAgent bool) string {
	var b strings.Builder
	b.WriteString("- **Record while penetrating (mandatory cadence)**:")
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
	b.WriteString("- **Record while penetrating (mandatory cadence)**: Do not wait until the end of the session to batch-write. Every time you **confirm** a new insight (open port / service version, entry path, authentication state or credential characteristics, exploitable point, or attack surface change), **immediately** call ")
	b.WriteString(builtin.ToolUpsertProjectFact)
	b.WriteString(" (overwrite-update with the same fact_key). Every time you **verify** a reproducible vulnerability (with POC / impact), **immediately** call ")
	b.WriteString(builtin.ToolRecordVulnerability)
	b.WriteString("; facts and vulnerabilities can each be recorded once. Prefer persisting to the blackboard before continuing to the next step to avoid losing details after context compression. When no project is bound, explain that the blackboard cannot be written, but still retain an evidence summary in the current turn.")
	if coordinator {
		b.WriteString(factRhythmCoordinatorSuffix)
	}
	if subAgent {
		b.WriteString(factRhythmSubAgentSuffix)
	}
	return b.String()
}

func factEdgeRecordingGuidance() string {
	return "### Fact Relationship Edges (links)\n" +
		"\n" +
		"- When writing **finding / chain / exploit / poc**, you **must** provide links in upsert_project_fact (**recommended from**: the source fact pointing to the current fact, i.e., from -> current fact_key).\n" +
		"- **Minimum requirement**: finding category must have at least 1 from=target/* + type=discovered_on (i.e., target -> finding); when recording exploit on a finding, use from=exploit/* + type=exploits (i.e., exploit -> finding).\n" +
		"- **Common types**: discovered_on (where it was found), depends_on (prerequisite for reproduction), leads_to (cognitive advancement), enables (attack surface expansion), exploits (exploitation relation), contains (asset contains), part_of (belongs to chain/group), supports (evidence support).\n" +
		"- On update: **omit links to preserve existing edges**; pass links to **replace all relationship edges** (from -> current fact).\n" +
		"- The \"Dependent facts\" section in body can co-exist with links (for human reading); the structured relationship is authoritative as links."
}

func factRecordingGuidanceBlock() string {
	return "### Fact Writing Guidelines (Audit Reproduction / Knowledge Accumulation)\n" +
		"\n" +
		"- **summary**: One line for the index, must contain \"what + where + how to trigger/verify\" essentials; do not only write the conclusion (e.g., only \"SQLi exists\").\n" +
		"- **body**: Complete reproducible context, written to the body field of upsert_project_fact; the index does not include the body, so subsequent conversations must call get_project_fact to retrieve it.\n" +
		"- **category / fact_key suggestions**:\n" +
		"  - Environment knowledge: target/, auth/, infra/, business/ (use the environment template for body)\n" +
		"  - Discovery & exploitation: finding/, chain/, exploit/, poc/ (**must** use the attack-chain template to fill the body: entry point, step-by-step attack chain, raw request/response or command, evidence, related vulnerability ID)\n" +
		"- **Division of labor with vulnerability records**: record_vulnerability records deliverable findings; facts record the **full reproduction context** (including failed attempts, bypasses, dependent sessions), and both can be recorded once.\n" +
		"- When updating the same finding, keep the same fact_key for overwrite-write, do not scatter into multiple keys causing context loss."
}

// FactRecordingBlackboardSection is the complete system prompt block for the project blackboard and vulnerability recording (shared by single/multi-agent main agents).
func FactRecordingBlackboardSection(coordinatorDelegate bool) string {
	var b strings.Builder
	b.WriteString("## Project Blackboard (facts) and Vulnerability Records (separated)\n\n")
	b.WriteString("If the current conversation is bound to a project, the system will automatically inject the \"project blackboard index\" (only fact_key + summary). **When the summary is insufficient, you must call ")
	b.WriteString(builtin.ToolGetProjectFact)
	b.WriteString("(fact_key) to retrieve the body; do not fabricate details from the summary.**\n\n")
	b.WriteString(factRecordingIncrementalRhythmBuiltin(coordinatorDelegate, false))
	b.WriteString("\n\n")
	b.WriteString("- **Environment / target / authentication etc. insights** (non-formal-vulnerability entries): use ")
	b.WriteString(builtin.ToolUpsertProjectFact)
	b.WriteString(", fact_key suggests category/slug (e.g., target/primary_domain), same key for overwrite-update; body records port / version / credential characteristics / evidence source.\n")
	b.WriteString("- **Discovery & exploitation context** (audit reproduction): fact_key suggests finding/, chain/, exploit/, poc/ prefixes; **body is required** with the complete attack chain (entry point -> steps -> raw request/response or command -> phenomenon -> related related_vulnerability_id); **do not only write the conclusion**; summary writes the one-line \"what + where + how to verify\" essentials.\n")
	b.WriteString("- **Deliverable vulnerabilities**: use ")
	b.WriteString(builtin.ToolRecordVulnerability)
	b.WriteString(", with title, severity, type, target, proof (POC), impact, remediation suggestions. Before recording, you can first ")
	b.WriteString(builtin.ToolListVulnerabilities)
	b.WriteString(" to dedup; for details use ")
	b.WriteString(builtin.ToolGetVulnerability)
	b.WriteString("(id) (default only current project/session).\n")
	b.WriteString("- The same finding may need to be **recorded once each** (facts record the **complete attack chain and exploit details** for reproduction, vulnerabilities record formal findings). False positives use ")
	b.WriteString(builtin.ToolDeprecateProjectFact)
	b.WriteString(" or vulnerability status false_positive.\n")
	b.WriteString("- When there are many facts, use ")
	b.WriteString(builtin.ToolListProjectFacts)
	b.WriteString(" / ")
	b.WriteString(builtin.ToolSearchProjectFacts)
	b.WriteString(" to search.\n\n")
	b.WriteString(factEdgeRecordingGuidance())
	b.WriteString("\n\n")
	b.WriteString(factRecordingGuidanceBlock())
	b.WriteString("\n\nSeverity levels: critical / high / medium / low / info. Proof must contain sufficient evidence (request-response, screenshot, command output, etc.).")
	return b.String()
}

// FactRecordingSubAgentSection is the record-while-penetrating section for sub-agents (outputs to-be-recorded entries when no tools are available).
func FactRecordingSubAgentSection() string {
	return "## Record While Penetrating\n\n" + factRecordingIncrementalRhythmBuiltin(false, true) + "\n"
}

// FactRecordingBlackboardSectionMarkdown is the Markdown equivalent of FactRecordingBlackboardSection (tool names as literals, for agents/*.md).
func FactRecordingBlackboardSectionMarkdown(coordinatorDelegate bool) string {
	var b strings.Builder
	b.WriteString("## Project Blackboard (facts) and Vulnerability Records (separated)\n\n")
	b.WriteString("If the current conversation is bound to a project, the system will automatically inject the \"project blackboard index\" (only fact_key + summary). **When the summary is insufficient, you must call get_project_fact(fact_key) to retrieve the body; do not fabricate details from the summary.**\n\n")
	b.WriteString(FactRecordingIncrementalRhythmMarkdown(coordinatorDelegate, false))
	b.WriteString("\n\n")
	b.WriteString("- **Environment / target / authentication etc. insights** (non-formal-vulnerabilities): use **upsert_project_fact**, fact_key suggests category/slug (e.g., target/primary_domain), same key for overwrite-update; body records port / version / credential characteristics / evidence source.\n")
	b.WriteString("- **Discovery & exploitation context** (audit reproduction): fact_key suggests finding/, chain/, exploit/, poc/ prefixes; **body is required** with the complete attack chain (entry point -> steps -> raw request/response or command -> phenomenon -> related related_vulnerability_id); **do not only write the conclusion**; summary writes the one-line \"what + where + how to verify\" essentials.\n")
	b.WriteString("- **Deliverable vulnerabilities**: use **record_vulnerability** (title, description, severity, type, target, proof POC, impact, remediation suggestions). Severity levels: critical / high / medium / low / info.\n")
	b.WriteString("- The same finding may need to be **recorded once each** (facts record the reproducible attack chain, vulnerabilities record formal findings). False positives use **deprecate_project_fact** or vulnerability status false_positive.\n")
	b.WriteString("- When there are many facts, use **list_project_facts** / **search_project_facts** to search.\n\n")
	b.WriteString(factEdgeRecordingGuidance())
	b.WriteString("\n\n")
	b.WriteString(factRecordingGuidanceBlock())
	b.WriteString("\n\nSeverity levels: critical / high / medium / low / info. Proof must contain sufficient evidence (request-response, screenshot, command output, etc.).")
	return b.String()
}

// FactEdgeRecordingGuidance returns the agent guidance for writing edges (reused by the project package).
func FactEdgeRecordingGuidance() string { return factEdgeRecordingGuidance() }

// FactRecordingGuidanceBlock returns the fact-writing guidance block (reused by the project package).
func FactRecordingGuidanceBlock() string { return factRecordingGuidanceBlock() }
