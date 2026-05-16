package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/security"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// MonitorHandler 监控处理器
type MonitorHandler struct {
	mcpServer      *mcp.Server
	externalMCPMgr *mcp.ExternalMCPManager
	executor       *security.Executor
	db             *database.DB
	logger         *zap.Logger
}

// NewMonitorHandler 创建新的监控处理器
func NewMonitorHandler(mcpServer *mcp.Server, executor *security.Executor, db *database.DB, logger *zap.Logger) *MonitorHandler {
	return &MonitorHandler{
		mcpServer:      mcpServer,
		externalMCPMgr: nil, // 将在创建后设置
		executor:       executor,
		db:             db,
		logger:         logger,
	}
}

// SetExternalMCPManager 设置外部MCP管理器
func (h *MonitorHandler) SetExternalMCPManager(mgr *mcp.ExternalMCPManager) {
	h.externalMCPMgr = mgr
}

// MonitorResponse 监控响应
type MonitorResponse struct {
	Executions []*mcp.ToolExecution      `json:"executions"`
	Stats      map[string]*mcp.ToolStats `json:"stats"`
	Timestamp  time.Time                 `json:"timestamp"`
	Total      int                       `json:"total,omitempty"`
	Page       int                       `json:"page,omitempty"`
	PageSize   int                       `json:"page_size,omitempty"`
	TotalPages int                       `json:"total_pages,omitempty"`
}

