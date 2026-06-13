package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Version     string                `yaml:"version,omitempty" json:"version,omitempty"` // version displayed by the frontend, e.g. v1.3.3
	Server      ServerConfig          `yaml:"server"`
	Log         LogConfig             `yaml:"log"`
	MCP         MCPConfig             `yaml:"mcp"`
	OpenAI      OpenAIConfig          `yaml:"openai"`
	FOFA        FofaConfig            `yaml:"fofa,omitempty" json:"fofa,omitempty"`
	Agent       AgentConfig           `yaml:"agent"`
	Hitl        HitlConfig            `yaml:"hitl,omitempty" json:"hitl,omitempty"`
	Security    SecurityConfig        `yaml:"security"`
	Database    DatabaseConfig        `yaml:"database"`
	Auth        AuthConfig            `yaml:"auth"`
	Audit       AuditConfig           `yaml:"audit,omitempty" json:"audit,omitempty"`
	ExternalMCP ExternalMCPConfig     `yaml:"external_mcp,omitempty"`
	Knowledge   KnowledgeConfig       `yaml:"knowledge,omitempty"`
	C2          C2Config              `yaml:"c2,omitempty" json:"c2,omitempty"`                 // built-in C2 master switch; enabled by default when not configured
	Robots      RobotsConfig          `yaml:"robots,omitempty" json:"robots,omitempty"`         // robot configuration for WeCom, DingTalk, Lark, etc.
	RolesDir    string                `yaml:"roles_dir,omitempty" json:"roles_dir,omitempty"`   // role configuration file directory (new mode)
	Roles       map[string]RoleConfig `yaml:"roles,omitempty" json:"roles,omitempty"`           // backward compatibility: supports defining roles in the main configuration file
	SkillsDir   string                `yaml:"skills_dir,omitempty" json:"skills_dir,omitempty"` // Skills configuration file directory
	AgentsDir   string                `yaml:"agents_dir,omitempty" json:"agents_dir,omitempty"` // multi-agent sub-agent Markdown definition directory (*.md, YAML front matter)
	MultiAgent  MultiAgentConfig      `yaml:"multi_agent,omitempty" json:"multi_agent,omitempty"`
	Project     ProjectConfig         `yaml:"project,omitempty" json:"project,omitempty"`
	Vision      VisionConfig          `yaml:"vision,omitempty" json:"vision,omitempty"`
}

// ProjectConfig project blackboard (cross-conversation shared facts) configuration.
type ProjectConfig struct {
	Enabled                 bool   `yaml:"enabled" json:"enabled"`
	DefaultProjectID        string `yaml:"default_project_id,omitempty" json:"default_project_id,omitempty"` // default project bound when robots/batch tasks have no explicit project
	FactIndexMaxRunes       int    `yaml:"fact_index_max_runes,omitempty" json:"fact_index_max_runes,omitempty"`
	FactSummaryMaxRunes     int    `yaml:"fact_summary_max_runes,omitempty" json:"fact_summary_max_runes,omitempty"`
	DefaultInjectDeprecated bool   `yaml:"default_inject_deprecated,omitempty" json:"default_inject_deprecated,omitempty"`
}

// FactIndexMaxRunesEffective maximum rune count for automatic blackboard index injection.
func (c ProjectConfig) FactIndexMaxRunesEffective() int {
	if c.FactIndexMaxRunes <= 0 {
		return 3500
	}
	return c.FactIndexMaxRunes
}

// FactSummaryMaxRunesEffective upsert maximum summary rune count during upsert (one index line, should include verification points).
func (c ProjectConfig) FactSummaryMaxRunesEffective() int {
	if c.FactSummaryMaxRunes <= 0 {
		return 200
	}
	return c.FactSummaryMaxRunes
}

// MultiAgentConfig configures CloudWeGo Eino adk/prebuilt multi-agent orchestration (deep | plan_execute | supervisor).
type MultiAgentConfig struct {
	Enabled               bool   `yaml:"enabled" json:"enabled"`
	RobotDefaultAgentMode string `yaml:"robot_default_agent_mode,omitempty" json:"robot_default_agent_mode,omitempty"` // eino_single | deep | plan_execute | supervisor
	BatchUseMultiAgent    bool   `yaml:"batch_use_multi_agent" json:"batch_use_multi_agent"`                           // when true, each subtask in the batch task queue uses Eino multi-agent
	// Orchestration is deprecated and kept only for compatibility with old config.yaml; chat/WebShell request body orchestration decides the mode and defaults to deep when omitted.
	Orchestration string `yaml:"orchestration,omitempty" json:"orchestration,omitempty"`
	// MaxIteration deprecated: use agent.max_iterations (YAML field kept for backward compat; not read at runtime).
	MaxIteration int `yaml:"max_iteration,omitempty" json:"max_iteration,omitempty"`
	// PlanExecuteLoopMaxIterations plan_execute outer execute-replan loop limit; 0 uses Eino default 10.
	PlanExecuteLoopMaxIterations int `yaml:"plan_execute_loop_max_iterations,omitempty" json:"plan_execute_loop_max_iterations,omitempty"`
	// SubAgentMaxIterations deprecated: sub-agents and main agent both use agent.max_iterations (Markdown max_iterations>0 overrides).
	SubAgentMaxIterations int `yaml:"sub_agent_max_iterations,omitempty" json:"sub_agent_max_iterations,omitempty"`
	WithoutGeneralSubAgent       bool   `yaml:"without_general_sub_agent" json:"without_general_sub_agent"`
	WithoutWriteTodos            bool   `yaml:"without_write_todos" json:"without_write_todos"`
	OrchestratorInstruction      string `yaml:"orchestrator_instruction" json:"orchestrator_instruction"`
	// OrchestratorInstructionPlanExecute plan_execute main agent (planning side) system prompt; effective when non-empty and agents/orchestrator-plan-execute.md body is empty or missing. Do not mix with Deep orchestrator_instruction.
	OrchestratorInstructionPlanExecute string `yaml:"orchestrator_instruction_plan_execute,omitempty" json:"orchestrator_instruction_plan_execute,omitempty"`
	// OrchestratorInstructionSupervisor supervisor main agent system prompt (transfer/exit instructions are still appended at runtime); effective when non-empty and agents/orchestrator-supervisor.md body is empty or missing.
	OrchestratorInstructionSupervisor string                `yaml:"orchestrator_instruction_supervisor,omitempty" json:"orchestrator_instruction_supervisor,omitempty"`
	SubAgents                         []MultiAgentSubConfig `yaml:"sub_agents" json:"sub_agents"`
	// SubAgentUserContextMaxRunes caps the user-context supplement appended to task descriptions for sub-agents.
	// 0 (default) uses the built-in default of 2000 runes; negative value disables injection entirely.
	SubAgentUserContextMaxRunes int `yaml:"sub_agent_user_context_max_runes,omitempty" json:"sub_agent_user_context_max_runes,omitempty"`
	// EinoSkills configures CloudWeGo Eino ADK skill middleware + optional local filesystem/execute on DeepAgent.
	EinoSkills MultiAgentEinoSkillsConfig `yaml:"eino_skills,omitempty" json:"eino_skills,omitempty"`
	// EinoMiddleware wires optional ADK middleware (patchtoolcalls, toolsearch, plantask, reduction) and Deep extras.
	EinoMiddleware MultiAgentEinoMiddlewareConfig `yaml:"eino_middleware,omitempty" json:"eino_middleware,omitempty"`
	// EinoCallbacks attaches CloudWeGo eino callbacks.InitCallbacks on ADK Runner context (structured logs + optional SSE trace).
	EinoCallbacks MultiAgentEinoCallbacksConfig `yaml:"eino_callbacks,omitempty" json:"eino_callbacks,omitempty"`
}

