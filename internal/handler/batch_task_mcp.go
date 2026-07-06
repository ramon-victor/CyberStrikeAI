package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/mcp/builtin"

	"go.uber.org/zap"
)

// RegisterBatchTaskMCPTools 注册批量任务队列相关 MCP 工具（需传入已初始化 DB 的 AgentHandler）
func RegisterBatchTaskMCPTools(mcpServer *mcp.Server, h *AgentHandler, logger *zap.Logger) {
	if mcpServer == nil || h == nil || logger == nil {
		return
	}

	reg := func(tool mcp.Tool, fn func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error)) {
		mcpServer.RegisterTool(tool, fn)
	}

	// --- list ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskList,
		Description:      "List batch task queues (concise summary, saves context). Includes queue metadata, subtask id/status/truncated message, and counts per status. For full subtasks (with result/error/conversationId/time, etc.), use batch_task_get(queue_id).\n\n⚠️ Call constraint: this tool belongs to the 'Task Management' module; only call it when the user explicitly mentions viewing/managing batch tasks or task queues. Do not call it on your own when the user has not asked.",
		ShortDescription: "List batch task queues",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"status": map[string]interface{}{
					"type":        "string",
					"description": "Filter by status: all (default), pending, running, paused, completed, cancelled",
					"enum":        []string{"all", "pending", "running", "paused", "completed", "cancelled"},
				},
				"keyword": map[string]interface{}{
					"type":        "string",
					"description": "Fuzzy search by queue ID or title",
				},
				"page": map[string]interface{}{
					"type":        "integer",
					"description": "Page number, starting from 1, default 1",
				},
				"page_size": map[string]interface{}{
					"type":        "integer",
					"description": "Page size, default 20, max 100",
				},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		status := mcpArgString(args, "status")
		if status == "" {
			status = "all"
		}
		keyword := mcpArgString(args, "keyword")
		page := int(mcpArgFloat(args, "page"))
		if page <= 0 {
			page = 1
		}
		pageSize := int(mcpArgFloat(args, "page_size"))
		if pageSize <= 0 {
			pageSize = 20
		}
		if pageSize > 100 {
			pageSize = 100
		}
		offset := (page - 1) * pageSize
		if offset > 100000 {
			offset = 100000
		}
		queues, total, err := h.batchTaskManager.ListQueues(pageSize, offset, status, keyword)
		if err != nil {
			return batchMCPTextResult(fmt.Sprintf("Failed to list queues: %v", err), true), nil
		}
		totalPages := (total + pageSize - 1) / pageSize
		if totalPages == 0 {
			totalPages = 1
		}
		slim := make([]batchTaskQueueMCPListItem, 0, len(queues))
		for _, q := range queues {
			if q == nil {
				continue
			}
			slim = append(slim, toBatchTaskQueueMCPListItem(q))
		}
		payload := map[string]interface{}{
			"queues":      slim,
			"total":       total,
			"page":        page,
			"page_size":   pageSize,
			"total_pages": totalPages,
		}
		logger.Info("MCP batch_task_list", zap.String("status", status), zap.Int("total", total))
		return batchMCPJSONResult(payload)
	})

	// --- get ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskGet,
		Description:      "Get details of a single batch task queue by queue_id (including subtask list, Cron, schedule switch and recent error info).\n\n⚠️ Call constraint: this tool belongs to the 'Task Management' module; only call it when the user explicitly mentions viewing/managing batch tasks or task queues. Do not call it on your own when the user has not asked.",
		ShortDescription: "Get batch task queue details",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "Queue ID",
				},
			},
			"required": []string{"queue_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id cannot be empty", true), nil
		}
		queue, ok := h.batchTaskManager.GetBatchQueue(qid)
		if !ok {
			return batchMCPTextResult("Queue does not exist: "+qid, true), nil
		}
		return batchMCPJSONResult(queue)
	})

	// --- create ---
	reg(mcp.Tool{
		Name: builtin.ToolBatchTaskCreate,
		Description: "⚠️ Call constraint: this tool belongs to the 'Task Management' module; only call it when the user explicitly asks to create a batch task or task queue. Do not call it on your own when the user has not mentioned keywords like 'batch task', 'task queue', or 'scheduled task'. If the user is just asking you to do something, complete it directly in the current conversation; do not create a task queue on your own initiative.\n\n[Purpose] In-app 'Task Management / Batch Task Queue': register multiple independent user instructions as a queue, so you can view progress, pause/resume, schedule reruns, etc. in the UI. This is a queue data and scheduling entry point, not a 'sub-agent session' to explore the current question for you.\n\n[When to use] Call when the user explicitly wants batch queued execution, periodic Cron runs of the same batch of instructions, or alignment with the Task Management page. For questions needing immediate follow-up or analysis/coding that strongly depends on the current conversation context, complete directly in this conversation; do not create a queue for the sake of 'delegation'.\n\n[Parameters] tasks (string array) or tasks_text (multi-line, one per line); choose one; each item is an instruction to be executed by the system in queue order. agent_mode: eino_single (Eino ADK, default), deep / plan_execute / supervisor (requires multi-agent enabled). It is not 'splitting the main conversation into sub-agents'. schedule_mode: manual (default) or cron; cron requires cron_expr (5 fields, e.g., \"0 */6 * * *\").\n\n[Execution] Defaults to pending after creation, does not run automatically. execute_now=true starts immediately after creation; otherwise call batch_task_start later. Cron auto next round requires schedule_enabled to be true (use batch_task_schedule_enabled).",
		ShortDescription: "Task management: create batch task queue (register multiple instructions, optional immediate or Cron)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"title": map[string]interface{}{
					"type":        "string",
					"description": "Optional queue title, for easy identification in task management",
				},
				"role": map[string]interface{}{
					"type":        "string",
					"description": "Role name used by the queue; empty means default",
				},
				"tasks": map[string]interface{}{
					"type":        "array",
					"description": "Subtask instructions in the queue, each one is an independent pending instruction (mutually exclusive with tasks_text)",
					"items":       map[string]interface{}{"type": "string"},
				},
				"tasks_text": map[string]interface{}{
					"type":        "string",
					"description": "Multi-line text, each line is a subtask instruction (mutually exclusive with tasks)",
				},
				"agent_mode": map[string]interface{}{
					"type":        "string",
					"description": "Execution mode: eino_single (Eino ADK, default), deep/plan_execute/supervisor (Eino orchestration, requires multi-agent enabled)",
					"enum":        []string{"eino_single", "deep", "plan_execute", "supervisor"},
				},
				"schedule_mode": map[string]interface{}{
					"type":        "string",
					"description": "manual (only manual/start runs) or cron (triggered by expression)",
					"enum":        []string{"manual", "cron"},
				},
				"cron_expr": map[string]interface{}{
					"type":        "string",
					"description": "Required when schedule_mode is cron. Standard 5 fields: minute hour day month weekday, e.g., \"0 */6 * * *\", \"30 2 * * 1-5\"",
				},
				"execute_now": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether to start executing the queue immediately after creation, default false (pending, requires batch_task_start)",
				},
				"project_id": map[string]interface{}{
					"type":        "string",
					"description": "Project ID bound to sub-conversations within the queue (optional; if not specified, config.project.default_project_id is used)",
				},
				"concurrency": map[string]interface{}{
					"type":        "integer",
					"description": "Number of subtasks running concurrently, default 1 (serial), max 8. Recommended 1-2 when scan-type tools are included.",
				},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		tasks, errMsg := batchMCPTasksFromArgs(args)
		if errMsg != "" {
			return batchMCPTextResult(errMsg, true), nil
		}
		title := mcpArgString(args, "title")
		role := mcpArgString(args, "role")
		agentMode := config.NormalizeAgentMode(mcpArgString(args, "agent_mode"))
		scheduleMode := normalizeBatchQueueScheduleMode(mcpArgString(args, "schedule_mode"))
		cronExpr := strings.TrimSpace(mcpArgString(args, "cron_expr"))
		var nextRunAt *time.Time
		if scheduleMode == "cron" {
			if cronExpr == "" {
				return batchMCPTextResult("cron_expr cannot be empty in cron schedule mode", true), nil
			}
			sch, err := h.batchCronParser.Parse(cronExpr)
			if err != nil {
				return batchMCPTextResult("Invalid cron expression: "+err.Error(), true), nil
			}
			n := sch.Next(time.Now())
			nextRunAt = &n
		}
		executeNow, ok := mcpArgBool(args, "execute_now")
		if !ok {
			executeNow = false
		}
		projectID := strings.TrimSpace(mcpArgString(args, "project_id"))
		concurrency := int(mcpArgFloat(args, "concurrency"))
		queue, createErr := h.batchTaskManager.CreateBatchQueue(title, role, agentMode, scheduleMode, cronExpr, projectID, nextRunAt, concurrency, tasks)
		if createErr != nil {
			return batchMCPTextResult("Failed to create queue: "+createErr.Error(), true), nil
		}
		started := false
		if executeNow {
			ok, err := h.startBatchQueueExecution(queue.ID, false)
			if !ok {
				return batchMCPTextResult("Queue does not exist: "+queue.ID, true), nil
			}
			if err != nil {
				return batchMCPTextResult("Created successfully but failed to start: "+err.Error(), true), nil
			}
			started = true
			if refreshed, exists := h.batchTaskManager.GetBatchQueue(queue.ID); exists {
				queue = refreshed
			}
		}
		logger.Info("MCP batch_task_create", zap.String("queueId", queue.ID), zap.Int("taskCount", len(tasks)))
		return batchMCPJSONResult(map[string]interface{}{
			"queue_id":    queue.ID,
			"queue":       queue,
			"started":     started,
			"execute_now": executeNow,
			"reminder": func() string {
				if started {
					return "Queue has been created and started immediately."
				}
				return "Queue has been created, currently pending. To start execution, call the MCP tool batch_task_start (queue_id same as above). Cron auto-scheduling requires schedule_enabled to be true; use batch_task_schedule_enabled."
			}(),
		})
	})

	// --- start ---
	reg(mcp.Tool{
		Name: builtin.ToolBatchTaskStart,
		Description: "Start or continue executing a batch task queue (pending / paused).\nUsed together with batch_task_create: creating a queue alone will not run it automatically; you must call this tool to start running the subtasks.\n\n⚠️ Call constraint: this tool belongs to the 'Task Management' module; only call it when the user explicitly asks to start/continue a batch task. Do not call it on your own when the user has not asked.",
		ShortDescription: "Start/continue batch task queue (must be called after creation to execute)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "Queue ID",
				},
			},
			"required": []string{"queue_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id cannot be empty", true), nil
		}
		ok, err := h.startBatchQueueExecution(qid, false)
		if !ok {
			return batchMCPTextResult("Queue does not exist: "+qid, true), nil
		}
		if err != nil {
			return batchMCPTextResult("Failed to start: "+err.Error(), true), nil
		}
		logger.Info("MCP batch_task_start", zap.String("queueId", qid))
		return batchMCPTextResult("Start submitted; the queue will begin executing.", false), nil
	})

	// --- rerun (reset + start for completed/cancelled queues) ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskRerun,
		Description:      "Rerun a completed or cancelled batch task queue. Will reset all subtask states and execute a new round.\n\n⚠️ Call constraint: this tool belongs to the 'Task Management' module; only call it when the user explicitly asks to rerun a batch task. Do not call it on your own when the user has not asked.",
		ShortDescription: "Rerun batch task queue",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "Queue ID",
				},
			},
			"required": []string{"queue_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id cannot be empty", true), nil
		}
		queue, exists := h.batchTaskManager.GetBatchQueue(qid)
		if !exists {
			return batchMCPTextResult("Queue does not exist: "+qid, true), nil
		}
		if queue.Status != "completed" && queue.Status != "cancelled" {
			return batchMCPTextResult("Only completed or cancelled queues can be rerun; current status: "+queue.Status, true), nil
		}
		if !h.batchTaskManager.ResetQueueForRerun(qid) {
			return batchMCPTextResult("Failed to reset queue", true), nil
		}
		ok, err := h.startBatchQueueExecution(qid, false)
		if !ok {
			return batchMCPTextResult("Failed to start", true), nil
		}
		if err != nil {
			return batchMCPTextResult("Failed to start: "+err.Error(), true), nil
		}
		logger.Info("MCP batch_task_rerun", zap.String("queueId", qid))
		return batchMCPTextResult("Queue has been reset and restarted.", false), nil
	})

	// --- pause ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskPause,
		Description:      "Pause a running batch task queue (the current subtask will be cancelled).\n\n⚠️ Call constraint: this tool belongs to the 'Task Management' module; only call it when the user explicitly asks to pause a batch task. Do not call it on your own when the user has not asked.",
		ShortDescription: "Pause batch task queue",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "Queue ID",
				},
			},
			"required": []string{"queue_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id cannot be empty", true), nil
		}
		if !h.batchTaskManager.PauseQueue(qid) {
			return batchMCPTextResult("Cannot pause: queue does not exist or is not in running state", true), nil
		}
		logger.Info("MCP batch_task_pause", zap.String("queueId", qid))
		return batchMCPTextResult("Queue has been paused.", false), nil
	})

	// --- delete queue ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskDelete,
		Description:      "Delete a batch task queue and its subtask records.\n\n⚠️ Call constraint: this tool belongs to the 'Task Management' module; only call it when the user explicitly asks to delete a batch task queue. Do not call it on your own when the user has not asked.",
		ShortDescription: "Delete batch task queue",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "Queue ID",
				},
			},
			"required": []string{"queue_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id cannot be empty", true), nil
		}
		if err := h.batchTaskManager.DeleteQueue(qid); err != nil {
			switch {
			case errors.Is(err, ErrBatchQueueNotFound):
				return batchMCPTextResult("Delete failed: queue does not exist", true), nil
			case errors.Is(err, ErrBatchQueueExecutorActive):
				return batchMCPTextResult("Delete failed: queue executor is still running, please try again later", true), nil
			case errors.Is(err, ErrBatchQueueStillRunning):
				return batchMCPTextResult("Delete failed: queue is currently running", true), nil
			default:
				return batchMCPTextResult("Delete failed: "+err.Error(), true), nil
			}
		}
		logger.Info("MCP batch_task_delete", zap.String("queueId", qid))
		return batchMCPTextResult("Queue has been deleted.", false), nil
	})

	// --- update metadata (title/role/agentMode) ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskUpdateMetadata,
		Description:      "Modify the title, role, and agent mode of a batch task queue. Only modifiable when the queue is not in the running state.\n\n⚠️ Call constraint: this tool belongs to the 'Task Management' module; only call it when the user explicitly asks to modify batch task queue properties. Do not call it on your own when the user has not asked.",
		ShortDescription: "Modify batch task queue title/role/agent mode",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "Queue ID",
				},
				"title": map[string]interface{}{
					"type":        "string",
					"description": "New title (empty string clears the title)",
				},
				"role": map[string]interface{}{
					"type":        "string",
					"description": "New role name (empty string uses default role)",
				},
				"agent_mode": map[string]interface{}{
					"type":        "string",
					"description": "Agent mode: eino_single, deep, plan_execute, supervisor",
					"enum":        []string{"eino_single", "deep", "plan_execute", "supervisor"},
				},
				"concurrency": map[string]interface{}{
					"type":        "integer",
					"description": "Number of subtasks running concurrently, default 1, max 8",
				},
			},
			"required": []string{"queue_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id cannot be empty", true), nil
		}
		title := mcpArgString(args, "title")
		role := mcpArgString(args, "role")
		agentMode := mcpArgString(args, "agent_mode")
		var concurrency *int
		if raw, ok := args["concurrency"]; ok && raw != nil {
			v := int(mcpArgFloat(args, "concurrency"))
			concurrency = &v
		}
		if err := h.batchTaskManager.UpdateQueueMetadata(qid, title, role, agentMode, concurrency); err != nil {
			return batchMCPTextResult(err.Error(), true), nil
		}
		updated, _ := h.batchTaskManager.GetBatchQueue(qid)
		logger.Info("MCP batch_task_update_metadata", zap.String("queueId", qid))
		return batchMCPJSONResult(updated)
	})

	// --- update schedule ---
	reg(mcp.Tool{
		Name: builtin.ToolBatchTaskUpdateSchedule,
		Description: "Modify the schedule mode and Cron expression of a batch task queue. Only modifiable when the queue is not in the running state.\nWhen schedule_mode is cron, a valid cron_expr must be provided; when manual, the Cron config will be cleared.\n\n⚠️ Call constraint: this tool belongs to the 'Task Management' module; only call it when the user explicitly asks to modify batch task schedule config. Do not call it on your own when the user has not asked.",
		ShortDescription: "Modify batch task schedule config (Cron expression)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "Queue ID",
				},
				"schedule_mode": map[string]interface{}{
					"type":        "string",
					"description": "manual or cron",
					"enum":        []string{"manual", "cron"},
				},
				"cron_expr": map[string]interface{}{
					"type":        "string",
					"description": "Cron expression (required when schedule_mode is cron). Standard 5 fields: minute hour day month weekday, e.g., \"0 */6 * * *\" (every 6 hours), \"30 2 * * 1-5\" (weekday 2:30 AM)",
				},
			},
			"required": []string{"queue_id", "schedule_mode"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id cannot be empty", true), nil
		}
		queue, exists := h.batchTaskManager.GetBatchQueue(qid)
		if !exists {
			return batchMCPTextResult("Queue does not exist: "+qid, true), nil
		}
		if queue.Status == "running" {
			return batchMCPTextResult("Queue is currently running; schedule config cannot be modified", true), nil
		}
		scheduleMode := normalizeBatchQueueScheduleMode(mcpArgString(args, "schedule_mode"))
		cronExpr := strings.TrimSpace(mcpArgString(args, "cron_expr"))
		var nextRunAt *time.Time
		if scheduleMode == "cron" {
			if cronExpr == "" {
				return batchMCPTextResult("cron_expr cannot be empty in cron schedule mode", true), nil
			}
			sch, err := h.batchCronParser.Parse(cronExpr)
			if err != nil {
				return batchMCPTextResult("Invalid cron expression: "+err.Error(), true), nil
			}
			n := sch.Next(time.Now())
			nextRunAt = &n
		}
		h.batchTaskManager.UpdateQueueSchedule(qid, scheduleMode, cronExpr, nextRunAt)
		updated, _ := h.batchTaskManager.GetBatchQueue(qid)
		logger.Info("MCP batch_task_update_schedule", zap.String("queueId", qid), zap.String("scheduleMode", scheduleMode), zap.String("cronExpr", cronExpr))
		return batchMCPJSONResult(updated)
	})

	// --- schedule enabled ---
	reg(mcp.Tool{
		Name: builtin.ToolBatchTaskScheduleEnabled,
		Description: "Set whether to allow Cron to auto-trigger this queue. When disabled, the Cron expression is preserved but scheduled auto-runs are stopped; manual 'Start' can still execute it.\nOnly meaningful for queues whose schedule_mode is cron.\n\n⚠️ Call constraint: this tool belongs to the 'Task Management' module; only call it when the user explicitly asks to toggle batch task auto-scheduling. Do not call it on your own when the user has not asked.",
		ShortDescription: "Toggle batch task Cron auto-schedule",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "Queue ID",
				},
				"schedule_enabled": map[string]interface{}{
					"type":        "boolean",
					"description": "true allows scheduled trigger, false is manual only",
				},
			},
			"required": []string{"queue_id", "schedule_enabled"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		if qid == "" {
			return batchMCPTextResult("queue_id cannot be empty", true), nil
		}
		en, ok := mcpArgBool(args, "schedule_enabled")
		if !ok {
			return batchMCPTextResult("schedule_enabled must be a boolean value", true), nil
		}
		if _, exists := h.batchTaskManager.GetBatchQueue(qid); !exists {
			return batchMCPTextResult("Queue does not exist", true), nil
		}
		if !h.batchTaskManager.SetScheduleEnabled(qid, en) {
			return batchMCPTextResult("Update failed", true), nil
		}
		queue, _ := h.batchTaskManager.GetBatchQueue(qid)
		logger.Info("MCP batch_task_schedule_enabled", zap.String("queueId", qid), zap.Bool("enabled", en))
		return batchMCPJSONResult(queue)
	})

	// --- add task ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskAdd,
		Description:      "Append a subtask to a queue in the pending state.\n\n⚠️ Call constraint: this tool belongs to the 'Task Management' module; only call it when the user explicitly asks to add a subtask to a batch task queue. Do not call it on your own when the user has not asked.",
		ShortDescription: "Add subtask to batch queue",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "Queue ID",
				},
				"message": map[string]interface{}{
					"type":        "string",
					"description": "Task instruction content",
				},
			},
			"required": []string{"queue_id", "message"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		msg := strings.TrimSpace(mcpArgString(args, "message"))
		if qid == "" || msg == "" {
			return batchMCPTextResult("queue_id and message cannot both be empty", true), nil
		}
		task, err := h.batchTaskManager.AddTaskToQueue(qid, msg)
		if err != nil {
			return batchMCPTextResult(err.Error(), true), nil
		}
		queue, _ := h.batchTaskManager.GetBatchQueue(qid)
		logger.Info("MCP batch_task_add_task", zap.String("queueId", qid), zap.String("taskId", task.ID))
		return batchMCPJSONResult(map[string]interface{}{"task": task, "queue": queue})
	})

	// --- update task ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskUpdate,
		Description:      "Modify the message of a subtask that is still pending in a pending queue.\n\n⚠️ Call constraint: this tool belongs to the 'Task Management' module; only call it when the user explicitly asks to modify batch subtask content. Do not call it on your own when the user has not asked.",
		ShortDescription: "Update batch subtask content",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "Queue ID",
				},
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "Subtask ID",
				},
				"message": map[string]interface{}{
					"type":        "string",
					"description": "New task instruction",
				},
			},
			"required": []string{"queue_id", "task_id", "message"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		tid := mcpArgString(args, "task_id")
		msg := strings.TrimSpace(mcpArgString(args, "message"))
		if qid == "" || tid == "" || msg == "" {
			return batchMCPTextResult("queue_id, task_id, message cannot all be empty", true), nil
		}
		if err := h.batchTaskManager.UpdateTaskMessage(qid, tid, msg); err != nil {
			return batchMCPTextResult(err.Error(), true), nil
		}
		queue, _ := h.batchTaskManager.GetBatchQueue(qid)
		logger.Info("MCP batch_task_update_task", zap.String("queueId", qid), zap.String("taskId", tid))
		return batchMCPJSONResult(queue)
	})

	// --- remove task ---
	reg(mcp.Tool{
		Name:             builtin.ToolBatchTaskRemove,
		Description:      "Delete a subtask that is still pending from a pending queue.\n\n⚠️ Call constraint: this tool belongs to the 'Task Management' module; only call it when the user explicitly asks to delete a batch subtask. Do not call it on your own when the user has not asked.",
		ShortDescription: "Delete batch subtask",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queue_id": map[string]interface{}{
					"type":        "string",
					"description": "Queue ID",
				},
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "Subtask ID",
				},
			},
			"required": []string{"queue_id", "task_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		qid := mcpArgString(args, "queue_id")
		tid := mcpArgString(args, "task_id")
		if qid == "" || tid == "" {
			return batchMCPTextResult("queue_id and task_id cannot both be empty", true), nil
		}
		if err := h.batchTaskManager.DeleteTask(qid, tid); err != nil {
			return batchMCPTextResult(err.Error(), true), nil
		}
		queue, _ := h.batchTaskManager.GetBatchQueue(qid)
		logger.Info("MCP batch_task_remove_task", zap.String("queueId", qid), zap.String("taskId", tid))
		return batchMCPJSONResult(queue)
	})

	logger.Info("batch task MCP tools registered", zap.Int("count", 12))
}

