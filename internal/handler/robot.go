package handler

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	robotCmdHelp       = "help"
	robotCmdList       = "list"
	robotCmdListAlt    = "conversation list"
	robotCmdSwitch     = "switch"
	robotCmdContinue   = "continue"
	robotCmdNew        = "new conversation"
	robotCmdClear      = "clear"
	robotCmdCurrent    = "current"
	robotCmdStop       = "stop"
	robotCmdRoles      = "role"
	robotCmdRolesList  = "roles"
	robotCmdSwitchRole = "switch role"
	robotCmdDelete     = "delete"
	robotCmdVersion    = "version"
)

// RobotHandler WeCom/Dingtalk/Lark robot callback handler
type RobotHandler struct {
	config         *config.Config
	db             *database.DB
	agentHandler   *AgentHandler
	logger         *zap.Logger
	mu             sync.RWMutex
	sessions       map[string]string             // key: "platform_userID", value: conversationID
	sessionRoles   map[string]string             // key: "platform_userID", value: roleName (default "default")
	cancelMu       sync.Mutex                    // protects runningCancels
	runningCancels map[string]context.CancelFunc // key: "platform_userID", used by the stop command to interrupt tasks
}

// NewRobotHandler creates a robot handler
func NewRobotHandler(cfg *config.Config, db *database.DB, agentHandler *AgentHandler, logger *zap.Logger) *RobotHandler {
	return &RobotHandler{
		config:         cfg,
		db:             db,
		agentHandler:   agentHandler,
		logger:         logger,
		sessions:       make(map[string]string),
		sessionRoles:   make(map[string]string),
		runningCancels: make(map[string]context.CancelFunc),
	}
}

// sessionKey builds the session key
func (h *RobotHandler) sessionKey(platform, userID string) string {
	return platform + "_" + userID
}

func (h *RobotHandler) loadSessionBinding(sk string) (convID, role string) {
	if h.db == nil || strings.TrimSpace(sk) == "" {
		return "", ""
	}
	binding, err := h.db.GetRobotSessionBinding(sk)
	if err != nil {
		h.logger.Warn("failed to read robot session binding", zap.String("session_key", sk), zap.Error(err))
		return "", ""
	}
	if binding == nil {
		return "", ""
	}
	return binding.ConversationID, binding.RoleName
}

func (h *RobotHandler) persistSessionBinding(sk, convID, role string) {
	if h.db == nil || strings.TrimSpace(sk) == "" || strings.TrimSpace(convID) == "" {
		return
	}
	if err := h.db.UpsertRobotSessionBinding(sk, convID, role); err != nil {
		h.logger.Warn("failed to write robot session binding", zap.String("session_key", sk), zap.Error(err))
	}
}

func (h *RobotHandler) deleteSessionBinding(sk string) {
	if h.db == nil || strings.TrimSpace(sk) == "" {
		return
	}
	if err := h.db.DeleteRobotSessionBinding(sk); err != nil {
		h.logger.Warn("failed to delete robot session binding", zap.String("session_key", sk), zap.Error(err))
	}
}

// getOrCreateConversation gets or creates the current conversation; title is used for a new conversation title (first 50 chars of the user message)
func (h *RobotHandler) getOrCreateConversation(platform, userID, title string) (convID string, isNew bool) {
	sk := h.sessionKey(platform, userID)
	h.mu.RLock()
	convID = h.sessions[sk]
	h.mu.RUnlock()
	if convID != "" {
		return convID, false
	}
	if persistedConvID, persistedRole := h.loadSessionBinding(sk); strings.TrimSpace(persistedConvID) != "" {
		// Session binding is persisted so the current conversation and role survive service restarts.
		h.mu.Lock()
		h.sessions[sk] = persistedConvID
		if strings.TrimSpace(persistedRole) != "" {
			h.sessionRoles[sk] = persistedRole
		}
		h.mu.Unlock()
		return persistedConvID, false
	}
	t := strings.TrimSpace(title)
	if t == "" {
		t = "new conversation " + time.Now().Format("01-02 15:04")
	} else {
		t = safeTruncateString(t, 50)
	}
	meta := database.ConversationCreateMeta{Source: "robot:" + platform}
	meta.ProjectID = effectiveProjectID(h.config, "")
	conv, err := h.db.CreateConversation(t, meta)
	if err != nil {
		h.logger.Warn("failed to create robot conversation", zap.Error(err))
		return "", false
	}
	convID = conv.ID
	h.mu.Lock()
	role := h.sessionRoles[sk]
	h.sessions[sk] = convID
	h.mu.Unlock()
	h.persistSessionBinding(sk, convID, role)
	return convID, true
}

