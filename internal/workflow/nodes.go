package workflow

import (
	"context"
	"fmt"
	"strings"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/multiagent"
)

func runBuiltinNode(ctx context.Context, args RunArgs, node graphNode, state *WorkflowLocalState) (map[string]any, bool, string, string) {
	cfg := node.Config
	switch strings.ToLower(strings.TrimSpace(node.Type)) {
	case "start":
		out := map[string]any{
			"output":         state.Inputs["message"],
			"message":        state.Inputs["message"],
			"conversationId": state.Inputs["conversationId"],
			"projectId":      state.Inputs["projectId"],
		}
		return out, true, "completed", ""
	case "condition":
		expr := cfgString(cfg, "expression")
		ok := evalCondition(expr, state)
		out := map[string]any{"output": ok, "condition": expr, "matched": ok}
		return out, true, "completed", ""
	case "output":
		key := cfgString(cfg, "output_key")
		if key == "" {
			key = "result"
		}
		var value any
		if v := cfgString(cfg, "static_value"); v != "" {
			value = v
		} else {
			value = resolveOutputSourceBinding(cfg, state)
		}
		state.Outputs[key] = value
		return map[string]any{"output": value, "outputs": map[string]any{key: value}}, true, "completed", ""
	case "end":
		value := resolveOutputSourceBinding(cfg, state)
		if b, ok := parseFieldBinding(cfg, "result_binding"); ok {
			value = resolveBinding(b, state)
		}
		return map[string]any{"output": value}, false, "completed", ""
	case "tool":
		return runToolNode(ctx, args, node, state)
	case "agent":
		return runAgentNode(ctx, args, node, state)
	case "hitl":
		return runHITLNode(args, node, state)
	default:
		reason := "未知节点类型"
		return map[string]any{"output": "", "skipped": true, "reason": reason, "node_type": node.Type}, true, "skipped", reason
	}
}

func runToolNode(ctx context.Context, args RunArgs, node graphNode, state *WorkflowLocalState) (map[string]any, bool, string, string) {
	toolName := cfgString(node.Config, "tool_name")
	if toolName == "" {
		errText := "工具节点未选择 MCP 工具"
		return map[string]any{"output": "", "error": errText}, false, "failed", errText
	}
	if args.Agent == nil {
		errText := "工具节点执行失败：Agent 为空"
		return map[string]any{"output": "", "tool_name": toolName, "error": errText}, false, "failed", errText
	}
	toolArgs, err := resolveToolArguments(node.Config, state)
	if err != nil {
		errText := fmt.Sprintf("工具参数不是合法 JSON：%v", err)
		return map[string]any{"output": "", "tool_name": toolName, "error": errText}, false, "failed", errText
	}
	if args.Progress != nil {
		args.Progress("workflow_tool_start", fmt.Sprintf("调用工具：%s", toolName), map[string]any{
			"nodeId": node.ID,
			"tool":   toolName,
			"args":   toolArgs,
		})
	}
	result, err := args.Agent.ExecuteMCPToolForConversation(ctx, args.ConversationID, toolName, toolArgs)
	if err != nil {
		errText := err.Error()
		return map[string]any{"output": "", "tool_name": toolName, "arguments": toolArgs, "error": errText}, false, "failed", errText
	}
	output := ""
	executionID := ""
	isError := false
	if result != nil {
		output = result.Result
		executionID = result.ExecutionID
		isError = result.IsError
	}
	out := map[string]any{
		"output":       output,
		"tool_name":    toolName,
		"arguments":    toolArgs,
		"execution_id": executionID,
		"is_error":     isError,
	}
	if key := cfgString(node.Config, "output_key"); key != "" {
		state.Outputs[key] = output
	}
	if isError {
		errText := strings.TrimSpace(output)
		if errText == "" {
			errText = "工具返回错误"
		}
		return out, false, "failed", errText
	}
	return out, true, "completed", ""
}