// MultiAgentEinoCallbacksConfig enables Eino unified callbacks on each ADK agent run (deep / plan_execute / supervisor / eino_single).
// Modes: log_only (zap + optional OTel; no SSE to browser), sse (adds client SSE eino_trace_* when sse_trace_to_client), full (sse rules + stream callback copies closed).
type MultiAgentEinoCallbacksConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Mode    string `yaml:"mode,omitempty" json:"mode,omitempty"` // log_only | sse | full; empty with enabled=true defaults to log_only
	// SseTraceToClient when true emits eino_trace_* SSE for UI (use only for admin/debug; nil/false recommended in production).
	SseTraceToClient *bool `yaml:"sse_trace_to_client,omitempty" json:"sse_trace_to_client,omitempty"`
	// Otel configures OpenTelemetry trace export (independent of mode; exporter none disables export even if enabled).
	Otel MultiAgentEinoCallbacksOtelConfig `yaml:"otel,omitempty" json:"otel,omitempty"`
	// MaxInputSummaryRunes / MaxOutputSummaryRunes cap text placed in SSE payloads and debug logs (not full payloads).
	MaxInputSummaryRunes  int `yaml:"max_input_summary_runes,omitempty" json:"max_input_summary_runes,omitempty"`
	MaxOutputSummaryRunes int `yaml:"max_output_summary_runes,omitempty" json:"max_output_summary_runes,omitempty"`
	// ZapVerbose when true logs input/output summaries at zap.Debug on start/end; false uses Info with short fields only.
	ZapVerbose bool `yaml:"zap_verbose,omitempty" json:"zap_verbose,omitempty"`
}

// MultiAgentEinoCallbacksOtelConfig OpenTelemetry for Eino callback spans (W3C trace in collector / stdout).
type MultiAgentEinoCallbacksOtelConfig struct {
	Enabled      bool    `yaml:"enabled" json:"enabled"`
	ServiceName  string  `yaml:"service_name,omitempty" json:"service_name,omitempty"`
	Exporter     string  `yaml:"exporter,omitempty" json:"exporter,omitempty"`           // none | stdout | otlphttp
	OTLPEndpoint string  `yaml:"otlp_endpoint,omitempty" json:"otlp_endpoint,omitempty"` // host:port, e.g. localhost:4318 (path /v1/traces)
	SampleRatio  float64 `yaml:"sample_ratio,omitempty" json:"sample_ratio,omitempty"`   // 0–1, default 1.0
}

// EinoCallbacksModeEffective returns off | log_only | sse | full.
func (c MultiAgentEinoCallbacksConfig) EinoCallbacksModeEffective() string {
	if !c.Enabled {
		return "off"
	}
	m := strings.TrimSpace(strings.ToLower(c.Mode))
	switch m {
	case "log_only":
		return "log_only"
	case "sse":
		return "sse"
	case "full":
		return "full"
	case "":
		return "log_only"
	default:
		return "log_only"
	}
}

// SseTraceToClientEffective is false unless explicitly set true (best practice: do not expose framework traces to end users by default).
func (c MultiAgentEinoCallbacksConfig) SseTraceToClientEffective() bool {
	if c.SseTraceToClient == nil {
		return false
	}
	return *c.SseTraceToClient
}

// ShouldEmitEinoTraceSSE is true when client-visible trace events should be sent over progress/SSE.
func (c MultiAgentEinoCallbacksConfig) ShouldEmitEinoTraceSSE(mode string) bool {
	if !c.SseTraceToClientEffective() {
		return false
	}
	return mode == "sse" || mode == "full"
}

// OtelExporterEffective returns none | stdout | otlphttp.
func (c MultiAgentEinoCallbacksOtelConfig) OtelExporterEffective() string {
	e := strings.TrimSpace(strings.ToLower(c.Exporter))
	switch e {
	case "none", "stdout", "otlphttp":
		return e
	case "":
		if c.Enabled {
			return "stdout"
		}
		return "none"
	default:
		return "none"
	}
}

// OtelTracingActive is true when spans should be started (enabled + non-none exporter).
func (c MultiAgentEinoCallbacksConfig) OtelTracingActive() bool {
	if !c.Otel.Enabled {
		return false
	}
	return c.Otel.OtelExporterEffective() != "none"
}

func (c MultiAgentEinoCallbacksOtelConfig) ServiceNameEffective() string {
	s := strings.TrimSpace(c.ServiceName)
	if s != "" {
		return s
	}
	return "cyberstrike-ai"
}

func (c MultiAgentEinoCallbacksOtelConfig) SampleRatioEffective() float64 {
	r := c.SampleRatio
	if r <= 0 {
		return 1.0
	}
	if r > 1 {
		return 1.0
	}
	return r
}

func (c MultiAgentEinoCallbacksConfig) EinoCallbacksMaxInputSummaryRunes() int {
	if c.MaxInputSummaryRunes > 0 {
		return c.MaxInputSummaryRunes
	}
	return 400
}

func (c MultiAgentEinoCallbacksConfig) EinoCallbacksMaxOutputSummaryRunes() int {
	if c.MaxOutputSummaryRunes > 0 {
		return c.MaxOutputSummaryRunes
	}
	return 400
}

// MultiAgentEinoMiddlewareConfig optional Eino ADK middleware and Deep / supervisor tuning.
type MultiAgentEinoMiddlewareConfig struct {
	// PatchToolCalls inserts placeholder tool results for dangling assistant tool_calls (nil = enabled).
	PatchToolCalls *bool `yaml:"patch_tool_calls,omitempty" json:"patch_tool_calls,omitempty"`
	// ToolSearch enables dynamictool/toolsearch: hide tail tools until model calls tool_search (reduces prompt tools).
	ToolSearchEnable        bool `yaml:"tool_search_enable,omitempty" json:"tool_search_enable,omitempty"`
	ToolSearchMinTools      int  `yaml:"tool_search_min_tools,omitempty" json:"tool_search_min_tools,omitempty"`           // default 20; applies when len(tools) >= this
	ToolSearchAlwaysVisible int  `yaml:"tool_search_always_visible,omitempty" json:"tool_search_always_visible,omitempty"` // default 12; first N tools stay always visible
	// ToolSearchAlwaysVisibleTools keeps specified tool names always visible (never hidden by tool_search).
	ToolSearchAlwaysVisibleTools []string `yaml:"tool_search_always_visible_tools,omitempty" json:"tool_search_always_visible_tools,omitempty"`
	// Plantask adds TaskCreate/Get/Update/List (file-backed under skills dir); requires eino_skills + local backend.
	PlantaskEnable bool `yaml:"plantask_enable,omitempty" json:"plantask_enable,omitempty"`
	// PlantaskRelDir relative to skills_dir for per-conversation task boards (default .eino/plantask).
	PlantaskRelDir string `yaml:"plantask_rel_dir,omitempty" json:"plantask_rel_dir,omitempty"`
	// Reduction truncates/offloads large tool outputs (requires eino local backend for Write).
	ReductionEnable            bool     `yaml:"reduction_enable,omitempty" json:"reduction_enable,omitempty"`
	ReductionRootDir           string   `yaml:"reduction_root_dir,omitempty" json:"reduction_root_dir,omitempty"`                         // default: os temp + conversation id
	ReductionMaxLengthForTrunc int      `yaml:"reduction_max_length_for_trunc,omitempty" json:"reduction_max_length_for_trunc,omitempty"` // default 12000
	ReductionMaxTokensForClear int      `yaml:"reduction_max_tokens_for_clear,omitempty" json:"reduction_max_tokens_for_clear,omitempty"` // default 50000
	ReductionClearExclude      []string `yaml:"reduction_clear_exclude,omitempty" json:"reduction_clear_exclude,omitempty"`
	ReductionSubAgents         bool     `yaml:"reduction_sub_agents,omitempty" json:"reduction_sub_agents,omitempty"` // also attach to sub-agents
	// SummarizationTriggerRatio controls summarization trigger threshold as max_total_tokens * ratio (default 0.8).
	SummarizationTriggerRatio float64 `yaml:"summarization_trigger_ratio,omitempty" json:"summarization_trigger_ratio,omitempty"`
	// SummarizationEmitInternalEvents controls middleware internal event emission (default true).
	SummarizationEmitInternalEvents *bool `yaml:"summarization_emit_internal_events,omitempty" json:"summarization_emit_internal_events,omitempty"`
	// SummarizationRetryMaxAttempts is extra retries after the first summarization Generate attempt; 0 = default 3.
	SummarizationRetryMaxAttempts int `yaml:"summarization_retry_max_attempts,omitempty" json:"summarization_retry_max_attempts,omitempty"`
	// PlanExecuteUserInputBudgetRatio caps planner/replanner/executor userInput prompt budget ratio (default 0.35).
	PlanExecuteUserInputBudgetRatio float64 `yaml:"plan_execute_user_input_budget_ratio,omitempty" json:"plan_execute_user_input_budget_ratio,omitempty"`
	// PlanExecuteExecutedStepsBudgetRatio caps executed_steps prompt budget ratio (default 0.2).
	PlanExecuteExecutedStepsBudgetRatio float64 `yaml:"plan_execute_executed_steps_budget_ratio,omitempty" json:"plan_execute_executed_steps_budget_ratio,omitempty"`
	// PlanExecuteMaxStepResultRunes caps each executed step result length for prompt view (default 4000).
	PlanExecuteMaxStepResultRunes int `yaml:"plan_execute_max_step_result_runes,omitempty" json:"plan_execute_max_step_result_runes,omitempty"`
	// PlanExecuteKeepLastSteps keeps only the tail steps in prompt view (default 8).
	PlanExecuteKeepLastSteps int `yaml:"plan_execute_keep_last_steps,omitempty" json:"plan_execute_keep_last_steps,omitempty"`
	// CheckpointDir when non-empty enables adk.Runner CheckPointStore (file-backed) for interrupt/resume persistence.
	CheckpointDir string `yaml:"checkpoint_dir,omitempty" json:"checkpoint_dir,omitempty"`
	// DeepOutputKey passed to deep.Config OutputKey (session final text); empty = off.
	DeepOutputKey string `yaml:"deep_output_key,omitempty" json:"deep_output_key,omitempty"`
	// DeepModelRetryMaxRetries > 0 enables deep.Config ModelRetryConfig (framework-level chat model retries).
	DeepModelRetryMaxRetries int `yaml:"deep_model_retry_max_retries,omitempty" json:"deep_model_retry_max_retries,omitempty"`
	// RunRetryMaxAttempts > 0：429/5xx/handler segmented-resume attempts for 429/5xx/network jitter; 0 = default 10.
	RunRetryMaxAttempts int `yaml:"run_retry_max_attempts,omitempty" json:"run_retry_max_attempts,omitempty"`
	// RunRetryMaxBackoffSec maximum backoff seconds for one attempt; 0 = default 30.
	RunRetryMaxBackoffSec int `yaml:"run_retry_max_backoff_sec,omitempty" json:"run_retry_max_backoff_sec,omitempty"`
	// TaskToolDescriptionPrefix when non-empty sets deep.Config TaskToolDescriptionGenerator (sub-agent names appended).
	TaskToolDescriptionPrefix string `yaml:"task_tool_description_prefix,omitempty" json:"task_tool_description_prefix,omitempty"`
}

