package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/c2"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/mcp/builtin"
	"cyberstrike-ai/internal/openai"
	"cyberstrike-ai/internal/security"
	"cyberstrike-ai/internal/storage"

	"go.uber.org/zap"
)

// Agent AI代理
type Agent struct {
	openAIClient          *openai.Client
	config                *config.OpenAIConfig
	agentConfig           *config.AgentConfig
	memoryCompressor      *MemoryCompressor
	mcpServer             *mcp.Server
	externalMCPMgr        *mcp.ExternalMCPManager // 外部MCP管理器
	logger                *zap.Logger
	maxIterations         int
	resultStorage         ResultStorage     // 结果存储
	largeResultThreshold  int               // 大结果阈值（字节）
	mu                    sync.RWMutex      // 添加互斥锁以支持并发更新
	toolNameMapping       map[string]string // 工具名称映射：OpenAI格式 -> 原始格式（用于外部MCP工具）
	currentConversationID string            // 当前对话ID（用于自动传递给工具）
	promptBaseDir         string            // 解析 system_prompt_path 时相对路径的基准目录（通常为 config.yaml 所在目录）
	toolDescriptionMode   string            // 工具描述模式: "short" | "full"，默认 short
}

// ResultStorage 结果存储接口（直接使用 storage 包的类型）
type ResultStorage interface {
	SaveResult(executionID string, toolName string, result string) error
	GetResult(executionID string) (string, error)
	GetResultPage(executionID string, page int, limit int) (*storage.ResultPage, error)
	SearchResult(executionID string, keyword string, useRegex bool) ([]string, error)
	FilterResult(executionID string, filter string, useRegex bool) ([]string, error)
	GetResultMetadata(executionID string) (*storage.ResultMetadata, error)
	GetResultPath(executionID string) string
	DeleteResult(executionID string) error
}

type toolCallInterceptorCtxKey struct{}

type agentConversationIDKey struct{}

func withAgentConversationID(ctx context.Context, id string) context.Context {
	id = strings.TrimSpace(id)
	if id == "" || ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, agentConversationIDKey{}, id)
}

func agentConversationIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(agentConversationIDKey{}).(string)
	return v
}

// ConversationIDFromContext 返回当前 Agent 请求上下文中注入的对话 ID（如 C2 MCP 入队与人机协同门控使用）。
func ConversationIDFromContext(ctx context.Context) string {
	return agentConversationIDFromContext(ctx)
}

// ToolCallInterceptor allows caller to gate or rewrite tool arguments just before execution.
// Returning a non-nil error means the tool call is rejected and execution is skipped.
type ToolCallInterceptor func(ctx context.Context, toolName string, args map[string]interface{}, toolCallID string) (map[string]interface{}, error)

func WithToolCallInterceptor(ctx context.Context, fn ToolCallInterceptor) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, toolCallInterceptorCtxKey{}, fn)
}

// NewAgent 创建新的Agent
func NewAgent(cfg *config.OpenAIConfig, agentCfg *config.AgentConfig, mcpServer *mcp.Server, externalMCPMgr *mcp.ExternalMCPManager, logger *zap.Logger, maxIterations int) *Agent {
	// 如果 maxIterations 为 0 或负数，使用默认值 30
	if maxIterations <= 0 {
		maxIterations = 30
	}

	// 设置大结果阈值，默认50KB
	largeResultThreshold := 50 * 1024
	if agentCfg != nil && agentCfg.LargeResultThreshold > 0 {
		largeResultThreshold = agentCfg.LargeResultThreshold
	}

	// 设置结果存储目录，默认tmp
	resultStorageDir := "tmp"
	if agentCfg != nil && agentCfg.ResultStorageDir != "" {
		resultStorageDir = agentCfg.ResultStorageDir
	}

	// 初始化结果存储
	var resultStorage ResultStorage
	if resultStorageDir != "" {
		// 导入storage包（避免循环依赖，使用接口）
		// 这里需要在实际使用时初始化
		// 暂时设为nil，在需要时初始化
	}

	// 配置HTTP Transport，优化连接管理和超时设置
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   300 * time.Second,
			KeepAlive: 300 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   30 * time.Second,
		ResponseHeaderTimeout: 60 * time.Minute, // 响应头超时：增加到15分钟，应对大响应
		DisableKeepAlives:     false,            // 启用连接复用
	}

	// 增加超时时间到30分钟，以支持长时间运行的AI推理
	// 特别是当使用流式响应或处理复杂任务时
	httpClient := &http.Client{
		Timeout:   30 * time.Minute, // 从5分钟增加到30分钟
		Transport: transport,
	}
	llmClient := openai.NewClient(cfg, httpClient, logger)

	var memoryCompressor *MemoryCompressor
	if cfg != nil {
		mc, err := NewMemoryCompressor(MemoryCompressorConfig{
			MaxTotalTokens: cfg.MaxTotalTokens,
			OpenAIConfig:   cfg,
			HTTPClient:     httpClient,
			Logger:         logger,
		})
		if err != nil {
			logger.Warn("初始化MemoryCompressor失败，将跳过上下文压缩", zap.Error(err))
		} else {
			memoryCompressor = mc
		}
	} else {
		logger.Warn("OpenAI配置为空，无法初始化MemoryCompressor")
	}

	return &Agent{
		openAIClient:         llmClient,
		config:               cfg,
		agentConfig:          agentCfg,
		memoryCompressor:     memoryCompressor,
		mcpServer:            mcpServer,
		externalMCPMgr:       externalMCPMgr,
		logger:               logger,
		maxIterations:        maxIterations,
		resultStorage:        resultStorage,
		largeResultThreshold: largeResultThreshold,
		toolNameMapping:      make(map[string]string), // 初始化工具名称映射
		toolDescriptionMode:  "short",
	}
}

// SetResultStorage 设置结果存储（用于避免循环依赖）
func (a *Agent) SetResultStorage(storage ResultStorage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.resultStorage = storage
}

// SetPromptBaseDir 设置单代理 system_prompt_path 相对路径的基准目录（一般为 config.yaml 所在目录）。
func (a *Agent) SetPromptBaseDir(dir string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.promptBaseDir = strings.TrimSpace(dir)
}

// ChatMessage 聊天消息
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	// ToolName 仅 tool 角色：从 Eino/轨迹 JSON 的 name 或 tool_name 恢复，供续跑构造 ToolMessage。
	ToolName string `json:"tool_name,omitempty"`
	// ReasoningContent 对应 OpenAI/DeepSeek 的 reasoning_content；思考模式 + 工具调用后续跑须回传（见 DeepSeek 文档）。
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

// MarshalJSON 自定义JSON序列化，将tool_calls中的arguments转换为JSON字符串
func (cm ChatMessage) MarshalJSON() ([]byte, error) {
	// 构建序列化结构
	aux := map[string]interface{}{
		"role": cm.Role,
	}

	// 添加content（如果存在）
	if cm.Content != "" {
		aux["content"] = cm.Content
	}
	if cm.ReasoningContent != "" {
		aux["reasoning_content"] = cm.ReasoningContent
	}

	// 添加tool_call_id（如果存在）
	if cm.ToolCallID != "" {
		aux["tool_call_id"] = cm.ToolCallID
	}
	if cm.ToolName != "" {
		aux["tool_name"] = cm.ToolName
	}

	// 转换tool_calls，将arguments转换为JSON字符串
	if len(cm.ToolCalls) > 0 {
		toolCallsJSON := make([]map[string]interface{}, len(cm.ToolCalls))
		for i, tc := range cm.ToolCalls {
			// 将arguments转换为JSON字符串
			argsJSON := ""
			if tc.Function.Arguments != nil {
				argsBytes, err := json.Marshal(tc.Function.Arguments)
				if err != nil {
					return nil, err
				}
				argsJSON = string(argsBytes)
			}

			toolCallsJSON[i] = map[string]interface{}{
				"id":   tc.ID,
				"type": tc.Type,
				"function": map[string]interface{}{
					"name":      tc.Function.Name,
					"arguments": argsJSON,
				},
			}
		}
		aux["tool_calls"] = toolCallsJSON
	}

	return json.Marshal(aux)
}

// OpenAIRequest OpenAI API请求
type OpenAIRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Tools    []Tool        `json:"tools,omitempty"`
	Stream   bool          `json:"stream,omitempty"`
}

