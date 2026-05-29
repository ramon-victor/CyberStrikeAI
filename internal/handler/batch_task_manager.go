package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"cyberstrike-ai/internal/database"

	"go.uber.org/zap"
)

// Batch task status constants
const (
	BatchQueueStatusPending   = "pending"
	BatchQueueStatusRunning   = "running"
	BatchQueueStatusPaused    = "paused"
	BatchQueueStatusCompleted = "completed"
	BatchQueueStatusCancelled = "cancelled"

	BatchTaskStatusPending   = "pending"
	BatchTaskStatusRunning   = "running"
	BatchTaskStatusCompleted = "completed"
	BatchTaskStatusFailed    = "failed"
	BatchTaskStatusCancelled = "cancelled"

	// MaxBatchTasksPerQueue Maximum number of tasks per queue
	MaxBatchTasksPerQueue = 10000

	// MaxBatchQueueTitleLen Maximum queue title length
	MaxBatchQueueTitleLen = 200

	// MaxBatchQueueRoleLen Maximum role name length
	MaxBatchQueueRoleLen = 100
)

// BatchTask Batch task item
type BatchTask struct {
	ID             string     `json:"id"`
	Message        string     `json:"message"`
	ConversationID string     `json:"conversationId,omitempty"`
	Status         string     `json:"status"` // pending, running, completed, failed, cancelled
	StartedAt      *time.Time `json:"startedAt,omitempty"`
	CompletedAt    *time.Time `json:"completedAt,omitempty"`
	Error          string     `json:"error,omitempty"`
	Result         string     `json:"result,omitempty"`
}

// BatchTaskQueue Batch task queue
type BatchTaskQueue struct {
	ID                    string       `json:"id"`
	Title                 string       `json:"title,omitempty"`
	Role                  string       `json:"role,omitempty"` // Role name（empty string means default role）
	AgentMode             string       `json:"agentMode"`      // single | eino_single | deep | plan_execute | supervisor
	ScheduleMode          string       `json:"scheduleMode"`   // manual | cron
	CronExpr              string       `json:"cronExpr,omitempty"`
	NextRunAt             *time.Time   `json:"nextRunAt,omitempty"`
	ScheduleEnabled       bool         `json:"scheduleEnabled"`
	LastScheduleTriggerAt *time.Time   `json:"lastScheduleTriggerAt,omitempty"`
	LastScheduleError     string       `json:"lastScheduleError,omitempty"`
	LastRunError          string       `json:"lastRunError,omitempty"`
	ProjectID             string       `json:"projectId,omitempty"`
	Tasks                 []*BatchTask `json:"tasks"`
	Status                string       `json:"status"` // pending, running, paused, completed, cancelled
	CreatedAt             time.Time    `json:"createdAt"`
	StartedAt             *time.Time   `json:"startedAt,omitempty"`
	CompletedAt           *time.Time   `json:"completedAt,omitempty"`
	CurrentIndex          int          `json:"currentIndex"`
}

// BatchTaskManager Batch task manager
type BatchTaskManager struct {
	db          *database.DB
	logger      *zap.Logger
	queues      map[string]*BatchTaskQueue
	taskCancels map[string]context.CancelFunc // Stores the cancel function for each queue's current task
	mu          sync.RWMutex
}

// NewBatchTaskManager creates a batch task manager
func NewBatchTaskManager(logger *zap.Logger) *BatchTaskManager {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &BatchTaskManager{
		logger:      logger,
		queues:      make(map[string]*BatchTaskQueue),
		taskCancels: make(map[string]context.CancelFunc),
	}
}

// SetDB sets the database connection
func (m *BatchTaskManager) SetDB(db *database.DB) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.db = db
}