func (c MultiAgentEinoMiddlewareConfig) SummarizationTriggerRatioEffective() float64 {
	v := c.SummarizationTriggerRatio
	if v <= 0 {
		return 0.8
	}
	if v < 0.5 {
		return 0.5
	}
	if v > 0.95 {
		return 0.95
	}
	return v
}

func (c MultiAgentEinoMiddlewareConfig) SummarizationEmitInternalEventsEffective() bool {
	if c.SummarizationEmitInternalEvents != nil {
		return *c.SummarizationEmitInternalEvents
	}
	return true
}

func (c MultiAgentEinoMiddlewareConfig) PlanExecuteUserInputBudgetRatioEffective() float64 {
	v := c.PlanExecuteUserInputBudgetRatio
	if v <= 0 {
		return 0.35
	}
	if v < 0.1 {
		return 0.1
	}
	if v > 0.6 {
		return 0.6
	}
	return v
}

func (c MultiAgentEinoMiddlewareConfig) PlanExecuteExecutedStepsBudgetRatioEffective() float64 {
	v := c.PlanExecuteExecutedStepsBudgetRatio
	if v <= 0 {
		return 0.2
	}
	if v < 0.08 {
		return 0.08
	}
	if v > 0.5 {
		return 0.5
	}
	return v
}

func (c MultiAgentEinoMiddlewareConfig) PlanExecuteMaxStepResultRunesEffective() int {
	if c.PlanExecuteMaxStepResultRunes > 0 {
		return c.PlanExecuteMaxStepResultRunes
	}
	return 4000
}

func (c MultiAgentEinoMiddlewareConfig) PlanExecuteKeepLastStepsEffective() int {
	if c.PlanExecuteKeepLastSteps > 0 {
		return c.PlanExecuteKeepLastSteps
	}
	return 8
}

func (c MultiAgentEinoMiddlewareConfig) ReductionMaxLengthForTruncEffective() int {
	if c.ReductionMaxLengthForTrunc > 0 {
		return c.ReductionMaxLengthForTrunc
	}
	return 12000
}

func (c MultiAgentEinoMiddlewareConfig) ReductionMaxTokensForClearEffective() int {
	if c.ReductionMaxTokensForClear > 0 {
		return c.ReductionMaxTokensForClear
	}
	return 50000
}

// MultiAgentEinoSkillsConfig toggles Eino official skill progressive disclosure and host filesystem tools.
type MultiAgentEinoSkillsConfig struct {
	// Disable skips skill middleware (and does not attach local FS tools for Deep).
	Disable bool `yaml:"disable" json:"disable"`
	// FilesystemTools registers read_file/glob/grep/write/edit/execute (eino-ext local backend). Nil/omitted = true.
	FilesystemTools *bool `yaml:"filesystem_tools,omitempty" json:"filesystem_tools,omitempty"`
	// SkillToolName overrides the default Eino tool name "skill".
	SkillToolName string `yaml:"skill_tool_name,omitempty" json:"skill_tool_name,omitempty"`
}

// EinoSkillFilesystemToolsEffective returns whether Deep/sub-agents should attach local filesystem + streaming shell.
func (c MultiAgentEinoSkillsConfig) EinoSkillFilesystemToolsEffective() bool {
	if c.FilesystemTools != nil {
		return *c.FilesystemTools
	}
	return true
}

// PatchToolCallsEffective returns whether patchtoolcalls middleware should run (default true).
func (c MultiAgentEinoMiddlewareConfig) PatchToolCallsEffective() bool {
	if c.PatchToolCalls != nil {
		return *c.PatchToolCalls
	}
	return true
}

// MultiAgentSubConfig sub-agent (Eino ChatModelAgent): scheduled by task under deep, delegated by transfer under supervisor; plan_execute does not use the sub-agent list.
type MultiAgentSubConfig struct {
	ID            string   `yaml:"id" json:"id"`
	Name          string   `yaml:"name" json:"name"`
	Description   string   `yaml:"description" json:"description"`
	Instruction   string   `yaml:"instruction" json:"instruction"`
	BindRole      string   `yaml:"bind_role,omitempty" json:"bind_role,omitempty"` // optional: related role name in main config roles; uses that role tools when role_tools is not configured
	RoleTools     []string `yaml:"role_tools" json:"role_tools"`                   // same keys as single-Agent role tools; empty means all tools (bind_role can fill tools)
	MaxIterations int      `yaml:"max_iterations" json:"max_iterations"`
	Kind          string   `yaml:"kind,omitempty" json:"kind,omitempty"` // Markdown only: kind=orchestrator means Deep main agent (mutually exclusive by convention with orchestrator.md)
}

// MultiAgentPublic returns compact information for the frontend (without full sub-agent instructions).
type MultiAgentPublic struct {
	Enabled                               bool     `json:"enabled"`
	RobotDefaultAgentMode                 string   `json:"robot_default_agent_mode,omitempty"`
	BatchUseMultiAgent                    bool     `json:"batch_use_multi_agent"`
	SubAgentCount                         int      `json:"sub_agent_count"`
	Orchestration                         string   `json:"orchestration,omitempty"`
	PlanExecuteLoopMaxIterations          int      `json:"plan_execute_loop_max_iterations"`
	ToolSearchAlwaysVisibleTools          []string `json:"tool_search_always_visible_tools,omitempty"`
	ToolSearchAlwaysVisibleEffectiveTools []string `json:"tool_search_always_visible_effective_tools,omitempty"`
}

// NormalizeAgentMode parses agent mode (eino_single | deep | plan_execute | supervisor); empty defaults to eino_single.
func NormalizeAgentMode(mode string) string {
	s := strings.TrimSpace(strings.ToLower(mode))
	switch s {
	case "", "eino_single":
		return "eino_single"
	case "deep":
		return "deep"
	case "plan_execute", "plan-execute", "planexecute", "pe":
		return "plan_execute"
	case "supervisor", "super", "sv":
		return "supervisor"
	default:
		return "eino_single"
	}
}

// NormalizeRobotAgentMode parses the default robot conversation mode.
func NormalizeRobotAgentMode(ma MultiAgentConfig) string {
	return NormalizeAgentMode(ma.RobotDefaultAgentMode)
}

// NormalizeMultiAgentOrchestration returns deep, plan_execute, or supervisor.
func NormalizeMultiAgentOrchestration(s string) string {
	v := strings.TrimSpace(strings.ToLower(s))
	switch v {
	case "plan_execute", "plan-execute", "planexecute", "pe":
		return "plan_execute"
	case "supervisor", "super", "sv":
		return "supervisor"
	default:
		return "deep"
	}
}

