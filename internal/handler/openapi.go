package handler

import (
	"net/http"
	"time"

	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/storage"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// OpenAPIHandler OpenAPI handler
type OpenAPIHandler struct {
	db               *database.DB
	logger           *zap.Logger
	resultStorage    storage.ResultStorage
	conversationHdlr *ConversationHandler
	agentHdlr        *AgentHandler
}

// NewOpenAPIHandler creates a new OpenAPI handler
func NewOpenAPIHandler(db *database.DB, logger *zap.Logger, resultStorage storage.ResultStorage, conversationHdlr *ConversationHandler, agentHdlr *AgentHandler) *OpenAPIHandler {
	return &OpenAPIHandler{
		db:               db,
		logger:           logger,
		resultStorage:    resultStorage,
		conversationHdlr: conversationHdlr,
		agentHdlr:        agentHdlr,
	}
}

// GetOpenAPISpec gets the OpenAPI specification
func (h *OpenAPIHandler) GetOpenAPISpec(c *gin.Context) {
	host := c.Request.Host
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}

	spec := map[string]interface{}{
		"openapi": "3.0.0",
		"info": map[string]interface{}{
			"title":       "CyberStrikeAI API",
			"description": "AI-driven automated security testing platform API documentation",
			"version":     "1.0.0",
			"contact": map[string]interface{}{
				"name": "CyberStrikeAI",
			},
		},
		"servers": []map[string]interface{}{
			{
				"url":         scheme + "://" + host,
				"description": "Current server",
			},
		},
		"components": map[string]interface{}{
			"securitySchemes": map[string]interface{}{
				"bearerAuth": map[string]interface{}{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "JWT",
					"description":  "Authenticate with a Bearer token. Obtain the token from /api/auth/login.",
				},
			},
			"schemas": map[string]interface{}{
				"CreateConversationRequest": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"title": map[string]interface{}{
							"type":        "string",
							"description": "Conversation title",
							"example":     "Web application security test",
						},
						"projectId": map[string]interface{}{
							"type":        "string",
							"description": "Bound project ID, optional; shared fact blackboard",
						},
					},
				},
				"SetConversationProjectRequest": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"projectId": map[string]interface{}{
							"type":        "string",
							"description": "Project ID；empty string means unbind",
						},
					},
					"required": []string{"projectId"},
				},
				"Conversation": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "Conversation ID",
							"example":     "550e8400-e29b-41d4-a716-446655440000",
						},
						"title": map[string]interface{}{
							"type":        "string",
							"description": "Conversation title",
							"example":     "Web application security test",
						},
						"createdAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Creation time",
						},
						"updatedAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Update time",
						},
						"projectId": map[string]interface{}{
							"type":        "string",
							"description": "Bound project ID, optional",
						},
					},
				},
				"ConversationDetail": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "Conversation ID",
						},
						"title": map[string]interface{}{
							"type":        "string",
							"description": "Conversation title",
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "Conversation status: active (in progress), completed, failed",
							"enum":        []string{"active", "completed", "failed"},
						},
						"createdAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Creation time",
						},
						"updatedAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Update time",
						},
						"messages": map[string]interface{}{
							"type":        "array",
							"description": "Message list",
							"items": map[string]interface{}{
								"$ref": "#/components/schemas/Message",
							},
						},
						"messageCount": map[string]interface{}{
							"type":        "integer",
							"description": "Message count",
						},
					},
				},
				"Message": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "Message ID",
						},
						"conversationId": map[string]interface{}{
							"type":        "string",
							"description": "Conversation ID",
						},
						"role": map[string]interface{}{
							"type":        "string",
							"description": "Message role: user or assistant",
							"enum":        []string{"user", "assistant"},
						},
						"content": map[string]interface{}{
							"type":        "string",
							"description": "Message content",
						},
						"createdAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Creation time",
						},
					},
				},
				"ConversationResults": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"conversationId": map[string]interface{}{
							"type":        "string",
							"description": "Conversation ID",
						},
						"messages": map[string]interface{}{
							"type":        "array",
							"description": "Message list",
							"items": map[string]interface{}{
								"$ref": "#/components/schemas/Message",
							},
						},
						"vulnerabilities": map[string]interface{}{
							"type":        "array",
							"description": "Discovered vulnerability list",
							"items": map[string]interface{}{
								"$ref": "#/components/schemas/Vulnerability",
							},
						},
						"executionResults": map[string]interface{}{
							"type":        "array",
							"description": "Execution result list",
							"items": map[string]interface{}{
								"$ref": "#/components/schemas/ExecutionResult",
							},
						},
					},
				},
				"Vulnerability": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "Vulnerability ID",
						},
						"title": map[string]interface{}{
							"type":        "string",
							"description": "Vulnerability title",
						},
						"description": map[string]interface{}{
							"type":        "string",
							"description": "Vulnerability description",
						},
						"severity": map[string]interface{}{
							"type":        "string",
							"description": "Severity",
							"enum":        []string{"critical", "high", "medium", "low", "info"},
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "Status",
							"enum":        []string{"open", "closed", "fixed"},
						},
						"target": map[string]interface{}{
							"type":        "string",
							"description": "Affected target",
						},
					},
				},
				"ExecutionResult": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "Execution ID",
						},
						"toolName": map[string]interface{}{
							"type":        "string",
							"description": "Tool name",
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "Execution status",
							"enum":        []string{"success", "failed", "running"},
						},
						"result": map[string]interface{}{
							"type":        "string",
							"description": "Execution result",
						},
						"createdAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Creation time",
						},
					},
				},
				"Error": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"error": map[string]interface{}{
							"type":        "string",
							"description": "Error message",
						},
					},
				},
				"LoginRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"password"},
					"properties": map[string]interface{}{
						"password": map[string]interface{}{
							"type":        "string",
							"description": "Login password",
						},
					},
				},
				"LoginResponse": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"token": map[string]interface{}{
							"type":        "string",
							"description": "Authentication token",
						},
						"expires_at": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Token expiration time",
						},
						"session_duration_hr": map[string]interface{}{
							"type":        "integer",
							"description": "Session duration in hours",
						},
					},
				},
				"ChangePasswordRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"oldPassword", "newPassword"},
					"properties": map[string]interface{}{
						"oldPassword": map[string]interface{}{
							"type":        "string",
							"description": "Current password",
						},
						"newPassword": map[string]interface{}{
							"type":        "string",
							"description": "New password, at least 8 characters",
						},
					},
				},
				"UpdateConversationRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"title"},
					"properties": map[string]interface{}{
						"title": map[string]interface{}{
							"type":        "string",
							"description": "Conversation title",
						},
					},
				},
				"Group": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "Group ID",
						},
						"name": map[string]interface{}{
							"type":        "string",
							"description": "Group name",
						},
						"icon": map[string]interface{}{
							"type":        "string",
							"description": "Group icon",
						},
						"createdAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Creation time",
						},
						"updatedAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Update time",
						},
					},
				},
				"CreateGroupRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"name"},
					"properties": map[string]interface{}{
						"name": map[string]interface{}{
							"type":        "string",
							"description": "Group name",
						},
						"icon": map[string]interface{}{
							"type":        "string",
							"description": "Group icon, optional",
						},
					},
				},
				"UpdateGroupRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"name"},
					"properties": map[string]interface{}{
						"name": map[string]interface{}{
							"type":        "string",
							"description": "Group name",
						},
						"icon": map[string]interface{}{
							"type":        "string",
							"description": "Group icon",
						},
					},
				},
				"AddConversationToGroupRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"conversationId", "groupId"},
					"properties": map[string]interface{}{
						"conversationId": map[string]interface{}{
							"type":        "string",
							"description": "Conversation ID",
						},
						"groupId": map[string]interface{}{
							"type":        "string",
							"description": "Group ID",
						},
					},
				},
				"BatchTaskRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"tasks"},
					"properties": map[string]interface{}{
						"title": map[string]interface{}{
							"type":        "string",
							"description": "Task title, optional",
						},
						"tasks": map[string]interface{}{
							"type":        "array",
							"description": "Task list, one task per line",
							"items": map[string]interface{}{
								"type": "string",
							},
						},
						"role": map[string]interface{}{
							"type":        "string",
							"description": "Role name, optional",
						},
						"agentMode": map[string]interface{}{
							"type":        "string",
							"description": "Agent mode: eino_single (Eino ADK single agent, default), deep, plan_execute, or supervisor",
							"enum":        []string{"eino_single", "deep", "plan_execute", "supervisor"},
						},
						"scheduleMode": map[string]interface{}{
							"type":        "string",
							"description": "Schedule mode (manual | cron)",
							"enum":        []string{"manual", "cron"},
						},
						"cronExpr": map[string]interface{}{
							"type":        "string",
							"description": "Cron expression, required when scheduleMode=cron",
						},
						"executeNow": map[string]interface{}{
							"type":        "boolean",
							"description": "Whether to execute immediately after creation; default false",
						},
					},
				},
				"BatchQueue": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "Queue ID",
						},
						"title": map[string]interface{}{
							"type":        "string",
							"description": "Queue title",
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "Queue status",
							"enum":        []string{"pending", "running", "paused", "completed", "failed"},
						},
						"tasks": map[string]interface{}{
							"type":        "array",
							"description": "Task list",
							"items": map[string]interface{}{
								"type": "object",
							},
						},
						"createdAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Creation time",
						},
					},
				},
				"CancelAgentLoopRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"conversationId"},
					"properties": map[string]interface{}{
						"conversationId": map[string]interface{}{
							"type":        "string",
							"description": "Conversation ID",
						},
						"reason": map[string]interface{}{
							"type":        "string",
							"description": "Optional. Matches Terminate and explain on the MCP monitor page: when non-empty, merges into the text returned from the current tool to the model, including a USER INTERRUPT NOTE block.",
						},
						"continueAfter": map[string]interface{}{
							"type":        "boolean",
							"description": "When true, terminate only the current MCP tool call without cancelling the whole turn; requires a running tool, otherwise returns 400.",
						},
					},
				},
				"AgentTask": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"conversationId": map[string]interface{}{
							"type":        "string",
							"description": "Conversation ID",
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "Task status",
							"enum":        []string{"running", "completed", "failed", "cancelled", "timeout"},
						},
						"startedAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Start time",
						},
					},
				},
				"CreateVulnerabilityRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"conversation_id", "title", "severity"},
					"properties": map[string]interface{}{
						"conversation_id": map[string]interface{}{
							"type":        "string",
							"description": "Conversation ID",
						},
						"title": map[string]interface{}{
							"type":        "string",
							"description": "Vulnerability title",
						},
						"description": map[string]interface{}{
							"type":        "string",
							"description": "Vulnerability description",
						},
						"severity": map[string]interface{}{
							"type":        "string",
							"description": "Severity",
							"enum":        []string{"critical", "high", "medium", "low", "info"},
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "Status",
							"enum":        []string{"open", "closed", "fixed"},
						},
						"type": map[string]interface{}{
							"type":        "string",
							"description": "Vulnerability type",
						},
						"target": map[string]interface{}{
							"type":        "string",
							"description": "Affected target",
						},
						"proof": map[string]interface{}{
							"type":        "string",
							"description": "Proof of vulnerability",
						},
						"impact": map[string]interface{}{
							"type":        "string",
							"description": "Impact",
						},
						"recommendation": map[string]interface{}{
							"type":        "string",
							"description": "Remediation advice",
						},
					},
				},
				"UpdateVulnerabilityRequest": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"title": map[string]interface{}{
							"type":        "string",
							"description": "Vulnerability title",
						},
						"description": map[string]interface{}{
							"type":        "string",
							"description": "Vulnerability description",
						},
						"severity": map[string]interface{}{
							"type":        "string",
							"description": "Severity",
							"enum":        []string{"critical", "high", "medium", "low", "info"},
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "Status",
							"enum":        []string{"open", "closed", "fixed"},
						},
						"type": map[string]interface{}{
							"type":        "string",
							"description": "Vulnerability type",
						},
						"target": map[string]interface{}{
							"type":        "string",
							"description": "Affected target",
						},
						"proof": map[string]interface{}{
							"type":        "string",
							"description": "Proof of vulnerability",
						},
						"impact": map[string]interface{}{
							"type":        "string",
							"description": "Impact",
						},
						"recommendation": map[string]interface{}{
							"type":        "string",
							"description": "Remediation advice",
						},
					},
				},
				"ListVulnerabilitiesResponse": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"vulnerabilities": map[string]interface{}{
							"type":        "array",
							"description": "Vulnerability list",
							"items": map[string]interface{}{
								"$ref": "#/components/schemas/Vulnerability",
							},
						},
						"total": map[string]interface{}{
							"type":        "integer",
							"description": "Total count",
						},
						"page": map[string]interface{}{
							"type":        "integer",
							"description": "Current page",
						},
						"page_size": map[string]interface{}{
							"type":        "integer",
							"description": "Items per page",
						},
						"total_pages": map[string]interface{}{
							"type":        "integer",
							"description": "Total pages",
						},
					},
				},
				"VulnerabilityStats": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"total": map[string]interface{}{
							"type":        "integer",
							"description": "Total vulnerabilities",
						},
						"by_severity": map[string]interface{}{
							"type":        "object",
							"description": "Counts by severity",
						},
						"by_status": map[string]interface{}{
							"type":        "object",
							"description": "Counts by status",
						},
					},
				},
				"RoleConfig": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name": map[string]interface{}{
							"type":        "string",
							"description": "Role name",
						},
						"description": map[string]interface{}{
							"type":        "string",
							"description": "Role description",
						},
						"enabled": map[string]interface{}{
							"type":        "boolean",
							"description": "Enabled",
						},
						"systemPrompt": map[string]interface{}{
							"type":        "string",
							"description": "System prompt",
						},
						"userPrompt": map[string]interface{}{
							"type":        "string",
							"description": "User prompt",
						},
						"tools": map[string]interface{}{
							"type":        "array",
							"description": "Tool list",
							"items": map[string]interface{}{
								"type": "string",
							},
						},
					},
				},
				"Skill": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name": map[string]interface{}{
							"type":        "string",
							"description": "Skill name",
						},
						"description": map[string]interface{}{
							"type":        "string",
							"description": "Skill description",
						},
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Skill path",
						},
					},
				},
				"CreateSkillRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"name", "description"},
					"properties": map[string]interface{}{
						"name": map[string]interface{}{
							"type":        "string",
							"description": "Skill name",
						},
						"description": map[string]interface{}{
							"type":        "string",
							"description": "Skill description",
						},
					},
				},
				"UpdateSkillRequest": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"description": map[string]interface{}{
							"type":        "string",
							"description": "Skill description",
						},
					},
				},
				"ToolExecution": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "Execution ID",
						},
						"toolName": map[string]interface{}{
							"type":        "string",
							"description": "Tool name",
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "Execution status",
							"enum":        []string{"success", "failed", "running"},
						},
						"createdAt": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Creation time",
						},
					},
				},
				"MonitorResponse": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"executions": map[string]interface{}{
							"type":        "array",
							"description": "Execution record list",
							"items": map[string]interface{}{
								"$ref": "#/components/schemas/ToolExecution",
							},
						},
						"stats": map[string]interface{}{
							"type":        "object",
							"description": "Statistics",
						},
						"timestamp": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Timestamp",
						},
						"total": map[string]interface{}{
							"type":        "integer",
							"description": "Total count",
						},
						"page": map[string]interface{}{
							"type":        "integer",
							"description": "Current page",
						},
						"page_size": map[string]interface{}{
							"type":        "integer",
							"description": "Items per page",
						},
						"total_pages": map[string]interface{}{
							"type":        "integer",
							"description": "Total pages",
						},
					},
				},
				"ConfigResponse": map[string]interface{}{
					"type":        "object",
					"description": "Configuration information",
				},
				"UpdateConfigRequest": map[string]interface{}{
					"type":        "object",
					"description": "Update configuration request",
				},
				"ExternalMCPConfig": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"enabled": map[string]interface{}{
							"type":        "boolean",
							"description": "Enabled",
						},
						"command": map[string]interface{}{
							"type":        "string",
							"description": "Command",
						},
						"args": map[string]interface{}{
							"type":        "array",
							"description": "Argument list",
							"items": map[string]interface{}{
								"type": "string",
							},
						},
					},
				},
				"ExternalMCPResponse": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"config": map[string]interface{}{
							"$ref": "#/components/schemas/ExternalMCPConfig",
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "Status",
							"enum":        []string{"connected", "disconnected", "error", "disabled"},
						},
						"toolCount": map[string]interface{}{
							"type":        "integer",
							"description": "Tool count",
						},
						"error": map[string]interface{}{
							"type":        "string",
							"description": "Error message",
						},
					},
				},
				"AddOrUpdateExternalMCPRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"config"},
					"properties": map[string]interface{}{
						"config": map[string]interface{}{
							"$ref": "#/components/schemas/ExternalMCPConfig",
						},
					},
				},
				"AttackChain": map[string]interface{}{
					"type":        "object",
					"description": "Attack chain data",
				},
				"MCPMessage": map[string]interface{}{
					"type":        "object",
					"description": "MCP message compliant with JSON-RPC 2.0",
					"required":    []string{"jsonrpc"},
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"description": "Message ID; can be a string, number, or null. Required for requests and optional for notifications.",
							"oneOf": []map[string]interface{}{
								{"type": "string"},
								{"type": "number"},
								{"type": "null"},
							},
							"example": "550e8400-e29b-41d4-a716-446655440000",
						},
						"method": map[string]interface{}{
							"type":        "string",
							"description": "Method name。supported methods：\n- `initialize`: initialize MCP connection\n- `tools/list`: list all available tools\n- `tools/call`: call tool\n- `prompts/list`: list all prompt templates\n- `prompts/get`: get prompt template\n- `resources/list`: list all resources\n- `resources/read`: read resource content\n- `sampling/request`: sampling request",
							"enum": []string{
								"initialize",
								"tools/list",
								"tools/call",
								"prompts/list",
								"prompts/get",
								"resources/list",
								"resources/read",
								"sampling/request",
							},
							"example": "tools/list",
						},
						"params": map[string]interface{}{
							"description": "Method parameters as a JSON object; structure depends on method.",
							"type":        "object",
						},
						"jsonrpc": map[string]interface{}{
							"type":        "string",
							"description": "JSON-RPC version，fixed to\"2.0\"",
							"enum":        []string{"2.0"},
							"example":     "2.0",
						},
					},
				},
				"MCPInitializeParams": map[string]interface{}{
					"type":     "object",
					"required": []string{"protocolVersion", "capabilities", "clientInfo"},
					"properties": map[string]interface{}{
						"protocolVersion": map[string]interface{}{
							"type":        "string",
							"description": "Protocol version",
							"example":     "2024-11-05",
						},
						"capabilities": map[string]interface{}{
							"type":        "object",
							"description": "Client capabilities",
						},
						"clientInfo": map[string]interface{}{
							"type":     "object",
							"required": []string{"name", "version"},
							"properties": map[string]interface{}{
								"name": map[string]interface{}{
									"type":        "string",
									"description": "Client name",
									"example":     "MyClient",
								},
								"version": map[string]interface{}{
									"type":        "string",
									"description": "Client version",
									"example":     "1.0.0",
								},
							},
						},
					},
				},
				"MCPCallToolParams": map[string]interface{}{
					"type":     "object",
					"required": []string{"name", "arguments"},
					"properties": map[string]interface{}{
						"name": map[string]interface{}{
							"type":        "string",
							"description": "Tool name",
							"example":     "nmap",
						},
						"arguments": map[string]interface{}{
							"type":        "object",
							"description": "Tool arguments as key-value pairs; exact parameters depend on the tool definition.",
							"example": map[string]interface{}{
								"target": "192.168.1.1",
								"ports":  "80,443",
							},
						},
					},
				},
				"MCPResponse": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"description": "Message ID, same as the request id",
							"oneOf": []map[string]interface{}{
								{"type": "string"},
								{"type": "number"},
								{"type": "null"},
							},
						},
						"result": map[string]interface{}{
							"description": "Method execution result（JSON object），structure depends on the called method",
							"type":        "object",
						},
						"error": map[string]interface{}{
							"type":        "object",
							"description": "Error information if execution failed",
							"properties": map[string]interface{}{
								"code": map[string]interface{}{
									"type":        "integer",
									"description": "Error code",
									"example":     -32600,
								},
								"message": map[string]interface{}{
									"type":        "string",
									"description": "Error message",
									"example":     "Invalid Request",
								},
								"data": map[string]interface{}{
									"description": "Error details, optional",
								},
							},
						},
						"jsonrpc": map[string]interface{}{
							"type":        "string",
							"description": "JSON-RPC version",
							"example":     "2.0",
						},
					},
				},
			},
		},
		"security": []map[string]interface{}{
			{
				"bearerAuth": []string{},
			},
		},
		"paths": map[string]interface{}{
			"/api/auth/login": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Authentication"},
					"summary":     "User login",
					"description": "Log in with a password to obtain an authentication token",
					"operationId": "login",
					"security":    []map[string]interface{}{},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/LoginRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Login successful",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/LoginResponse",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Incorrect password",
						},
					},
				},
			},
			"/api/auth/logout": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Authentication"},
					"summary":     "User logout",
					"description": "Log out of the current session and invalidate the token",
					"operationId": "logout",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Logout successful",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"message": map[string]interface{}{
												"type":    "string",
												"example": "Logged out",
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/auth/change-password": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Authentication"},
					"summary":     "Change password",
					"description": "Change the login password; all sessions become invalid afterwards",
					"operationId": "changePassword",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/ChangePasswordRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Password changed successfully",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"message": map[string]interface{}{
												"type":    "string",
												"example": "Password updated; please log in again with the new password",
											},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "Invalid request parameters",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/auth/validate": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Authentication"},
					"summary":     "Validate token",
					"description": "Validate whether the current token is valid",
					"operationId": "validateToken",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Token is valid",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"token": map[string]interface{}{
												"type":        "string",
												"description": "Token",
											},
											"expires_at": map[string]interface{}{
												"type":        "string",
												"format":      "date-time",
												"description": "Expiration time",
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Token is invalid or expired",
						},
					},
				},
			},
			"/api/conversations": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Conversation management"},
					"summary":     "Create conversation",
					"description": "Create a new security testing conversation.\n**Important notes**:\n- Created conversations are **saved to the database immediately**\n- The frontend page can refresh automatically to show the new conversation\n- The result is fully consistent with conversations created from the frontend\n**Two ways to create a conversation**:\n**Method 1 (recommended):** send a message directly with `/api/eino-agent` and omit `conversationId`; the system creates a new conversation and sends the message in one step.\n**Method 2:** call this endpoint first to create an empty conversation, then use the returned `conversationId` with `/api/eino-agent`. This is useful when a conversation must exist before sending messages.\n**Example**:\n```json\n{\n  \"title\": \"Web application security test\"\n}\n```",
					"operationId": "createConversation",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/CreateConversationRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Conversation created successfully",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/Conversation",
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "Invalid request parameters",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized，requires a valid token",
						},
						"500": map[string]interface{}{
							"description": "Internal server error",
						},
					},
				},
				"get": map[string]interface{}{
					"tags":        []string{"Conversation management"},
					"summary":     "List conversations",
					"description": "Get conversation list with pagination and search",
					"operationId": "listConversations",
					"parameters": []map[string]interface{}{
						{
							"name":        "limit",
							"in":          "query",
							"required":    false,
							"description": "Return count limit",
							"schema": map[string]interface{}{
								"type":    "integer",
								"default": 50,
								"minimum": 1,
								"maximum": 100,
							},
						},
						{
							"name":        "offset",
							"in":          "query",
							"required":    false,
							"description": "Offset",
							"schema": map[string]interface{}{
								"type":    "integer",
								"default": 0,
								"minimum": 0,
							},
						},
						{
							"name":        "search",
							"in":          "query",
							"required":    false,
							"description": "Search keyword",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "array",
										"items": map[string]interface{}{
											"$ref": "#/components/schemas/Conversation",
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized，requires a valid token",
						},
					},
				},
			},
			"/api/conversations/{id}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Conversation management"},
					"summary":     "View conversation details",
					"description": "Get detailed information for a conversation, including conversation data and messages",
					"operationId": "getConversation",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Conversation ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/ConversationDetail",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "Conversation does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized，requires a valid token",
						},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"Conversation management"},
					"summary":     "Update conversation",
					"description": "Update conversationtitle",
					"operationId": "updateConversation",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Conversation ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/UpdateConversationRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Update successful",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/Conversation",
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "Invalid request parameters",
						},
						"404": map[string]interface{}{
							"description": "Conversation does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized，requires a valid token",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"Conversation management"},
					"summary":     "Delete conversation",
					"description": "Delete the specified conversation and all related data such as messages and vulnerabilities. This operation cannot be undone.",
					"operationId": "deleteConversation",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Conversation ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Delete successful",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"message": map[string]interface{}{
												"type":        "string",
												"description": "Success message",
												"example":     "Delete successful",
											},
										},
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "Conversation does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized，requires a valid token",
						},
						"500": map[string]interface{}{
							"description": "Internal server error",
						},
					},
				},
			},
			"/api/conversations/{id}/project": map[string]interface{}{
				"put": map[string]interface{}{
					"tags":        []string{"Conversation management"},
					"summary":     "set conversation project",
					"description": "Bind or unbind a conversation and project for shared fact blackboard access",
					"operationId": "setConversationProject",
					"parameters": []map[string]interface{}{
						{
							"name": "id", "in": "path", "required": true,
							"description": "Conversation ID",
							"schema":      map[string]interface{}{"type": "string"},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/SetConversationProjectRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Set successfully"},
						"400": map[string]interface{}{"description": "Project does not exist or parameters are invalid"},
						"404": map[string]interface{}{"description": "Conversation does not exist"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/conversations/{id}/results": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Conversation management"},
					"summary":     "Get conversationResult",
					"description": "Get the specified conversation execution result，including messages、vulnerability information and execution results",
					"operationId": "getConversationResults",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Conversation ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/ConversationResults",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "Conversation does not existorresult does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized，requires a valid token",
						},
					},
				},
			},
			"/api/eino-agent": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Conversation interaction"},
					"summary":     "Send message and get AI reply (Eino ADK single agent, non-streaming)",
					"description": "Send a message to AI and get a non-streaming response. The request is executed by **CloudWeGo Eino** `adk.NewChatModelAgent` plus `adk.NewRunner.Run` as a single-agent MCP toolchain. It **does not depend on** `multi_agent.enabled`; `multi_agent.eino_skills`, `eino_middleware`, and related settings can apply consistently with the multi-agent main agent. Supports `webshellConnectionId`, roles, and attachments.",
					"operationId": "sendMessageEinoSingleAgent",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"message":              map[string]interface{}{"type": "string"},
										"conversationId":       map[string]interface{}{"type": "string"},
										"role":                 map[string]interface{}{"type": "string"},
										"webshellConnectionId": map[string]interface{}{"type": "string"},
									},
									"required": []string{"message"},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Success; response format matches /api/eino-agent"},
						"400": map[string]interface{}{"description": "Invalid parameters"},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"500": map[string]interface{}{"description": "Execution failed"},
					},
				},
			},
			"/api/eino-agent/stream": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Conversation interaction"},
					"summary":     "Send message and get AI reply (Eino ADK single agent, SSE)",
					"description": "Send a message to AI and get a streaming SSE response. Eino single-agent ADK executes the request; event types match multi-agent streaming, including `tool_call`, `response_delta`, and `thinking`. It **does not depend on** `multi_agent.enabled`.",
					"operationId": "sendMessageEinoSingleAgentStream",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"message":              map[string]interface{}{"type": "string"},
										"conversationId":       map[string]interface{}{"type": "string"},
										"role":                 map[string]interface{}{"type": "string"},
										"webshellConnectionId": map[string]interface{}{"type": "string"},
									},
									"required": []string{"message"},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "text/event-stream（SSE）",
							"content": map[string]interface{}{
								"text/event-stream": map[string]interface{}{
									"schema": map[string]interface{}{
										"type":        "string",
										"description": "SSE stream",
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/multi-agent": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Conversation interaction"},
					"summary":     "Send message and get AI reply (Eino multi-agent, non-streaming)",
					"description": "Uses the same request body as `POST /api/eino-agent`, but executes with CloudWeGo Eino multi-agent. Orchestration is specified by request body `orchestration` (`deep` | `plan_execute` | `supervisor`) and defaults to `deep`. Requires `multi_agent.enabled: true`; returns 404 JSON when disabled. Supports `webshellConnectionId`.",
					"operationId": "sendMessageMultiAgent",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"message": map[string]interface{}{
											"type":        "string",
											"description": "Message to send, required",
										},
										"conversationId": map[string]interface{}{
											"type":        "string",
											"description": "Conversation ID, optional; creates a new one if omitted",
										},
										"role": map[string]interface{}{
											"type":        "string",
											"description": "Role name, optional",
										},
										"webshellConnectionId": map[string]interface{}{
											"type":        "string",
											"description": "WebShell connection ID, optional; matches Eino single-agent and multi-agent streaming behavior",
										},
										"orchestration": map[string]interface{}{
											"type":        "string",
											"description": "Eino preset orchestration: deep | plan_execute | supervisor; default deep",
											"enum":        []string{"deep", "plan_execute", "supervisor"},
										},
									},
									"required": []string{"message"},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success; response format matches /api/eino-agent",
						},
						"400": map[string]interface{}{"description": "Invalid parameters"},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"404": map[string]interface{}{"description": "Multi-agent is disabled or conversation does not exist"},
						"500": map[string]interface{}{"description": "Execution failed"},
					},
				},
			},
			"/api/multi-agent/stream": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Conversation interaction"},
					"summary":     "Send message and get AI reply (Eino multi-agent, SSE)",
					"description": "Similar to `POST /api/eino-agent/stream`; executed by Eino multi-agent. `orchestration` specifies deep, plan_execute, or supervisor, defaulting to deep. Requires `multi_agent.enabled: true`; when disabled, the first SSE item is `type: error` followed by `done`. Supports `webshellConnectionId`.",
					"operationId": "sendMessageMultiAgentStream",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"message":              map[string]interface{}{"type": "string"},
										"conversationId":       map[string]interface{}{"type": "string"},
										"role":                 map[string]interface{}{"type": "string"},
										"webshellConnectionId": map[string]interface{}{"type": "string"},
										"orchestration": map[string]interface{}{
											"type":        "string",
											"description": "deep | plan_execute | supervisor; default deep",
											"enum":        []string{"deep", "plan_execute", "supervisor"},
										},
									},
									"required": []string{"message"},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "text/event-stream（SSE）",
							"content": map[string]interface{}{
								"text/event-stream": map[string]interface{}{
									"schema": map[string]interface{}{
										"type":        "string",
										"description": "SSE stream",
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/agent-loop/cancel": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Conversation interaction"},
					"summary":     "Cancel task",
					"description": "Cancel a running Agent Loop task",
					"operationId": "cancelAgentLoop",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/CancelAgentLoopRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Cancellation request submitted",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"status": map[string]interface{}{
												"type":    "string",
												"example": "cancelling",
											},
											"conversationId": map[string]interface{}{
												"type":        "string",
												"description": "Conversation ID",
											},
											"message": map[string]interface{}{
												"type":    "string",
												"example": "Cancellation requested. The task will stop after the current step completes.",
											},
										},
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "No running task found",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/agent-loop/tasks": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Conversation interaction"},
					"summary":     "List running tasks",
					"description": "Get all running Agent Loop tasks",
					"operationId": "listAgentTasks",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"tasks": map[string]interface{}{
												"type":        "array",
												"description": "Task list",
												"items": map[string]interface{}{
													"$ref": "#/components/schemas/AgentTask",
												},
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/agent-loop/tasks/completed": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Conversation interaction"},
					"summary":     "List completed tasks",
					"description": "Get recent completed Agent Loop task history",
					"operationId": "listCompletedTasks",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"tasks": map[string]interface{}{
												"type":        "array",
												"description": "Completed task list",
												"items": map[string]interface{}{
													"$ref": "#/components/schemas/AgentTask",
												},
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/batch-tasks": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Batch tasks"},
					"summary":     "Create batch task queue",
					"description": "Create a batch task queue containing multiple tasks",
					"operationId": "createBatchQueue",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/BatchTaskRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Create successful",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"queueId": map[string]interface{}{
												"type":        "string",
												"description": "Queue ID",
											},
											"queue": map[string]interface{}{
												"$ref": "#/components/schemas/BatchQueue",
											},
											"started": map[string]interface{}{
												"type":        "boolean",
												"description": "Whether execution started immediately",
											},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "Invalid request parameters",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"get": map[string]interface{}{
					"tags":        []string{"Batch tasks"},
					"summary":     "List batch task queues",
					"description": "Get all batch task queues",
					"operationId": "listBatchQueues",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"queues": map[string]interface{}{
												"type":        "array",
												"description": "Queue list",
												"items": map[string]interface{}{
													"$ref": "#/components/schemas/BatchQueue",
												},
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/batch-tasks/{queueId}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Batch tasks"},
					"summary":     "Get batch task queue",
					"description": "Get details for the specified batch task queue",
					"operationId": "getBatchQueue",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "Queue ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/BatchQueue",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "Queue does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"Batch tasks"},
					"summary":     "Delete batch task queue",
					"description": "Delete the specified batch task queue",
					"operationId": "deleteBatchQueue",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "Queue ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Delete successful",
						},
						"404": map[string]interface{}{
							"description": "Queue does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/batch-tasks/{queueId}/start": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Batch tasks"},
					"summary":     "Start batch task queue",
					"description": "Start executing batch task queuetasks in",
					"operationId": "startBatchQueue",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "Queue ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
						},
						"404": map[string]interface{}{
							"description": "Queue does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/batch-tasks/{queueId}/pause": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Batch tasks"},
					"summary":     "Pause batch task queue",
					"description": "Pause the runningBatch task queue",
					"operationId": "pauseBatchQueue",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "Queue ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Pause successful",
						},
						"404": map[string]interface{}{
							"description": "Queue does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/batch-tasks/{queueId}/tasks": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Batch tasks"},
					"summary":     "Add task to queue",
					"description": "textBatch task queueadd a new task。The task is appended to the end of the queue，and executed sequentially in queue order。Each task creates an independent conversation，with full status tracking。\n**Task format**：\nTask content is a string，describing the security testing task to execute。for example：\n- \"Scan http://example.com for SQL injection vulnerabilities\"\n- \"text 192.168.1.1 perform port scan\"\n- \"test https://target.com ofXSSvulnerability\"\n**Usage example**：\n```json\n{\n  \"task\": \"Scan http://example.com for SQL injection vulnerabilities\"\n}\n```",
					"operationId": "addBatchTask",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "Queue ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"task"},
									"properties": map[string]interface{}{
										"task": map[string]interface{}{
											"type":        "string",
											"description": "Task content，describing the security testing task to execute（required）",
											"example":     "Scan http://example.com for SQL injection vulnerabilities",
										},
									},
								},
								"examples": map[string]interface{}{
									"sqlInjection": map[string]interface{}{
										"summary":     "SQLinjection scan",
										"description": "scan target website forSQLinjection vulnerability",
										"value": map[string]interface{}{
											"task": "Scan http://example.com for SQL injection vulnerabilities",
										},
									},
									"portScan": map[string]interface{}{
										"summary":     "Port scan",
										"description": "against targetIPperform port scan",
										"value": map[string]interface{}{
											"task": "text 192.168.1.1 perform port scan",
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"taskId": map[string]interface{}{
												"type":        "string",
												"description": "Newly added task ID",
											},
											"message": map[string]interface{}{
												"type":        "string",
												"description": "Success message",
												"example":     "Task addedto queue",
											},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "Invalid request parameters（texttaskistext）",
						},
						"404": map[string]interface{}{
							"description": "Queue does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/batch-tasks/{queueId}/tasks/{taskId}": map[string]interface{}{
				"put": map[string]interface{}{
					"tags":        []string{"Batch tasks"},
					"summary":     "updateBatch tasks",
					"description": "updateBatch task queuetextofspecifiestext",
					"operationId": "updateBatchTask",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "Queue ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
						{
							"name":        "taskId",
							"in":          "path",
							"required":    true,
							"description": "Task ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"task": map[string]interface{}{
											"type":        "string",
											"description": "Task content",
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Update successful",
						},
						"404": map[string]interface{}{
							"description": "Task does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"Batch tasks"},
					"summary":     "Delete batch task",
					"description": "fromBatch task queuetextDelete specifiedtext",
					"operationId": "deleteBatchTask",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "Queue ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
						{
							"name":        "taskId",
							"in":          "path",
							"required":    true,
							"description": "Task ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Delete successful",
						},
						"404": map[string]interface{}{
							"description": "Task does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/groups": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Conversation groups"},
					"summary":     "Create group",
					"description": "Create aattachments are allowednew conversation group",
					"operationId": "createGroup",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/CreateGroupRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Create successful",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/Group",
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "Invalid request parametersorGroup nametext",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"get": map[string]interface{}{
					"tags":        []string{"Conversation groups"},
					"summary":     "List groups",
					"description": "Get all conversation groups",
					"operationId": "listGroups",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "array",
										"items": map[string]interface{}{
											"$ref": "#/components/schemas/Group",
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/groups/{id}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Conversation groups"},
					"summary":     "Get group",
					"description": "Get details for the specified group",
					"operationId": "getGroup",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Group ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/Group",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "Group does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"Conversation groups"},
					"summary":     "Update group",
					"description": "Update group information",
					"operationId": "updateGroup",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Group ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/UpdateGroupRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Update successful",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/Group",
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "Invalid request parametersorGroup nametext",
						},
						"404": map[string]interface{}{
							"description": "Group does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"Conversation groups"},
					"summary":     "Delete group",
					"description": "Delete specified group",
					"operationId": "deleteGroup",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Group ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Delete successful",
						},
						"404": map[string]interface{}{
							"description": "Group does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/groups/{id}/conversations": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Conversation groups"},
					"summary":     "Get conversations in group",
					"description": "Get all conversations in specified group",
					"operationId": "getGroupConversations",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Group ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "array",
										"items": map[string]interface{}{
											"$ref": "#/components/schemas/Conversation",
										},
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "Group does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/groups/conversations": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Conversation groups"},
					"summary":     "Add conversation to group",
					"description": "Add conversation to specified group",
					"operationId": "addConversationToGroup",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/AddConversationToGroupRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
						},
						"400": map[string]interface{}{
							"description": "Invalid request parameters",
						},
						"404": map[string]interface{}{
							"description": "Conversation or group does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/groups/{id}/conversations/{conversationId}": map[string]interface{}{
				"delete": map[string]interface{}{
					"tags":        []string{"Conversation groups"},
					"summary":     "Remove conversation from group",
					"description": "Remove conversation from specified group",
					"operationId": "removeConversationFromGroup",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Group ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
						{
							"name":        "conversationId",
							"in":          "path",
							"required":    true,
							"description": "Conversation ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Removal successful",
						},
						"404": map[string]interface{}{
							"description": "Conversation or group does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/projects": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Project management"},
					"summary":     "List projects",
					"operationId": "listProjects",
					"parameters": []map[string]interface{}{
						{"name": "status", "in": "query", "schema": map[string]interface{}{"type": "string", "enum": []string{"active", "archived"}}},
						{"name": "limit", "in": "query", "schema": map[string]interface{}{"type": "integer", "default": 200}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Project list"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"Project management"},
					"summary":     "Create project",
					"operationId": "createProject",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"name":        map[string]interface{}{"type": "string"},
										"description": map[string]interface{}{"type": "string"},
										"scope_json":  map[string]interface{}{"type": "string"},
									},
									"required": []string{"name"},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Create successful"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/projects/{id}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags": []string{"Project management"}, "summary": "Get project", "operationId": "getProject",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{"200": map[string]interface{}{"description": "Project details"}},
				},
				"put": map[string]interface{}{
					"tags": []string{"Project management"}, "summary": "Update project", "operationId": "updateProject",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{"200": map[string]interface{}{"description": "Update successful"}},
				},
				"delete": map[string]interface{}{
					"tags": []string{"Project management"}, "summary": "Delete project", "operationId": "deleteProject",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{"200": map[string]interface{}{"description": "Delete successful"}},
				},
			},
			"/api/projects/{id}/facts": map[string]interface{}{
				"get": map[string]interface{}{
					"tags": []string{"Project management"}, "summary": "List or filter by key Getfact", "operationId": "listProjectFacts",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
						{"name": "fact_key", "in": "query", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{"200": map[string]interface{}{"description": "fact list or single itemitems"}},
				},
				"post": map[string]interface{}{
					"tags": []string{"Project management"}, "summary": "create/Update fact", "operationId": "upsertProjectFactREST",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{"200": map[string]interface{}{"description": "Success"}},
				},
			},
			"/api/vulnerabilities": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Vulnerability management"},
					"summary":     "List vulnerabilities",
					"description": "GetVulnerability list，supportPaginateandFilter",
					"operationId": "listVulnerabilities",
					"parameters": []map[string]interface{}{
						{
							"name":        "limit",
							"in":          "query",
							"required":    false,
							"description": "Items per page",
							"schema": map[string]interface{}{
								"type":    "integer",
								"default": 20,
								"minimum": 1,
								"maximum": 100,
							},
						},
						{
							"name":        "offset",
							"in":          "query",
							"required":    false,
							"description": "Offset",
							"schema": map[string]interface{}{
								"type":    "integer",
								"default": 0,
								"minimum": 0,
							},
						},
						{
							"name":        "page",
							"in":          "query",
							"required":    false,
							"description": "text（andoffsetchoose one of two）",
							"schema": map[string]interface{}{
								"type":    "integer",
								"minimum": 1,
							},
						},
						{
							"name":        "id",
							"in":          "query",
							"required":    false,
							"description": "Vulnerability ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
						{
							"name":        "conversation_id",
							"in":          "query",
							"required":    false,
							"description": "Conversation ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
						{
							"name":        "project_id",
							"in":          "query",
							"required":    false,
							"description": "ProjectID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
						{
							"name":        "severity",
							"in":          "query",
							"required":    false,
							"description": "Severity",
							"schema": map[string]interface{}{
								"type": "string",
								"enum": []string{"critical", "high", "medium", "low", "info"},
							},
						},
						{
							"name":        "status",
							"in":          "query",
							"required":    false,
							"description": "Status",
							"schema": map[string]interface{}{
								"type": "string",
								"enum": []string{"open", "closed", "fixed"},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/ListVulnerabilitiesResponse",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"Vulnerability management"},
					"summary":     "Create vulnerability",
					"description": "Create aattachments are allowednew vulnerability record",
					"operationId": "createVulnerability",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/CreateVulnerabilityRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Create successful",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/Vulnerability",
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "Invalid request parameters",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/vulnerabilities/stats": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Vulnerability management"},
					"summary":     "Get vulnerability statistics",
					"description": "GetvulnerabilityStatistics",
					"operationId": "getVulnerabilityStats",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/VulnerabilityStats",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/vulnerabilities/{id}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Vulnerability management"},
					"summary":     "Getvulnerability",
					"description": "Get details for specified vulnerability",
					"operationId": "getVulnerability",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Vulnerability ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/Vulnerability",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "Vulnerability does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"Vulnerability management"},
					"summary":     "Update vulnerability",
					"description": "Update vulnerability information",
					"operationId": "updateVulnerability",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Vulnerability ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/UpdateVulnerabilityRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Update successful",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/Vulnerability",
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "Invalid request parameters",
						},
						"404": map[string]interface{}{
							"description": "Vulnerability does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"Vulnerability management"},
					"summary":     "Delete vulnerability",
					"description": "Delete specified vulnerability",
					"operationId": "deleteVulnerability",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Vulnerability ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Delete successful",
						},
						"404": map[string]interface{}{
							"description": "Vulnerability does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/roles": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Role management"},
					"summary":     "List roles",
					"description": "Get all security testing roles",
					"operationId": "getRoles",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"roles": map[string]interface{}{
												"type":        "array",
												"description": "Role list",
												"items": map[string]interface{}{
													"$ref": "#/components/schemas/RoleConfig",
												},
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"Role management"},
					"summary":     "Create role",
					"description": "Create aattachments are allowednew security testing role",
					"operationId": "createRole",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/RoleConfig",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Create successful",
						},
						"400": map[string]interface{}{
							"description": "Invalid request parameters",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/roles/{name}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Role management"},
					"summary":     "Get role",
					"description": "Get details for specified role",
					"operationId": "getRole",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "Role name",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"role": map[string]interface{}{
												"$ref": "#/components/schemas/RoleConfig",
											},
										},
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "Role does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"Role management"},
					"summary":     "Update role",
					"description": "Update specified role configuration",
					"operationId": "updateRole",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "Role name",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/RoleConfig",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Update successful",
						},
						"400": map[string]interface{}{
							"description": "Invalid request parameters",
						},
						"404": map[string]interface{}{
							"description": "Role does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"Role management"},
					"summary":     "Delete role",
					"description": "Delete specified role",
					"operationId": "deleteRole",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "Role name",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Delete successful",
						},
						"404": map[string]interface{}{
							"description": "Role does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/skills": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Skillsmanagement"},
					"summary":     "listSkills",
					"description": "Get allSkillslist，supportPaginateand search",
					"operationId": "getSkills",
					"parameters": []map[string]interface{}{
						{
							"name":        "limit",
							"in":          "query",
							"required":    false,
							"description": "Items per page",
							"schema": map[string]interface{}{
								"type":    "integer",
								"default": 20,
							},
						},
						{
							"name":        "offset",
							"in":          "query",
							"required":    false,
							"description": "Offset",
							"schema": map[string]interface{}{
								"type":    "integer",
								"default": 0,
							},
						},
						{
							"name":        "search",
							"in":          "query",
							"required":    false,
							"description": "Search keyword",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"skills": map[string]interface{}{
												"type":        "array",
												"description": "Skillslist",
												"items": map[string]interface{}{
													"$ref": "#/components/schemas/Skill",
												},
											},
											"total": map[string]interface{}{
												"type":        "integer",
												"description": "Total count",
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"Skillsmanagement"},
					"summary":     "createSkill",
					"description": "Create aattachments are allowednewSkill",
					"operationId": "createSkill",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/CreateSkillRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Create successful",
						},
						"400": map[string]interface{}{
							"description": "Invalid request parameters",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/skills/stats": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Skillsmanagement"},
					"summary":     "GetSkilltext",
					"description": "GetSkillcallStatistics",
					"operationId": "getSkillStats",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type":        "object",
										"description": "Statistics",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"Skillsmanagement"},
					"summary":     "textSkilltext",
					"description": "Clear allSkillcall statistics",
					"operationId": "clearSkillStats",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Clear successful",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/skills/{name}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Skillsmanagement"},
					"summary":     "GetSkill",
					"description": "GetspecifiesSkilldetails",
					"operationId": "getSkill",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "Skill name",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/Skill",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "Skilltext",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"Skillsmanagement"},
					"summary":     "updateSkill",
					"description": "Update specifiedSkillinformation",
					"operationId": "updateSkill",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "Skill name",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/UpdateSkillRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Update successful",
						},
						"400": map[string]interface{}{
							"description": "Invalid request parameters",
						},
						"404": map[string]interface{}{
							"description": "Skilltext",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"Skillsmanagement"},
					"summary":     "deleteSkill",
					"description": "Delete specifiedSkill",
					"operationId": "deleteSkill",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "Skill name",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Delete successful",
						},
						"404": map[string]interface{}{
							"description": "Skilltext",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/skills/{name}/bound-roles": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Skillsmanagement"},
					"summary":     "Get bound roles",
					"description": "Get roles using specifiedSkillall roles",
					"operationId": "getSkillBoundRoles",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "Skill name",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"roles": map[string]interface{}{
												"type":        "array",
												"description": "Role list",
												"items": map[string]interface{}{
													"type": "string",
												},
											},
										},
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "Skilltext",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/skills/{name}/stats": map[string]interface{}{
				"delete": map[string]interface{}{
					"tags":        []string{"Skillsmanagement"},
					"summary":     "textSkilltext",
					"description": "Clear specifiedSkillcall statistics",
					"operationId": "clearSkillStatsByName",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "Skill name",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Clear successful",
						},
						"404": map[string]interface{}{
							"description": "Skilltext",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/monitor": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"monitor"},
					"summary":     "Get monitoring information",
					"description": "Get tool execution monitoring information，supportPaginateandFilter",
					"operationId": "monitor",
					"parameters": []map[string]interface{}{
						{
							"name":        "page",
							"in":          "query",
							"required":    false,
							"description": "text",
							"schema": map[string]interface{}{
								"type":    "integer",
								"default": 1,
								"minimum": 1,
							},
						},
						{
							"name":        "page_size",
							"in":          "query",
							"required":    false,
							"description": "Items per page",
							"schema": map[string]interface{}{
								"type":    "integer",
								"default": 20,
								"minimum": 1,
								"maximum": 100,
							},
						},
						{
							"name":        "status",
							"in":          "query",
							"required":    false,
							"description": "Status filter",
							"schema": map[string]interface{}{
								"type": "string",
								"enum": []string{"success", "failed", "running"},
							},
						},
						{
							"name":        "tool",
							"in":          "query",
							"required":    false,
							"description": "Tool nameFilter（supports partial matching）",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/MonitorResponse",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/monitor/execution/{id}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"monitor"},
					"summary":     "Get execution record",
					"description": "Get details for specified execution record",
					"operationId": "getExecution",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Execution ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/ToolExecution",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "Execution record does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"monitor"},
					"summary":     "Delete execution record",
					"description": "Delete specified execution record",
					"operationId": "deleteExecution",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Execution ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Delete successful",
						},
						"404": map[string]interface{}{
							"description": "Execution record does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/monitor/execution/{id}/cancel": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"monitor"},
					"summary":     "Cancel running tool execution",
					"description": "to currently running in-process MCP tool call send context cancel signal；upper-level conversation/multi-step task can continue。returns if execution has ended or is not running in this process 404。",
					"operationId": "cancelExecution",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Execution ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"requestBody": map[string]interface{}{
						"required": false,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"note": map[string]interface{}{
											"type":        "string",
											"description": "optional。when non-empty, merge with tool output and provide to the model，with「user termination note」titleblock to distinguish fromCommandoriginal output lines",
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "termination signal sent",
						},
						"400": map[string]interface{}{
							"description": "request body is not valid JSON",
						},
						"404": map[string]interface{}{
							"description": "Not foundrunning tool execution",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/monitor/executions": map[string]interface{}{
				"delete": map[string]interface{}{
					"tags":        []string{"monitor"},
					"summary":     "textDelete execution record",
					"description": "textDelete execution record",
					"operationId": "deleteExecutions",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Delete successful",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/monitor/stats": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"monitor"},
					"summary":     "GetStatistics",
					"description": "Get tool executionStatistics",
					"operationId": "getStats",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type":        "object",
										"description": "Statistics",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/config": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Configuration management"},
					"summary":     "Get configuration",
					"description": "Get systemConfiguration information",
					"operationId": "getConfig",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/ConfigResponse",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"Configuration management"},
					"summary":     "Update configuration",
					"description": "Update system configuration",
					"operationId": "updateConfig",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/UpdateConfigRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Update successful",
						},
						"400": map[string]interface{}{
							"description": "Invalid request parameters",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/config/tools": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Configuration management"},
					"summary":     "Get tool configuration",
					"description": "Get all toolsConfiguration information",
					"operationId": "getTools",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type":        "array",
										"description": "tool configuration list",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/config/apply": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Configuration management"},
					"summary":     "Apply configuration",
					"description": "Apply configuration changes",
					"operationId": "applyConfig",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Apply successful",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/external-mcp": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"externalMCPmanagement"},
					"summary":     "List externalMCP",
					"description": "Get all externalMCPconfiguration andStatus",
					"operationId": "getExternalMCPs",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"servers": map[string]interface{}{
												"type":        "object",
												"description": "MCPserver configuration",
												"additionalProperties": map[string]interface{}{
													"$ref": "#/components/schemas/ExternalMCPResponse",
												},
											},
											"stats": map[string]interface{}{
												"type":        "object",
												"description": "Statistics",
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/external-mcp/stats": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"externalMCPmanagement"},
					"summary":     "GetexternalMCPtext",
					"description": "GetexternalMCPStatistics",
					"operationId": "getExternalMCPStats",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type":        "object",
										"description": "Statistics",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/external-mcp/{name}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"externalMCPmanagement"},
					"summary":     "GetexternalMCP",
					"description": "Get specified externalMCPconfiguration andStatus",
					"operationId": "getExternalMCP",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "MCPname",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/ExternalMCPResponse",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "MCPtext",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"externalMCPmanagement"},
					"summary":     "Add or update externalMCP",
					"description": "Add new externalMCPconfiguration or update existing configuration。\n**transport mode**：\nsupports two transport modes：\n**1. stdio（stdio）**：\n```json\n{\n  \"config\": {\n    \"enabled\": true,\n    \"command\": \"node\",\n    \"args\": [\"/path/to/mcp-server.js\"],\n    \"env\": {}\n  }\n}\n```\n**2. sse（Server-Sent Events）**：\n```json\n{\n  \"config\": {\n    \"enabled\": true,\n    \"transport\": \"sse\",\n    \"url\": \"http://127.0.0.1:8082/sse\",\n    \"timeout\": 30\n  }\n}\n```\n**configuration parameter description**：\n- `enabled`: Enabled（boolean，required）\n- `command`: Command（stdiorequired，text：\"node\", \"python\"）\n- `args`: Commandargument array（stdiorequired）\n- `env`: environment variables（object，optional）\n- `transport`: transport mode（\"stdio\" or \"sse\"，sserequired）\n- `url`: SSEendpointURL（sserequired）\n- `timeout`: timeout（seconds，optional，default30）\n- `description`: description（optional）",
					"operationId": "addOrUpdateExternalMCP",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "MCPname（unique identifier）",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/AddOrUpdateExternalMCPRequest",
								},
								"examples": map[string]interface{}{
									"stdio": map[string]interface{}{
										"summary":     "stdiotextconfiguration",
										"description": "connect to external using stdioMCPserver",
										"value": map[string]interface{}{
											"config": map[string]interface{}{
												"enabled":     true,
												"command":     "node",
												"args":        []string{"/path/to/mcp-server.js"},
												"env":         map[string]interface{}{},
												"timeout":     30,
												"description": "Node.js MCPserver",
											},
										},
									},
									"sse": map[string]interface{}{
										"summary":     "SSEtextconfiguration",
										"description": "textServer-Sent Eventsconnect to external by modeMCPserver",
										"value": map[string]interface{}{
											"config": map[string]interface{}{
												"enabled":     true,
												"transport":   "sse",
												"url":         "http://127.0.0.1:8082/sse",
												"timeout":     30,
												"description": "SSE MCPserver",
											},
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Operation successful",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"message": map[string]interface{}{
												"type":    "string",
												"example": "externalMCPConfiguration saved",
											},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "Invalid request parameters（such as invalid configuration format、or missing required fields）",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/Error",
									},
									"example": map[string]interface{}{
										"error": "stdiomode requirescommandandargsparameter",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"externalMCPmanagement"},
					"summary":     "Delete externalMCP",
					"description": "Delete specifiedofexternalMCPconfiguration",
					"operationId": "deleteExternalMCP",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "MCPname",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Delete successful",
						},
						"404": map[string]interface{}{
							"description": "MCPtext",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/external-mcp/{name}/start": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"externalMCPmanagement"},
					"summary":     "Start externalMCP",
					"description": "textspecifiesofexternalMCPserver",
					"operationId": "startExternalMCP",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "MCPname",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
						},
						"404": map[string]interface{}{
							"description": "MCPtext",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/external-mcp/{name}/stop": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"externalMCPmanagement"},
					"summary":     "Stop externalMCP",
					"description": "textspecifiesofexternalMCPserver",
					"operationId": "stopExternalMCP",
					"parameters": []map[string]interface{}{
						{
							"name":        "name",
							"in":          "path",
							"required":    true,
							"description": "MCPname",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Stop successful",
						},
						"404": map[string]interface{}{
							"description": "MCPtext",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/attack-chain/{conversationId}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"attack chain"},
					"summary":     "Get attack chain",
					"description": "Get attack-chain visualization data for specified conversation",
					"operationId": "getAttackChain",
					"parameters": []map[string]interface{}{
						{
							"name":        "conversationId",
							"in":          "path",
							"required":    true,
							"description": "Conversation ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/AttackChain",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "Conversation does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/attack-chain/{conversationId}/regenerate": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"attack chain"},
					"summary":     "Regenerate attack chain",
					"description": "Regenerate attack-chain visualization data for specified conversation",
					"operationId": "regenerateAttackChain",
					"parameters": []map[string]interface{}{
						{
							"name":        "conversationId",
							"in":          "path",
							"required":    true,
							"description": "Conversation ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Regenerate successful",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/AttackChain",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "Conversation does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/conversations/{id}/pinned": map[string]interface{}{
				"put": map[string]interface{}{
					"tags":        []string{"Conversation management"},
					"summary":     "Pin conversation",
					"description": "Set or clear conversation pinStatus",
					"operationId": "updateConversationPinned",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Conversation ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"pinned"},
									"properties": map[string]interface{}{
										"pinned": map[string]interface{}{
											"type":        "boolean",
											"description": "Pinned",
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Update successful",
						},
						"404": map[string]interface{}{
							"description": "Conversation does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/groups/{id}/pinned": map[string]interface{}{
				"put": map[string]interface{}{
					"tags":        []string{"Conversation groups"},
					"summary":     "Pin group",
					"description": "Set or clear group pinStatus",
					"operationId": "updateGroupPinned",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Group ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"pinned"},
									"properties": map[string]interface{}{
										"pinned": map[string]interface{}{
											"type":        "boolean",
											"description": "Pinned",
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Update successful",
						},
						"404": map[string]interface{}{
							"description": "Group does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/groups/{id}/conversations/{conversationId}/pinned": map[string]interface{}{
				"put": map[string]interface{}{
					"tags":        []string{"Conversation groups"},
					"summary":     "Pin conversation in group",
					"description": "Set or clear conversation pin in groupStatus",
					"operationId": "updateConversationPinnedInGroup",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Group ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
						{
							"name":        "conversationId",
							"in":          "path",
							"required":    true,
							"description": "Conversation ID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"pinned"},
									"properties": map[string]interface{}{
										"pinned": map[string]interface{}{
											"type":        "boolean",
											"description": "Pinned",
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Update successful",
						},
						"404": map[string]interface{}{
							"description": "Conversation or group does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/knowledge/categories": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Knowledge base"},
					"summary":     "Getcategory",
					"description": "Get all knowledge basecategory",
					"operationId": "getKnowledgeCategories",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"categories": map[string]interface{}{
												"type":        "array",
												"description": "categorylist",
												"items": map[string]interface{}{
													"type": "string",
												},
											},
											"enabled": map[string]interface{}{
												"type":        "boolean",
												"description": "Knowledge baseEnabled",
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/knowledge/items": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Knowledge base"},
					"summary":     "List knowledge items",
					"description": "Get all knowledge items in the knowledge base",
					"operationId": "getKnowledgeItems",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"items": map[string]interface{}{
												"type":        "array",
												"description": "Knowledge item list",
											},
											"enabled": map[string]interface{}{
												"type":        "boolean",
												"description": "Knowledge baseEnabled",
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"Knowledge base"},
					"summary":     "Create knowledge item",
					"description": "Create new knowledge item",
					"operationId": "createKnowledgeItem",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":        "object",
									"description": "Knowledge item data",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Create successful",
						},
						"400": map[string]interface{}{
							"description": "Invalid request parameters",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/knowledge/items/{id}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Knowledge base"},
					"summary":     "Get knowledge item",
					"description": "Get details for specified knowledge item",
					"operationId": "getKnowledgeItem",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "knowledge itemID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
						},
						"404": map[string]interface{}{
							"description": "Knowledge item does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"Knowledge base"},
					"summary":     "Update knowledge item",
					"description": "Update specified knowledge item",
					"operationId": "updateKnowledgeItem",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "knowledge itemID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":        "object",
									"description": "Knowledge item data",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Update successful",
						},
						"404": map[string]interface{}{
							"description": "Knowledge item does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"Knowledge base"},
					"summary":     "Delete knowledge item",
					"description": "Delete specified knowledge item",
					"operationId": "deleteKnowledgeItem",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "knowledge itemID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Delete successful",
						},
						"404": map[string]interface{}{
							"description": "Knowledge item does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/knowledge/index-status": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Knowledge base"},
					"summary":     "Get indexStatus",
					"description": "Get knowledge base index buildStatus",
					"operationId": "getIndexStatus",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"enabled": map[string]interface{}{
												"type":        "boolean",
												"description": "Knowledge baseEnabled",
											},
											"total_items": map[string]interface{}{
												"type":        "integer",
												"description": "Total knowledge items",
											},
											"indexed_items": map[string]interface{}{
												"type":        "integer",
												"description": "Indexed knowledge items",
											},
											"progress_percent": map[string]interface{}{
												"type":        "number",
												"description": "Index progress percentage",
											},
											"is_complete": map[string]interface{}{
												"type":        "boolean",
												"description": "Whether index is complete",
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/knowledge/index": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Knowledge base"},
					"summary":     "Rebuild index",
					"description": "Rebuild knowledge base index",
					"operationId": "rebuildIndex",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Index rebuild task started",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/knowledge/scan": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Knowledge base"},
					"summary":     "Scan knowledge base",
					"description": "Scan knowledge base directory，Import new knowledge files",
					"operationId": "scanKnowledgeBase",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Scan task started",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/knowledge/search": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Knowledge base"},
					"summary":     "Search knowledge base",
					"description": "Search relevant content in the knowledge base。based on vector retrieval，by semantic similarity between query and knowledge snippetssimilarity（text）return most relevantResult。\n**Search notes**：\n- semanticsimilaritytext：embedding vector + textsimilarity，configurablesimilaritythreshold and TopK\n- can filter by metadata such as risk type（text：SQLinjection、XSS、filetextetc.）\n- recommend calling first `/api/knowledge/categories` get available risk type list\n**Usage example**：\n```json\n{\n  \"query\": \"SQLinjection vulnerabilityoftesttext\",\n  \"riskType\": \"SQLinjection\",\n  \"topK\": 5,\n  \"threshold\": 0.7\n}\n```",
					"operationId": "searchKnowledge",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"query"},
									"properties": map[string]interface{}{
										"query": map[string]interface{}{
											"type":        "string",
											"description": "Search query content，Describe the security knowledge topic you want（required）",
											"example":     "SQLinjection vulnerabilityoftesttext",
										},
										"riskType": map[string]interface{}{
											"type":        "string",
											"description": "optional：Specify risk type（text：SQLinjection、XSS、filetextetc.）。recommend calling first `/api/knowledge/categories` get available risk type list，then use the correct risk type for precise search，this can greatly reduce retrieval time。if omitted, all types are searched。",
											"example":     "SQLinjection",
										},
										"topK": map[string]interface{}{
											"type":        "integer",
											"description": "optional：returnTop-KResultcount，default5",
											"default":     5,
											"minimum":     1,
											"maximum":     50,
											"example":     5,
										},
										"threshold": map[string]interface{}{
											"type":        "number",
											"format":      "float",
											"description": "optional：similaritythreshold（0-1text），default0.7。onlysimilaritygreater than or equal to this valueResultwill be returned",
											"default":     0.7,
											"minimum":     0,
											"maximum":     1,
											"example":     0.7,
										},
									},
								},
								"examples": map[string]interface{}{
									"basic": map[string]interface{}{
										"summary":     "Basic search",
										"description": "simplest search，provide only query content",
										"value": map[string]interface{}{
											"query": "SQLinjection vulnerabilityoftesttext",
										},
									},
									"withRiskType": map[string]interface{}{
										"summary":     "Search by risk type",
										"description": "specify risk type for precise search",
										"value": map[string]interface{}{
											"query":     "SQLinjection vulnerabilityoftesttext",
											"riskType":  "SQLinjection",
											"topK":      5,
											"threshold": 0.7,
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"results": map[string]interface{}{
												"type":        "array",
												"description": "Resultlist，eachattachments are allowedResultcontains：item（knowledge itemtext）、chunks（matching knowledge snippets）、score（similaritytext）",
												"items": map[string]interface{}{
													"type": "object",
													"properties": map[string]interface{}{
														"item": map[string]interface{}{
															"type":        "object",
															"description": "knowledge itemtext",
														},
														"chunks": map[string]interface{}{
															"type":        "array",
															"description": "matching knowledge snippet list",
														},
														"score": map[string]interface{}{
															"type":        "number",
															"description": "similaritytext（0-1text）",
														},
													},
												},
											},
											"enabled": map[string]interface{}{
												"type":        "boolean",
												"description": "Knowledge baseEnabled",
											},
										},
									},
									"example": map[string]interface{}{
										"results": []map[string]interface{}{
											{
												"item": map[string]interface{}{
													"id":       "item-1",
													"title":    "SQLinjection vulnerability detection",
													"category": "SQLinjection",
												},
												"chunks": []map[string]interface{}{
													{
														"text": "SQLinjection vulnerability detection methods include...",
													},
												},
												"score": 0.85,
											},
										},
										"enabled": true,
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "Invalid request parameters（textqueryistext）",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/Error",
									},
									"example": map[string]interface{}{
										"error": "Query cannot be empty",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
						"500": map[string]interface{}{
							"description": "Internal server error（such as knowledge base disabled or retrievalfailed）",
						},
					},
				},
			},
			"/api/knowledge/retrieval-logs": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Knowledge base"},
					"summary":     "Get retrieval logs",
					"description": "Get knowledge base retrieval logs",
					"operationId": "getRetrievalLogs",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"logs": map[string]interface{}{
												"type":        "array",
												"description": "Retrieval log list",
											},
											"enabled": map[string]interface{}{
												"type":        "boolean",
												"description": "Knowledge baseEnabled",
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/knowledge/retrieval-logs/{id}": map[string]interface{}{
				"delete": map[string]interface{}{
					"tags":        []string{"Knowledge base"},
					"summary":     "Delete retrieval log",
					"description": "Delete specified retrieval log",
					"operationId": "deleteRetrievalLog",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "logID",
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Delete successful",
						},
						"404": map[string]interface{}{
							"description": "Log does not exist",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			// ==================== Conversation interaction - missing endpoint ====================
			"/api/conversations/{id}/delete-turn": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Conversation interaction"},
					"summary":     "Delete conversation turn",
					"description": "Delete the conversation turn containing the specified message（from that turn user message to the next turn user all messages before the message），and clear last_react Status。",
					"operationId": "deleteConversationTurn",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Conversation ID",
							"schema":      map[string]interface{}{"type": "string"},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"messageId"},
									"properties": map[string]interface{}{
										"messageId": map[string]interface{}{
											"type":        "string",
											"description": "anchorMessage ID，identifies the turn to delete",
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Delete successful",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"deletedMessageIds": map[string]interface{}{
												"type":        "array",
												"items":       map[string]interface{}{"type": "string"},
												"description": "deletedMessage IDlist",
											},
											"message": map[string]interface{}{
												"type":    "string",
												"example": "ok",
											},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "invalid parameters or deletionfailed"},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"404": map[string]interface{}{"description": "Conversation does not exist"},
					},
				},
			},
			"/api/messages/{id}/process-details": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Conversation interaction"},
					"summary":     "Get message process details",
					"description": "Load process details for the specified message on demand，including tool calls、thinking process and other events。",
					"operationId": "getMessageProcessDetails",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"required":    true,
							"description": "Message ID",
							"schema":      map[string]interface{}{"type": "string"},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"processDetails": map[string]interface{}{
												"type": "array",
												"items": map[string]interface{}{
													"type": "object",
													"properties": map[string]interface{}{
														"id":             map[string]interface{}{"type": "string", "description": "detail recordsID"},
														"messageId":      map[string]interface{}{"type": "string", "description": "textMessage ID"},
														"conversationId": map[string]interface{}{"type": "string", "description": "textConversation ID"},
														"eventType":      map[string]interface{}{"type": "string", "description": "Event type（texttool_call, thinkingetc.）"},
														"message":        map[string]interface{}{"type": "string", "description": "Event message"},
														"data":           map[string]interface{}{"description": "Event additional data（JSON object）"},
														"createdAt":      map[string]interface{}{"type": "string", "format": "date-time", "description": "Creation time"},
													},
												},
											},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "Invalid parameters"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},

			// ==================== Batch tasks - missing endpoint ====================
			"/api/batch-tasks/{queueId}/rerun": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Batch tasks"},
					"summary":     "Rerun batch task queue",
					"description": "Reset completed or cancelledBatch task queue，Restart all tasks。",
					"operationId": "rerunBatchQueue",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "Queue ID",
							"schema":      map[string]interface{}{"type": "string"},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Rerun successful",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"message": map[string]interface{}{"type": "string", "example": "Batch task execution has restarted"},
											"queueId": map[string]interface{}{"type": "string", "description": "Queue ID"},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "Only completed or cancelled queues can be rerun"},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"404": map[string]interface{}{"description": "Queue does not exist"},
					},
				},
			},
			"/api/batch-tasks/{queueId}/metadata": map[string]interface{}{
				"put": map[string]interface{}{
					"tags":        []string{"Batch tasks"},
					"summary":     "Modify queue metadata",
					"description": "textBatch task queueoftitle、role and agent mode。",
					"operationId": "updateBatchQueueMetadata",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "Queue ID",
							"schema":      map[string]interface{}{"type": "string"},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"title":     map[string]interface{}{"type": "string", "description": "Queue title"},
										"role":      map[string]interface{}{"type": "string", "description": "Role name to use"},
										"agentMode": map[string]interface{}{"type": "string", "description": "Agent mode", "enum": []string{"eino_single", "deep", "plan_execute", "supervisor"}},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Update successful",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"queue": map[string]interface{}{"$ref": "#/components/schemas/BatchQueue"},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "Invalid parameters"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/batch-tasks/{queueId}/schedule": map[string]interface{}{
				"put": map[string]interface{}{
					"tags":        []string{"Batch tasks"},
					"summary":     "Modify queue schedule configuration",
					"description": "textBatch task queueschedule mode andCrontext。Cannot modify while queue is running。",
					"operationId": "updateBatchQueueSchedule",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "Queue ID",
							"schema":      map[string]interface{}{"type": "string"},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"scheduleMode": map[string]interface{}{"type": "string", "description": "Schedule mode", "enum": []string{"manual", "cron"}},
										"cronExpr":     map[string]interface{}{"type": "string", "description": "Crontext（scheduleModeiscronis required when）", "example": "0 2 * * *"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Update successful",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"queue": map[string]interface{}{"$ref": "#/components/schemas/BatchQueue"},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "Invalid parameters or queue is running"},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"404": map[string]interface{}{"description": "Queue does not exist"},
					},
				},
			},
			"/api/batch-tasks/{queueId}/schedule-enabled": map[string]interface{}{
				"put": map[string]interface{}{
					"tags":        []string{"Batch tasks"},
					"summary":     "ToggleCronautomatic scheduling",
					"description": "Enable or disableBatch task queueofCronautomatic scheduling feature，manual execution is notImpact。",
					"operationId": "setBatchQueueScheduleEnabled",
					"parameters": []map[string]interface{}{
						{
							"name":        "queueId",
							"in":          "path",
							"required":    true,
							"description": "Queue ID",
							"schema":      map[string]interface{}{"type": "string"},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"scheduleEnabled"},
									"properties": map[string]interface{}{
										"scheduleEnabled": map[string]interface{}{"type": "boolean", "description": "Enabledautomatic scheduling"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Set successfully",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"queue": map[string]interface{}{"$ref": "#/components/schemas/BatchQueue"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"404": map[string]interface{}{"description": "Queue does not exist"},
					},
				},
			},

			// ==================== Conversation groups - missing endpoint ====================
			"/api/groups/mappings": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Conversation groups"},
					"summary":     "Get all group mappings",
					"description": "Get all mapping relationships between conversations and groups。",
					"operationId": "getAllGroupMappings",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "array",
										"items": map[string]interface{}{
											"type": "object",
											"properties": map[string]interface{}{
												"conversation_id": map[string]interface{}{"type": "string", "description": "Conversation ID"},
												"group_id":        map[string]interface{}{"type": "string", "description": "Group ID"},
												"pinned":          map[string]interface{}{"type": "boolean", "description": "Pinned"},
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},

			// ==================== FOFAInformation gathering ====================
			"/api/fofa/search": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"FOFAInformation gathering"},
					"summary":     "FOFAtext",
					"description": "Execute through backend proxyFOFASearch query，Return asset information。",
					"operationId": "fofaSearch",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"query"},
									"properties": map[string]interface{}{
										"query":  map[string]interface{}{"type": "string", "description": "FOFAquery syntax", "example": "domain=\"example.com\""},
										"size":   map[string]interface{}{"type": "integer", "description": "Return count（default100，maximum10000）", "default": 100},
										"page":   map[string]interface{}{"type": "integer", "description": "text（default1）", "default": 1},
										"fields": map[string]interface{}{"type": "string", "description": "Return fields，comma-separated", "example": "host,ip,port,title"},
										"full":   map[string]interface{}{"type": "boolean", "description": "Whether to query all data", "default": false},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"query":         map[string]interface{}{"type": "string", "description": "Actual executed query"},
											"size":          map[string]interface{}{"type": "integer"},
											"page":          map[string]interface{}{"type": "integer"},
											"total":         map[string]interface{}{"type": "integer", "description": "Total matches"},
											"fields":        map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
											"results_count": map[string]interface{}{"type": "integer"},
											"results":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object"}, "description": "Resultlist"},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "Invalid parameters"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/fofa/parse": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"FOFAInformation gathering"},
					"summary":     "Parse natural language asFOFAsyntax",
					"description": "textAIParse natural language description asFOFAquery syntax，text。",
					"operationId": "fofaParse",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"text"},
									"properties": map[string]interface{}{
										"text": map[string]interface{}{"type": "string", "description": "textdescription", "example": "textWordPressoftext"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"query":       map[string]interface{}{"type": "string", "description": "textofFOFAquery syntax"},
											"explanation": map[string]interface{}{"type": "string", "description": "syntaxtext"},
											"warnings":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "textortext"},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "Invalid parameters"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},

			// ==================== Configuration management - missing endpoint ====================
			"/api/config/test-openai": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Configuration management"},
					"summary":     "testOpenAI APIconnection",
					"description": "testspecifiesofOpenAI/Claude APIconfigurationtext，textattachments are allowedtext。",
					"operationId": "testOpenAI",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"api_key", "model"},
									"properties": map[string]interface{}{
										"provider": map[string]interface{}{"type": "string", "description": "LLMprovidetext（openai/claude）", "example": "openai"},
										"base_url": map[string]interface{}{"type": "string", "description": "APItext（optional，defaultproviderautomatictext）"},
										"api_key":  map[string]interface{}{"type": "string", "description": "APItext"},
										"model":    map[string]interface{}{"type": "string", "description": "textname", "example": "gpt-4"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "testResult",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"success":    map[string]interface{}{"type": "boolean", "description": "textconnectionSuccess"},
											"error":      map[string]interface{}{"type": "string", "description": "failed（success=falsewhen）"},
											"model":      map[string]interface{}{"type": "string", "description": "textusedtext（success=truewhen）"},
											"latency_ms": map[string]interface{}{"type": "number", "description": "textsecondstext（success=truewhen）"},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "Invalid parameters"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},

			// ==================== Terminal ====================
			"/api/terminal/run": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Terminal"},
					"summary":     "Execute terminalCommand",
					"description": "textservertextShellCommandtextreturnResult。",
					"operationId": "terminalRun",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"command"},
									"properties": map[string]interface{}{
										"command": map[string]interface{}{"type": "string", "description": "textofCommand"},
										"shell":   map[string]interface{}{"type": "string", "description": "Shelltype（defaultsh/cmd）"},
										"cwd":     map[string]interface{}{"type": "string", "description": "Working directory（optional）"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "text",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"stdout":    map[string]interface{}{"type": "string", "description": "textOutput"},
											"stderr":    map[string]interface{}{"type": "string", "description": "text"},
											"exit_code": map[string]interface{}{"type": "integer", "description": "Exit code"},
											"error":     map[string]interface{}{"type": "string", "description": "text（optional）"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/terminal/run/stream": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Terminal"},
					"summary":     "streamingExecute terminalCommand",
					"description": "textSSEstreamingmethodtextShellCommand，textwhenreturnOutput。eachattachments are allowedtextcontains JSON: {\"t\": \"out\"|\"err\"|\"exit\", \"d\": \"data\", \"c\": Exit code}",
					"operationId": "terminalRunStream",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"command"},
									"properties": map[string]interface{}{
										"command": map[string]interface{}{"type": "string", "description": "textofCommand"},
										"shell":   map[string]interface{}{"type": "string", "description": "Shelltype（defaultsh/cmd）"},
										"cwd":     map[string]interface{}{"type": "string", "description": "Working directory（optional）"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "SSEtext",
							"content": map[string]interface{}{
								"text/event-stream": map[string]interface{}{
									"schema": map[string]interface{}{
										"type":        "string",
										"description": "Server-Sent Eventstext，eachattachments are allowedtextisJSON: {\"t\":\"out|err|exit\",\"d\":\"data\",\"c\":exitCode}",
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/terminal/ws": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Terminal"},
					"summary":     "WebSocketTerminal",
					"description": "textWebSockettextTerminalconnection，supportPTY。text/textdatatextisCommandtext，textJSON: {\"type\":\"resize\",\"cols\":80,\"rows\":24} textTerminaltext。textreturntextPTYOutput。",
					"operationId": "terminalWS",
					"responses": map[string]interface{}{
						"101": map[string]interface{}{"description": "WebSocketconnectiontext"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},

			// ==================== WebShellmanagement ====================
			"/api/webshell/connections": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"WebShellmanagement"},
					"summary":     "listWebShellconnection",
					"description": "Get alltextofWebShellconnection configurationlist。",
					"operationId": "listWebshellConnections",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "array",
										"items": map[string]interface{}{
											"type": "object",
											"properties": map[string]interface{}{
												"id":         map[string]interface{}{"type": "string", "description": "connectionID"},
												"url":        map[string]interface{}{"type": "string", "description": "WebShell URL"},
												"password":   map[string]interface{}{"type": "string", "description": "connection password"},
												"type":       map[string]interface{}{"type": "string", "description": "Shelltype", "enum": []string{"php", "asp", "aspx", "jsp", "custom"}},
												"method":     map[string]interface{}{"type": "string", "description": "Request method", "enum": []string{"get", "post"}},
												"cmd_param":  map[string]interface{}{"type": "string", "description": "CommandParameter name"},
												"remark":     map[string]interface{}{"type": "string", "description": "Notes"},
												"created_at": map[string]interface{}{"type": "string", "format": "date-time"},
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"WebShellmanagement"},
					"summary":     "createWebShellconnection",
					"description": "textattachments are allowednewWebShellconnection configuration。",
					"operationId": "createWebshellConnection",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"url"},
									"properties": map[string]interface{}{
										"url":       map[string]interface{}{"type": "string", "description": "WebShell URL"},
										"password":  map[string]interface{}{"type": "string", "description": "connection password"},
										"type":      map[string]interface{}{"type": "string", "description": "Shelltype", "enum": []string{"php", "asp", "aspx", "jsp", "custom"}},
										"method":    map[string]interface{}{"type": "string", "description": "Request method", "enum": []string{"get", "post"}},
										"cmd_param": map[string]interface{}{"type": "string", "description": "CommandParameter name"},
										"remark":    map[string]interface{}{"type": "string", "description": "Notes"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Create successful"},
						"400": map[string]interface{}{"description": "Invalid parameters"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/webshell/connections/{id}": map[string]interface{}{
				"put": map[string]interface{}{
					"tags":        []string{"WebShellmanagement"},
					"summary":     "updateWebShellconnection",
					"description": "updatetextofWebShellconnection configuration。",
					"operationId": "updateWebshellConnection",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "description": "connectionID", "schema": map[string]interface{}{"type": "string"}},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"url":       map[string]interface{}{"type": "string"},
										"password":  map[string]interface{}{"type": "string"},
										"type":      map[string]interface{}{"type": "string", "enum": []string{"php", "asp", "aspx", "jsp", "custom"}},
										"method":    map[string]interface{}{"type": "string", "enum": []string{"get", "post"}},
										"cmd_param": map[string]interface{}{"type": "string"},
										"remark":    map[string]interface{}{"type": "string"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Update successful"},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"404": map[string]interface{}{"description": "Connection does not exist"},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"WebShellmanagement"},
					"summary":     "deleteWebShellconnection",
					"description": "Delete specifiedofWebShellconnection configuration。",
					"operationId": "deleteWebshellConnection",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "description": "connectionID", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Delete successful"},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"404": map[string]interface{}{"description": "Connection does not exist"},
					},
				},
			},
			"/api/webshell/connections/{id}/state": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"WebShellmanagement"},
					"summary":     "GetconnectionStatus",
					"description": "GetWebShellconnectionoftextStatusdata。",
					"operationId": "getWebshellConnectionState",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "description": "connectionID", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"state": map[string]interface{}{"type": "object", "description": "Statusdata（textJSON）"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"WebShellmanagement"},
					"summary":     "textconnectionStatus",
					"description": "textWebShellconnectionofStatusdata。",
					"operationId": "saveWebshellConnectionState",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "description": "connectionID", "schema": map[string]interface{}{"type": "string"}},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"state": map[string]interface{}{"type": "object", "description": "Statusdata（textJSON）"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Save successful"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/webshell/connections/{id}/ai-history": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"WebShellmanagement"},
					"summary":     "GetAItext",
					"description": "GetspecifiesWebShellconnectionofAItext。",
					"operationId": "getWebshellAIHistory",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "description": "connectionID", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"conversationId": map[string]interface{}{"type": "string"},
											"messages": map[string]interface{}{
												"type": "array",
												"items": map[string]interface{}{
													"type": "object",
													"properties": map[string]interface{}{
														"id":        map[string]interface{}{"type": "string"},
														"role":      map[string]interface{}{"type": "string"},
														"content":   map[string]interface{}{"type": "string"},
														"createdAt": map[string]interface{}{"type": "string", "format": "date-time"},
													},
												},
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/webshell/connections/{id}/ai-conversations": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"WebShellmanagement"},
					"summary":     "listAItext",
					"description": "GetspecifiesWebShellconnectionoftextAItextlist。",
					"operationId": "listWebshellAIConversations",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "description": "connectionID", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "array",
										"items": map[string]interface{}{
											"type": "object",
											"properties": map[string]interface{}{
												"id":        map[string]interface{}{"type": "string"},
												"title":     map[string]interface{}{"type": "string"},
												"createdAt": map[string]interface{}{"type": "string", "format": "date-time"},
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/webshell/exec": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"WebShellmanagement"},
					"summary":     "textWebShellCommand",
					"description": "textspecifiesofWebShellconnectiontextCommand。",
					"operationId": "webshellExec",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"url", "command"},
									"properties": map[string]interface{}{
										"url":       map[string]interface{}{"type": "string", "description": "WebShell URL"},
										"password":  map[string]interface{}{"type": "string"},
										"type":      map[string]interface{}{"type": "string", "enum": []string{"php", "asp", "aspx", "jsp", "custom"}},
										"method":    map[string]interface{}{"type": "string", "enum": []string{"get", "post"}},
										"cmd_param": map[string]interface{}{"type": "string"},
										"command":   map[string]interface{}{"type": "string", "description": "textofCommand"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Execution result",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"ok":        map[string]interface{}{"type": "boolean"},
											"output":    map[string]interface{}{"type": "string", "description": "CommandOutput"},
											"error":     map[string]interface{}{"type": "string", "description": "Error message"},
											"http_code": map[string]interface{}{"type": "integer", "description": "HTTPresponsetext"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/webshell/file": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"WebShellmanagement"},
					"summary":     "WebShellfiletext",
					"description": "textWebShelltextfiletext（textdirectory、textfile、createdirectory、Rename、delete、textetc.）。",
					"operationId": "webshellFileOp",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"url", "action", "path"},
									"properties": map[string]interface{}{
										"url":         map[string]interface{}{"type": "string", "description": "WebShell URL"},
										"password":    map[string]interface{}{"type": "string"},
										"type":        map[string]interface{}{"type": "string", "enum": []string{"php", "asp", "aspx", "jsp", "custom"}},
										"method":      map[string]interface{}{"type": "string", "enum": []string{"get", "post"}},
										"cmd_param":   map[string]interface{}{"type": "string"},
										"action":      map[string]interface{}{"type": "string", "description": "texttype", "enum": []string{"list", "read", "delete", "write", "mkdir", "rename", "upload", "upload_chunk"}},
										"path":        map[string]interface{}{"type": "string", "description": "textfile/directorytext"},
										"target_path": map[string]interface{}{"type": "string", "description": "text（renamewhentext）"},
										"content":     map[string]interface{}{"type": "string", "description": "File content（write/uploadwhentext）"},
										"chunk_index": map[string]interface{}{"type": "integer", "description": "text（upload_chunkwhentext）"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Result",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"ok":     map[string]interface{}{"type": "boolean"},
											"output": map[string]interface{}{"type": "string"},
											"error":  map[string]interface{}{"type": "string"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},

			// ==================== textAttachment ====================
			"/api/chat-uploads": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"textAttachment"},
					"summary":     "listAttachment",
					"description": "Get conversationAttachmentfilelist，textConversation IDFilter。",
					"operationId": "listChatUploads",
					"parameters": []map[string]interface{}{
						{"name": "conversation", "in": "query", "required": false, "description": "textConversation IDFilter", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"files": map[string]interface{}{
												"type": "array",
												"items": map[string]interface{}{
													"type": "object",
													"properties": map[string]interface{}{
														"relativePath":   map[string]interface{}{"type": "string"},
														"absolutePath":   map[string]interface{}{"type": "string"},
														"name":           map[string]interface{}{"type": "string"},
														"size":           map[string]interface{}{"type": "integer"},
														"modifiedUnix":   map[string]interface{}{"type": "integer"},
														"date":           map[string]interface{}{"type": "string"},
														"conversationId": map[string]interface{}{"type": "string"},
														"subPath":        map[string]interface{}{"type": "string"},
													},
												},
											},
											"folders": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"textAttachment"},
					"summary":     "textAttachment",
					"description": "textfiletextAttachmentdirectory（multipart/form-data）。",
					"operationId": "uploadChatFile",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"multipart/form-data": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"file"},
									"properties": map[string]interface{}{
										"file":           map[string]interface{}{"type": "string", "format": "binary", "description": "textoffile"},
										"conversationId": map[string]interface{}{"type": "string", "description": "textofConversation ID（optional）"},
										"relativeDir":    map[string]interface{}{"type": "string", "description": "textdirectorytext（optional）"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"ok":           map[string]interface{}{"type": "boolean"},
											"relativePath": map[string]interface{}{"type": "string"},
											"absolutePath": map[string]interface{}{"type": "string"},
											"name":         map[string]interface{}{"type": "string"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"textAttachment"},
					"summary":     "deleteAttachment",
					"description": "Delete specifiedoftextAttachmentfile。",
					"operationId": "deleteChatUpload",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"path"},
									"properties": map[string]interface{}{
										"path": map[string]interface{}{"type": "string", "description": "File relative path"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Delete successful"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/chat-uploads/download": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"textAttachment"},
					"summary":     "textAttachment",
					"description": "textspecifiesoftextAttachmentfile。",
					"operationId": "downloadChatUpload",
					"parameters": []map[string]interface{}{
						{"name": "path", "in": "query", "required": true, "description": "File relative path", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "filetext",
							"content": map[string]interface{}{
								"application/octet-stream": map[string]interface{}{
									"schema": map[string]interface{}{"type": "string", "format": "binary"},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"404": map[string]interface{}{"description": "File does not exist"},
					},
				},
			},
			"/api/chat-uploads/content": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"textAttachment"},
					"summary":     "GetAttachmenttext content",
					"description": "textreturntextfileofContent。",
					"operationId": "getChatUploadContent",
					"parameters": []map[string]interface{}{
						{"name": "path", "in": "query", "required": true, "description": "File relative path", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"content": map[string]interface{}{"type": "string", "description": "file text content"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"404": map[string]interface{}{"description": "File does not exist"},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"textAttachment"},
					"summary":     "textAttachmenttext content",
					"description": "textortextfileofContent。",
					"operationId": "putChatUploadContent",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"path", "content"},
									"properties": map[string]interface{}{
										"path":    map[string]interface{}{"type": "string", "description": "File relative path"},
										"content": map[string]interface{}{"type": "string", "description": "file text content"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Success"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/chat-uploads/mkdir": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"textAttachment"},
					"summary":     "createAttachmentdirectory",
					"description": "textAttachmentdirectorytextcreatetextdirectory。",
					"operationId": "mkdirChatUpload",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"name"},
									"properties": map[string]interface{}{
										"parent": map[string]interface{}{"type": "string", "description": "textdirectorytext"},
										"name":   map[string]interface{}{"type": "string", "description": "directoryname"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Create successful",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"ok":           map[string]interface{}{"type": "boolean"},
											"relativePath": map[string]interface{}{"type": "string"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/chat-uploads/rename": map[string]interface{}{
				"put": map[string]interface{}{
					"tags":        []string{"textAttachment"},
					"summary":     "RenameAttachment",
					"description": "RenametextAttachmentfileordirectory。",
					"operationId": "renameChatUpload",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"path", "newName"},
									"properties": map[string]interface{}{
										"path":    map[string]interface{}{"type": "string", "description": "textFile relative path"},
										"newName": map[string]interface{}{"type": "string", "description": "textname"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "RenameSuccess",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"ok":           map[string]interface{}{"type": "boolean"},
											"relativePath": map[string]interface{}{"type": "string"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},

			// ==================== Robot integration ====================
			"/api/robot/wecom": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Robot integration"},
					"summary":     "text",
					"description": "textserverURLtext（textconfigurationtextwhenoftext）。noAuthentication。",
					"operationId": "wecomCallbackVerify",
					"security":    []map[string]interface{}{},
					"parameters": []map[string]interface{}{
						{"name": "msg_signature", "in": "query", "required": true, "schema": map[string]interface{}{"type": "string"}},
						{"name": "timestamp", "in": "query", "required": true, "schema": map[string]interface{}{"type": "string"}},
						{"name": "nonce", "in": "query", "required": true, "schema": map[string]interface{}{"type": "string"}},
						{"name": "echostr", "in": "query", "required": true, "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Success，returntextofechostr"},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"Robot integration"},
					"summary":     "text",
					"description": "textoftext。noAuthentication，textservercall。",
					"operationId": "wecomCallbackMessage",
					"security":    []map[string]interface{}{},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Processed successfully"},
					},
				},
			},
			"/api/robot/dingtalk": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Robot integration"},
					"summary":     "text",
					"description": "textoftext。noAuthentication，textservercall。",
					"operationId": "dingtalkCallback",
					"security":    []map[string]interface{}{},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Processed successfully"},
					},
				},
			},
			"/api/robot/lark": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Robot integration"},
					"summary":     "text",
					"description": "textoftext。noAuthentication，textservercall。",
					"operationId": "larkCallback",
					"security":    []map[string]interface{}{},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Processed successfully"},
					},
				},
			},
			"/api/robot/test": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Robot integration"},
					"summary":     "testtext",
					"description": "text，textandtext。textAuthentication。",
					"operationId": "testRobot",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"platform", "text"},
									"properties": map[string]interface{}{
										"platform": map[string]interface{}{"type": "string", "description": "texttype", "enum": []string{"dingtalk", "lark", "wecom"}},
										"user_id":  map[string]interface{}{"type": "string", "description": "textID", "example": "test"},
										"text":     map[string]interface{}{"type": "string", "description": "text", "example": "text"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Processed successfully"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},

			// ==================== multi-agentMarkdown ====================
			"/api/multi-agent/markdown-agents": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"multi-agentMarkdown"},
					"summary":     "listMarkdownProxy",
					"description": "Get allmulti-agentMarkdowntextfilelist。",
					"operationId": "listMarkdownAgents",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"agents": map[string]interface{}{
												"type": "array",
												"items": map[string]interface{}{
													"type": "object",
													"properties": map[string]interface{}{
														"filename":        map[string]interface{}{"type": "string", "description": "File name"},
														"id":              map[string]interface{}{"type": "string", "description": "ProxyID"},
														"name":            map[string]interface{}{"type": "string", "description": "Proxy name"},
														"description":     map[string]interface{}{"type": "string", "description": "Proxy description"},
														"is_orchestrator": map[string]interface{}{"type": "boolean", "description": "textistext"},
														"kind":            map[string]interface{}{"type": "string", "description": "Orchestration type"},
													},
												},
											},
											"dir": map[string]interface{}{"type": "string", "description": "Proxytextdirectorytext"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"multi-agentMarkdown"},
					"summary":     "createMarkdownProxy",
					"description": "createnewmulti-agentMarkdowntextfile。",
					"operationId": "createMarkdownAgent",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"name"},
									"properties": map[string]interface{}{
										"filename":       map[string]interface{}{"type": "string", "description": "File name（optional，automatictext）"},
										"id":             map[string]interface{}{"type": "string", "description": "ProxyID"},
										"name":           map[string]interface{}{"type": "string", "description": "Proxy name"},
										"description":    map[string]interface{}{"type": "string", "description": "Proxy description"},
										"tools":          map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "textTool list"},
										"instruction":    map[string]interface{}{"type": "string", "description": "Proxytext"},
										"bind_role":      map[string]interface{}{"type": "string", "description": "text"},
										"max_iterations": map[string]interface{}{"type": "integer", "description": "maximumtext"},
										"kind":           map[string]interface{}{"type": "string", "description": "Orchestration type"},
										"raw":            map[string]interface{}{"type": "string", "description": "OriginalMarkdownContent"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Create successful",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"filename": map[string]interface{}{"type": "string"},
											"message":  map[string]interface{}{"type": "string", "example": "textcreate"},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "Invalid parameters"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},
			"/api/multi-agent/markdown-agents/{filename}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"multi-agentMarkdown"},
					"summary":     "GetMarkdownProxytext",
					"description": "GetspecifiesMarkdownProxytextfileoftextContent。",
					"operationId": "getMarkdownAgent",
					"parameters": []map[string]interface{}{
						{"name": "filename", "in": "path", "required": true, "description": "File name", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"filename":        map[string]interface{}{"type": "string"},
											"raw":             map[string]interface{}{"type": "string", "description": "OriginalMarkdownContent"},
											"id":              map[string]interface{}{"type": "string"},
											"name":            map[string]interface{}{"type": "string"},
											"description":     map[string]interface{}{"type": "string"},
											"tools":           map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
											"instruction":     map[string]interface{}{"type": "string"},
											"bind_role":       map[string]interface{}{"type": "string"},
											"max_iterations":  map[string]interface{}{"type": "integer"},
											"kind":            map[string]interface{}{"type": "string"},
											"is_orchestrator": map[string]interface{}{"type": "boolean"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"404": map[string]interface{}{"description": "Proxy does not exist"},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"multi-agentMarkdown"},
					"summary":     "updateMarkdownProxy",
					"description": "Update specifiedofMarkdownProxytext。",
					"operationId": "updateMarkdownAgent",
					"parameters": []map[string]interface{}{
						{"name": "filename", "in": "path", "required": true, "description": "File name", "schema": map[string]interface{}{"type": "string"}},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"name":           map[string]interface{}{"type": "string"},
										"description":    map[string]interface{}{"type": "string"},
										"tools":          map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
										"instruction":    map[string]interface{}{"type": "string"},
										"bind_role":      map[string]interface{}{"type": "string"},
										"max_iterations": map[string]interface{}{"type": "integer"},
										"kind":           map[string]interface{}{"type": "string"},
										"raw":            map[string]interface{}{"type": "string"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Update successful",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"message": map[string]interface{}{"type": "string", "example": "text"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"404": map[string]interface{}{"description": "Proxy does not exist"},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"multi-agentMarkdown"},
					"summary":     "deleteMarkdownProxy",
					"description": "Delete specifiedofMarkdownProxytextfile。",
					"operationId": "deleteMarkdownAgent",
					"parameters": []map[string]interface{}{
						{"name": "filename", "in": "path", "required": true, "description": "File name", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Delete successful",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"message": map[string]interface{}{"type": "string", "example": "textdelete"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"404": map[string]interface{}{"description": "Proxy does not exist"},
					},
				},
			},

			// ==================== Skillsmanagement - missing endpoint ====================
			"/api/skills/{name}/files": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Skillsmanagement"},
					"summary":     "listtextfile",
					"description": "Getspecifiestextdirectorytextoftextfilelist。",
					"operationId": "listSkillPackageFiles",
					"parameters": []map[string]interface{}{
						{"name": "name", "in": "path", "required": true, "description": "Skill name/ID", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"files": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "filetextlist"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"404": map[string]interface{}{"description": "text"},
					},
				},
			},
			"/api/skills/{name}/file": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Skillsmanagement"},
					"summary":     "GettextFile content",
					"description": "textspecifiesfileofContent。",
					"operationId": "getSkillPackageFile",
					"parameters": []map[string]interface{}{
						{"name": "name", "in": "path", "required": true, "description": "Skill name/ID", "schema": map[string]interface{}{"type": "string"}},
						{"name": "path", "in": "query", "required": true, "description": "File relative path", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"path":    map[string]interface{}{"type": "string", "description": "filetext"},
											"content": map[string]interface{}{"type": "string", "description": "File content"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
						"404": map[string]interface{}{"description": "File does not exist"},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"Skillsmanagement"},
					"summary":     "textfile",
					"description": "textorupdatetextofFile content。",
					"operationId": "putSkillPackageFile",
					"parameters": []map[string]interface{}{
						{"name": "name", "in": "path", "required": true, "description": "Skill name/ID", "schema": map[string]interface{}{"type": "string"}},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"path"},
									"properties": map[string]interface{}{
										"path":    map[string]interface{}{"type": "string", "description": "File relative path"},
										"content": map[string]interface{}{"type": "string", "description": "File content"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Save successful",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"message": map[string]interface{}{"type": "string", "example": "saved"},
											"path":    map[string]interface{}{"type": "string"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},

			// ==================== monitor - missing endpoint ====================
			"/api/monitor/executions/names": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"monitor"},
					"summary":     "textGetTool name",
					"description": "textExecution IDlisttextGettextofTool name，textN+1text。",
					"operationId": "batchGetToolNames",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"ids"},
									"properties": map[string]interface{}{
										"ids": map[string]interface{}{
											"type":        "array",
											"items":       map[string]interface{}{"type": "string"},
											"description": "textIDlist",
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success，returnIDtextTool nameoftext",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type":                 "object",
										"additionalProperties": map[string]interface{}{"type": "string"},
										"description":          "textisExecution ID，textisTool name",
										"example":              map[string]interface{}{"exec-001": "nmap", "exec-002": "sqlmap"},
									},
								},
							},
						},
						"400": map[string]interface{}{"description": "Invalid parameters"},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},

			// ==================== Knowledge base - missing endpoint ====================
			"/api/knowledge/stats": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Knowledge base"},
					"summary":     "GetKnowledge basetext",
					"description": "GetKnowledge baseoftextStatistics，textcategorytextanditemstext。",
					"operationId": "getKnowledgeStats",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"enabled":          map[string]interface{}{"type": "boolean", "description": "Knowledge baseEnabled"},
											"total_categories": map[string]interface{}{"type": "integer", "description": "categoryTotal count"},
											"total_items":      map[string]interface{}{"type": "integer", "description": "itemstextTotal count"},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{"description": "Unauthorized"},
					},
				},
			},

			"/api/mcp": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"MCP"},
					"summary":     "MCPendpoint",
					"description": "MCP (Model Context Protocol) endpoint，textMCPtext。\n**text**：\ntextendpointtext JSON-RPC 2.0 text，supporttext：\n**1. initialize** - initialize MCP connection\n```json\n{\n  \"jsonrpc\": \"2.0\",\n  \"id\": \"init-1\",\n  \"method\": \"initialize\",\n  \"params\": {\n    \"protocolVersion\": \"2024-11-05\",\n    \"capabilities\": {},\n    \"clientInfo\": {\n      \"name\": \"MyClient\",\n      \"version\": \"1.0.0\"\n    }\n  }\n}\n```\n**2. tools/list** - list all available tools\n```json\n{\n  \"jsonrpc\": \"2.0\",\n  \"id\": \"list-1\",\n  \"method\": \"tools/list\",\n  \"params\": {}\n}\n```\n**3. tools/call** - call tool\n```json\n{\n  \"jsonrpc\": \"2.0\",\n  \"id\": \"call-1\",\n  \"method\": \"tools/call\",\n  \"params\": {\n    \"name\": \"nmap\",\n    \"arguments\": {\n      \"target\": \"192.168.1.1\",\n      \"ports\": \"80,443\"\n    }\n  }\n}\n```\n**4. prompts/list** - list all prompt templates\n```json\n{\n  \"jsonrpc\": \"2.0\",\n  \"id\": \"prompts-list-1\",\n  \"method\": \"prompts/list\",\n  \"params\": {}\n}\n```\n**5. prompts/get** - get prompt template\n```json\n{\n  \"jsonrpc\": \"2.0\",\n  \"id\": \"prompt-get-1\",\n  \"method\": \"prompts/get\",\n  \"params\": {\n    \"name\": \"prompt-name\",\n    \"arguments\": {}\n  }\n}\n```\n**6. resources/list** - list all resources\n```json\n{\n  \"jsonrpc\": \"2.0\",\n  \"id\": \"resources-list-1\",\n  \"method\": \"resources/list\",\n  \"params\": {}\n}\n```\n**7. resources/read** - read resource content\n```json\n{\n  \"jsonrpc\": \"2.0\",\n  \"id\": \"resource-read-1\",\n  \"method\": \"resources/read\",\n  \"params\": {\n    \"uri\": \"resource://example\"\n  }\n}\n```\n**Error codetext**：\n- `-32700`: Parse error - JSONtext\n- `-32600`: Invalid Request - text\n- `-32601`: Method not found - text\n- `-32602`: Invalid params - parametertext\n- `-32603`: Internal error - text",
					"operationId": "mcpEndpoint",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/MCPMessage",
								},
								"examples": map[string]interface{}{
									"listTools": map[string]interface{}{
										"summary":     "listtexttool",
										"description": "Get systemtextofMCPTool list",
										"value": map[string]interface{}{
											"jsonrpc": "2.0",
											"id":      "list-tools-1",
											"method":  "tools/list",
											"params":  map[string]interface{}{},
										},
									},
									"callTool": map[string]interface{}{
										"summary":     "call tool",
										"description": "callspecifiesofMCPtool",
										"value": map[string]interface{}{
											"jsonrpc": "2.0",
											"id":      "call-tool-1",
											"method":  "tools/call",
											"params": map[string]interface{}{
												"name": "nmap",
												"arguments": map[string]interface{}{
													"target": "192.168.1.1",
													"ports":  "80,443",
												},
											},
										},
									},
									"initialize": map[string]interface{}{
										"summary":     "textconnection",
										"description": "initialize MCP connection，Getservertext",
										"value": map[string]interface{}{
											"jsonrpc": "2.0",
											"id":      "init-1",
											"method":  "initialize",
											"params": map[string]interface{}{
												"protocolVersion": "2024-11-05",
												"capabilities":    map[string]interface{}{},
												"clientInfo": map[string]interface{}{
													"name":    "MyClient",
													"version": "1.0.0",
												},
											},
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "MCPresponse（JSON-RPC 2.0text）",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/MCPResponse",
									},
									"examples": map[string]interface{}{
										"success": map[string]interface{}{
											"summary":     "Successresponse",
											"description": "toolcallSuccessofresponseExample",
											"value": map[string]interface{}{
												"jsonrpc": "2.0",
												"id":      "call-tool-1",
												"result": map[string]interface{}{
													"content": []map[string]interface{}{
														{
															"type": "text",
															"text": "toolExecution result...",
														},
													},
													"isError": false,
												},
											},
										},
										"error": map[string]interface{}{
											"summary":     "textresponse",
											"description": "toolcallfailedofresponseExample",
											"value": map[string]interface{}{
												"jsonrpc": "2.0",
												"id":      "call-tool-1",
												"error": map[string]interface{}{
													"code":    -32601,
													"message": "Tool not found",
													"data":    "tool 'unknown-tool' text",
												},
											},
										},
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "text（JSONfailed）",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/MCPResponse",
									},
									"example": map[string]interface{}{
										"id": nil,
										"error": map[string]interface{}{
											"code":    -32700,
											"message": "Parse error",
											"data":    "unexpected end of JSON input",
										},
										"jsonrpc": "2.0",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized，requires a valid token",
						},
						"405": map[string]interface{}{
							"description": "text（textsupportPOSTtext）",
						},
					},
				},
			},
		},
	}

	enrichSpecWithI18nKeys(spec)
	c.JSON(http.StatusOK, spec)
}

// GetConversationResults Get conversationResult（OpenAPIendpoint）
// text：Create conversationandGet conversationtextuse directlytextof /api/conversations endpoint
// textattachments are allowedendpointtextistextprovideResult
func (h *OpenAPIHandler) GetConversationResults(c *gin.Context) {
	conversationID := c.Param("id")

	// Verify that the conversation exists
	conv, err := h.db.GetConversation(conversationID)
	if err != nil {
		h.logger.Error("Get conversationfailed", zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{"error": "Conversation does not exist"})
		return
	}

	// GetMessage list
	messages, err := h.db.GetMessages(conversationID)
	if err != nil {
		h.logger.Error("Getfailed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// GetVulnerability list
	vulnList, err := h.db.ListVulnerabilities(1000, 0, database.VulnerabilityListFilter{ConversationID: conversationID})
	if err != nil {
		h.logger.Warn("GetVulnerability listfailed", zap.Error(err))
		vulnList = []*database.Vulnerability{}
	}
	vulnerabilities := make([]database.Vulnerability, len(vulnList))
	for i, v := range vulnList {
		vulnerabilities[i] = *v
	}

	// GetExecution result（fromMCPtextGet）
	executionResults := []map[string]interface{}{}
	for _, msg := range messages {
		if len(msg.MCPExecutionIDs) > 0 {
			for _, execID := range msg.MCPExecutionIDs {
				// textfromResultGetExecution result
				if h.resultStorage != nil {
					result, err := h.resultStorage.GetResult(execID)
					if err == nil && result != "" {
						// GettextdatatextGetTool nameandCreation time
						metadata, err := h.resultStorage.GetResultMetadata(execID)
						toolName := "unknown"
						createdAt := time.Now()
						if err == nil && metadata != nil {
							toolName = metadata.ToolName
							createdAt = metadata.CreatedAt
						}
						executionResults = append(executionResults, map[string]interface{}{
							"id":        execID,
							"toolName":  toolName,
							"status":    "success",
							"result":    result,
							"createdAt": createdAt.Format(time.RFC3339),
						})
					}
				}
			}
		}
	}

	response := map[string]interface{}{
		"conversationId":   conv.ID,
		"messages":         messages,
		"vulnerabilities":  vulnerabilities,
		"executionResults": executionResults,
	}

	c.JSON(http.StatusOK, response)
}