// CreateBatchQueue Create batch task queue
func (m *BatchTaskManager) CreateBatchQueue(
	title, role, agentMode, scheduleMode, cronExpr, projectID string,
	nextRunAt *time.Time,
	tasks []string,
) (*BatchTaskQueue, error) {
	// Input validation
	if utf8.RuneCountInString(title) > MaxBatchQueueTitleLen {
		return nil, fmt.Errorf("Title cannot exceed %d characters", MaxBatchQueueTitleLen)
	}
	if utf8.RuneCountInString(role) > MaxBatchQueueRoleLen {
		return nil, fmt.Errorf("Role name cannot exceed %d characters", MaxBatchQueueRoleLen)
	}
	if len(tasks) > MaxBatchTasksPerQueue {
		return nil, fmt.Errorf("Max %d tasks per queue", MaxBatchTasksPerQueue)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	queueID := time.Now().Format("20060102150405") + "-" + generateShortID()
	queue := &BatchTaskQueue{
		ID:              queueID,
		Title:           title,
		Role:            role,
		ProjectID:       strings.TrimSpace(projectID),
		AgentMode:       normalizeBatchQueueAgentMode(agentMode),
		ScheduleMode:    normalizeBatchQueueScheduleMode(scheduleMode),
		CronExpr:        strings.TrimSpace(cronExpr),
		NextRunAt:       nextRunAt,
		ScheduleEnabled: true,
		Tasks:           make([]*BatchTask, 0, len(tasks)),
		Status:          BatchQueueStatusPending,
		CreatedAt:       time.Now(),
		CurrentIndex:    0,
	}
	if queue.ScheduleMode != "cron" {
		queue.CronExpr = ""
		queue.NextRunAt = nil
	}

	// Prepare task data for database persistence
	dbTasks := make([]map[string]interface{}, 0, len(tasks))

	for _, message := range tasks {
		if message == "" {
			continue // Skip empty lines
		}
		taskID := generateShortID()
		task := &BatchTask{
			ID:      taskID,
			Message: message,
			Status:  BatchTaskStatusPending,
		}
		queue.Tasks = append(queue.Tasks, task)
		dbTasks = append(dbTasks, map[string]interface{}{
			"id":      taskID,
			"message": message,
		})
	}

	// Save to database
	if m.db != nil {
		if err := m.db.CreateBatchQueue(
			queueID,
			title,
			role,
			queue.AgentMode,
			queue.ScheduleMode,
			queue.CronExpr,
			queue.NextRunAt,
			queue.ProjectID,
			dbTasks,
		); err != nil {
			m.logger.Warn("batch queue DB create failed", zap.String("queueId", queueID), zap.Error(err))
		}
	}

	m.queues[queueID] = queue
	return queue, nil
}

// GetBatchQueue Get batch task queue
func (m *BatchTaskManager) GetBatchQueue(queueID string) (*BatchTaskQueue, bool) {
	m.mu.RLock()
	queue, exists := m.queues[queueID]
	m.mu.RUnlock()

	if exists {
		return queue, true
	}

	// If not in memory, try loading from the database
	if m.db != nil {
		if queue := m.loadQueueFromDB(queueID); queue != nil {
			m.mu.Lock()
			m.queues[queueID] = queue
			m.mu.Unlock()
			return queue, true
		}
	}

	return nil, false
}

// loadQueueFromDB loads one queue from the database
func (m *BatchTaskManager) loadQueueFromDB(queueID string) *BatchTaskQueue {
	if m.db == nil {
		return nil
	}

	queueRow, err := m.db.GetBatchQueue(queueID)
	if err != nil || queueRow == nil {
		return nil
	}

	taskRows, err := m.db.GetBatchTasks(queueID)
	if err != nil {
		return nil
	}

	queue := &BatchTaskQueue{
		ID:           queueRow.ID,
		AgentMode:    "single",
		ScheduleMode: "manual",
		Status:       queueRow.Status,
		CreatedAt:    queueRow.CreatedAt,
		CurrentIndex: queueRow.CurrentIndex,
		Tasks:        make([]*BatchTask, 0, len(taskRows)),
	}

	if queueRow.Title.Valid {
		queue.Title = queueRow.Title.String
	}
	if queueRow.Role.Valid {
		queue.Role = queueRow.Role.String
	}
	if queueRow.AgentMode.Valid {
		queue.AgentMode = normalizeBatchQueueAgentMode(queueRow.AgentMode.String)
	}
	if queueRow.ScheduleMode.Valid {
		queue.ScheduleMode = normalizeBatchQueueScheduleMode(queueRow.ScheduleMode.String)
	}
	if queueRow.CronExpr.Valid && queue.ScheduleMode == "cron" {
		queue.CronExpr = strings.TrimSpace(queueRow.CronExpr.String)
	}
	if queueRow.NextRunAt.Valid && queue.ScheduleMode == "cron" {
		t := queueRow.NextRunAt.Time
		queue.NextRunAt = &t
	}
	queue.ScheduleEnabled = true
	if queueRow.ScheduleEnabled.Valid && queueRow.ScheduleEnabled.Int64 == 0 {
		queue.ScheduleEnabled = false
	}
	if queueRow.LastScheduleTriggerAt.Valid {
		t := queueRow.LastScheduleTriggerAt.Time
		queue.LastScheduleTriggerAt = &t
	}
	if queueRow.LastScheduleError.Valid {
		queue.LastScheduleError = strings.TrimSpace(queueRow.LastScheduleError.String)
	}
	if queueRow.LastRunError.Valid {
		queue.LastRunError = strings.TrimSpace(queueRow.LastRunError.String)
	}
	if queueRow.ProjectID.Valid {
		queue.ProjectID = strings.TrimSpace(queueRow.ProjectID.String)
	}
	if queueRow.StartedAt.Valid {
		queue.StartedAt = &queueRow.StartedAt.Time
	}
	if queueRow.CompletedAt.Valid {
		queue.CompletedAt = &queueRow.CompletedAt.Time
	}

	for _, taskRow := range taskRows {
		task := &BatchTask{
			ID:      taskRow.ID,
			Message: taskRow.Message,
			Status:  taskRow.Status,
		}
		if taskRow.ConversationID.Valid {
			task.ConversationID = taskRow.ConversationID.String
		}
		if taskRow.StartedAt.Valid {
			task.StartedAt = &taskRow.StartedAt.Time
		}
		if taskRow.CompletedAt.Valid {
			task.CompletedAt = &taskRow.CompletedAt.Time
		}
		if taskRow.Error.Valid {
			task.Error = taskRow.Error.String
		}
		if taskRow.Result.Valid {
			task.Result = taskRow.Result.String
		}
		queue.Tasks = append(queue.Tasks, task)
	}

	return queue
}

// GetLoadedQueues returns queues already loaded in memory without triggering DB loads, using only RLock
func (m *BatchTaskManager) GetLoadedQueues() []*BatchTaskQueue {
	m.mu.RLock()
	result := make([]*BatchTaskQueue, 0, len(m.queues))
	for _, queue := range m.queues {
		result = append(result, queue)
	}
	m.mu.RUnlock()
	return result
}

// GetAllQueues returns all queues
func (m *BatchTaskManager) GetAllQueues() []*BatchTaskQueue {
	m.mu.RLock()
	result := make([]*BatchTaskQueue, 0, len(m.queues))
	for _, queue := range m.queues {
		result = append(result, queue)
	}
	m.mu.RUnlock()

	// If the database is available, ensure all database queues are loaded into memory
	if m.db != nil {
		dbQueues, err := m.db.GetAllBatchQueues()
		if err == nil {
			m.mu.Lock()
			for _, queueRow := range dbQueues {
				if _, exists := m.queues[queueRow.ID]; !exists {
					if queue := m.loadQueueFromDB(queueRow.ID); queue != nil {
						m.queues[queueRow.ID] = queue
						result = append(result, queue)
					}
				}
			}
			m.mu.Unlock()
		}
	}

	return result
}

// ListQueues lists queues with filtering and pagination
func (m *BatchTaskManager) ListQueues(limit, offset int, status, keyword string) ([]*BatchTaskQueue, int, error) {
	var queues []*BatchTaskQueue
	var total int

	// If the database is available, query it
	if m.db != nil {
		// Get total count
		count, err := m.db.CountBatchQueues(status, keyword)
		if err != nil {
			return nil, 0, fmt.Errorf("Failed to count queues: %w", err)
		}
		total = count

		// Get queue list, IDs only
		queueRows, err := m.db.ListBatchQueues(limit, offset, status, keyword)
		if err != nil {
			return nil, 0, fmt.Errorf("Failed to query queue list: %w", err)
		}

		// Load full queue information from memory or database
		m.mu.Lock()
		for _, queueRow := range queueRows {
			var queue *BatchTaskQueue
			// Look in memory first
			if cached, exists := m.queues[queueRow.ID]; exists {
				queue = cached
			} else {
				// Load from database
				queue = m.loadQueueFromDB(queueRow.ID)
				if queue != nil {
					m.queues[queueRow.ID] = queue
				}
			}
			if queue != nil {
				queues = append(queues, queue)
			}
		}
		m.mu.Unlock()
	} else {
		// No database; filter and paginate in memory
		m.mu.RLock()
		allQueues := make([]*BatchTaskQueue, 0, len(m.queues))
		for _, queue := range m.queues {
			allQueues = append(allQueues, queue)
		}
		m.mu.RUnlock()

		// Filter
		filtered := make([]*BatchTaskQueue, 0)
		for _, queue := range allQueues {
			// Status filter
			if status != "" && status != "all" && queue.Status != status {
				continue
			}
			// Keyword search over queue ID and title
			if keyword != "" {
				keywordLower := strings.ToLower(keyword)
				queueIDLower := strings.ToLower(queue.ID)
				queueTitleLower := strings.ToLower(queue.Title)
				if !strings.Contains(queueIDLower, keywordLower) && !strings.Contains(queueTitleLower, keywordLower) {
					// Also search created time
					createdAtStr := queue.CreatedAt.Format("2006-01-02 15:04:05")
					if !strings.Contains(createdAtStr, keyword) {
						continue
					}
				}
			}
			filtered = append(filtered, queue)
		}

		// Sort by created time descending
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
		})

		total = len(filtered)

		// Paginate
		start := offset
		if start > len(filtered) {
			start = len(filtered)
		}
		end := start + limit
		if end > len(filtered) {
			end = len(filtered)
		}
		if start < len(filtered) {
			queues = filtered[start:end]
		}
	}

	return queues, total, nil
}

