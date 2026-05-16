package handler

import (
	"net/http"
	"strings"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/security"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// AuthHandler handles authentication-related endpoints.
type AuthHandler struct {
	manager    *security.AuthManager
	config     *config.Config
	configPath string
	logger     *zap.Logger
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(manager *security.AuthManager, cfg *config.Config, configPath string, logger *zap.Logger) *AuthHandler {
	return &AuthHandler{
		manager:    manager,
		config:     cfg,
		configPath: configPath,
		logger:     logger,
	}
}

type loginRequest struct {
	Password string `json:"password" binding:"required"`
}

type changePasswordRequest struct {
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword"`
}

// Login verifies password and returns a session token.
func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password cannot be empty"})
		return
	}

	token, expiresAt, err := h.manager.Authenticate(req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Incorrect password"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token":               token,
		"expires_at":          expiresAt.UTC().Format(time.RFC3339),
		"session_duration_hr": h.manager.SessionDurationHours(),
	})
}

// Logout revokes the current session token.
func (h *AuthHandler) Logout(c *gin.Context) {
	token := c.GetString(security.ContextAuthTokenKey)
	if token == "" {
		authHeader := c.GetHeader("Authorization")
		if len(authHeader) > 7 && strings.EqualFold(authHeader[:7], "Bearer ") {
			token = strings.TrimSpace(authHeader[7:])
		} else {
			token = strings.TrimSpace(authHeader)
		}
	}

	h.manager.RevokeToken(token)
	c.JSON(http.StatusOK, gin.H{"message": "Logged out"})
}

// ChangePassword updates the login password.
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid parameters"})
		return
	}

	oldPassword := strings.TrimSpace(req.OldPassword)
	newPassword := strings.TrimSpace(req.NewPassword)

	if oldPassword == "" || newPassword == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Current password and new password cannot be empty"})
		return
	}

	if len(newPassword) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "New password must be at least 8 characters"})
		return
	}

	if oldPassword == newPassword {
		c.JSON(http.StatusBadRequest, gin.H{"error": "New password cannot be the same as old password"})
		return
	}

	if !h.manager.CheckPassword(oldPassword) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Current password is incorrect"})
		return
	}

	if err := config.PersistAuthPassword(h.configPath, newPassword); err != nil {
		if h.logger != nil {
			h.logger.Error("Failed to save new password", zap.Error(err))
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save new password, please retry"})
		return
	}

	if err := h.manager.UpdateConfig(newPassword, h.config.Auth.SessionDurationHours); err != nil {
		if h.logger != nil {
			h.logger.Error("Failed to update auth config", zap.Error(err))
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update auth config"})
		return
	}

	h.config.Auth.Password = newPassword
	h.config.Auth.GeneratedPassword = ""
	h.config.Auth.GeneratedPasswordPersisted = false
	h.config.Auth.GeneratedPasswordPersistErr = ""

	if h.logger != nil {
		h.logger.Info("Login password updated, all sessions invalidated")
	}

	c.JSON(http.StatusOK, gin.H{"message": "Password updated, please login with new password"})
}

// Validate returns the current session status.
func (h *AuthHandler) Validate(c *gin.Context) {
	token := c.GetString(security.ContextAuthTokenKey)
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Session invalid"})
		return
	}

	session, ok := h.manager.ValidateToken(token)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Session expired"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token":      session.Token,
		"expires_at": session.ExpiresAt.UTC().Format(time.RFC3339),
	})
}
