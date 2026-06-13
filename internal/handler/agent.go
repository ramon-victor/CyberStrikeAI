package handler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/audit"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/mcp/builtin"
	"cyberstrike-ai/internal/multiagent"
	"cyberstrike-ai/internal/openai"
	"cyberstrike-ai/internal/reasoning"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

// safeTruncateString safely truncates a string without cutting through a UTF-8 character
func safeTruncateString(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}

	// Convert the string to runes to count characters correctly
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}

	// Truncate to the maximum length
	truncated := string(runes[:maxLen])

	// Try to truncate at punctuation or whitespace for a more natural break
	// Search backward from the truncation point for a suitable break point, up to 20% of the length
	searchRange := maxLen / 5
	if searchRange > maxLen {
		searchRange = maxLen
	}
	breakChars := []rune("，。、 ,.;:!?！？/\\-_")
	bestBreakPos := len(runes[:maxLen])

	for i := bestBreakPos - 1; i >= bestBreakPos-searchRange && i >= 0; i-- {
		for _, breakChar := range breakChars {
			if runes[i] == breakChar {
				bestBreakPos = i + 1 // Break after punctuation
				goto found
			}
		}
	}

found:
	truncated = string(runes[:bestBreakPos])
	return truncated + "..."
}

// responsePlanAgg buffers main-assistant response_stream chunks for one "planning" process_detail row.
type responsePlanAgg struct {
	meta map[string]interface{}
	b    strings.Builder
}

func normalizeProcessDetailText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.TrimSpace(s)
}

// discardPlanningIfEchoesToolResult drops buffered planning text when it only repeats the
// upcoming tool_result body. Streaming models often echo tool stdout in chunk.Content; flushing
// that into "planning" before persisting tool_result duplicates the output after page refresh.
// sameResponseStreamMeta checks whether this is the same main-channel stream; Eino ADK may emit duplicate response_start events for one MessageStream.
func sameResponseStreamMeta(a, b map[string]interface{}) bool {
	if a == nil || b == nil {
		return false
	}
	agentA, _ := a["einoAgent"].(string)
	agentB, _ := b["einoAgent"].(string)
	agentA = strings.TrimSpace(agentA)
	agentB = strings.TrimSpace(agentB)
	if agentA == "" || !strings.EqualFold(agentA, agentB) {
		return false
	}
	orchA, _ := a["orchestration"].(string)
	orchB, _ := b["orchestration"].(string)
	if strings.TrimSpace(orchA) != strings.TrimSpace(orchB) {
		return false
	}
	iterA := responseStreamIterationFromMeta(a)
	iterB := responseStreamIterationFromMeta(b)
	if iterA != 0 && iterB != 0 && iterA != iterB {
		return false
	}
	streamA, _ := a["streamId"].(string)
	streamB, _ := b["streamId"].(string)
	streamA = strings.TrimSpace(streamA)
	streamB = strings.TrimSpace(streamB)
	if streamA != "" && streamB != "" && streamA != streamB {
		return false
	}
	return true
}

func responseStreamIterationFromMeta(m map[string]interface{}) int {
	if m == nil {
		return 0
	}
	switch v := m["iteration"].(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func discardPlanningIfEchoesToolResult(respPlan *responsePlanAgg, toolData interface{}) {
	if respPlan == nil {
		return
	}
	plan := normalizeProcessDetailText(respPlan.b.String())
	if plan == "" {
		return
	}
	dataMap, ok := toolData.(map[string]interface{})
	if !ok {
		return
	}
	res, ok := dataMap["result"].(string)
	if !ok {
		return
	}
	r := normalizeProcessDetailText(res)
	if r == "" {
		return
	}
	if plan == r || strings.HasSuffix(plan, r) {
		respPlan.meta = nil
		respPlan.b.Reset()
	}
}

// AgentHandler Agent handler
type AgentHandler struct {
	agent            *agent.Agent
	db               *database.DB
	logger           *zap.Logger
	tasks            *AgentTaskManager
	taskEventBus     *TaskEventBus // Mirrors SSE events so refreshes can subscribe to the same running task
	batchTaskManager *BatchTaskManager
	hitlManager      *HITLManager
	config           *config.Config // Configuration reference used to read role information
	knowledgeManager interface {    // Knowledge base manager interface
		LogRetrieval(conversationID, messageID, query, riskType string, retrievedItems []string) error
	}
	agentsMarkdownDir string // Multi-agent Markdown sub-agent directory; absolute path, empty means do not merge from disk
	batchCronParser   cron.Parser
	batchRunnerMu     sync.Mutex
	batchRunning      map[string]struct{}
	// hitlWhitelistSaver Merges the conversation-level HITL whitelist into config.yaml when applying HITL from the sidebar; optional
	hitlWhitelistSaver HitlToolWhitelistSaver
	audit              *audit.Service
}

// SetAudit wires platform audit logging.
func (h *AgentHandler) SetAudit(s *audit.Service) {
	h.audit = s
}

// HitlToolWhitelistSaver merges HITL approval-free tools into the global configuration and persists them
type HitlToolWhitelistSaver interface {
	MergeHitlToolWhitelistIntoConfig(add []string) error
}

// NewAgentHandler creates a new Agent handler
func NewAgentHandler(agent *agent.Agent, db *database.DB, cfg *config.Config, logger *zap.Logger) *AgentHandler {
	batchTaskManager := NewBatchTaskManager(logger)
	batchTaskManager.SetDB(db)

	// Load all batch task queues from the database
	if err := batchTaskManager.LoadFromDB(); err != nil {
		logger.Warn("Failed to load batch task queues from database", zap.Error(err))
	}

	bus := NewTaskEventBus()
	tm := NewAgentTaskManager()
	tm.SetTaskEventBus(bus)
	handler := &AgentHandler{
		agent:            agent,
		db:               db,
		logger:           logger,
		tasks:            tm,
		taskEventBus:     bus,
		batchTaskManager: batchTaskManager,
		config:           cfg,
		hitlManager:      NewHITLManager(db, logger),
		batchCronParser:  cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor),
		batchRunning:     make(map[string]struct{}),
	}
	if err := handler.hitlManager.EnsureSchema(); err != nil {
		logger.Warn("Failed to initialize HITL tables", zap.Error(err))
	}
	go handler.batchQueueSchedulerLoop()
	return handler
}

// SetKnowledgeManager sets the knowledge base manager used to record retrieval logs
func (h *AgentHandler) SetKnowledgeManager(manager interface {
	LogRetrieval(conversationID, messageID, query, riskType string, retrievedItems []string) error
}) {
	h.knowledgeManager = manager
}

// SetAgentsMarkdownDir sets the agents/*.md sub-agent directory; absolute path. Empty means only use sub_agents from config.yaml.
func (h *AgentHandler) SetAgentsMarkdownDir(absDir string) {
	h.agentsMarkdownDir = strings.TrimSpace(absDir)
}

// SetHitlToolWhitelistSaver sets HITL whitelist persistence; uses an interface to cooperate with ConfigHandler without a circular dependency
func (h *AgentHandler) SetHitlToolWhitelistSaver(s HitlToolWhitelistSaver) {
	h.hitlWhitelistSaver = s
}

// HITLNeedsToolApproval for C2 dangerous-task gating; matches conversation-side HITL and approval-free whitelist checks.
func (h *AgentHandler) HITLNeedsToolApproval(conversationID, toolName string) bool {
	if h == nil || h.hitlManager == nil {
		return false
	}
	return h.hitlManager.NeedsToolApproval(conversationID, toolName)
}

// ChatAttachment chat attachment uploaded by the user
type ChatAttachment struct {
	FileName   string `json:"fileName"`          // Display file name
	Content    string `json:"content,omitempty"` // Text or base64; may be empty if already uploaded to the server
	MimeType   string `json:"mimeType,omitempty"`
	ServerPath string `json:"serverPath,omitempty"` // Absolute path already saved under chat_uploads, returned by POST /api/chat-uploads
}

// ChatReasoningRequest is the conversation-page model reasoning intent consumed by Eino single-agent and multi-agent paths.
type ChatReasoningRequest struct {
	// Mode: default（follow system settings）| off | on | auto
	Mode string `json:"mode,omitempty"`
	// Effort: low | medium | high | max | xhigh（passed through unchanged; different gateways use different names for the highest tier）。empty means unspecified。
	Effort string `json:"effort,omitempty"`
}

// ChatRequest chat request
type ChatRequest struct {
	Message              string                `json:"message" binding:"required"`
	ConversationID       string                `json:"conversationId,omitempty"`
	ProjectID            string                `json:"projectId,omitempty"` // Project bound to a new conversation; optional, config.project.default_project_id may be used when omitted
	Role                 string                `json:"role,omitempty"`      // Role name
	Attachments          []ChatAttachment      `json:"attachments,omitempty"`
	WebShellConnectionID string                `json:"webshellConnectionId,omitempty"` // WebShell management - AI assistant: currently selected connection ID; only webshell_* tools are used
	Hitl                 *HITLRequest          `json:"hitl,omitempty"`
	Reasoning            *ChatReasoningRequest `json:"reasoning,omitempty"`
	// Orchestration Only for /api/multi-agent and /api/multi-agent/stream: deep | plan_execute | supervisor. Empty is equivalent to deep. For robots/batch flows without a request body, the server defaults to deep. /api/eino-agent* does not use this field.
	Orchestration string `json:"orchestration,omitempty"`
}

func chatReasoningToClientIntent(r *ChatReasoningRequest) *reasoning.ClientIntent {
	if r == nil {
		return nil
	}
	return &reasoning.ClientIntent{Mode: r.Mode, Effort: r.Effort}
}

type HITLRequest struct {
	Enabled        bool     `json:"enabled"`
	Mode           string   `json:"mode,omitempty"`
	SensitiveTools []string `json:"sensitiveTools,omitempty"`
	TimeoutSeconds int      `json:"timeoutSeconds,omitempty"`
}

const (
	maxAttachments     = 10
	chatUploadsDirName = "chat_uploads" // Root directory for saved chat attachments, relative to the current working directory
)

// validateChatAttachmentServerPath validates that an absolute path is a regular file under the workspace chat_uploads directory, preventing path traversal
func validateChatAttachmentServerPath(abs string) (string, error) {
	p := strings.TrimSpace(abs)
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("Failed to get current working directory: %w", err)
	}
	root := filepath.Join(cwd, chatUploadsDirName)
	rootAbs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", err
	}
	pathAbs, err := filepath.Abs(filepath.Clean(p))
	if err != nil {
		return "", err
	}
	sep := string(filepath.Separator)
	if pathAbs != rootAbs && !strings.HasPrefix(pathAbs, rootAbs+sep) {
		return "", fmt.Errorf("path outside chat_uploads")
	}
	st, err := os.Stat(pathAbs)
	if err != nil {
		return "", err
	}
	if st.IsDir() {
		return "", fmt.Errorf("not a regular file")
	}
	return pathAbs, nil
}

// avoidChatUploadDestCollision generates a timestamped random-suffix file name if path already exists, matching the upload API naming style
func avoidChatUploadDestCollision(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	nameNoExt := strings.TrimSuffix(base, ext)
	suffix := fmt.Sprintf("_%s_%s", time.Now().Format("150405"), shortRand(6))
	var unique string
	if ext != "" {
		unique = nameNoExt + suffix + ext
	} else {
		unique = base + suffix
	}
	return filepath.Join(dir, unique)
}

