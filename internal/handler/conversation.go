package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"cyberstrike-ai/internal/audit"
	"cyberstrike-ai/internal/database"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ConversationHandler Conversation handler
type ConversationHandler struct {
	db     *database.DB
	logger *zap.Logger
	audit  *audit.Service
}

// SetAudit wires platform audit logging.
func (h *ConversationHandler) SetAudit(s *audit.Service) {
	h.audit = s
}

// NewConversationHandler creates a new conversation handler
func NewConversationHandler(db *database.DB, logger *zap.Logger) *ConversationHandler {
	return &ConversationHandler{
		db:     db,
		logger: logger,
	}
}

// CreateConversationRequest create conversation request
type CreateConversationRequest struct {
	Title     string `json:"title"`
	ProjectID string `json:"projectId,omitempty"`
}

// SetConversationProjectRequest set conversation project
type SetConversationProjectRequest struct {
	ProjectID string `json:"projectId"` // empty string means unbind
}

// CreateConversation Create new conversation
func (h *ConversationHandler) CreateConversation(c *gin.Context) {
	var req CreateConversationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	title := req.Title
	if title == "" {
		title = "New conversation"
	}

	meta := audit.ConversationCreateMetaFromGin(c, "api")
	meta.ProjectID = strings.TrimSpace(req.ProjectID)
	conv, err := h.db.CreateConversation(title, meta)
	if err != nil {
		h.logger.Error("Failed to create conversation", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, conv)
}

// SetConversationProject Set or clear the project bound to a conversation
func (h *ConversationHandler) SetConversationProject(c *gin.Context) {
	id := c.Param("id")
	var req SetConversationProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if _, err := h.db.GetConversation(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Conversation does not exist"})
		return
	}
	if err := h.db.SetConversationProjectID(id, req.ProjectID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "projectId": strings.TrimSpace(req.ProjectID)})
}

// ListConversations List conversations
func (h *ConversationHandler) ListConversations(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "50")
	offsetStr := c.DefaultQuery("offset", "0")
	search := c.Query("search") // Get search parameter

	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)

	if limit <= 0 || limit > 100 {
		limit = 50
	}

	conversations, err := h.db.ListConversations(limit, offset, search)
	if err != nil {
		h.logger.Error("Failed to get conversation list", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, conversations)
}

// GetConversation Get conversation
func (h *ConversationHandler) GetConversation(c *gin.Context) {
	id := c.Param("id")

	// Lightweight load by default; fetch details on demand only when the user expands them
	// include_process_details=1/true returns full processDetails for backward compatibility
	includeStr := c.DefaultQuery("include_process_details", "0")
	include := includeStr == "1" || includeStr == "true" || includeStr == "yes"

	var (
		conv *database.Conversation
		err  error
	)
	if include {
		conv, err = h.db.GetConversation(id)
	} else {
		conv, err = h.db.GetConversationLite(id)
	}
	if err != nil {
		h.logger.Error("Failed to get conversation", zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{"error": "Conversation not found"})
		return
	}

	c.JSON(http.StatusOK, conv)
}

// GetMessageProcessDetails Get process details for the specified message on demand
func (h *ConversationHandler) GetMessageProcessDetails(c *gin.Context) {
	messageID := c.Param("id")
	if messageID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "message id required"})
		return
	}

	details, err := h.db.GetProcessDetails(messageID)
	if err != nil {
		h.logger.Error("Failed to get process details", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	details = database.DedupeConsecutiveProcessDetails(details)

	// Convert to the JSON structure expected by the frontend, matching processDetails in GetConversation
	out := make([]map[string]interface{}, 0, len(details))
	for _, d := range details {
		var data interface{}
		if d.Data != "" {
			if err := json.Unmarshal([]byte(d.Data), &data); err != nil {
				h.logger.Warn("Failed to parse process detail data", zap.Error(err))
			}
		}
		out = append(out, map[string]interface{}{
			"id":             d.ID,
			"messageId":      d.MessageID,
			"conversationId": d.ConversationID,
			"eventType":      d.EventType,
			"message":        d.Message,
			"data":           data,
			"createdAt":      d.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{"processDetails": out})
}

// UpdateConversationRequest update conversation request
type UpdateConversationRequest struct {
	Title string `json:"title"`
}

// UpdateConversation Update conversation
func (h *ConversationHandler) UpdateConversation(c *gin.Context) {
	id := c.Param("id")

	var req UpdateConversationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Title == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Title cannot be empty"})
		return
	}

	if err := h.db.UpdateConversationTitle(id, req.Title); err != nil {
		h.logger.Error("Failed to update conversation", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Return updated conversation
	conv, err := h.db.GetConversation(id)
	if err != nil {
		h.logger.Error("Failed to get updated conversation", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, conv)
}

// DeleteConversation Delete conversation
func (h *ConversationHandler) DeleteConversation(c *gin.Context) {
	id := c.Param("id")

	if err := h.db.DeleteConversation(id); err != nil {
		h.logger.Error("Failed to delete conversation", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if h.audit != nil {
		h.audit.Record(c, audit.Entry{
			Category:     "conversation",
			Action:       "delete",
			Result:       "success",
			ResourceType: "conversation",
			ResourceID:   id,
			Message:      "Deleted conversation",
		})
	}

	c.JSON(http.StatusOK, gin.H{"message": "Deleted successfully"})
}

// DeleteTurnRequest delete one conversation turn (POST /api/conversations/:id/delete-turn)
type DeleteTurnRequest struct {
	MessageID string `json:"messageId"`
}

// DeleteConversationTurn deletes the turn containing the anchor message, from that turn's user message to before the next user message, and clears last_react_*.
func (h *ConversationHandler) DeleteConversationTurn(c *gin.Context) {
	conversationID := c.Param("id")
	if conversationID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "conversation id required"})
		return
	}

	var req DeleteTurnRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.MessageID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "messageId required"})
		return
	}

	if _, err := h.db.GetConversation(conversationID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Conversation not found"})
		return
	}

	deletedIDs, err := h.db.DeleteConversationTurn(conversationID, req.MessageID)
	if err != nil {
		h.logger.Warn("Failed to delete conversation turn",
			zap.String("conversationId", conversationID),
			zap.String("messageId", req.MessageID),
			zap.Error(err),
		)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if h.audit != nil {
		h.audit.RecordOK(c, "conversation", "delete_turn", "Delete conversation turn", "conversation", conversationID, map[string]interface{}{
			"message_id": req.MessageID,
			"deleted":    len(deletedIDs),
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"deletedMessageIds": deletedIDs,
		"message":           "ok",
	})
}