// OpenAIResponse OpenAI API响应
type OpenAIResponse struct {
	ID      string   `json:"id"`
	Choices []Choice `json:"choices"`
	Error   *Error   `json:"error,omitempty"`
}

// Choice 选择
type Choice struct {
	Message      MessageWithTools `json:"message"`
	FinishReason string           `json:"finish_reason"`
}

// MessageWithTools 带工具调用的消息
type MessageWithTools struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// Tool OpenAI工具定义
type Tool struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}

// FunctionDefinition 函数定义
type FunctionDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// Error OpenAI错误
type Error struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

// ToolCall 工具调用
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall 函数调用
type FunctionCall struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// UnmarshalJSON 自定义JSON解析，处理arguments可能是字符串或对象的情况
func (fc *FunctionCall) UnmarshalJSON(data []byte) error {
	type Alias FunctionCall
	aux := &struct {
		Name      string      `json:"name"`
		Arguments interface{} `json:"arguments"`
		*Alias
	}{
		Alias: (*Alias)(fc),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	fc.Name = aux.Name

	// 处理arguments可能是字符串或对象的情况
	switch v := aux.Arguments.(type) {
	case map[string]interface{}:
		fc.Arguments = v
	case string:
		// 如果是字符串，尝试解析为JSON
		if err := json.Unmarshal([]byte(v), &fc.Arguments); err != nil {
			// 如果解析失败，创建一个包含原始字符串的map
			fc.Arguments = map[string]interface{}{
				"raw": v,
			}
		}
	case nil:
		fc.Arguments = make(map[string]interface{})
	default:
		// 其他类型，尝试转换为map
		fc.Arguments = map[string]interface{}{
			"value": v,
		}
	}

	return nil
}

// AgentLoopResult Agent Loop执行结果
type AgentLoopResult struct {
	Response             string
	MCPExecutionIDs      []string
	LastAgentTraceInput  string // 最后一轮代理消息轨迹（压缩后的 messages，JSON；与 multiagent.RunResult 字段对齐）
	LastAgentTraceOutput string // 最终助手输出文本
}

// ProgressCallback 进度回调函数类型
type ProgressCallback func(eventType, message string, data interface{})

// AgentLoop 执行Agent循环
func (a *Agent) AgentLoop(ctx context.Context, userInput string, historyMessages []ChatMessage) (*AgentLoopResult, error) {
	return a.AgentLoopWithProgress(ctx, userInput, historyMessages, "", nil, nil)
}

// AgentLoopWithConversationID 执行Agent循环（带对话ID）
func (a *Agent) AgentLoopWithConversationID(ctx context.Context, userInput string, historyMessages []ChatMessage, conversationID string) (*AgentLoopResult, error) {
	return a.AgentLoopWithProgress(ctx, userInput, historyMessages, conversationID, nil, nil)
}

// EinoSingleAgentSystemInstruction 供 Eino adk.ChatModelAgent.Instruction 使用，与 AgentLoopWithProgress 首条 system 对齐（含 system_prompt_path）。
func (a *Agent) EinoSingleAgentSystemInstruction() string {
	systemPrompt := DefaultSingleAgentSystemPrompt()
	if a.agentConfig != nil {
		if p := strings.TrimSpace(a.agentConfig.SystemPromptPath); p != "" {
			path := p
			a.mu.RLock()
			base := a.promptBaseDir
			a.mu.RUnlock()
			if !filepath.IsAbs(path) && base != "" {
				path = filepath.Join(base, path)
			}
			if b, err := os.ReadFile(path); err != nil {
			a.logger.Warn("Failed to read single-agent system_prompt_path, using built-in prompt", zap.String("path", path), zap.Error(err))
			} else if s := strings.TrimSpace(string(b)); s != "" {
				systemPrompt = s
			}
		}
	}
	return systemPrompt
}

// AgentLoopWithProgress 执行Agent循环（带进度回调和对话ID）
func (a *Agent) AgentLoopWithProgress(ctx context.Context, userInput string, historyMessages []ChatMessage, conversationID string, callback ProgressCallback, roleTools []string) (*AgentLoopResult, error) {
	ctx = withAgentConversationID(ctx, conversationID)
	// 设置当前对话ID（兼容未走 context 的旧路径；并发会话应以 context 为准）
	a.mu.Lock()
	a.currentConversationID = conversationID
	a.mu.Unlock()
	// 发送进度更新
	sendProgress := func(eventType, message string, data interface{}) {
		if callback != nil {
			callback(eventType, message, data)
		}
	}

	systemPrompt := DefaultSingleAgentSystemPrompt()
	if a.agentConfig != nil {
		if p := strings.TrimSpace(a.agentConfig.SystemPromptPath); p != "" {
			path := p
			a.mu.RLock()
			base := a.promptBaseDir
			a.mu.RUnlock()
			if !filepath.IsAbs(path) && base != "" {
				path = filepath.Join(base, path)
			}
			if b, err := os.ReadFile(path); err != nil {
			a.logger.Warn("Failed to read single-agent system_prompt_path, using built-in prompt", zap.String("path", path), zap.Error(err))
			} else if s := strings.TrimSpace(string(b)); s != "" {
				systemPrompt = s
			}
		}
	}

	messages := []ChatMessage{
		{
			Role:    "system",
			Content: systemPrompt,
		},
	}

	// 添加历史消息（保留所有字段，包括ToolCalls和ToolCallID）
	a.logger.Info("Processing history messages",
		zap.Int("count", len(historyMessages)),
	)
	addedCount := 0
	for i, msg := range historyMessages {
		// 对于tool消息，即使content为空也要添加（因为tool消息可能只有ToolCallID）
		// 对于其他消息，只添加有内容的消息
		if msg.Role == "tool" || msg.Content != "" {
			messages = append(messages, ChatMessage{
				Role:       msg.Role,
				Content:    msg.Content,
				ToolCalls:  msg.ToolCalls,
				ToolCallID: msg.ToolCallID,
				ToolName:   msg.ToolName,
			})
			addedCount++
			contentPreview := msg.Content
			if len(contentPreview) > 50 {
				contentPreview = contentPreview[:50] + "..."
			}
		a.logger.Info("Adding history message to context",
				zap.Int("index", i),
				zap.String("role", msg.Role),
				zap.String("content", contentPreview),
				zap.Int("toolCalls", len(msg.ToolCalls)),
				zap.String("toolCallID", msg.ToolCallID),
			)
		}
	}

	a.logger.Info("Building message array",
		zap.Int("historyMessages", len(historyMessages)),
		zap.Int("addedMessages", addedCount),
		zap.Int("totalMessages", len(messages)),
	)

	// 在添加当前用户消息之前，先修复可能存在的失配tool消息
	// 这可以防止在继续对话时出现"messages with role 'tool' must be a response to a preceeding message with 'tool_calls'"错误
	if len(messages) > 0 {
		if fixed := a.repairOrphanToolMessages(&messages); fixed {
			a.logger.Info("Fixed orphaned tool messages in history")
		}
	}

	// 添加当前用户消息
	messages = append(messages, ChatMessage{
		Role:    "user",
		Content: userInput,
	})

	result := &AgentLoopResult{
		MCPExecutionIDs: make([]string, 0),
	}

	// 用于保存当前的messages，以便在异常情况下也能保存ReAct输入
	var currentAgentTraceInput string

	maxIterations := a.maxIterations
	thinkingStreamSeq := 0
	for i := 0; i < maxIterations; i++ {
		// 先获取本轮可用工具并统计 tools token，再压缩，以便压缩时预留 tools 占用的空间
		tools := a.getAvailableTools(roleTools)
		toolsTokens := a.countToolsTokens(tools)
		messages = a.applyMemoryCompression(ctx, messages, toolsTokens)

		// 检查是否是最后一次迭代
		isLastIteration := (i == maxIterations-1)

		// 每次迭代都保存压缩后的messages，以便在异常中断（取消、错误等）时也能保存最新的ReAct输入
		// 保存压缩后的数据，这样后续使用时就不需要再考虑压缩了
		messagesJSON, err := json.Marshal(messages)
		if err != nil {
			a.logger.Warn("序列化ReAct输入失败", zap.Error(err))
		} else {
			currentAgentTraceInput = string(messagesJSON)
			// 更新result中的值，确保始终保存最新的ReAct输入（压缩后的）
			result.LastAgentTraceInput = currentAgentTraceInput
		}

		// 检查上下文是否已取消
		select {
		case <-ctx.Done():
			// 上下文被取消（可能是用户主动暂停或其他原因）
		a.logger.Info("Context cancellation detected, saving current ReAct data", zap.Error(ctx.Err()))
			result.LastAgentTraceInput = currentAgentTraceInput
			if ctx.Err() == context.Canceled {
			result.Response = "Task was cancelled."
			} else {
			result.Response = fmt.Sprintf("Task execution interrupted: %v", ctx.Err())
			}
			result.LastAgentTraceOutput = result.Response
			return result, ctx.Err()
		default:
		}

		// 记录当前上下文的 Token 用量（messages + tools），展示压缩器运行状态
		if a.memoryCompressor != nil {
			messagesTokens, systemCount, regularCount := a.memoryCompressor.totalTokensFor(messages)
			totalTokens := messagesTokens + toolsTokens
			a.logger.Info("memory compressor context stats",
				zap.Int("iteration", i+1),
				zap.Int("messagesCount", len(messages)),
				zap.Int("systemMessages", systemCount),
				zap.Int("regularMessages", regularCount),
				zap.Int("messagesTokens", messagesTokens),
				zap.Int("toolsTokens", toolsTokens),
				zap.Int("totalTokens", totalTokens),
				zap.Int("maxTotalTokens", a.memoryCompressor.maxTotalTokens),
			)
		}

		// 发送迭代开始事件
		if i == 0 {
		sendProgress("iteration", "Starting analysis and test strategy planning", map[string]interface{}{
				"iteration": i + 1,
				"total":     maxIterations,
			})
		} else if isLastIteration {
		sendProgress("iteration", fmt.Sprintf("Iteration %d (final)", i+1), map[string]interface{}{
				"iteration": i + 1,
				"total":     maxIterations,
				"isLast":    true,
			})
		} else {
		sendProgress("iteration", fmt.Sprintf("Iteration %d", i+1), map[string]interface{}{
				"iteration": i + 1,
				"total":     maxIterations,
			})
		}

		// 记录每次调用OpenAI
		if i == 0 {
		a.logger.Info("Calling OpenAI",
				zap.Int("iteration", i+1),
				zap.Int("messagesCount", len(messages)),
			)
			// 记录前几条消息的内容（用于调试）
			for j, msg := range messages {
				if j >= 5 { // 只记录前5条
					break
				}
				contentPreview := msg.Content
				if len(contentPreview) > 100 {
					contentPreview = contentPreview[:100] + "..."
				}
				a.logger.Debug("消息内容",
					zap.Int("index", j),
					zap.String("role", msg.Role),
					zap.String("content", contentPreview),
				)
			}
		} else {
		a.logger.Info("Calling OpenAI",
				zap.Int("iteration", i+1),
				zap.Int("messagesCount", len(messages)),
			)
		}

		// 调用OpenAI
	sendProgress("progress", "Calling AI model...", nil)
		thinkingStreamSeq++
		thinkingStreamId := fmt.Sprintf("thinking-stream-%s-%d-%d", conversationID, i+1, thinkingStreamSeq)
		thinkingStreamStarted := false
		var thinkingWire string

		response, err := a.callOpenAIStreamWithToolCalls(ctx, messages, tools, func(delta string) error {
			if delta == "" {
				return nil
			}
			var deltaOut string
			thinkingWire, deltaOut = openai.NormalizeStreamingDelta(thinkingWire, delta)
			if deltaOut == "" {
				return nil
			}
			if !thinkingStreamStarted {
				thinkingStreamStarted = true
				sendProgress("thinking_stream_start", " ", map[string]interface{}{
					"streamId":   thinkingStreamId,
					"iteration":  i + 1,
					"toolStream": false,
				})
			}
			sendProgress("thinking_stream_delta", deltaOut, openai.WithSSEAccumulated(map[string]interface{}{
				"streamId":  thinkingStreamId,
				"iteration": i + 1,
			}, thinkingWire))
			return nil
		})
		if err != nil {
			// API调用失败，保存当前的ReAct输入和错误信息作为输出
			result.LastAgentTraceInput = currentAgentTraceInput
		errorMsg := fmt.Sprintf("OpenAI call failed: %v", err)
			result.Response = errorMsg
			result.LastAgentTraceOutput = errorMsg
		a.logger.Warn("OpenAI call failed, ReAct data saved", zap.Error(err))
		return result, fmt.Errorf("OpenAI call failed: %w", err)
		}

		if response.Error != nil {
			if handled, toolName := a.handleMissingToolError(response.Error.Message, &messages); handled {
			sendProgress("warning", fmt.Sprintf("Model attempted to call non-existent tool: %s, prompted to use available tools.", toolName), map[string]interface{}{
					"toolName": toolName,
				})
			a.logger.Warn("Model called non-existent tool, will retry",
					zap.String("tool", toolName),
					zap.String("error", response.Error.Message),
				)
				continue
			}
			if a.handleToolRoleError(response.Error.Message, &messages) {
			sendProgress("warning", "Detected orphaned tool results, auto-repaired context and retrying.", map[string]interface{}{
					"error": response.Error.Message,
				})
			a.logger.Warn("Detected orphaned tool messages, repaired and retrying",
					zap.String("error", response.Error.Message),
				)
				continue
			}
			// OpenAI返回错误，保存当前的ReAct输入和错误信息作为输出
			result.LastAgentTraceInput = currentAgentTraceInput
		errorMsg := fmt.Sprintf("OpenAI error: %s", response.Error.Message)
			result.Response = errorMsg
			result.LastAgentTraceOutput = errorMsg
		return result, fmt.Errorf("OpenAI error: %s", response.Error.Message)
		}

		if len(response.Choices) == 0 {
			// 没有收到响应，保存当前的ReAct输入和错误信息作为输出
			result.LastAgentTraceInput = currentAgentTraceInput
		errorMsg := "No response received"
			result.Response = errorMsg
			result.LastAgentTraceOutput = errorMsg
		return result, fmt.Errorf("no response received")
		}

		choice := response.Choices[0]

		// 检查是否有工具调用
		if len(choice.Message.ToolCalls) > 0 {
			// ReAct 助手正文流式增量（thinking_stream_*）在 UI 上归为「思考」；若与 streamId 重复则前端会去重。
			// 该条 thinking 用于刷新后持久化展示（与流式聚合一致）。
			if choice.Message.Content != "" {
				sendProgress("thinking", choice.Message.Content, map[string]interface{}{
					"iteration": i + 1,
					"streamId":  thinkingStreamId,
				})
			}

			// 添加assistant消息（包含工具调用）
			messages = append(messages, ChatMessage{
				Role:      "assistant",
				Content:   choice.Message.Content,
				ToolCalls: choice.Message.ToolCalls,
			})

			// 发送工具调用进度
		sendProgress("tool_calls_detected", fmt.Sprintf("Detected %d tool call(s)", len(choice.Message.ToolCalls)), map[string]interface{}{
				"count":     len(choice.Message.ToolCalls),
				"iteration": i + 1,
			})

			// 执行所有工具调用
			for idx, toolCall := range choice.Message.ToolCalls {
				// 发送工具调用开始事件
				toolArgsJSON, _ := json.Marshal(toolCall.Function.Arguments)
			sendProgress("tool_call", fmt.Sprintf("Calling tool: %s", toolCall.Function.Name), map[string]interface{}{
					"toolName":     toolCall.Function.Name,
					"arguments":    string(toolArgsJSON),
					"argumentsObj": toolCall.Function.Arguments,
					"toolCallId":   toolCall.ID,
					"index":        idx + 1,
					"total":        len(choice.Message.ToolCalls),
					"iteration":    i + 1,
				})

				execArgs := toolCall.Function.Arguments
				if interceptor, ok := ctx.Value(toolCallInterceptorCtxKey{}).(ToolCallInterceptor); ok && interceptor != nil {
					newArgs, interceptErr := interceptor(ctx, toolCall.Function.Name, execArgs, toolCall.ID)
					if interceptErr != nil {
					errorMsg := fmt.Sprintf("Tool call rejected by human: %v", interceptErr)
						messages = append(messages, ChatMessage{
							Role:       "tool",
							ToolCallID: toolCall.ID,
							Content:    errorMsg,
						})
					sendProgress("tool_result", fmt.Sprintf("Tool %s execution failed", toolCall.Function.Name), map[string]interface{}{
							"toolName":   toolCall.Function.Name,
							"success":    false,
							"isError":    true,
							"error":      errorMsg,
							"toolCallId": toolCall.ID,
							"index":      idx + 1,
							"total":      len(choice.Message.ToolCalls),
							"iteration":  i + 1,
						})
						continue
					}
					if newArgs != nil {
						execArgs = newArgs
					}
				}

				// 执行工具
				toolCtx := context.WithValue(ctx, security.ToolOutputCallbackCtxKey, security.ToolOutputCallback(func(chunk string) {
					if strings.TrimSpace(chunk) == "" {
						return
					}
					sendProgress("tool_result_delta", chunk, map[string]interface{}{
						"toolName":   toolCall.Function.Name,
						"toolCallId": toolCall.ID,
						"index":      idx + 1,
						"total":      len(choice.Message.ToolCalls),
						"iteration":  i + 1,
						// success 在最终 tool_result 事件里会以 success/isError 标记为准
					})
				}))

				execResult, err := a.executeToolViaMCP(toolCtx, toolCall.Function.Name, execArgs)
				if err != nil {
					// 构建详细的错误信息，帮助AI理解问题并做出决策
					errorMsg := a.formatToolError(toolCall.Function.Name, toolCall.Function.Arguments, err)
					messages = append(messages, ChatMessage{
						Role:       "tool",
						ToolCallID: toolCall.ID,
						Content:    errorMsg,
					})

					// 发送工具执行失败事件
				sendProgress("tool_result", fmt.Sprintf("Tool %s execution failed", toolCall.Function.Name), map[string]interface{}{
						"toolName":   toolCall.Function.Name,
						"success":    false,
						"isError":    true,
						"error":      err.Error(),
						"toolCallId": toolCall.ID,
						"index":      idx + 1,
						"total":      len(choice.Message.ToolCalls),
						"iteration":  i + 1,
					})

				a.logger.Warn("Tool execution failed, detailed error returned",
						zap.String("tool", toolCall.Function.Name),
						zap.Error(err),
					)
				} else {
					// 即使工具返回了错误结果（IsError=true），也继续处理，让AI决定下一步
					messages = append(messages, ChatMessage{
						Role:       "tool",
						ToolCallID: toolCall.ID,
						Content:    execResult.Result,
					})
					// 收集执行ID
					if execResult.ExecutionID != "" {
						result.MCPExecutionIDs = append(result.MCPExecutionIDs, execResult.ExecutionID)
					}

					// 发送工具执行成功事件
					resultPreview := execResult.Result
					if len(resultPreview) > 200 {
						resultPreview = resultPreview[:200] + "..."
					}
				sendProgress("tool_result", fmt.Sprintf("Tool %s execution completed", toolCall.Function.Name), map[string]interface{}{
						"toolName":      toolCall.Function.Name,
						"success":       !execResult.IsError,
						"isError":       execResult.IsError,
						"result":        execResult.Result, // 完整结果
						"resultPreview": resultPreview,     // 预览结果
						"executionId":   execResult.ExecutionID,
						"toolCallId":    toolCall.ID,
						"index":         idx + 1,
						"total":         len(choice.Message.ToolCalls),
						"iteration":     i + 1,
					})

					// 如果工具返回了错误，记录日志但不中断流程
					if execResult.IsError {
				a.logger.Warn("Tool returned error result, continuing",
							zap.String("tool", toolCall.Function.Name),
							zap.String("result", execResult.Result),
						)
					}
				}
			}

			// 如果是最后一次迭代，执行完工具后要求AI进行总结
			if isLastIteration {
			sendProgress("progress", "Final iteration: generating summary and next steps...", nil)
				// 添加用户消息，要求AI进行总结
				messages = append(messages, ChatMessage{
					Role:    "user",
				Content: "This is the final iteration. Please summarize all test results, findings, and completed work so far. If further testing is needed, provide a detailed next-step plan. Reply directly without calling tools.",
				})
				messages = a.applyMemoryCompression(ctx, messages, 0) // 总结时不带 tools，不预留
				// 流式调用OpenAI获取总结（不提供工具，强制AI直接回复）
				sendProgress("response_start", "", map[string]interface{}{
					"conversationId":     conversationID,
					"mcpExecutionIds":    result.MCPExecutionIDs,
					"messageGeneratedBy": "summary",
				})
				var summaryWire string
				streamText, _ := a.callOpenAIStreamText(ctx, messages, []Tool{}, func(delta string) error {
					var deltaOut string
					summaryWire, deltaOut = openai.NormalizeStreamingDelta(summaryWire, delta)
					if deltaOut == "" {
						return nil
					}
					sendProgress("response_delta", deltaOut, openai.WithSSEAccumulated(map[string]interface{}{
						"conversationId": conversationID,
					}, summaryWire))
					return nil
				})
				if strings.TrimSpace(streamText) != "" {
					result.Response = streamText
					result.LastAgentTraceOutput = result.Response
				sendProgress("progress", "Summary generation complete", nil)
					return result, nil
				}
				// 如果获取总结失败，跳出循环，让后续逻辑处理
				break
			}

			continue
		}

		// 添加assistant响应
		messages = append(messages, ChatMessage{
			Role:    "assistant",
			Content: choice.Message.Content,
		})

		// 发送AI思考内容（如果没有工具调用）
		if choice.Message.Content != "" && !thinkingStreamStarted {
			sendProgress("thinking", choice.Message.Content, map[string]interface{}{
				"iteration": i + 1,
			})
		}

		// 如果是最后一次迭代，无论finish_reason是什么，都要求AI进行总结
		if isLastIteration {
		sendProgress("progress", "Final iteration: generating summary and next steps...", nil)
			// 添加用户消息，要求AI进行总结
			messages = append(messages, ChatMessage{
				Role:    "user",
			Content: "This is the final iteration. Please summarize all test results, findings, and completed work so far. If further testing is needed, provide a detailed next-step plan. Reply directly without calling tools.",
			})
			messages = a.applyMemoryCompression(ctx, messages, 0) // 总结时不带 tools，不预留
			// 流式调用OpenAI获取总结（不提供工具，强制AI直接回复）
			sendProgress("response_start", "", map[string]interface{}{
				"conversationId":     conversationID,
				"mcpExecutionIds":    result.MCPExecutionIDs,
				"messageGeneratedBy": "summary",
			})
			var summaryWire string
			streamText, _ := a.callOpenAIStreamText(ctx, messages, []Tool{}, func(delta string) error {
				var deltaOut string
				summaryWire, deltaOut = openai.NormalizeStreamingDelta(summaryWire, delta)
				if deltaOut == "" {
					return nil
				}
				sendProgress("response_delta", deltaOut, openai.WithSSEAccumulated(map[string]interface{}{
					"conversationId": conversationID,
				}, summaryWire))
				return nil
			})
			if strings.TrimSpace(streamText) != "" {
				result.Response = streamText
				result.LastAgentTraceOutput = result.Response
			sendProgress("progress", "Summary generation complete", nil)
				return result, nil
			}
			// 如果获取总结失败，使用当前回复作为结果
			if choice.Message.Content != "" {
				result.Response = choice.Message.Content
				result.LastAgentTraceOutput = result.Response
				return result, nil
			}
			// 如果都没有内容，跳出循环，让后续逻辑处理
			break
		}

		// 如果完成，返回结果
		if choice.FinishReason == "stop" {
	sendProgress("progress", "Generating final response...", nil)
			result.Response = choice.Message.Content
			result.LastAgentTraceOutput = result.Response
			return result, nil
		}
	}

	// 如果循环结束仍未返回，说明达到了最大迭代次数
	// 尝试最后一次调用AI获取总结
	sendProgress("progress", "Reached maximum iterations, generating summary...", nil)
	finalSummaryPrompt := ChatMessage{
		Role:    "user",
	Content: fmt.Sprintf("Maximum iterations (%d) reached. Please summarize all test results, findings, and completed work so far. If further testing is needed, provide a detailed next-step plan. Reply directly without calling tools.", a.maxIterations),
	}
	messages = append(messages, finalSummaryPrompt)
	messages = a.applyMemoryCompression(ctx, messages, 0) // 总结时不带 tools，不预留

	// 流式调用OpenAI获取总结（不提供工具，强制AI直接回复）
	sendProgress("response_start", "", map[string]interface{}{
		"conversationId":     conversationID,
		"mcpExecutionIds":    result.MCPExecutionIDs,
		"messageGeneratedBy": "max_iter_summary",
	})
	var summaryWire string
	streamText, _ := a.callOpenAIStreamText(ctx, messages, []Tool{}, func(delta string) error {
		var deltaOut string
		summaryWire, deltaOut = openai.NormalizeStreamingDelta(summaryWire, delta)
		if deltaOut == "" {
			return nil
		}
		sendProgress("response_delta", deltaOut, openai.WithSSEAccumulated(map[string]interface{}{
			"conversationId": conversationID,
		}, summaryWire))
		return nil
	})
	if strings.TrimSpace(streamText) != "" {
		result.Response = streamText
		result.LastAgentTraceOutput = result.Response
	sendProgress("progress", "Summary generation complete", nil)
		return result, nil
	}

	// 如果无法生成总结，返回友好的提示
	result.Response = fmt.Sprintf("Maximum iterations (%d) reached. Multiple rounds of testing were executed but the iteration limit was hit. Review executed tool results or submit a new test request to continue.", a.maxIterations)
	result.LastAgentTraceOutput = result.Response
	return result, nil
}

// getAvailableTools 获取可用工具
// 从MCP服务器动态获取工具列表，描述模式由 tool_description_mode 控制
// roleTools: 角色配置的工具列表（toolKey格式），如果为空或nil，则使用所有工具（默认角色）
func (a *Agent) getAvailableTools(roleTools []string) []Tool {
	// 构建角色工具集合（用于快速查找）
	roleToolSet := make(map[string]bool)
	if len(roleTools) > 0 {
		for _, toolKey := range roleTools {
			roleToolSet[toolKey] = true
		}
	}

	// 从MCP服务器获取所有已注册的内部工具
	mcpTools := a.mcpServer.GetAllTools()

	// 转换为OpenAI格式的工具定义
	tools := make([]Tool, 0, len(mcpTools))
	for _, mcpTool := range mcpTools {
		// 如果指定了角色工具列表，只添加在列表中的工具
		if len(roleToolSet) > 0 {
			toolKey := mcpTool.Name // 内置工具使用工具名称作为key
			if !roleToolSet[toolKey] {
				continue // 不在角色工具列表中，跳过
			}
		}
		description := a.pickToolDescription(mcpTool.ShortDescription, mcpTool.Description)

		// 转换schema中的类型为OpenAI标准类型
		convertedSchema := a.convertSchemaTypes(mcpTool.InputSchema)

		tools = append(tools, Tool{
			Type: "function",
			Function: FunctionDefinition{
				Name:        mcpTool.Name,
				Description: description, // 使用简短描述减少token消耗
				Parameters:  convertedSchema,
			},
		})
	}

	// 获取外部MCP工具
	if a.externalMCPMgr != nil {
		// 增加超时时间到30秒，因为通过代理连接远程服务器可能需要更长时间
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		externalTools, err := a.externalMCPMgr.GetAllTools(ctx)
		extMap := make(map[string]string)
		if err != nil {
		a.logger.Warn("Failed to get external MCP tools", zap.Error(err))
		} else {
			// 获取外部MCP配置，用于检查工具启用状态
			externalMCPConfigs := a.externalMCPMgr.GetConfigs()

			// 将外部MCP工具添加到工具列表（只添加启用的工具）
			for _, externalTool := range externalTools {
				// 外部工具使用 "mcpName::toolName" 作为toolKey
				externalToolKey := externalTool.Name

				// 如果指定了角色工具列表，只添加在列表中的工具
				if len(roleToolSet) > 0 {
					if !roleToolSet[externalToolKey] {
						continue // 不在角色工具列表中，跳过
					}
				}

				// 解析工具名称：mcpName::toolName
				var mcpName, actualToolName string
				if idx := strings.Index(externalTool.Name, "::"); idx > 0 {
					mcpName = externalTool.Name[:idx]
					actualToolName = externalTool.Name[idx+2:]
				} else {
					continue // 跳过格式不正确的工具
				}

				// 检查工具是否启用
				enabled := false
				if cfg, exists := externalMCPConfigs[mcpName]; exists {
					// 首先检查外部MCP是否启用
					if !cfg.ExternalMCPEnable {
						enabled = false // MCP未启用，所有工具都禁用
					} else {
						// MCP已启用，检查单个工具的启用状态
						// 如果ToolEnabled为空或未设置该工具，默认为启用（向后兼容）
						if cfg.ToolEnabled == nil {
							enabled = true // 未设置工具状态，默认为启用
						} else if toolEnabled, exists := cfg.ToolEnabled[actualToolName]; exists {
							enabled = toolEnabled // 使用配置的工具状态
						} else {
							enabled = true // 工具未在配置中，默认为启用
						}
					}
				}

				// 只添加启用的工具
				if !enabled {
					continue
				}

				description := a.pickToolDescription(externalTool.ShortDescription, externalTool.Description)

				// 转换schema中的类型为OpenAI标准类型
				convertedSchema := a.convertSchemaTypes(externalTool.InputSchema)

				// 将工具名称中的 "::" 替换为 "__" 以符合OpenAI命名规范
				// OpenAI要求工具名称只能包含 [a-zA-Z0-9_-]
				openAIName := strings.ReplaceAll(externalTool.Name, "::", "__")

				// 保存名称映射关系（OpenAI格式 -> 原始格式）
				extMap[openAIName] = externalTool.Name

				tools = append(tools, Tool{
					Type: "function",
					Function: FunctionDefinition{
						Name:        openAIName, // 使用符合OpenAI规范的名称
						Description: description,
						Parameters:  convertedSchema,
					},
				})
			}
		}
		a.mu.Lock()
		a.toolNameMapping = extMap
		a.mu.Unlock()
	}

	a.logger.Debug("获取可用工具列表",
		zap.Int("internalTools", len(mcpTools)),
		zap.Int("totalTools", len(tools)),
	)

	return tools
}

func (a *Agent) pickToolDescription(shortDesc, fullDesc string) string {
	a.mu.RLock()
	mode := strings.TrimSpace(strings.ToLower(a.toolDescriptionMode))
	a.mu.RUnlock()
	if mode == "full" {
		return fullDesc
	}
	if shortDesc != "" {
		return shortDesc
	}
	return fullDesc
}

// convertSchemaTypes 递归转换schema中的类型为OpenAI标准类型
func (a *Agent) convertSchemaTypes(schema map[string]interface{}) map[string]interface{} {
	if schema == nil {
		return schema
	}

	// 创建新的schema副本
	converted := make(map[string]interface{})
	for k, v := range schema {
		converted[k] = v
	}

	// 转换properties中的类型
	if properties, ok := converted["properties"].(map[string]interface{}); ok {
		convertedProperties := make(map[string]interface{})
		for propName, propValue := range properties {
			if prop, ok := propValue.(map[string]interface{}); ok {
				convertedProp := make(map[string]interface{})
				for pk, pv := range prop {
					if pk == "type" {
						// 转换类型
						if typeStr, ok := pv.(string); ok {
							convertedProp[pk] = a.convertToOpenAIType(typeStr)
						} else {
							convertedProp[pk] = pv
						}
					} else {
						convertedProp[pk] = pv
					}
				}
				convertedProperties[propName] = convertedProp
			} else {
				convertedProperties[propName] = propValue
			}
		}
		converted["properties"] = convertedProperties
	}

	return converted
}

// convertToOpenAIType 将配置中的类型转换为OpenAI/JSON Schema标准类型
func (a *Agent) convertToOpenAIType(configType string) string {
	switch configType {
	case "bool":
		return "boolean"
	case "int", "integer":
		return "number"
	case "float", "double":
		return "number"
	case "string", "array", "object":
		return configType
	default:
		// 默认返回原类型
		return configType
	}
}

// isRetryableError 判断错误是否可重试
func (a *Agent) isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// 网络相关错误，可以重试
	retryableErrors := []string{
		"connection reset",
		"connection reset by peer",
		"connection refused",
		"timeout",
		"i/o timeout",
		"context deadline exceeded",
		"no such host",
		"network is unreachable",
		"broken pipe",
		"EOF",
		"read tcp",
		"write tcp",
		"dial tcp",
	}
	for _, retryable := range retryableErrors {
		if strings.Contains(strings.ToLower(errStr), retryable) {
			return true
		}
	}
	return false
}