// relocateManualOrNewUploadToConversation when there is no conversation ID, the frontend uploads to .../date/_manual; after the first message creates a conversation, move the file into .../date/{conversationId}/ to isolate by conversation.
func relocateManualOrNewUploadToConversation(absPath, conversationID string, logger *zap.Logger) (string, error) {
	conv := strings.TrimSpace(conversationID)
	if conv == "" {
		return absPath, nil
	}
	convSan := strings.ReplaceAll(conv, string(filepath.Separator), "_")
	if convSan == "" || convSan == "_manual" || convSan == "_new" {
		return absPath, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return absPath, err
	}
	rootAbs, err := filepath.Abs(filepath.Join(cwd, chatUploadsDirName))
	if err != nil {
		return absPath, err
	}
	rel, err := filepath.Rel(rootAbs, absPath)
	if err != nil {
		return absPath, nil
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	var segs []string
	for _, p := range strings.Split(rel, "/") {
		if p != "" && p != "." {
			segs = append(segs, p)
		}
	}
	// Only handle the flat date/_manual|_new/fileName structure
	if len(segs) != 3 {
		return absPath, nil
	}
	datePart, placeFolder, baseName := segs[0], segs[1], segs[2]
	if placeFolder != "_manual" && placeFolder != "_new" {
		return absPath, nil
	}
	targetDir := filepath.Join(rootAbs, datePart, convSan)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", fmt.Errorf("Failed to create conversation attachment directory: %w", err)
	}
	dest := filepath.Join(targetDir, baseName)
	dest = avoidChatUploadDestCollision(dest)
	if err := os.Rename(absPath, dest); err != nil {
		return "", fmt.Errorf("Failed to move attachment into conversation directory: %w", err)
	}
	out, _ := filepath.Abs(dest)
	if logger != nil {
		logger.Info("Moved chat attachment from placeholder directory into conversation directory",
			zap.String("from", absPath),
			zap.String("to", out),
			zap.String("conversationId", conv))
	}
	return out, nil
}

// saveAttachmentsToDateAndConversationDir handles attachments: if serverPath is present, only validate the existing file; otherwise write content to chat_uploads/YYYY-MM-DD/{conversationID}/.
// conversationID is empty, use "_new" as the directory name because a new conversation does not have an ID yet
func saveAttachmentsToDateAndConversationDir(attachments []ChatAttachment, conversationID string, logger *zap.Logger) (savedPaths []string, err error) {
	if len(attachments) == 0 {
		return nil, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("Failed to get current working directory: %w", err)
	}
	dateDir := filepath.Join(cwd, chatUploadsDirName, time.Now().Format("2006-01-02"))
	convDirName := strings.TrimSpace(conversationID)
	if convDirName == "" {
		convDirName = "_new"
	} else {
		convDirName = strings.ReplaceAll(convDirName, string(filepath.Separator), "_")
	}
	targetDir := filepath.Join(dateDir, convDirName)
	if err = os.MkdirAll(targetDir, 0755); err != nil {
		return nil, fmt.Errorf("Failed to create upload directory: %w", err)
	}
	savedPaths = make([]string, 0, len(attachments))
	for i, a := range attachments {
		if sp := strings.TrimSpace(a.ServerPath); sp != "" {
			valid, verr := validateChatAttachmentServerPath(sp)
			if verr != nil {
				return nil, fmt.Errorf("Attachment %s: %w", a.FileName, verr)
			}
			finalPath, rerr := relocateManualOrNewUploadToConversation(valid, conversationID, logger)
			if rerr != nil {
				return nil, fmt.Errorf("Attachment %s: %w", a.FileName, rerr)
			}
			savedPaths = append(savedPaths, finalPath)
			if logger != nil {
				logger.Debug("Chat attachment uses existing uploaded path", zap.Int("index", i+1), zap.String("fileName", a.FileName), zap.String("path", finalPath))
			}
			continue
		}
		if strings.TrimSpace(a.Content) == "" {
			return nil, fmt.Errorf("Attachment %s is missing content or serverPath was not provided", a.FileName)
		}
		raw, decErr := attachmentContentToBytes(a)
		if decErr != nil {
			return nil, fmt.Errorf("Attachment %s decode failed: %w", a.FileName, decErr)
		}
		baseName := filepath.Base(a.FileName)
		if baseName == "" || baseName == "." {
			baseName = "file"
		}
		baseName = strings.ReplaceAll(baseName, string(filepath.Separator), "_")
		ext := filepath.Ext(baseName)
		nameNoExt := strings.TrimSuffix(baseName, ext)
		suffix := fmt.Sprintf("_%s_%s", time.Now().Format("150405"), shortRand(6))
		var unique string
		if ext != "" {
			unique = nameNoExt + suffix + ext
		} else {
			unique = baseName + suffix
		}
		fullPath := filepath.Join(targetDir, unique)
		if err = os.WriteFile(fullPath, raw, 0644); err != nil {
			return nil, fmt.Errorf("Failed to write file %s failed: %w", a.FileName, err)
		}
		absPath, _ := filepath.Abs(fullPath)
		savedPaths = append(savedPaths, absPath)
		if logger != nil {
			logger.Debug("Chat attachment saved", zap.Int("index", i+1), zap.String("fileName", a.FileName), zap.String("path", absPath))
		}
	}
	return savedPaths, nil
}

func shortRand(n int) string {
	const letters = "0123456789abcdef"
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = letters[int(b[i])%len(letters)]
	}
	return string(b)
}

func attachmentContentToBytes(a ChatAttachment) ([]byte, error) {
	content := a.Content
	if decoded, err := base64.StdEncoding.DecodeString(content); err == nil && len(decoded) > 0 {
		return decoded, nil
	}
	return []byte(content), nil
}

// userMessageContentForStorage returns user-message content to store in the database; with attachments, append attachment names and paths so refreshes still display them and later turns expose paths through history
func userMessageContentForStorage(message string, attachments []ChatAttachment, savedPaths []string) string {
	if len(attachments) == 0 {
		return message
	}
	var b strings.Builder
	b.WriteString(message)
	for i, a := range attachments {
		b.WriteString("\n📎 ")
		b.WriteString(a.FileName)
		if i < len(savedPaths) && savedPaths[i] != "" {
			b.WriteString(": ")
			b.WriteString(savedPaths[i])
		}
	}
	return b.String()
}

// appendAttachmentsToMessage appends only saved attachment paths to the user message and no longer inlines attachment content, avoiding excessive context
func appendAttachmentsToMessage(msg string, attachments []ChatAttachment, savedPaths []string) string {
	if len(attachments) == 0 {
		return msg
	}
	var b strings.Builder
	b.WriteString(msg)
	b.WriteString("\n\n[User-uploaded files have been saved at these paths; read file content as needed instead of relying on inline content]\n")
	for i, a := range attachments {
		if i < len(savedPaths) && savedPaths[i] != "" {
			b.WriteString(fmt.Sprintf("- %s: %s\n", a.FileName, savedPaths[i]))
		} else {
			b.WriteString(fmt.Sprintf("- %s: （path unknown; save may have failed）\n", a.FileName))
		}
	}
	return b.String()
}

// appendAssistantMessageNotice appends a notice to the end of an assistant message without overwriting generated content.
// If the message is empty, write the notice directly; if it already contains the same notice, leave it unchanged.
func (h *AgentHandler) appendAssistantMessageNotice(messageID, notice string) error {
	trimmedNotice := strings.TrimSpace(notice)
	if strings.TrimSpace(messageID) == "" || trimmedNotice == "" {
		return nil
	}
	_, err := h.db.Exec(
		`UPDATE messages
		 SET content = CASE
			WHEN content IS NULL OR TRIM(content) = '' THEN ?
			WHEN INSTR(content, ?) > 0 THEN content
			ELSE content || '\n\n' || ?
		 END,
		     updated_at = ?
		 WHERE id = ?`,
		trimmedNotice,
		trimmedNotice,
		trimmedNotice,
		time.Now(),
		messageID,
	)
	return err
}

// mergeAssistantMessagePartialOnCancel merges the partial response generated before cancellation into the message as best as possible:
// - content is empty or only a placeholder (processing...), replace it directly with partial;
// - when content already exists, append only if it does not already contain partial to avoid loss and duplication.
func (h *AgentHandler) mergeAssistantMessagePartialOnCancel(messageID, partial string) error {
	trimmedPartial := strings.TrimSpace(partial)
	if strings.TrimSpace(messageID) == "" || trimmedPartial == "" {
		return nil
	}
	_, err := h.db.Exec(
		`UPDATE messages
		 SET content = CASE
			WHEN content IS NULL OR TRIM(content) = '' OR TRIM(content) = '\u5904\u7406\u4e2d...' THEN ?
			WHEN INSTR(content, ?) > 0 THEN content
			ELSE content || '\n\n' || ?
		 END,
		     updated_at = ?
		 WHERE id = ?`,
		trimmedPartial,
		trimmedPartial,
		trimmedPartial,
		time.Now(),
		messageID,
	)
	return err
}

// ChatResponse chat response
type ChatResponse struct {
	Response        string    `json:"response"`
	MCPExecutionIDs []string  `json:"mcpExecutionIds,omitempty"` // MCP call ID list executed in this chat
	ConversationID  string    `json:"conversationId"`            // Conversation ID
	Time            time.Time `json:"time"`
}

func (h *AgentHandler) finalizeRobotAgentError(ctx context.Context, assistantMessageID, conversationID string, resultMA *multiagent.RunResult, errMA error) (string, string, error) {
	if shouldPersistEinoAgentTraceAfterRunError(ctx) {
		h.persistEinoAgentTraceForResume(conversationID, resultMA)
	}
	errMsg := "Execution failed: " + errMA.Error()
	if assistantMessageID != "" {
		_, _ = h.db.Exec("UPDATE messages SET content = ?, updated_at = ? WHERE id = ?", errMsg, time.Now(), assistantMessageID)
		_ = h.db.AddProcessDetail(assistantMessageID, conversationID, "error", errMsg, nil)
	}
	return "", conversationID, errMA
}

func (h *AgentHandler) finalizeRobotAgentSuccess(assistantMessageID, conversationID string, resultMA *multiagent.RunResult) (string, string, error) {
	if assistantMessageID != "" {
		if errU := h.db.UpdateAssistantMessageFinalize(assistantMessageID, resultMA.Response, resultMA.MCPExecutionIDs, multiagent.AggregatedReasoningFromTraceJSON(resultMA.LastAgentTraceInput)); errU != nil {
			h.logger.Warn("Robot: failed to update assistant message", zap.Error(errU))
		}
	} else {
		if _, err := h.db.AddMessage(conversationID, "assistant", resultMA.Response, resultMA.MCPExecutionIDs); err != nil {
			h.logger.Warn("Robot: failed to save assistant message", zap.Error(err))
		}
	}
	if resultMA.LastAgentTraceInput != "" || resultMA.LastAgentTraceOutput != "" {
		_ = h.db.SaveAgentTrace(conversationID, resultMA.LastAgentTraceInput, resultMA.LastAgentTraceOutput)
	}
	return resultMA.Response, conversationID, nil
}

