package multiagent

import (
	"context"
	"fmt"
	"strings"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/planexecute"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// PlanExecuteRootArgs contains the parameters required to build the Eino adk/prebuilt/planexecute root Agent.
type PlanExecuteRootArgs struct {
	MainToolCallingModel *openai.ChatModel
	ExecModel            *openai.ChatModel
	OrchInstruction      string
	ToolsCfg             adk.ToolsConfig
	ExecMaxIter          int
	LoopMaxIter          int
	// AppCfg / Logger attach the same Eino summarization middleware used by Deep/Supervisor to the Executor when non-nil.
	AppCfg *config.Config
	MwCfg  *config.MultiAgentEinoMiddlewareConfig
	// ConversationID is used for transcript/isolation paths in middleware.
	ConversationID string
	Logger         *zap.Logger
	// ModelName is used for model input token estimation logs.
	ModelName string
	// ExecPreMiddlewares are prepended middlewares built by prependEinoMiddlewares (patchtoolcalls, reduction, toolsearch, plantask),
	// consistent with mainOrchestratorPre for the Deep/Supervisor main agent.
	ExecPreMiddlewares []adk.ChatModelAgentMiddleware
	// SkillMiddleware is the official Eino skill progressive-disclosure middleware; optional.
	SkillMiddleware adk.ChatModelAgentMiddleware
	// FilesystemMiddleware is the Eino filesystem middleware, providing local file read/write and shell capabilities when eino_skills.filesystem_tools is enabled; optional.
	FilesystemMiddleware adk.ChatModelAgentMiddleware
	// PlannerReplannerRewriteHandlers applies BeforeModelRewriteState pipeline for planner/replanner input.
	PlannerReplannerRewriteHandlers []adk.ChatModelAgentMiddleware
	// ModelFacingTrace is optional: the end of the Executor Handlers chain writes to it so last_react aligns with context after summarization.
	ModelFacingTrace *modelFacingTraceHolder
}

// NewPlanExecuteRoot returns the preset plan -> execute -> replan orchestration root node, alongside Deep / Supervisor.
func NewPlanExecuteRoot(ctx context.Context, a *PlanExecuteRootArgs) (adk.ResumableAgent, error) {
	if a == nil {
		return nil, fmt.Errorf("plan_execute: args is nil")
	}
	if a.MainToolCallingModel == nil || a.ExecModel == nil {
		return nil, fmt.Errorf("plan_execute: model is nil")
	}
	tcm, ok := interface{}(a.MainToolCallingModel).(model.ToolCallingChatModel)
	if !ok {
		return nil, fmt.Errorf("plan_execute: main model must implement ToolCallingChatModel")
	}
	plannerCfg := &planexecute.PlannerConfig{
		ToolCallingChatModel: tcm,
		NewPlan:              newLenientPlan,
	}
	if fn := planExecutePlannerGenInput(a.OrchInstruction, a.AppCfg, a.MwCfg, a.Logger, a.ModelName, a.ConversationID, a.PlannerReplannerRewriteHandlers); fn != nil {
		plannerCfg.GenInputFn = fn
	}
	planner, err := planexecute.NewPlanner(ctx, plannerCfg)
	if err != nil {
		return nil, fmt.Errorf("plan_execute planner: %w", err)
	}
	replanner, err := planexecute.NewReplanner(ctx, &planexecute.ReplannerConfig{
		ChatModel:  tcm,
		GenInputFn: planExecuteReplannerGenInput(a.OrchInstruction, a.AppCfg, a.MwCfg, a.Logger, a.ModelName, a.ConversationID, a.PlannerReplannerRewriteHandlers),
		NewPlan:    newLenientPlan,
	})
	if err != nil {
		return nil, fmt.Errorf("plan_execute replanner: %w", err)
	}

	// Assemble the executor handler stack in the same order as the Deep/Supervisor main agent (outermost first).
	var execHandlers []adk.ChatModelAgentMiddleware
	// 1. patchtoolcalls, reduction, toolsearch, plantask from prependEinoMiddlewares.
	if len(a.ExecPreMiddlewares) > 0 {
		execHandlers = append(execHandlers, a.ExecPreMiddlewares...)
	}
	// 2. filesystem middleware; optional.
	if a.FilesystemMiddleware != nil {
		execHandlers = append(execHandlers, a.FilesystemMiddleware)
	}
	// 3. skill middleware; optional.
	if a.SkillMiddleware != nil {
		execHandlers = append(execHandlers, a.SkillMiddleware)
	}
	// 4. summarization last, consistent with Deep/Supervisor.
	if a.AppCfg != nil {
		sumMw, sumErr := newEinoSummarizationMiddleware(ctx, a.ExecModel, a.AppCfg, a.MwCfg, a.ConversationID, a.Logger)
		if sumErr != nil {
			return nil, fmt.Errorf("plan_execute executor summarization: %w", sumErr)
		}
		execHandlers = append(execHandlers, sumMw)
	}
	// 5. Orphan tool-message fallback: it must run after all history-rewriting middleware (summarization/reduction/skill)
	//    and before telemetry, ensuring the message sequence sent to ChatModel has complete tool_call <-> tool_result pairs.
	execHandlers = append(execHandlers, newOrphanToolPrunerMiddleware(a.Logger, "plan_execute_executor"))
	if teleMw := newEinoModelInputTelemetryMiddleware(a.Logger, a.ModelName, a.ConversationID, "plan_execute_executor"); teleMw != nil {
		execHandlers = append(execHandlers, teleMw)
	}
	if a.ModelFacingTrace != nil {
		if capMw := newModelFacingTraceMiddleware(a.ModelFacingTrace); capMw != nil {
			execHandlers = append(execHandlers, capMw)
		}
	}
	executor, err := newPlanExecuteExecutor(ctx, &planexecute.ExecutorConfig{
		Model:         a.ExecModel,
		ToolsConfig:   a.ToolsCfg,
		MaxIterations: a.ExecMaxIter,
		GenInputFn:    planExecuteExecutorGenInput(a.OrchInstruction, a.AppCfg, a.MwCfg, a.Logger, a.ModelName, a.ConversationID),
	}, execHandlers)
	if err != nil {
		return nil, fmt.Errorf("plan_execute executor: %w", err)
	}
	loopMax := a.LoopMaxIter
	if loopMax <= 0 {
		loopMax = 10
	}
	return planexecute.New(ctx, &planexecute.Config{
		Planner:       planner,
		Executor:      executor,
		Replanner:     replanner,
		MaxIterations: loopMax,
	})
}

// planExecutePlannerGenInput injects the orchestrator instruction as a SystemMessage into planner input.
// When it returns nil, Eino uses the built-in default planner prompt.
func planExecutePlannerGenInput(
	orchInstruction string,
	appCfg *config.Config,
	mwCfg *config.MultiAgentEinoMiddlewareConfig,
	logger *zap.Logger,
	modelName string,
	conversationID string,
	rewriteHandlers []adk.ChatModelAgentMiddleware,
) planexecute.GenPlannerModelInputFn {
	oi := strings.TrimSpace(orchInstruction)
	if oi == "" && appCfg == nil {
		return nil
	}
	return func(ctx context.Context, userInput []adk.Message) ([]adk.Message, error) {
		userInput = capPlanExecuteUserInputMessages(userInput, appCfg, mwCfg)
		msgs := make([]adk.Message, 0, len(userInput))
		msgs = append(msgs, userInput...)
		if rewritten, rerr := applyBeforeModelRewriteHandlers(ctx, msgs, rewriteHandlers); rerr == nil && len(rewritten) > 0 {
			msgs = rewritten
		}
		msgs = normalizeSingleLeadingSystemMessage(msgs, oi)
		logPlanExecuteModelInputEstimate(logger, modelName, conversationID, "plan_execute_planner", msgs)
		return msgs, nil
	}
}

func planExecuteExecutorGenInput(
	orchInstruction string,
	appCfg *config.Config,
	mwCfg *config.MultiAgentEinoMiddlewareConfig,
	logger *zap.Logger,
	modelName string,
	conversationID string,
) planexecute.GenModelInputFn {
	oi := strings.TrimSpace(orchInstruction)
	return func(ctx context.Context, in *planexecute.ExecutionContext) ([]adk.Message, error) {
		planContent, err := in.Plan.MarshalJSON()
		if err != nil {
			return nil, err
		}
		userMsgs, err := planexecute.ExecutorPrompt.Format(ctx, map[string]any{
			"input":          planExecuteFormatInput(capPlanExecuteUserInputMessages(in.UserInput, appCfg, mwCfg)),
			"plan":           string(planContent),
			"executed_steps": planExecuteFormatExecutedSteps(in.ExecutedSteps, appCfg, mwCfg),
			"step":           in.Plan.FirstStep(),
		})
		if err != nil {
			return nil, err
		}
		userMsgs = normalizeSingleLeadingSystemMessage(userMsgs, oi)
		logPlanExecuteModelInputEstimate(logger, modelName, conversationID, "plan_execute_executor_gen_input", userMsgs)
		return userMsgs, nil
	}
}

func planExecuteFormatInput(input []adk.Message) string {
	var sb strings.Builder
	for _, msg := range input {
		sb.WriteString(msg.Content)
		sb.WriteString("\n")
	}
	return sb.String()
}

func planExecuteFormatExecutedSteps(results []planexecute.ExecutedStep, appCfg *config.Config, mwCfg *config.MultiAgentEinoMiddlewareConfig) string {
	capped := capPlanExecuteExecutedStepsWithConfig(results, mwCfg)
	return renderPlanExecuteStepsByBudget(capped, appCfg, mwCfg)
}

// planExecuteReplannerGenInput matches Eino's default Replanner input, but writes capped executed_steps into the prompt
// and prepends a SystemMessage when orchInstruction is non-empty so the replanner also receives global instructions.
func planExecuteReplannerGenInput(
	orchInstruction string,
	appCfg *config.Config,
	mwCfg *config.MultiAgentEinoMiddlewareConfig,
	logger *zap.Logger,
	modelName string,
	conversationID string,
	rewriteHandlers []adk.ChatModelAgentMiddleware,
) planexecute.GenModelInputFn {
	oi := strings.TrimSpace(orchInstruction)
	return func(ctx context.Context, in *planexecute.ExecutionContext) ([]adk.Message, error) {
		planContent, err := in.Plan.MarshalJSON()
		if err != nil {
			return nil, err
		}
		msgs, err := planexecute.ReplannerPrompt.Format(ctx, map[string]any{
			"plan":           string(planContent),
			"input":          planExecuteFormatInput(capPlanExecuteUserInputMessages(in.UserInput, appCfg, mwCfg)),
			"executed_steps": planExecuteFormatExecutedSteps(in.ExecutedSteps, appCfg, mwCfg),
			"plan_tool":      planexecute.PlanToolInfo.Name,
			"respond_tool":   planexecute.RespondToolInfo.Name,
		})
		if err != nil {
			return nil, err
		}
		if rewritten, rerr := applyBeforeModelRewriteHandlers(ctx, msgs, rewriteHandlers); rerr == nil && len(rewritten) > 0 {
			msgs = rewritten
		}
		msgs = normalizeSingleLeadingSystemMessage(msgs, oi)
		logPlanExecuteModelInputEstimate(logger, modelName, conversationID, "plan_execute_replanner", msgs)
		return msgs, nil
	}
}

// normalizeSingleLeadingSystemMessage enforces a provider-friendly message shape:
// exactly one system message at index 0 (when any system context exists).
// For strict OpenAI-compatible backends (e.g. qwen/vllm templates), this avoids
// "System message must be at the beginning" caused by multiple/disordered system messages.
func normalizeSingleLeadingSystemMessage(msgs []adk.Message, extraSystem string) []adk.Message {
	extraSystem = strings.TrimSpace(extraSystem)
	if len(msgs) == 0 {
		if extraSystem == "" {
			return msgs
		}
		return []adk.Message{schema.SystemMessage(extraSystem)}
	}

	systemParts := make([]string, 0, 2)
	if extraSystem != "" {
		systemParts = append(systemParts, extraSystem)
	}
	nonSystem := make([]adk.Message, 0, len(msgs))
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		if msg.Role == schema.System {
			if s := strings.TrimSpace(msg.Content); s != "" {
				systemParts = append(systemParts, s)
			}
			continue
		}
		nonSystem = append(nonSystem, msg)
	}
	if len(systemParts) == 0 {
		return nonSystem
	}
	out := make([]adk.Message, 0, len(nonSystem)+1)
	out = append(out, schema.SystemMessage(strings.Join(systemParts, "\n\n")))
	out = append(out, nonSystem...)
	return out
}