// callOpenAI 调用OpenAI API。重试逻辑由 openai.Client 统一处理（最多5次，指数退避）。
func (a *Agent) callOpenAI(ctx context.Context, messages []ChatMessage, tools []Tool) (*OpenAIResponse, error) {
	return a.callOpenAISingle(ctx, messages, tools)
}

// callOpenAISingle 单次调用OpenAI API（不包含重试逻辑）
func (a *Agent) callOpenAISingle(ctx context.Context, messages []ChatMessage, tools []Tool) (*OpenAIResponse, error) {
	reqBody := OpenAIRequest{
		Model:    a.config.Model,
		Messages: messages,
	}

	if len(tools) > 0 {
		reqBody.Tools = tools
	}

	a.logger.Debug("准备发送OpenAI请求",
		zap.Int("messagesCount", len(messages)),
		zap.Int("toolsCount", len(tools)),
	)

	var response OpenAIResponse
	if a.openAIClient == nil {
		return nil, fmt.Errorf("OpenAI客户端未初始化")
	}
	if err := a.openAIClient.ChatCompletion(ctx, reqBody, &response); err != nil {
		return nil, err
	}

	return &response, nil
}

// callOpenAISingleStreamText 单次调用OpenAI的流式模式，只用于“不会调用工具”的纯文本输出（tools 为空时最佳）。
// onDelta 每收到一段 content delta，就回调一次；如果 callback 返回错误，会终止读取并返回错误。
func (a *Agent) callOpenAISingleStreamText(ctx context.Context, messages []ChatMessage, tools []Tool, onDelta func(delta string) error) (string, error) {
	reqBody := OpenAIRequest{
		Model:    a.config.Model,
		Messages: messages,
		Stream:   true,
	}
	if len(tools) > 0 {
		reqBody.Tools = tools
	}

	if a.openAIClient == nil {
		return "", fmt.Errorf("OpenAI客户端未初始化")
	}

	return a.openAIClient.ChatCompletionStream(ctx, reqBody, onDelta)
}

