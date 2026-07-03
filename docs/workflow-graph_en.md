# CyberStrikeAI Graph Orchestration Guide

[中文](workflow-graph.md)

This document explains how to use **Graph Orchestration**: building workflows on the canvas, configuring node types, passing data between nodes, and binding a graph to a role for automatic execution.

---

## 1. Where to find Graph Orchestration

1. Log in to the CyberStrikeAI web UI.  
2. Open **Graph Orchestration** in the left sidebar.  
3. Select an existing workflow from the list, or create a new one.  
4. Drag nodes, draw edges, and configure properties on the canvas.  
5. Fill in **ID**, **Name**, and **Description**, then click **Save**.

Saved workflows can be bound to a role under **Role Management**. When `workflow_policy` is `auto`, chatting with that role runs the bound graph automatically.

---

## 2. Canvas basics

| Action | Description |
|--------|-------------|
| Add node | Click a node type button above the canvas (Start, Tool, Agent, Condition, HITL, Output, End) |
| Connect | Click **Connect**, then click source and target nodes; click **Connect** again to exit connect mode |
| Select | Click a node or edge; properties appear in the right panel |
| Delete selected | Remove the current node or edge |
| Auto layout | Rearrange node positions |
| Delete workflow | Remove the entire workflow definition |

**Requirements:** Every workflow needs at least **one Start node** and **one Output node**. Start nodes must not have incoming edges; Output nodes must not have outgoing edges.

---

## 3. Execution model (read this before configuring)

The engine executes the workflow as a **directed graph**, starting from the **Start** node and following edges to downstream nodes.

During a run, the engine keeps internal state. Template expressions `{{...}}` read from that state:

| Internal state | Template prefix | Meaning |
|----------------|-----------------|---------|
| `inputs` | `{{inputs.xxx}}` | Workflow inputs at start (user message, conversation ID, etc.) |
| `lastOutput` | `{{previous.xxx}}` | Output of the **most recently executed** node |
| `outputs` | `{{outputs.xxx}}` | Global **named variable pool** (written by nodes with an output key) |
| `nodeOutputs` | `{{nodeId.xxx}}` | Full output object of a specific node ID |

### 3.1 What is `previous`?

`{{previous.output}}` is the `output` field of the **immediately preceding executed node**.

- After every node finishes, the engine updates `lastOutput`.
- It is **not** “the node drawn upstream on the canvas”; it is **the previous step in actual execution order**.

Example:

```text
Start → Agent A → Agent B
```

For Agent B, `{{previous.output}}` = Agent A’s output.

With a condition in between:

```text
Start → Agent A → Condition → Agent B
```

For Agent B, `{{previous.output}}` = the **condition node** output (`true` / `false`), **not** Agent A’s result.

### 3.2 What is `outputs`?

`outputs` is a **named variable registry** maintained by the engine during execution.

When an Agent, Tool, or Output node sets an **Output variable name** (`output_key`), the result is stored as:

```text
outputs["your_variable_name"] = node_output
```

Any downstream node can then reference it via `{{outputs.variable_name}}`, even if other nodes sit in between.

Example:

- Agent A **Output variable name**: `agent_result1`
- Agent B **Input source**: `{{outputs.agent_result1}}`

Agent B still receives Agent A’s output even when a condition node lies between them.

### 3.3 When to use `previous` vs `outputs`

| Scenario | Recommended |
|----------|-------------|
| Two nodes are **directly connected**; you only need the last step | `{{previous.output}}` |
| Other nodes sit in between (condition, tool, HITL, etc.) | `{{outputs.variable_name}}` |
| Reference output from an **earlier** node | `{{outputs.variable_name}}` or `{{nodeId.output}}` |
| Condition should test an Agent’s output | `{{outputs.variable_name}} != ""` |
| Read the original user input | `{{inputs.message}}` |

**Rule of thumb:**

- `previous` = last step (chained, adjacent)
- `outputs` = by name (cross-node, look back)

---

## 4. Template syntax

### 4.1 Basic format

```text
{{path.to.value}}
```

Allowed characters in paths: letters, digits, underscore, dot, hyphen. Examples:

```text
{{previous.output}}
{{outputs.agent_result1}}
{{inputs.message}}
{{inputs.conversationId}}
{{previous.matched}}
{{node-abc123.output}}
```

### 4.2 Available paths

