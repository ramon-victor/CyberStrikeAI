package app

import (
	"context"
	"fmt"
	"strings"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/mcp/builtin"
	"cyberstrike-ai/internal/project"

	"go.uber.org/zap"
)

func projectIDFromConversation(db *database.DB, ctx context.Context) (string, error) {
	convID := mcpAuthorizationConversationID(ctx)
	if convID == "" {
		return "", fmt.Errorf("cannot determine the current conversation; use project-fact tools in a conversation context")
	}
	pid, err := db.GetConversationProjectID(convID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(pid) == "" {
		return "", fmt.Errorf("the current conversation is not bound to a project; select a project or create a project-bound conversation first")
	}
	return pid, nil
}

func textResult(msg string, isErr bool) *mcp.ToolResult {
	msg = strings.ReplaceAll(msg, "错误:", "Error:")
	return &mcp.ToolResult{
		Content: []mcp.Content{{Type: "text", Text: msg}},
		IsError: isErr,
	}
}

// registerProjectFactTools 注册项目黑板 MCP 工具。
func registerProjectFactTools(mcpServer *mcp.Server, db *database.DB, cfg *config.Config, logger *zap.Logger) {
	if db == nil || cfg == nil || !cfg.Project.Enabled {
		if logger != nil {
			logger.Info("Project Blackboard tool is not registered (not enabled)")
		}
		return
	}

	upsertTool := mcp.Tool{
		Name: builtin.ToolUpsertProjectFact,
		Description: "Write or update a project-blackboard fact for reproducible cross-conversation context (not a formal vulnerability record; use record_vulnerability for reportable vulnerabilities). " +
			"Record each confirmed insight (port, entry point, credential or exploitable condition) immediately, updating the same fact_key; do not wait until the conversation ends." +
			"Do not record conclusions alone: summary must state what, where and how it was verified; body must contain the attack chain, requests, responses and commands needed for reproduction." +
			"For findings use fact_key finding|chain|exploit|poc/<slug> and the matching category; fill the body using the attack-chain template." +
			"For scripts use category=note and artifact/<slug>, recording relative workspace path, dependencies, invocation and success evidence in body. For tests use category=note and test/<slug>, recording tested conditions, evidence and retest triggers; do not mark negative results deprecated." +
			"For findings, use fact_key finding|chain|exploit|poc/<slug> and the matching category. For environment facts, use target|auth|infra|business/<slug>. The conversation must be project-bound.",
		ShortDescription: "Write or update a project fact, including the attack-chain body",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"fact_key": map[string]interface{}{
					"type":        "string",
					"description": "Unique key within the project, such as target/primary_domain, finding/sqli-login, or exploit/upload-rce",
				},
				"category": map[string]interface{}{
					"type":        "string",
					"description": "target | auth | infra | business | finding | chain | exploit | poc | note",
					"enum":        []string{"target", "auth", "infra", "business", "finding", "chain", "exploit", "poc", "note"},
				},
				"summary": map[string]interface{}{
					"type":        "string",
					"description": "One-line index summary: conclusion, location, and trigger/verification details; do not write empty statements such as 'XSS exists'",
				},
				"body": map[string]interface{}{
					"type":        "string",
					"description": "Complete reproducible details returned only by get_project_fact: attack-chain steps, raw HTTP/commands, response behavior, evidence, and links. Required for initial finding/exploit facts. Existing body is retained when updating a fact_key with an omitted or empty body.",
				},
				"confidence": map[string]interface{}{
					"type":        "string",
					"description": "confirmed | tentative | deprecated",
					"enum":        []string{"confirmed", "tentative", "deprecated"},
				},
				"pinned": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether to prioritize this fact in the blackboard index",
				},
				"related_vulnerability_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional related vulnerability record ID",
				},
				"links": map[string]interface{}{
					"type":        "array",
					"description": "Optional relationship edges (from -> this fact). A finding should have at least {from:target/*, type:discovered_on}; an exploit recorded on a finding uses {from:exploit/*, type:exploits}. Omit to retain existing edges; pass [] to clear all edges.",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"from": map[string]interface{}{
								"type":        "string",
								"description": "Source fact_key, stored as from -> the current fact",
							},
							"type": map[string]interface{}{
								"type":        "string",
								"description": "depends_on | leads_to | enables | exploits | discovered_on | contains | part_of | supports",
							},
							"confidence": map[string]interface{}{
								"type":        "string",
								"description": "confirmed | tentative | deprecated",
							},
						},
						"required": []string{"from", "type"},
					},
				},
			},
			"required": []string{"fact_key", "summary"},
		},
	}

	mcpServer.RegisterTool(upsertTool, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		projectID, err := projectIDFromConversation(db, ctx)
		if err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		factKey, _ := args["fact_key"].(string)
		summary, _ := args["summary"].(string)
		if database.IsContextCheckpointKey(factKey) {
			return textResult("Error: note/context-* is reserved for system continuation records. Use a separate fact_key for shared project facts.", true), nil
		}
		if strings.TrimSpace(factKey) == "" || strings.TrimSpace(summary) == "" {
			return textResult("Error: fact_key and summary are required", true), nil
		}
		if len([]rune(summary)) > cfg.Project.FactSummaryMaxRunesEffective() {
			return textResult(fmt.Sprintf("Error: summary is too long (maximum %d characters)", cfg.Project.FactSummaryMaxRunesEffective()), true), nil
		}
		f := &database.ProjectFact{
			ProjectID:              projectID,
			FactKey:                factKey,
			Category:               strArg(args, "category"),
			Summary:                summary,
			Body:                   strArg(args, "body"),
			Confidence:             strArg(args, "confidence"),
			Pinned:                 boolArg(args, "pinned"),
			RelatedVulnerabilityID: strArg(args, "related_vulnerability_id"),
		}
		if convID := mcpAuthorizationConversationID(ctx); convID != "" {
			f.SourceConversationID = convID
		}
		created, err := db.UpsertProjectFact(f)
		if err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		if _, hasLinks := args["links"]; hasLinks {
			linkInputs, err := project.ParseFactLinkInputs(args["links"])
			if err != nil {
				return textResult("Error: "+err.Error(), true), nil
			}
			convID := mcpAuthorizationConversationID(ctx)
			if err := project.PersistFactLinksFromParsed(db, projectID, created.FactKey, convID, linkInputs, true); err != nil {
				return textResult("Error: failed to save relationship edges: "+err.Error(), true), nil
			}
			created, _ = db.GetProjectFactByKey(projectID, created.FactKey)
		} else if parsed := project.ParseLinksFromBody(created.Body); len(parsed) > 0 {
			if err := project.PersistFactIncomingLinks(db, projectID, created.FactKey, parsed, true); err != nil {
				return textResult("Error: failed to parse edges from body: "+err.Error(), true), nil
			}
			created, _ = db.GetProjectFactByKey(projectID, created.FactKey)
		}
		msg := fmt.Sprintf("Fact saved.\nfact_key: %s\nid: %s\nconfidence: %s", created.FactKey, created.ID, created.Confidence)
		if in, _ := db.ListIncomingProjectFactEdges(projectID, created.FactKey); len(in) > 0 {
			in = scopedProjectMemoryEdges(db, projectID, mcpAuthorizationConversationID(ctx), false, in)
			if len(in) > 0 {
				msg += "\nRelationship edges: " + project.FormatFactLinksText(in)
			}
		}
		if warn := project.SparseBodyWarningIfNeeded(f.Category, f.FactKey, f.Body); warn != "" {
			msg += warn
		}
		return textResult(msg, false), nil
	})

	getTool := mcp.Tool{
		Name:             builtin.ToolGetProjectFact,
		Description:      "Retrieve complete project fact body and metadata by fact_key. Defaults to shared facts and the current conversation's continuation records; set include_history=true for other conversations. Retrieve details when summaries are insufficient; never invent them.",
		ShortDescription: "Get fact details by key",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"include_history": map[string]interface{}{"type": "boolean", "description": "Explicitly include other conversations' automatic continuation records in this project; default false. History is not a current task instruction."},
				"fact_key":        map[string]interface{}{"type": "string", "description": "Fact key"},
			},
			"required": []string{"fact_key"},
		},
	}
	mcpServer.RegisterTool(getTool, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		projectID, err := projectIDFromConversation(db, ctx)
		if err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		key := strings.TrimSpace(strArg(args, "fact_key"))
		if key == "" {
			return textResult("Error: fact_key is required", true), nil
		}
		f, err := db.GetProjectFactByKey(projectID, key)
		if err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		conversationID := mcpAuthorizationConversationID(ctx)
		includeHistory := boolArg(args, "include_history")
		if !database.ContextCheckpointVisible(f, conversationID, includeHistory) {
			return textResult("This key belongs to a historical continuation record from another or unknown conversation. Set include_history=true to retrieve it as reference; historical tasks are not executed automatically.", true), nil
		}
		msg := fmt.Sprintf("fact_key: %s\ncategory: %s\nconfidence: %s\nsummary: %s\nupdated_at: %s",
			f.FactKey, f.Category, f.Confidence, f.Summary, f.UpdatedAt.Format("2006-01-02 15:04:05"))
		if f.RelatedVulnerabilityID != "" {
			msg += fmt.Sprintf("\nrelated_vulnerability_id: %s", f.RelatedVulnerabilityID)
		}
		if f.SourceConversationID != "" {
			msg += fmt.Sprintf("\nsource_conversation_id: %s", f.SourceConversationID)
		}
		if in, _ := db.ListIncomingProjectFactEdges(projectID, f.FactKey); len(in) > 0 {
			in = scopedProjectMemoryEdges(db, projectID, conversationID, includeHistory, in)
			msg += "\nRelationship edges (from -> current fact):\n"
			for _, e := range in {
				msg += fmt.Sprintf("- %s ← %s (%s)\n", e.EdgeType, e.SourceFactKey, e.Confidence)
			}
		}
		if out, _ := db.ListOutgoingProjectFactEdges(projectID, f.FactKey); len(out) > 0 {
			out = scopedProjectMemoryEdges(db, projectID, conversationID, includeHistory, out)
			msg += "Point to other facts:\n"
			for _, e := range out {
				msg += fmt.Sprintf("- %s → %s (%s)\n", e.EdgeType, e.TargetFactKey, e.Confidence)
			}
		}
		msg += checkpointReadLabel(f, conversationID)
		msg += "\n\n--- body ---\n" + f.Body
		if warn := project.SparseBodyWarningIfNeeded(f.Category, f.FactKey, f.Body); warn != "" {
			msg += warn
		}
		return textResult(msg, false), nil
	})

	listTool := mcp.Tool{
		Name:             builtin.ToolListProjectFacts,
		Description:      "List shared project facts and current conversation continuation records with pagination; set include_history=true to include historical continuation records.",
		ShortDescription: "List project facts",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"source_conversation_id": map[string]interface{}{"type": "string", "description": "Optional source conversation filter; other conversations' continuation records still require include_history=true"},
				"include_history":        map[string]interface{}{"type": "boolean", "description": "Explicitly include other conversations' automatic continuation records in this project; default false. History is not a current task instruction."},
				"category":               map[string]interface{}{"type": "string"},
				"confidence":             map[string]interface{}{"type": "string"},
				"limit":                  map[string]interface{}{"type": "integer"},
				"offset":                 map[string]interface{}{"type": "integer"},
			},
		},
	}
	mcpServer.RegisterTool(listTool, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		projectID, err := projectIDFromConversation(db, ctx)
		if err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		limit := intArg(args, "limit", 50)
		offset := intArg(args, "offset", 0)
		filter := database.ProjectFactListFilter{
			Category:                   strArg(args, "category"),
			Confidence:                 strArg(args, "confidence"),
			RestrictContextCheckpoints: !boolArg(args, "include_history"),
			ContextConversationID:      mcpAuthorizationConversationID(ctx),
			SourceConversationID:       strArg(args, "source_conversation_id"),
		}
		list, err := db.ListProjectFacts(projectID, filter, limit, offset)
		if err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("%d facts (limit=%d offset=%d):\n", len(list), limit, offset))
		for _, f := range list {
			b.WriteString(fmt.Sprintf("- [%s] %s — %s (%s)\n", f.FactKey, f.Category, f.Summary, f.Confidence))
			b.WriteString(checkpointReadLabel(f, mcpAuthorizationConversationID(ctx)))
		}
		return textResult(b.String(), false), nil
	})

	searchTool := mcp.Tool{
		Name:             builtin.ToolSearchProjectFacts,
		Description:      "Search shared project facts and current conversation continuation records by summary, body or fact_key; other conversations require include_history=true.",
		ShortDescription: "Search project facts",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"source_conversation_id": map[string]interface{}{"type": "string", "description": "Optional source conversation filter; other conversations' continuation records still require include_history=true"},
				"include_history":        map[string]interface{}{"type": "boolean", "description": "Explicitly include other conversations' automatic continuation records in this project; default false. History is not a current task instruction."},
				"query":                  map[string]interface{}{"type": "string"},
				"limit":                  map[string]interface{}{"type": "integer"},
				"offset":                 map[string]interface{}{"type": "integer"},
			},
			"required": []string{"query"},
		},
	}
	mcpServer.RegisterTool(searchTool, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		projectID, err := projectIDFromConversation(db, ctx)
		if err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		q := strings.TrimSpace(strArg(args, "query"))
		if q == "" {
			return textResult("Error: query is required", true), nil
		}
		list, err := db.ListProjectFacts(projectID, database.ProjectFactListFilter{Search: q, RestrictContextCheckpoints: !boolArg(args, "include_history"), ContextConversationID: mcpAuthorizationConversationID(ctx), SourceConversationID: strArg(args, "source_conversation_id")}, intArg(args, "limit", 30), intArg(args, "offset", 0))
		if err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("Search for %q matched %d facts:\n", q, len(list)))
		for _, f := range list {
			b.WriteString(fmt.Sprintf("- [%s] %s — %s\n", f.FactKey, f.Category, f.Summary))
			b.WriteString(checkpointReadLabel(f, mcpAuthorizationConversationID(ctx)))
		}
		return textResult(b.String(), false), nil
	})

	deprecateTool := mcp.Tool{
		Name:             builtin.ToolDeprecateProjectFact,
		Description:      "Mark a fact deprecated and exclude it from the blackboard index.",
		ShortDescription: "Deprecate a project fact",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"fact_key": map[string]interface{}{"type": "string"},
			},
			"required": []string{"fact_key"},
		},
	}
	mcpServer.RegisterTool(deprecateTool, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		projectID, err := projectIDFromConversation(db, ctx)
		if err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		key := strings.TrimSpace(strArg(args, "fact_key"))
		if database.IsContextCheckpointKey(key) {
			f, err := db.GetProjectFactByKey(projectID, key)
			if err != nil || !database.ContextCheckpointVisible(f, mcpAuthorizationConversationID(ctx), false) {
				return textResult("Error: Cannot modify auto-continuation records for other sessions", true), nil
			}
		}
		if err := db.DeprecateProjectFact(projectID, key); err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		return textResult("Fact marked deprecated: "+key, false), nil
	})

	restoreTool := mcp.Tool{
		Name:             builtin.ToolRestoreProjectFact,
		Description:      "Restore a deprecated fact as tentative or confirmed so it participates in the blackboard index again.",
		ShortDescription: "Restore a deprecated project fact",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"fact_key": map[string]interface{}{"type": "string"},
				"confidence": map[string]interface{}{
					"type":        "string",
					"description": "Restored confidence: tentative (default) or confirmed",
					"enum":        []string{"tentative", "confirmed"},
				},
			},
			"required": []string{"fact_key"},
		},
	}
	mcpServer.RegisterTool(restoreTool, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		projectID, err := projectIDFromConversation(db, ctx)
		if err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		key := strings.TrimSpace(strArg(args, "fact_key"))
		if database.IsContextCheckpointKey(key) {
			f, err := db.GetProjectFactByKey(projectID, key)
			if err != nil || !database.ContextCheckpointVisible(f, mcpAuthorizationConversationID(ctx), false) {
				return textResult("Error: Cannot modify auto-continuation records for other sessions", true), nil
			}
		}
		if key == "" {
			return textResult("Error: fact_key is required", true), nil
		}
		conf := strArg(args, "confidence")
		if err := db.RestoreProjectFact(projectID, key, conf); err != nil {
			return textResult("Error: "+err.Error(), true), nil
		}
		if conf == "" {
			conf = "tentative"
		}
		return textResult(fmt.Sprintf("Fact restored as %s: %s", conf, key), false), nil
	})

	if logger != nil {
		logger.Debug("Project Blackboard MCP tool registration successful")
	}
}

