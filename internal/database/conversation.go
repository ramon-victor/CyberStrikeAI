package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Conversation conversation
type Conversation struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	ProjectID string    `json:"projectId,omitempty"`
	Pinned    bool      `json:"pinned"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Messages  []Message `json:"messages,omitempty"`
}

// Message message
type Message struct {
	ID               string                   `json:"id"`
	ConversationID   string                   `json:"conversationId"`
	Role             string                   `json:"role"`
	Content          string                   `json:"content"`
	ReasoningContent string                   `json:"reasoningContent,omitempty"`
	MCPExecutionIDs  []string                 `json:"mcpExecutionIds,omitempty"`
	ProcessDetails   []map[string]interface{} `json:"processDetails,omitempty"`
	CreatedAt        time.Time                `json:"createdAt"`
	UpdatedAt        time.Time                `json:"updatedAt"`
}

// CreateConversation creates a new conversation
func (db *DB) CreateConversation(title string, meta ConversationCreateMeta) (*Conversation, error) {
	return db.CreateConversationWithWebshell("", title, meta)
}

// CreateConversationWithWebshell creates a new conversation, optionally bound to a WebShell connection ID; empty means a normal conversation
func (db *DB) CreateConversationWithWebshell(webshellConnectionID, title string, meta ConversationCreateMeta) (*Conversation, error) {
	id := uuid.New().String()
	now := time.Now()

	projectID := strings.TrimSpace(meta.ProjectID)
	if projectID != "" {
		if _, err := db.GetProject(projectID); err != nil {
			return nil, err
		}
	}

	var err error
	wsID := strings.TrimSpace(webshellConnectionID)
	switch {
	case wsID != "" && projectID != "":
		_, err = db.Exec(
			"INSERT INTO conversations (id, title, created_at, updated_at, webshell_connection_id, project_id) VALUES (?, ?, ?, ?, ?, ?)",
			id, title, now, now, wsID, projectID,
		)
	case wsID != "":
		_, err = db.Exec(
			"INSERT INTO conversations (id, title, created_at, updated_at, webshell_connection_id) VALUES (?, ?, ?, ?, ?)",
			id, title, now, now, wsID,
		)
	case projectID != "":
		_, err = db.Exec(
			"INSERT INTO conversations (id, title, created_at, updated_at, project_id) VALUES (?, ?, ?, ?, ?)",
			id, title, now, now, projectID,
		)
	default:
		_, err = db.Exec(
			"INSERT INTO conversations (id, title, created_at, updated_at) VALUES (?, ?, ?, ?)",
			id, title, now, now,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create conversation: %w", err)
	}

	conv := &Conversation{
		ID:        id,
		Title:     title,
		ProjectID: projectID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if wsID != "" {
		meta.WebShellConnectionID = wsID
	}
	notifyConversationCreated(conv, meta)
	return conv, nil
}

// GetConversationByWebshellConnectionID gets the most recent conversation for a WebShell connection ID (for AI assistant persistence)
func (db *DB) GetConversationByWebshellConnectionID(connectionID string) (*Conversation, error) {
	if connectionID == "" {
		return nil, fmt.Errorf("connectionID is empty")
	}
	var conv Conversation
	var createdAt, updatedAt string
	var pinned int
	err := db.QueryRow(
		"SELECT id, title, pinned, created_at, updated_at FROM conversations WHERE webshell_connection_id = ? ORDER BY updated_at DESC LIMIT 1",
		connectionID,
	).Scan(&conv.ID, &conv.Title, &pinned, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query conversation: %w", err)
	}
	conv.Pinned = pinned != 0
	if t, e := time.Parse("2006-01-02 15:04:05.999999999-07:00", createdAt); e == nil {
		conv.CreatedAt = t
	} else if t, e := time.Parse("2006-01-02 15:04:05", createdAt); e == nil {
		conv.CreatedAt = t
	} else {
		conv.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	}
	if t, e := time.Parse("2006-01-02 15:04:05.999999999-07:00", updatedAt); e == nil {
		conv.UpdatedAt = t
	} else if t, e := time.Parse("2006-01-02 15:04:05", updatedAt); e == nil {
		conv.UpdatedAt = t
	} else {
		conv.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	}
	messages, err := db.GetMessages(conv.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to load messages: %w", err)
	}
	conv.Messages = messages

	// Load process details and attach them to the corresponding messages, matching GetConversation so execution details remain visible after refresh.
	processDetailsMap, err := db.GetProcessDetailsByConversation(conv.ID)
	if err != nil {
		db.logger.Warn("failed to load process details", zap.Error(err))
		processDetailsMap = make(map[string][]ProcessDetail)
	}
	for i := range conv.Messages {
		if details, ok := processDetailsMap[conv.Messages[i].ID]; ok {
			details = DedupeConsecutiveProcessDetails(details)
			detailsJSON := make([]map[string]interface{}, len(details))
			for j, detail := range details {
				var data interface{}
				if detail.Data != "" {
					if err := json.Unmarshal([]byte(detail.Data), &data); err != nil {
						db.logger.Warn("failed to parse process detail data", zap.Error(err))
					}
				}
				detailsJSON[j] = map[string]interface{}{
					"id":             detail.ID,
					"messageId":      detail.MessageID,
					"conversationId": detail.ConversationID,
					"eventType":      detail.EventType,
					"message":        detail.Message,
					"data":           data,
					"createdAt":      detail.CreatedAt,
				}
			}
			conv.Messages[i].ProcessDetails = detailsJSON
		}
	}

	return &conv, nil
}

// WebShellConversationItem is used for sidebar lists and excludes messages
type WebShellConversationItem struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ListConversationsByWebshellConnectionID lists all conversations for this WebShell connection in descending update time for sidebar display
func (db *DB) ListConversationsByWebshellConnectionID(connectionID string) ([]WebShellConversationItem, error) {
	if connectionID == "" {
		return nil, nil
	}
	rows, err := db.Query(
		"SELECT id, title, updated_at FROM conversations WHERE webshell_connection_id = ? ORDER BY updated_at DESC",
		connectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query conversation list: %w", err)
	}
	defer rows.Close()
	var list []WebShellConversationItem
	for rows.Next() {
		var item WebShellConversationItem
		var updatedAt string
		if err := rows.Scan(&item.ID, &item.Title, &updatedAt); err != nil {
			continue
		}
		if t, e := time.Parse("2006-01-02 15:04:05.999999999-07:00", updatedAt); e == nil {
			item.UpdatedAt = t
		} else if t, e := time.Parse("2006-01-02 15:04:05", updatedAt); e == nil {
			item.UpdatedAt = t
		} else {
			item.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

// ConversationExists reports whether a conversation row exists (lightweight check for audit links).
func (db *DB) ConversationExists(id string) (bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return false, nil
	}
	var one int
	err := db.QueryRow("SELECT 1 FROM conversations WHERE id = ? LIMIT 1", id).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// GetConversation gets a conversation
func (db *DB) GetConversation(id string) (*Conversation, error) {
	var conv Conversation
	var createdAt, updatedAt string
	var pinned int

	var projectID sql.NullString
	err := db.QueryRow(
		"SELECT id, title, pinned, created_at, updated_at, project_id FROM conversations WHERE id = ?",
		id,
	).Scan(&conv.ID, &conv.Title, &pinned, &createdAt, &updatedAt, &projectID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("conversation does not exist")
		}
		return nil, fmt.Errorf("failed to query conversation: %w", err)
	}
	if projectID.Valid {
		conv.ProjectID = strings.TrimSpace(projectID.String)
	}

	// Try parsing multiple time formats.
	var err1, err2 error
	conv.CreatedAt, err1 = time.Parse("2006-01-02 15:04:05.999999999-07:00", createdAt)
	if err1 != nil {
		conv.CreatedAt, err1 = time.Parse("2006-01-02 15:04:05", createdAt)
	}
	if err1 != nil {
		conv.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	}

	conv.UpdatedAt, err2 = time.Parse("2006-01-02 15:04:05.999999999-07:00", updatedAt)
	if err2 != nil {
		conv.UpdatedAt, err2 = time.Parse("2006-01-02 15:04:05", updatedAt)
	}
	if err2 != nil {
		conv.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	}

	conv.Pinned = pinned != 0

	// Load messages
	messages, err := db.GetMessages(id)
	if err != nil {
		return nil, fmt.Errorf("failed to load messages: %w", err)
	}
	conv.Messages = messages

	// Load process details grouped by message ID.
	processDetailsMap, err := db.GetProcessDetailsByConversation(id)
	if err != nil {
		db.logger.Warn("failed to load process details", zap.Error(err))
		processDetailsMap = make(map[string][]ProcessDetail)
	}

	// Attach process details to their corresponding messages.
	for i := range conv.Messages {
		if details, ok := processDetailsMap[conv.Messages[i].ID]; ok {
			details = DedupeConsecutiveProcessDetails(details)
			// Convert ProcessDetail to JSON format for frontend use.
			detailsJSON := make([]map[string]interface{}, len(details))
			for j, detail := range details {
				var data interface{}
				if detail.Data != "" {
					if err := json.Unmarshal([]byte(detail.Data), &data); err != nil {
						db.logger.Warn("failed to parse process detail data", zap.Error(err))
					}
				}
				detailsJSON[j] = map[string]interface{}{
					"id":             detail.ID,
					"messageId":      detail.MessageID,
					"conversationId": detail.ConversationID,
					"eventType":      detail.EventType,
					"message":        detail.Message,
					"data":           data,
					"createdAt":      detail.CreatedAt,
				}
			}
			conv.Messages[i].ProcessDetails = detailsJSON
		}
	}

	return &conv, nil
}

// GetConversationLite gets a lightweight conversation with messages but without loading process_details.
// Used for fast historical conversation switching; avoids sending large process detail payloads to the frontend at once and causing stalls.
func (db *DB) GetConversationLite(id string) (*Conversation, error) {
	var conv Conversation
	var createdAt, updatedAt string
	var pinned int

	var projectID sql.NullString
	err := db.QueryRow(
		"SELECT id, title, pinned, created_at, updated_at, project_id FROM conversations WHERE id = ?",
		id,
	).Scan(&conv.ID, &conv.Title, &pinned, &createdAt, &updatedAt, &projectID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("conversation does not exist")
		}
		return nil, fmt.Errorf("failed to query conversation: %w", err)
	}
	if projectID.Valid {
		conv.ProjectID = strings.TrimSpace(projectID.String)
	}

	// Try parsing multiple time formats.
	var err1, err2 error
	conv.CreatedAt, err1 = time.Parse("2006-01-02 15:04:05.999999999-07:00", createdAt)
	if err1 != nil {
		conv.CreatedAt, err1 = time.Parse("2006-01-02 15:04:05", createdAt)
	}
	if err1 != nil {
		conv.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	}

	conv.UpdatedAt, err2 = time.Parse("2006-01-02 15:04:05.999999999-07:00", updatedAt)
	if err2 != nil {
		conv.UpdatedAt, err2 = time.Parse("2006-01-02 15:04:05", updatedAt)
	}
	if err2 != nil {
		conv.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	}

	conv.Pinned = pinned != 0

	// Load messages without loading process_details.
	messages, err := db.GetMessages(id)
	if err != nil {
		return nil, fmt.Errorf("failed to load messages: %w", err)
	}
	conv.Messages = messages
	return &conv, nil
}

// CountConversations counts conversations.
func (db *DB) CountConversations(search string) (int, error) {
	var count int
	var err error
	if search != "" {
		searchPattern := "%" + search + "%"
		err = db.QueryRow(
			`SELECT COUNT(*) FROM conversations c
			 WHERE c.title LIKE ?
			    OR EXISTS (SELECT 1 FROM messages m WHERE m.conversation_id = c.id AND m.content LIKE ?)`,
			searchPattern, searchPattern,
		).Scan(&count)
	} else {
		err = db.QueryRow(`SELECT COUNT(*) FROM conversations`).Scan(&count)
	}
	if err != nil {
		return 0, fmt.Errorf("failed to count conversations: %w", err)
	}
	return count, nil
}

// ListConversations lists all conversations
func (db *DB) ListConversations(limit, offset int, search string) ([]*Conversation, error) {
	var rows *sql.Rows
	var err error

	if search != "" {
		// Use an EXISTS subquery instead of LEFT JOIN plus DISTINCT to avoid Cartesian products on large tables.
		searchPattern := "%" + search + "%"
		rows, err = db.Query(
			`SELECT c.id, c.title, COALESCE(c.pinned, 0), c.created_at, c.updated_at, c.project_id
			 FROM conversations c
			 WHERE c.title LIKE ?
			    OR EXISTS (SELECT 1 FROM messages m WHERE m.conversation_id = c.id AND m.content LIKE ?)
			 ORDER BY c.updated_at DESC
			 LIMIT ? OFFSET ?`,
			searchPattern, searchPattern, limit, offset,
		)
	} else {
		rows, err = db.Query(
			"SELECT id, title, COALESCE(pinned, 0), created_at, updated_at, project_id FROM conversations ORDER BY updated_at DESC LIMIT ? OFFSET ?",
			limit, offset,
		)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to query conversation list: %w", err)
	}
	defer rows.Close()

	var conversations []*Conversation
	for rows.Next() {
		var conv Conversation
		var createdAt, updatedAt string
		var pinned int
		var projectID sql.NullString

		if err := rows.Scan(&conv.ID, &conv.Title, &pinned, &createdAt, &updatedAt, &projectID); err != nil {
			return nil, fmt.Errorf("failed to scan conversation: %w", err)
		}
		if projectID.Valid {
			conv.ProjectID = strings.TrimSpace(projectID.String)
		}

		// Try parsing multiple time formats.
		var err1, err2 error
		conv.CreatedAt, err1 = time.Parse("2006-01-02 15:04:05.999999999-07:00", createdAt)
		if err1 != nil {
			conv.CreatedAt, err1 = time.Parse("2006-01-02 15:04:05", createdAt)
		}
		if err1 != nil {
			conv.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		}

		conv.UpdatedAt, err2 = time.Parse("2006-01-02 15:04:05.999999999-07:00", updatedAt)
		if err2 != nil {
			conv.UpdatedAt, err2 = time.Parse("2006-01-02 15:04:05", updatedAt)
		}
		if err2 != nil {
			conv.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		}

		conv.Pinned = pinned != 0

		conversations = append(conversations, &conv)
	}

	return conversations, nil
}

const ungroupedConversationsSQL = `
	FROM conversations c
	WHERE NOT EXISTS (
		SELECT 1 FROM conversation_group_mappings cgm WHERE cgm.conversation_id = c.id
	)`

// CountUngroupedConversations counts conversations not in any group.
func (db *DB) CountUngroupedConversations() (int, error) {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) ` + ungroupedConversationsSQL).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count ungrouped conversations: %w", err)
	}
	return count, nil
}