// setConversation switches the current conversation
func (h *RobotHandler) setConversation(platform, userID, convID string) {
	sk := h.sessionKey(platform, userID)
	h.mu.Lock()
	role := h.sessionRoles[sk]
	h.sessions[sk] = convID
	h.mu.Unlock()
	h.persistSessionBinding(sk, convID, role)
}

// getRole gets the current user role and returns "default" when unset
func (h *RobotHandler) getRole(platform, userID string) string {
	sk := h.sessionKey(platform, userID)
	h.mu.RLock()
	role := h.sessionRoles[sk]
	h.mu.RUnlock()
	if strings.TrimSpace(role) != "" {
		return role
	}
	if _, persistedRole := h.loadSessionBinding(sk); strings.TrimSpace(persistedRole) != "" {
		h.mu.Lock()
		h.sessionRoles[sk] = persistedRole
		h.mu.Unlock()
		return persistedRole
	}
	return "default"
}

// setRole sets the current user role
func (h *RobotHandler) setRole(platform, userID, roleName string) {
	sk := h.sessionKey(platform, userID)
	h.mu.Lock()
	h.sessionRoles[sk] = roleName
	convID := h.sessions[sk]
	h.mu.Unlock()
	h.persistSessionBinding(sk, convID, roleName)
}

// clearConversation clears the current conversation (switches to a new conversation)
func (h *RobotHandler) clearConversation(platform, userID string) (newConvID string) {
	title := "new conversation " + time.Now().Format("01-02 15:04")
	meta := database.ConversationCreateMeta{Source: "robot:" + platform + ":new"}
	meta.ProjectID = effectiveProjectID(h.config, "")
	conv, err := h.db.CreateConversation(title, meta)
	if err != nil {
		h.logger.Warn("failed to create a new conversation", zap.Error(err))
		return ""
	}
	h.setConversation(platform, userID, conv.ID)
	return conv.ID
}

// HandleMessage handles user input and returns reply text (used by platform webhooks)
func (h *RobotHandler) HandleMessage(platform, userID, text string) (reply string) {
	platform = strings.TrimSpace(platform)
	userID = strings.TrimSpace(userID)
	text = strings.TrimSpace(text)
	if platform == "" {
		platform = "unknown"
	}
	if userID == "" {
		h.logger.Warn("robot message missing user identifier; rejected", zap.String("platform", platform))
		return "Unable to identify the sender. Check robot event subscription permissions; a usable user ID must be returned."
	}
	if text == "" {
		return "Enter content or send `help` to view commands."
	}

	// Try command handling first.
	if cmdReply, ok := h.handleRobotCommand(platform, userID, text); ok {
		return cmdReply
	}

	// Normal messages go through the Agent.
	convID, _ := h.getOrCreateConversation(platform, userID, text)
	if convID == "" {
		return "Unable to create or get a conversation. Try again later."
	}
	// If the title has the `new conversation xx:xx` format created by the new command, update it to the first message for parity with the web UI.
	if conv, err := h.db.GetConversation(convID); err == nil && strings.HasPrefix(conv.Title, "new conversation ") {
		newTitle := safeTruncateString(text, 50)
		if newTitle != "" {
			_ = h.db.UpdateConversationTitle(convID, newTitle)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), h.robotMessageTimeout())
	sk := h.sessionKey(platform, userID)
	h.cancelMu.Lock()
	h.runningCancels[sk] = cancel
	h.cancelMu.Unlock()
	defer func() {
		cancel()
		h.cancelMu.Lock()
		delete(h.runningCancels, sk)
		h.cancelMu.Unlock()
	}()
	role := h.getRole(platform, userID)
	resp, newConvID, err := h.agentHandler.ProcessMessageForRobot(ctx, platform, convID, text, role)
	if err != nil {
		h.logger.Warn("robot Agent execution failed", zap.String("platform", platform), zap.String("userID", userID), zap.Error(err))
		if errors.Is(err, context.Canceled) {
			return "Task canceled."
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return "Task timed out. Try again later or narrow the request scope."
		}
		return "Processing failed: " + err.Error()
	}
	if newConvID != convID {
		h.setConversation(platform, userID, newConvID)
	}
	return resp
}

func (h *RobotHandler) robotMessageTimeout() time.Duration {
	// Overall robot message timeout (decoupled from per-tool agent.tool_timeout_minutes).
	return 10 * time.Hour
}

func (h *RobotHandler) cmdHelp() string {
	return "**[CyberStrikeAI robot commands]**\n\n" +
		"- `help` — Show this help\n" +
		"- `list` — List conversation titles and IDs\n" +
		"- `switch <ID>` — Switch to the specified conversation\n" +
		"- `new` — Start a new conversation\n" +
		"- `clear` — Clear current context\n" +
		"- `current` — Show current conversation ID and title\n" +
		"- `stop` — Stop the running task\n" +
		"- `roles` — List available roles\n" +
		"- `role <name>` — Switch current role\n" +
		"- `delete <ID>` — Delete the specified conversation\n" +
		"- `version` — Show current version\n\n" +
		"---\n" +
		"Outside these commands, send any text to the AI for penetration testing / security analysis.\n" +
		"Otherwise, send any text for AI penetration testing / security analysis."
}

