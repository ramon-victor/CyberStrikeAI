package handler

// addAgentRunSpec reuses each synchronous request schema without changing it.
func addAgentRunSpec(spec map[string]interface{}) {
	paths := spec["paths"].(map[string]interface{})
	for _, base := range []string{"/api/eino-agent", "/api/multi-agent"} {
		original := paths[base].(map[string]interface{})["post"].(map[string]interface{})
		paths[base+"/runs"] = map[string]interface{}{"post": map[string]interface{}{
			"tags": []string{"Conversation Interaction"}, "summary": "Submit conversation tasks asynchronously", "description": "Returns 202 after acceptance; client disconnect does not cancel execution. Requires a per-user Idempotency-Key. Identical key and request return the original receipt; different requests return 409. Poll statusUrl for results; cancel via /api/agent-loop/cancel.",
			"requestBody": original["requestBody"],
			"parameters":  []map[string]interface{}{{"in": "header", "name": "Idempotency-Key", "required": true, "schema": map[string]interface{}{"type": "string", "minLength": 1, "maxLength": 200}}},
			"responses":   map[string]interface{}{"202": map[string]interface{}{"description": "Accepted receipt: runId, conversationId, assistantMessageId, status and statusUrl; Location contains the status URL"}, "400": map[string]interface{}{"description": "Invalid parameter or pattern"}, "403": map[string]interface{}{"description": "No permission"}, "409": map[string]interface{}{"description": "Idempotent key payload conflict or session is executing"}, "500": map[string]interface{}{"description": "Failed to receive records"}},
		}}
		paths[base+"/runs/{runId}"] = map[string]interface{}{"get": map[string]interface{}{
			"tags": []string{"Conversation Interaction"}, "summary": "Query asynchronous conversation tasks", "description": "Retrieve persisted state, result, conversation/message IDs and timestamps. Unfinished tasks become interrupted after restart and are not replayed automatically.",
			"parameters": []map[string]interface{}{{"in": "path", "name": "runId", "required": true, "schema": map[string]interface{}{"type": "string"}}},
			"responses":  map[string]interface{}{"200": map[string]interface{}{"description": "Task status and results"}, "403": map[string]interface{}{"description": "Don't have access to conversation"}, "404": map[string]interface{}{"description": "Task not found"}},
		}}
	}
}
