package attackchain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/openai"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Builder is an attack chain builder.
type Builder struct {
	db           *database.DB
	logger       *zap.Logger
	openAIClient *openai.Client
	openAIConfig *config.OpenAIConfig
	tokenCounter agent.TokenCounter
	maxTokens    int // max token limit, default 100000
}

// Node is an attack chain node (uses types from the database package).
type Node = database.AttackChainNode

// Edge is an attack chain edge (uses types from the database package).
type Edge = database.AttackChainEdge

// Chain 完整的攻击链
type Chain struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// NewBuilder creates a new attack chain builder.
func NewBuilder(db *database.DB, openAIConfig *config.OpenAIConfig, logger *zap.Logger) *Builder {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}
	httpClient := &http.Client{Timeout: 5 * time.Minute, Transport: transport}

	// Prefer the unified token cap from the config file (config.yaml -> openai.max_total_tokens)
	maxTokens := 0
	if openAIConfig != nil && openAIConfig.MaxTotalTokens > 0 {
		maxTokens = openAIConfig.MaxTotalTokens
	} else if openAIConfig != nil {
	} else if openAIConfig != nil {
		model := strings.ToLower(openAIConfig.Model)
		if strings.Contains(model, "gpt-4") {
			maxTokens = 128000 // gpt-4 typically supports 128k
		} else if strings.Contains(model, "gpt-3.5") {
			maxTokens = 16000 // gpt-3.5-turbo typically supports 16k
		} else if strings.Contains(model, "deepseek") {
			maxTokens = 131072 // deepseek-chat typically supports 131k
		} else {
			maxTokens = 100000 // fallback default
		}
	} else {
		// No OpenAI config; use fallback value to avoid zero
		maxTokens = 100000
	}

	return &Builder{
		db:           db,
		logger:       logger,
		openAIClient: openai.NewClient(openAIConfig, httpClient, logger),
		openAIConfig: openAIConfig,
		tokenCounter: agent.NewTikTokenCounter(),
		maxTokens:    maxTokens,
	}
}