| Path | Description |
|------|-------------|
| `{{inputs.message}}` | User message (Start node input) |
| `{{inputs.conversationId}}` | Conversation ID |
| `{{inputs.projectId}}` | Project ID |
| `{{previous.output}}` | Primary output of the previous node |
| `{{previous.matched}}` | Match result of the previous condition node (`true` / `false`) |
| `{{outputs.variable_name}}` | Named output registered by a node |
| `{{nodeId.output}}` | `output` field of the node with that ID |

### 4.3 Condition expressions

Condition nodes and edge conditions support simple comparisons:

```text
{{outputs.agent_result1}} != ""
{{previous.output}} == "ok"
{{outputs.count}} == "100"
```

Rules:

- Use `==` or `!=` for string comparison (leading/trailing spaces and quotes are trimmed)
- Without a comparator, non-empty values that are not `false`, `0`, or `null` are treated as true

---

## 5. Node types and configuration

### 5.1 Start

Workflow entry point; injects user input into `inputs`.

| Field | Description | Default |
|-------|-------------|---------|
| Input keys | Comma-separated input key names | `message, conversationId, projectId` |

Start node output includes: `output`, `message`, `conversationId`, `projectId`.

### 5.2 Agent

Runs an LLM Agent task. Supports multiple modes.

| Field | Description | Default |
|-------|-------------|---------|
| Agent mode | `eino_single` / `deep` / `plan_execute` / `supervisor` | `eino_single` |
| Input source | Template for upstream data | `{{previous.output}}` |
| Node instruction | Task description for this node | empty |
| Output variable name | Key written into `outputs` | `agent_result` |

**Message assembly:**

- Instruction only → send instruction to the Agent  
- Input source only → “Continue based on upstream output: …”  
- Both → combined “upstream input + node instruction”

After execution:

- `previous.output` becomes this node’s response text  
- If **Output variable name** is set, the value is also stored in `outputs[variable_name]`

### 5.3 Tool

Calls an enabled MCP tool.

| Field | Description | Default |
|-------|-------------|---------|
| MCP tool | Tool name (required) | — |
| Argument template | JSON with `{{...}}` templates | `{}` |
| Timeout (seconds) | Optional | empty |

Example argument template:

```json
{"target": "{{inputs.message}}", "port": "443"}
```

If an output variable name is configured, the tool result is written to `outputs`.

### 5.4 Condition

Evaluates an expression and outputs `matched` (`true` / `false`).

| Field | Description | Default |
|-------|-------------|---------|
| Expression | Supports `{{...}}` and `==` / `!=` | `{{previous.output}} != ""` |

**Branching rules:**

- The **first outgoing edge** defaults to the **“yes”** branch (`matched == true`)
- The **second outgoing edge** defaults to the **“no”** branch (`matched == false`)
- Edge labels such as `是` / `否` (or `yes` / `no`, `true` / `false`) help identify branches
- A third or later edge needs a custom **edge condition**

Edge condition examples (select an edge, configure in the right panel):

```text
{{previous.matched}} == "true"
{{previous.matched}} == "false"
```

### 5.5 HITL (human-in-the-loop)

Human approval checkpoint (currently record-only; marks `approved: true` and continues).

| Field | Description | Default |
|-------|-------------|---------|
| Prompt | Supports templates | `Please approve before continuing` |
| Reviewer | `human` / `audit_agent` | `human` |

### 5.6 Output

Writes the final workflow result into `outputs` for summary and chat display.

| Field | Description | Default |
|-------|-------------|---------|
| Output variable name | Required key for the final result | `result` |
| Variable source | Template deciding what to write | `{{previous.output}}` |

**Note:** Output nodes are workflow exits and must not have outgoing edges.

### 5.7 End

Optional node for an end summary template (less common in role-bound flows).

| Field | Description | Default |
|-------|-------------|---------|
| Result template | Supports `{{outputs.xxx}}` | `{{outputs.result}}` |

---

## 6. Edge configuration

Select an **edge** to configure its **condition** in the right panel.

| Scenario | Example |
|----------|---------|
| Filter after a normal node | `{{previous.output}} == "ok"` |
| “Yes” branch from a condition | `{{previous.matched}} == "true"` |
| “No” branch from a condition | `{{previous.matched}} == "false"` |

If no edge condition is set:

- Non-condition nodes: edge is always allowed  
- Condition nodes: yes/no branches are assigned by edge order automatically

---

## 7. Full example: passing Agent output across a condition

