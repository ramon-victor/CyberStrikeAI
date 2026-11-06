package handler

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"cyberstrike-ai/internal/toolguard"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

func (h *ConfigHandler) SetToolGuard(manager *toolguard.Manager) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.toolGuard = manager
	h.toolGuardRevision = uuid.NewString()
}

// Call with h.mu held. The nonce fences no-op saves and ABA; the hash detects
// policy changes made directly through the manager as well.
func (h *ConfigHandler) toolGuardETag() string {
	if h.toolGuardRevision == "" {
		h.toolGuardRevision = uuid.NewString()
	}
	data, _ := json.Marshal(h.toolGuard.Config())
	return fmt.Sprintf(`"%s-%x"`, h.toolGuardRevision, sha256.Sum256(data))
}

func (h *ConfigHandler) GetToolGuard(c *gin.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.toolGuard == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "tool call guard service not initialized"})
		return
	}
	c.Header("ETag", h.toolGuardETag())
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, h.toolGuard.Config())
}

// decodeToolGuardRequest bounds both config and dry-run inputs, rejects unknown
// fields and trailing JSON, and never invokes an actual tool.
func decodeToolGuardRequest(c *gin.Context, dst interface{}) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		return fmt.Errorf("request must contain exactly one JSON object")
	}
	return nil
}

func (h *ConfigHandler) UpdateToolGuard(c *gin.Context) {
	var req struct {
		Enabled *bool             `json:"enabled"`
		Rules   *[]toolguard.Rule `json:"rules"`
	}
	if err := decodeToolGuardRequest(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tool call guard config: " + err.Error()})
		return
	}
	if req.Enabled == nil || req.Rules == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "both enabled and rules must be provided explicitly; pass an empty array to clear all rules"})
		return
	}
	cfg := toolguard.Config{Enabled: *req.Enabled, Rules: *req.Rules}
	if _, err := toolguard.Compile(cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.toolGuard == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "tool call guard service not initialized"})
		return
	}
	if c.GetHeader("If-Match") == "" {
		c.JSON(http.StatusPreconditionRequired, gin.H{"code": "tool_guard_version_required", "error": "Missing interception configuration version; refresh and retry"})
		return
	}
	if c.GetHeader("If-Match") != h.toolGuardETag() {
		c.JSON(http.StatusConflict, gin.H{"code": "tool_guard_conflict", "error": "Another user changed the interception configuration. Changes were not saved; refresh and retry."})
		return
	}
	// Commit the file first; a validation/write failure must leave the current
	// effective policy and in-memory config intact.
	if err := h.saveToolGuardConfig(cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save tool call guard config: " + err.Error()})
		return
	}
	if err := h.toolGuard.Update(cfg); err != nil {
		// The same immutable input was compiled above, so this cannot fail
		// unless validation gains an additional runtime dependency.
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to apply tool call guard config: " + err.Error()})
		return
	}
	h.config.ToolGuard = &cfg
	h.toolGuardRevision = uuid.NewString()
	if h.audit != nil {
		h.audit.RecordOK(c, "config", "tool_guard_update", "Updated tool call guard rules", "config", "tool_guard", map[string]interface{}{
			"enabled": cfg.Enabled, "rule_count": len(cfg.Rules),
		})
	}
	c.Header("ETag", h.toolGuardETag())
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, h.toolGuard.Config())
}

func (h *ConfigHandler) TestToolGuard(c *gin.Context) {
	var req struct {
		Config    *toolguard.Config      `json:"config"`
		ToolName  string                 `json:"toolName"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := decodeToolGuardRequest(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid dry-run parameters: " + err.Error()})
		return
	}
	if req.Config == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a config to test is required"})
		return
	}
	policy, err := toolguard.Compile(*req.Config)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if match := policy.Check(req.ToolName, req.Arguments); match != nil {
		c.JSON(http.StatusOK, gin.H{"blocked": true, "match": match})
		return
	}
	c.JSON(http.StatusOK, gin.H{"blocked": false})
}

// saveToolGuardConfig changes only this YAML section, preserving unrelated
// settings/comments and file permissions. Rename makes the write atomic.
// h.mu protects the runtime configuration; configFileMu also covers independent
// writers such as ExternalMCPHandler.
func (h *ConfigHandler) saveToolGuardConfig(cfg toolguard.Config) error {
	configFileMu.Lock()
	defer configFileMu.Unlock()
	path, err := filepath.EvalSymlinks(h.configPath)
	if err != nil {
		return err
	}
	doc, err := loadYAMLDocument(path)
	if err != nil {
		return err
	}
	var node yaml.Node
	if err := node.Encode(cfg); err != nil {
		return err
	}
	_, value := ensureKeyValue(doc.Content[0], "tool_guard")
	*value = node
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return err
	}
	if err := encoder.Close(); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tool-guard-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