func (h *RobotHandler) cmdList() string {
	convs, err := h.db.ListConversations(50, 0, "")
	if err != nil {
		return "Failed to get conversation list: " + err.Error()
	}
	if len(convs) == 0 {
		return "No conversations yet. Send any content to create a new conversation automatically."
	}
	var b strings.Builder
	b.WriteString("[Conversation list]\n")
	for i, c := range convs {
		if i >= 20 {
			b.WriteString("… showing only the first 20 items\n")
			break
		}
		b.WriteString(fmt.Sprintf("· %s\n  ID: %s\n", c.Title, c.ID))
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func (h *RobotHandler) cmdSwitch(platform, userID, convID string) string {
	if convID == "" {
		return "Specify a conversation ID, for example: switch xxx-xxx-xxx"
	}
	conv, err := h.db.GetConversation(convID)
	if err != nil {
		return "Conversation not found or ID is incorrect."
	}
	h.setConversation(platform, userID, conv.ID)
	return fmt.Sprintf("Switched to conversation: `%s`\nID: %s", conv.Title, conv.ID)
}

func (h *RobotHandler) cmdNew(platform, userID string) string {
	newID := h.clearConversation(platform, userID)
	if newID == "" {
		return "Failed to create a new conversation. Try again."
	}
	return "Started a new conversation. You can send content now."
}

func (h *RobotHandler) cmdClear(platform, userID string) string {
	return h.cmdNew(platform, userID)
}

func (h *RobotHandler) cmdStop(platform, userID string) string {
	sk := h.sessionKey(platform, userID)
	h.cancelMu.Lock()
	cancel, ok := h.runningCancels[sk]
	if ok {
		delete(h.runningCancels, sk)
		cancel()
	}
	h.cancelMu.Unlock()
	if !ok {
		return "No task is currently running."
	}
	return "Stopped the current task."
}

func (h *RobotHandler) cmdCurrent(platform, userID string) string {
	h.mu.RLock()
	convID := h.sessions[h.sessionKey(platform, userID)]
	h.mu.RUnlock()
	if convID == "" {
		return "No active conversation. Send any content to create a new conversation."
	}
	conv, err := h.db.GetConversation(convID)
	if err != nil {
		return "Current conversation ID: " + convID + " (failed to get title)"
	}
	role := h.getRole(platform, userID)
	return fmt.Sprintf("Current conversation: `%s`\nID: %s\nCurrent role: %s", conv.Title, conv.ID, role)
}

func (h *RobotHandler) cmdRoles() string {
	if h.config.Roles == nil || len(h.config.Roles) == 0 {
		return "No roles available."
	}
	names := make([]string, 0, len(h.config.Roles))
	for name, role := range h.config.Roles {
		if role.Enabled {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "No roles available."
	}
	sort.Slice(names, func(i, j int) bool {
		if names[i] == "default" {
			return true
		}
		if names[j] == "default" {
			return false
		}
		return names[i] < names[j]
	})
	var b strings.Builder
	b.WriteString("[Role list]\n")
	for _, name := range names {
		role := h.config.Roles[name]
		desc := role.Description
		if desc == "" {
			desc = "No description"
		}
		b.WriteString(fmt.Sprintf("· %s — %s\n", name, desc))
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func (h *RobotHandler) cmdSwitchRole(platform, userID, roleName string) string {
	if roleName == "" {
		return "Specify a role name, for example: role default"
	}
	if h.config.Roles == nil {
		return "No roles available."
	}
	role, exists := h.config.Roles[roleName]
	if !exists {
		return fmt.Sprintf("Role `%s` does not exist. Send `roles` to view available roles.", roleName)
	}
	if !role.Enabled {
		return fmt.Sprintf("Role `%s` is disabled.", roleName)
	}
	h.setRole(platform, userID, roleName)
	return fmt.Sprintf("Switched to role: `%s`\n%s", roleName, role.Description)
}

func (h *RobotHandler) cmdDelete(platform, userID, convID string) string {
	if convID == "" {
		return "Specify a conversation ID, for example: delete xxx-xxx-xxx"
	}
	sk := h.sessionKey(platform, userID)
	h.mu.RLock()
	currentConvID := h.sessions[sk]
	h.mu.RUnlock()
	if convID == currentConvID {
		// When deleting the current conversation, clear the session binding first.
		h.mu.Lock()
		delete(h.sessions, sk)
		delete(h.sessionRoles, sk)
		h.mu.Unlock()
		h.deleteSessionBinding(sk)
	}
	if err := h.db.DeleteConversation(convID); err != nil {
		return "Delete failed: " + err.Error()
	}
	return fmt.Sprintf("Deleted conversation ID: %s", convID)
}

func (h *RobotHandler) cmdVersion() string {
	v := h.config.Version
	if v == "" {
		v = "unknown"
	}
	return "CyberStrikeAI " + v
}

// handleRobotCommand handles built-in robot commands; returns (reply, true) on match, otherwise ("", false)
func (h *RobotHandler) handleRobotCommand(platform, userID, text string) (string, bool) {
	switch {
	case text == robotCmdHelp || text == "help" || text == "?":
		return h.cmdHelp(), true
	case text == robotCmdList || text == robotCmdListAlt || text == "list":
		return h.cmdList(), true
	case strings.HasPrefix(text, robotCmdSwitch+" ") || strings.HasPrefix(text, robotCmdContinue+" ") || strings.HasPrefix(text, "switch ") || strings.HasPrefix(text, "continue "):
		var id string
		switch {
		case strings.HasPrefix(text, robotCmdSwitch+" "):
			id = strings.TrimSpace(text[len(robotCmdSwitch)+1:])
		case strings.HasPrefix(text, robotCmdContinue+" "):
			id = strings.TrimSpace(text[len(robotCmdContinue)+1:])
		case strings.HasPrefix(text, "switch "):
			id = strings.TrimSpace(text[7:])
		default:
			id = strings.TrimSpace(text[9:])
		}
		return h.cmdSwitch(platform, userID, id), true
	case text == robotCmdNew || text == "new":
		return h.cmdNew(platform, userID), true
	case text == robotCmdClear || text == "clear":
		return h.cmdClear(platform, userID), true
	case text == robotCmdCurrent || text == "current":
		return h.cmdCurrent(platform, userID), true
	case text == robotCmdStop || text == "stop":
		return h.cmdStop(platform, userID), true
	case text == robotCmdRoles || text == robotCmdRolesList || text == "roles":
		return h.cmdRoles(), true
	case strings.HasPrefix(text, robotCmdRoles+" ") || strings.HasPrefix(text, robotCmdSwitchRole+" ") || strings.HasPrefix(text, "role "):
		var roleName string
		switch {
		case strings.HasPrefix(text, robotCmdRoles+" "):
			roleName = strings.TrimSpace(text[len(robotCmdRoles)+1:])
		case strings.HasPrefix(text, robotCmdSwitchRole+" "):
			roleName = strings.TrimSpace(text[len(robotCmdSwitchRole)+1:])
		default:
			roleName = strings.TrimSpace(text[5:])
		}
		return h.cmdSwitchRole(platform, userID, roleName), true
	case strings.HasPrefix(text, robotCmdDelete+" ") || strings.HasPrefix(text, "delete "):
		var convID string
		if strings.HasPrefix(text, robotCmdDelete+" ") {
			convID = strings.TrimSpace(text[len(robotCmdDelete)+1:])
		} else {
			convID = strings.TrimSpace(text[7:])
		}
		return h.cmdDelete(platform, userID, convID), true
	case text == robotCmdVersion || text == "version":
		return h.cmdVersion(), true
	default:
		return "", false
	}
}

// —————— WeCom ——————

// wecomXML WeCom callback XML (simplified structure for plaintext mode; encrypted mode must be decrypted before parsing)
type wecomXML struct {
	ToUserName   string `xml:"ToUserName"`
	FromUserName string `xml:"FromUserName"`
	CreateTime   int64  `xml:"CreateTime"`
	MsgType      string `xml:"MsgType"`
	Content      string `xml:"Content"`
	MsgID        string `xml:"MsgId"`
	AgentID      int64  `xml:"AgentID"`
	Encrypt      string `xml:"Encrypt"` // message is stored here in encrypted mode
}

// wecomReplyXML passive reply XML (compatibility only; current code builds XML manually)
type wecomReplyXML struct {
	XMLName      xml.Name `xml:"xml"`
	ToUserName   string   `xml:"ToUserName"`
	FromUserName string   `xml:"FromUserName"`
	CreateTime   int64    `xml:"CreateTime"`
	MsgType      string   `xml:"MsgType"`
	Content      string   `xml:"Content"`
}

// HandleWecomGET WeCom URL verification (GET)
func (h *RobotHandler) HandleWecomGET(c *gin.Context) {
	if !h.config.Robots.Wecom.Enabled {
		c.String(http.StatusNotFound, "")
		return
	}
	// Gin Query() URL-decodes automatically, so this is the correct base64 string.
	echostr := c.Query("echostr")
	msgSignature := c.Query("msg_signature")
	timestamp := c.Query("timestamp")
	nonce := c.Query("nonce")

	// Verify the signature by sorting token, timestamp, nonce, and echostr, concatenating them, then calculating SHA1.
	signature := h.signWecomRequest(h.config.Robots.Wecom.Token, timestamp, nonce, echostr)
	if signature != msgSignature {
		h.logger.Warn("WeCom URL signature verification failed", zap.String("expected", msgSignature), zap.String("got", signature))
		c.String(http.StatusBadRequest, "invalid signature")
		return
	}

	if echostr == "" {
		c.String(http.StatusBadRequest, "missing echostr")
		return
	}

	// If EncodingAESKey is configured, encrypted mode is enabled and echostr must be decrypted.
	if h.config.Robots.Wecom.EncodingAESKey != "" {
		decrypted, err := wecomDecrypt(h.config.Robots.Wecom.EncodingAESKey, echostr)
		if err != nil {
			h.logger.Warn("WeCom echostr decrypt failed", zap.Error(err))
			c.String(http.StatusBadRequest, "decrypt failed")
			return
		}
		c.String(http.StatusOK, string(decrypted))
		return
	}

	// Plaintext mode returns echostr directly.
	c.String(http.StatusOK, echostr)
}

// signWecomRequest generates a WeCom request signature
// WeCom signature algorithm: sort token, timestamp, nonce, and echostr, concatenate them, then calculate SHA1.
func (h *RobotHandler) signWecomRequest(token, timestamp, nonce, echostr string) string {
	strs := []string{token, timestamp, nonce, echostr}
	sort.Strings(strs)
	s := strings.Join(strs, "")
	hash := sha1.Sum([]byte(s))
	return fmt.Sprintf("%x", hash)
}

// wecomDecrypt decrypts a WeCom message (AES-256-CBC, PKCS7; plaintext format: 16 random bytes + 4-byte length + message + corpID)
func wecomDecrypt(encodingAESKey, encryptedB64 string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(encodingAESKey + "=")
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("encoding_aes_key must decode to 32 bytes")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(encryptedB64)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	iv := key[:16]
	mode := cipher.NewCBCDecrypter(block, iv)
	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("ciphertext length is not a multiple of the block size")
	}
	plain := make([]byte, len(ciphertext))
	mode.CryptBlocks(plain, ciphertext)
	// Remove PKCS7 padding.
	n := int(plain[len(plain)-1])
	if n < 1 || n > 32 {
		return nil, fmt.Errorf("invalid PKCS7 padding")
	}
	plain = plain[:len(plain)-n]
	// WeCom format: 16 random bytes + 4-byte length (big endian) + message + corpID.
	if len(plain) < 20 {
		return nil, fmt.Errorf("plaintext is too short")
	}
	msgLen := binary.BigEndian.Uint32(plain[16:20])
	if int(20+msgLen) > len(plain) {
		return nil, fmt.Errorf("message length is out of range")
	}
	return plain[20 : 20+msgLen], nil
}

// wecomEncrypt encrypts a WeCom message (AES-256-CBC, PKCS7; plaintext format: 16 random bytes + 4-byte length + message + corpID)
func wecomEncrypt(encodingAESKey, message, corpID string) (string, error) {
	key, err := base64.StdEncoding.DecodeString(encodingAESKey + "=")
	if err != nil {
		return "", err
	}
	if len(key) != 32 {
		return "", fmt.Errorf("encoding_aes_key must decode to 32 bytes")
	}
	// Build plaintext: 16 random bytes + 4-byte length (big endian) + message + corpID.
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		// Fallback: generate random bytes from the timestamp.
		for i := range random {
			random[i] = byte(time.Now().UnixNano() % 256)
		}
	}
	msgLen := len(message)
	msgBytes := []byte(message)
	corpBytes := []byte(corpID)
	plain := make([]byte, 16+4+msgLen+len(corpBytes))
	copy(plain[:16], random)
	binary.BigEndian.PutUint32(plain[16:20], uint32(msgLen))
	copy(plain[20:20+msgLen], msgBytes)
	copy(plain[20+msgLen:], corpBytes)
	// PKCS7 padding.
	padding := aes.BlockSize - len(plain)%aes.BlockSize
	pad := bytes.Repeat([]byte{byte(padding)}, padding)
	plain = append(plain, pad...)
	// AES-256-CBC encryption.
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	iv := key[:16]
	ciphertext := make([]byte, len(plain))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, plain)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// HandleWecomPOST WeCom message callback (POST), supports plaintext and encrypted modes
func (h *RobotHandler) HandleWecomPOST(c *gin.Context) {
	if !h.config.Robots.Wecom.Enabled {
		h.logger.Debug("WeCom robot disabled; skipping request")
		c.String(http.StatusOK, "")
		return
	}
	// Read signature parameters from the URL (needed when replying in encrypted mode).
	timestamp := c.Query("timestamp")
	nonce := c.Query("nonce")
	msgSignature := c.Query("msg_signature")

	// Read the request body first; parsing and signature verification both need it.
	bodyRaw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.logger.Warn("WeCom POST failed to read request body", zap.Error(err))
		c.String(http.StatusOK, "")
		return
	}
	h.logger.Debug("WeCom POST received request", zap.String("body", string(bodyRaw)))

	// Verify the request signature to prevent forgery. WeCom uses the same algorithm as URL verification, with token, timestamp, nonce, and Encrypt.
	// If Token is configured, signature verification is required to prevent unauthorized requests from triggering the Agent.
	token := h.config.Robots.Wecom.Token
	if token != "" {
		if msgSignature == "" {
			h.logger.Warn("WeCom POST missing signature; rejected (configure token and ensure callbacks include msg_signature)")
			c.String(http.StatusOK, "")
			return
		}
		var tmp wecomXML
		if err := xml.Unmarshal(bodyRaw, &tmp); err != nil {
			h.logger.Warn("WeCom POST failed to parse XML before signature verification", zap.Error(err))
			c.String(http.StatusOK, "")
			return
		}
		expected := h.signWecomRequest(token, timestamp, nonce, tmp.Encrypt)
		if expected != msgSignature {
			h.logger.Warn("WeCom POST signature verification failed", zap.String("expected", expected), zap.String("got", msgSignature))
			c.String(http.StatusOK, "")
			return
		}
	}

	var body wecomXML
	if err := xml.Unmarshal(bodyRaw, &body); err != nil {
		h.logger.Warn("WeCom POST failed to parse XML", zap.Error(err))
		c.String(http.StatusOK, "")
		return
	}
	h.logger.Debug("WeCom XML parsed successfully", zap.String("ToUserName", body.ToUserName), zap.String("FromUserName", body.FromUserName), zap.String("MsgType", body.MsgType), zap.String("Content", body.Content), zap.String("Encrypt", body.Encrypt))

	// Save enterprise ID (used for plaintext replies).
	enterpriseID := body.ToUserName

	// Encrypted mode: decrypt first, then parse the inner XML.
	if body.Encrypt != "" && h.config.Robots.Wecom.EncodingAESKey != "" {
		h.logger.Debug("WeCom entering encrypted-mode decrypt flow")
		decrypted, err := wecomDecrypt(h.config.Robots.Wecom.EncodingAESKey, body.Encrypt)
		if err != nil {
			h.logger.Warn("WeCom message decrypt failed", zap.Error(err))
			c.String(http.StatusOK, "")
			return
		}
		h.logger.Debug("WeCom decrypt succeeded", zap.String("decrypted", string(decrypted)))
		if err := xml.Unmarshal(decrypted, &body); err != nil {
			h.logger.Warn("WeCom failed to parse XML after decrypt", zap.Error(err))
			c.String(http.StatusOK, "")
			return
		}
		h.logger.Debug("WeCom inner XML parsed successfully", zap.String("FromUserName", body.FromUserName), zap.String("Content", body.Content))
	}

	tenantKey := strings.TrimSpace(enterpriseID)
	if tenantKey == "" {
		tenantKey = strings.TrimSpace(h.config.Robots.Wecom.CorpID)
	}
	if tenantKey == "" {
		tenantKey = "default"
	}
	rawUserID := strings.TrimSpace(body.FromUserName)
	replyUserID := rawUserID
	userID := ""
	if rawUserID != "" {
		userID = "t:" + tenantKey + "|u:" + rawUserID
	}
	text := strings.TrimSpace(body.Content)
	if userID == "" {
		h.logger.Warn("WeCom message missing usable user identifier; ignored")
		c.String(http.StatusOK, "success")
		return
	}

	// Limit reply length (WeCom limit is 2048 bytes).
	maxReplyLen := 2000
	limitReply := func(s string) string {
		if len(s) > maxReplyLen {
			return s[:maxReplyLen] + "\n\n(content too long; truncated)"
		}
		return s
	}

	if body.MsgType != "text" {
		h.logger.Debug("WeCom received a non-text message", zap.String("MsgType", body.MsgType))
		h.sendWecomReply(c, replyUserID, enterpriseID, limitReply("Only text messages are supported. Send a text message."), timestamp, nonce)
		return
	}

	// For text messages, handle built-in commands first; they are fast and can use passive replies without depending on the proactive send API.
	if cmdReply, ok := h.handleRobotCommand("wecom", userID, text); ok {
		h.logger.Debug("WeCom received command message; using passive reply", zap.String("userID", userID), zap.String("text", text))
		h.sendWecomReply(c, replyUserID, enterpriseID, limitReply(cmdReply), timestamp, nonce)
		return
	}

	h.logger.Debug("WeCom start processing message (async AI)", zap.String("userID", userID), zap.String("text", text))

	// WeCom passive replies have a 5-second timeout, while AI calls usually take longer.
	// Use the recommended approach: return success (or empty string) immediately, then push the full reply through the proactive send API.
	c.String(http.StatusOK, "success")

	// Process the message asynchronously and send the result through the WeCom proactive message API.
	go func() {
		reply := h.HandleMessage("wecom", userID, text)
		reply = limitReply(reply)
		h.logger.Debug("WeCom message processing completed", zap.String("userID", userID), zap.String("reply", reply))
		// Call the WeCom API to proactively send the message.
		h.sendWecomMessageViaAPI(rawUserID, enterpriseID, reply)
	}()
}

// sendWecomReply sends a WeCom reply (encrypted automatically in encrypted mode)
// Parameters: toUser=user ID, fromUser=enterprise ID (plaintext mode) / CorpID (encrypted mode), content=reply content, timestamp/nonce=request parameters.
func (h *RobotHandler) sendWecomReply(c *gin.Context, toUser, fromUser, content, timestamp, nonce string) {
	// Encrypted mode: check whether EncodingAESKey is configured.
	if h.config.Robots.Wecom.EncodingAESKey != "" {
		// Encrypted mode uses CorpID for encryption.
		corpID := h.config.Robots.Wecom.CorpID
		if corpID == "" {
			h.logger.Warn("WeCom encrypted mode missing CorpID config")
			c.String(http.StatusOK, "")
			return
		}

		// Build the full plaintext XML reply in the format required by WeCom docs.
		plainResp := fmt.Sprintf(`<xml>
<ToUserName><![CDATA[%s]]></ToUserName>
<FromUserName><![CDATA[%s]]></FromUserName>
<CreateTime>%d</CreateTime>
<MsgType><![CDATA[text]]></MsgType>
<Content><![CDATA[%s]]></Content>
</xml>`, toUser, fromUser, time.Now().Unix(), content)

		encrypted, err := wecomEncrypt(h.config.Robots.Wecom.EncodingAESKey, plainResp, corpID)
		if err != nil {
			h.logger.Warn("WeCom reply encryption failed", zap.Error(err))
			c.String(http.StatusOK, "")
			return
		}
		// Generate the signature with timestamp/nonce from the request; WeCom requires replies to use the same timestamp and nonce.
		msgSignature := h.signWecomRequest(h.config.Robots.Wecom.Token, timestamp, nonce, encrypted)

		h.logger.Debug("WeCom sending encrypted reply",
			zap.String("Encrypt", encrypted[:50]+"..."),
			zap.String("MsgSignature", msgSignature),
			zap.String("TimeStamp", timestamp),
			zap.String("Nonce", nonce))

		// Encrypted mode returns only the four core fields required by WeCom.
		xmlResp := fmt.Sprintf(`<xml><Encrypt><![CDATA[%s]]></Encrypt><MsgSignature><![CDATA[%s]]></MsgSignature><TimeStamp><![CDATA[%s]]></TimeStamp><Nonce><![CDATA[%s]]></Nonce></xml>`, encrypted, msgSignature, timestamp, nonce)
		// also log the final response body so we can cross-check with the
		// network traffic or developer console
		h.logger.Debug("WeCom encrypted reply package", zap.String("xml", xmlResp))
		// for additional confidence, decrypt the payload ourselves and log it
		if dec, err2 := wecomDecrypt(h.config.Robots.Wecom.EncodingAESKey, encrypted); err2 == nil {
			h.logger.Debug("WeCom encrypted reply decrypt check", zap.String("plain", string(dec)))
		} else {
			h.logger.Warn("WeCom encrypted reply decrypt check failed", zap.Error(err2))
		}

		// Write the response directly with c.Writer.Write to avoid c.String escaping issues.
		c.Writer.WriteHeader(http.StatusOK)
		// use text/xml as that's what WeCom examples show
		c.Writer.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = c.Writer.Write([]byte(xmlResp))
		h.logger.Debug("WeCom encrypted reply sent")
		return
	}

	// Plaintext mode.
	h.logger.Debug("WeCom sending plaintext reply", zap.String("ToUserName", toUser), zap.String("FromUserName", fromUser), zap.String("Content", content[:50]+"..."))

	// Manually build the XML response, wrapping fields in CDATA.
	xmlResp := fmt.Sprintf(`<xml>
<ToUserName><![CDATA[%s]]></ToUserName>
<FromUserName><![CDATA[%s]]></FromUserName>
<CreateTime>%d</CreateTime>
<MsgType><![CDATA[text]]></MsgType>
<Content><![CDATA[%s]]></Content>
</xml>`, toUser, fromUser, time.Now().Unix(), content)

	// log the exact plaintext response for debugging
	h.logger.Debug("WeCom plaintext reply package", zap.String("xml", xmlResp))

	// use text/xml as recommended by WeCom docs
	c.Header("Content-Type", "text/xml; charset=utf-8")
	c.String(http.StatusOK, xmlResp)
	h.logger.Debug("WeCom plaintext reply sent")
}

// —————— Test interface (login required; verifies robot logic without Dingtalk/Lark clients) ——————

// RobotTestRequest simulated robot message request
type RobotTestRequest struct {
	Platform string `json:"platform"` // for example "dingtalk", "lark", or "wecom"
	UserID   string `json:"user_id"`
	Text     string `json:"text"`
}

// HandleRobotTest local verification: POST JSON { "platform", "user_id", "text" }, returns { "reply": "..." }
func (h *RobotHandler) HandleRobotTest(c *gin.Context) {
	var req RobotTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request body must be JSON and include platform, user_id, and text"})
		return
	}
	platform := strings.TrimSpace(req.Platform)
	if platform == "" {
		platform = "test"
	}
	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		userID = "test_user"
	}
	reply := h.HandleMessage(platform, userID, req.Text)
	c.JSON(http.StatusOK, gin.H{"reply": reply})
}

