package projectprompt

// ShellExecExecuteGuidanceSection is appended to single-agent/multi-agent system prompts: division of labor between exec and execute (kept short).
func ShellExecExecuteGuidanceSection() string {
	return `Shell (exec/execute): prefer dedicated MCP tools when available; use exec for system commands (pipes, workdir, background &); use execute for scripts under skills/ (paired with read_file, skill); split multi-step scanning into separate calls, never chain multiple scanners in a single shell. Downloads / temporary files must be written to the "session working directory" in the system prompt; do not use /tmp.`
}

// ShellExecExecuteGuidanceReconSuffix is optionally appended to reconnaissance sub-agents (one line).
func ShellExecExecuteGuidanceReconSuffix() string {
	return `Prefer dedicated MCP tools (e.g., subfinder, amass) for enumeration; do not chain long exec/execute calls.`
}
