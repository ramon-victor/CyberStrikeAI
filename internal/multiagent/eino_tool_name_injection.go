package multiagent

import (
	"context"
	"strings"

	"github.com/cloudwego/eino/components/tool"
)

// injectToolNamesOnlyInstruction prepends a compact tool-name-only section into
// the system instruction so the model can reference current callable names.
// toolSearchMiddlewareActive must be true when prependEinoMiddlewares mounted toolsearch (dynamic tools); do not infer this
// by scanning tool names — tool_search is injected by middleware and is usually absent from the pre-split tools list.
func injectToolNamesOnlyInstruction(ctx context.Context, instruction string, tools []tool.BaseTool, toolSearchMiddlewareActive bool) string {
	names := collectToolNames(ctx, tools)
	if len(names) == 0 {
		return strings.TrimSpace(instruction)
	}
	hasToolSearch := toolSearchMiddlewareActive
	if !hasToolSearch {
		for _, n := range names {
			if strings.EqualFold(strings.TrimSpace(n), "tool_search") {
				hasToolSearch = true
				break
			}
		}
	}

	var sb strings.Builder
	sb.WriteString("The following is the tool name index bound to this session (names only, no parameter JSON schemas).\n")
	sb.WriteString("Note: if tool_search is enabled, the list may contain non-resident tools that do not necessarily appear in the tool definitions sent to the model in the current turn. Do not guess parameters by name before seeing the full schema.\n")
	for _, name := range names {
		sb.WriteString("- ")
		sb.WriteString(name)
		sb.WriteByte('\n')
	}
	sb.WriteString("\nUsage rules:\n")
	sb.WriteString("1) The table above is a name index only; it does not contain parameter definitions. Do not guess parameter names, types, enum values, or whether they are required.\n")
	if hasToolSearch {
		sb.WriteString("[Mandatory / Highest Priority] This session has tool_search enabled (dynamic tool pool). Any tool that appears in the name index but whose full parameter schema is not visible in the current request tools definition MUST be searched via tool_search first. Skipping tool_search to save tokens or speed up progress and directly calling a business tool is explicitly forbidden.\n")
		sb.WriteString("2) Default strategy: if you have any uncertainty about a target tool's parameter definition, call tool_search first. It is better to make an extra tool_search call than to blindly call a business tool without seeing its schema.\n")
		sb.WriteString("3) Call order: first tool_search (only required parameter: regex_pattern, a regex matching tool names, e.g. substring nuclei or ^exact_tool_name$) -> in a subsequent turn, confirm the target tool appears in the tools list and you have read its schema -> then make the real call to that tool.\n")
		sb.WriteString("4) tool_search returns only a list of matching tool names; the schema is delivered in the next turn after unlocking. Do not fabricate JSON parameters before the schema appears.\n")
		sb.WriteString("5) Do not fabricate non-existent tool names.\n\n")
	} else {
		sb.WriteString("2) Before calling a specific tool, confirm its parameter requirements (based on the tool definitions in the current request); if unsure, clarify first before calling.\n")
		sb.WriteString("3) Do not fabricate non-existent tool names.\n\n")
	}
	if s := strings.TrimSpace(instruction); s != "" {
		sb.WriteString(s)
	}
	return sb.String()
}

func collectToolNames(ctx context.Context, tools []tool.BaseTool) []string {
	if len(tools) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tools))
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		if t == nil {
			continue
		}
		info, err := t.Info(ctx)
		if err != nil || info == nil {
			continue
		}
		name := strings.TrimSpace(info.Name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, name)
	}
	return out
}