func (h *AgentHandler) runRobotEinoSingleWithRetry(
	taskCtx context.Context,
	conversationID, finalMessage string,
	history []agent.ChatMessage,
	roleTools []string,
	progressCallback agent.ProgressCallback,
	assistantMessageID string,
	taskStatus *string,
) (string, string, error) {
	curHist := history
	curMsg := finalMessage
	segmentUserMessage := finalMessage
	var resultMA *multiagent.RunResult
	var errMA error
	var transientRunAttempts int
	var emptyResponseAttempts int
	for {
		resultMA, errMA = multiagent.RunEinoSingleChatModelAgent(
			taskCtx, h.config, &h.config.MultiAgent, h.agent, h.logger,
			conversationID, curMsg, curHist, roleTools, progressCallback, nil, h.projectBlackboardBlock(conversationID),
		)
		handledEmpty, exhaustedEmpty := h.handleEinoEmptyResponseContinue(
			taskCtx, conversationID, resultMA, errMA, &emptyResponseAttempts,
			&curHist, &curMsg, segmentUserMessage, progressCallback, nil,
		)
		if exhaustedEmpty {
			errMA = nil
			break
		}
		if handledEmpty {
			continue
		}
		if errMA == nil {
			transientRunAttempts = 0
			emptyResponseAttempts = 0
			break
		}
		if handled, _ := h.handleEinoTransientRetryContinue(
			taskCtx, conversationID, resultMA, errMA, &transientRunAttempts,
			&curHist, &curMsg, segmentUserMessage, progressCallback, nil,
		); handled {
			continue
		}
		*taskStatus = "failed"
		return h.finalizeRobotAgentError(taskCtx, assistantMessageID, conversationID, resultMA, errMA)
	}
	return h.finalizeRobotAgentSuccess(assistantMessageID, conversationID, resultMA)
}

func (h *AgentHandler) runRobotMultiAgentWithRetry(
	taskCtx context.Context,
	conversationID, finalMessage, orchestration string,
	history []agent.ChatMessage,
	roleTools []string,
	progressCallback agent.ProgressCallback,
	assistantMessageID string,
	taskStatus *string,
) (string, string, error) {
	curHist := history
	curMsg := finalMessage
	segmentUserMessage := finalMessage
	var resultMA *multiagent.RunResult
	var errMA error
	var transientRunAttempts int
	var emptyResponseAttempts int
	for {
		resultMA, errMA = multiagent.RunDeepAgent(
			taskCtx, h.config, &h.config.MultiAgent, h.agent, h.logger,
			conversationID, curMsg, curHist, roleTools, progressCallback,
			h.agentsMarkdownDir, orchestration, nil, h.projectBlackboardBlock(conversationID),
		)
		handledEmpty, exhaustedEmpty := h.handleEinoEmptyResponseContinue(
			taskCtx, conversationID, resultMA, errMA, &emptyResponseAttempts,
			&curHist, &curMsg, segmentUserMessage, progressCallback, nil,
		)
		if exhaustedEmpty {
			errMA = nil
			break
		}
		if handledEmpty {
			continue
		}
		if errMA == nil {
			transientRunAttempts = 0
			emptyResponseAttempts = 0
			break
		}
		if handled, _ := h.handleEinoTransientRetryContinue(
			taskCtx, conversationID, resultMA, errMA, &transientRunAttempts,
			&curHist, &curMsg, segmentUserMessage, progressCallback, nil,
		); handled {
			continue
		}
		*taskStatus = "failed"
		return h.finalizeRobotAgentError(taskCtx, assistantMessageID, conversationID, resultMA, errMA)
	}
	return h.finalizeRobotAgentSuccess(assistantMessageID, conversationID, resultMA)
}

// ProcessMessageForRobot is called by robots (WeCom/DingTalk/Lark): it uses the Eino single-agent/multi-agent execution paths with progressCallback and process details, but does not send SSE and returns the full reply at the end.
func (h *AgentHandler) ProcessMessageForRobot(ctx context.Context, platform, conversationID, message, role string) (response string, convID string, err error) {
	if conversationID == "" {
		title := safeTruncateString(message, 50)
		src := "robot"
		if strings.TrimSpace(platform) != "" {
			src = "robot:" + strings.TrimSpace(platform)
		}
		meta := audit.ConversationCreateMeta(src)
		meta.ProjectID = effectiveProjectID(h.config, "")
		conv, createErr := h.db.CreateConversation(title, meta)
		if createErr != nil {
			return "", "", fmt.Errorf("Failed to create conversation: %w", createErr)
		}
		conversationID = conv.ID
	} else {
		if _, getErr := h.db.GetConversation(conversationID); getErr != nil {
			return "", "", fmt.Errorf("Conversation does not exist")
		}
	}

	agentHistoryMessages, err := h.loadHistoryFromAgentTrace(conversationID)
	if err != nil {
		historyMessages, getErr := h.db.GetMessages(conversationID)
		if getErr != nil {
			agentHistoryMessages = []agent.ChatMessage{}
		} else {
			agentHistoryMessages = make([]agent.ChatMessage, 0, len(historyMessages))
			for _, msg := range historyMessages {
				agentHistoryMessages = append(agentHistoryMessages, agent.ChatMessage{Role: msg.Role, Content: msg.Content})
			}
		}
	}

	finalMessage := message
	var roleTools []string
	if role != "" && role != "\u9ed8\u8ba4" && h.config.Roles != nil {
		if r, exists := h.config.Roles[role]; exists && r.Enabled {
			if r.UserPrompt != "" {
				finalMessage = r.UserPrompt + "\n\n" + message
			}
			roleTools = r.Tools
		}
	}

	if _, err = h.db.AddMessage(conversationID, "user", message, nil); err != nil {
		return "", "", fmt.Errorf("Failed to save user message: %w", err)
	}

	// Match Eino streaming conversation behavior: create an assistant message placeholder first and let progressCallback write process details without sending SSE.
	assistantMsg, err := h.db.AddMessage(conversationID, "assistant", "Processing...", nil)
	if err != nil {
		h.logger.Warn("Robot: failed to create assistant message placeholder", zap.Error(err))
	}
	var assistantMessageID string
	if assistantMsg != nil {
		assistantMessageID = assistantMsg.ID
	}

	// Register the running task and mirror progress events to taskEventBus so web task-events can resume the stream.
	taskCtx, cancelWithCause := context.WithCancelCause(ctx)
	defer cancelWithCause(nil)
	taskStatus := "completed"
	defer func() {
		h.tasks.FinishTask(conversationID, taskStatus)
	}()
	if _, err := h.tasks.StartTask(conversationID, message, cancelWithCause); err != nil {
		if errors.Is(err, ErrTaskAlreadyRunning) {
			return "", conversationID, fmt.Errorf("A task is already running in the current conversation; please try again later")
		}
		return "", conversationID, fmt.Errorf("Unable to start task: %w", err)
	}
	progressCallback := h.createProgressCallback(taskCtx, cancelWithCause, conversationID, assistantMessageID, nil)

	robotMode := "eino_single"
	if h.config != nil {
		robotMode = config.NormalizeRobotAgentMode(h.config.MultiAgent)
	}
	switch robotMode {
	case "eino_single":
		return h.runRobotEinoSingleWithRetry(taskCtx, conversationID, finalMessage, agentHistoryMessages, roleTools, progressCallback, assistantMessageID, &taskStatus)
	case "deep", "plan_execute", "supervisor":
		if h.config == nil || !h.config.MultiAgent.Enabled {
			h.logger.Warn("Robot is configured for multi-agent mode but multi_agent is not enabled; falling back to Eino single-agent",
				zap.String("robot_mode", robotMode))
			return h.runRobotEinoSingleWithRetry(taskCtx, conversationID, finalMessage, agentHistoryMessages, roleTools, progressCallback, assistantMessageID, &taskStatus)
		}
		return h.runRobotMultiAgentWithRetry(taskCtx, conversationID, finalMessage, robotMode, agentHistoryMessages, roleTools, progressCallback, assistantMessageID, &taskStatus)
	}

	taskStatus = "failed"
	return "", conversationID, fmt.Errorf("unsupported robot agent mode: %s", robotMode)
}

// StreamEvent stream event
type StreamEvent struct {
	Type    string      `json:"type"`    // conversation, progress, tool_call, tool_result, response, error, cancelled, done
	Message string      `json:"message"` // Display message
	Data    interface{} `json:"data,omitempty"`
}

// publishProgressToTaskEventBus mirrors progress events to taskEventBus for web task-events subscriptions when there is a robot or no HTTP SSE client.
func (h *AgentHandler) publishProgressToTaskEventBus(conversationID, eventType, message string, data interface{}) {
	if h == nil || h.taskEventBus == nil || strings.TrimSpace(conversationID) == "" {
		return
	}
	event := StreamEvent{Type: eventType, Message: message, Data: data}
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return
	}
	sseLine := make([]byte, 0, len(eventJSON)+8)
	sseLine = append(sseLine, []byte("data: ")...)
	sseLine = append(sseLine, eventJSON...)
	sseLine = append(sseLine, '\n', '\n')
	h.taskEventBus.Publish(conversationID, sseLine)
}

