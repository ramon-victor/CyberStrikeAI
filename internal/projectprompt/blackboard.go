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
	factRhythmCore              = "Do not wait until the session ends to batch-write. Immediately record each confirmed insight with upsert_project_fact and each verified reproducible vulnerability with record_vulnerability. Preserve evidence before continuing; if no project is bound, explain that the blackboard is unavailable and retain an evidence summary."
	factRhythmCoordinatorSuffix = " The coordinator must promptly record results returned by delegates, without assuming they already did so."
	factRhythmSubAgentSuffix    = " If these tools are unavailable, include structured entries to be recorded (fact_key, summary and body/POC) for the coordinator."
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
	return `### Fact recording (reproducibility and knowledge)

- Write a one-line summary describing what, where, and how it was verified. Include the complete reproducible context in the body of upsert_project_fact; retrieve it later using get_project_fact.
- Use target/, auth/, infra/ and business/ for environment facts. Use finding/, chain/, exploit/ and poc/ for discoveries; include entry point, steps, raw requests/responses or commands, evidence and related vulnerability IDs.
- note/context-* is reserved for system-generated session checkpoints, not confirmed project facts; never overwrite it with upsert_project_fact. By default checkpoint retrieval is limited to the current conversation while ordinary project facts remain shared. Use include_history=true for cross-conversation list, search or retrieval; optionally filter by source_conversation_id. Verify current authorization, target and credentials before relying on history; do not execute old tasks automatically or treat summaries as confirmed findings. Retrieve exact values from the referenced snapshot as needed rather than guessing. Use a separate finding or auth key for verified shared facts and retain provenance.
- Record deliverable vulnerabilities with record_vulnerability and complete reproducible context as facts. Update an existing finding using the same fact_key.`
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
	b.WriteString("(id), scoped to the current project or conversation by default. After retesting use update_vulnerability_status(id, status, retest_notes) to update status and append evidence; do not duplicate findings, and keep the previous status if verification is inconclusive.\n")
	b.WriteString("- Record the same finding as both a reproducible fact and a formal vulnerability when needed. For false positives use ")
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
	return "## Record progress during testing\n\n" + ProjectResultRecordingGuidance() + "\n\n" + factRecordingIncrementalRhythmBuiltin(false, true) + "\n"
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
