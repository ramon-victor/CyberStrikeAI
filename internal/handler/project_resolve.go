package handler

import (
	"strings"

	"cyberstrike-ai/internal/config"
)

// effectiveProjectID prefers the explicit request/queue project, then falls back to config.project.default_project_id.
func effectiveProjectID(cfg *config.Config, explicit string) string {
	if pid := strings.TrimSpace(explicit); pid != "" {
		return pid
	}
	if cfg != nil {
		return strings.TrimSpace(cfg.Project.DefaultProjectID)
	}
	return ""
}