// ListUngroupedConversations lists conversations not in any group (recent conversation sidebar).
func (db *DB) ListUngroupedConversations(limit, offset int) ([]*Conversation, error) {
	rows, err := db.Query(
		`SELECT c.id, c.title, COALESCE(c.pinned, 0), c.created_at, c.updated_at, c.project_id `+
			ungroupedConversationsSQL+`
		 ORDER BY c.updated_at DESC
		 LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query ungrouped conversations: %w", err)
	}
	defer rows.Close()

	var conversations []*Conversation
	for rows.Next() {
		var conv Conversation
		var createdAt, updatedAt string
		var pinned int
		var projectID sql.NullString

		if err := rows.Scan(&conv.ID, &conv.Title, &pinned, &createdAt, &updatedAt, &projectID); err != nil {
			return nil, fmt.Errorf("failed to scan conversation: %w", err)
		}
		if projectID.Valid {
			conv.ProjectID = strings.TrimSpace(projectID.String)
		}

		var err1, err2 error
		conv.CreatedAt, err1 = time.Parse("2006-01-02 15:04:05.999999999-07:00", createdAt)
		if err1 != nil {
			conv.CreatedAt, err1 = time.Parse("2006-01-02 15:04:05", createdAt)
		}
		if err1 != nil {
			conv.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		}

		conv.UpdatedAt, err2 = time.Parse("2006-01-02 15:04:05.999999999-07:00", updatedAt)
		if err2 != nil {
			conv.UpdatedAt, err2 = time.Parse("2006-01-02 15:04:05", updatedAt)
		}
		if err2 != nil {
			conv.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		}

		conv.Pinned = pinned != 0
		conversations = append(conversations, &conv)
	}

	return conversations, rows.Err()
}

// UpdateConversationTitle updates a conversation title
func (db *DB) UpdateConversationTitle(id, title string) error {
	// Note: do not update updated_at because renaming should not change the conversation update time.
	_, err := db.Exec(
		"UPDATE conversations SET title = ? WHERE id = ?",
		title, id,
	)
	if err != nil {
		return fmt.Errorf("failed to update conversation title: %w", err)
	}
	return nil
}

// UpdateConversationTime updates conversation time
func (db *DB) UpdateConversationTime(id string) error {
	_, err := db.Exec(
		"UPDATE conversations SET updated_at = ? WHERE id = ?",
		time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("failed to update conversation time: %w", err)
	}
	return nil
}

// DeleteConversation deletes a conversation and all related data.
// Due to ON DELETE CASCADE foreign keys, deleting a conversation also deletes:
// - messages
// - process_details
// - attack_chain_nodes
// - attack_chain_edges
// - conversation_group_mappings
// Vulnerability records are preserved: vulnerabilities.conversation_id uses ON DELETE SET NULL, only unbinding from the conversation.
// Note: knowledge_retrieval_logs are explicitly cleaned up before deletion.
func (db *DB) DeleteConversation(id string) error {
	// Backfill conversation title into vulnerability conversation_tag before deletion, for traceability.
	_, err := db.Exec(`
		UPDATE vulnerabilities
		SET conversation_tag = COALESCE(NULLIF(TRIM(conversation_tag), ''), (SELECT title FROM conversations WHERE id = ?))
		WHERE conversation_id = ?
	`, id, id)
	if err != nil {
		db.logger.Warn("failed to update vulnerability conversation tag", zap.String("conversationId", id), zap.Error(err))
	}

	// Explicitly delete knowledge retrieval logs (FK is SET NULL, but we clean them up fully).
	_, err = db.Exec("DELETE FROM knowledge_retrieval_logs WHERE conversation_id = ?", id)
	if err != nil {
		db.logger.Warn("failed to delete knowledge retrieval logs", zap.String("conversationId", id), zap.Error(err))
		// Do not return an error; continue deleting the conversation.
	}

	// Delete the conversation; CASCADE foreign keys delete other related data automatically.
	_, err = db.Exec("DELETE FROM conversations WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete conversation: %w", err)
	}
	db.removeConversationScopedDirs(id)

	db.logger.Info("conversation deleted (vulnerability records preserved)", zap.String("conversationId", id))
	return nil
}

func sanitizeConversationPathSegment(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "default"
	}
	s = strings.ReplaceAll(s, string(filepath.Separator), "-")
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, "\\", "-")
	s = strings.ReplaceAll(s, "..", "__")
	if len(s) > 180 {
		s = s[:180]
	}
	return s
}

func (db *DB) removeConversationScopedDir(base, conversationID, label string) {
	base = strings.TrimSpace(base)
	if base == "" {
		return
	}
	dir := filepath.Join(base, sanitizeConversationPathSegment(conversationID))
	if rmErr := os.RemoveAll(dir); rmErr != nil {
		if db.logger != nil {
			db.logger.Warn("failed to remove conversation scoped directory",
				zap.String("conversationId", conversationID),
				zap.String("kind", label),
				zap.String("dir", dir),
				zap.Error(rmErr))
		}
	}
}

func (db *DB) removeConversationScopedDirs(conversationID string) {
	// summarization transcript, reduction files, etc.
	db.removeConversationScopedDir(db.conversationArtifactsDir, conversationID, "conversation_artifacts")
	// Eino plantask JSON boards (skills_dir/.eino/plantask/<id>/).
	db.removeConversationScopedDir(db.einoPlantaskBaseDir, conversationID, "plantask")
	// Eino ADK runner checkpoints (checkpoint_dir/<id>/).
	db.removeConversationScopedDir(db.einoCheckpointBaseDir, conversationID, "eino_checkpoint")
}

// SaveAgentTrace saves the last agent message trace and assistant output summary.
// SQLite column names remain last_react_input / last_react_output for compatibility with historical database schemas; semantically this is an all-mode agent trace, not only ReAct.
func (db *DB) SaveAgentTrace(conversationID, traceInputJSON, assistantOutput string) error {
	_, err := db.Exec(
		"UPDATE conversations SET last_react_input = ?, last_react_output = ?, updated_at = ? WHERE id = ?",
		traceInputJSON, assistantOutput, time.Now(), conversationID,
	)
	if err != nil {
		return fmt.Errorf("failed to save agent trace: %w", err)
	}
	return nil
}

// GetAgentTrace reads the agent trace saved in conversations (last_react_* columns).
func (db *DB) GetAgentTrace(conversationID string) (traceInputJSON, assistantOutput string, err error) {
	var input, output sql.NullString
	err = db.QueryRow(
		"SELECT last_react_input, last_react_output FROM conversations WHERE id = ?",
		conversationID,
	).Scan(&input, &output)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", "", fmt.Errorf("conversation does not exist")
		}
		return "", "", fmt.Errorf("failed to get agent trace: %w", err)
	}

	if input.Valid {
		traceInputJSON = input.String
	}
	if output.Valid {
		assistantOutput = output.String
	}

	return traceInputJSON, assistantOutput, nil
}

// ConversationHasToolProcessDetails reports whether persisted tool calls/results exist for a conversation; used for attack-chain decisions when MCP execution IDs were not aggregated in multi-agent and similar scenarios.
func (db *DB) ConversationHasToolProcessDetails(conversationID string) (bool, error) {
	var n int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM process_details WHERE conversation_id = ? AND event_type IN ('tool_call', 'tool_result')`,
		conversationID,
	).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("failed to query process details: %w", err)
	}
	return n > 0, nil
}