### 7.1 Graph structure

```text
Start → Agent (initial value) → Condition → Agent (transform) → Output
                                    ↘ no → Output
```

### 7.2 Node configuration

**Agent 1**

| Field | Value |
|-------|-------|
| Node instruction | Output only `123333333` |
| Output variable name | `agent_result1` |

**Condition**

| Field | Value |
|-------|-------|
| Expression | `{{outputs.agent_result1}} != ""` |

**Agent 2**

| Field | Value |
|-------|-------|
| Input source | `{{outputs.agent_result1}}` |
| Node instruction | Add 100 to the input, then output |
| Output variable name | `agent_result` |

**Output**

| Field | Value |
|-------|-------|
| Output variable name | `result` |
| Variable source | `{{outputs.agent_result}}` |

### 7.3 Common mistakes

| Wrong config | Why it fails |
|--------------|--------------|
| Agent 2 input source = `{{previous.output}}` | `previous` points to the condition node → `true`/`false`, not Agent 1’s text |
| Agent 1 has no output variable name | `outputs.agent_result1` does not exist → empty downstream |
| Condition uses `{{previous.output}}` | Tests the wrong upstream value instead of Agent 1’s named output |

---

## 8. Bind to a role and run

### 8.1 Bind in Role Management

1. Open **Role Management**, edit or create a role.  
2. Select the workflow / graph ID to bind.  
3. Set policy to `auto` (default when `workflow_id` is set).  
4. Save the role.

You can also configure this in role YAML:

```yaml
name: workflow-test
workflow_id: "1233"
workflow_version: latest
workflow_policy: auto
```

### 8.2 Runtime behavior

When a user chats with that role:

1. The engine loads `graph_json` and executes the graph.  
2. The chat UI shows progress events (`workflow_start`, `workflow_node_start`, Agent reasoning, etc.).  
3. When finished, a summary lists all named entries in `outputs`.

If no Output node is reached or no branch matches, `outputs` may be empty and the summary will suggest checking the Output node and branches.

---

## 9. Validation before save

On save, the system checks:

| Rule | Description |
|------|-------------|
| Start node required | At least one `start` node |
| Output node required | At least one `output` node with an output variable name |
| Valid edges | Source and target exist; no self-loops |
| Start has no incoming edges | Start must not be targeted |
| Output has no outgoing edges | Nothing after Output |
| Tool nodes | MCP tool must be selected |
| Condition nodes | Expression required; ideally 1–2 outgoing edges (yes/no) |

---

## 10. Troubleshooting

| Symptom | Likely cause | Fix |
|---------|--------------|-----|
| Downstream gets empty value | Upstream has no output variable name | Set **Output variable name** on upstream; use `{{outputs.xxx}}` downstream |
| Downstream gets `true`/`false` | Used `{{previous.output}}` while previous node is a condition | Use `{{outputs.xxx}}` instead |
| Condition always takes “no” | Expression does not match actual output format | Check Agent output for quotes/newlines; try `!= ""` first |
| No final output | Output node branch not reached | Verify condition wiring; ensure every path reaches an **Output** node |
| Role chat does not run workflow | Role not bound or disabled | Check `workflow_id`, `workflow_policy: auto`, workflow `enabled: true` |
| Tool node fails | Invalid JSON in arguments or tool disabled | Fix argument template; enable the tool in MCP settings |

---

## 11. Best practices

1. **Meaningful names**: Use descriptive output variable names (`scan_result`, `parsed_targets`) instead of reusing `agent_result` everywhere.  
2. **Prefer `outputs` for cross-node data**: If a condition, tool, or HITL node might sit in between, use named variables.  
3. **Use `previous` only for direct links**: `A → B` with nothing in between is the ideal case for `{{previous.output}}`.  
4. **Conditions should reference source data**: When testing Agent output, use `{{outputs.xxx}}` unless the condition immediately follows that Agent.  
5. **Every path needs an exit**: Ensure both yes and no branches eventually reach an **Output** node (or your intended end).  
6. **Validate with a simple run**: Use fixed-string outputs to verify data flow before swapping in real business logic.

---

## 12. Code references (for developers)

| Module | Path |
|--------|------|
| Execution engine | `internal/workflow/runner.go` |
| Canvas UI | `web/static/js/workflows.js` |
| Workflow API | `internal/handler/workflow.go` |
| Role binding | `internal/config/config.go` (`workflow_id` field) |
