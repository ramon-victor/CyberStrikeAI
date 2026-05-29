package handler

import (
	"net/http"
	"strconv"
	"strings"

	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/project"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ProjectHandler project management handler.
type ProjectHandler struct {
	db     *database.DB
	logger *zap.Logger
}

// NewProjectHandler creates a project management handler.
func NewProjectHandler(db *database.DB, logger *zap.Logger) *ProjectHandler {
	return &ProjectHandler{db: db, logger: logger}
}

type createProjectRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	ScopeJSON   string `json:"scope_json"`
	Status      string `json:"status"`
}

// updateProjectRequest partial update: omitted fields are unchanged; pass null or "" to clear string fields.
type updateProjectRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	ScopeJSON   *string `json:"scope_json"`
	Status      *string `json:"status"`
	Pinned      *bool   `json:"pinned"`
}

// CreateProject POST /api/projects
func (h *ProjectHandler) CreateProject(c *gin.Context) {
	var req createProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p := &database.Project{
		Name:        strings.TrimSpace(req.Name),
		Description: req.Description,
		ScopeJSON:   req.ScopeJSON,
		Status:      strings.TrimSpace(req.Status),
	}
	created, err := h.db.CreateProject(p)
	if err != nil {
		h.logger.Error("failed to create project", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, created)
}

// ListProjects GET /api/projects
func (h *ProjectHandler) ListProjects(c *gin.Context) {
	status := c.Query("status")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	offset, _ := strconv.Atoi(c.Query("offset"))
	list, err := h.db.ListProjects(status, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if list == nil {
		list = []*database.Project{}
	}
	c.JSON(http.StatusOK, list)
}

// GetProjectStats GET /api/projects/:id/stats
func (h *ProjectHandler) GetProjectStats(c *gin.Context) {
	stats, err := project.GetProjectStats(h.db, c.Param("id"))
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			c.JSON(http.StatusNotFound, gin.H{"error": "project does not exist"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

// ListProjectConversations GET /api/projects/:id/conversations
func (h *ProjectHandler) ListProjectConversations(c *gin.Context) {
	projectID := c.Param("id")
	if _, err := h.db.GetProject(projectID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project does not exist"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	offset, _ := strconv.Atoi(c.Query("offset"))
	list, err := h.db.ListConversationsByProjectID(projectID, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if list == nil {
		list = []*database.Conversation{}
	}
	total, _ := h.db.CountConversationsByProjectID(projectID)
	c.JSON(http.StatusOK, gin.H{
		"conversations": list,
		"total":         total,
		"limit":         limit,
		"offset":        offset,
	})
}

// GetProject GET /api/projects/:id
func (h *ProjectHandler) GetProject(c *gin.Context) {
	p, err := h.db.GetProject(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project does not exist"})
		return
	}
	c.JSON(http.StatusOK, p)
}

// UpdateProject PUT /api/projects/:id
func (h *ProjectHandler) UpdateProject(c *gin.Context) {
	id := c.Param("id")
	p, err := h.db.GetProject(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project does not exist"})
		return
	}
	var req updateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Name != nil {
		if s := strings.TrimSpace(*req.Name); s != "" {
			p.Name = s
		}
	}
	if req.Description != nil {
		p.Description = *req.Description
	}
	if req.ScopeJSON != nil {
		p.ScopeJSON = *req.ScopeJSON
	}
	if req.Status != nil {
		if s := strings.TrimSpace(*req.Status); s != "" {
			p.Status = s
		}
	}
	if req.Pinned != nil {
		p.Pinned = *req.Pinned
	}
	if err := h.db.UpdateProject(p); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, p)
}

// DeleteProject DELETE /api/projects/:id
func (h *ProjectHandler) DeleteProject(c *gin.Context) {
	if err := h.db.DeleteProject(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

type upsertFactRequest struct {
	FactKey                string `json:"fact_key" binding:"required"`
	Category               string `json:"category"`
	Summary                string `json:"summary" binding:"required"`
	Body                   string `json:"body"`
	Confidence             string `json:"confidence"`
	Pinned                 bool   `json:"pinned"`
	RelatedVulnerabilityID string `json:"related_vulnerability_id"`
}

// updateFactRequest partial fact update; omitted pointer fields are unchanged, body "" can clear the value (still uses merge logic; see Upsert).
type updateFactRequest struct {
	FactKey                *string `json:"fact_key"`
	Category               *string `json:"category"`
	Summary                *string `json:"summary"`
	Body                   *string `json:"body"`
	Confidence             *string `json:"confidence"`
	Pinned                 *bool   `json:"pinned"`
	RelatedVulnerabilityID *string `json:"related_vulnerability_id"`
	ClearBody              bool    `json:"clear_body"`
}

// ListFacts GET /api/projects/:id/facts (fact_key query parameter can retrieve single-item details)
func (h *ProjectHandler) ListFacts(c *gin.Context) {
	projectID := c.Param("id")
	if key := strings.TrimSpace(c.Query("fact_key")); key != "" {
		f, err := h.db.GetProjectFactByKey(projectID, key)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, f)
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	offset, _ := strconv.Atoi(c.Query("offset"))
	filter := database.ProjectFactListFilter{
		Category:               c.Query("category"),
		Confidence:             c.Query("confidence"),
		Search:                 c.Query("search"),
		RelatedVulnerabilityID: c.Query("related_vulnerability_id"),
	}
	if c.Query("exclude_deprecated") == "1" || c.Query("exclude_deprecated") == "true" {
		filter.ExcludeDeprecated = true
	}
	list, err := h.db.ListProjectFacts(projectID, filter, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if list == nil {
		list = []*database.ProjectFact{}
	}
	if sparseOnly := c.Query("sparse_only"); sparseOnly == "1" || sparseOnly == "true" {
		filtered := make([]*database.ProjectFact, 0, len(list))
		for _, f := range list {
			if project.IsSparseFactBody(f.Category, f.FactKey, f.Body) {
				filtered = append(filtered, f)
			}
		}
		list = filtered
	}
	c.JSON(http.StatusOK, list)
}

// GetFactPreviousVersion GET /api/projects/:id/facts/:factId/previous-version
func (h *ProjectHandler) GetFactPreviousVersion(c *gin.Context) {
	existing, err := h.db.GetProjectFact(c.Param("factId"))
	if err != nil || existing.ProjectID != c.Param("id") {
		c.JSON(http.StatusNotFound, gin.H{"error": "fact does not exist"})
		return
	}
	if strings.TrimSpace(existing.SupersedesFactID) == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "no previous version"})
		return
	}
	v, err := h.db.GetProjectFactVersion(existing.SupersedesFactID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, v)
}

// ListFactVersions GET /api/projects/:id/facts/:factId/versions
func (h *ProjectHandler) ListFactVersions(c *gin.Context) {
	existing, err := h.db.GetProjectFact(c.Param("factId"))
	if err != nil || existing.ProjectID != c.Param("id") {
		c.JSON(http.StatusNotFound, gin.H{"error": "fact does not exist"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	list, err := h.db.ListProjectFactVersions(existing.ID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if list == nil {
		list = []*database.ProjectFactVersion{}
	}
	c.JSON(http.StatusOK, list)
}

// CreateFact POST /api/projects/:id/facts
func (h *ProjectHandler) CreateFact(c *gin.Context) {
	var req upsertFactRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	f := &database.ProjectFact{
		ProjectID:              c.Param("id"),
		FactKey:                req.FactKey,
		Category:               req.Category,
		Summary:                req.Summary,
		Body:                   req.Body,
		Confidence:             req.Confidence,
		Pinned:                 req.Pinned,
		RelatedVulnerabilityID: req.RelatedVulnerabilityID,
	}
	created, err := h.db.UpsertProjectFact(f)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, created)
}

// UpdateFact PUT /api/projects/:id/facts/:factId
func (h *ProjectHandler) UpdateFact(c *gin.Context) {
	existing, err := h.db.GetProjectFact(c.Param("factId"))
	if err != nil || existing.ProjectID != c.Param("id") {
		c.JSON(http.StatusNotFound, gin.H{"error": "fact does not exist"})
		return
	}
	var req updateFactRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.FactKey != nil {
		if k := strings.TrimSpace(*req.FactKey); k != "" {
			existing.FactKey = k
		}
	}
	if req.Category != nil && strings.TrimSpace(*req.Category) != "" {
		existing.Category = *req.Category
	}
	if req.Summary != nil && strings.TrimSpace(*req.Summary) != "" {
		existing.Summary = *req.Summary
	}
	if req.ClearBody {
		existing.Body = ""
	} else if req.Body != nil {
		existing.Body = *req.Body
	}
	if req.Confidence != nil && strings.TrimSpace(*req.Confidence) != "" {
		existing.Confidence = *req.Confidence
	}
	if req.Pinned != nil {
		existing.Pinned = *req.Pinned
	}
	if req.RelatedVulnerabilityID != nil {
		existing.RelatedVulnerabilityID = *req.RelatedVulnerabilityID
	}
	updated, err := h.db.UpsertProjectFact(existing)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// DeleteFact DELETE /api/projects/:id/facts/:factId
func (h *ProjectHandler) DeleteFact(c *gin.Context) {
	existing, err := h.db.GetProjectFact(c.Param("factId"))
	if err != nil || existing.ProjectID != c.Param("id") {
		c.JSON(http.StatusNotFound, gin.H{"error": "fact does not exist"})
		return
	}
	if err := h.db.DeleteProjectFact(existing.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

type deprecateFactRequest struct {
	FactKey string `json:"fact_key" binding:"required"`
}

// DeprecateFact POST /api/projects/:id/facts/deprecate
func (h *ProjectHandler) DeprecateFact(c *gin.Context) {
	var req deprecateFactRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.db.DeprecateProjectFact(c.Param("id"), req.FactKey); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

type restoreFactRequest struct {
	FactKey    string `json:"fact_key" binding:"required"`
	Confidence string `json:"confidence"` // optional: confirmed | tentative, defaults to tentative
}

// RestoreFact POST /api/projects/:id/facts/restore
func (h *ProjectHandler) RestoreFact(c *gin.Context) {
	var req restoreFactRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.db.RestoreProjectFact(c.Param("id"), req.FactKey, req.Confidence); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