// AddMessage adds a message
func (db *DB) AddMessage(conversationID, role, content string, mcpExecutionIDs []string) (*Message, error) {
	id := uuid.New().String()
	now := time.Now()

	var mcpIDsJSON string
	if len(mcpExecutionIDs) > 0 {
		jsonData, err := json.Marshal(mcpExecutionIDs)
		if err != nil {
			db.logger.Warn("failed to serialize MCP execution IDs", zap.Error(err))
		} else {
			mcpIDsJSON = string(jsonData)
		}
	}

	_, err := db.Exec(
		"INSERT INTO messages (id, conversation_id, role, content, reasoning_content, mcp_execution_ids, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		id, conversationID, role, content, "", mcpIDsJSON, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to add message: %w", err)
	}

	// updates conversation time
	if err := db.UpdateConversationTime(conversationID); err != nil {
		db.logger.Warn("failed to update conversation time", zap.Error(err))
	}

	message := &Message{
		ID:              id,
		ConversationID:  conversationID,
		Role:            role,
		Content:         content,
		MCPExecutionIDs: mcpExecutionIDs,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	return message, nil
}

// UpdateAssistantMessageFinalize updates the final assistant message state: content, MCP IDs, and aggregated reasoning text for fallback replay when no trace is available.
func (db *DB) UpdateAssistantMessageFinalize(messageID, content string, mcpExecutionIDs []string, reasoningContent string) error {
	var mcpIDsJSON string
	if len(mcpExecutionIDs) > 0 {
		jsonData, err := json.Marshal(mcpExecutionIDs)
		if err != nil {
			return fmt.Errorf("failed to serialize MCP execution IDs: %w", err)
		}
		mcpIDsJSON = string(jsonData)
	}
	_, err := db.Exec(
		"UPDATE messages SET content = ?, mcp_execution_ids = ?, reasoning_content = ?, updated_at = ? WHERE id = ?",
		content, mcpIDsJSON, strings.TrimSpace(reasoningContent), time.Now(), messageID,
	)
	if err != nil {
		return fmt.Errorf("failed to update assistant message: %w", err)
	}
	return nil
}

