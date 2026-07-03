package workflow

import (
	"context"

	"github.com/cloudwego/eino/compose"
)

// compileAgentSubgraph wraps an Agent canvas node as an Eino subgraph (AddGraphNode best practice).
func compileAgentSubgraph(_ context.Context, node graphNode) (compose.AnyGraph, error) {
	n := node
	innerID := n.ID + "__agent"
	g := compose.NewGraph[WorkflowNodeOutput, WorkflowNodeOutput]()
	_ = g.AddLambdaNode(innerID, compose.InvokableLambda(func(runCtx context.Context, _ WorkflowNodeOutput) (WorkflowNodeOutput, error) {
		return runWorkflowNodeLambda(runCtx, n)
	}))
	if err := g.AddEdge(compose.START, innerID); err != nil {
		return nil, err
	}
	if err := g.AddEdge(innerID, compose.END); err != nil {
		return nil, err
	}
	return g, nil
}