// --- batch_task_list 精简结构（避免把每条子任务的 result 等大段文本塞进列表上下文） ---

const mcpBatchListTaskMessageMaxRunes = 160

// batchTaskMCPListSummary 列表中的子任务摘要（完整字段用 batch_task_get）
type batchTaskMCPListSummary struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// batchTaskQueueMCPListItem 列表中的队列摘要
type batchTaskQueueMCPListItem struct {
	ID                    string                    `json:"id"`
	Title                 string                    `json:"title,omitempty"`
	Role                  string                    `json:"role,omitempty"`
	AgentMode             string                    `json:"agentMode"`
	ScheduleMode          string                    `json:"scheduleMode"`
	CronExpr              string                    `json:"cronExpr,omitempty"`
	NextRunAt             *time.Time                `json:"nextRunAt,omitempty"`
	ScheduleEnabled       bool                      `json:"scheduleEnabled"`
	LastScheduleTriggerAt *time.Time                `json:"lastScheduleTriggerAt,omitempty"`
	Status                string                    `json:"status"`
	CreatedAt             time.Time                 `json:"createdAt"`
	StartedAt             *time.Time                `json:"startedAt,omitempty"`
	CompletedAt           *time.Time                `json:"completedAt,omitempty"`
	CurrentIndex          int                       `json:"currentIndex"`
	Concurrency           int                       `json:"concurrency"`
	TaskTotal             int                       `json:"task_total"`
	TaskCounts            map[string]int            `json:"task_counts"`
	Tasks                 []batchTaskMCPListSummary `json:"tasks"`
}

func truncateStringRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	n := 0
	for i := range s {
		if n == maxRunes {
			out := strings.TrimSpace(s[:i])
			if out == "" {
				return "…"
			}
			return out + "…"
		}
		n++
	}
	return s
}

const mcpBatchListMaxTasksPerQueue = 200 // maximum number of subtask summaries returned per queue in the list

func toBatchTaskQueueMCPListItem(q *BatchTaskQueue) batchTaskQueueMCPListItem {
	counts := map[string]int{
		"pending":   0,
		"running":   0,
		"completed": 0,
		"failed":    0,
		"cancelled": 0,
	}
	tasks := make([]batchTaskMCPListSummary, 0, len(q.Tasks))
	for _, t := range q.Tasks {
		if t == nil {
			continue
		}
		counts[t.Status]++
		// List view limits the number of subtask summaries; for the full list use batch_task_get
		if len(tasks) < mcpBatchListMaxTasksPerQueue {
			tasks = append(tasks, batchTaskMCPListSummary{
				ID:      t.ID,
				Status:  t.Status,
				Message: truncateStringRunes(t.Message, mcpBatchListTaskMessageMaxRunes),
			})
		}
	}
	return batchTaskQueueMCPListItem{
		ID:                    q.ID,
		Title:                 q.Title,
		Role:                  q.Role,
		AgentMode:             q.AgentMode,
		ScheduleMode:          q.ScheduleMode,
		CronExpr:              q.CronExpr,
		NextRunAt:             q.NextRunAt,
		ScheduleEnabled:       q.ScheduleEnabled,
		LastScheduleTriggerAt: q.LastScheduleTriggerAt,
		Status:                q.Status,
		CreatedAt:             q.CreatedAt,
		StartedAt:             q.StartedAt,
		CompletedAt:           q.CompletedAt,
		CurrentIndex:          q.CurrentIndex,
		Concurrency:           q.Concurrency,
		TaskTotal:             len(tasks),
		TaskCounts:            counts,
		Tasks:                 tasks,
	}
}