// BuildChainFromConversation builds an attack chain from a conversation (single LLM call; input is the last_react trace of the current task turn, aligned with the continue-conversation scope).
func (b *Builder) BuildChainFromConversation(ctx context.Context, conversationID string) (*Chain, error) {
	b.logger.Info("Starting attack chain construction (simplified version)", zap.String("conversationId", conversationID))

	// 0. First check if there are actual tool execution records
	messages, err := b.db.GetMessages(conversationID)
	if err != nil {
		return nil, fmt.Errorf("failed to get conversation messages: %w", err)
	}

	if len(messages) == 0 {
		b.logger.Info("No data in conversation", zap.String("conversationId", conversationID))
		return &Chain{Nodes: []Node{}, Edges: []Edge{}}, nil
	}

	// Check if there are actual tool executions: assistant's mcp_execution_ids, or tool_call/tool_result in process details
	// (in multi-agent mode, MCP may not return execution_id so IDs may be empty, but tools were executed via Eino and written to process_details)
	hasToolExecutions := false
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(messages[i].Role, "assistant") {
			if len(messages[i].MCPExecutionIDs) > 0 {
				hasToolExecutions = true
				break
			}
		}
	}
	if !hasToolExecutions {
		if pdOK, err := b.db.ConversationHasToolProcessDetails(conversationID); err != nil {
			b.logger.Warn("failed to query process details to determine tool execution", zap.Error(err))
		} else if pdOK {
			hasToolExecutions = true
		}
	}

	// Check if the task was cancelled (by checking the last assistant message content or process_details)
	taskCancelled := false
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(messages[i].Role, "assistant") {
			content := strings.ToLower(messages[i].Content)
		if strings.Contains(content, "取消") || strings.Contains(content, "cancelled") {
				taskCancelled = true
			}
			break
		}
	}

	// If the task was cancelled and there were no actual tool executions, return an empty attack chain
	if taskCancelled && !hasToolExecutions {
		b.logger.Info("task cancelled with no actual tool executions, returning empty attack chain",
			zap.String("conversationId", conversationID),
			zap.Bool("taskCancelled", taskCancelled),
			zap.Bool("hasToolExecutions", hasToolExecutions))
		return &Chain{Nodes: []Node{}, Edges: []Edge{}}, nil
	}

	// If there are no actual tool executions, also return an empty attack chain (avoid AI fabrication)
	if !hasToolExecutions {
		b.logger.Info("no actual tool execution records, returning empty attack chain",
			zap.String("conversationId", conversationID))
		return &Chain{Nodes: []Node{}, Edges: []Edge{}}, nil
	}

	// 1. First try to get saved last-round ReAct input and output from database
	reactInputJSON, modelOutput, err := b.db.GetAgentTrace(conversationID)
	if err != nil {
		b.logger.Warn("failed to get saved ReAct data, will use message history instead", zap.Error(err))
		// Continue with original logic
		reactInputJSON = ""
		modelOutput = ""
	}

	// var userInput string
	var reactInputFinal string
	var dataSource string // track data source

	// Prefer the stored agent trace (same source as continue-conversation loadHistoryFromAgentTrace), trimmed to "current task turn"
	if reactInputJSON != "" {
		trimmedJSON := agent.ExtractLastUserTurnTraceJSON(reactInputJSON)
		hash := sha256.Sum256([]byte(trimmedJSON))
		reactInputHash := hex.EncodeToString(hash[:])[:16]

		var messageCount int
		if msgs, parseErr := agent.ParseTraceMessages(trimmedJSON); parseErr == nil {
			messageCount = len(msgs)
			msgs = agent.MergeAssistantTraceOutput(msgs, modelOutput)
			reactInputFinal = b.formatAgentTraceFromChatMessages(msgs)
		} else {
			b.logger.Warn("failed to parse agent trace, falling back to raw JSON formatting", zap.Error(parseErr))
			reactInputFinal = b.formatAgentTraceInputFromJSON(trimmedJSON)
			if strings.TrimSpace(modelOutput) != "" {
				reactInputFinal += "\n\n## Assistant Conclusion (last_react_output)\n\n" + modelOutput
			}
		}

		dataSource = "last_user_turn_agent_trace"
		dataSource = "last_user_turn_agent_trace"
		b.logger.Info("building attack chain using current task turn agent trace (aligned with continue-conversation context scope)",
			zap.String("conversationId", conversationID),
			zap.String("dataSource", dataSource),
			zap.Int("traceInputSizeBeforeTrim", len(reactInputJSON)),
			zap.Int("traceInputSizeAfterTrim", len(trimmedJSON)),
			zap.Int("messageCount", messageCount),
			zap.String("reactInputHash", reactInputHash),
			zap.Int("modelOutputSize", len(modelOutput)))
	} else {
		// 2. If there is no saved ReAct data, build from conversation messages
		dataSource = "messages_table"
		b.logger.Info("building ReAct data from message history",
			zap.String("conversationId", conversationID),
			zap.String("dataSource", dataSource),
			zap.Int("messageCount", len(messages)))

		// Extract user input (last user message)
		for i := len(messages) - 1; i >= 0; i-- {
			if strings.EqualFold(messages[i].Role, "user") {
				// userInput = messages[i].Content
				break
			}
		}

		// Extract the last round of ReAct input (history messages + current user input)
		reactInputFinal = b.buildAgentTraceInput(messages)

		// Extract the model's last output (last assistant message)
		for i := len(messages) - 1; i >= 0; i-- {
			if strings.EqualFold(messages[i].Role, "assistant") {
				modelOutput = messages[i].Content
				break
			}
		}
	}

	// Multi-agent: the saved trace column may only contain the first-round user message without tool traces; supplement with the last assistant turn's process details (align with single-agent complete trace)
	hasMCPOnAssistant := false
	var lastAssistantID string
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(messages[i].Role, "assistant") {
			lastAssistantID = messages[i].ID
			if len(messages[i].MCPExecutionIDs) > 0 {
				hasMCPOnAssistant = true
			}
			break
		}
	}
	if lastAssistantID != "" {
		pdHasTools, _ := b.db.ConversationHasToolProcessDetails(conversationID)
		if pdHasTools && !(hasMCPOnAssistant && reactInputContainsToolTrace(reactInputJSON)) {
			detailsMap, err := b.db.GetProcessDetailsByConversation(conversationID)
			if err != nil {
			b.logger.Warn("failed to load process details for attack chain", zap.Error(err))
			} else if dets := detailsMap[lastAssistantID]; len(dets) > 0 {
				extra := b.formatProcessDetailsForAttackChain(dets)
				if strings.TrimSpace(extra) != "" {
					reactInputFinal = reactInputFinal + "\n\n## Execution Process and Tool Records (including multi-agent orchestration and subtasks)\n\n" + extra
				b.logger.Info("attack chain input supplemented with process details",
					zap.String("conversationId", conversationID),
					zap.String("messageId", lastAssistantID),
					zap.Int("detailEvents", len(dets)))
				}
			}
		}
	}

	// 3. Compress input by token budget, then build prompt (avoid exceeding model context)
	reactInputFinal, modelOutput, _ = b.fitAttackChainPayload(reactInputFinal, modelOutput)

	// 4. Build prompt and call LLM once (assistant conclusion already merged into trace — don't pass it again)
	promptAssistantOut := modelOutput
	if reactInputJSON != "" {
		promptAssistantOut = ""
	}
	prompt := b.buildSimplePrompt(reactInputFinal, promptAssistantOut)
	// fmt.Println(prompt)
	// 6. Call AI to generate attack chain (one-shot, no post-processing)
	chainJSON, err := b.callAIForChainGeneration(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("AI generation failed: %w", err)
	}

	// 7. Parse JSON and generate node/edge IDs (frontend requires valid IDs)
	chainData, err := b.parseChainJSON(chainJSON)
	if err != nil {
		// If parsing fails, return empty chain — let the frontend handle the error
		b.logger.Warn("failed to parse attack chain JSON", zap.Error(err), zap.String("raw_json", chainJSON))
		return &Chain{
			Nodes: []Node{},
			Edges: []Edge{},
		}, nil
	}

	b.logger.Info("attack chain construction completed",
		zap.String("conversationId", conversationID),
		zap.String("dataSource", dataSource),
		zap.Int("nodes", len(chainData.Nodes)),
		zap.Int("edges", len(chainData.Edges)))

	// Save to database (for subsequent loading)
	if err := b.saveChain(conversationID, chainData.Nodes, chainData.Edges); err != nil {
		b.logger.Warn("failed to save attack chain to database", zap.Error(err))
		// Even if saving fails, return data to the frontend
	}

	// Return directly without any post-processing or validation
	return chainData, nil
}