// sendWecomMessageViaAPI proactively sends a message through the WeCom API (used for async results)
func (h *RobotHandler) sendWecomMessageViaAPI(toUser, toParty, content string) {
	if !h.config.Robots.Wecom.Enabled {
		return
	}

	secret := h.config.Robots.Wecom.Secret
	corpID := h.config.Robots.Wecom.CorpID
	agentID := h.config.Robots.Wecom.AgentID

	if secret == "" || corpID == "" {
		h.logger.Warn("WeCom proactive API missing secret or corpID config")
		return
	}

	// Step 1: get access_token.
	tokenURL := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=%s&corpsecret=%s", corpID, secret)
	resp, err := http.Get(tokenURL)
	if err != nil {
		h.logger.Warn("WeCom failed to get token", zap.Error(err))
		return
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		h.logger.Warn("WeCom token response parse failed", zap.Error(err))
		return
	}
	if tokenResp.ErrCode != 0 {
		h.logger.Warn("WeCom token returned an error", zap.String("errmsg", tokenResp.ErrMsg), zap.Int("errcode", tokenResp.ErrCode))
		return
	}

	// Step 2: build the send message request.
	msgReq := map[string]interface{}{
		"touser":  toUser,
		"msgtype": "text",
		"agentid": agentID,
		"text": map[string]interface{}{
			"content": content,
		},
	}

	msgBody, err := json.Marshal(msgReq)
	if err != nil {
		h.logger.Warn("WeCom message serialization failed", zap.Error(err))
		return
	}

	// Step 3: send the message.
	sendURL := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token=%s", tokenResp.AccessToken)
	msgResp, err := http.Post(sendURL, "application/json", bytes.NewReader(msgBody))
	if err != nil {
		h.logger.Warn("WeCom failed to proactively send message", zap.Error(err))
		return
	}
	defer msgResp.Body.Close()

	var sendResp struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		InvalidUser string `json:"invaliduser"`
		MsgID       string `json:"msgid"`
	}
	if err := json.NewDecoder(msgResp.Body).Decode(&sendResp); err != nil {
		h.logger.Warn("WeCom send response parse failed", zap.Error(err))
		return
	}

	if sendResp.ErrCode == 0 {
		h.logger.Debug("WeCom proactively sent message successfully", zap.String("msgid", sendResp.MsgID))
	} else {
		h.logger.Warn("WeCom failed to proactively send message", zap.String("errmsg", sendResp.ErrMsg), zap.Int("errcode", sendResp.ErrCode), zap.String("invaliduser", sendResp.InvalidUser))
	}
}

// —————— Dingtalk ——————

// HandleDingtalkPOST Dingtalk event callback (streaming access, etc.); currently a placeholder that returns 200
func (h *RobotHandler) HandleDingtalkPOST(c *gin.Context) {
	if !h.config.Robots.Dingtalk.Enabled {
		c.JSON(http.StatusOK, gin.H{})
		return
	}
	// Dingtalk streaming/event callback format must be parsed according to official docs and replied to asynchronously; this currently returns 200 only.
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

// —————— Lark ——————

// HandleLarkPOST Lark event callback; currently a placeholder that returns 200; verification must return challenge
func (h *RobotHandler) HandleLarkPOST(c *gin.Context) {
	if !h.config.Robots.Lark.Enabled {
		c.JSON(http.StatusOK, gin.H{})
		return
	}
	var body struct {
		Challenge string `json:"challenge"`
	}
	if err := c.ShouldBindJSON(&body); err == nil && body.Challenge != "" {
		c.JSON(http.StatusOK, gin.H{"challenge": body.Challenge})
		return
	}
	c.JSON(http.StatusOK, gin.H{})
}