// callOpenAIStreamText 调用OpenAI流式模式。重试逻辑（含 deltasSent 保护）由 openai.Client 统一处理。
func (a *Agent) callOpenAIStreamText(ctx context.Context, messages []ChatMessage, tools []Tool, onDelta func(delta string) error) (string, error) {
	return a.callOpenAISingleStreamText(ctx, messages, tools, onDelta)
}

// callOpenAISingleStreamWithToolCalls 单次调用OpenAI流式模式（带工具调用解析），不包含重试逻辑。
func (a *Agent) callOpenAISingleStreamWithToolCalls(
	ctx context.Context,
	messages []ChatMessage,
	tools []Tool,
	onContentDelta func(delta string) error,
) (*OpenAIResponse, error) {
	reqBody := OpenAIRequest{
		Model:    a.config.Model,
		Messages: messages,
		Stream:   true,
	}
	if len(tools) > 0 {
		reqBody.Tools = tools
	}
	if a.openAIClient == nil {
		return nil, fmt.Errorf("OpenAI客户端未初始化")
	}

	content, streamToolCalls, finishReason, err := a.openAIClient.ChatCompletionStreamWithToolCalls(ctx, reqBody, onContentDelta)
	if err != nil {
		return nil, err
	}

	toolCalls := make([]ToolCall, 0, len(streamToolCalls))
	for _, stc := range streamToolCalls {
		fnArgsStr := stc.FunctionArgsStr
		args := make(map[string]interface{})
		if strings.TrimSpace(fnArgsStr) != "" {
			if err := json.Unmarshal([]byte(fnArgsStr), &args); err != nil {
				// 兼容：arguments 不一定是严格 JSON
				args = map[string]interface{}{"raw": fnArgsStr}
			}
		}

		typ := stc.Type
		if strings.TrimSpace(typ) == "" {
			typ = "function"
		}

		toolCalls = append(toolCalls, ToolCall{
			ID:   stc.ID,
			Type: typ,
			Function: FunctionCall{
				Name:      stc.FunctionName,
				Arguments: args,
			},
		})
	}

	response := &OpenAIResponse{
		ID: "",
		Choices: []Choice{
			{
				Message: MessageWithTools{
					Role:      "assistant",
					Content:   content,
					ToolCalls: toolCalls,
				},
				FinishReason: finishReason,
			},
		},
	}
	return response, nil
}