// MultiAgentAPIUpdate settings page/API updates only multi-agent scalar fields; does not overwrite blocks such as sub_agents when writing YAML.
type MultiAgentAPIUpdate struct {
	Enabled                      bool   `json:"enabled"`
	RobotDefaultAgentMode        string `json:"robot_default_agent_mode,omitempty"`
	BatchUseMultiAgent           bool   `json:"batch_use_multi_agent"`
	PlanExecuteLoopMaxIterations *int   `json:"plan_execute_loop_max_iterations,omitempty"`
	// pointers distinguish "JSON did not pass this field" from "passed empty array to clear"; omitted fields must not overwrite the persistent tool whitelist in YAML.
	ToolSearchAlwaysVisibleTools *[]string `json:"tool_search_always_visible_tools,omitempty"`
}

// RobotsConfig robot configuration (WeCom, DingTalk, Lark, WeChat iLink, etc.)
type RobotsConfig struct {
	Session  RobotSessionConfig  `yaml:"session,omitempty" json:"session,omitempty"`   // robot session isolation policy
	Wechat   RobotWechatConfig   `yaml:"wechat,omitempty" json:"wechat,omitempty"`     // WeChat (iLink QR-code binding)
	Wecom    RobotWecomConfig    `yaml:"wecom,omitempty" json:"wecom,omitempty"`       // WeCom
	Dingtalk RobotDingtalkConfig `yaml:"dingtalk,omitempty" json:"dingtalk,omitempty"` // DingTalk
	Lark     RobotLarkConfig     `yaml:"lark,omitempty" json:"lark,omitempty"`         // Lark
}

// RobotWechatConfig WeChat iLink robot configuration (personal WeChat ClawBot / iLink protocol)
type RobotWechatConfig struct {
	Enabled       bool   `yaml:"enabled" json:"enabled"`
	BotToken      string `yaml:"bot_token,omitempty" json:"bot_token,omitempty"`
	ILinkBotID    string `yaml:"ilink_bot_id,omitempty" json:"ilink_bot_id,omitempty"`
	ILinkUserID   string `yaml:"ilink_user_id,omitempty" json:"ilink_user_id,omitempty"`
	BaseURL       string `yaml:"base_url,omitempty" json:"base_url,omitempty"`               // default https://ilinkai.weixin.qq.com
	BotType       string `yaml:"bot_type,omitempty" json:"bot_type,omitempty"`               // get_bot_qrcode parameter, default 3
	BotAgent      string `yaml:"bot_agent,omitempty" json:"bot_agent,omitempty"`             // base_info.bot_agent
	GetUpdatesBuf string `yaml:"get_updates_buf,omitempty" json:"get_updates_buf,omitempty"` // long-poll cursor (runtime)
}

// RobotSessionConfig robot session isolation policy
type RobotSessionConfig struct {
	StrictUserIdentity *bool `yaml:"strict_user_identity,omitempty" json:"strict_user_identity,omitempty"` // true only real user identifiers are allowed; conversation/group ID fallback is not allowed
}

// StrictUserIdentityEnabled returns whether strict user identity mode is enabled; defaults to true when not configured.
func (c RobotSessionConfig) StrictUserIdentityEnabled() bool {
	if c.StrictUserIdentity == nil {
		return true
	}
	return *c.StrictUserIdentity
}

// RobotWecomConfig WeCom robot configuration
type RobotWecomConfig struct {
	Enabled        bool   `yaml:"enabled" json:"enabled"`
	Token          string `yaml:"token" json:"token"`                       // callback URL verification token
	EncodingAESKey string `yaml:"encoding_aes_key" json:"encoding_aes_key"` // EncodingAESKey
	CorpID         string `yaml:"corp_id" json:"corp_id"`                   // corporate ID
	Secret         string `yaml:"secret" json:"secret"`                     // application secret
	AgentID        int64  `yaml:"agent_id" json:"agent_id"`                 // application AgentId
}

// RobotDingtalkConfig DingTalk robot configuration
type RobotDingtalkConfig struct {
	Enabled                     bool   `yaml:"enabled" json:"enabled"`
	ClientID                    string `yaml:"client_id" json:"client_id"`                                           // application Key (AppKey)
	ClientSecret                string `yaml:"client_secret" json:"client_secret"`                                   // application secret
	AllowConversationIDFallback bool   `yaml:"allow_conversation_id_fallback" json:"allow_conversation_id_fallback"` // sender_id whether to allow fallback to conversation ID when sender_id is missing
}

// RobotLarkConfig Lark robot configuration
type RobotLarkConfig struct {
	Enabled             bool   `yaml:"enabled" json:"enabled"`
	AppID               string `yaml:"app_id" json:"app_id"`                                 // application App ID
	AppSecret           string `yaml:"app_secret" json:"app_secret"`                         // application App Secret
	VerifyToken         string `yaml:"verify_token" json:"verify_token"`                     // event subscription Verification Token (optional)
	AllowChatIDFallback bool   `yaml:"allow_chat_id_fallback" json:"allow_chat_id_fallback"` // whether to allow fallback to chat_id when user ID is missing
}

type ServerConfig struct {
	Host string `yaml:"host" json:"host"`
	Port int    `yaml:"port" json:"port"`
	// TLSEnabled when true, the main Web UI uses HTTPS; modern browsers negotiate HTTP/2 on the same origin, reducing HTTP/1.1 per-origin connection limits.
	TLSEnabled bool `yaml:"tls_enabled,omitempty" json:"tls_enabled,omitempty"`
	// TLSCertPath / TLSKeyPath when non-empty, load certificates from PEM files (recommended for production).
	TLSCertPath string `yaml:"tls_cert_path,omitempty" json:"tls_cert_path,omitempty"`
	TLSKeyPath  string `yaml:"tls_key_path,omitempty" json:"tls_key_path,omitempty"`
	// TLSAutoSelfSign when true and no valid certificate path is configured, generate an in-memory self-signed certificate at startup (local/test only; browsers will warn that it is untrusted).
	TLSAutoSelfSign bool `yaml:"tls_auto_self_sign,omitempty" json:"tls_auto_self_sign,omitempty"`
	// TLSHTTPRedirect when false, disables HTTP-to-HTTPS redirects; when omitted or true and HTTPS is enabled, plaintext HTTP access is redirected to HTTPS with 308 (same-port protocol sniffing).
	TLSHTTPRedirect *bool `yaml:"tls_http_redirect,omitempty" json:"tls_http_redirect,omitempty"`
}

type LogConfig struct {
	Level  string `yaml:"level"`
	Output string `yaml:"output"`
}

type MCPConfig struct {
	Enabled         bool   `yaml:"enabled"`
	Host            string `yaml:"host"`
	Port            int    `yaml:"port"`
	AuthHeader      string `yaml:"auth_header,omitempty"`       // authentication header name; empty means no authentication
	AuthHeaderValue string `yaml:"auth_header_value,omitempty"` // authentication header value; must match that header in the request
}

type OpenAIConfig struct {
	Provider       string `yaml:"provider,omitempty" json:"provider,omitempty"` // API provider: "openai" (default) or "claude"; claude is automatically bridged to the Anthropic Messages API
	APIKey         string `yaml:"api_key" json:"api_key"`
	BaseURL        string `yaml:"base_url" json:"base_url"`
	Model          string `yaml:"model" json:"model"`
	MaxTotalTokens int    `yaml:"max_total_tokens,omitempty" json:"max_total_tokens,omitempty"`
	// Reasoning controls Eino ChatModel thinking / reasoning_effort / output_config and related fields (effective on Eino single-agent and multi-agent paths).
	Reasoning OpenAIReasoningConfig `yaml:"reasoning,omitempty" json:"reasoning,omitempty"`
}

// OpenAIReasoningConfig global defaults and gateway profile (conversation page can override through ChatRequest.reasoning, constrained by AllowClientReasoning).
type OpenAIReasoningConfig struct {
	// Mode: auto（default) | on | off | default (same as auto). off does not attach reasoning extension fields to the model.
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"`
	// Effort: low | medium | high | max | xhigh；max/xhigh are different gateway names for the highest tier and are sent as-is without conversion. Empty means no separate effort is specified.
	Effort string `yaml:"effort,omitempty" json:"effort,omitempty"`
	// AllowClientReasoning when false, ignores request-body reasoning; nil or unset is equivalent to true.
	AllowClientReasoning *bool `yaml:"allow_client_reasoning,omitempty" json:"allow_client_reasoning,omitempty"`
	// Profile: auto | deepseek_compat | openai_compat | output_config_effort
	Profile string `yaml:"profile,omitempty" json:"profile,omitempty"`
	// ExtraRequestFields merged into the Chat Completions root JSON (admin use; automatic fields override on key conflicts).
	ExtraRequestFields map[string]interface{} `yaml:"extra_request_fields,omitempty" json:"extra_request_fields,omitempty"`
}