// LoadFromDB loads all queues from the database
func (m *BatchTaskManager) LoadFromDB() error {
	if m.db == nil {
		return nil
	}

	queueRows, err := m.db.GetAllBatchQueues()
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, queueRow := range queueRows {
		if _, exists := m.queues[queueRow.ID]; exists {
			continue // Already exists; skip
		}

		taskRows, err := m.db.GetBatchTasks(queueRow.ID)
		if err != nil {
			continue // Skip tasks that failed to load
		}

		queue := &BatchTaskQueue{
			ID:           queueRow.ID,
			AgentMode:    "single",
			ScheduleMode: "manual",
			Status:       queueRow.Status,
			CreatedAt:    queueRow.CreatedAt,
			CurrentIndex: queueRow.CurrentIndex,
			Tasks:        make([]*BatchTask, 0, len(taskRows)),
		}

		if queueRow.Title.Valid {
			queue.Title = queueRow.Title.String
		}
		if queueRow.Role.Valid {
			queue.Role = queueRow.Role.String
		}
		if queueRow.AgentMode.Valid {
			queue.AgentMode = normalizeBatchQueueAgentMode(queueRow.AgentMode.String)
		}
		if queueRow.ScheduleMode.Valid {
			queue.ScheduleMode = normalizeBatchQueueScheduleMode(queueRow.ScheduleMode.String)
		}
		if queueRow.CronExpr.Valid && queue.ScheduleMode == "cron" {
			queue.CronExpr = strings.TrimSpace(queueRow.CronExpr.String)
		}
		if queueRow.NextRunAt.Valid && queue.ScheduleMode == "cron" {
			t := queueRow.NextRunAt.Time
			queue.NextRunAt = &t
		}
		queue.ScheduleEnabled = true
		if queueRow.ScheduleEnabled.Valid && queueRow.ScheduleEnabled.Int64 == 0 {
			queue.ScheduleEnabled = false
		}
		if queueRow.LastScheduleTriggerAt.Valid {
			t := queueRow.LastScheduleTriggerAt.Time
			queue.LastScheduleTriggerAt = &t
		}
		if queueRow.LastScheduleError.Valid {
			queue.LastScheduleError = strings.TrimSpace(queueRow.LastScheduleError.String)
		}
		if queueRow.LastRunError.Valid {
			queue.LastRunError = strings.TrimSpace(queueRow.LastRunError.String)
		}
		if queueRow.ProjectID.Valid {
			queue.ProjectID = strings.TrimSpace(queueRow.ProjectID.String)
		}
		if queueRow.StartedAt.Valid {
			queue.StartedAt = &queueRow.StartedAt.Time
		}
		if queueRow.CompletedAt.Valid {
			queue.CompletedAt = &queueRow.CompletedAt.Time
		}

		for _, taskRow := range taskRows {
			task := &BatchTask{
				ID:      taskRow.ID,
				Message: taskRow.Message,
				Status:  taskRow.Status,
			}
			if taskRow.ConversationID.Valid {
				task.ConversationID = taskRow.ConversationID.String
			}
			if taskRow.StartedAt.Valid {
				task.StartedAt = &taskRow.StartedAt.Time
			}
			if taskRow.CompletedAt.Valid {
				task.CompletedAt = &taskRow.CompletedAt.Time
			}
			if taskRow.Error.Valid {
				task.Error = taskRow.Error.String
			}
			if taskRow.Result.Valid {
				task.Result = taskRow.Result.String
			}
			queue.Tasks = append(queue.Tasks, task)
		}

		m.queues[queueRow.ID] = queue
	}

	return nil
}