// createProgressCallback creates the progress callback used to save processDetails
// sendEventFunc: optional streaming event sender; if nil, no streaming event is sent
func (h *AgentHandler) createProgressCallback(runCtx context.Context, cancelRun context.CancelCauseFunc, conversationID, assistantMessageID string, sendEventFunc func(eventType, message string, data interface{})) agent.ProgressCallback {
	// Used to save tool_call arguments for use when tool_result arrives
	toolCallCache := make(map[string]map[string]interface{}) // toolCallId -> arguments
	skillCallCache := make(map[string]string)                // toolCallId -> skillName
	skillToolName := "skill"
	if h.config != nil {
		if customName := strings.TrimSpace(h.config.MultiAgent.EinoSkills.SkillToolName); customName != "" {
			skillToolName = customName
		}
	}

	extractSkillName := func(args map[string]interface{}) string {
		if len(args) == 0 {
			return ""
		}
		for _, key := range []string{"skill_name", "skillName", "name", "skill", "id", "skill_id", "skillId"} {
			if v, ok := args[key]; ok {
				switch vv := v.(type) {
				case string:
					if s := strings.TrimSpace(vv); s != "" {
						return s
					}
				case map[string]interface{}:
					for _, nestedKey := range []string{"name", "id", "skill_name", "skillId"} {
						if nestedV, nestedOK := vv[nestedKey].(string); nestedOK {
							if s := strings.TrimSpace(nestedV); s != "" {
								return s
							}
						}
					}
				}
			}
		}
		return ""
	}

	// thinking_stream_* (ReAct and other assistant content streams) and reasoning_chain_stream_* (Eino ReasoningContent):
	// Do not persist each event; aggregate by streamId and persist as thinking / reasoning_chain on flush.
	type thinkingBuf struct {
		b         strings.Builder
		meta      map[string]interface{}
		persistAs string // "thinking" | "reasoning_chain"
	}
	thinkingStreams := make(map[string]*thinkingBuf) // streamId -> buf
	flushedThinking := make(map[string]bool)         // streamId -> flushed
	seenToolCallSigs := make(map[string]string)      // toolCallId -> payload signature
	seenToolResultSigs := make(map[string]string)    // toolCallId -> payload signature

	// progressMu protects closure map and aggregation state. Eino parallelRunToolCall triggers concurrent progress callbacks
	// (ToolInvokeNotifyHolder.Fire → createProgressCallback); unlocked map access causes fatal panic.
	var progressMu sync.Mutex

	// response_start + response_delta: frontend timeline displays this as planning in monitor.js; do not persist each delta;
	// aggregate into one planning row in process_details so refresh matches the live stream.
	var respPlan responsePlanAgg
	flushResponsePlan := func() {
		if assistantMessageID == "" {
			return
		}
		content := strings.TrimSpace(respPlan.b.String())
		if content == "" {
			respPlan.meta = nil
			respPlan.b.Reset()
			return
		}
		data := map[string]interface{}{
			"source": "response_stream",
		}
		for k, v := range respPlan.meta {
			data[k] = v
		}
		if err := h.db.AddProcessDetail(assistantMessageID, conversationID, "planning", content, data); err != nil {
			h.logger.Warn("Failed to save process details", zap.Error(err), zap.String("eventType", "planning"))
		}
		respPlan.meta = nil
		respPlan.b.Reset()
	}

	flushThinkingStreams := func() {
		if assistantMessageID == "" {
			return
		}
		for sid, tb := range thinkingStreams {
			if sid == "" || flushedThinking[sid] || tb == nil {
				continue
			}
			content := strings.TrimSpace(tb.b.String())
			if content == "" {
				flushedThinking[sid] = true
				continue
			}
			data := map[string]interface{}{
				"streamId": sid,
			}
			for k, v := range tb.meta {
				// Avoid overwriting streamId
				if k == "streamId" {
					continue
				}
				data[k] = v
			}
			persist := tb.persistAs
			if persist != "reasoning_chain" {
				persist = "thinking"
			}
			if err := h.db.AddProcessDetail(assistantMessageID, conversationID, persist, content, data); err != nil {
				h.logger.Warn("Failed to save process details", zap.Error(err), zap.String("eventType", persist))
			}
			flushedThinking[sid] = true
		}
	}

	return func(eventType, message string, data interface{}) {
		progressMu.Lock()
		defer progressMu.Unlock()

		// Upstream retries or compensation may call back the same tool_call/tool_result more than once.
		// Apply idempotency filtering here so frontend display and process_details are based on unique events.
		if (eventType == "tool_call" || eventType == "tool_result") && data != nil {
			if dataMap, ok := data.(map[string]interface{}); ok {
				toolCallID := strings.TrimSpace(fmt.Sprint(dataMap["toolCallId"]))
				if toolCallID != "" && toolCallID != "<nil>" {
					payloadJSON, _ := json.Marshal(dataMap)
					sig := eventType + "|" + message + "|" + string(payloadJSON)
					seen := seenToolCallSigs
					if eventType == "tool_result" {
						seen = seenToolResultSigs
					}
					if prev, exists := seen[toolCallID]; exists && prev == sig {
						h.logger.Debug("Skipping duplicate tool progress event",
							zap.String("eventType", eventType),
							zap.String("toolCallId", toolCallID))
						return
					}
					seen[toolCallID] = sig
				}
			}
		}

		// Streaming writes HTTP SSE; non-streaming, such as robots, mirrors to taskEventBus for web subscriptions
		if sendEventFunc != nil {
			sendEventFunc(eventType, message, data)
		} else {
			h.publishProgressToTaskEventBus(conversationID, eventType, message, data)
		}

		// Save arguments from tool_call events
		if eventType == "tool_call" {
			if dataMap, ok := data.(map[string]interface{}); ok {
				toolName, _ := dataMap["toolName"].(string)
				if toolName == builtin.ToolSearchKnowledgeBase {
					if toolCallId, ok := dataMap["toolCallId"].(string); ok && toolCallId != "" {
						if argumentsObj, ok := dataMap["argumentsObj"].(map[string]interface{}); ok {
							toolCallCache[toolCallId] = argumentsObj
						}
					}
				}
				if strings.EqualFold(strings.TrimSpace(toolName), skillToolName) {
					toolCallID, _ := dataMap["toolCallId"].(string)
					if toolCallID != "" {
						if argumentsObj, ok := dataMap["argumentsObj"].(map[string]interface{}); ok {
							if skillName := extractSkillName(argumentsObj); skillName != "" {
								skillCallCache[toolCallID] = skillName
							}
						}
					}
				}
			}
		}

		// Handle knowledge retrieval logging
		if eventType == "tool_result" && h.knowledgeManager != nil {
			if dataMap, ok := data.(map[string]interface{}); ok {
				toolName, _ := dataMap["toolName"].(string)
				if toolName == builtin.ToolSearchKnowledgeBase {
					// Extract retrieval information
					query := ""
					riskType := ""
					var retrievedItems []string

					// First try to get arguments from the tool_call cache
					if toolCallId, ok := dataMap["toolCallId"].(string); ok && toolCallId != "" {
						if cachedArgs, exists := toolCallCache[toolCallId]; exists {
							if q, ok := cachedArgs["query"].(string); ok && q != "" {
								query = q
							}
							if rt, ok := cachedArgs["risk_type"].(string); ok && rt != "" {
								riskType = rt
							}
							// Clear cache after use
							delete(toolCallCache, toolCallId)
						}
					}

					// If not found in cache, try extracting from argumentsObj
					if query == "" {
						if arguments, ok := dataMap["argumentsObj"].(map[string]interface{}); ok {
							if q, ok := arguments["query"].(string); ok && q != "" {
								query = q
							}
							if rt, ok := arguments["risk_type"].(string); ok && rt != "" {
								riskType = rt
							}
						}
					}

					// If query is still empty, try extracting it from result, using the first line of result text
					if query == "" {
						if result, ok := dataMap["result"].(string); ok && result != "" {
							// Try to extract query content from result if it contains"No knowledge found for query 'xxx' related knowledge"）
							if strings.Contains(result, "No knowledge found for query '") {
								start := strings.Index(result, "No knowledge found for query '") + len("No knowledge found for query '")
								end := strings.Index(result[start:], "'")
								if end > 0 {
									query = result[start : start+end]
								}
							}
						}
						// If still empty, use the default value
						if query == "" {
							query = "unknown query"
						}
					}

					// Extract retrieved knowledge item IDs from the tool result
					// Result format："Found X related knowledge items：\n\n--- Result 1 (similarity: XX.XX%) ---\nSource: [category] title\n...\n<!-- METADATA: {...} -->"
					if result, ok := dataMap["result"].(string); ok && result != "" {
						// Try to extract knowledge item IDs from metadata
						metadataMatch := strings.Index(result, "<!-- METADATA:")
						if metadataMatch > 0 {
							// Extract metadata JSON
							metadataStart := metadataMatch + len("<!-- METADATA: ")
							metadataEnd := strings.Index(result[metadataStart:], " -->")
							if metadataEnd > 0 {
								metadataJSON := result[metadataStart : metadataStart+metadataEnd]
								var metadata map[string]interface{}
								if err := json.Unmarshal([]byte(metadataJSON), &metadata); err == nil {
									if meta, ok := metadata["_metadata"].(map[string]interface{}); ok {
										if ids, ok := meta["retrievedItemIDs"].([]interface{}); ok {
											retrievedItems = make([]string, 0, len(ids))
											for _, id := range ids {
												if idStr, ok := id.(string); ok {
													retrievedItems = append(retrievedItems, idStr)
												}
											}
										}
									}
								}
							}
						}

						// If no metadata IDs were extracted but result contains"Found X items"，at least mark that results exist
						if len(retrievedItems) == 0 && strings.Contains(result, "Found") && !strings.Contains(result, "Not found") {
							// There are results, but IDs could not be extracted accurately; use a special marker
							retrievedItems = []string{"_has_results"}
						}
					}

					// Record retrieval logs asynchronously without blocking
					go func() {
						if err := h.knowledgeManager.LogRetrieval(conversationID, assistantMessageID, query, riskType, retrievedItems); err != nil {
							h.logger.Warn("Failed to record knowledge retrieval log", zap.Error(err))
						}
					}()

					// Add a knowledge retrieval event to processDetails
					if assistantMessageID != "" {
						retrievalData := map[string]interface{}{
							"query":    query,
							"riskType": riskType,
							"toolName": toolName,
						}
						if err := h.db.AddProcessDetail(assistantMessageID, conversationID, "knowledge_retrieval", fmt.Sprintf("Retrieve knowledge: %s", query), retrievalData); err != nil {
							h.logger.Warn("Failed to save knowledge retrieval details", zap.Error(err))
						}
					}
				}
			}
		}

		// Record skill call statistics by associating tool_call and tool_result
		if eventType == "tool_result" && h.db != nil {
			if dataMap, ok := data.(map[string]interface{}); ok {
				toolName, _ := dataMap["toolName"].(string)
				if strings.EqualFold(strings.TrimSpace(toolName), skillToolName) {
					toolCallID, _ := dataMap["toolCallId"].(string)
					skillName := ""
					if toolCallID != "" {
						skillName = strings.TrimSpace(skillCallCache[toolCallID])
						delete(skillCallCache, toolCallID)
					}
					if skillName == "" {
						if argumentsObj, ok := dataMap["argumentsObj"].(map[string]interface{}); ok {
							skillName = strings.TrimSpace(extractSkillName(argumentsObj))
						}
					}
					if skillName != "" {
						success, ok := dataMap["success"].(bool)
						if !ok {
							if isError, okErr := dataMap["isError"].(bool); okErr {
								success = !isError
							}
						}
						successCalls := 0
						failedCalls := 0
						if success {
							successCalls = 1
						} else {
							failedCalls = 1
						}
						now := time.Now()
						if err := h.db.UpdateSkillStats(skillName, 1, successCalls, failedCalls, &now); err != nil {
							h.logger.Warn("Failed to update skill call statistics", zap.Error(err), zap.String("skill", skillName))
						}
					}
				}
			}
		}

		// Do not persist sub-agent reply streaming deltas; merge them into one eino_agent_reply at the end
		if assistantMessageID != "" && eventType == "eino_agent_reply_stream_end" {
			flushResponsePlan()
			// Ensure thinking streams are persisted before the sub-agent reply so they are readable after refresh
			flushThinkingStreams()
			if err := h.db.AddProcessDetail(assistantMessageID, conversationID, "eino_agent_reply", message, data); err != nil {
				h.logger.Warn("Failed to save process details", zap.Error(err), zap.String("eventType", eventType))
			}
			return
		}

		// Multi-agent main-agent planning: response_start / response_delta are only for SSE; aggregate them into one planning row
		if eventType == "response_start" {
			if dataMap, ok := data.(map[string]interface{}); ok {
				if sameResponseStreamMeta(respPlan.meta, dataMap) {
					if respPlan.meta == nil {
						respPlan.meta = make(map[string]interface{}, len(dataMap))
					}
					for k, v := range dataMap {
						respPlan.meta[k] = v
					}
					return
				}
			}
			flushResponsePlan()
			respPlan.meta = nil
			if dataMap, ok := data.(map[string]interface{}); ok {
				respPlan.meta = make(map[string]interface{}, len(dataMap))
				for k, v := range dataMap {
					respPlan.meta[k] = v
				}
			}
			respPlan.b.Reset()
			return
		}
		if eventType == "response_delta" {
			if dataMap, ok := data.(map[string]interface{}); ok {
				if acc, okAcc := dataMap[openai.SSEAccumulatedKey].(string); okAcc {
					respPlan.b.Reset()
					respPlan.b.WriteString(acc)
				} else {
					respPlan.b.WriteString(message)
				}
			} else {
				respPlan.b.WriteString(message)
			}
			if dataMap, ok := data.(map[string]interface{}); ok && respPlan.meta == nil {
				respPlan.meta = make(map[string]interface{}, len(dataMap))
				for k, v := range dataMap {
					respPlan.meta[k] = v
				}
			} else if dataMap, ok := data.(map[string]interface{}); ok {
				for k, v := range dataMap {
					respPlan.meta[k] = v
				}
			}
			return
		}
		if eventType == "response" {
			flushResponsePlan()
			return
		}

		// Aggregate thinking_stream_* / reasoning_chain_stream_* and do not persist each event
		if eventType == "thinking_stream_start" || eventType == "reasoning_chain_stream_start" {
			persistAs := "thinking"
			if eventType == "reasoning_chain_stream_start" {
				persistAs = "reasoning_chain"
			}
			if dataMap, ok := data.(map[string]interface{}); ok {
				if sid, ok2 := dataMap["streamId"].(string); ok2 && sid != "" {
					tb := thinkingStreams[sid]
					if tb == nil {
						tb = &thinkingBuf{meta: map[string]interface{}{}, persistAs: persistAs}
						thinkingStreams[sid] = tb
					} else {
						tb.persistAs = persistAs
					}
					// Record metadata such as source, einoAgent, einoRole, iteration, etc.
					for k, v := range dataMap {
						tb.meta[k] = v
					}
				}
			}
			return
		}
		if eventType == "thinking_stream_delta" || eventType == "reasoning_chain_stream_delta" {
			persistAs := "thinking"
			if eventType == "reasoning_chain_stream_delta" {
				persistAs = "reasoning_chain"
			}
			if dataMap, ok := data.(map[string]interface{}); ok {
				if sid, ok2 := dataMap["streamId"].(string); ok2 && sid != "" {
					tb := thinkingStreams[sid]
					if tb == nil {
						tb = &thinkingBuf{meta: map[string]interface{}{}, persistAs: persistAs}
						thinkingStreams[sid] = tb
					} else if tb.persistAs == "" {
						tb.persistAs = persistAs
					}
					if acc, okAcc := dataMap[openai.SSEAccumulatedKey].(string); okAcc {
						tb.b.Reset()
						tb.b.WriteString(acc)
					} else {
						tb.b.WriteString(message)
					}
					// Sometimes delta arrives before start; fill in metadata
					for k, v := range dataMap {
						tb.meta[k] = v
					}
				}
			}
			return
		}

		// When the agent sends both *_stream_* and thinking/reasoning_chain with the same streamId,
		// stream aggregation will already persist it in flushThinkingStreams(); skip duplicate per-event persistence here.
		if eventType == "thinking" || eventType == "reasoning_chain" {
			if dataMap, ok := data.(map[string]interface{}); ok {
				if sid, ok2 := dataMap["streamId"].(string); ok2 && sid != "" {
					if tb, exists := thinkingStreams[sid]; exists && tb != nil {
						if strings.TrimSpace(tb.b.String()) != "" {
							return
						}
					}
					if flushedThinking[sid] {
						return
					}
				}
			}
		}

		// Save process details to the database, excluding response/done because response content is already in messages
		// response_start/response_delta has already been aggregated into planning; do not persist each event.
		if assistantMessageID != "" &&
			eventType != "response" &&
			eventType != "done" &&
			eventType != "response_start" &&
			eventType != "response_delta" &&
			eventType != "tool_result_delta" &&
			eventType != "eino_trace_run" &&
			eventType != "eino_trace_start" &&
			eventType != "eino_trace_end" &&
			eventType != "eino_trace_error" &&
			eventType != "eino_agent_reply_stream_start" &&
			eventType != "eino_agent_reply_stream_delta" &&
			eventType != "eino_agent_reply_stream_end" {
			if eventType == "tool_result" {
				discardPlanningIfEchoesToolResult(&respPlan, data)
			}
			// Before persisting key process events, first persist planning and aggregated thinking / reasoning_chain streams
			flushResponsePlan()
			flushThinkingStreams()
			if err := h.db.AddProcessDetail(assistantMessageID, conversationID, eventType, message, data); err != nil {
				h.logger.Warn("Failed to save process details", zap.Error(err), zap.String("eventType", eventType))
			}
		}
	}
}