// ModeEffective returns auto when empty or default.
func (c OpenAIReasoningConfig) ModeEffective() string {
	m := strings.ToLower(strings.TrimSpace(c.Mode))
	if m == "" || m == "default" {
		return "auto"
	}
	return m
}

// ProfileEffective returns auto when empty.
func (c OpenAIReasoningConfig) ProfileEffective() string {
	p := strings.ToLower(strings.TrimSpace(c.Profile))
	if p == "" {
		return "auto"
	}
	return p
}

// AllowClientReasoningEffective true when client may send ChatRequest.reasoning.
func (c OpenAIReasoningConfig) AllowClientReasoningEffective() bool {
	if c.AllowClientReasoning == nil {
		return true
	}
	return *c.AllowClientReasoning
}

type FofaConfig struct {
	// Email is the FOFA account email; APIKey is the FOFA API key (read-only key recommended)
	Email   string `yaml:"email,omitempty" json:"email,omitempty"`
	APIKey  string `yaml:"api_key,omitempty" json:"api_key,omitempty"`
	BaseURL string `yaml:"base_url,omitempty" json:"base_url,omitempty"` // default https://fofa.info/api/v1/search/all
}

type SecurityConfig struct {
	Tools               []ToolConfig `yaml:"tools,omitempty"`                 // backward compatibility: supports defining tools in the main configuration file
	ToolsDir            string       `yaml:"tools_dir,omitempty"`             // tool configuration file directory (new mode)
	ToolDescriptionMode string       `yaml:"tool_description_mode,omitempty"` // tool description mode: "short" | "full", default short
}

type DatabaseConfig struct {
	Path            string `yaml:"path"`                        // conversation database path
	KnowledgeDBPath string `yaml:"knowledge_db_path,omitempty"` // knowledge base database path (optional; empty uses conversation database)
}

type AgentConfig struct {
	MaxIterations        int    `yaml:"max_iterations" json:"max_iterations"`
	LargeResultThreshold int    `yaml:"large_result_threshold" json:"large_result_threshold"` // large result threshold (bytes), default 50KB
	ResultStorageDir     string `yaml:"result_storage_dir" json:"result_storage_dir"`         // result storage directory, default tmp
	ToolTimeoutMinutes   int    `yaml:"tool_timeout_minutes" json:"tool_timeout_minutes"`     // maximum duration for a single tool execution (minutes); terminates automatically on timeout to prevent hangs; 0 means unlimited (not recommended)
	// SystemPromptPath single-agent system prompt Markdown/text file path (relative to config.yaml directory, or writable absolute path). When non-empty and readable, replaces the built-in single-agent prompt; empty uses the built-in prompt.
	SystemPromptPath string `yaml:"system_prompt_path,omitempty" json:"system_prompt_path,omitempty"`
}

// HitlConfig global human-in-the-loop options; merged with conversation sidebar/API whitelist as a union before evaluation.
// tool_whitelist can be merged into config.yaml when applying from the sidebar and takes effect immediately; other fields still require restart if only the file is changed.
type HitlConfig struct {
	// ToolWhitelist global approval-exempt tool names (same semantics as sensitiveTools in each conversation configuration: whitelisted tools do not trigger HITL).
	ToolWhitelist []string `yaml:"tool_whitelist,omitempty" json:"tool_whitelist,omitempty"`
}

type AuthConfig struct {
	Password                    string `yaml:"password" json:"password"`
	SessionDurationHours        int    `yaml:"session_duration_hours" json:"session_duration_hours"`
	GeneratedPassword           string `yaml:"-" json:"-"`
	GeneratedPasswordPersisted  bool   `yaml:"-" json:"-"`
	GeneratedPasswordPersistErr string `yaml:"-" json:"-"`
}

// AuditConfig platform operation audit log settings (not chat/tool execution bodies).
type AuditConfig struct {
	// Enabled nil or true enables persistence; explicit false disables.
	Enabled        *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	RetentionDays  int   `yaml:"retention_days,omitempty" json:"retention_days,omitempty"`
	MaxDetailBytes int   `yaml:"max_detail_bytes,omitempty" json:"max_detail_bytes,omitempty"`
	// AuthFailureCooldownSeconds: per-IP cooldown for auth login/change_password failure audit rows; -1 disables; 0 uses default 60.
	AuthFailureCooldownSeconds int `yaml:"auth_failure_cooldown_seconds,omitempty" json:"auth_failure_cooldown_seconds,omitempty"`
}

// EnabledEffective returns true unless audit.enabled is explicitly false.
func (a AuditConfig) EnabledEffective() bool {
	if a.Enabled == nil {
		return true
	}
	return *a.Enabled
}

// RetentionDaysEffective returns retention; 0 means keep forever.
func (a AuditConfig) RetentionDaysEffective() int {
	if a.RetentionDays < 0 {
		return 0
	}
	return a.RetentionDays
}

// MaxDetailBytesEffective caps serialized detail JSON size.
func (a AuditConfig) MaxDetailBytesEffective() int {
	if a.MaxDetailBytes <= 0 {
		return 8192
	}
	return a.MaxDetailBytes
}

// AuthFailureCooldownEffective returns seconds between duplicate auth-failure audit rows per IP (default 60; -1 disables).
func (a AuditConfig) AuthFailureCooldownEffective() int {
	if a.AuthFailureCooldownSeconds < 0 {
		return 0
	}
	if a.AuthFailureCooldownSeconds == 0 {
		return 60
	}
	return a.AuthFailureCooldownSeconds
}

// ExternalMCPConfig external MCP configuration
type ExternalMCPConfig struct {
	Servers map[string]ExternalMCPServerConfig `yaml:"servers,omitempty" json:"servers,omitempty"`
}

// ExternalMCPServerConfig external MCP server configuration (follows official MCP config format, compatible with Claude Desktop / Cursor / VS Code).
// all string fields support ${VAR} and ${VAR:-default} environment variable expansion syntax.
type ExternalMCPServerConfig struct {
	// transport type: "stdio" | "sse" | "http" (Streamable HTTP).
	// stdio mode can be omitted and is inferred automatically when command is present.
	Type string `yaml:"type,omitempty" json:"type,omitempty"`

	// stdio mode configuration
	Command string            `yaml:"command,omitempty" json:"command,omitempty"`
	Args    []string          `yaml:"args,omitempty" json:"args,omitempty"`
	Env     map[string]string `yaml:"env,omitempty" json:"env,omitempty"`

	// HTTP/SSE mode configuration
	URL     string            `yaml:"url,omitempty" json:"url,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`

	// official standard fields
	Disabled    bool     `yaml:"disabled,omitempty" json:"disabled,omitempty"`       // disable server (official field)
	AutoApprove []string `yaml:"autoApprove,omitempty" json:"autoApprove,omitempty"` // auto-approved tool list (official field)

	// SDK advanced configuration (maps to MCP Go SDK transport-layer parameters)
	MaxRetries        int `yaml:"max_retries,omitempty" json:"max_retries,omitempty"`               // Streamable HTTP reconnection attempts after disconnect (default 5)
	TerminateDuration int `yaml:"terminate_duration,omitempty" json:"terminate_duration,omitempty"` // stdio seconds to wait for graceful stdio process shutdown (default 5)
	KeepAlive         int `yaml:"keep_alive,omitempty" json:"keep_alive,omitempty"`                 // client heartbeat interval in seconds (0 = disabled)

	// common configuration
	Description       string          `yaml:"description,omitempty" json:"description,omitempty"`
	Timeout           int             `yaml:"timeout,omitempty" json:"timeout,omitempty"`                         // connection timeout (seconds)
	ExternalMCPEnable bool            `yaml:"external_mcp_enable,omitempty" json:"external_mcp_enable,omitempty"` // whether enabled
	ToolEnabled       map[string]bool `yaml:"tool_enabled,omitempty" json:"tool_enabled,omitempty"`               // enabled status for each tool
}

// GetTransportType returns the effective transport type. Prefer Type, otherwise infer from Command/URL.
func (c ExternalMCPServerConfig) GetTransportType() string {
	if c.Type != "" {
		return c.Type
	}
	if c.Command != "" {
		return "stdio"
	}
	if c.URL != "" {
		return "http"
	}
	return ""
}

