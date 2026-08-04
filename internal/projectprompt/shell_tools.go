package projectprompt

// ShellExecExecuteGuidanceSection 供单代理/多代理系统提示追加：exec 与 execute 分工（尽量短）。
func ShellExecExecuteGuidanceSection() string {
	return `Shell (exec/execute): Prefer a dedicated MCP tool when one is available. Use exec for system commands (pipes, workdir, background &); use execute for scripts under skills/ together with read_file and skill. Split multi-step scans into separate calls; never chain multiple scanners in one shell command. Write long scripts, request bodies, or payloads to the session working directory with write_file first, then run a short exec/execute command; never embed long content in command. Store downloads and temporary files in the "session working directory" specified by the system prompt, never in /tmp.`
}

// ShellExecExecuteGuidanceReconSuffix 侦察子代理可选追加（一行）。
func ShellExecExecuteGuidanceReconSuffix() string {
	return `For enumeration, prefer dedicated MCP tools such as subfinder and amass; do not build long exec/execute chains.`
}