// CancelAgentLoop cancels a running task.
func (h *AgentHandler) CancelAgentLoop(c *gin.Context) {
	var req struct {
		ConversationID string `json:"conversationId" binding:"required"`
		Reason         string `json:"reason,omitempty"`
		ContinueAfter  bool   `json:"continueAfter,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.ContinueAfter {
		if h.tasks.GetTask(req.ConversationID) == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "No running task found"})
			return
		}
		execID := h.tasks.ActiveMCPExecutionID(req.ConversationID)
		note := strings.TrimSpace(req.Reason)
		if execID != "" {
			if !h.agent.CancelMCPToolExecutionWithNote(execID, note) {
				c.JSON(http.StatusNotFound, gin.H{"error": "No running tool execution was found, or that call has already ended"})
				return
			}
			h.logger.Info("Conversation page terminates only the current MCP tool",
				zap.String("conversationId", req.ConversationID),
				zap.String("executionId", execID),
				zap.Bool("hasNote", note != ""),
			)
			c.JSON(http.StatusOK, gin.H{
				"status":              "tool_abort_requested",
				"conversationId":      req.ConversationID,
				"executionId":         execID,
				"message":             "Requested termination of the current tool call. After the tool returns, this reasoning turn will continue, matching termination from the MCP monitor page.",
				"continueAfter":       true,
				"interruptWithNote":   note != "",
				"continueWithoutTool": false,
			})
			return
		}
		// No MCP tool is running; during pure model reasoning or streaming output, cancel the current context and let the Eino streaming handler merge the user supplement and automatically continue.
		h.tasks.SetInterruptContinueNote(req.ConversationID, note)
		ok, err := h.tasks.CancelTask(req.ConversationID, multiagent.ErrInterruptContinue)
		if err != nil {
			h.logger.Error("Interrupt-and-continue without a tool failed", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "No running task found"})
			return
		}
		h.logger.Info("Conversation page interrupt-and-continue without MCP tool; will auto-resume",
			zap.String("conversationId", req.ConversationID),
			zap.Bool("hasNote", note != ""),
		)
		c.JSON(http.StatusOK, gin.H{
			"status":              "interrupt_continue_scheduled",
			"conversationId":      req.ConversationID,
			"message":             "Requested pausing current reasoning. The user supplement will be merged into context and execution will continue automatically without stopping the whole turn.",
			"continueAfter":       true,
			"interruptWithNote":   note != "",
			"continueWithoutTool": true,
		})
		return
	}

	var cause error = ErrTaskCancelled
	msg := "Cancellation requested. The task will stop after the current step completes."
	ok, err := h.tasks.CancelTask(req.ConversationID, cause)
	if err != nil {
		h.logger.Error("Failed to cancel task", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "No running task found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":            "cancelling",
		"conversationId":    req.ConversationID,
		"message":           msg,
		"continueAfter":     false,
		"interruptWithNote": false,
	})
}

// SubscribeAgentTaskEvents GET SSE：subscribes to mirrored events for the current running task in a conversation; frame format matches POST .../stream and is used to resume UI after refresh or disconnect.
func (h *AgentHandler) SubscribeAgentTaskEvents(c *gin.Context) {
	conversationID := strings.TrimSpace(c.Query("conversationId"))
	if conversationID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "conversationId is required"})
		return
	}
	if h.tasks.GetTask(conversationID) == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no active task for this conversation"})
		return
	}
	if h.taskEventBus == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "task event bus unavailable"})
		return
	}

	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	sub, ch := h.taskEventBus.Subscribe(conversationID)
	defer h.taskEventBus.Unsubscribe(conversationID, sub)

	flusher, _ := c.Writer.(http.Flusher)
	ctx := c.Request.Context()

	for {
		select {
		case <-ctx.Done():
			return
		case chunk, ok := <-ch:
			if !ok {
				return
			}
			if _, err := c.Writer.Write(chunk); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

// ListAgentTasks lists all running tasks
func (h *AgentHandler) ListAgentTasks(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"tasks": h.tasks.GetActiveTasks(),
	})
}

// ListCompletedTasks lists recent completed task history
func (h *AgentHandler) ListCompletedTasks(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"tasks": h.tasks.GetCompletedTasks(),
	})
}

// BatchTaskRequest batch task request
type BatchTaskRequest struct {
	Title        string   `json:"title"`                    // Task title, optional
	Tasks        []string `json:"tasks" binding:"required"` // Task list, one task per entry
	Role         string   `json:"role,omitempty"`           // Role name, optional; empty string means the default role
	AgentMode    string   `json:"agentMode,omitempty"`      // eino_single | deep | plan_execute | supervisor
	ScheduleMode string   `json:"scheduleMode,omitempty"`   // manual | cron
	CronExpr     string   `json:"cronExpr,omitempty"`       // scheduleMode=cron is required when
	ExecuteNow   bool     `json:"executeNow,omitempty"`     // Whether to execute immediately after creation, default false
	ProjectID    string   `json:"projectId,omitempty"`      // Project bound to child conversations in the queue, optional
}

// batchQueueWantsEino reports whether the queue is configured to use Eino multi-agent.
func batchQueueWantsEino(agentMode string) bool {
	m := strings.TrimSpace(strings.ToLower(agentMode))
	return m == "deep" || m == "plan_execute" || m == "supervisor"
}

func normalizeBatchQueueScheduleMode(mode string) string {
	if strings.TrimSpace(mode) == "cron" {
		return "cron"
	}
	return "manual"
}

// CreateBatchQueue Create batch task queue
func (h *AgentHandler) CreateBatchQueue(c *gin.Context) {
	var req BatchTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if len(req.Tasks) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Task list cannot be empty"})
		return
	}

	// Filter empty tasks
	validTasks := make([]string, 0, len(req.Tasks))
	for _, task := range req.Tasks {
		if task != "" {
			validTasks = append(validTasks, task)
		}
	}

	if len(validTasks) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No valid tasks"})
		return
	}

	agentMode := config.NormalizeAgentMode(req.AgentMode)
	scheduleMode := normalizeBatchQueueScheduleMode(req.ScheduleMode)
	cronExpr := strings.TrimSpace(req.CronExpr)
	var nextRunAt *time.Time
	if scheduleMode == "cron" {
		if cronExpr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Cron expression cannot be empty when Cron scheduling is enabled"})
			return
		}
		schedule, err := h.batchCronParser.Parse(cronExpr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Cron expression: " + err.Error()})
			return
		}
		next := schedule.Next(time.Now())
		nextRunAt = &next
	}

	queue, createErr := h.batchTaskManager.CreateBatchQueue(req.Title, req.Role, agentMode, scheduleMode, cronExpr, req.ProjectID, nextRunAt, validTasks)
	if createErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": createErr.Error()})
		return
	}
	started := false
	if req.ExecuteNow {
		ok, err := h.startBatchQueueExecution(queue.ID, false)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Queue does not exist"})
			return
		}
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "queueId": queue.ID})
			return
		}
		started = true
		if refreshed, exists := h.batchTaskManager.GetBatchQueue(queue.ID); exists {
			queue = refreshed
		}
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "task", "create_queue", "Create batch task queue", "batch_queue", queue.ID, map[string]interface{}{
			"task_count": len(validTasks), "started": started,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"queueId": queue.ID,
		"queue":   queue,
		"started": started,
	})
}

// GetBatchQueue Get batch task queue
func (h *AgentHandler) GetBatchQueue(c *gin.Context) {
	queueID := c.Param("queueId")
	queue, exists := h.batchTaskManager.GetBatchQueue(queueID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Queue does not exist"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"queue": queue})
}

// ListBatchQueuesResponse batch task queue list response
type ListBatchQueuesResponse struct {
	Queues     []*BatchTaskQueue `json:"queues"`
	Total      int               `json:"total"`
	Page       int               `json:"page"`
	PageSize   int               `json:"page_size"`
	TotalPages int               `json:"total_pages"`
}

// ListBatchQueues lists all batch task queues with filtering and pagination
func (h *AgentHandler) ListBatchQueues(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "10")
	offsetStr := c.DefaultQuery("offset", "0")
	pageStr := c.Query("page")
	status := c.Query("status")
	keyword := c.Query("keyword")

	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)
	page := 1

	// If page is provided, prefer using page to calculate offset
	if pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
			offset = (page - 1) * limit
		}
	}

	// Limit pageSize range
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	if offset < 0 {
		offset = 0
	}
	// Prevent maliciously large offsets from causing DB performance issues
	const maxOffset = 100000
	if offset > maxOffset {
		offset = maxOffset
	}

	// Default status is"all"
	if status == "" {
		status = "all"
	}

	// Get queue list and total count
	queues, total, err := h.batchTaskManager.ListQueues(limit, offset, status, keyword)
	if err != nil {
		h.logger.Error("Failed to get batch task queue list", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Calculate total pages
	totalPages := (total + limit - 1) / limit
	if totalPages == 0 {
		totalPages = 1
	}

	// If offset is used to calculate page, recalculate it
	if pageStr == "" {
		page = (offset / limit) + 1
	}

	response := ListBatchQueuesResponse{
		Queues:     queues,
		Total:      total,
		Page:       page,
		PageSize:   limit,
		TotalPages: totalPages,
	}

	c.JSON(http.StatusOK, response)
}

// StartBatchQueue Start executing batch task queue
func (h *AgentHandler) StartBatchQueue(c *gin.Context) {
	queueID := c.Param("queueId")
	ok, err := h.startBatchQueueExecution(queueID, false)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Queue does not exist"})
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "task", "start_queue", "Start batch task queue", "batch_queue", queueID, nil)
	}
	c.JSON(http.StatusOK, gin.H{"message": "Batch task execution has started", "queueId": queueID})
}

// RerunBatchQueue reruns a batch task queue by resetting all child tasks before executing again
func (h *AgentHandler) RerunBatchQueue(c *gin.Context) {
	queueID := c.Param("queueId")
	queue, exists := h.batchTaskManager.GetBatchQueue(queueID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Queue does not exist"})
		return
	}
	if queue.Status != "completed" && queue.Status != "cancelled" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Only completed or cancelled queues can be rerun"})
		return
	}
	if !h.batchTaskManager.ResetQueueForRerun(queueID) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reset queue"})
		return
	}
	ok, err := h.startBatchQueueExecution(queueID, false)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start"})
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "task", "rerun_queue", "Rerun batch task queue", "batch_queue", queueID, nil)
	}
	c.JSON(http.StatusOK, gin.H{"message": "Batch task execution has restarted", "queueId": queueID})
}

// PauseBatchQueue Pause batch task queue
func (h *AgentHandler) PauseBatchQueue(c *gin.Context) {
	queueID := c.Param("queueId")
	success := h.batchTaskManager.PauseQueue(queueID)
	if !success {
		c.JSON(http.StatusNotFound, gin.H{"error": "Queue does not exist or cannot be paused"})
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "task", "pause_queue", "Pause batch task queue", "batch_queue", queueID, nil)
	}
	c.JSON(http.StatusOK, gin.H{"message": "Batch task queue paused"})
}

// UpdateBatchQueueMetadata updates a batch task queue title, role, and agent mode
func (h *AgentHandler) UpdateBatchQueueMetadata(c *gin.Context) {
	queueID := c.Param("queueId")
	var req struct {
		Title     string `json:"title"`
		Role      string `json:"role"`
		AgentMode string `json:"agentMode"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.batchTaskManager.UpdateQueueMetadata(queueID, req.Title, req.Role, req.AgentMode); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updated, _ := h.batchTaskManager.GetBatchQueue(queueID)
	c.JSON(http.StatusOK, gin.H{"queue": updated})
}

// UpdateBatchQueueSchedule updates batch task queue schedule configuration (scheduleMode / cronExpr)
func (h *AgentHandler) UpdateBatchQueueSchedule(c *gin.Context) {
	queueID := c.Param("queueId")
	queue, exists := h.batchTaskManager.GetBatchQueue(queueID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Queue does not exist"})
		return
	}
	// Schedule can be modified only when not running
	if queue.Status == "running" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Queue is running; schedule configuration cannot be modified"})
		return
	}
	var req struct {
		ScheduleMode string `json:"scheduleMode"`
		CronExpr     string `json:"cronExpr"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	scheduleMode := normalizeBatchQueueScheduleMode(req.ScheduleMode)
	cronExpr := strings.TrimSpace(req.CronExpr)
	var nextRunAt *time.Time
	if scheduleMode == "cron" {
		if cronExpr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Cron expression cannot be empty when Cron scheduling is enabled"})
			return
		}
		schedule, err := h.batchCronParser.Parse(cronExpr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Cron expression: " + err.Error()})
			return
		}
		next := schedule.Next(time.Now())
		nextRunAt = &next
	}
	h.batchTaskManager.UpdateQueueSchedule(queueID, scheduleMode, cronExpr, nextRunAt)
	updated, _ := h.batchTaskManager.GetBatchQueue(queueID)
	c.JSON(http.StatusOK, gin.H{"queue": updated})
}

// SetBatchQueueScheduleEnabled enables or disables automatic Cron scheduling; manual execution is unaffected
func (h *AgentHandler) SetBatchQueueScheduleEnabled(c *gin.Context) {
	queueID := c.Param("queueId")
	if _, exists := h.batchTaskManager.GetBatchQueue(queueID); !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Queue does not exist"})
		return
	}
	var req struct {
		ScheduleEnabled bool `json:"scheduleEnabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !h.batchTaskManager.SetScheduleEnabled(queueID, req.ScheduleEnabled) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Queue does not exist"})
		return
	}
	queue, _ := h.batchTaskManager.GetBatchQueue(queueID)
	c.JSON(http.StatusOK, gin.H{"queue": queue})
}

