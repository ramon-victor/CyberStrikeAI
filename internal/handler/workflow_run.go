package handler

import (
	"net/http"
	"strings"
	"time"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	workflowrunner "cyberstrike-ai/internal/workflow"

	"github.com/gin-gonic/gin"
)

func (h *WorkflowHandler) SetRuntime(agent *agent.Agent, cfg *config.Config) {
	h.agent = agent
	h.cfg = cfg
}

func (h *WorkflowHandler) GetRun(c *gin.Context) {
	runID := strings.TrimSpace(c.Param("runId"))
	run, err := h.db.GetWorkflowRun(runID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if run == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "工作流运行不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"run": run})
}

func (h *WorkflowHandler) ListPendingRuns(c *gin.Context) {
	conversationID := strings.TrimSpace(c.Query("conversationId"))
	runs, err := h.db.ListWorkflowRunsAwaitingHITLFiltered(conversationID, 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"runs": runs})
}

type workflowResumeRequest struct {
	Approved bool   `json:"approved"`
	Comment  string `json:"comment,omitempty"`
}

func (h *WorkflowHandler) ResumeRun(c *gin.Context) {
	if h.agent == nil || h.cfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "工作流运行时未初始化"})
		return
	}
	runID := strings.TrimSpace(c.Param("runId"))
	var req workflowResumeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数: " + err.Error()})
		return
	}
	run, err := h.db.GetWorkflowRun(runID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if run == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "工作流运行不存在"})
		return
	}
	role := config.RoleConfig{Name: strings.TrimSpace(run.RoleID)}
	if role.Name != "" && h.cfg.Roles != nil {
		if r, ok := h.cfg.Roles[role.Name]; ok {
			role = r
			if role.Name == "" {
				role.Name = run.RoleID
			}
		}
	}
	if run.Status != "awaiting_hitl" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "工作流运行不在等待审批状态: " + run.Status})
		return
	}
	if err := h.db.RecordWorkflowRunHITLDecision(runID, req.Approved, req.Comment); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	decision := workflowrunner.HITLDecision{
		Approved: req.Approved,
		Comment:  strings.TrimSpace(req.Comment),
	}
	delegated := workflowrunner.NotifyHITLDecision(runID, decision)
	if !delegated {
		for i := 0; i < 10; i++ {
			time.Sleep(50 * time.Millisecond)
			if workflowrunner.NotifyHITLDecision(runID, decision) {
				delegated = true
				break
			}
		}
	}
	if delegated {
		c.JSON(http.StatusOK, gin.H{
			"workflowRunId":    runID,
			"status":           "delegated",
			"streamResuming":   true,
			"approved":         req.Approved,
		})
		return
	}
	result, err := workflowrunner.ResumeWorkflowRun(c.Request.Context(), workflowrunner.RunArgs{
		DB:             h.db,
		Logger:         h.logger,
		Role:           role,
		AppCfg:         h.cfg,
		Agent:          h.agent,
		ConversationID: run.ConversationID,
		ProjectID:      run.ProjectID,
	}, runID, req.Approved, req.Comment)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"response":      result.Response,
		"workflowRunId": result.RunID,
		"status":        result.Status,
		"awaitingHitl":  result.AwaitingHITL,
	})
}
