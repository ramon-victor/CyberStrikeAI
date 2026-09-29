package handler

import (
	"errors"
	"net/http"
	"strconv"

	"cyberstrike-ai/internal/audit"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/storage"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// StorageHandler serves the runtime storage usage and garbage cleanup APIs.
type StorageHandler struct {
	cleaner *storage.Cleaner
	cfg     *config.Config
	audit   *audit.Service
	logger  *zap.Logger
}

// NewStorageHandler creates the storage cleanup handler.
func NewStorageHandler(cleaner *storage.Cleaner, cfg *config.Config, logger *zap.Logger) *StorageHandler {
	return &StorageHandler{cleaner: cleaner, cfg: cfg, logger: logger}
}

// SetAudit wires platform audit logging.
func (h *StorageHandler) SetAudit(s *audit.Service) {
	if h != nil {
		h.audit = s
	}
}

// storageCleanupRequest is the body of POST /api/storage/cleanup.
type storageCleanupRequest struct {
	// DryRun defaults to true when omitted: report only, no deletion. A real deletion must explicitly pass false.
	DryRun *bool `json:"dry_run"`
	// Confirm: execution is refused even with dry_run=false when it is false.
	// Disk deletion is irreversible, so confirmation must be an explicit API-level action, not just a frontend dialog.
	Confirm    bool     `json:"confirm"`
	Categories []string `json:"categories"`
}

// Meta (GET /api/storage/meta) returns the cleanup policy and per-category metadata.
func (h *StorageHandler) Meta(c *gin.Context) {
	st := h.effectiveConfig()
	items := make([]gin.H, 0, len(config.StorageCategoryOrder))
	for _, info := range storage.DescribeCategories() {
		items = append(items, gin.H{
			"key":               info.Key,
			"label":             info.Label,
			"hint":              info.Hint,
			"enabled":           st.CategoryEnabled(info.Key),
			"retention_days":    st.CategoryRetentionDays(info.Key),
			"default_retention": config.StorageCategoryDefaults[info.Key],
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"auto_clean":         st.AutoCleanEffective(),
		"interval_minutes":   st.IntervalMinutesEffective(),
		"orphan_grace_days":  st.OrphanGraceDaysEffective(),
		"active_grace_hours": st.ActiveGraceHoursEffective(),
		"categories":         items,
	})
}

// Status (GET /api/storage/status) returns filesystem capacity and per-category usage/reclaimable space.
// ?refresh=1 forces a fresh directory traversal; otherwise a short-TTL cache is used.
func (h *StorageHandler) Status(c *gin.Context) {
	if h.cleaner == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "storage cleanup not initialized"})
		return
	}
	refresh, _ := strconv.ParseBool(c.Query("refresh"))
	rep := h.cleaner.Inspect(refresh)
	c.JSON(http.StatusOK, gin.H{
		"filesystem":  rep.Filesystem,
		"categories":  rep.Categories,
		"totals":      rep.Totals,
		"scanned_at":  rep.StartedAt,
		"duration_ms": rep.DurationMS,
	})
}

// Cleanup (POST /api/storage/cleanup) runs a cleanup (or preview).
func (h *StorageHandler) Cleanup(c *gin.Context) {
	if h.cleaner == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "storage cleanup not initialized"})
		return
	}
	var req storageCleanupRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request parameters: " + err.Error()})
			return
		}
	}
	dryRun := req.DryRun == nil || *req.DryRun
	if !dryRun && !req.Confirm {
		c.JSON(http.StatusBadRequest, gin.H{"error": "deletion is irreversible; a real cleanup requires both dry_run=false and confirm=true"})
		return
	}

	rep, err := h.cleaner.Clean(storage.CleanRequest{
		DryRun:     dryRun,
		Categories: req.Categories,
		Trigger:    "manual",
	})
	switch {
	case errors.Is(err, storage.ErrCleanupInProgress):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	case errors.Is(err, storage.ErrUnknownCategory):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if !dryRun {
		h.recordCleanup(c, rep)
	}
	c.JSON(http.StatusOK, gin.H{
		"dry_run":     rep.DryRun,
		"filesystem":  rep.Filesystem,
		"categories":  rep.Categories,
		"totals":      rep.Totals,
		"duration_ms": rep.DurationMS,
	})
}

func (h *StorageHandler) recordCleanup(c *gin.Context, rep *storage.Report) {
	if h.audit == nil {
		return
	}
	detail := map[string]interface{}{
		"removed_units": rep.Totals.RemovedUnits,
		"freed_bytes":   rep.Totals.FreedBytes,
		"errors":        rep.Totals.Errors,
	}
	if rep.Totals.Errors > 0 {
		h.audit.RecordFail(c, "storage", "cleanup", "runtime storage cleanup finished with failures", detail)
		return
	}
	h.audit.RecordOK(c, "storage", "cleanup", "clean runtime garbage", "storage", "", detail)
}

func (h *StorageHandler) effectiveConfig() config.StorageConfig {
	if h.cfg == nil {
		return config.StorageConfig{}
	}
	return h.cfg.Storage
}