// DeleteBatchQueue Delete batch task queue
func (h *AgentHandler) DeleteBatchQueue(c *gin.Context) {
	queueID := c.Param("queueId")
	success := h.batchTaskManager.DeleteQueue(queueID)
	if !success {
		c.JSON(http.StatusNotFound, gin.H{"error": "Queue does not exist"})
		return
	}
	if h.audit != nil {
		h.audit.Record(c, audit.Entry{
			Category:     "task",
			Action:       "delete_queue",
			Result:       "success",
			ResourceType: "batch_queue",
			ResourceID:   queueID,
			Message:      "Delete batch task queue",
		})
	}
	c.JSON(http.StatusOK, gin.H{"message": "Batch task queue deleted"})
}

// UpdateBatchTask Update batch task message
func (h *AgentHandler) UpdateBatchTask(c *gin.Context) {
	queueID := c.Param("queueId")
	taskID := c.Param("taskId")

	var req struct {
		Message string `json:"message" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request parameters: " + err.Error()})
		return
	}

	if req.Message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Task message cannot be empty"})
		return
	}

	err := h.batchTaskManager.UpdateTaskMessage(queueID, taskID, req.Message)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Return updated queue information
	queue, exists := h.batchTaskManager.GetBatchQueue(queueID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Queue does not exist"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Task updated", "queue": queue})
}

// AddBatchTask Add task to batch task queue
func (h *AgentHandler) AddBatchTask(c *gin.Context) {
	queueID := c.Param("queueId")

	var req struct {
		Message string `json:"message" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request parameters: " + err.Error()})
		return
	}

	if req.Message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Task message cannot be empty"})
		return
	}

	task, err := h.batchTaskManager.AddTaskToQueue(queueID, req.Message)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Return updated queue information
	queue, exists := h.batchTaskManager.GetBatchQueue(queueID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Queue does not exist"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Task added", "task": task, "queue": queue})
}

// DeleteBatchTask Delete batch task
func (h *AgentHandler) DeleteBatchTask(c *gin.Context) {
	queueID := c.Param("queueId")
	taskID := c.Param("taskId")

	err := h.batchTaskManager.DeleteTask(queueID, taskID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Return updated queue information
	queue, exists := h.batchTaskManager.GetBatchQueue(queueID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Queue does not exist"})
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "task", "delete_batch_task", "Delete batch child task", "batch_task", taskID, map[string]interface{}{
			"batch_queue_id": queueID,
		})
	}
	c.JSON(http.StatusOK, gin.H{"message": "Task deleted", "queue": queue})
}

func (h *AgentHandler) markBatchQueueRunning(queueID string) bool {
	h.batchRunnerMu.Lock()
	defer h.batchRunnerMu.Unlock()
	if _, exists := h.batchRunning[queueID]; exists {
		return false
	}
	h.batchRunning[queueID] = struct{}{}
	return true
}

func (h *AgentHandler) unmarkBatchQueueRunning(queueID string) {
	h.batchRunnerMu.Lock()
	defer h.batchRunnerMu.Unlock()
	delete(h.batchRunning, queueID)
}

func (h *AgentHandler) nextBatchQueueRunAt(cronExpr string, from time.Time) (*time.Time, error) {
	expr := strings.TrimSpace(cronExpr)
	if expr == "" {
		return nil, nil
	}
	schedule, err := h.batchCronParser.Parse(expr)
	if err != nil {
		return nil, err
	}
	next := schedule.Next(from)
	return &next, nil
}

func (h *AgentHandler) startBatchQueueExecution(queueID string, scheduled bool) (bool, error) {
	// Acquire the execution mutex first, then read queue status to avoid decisions based on stale snapshots
	if !h.markBatchQueueRunning(queueID) {
		return true, nil
	}

	queue, exists := h.batchTaskManager.GetBatchQueue(queueID)
	if !exists {
		h.unmarkBatchQueueRunning(queueID)
		return false, nil
	}

	if scheduled {
		if queue.ScheduleMode != "cron" {
			h.unmarkBatchQueueRunning(queueID)
			err := fmt.Errorf("Queue does not have cron scheduling enabled")
			h.batchTaskManager.SetLastScheduleError(queueID, err.Error())
			return true, err
		}
		if queue.Status == "running" || queue.Status == "paused" || queue.Status == "cancelled" {
			h.unmarkBatchQueueRunning(queueID)
			err := fmt.Errorf("Current queue status does not allow scheduled execution")
			h.batchTaskManager.SetLastScheduleError(queueID, err.Error())
			return true, err
		}
		if !h.batchTaskManager.ResetQueueForRerun(queueID) {
			h.unmarkBatchQueueRunning(queueID)
			err := fmt.Errorf("Failed to reset queue")
			h.batchTaskManager.SetLastScheduleError(queueID, err.Error())
			return true, err
		}
		queue, _ = h.batchTaskManager.GetBatchQueue(queueID)
	} else if queue.Status != "pending" && queue.Status != "paused" {
		h.unmarkBatchQueueRunning(queueID)
		return true, fmt.Errorf("Queue status does not allow starting")
	}

	if queue != nil && batchQueueWantsEino(queue.AgentMode) && (h.config == nil || !h.config.MultiAgent.Enabled) {
		h.unmarkBatchQueueRunning(queueID)
		err := fmt.Errorf("Queue is configured for Eino multi-agent, but multi-agent is not enabled in the system")
		if scheduled {
			h.batchTaskManager.SetLastScheduleError(queueID, err.Error())
		}
		return true, err
	}

	if scheduled {
		h.batchTaskManager.RecordScheduledRunStart(queueID)
	}
	h.batchTaskManager.UpdateQueueStatus(queueID, "running")
	if queue != nil && queue.ScheduleMode == "cron" {
		nextRunAt, err := h.nextBatchQueueRunAt(queue.CronExpr, time.Now())
		if err == nil {
			h.batchTaskManager.UpdateQueueSchedule(queueID, "cron", queue.CronExpr, nextRunAt)
		}
	}

	go h.executeBatchQueue(queueID)
	return true, nil
}