// reactInputContainsToolTrace checks whether the saved ReAct JSON contains parseable tool-call traces (true when single-agent saves the full trace).
func reactInputContainsToolTrace(reactInputJSON string) bool {
	s := strings.TrimSpace(reactInputJSON)
	if s == "" {
		return false
	}
	return strings.Contains(s, "tool_calls") ||
		strings.Contains(s, "tool_call_id") ||
		strings.Contains(s, `"role":"tool"`) ||
		strings.Contains(s, `"role": "tool"`)
}

// formatProcessDetailsForAttackChain formats the last assistant turn's process details as input for attack chain analysis (covers incomplete last_react_input in multi-agent scenarios).
func (b *Builder) formatProcessDetailsForAttackChain(details []database.ProcessDetail) string {
	if len(details) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, d := range details {
		// Goal: output the full iteration from the perspective of the main agent (orchestrator)
		// - Keep: orchestrator tool calls/results, subtask dispatches to sub-agents, sub-agent final replies (excluding reasoning)
		// - Drop: noise such as thinking/planning/progress, sub-agent tool details and reasoning process
		if d.EventType == "progress" || d.EventType == "thinking" || d.EventType == "reasoning_chain" || d.EventType == "planning" {
			continue
		}

		// Parse data (JSON string) to identify einoRole / toolName, etc.
		var dataMap map[string]interface{}
		if strings.TrimSpace(d.Data) != "" {
			_ = json.Unmarshal([]byte(d.Data), &dataMap)
		}
		einoRole := ""
		if v, ok := dataMap["einoRole"]; ok {
			einoRole = strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
		}
		toolName := ""
		if v, ok := dataMap["toolName"]; ok {
			toolName = strings.TrimSpace(fmt.Sprint(v))
		}

		// 1) Orchestrator tool calls/results: keep (these are "what tools the main agent called")
		if (d.EventType == "tool_call" || d.EventType == "tool_result" || d.EventType == "tool_calls_detected" || d.EventType == "iteration") && einoRole == "orchestrator" {
			sb.WriteString("[")
			sb.WriteString(d.EventType)
			sb.WriteString("] ")
			sb.WriteString(strings.TrimSpace(d.Message))
			sb.WriteString("\n")
			if strings.TrimSpace(d.Data) != "" {
				sb.WriteString(d.Data)
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
			continue
		}

		// 2) Sub-agent dispatch: tool_call(toolName=="task") represents the orchestrator dispatching a subtask; keep (only the task, not sub-agent reasoning)
		if d.EventType == "tool_call" && strings.EqualFold(toolName, "task") {
			sb.WriteString("[dispatch_subagent_task] ")
			sb.WriteString(strings.TrimSpace(d.Message))
			sb.WriteString("\n")
			if strings.TrimSpace(d.Data) != "" {
				sb.WriteString(d.Data)
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
			continue
		}

		// 3) Sub-agent final reply: keep (only the final output, not the analysis process)
		if d.EventType == "eino_agent_reply" && einoRole == "sub" {
			sb.WriteString("[subagent_final_reply] ")
			sb.WriteString(strings.TrimSpace(d.Message))
			sb.WriteString("\n")
			// data contains metadata like einoAgent — keeping it helps trace "which sub-agent said this"
			if strings.TrimSpace(d.Data) != "" {
				sb.WriteString(d.Data)
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
			continue
		}

		// Other events are dropped by default to avoid stuffing sub-agent tool details/reasoning into the prompt, deviating from the "main agent one-round iteration" perspective.
	}
	return strings.TrimSpace(sb.String())
}

// buildAgentTraceInput builds the last round of ReAct input (starting from the last user message, excluding earlier turns).
func (b *Builder) buildAgentTraceInput(messages []database.Message) string {
	start := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(messages[i].Role, "user") {
			start = i
			break
		}
	}
	var builder strings.Builder
	for _, msg := range messages[start:] {
		builder.WriteString(fmt.Sprintf("[%s]: %s\n\n", msg.Role, msg.Content))
	}
	return builder.String()
}

// extractUserInputFromReActInput extracts the last user input from saved ReAct input (JSON-formatted messages array)
// func (b *Builder) extractUserInputFromReActInput(reactInputJSON string) string {
// 	// reactInputJSON is a JSON-formatted ChatMessage array that needs parsing
// 	var messages []map[string]interface{}
// 	if err := json.Unmarshal([]byte(reactInputJSON), &messages); err != nil {
// 		b.logger.Warn("failed to parse ReAct input JSON", zap.Error(err))
// 		return ""
// 	}

// 	// Iterate backward to find the last user message
// 	for i := len(messages) - 1; i >= 0; i-- {
// 		if role, ok := messages[i]["role"].(string); ok && strings.EqualFold(role, "user") {
// 			if content, ok := messages[i]["content"].(string); ok {
// 				return content
// 			}
// 		}
// 	}

// 	return ""
// }

// formatAgentTraceInputFromJSON converts JSON trace to readable text (trims to the current task turn first).
func (b *Builder) formatAgentTraceInputFromJSON(reactInputJSON string) string {
	trimmed := agent.ExtractLastUserTurnTraceJSON(reactInputJSON)
	msgs, err := agent.ParseTraceMessages(trimmed)
	if err != nil {
		b.logger.Warn("failed to parse ReAct input JSON", zap.Error(err))
		return trimmed
	}
	return b.formatAgentTraceFromChatMessages(msgs)
}

// formatAgentTraceFromChatMessages formats agent messages as attack chain analysis input (aligned with continue-conversation trace fields).
func (b *Builder) formatAgentTraceFromChatMessages(msgs []agent.ChatMessage) string {
	var builder strings.Builder
	for _, msg := range msgs {
		role := msg.Role
		content := msg.Content

		if strings.EqualFold(role, "assistant") && len(msg.ToolCalls) > 0 {
			if content != "" {
				builder.WriteString(fmt.Sprintf("[%s]: %s\n", role, content))
			}
			builder.WriteString(fmt.Sprintf("[%s] tool calls (%d):\n", role, len(msg.ToolCalls)))
			for i, tc := range msg.ToolCalls {
				args := ""
				if tc.Function.Arguments != nil {
					if b, err := json.Marshal(tc.Function.Arguments); err == nil {
						args = string(b)
					}
				}
				builder.WriteString(fmt.Sprintf("  [tool call %d]\n", i+1))
				builder.WriteString(fmt.Sprintf("    ID: %s\n", tc.ID))
				builder.WriteString(fmt.Sprintf("    tool name: %s\n", tc.Function.Name))
				builder.WriteString(fmt.Sprintf("    arguments: %s\n", args))
			}
			builder.WriteString("\n")
			continue
		}

		if strings.EqualFold(role, "tool") {
			if msg.ToolCallID != "" {
				builder.WriteString(fmt.Sprintf("[%s] (tool_call_id: %s):\n%s\n\n", role, msg.ToolCallID, content))
			} else {
				builder.WriteString(fmt.Sprintf("[%s]: %s\n\n", role, content))
			}
			continue
		}

		builder.WriteString(fmt.Sprintf("[%s]: %s\n\n", role, content))
	}
	return builder.String()
}

// buildSimplePrompt builds a simplified prompt.
func (b *Builder) buildSimplePrompt(reactInput, modelOutput string) string {
	return fmt.Sprintf(`You are a professional security testing analyst and attack-chain construction expert. Your task is to output the attack chain JSON in a single pass based on the **current task turn's** conversation history and tool execution results (do not split into multiple turns of follow-up questions).

## Input scope (consistent with "Continue Conversation" resume)
- The "ReAct Trace" below contains only messages and tool results **after the last user question** (the last_react current task turn), and does not include earlier user turns.
- The "Assistant Conclusion" is the final output summary for the same turn (last_react_output); nodes must match the actual tool executions in the trace. Fabrication is strictly forbidden.

%s
%s

## Core goal

Build an attack chain that tells a complete attack story so learners can:
1. Understand the full flow and reasoning of penetration testing (from target identification to vulnerability discovery at every step)
2. Learn how to extract clues from failures and adjust strategy
3. Master the real effects and limitations of tool usage
4. Understand the causal relationship between vulnerability discovery and exploitation

**Key principle**: completeness first. You must include all meaningful tool executions and key steps; do not omit important information just to control node count.

## Construction process (think in this order)

### Step 1: Understand the context
Carefully analyze the tool call sequence and LLM output in the ReAct input, identifying:
- Test targets (IP, domain, URL, etc.)
- Tools and parameters actually executed
- Key information returned by tools (success results, error messages, timeouts, etc.)
- The AI's analysis and decision-making process

### Step 2: Extract key nodes
Extract meaningful nodes from the tool execution records, **making sure not to miss any key steps**:
- **target node**: create a target node for each independent test target
- **action node**: create an action node for each meaningful tool execution (including failures that provide clues, successful information gathering, vulnerability verification, etc.)
- **vulnerability node**: create a vulnerability node for each truly confirmed vulnerability
- **Completeness check**: cross-check the tool call sequence in the ReAct input, ensuring every meaningful tool execution is included in the attack chain

### Step 3: Build logical relationships (tree structure)
**Important: you must build a tree structure, not a simple linear chain.**
Connect nodes by causal relationships, forming a tree (since this is a single agent execution, you don't have to follow strict time order):
- **Branching structure**: a node can have multiple successors (e.g., after a port scan discovers multiple ports, multiple different tests can run in parallel)
- **Convergent structure**: multiple nodes can point to the same node (e.g., multiple different tests all discovering the same vulnerability)
- Identify which actions are based on the results of prior actions
- Identify which vulnerabilities are discovered by which actions
- Identify how failed nodes provide clues for subsequent successes
- **Avoid linear chains**: do not connect all nodes in a single line; build a tree based on actual parallel testing and branching exploration

### Step 4: Optimize and refine
- **Completeness check**: ensure all meaningful tool executions are included; do not omit key steps
- **Merge rules**: only merge truly similar or duplicate action nodes (e.g., similar calls of the same tool)
- **Delete rules**: only delete completely valueless failure nodes (no output at all, pure system errors, repeated identical failures)
- **Important reminder**: better to keep more nodes than miss key steps. The attack chain must completely represent the penetration testing process
- Ensure the attack chain is logically coherent and can tell a complete story

## Detailed node types

### target (target node)
- **Purpose**: identify the test target
- **Creation rule**: one target node per independent target (different IPs/domains)
- **Multi-target handling**: nodes of different targets do not connect to each other; each forms its own subgraph
- **metadata.target**: precisely record the target identifier (IP address, domain, URL, etc.)

### action (action node)
- **Purpose**: record tool execution and AI analysis results
- **Label rules**:
  * 15-25 Chinese characters (or short English phrase), verb-object structure
  * Success node: describe the execution result (e.g., "port scan discovered 80/443/8080", "directory scan discovered /admin path")
  * Failure node: describe the failure reason (e.g., "attempted SQL injection (blocked by WAF)", "port scan timeout (target unreachable)")
- **ai_analysis requirements**:
  * Success node: summarize the key findings of the tool execution, explain the significance of those findings
  * Failure node: must explain the failure reason, the clues obtained, and how those clues guide subsequent actions
  * No more than 150 words; be specific and informative
- **findings requirements**:
  * Extract key information points from tool return results
  * Each finding should be an independent, valuable information fragment
  * Success node: list key findings (e.g., ["port 80 open", "port 443 open", "HTTP service is Apache 2.4"])
  * Failure node: list failure clues (e.g., ["WAF block", "return 403", "detected Cloudflare"])
- **status tag**:
  * Success node: do not set, or set to "success"
  * Failure node that provides clues: must be set to "failed_insight"
- **risk_score**: always 0 (action nodes do not assess risk)

### vulnerability (vulnerability node)
- **Purpose**: record truly confirmed security vulnerabilities
- **Creation rule**:
  * Must be a truly confirmed vulnerability; not every finding is a vulnerability
  * Requires clear evidence of the vulnerability (e.g., SQL injection returns database error, XSS executes successfully, etc.)
- **risk_score rules**:
  * critical (90-100): can cause complete system compromise (RCE, SQL injection leading to data leak, etc.)
  * high (80-89): can cause sensitive information leak or privilege escalation
  * medium (60-79): has security risk but limited impact
  * low (40-59): minor security issue
- **metadata requirements**:
  * vulnerability_type: vulnerability type (SQL injection, XSS, RCE, etc.)
  * description: detailed description of the vulnerability location, principle, and impact
  * severity: critical/high/medium/low
  * location: precise vulnerability location (URL, parameter, file path, etc.)

## Node filtering and merge rules

### Failure nodes that must be kept
The following failure situations must create nodes, because they provide valuable clues:
- Tools return clear error messages (permission errors, connection refused, auth failure, etc.)
- Timeout or connection failure (may indicate firewall, network isolation, etc.)
- WAF/firewall block (returns 403, 406, etc., indicating the presence of a protection mechanism)
- Tool not installed or configuration error (but the call was executed)
- Target unreachable (DNS resolution failure, network unreachable, etc.)

### Failure nodes that should be deleted
The following situations should not create a node:
- Tool calls with no output at all
- Pure system errors (unrelated to the target, e.g., local environment issues)
- Repeated identical failures (keep only the first for multiple identical errors)

### Node merge rules
The following situations should merge nodes:
- Multiple similar calls of the same tool (e.g., multiple nmap scans on different port ranges, merged into one "port scan" node)
- Multiple similar probes of the same target (e.g., multiple directory-scan tools, merged into one "directory scan" node)

### Node count control
- **Completeness first**: must include all meaningful tool executions and key steps; do not delete important nodes just to control count
- **Recommended range**: usually 8-15 nodes for a single target, but may be increased appropriately if there are many actual execution steps (max 20 nodes)
- **Prefer to keep**: key success steps, failures that provide clues, discovered vulnerabilities, important information-gathering steps
- **Can merge**: multiple similar calls of the same tool (e.g., multiple nmap scans on different port ranges, merged into one "port scan" node)
- **Can delete**: tool calls with no output at all, pure system errors, repeated identical failures (keep only the first for multiple identical errors)
- **Important principle**: better to have slightly more nodes than miss key steps. The attack chain must completely represent the full penetration testing process

## Edge types and weights

### Edge types
- **leads_to**: means "leads to" or "guides to", used for action->action, target->action
  * Example: port scan -> directory scan (because port 80 was discovered, directory scan was performed)
- **discovers**: means "discovers", **specifically used for action->vulnerability**
  * Example: SQL injection test -> SQL injection vulnerability
  * **Important**: all action->vulnerability edges must use the discovers type; even if multiple actions point to the same vulnerability, they should all use discovers
- **enables**: means "enables" or "facilitates", **only used for vulnerability->vulnerability, action->action (when a subsequent action depends on the prior result)**
  * Example: information leak vulnerability -> privilege escalation vulnerability (information obtained through the leak facilitated the privilege escalation)
  * **Important**: enables cannot be used for action->vulnerability; action->vulnerability must use discovers

### Edge weights
- **Weight 1-2**: weak association (e.g., initial probe to further probe)
- **Weight 3-4**: medium association (e.g., port discovery to service identification)
- **Weight 5-7**: strong association (e.g., vulnerability discovery, key information leak)
- **Weight 8-10**: very strong association (e.g., successful exploit, privilege escalation)

### DAG structure requirements (directed acyclic graph)
**Key: you must ensure a true DAG (directed acyclic graph) with no cycles at all.**

- **Node numbering rule**: node ids start at "node_1" and increment (node_1, node_2, node_3...)
- **Edge direction rule**: all edges' source node id must be strictly less than the target node id (source < target); this is the key to ensuring no cycles
  * Example: node_1 -> node_2 (correct)
  * Example: node_2 -> node_1 (incorrect, would form a cycle)
  * Example: node_3 -> node_5 (correct)
- **Cycle check**: before outputting JSON, must check all edges to ensure no edge has source >= target
- **No isolated nodes**: ensure every node has at least one edge connected (except possibly the root node)
Now start the analysis and build the attack chain:`, reactInput, assistantOutSection(modelOutput))
}

func assistantOutSection(modelOutput string) string {
	modelOutput = strings.TrimSpace(modelOutput)
	if modelOutput == "" {
		return ""
	}
	return "\n## Assistant Conclusion (supplementary)\n\n" + modelOutput + "\n"
}

// saveChain saves the attack chain to the database.
func (b *Builder) saveChain(conversationID string, nodes []Node, edges []Edge) error {
	// Delete old attack chain data first
	if err := b.db.DeleteAttackChain(conversationID); err != nil {
		b.logger.Warn("failed to delete old attack chain", zap.Error(err))
	}

	for _, node := range nodes {
		metadataJSON, _ := json.Marshal(node.Metadata)
		if err := b.db.SaveAttackChainNode(conversationID, node.ID, node.Type, node.Label, "", string(metadataJSON), node.RiskScore); err != nil {
		b.logger.Warn("failed to save attack chain node", zap.String("nodeId", node.ID), zap.Error(err))
		}
	}

	// Save edges
	for _, edge := range edges {
		if err := b.db.SaveAttackChainEdge(conversationID, edge.ID, edge.Source, edge.Target, edge.Type, edge.Weight); err != nil {
		b.logger.Warn("failed to save attack chain edge", zap.String("edgeId", edge.ID), zap.Error(err))
		}
	}

	return nil
}

// LoadChainFromDatabase loads an attack chain from the database.
func (b *Builder) LoadChainFromDatabase(conversationID string) (*Chain, error) {
	nodes, err := b.db.LoadAttackChainNodes(conversationID)
	if err != nil {
		return nil, fmt.Errorf("failed to load attack chain nodes: %w", err)
	}

	edges, err := b.db.LoadAttackChainEdges(conversationID)
	if err != nil {
		return nil, fmt.Errorf("failed to load attack chain edges: %w", err)
	}

	return &Chain{
		Nodes: nodes,
		Edges: edges,
	}, nil
}

// callAIForChainGeneration calls AI to generate an attack chain.
func (b *Builder) callAIForChainGeneration(ctx context.Context, prompt string) (string, error) {
	requestBody := map[string]interface{}{
		"model": b.openAIConfig.Model,
		"messages": []map[string]interface{}{
			{
				"role":    "system",
				"content": "You are a professional security testing analyst skilled in building attack chain graphs. Please return attack chain data strictly in JSON format.",
			},
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"temperature":           0.3,
		"max_completion_tokens": attackChainMaxCompletionTokens(b.maxTokens),
	}

	var apiResponse struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if b.openAIClient == nil {
		return "", fmt.Errorf("OpenAI client not initialized")
	}
	if err := b.openAIClient.ChatCompletion(ctx, requestBody, &apiResponse); err != nil {
		var apiErr *openai.APIError
		if errors.As(err, &apiErr) {
			bodyStr := strings.ToLower(apiErr.Body)
			if strings.Contains(bodyStr, "context") || strings.Contains(bodyStr, "length") || strings.Contains(bodyStr, "too long") {
				return "", fmt.Errorf("context length exceeded")
			}
		} else if strings.Contains(strings.ToLower(err.Error()), "context") || strings.Contains(strings.ToLower(err.Error()), "length") {
			return "", fmt.Errorf("context length exceeded")
		}
		return "", fmt.Errorf("request failed: %w", err)
	}

	if len(apiResponse.Choices) == 0 {
		return "", fmt.Errorf("API returned no valid response")
	}

	content := strings.TrimSpace(apiResponse.Choices[0].Message.Content)
	// Try to extract JSON (may be wrapped in markdown code blocks)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	return content, nil
}

// ChainJSON is the attack chain JSON structure.
type ChainJSON struct {
	Nodes []struct {
		ID        string                 `json:"id"`
		Type      string                 `json:"type"`
		Label     string                 `json:"label"`
		RiskScore int                    `json:"risk_score"`
		Metadata  map[string]interface{} `json:"metadata"`
	} `json:"nodes"`
	Edges []struct {
		Source string `json:"source"`
		Target string `json:"target"`
		Type   string `json:"type"`
		Weight int    `json:"weight"`
	} `json:"edges"`
}

// parseChainJSON parses attack chain JSON.
func (b *Builder) parseChainJSON(chainJSON string) (*Chain, error) {
	var chainData ChainJSON
	if err := json.Unmarshal([]byte(chainJSON), &chainData); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Create node ID mapping (AI-returned ID -> new UUID)
	nodeIDMap := make(map[string]string)

	// Convert to Chain structure
	nodes := make([]Node, 0, len(chainData.Nodes))
	for _, n := range chainData.Nodes {
		// Generate new UUID node ID
		newNodeID := fmt.Sprintf("node_%s", uuid.New().String())
		nodeIDMap[n.ID] = newNodeID

		node := Node{
			ID:        newNodeID,
			Type:      n.Type,
			Label:     n.Label,
			RiskScore: n.RiskScore,
			Metadata:  n.Metadata,
		}
		if node.Metadata == nil {
			node.Metadata = make(map[string]interface{})
		}
		nodes = append(nodes, node)
	}

	// Convert edges
	edges := make([]Edge, 0, len(chainData.Edges))
	for _, e := range chainData.Edges {
		sourceID, ok := nodeIDMap[e.Source]
		if !ok {
			continue
		}
		targetID, ok := nodeIDMap[e.Target]
		if !ok {
			continue
		}

		// Generate edge ID (required by frontend)
		edgeID := fmt.Sprintf("edge_%s", uuid.New().String())

		edges = append(edges, Edge{
			ID:     edgeID,
			Source: sourceID,
			Target: targetID,
			Type:   e.Type,
			Weight: e.Weight,
		})
	}

	return &Chain{
		Nodes: nodes,
		Edges: edges,
	}, nil
}

// All methods below are no longer used and have been removed to simplify the code.
