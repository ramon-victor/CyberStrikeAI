package handler

import (
	"strings"

	"cyberstrike-ai/internal/database"
)

// WebshellSkillHintDefault Skills note shared by the conversation page and Eino single-agent flow, appended to the webshell context,
// for AI reference when choosing the skill loading entrypoint.
const WebshellSkillHintDefault = "Use the built-in `skill` tool in Multi-Agent / Eino DeepAgent conversations to load Skills progressively."

// WebshellSkillHintMultiAgent Skills note used during Multi-Agent / Eino multi-agent preparation
const WebshellSkillHintMultiAgent = "Use the built-in Eino multi-agent `skill` tool for Skills."

// webshellAssistantToolList Tool list the AI assistant may use in WebShell context, shown to the model.
// Note: this is only a display string; actual permission limits are set by caller roleTools slices.
const webshellAssistantToolList = "webshell_exec, webshell_file_list, webshell_file_read, webshell_file_write, record_vulnerability, list_vulnerabilities, get_vulnerability, upsert_project_fact, get_project_fact, list_project_facts, search_project_facts, deprecate_project_fact, restore_project_fact, list_knowledge_risk_types, search_knowledge_base"

// BuildWebshellAssistantContext builds the AI assistant context prompt from connection info and the raw user message.
// The context includes connection ID, remark, target OS with command-set guidance, response encoding, available tool list, Skills loading entrypoint,
// and the final user request. Callers only choose the skillHint text; default is WebshellSkillHintDefault.
//
// This logic is shared to avoid copy-paste across agent.go, multi_agent_prepare.go, and other callers,
// and to ensure OS/Encoding prompt changes are edited once, tested once, and applied everywhere.
func BuildWebshellAssistantContext(conn *database.WebShellConnection, skillHint, userMsg string) string {
	if conn == nil {
		// Fallback: callers already guarantee conn is non-nil; this defensively returns the original message.
		return userMsg
	}
	remark := conn.Remark
	if remark == "" {
		remark = conn.URL
	}

	targetOS := resolveWebshellOS(conn.OS, conn.Type) // normalized to "linux" / "windows"
	encoding := normalizeWebshellEncoding(conn.Encoding)
	if skillHint == "" {
		skillHint = WebshellSkillHintDefault
	}

	var b strings.Builder
	b.Grow(512 + len(userMsg))

	b.WriteString("[WebShell assistant context] Connection ID: ")
	b.WriteString(conn.ID)
	b.WriteString(", Remark: ")
	b.WriteString(remark)
	b.WriteByte('\n')

	// Target system: explicitly tell the AI which command sets are valid to avoid sending ls/cat/rm to Windows.
	b.WriteString("- Target system: ")
	b.WriteString(describeTargetOSForPrompt(targetOS))
	b.WriteByte('\n')

	// Response encoding: mention only non-auto values; auto is handled by the backend to avoid distracting the model.
	if encHint := describeEncodingForPrompt(encoding); encHint != "" {
		b.WriteString("- Response encoding: ")
		b.WriteString(encHint)
		b.WriteByte('\n')
	}

	// Tool list and connection_id constraint: keep the existing expression style the AI is already familiar with.
	b.WriteString("Available tools (use only when operating on this connection; set connection_id to \"")
	b.WriteString(conn.ID)
	b.WriteString("\"): ")
	b.WriteString(webshellAssistantToolList)
	b.WriteString(" Record findings while testing: upsert_project_fact for each confirmed new fact and record_vulnerability for each verified vulnerability; do not wait until the session ends. ")
	b.WriteString(skillHint)
	b.WriteString("\n\nUser request: ")
	b.WriteString(userMsg)

	return b.String()
}

// describeTargetOSForPrompt returns the description, recommended command set, and counterexamples for an OS,
// covering the six most common file-management actions (list/read/delete/rename/mkdir/find) so the AI can copy them directly.
func describeTargetOSForPrompt(targetOS string) string {
	switch targetOS {
	case "windows":
		return "Windows (recommended cmd/PowerShell: dir /a, type, del /q /f, move /y, md, ren; " +
			"find files with `dir /s /b filter-term` or PowerShell `Get-ChildItem -Recurse`; " +
			"avoid Unix commands such as ls / cat / rm / mv / find, otherwise Windows will return a not-recognized command error)"
	case "linux":
		return "Linux/Unix (recommended sh/bash: ls -la, cat, rm -f, mv, mkdir -p; " +
			"find files with `find /path -name '*pattern*'`; " +
			"avoid Windows commands such as dir, type, del, and move)"
	default:
		// This should not happen because resolveWebshellOS already has a fallback.
		return "Unknown (run `uname || ver` first, then choose the command set)"
	}
}

// describeEncodingForPrompt returns a human-readable response encoding description; auto returns an empty string to reduce tokens.
func describeEncodingForPrompt(encoding string) string {
	switch encoding {
	case "utf-8":
		return "UTF-8 (target is native UTF-8; no extra decoding needed)"
	case "gbk":
		return "GBK (common on Simplified Chinese Windows; backend transcodes to UTF-8; many \\uFFFD replacement characters indicate command failure or an encoding mismatch)"
	case "gb18030":
		return "GB18030 (backend transcodes to UTF-8)"
	default:
		return ""
	}
}