// GetMessages gets all messages for a conversation
func (db *DB) GetMessages(conversationID string) ([]Message, error) {
	rows, err := db.Query(
		"SELECT id, conversation_id, role, content, reasoning_content, mcp_execution_ids, created_at, updated_at FROM messages WHERE conversation_id = ? ORDER BY created_at ASC, rowid ASC",
		conversationID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		var reasoning sql.NullString
		var mcpIDsJSON sql.NullString
		var createdAt string
		var updatedAt sql.NullString

		if err := rows.Scan(&msg.ID, &msg.ConversationID, &msg.Role, &msg.Content, &reasoning, &mcpIDsJSON, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}
		if reasoning.Valid {
			msg.ReasoningContent = reasoning.String
		}

		// Try parsing multiple time formats.
		var err error
		msg.CreatedAt, err = time.Parse("2006-01-02 15:04:05.999999999-07:00", createdAt)
		if err != nil {
			msg.CreatedAt, err = time.Parse("2006-01-02 15:04:05", createdAt)
		}
		if err != nil {
			msg.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		}

		// updated_at compatibility for old databases: fall back to created_at when the column is missing or empty.
		if updatedAt.Valid && strings.TrimSpace(updatedAt.String) != "" {
			msg.UpdatedAt, err = time.Parse("2006-01-02 15:04:05.999999999-07:00", updatedAt.String)
			if err != nil {
				msg.UpdatedAt, err = time.Parse("2006-01-02 15:04:05", updatedAt.String)
			}
			if err != nil {
				msg.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt.String)
			}
		}
		if msg.UpdatedAt.IsZero() {
			msg.UpdatedAt = msg.CreatedAt
		}

		// Parse MCP execution IDs
		if mcpIDsJSON.Valid && mcpIDsJSON.String != "" {
			if err := json.Unmarshal([]byte(mcpIDsJSON.String), &msg.MCPExecutionIDs); err != nil {
				db.logger.Warn("failed to parse MCP execution IDs", zap.Error(err))
			}
		}

		messages = append(messages, msg)
	}

	return messages, nil
}