// callOpenAIStreamWithToolCalls 调用OpenAI流式模式（含工具调用）。重试逻辑由 openai.Client 统一处理。
func (a *Agent) callOpenAIStreamWithToolCalls(
	ctx context.Context,
	messages []ChatMessage,
	tools []Tool,
	onContentDelta func(delta string) error,
) (*OpenAIResponse, error) {
	return a.callOpenAISingleStreamWithToolCalls(ctx, messages, tools, onContentDelta)
}

// ToolExecutionResult 工具执行结果
type ToolExecutionResult struct {
	Result      string
	ExecutionID string
	IsError     bool // 标记是否为错误结果
}

// executeToolViaMCP 通过MCP执行工具
// 即使工具执行失败，也返回结果而不是错误，让AI能够处理错误情况
func (a *Agent) executeToolViaMCP(ctx context.Context, toolName string, args map[string]interface{}) (*ToolExecutionResult, error) {
	a.logger.Info("Executing tool via MCP",
		zap.String("tool", toolName),
		zap.Any("args", args),
	)

	// If record_vulnerability tool, auto-add conversation_id
	if toolName == builtin.ToolRecordVulnerability {
		conversationID := agentConversationIDFromContext(ctx)
		if conversationID == "" {
			a.mu.RLock()
			conversationID = a.currentConversationID
			a.mu.RUnlock()
		}

		if conversationID != "" {
			args["conversation_id"] = conversationID
			a.logger.Debug("Auto-added conversation_id to record_vulnerability tool",
				zap.String("conversation_id", conversationID),
			)
		} else {
			a.logger.Warn("conversation_id empty when calling record_vulnerability tool")
		}
	}

	var result *mcp.ToolResult
	var executionID string
	var err error

	// 单次工具执行超时：防止单个工具长时间挂起（如 30 分钟仍显示执行中）
	toolCtx := ctx
	var toolCancel context.CancelFunc
	if a.agentConfig != nil && a.agentConfig.ToolTimeoutMinutes > 0 {
		toolCtx, toolCancel = context.WithTimeout(ctx, time.Duration(a.agentConfig.ToolTimeoutMinutes)*time.Minute)
		defer func() {
			if toolCancel != nil {
				toolCancel()
			}
		}()
	}
	// C2 危险任务 HITL 异步等待：须绑定整条 Agent 运行期 ctx，而非单次工具子 ctx（return 时会被 cancel）
	toolCtx = c2.WithHITLRunContext(toolCtx, ctx)

	// 检查是否是外部MCP工具（通过工具名称映射）
	a.mu.RLock()
	originalToolName, isExternalTool := a.toolNameMapping[toolName]
	a.mu.RUnlock()

	if isExternalTool && a.externalMCPMgr != nil {
		// Call external MCP tool using original name
		a.logger.Debug("Calling external MCP tool",
			zap.String("openAIName", toolName),
			zap.String("originalName", originalToolName),
		)
		result, executionID, err = a.externalMCPMgr.CallTool(toolCtx, originalToolName, args)
	} else {
		// Call internal MCP tool
		result, executionID, err = a.mcpServer.CallTool(toolCtx, toolName, args)
	}

	// 如果调用失败（如工具不存在、超时），返回友好的错误信息而不是抛出异常
	if err != nil {
		detail := err.Error()
		if errors.Is(err, context.Canceled) {
			detail = "Tool call was manually terminated (MCP monitor page). The agent will continue with this result; the entire task is not stopped."
		} else if errors.Is(err, context.DeadlineExceeded) {
			min := 10
			if a.agentConfig != nil && a.agentConfig.ToolTimeoutMinutes > 0 {
				min = a.agentConfig.ToolTimeoutMinutes
			}
			detail = fmt.Sprintf("Tool execution exceeded %d minutes and was auto-terminated (adjust agent.tool_timeout_minutes in config.yaml)", min)
		}
		errorMsg := fmt.Sprintf(`Tool call failed

Tool name: %s
Error type: System error
Error details: %s

Possible causes:
- Tool "%s" does not exist or is not enabled
- Single execution timeout (agent.tool_timeout_minutes)
- System configuration issue
- Network or permission issue

Suggestions:
- Check if the tool name is correct
- Increase agent.tool_timeout_minutes if more execution time is needed
- Try alternative tools
- If this is a required tool, explain the situation to the user`, toolName, detail, toolName)

		return &ToolExecutionResult{
			Result:      errorMsg,
			ExecutionID: executionID,
			IsError:     true,
		}, nil // 返回 nil 错误，让调用者处理结果
	}

	// 格式化结果
	var resultText strings.Builder
	for _, content := range result.Content {
		resultText.WriteString(content.Text)
		resultText.WriteString("\n")
	}

	resultStr := resultText.String()
	resultSize := len(resultStr)

	// 检测大结果并保存
	a.mu.RLock()
	threshold := a.largeResultThreshold
	storage := a.resultStorage
	a.mu.RUnlock()

	if resultSize > threshold && storage != nil {
		// 异步保存大结果
		go func() {
			if err := storage.SaveResult(executionID, toolName, resultStr); err != nil {
				a.logger.Warn("保存大结果失败",
					zap.String("executionID", executionID),
					zap.String("toolName", toolName),
					zap.Error(err),
				)
			} else {
				a.logger.Info("Large result saved",
					zap.String("executionID", executionID),
					zap.String("toolName", toolName),
					zap.Int("size", resultSize),
				)
			}
		}()

		// 返回最小化通知
		lines := strings.Split(resultStr, "\n")
		filePath := ""
		if storage != nil {
			filePath = storage.GetResultPath(executionID)
		}
		notification := a.formatMinimalNotification(executionID, toolName, resultSize, len(lines), filePath)

		return &ToolExecutionResult{
			Result:      notification,
			ExecutionID: executionID,
			IsError:     result != nil && result.IsError,
		}, nil
	}

	return &ToolExecutionResult{
		Result:      resultStr,
		ExecutionID: executionID,
		IsError:     result != nil && result.IsError,
	}, nil
}