func (h *AgentHandler) batchQueueSchedulerLoop() {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		queues := h.batchTaskManager.GetLoadedQueues()
		now := time.Now()
		for _, queue := range queues {
			if queue == nil || queue.ScheduleMode != "cron" || !queue.ScheduleEnabled || queue.Status == "cancelled" || queue.Status == "running" || queue.Status == "paused" {
				continue
			}
			nextRunAt := queue.NextRunAt
			if nextRunAt == nil {
				next, err := h.nextBatchQueueRunAt(queue.CronExpr, now)
				if err != nil {
					h.logger.Warn("Batch task cron expression is invalid; skipping schedule", zap.String("queueId", queue.ID), zap.String("cronExpr", queue.CronExpr), zap.Error(err))
					continue
				}
				h.batchTaskManager.UpdateQueueSchedule(queue.ID, "cron", queue.CronExpr, next)
				nextRunAt = next
			}
			if nextRunAt != nil && (nextRunAt.Before(now) || nextRunAt.Equal(now)) {
				if _, err := h.startBatchQueueExecution(queue.ID, true); err != nil {
					h.logger.Warn("Failed to auto-schedule batch task", zap.String("queueId", queue.ID), zap.Error(err))
				}
			}
		}
	}
}

// executeBatchQueue executes a batch task queue
func (h *AgentHandler) executeBatchQueue(queueID string) {
	defer h.unmarkBatchQueueRunning(queueID)
	h.logger.Info("Start executing batch task queue", zap.String("queueId", queueID))

	for {
		// Check queue status
		queue, exists := h.batchTaskManager.GetBatchQueue(queueID)
		if !exists || queue.Status == "cancelled" || queue.Status == "completed" || queue.Status == "paused" {
			break
		}

		// Get next task
		task, hasNext := h.batchTaskManager.GetNextTask(queueID)
		if !hasNext {
			// All tasks completed: summarize failed child tasks for troubleshooting
			q, ok := h.batchTaskManager.GetBatchQueue(queueID)
			lastRunErr := ""
			if ok {
				for _, t := range q.Tasks {
					if t.Status == "failed" && t.Error != "" {
						lastRunErr = t.Error
					}
				}
			}
			h.batchTaskManager.SetLastRunError(queueID, lastRunErr)
			h.batchTaskManager.UpdateQueueStatus(queueID, "completed")
			h.logger.Info("Batch task queue execution completed", zap.String("queueId", queueID))
			break
		}

		// Update task status to running
		h.batchTaskManager.UpdateTaskStatus(queueID, task.ID, "running", "", "")

		// Create new conversation
		title := safeTruncateString(task.Message, 50)
		batchMeta := audit.ConversationCreateMeta("batch_task")
		batchMeta.ProjectID = effectiveProjectID(h.config, queue.ProjectID)
		conv, err := h.db.CreateConversation(title, batchMeta)
		var conversationID string
		if err != nil {
			h.logger.Error("Failed to create conversation", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.Error(err))
			h.batchTaskManager.UpdateTaskStatus(queueID, task.ID, "failed", "", "Failed to create conversation: "+err.Error())
			h.batchTaskManager.MoveToNextTask(queueID)
			continue
		}
		conversationID = conv.ID

		// Save conversationId into the task, even while running, so the conversation can be viewed
		h.batchTaskManager.UpdateTaskStatusWithConversationID(queueID, task.ID, "running", "", "", conversationID)

		// Apply role user prompt and tool configuration
		finalMessage := task.Message
		var roleTools []string // Tool list configured by the role
		if queue.Role != "" && queue.Role != "\u9ed8\u8ba4" {
			if h.config.Roles != nil {
				if role, exists := h.config.Roles[queue.Role]; exists && role.Enabled {
					// Apply user prompt
					if role.UserPrompt != "" {
						finalMessage = role.UserPrompt + "\n\n" + task.Message
						h.logger.Info("Applied role user prompt", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("role", queue.Role))
					}
					// Get the tool list configured by the role, preferring the tools field and remaining backward-compatible with mcps
					if len(role.Tools) > 0 {
						roleTools = role.Tools
						h.logger.Info("Using role-configured tool list", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("role", queue.Role), zap.Int("toolCount", len(roleTools)))
					}
				}
			}
		}

		// Save user message with the original message and no role prompt
		_, err = h.db.AddMessage(conversationID, "user", task.Message, nil)
		if err != nil {
			h.logger.Error("Failed to save user message", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("conversationId", conversationID), zap.Error(err))
		}

		// Create assistant message in advance so process details can be associated
		assistantMsg, err := h.db.AddMessage(conversationID, "assistant", "Processing...", nil)
		if err != nil {
			h.logger.Error("Failed to create assistant message", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("conversationId", conversationID), zap.Error(err))
			// If creation fails, continue execution but do not save process details
			assistantMsg = nil
		}

		// Create progress callback and reuse the unified logic（Batch tasks do not need stream events, so pass nil）
		var assistantMessageID string
		if assistantMsg != nil {
			assistantMessageID = assistantMsg.ID
		}
		// Note: batch tasks do not have a frontend-connected POST /stream, so to support stream resume after refresh,
		// mirror progress events to TaskEventBus, which GET /api/agent-loop/task-events subscribes to.
		// progressCallback is created inside the child task IIFE so it can access taskCtx/cancelWithCause and sendEvent.
		var progressCallback func(eventType, message string, data interface{})

		// Execute task using finalMessage with role prompt and the role tool list
		h.logger.Info("Executing batch task", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("message", task.Message), zap.String("role", queue.Role), zap.String("conversationId", conversationID))

		func() {
			// Match the conversation streaming API: only one running task is allowed per conversationId, and /api/agent-loop/cancel aligns with the conversation lock.
			baseCtx, cancelWithCause := context.WithCancelCause(context.Background())
			// Per-child-task timeout: 6 hours, matching the previous WithTimeout(Background).
			taskCtx, timeoutCancel := context.WithTimeout(baseCtx, 6*time.Hour)

			registered := false
			finishStatus := "completed"

			defer func() {
				h.batchTaskManager.SetTaskCancel(queueID, nil)
				timeoutCancel()
				if registered {
					// Match the streaming API: emit one done before ending so frontend task-events can close the UI promptly.
					if h.taskEventBus != nil {
						ev := StreamEvent{Type: "done", Message: "", Data: map[string]interface{}{"conversationId": conversationID}}
						if b, err := json.Marshal(ev); err == nil {
							h.taskEventBus.Publish(conversationID, append(append([]byte("data: "), b...), '\n', '\n'))
						}
					}
					h.tasks.FinishTask(conversationID, finishStatus)
				}
				cancelWithCause(nil)
			}()

			// Event mirroring: publish only to TaskEventBus and do not write the HTTP response directly; used to resume after refresh.
			sendEvent := func(eventType, message string, data interface{}) {
				if h.taskEventBus == nil {
					return
				}
				ev := StreamEvent{Type: eventType, Message: message, Data: data}
				b, err := json.Marshal(ev)
				if err != nil {
					b = []byte(`{"type":"error","message":"marshal failed"}`)
				}
				line := make([]byte, 0, len(b)+8)
				line = append(line, []byte("data: ")...)
				line = append(line, b...)
				line = append(line, '\n', '\n')
				h.taskEventBus.Publish(conversationID, line)
			}

			if _, err := h.tasks.StartTask(conversationID, task.Message, cancelWithCause); err != nil {
				h.logger.Warn("Failed to register conversation running status for batch queue child task",
					zap.String("queueId", queueID),
					zap.String("taskId", task.ID),
					zap.String("conversationId", conversationID),
					zap.Error(err))
				failMsg := err.Error()
				if errors.Is(err, ErrTaskAlreadyRunning) {
					failMsg = "A task is already running in the conversation; cannot start a batch child task concurrently in that conversation"
				}
				h.batchTaskManager.UpdateTaskStatus(queueID, task.ID, "failed", "", failMsg)
				return
			}
			registered = true
			// Store cancel function: pausing a queue cancels the child task context, matching previous semantics
			h.batchTaskManager.SetTaskCancel(queueID, timeoutCancel)

			// Create progress callback: write DB and mirror to task-events, supporting continued stream display after refresh.
			progressCallback = h.createProgressCallback(taskCtx, cancelWithCause, conversationID, assistantMessageID, sendEvent)
			taskCtx = mcp.WithMCPConversationID(taskCtx, conversationID)
			taskCtx = mcp.WithToolRunRegistry(taskCtx, h.tasks)

			// Use the role tool list configured by the queue; if empty, use all tools
			useBatchMulti := false
			batchOrch := "deep"
			am := strings.TrimSpace(strings.ToLower(queue.AgentMode))
			if am == "multi" {
				am = "deep"
			}
			if batchQueueWantsEino(queue.AgentMode) && h.config != nil && h.config.MultiAgent.Enabled {
				useBatchMulti = true
				batchOrch = config.NormalizeMultiAgentOrchestration(am)
			} else if queue.AgentMode == "" && h.config != nil && h.config.MultiAgent.Enabled && h.config.MultiAgent.BatchUseMultiAgent {
				// Backward compatibility: if queue agent mode is unset, keep using the legacy system-level switch.
				useBatchMulti = true
				batchOrch = "deep"
			}
			var resultMA *multiagent.RunResult
			var runErr error
			switch {
			case useBatchMulti:
				resultMA, runErr = multiagent.RunDeepAgent(taskCtx, h.config, &h.config.MultiAgent, h.agent, h.logger, conversationID, finalMessage, []agent.ChatMessage{}, roleTools, progressCallback, h.agentsMarkdownDir, batchOrch, nil, h.projectBlackboardBlock(conversationID))
			default:
				if h.config == nil {
					runErr = fmt.Errorf("Server configuration is not loaded")
				} else {
					resultMA, runErr = multiagent.RunEinoSingleChatModelAgent(taskCtx, h.config, &h.config.MultiAgent, h.agent, h.logger, conversationID, finalMessage, []agent.ChatMessage{}, roleTools, progressCallback, nil, h.projectBlackboardBlock(conversationID))
				}
			}

			if runErr != nil {
				if shouldPersistEinoAgentTraceAfterRunError(baseCtx) {
					h.persistEinoAgentTraceForResume(conversationID, resultMA)
				}

				errStr := runErr.Error()
				partialResp := ""
				if resultMA != nil {
					partialResp = resultMA.Response
				}
				isCancelled := errors.Is(context.Cause(baseCtx), ErrTaskCancelled) ||
					errors.Is(runErr, context.Canceled) ||
					strings.Contains(strings.ToLower(errStr), "context canceled") ||
					strings.Contains(strings.ToLower(errStr), "context cancelled") ||
					(partialResp != "" && (strings.Contains(partialResp, "Task has been canceled") || strings.Contains(partialResp, "Task execution interrupted")))
				isTimeout := errors.Is(runErr, context.DeadlineExceeded) || errors.Is(context.Cause(taskCtx), context.DeadlineExceeded)

				if isTimeout {
					finishStatus = "timeout"
				} else if isCancelled {
					finishStatus = "cancelled"
				} else {
					finishStatus = "failed"
				}

				if isCancelled {
					h.logger.Info("Batch task was canceled", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("conversationId", conversationID))
					cancelMsg := "The task was canceled by the user; subsequent operations have stopped."
					// If the execution result has a more specific cancellation message, use it
					if partialResp != "" && (strings.Contains(partialResp, "Task has been canceled") || strings.Contains(partialResp, "Task execution interrupted")) {
						cancelMsg = partialResp
					}
					// Update assistant message content
					if assistantMessageID != "" {
						if updateErr := h.appendAssistantMessageNotice(assistantMessageID, cancelMsg); updateErr != nil {
							h.logger.Warn("Failed to update assistant message after cancellation", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.Error(updateErr))
						}
						// Save cancellation details to database
						if err := h.db.AddProcessDetail(assistantMessageID, conversationID, "cancelled", cancelMsg, nil); err != nil {
							h.logger.Warn("Failed to save cancellation details", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.Error(err))
						}
					} else {
						// If no assistant message was created in advance, create a new one
						_, errMsg := h.db.AddMessage(conversationID, "assistant", cancelMsg, nil)
						if errMsg != nil {
							h.logger.Warn("Failed to save cancellation message", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.Error(errMsg))
						}
					}
					h.batchTaskManager.UpdateTaskStatusWithConversationID(queueID, task.ID, "cancelled", cancelMsg, "", conversationID)
				} else {
					h.logger.Error("Batch task execution failed", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("conversationId", conversationID), zap.Error(runErr))
					errorMsg := "Execution failed: " + runErr.Error()
					// Update assistant message content
					if assistantMessageID != "" {
						if _, updateErr := h.db.Exec(
							"UPDATE messages SET content = ?, updated_at = ? WHERE id = ?",
							errorMsg,
							time.Now(), assistantMessageID,
						); updateErr != nil {
							h.logger.Warn("Failed to update assistant message after failure", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.Error(updateErr))
						}
						// Save error details to database
						if err := h.db.AddProcessDetail(assistantMessageID, conversationID, "error", errorMsg, nil); err != nil {
							h.logger.Warn("Failed to save error details", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.Error(err))
						}
					}
					h.batchTaskManager.UpdateTaskStatus(queueID, task.ID, "failed", "", runErr.Error())
				}
			} else {
				h.logger.Info("Batch task execution succeeded", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("conversationId", conversationID))

				resText := ""
				var mcpIDs []string
				var lastIn, lastOut string
				if resultMA != nil {
					resText = resultMA.Response
					mcpIDs = resultMA.MCPExecutionIDs
					lastIn = resultMA.LastAgentTraceInput
					lastOut = resultMA.LastAgentTraceOutput
				}

				// Update assistant message content
				if assistantMessageID != "" {
					if updateErr := h.db.UpdateAssistantMessageFinalize(assistantMessageID, resText, mcpIDs, multiagent.AggregatedReasoningFromTraceJSON(lastIn)); updateErr != nil {
						h.logger.Warn("Failed to update assistant message", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.Error(updateErr))
						// If update failed, try creating a new message.
						_, err = h.db.AddMessage(conversationID, "assistant", resText, mcpIDs)
						if err != nil {
							h.logger.Error("Failed to save assistant message", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("conversationId", conversationID), zap.Error(err))
						}
					}
				} else {
					// If no assistant message was created in advance, create a new one.
					_, err = h.db.AddMessage(conversationID, "assistant", resText, mcpIDs)
					if err != nil {
						h.logger.Error("Failed to save assistant message", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("conversationId", conversationID), zap.Error(err))
					}
				}

				// Save agent trace.
				if lastIn != "" || lastOut != "" {
					if err := h.db.SaveAgentTrace(conversationID, lastIn, lastOut); err != nil {
						h.logger.Warn("Failed to save agent trace", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.Error(err))
					} else {
						h.logger.Info("Saved agent trace", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("conversationId", conversationID))
					}
				}

				// Save result.
				h.batchTaskManager.UpdateTaskStatusWithConversationID(queueID, task.ID, "completed", resText, "", conversationID)
			}
		}()

		// moves to the next task
		h.batchTaskManager.MoveToNextTask(queueID)

		// Check whether it was canceled or paused
		queue, _ = h.batchTaskManager.GetBatchQueue(queueID)
		if queue.Status == "cancelled" || queue.Status == "paused" {
			break
		}
	}
}