type ToolConfig struct {
	Name             string            `yaml:"name"`
	Command          string            `yaml:"command"`
	Args             []string          `yaml:"args,omitempty"`              // fixed arguments (optional)
	ShortDescription string            `yaml:"short_description,omitempty"` // short description (for tool lists, reduces token usage)
	Description      string            `yaml:"description"`                 // detailed description (for tool documentation)
	Enabled          bool              `yaml:"enabled"`
	Parameters       []ParameterConfig `yaml:"parameters,omitempty"`         // parameter definitions (optional)
	ArgMapping       string            `yaml:"arg_mapping,omitempty"`        // argument mapping mode: "auto", "manual", "template" (optional)
	AllowedExitCodes []int             `yaml:"allowed_exit_codes,omitempty"` // allowed exit code list (some tools return nonzero exit codes even on success)
}

// ParameterConfig parameter configuration
type ParameterConfig struct {
	Name        string      `yaml:"name"`                // parameter name
	Type        string      `yaml:"type"`                // parameter type: string, int, bool, array
	Description string      `yaml:"description"`         // parameter description
	Required    bool        `yaml:"required,omitempty"`  // whether required
	Default     interface{} `yaml:"default,omitempty"`   // default value
	ItemType    string      `yaml:"item_type,omitempty"` // array item type when type is array, such as string, number, object
	Flag        string      `yaml:"flag,omitempty"`      // command-line flag, such as "-u", "--url", "-p"
	Position    *int        `yaml:"position,omitempty"`  // positional argument index (starting from 0)
	Format      string      `yaml:"format,omitempty"`    // parameter format: "flag", "positional", "combined" (flag=value), "template"
	Template    string      `yaml:"template,omitempty"`  // template string, such as "{flag} {value}" or "{value}"
	Options     []string    `yaml:"options,omitempty"`   // allowed values list (for enum)
}

func Load(path string) (*Config, error) {
	if absPath, err := filepath.Abs(path); err == nil {
		if resolvedPath, err := filepath.EvalSymlinks(absPath); err == nil {
			path = resolvedPath
		} else {
			path = absPath
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read configuration file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse configuration file: %w", err)
	}

	if cfg.Auth.SessionDurationHours <= 0 {
		cfg.Auth.SessionDurationHours = 12
	}
	if cfg.Audit.MaxDetailBytes <= 0 {
		cfg.Audit.MaxDetailBytes = 8192
	}
	if strings.TrimSpace(cfg.Auth.Password) == "" {
		password, err := generateStrongPassword(24)
		if err != nil {
			return nil, fmt.Errorf("failed to generate default password: %w", err)
		}

		cfg.Auth.Password = password
		cfg.Auth.GeneratedPassword = password

		if err := PersistAuthPassword(path, password); err != nil {
			cfg.Auth.GeneratedPasswordPersisted = false
			cfg.Auth.GeneratedPasswordPersistErr = err.Error()
		} else {
			cfg.Auth.GeneratedPasswordPersisted = true
		}
	}

	// if tools directory is configured, load tool configurations from the directory
	if cfg.Security.ToolsDir != "" {
		configDir := filepath.Dir(path)
		toolsDir := cfg.Security.ToolsDir

		// if relative, resolve from the real configuration file directory
		if !filepath.IsAbs(toolsDir) {
			toolsDir = filepath.Join(configDir, toolsDir)
		}

		tools, err := LoadToolsFromDir(toolsDir)
		if err != nil {
			return nil, fmt.Errorf("failed to load tool configurations from tools directory: %w", err)
		}

		// merge tool configurations: tools in the directory take precedence, and tools in the main config are supplemental
		existingTools := make(map[string]bool)
		for _, tool := range tools {
			existingTools[tool.Name] = true
		}

		// add tools from the main config that do not exist in the directory (backward compatibility)
		for _, tool := range cfg.Security.Tools {
			if !existingTools[tool.Name] {
				tools = append(tools, tool)
			}
		}

		cfg.Security.Tools = tools
	}

	// external MCP: migration + environment variable expansion
	if cfg.ExternalMCP.Servers != nil {
		for name, serverCfg := range cfg.ExternalMCP.Servers {
			// official disabled field -> ExternalMCPEnable
			if serverCfg.Disabled {
				serverCfg.ExternalMCPEnable = false
			} else if !serverCfg.ExternalMCPEnable {
				// enabled by default
				serverCfg.ExternalMCPEnable = true
			}

			// expand all ${VAR} / ${VAR:-default} environment variable references
			ExpandConfigEnv(&serverCfg)

			cfg.ExternalMCP.Servers[name] = serverCfg
		}
	}

	// load role configurations from roles directory
	if cfg.RolesDir != "" {
		configDir := filepath.Dir(path)
		rolesDir := cfg.RolesDir

		// if relative, resolve from the real configuration file directory
		if !filepath.IsAbs(rolesDir) {
			rolesDir = filepath.Join(configDir, rolesDir)
		}

		roles, err := LoadRolesFromDir(rolesDir)
		if err != nil {
			return nil, fmt.Errorf("failed to load role configurations from roles directory: %w", err)
		}

		cfg.Roles = roles
	} else {
		// if roles_dir is not configured, initialize as an empty map
		if cfg.Roles == nil {
			cfg.Roles = make(map[string]RoleConfig)
		}
	}

	return &cfg, nil
}

func generateStrongPassword(length int) (string, error) {
	if length <= 0 {
		length = 24
	}

	bytesLen := length
	randomBytes := make([]byte, bytesLen)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}

	password := base64.RawURLEncoding.EncodeToString(randomBytes)
	if len(password) > length {
		password = password[:length]
	}
	return password, nil
}

func PersistAuthPassword(path, password string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	inAuthBlock := false
	authIndent := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inAuthBlock {
			if strings.HasPrefix(trimmed, "auth:") {
				inAuthBlock = true
				authIndent = len(line) - len(strings.TrimLeft(line, " "))
			}
			continue
		}

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		leadingSpaces := len(line) - len(strings.TrimLeft(line, " "))
		if leadingSpaces <= authIndent {
			// leave auth block
			inAuthBlock = false
			authIndent = -1
			// continue looking for other auth blocks (theoretically none)
			if strings.HasPrefix(trimmed, "auth:") {
				inAuthBlock = true
				authIndent = leadingSpaces
			}
			continue
		}

		if strings.HasPrefix(strings.TrimSpace(line), "password:") {
			prefix := line[:len(line)-len(strings.TrimLeft(line, " "))]
			comment := ""
			if idx := strings.Index(line, "#"); idx >= 0 {
				comment = strings.TrimRight(line[idx:], " ")
			}

			newLine := fmt.Sprintf("%spassword: %s", prefix, password)
			if comment != "" {
				if !strings.HasPrefix(comment, " ") {
					newLine += " "
				}
				newLine += comment
			}
			lines[i] = newLine
			break
		}
	}

	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}

func PrintGeneratedPasswordWarning(password string, persisted bool, persistErr string) {
	if strings.TrimSpace(password) == "" {
		return
	}

	if persisted {
		fmt.Println("[CyberStrikeAI] ✅ Web login password has been generated and written automatically.")
	} else {
		if persistErr != "" {
			fmt.Printf("[CyberStrikeAI] ⚠️ could not automatically write password to configuration file: %s\n", persistErr)
		} else {
			fmt.Println("[CyberStrikeAI] ⚠️ could not automatically write password to configuration file。")
		}
		fmt.Println("Please manually write the following random password to auth.password in config.yaml:")
	}

	fmt.Println("----------------------------------------------------------------")
	fmt.Println("CyberStrikeAI Auto-Generated Web Password")
	fmt.Printf("Password: %s\n", password)
	fmt.Println("WARNING: Anyone with this password can fully control CyberStrikeAI.")
	fmt.Println("Please store it securely and change it in config.yaml as soon as possible.")
	fmt.Println("Warning: anyone holding this password has full control over CyberStrikeAI.")
	fmt.Println("Keep it secure and change auth.password in config.yaml as soon as possible!")
	fmt.Println("----------------------------------------------------------------")
}