// UpdateTaskStatus updates task status
func (m *BatchTaskManager) UpdateTaskStatus(queueID, taskID, status string, result, errorMsg string) {
	m.UpdateTaskStatusWithConversationID(queueID, taskID, status, result, errorMsg, "")
}

// UpdateTaskStatusWithConversationID updates task status including conversationId
func (m *BatchTaskManager) UpdateTaskStatusWithConversationID(queueID, taskID, status string, result, errorMsg, conversationID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	queue, exists := m.queues[queueID]
	if !exists {
		return
	}

	// DB first: persist first, then update memory after success to avoid inconsistent status after restart
	if m.db != nil {
		if err := m.db.UpdateBatchTaskStatus(queueID, taskID, status, conversationID, result, errorMsg); err != nil {
			m.logger.Warn("batch task DB status update failed, skipping memory update",
				zap.String("queueId", queueID), zap.String("taskId", taskID), zap.Error(err))
			return
		}
	}

	for _, task := range queue.Tasks {
		if task.ID == taskID {
			task.Status = status
			if result != "" {
				task.Result = result
			}
			if errorMsg != "" {
				task.Error = errorMsg
			}
			if conversationID != "" {
				task.ConversationID = conversationID
			}
			now := time.Now()
			if status == BatchTaskStatusRunning && task.StartedAt == nil {
				task.StartedAt = &now
			}
			if status == BatchTaskStatusCompleted || status == BatchTaskStatusFailed || status == BatchTaskStatusCancelled {
				task.CompletedAt = &now
			}
			break
		}
	}
}