// loadHistoryFromAgentTrace restores history from agent message traces saved in the database, columns last_react_*, including single-agent and Eino.
// Logic matches attack chains: prefer the saved JSON message tape plus last assistant summary; otherwise fall back to the messages table.
func (h *AgentHandler) loadHistoryFromAgentTrace(conversationID string) ([]agent.ChatMessage, error) {
	traceInputJSON, assistantOut, err := h.db.GetAgentTrace(conversationID)
	if err != nil {
		return nil, fmt.Errorf("Failed to get agent trace: %w", err)
	}

	if traceInputJSON == "" {
		return nil, fmt.Errorf("Agent trace is empty; messages table will be used")
	}

	dataSource := "database_last_agent_trace"

	var messagesArray []map[string]interface{}
	if err := json.Unmarshal([]byte(traceInputJSON), &messagesArray); err != nil {
		return nil, fmt.Errorf("Failed to parse agent trace JSON: %w", err)
	}

	messageCount := len(messagesArray)

	h.logger.Info("Restoring history context from saved agent trace",
		zap.String("conversationId", conversationID),
		zap.String("dataSource", dataSource),
		zap.Int("traceInputSize", len(traceInputJSON)),
		zap.Int("messageCount", messageCount),
		zap.Int("assistantOutSize", len(assistantOut)),
	)
	// fmt.Println("messagesArray:", messagesArray)//debug

	// Convert to Agent message format
	agentMessages := make([]agent.ChatMessage, 0, len(messagesArray))
	for _, msgMap := range messagesArray {
		msg := agent.ChatMessage{}

		// Parse role
		if role, ok := msgMap["role"].(string); ok {
			msg.Role = role
		} else {
			continue // Skip invalid message
		}

		// Skip system messages; Eino Instruction provides them.
		if msg.Role == "system" {
			continue
		}

		// Parse content
		if content, ok := msgMap["content"].(string); ok {
			msg.Content = content
		}
		// DeepSeek reasoning mode：assistant messages with tool calls must send reasoning_content back in later requests
		if rc, ok := msgMap["reasoning_content"].(string); ok && strings.TrimSpace(rc) != "" {
			msg.ReasoningContent = rc
		}

		// Parse tool_calls if present
		if toolCallsRaw, ok := msgMap["tool_calls"]; ok && toolCallsRaw != nil {
			if toolCallsArray, ok := toolCallsRaw.([]interface{}); ok {
				msg.ToolCalls = make([]agent.ToolCall, 0, len(toolCallsArray))
				for _, tcRaw := range toolCallsArray {
					if tcMap, ok := tcRaw.(map[string]interface{}); ok {
						toolCall := agent.ToolCall{}

						// Parse ID
						if id, ok := tcMap["id"].(string); ok {
							toolCall.ID = id
						}

						// Parse Type
						if toolType, ok := tcMap["type"].(string); ok {
							toolCall.Type = toolType
						}

						// Parse Function
						if funcMap, ok := tcMap["function"].(map[string]interface{}); ok {
							toolCall.Function = agent.FunctionCall{}

							// Parse function name
							if name, ok := funcMap["name"].(string); ok {
								toolCall.Function.Name = name
							}

							// Parse arguments, which may be a string or object
							if argsRaw, ok := funcMap["arguments"]; ok {
								if argsStr, ok := argsRaw.(string); ok {
									// If it is a string, parse it as JSON
									var argsMap map[string]interface{}
									if err := json.Unmarshal([]byte(argsStr), &argsMap); err == nil {
										toolCall.Function.Arguments = argsMap
									}
								} else if argsMap, ok := argsRaw.(map[string]interface{}); ok {
									// If it is already an object, use it directly
									toolCall.Function.Arguments = argsMap
								}
							}
						}

						if toolCall.ID != "" {
							msg.ToolCalls = append(msg.ToolCalls, toolCall)
						}
					}
				}
			}
		}

		// Parse tool_call_id for tool role messages
		if toolCallID, ok := msgMap["tool_call_id"].(string); ok {
			msg.ToolCallID = toolCallID
		}
		if tn, ok := msgMap["tool_name"].(string); ok && strings.TrimSpace(tn) != "" {
			msg.ToolName = strings.TrimSpace(tn)
		} else if tn, ok := msgMap["name"].(string); ok && strings.TrimSpace(tn) != "" && strings.EqualFold(msg.Role, "tool") {
			msg.ToolName = strings.TrimSpace(tn)
		}

		agentMessages = append(agentMessages, msg)
	}

	// If last_react_output exists (assistant summary), merge it into the last assistant message, matching the saved format
	if assistantOut != "" {
		if len(agentMessages) > 0 {
			lastMsg := &agentMessages[len(agentMessages)-1]
			if strings.EqualFold(lastMsg.Role, "assistant") && len(lastMsg.ToolCalls) == 0 {
				lastMsg.Content = assistantOut
			} else {
				agentMessages = append(agentMessages, agent.ChatMessage{
					Role:    "assistant",
					Content: assistantOut,
				})
			}
		} else {
			agentMessages = append(agentMessages, agent.ChatMessage{
				Role:    "assistant",
				Content: assistantOut,
			})
		}
	}

	if len(agentMessages) == 0 {
		return nil, fmt.Errorf("Messages parsed from agent trace are empty")
	}

	if h.agent != nil {
		if fixed := h.agent.RepairOrphanToolMessages(&agentMessages); fixed {
			h.logger.Info("Fixed mismatched tool messages in history restored from agent trace",
				zap.String("conversationId", conversationID),
			)
		}
	}

	h.logger.Info("Finished restoring history messages from agent trace",
		zap.String("conversationId", conversationID),
		zap.String("dataSource", dataSource),
		zap.Int("originalMessageCount", messageCount),
		zap.Int("finalMessageCount", len(agentMessages)),
		zap.Bool("hasAssistantOut", assistantOut != ""),
	)
	return agentMessages, nil
}

// dbMessagesToAgentChatMessages maps DB rows to agent ChatMessage for history fallback
// (includes reasoning_content for DeepSeek thinking + tool replay).
func dbMessagesToAgentChatMessages(msgs []database.Message) []agent.ChatMessage {
	out := make([]agent.ChatMessage, 0, len(msgs))
	for i := range msgs {
		m := msgs[i]
		out = append(out, agent.ChatMessage{
			Role:             m.Role,
			Content:          m.Content,
			ReasoningContent: m.ReasoningContent,
		})
	}
	return out
}