func capPlanExecuteUserInputMessages(input []adk.Message, appCfg *config.Config, mwCfg *config.MultiAgentEinoMiddlewareConfig) []adk.Message {
	if len(input) == 0 {
		return input
	}
	maxTotal := 120000
	modelName := "gpt-4o"
	if appCfg != nil {
		if appCfg.OpenAI.MaxTotalTokens > 0 {
			maxTotal = appCfg.OpenAI.MaxTotalTokens
		}
		if m := strings.TrimSpace(appCfg.OpenAI.Model); m != "" {
			modelName = m
		}
	}
	// Reserve most tokens for planner/replanner prompt and tool schema.
	ratio := 0.35
	if mwCfg != nil {
		ratio = mwCfg.PlanExecuteUserInputBudgetRatioEffective()
	}
	budget := int(float64(maxTotal) * ratio)
	if budget < 4096 {
		budget = 4096
	}
	tc := agent.NewTikTokenCounter()
	out := make([]adk.Message, 0, len(input))
	used := 0
	for i := len(input) - 1; i >= 0; i-- {
		msg := input[i]
		if msg == nil {
			continue
		}
		n, err := tc.Count(modelName, string(msg.Role)+"\n"+msg.Content)
		if err != nil {
			n = (len(msg.Content) + 3) / 4
		}
		if n <= 0 {
			n = 1
		}
		if used+n > budget {
			break
		}
		used += n
		out = append(out, msg)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if len(out) == 0 {
		// Keep the latest user message at least.
		return []adk.Message{input[len(input)-1]}
	}
	return out
}

func renderPlanExecuteStepsByBudget(steps []planexecute.ExecutedStep, appCfg *config.Config, mwCfg *config.MultiAgentEinoMiddlewareConfig) string {
	if len(steps) == 0 {
		return ""
	}
	maxTotal := 120000
	modelName := "gpt-4o"
	if appCfg != nil {
		if appCfg.OpenAI.MaxTotalTokens > 0 {
			maxTotal = appCfg.OpenAI.MaxTotalTokens
		}
		if m := strings.TrimSpace(appCfg.OpenAI.Model); m != "" {
			modelName = m
		}
	}
	ratio := 0.2
	if mwCfg != nil {
		ratio = mwCfg.PlanExecuteExecutedStepsBudgetRatioEffective()
	}
	budget := int(float64(maxTotal) * ratio)
	if budget < 3072 {
		budget = 3072
	}
	tc := agent.NewTikTokenCounter()
	var kept []string
	used := 0
	skipped := 0
	for i := len(steps) - 1; i >= 0; i-- {
		block := fmt.Sprintf("Step: %s\nResult: %s\n\n", steps[i].Step, steps[i].Result)
		n, err := tc.Count(modelName, block)
		if err != nil {
			n = (len(block) + 3) / 4
		}
		if n <= 0 {
			n = 1
		}
		if used+n > budget {
			skipped = i + 1
			break
		}
		used += n
		kept = append(kept, block)
	}
	var sb strings.Builder
	if skipped > 0 {
		sb.WriteString(fmt.Sprintf("Earlier executed steps omitted due to context budget: %d steps.\n\n", skipped))
	}
	for i := len(kept) - 1; i >= 0; i-- {
		sb.WriteString(kept[i])
	}
	return sb.String()
}

// planExecuteStreamsMainAssistant maps assistant streaming output from planning, execution, and replanning stages to the main conversation area.
func planExecuteStreamsMainAssistant(agent string) bool {
	if agent == "" {
		return true
	}
	switch agent {
	case "planner", "executor", "replanner", "execute_replan", "plan_execute_replan":
		return true
	default:
		return false
	}
}

func planExecuteEinoRoleTag(agent string) string {
	_ = agent
	return "orchestrator"
}