// turnSliceRange locates the [start, end) index range for one conversation turn in msgs by any message ID; msgs must already be sorted ascending by time, matching GetMessages.
// One turn starts at a user message and ends before the next user message, including all intervening assistant messages.
func turnSliceRange(msgs []Message, anchorID string) (start, end int, err error) {
	idx := -1
	for i := range msgs {
		if msgs[i].ID == anchorID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return 0, 0, fmt.Errorf("message not found")
	}
	start = idx
	for start > 0 && msgs[start].Role != "user" {
		start--
	}
	if start < len(msgs) && msgs[start].Role != "user" {
		start = 0
	}
	end = len(msgs)
	for i := start + 1; i < len(msgs); i++ {
		if msgs[i].Role == "user" {
			end = i
			break
		}
	}
	return start, end, nil
}

// DeleteConversationTurn deletes every message in the turn containing the anchor, including the user prompt and assistant replies, and clears last_react_* to avoid inconsistencies with the message table.
func (db *DB) DeleteConversationTurn(conversationID, anchorMessageID string) (deletedIDs []string, err error) {
	msgs, err := db.GetMessages(conversationID)
	if err != nil {
		return nil, err
	}
	start, end, err := turnSliceRange(msgs, anchorMessageID)
	if err != nil {
		return nil, err
	}
	if start >= end {
		return nil, fmt.Errorf("empty turn range")
	}
	deletedIDs = make([]string, 0, end-start)
	for i := start; i < end; i++ {
		deletedIDs = append(deletedIDs, msgs[i].ID)
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	ph := strings.Repeat("?,", len(deletedIDs))
	ph = ph[:len(ph)-1]
	args := make([]interface{}, 0, 1+len(deletedIDs))
	args = append(args, conversationID)
	for _, id := range deletedIDs {
		args = append(args, id)
	}
	res, err := tx.Exec(
		"DELETE FROM messages WHERE conversation_id = ? AND id IN ("+ph+")",
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("delete messages: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if int(n) != len(deletedIDs) {
		return nil, fmt.Errorf("deleted count mismatch")
	}

	_, err = tx.Exec(
		`UPDATE conversations SET last_react_input = NULL, last_react_output = NULL, updated_at = ? WHERE id = ?`,
		time.Now(), conversationID,
	)
	if err != nil {
		return nil, fmt.Errorf("clear react data: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	db.logger.Info("conversation turn deleted",
		zap.String("conversationId", conversationID),
		zap.Strings("deletedMessageIds", deletedIDs),
		zap.Int("count", len(deletedIDs)),
	)
	return deletedIDs, nil
}

// ProcessDetail process detail event
type ProcessDetail struct {
	ID             string    `json:"id"`
	MessageID      string    `json:"messageId"`
	ConversationID string    `json:"conversationId"`
	EventType      string    `json:"eventType"` // iteration, thinking, reasoning_chain, tool_calls_detected, tool_call, tool_result, progress, error
	Message        string    `json:"message"`
	Data           string    `json:"data"` // JSON-formatted data
	CreatedAt      time.Time `json:"createdAt"`
}

// AddProcessDetail adds a process detail event
func (db *DB) AddProcessDetail(messageID, conversationID, eventType, message string, data interface{}) error {
	id := uuid.New().String()

	var dataJSON string
	if data != nil {
		jsonData, err := json.Marshal(data)
		if err != nil {
			db.logger.Warn("failed to serialize process detail data", zap.Error(err))
		} else {
			dataJSON = string(jsonData)
		}
	}

	_, err := db.Exec(
		"INSERT INTO process_details (id, message_id, conversation_id, event_type, message, data, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		id, messageID, conversationID, eventType, message, dataJSON, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("failed to add process detail: %w", err)
	}

	return nil
}

// GetProcessDetails gets process details for a message
func (db *DB) GetProcessDetails(messageID string) ([]ProcessDetail, error) {
	rows, err := db.Query(
		"SELECT id, message_id, conversation_id, event_type, message, data, created_at FROM process_details WHERE message_id = ? ORDER BY created_at ASC, rowid ASC",
		messageID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query process details: %w", err)
	}
	defer rows.Close()

	var details []ProcessDetail
	for rows.Next() {
		var detail ProcessDetail
		var createdAt string

		if err := rows.Scan(&detail.ID, &detail.MessageID, &detail.ConversationID, &detail.EventType, &detail.Message, &detail.Data, &createdAt); err != nil {
			return nil, fmt.Errorf("failed to scan process detail: %w", err)
		}

		// Try parsing multiple time formats.
		var err error
		detail.CreatedAt, err = time.Parse("2006-01-02 15:04:05.999999999-07:00", createdAt)
		if err != nil {
			detail.CreatedAt, err = time.Parse("2006-01-02 15:04:05", createdAt)
		}
		if err != nil {
			detail.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		}

		details = append(details, detail)
	}

	return details, nil
}

// GetProcessDetailsByConversation gets all process details for a conversation grouped by message
func (db *DB) GetProcessDetailsByConversation(conversationID string) (map[string][]ProcessDetail, error) {
	rows, err := db.Query(
		"SELECT id, message_id, conversation_id, event_type, message, data, created_at FROM process_details WHERE conversation_id = ? ORDER BY created_at ASC, rowid ASC",
		conversationID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query process details: %w", err)
	}
	defer rows.Close()

	detailsMap := make(map[string][]ProcessDetail)
	for rows.Next() {
		var detail ProcessDetail
		var createdAt string

		if err := rows.Scan(&detail.ID, &detail.MessageID, &detail.ConversationID, &detail.EventType, &detail.Message, &detail.Data, &createdAt); err != nil {
			return nil, fmt.Errorf("failed to scan process detail: %w", err)
		}

		// Try parsing multiple time formats.
		var err error
		detail.CreatedAt, err = time.Parse("2006-01-02 15:04:05.999999999-07:00", createdAt)
		if err != nil {
			detail.CreatedAt, err = time.Parse("2006-01-02 15:04:05", createdAt)
		}
		if err != nil {
			detail.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		}

		detailsMap[detail.MessageID] = append(detailsMap[detail.MessageID], detail)
	}

	return detailsMap, nil
}