// Monitor 获取监控信息
func (h *MonitorHandler) Monitor(c *gin.Context) {
	// 解析分页参数
	page := 1
	pageSize := 20
	if pageStr := c.Query("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}
	if pageSizeStr := c.Query("page_size"); pageSizeStr != "" {
		if ps, err := strconv.Atoi(pageSizeStr); err == nil && ps > 0 && ps <= 100 {
			pageSize = ps
		}
	}

	// 解析状态筛选参数
	status := c.Query("status")
	// 解析工具筛选参数
	toolName := c.Query("tool")

	executions, total := h.loadExecutionsWithPagination(page, pageSize, status, toolName)
	stats := h.loadStats()

	totalPages := (total + pageSize - 1) / pageSize
	if totalPages == 0 {
		totalPages = 1
	}

	c.JSON(http.StatusOK, MonitorResponse{
		Executions: executions,
		Stats:      stats,
		Timestamp:  time.Now(),
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	})
}

func (h *MonitorHandler) loadExecutions() []*mcp.ToolExecution {
	executions, _ := h.loadExecutionsWithPagination(1, 1000, "", "")
	return executions
}

func (h *MonitorHandler) loadExecutionsWithPagination(page, pageSize int, status, toolName string) ([]*mcp.ToolExecution, int) {
	if h.db == nil {
		allExecutions := h.mcpServer.GetAllExecutions()
		// 如果指定了状态筛选或工具筛选，先进行筛选
		if status != "" || toolName != "" {
			filtered := make([]*mcp.ToolExecution, 0)
			for _, exec := range allExecutions {
				matchStatus := status == "" || exec.Status == status
				// 支持部分匹配（模糊搜索）
				matchTool := toolName == "" || strings.Contains(strings.ToLower(exec.ToolName), strings.ToLower(toolName))
				if matchStatus && matchTool {
					filtered = append(filtered, exec)
				}
			}
			allExecutions = filtered
		}
		total := len(allExecutions)
		offset := (page - 1) * pageSize
		end := offset + pageSize
		if end > total {
			end = total
		}
		if offset >= total {
			return []*mcp.ToolExecution{}, total
		}
		return allExecutions[offset:end], total
	}

	offset := (page - 1) * pageSize
	executions, err := h.db.LoadToolExecutionsWithPagination(offset, pageSize, status, toolName)
	if err != nil {
		h.logger.Warn("Failed to load execution records from database, falling back to memory", zap.Error(err))
		allExecutions := h.mcpServer.GetAllExecutions()
		// 如果指定了状态筛选或工具筛选，先进行筛选
		if status != "" || toolName != "" {
			filtered := make([]*mcp.ToolExecution, 0)
			for _, exec := range allExecutions {
				matchStatus := status == "" || exec.Status == status
				// 支持部分匹配（模糊搜索）
				matchTool := toolName == "" || strings.Contains(strings.ToLower(exec.ToolName), strings.ToLower(toolName))
				if matchStatus && matchTool {
					filtered = append(filtered, exec)
				}
			}
			allExecutions = filtered
		}
		total := len(allExecutions)
		offset := (page - 1) * pageSize
		end := offset + pageSize
		if end > total {
			end = total
		}
		if offset >= total {
			return []*mcp.ToolExecution{}, total
		}
		return allExecutions[offset:end], total
	}

	// 获取总数（考虑状态筛选和工具筛选）
	total, err := h.db.CountToolExecutions(status, toolName)
	if err != nil {
		h.logger.Warn("Failed to get execution record count", zap.Error(err))
		// 回退：使用已加载的记录数估算
		total = offset + len(executions)
		if len(executions) == pageSize {
			total = offset + len(executions) + 1
		}
	}

	return executions, total
}

func (h *MonitorHandler) loadStats() map[string]*mcp.ToolStats {
	// 合并内部MCP服务器和外部MCP管理器的统计信息
	stats := make(map[string]*mcp.ToolStats)

	// 加载内部MCP服务器的统计信息
	if h.db == nil {
		internalStats := h.mcpServer.GetStats()
		for k, v := range internalStats {
			stats[k] = v
		}
	} else {
		dbStats, err := h.db.LoadToolStats()
		if err != nil {
			h.logger.Warn("Failed to load stats from database, falling back to memory", zap.Error(err))
			internalStats := h.mcpServer.GetStats()
			for k, v := range internalStats {
				stats[k] = v
			}
		} else {
			for k, v := range dbStats {
				stats[k] = v
			}
		}
	}

	// 合并外部MCP管理器的统计信息
	if h.externalMCPMgr != nil {
		externalStats := h.externalMCPMgr.GetToolStats()
		for k, v := range externalStats {
			// 如果已存在，合并统计信息
			if existing, exists := stats[k]; exists {
				existing.TotalCalls += v.TotalCalls
				existing.SuccessCalls += v.SuccessCalls
				existing.FailedCalls += v.FailedCalls
				// 使用最新的调用时间
				if v.LastCallTime != nil && (existing.LastCallTime == nil || v.LastCallTime.After(*existing.LastCallTime)) {
					existing.LastCallTime = v.LastCallTime
				}
			} else {
				stats[k] = v
			}
		}
	}

	return stats
}

// GetExecution 获取特定执行记录
func (h *MonitorHandler) GetExecution(c *gin.Context) {
	id := c.Param("id")

	// 先从内部MCP服务器查找
	exec, exists := h.mcpServer.GetExecution(id)
	if exists {
		c.JSON(http.StatusOK, exec)
		return
	}

	// 如果找不到，尝试从外部MCP管理器查找
	if h.externalMCPMgr != nil {
		exec, exists = h.externalMCPMgr.GetExecution(id)
		if exists {
			c.JSON(http.StatusOK, exec)
			return
		}
	}

	// 如果都找不到，尝试从数据库查找（如果使用数据库存储）
	if h.db != nil {
		exec, err := h.db.GetToolExecution(id)
		if err == nil && exec != nil {
			c.JSON(http.StatusOK, exec)
			return
		}
	}

	c.JSON(http.StatusNotFound, gin.H{"error": "Execution record not found"})
}

// CancelExecution 手动取消进行中的 MCP 工具调用（仅取消该次 tools/call 的上下文，不停止整条 Agent / 迭代任务）
// 请求体可选 JSON：{ "note": "用户说明" }，将与工具已返回输出合并交给模型（含「用户终止说明」标题块，与命令行原文区分）。
func (h *MonitorHandler) CancelExecution(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Execution record ID cannot be empty"})
		return
	}
	note := ""
	dec := json.NewDecoder(c.Request.Body)
	var body struct {
		Note string `json:"note"`
	}
	if err := dec.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体须为 JSON，例如 {\"note\":\"说明\"}，可为空对象"})
		return
	}
	note = strings.TrimSpace(body.Note)
	if h.mcpServer.CancelToolExecutionWithNote(id, note) {
		h.logger.Info("Requested cancel of MCP tool execution", zap.String("executionId", id), zap.String("source", "internal"), zap.Bool("hasNote", note != ""))
		c.JSON(http.StatusOK, gin.H{"message": "Termination signal sent", "executionId": id})
		return
	}
	if h.externalMCPMgr != nil && h.externalMCPMgr.CancelToolExecutionWithNote(id, note) {
		h.logger.Info("Requested cancel of MCP tool execution", zap.String("executionId", id), zap.String("source", "external"), zap.Bool("hasNote", note != ""))
		c.JSON(http.StatusOK, gin.H{"message": "Termination signal sent", "executionId": id})
		return
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "No active tool execution found or task already ended"})
}

// BatchGetToolNames 批量获取工具执行的工具名称（消除前端 N+1 请求）
func (h *MonitorHandler) BatchGetToolNames(c *gin.Context) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result := make(map[string]string, len(req.IDs))
	for _, id := range req.IDs {
		// 先从内部MCP服务器查找
		if exec, exists := h.mcpServer.GetExecution(id); exists {
			result[id] = exec.ToolName
			continue
		}
		// 再从外部MCP管理器查找
		if h.externalMCPMgr != nil {
			if exec, exists := h.externalMCPMgr.GetExecution(id); exists {
				result[id] = exec.ToolName
				continue
			}
		}
		// 最后从数据库查找
		if h.db != nil {
			if exec, err := h.db.GetToolExecution(id); err == nil && exec != nil {
				result[id] = exec.ToolName
			}
		}
	}

	c.JSON(http.StatusOK, result)
}