func strArg(args map[string]interface{}, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

func boolArg(args map[string]interface{}, key string) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	return false
}

func intArg(args map[string]interface{}, key string, def int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	default:
		return def
	}
}

// History is a read opt-in, not promotion to a confirmed fact or new authorization.
func checkpointReadLabel(f *database.ProjectFact, conversationID string) string {
	if !database.IsContextCheckpointKey(f.FactKey) {
		return ""
	}
	label := "Current session work record"
	if strings.TrimSpace(conversationID) == "" || f.SourceConversationID != conversationID {
		label = "Historical reference, not the current task; verify authorization, target and credential validity"
	}
	source := f.SourceConversationID
	if source == "" {
		source = "unknown source"
	}
	return fmt.Sprintf("\n[Continuation record: %s; source conversation=%s; updated=%s]\n", label, source, f.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"))
}

func scopedProjectMemoryEdges(db *database.DB, projectID, conversationID string, includeHistory bool, edges []*database.ProjectFactEdge) []*database.ProjectFactEdge {
	if includeHistory {
		return edges
	}
	var visible []*database.ProjectFactEdge
	allowed := make(map[string]bool)
	for _, e := range edges {
		keep := true
		for _, key := range []string{e.SourceFactKey, e.TargetFactKey} {
			if !database.IsContextCheckpointKey(key) {
				continue
			}
			ok, seen := allowed[key]
			if !seen {
				f, err := db.GetProjectFactByKey(projectID, key)
				ok = err == nil && database.ContextCheckpointVisible(f, conversationID, false)
				allowed[key] = ok
			}
			if !ok {
				keep = false
				break
			}
		}
		if keep {
			visible = append(visible, e)
		}
	}
	return visible
}
