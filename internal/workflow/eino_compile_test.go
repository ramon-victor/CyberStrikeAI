package workflow

import (
	"context"
	"path/filepath"
	"testing"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"

	"go.uber.org/zap"
)

func testWorkflowDB(t *testing.T) *database.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := database.NewDB(filepath.Join(dir, "workflow.db"), zap.NewNop())
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func linearStartOutputGraph() string {
	return `{
  "nodes": [
    {"id": "start-1", "type": "start", "label": "开始", "position": {"x": 0, "y": 0}, "config": {}},
    {"id": "out-1", "type": "output", "label": "输出", "position": {"x": 0, "y": 120}, "config": {"output_key": "result", "source_binding": {"from": "inputs", "field": "message"}}}
  ],
  "edges": [
    {"id": "e1", "source": "start-1", "target": "out-1"}
  ],
  "config": {"schema_version": 1}
}`
}

func conditionBranchGraph() string {
	return `{
  "nodes": [
    {"id": "start-1", "type": "start", "label": "开始", "position": {"x": 0, "y": 0}, "config": {}},
    {"id": "cond-1", "type": "condition", "label": "判断", "position": {"x": 0, "y": 80}, "config": {"expression": "{{inputs.message}} == yes"}},
    {"id": "out-yes", "type": "output", "label": "是", "position": {"x": -80, "y": 160}, "config": {"output_key": "branch", "static_value": "yes"}},
    {"id": "out-no", "type": "output", "label": "否", "position": {"x": 80, "y": 160}, "config": {"output_key": "branch", "static_value": "no"}}
  ],
  "edges": [
    {"id": "e1", "source": "start-1", "target": "cond-1"},
    {"id": "e2", "source": "cond-1", "target": "out-yes", "label": "是"},
    {"id": "e3", "source": "cond-1", "target": "out-no", "label": "否"}
  ],
  "config": {"schema_version": 1}
}`
}

func TestValidateGraphJSON_linear(t *testing.T) {
	if err := ValidateGraphJSON(context.Background(), linearStartOutputGraph()); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestCompileEngine_linear(t *testing.T) {
	ctx := context.Background()
	SetCheckpointDir(t.TempDir())
	g, err := parseGraph(linearStartOutputGraph())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := defaultEngine.compile(ctx, g); err != nil {
		t.Fatalf("compile: %v", err)
	}
}

func createTestWorkflowRun(t *testing.T, db *database.DB, runID string) {
	t.Helper()
	if err := db.CreateWorkflowRun(&database.WorkflowRun{
		ID:         runID,
		WorkflowID: "test-wf",
		Status:     "running",
	}); err != nil {
		t.Fatalf("CreateWorkflowRun: %v", err)
	}
}

func TestExecuteEinoGraph_linearStartOutput(t *testing.T) {
	ctx := context.Background()
	SetCheckpointDir(t.TempDir())
	db := testWorkflowDB(t)
	createTestWorkflowRun(t, db, "run-linear")
	g, err := parseGraph(linearStartOutputGraph())
	if err != nil {
		t.Fatal(err)
	}
	state := newWorkflowLocalState(map[string]interface{}{"message": "ping"}, "run-linear")
	args := RunArgs{DB: db}
	if err := executeEinoGraph(ctx, args, "run-linear", "test-wf", 1, g, state); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := state.Outputs["result"]; got != "ping" {
		t.Fatalf("outputs[result] = %v, want ping", got)
	}
	if len(state.Executed) != 2 {
		t.Fatalf("executed nodes = %d, want 2", len(state.Executed))
	}
}

func TestExecuteEinoGraph_conditionBranch(t *testing.T) {
	ctx := context.Background()
	SetCheckpointDir(t.TempDir())
	db := testWorkflowDB(t)
	createTestWorkflowRun(t, db, "run-yes")
	createTestWorkflowRun(t, db, "run-no")
	g, err := parseGraph(conditionBranchGraph())
	if err != nil {
		t.Fatal(err)
	}

	stateYes := newWorkflowLocalState(map[string]interface{}{"message": "yes"}, "run-yes")
	if err := executeEinoGraph(ctx, RunArgs{DB: db}, "run-yes", "test-wf-branch", 1, g, stateYes); err != nil {
		t.Fatalf("execute yes: %v", err)
	}
	if got := stateYes.Outputs["branch"]; got != "yes" {
		t.Fatalf("yes branch output = %v", got)
	}

	stateNo := newWorkflowLocalState(map[string]interface{}{"message": "no"}, "run-no")
	if err := executeEinoGraph(ctx, RunArgs{DB: db}, "run-no", "test-wf-branch", 1, g, stateNo); err != nil {
		t.Fatalf("execute no: %v", err)
	}
	if got := stateNo.Outputs["branch"]; got != "no" {
		t.Fatalf("no branch output = %v", got)
	}
}

func TestRunRoleBoundWorkflow_integration(t *testing.T) {
	ctx := context.Background()
	SetCheckpointDir(t.TempDir())
	db := testWorkflowDB(t)
	graph := linearStartOutputGraph()
	if err := db.UpsertWorkflowDefinition(&database.WorkflowDefinition{
		ID:        "wf-linear",
		Name:      "线性流程",
		Version:   1,
		GraphJSON: graph,
		Enabled:   true,
	}); err != nil {
		t.Fatal(err)
	}
	role := config.RoleConfig{
		Name:           "tester",
		Enabled:        true,
		WorkflowID:     "wf-linear",
		WorkflowPolicy: "auto",
	}
	result, err := RunRoleBoundWorkflow(ctx, RunArgs{
		DB:          db,
		Logger:      zap.NewNop(),
		Role:        role,
		UserMessage: "from-role",
	})
	if err != nil {
		t.Fatalf("RunRoleBoundWorkflow: %v", err)
	}
	if result == nil || result.RunID == "" {
		t.Fatal("expected run result")
	}
}

func TestCompiledCache_reuse(t *testing.T) {
	ctx := context.Background()
	SetCheckpointDir(t.TempDir())
	InvalidateCompiledCache("cache-wf")
	g, err := parseGraph(linearStartOutputGraph())
	if err != nil {
		t.Fatal(err)
	}
	a1, err := defaultEngine.getOrCompile(ctx, "cache-wf", 1, g)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := defaultEngine.getOrCompile(ctx, "cache-wf", 1, g)
	if err != nil {
		t.Fatal(err)
	}
	if a1 != a2 {
		t.Fatal("expected cached artifact pointer reuse")
	}
	InvalidateCompiledCache("cache-wf")
	a3, err := defaultEngine.getOrCompile(ctx, "cache-wf", 1, g)
	if err != nil {
		t.Fatal(err)
	}
	if a1 == a3 {
		t.Fatal("expected new artifact after invalidation")
	}
}
