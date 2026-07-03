package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cyberstrike-ai/internal/database"

	"github.com/google/uuid"
)

func executeNode(ctx context.Context, args RunArgs, runID string, node graphNode, state *WorkflowLocalState) (map[string]any, bool, error) {
	label := node.Label
	if strings.TrimSpace(label) == "" {
		label = node.ID
	}
	nodeRunID := uuid.NewString()
	input := map[string]any{
		"nodeId":   node.ID,
		"nodeType": node.Type,
		"label":    label,
		"inputs":   state.Inputs,
		"previous": state.LastOutput,
	}
	inputJSON, _ := json.Marshal(input)
	if err := args.DB.CreateWorkflowNodeRun(&database.WorkflowNodeRun{
		ID:        nodeRunID,
		RunID:     runID,
		NodeID:    node.ID,
		Status:    "running",
		InputJSON: string(inputJSON),
		StartedAt: time.Now(),
	}); err != nil {
		return nil, false, err
	}
	if args.Progress != nil {
		args.Progress("workflow_node_start", fmt.Sprintf("开始节点：%s", label), map[string]any{
			"workflowRunId": runID,
			"nodeRunId":     nodeRunID,
			"nodeId":        node.ID,
			"nodeType":      node.Type,
			"label":         label,
		})
	}

	result, proceed, status, errText := runBuiltinNode(ctx, args, node, state)
	outputJSON, _ := json.Marshal(result)
	if err := args.DB.FinishWorkflowNodeRun(nodeRunID, status, string(outputJSON), errText); err != nil {
		return nil, false, err
	}
	if status == "skipped" {
		state.Skipped = append(state.Skipped, label)
	} else {
		state.Executed = append(state.Executed, label)
	}
	if args.Progress != nil {
		progressData := map[string]any{
			"workflowRunId": runID,
			"nodeRunId":     nodeRunID,
			"nodeId":        node.ID,
			"nodeType":      node.Type,
			"label":         label,
			"status":        status,
			"output":        result,
		}
		progressMsg := fmt.Sprintf("节点完成：%s（%s）", label, status)
		if strings.EqualFold(node.Type, "condition") {
			matched := false
			if v, ok := result["matched"].(bool); ok {
				matched = v
			}
			expr := cfgString(node.Config, "expression")
			if matched {
				progressMsg = fmt.Sprintf("条件判断：%s → 是", label)
			} else {
				progressMsg = fmt.Sprintf("条件判断：%s → 否", label)
			}
			progressData["expression"] = expr
			progressData["matched"] = matched
		}
		args.Progress("workflow_node_result", progressMsg, progressData)
	}
	state.NodeProceed[node.ID] = proceed
	return result, proceed, nil
}

func emitConditionBranchProgress(args RunArgs, runID string, node graphNode, edges []graphEdge, nodes map[string]graphNode, state *WorkflowLocalState) {
	if args.Progress == nil || len(edges) == 0 {
		return
	}
	for edgeIdx, edge := range edges {
		allowed := edgeAllowed(edge, node, edgeIdx, state)
		target := nodes[edge.Target]
		targetLabel := strings.TrimSpace(target.Label)
		if targetLabel == "" {
			targetLabel = edge.Target
		}
		branchLabel := strings.TrimSpace(edge.Label)
		if branchLabel == "" {
			switch edgeIdx {
			case 0:
				branchLabel = "是"
			case 1:
				branchLabel = "否"
			default:
				branchLabel = fmt.Sprintf("分支 %d", edgeIdx+1)
			}
		}
		cond := firstNonEmpty(cfgString(edge.Config, "condition"), cfgString(edge.Config, "expression"))
		eventType := "workflow_branch_skipped"
		msg := fmt.Sprintf("跳过分支「%s」→ %s", branchLabel, targetLabel)
		if allowed {
			eventType = "workflow_branch_taken"
			msg = fmt.Sprintf("执行分支「%s」→ %s", branchLabel, targetLabel)
		}
		args.Progress(eventType, msg, map[string]any{
			"workflowRunId": runID,
			"nodeId":        node.ID,
			"nodeType":      node.Type,
			"label":         node.Label,
			"branchLabel":   branchLabel,
			"targetId":      edge.Target,
			"targetLabel":   targetLabel,
			"edgeCondition": cond,
			"matched":       conditionMatched(state),
		})
	}
}