// generateRandomToken generate a random string for MCP authentication (64 hexadecimal characters)
func generateRandomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// persistMCPAuth write MCP auth_header / auth_header_value back to the configuration file
func persistMCPAuth(path string, mcp *MCPConfig) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	inMcpBlock := false
	mcpIndent := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inMcpBlock {
			if strings.HasPrefix(trimmed, "mcp:") {
				inMcpBlock = true
				mcpIndent = len(line) - len(strings.TrimLeft(line, " "))
			}
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		leadingSpaces := len(line) - len(strings.TrimLeft(line, " "))
		if leadingSpaces <= mcpIndent {
			inMcpBlock = false
			mcpIndent = -1
			if strings.HasPrefix(trimmed, "mcp:") {
				inMcpBlock = true
				mcpIndent = leadingSpaces
			}
			continue
		}

		prefix := line[:leadingSpaces]
		rest := strings.TrimSpace(line[leadingSpaces:])
		comment := ""
		if idx := strings.Index(line, "#"); idx >= 0 {
			comment = strings.TrimRight(line[idx:], " ")
		}
		withComment := ""
		if comment != "" {
			if !strings.HasPrefix(comment, " ") {
				withComment = " "
			}
			withComment += comment
		}

		if strings.HasPrefix(rest, "auth_header_value:") {
			lines[i] = fmt.Sprintf("%sauth_header_value: %q%s", prefix, mcp.AuthHeaderValue, withComment)
		} else if strings.HasPrefix(rest, "auth_header:") {
			lines[i] = fmt.Sprintf("%sauth_header: %q%s", prefix, mcp.AuthHeader, withComment)
		}
	}

	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}

// EnsureMCPAuth when MCP is enabled and auth_header_value is empty, automatically generate a random key and write it back to config
func EnsureMCPAuth(path string, cfg *Config) error {
	if !cfg.MCP.Enabled || strings.TrimSpace(cfg.MCP.AuthHeaderValue) != "" {
		return nil
	}
	token, err := generateRandomToken()
	if err != nil {
		return fmt.Errorf("failed to generate MCP authentication key: %w", err)
	}
	cfg.MCP.AuthHeaderValue = token
	if strings.TrimSpace(cfg.MCP.AuthHeader) == "" {
		cfg.MCP.AuthHeader = "X-MCP-Token"
	}
	return persistMCPAuth(path, &cfg.MCP)
}

// PrintMCPConfigJSON prints MCP configuration JSON to the terminal for direct copy into Cursor / Claude Code mcp configuration
func PrintMCPConfigJSON(mcp MCPConfig) {
	if !mcp.Enabled {
		return
	}
	hostForURL := strings.TrimSpace(mcp.Host)
	if hostForURL == "" || hostForURL == "0.0.0.0" {
		hostForURL = "localhost"
	}
	url := fmt.Sprintf("http://%s:%d/mcp", hostForURL, mcp.Port)
	headers := map[string]string{}
	if mcp.AuthHeader != "" {
		headers[mcp.AuthHeader] = mcp.AuthHeaderValue
	}
	serverEntry := map[string]interface{}{
		"url": url,
	}
	if len(headers) > 0 {
		serverEntry["headers"] = headers
	}
	// Claude Code requires type: "http"
	serverEntry["type"] = "http"
	out := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"cyberstrike-ai": serverEntry,
		},
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println("[CyberStrikeAI] MCP configuration (can be copied for Cursor / Claude Code use)：")
	fmt.Println("  Cursor: put into mcpServers in ~/.cursor/mcp.json, or project .cursor/mcp.json")
	fmt.Println("  Claude Code: put into mcpServers in .mcp.json or ~/.claude.json")
	fmt.Println("----------------------------------------------------------------")
	fmt.Println(string(b))
	fmt.Println("----------------------------------------------------------------")
}

// LoadToolsFromDir load all tool configuration files from a directory
func LoadToolsFromDir(dir string) ([]ToolConfig, error) {
	var tools []ToolConfig

	// check whether the directory exists
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return tools, nil // return an empty list when the directory does not exist, without error
	}

	// read all .yaml and .yml files in the directory
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read tools directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		filePath := filepath.Join(dir, name)
		tool, err := LoadToolFromFile(filePath)
		if err != nil {
			// record the error but continue loading other files
			fmt.Printf("Warning: failed to load tool configuration file %s: %v\n", filePath, err)
			continue
		}

		tools = append(tools, *tool)
	}

	return tools, nil
}

// LoadToolFromFile load tool configuration from a single file
func LoadToolFromFile(path string) (*ToolConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var tool ToolConfig
	if err := yaml.Unmarshal(data, &tool); err != nil {
		return nil, fmt.Errorf("failed to parse tool configuration: %w", err)
	}

	// validate required fields
	if tool.Name == "" {
		return nil, fmt.Errorf("tool name cannot be empty")
	}
	if tool.Command == "" {
		return nil, fmt.Errorf("tool command cannot be empty")
	}

	return &tool, nil
}

// LoadRolesFromDir load all role configuration files from a directory
func LoadRolesFromDir(dir string) (map[string]RoleConfig, error) {
	roles := make(map[string]RoleConfig)

	// check whether the directory exists
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return roles, nil // return an empty map when the directory does not exist, without error
	}

	// read all .yaml and .yml files in the directory
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read roles directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		filePath := filepath.Join(dir, name)
		role, err := LoadRoleFromFile(filePath)
		if err != nil {
			// record the error but continue loading other files
			fmt.Printf("Warning: failed to load role configuration file %s: %v\n", filePath, err)
			continue
		}

		// use role name as key
		roleName := role.Name
		if roleName == "" {
			// if role name is empty, use the file name (without extension) as the name
			roleName = strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml")
			role.Name = roleName
		}

		roles[roleName] = *role
	}

	return roles, nil
}

// LoadRoleFromFile load role configuration from a single file
func LoadRoleFromFile(path string) (*RoleConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var role RoleConfig
	if err := yaml.Unmarshal(data, &role); err != nil {
		return nil, fmt.Errorf("failed to parse role configuration: %w", err)
	}

	// process icon field: if it contains Unicode escape format (\U0001F3C6), convert it to the actual Unicode character
	// Go yaml library may not automatically parse \U escape sequences, so manual conversion is needed
	if role.Icon != "" {
		icon := role.Icon
		// remove possible quotes
		icon = strings.Trim(icon, `"`)

		// check whether it is Unicode escape format \U0001F3C6 (8 hex digits) or \uXXXX (4 hex digits)
		if len(icon) >= 3 && icon[0] == '\\' {
			if icon[1] == 'U' && len(icon) >= 10 {
				// \U0001F3C6 format (8 hex digits)
				if codePoint, err := strconv.ParseInt(icon[2:10], 16, 32); err == nil {
					role.Icon = string(rune(codePoint))
				}
			} else if icon[1] == 'u' && len(icon) >= 6 {
				// \uXXXX format (4 hex digits)
				if codePoint, err := strconv.ParseInt(icon[2:6], 16, 32); err == nil {
					role.Icon = string(rune(codePoint))
				}
			}
		}
	}

	// validate required fields
	if role.Name == "" {
		// if name is empty, try to get it from the file name
		baseName := filepath.Base(path)
		role.Name = strings.TrimSuffix(strings.TrimSuffix(baseName, ".yaml"), ".yml")
	}

	return &role, nil
}

func Default() *Config {
	strictRobotIdentity := true
	return &Config{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 8080,
		},
		Log: LogConfig{
			Level:  "info",
			Output: "stdout",
		},
		MCP: MCPConfig{
			Enabled: true,
			Host:    "0.0.0.0",
			Port:    8081,
		},
		OpenAI: OpenAIConfig{
			BaseURL:        "https://api.openai.com/v1",
			Model:          "gpt-4",
			MaxTotalTokens: 120000,
		},
		Agent: AgentConfig{
			MaxIterations:      30, // default maximum iteration count
			ToolTimeoutMinutes: 10, // default single tool execution limit is 10 minutes to avoid abnormal long-running occupation
		},
		Security: SecurityConfig{
			Tools:    []ToolConfig{}, // tool configuration should be loaded from config.yaml or the tools/ directory
			ToolsDir: "tools",        // default tools directory
		},
		Database: DatabaseConfig{
			Path:            "data/conversations.db",
			KnowledgeDBPath: "data/knowledge.db", // default knowledge base database path
		},
		Auth: AuthConfig{
			SessionDurationHours: 12,
		},
		Audit: func() AuditConfig {
			on := true
			return AuditConfig{
				RetentionDays:  90,
				MaxDetailBytes: 8192,
				Enabled:        &on,
			}
		}(),
		Robots: RobotsConfig{
			Session: RobotSessionConfig{
				StrictUserIdentity: &strictRobotIdentity,
			},
		},
		Knowledge: KnowledgeConfig{
			Enabled:  true,
			BasePath: "knowledge_base",
			Embedding: EmbeddingConfig{
				Provider: "openai",
				Model:    "text-embedding-3-small",
				BaseURL:  "https://api.openai.com/v1",
			},
			Retrieval: RetrievalConfig{
				TopK:                5,
				SimilarityThreshold: 0.65, // lower threshold to 0.65 to reduce false negatives
			},
			Indexing: IndexingConfig{
				ChunkStrategy:         "markdown_then_recursive",
				RequestTimeoutSeconds: 120,
				ChunkSize:             768, // increase to 768 for better context preservation
				ChunkOverlap:          50,
				MaxChunksPerItem:      20, // limit each knowledge item to at most 20 chunks to avoid excessive quota usage
				BatchSize:             64,
				PreferSourceFile:      false,
				MaxRPM:                100, // default 100 RPM to avoid 429 errors
				RateLimitDelayMs:      600, // 600ms interval, corresponding to 100 RPM
				MaxRetries:            3,
				RetryDelayMs:          1000,
				SubIndexes:            nil,
			},
		},
	}
}