// formatMinimalNotification formats a minimal notification for large result storage
func (a *Agent) formatMinimalNotification(executionID string, toolName string, size int, lineCount int, filePath string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Tool execution completed. Result saved (ID: %s).\n\n", executionID))
	sb.WriteString("Result info:\n")
	sb.WriteString(fmt.Sprintf("  - Tool: %s\n", toolName))
	sb.WriteString(fmt.Sprintf("  - Size: %d bytes (%.2f KB)\n", size, float64(size)/1024))
	sb.WriteString(fmt.Sprintf("  - Lines: %d\n", lineCount))
	if filePath != "" {
		sb.WriteString(fmt.Sprintf("  - File path: %s\n", filePath))
	}
	sb.WriteString("\n")
	sb.WriteString("Recommended: use query_execution_result tool to query full results:\n")
	sb.WriteString(fmt.Sprintf("  - Query first page: query_execution_result(execution_id=\"%s\", page=1, limit=100)\n", executionID))
	sb.WriteString(fmt.Sprintf("  - Search keyword: query_execution_result(execution_id=\"%s\", search=\"keyword\")\n", executionID))
	sb.WriteString(fmt.Sprintf("  - Filter: query_execution_result(execution_id=\"%s\", filter=\"error\")\n", executionID))
	sb.WriteString(fmt.Sprintf("  - Regex match: query_execution_result(execution_id=\"%s\", search=\"\\\\d+\\\\.\\\\d+\\\\.\\\\d+\\\\.\\\\d+\", use_regex=true)\n", executionID))
	sb.WriteString("\n")
	if filePath != "" {
		sb.WriteString("If query_execution_result doesn't meet your needs, use other tools:\n")
		sb.WriteString("\n")
		sb.WriteString("**Segmented read examples:**\n")
		sb.WriteString(fmt.Sprintf("  - View first 100 lines: exec(command=\"head\", args=[\"-n\", \"100\", \"%s\"])\n", filePath))
		sb.WriteString(fmt.Sprintf("  - View last 100 lines: exec(command=\"tail\", args=[\"-n\", \"100\", \"%s\"])\n", filePath))
		sb.WriteString(fmt.Sprintf("  - View lines 50-150: exec(command=\"sed\", args=[\"-n\", \"50,150p\", \"%s\"])\n", filePath))
		sb.WriteString("\n")
		sb.WriteString("**Search and regex examples:**\n")
		sb.WriteString(fmt.Sprintf("  - Search keyword: exec(command=\"grep\", args=[\"keyword\", \"%s\"])\n", filePath))
		sb.WriteString(fmt.Sprintf("  - Regex match IP addresses: exec(command=\"grep\", args=[\"-E\", \"\\\\d+\\\\.\\\\d+\\\\.\\\\d+\\\\.\\\\d+\", \"%s\"])\n", filePath))
		sb.WriteString(fmt.Sprintf("  - Case-insensitive search: exec(command=\"grep\", args=[\"-i\", \"keyword\", \"%s\"])\n", filePath))
		sb.WriteString(fmt.Sprintf("  - Show matching line numbers: exec(command=\"grep\", args=[\"-n\", \"keyword\", \"%s\"])\n", filePath))
		sb.WriteString("\n")
		sb.WriteString("**Filter and statistics examples:**\n")
		sb.WriteString(fmt.Sprintf("  - Count total lines: exec(command=\"wc\", args=[\"-l\", \"%s\"])\n", filePath))
		sb.WriteString(fmt.Sprintf("  - Filter lines containing error: exec(command=\"grep\", args=[\"error\", \"%s\"])\n", filePath))
		sb.WriteString(fmt.Sprintf("  - Exclude empty lines: exec(command=\"grep\", args=[\"-v\", \"^$\", \"%s\"])\n", filePath))
		sb.WriteString("\n")
		sb.WriteString("**Full read (not recommended for large files):**\n")
		sb.WriteString(fmt.Sprintf("  - Using cat tool: cat(file=\"%s\")\n", filePath))
		sb.WriteString(fmt.Sprintf("  - Using exec tool: exec(command=\"cat\", args=[\"%s\"])\n", filePath))
		sb.WriteString("\n")
		sb.WriteString("**Note:**\n")
		sb.WriteString("  - Reading large files directly may trigger the result save mechanism again\n")
		sb.WriteString("  - Prefer segmented reading and search to avoid loading entire file at once\n")
		sb.WriteString("  - Regex syntax follows standard POSIX regex specification\n")
	}

	return sb.String()
}