func batchMCPTextResult(text string, isErr bool) *mcp.ToolResult {
	return &mcp.ToolResult{
		Content: []mcp.Content{{Type: "text", Text: text}},
		IsError: isErr,
	}
}

func batchMCPJSONResult(v interface{}) (*mcp.ToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return batchMCPTextResult(fmt.Sprintf("JSON encoding failed: %v", err), true), nil
	}
	return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: string(b)}}}, nil
}

func batchMCPTasksFromArgs(args map[string]interface{}) ([]string, string) {
	if raw, ok := args["tasks"]; ok && raw != nil {
		switch t := raw.(type) {
		case []interface{}:
			out := make([]string, 0, len(t))
			for _, x := range t {
				if s, ok := x.(string); ok {
					if tr := strings.TrimSpace(s); tr != "" {
						out = append(out, tr)
					}
				}
			}
			if len(out) > 0 {
				return out, ""
			}
		}
	}
	if txt := mcpArgString(args, "tasks_text"); txt != "" {
		lines := strings.Split(txt, "\n")
		out := make([]string, 0, len(lines))
		for _, line := range lines {
			if tr := strings.TrimSpace(line); tr != "" {
				out = append(out, tr)
			}
		}
		if len(out) > 0 {
			return out, ""
		}
	}
	return nil, "need to provide tasks (string array) or tasks_text (multi-line text, one task per line)"
}

func mcpArgString(args map[string]interface{}, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		return strings.TrimSpace(strconv.FormatFloat(t, 'f', -1, 64))
	case json.Number:
		return strings.TrimSpace(t.String())
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func mcpArgFloat(args map[string]interface{}, key string) float64 {
	v, ok := args[key]
	if !ok || v == nil {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f
	default:
		return 0
	}
}

func mcpArgBool(args map[string]interface{}, key string) (val bool, ok bool) {
	v, exists := args[key]
	if !exists {
		return false, false
	}
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		if s == "true" || s == "1" || s == "yes" {
			return true, true
		}
		if s == "false" || s == "0" || s == "no" {
			return false, true
		}
	case float64:
		return t != 0, true
	}
	return false, false
}