// C2Config built-in C2 module switch (same semantics as knowledge base enabled: when disabled, listeners are not initialized and C2 MCP tools are not registered).
type C2Config struct {
	// Enabled nil means not configured and is treated as true (compatible with old config.yaml)
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

// EnabledEffective returns whether C2 is enabled; enabled by default when not explicitly configured.
func (c C2Config) EnabledEffective() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// C2Public returns C2 status for the frontend (scalars only).
type C2Public struct {
	Enabled bool `json:"enabled"`
}

// Public converts internal configuration to an API response.
func (c C2Config) Public() C2Public {
	return C2Public{Enabled: c.EnabledEffective()}
}

// C2APIUpdate settings page/API update for C2 switch.
type C2APIUpdate struct {
	Enabled bool `json:"enabled"`
}

// KnowledgeConfig knowledge base configuration
type KnowledgeConfig struct {
	Enabled   bool            `yaml:"enabled" json:"enabled"`     // whether knowledge retrieval is enabled
	BasePath  string          `yaml:"base_path" json:"base_path"` // knowledge base path
	Embedding EmbeddingConfig `yaml:"embedding" json:"embedding"`
	Retrieval RetrievalConfig `yaml:"retrieval" json:"retrieval"`
	Indexing  IndexingConfig  `yaml:"indexing,omitempty" json:"indexing,omitempty"` // index build configuration
}

// IndexingConfig index build configuration (controls behavior while building the knowledge base index)
type IndexingConfig struct {
	// ChunkStrategy: "markdown_then_recursive"（default, split by Eino Markdown headings then recursively split) or "recursive" (recursive split only)
	ChunkStrategy string `yaml:"chunk_strategy,omitempty" json:"chunk_strategy,omitempty"`
	// RequestTimeoutSeconds embedding HTTP client timeout (seconds), 0 uses default 120
	RequestTimeoutSeconds int `yaml:"request_timeout_seconds,omitempty" json:"request_timeout_seconds,omitempty"`
	// chunking configuration
	ChunkSize        int `yaml:"chunk_size,omitempty" json:"chunk_size,omitempty"`                   // maximum tokens per chunk (estimated), default 512
	ChunkOverlap     int `yaml:"chunk_overlap,omitempty" json:"chunk_overlap,omitempty"`             // overlap tokens between chunks, default 50
	MaxChunksPerItem int `yaml:"max_chunks_per_item,omitempty" json:"max_chunks_per_item,omitempty"` // maximum chunks per knowledge item, 0 means unlimited

	// PreferSourceFile when true, prefer Eino FileLoader to read original text from file_path before indexing (disk wins when it differs from stored content)
	PreferSourceFile bool `yaml:"prefer_source_file,omitempty" json:"prefer_source_file,omitempty"`

	// rate limit configuration (to avoid API rate limits)
	RateLimitDelayMs int `yaml:"rate_limit_delay_ms,omitempty" json:"rate_limit_delay_ms,omitempty"` // request interval (milliseconds), 0 means no fixed delay
	MaxRPM           int `yaml:"max_rpm,omitempty" json:"max_rpm,omitempty"`                         // maximum requests per minute, 0 means unlimited

	// retry configuration (for transient errors)
	MaxRetries   int `yaml:"max_retries,omitempty" json:"max_retries,omitempty"`       // maximum retry count, default 3
	RetryDelayMs int `yaml:"retry_delay_ms,omitempty" json:"retry_delay_ms,omitempty"` // retry interval (milliseconds), default 1000

	// BatchSize embedding batch size (SQLite index writes), 0 means default 64
	BatchSize int `yaml:"batch_size,omitempty" json:"batch_size,omitempty"`
	// SubIndexes passed into Eino indexer.WithSubIndexes (logical partition markers passed via Document metadata)
	SubIndexes []string `yaml:"sub_indexes,omitempty" json:"sub_indexes,omitempty"`
}

// EmbeddingConfig embedding configuration
type EmbeddingConfig struct {
	Provider string `yaml:"provider" json:"provider"` // embedding model provider
	Model    string `yaml:"model" json:"model"`       // model name
	BaseURL  string `yaml:"base_url" json:"base_url"` // API Base URL
	APIKey   string `yaml:"api_key" json:"api_key"`   // API Key（inherited from OpenAI configuration)
}

// PostRetrieveConfig post-retrieval processing: always normalizes and deduplicates content (best practice) and truncates to context budget; PrefetchTopK fetches extra candidates before converging to top_k.
type PostRetrieveConfig struct {
	// PrefetchTopK maximum candidates retained during vector retrieval (cosine order); should be >= top_k, 0 means same as top_k; upper bound is defined in the knowledge package constants.
	PrefetchTopK int `yaml:"prefetch_top_k,omitempty" json:"prefetch_top_k,omitempty"`
	// MaxContextChars returns maximum total Unicode character count for document content (whole chunks, no mid-chunk truncation); 0 means unlimited.
	MaxContextChars int `yaml:"max_context_chars,omitempty" json:"max_context_chars,omitempty"`
	// MaxContextTokens returns maximum total token count for document content (tiktoken, mapped by embedding model name, falling back to cl100k_base); 0 means unlimited.
	MaxContextTokens int `yaml:"max_context_tokens,omitempty" json:"max_context_tokens,omitempty"`
}

// RetrievalConfig retrieval configuration
type RetrievalConfig struct {
	TopK                int     `yaml:"top_k" json:"top_k"`                               // retrieval top-K
	SimilarityThreshold float64 `yaml:"similarity_threshold" json:"similarity_threshold"` // cosine similarity threshold
	// SubIndexFilter when non-empty, keep only rows whose sub_indexes contain one of the comma-separated labels; old rows with empty sub_indexes are still returned.
	SubIndexFilter string `yaml:"sub_index_filter,omitempty" json:"sub_index_filter,omitempty"`
	// PostRetrieve post-retrieval processing (deduplication, budget truncation); reranking is injected in code via [knowledge.DocumentReranker].
	PostRetrieve PostRetrieveConfig `yaml:"post_retrieve,omitempty" json:"post_retrieve,omitempty"`
}

// RolesConfig role configuration (deprecated; use map[string]RoleConfig instead)
// keep this type for old code compatibility, but prefer map[string]RoleConfig directly
type RolesConfig struct {
	Roles map[string]RoleConfig `yaml:"roles,omitempty" json:"roles,omitempty"`
}

// RoleConfig single role configuration
type RoleConfig struct {
	Name          string   `yaml:"name" json:"name"`                                         // role name
	NameEn        string   `yaml:"name_en,omitempty" json:"name_en,omitempty"`               // English role name (optional)
	Description   string   `yaml:"description" json:"description"`                           // role description
	DescriptionEn string   `yaml:"description_en,omitempty" json:"description_en,omitempty"` // English role description (optional)
	UserPrompt    string   `yaml:"user_prompt" json:"user_prompt"`                           // user prompt (prepended to user messages)
	Icon          string   `yaml:"icon,omitempty" json:"icon,omitempty"`                     // role icon (optional)
	Tools         []string `yaml:"tools,omitempty" json:"tools,omitempty"`                   // related tool list (toolKey format, such as "toolName" or "mcpName::toolName")
	MCPs          []string `yaml:"mcps,omitempty" json:"mcps,omitempty"`                     // backward compatibility: related MCP server list (deprecated, use tools instead)
	Enabled       bool     `yaml:"enabled" json:"enabled"`                                   // whether enabled
}