// UpdateConfig 更新OpenAI配置
func (a *Agent) UpdateConfig(cfg *config.OpenAIConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.config = cfg

	// 同时更新MemoryCompressor的配置（如果存在）
	if a.memoryCompressor != nil {
		a.memoryCompressor.UpdateConfig(cfg)
	}

	a.logger.Info("Agent config updated",
		zap.String("base_url", cfg.BaseURL),
		zap.String("model", cfg.Model),
	)
}

// UpdateMaxIterations 更新最大迭代次数
func (a *Agent) UpdateMaxIterations(maxIterations int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if maxIterations > 0 {
		a.maxIterations = maxIterations
		a.logger.Info("Agent max iterations updated", zap.Int("max_iterations", maxIterations))
	}
}

// UpdateToolDescriptionMode 更新工具描述模式（short/full）
func (a *Agent) UpdateToolDescriptionMode(mode string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode != "full" {
		mode = "short"
	}
	a.toolDescriptionMode = mode
	a.logger.Info("Agent工具描述模式已更新", zap.String("tool_description_mode", mode))
}

// formatToolError 格式化工具错误信息，提供更友好的错误描述
func (a *Agent) formatToolError(toolName string, args map[string]interface{}, err error) string {
	errorMsg := fmt.Sprintf(`工具执行失败

工具名称: %s
调用参数: %v
错误信息: %v

请分析错误原因并采取以下行动之一：
1. 如果参数错误，请修正参数后重试
2. 如果工具不可用，请尝试使用替代工具
3. 如果这是系统问题，请向用户说明情况并提供建议
4. 如果错误信息中包含有用信息，可以基于这些信息继续分析`, toolName, args, err)

	return errorMsg
}