func runAgentNode(ctx context.Context, args RunArgs, node graphNode, state *WorkflowLocalState) (map[string]any, bool, string, string) {
	if args.AppCfg == nil || args.Agent == nil {
		errText := "Agent 节点执行失败：应用配置或 Agent 为空"
		return map[string]any{"output": "", "error": errText}, false, "failed", errText
	}
	mode := strings.ToLower(cfgString(node.Config, "agent_mode"))
	if mode == "" {
		mode = "eino_single"
	}
	inputSource := resolveNodeInputBinding(node.Config, state)
	message := buildAgentNodeMessage(node, state, inputSource)
	var result *multiagent.RunResult
	var err error
	state.SegmentMaxIteration = 0
	agentProgress := workflowAgentProgress(args.Progress, state, node)
	switch mode {
	case "eino_single", "single", "chat":
		result, err = multiagent.RunEinoSingleChatModelAgent(
			ctx,
			args.AppCfg,
			&args.AppCfg.MultiAgent,
			args.Agent,
			args.DB,
			args.Logger,
			args.ConversationID,
			args.ProjectID,
			message,
			args.History,
			args.RoleTools,
			agentProgress,
			nil,
			args.SystemPromptExtra,
		)
	default:
		result, err = multiagent.RunDeepAgent(
			ctx,
			args.AppCfg,
			&args.AppCfg.MultiAgent,
			args.Agent,
			args.DB,
			args.Logger,
			args.ConversationID,
			args.ProjectID,
			message,
			args.History,
			args.RoleTools,
			agentProgress,
			args.AgentsMarkdownDir,
			mode,
			nil,
			args.SystemPromptExtra,
		)
	}
	if err != nil {
		errText := err.Error()
		state.MainIterationOffset += state.SegmentMaxIteration
		return map[string]any{"output": "", "mode": mode, "error": errText}, false, "failed", errText
	}
	state.MainIterationOffset += state.SegmentMaxIteration
	response := ""
	mcpIDs := []string{}
	if result != nil {
		response = result.Response
		mcpIDs = result.MCPExecutionIDs
	}
	if args.Progress != nil {
		args.Progress("workflow_agent_output", response, map[string]any{
			"nodeId":          node.ID,
			"label":           firstNonEmpty(node.Label, node.ID),
			"mode":            mode,
			"inputSource":     inputSource,
			"inputPreview":    truncateWorkflowPreview(inputSource, 500),
			"mcpExecutionIds": mcpIDs,
		})
	}
	if key := cfgString(node.Config, "output_key"); key != "" {
		state.Outputs[key] = response
	}
	return map[string]any{
		"output":            response,
		"mode":              mode,
		"mcp_execution_ids": mcpIDs,
	}, true, "completed", ""
}

func buildAgentNodeMessage(node graphNode, state *WorkflowLocalState, upstreamInput string) string {
	instruction := strings.TrimSpace(cfgString(node.Config, "instruction"))
	upstreamInput = strings.TrimSpace(upstreamInput)
	if instruction == "" {
		if upstreamInput != "" {
			return fmt.Sprintf("请基于上游节点输出继续处理：\n%s", upstreamInput)
		}
		return fmt.Sprintf("请基于上游节点输出继续处理：\n%v", state.LastOutput["output"])
	}
	if upstreamInput == "" {
		return instruction
	}
	return strings.TrimSpace(fmt.Sprintf("上游输入：\n%s\n\n节点指令：\n%s", upstreamInput, instruction))
}

func workflowAgentProgress(progress agent.ProgressCallback, state *WorkflowLocalState, node graphNode) agent.ProgressCallback {
	if progress == nil {
		return nil
	}
	return func(eventType, message string, data interface{}) {
		switch eventType {
		case "response_start", "response_delta", "response", "done":
			return
		default:
			enrichWorkflowAgentEventData(data, state, node)
			if eventType == "iteration" {
				applyWorkflowMainIterationOffset(data, state)
			}
			progress(eventType, message, data)
		}
	}
}

func enrichWorkflowAgentEventData(data interface{}, state *WorkflowLocalState, node graphNode) {
	m, ok := data.(map[string]interface{})
	if !ok || m == nil {
		return
	}
	if node.ID != "" {
		m["workflowNodeId"] = node.ID
	}
	if state != nil && strings.TrimSpace(state.WorkflowRunID) != "" {
		m["workflowRunId"] = state.WorkflowRunID
	}
}

func applyWorkflowMainIterationOffset(data interface{}, state *WorkflowLocalState) {
	if state == nil {
		return
	}
	m, ok := data.(map[string]interface{})
	if !ok || m == nil {
		return
	}
	scope, _ := m["einoScope"].(string)
	if strings.TrimSpace(scope) != "main" {
		return
	}
	raw := iterationNumberFromProgressData(m)
	if raw <= 0 {
		return
	}
	if raw > state.SegmentMaxIteration {
		state.SegmentMaxIteration = raw
	}
	m["iteration"] = raw + state.MainIterationOffset
}

func iterationNumberFromProgressData(m map[string]interface{}) int {
	switch v := m["iteration"].(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	default:
		return 0
	}
}

func runHITLNode(args RunArgs, node graphNode, state *WorkflowLocalState) (map[string]any, bool, string, string) {
	prompt := resolveHITLPromptBinding(node.Config, state)
	reviewer := cfgString(node.Config, "reviewer")
	if reviewer == "" {
		reviewer = "human"
	}
	approved := true
	if state != nil && state.Inputs != nil {
		if v, ok := state.Inputs["_hitl_approved"]; ok {
			approved = fmt.Sprint(v) == "true"
		}
	}
	if !approved {
		reason := "人工审批已拒绝"
		if state != nil && state.Inputs != nil {
			if v, ok := state.Inputs["_hitl_comment"]; ok {
				if s := strings.TrimSpace(fmt.Sprint(v)); s != "" {
					reason = s
				}
			}
		}
		return map[string]any{"output": "", "prompt": prompt, "approved": false, "mode": "interactive"}, false, "failed", reason
	}
	if args.Progress != nil {
		args.Progress("workflow_hitl_checkpoint", "人工确认节点已通过", map[string]any{
			"nodeId":   node.ID,
			"prompt":   prompt,
			"reviewer": reviewer,
			"mode":     "interactive",
			"approved": true,
		})
	}
	return map[string]any{
		"output":   prompt,
		"prompt":   prompt,
		"reviewer": reviewer,
		"approved": true,
		"mode":     "interactive",
	}, true, "completed", ""
}