// UpdateQueueStatus updates queue status
func (m *BatchTaskManager) UpdateQueueStatus(queueID, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	queue, exists := m.queues[queueID]
	if !exists {
		return
	}

	// DB first: persist first, then update memory after success
	if m.db != nil {
		if err := m.db.UpdateBatchQueueStatus(queueID, status); err != nil {
			m.logger.Warn("batch queue DB status update failed, skipping memory update",
				zap.String("queueId", queueID), zap.Error(err))
			return
		}
	}

	queue.Status = status
	now := time.Now()
	if status == BatchQueueStatusRunning && queue.StartedAt == nil {
		queue.StartedAt = &now
	}
	if status == BatchQueueStatusCompleted || status == BatchQueueStatusCancelled {
		queue.CompletedAt = &now
	}
}

// UpdateQueueSchedule updates queue scheduling configuration
func (m *BatchTaskManager) UpdateQueueSchedule(queueID, scheduleMode, cronExpr string, nextRunAt *time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	queue, exists := m.queues[queueID]
	if !exists {
		return
	}

	queue.ScheduleMode = normalizeBatchQueueScheduleMode(scheduleMode)
	if queue.ScheduleMode == "cron" {
		queue.CronExpr = strings.TrimSpace(cronExpr)
		queue.NextRunAt = nextRunAt
	} else {
		queue.CronExpr = ""
		queue.NextRunAt = nil
	}

	if m.db != nil {
		if err := m.db.UpdateBatchQueueSchedule(queueID, queue.ScheduleMode, queue.CronExpr, queue.NextRunAt); err != nil {
			m.logger.Warn("batch queue DB schedule update failed", zap.String("queueId", queueID), zap.Error(err))
		}
	}
}