// applyMemoryCompression 在调用LLM前对消息进行压缩，避免超过 token 限制。reservedTokens 为预留给 tools 的 token 数，传 0 表示不预留。
func (a *Agent) applyMemoryCompression(ctx context.Context, messages []ChatMessage, reservedTokens int) []ChatMessage {
	if a.memoryCompressor == nil {
		return messages
	}

	compressed, changed, err := a.memoryCompressor.CompressHistory(ctx, messages, reservedTokens)
	if err != nil {
		a.logger.Warn("上下文压缩失败，将使用原始消息继续", zap.Error(err))
		return messages
	}
	if changed {
		a.logger.Info("历史上下文已压缩",
			zap.Int("originalMessages", len(messages)),
			zap.Int("compressedMessages", len(compressed)),
		)
		return compressed
	}

	return messages
}

// countToolsTokens 统计 tools 序列化后的 token 数，用于日志与压缩时预留空间。mc 为 nil 时返回 0。
func (a *Agent) countToolsTokens(tools []Tool) int {
	if len(tools) == 0 || a.memoryCompressor == nil {
		return 0
	}
	data, err := json.Marshal(tools)
	if err != nil {
		return 0
	}
	return a.memoryCompressor.CountTextTokens(string(data))
}

// handleMissingToolError 当LLM调用不存在的工具时，向其追加提示消息并允许继续迭代
func (a *Agent) handleMissingToolError(errMsg string, messages *[]ChatMessage) (bool, string) {
	lowerMsg := strings.ToLower(errMsg)
	if !(strings.Contains(lowerMsg, "non-exist tool") || strings.Contains(lowerMsg, "non exist tool")) {
		return false, ""
	}

	toolName := extractQuotedToolName(errMsg)
	if toolName == "" {
		toolName = "unknown_tool"
	}

	notice := fmt.Sprintf("System notice: the previous call failed with error: %s. Please verify tool availability and proceed using existing tools or pure reasoning.", errMsg)
	*messages = append(*messages, ChatMessage{
		Role:    "user",
		Content: notice,
	})

	return true, toolName
}

// handleToolRoleError 自动修复因缺失tool_calls导致的OpenAI错误
func (a *Agent) handleToolRoleError(errMsg string, messages *[]ChatMessage) bool {
	if messages == nil {
		return false
	}

	lowerMsg := strings.ToLower(errMsg)
	if !(strings.Contains(lowerMsg, "role 'tool'") && strings.Contains(lowerMsg, "tool_calls")) {
		return false
	}

	fixed := a.repairOrphanToolMessages(messages)
	if !fixed {
		return false
	}

	notice := "System notice: the previous call failed because some tool outputs lost their corresponding assistant tool_calls context. The history has been repaired. Please continue."
	*messages = append(*messages, ChatMessage{
		Role:    "user",
		Content: notice,
	})

	return true
}

// RepairOrphanToolMessages 清理失去配对的tool消息和未完成的tool_calls，避免OpenAI报错
// 同时确保历史消息中的tool_calls只作为上下文记忆，不会触发重新执行
// 这是一个公开方法，可以在恢复历史消息时调用
func (a *Agent) RepairOrphanToolMessages(messages *[]ChatMessage) bool {
	return a.repairOrphanToolMessages(messages)
}

// repairOrphanToolMessages 清理失去配对的tool消息和未完成的tool_calls，避免OpenAI报错
// 同时确保历史消息中的tool_calls只作为上下文记忆，不会触发重新执行
func (a *Agent) repairOrphanToolMessages(messages *[]ChatMessage) bool {
	if messages == nil {
		return false
	}

	msgs := *messages
	if len(msgs) == 0 {
		return false
	}

	pending := make(map[string]int)
	cleaned := make([]ChatMessage, 0, len(msgs))
	removed := false

	for _, msg := range msgs {
		switch strings.ToLower(msg.Role) {
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				// 记录所有tool_call IDs
				for _, tc := range msg.ToolCalls {
					if tc.ID != "" {
						pending[tc.ID]++
					}
				}
			}
			cleaned = append(cleaned, msg)
		case "tool":
			callID := msg.ToolCallID
			if callID == "" {
				removed = true
				continue
			}
			if count, exists := pending[callID]; exists && count > 0 {
				if count == 1 {
					delete(pending, callID)
				} else {
					pending[callID] = count - 1
				}
				cleaned = append(cleaned, msg)
			} else {
				removed = true
				continue
			}
		default:
			cleaned = append(cleaned, msg)
		}
	}

	// 如果还有未匹配的tool_calls（即assistant消息有tool_calls但没有对应的tool响应）
	// 需要从最后的assistant消息中移除这些tool_calls，避免AI重新执行它们
	if len(pending) > 0 {
		// 从后往前查找最后一个assistant消息
		for i := len(cleaned) - 1; i >= 0; i-- {
			if strings.ToLower(cleaned[i].Role) == "assistant" && len(cleaned[i].ToolCalls) > 0 {
				// 移除未匹配的tool_calls
				originalCount := len(cleaned[i].ToolCalls)
				validToolCalls := make([]ToolCall, 0)
				for _, tc := range cleaned[i].ToolCalls {
					if tc.ID != "" && pending[tc.ID] > 0 {
						// 这个tool_call没有对应的tool响应，移除它
						removed = true
						delete(pending, tc.ID)
					} else {
						validToolCalls = append(validToolCalls, tc)
					}
				}
				// 更新消息的ToolCalls
				if len(validToolCalls) != originalCount {
					cleaned[i].ToolCalls = validToolCalls
					a.logger.Info("移除了未完成的tool_calls，避免重新执行",
						zap.Int("removed_count", originalCount-len(validToolCalls)),
					)
				}
				break
			}
		}
	}

	if removed {
		a.logger.Warn("修复了对话历史中的tool消息和tool_calls",
			zap.Int("original_messages", len(msgs)),
			zap.Int("cleaned_messages", len(cleaned)),
		)
		*messages = cleaned
	}

	return removed
}

// ToolsForRole 返回与单 Agent 循环一致的工具定义（OpenAI function 格式），供 Eino DeepAgent 等编排层绑定 MCP 工具。
func (a *Agent) ToolsForRole(roleTools []string) []Tool {
	return a.getAvailableTools(roleTools)
}

// ExecuteMCPToolForConversation 在指定会话上下文中执行 MCP 工具（行为与主 Agent 循环中的工具调用一致，如自动注入 conversation_id）。
func (a *Agent) ExecuteMCPToolForConversation(ctx context.Context, conversationID, toolName string, args map[string]interface{}) (*ToolExecutionResult, error) {
	a.mu.Lock()
	prev := a.currentConversationID
	a.currentConversationID = conversationID
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.currentConversationID = prev
		a.mu.Unlock()
	}()
	ctx = withAgentConversationID(ctx, conversationID)
	return a.executeToolViaMCP(ctx, toolName, args)
}

// RecordLocalToolExecution 将非 CallTool 路径完成的工具调用写入 MCP 监控库（与 CallTool 落库一致），返回 executionId。
// 用于 Eino filesystem execute 等场景，使助手气泡「渗透测试详情」与常规 MCP 一致可点进监控。
func (a *Agent) RecordLocalToolExecution(toolName string, args map[string]interface{}, resultText string, invokeErr error) string {
	if a == nil || a.mcpServer == nil {
		return ""
	}
	return a.mcpServer.RecordCompletedToolInvocation(toolName, args, resultText, invokeErr)
}

// CancelMCPToolExecutionWithNote 取消一次进行中的 MCP 工具（先内部后外部），与监控页「终止工具」一致；note 非空时合并进返回给模型的文本。
func (a *Agent) CancelMCPToolExecutionWithNote(executionID, note string) bool {
	executionID = strings.TrimSpace(executionID)
	note = strings.TrimSpace(note)
	if executionID == "" {
		return false
	}
	if a.mcpServer != nil && a.mcpServer.CancelToolExecutionWithNote(executionID, note) {
		return true
	}
	if a.externalMCPMgr != nil && a.externalMCPMgr.CancelToolExecutionWithNote(executionID, note) {
		return true
	}
	return false
}

// extractQuotedToolName 尝试从错误信息中提取被引用的工具名称
func extractQuotedToolName(errMsg string) string {
	start := strings.Index(errMsg, "\"")
	if start == -1 {
		return ""
	}
	rest := errMsg[start+1:]
	end := strings.Index(rest, "\"")
	if end == -1 {
		return ""
	}
	return rest[:end]
}