// GetStats 获取统计信息
func (h *MonitorHandler) GetStats(c *gin.Context) {
	stats := h.loadStats()
	c.JSON(http.StatusOK, stats)
}

// DeleteExecution 删除执行记录
func (h *MonitorHandler) DeleteExecution(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Execution record ID cannot be empty"})
		return
	}

	// 如果使用数据库，先获取执行记录信息，然后删除并更新统计
	if h.db != nil {
		// 先获取执行记录信息（用于更新统计）
		exec, err := h.db.GetToolExecution(id)
		if err != nil {
			// 如果找不到记录，可能已经被删除，直接返回成功
			h.logger.Warn("Execution record does not exist or may have been deleted", zap.String("executionId", id), zap.Error(err))
			c.JSON(http.StatusOK, gin.H{"message": "Execution record does not exist or has been deleted"})
			return
		}

		// 删除执行记录
		err = h.db.DeleteToolExecution(id)
		if err != nil {
			h.logger.Error("Failed to delete execution record", zap.Error(err), zap.String("executionId", id))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete execution record: " + err.Error()})
			return
		}

		// 更新统计信息（减少相应的计数）
		totalCalls := 1
		successCalls := 0
		failedCalls := 0
		if exec.Status == "failed" || exec.Status == "cancelled" {
			failedCalls = 1
		} else if exec.Status == "completed" {
			successCalls = 1
		}

		if exec.ToolName != "" {
			if err := h.db.DecreaseToolStats(exec.ToolName, totalCalls, successCalls, failedCalls); err != nil {
				h.logger.Warn("Failed to update stats", zap.Error(err), zap.String("toolName", exec.ToolName))
				// 不返回错误，因为记录已经Deleted successfully
			}
		}

		h.logger.Info("Execution record deleted from database", zap.String("executionId", id), zap.String("toolName", exec.ToolName))
		c.JSON(http.StatusOK, gin.H{"message": "Execution record deleted"})
		return
	}

	// 如果不使用数据库，尝试从内存中删除（内部MCP服务器）
	// 注意：内存中的记录可能已经被清理，所以这里只记录日志
	h.logger.Info("Attempting to delete in-memory execution record", zap.String("executionId", id))
	c.JSON(http.StatusOK, gin.H{"message": "Execution record deleted (if existed)"})
}

// DeleteExecutions 批量删除执行记录
func (h *MonitorHandler) DeleteExecutions(c *gin.Context) {
	var request struct {
		IDs []string `json:"ids"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request parameters: " + err.Error()})
		return
	}

	if len(request.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Execution record ID list cannot be empty"})
		return
	}

	// 如果使用数据库，先获取执行记录信息，然后删除并更新统计
	if h.db != nil {
		// 先获取执行记录信息（用于更新统计）
		executions, err := h.db.GetToolExecutionsByIds(request.IDs)
		if err != nil {
			h.logger.Error("Failed to get execution record", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get execution record: " + err.Error()})
			return
		}

		// 按工具名称分组统计需要减少的数量
		toolStats := make(map[string]struct {
			totalCalls   int
			successCalls int
			failedCalls  int
		})

		for _, exec := range executions {
			if exec.ToolName == "" {
				continue
			}

			stats := toolStats[exec.ToolName]
			stats.totalCalls++
			if exec.Status == "failed" || exec.Status == "cancelled" {
				stats.failedCalls++
			} else if exec.Status == "completed" {
				stats.successCalls++
			}
			toolStats[exec.ToolName] = stats
		}

		// 批量删除执行记录
		err = h.db.DeleteToolExecutions(request.IDs)
		if err != nil {
			h.logger.Error("Failed to batch delete execution records", zap.Error(err), zap.Int("count", len(request.IDs)))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to batch delete execution records: " + err.Error()})
			return
		}

		// 更新统计信息（减少相应的计数）
		for toolName, stats := range toolStats {
			if err := h.db.DecreaseToolStats(toolName, stats.totalCalls, stats.successCalls, stats.failedCalls); err != nil {
				h.logger.Warn("Failed to update stats", zap.Error(err), zap.String("toolName", toolName))
				// 不返回错误，因为记录已经Deleted successfully
			}
		}

		h.logger.Info("Batch delete execution records succeeded", zap.Int("count", len(request.IDs)))
		c.JSON(http.StatusOK, gin.H{"message": "Successfully deleted execution records", "deleted": len(executions)})
		return
	}

	// 如果不使用数据库，尝试从内存中删除（内部MCP服务器）
	// 注意：内存中的记录可能已经被清理，所以这里只记录日志
	h.logger.Info("Attempting batch delete of in-memory execution records", zap.Int("count", len(request.IDs)))
	c.JSON(http.StatusOK, gin.H{"message": "Execution record deleted (if existed)"})
}