// UpdateQueueMetadata updates queue title, role, and agent mode when not running
func (m *BatchTaskManager) UpdateQueueMetadata(queueID, title, role, agentMode string) error {
	if utf8.RuneCountInString(title) > MaxBatchQueueTitleLen {
		return fmt.Errorf("Title cannot exceed %d characters", MaxBatchQueueTitleLen)
	}
	if utf8.RuneCountInString(role) > MaxBatchQueueRoleLen {
		return fmt.Errorf("Role name cannot exceed %d characters", MaxBatchQueueRoleLen)
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	queue, exists := m.queues[queueID]
	if !exists {
		return fmt.Errorf("Queue not found")
	}
	if queue.Status == BatchQueueStatusRunning {
		return fmt.Errorf("Queue is running, cannot modify")
	}

	// If agentMode was not provided, keep the existing value
	if strings.TrimSpace(agentMode) != "" {
		agentMode = normalizeBatchQueueAgentMode(agentMode)
	} else {
		agentMode = queue.AgentMode
	}

	queue.Title = title
	queue.Role = role
	queue.AgentMode = agentMode

	if m.db != nil {
		if err := m.db.UpdateBatchQueueMetadata(queueID, title, role, agentMode); err != nil {
			m.logger.Warn("batch queue DB metadata update failed", zap.String("queueId", queueID), zap.Error(err))
		}
	}
	return nil
}

// SetScheduleEnabled pauses or resumes automatic Cron scheduling without affecting manual execution
func (m *BatchTaskManager) SetScheduleEnabled(queueID string, enabled bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	queue, exists := m.queues[queueID]
	if !exists {
		return false
	}
	queue.ScheduleEnabled = enabled
	if m.db != nil {
		_ = m.db.UpdateBatchQueueScheduleEnabled(queueID, enabled)
	}
	return true
}

// RecordScheduledRunStart Cron called after Cron trigger succeeds and before child tasks execute
func (m *BatchTaskManager) RecordScheduledRunStart(queueID string) {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()

	queue, exists := m.queues[queueID]
	if !exists {
		return
	}
	queue.LastScheduleTriggerAt = &now
	queue.LastScheduleError = ""
	if m.db != nil {
		_ = m.db.RecordBatchQueueScheduledTriggerStart(queueID, now)
	}
}

// SetLastScheduleError scheduling-layer failure before execution starts
func (m *BatchTaskManager) SetLastScheduleError(queueID, msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	queue, exists := m.queues[queueID]
	if !exists {
		return
	}
	queue.LastScheduleError = strings.TrimSpace(msg)
	if m.db != nil {
		_ = m.db.SetBatchQueueLastScheduleError(queueID, queue.LastScheduleError)
	}
}

// SetLastRunError failure summary from the most recent batch execution
func (m *BatchTaskManager) SetLastRunError(queueID, msg string) {
	msg = strings.TrimSpace(msg)
	m.mu.Lock()
	defer m.mu.Unlock()

	queue, exists := m.queues[queueID]
	if !exists {
		return
	}
	queue.LastRunError = msg
	if m.db != nil {
		_ = m.db.SetBatchQueueLastRunError(queueID, msg)
	}
}

// ResetQueueForRerun resets queue and child task status for the next cron run
func (m *BatchTaskManager) ResetQueueForRerun(queueID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	queue, exists := m.queues[queueID]
	if !exists {
		return false
	}

	// DB first: persist the reset first, then update memory after success to avoid dirty memory state if DB fails
	if m.db != nil {
		if err := m.db.ResetBatchQueueForRerun(queueID); err != nil {
			m.logger.Warn("batch queue DB reset for rerun failed, skipping memory update",
				zap.String("queueId", queueID), zap.Error(err))
			return false
		}
	}

	queue.Status = BatchQueueStatusPending
	queue.CurrentIndex = 0
	queue.StartedAt = nil
	queue.CompletedAt = nil
	queue.NextRunAt = nil
	queue.LastRunError = ""
	queue.LastScheduleError = ""
	for _, task := range queue.Tasks {
		task.Status = BatchTaskStatusPending
		task.ConversationID = ""
		task.StartedAt = nil
		task.CompletedAt = nil
		task.Error = ""
		task.Result = ""
	}
	return true
}

// UpdateTaskMessage updates task message when the queue is idle and the task is not running
func (m *BatchTaskManager) UpdateTaskMessage(queueID, taskID, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	queue, exists := m.queues[queueID]
	if !exists {
		return fmt.Errorf("Queue not found")
	}

	if !queueAllowsTaskListMutationLocked(queue) {
		return fmt.Errorf("Queue is executing or not ready, cannot edit task")
	}

	// Find and update task
	for _, task := range queue.Tasks {
		if task.ID == taskID {
			if task.Status == BatchTaskStatusRunning {
				return fmt.Errorf("Running task cannot be edited")
			}
			task.Message = message

			// Sync to database
			if m.db != nil {
				if err := m.db.UpdateBatchTaskMessage(queueID, taskID, message); err != nil {
					return fmt.Errorf("Failed to update task message: %w", err)
				}
			}
			return nil
		}
	}

	return fmt.Errorf("Task not found")
}

// AddTaskToQueue adds a task to a queue when the queue is idle, including completed cron rounds and after manual pause
func (m *BatchTaskManager) AddTaskToQueue(queueID, message string) (*BatchTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	queue, exists := m.queues[queueID]
	if !exists {
		return nil, fmt.Errorf("Queue not found")
	}

	if !queueAllowsTaskListMutationLocked(queue) {
		return nil, fmt.Errorf("Queue is executing or not ready, cannot add task")
	}

	if message == "" {
		return nil, fmt.Errorf("Task message cannot be empty")
	}

	// Generate task ID
	taskID := generateShortID()
	task := &BatchTask{
		ID:      taskID,
		Message: message,
		Status:  BatchTaskStatusPending,
	}

	// Add to in-memory queue
	queue.Tasks = append(queue.Tasks, task)

	// Sync to database
	if m.db != nil {
		if err := m.db.AddBatchTask(queueID, taskID, message); err != nil {
			// If database save fails, remove it from memory
			queue.Tasks = queue.Tasks[:len(queue.Tasks)-1]
			return nil, fmt.Errorf("Failed to add task: %w", err)
		}
	}

	return task, nil
}

// DeleteTask deletes a task when the queue is idle; running tasks cannot be deleted
func (m *BatchTaskManager) DeleteTask(queueID, taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	queue, exists := m.queues[queueID]
	if !exists {
		return fmt.Errorf("Queue not found")
	}

	if !queueAllowsTaskListMutationLocked(queue) {
		return fmt.Errorf("Queue is executing or not ready, cannot delete task")
	}

	// Find task
	taskIndex := -1
	for i, task := range queue.Tasks {
		if task.ID == taskID {
			if task.Status == BatchTaskStatusRunning {
				return fmt.Errorf("Running task cannot be deleted")
			}
			taskIndex = i
			break
		}
	}

	if taskIndex == -1 {
		return fmt.Errorf("Task not found")
	}

	// DB first: delete from the database first, then remove from memory after success
	if m.db != nil {
		if err := m.db.DeleteBatchTask(queueID, taskID); err != nil {
			return fmt.Errorf("Failed to delete task: %w", err)
		}
	}

	queue.Tasks = append(queue.Tasks[:taskIndex], queue.Tasks[taskIndex+1:]...)
	return nil
}

func queueHasRunningTaskLocked(queue *BatchTaskQueue) bool {
	if queue == nil {
		return false
	}
	for _, t := range queue.Tasks {
		if t != nil && t.Status == BatchTaskStatusRunning {
			return true
		}
	}
	return false
}

// queueAllowsTaskListMutationLocked whether child task text/list mutation is allowed; must be called while holding BatchTaskManager.mu
func queueAllowsTaskListMutationLocked(queue *BatchTaskQueue) bool {
	if queue == nil {
		return false
	}
	if queue.Status == BatchQueueStatusRunning {
		return false
	}
	if queueHasRunningTaskLocked(queue) {
		return false
	}
	switch queue.Status {
	case BatchQueueStatusPending, BatchQueueStatusPaused, BatchQueueStatusCompleted, BatchQueueStatusCancelled:
		return true
	default:
		return false
	}
}

// GetNextTask gets the next pending task
func (m *BatchTaskManager) GetNextTask(queueID string) (*BatchTask, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	queue, exists := m.queues[queueID]
	if !exists {
		return nil, false
	}

	for i := queue.CurrentIndex; i < len(queue.Tasks); i++ {
		task := queue.Tasks[i]
		if task.Status == BatchTaskStatusPending {
			queue.CurrentIndex = i
			return task, true
		}
	}

	return nil, false
}

// MoveToNextTask moves to the next task
func (m *BatchTaskManager) MoveToNextTask(queueID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	queue, exists := m.queues[queueID]
	if !exists {
		return
	}

	queue.CurrentIndex++

	// Sync to database
	if m.db != nil {
		if err := m.db.UpdateBatchQueueCurrentIndex(queueID, queue.CurrentIndex); err != nil {
			m.logger.Warn("batch queue DB index update failed", zap.String("queueId", queueID), zap.Error(err))
		}
	}
}

// SetTaskCancel sets the cancel function for the current task
func (m *BatchTaskManager) SetTaskCancel(queueID string, cancel context.CancelFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cancel != nil {
		m.taskCancels[queueID] = cancel
	} else {
		delete(m.taskCancels, queueID)
	}
}

// PauseQueue pauses a queue
func (m *BatchTaskManager) PauseQueue(queueID string) bool {
	var cancelFunc context.CancelFunc

	m.mu.Lock()
	queue, exists := m.queues[queueID]
	if !exists {
		m.mu.Unlock()
		return false
	}

	if queue.Status != BatchQueueStatusRunning {
		m.mu.Unlock()
		return false
	}

	// DB first: persist first, then update memory after success
	if m.db != nil {
		if err := m.db.UpdateBatchQueueStatus(queueID, BatchQueueStatusPaused); err != nil {
			m.logger.Warn("batch queue DB pause update failed, skipping memory update",
				zap.String("queueId", queueID), zap.Error(err))
			m.mu.Unlock()
			return false
		}
	}

	queue.Status = BatchQueueStatusPaused

	// Cancel the current running task by canceling context
	if cancel, ok := m.taskCancels[queueID]; ok {
		cancelFunc = cancel
		delete(m.taskCancels, queueID)
	}
	m.mu.Unlock()

	// Run cancel callback after releasing the lock because cancel may block and should not run under lock
	if cancelFunc != nil {
		cancelFunc()
	}

	return true
}

// CancelQueue cancels a queue; kept for backward compatibility, but PauseQueue is recommended
func (m *BatchTaskManager) CancelQueue(queueID string) bool {
	now := time.Now()
	var cancelFunc context.CancelFunc

	m.mu.Lock()
	queue, exists := m.queues[queueID]
	if !exists {
		m.mu.Unlock()
		return false
	}

	if queue.Status == BatchQueueStatusCompleted || queue.Status == BatchQueueStatusCancelled {
		m.mu.Unlock()
		return false
	}

	// DB first: persist first, then update memory after success
	if m.db != nil {
		if err := m.db.CancelPendingBatchTasks(queueID, now); err != nil {
			m.logger.Warn("batch task DB batch cancel failed, skipping memory update",
				zap.String("queueId", queueID), zap.Error(err))
			m.mu.Unlock()
			return false
		}
		if err := m.db.UpdateBatchQueueStatus(queueID, BatchQueueStatusCancelled); err != nil {
			m.logger.Warn("batch queue DB cancel update failed, skipping memory update",
				zap.String("queueId", queueID), zap.Error(err))
			m.mu.Unlock()
			return false
		}
	}

	queue.Status = BatchQueueStatusCancelled
	queue.CompletedAt = &now

	// Mark all pending tasks as cancelled in memory
	for _, task := range queue.Tasks {
		if task.Status == BatchTaskStatusPending {
			task.Status = BatchTaskStatusCancelled
			task.CompletedAt = &now
		}
	}

	// Cancel the current running task
	if cancel, ok := m.taskCancels[queueID]; ok {
		cancelFunc = cancel
		delete(m.taskCancels, queueID)
	}
	m.mu.Unlock()

	// Run cancel callback after releasing the lock because cancel may block and should not run under lock
	if cancelFunc != nil {
		cancelFunc()
	}

	return true
}

// DeleteQueue deletes a queue; running queues cannot be deleted
func (m *BatchTaskManager) DeleteQueue(queueID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	queue, exists := m.queues[queueID]
	if !exists {
		return false
	}

	// Running queues cannot be deleted to avoid orphan goroutines and data loss
	if queue.Status == BatchQueueStatusRunning {
		return false
	}

	// Clean up cancel function
	delete(m.taskCancels, queueID)

	// Delete from database
	if m.db != nil {
		if err := m.db.DeleteBatchQueue(queueID); err != nil {
			m.logger.Warn("batch queue DB delete failed", zap.String("queueId", queueID), zap.Error(err))
		}
	}

	delete(m.queues, queueID)
	return true
}

// generateShortID generates a short ID
func generateShortID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return time.Now().Format("150405") + "-" + hex.EncodeToString(b)
}
