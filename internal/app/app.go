package app

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/audit"
	"cyberstrike-ai/internal/c2"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/einoobserve"
	"cyberstrike-ai/internal/handler"
	"cyberstrike-ai/internal/knowledge"
	"cyberstrike-ai/internal/logger"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/mcp/builtin"
	"cyberstrike-ai/internal/robot"
	"cyberstrike-ai/internal/security"
	"cyberstrike-ai/internal/skillpackage"
	"cyberstrike-ai/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/net/http2"
)

// App 应用
type App struct {
	config             *config.Config
	logger             *logger.Logger
	router             *gin.Engine
	mcpServer          *mcp.Server
	externalMCPMgr     *mcp.ExternalMCPManager
	agent              *agent.Agent
	executor           *security.Executor
	db                 *database.DB
	knowledgeDB        *database.DB // 知识库数据库连接（如果使用独立数据库）
	auth               *security.AuthManager
	knowledgeManager   *knowledge.Manager        // 知识库管理器（用于动态初始化）
	knowledgeRetriever *knowledge.Retriever      // 知识库检索器（用于动态初始化）
	knowledgeIndexer   *knowledge.Indexer        // 知识库索引器（用于动态初始化）
	knowledgeHandler   *handler.KnowledgeHandler // 知识库处理器（用于动态初始化）
	agentHandler       *handler.AgentHandler     // Agent处理器（用于更新知识库管理器）
	robotHandler       *handler.RobotHandler     // 机器人处理器（钉钉/飞书/企业微信）
	robotMu            sync.Mutex                // 保护钉钉/飞书长连接的 cancel
	dingCancel         context.CancelFunc        // 钉钉 Stream 取消函数，用于配置变更时重启
	larkCancel         context.CancelFunc        // 飞书长连接取消函数，用于配置变更时重启
	wechatCancel       context.CancelFunc        // 微信 iLink 长轮询取消函数
	c2Manager          *c2.Manager               // C2 管理器（未启用 C2 时为 nil）
	c2Watchdog         *c2.SessionWatchdog       // C2 会话看门狗
	c2WatchdogCancel   context.CancelFunc        // 看门狗取消函数
	c2Handler          *handler.C2Handler        // C2 REST（与 Manager 生命周期同步）
	auditSvc           *audit.Service
}

// New 创建新应用
func New(cfg *config.Config, log *logger.Logger, configPath string) (*App, error) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()

	// CORS中间件
	router.Use(corsMiddleware())

	// 认证管理器
	authManager, err := security.NewAuthManager(cfg.Auth.Password, cfg.Auth.SessionDurationHours)
	if err != nil {
		return nil, fmt.Errorf("auth initialization failed: %w", err)
	}

	// 初始化数据库
	dbPath := cfg.Database.Path
	if dbPath == "" {
		dbPath = "data/conversations.db"
	}

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	db, err := database.NewDB(dbPath, log.Logger)
	if err != nil {
		return nil, fmt.Errorf("database initialization failed: %w", err)
	}

	auditSvc := audit.NewService(db, cfg, log.Logger)
	audit.RegisterConversationCreateHook(auditSvc)
	auditSvc.PurgeExpired()
	audit.StartRetentionLoop(auditSvc, log.Logger)

	// 创建MCP服务器（带数据库持久化）
	mcpServer := mcp.NewServerWithStorage(log.Logger, db)
	mcpServer.ConfigureHTTPToolCallTimeoutFromAgentMinutes(cfg.Agent.ToolTimeoutMinutes)

	// 创建安全工具执行器
	executor := security.NewExecutor(&cfg.Security, mcpServer, log.Logger)

	// 注册工具
	executor.RegisterTools(mcpServer)

	// 注册漏洞记录工具
	registerVulnerabilityTool(mcpServer, db, log.Logger)

	if cfg.Auth.GeneratedPassword != "" {
		config.PrintGeneratedPasswordWarning(cfg.Auth.GeneratedPassword, cfg.Auth.GeneratedPasswordPersisted, cfg.Auth.GeneratedPasswordPersistErr)
		cfg.Auth.GeneratedPassword = ""
		cfg.Auth.GeneratedPasswordPersisted = false
		cfg.Auth.GeneratedPasswordPersistErr = ""
	}

	// 创建外部MCP管理器（使用与内部MCP服务器相同的存储）
	externalMCPMgr := mcp.NewExternalMCPManagerWithStorage(log.Logger, db)
	if cfg.ExternalMCP.Servers != nil {
		externalMCPMgr.LoadConfigs(&cfg.ExternalMCP)
		// 启动所有启用的外部MCP客户端
		externalMCPMgr.StartAllEnabled()
	}

	// 初始化结果存储
	resultStorageDir := "tmp"
	if cfg.Agent.ResultStorageDir != "" {
		resultStorageDir = cfg.Agent.ResultStorageDir
	}

	// 确保存储目录存在
	if err := os.MkdirAll(resultStorageDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create result storage directory: %w", err)
	}

	// 创建结果存储实例
	resultStorage, err := storage.NewFileResultStorage(resultStorageDir, log.Logger)
	if err != nil {
		return nil, fmt.Errorf("result storage initialization failed: %w", err)
	}

	// 创建Agent
	maxIterations := cfg.Agent.MaxIterations
	if maxIterations <= 0 {
		maxIterations = 30 // 默认值
	}
	agent := agent.NewAgent(&cfg.OpenAI, &cfg.Agent, mcpServer, externalMCPMgr, log.Logger, maxIterations)
	agent.UpdateToolDescriptionMode(cfg.Security.ToolDescriptionMode)

	// 设置结果存储到Agent
	agent.SetResultStorage(resultStorage)

	// 设置结果存储到Executor（用于查询工具）
	executor.SetResultStorage(resultStorage)

	// 初始化知识库模块（如果启用）
	var knowledgeManager *knowledge.Manager
	var knowledgeRetriever *knowledge.Retriever
	var knowledgeIndexer *knowledge.Indexer
	var knowledgeHandler *handler.KnowledgeHandler

	var knowledgeDBConn *database.DB
	log.Logger.Info("Checking knowledge base config", zap.Bool("enabled", cfg.Knowledge.Enabled))
	if cfg.Knowledge.Enabled {
		// 确定知识库数据库路径
		knowledgeDBPath := cfg.Database.KnowledgeDBPath
		var knowledgeDB *sql.DB

		if knowledgeDBPath != "" {
			// 使用独立的知识库数据库
			// 确保目录存在
			if err := os.MkdirAll(filepath.Dir(knowledgeDBPath), 0755); err != nil {
				return nil, fmt.Errorf("failed to create knowledge base database directory: %w", err)
			}

			var err error
			knowledgeDBConn, err = database.NewKnowledgeDB(knowledgeDBPath, log.Logger)
			if err != nil {
				return nil, fmt.Errorf("knowledge base database initialization failed: %w", err)
			}
			knowledgeDB = knowledgeDBConn.DB
			log.Logger.Info("Using separate knowledge base database", zap.String("path", knowledgeDBPath))
		} else {
			// 向后兼容：使用会话数据库
			knowledgeDB = db.DB
			log.Logger.Info("Using session database for knowledge base storage (recommend configuring knowledge_db_path for data separation)")
		}

		// 创建知识库管理器
		knowledgeManager = knowledge.NewManager(knowledgeDB, cfg.Knowledge.BasePath, log.Logger)

		// 创建嵌入器
		// 使用OpenAI配置的API Key（如果知识库配置中没有指定）
		if cfg.Knowledge.Embedding.APIKey == "" {
			cfg.Knowledge.Embedding.APIKey = cfg.OpenAI.APIKey
		}
		if cfg.Knowledge.Embedding.BaseURL == "" {
			cfg.Knowledge.Embedding.BaseURL = cfg.OpenAI.BaseURL
		}

		embedder, err := knowledge.NewEmbedder(context.Background(), &cfg.Knowledge, &cfg.OpenAI, log.Logger)
		if err != nil {
			return nil, fmt.Errorf("knowledge base embedder initialization failed: %w", err)
		}

		// 创建检索器
		retrievalConfig := &knowledge.RetrievalConfig{
			TopK:                cfg.Knowledge.Retrieval.TopK,
			SimilarityThreshold: cfg.Knowledge.Retrieval.SimilarityThreshold,
			SubIndexFilter:      cfg.Knowledge.Retrieval.SubIndexFilter,
			PostRetrieve:        cfg.Knowledge.Retrieval.PostRetrieve,
		}
		knowledgeRetriever = knowledge.NewRetriever(knowledgeDB, embedder, retrievalConfig, log.Logger)

		// 创建索引器（Eino Compose 链）
		knowledgeIndexer, err = knowledge.NewIndexer(context.Background(), knowledgeDB, embedder, log.Logger, &cfg.Knowledge)
		if err != nil {
			return nil, fmt.Errorf("knowledge base indexer initialization failed: %w", err)
		}

		// 注册知识检索工具到MCP服务器
		knowledge.RegisterKnowledgeTool(mcpServer, knowledgeRetriever, knowledgeManager, log.Logger)

		// 创建知识库API处理器
		knowledgeHandler = handler.NewKnowledgeHandler(knowledgeManager, knowledgeRetriever, knowledgeIndexer, db, log.Logger)
		knowledgeHandler.SetAudit(auditSvc)
		log.Logger.Info("Knowledge base module initialized", zap.Bool("handler_created", knowledgeHandler != nil))

		// 扫描知识库并建立索引（异步）
		go func() {
			itemsToIndex, err := knowledgeManager.ScanKnowledgeBase()
			if err != nil {
				log.Logger.Warn("Knowledge base scan failed", zap.Error(err))
				return
			}

			// 检查是否已有索引
			hasIndex, err := knowledgeIndexer.HasIndex()
			if err != nil {
				log.Logger.Warn("Index status check failed", zap.Error(err))
				return
			}

			if hasIndex {
				// 如果已有索引，只索引新添加或更新的项
				if len(itemsToIndex) > 0 {
					log.Logger.Info("Existing knowledge base index detected, starting incremental indexing", zap.Int("count", len(itemsToIndex)))
					ctx := context.Background()
					consecutiveFailures := 0
					var firstFailureItemID string
					var firstFailureError error
					failedCount := 0

					for _, itemID := range itemsToIndex {
						if err := knowledgeIndexer.IndexItem(ctx, itemID); err != nil {
							failedCount++
							consecutiveFailures++

							if consecutiveFailures == 1 {
								firstFailureItemID = itemID
								firstFailureError = err
								log.Logger.Warn("Knowledge item indexing failed", zap.String("itemId", itemID), zap.Error(err))
							}

							// 如果连续失败2次，立即停止增量索引
							if consecutiveFailures >= 2 {
								log.Logger.Error("Too many consecutive indexing failures, stopping incremental indexing immediately",
									zap.Int("consecutiveFailures", consecutiveFailures),
									zap.Int("totalItems", len(itemsToIndex)),
									zap.String("firstFailureItemId", firstFailureItemID),
									zap.Error(firstFailureError),
								)
								break
							}
							continue
						}

						// 成功时重置连续失败计数
						if consecutiveFailures > 0 {
							consecutiveFailures = 0
							firstFailureItemID = ""
							firstFailureError = nil
						}
					}
					log.Logger.Info("Incremental indexing complete", zap.Int("totalItems", len(itemsToIndex)), zap.Int("failedCount", failedCount))
				} else {
					log.Logger.Info("Existing knowledge base index detected, no new or updated items to index")
				}
				return
			}

			// 只有在没有索引时才自动重建
			log.Logger.Info("No knowledge base index detected, starting automatic index build")
			ctx := context.Background()
			if err := knowledgeIndexer.RebuildIndex(ctx); err != nil {
				log.Logger.Warn("Knowledge base index rebuild failed", zap.Error(err))
			}
		}()
	}

	// 配置文件路径必须由入口传入（与 flag -config 一致）。勿再用 os.Args[1]，否则 ./cyberstrike-ai --https 会把 --https 当成路径。
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		configPath = "config.yaml"
	}

	skillsDir := skillpackage.SkillsRootFromConfig(cfg.SkillsDir, configPath)
	log.Logger.Info("Skills directory (Eino ADK skill middleware + Web management API)", zap.String("skillsDir", skillsDir))
	configDir := filepath.Dir(configPath)
	agent.SetPromptBaseDir(configDir)

	agentsDir := cfg.AgentsDir
	if agentsDir == "" {
		agentsDir = "agents"
	}
	if !filepath.IsAbs(agentsDir) {
		agentsDir = filepath.Join(configDir, agentsDir)
	}
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		log.Logger.Warn("Failed to create agents directory", zap.String("path", agentsDir), zap.Error(err))
	}
	markdownAgentsHandler := handler.NewMarkdownAgentsHandler(agentsDir)
	markdownAgentsHandler.SetAudit(auditSvc)
	log.Logger.Info("Multi-agent Markdown sub-agent directory", zap.String("agentsDir", agentsDir))

	// 创建处理器
	agentHandler := handler.NewAgentHandler(agent, db, cfg, log.Logger)
	agentHandler.SetAudit(auditSvc)
	agentHandler.SetAgentsMarkdownDir(agentsDir)
	// 如果知识库已启用，设置知识库管理器到AgentHandler以便记录检索日志
	if knowledgeManager != nil {
		agentHandler.SetKnowledgeManager(knowledgeManager)
	}
	monitorHandler := handler.NewMonitorHandler(mcpServer, executor, db, log.Logger)
	monitorHandler.SetAudit(auditSvc)
	monitorHandler.SetExternalMCPManager(externalMCPMgr) // 设置外部MCP管理器，以便获取外部MCP执行记录
	notificationHandler := handler.NewNotificationHandler(db, agentHandler, log.Logger)
	groupHandler := handler.NewGroupHandler(db, log.Logger)
	authHandler := handler.NewAuthHandler(authManager, cfg, configPath, log.Logger)
	authHandler.SetAudit(auditSvc)
	attackChainHandler := handler.NewAttackChainHandler(db, &cfg.OpenAI, log.Logger)
	vulnerabilityHandler := handler.NewVulnerabilityHandler(db, log.Logger)
	vulnerabilityHandler.SetAudit(auditSvc)
	webshellHandler := handler.NewWebShellHandler(log.Logger, db)
	webshellHandler.SetAudit(auditSvc)
	chatUploadsHandler := handler.NewChatUploadsHandler(log.Logger)
	chatUploadsHandler.SetAudit(auditSvc)
	registerWebshellTools(mcpServer, db, webshellHandler, log.Logger)
	registerWebshellManagementTools(mcpServer, db, webshellHandler, log.Logger)
	configHandler := handler.NewConfigHandler(configPath, cfg, mcpServer, executor, agent, attackChainHandler, externalMCPMgr, log.Logger)
	configHandler.SetAudit(auditSvc)
	agentHandler.SetHitlToolWhitelistSaver(configHandler)
	externalMCPHandler := handler.NewExternalMCPHandler(externalMCPMgr, cfg, configPath, log.Logger)
	externalMCPHandler.SetAudit(auditSvc)
	roleHandler := handler.NewRoleHandler(cfg, configPath, log.Logger)
	roleHandler.SetAudit(auditSvc)
	skillsHandler := handler.NewSkillsHandler(cfg, configPath, log.Logger)
	skillsHandler.SetAudit(auditSvc)
	fofaHandler := handler.NewFofaHandler(cfg, log.Logger)
	terminalHandler := handler.NewTerminalHandler(log.Logger)
	if db != nil {
		skillsHandler.SetDB(db) // 设置数据库连接以便获取调用统计
	}

	// ============================================================================
	// 初始化 C2 模块（可按配置关闭，节省本机部署资源）
	// ============================================================================
	c2Manager, c2Watchdog, watchdogCancel := setupC2Runtime(cfg, db, agentHandler, log.Logger)
	if c2Manager != nil {
		registerC2Tools(mcpServer, c2Manager, log.Logger, cfg.Server.Port)
	}
	c2Handler := handler.NewC2Handler(c2Manager, log.Logger)
	c2Handler.SetAudit(auditSvc)

	// 创建OpenAPI处理器
	conversationHandler := handler.NewConversationHandler(db, log.Logger)
	conversationHandler.SetAudit(auditSvc)
	auditHandler := handler.NewAuditHandler(db, auditSvc, log.Logger)
	robotHandler := handler.NewRobotHandler(cfg, db, agentHandler, log.Logger)
	openAPIHandler := handler.NewOpenAPIHandler(db, log.Logger, resultStorage, conversationHandler, agentHandler)

	// 创建 App 实例（部分字段稍后填充）
	app := &App{
		config:             cfg,
		logger:             log,
		router:             router,
		mcpServer:          mcpServer,
		externalMCPMgr:     externalMCPMgr,
		agent:              agent,
		executor:           executor,
		db:                 db,
		knowledgeDB:        knowledgeDBConn,
		auth:               authManager,
		knowledgeManager:   knowledgeManager,
		knowledgeRetriever: knowledgeRetriever,
		knowledgeIndexer:   knowledgeIndexer,
		knowledgeHandler:   knowledgeHandler,
		agentHandler:       agentHandler,
		robotHandler:       robotHandler,
		c2Manager:          c2Manager,
		c2Watchdog:         c2Watchdog,
		c2WatchdogCancel:   watchdogCancel,
		c2Handler:          c2Handler,
		auditSvc:           auditSvc,
	}
	// 飞书/钉钉长连接（无需公网），启用时在后台启动；后续前端应用配置时会通过 RestartRobotConnections 重启
	app.startRobotConnections()

	// 设置漏洞工具注册器（内置工具，必须设置）
	vulnerabilityRegistrar := func() error {
		registerVulnerabilityTool(mcpServer, db, log.Logger)
		return nil
	}
	configHandler.SetVulnerabilityToolRegistrar(vulnerabilityRegistrar)

	// 设置 WebShell 工具注册器（ApplyConfig 时重新注册）
	webshellRegistrar := func() error {
		registerWebshellTools(mcpServer, db, webshellHandler, log.Logger)
		registerWebshellManagementTools(mcpServer, db, webshellHandler, log.Logger)
		return nil
	}
	configHandler.SetWebshellToolRegistrar(webshellRegistrar)

	// Skills 由 Eino ADK skill 中间件提供（多代理）；此处不注册 MCP 形态的技能工具
	configHandler.SetSkillsToolRegistrar(func() error { return nil })

	handler.RegisterBatchTaskMCPTools(mcpServer, agentHandler, log.Logger)
	batchTaskToolRegistrar := func() error {
		handler.RegisterBatchTaskMCPTools(mcpServer, agentHandler, log.Logger)
		return nil
	}
	configHandler.SetBatchTaskToolRegistrar(batchTaskToolRegistrar)

	// 设置知识库初始化器（用于动态初始化，需要在 App 创建后设置）
	configHandler.SetKnowledgeInitializer(func() (*handler.KnowledgeHandler, error) {
		knowledgeHandler, err := initializeKnowledge(cfg, db, knowledgeDBConn, mcpServer, agentHandler, app, log.Logger)
		if err != nil {
			return nil, err
		}

		// 动态初始化后，设置知识库工具注册器和检索器更新器
		// 这样后续 ApplyConfig 时就能重新注册工具了
		if app.knowledgeRetriever != nil && app.knowledgeManager != nil {
			// 创建闭包，捕获knowledgeRetriever和knowledgeManager的引用
			registrar := func() error {
				knowledge.RegisterKnowledgeTool(mcpServer, app.knowledgeRetriever, app.knowledgeManager, log.Logger)
				return nil
			}
			configHandler.SetKnowledgeToolRegistrar(registrar)
			// 设置检索器更新器，以便在ApplyConfig时更新检索器配置
			configHandler.SetRetrieverUpdater(app.knowledgeRetriever)
			log.Logger.Info("Knowledge base tool registrar and retriever updater set after dynamic init")
		}

		return knowledgeHandler, nil
	})

	// 如果知识库已启用，设置知识库工具注册器和检索器更新器
	if cfg.Knowledge.Enabled && knowledgeRetriever != nil && knowledgeManager != nil {
		// 创建闭包，捕获knowledgeRetriever和knowledgeManager的引用
		registrar := func() error {
			knowledge.RegisterKnowledgeTool(mcpServer, knowledgeRetriever, knowledgeManager, log.Logger)
			return nil
		}
		configHandler.SetKnowledgeToolRegistrar(registrar)
		// 设置检索器更新器，以便在ApplyConfig时更新检索器配置
		configHandler.SetRetrieverUpdater(knowledgeRetriever)
	}

	// 设置机器人连接重启器，前端应用配置后无需重启服务即可使钉钉/飞书/微信新配置生效
	configHandler.SetRobotRestarter(app)

	wechatRobotHandler := handler.NewWechatRobotHandler(cfg, configHandler, log.Logger)

	configHandler.SetC2Runtime(app)
	configHandler.SetC2ToolRegistrar(func() error {
		if app.config.C2.EnabledEffective() && app.c2Manager != nil {
			registerC2Tools(mcpServer, app.c2Manager, log.Logger, app.config.Server.Port)
		}
		return nil
	})

	// 设置路由（使用 App 实例以便动态获取 handler）
	setupRoutes(
		router,
		authHandler,
		agentHandler,
		monitorHandler,
		notificationHandler,
		conversationHandler,
		robotHandler,
		wechatRobotHandler,
		groupHandler,
		configHandler,
		externalMCPHandler,
		attackChainHandler,
		app, // 传递 App 实例以便动态获取 knowledgeHandler
		vulnerabilityHandler,
		webshellHandler,
		chatUploadsHandler,
		roleHandler,
		skillsHandler,
		markdownAgentsHandler,
		fofaHandler,
		terminalHandler,
		app.c2Handler,
		auditHandler,
		mcpServer,
		authManager,
		openAPIHandler,
	)

	return app, nil

}

// mcpHandlerWithAuth 在鉴权通过后转发到 MCP 处理；若配置了 auth_header 则校验请求头，否则直接放行
func (a *App) mcpHandlerWithAuth(w http.ResponseWriter, r *http.Request) {
	cfg := a.config.MCP
	if cfg.AuthHeader != "" {
		actual := []byte(r.Header.Get(cfg.AuthHeader))
		expected := []byte(cfg.AuthHeaderValue)
		if subtle.ConstantTimeCompare(actual, expected) != 1 {
			a.logger.Logger.Debug("MCP auth failed: header missing or value mismatch", zap.String("header", cfg.AuthHeader))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
	}
	a.mcpServer.HandleHTTP(w, r)
}

// Run 启动应用（向后兼容，不支持优雅关闭）
func (a *App) Run() error {
	return a.RunWithContext(context.Background())
}

// RunWithContext 启动应用，支持通过 context 取消来优雅关闭
func (a *App) RunWithContext(ctx context.Context) error {
	// 启动MCP服务器（如果启用）
	var mcpServer *http.Server
	if a.config.MCP.Enabled {
		mcpAddr := fmt.Sprintf("%s:%d", a.config.MCP.Host, a.config.MCP.Port)
		a.logger.Info("Starting MCP server", zap.String("address", mcpAddr))

		mux := http.NewServeMux()
		mux.HandleFunc("/mcp", a.mcpHandlerWithAuth)

		mcpServer = &http.Server{Addr: mcpAddr, Handler: mux}
		go func() {
			if err := mcpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				a.logger.Error("MCP server start failed", zap.Error(err))
			}
		}()
	}

	// 启动主服务器（可选 HTTPS + HTTP/2，见 config server.tls_*）
	addr := fmt.Sprintf("%s:%d", a.config.Server.Host, a.config.Server.Port)
	tlsMode, tlsConf, certFile, keyFile, tlsErr := prepareMainServerTLS(&a.config.Server)
	if tlsErr != nil {
		return tlsErr
	}

	srv := &http.Server{Addr: addr, Handler: a.router}
	var mainMux *mainServerMux
	httpRedirect := config.ServerHTTPRedirectEnabled(&a.config.Server)
	if tlsMode != mainTLSOff {
		srv.TLSConfig = tlsConf
		if err := http2.ConfigureServer(srv, &http2.Server{}); err != nil {
			return fmt.Errorf("main server HTTP/2 config failed: %w", err)
		}
		switch tlsMode {
		case mainTLSFromFiles:
			a.logger.Info("Starting HTTPS main service (HTTP/2 negotiation enabled)",
				zap.String("address", addr),
				zap.String("cert", certFile),
			)
		case mainTLSInMemorySelfSigned:
			a.logger.Info("Starting HTTPS main service (in-memory self-signed cert, testing only; HTTP/2 negotiation enabled)",
				zap.String("address", addr),
			)
		}
		if httpRedirect {
			a.logger.Info("HTTP→HTTPS auto-redirect enabled (same-port sniffing)", zap.String("address", addr))
		}
	} else {
		a.logger.Info("Starting HTTP main service", zap.String("address", addr))
	}

	// 监听 context 取消，优雅关闭 HTTP 服务器
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if mainMux != nil {
			if err := mainMux.Shutdown(shutdownCtx); err != nil {
				a.logger.Error("HTTP/HTTPS split server shutdown failed", zap.Error(err))
			}
		} else if err := srv.Shutdown(shutdownCtx); err != nil {
			a.logger.Error("HTTP server shutdown failed", zap.Error(err))
		}
		if mcpServer != nil {
			if err := mcpServer.Shutdown(shutdownCtx); err != nil {
				a.logger.Error("MCP server shutdown failed", zap.Error(err))
			}
		}
	}()

	var err error
	switch {
	case tlsMode != mainTLSOff && httpRedirect:
		var tlsConfReady *tls.Config
		tlsConfReady, err = ensureMainTLSConfigCerts(tlsMode, tlsConf, certFile, keyFile)
		if err != nil {
			return fmt.Errorf("TLS certificate load: %w", err)
		}
		srv.TLSConfig = tlsConfReady
		var ln net.Listener
		ln, err = net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		mainMux = newMainServerMux(ln, srv, portFromListenAddr(addr), a.logger.Logger)
		err = mainMux.Serve()
	case tlsMode == mainTLSOff:
		err = srv.ListenAndServe()
	case tlsMode == mainTLSFromFiles:
		err = srv.ListenAndServeTLS(certFile, keyFile)
	case tlsMode == mainTLSInMemorySelfSigned:
		var ln net.Listener
		ln, err = tls.Listen("tcp", addr, srv.TLSConfig)
		if err == nil {
			err = srv.Serve(ln)
		}
	default:
		err = srv.ListenAndServe()
	}
	if err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Shutdown 关闭应用
func (a *App) Shutdown() {
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = einoobserve.ShutdownOtel(shutdownCtx)
	shutdownCancel()

	// 停止钉钉/飞书长连接
	a.robotMu.Lock()
	if a.dingCancel != nil {
		a.dingCancel()
		a.dingCancel = nil
	}
	if a.larkCancel != nil {
		a.larkCancel()
		a.larkCancel = nil
	}
	a.robotMu.Unlock()

	a.shutdownC2()

	// 停止所有外部MCP客户端
	if a.externalMCPMgr != nil {
		a.externalMCPMgr.StopAll()
	}

	// 关闭知识库数据库连接（如果使用独立数据库）
	if a.knowledgeDB != nil {
		if err := a.knowledgeDB.Close(); err != nil {
			a.logger.Logger.Warn("Failed to close knowledge base database connection", zap.Error(err))
		}
	}

	// 关闭主数据库连接
	if a.db != nil {
		if err := a.db.Close(); err != nil {
			a.logger.Logger.Warn("Failed to close main database connection", zap.Error(err))
		}
	}
}

// startRobotConnections 根据当前配置启动钉钉/飞书长连接（不先关闭已有连接，仅用于首次启动）
func (a *App) startRobotConnections() {
	a.robotMu.Lock()
	defer a.robotMu.Unlock()
	cfg := a.config
	if cfg.Robots.Lark.Enabled && cfg.Robots.Lark.AppID != "" && cfg.Robots.Lark.AppSecret != "" {
		ctx, cancel := context.WithCancel(context.Background())
		a.larkCancel = cancel
		go robot.StartLark(ctx, cfg.Robots, a.robotHandler, a.logger.Logger)
	}
	if cfg.Robots.Dingtalk.Enabled && cfg.Robots.Dingtalk.ClientID != "" && cfg.Robots.Dingtalk.ClientSecret != "" {
		ctx, cancel := context.WithCancel(context.Background())
		a.dingCancel = cancel
		go robot.StartDing(ctx, cfg.Robots, a.robotHandler, a.logger.Logger)
	}
	if cfg.Robots.Wechat.Enabled && cfg.Robots.Wechat.BotToken != "" {
		ctx, cancel := context.WithCancel(context.Background())
		a.wechatCancel = cancel
		go robot.StartWechat(ctx, cfg.Robots, a.robotHandler, cfg.Version, a.logger.Logger)
	}
}

// RestartRobotConnections 重启钉钉/飞书/微信长连接，使前端应用配置后立即生效（实现 handler.RobotRestarter）
func (a *App) RestartRobotConnections() {
	a.robotMu.Lock()
	if a.dingCancel != nil {
		a.dingCancel()
		a.dingCancel = nil
	}
	if a.larkCancel != nil {
		a.larkCancel()
		a.larkCancel = nil
	}
	if a.wechatCancel != nil {
		a.wechatCancel()
		a.wechatCancel = nil
	}
	a.robotMu.Unlock()
	// 给旧 goroutine 一点时间退出
	time.Sleep(200 * time.Millisecond)
	a.startRobotConnections()
}

// setupRoutes 设置路由
func setupRoutes(
	router *gin.Engine,
	authHandler *handler.AuthHandler,
	agentHandler *handler.AgentHandler,
	monitorHandler *handler.MonitorHandler,
	notificationHandler *handler.NotificationHandler,
	conversationHandler *handler.ConversationHandler,
	robotHandler *handler.RobotHandler,
	wechatRobotHandler *handler.WechatRobotHandler,
	groupHandler *handler.GroupHandler,
	configHandler *handler.ConfigHandler,
	externalMCPHandler *handler.ExternalMCPHandler,
	attackChainHandler *handler.AttackChainHandler,
	app *App, // 传递 App 实例以便动态获取 knowledgeHandler
	vulnerabilityHandler *handler.VulnerabilityHandler,
	webshellHandler *handler.WebShellHandler,
	chatUploadsHandler *handler.ChatUploadsHandler,
	roleHandler *handler.RoleHandler,
	skillsHandler *handler.SkillsHandler,
	markdownAgentsHandler *handler.MarkdownAgentsHandler,
	fofaHandler *handler.FofaHandler,
	terminalHandler *handler.TerminalHandler,
	c2Handler *handler.C2Handler,
	auditHandler *handler.AuditHandler,
	mcpServer *mcp.Server,
	authManager *security.AuthManager,
	openAPIHandler *handler.OpenAPIHandler,
) {
	// API路由
	api := router.Group("/api")

	// 认证相关路由
	authRoutes := api.Group("/auth")
	{
		authRoutes.POST("/login", authHandler.Login)
		authRoutes.POST("/logout", security.AuthMiddleware(authManager), authHandler.Logout)
		authRoutes.POST("/change-password", security.AuthMiddleware(authManager), authHandler.ChangePassword)
		authRoutes.GET("/validate", security.AuthMiddleware(authManager), authHandler.Validate)
	}

	// 机器人回调（无需登录，供企业微信/钉钉/飞书服务器调用）
	// 添加速率限制：每个 IP 每分钟最多 60 次请求，防止滥用
	robotRL := security.NewRateLimiter(60, 1*time.Minute)
	robotGroup := api.Group("/robot")
	robotGroup.Use(security.RateLimitMiddleware(robotRL))
	{
		robotGroup.GET("/wecom", robotHandler.HandleWecomGET)
		robotGroup.POST("/wecom", robotHandler.HandleWecomPOST)
		robotGroup.POST("/dingtalk", robotHandler.HandleDingtalkPOST)
		robotGroup.POST("/lark", robotHandler.HandleLarkPOST)
	}

	protected := api.Group("")
	protected.Use(security.AuthMiddleware(authManager))
	{
		// 机器人测试（需登录）：POST /api/robot/test，body: {"platform":"dingtalk","user_id":"test","text":"帮助"}，用于验证机器人逻辑
		protected.POST("/robot/test", robotHandler.HandleRobotTest)

		// 微信 iLink 扫码绑定（需登录）
		protected.POST("/robot/wechat/qrcode", wechatRobotHandler.HandleWechatQRCode)
		protected.GET("/robot/wechat/qrcode/status", wechatRobotHandler.HandleWechatQRCodeStatus)
		protected.POST("/robot/wechat/qrcode/verify", wechatRobotHandler.HandleWechatVerifyCode)
		protected.GET("/robot/wechat/status", wechatRobotHandler.HandleWechatStatus)

		// Agent Loop
		protected.POST("/agent-loop", agentHandler.AgentLoop)
		// Agent Loop 流式输出
		protected.POST("/agent-loop/stream", agentHandler.AgentLoopStream)
		// Eino ADK 单代理（ChatModelAgent + Runner；不依赖 multi_agent.enabled）
		protected.POST("/eino-agent", agentHandler.EinoSingleAgentLoop)
		protected.POST("/eino-agent/stream", agentHandler.EinoSingleAgentLoopStream)
		protected.GET("/hitl/pending", agentHandler.ListHITLPending)
		protected.POST("/hitl/decision", agentHandler.DecideHITLInterrupt)
		protected.POST("/hitl/dismiss", agentHandler.DismissHITLInterrupt)
		protected.GET("/hitl/config/:conversationId", agentHandler.GetHITLConversationConfig)
		protected.PUT("/hitl/config", agentHandler.UpsertHITLConversationConfig)
		protected.POST("/hitl/tool-whitelist", agentHandler.MergeHITLGlobalToolWhitelist)
		// Agent Loop 取消与任务列表
		protected.POST("/agent-loop/cancel", agentHandler.CancelAgentLoop)
		protected.GET("/agent-loop/tasks", agentHandler.ListAgentTasks)
		protected.GET("/agent-loop/task-events", agentHandler.SubscribeAgentTaskEvents)
		protected.GET("/agent-loop/tasks/completed", agentHandler.ListCompletedTasks)

		// Eino DeepAgent 多代理（与单 Agent 并存，需 config.multi_agent.enabled）
		// 多代理路由常注册；是否可用由运行时 h.config.MultiAgent.Enabled 决定（应用配置后无需重启）
		protected.POST("/multi-agent", agentHandler.MultiAgentLoop)
		protected.POST("/multi-agent/stream", agentHandler.MultiAgentLoopStream)
		protected.GET("/multi-agent/markdown-agents", markdownAgentsHandler.ListMarkdownAgents)
		protected.GET("/multi-agent/markdown-agents/:filename", markdownAgentsHandler.GetMarkdownAgent)
		protected.POST("/multi-agent/markdown-agents", markdownAgentsHandler.CreateMarkdownAgent)
		protected.PUT("/multi-agent/markdown-agents/:filename", markdownAgentsHandler.UpdateMarkdownAgent)
		protected.DELETE("/multi-agent/markdown-agents/:filename", markdownAgentsHandler.DeleteMarkdownAgent)

		// 信息收集 - FOFA 查询（后端代理）
		protected.POST("/fofa/search", fofaHandler.Search)
		// 信息收集 - 自然语言解析为 FOFA 语法（需人工确认后再查询）
		protected.POST("/fofa/parse", fofaHandler.ParseNaturalLanguage)

		// 批量任务管理
		protected.POST("/batch-tasks", agentHandler.CreateBatchQueue)
		protected.GET("/batch-tasks", agentHandler.ListBatchQueues)
		protected.GET("/batch-tasks/:queueId", agentHandler.GetBatchQueue)
		protected.POST("/batch-tasks/:queueId/start", agentHandler.StartBatchQueue)
		protected.POST("/batch-tasks/:queueId/rerun", agentHandler.RerunBatchQueue)
		protected.POST("/batch-tasks/:queueId/pause", agentHandler.PauseBatchQueue)
		protected.PUT("/batch-tasks/:queueId/metadata", agentHandler.UpdateBatchQueueMetadata)
		protected.PUT("/batch-tasks/:queueId/schedule", agentHandler.UpdateBatchQueueSchedule)
		protected.PUT("/batch-tasks/:queueId/schedule-enabled", agentHandler.SetBatchQueueScheduleEnabled)
		protected.DELETE("/batch-tasks/:queueId", agentHandler.DeleteBatchQueue)
		protected.PUT("/batch-tasks/:queueId/tasks/:taskId", agentHandler.UpdateBatchTask)
		protected.POST("/batch-tasks/:queueId/tasks", agentHandler.AddBatchTask)
		protected.DELETE("/batch-tasks/:queueId/tasks/:taskId", agentHandler.DeleteBatchTask)

		// 对话历史
		protected.POST("/conversations", conversationHandler.CreateConversation)
		protected.GET("/conversations", conversationHandler.ListConversations)
		protected.GET("/conversations/:id", conversationHandler.GetConversation)
		protected.GET("/messages/:id/process-details", conversationHandler.GetMessageProcessDetails)
		protected.PUT("/conversations/:id", conversationHandler.UpdateConversation)
		protected.DELETE("/conversations/:id", conversationHandler.DeleteConversation)
		protected.POST("/conversations/:id/delete-turn", conversationHandler.DeleteConversationTurn)
		protected.PUT("/conversations/:id/pinned", groupHandler.UpdateConversationPinned)

		// 对话分组
		protected.POST("/groups", groupHandler.CreateGroup)
		protected.GET("/groups", groupHandler.ListGroups)
		protected.GET("/groups/:id", groupHandler.GetGroup)
		protected.PUT("/groups/:id", groupHandler.UpdateGroup)
		protected.DELETE("/groups/:id", groupHandler.DeleteGroup)
		protected.PUT("/groups/:id/pinned", groupHandler.UpdateGroupPinned)
		protected.GET("/groups/:id/conversations", groupHandler.GetGroupConversations)
		protected.GET("/groups/mappings", groupHandler.GetAllMappings)
		protected.POST("/groups/conversations", groupHandler.AddConversationToGroup)
		protected.DELETE("/groups/:id/conversations/:conversationId", groupHandler.RemoveConversationFromGroup)
		protected.PUT("/groups/:id/conversations/:conversationId/pinned", groupHandler.UpdateConversationPinnedInGroup)

		// 监控
		protected.GET("/monitor", monitorHandler.Monitor)
		protected.GET("/monitor/execution/:id", monitorHandler.GetExecution)
		protected.POST("/monitor/execution/:id/cancel", monitorHandler.CancelExecution)
		protected.POST("/monitor/executions/names", monitorHandler.BatchGetToolNames)
		protected.DELETE("/monitor/execution/:id", monitorHandler.DeleteExecution)
		protected.DELETE("/monitor/executions", monitorHandler.DeleteExecutions)
		protected.GET("/monitor/stats", monitorHandler.GetStats)
		protected.GET("/notifications/summary", notificationHandler.GetSummary)
		protected.POST("/notifications/read", notificationHandler.MarkRead)

		// 配置管理
		protected.GET("/config", configHandler.GetConfig)
		protected.GET("/config/tools", configHandler.GetTools)
		protected.GET("/config/tools/:name/schema", configHandler.GetToolSchema)
		protected.PUT("/config", configHandler.UpdateConfig)
		protected.POST("/config/apply", configHandler.ApplyConfig)
		protected.POST("/config/test-openai", configHandler.TestOpenAI)

		// 系统设置 - 终端（执行命令，提高运维效率）
		protected.POST("/terminal/run", terminalHandler.RunCommand)
		protected.POST("/terminal/run/stream", terminalHandler.RunCommandStream)
		protected.GET("/terminal/ws", terminalHandler.RunCommandWS)

		// 平台审计日志
		protected.GET("/audit/meta", auditHandler.Meta)
		protected.GET("/audit/summary", auditHandler.Summary)
		protected.GET("/audit/logs", auditHandler.ListLogs)
		protected.GET("/audit/logs/export", auditHandler.ExportLogs)
		protected.GET("/audit/logs/:id", auditHandler.GetLog)

		// 外部MCP管理
		protected.GET("/external-mcp", externalMCPHandler.GetExternalMCPs)
		protected.GET("/external-mcp/stats", externalMCPHandler.GetExternalMCPStats)
		protected.GET("/external-mcp/:name", externalMCPHandler.GetExternalMCP)
		protected.PUT("/external-mcp/:name", externalMCPHandler.AddOrUpdateExternalMCP)
		protected.DELETE("/external-mcp/:name", externalMCPHandler.DeleteExternalMCP)
		protected.POST("/external-mcp/:name/start", externalMCPHandler.StartExternalMCP)
		protected.POST("/external-mcp/:name/stop", externalMCPHandler.StopExternalMCP)

		// 攻击链可视化
		protected.GET("/attack-chain/:conversationId", attackChainHandler.GetAttackChain)
		protected.POST("/attack-chain/:conversationId/regenerate", attackChainHandler.RegenerateAttackChain)

		// 知识库管理（始终注册路由，通过 App 实例动态获取 handler）
		knowledgeRoutes := protected.Group("/knowledge")
		{
			knowledgeRoutes.GET("/categories", func(c *gin.Context) {
				if app.knowledgeHandler == nil {
					c.JSON(http.StatusOK, gin.H{
						"categories": []string{},
						"enabled":    false,
						"message":    "Knowledge base feature is not enabled. Go to system settings to enable knowledge retrieval.",
					})
					return
				}
				app.knowledgeHandler.GetCategories(c)
			})
			knowledgeRoutes.GET("/items", func(c *gin.Context) {
				if app.knowledgeHandler == nil {
					c.JSON(http.StatusOK, gin.H{
						"items":   []interface{}{},
						"enabled": false,
						"message": "Knowledge base feature is not enabled. Go to system settings to enable knowledge retrieval.",
					})
					return
				}
				app.knowledgeHandler.GetItems(c)
			})
			knowledgeRoutes.GET("/items/:id", func(c *gin.Context) {
				if app.knowledgeHandler == nil {
					c.JSON(http.StatusOK, gin.H{
						"enabled": false,
						"message": "Knowledge base feature is not enabled. Go to system settings to enable knowledge retrieval.",
					})
					return
				}
				app.knowledgeHandler.GetItem(c)
			})
			knowledgeRoutes.POST("/items", func(c *gin.Context) {
				if app.knowledgeHandler == nil {
					c.JSON(http.StatusOK, gin.H{
						"enabled": false,
						"error":   "Knowledge base feature is not enabled. Go to system settings to enable knowledge retrieval.",
					})
					return
				}
				app.knowledgeHandler.CreateItem(c)
			})
			knowledgeRoutes.PUT("/items/:id", func(c *gin.Context) {
				if app.knowledgeHandler == nil {
					c.JSON(http.StatusOK, gin.H{
						"enabled": false,
						"error":   "Knowledge base feature is not enabled. Go to system settings to enable knowledge retrieval.",
					})
					return
				}
				app.knowledgeHandler.UpdateItem(c)
			})
			knowledgeRoutes.DELETE("/items/:id", func(c *gin.Context) {
				if app.knowledgeHandler == nil {
					c.JSON(http.StatusOK, gin.H{
						"enabled": false,
						"error":   "Knowledge base feature is not enabled. Go to system settings to enable knowledge retrieval.",
					})
					return
				}
				app.knowledgeHandler.DeleteItem(c)
			})
			knowledgeRoutes.GET("/index-status", func(c *gin.Context) {
				if app.knowledgeHandler == nil {
					c.JSON(http.StatusOK, gin.H{
						"enabled":          false,
						"total_items":      0,
						"indexed_items":    0,
						"progress_percent": 0,
						"is_complete":      false,
						"message":          "Knowledge base feature is not enabled. Go to system settings to enable knowledge retrieval.",
					})
					return
				}
				app.knowledgeHandler.GetIndexStatus(c)
			})
			knowledgeRoutes.POST("/index", func(c *gin.Context) {
				if app.knowledgeHandler == nil {
					c.JSON(http.StatusOK, gin.H{
						"enabled": false,
						"error":   "Knowledge base feature is not enabled. Go to system settings to enable knowledge retrieval.",
					})
					return
				}
				app.knowledgeHandler.RebuildIndex(c)
			})
			knowledgeRoutes.POST("/scan", func(c *gin.Context) {
				if app.knowledgeHandler == nil {
					c.JSON(http.StatusOK, gin.H{
						"enabled": false,
						"error":   "Knowledge base feature is not enabled. Go to system settings to enable knowledge retrieval.",
					})
					return
				}
				app.knowledgeHandler.ScanKnowledgeBase(c)
			})
			knowledgeRoutes.GET("/retrieval-logs", func(c *gin.Context) {
				if app.knowledgeHandler == nil {
					c.JSON(http.StatusOK, gin.H{
						"logs":    []interface{}{},
						"enabled": false,
						"message": "Knowledge base feature is not enabled. Go to system settings to enable knowledge retrieval.",
					})
					return
				}
				app.knowledgeHandler.GetRetrievalLogs(c)
			})
			knowledgeRoutes.DELETE("/retrieval-logs/:id", func(c *gin.Context) {
				if app.knowledgeHandler == nil {
					c.JSON(http.StatusOK, gin.H{
						"enabled": false,
						"error":   "Knowledge base feature is not enabled. Go to system settings to enable knowledge retrieval.",
					})
					return
				}
				app.knowledgeHandler.DeleteRetrievalLog(c)
			})
			knowledgeRoutes.POST("/search", func(c *gin.Context) {
				if app.knowledgeHandler == nil {
					c.JSON(http.StatusOK, gin.H{
						"results": []interface{}{},
						"enabled": false,
						"message": "Knowledge base feature is not enabled. Go to system settings to enable knowledge retrieval.",
					})
					return
				}
				app.knowledgeHandler.Search(c)
			})
			knowledgeRoutes.GET("/stats", func(c *gin.Context) {
				if app.knowledgeHandler == nil {
					c.JSON(http.StatusOK, gin.H{
						"enabled":          false,
						"total_categories": 0,
						"total_items":      0,
						"message":          "Knowledge base feature is not enabled. Go to system settings to enable knowledge retrieval.",
					})
					return
				}
				app.knowledgeHandler.GetStats(c)
			})
		}

		// 漏洞管理
		protected.GET("/vulnerabilities", vulnerabilityHandler.ListVulnerabilities)
		protected.GET("/vulnerabilities/export", vulnerabilityHandler.ExportVulnerabilities)
		protected.GET("/vulnerabilities/filter-options", vulnerabilityHandler.GetVulnerabilityFilterOptions)
		protected.GET("/vulnerabilities/stats", vulnerabilityHandler.GetVulnerabilityStats)
		protected.GET("/vulnerabilities/:id", vulnerabilityHandler.GetVulnerability)
		protected.POST("/vulnerabilities", vulnerabilityHandler.CreateVulnerability)
		protected.PUT("/vulnerabilities/:id", vulnerabilityHandler.UpdateVulnerability)
		protected.DELETE("/vulnerabilities/:id", vulnerabilityHandler.DeleteVulnerability)

		// WebShell 管理（代理执行 + 连接配置存 SQLite）
		protected.GET("/webshell/connections", webshellHandler.ListConnections)
		protected.POST("/webshell/connections", webshellHandler.CreateConnection)
		protected.GET("/webshell/connections/:id/ai-history", webshellHandler.GetAIHistory)
		protected.GET("/webshell/connections/:id/ai-conversations", webshellHandler.ListAIConversations)
		protected.GET("/webshell/connections/:id/state", webshellHandler.GetConnectionState)
		protected.PUT("/webshell/connections/:id", webshellHandler.UpdateConnection)
		protected.PUT("/webshell/connections/:id/state", webshellHandler.SaveConnectionState)
		protected.DELETE("/webshell/connections/:id", webshellHandler.DeleteConnection)
		protected.POST("/webshell/exec", webshellHandler.Exec)
		protected.POST("/webshell/file", webshellHandler.FileOp)

		// C2 管理（未启用时返回 503，避免 Handler 空指针）
		c2Routes := protected.Group("/c2")
		c2Routes.Use(func(c *gin.Context) {
			if app.c2Manager == nil {
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
					"error":   "c2_disabled",
					"message": "C2 feature is disabled in system settings",
					"enabled": false,
				})
				return
			}
			c.Next()
		})
		c2Routes.GET("/listeners", c2Handler.ListListeners)
		c2Routes.POST("/listeners", c2Handler.CreateListener)
		c2Routes.GET("/listeners/:id", c2Handler.GetListener)
		c2Routes.PUT("/listeners/:id", c2Handler.UpdateListener)
		c2Routes.DELETE("/listeners/:id", c2Handler.DeleteListener)
		c2Routes.POST("/listeners/:id/start", c2Handler.StartListener)
		c2Routes.POST("/listeners/:id/stop", c2Handler.StopListener)
		c2Routes.GET("/sessions", c2Handler.ListSessions)
		c2Routes.GET("/sessions/:id", c2Handler.GetSession)
		c2Routes.DELETE("/sessions/:id", c2Handler.DeleteSession)
		c2Routes.PUT("/sessions/:id/sleep", c2Handler.SetSessionSleep)
		c2Routes.GET("/tasks", c2Handler.ListTasks)
		c2Routes.DELETE("/tasks", c2Handler.DeleteTasks)
		c2Routes.GET("/tasks/:id", c2Handler.GetTask)
		c2Routes.POST("/tasks", c2Handler.CreateTask)
		c2Routes.POST("/tasks/:id/cancel", c2Handler.CancelTask)
		c2Routes.GET("/tasks/:id/wait", c2Handler.WaitTask)
		c2Routes.POST("/sessions/:id/tasks", c2Handler.CreateTask)
		c2Routes.POST("/payloads/oneliner", c2Handler.PayloadOneliner)
		c2Routes.POST("/payloads/build", c2Handler.PayloadBuild)
		c2Routes.GET("/payloads/:id/download", c2Handler.PayloadDownload)
		c2Routes.GET("/events", c2Handler.ListEvents)
		c2Routes.DELETE("/events", c2Handler.DeleteEvents)
		c2Routes.GET("/events/stream", c2Handler.EventStream)
		c2Routes.POST("/files/upload", c2Handler.UploadFileForImplant)
		c2Routes.GET("/files", c2Handler.ListFiles)
		c2Routes.GET("/tasks/:id/result-file", c2Handler.DownloadResultFile)
		c2Routes.GET("/profiles", c2Handler.ListProfiles)
		c2Routes.GET("/profiles/:id", c2Handler.GetProfile)
		c2Routes.POST("/profiles", c2Handler.CreateProfile)
		c2Routes.PUT("/profiles/:id", c2Handler.UpdateProfile)
		c2Routes.DELETE("/profiles/:id", c2Handler.DeleteProfile)

		// 对话附件（chat_uploads）管理
		protected.GET("/chat-uploads", chatUploadsHandler.List)
		protected.GET("/chat-uploads/download", chatUploadsHandler.Download)
		protected.GET("/chat-uploads/content", chatUploadsHandler.GetContent)
		protected.POST("/chat-uploads", chatUploadsHandler.Upload)
		protected.POST("/chat-uploads/mkdir", chatUploadsHandler.Mkdir)
		protected.DELETE("/chat-uploads", chatUploadsHandler.Delete)
		protected.PUT("/chat-uploads/rename", chatUploadsHandler.Rename)
		protected.PUT("/chat-uploads/content", chatUploadsHandler.PutContent)

		// 角色管理
		protected.GET("/roles", roleHandler.GetRoles)
		protected.GET("/roles/:name", roleHandler.GetRole)
		protected.POST("/roles", roleHandler.CreateRole)
		protected.PUT("/roles/:name", roleHandler.UpdateRole)
		protected.DELETE("/roles/:name", roleHandler.DeleteRole)

		// Skills管理（具体路径需注册在 /skills/:name 之前）
		protected.GET("/skills", skillsHandler.GetSkills)
		protected.GET("/skills/stats", skillsHandler.GetSkillStats)
		protected.DELETE("/skills/stats", skillsHandler.ClearSkillStats)
		protected.GET("/skills/:name/files", skillsHandler.ListSkillPackageFiles)
		protected.GET("/skills/:name/file", skillsHandler.GetSkillPackageFile)
		protected.PUT("/skills/:name/file", skillsHandler.PutSkillPackageFile)
		protected.GET("/skills/:name/bound-roles", skillsHandler.GetSkillBoundRoles)
		protected.POST("/skills", skillsHandler.CreateSkill)
		protected.PUT("/skills/:name", skillsHandler.UpdateSkill)
		protected.DELETE("/skills/:name", skillsHandler.DeleteSkill)
		protected.DELETE("/skills/:name/stats", skillsHandler.ClearSkillStatsByName)
		protected.GET("/skills/:name", skillsHandler.GetSkill)

		// MCP端点
		protected.POST("/mcp", func(c *gin.Context) {
			mcpServer.HandleHTTP(c.Writer, c.Request)
		})

		// OpenAPI结果聚合端点（可选，用于获取对话的完整结果）
		protected.GET("/conversations/:id/results", openAPIHandler.GetConversationResults)
	}

	// OpenAPI规范（需要认证，避免暴露API结构信息）
	protected.GET("/openapi/spec", openAPIHandler.GetOpenAPISpec)

	// API文档页面（公开访问，但需要登录后才能使用API）
	router.GET("/api-docs", func(c *gin.Context) {
		c.HTML(http.StatusOK, "api-docs.html", nil)
	})

	// 静态文件
	router.Static("/static", "./web/static")
	router.LoadHTMLGlob("web/templates/*")

	// 前端页面
	router.GET("/", func(c *gin.Context) {
		version := app.config.Version
		if version == "" {
			version = "v1.0.0"
		}
		c.HTML(http.StatusOK, "index.html", gin.H{"Version": version})
	})
}

// registerVulnerabilityTool 注册漏洞记录工具到MCP服务器
func registerVulnerabilityTool(mcpServer *mcp.Server, db *database.DB, logger *zap.Logger) {
	tool := mcp.Tool{
		Name:             builtin.ToolRecordVulnerability,
		Description:      "Record discovered vulnerability details in the vulnerability management system. When a valid vulnerability is found, use this tool to record vulnerability info including title, description, severity, type, target, proof, impact, and recommendation.",
		ShortDescription: "Record discovered vulnerability details",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"title": map[string]interface{}{
					"type":        "string",
					"description": "Vulnerability title (required)",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Detailed description of the vulnerability",
				},
				"severity": map[string]interface{}{
					"type":        "string",
					"description": "Severity: critical, high, medium, low, info",
					"enum":        []string{"critical", "high", "medium", "low", "info"},
				},
				"vulnerability_type": map[string]interface{}{
					"type":        "string",
					"description": "Vulnerability type, e.g.: SQL Injection, XSS, CSRF, Command Injection, etc.",
				},
				"target": map[string]interface{}{
					"type":        "string",
					"description": "Affected target (URL, IP address, service, etc.)",
				},
				"proof": map[string]interface{}{
					"type":        "string",
					"description": "Proof of vulnerability (POC, screenshots, request/response, etc.)",
				},
				"impact": map[string]interface{}{
					"type":        "string",
					"description": "Impact description",
				},
				"recommendation": map[string]interface{}{
					"type":        "string",
					"description": "Remediation recommendation",
				},
			},
			"required": []string{"title", "severity"},
		},
	}

	handler := func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		// 从参数中获取conversation_id（由Agent自动添加）
		conversationID, _ := args["conversation_id"].(string)
		if conversationID == "" {
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: "Error: conversation_id not set. This is a system error, please retry.",
					},
				},
				IsError: true,
			}, nil
		}

		title, ok := args["title"].(string)
		if !ok || title == "" {
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: "Error: title parameter is required and cannot be empty",
					},
				},
				IsError: true,
			}, nil
		}

		severity, ok := args["severity"].(string)
		if !ok || severity == "" {
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: "Error: severity parameter is required and cannot be empty",
					},
				},
				IsError: true,
			}, nil
		}

		// 验证严重程度
		validSeverities := map[string]bool{
			"critical": true,
			"high":     true,
			"medium":   true,
			"low":      true,
			"info":     true,
		}
		if !validSeverities[severity] {
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: fmt.Sprintf("Error: severity must be one of critical, high, medium, low, or info, current: %s", severity),
					},
				},
				IsError: true,
			}, nil
		}

		// 获取可选参数
		description := ""
		if d, ok := args["description"].(string); ok {
			description = d
		}

		vulnType := ""
		if t, ok := args["vulnerability_type"].(string); ok {
			vulnType = t
		}

		target := ""
		if t, ok := args["target"].(string); ok {
			target = t
		}

		proof := ""
		if p, ok := args["proof"].(string); ok {
			proof = p
		}

		impact := ""
		if i, ok := args["impact"].(string); ok {
			impact = i
		}

		recommendation := ""
		if r, ok := args["recommendation"].(string); ok {
			recommendation = r
		}

		// 创建漏洞记录
		vuln := &database.Vulnerability{
			ConversationID: conversationID,
			Title:          title,
			Description:    description,
			Severity:       severity,
			Status:         "open",
			Type:           vulnType,
			Target:         target,
			Proof:          proof,
			Impact:         impact,
			Recommendation: recommendation,
		}

		created, err := db.CreateVulnerability(vuln)
		if err != nil {
			logger.Error("Failed to record vulnerability", zap.Error(err))
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: fmt.Sprintf("Failed to record vulnerability: %v", err),
					},
				},
				IsError: true,
			}, nil
		}

		logger.Info("Vulnerability recorded successfully",
			zap.String("id", created.ID),
			zap.String("title", created.Title),
			zap.String("severity", created.Severity),
			zap.String("conversation_id", conversationID),
		)

		return &mcp.ToolResult{
			Content: []mcp.Content{
				{
					Type: "text",
					Text: fmt.Sprintf("Vulnerability recorded successfully!\n\nVulnerability ID: %s\nTitle: %s\nSeverity: %s\nStatus: %s\n\nYou can view and manage this vulnerability on the vulnerability management page.", created.ID, created.Title, created.Severity, created.Status),
				},
			},
			IsError: false,
		}, nil
	}

	mcpServer.RegisterTool(tool, handler)
	logger.Info("Vulnerability recording tool registered successfully")
}

// registerWebshellTools 注册 WebShell 相关 MCP 工具，供 AI 助手在指定连接上执行命令与文件操作
func registerWebshellTools(mcpServer *mcp.Server, db *database.DB, webshellHandler *handler.WebShellHandler, logger *zap.Logger) {
	if db == nil || webshellHandler == nil {
		logger.Warn("Skipping WebShell tool registration: db or webshellHandler is nil")
		return
	}

	// webshell_exec
	execTool := mcp.Tool{
		Name:             builtin.ToolWebshellExec,
		Description:      "Execute a system command on the specified WebShell connection, returning stdout. connection_id is selected by the user in the AI assistant context.",
		ShortDescription: "Execute command on WebShell connection",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"connection_id": map[string]interface{}{
					"type":        "string",
					"description": "WebShell connection ID (e.g. ws_xxx)",
				},
				"command": map[string]interface{}{
					"type":        "string",
					"description": "System command to execute",
				},
			},
			"required": []string{"connection_id", "command"},
		},
	}
	execHandler := func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		cid, _ := args["connection_id"].(string)
		cmd, _ := args["command"].(string)
		if cid == "" || cmd == "" {
			return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: "connection_id and command are both required"}}, IsError: true}, nil
		}
		conn, err := db.GetWebshellConnection(cid)
		if err != nil || conn == nil {
			return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: "WebShell connection not found or query failed"}}, IsError: true}, nil
		}
		output, ok, errMsg := webshellHandler.ExecWithConnection(conn, cmd)
		if errMsg != "" {
			return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: errMsg}}, IsError: true}, nil
		}
		if !ok {
			return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: "Non-200 HTTP status, output:\n" + output}}, IsError: false}, nil
		}
		return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: output}}, IsError: false}, nil
	}
	mcpServer.RegisterTool(execTool, execHandler)

	// webshell_file_list
	listTool := mcp.Tool{
		Name:             builtin.ToolWebshellFileList,
		Description:      "List directory contents on the specified WebShell connection. path defaults to current directory (.).",
		ShortDescription: "List directory on WebShell",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"connection_id": map[string]interface{}{"type": "string", "description": "WebShell connection ID"},
				"path":          map[string]interface{}{"type": "string", "description": "Directory path, default ."},
			},
			"required": []string{"connection_id"},
		},
	}
	listHandler := func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		cid, _ := args["connection_id"].(string)
		path, _ := args["path"].(string)
		if cid == "" {
			return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: "connection_id is required"}}, IsError: true}, nil
		}
		conn, err := db.GetWebshellConnection(cid)
		if err != nil || conn == nil {
			return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: "WebShell connection not found"}}, IsError: true}, nil
		}
		output, ok, errMsg := webshellHandler.FileOpWithConnection(conn, "list", path, "", "")
		if errMsg != "" {
			return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: errMsg}}, IsError: true}, nil
		}
		return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: output}}, IsError: !ok}, nil
	}
	mcpServer.RegisterTool(listTool, listHandler)

	// webshell_file_read
	readTool := mcp.Tool{
		Name:             builtin.ToolWebshellFileRead,
		Description:      "Read file content on the specified WebShell connection.",
		ShortDescription: "Read file on WebShell",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"connection_id": map[string]interface{}{"type": "string", "description": "WebShell connection ID"},
				"path":          map[string]interface{}{"type": "string", "description": "File path"},
			},
			"required": []string{"connection_id", "path"},
		},
	}
	readHandler := func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		cid, _ := args["connection_id"].(string)
		path, _ := args["path"].(string)
		if cid == "" || path == "" {
			return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: "connection_id and path are both required"}}, IsError: true}, nil
		}
		conn, err := db.GetWebshellConnection(cid)
		if err != nil || conn == nil {
			return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: "WebShell connection not found"}}, IsError: true}, nil
		}
		output, ok, errMsg := webshellHandler.FileOpWithConnection(conn, "read", path, "", "")
		if errMsg != "" {
			return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: errMsg}}, IsError: true}, nil
		}
		return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: output}}, IsError: !ok}, nil
	}
	mcpServer.RegisterTool(readTool, readHandler)

	// webshell_file_write
	writeTool := mcp.Tool{
		Name:             builtin.ToolWebshellFileWrite,
		Description:      "Write file content on the specified WebShell connection (overwrites existing file).",
		ShortDescription: "Write file on WebShell",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"connection_id": map[string]interface{}{"type": "string", "description": "WebShell connection ID"},
				"path":          map[string]interface{}{"type": "string", "description": "File path"},
				"content":       map[string]interface{}{"type": "string", "description": "Content to write"},
			},
			"required": []string{"connection_id", "path", "content"},
		},
	}
	writeHandler := func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		cid, _ := args["connection_id"].(string)
		path, _ := args["path"].(string)
		content, _ := args["content"].(string)
		if cid == "" || path == "" {
			return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: "connection_id and path are both required"}}, IsError: true}, nil
		}
		conn, err := db.GetWebshellConnection(cid)
		if err != nil || conn == nil {
			return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: "WebShell connection not found"}}, IsError: true}, nil
		}
		output, ok, errMsg := webshellHandler.FileOpWithConnection(conn, "write", path, content, "")
		if errMsg != "" {
			return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: errMsg}}, IsError: true}, nil
		}
		if !ok {
			return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: "Write may have failed, output:\n" + output}}, IsError: false}, nil
		}
		return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: "Write successful\n" + output}}, IsError: false}, nil
	}
	mcpServer.RegisterTool(writeTool, writeHandler)

	logger.Info("WebShell tools registered successfully")
}

// registerWebshellManagementTools 注册 WebShell 连接管理 MCP 工具
func registerWebshellManagementTools(mcpServer *mcp.Server, db *database.DB, webshellHandler *handler.WebShellHandler, logger *zap.Logger) {
	if db == nil {
		logger.Warn("Skipping WebShell management tool registration: db is nil")
		return
	}

	// manage_webshell_list - 列出所有 webshell 连接
	listTool := mcp.Tool{
		Name:             builtin.ToolManageWebshellList,
		Description:      "List all saved WebShell connections, returning connection ID, URL, type, notes, etc.",
		ShortDescription: "List all WebShell connections",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}
	listHandler := func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		connections, err := db.ListWebshellConnections()
		if err != nil {
			return &mcp.ToolResult{
				Content: []mcp.Content{{Type: "text", Text: "Failed to get connection list: " + err.Error()}},
				IsError: true,
			}, nil
		}
		if len(connections) == 0 {
			return &mcp.ToolResult{
				Content: []mcp.Content{{Type: "text", Text: "No WebShell connections"}},
				IsError: false,
			}, nil
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Found %d WebShell connection(s):\n\n", len(connections)))
		for _, conn := range connections {
			sb.WriteString(fmt.Sprintf("ID: %s\n", conn.ID))
			sb.WriteString(fmt.Sprintf("  URL: %s\n", conn.URL))
			sb.WriteString(fmt.Sprintf("  Type: %s\n", conn.Type))
			sb.WriteString(fmt.Sprintf("  Method: %s\n", conn.Method))
			sb.WriteString(fmt.Sprintf("  Command param: %s\n", conn.CmdParam))
			if conn.Remark != "" {
				sb.WriteString(fmt.Sprintf("  Notes: %s\n", conn.Remark))
			}
			sb.WriteString(fmt.Sprintf("  Created: %s\n", conn.CreatedAt.Format("2006-01-02 15:04:05")))
			sb.WriteString("\n")
		}
		return &mcp.ToolResult{
			Content: []mcp.Content{{Type: "text", Text: sb.String()}},
			IsError: false,
		}, nil
	}
	mcpServer.RegisterTool(listTool, listHandler)

	// manage_webshell_add - 添加新的 webshell 连接
	addTool := mcp.Tool{
		Name:             builtin.ToolManageWebshellAdd,
		Description:      "Add a new WebShell connection to the management system. Supports PHP, ASP, ASPX, JSP, and other one-liner shells.",
		ShortDescription: "Add WebShell connection",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "Shell URL, e.g. http://target.com/shell.php (required)",
				},
				"password": map[string]interface{}{
					"type":        "string",
					"description": "Connection password/key, e.g. Behinder/AntSword password",
				},
				"type": map[string]interface{}{
					"type":        "string",
					"description": "Shell type: php, asp, aspx, jsp, default php",
					"enum":        []string{"php", "asp", "aspx", "jsp"},
				},
				"method": map[string]interface{}{
					"type":        "string",
					"description": "HTTP method: GET or POST, default POST",
					"enum":        []string{"GET", "POST"},
				},
				"cmd_param": map[string]interface{}{
					"type":        "string",
					"description": "Command parameter name, default cmd",
				},
				"remark": map[string]interface{}{
					"type":        "string",
					"description": "Notes/label for easy identification",
				},
			},
			"required": []string{"url"},
		},
	}
	addHandler := func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		urlStr, _ := args["url"].(string)
		if urlStr == "" {
			return &mcp.ToolResult{
				Content: []mcp.Content{{Type: "text", Text: "Error: url parameter is required"}},
				IsError: true,
			}, nil
		}

		password, _ := args["password"].(string)
		shellType, _ := args["type"].(string)
		if shellType == "" {
			shellType = "php"
		}
		method, _ := args["method"].(string)
		if method == "" {
			method = "post"
		}
		cmdParam, _ := args["cmd_param"].(string)
		if cmdParam == "" {
			cmdParam = "cmd"
		}
		remark, _ := args["remark"].(string)

		// 生成连接ID
		connID := "ws_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:12]
		conn := &database.WebShellConnection{
			ID:        connID,
			URL:       urlStr,
			Password:  password,
			Type:      strings.ToLower(shellType),
			Method:    strings.ToLower(method),
			CmdParam:  cmdParam,
			Remark:    remark,
			CreatedAt: time.Now(),
		}

		if err := db.CreateWebshellConnection(conn); err != nil {
			return &mcp.ToolResult{
				Content: []mcp.Content{{Type: "text", Text: "Failed to add WebShell connection: " + err.Error()}},
				IsError: true,
			}, nil
		}

		return &mcp.ToolResult{
			Content: []mcp.Content{{
				Type: "text",
				Text: fmt.Sprintf("WebShell connection added successfully!\n\nConnection ID: %s\nURL: %s\nType: %s\nMethod: %s\nCommand param: %s", conn.ID, conn.URL, conn.Type, conn.Method, conn.CmdParam),
			}},
			IsError: false,
		}, nil
	}
	mcpServer.RegisterTool(addTool, addHandler)

	// manage_webshell_update - 更新 webshell 连接
	updateTool := mcp.Tool{
		Name:             builtin.ToolManageWebshellUpdate,
		Description:      "Update an existing WebShell connection's information.",
		ShortDescription: "Update WebShell connection",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"connection_id": map[string]interface{}{
					"type":        "string",
					"description": "WebShell connection ID to update (required)",
				},
				"url": map[string]interface{}{
					"type":        "string",
					"description": "New shell URL",
				},
				"password": map[string]interface{}{
					"type":        "string",
					"description": "New connection password/key",
				},
				"type": map[string]interface{}{
					"type":        "string",
					"description": "New shell type: php, asp, aspx, jsp",
					"enum":        []string{"php", "asp", "aspx", "jsp"},
				},
				"method": map[string]interface{}{
					"type":        "string",
					"description": "New HTTP method: GET or POST",
					"enum":        []string{"GET", "POST"},
				},
				"cmd_param": map[string]interface{}{
					"type":        "string",
					"description": "New command parameter name",
				},
				"remark": map[string]interface{}{
					"type":        "string",
					"description": "New notes",
				},
			},
			"required": []string{"connection_id"},
		},
	}
	updateHandler := func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		connID, _ := args["connection_id"].(string)
		if connID == "" {
			return &mcp.ToolResult{
				Content: []mcp.Content{{Type: "text", Text: "Error: connection_id parameter is required"}},
				IsError: true,
			}, nil
		}

		// 获取现有连接
		existing, err := db.GetWebshellConnection(connID)
		if err != nil || existing == nil {
			return &mcp.ToolResult{
				Content: []mcp.Content{{Type: "text", Text: "Specified WebShell connection not found: " + connID}},
				IsError: true,
			}, nil
		}

		// 更新字段（如果提供了新值）
		if urlStr, ok := args["url"].(string); ok && urlStr != "" {
			existing.URL = urlStr
		}
		if password, ok := args["password"].(string); ok {
			existing.Password = password
		}
		if shellType, ok := args["type"].(string); ok && shellType != "" {
			existing.Type = strings.ToLower(shellType)
		}
		if method, ok := args["method"].(string); ok && method != "" {
			existing.Method = strings.ToLower(method)
		}
		if cmdParam, ok := args["cmd_param"].(string); ok && cmdParam != "" {
			existing.CmdParam = cmdParam
		}
		if remark, ok := args["remark"].(string); ok {
			existing.Remark = remark
		}

		if err := db.UpdateWebshellConnection(existing); err != nil {
			return &mcp.ToolResult{
				Content: []mcp.Content{{Type: "text", Text: "Failed to update WebShell connection: " + err.Error()}},
				IsError: true,
			}, nil
		}

		return &mcp.ToolResult{
			Content: []mcp.Content{{
				Type: "text",
				Text: fmt.Sprintf("WebShell connection updated successfully!\n\nConnection ID: %s\nURL: %s\nType: %s\nMethod: %s\nCommand param: %s\nNotes: %s", existing.ID, existing.URL, existing.Type, existing.Method, existing.CmdParam, existing.Remark),
			}},
			IsError: false,
		}, nil
	}
	mcpServer.RegisterTool(updateTool, updateHandler)

	// manage_webshell_delete - 删除 webshell 连接
	deleteTool := mcp.Tool{
		Name:             builtin.ToolManageWebshellDelete,
		Description:      "Delete the specified WebShell connection.",
		ShortDescription: "Delete WebShell connection",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"connection_id": map[string]interface{}{
					"type":        "string",
					"description": "WebShell connection ID to delete (required)",
				},
			},
			"required": []string{"connection_id"},
		},
	}
	deleteHandler := func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		connID, _ := args["connection_id"].(string)
		if connID == "" {
			return &mcp.ToolResult{
				Content: []mcp.Content{{Type: "text", Text: "Error: connection_id parameter is required"}},
				IsError: true,
			}, nil
		}

		if err := db.DeleteWebshellConnection(connID); err != nil {
			return &mcp.ToolResult{
				Content: []mcp.Content{{Type: "text", Text: "Failed to delete WebShell connection: " + err.Error()}},
				IsError: true,
			}, nil
		}

		return &mcp.ToolResult{
			Content: []mcp.Content{{
				Type: "text",
				Text: fmt.Sprintf("WebShell connection %s deleted successfully", connID),
			}},
			IsError: false,
		}, nil
	}
	mcpServer.RegisterTool(deleteTool, deleteHandler)

	// manage_webshell_test - 测试 webshell 连接
	testTool := mcp.Tool{
		Name:             builtin.ToolManageWebshellTest,
		Description:      "Test whether the specified WebShell connection is working by executing a simple command (e.g. whoami or dir).",
		ShortDescription: "Test WebShell connection",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"connection_id": map[string]interface{}{
					"type":        "string",
					"description": "WebShell connection ID to test (required)",
				},
				"command": map[string]interface{}{
					"type":        "string",
					"description": "Test command, default whoami (Linux) or dir (Windows)",
				},
			},
			"required": []string{"connection_id"},
		},
	}
	testHandler := func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		connID, _ := args["connection_id"].(string)
		if connID == "" {
			return &mcp.ToolResult{
				Content: []mcp.Content{{Type: "text", Text: "Error: connection_id parameter is required"}},
				IsError: true,
			}, nil
		}

		// 获取连接
		conn, err := db.GetWebshellConnection(connID)
		if err != nil || conn == nil {
			return &mcp.ToolResult{
				Content: []mcp.Content{{Type: "text", Text: "Specified WebShell connection not found: " + connID}},
				IsError: true,
			}, nil
		}

		// 确定测试命令
		testCmd, _ := args["command"].(string)
		if testCmd == "" {
			// 根据 shell 类型选择默认命令
			if conn.Type == "asp" || conn.Type == "aspx" {
				testCmd = "dir"
			} else {
				testCmd = "whoami"
			}
		}

		// 执行测试命令
		output, ok, errMsg := webshellHandler.ExecWithConnection(conn, testCmd)
		if errMsg != "" {
			return &mcp.ToolResult{
				Content: []mcp.Content{{Type: "text", Text: fmt.Sprintf("Connection test failed!\n\nConnection ID: %s\nURL: %s\nError: %s", connID, conn.URL, errMsg)}},
				IsError: true,
			}, nil
		}

		if !ok {
			return &mcp.ToolResult{
				Content: []mcp.Content{{Type: "text", Text: fmt.Sprintf("Connection test failed! Non-200 HTTP status\n\nConnection ID: %s\nURL: %s\nOutput: %s", connID, conn.URL, output)}},
				IsError: true,
			}, nil
		}

		return &mcp.ToolResult{
			Content: []mcp.Content{{
				Type: "text",
				Text: fmt.Sprintf("Connection test successful!\n\nConnection ID: %s\nURL: %s\nType: %s\n\nTest command: %s\nOutput:\n%s", connID, conn.URL, conn.Type, testCmd, output),
			}},
			IsError: false,
		}, nil
	}
	mcpServer.RegisterTool(testTool, testHandler)

	logger.Info("WebShell management tools registered successfully")
}

// initializeKnowledge 初始化知识库组件（用于动态初始化）
func initializeKnowledge(
	cfg *config.Config,
	db *database.DB,
	knowledgeDBConn *database.DB,
	mcpServer *mcp.Server,
	agentHandler *handler.AgentHandler,
	app *App, // 传递 App 引用以便更新知识库组件
	logger *zap.Logger,
) (*handler.KnowledgeHandler, error) {
	// 确定知识库数据库路径
	knowledgeDBPath := cfg.Database.KnowledgeDBPath
	var knowledgeDB *sql.DB

	if knowledgeDBPath != "" {
		// 使用独立的知识库数据库
		// 确保目录存在
		if err := os.MkdirAll(filepath.Dir(knowledgeDBPath), 0755); err != nil {
			return nil, fmt.Errorf("failed to create knowledge base database directory: %w", err)
		}

		var err error
		knowledgeDBConn, err = database.NewKnowledgeDB(knowledgeDBPath, logger)
		if err != nil {
			return nil, fmt.Errorf("knowledge base database initialization failed: %w", err)
		}
		knowledgeDB = knowledgeDBConn.DB
		logger.Info("Using separate knowledge base database", zap.String("path", knowledgeDBPath))
	} else {
		// 向后兼容：使用会话数据库
		knowledgeDB = db.DB
		logger.Info("Using session database for knowledge base storage (recommend configuring knowledge_db_path for data separation)")
	}

	// 创建知识库管理器
	knowledgeManager := knowledge.NewManager(knowledgeDB, cfg.Knowledge.BasePath, logger)

	// 创建嵌入器
	// 使用OpenAI配置的API Key（如果知识库配置中没有指定）
	if cfg.Knowledge.Embedding.APIKey == "" {
		cfg.Knowledge.Embedding.APIKey = cfg.OpenAI.APIKey
	}
	if cfg.Knowledge.Embedding.BaseURL == "" {
		cfg.Knowledge.Embedding.BaseURL = cfg.OpenAI.BaseURL
	}

	embedder, err := knowledge.NewEmbedder(context.Background(), &cfg.Knowledge, &cfg.OpenAI, logger)
	if err != nil {
		return nil, fmt.Errorf("knowledge base embedder initialization failed: %w", err)
	}

	// 创建检索器
	retrievalConfig := &knowledge.RetrievalConfig{
		TopK:                cfg.Knowledge.Retrieval.TopK,
		SimilarityThreshold: cfg.Knowledge.Retrieval.SimilarityThreshold,
		SubIndexFilter:      cfg.Knowledge.Retrieval.SubIndexFilter,
		PostRetrieve:        cfg.Knowledge.Retrieval.PostRetrieve,
	}
	knowledgeRetriever := knowledge.NewRetriever(knowledgeDB, embedder, retrievalConfig, logger)

	// 创建索引器（Eino Compose 链）
	knowledgeIndexer, err := knowledge.NewIndexer(context.Background(), knowledgeDB, embedder, logger, &cfg.Knowledge)
	if err != nil {
		return nil, fmt.Errorf("knowledge base indexer initialization failed: %w", err)
	}

	// 注册知识检索工具到MCP服务器
	knowledge.RegisterKnowledgeTool(mcpServer, knowledgeRetriever, knowledgeManager, logger)

	// 创建知识库API处理器
	knowledgeHandler := handler.NewKnowledgeHandler(knowledgeManager, knowledgeRetriever, knowledgeIndexer, db, logger)
	if app != nil && app.auditSvc != nil {
		knowledgeHandler.SetAudit(app.auditSvc)
	}
	logger.Info("Knowledge base module initialized", zap.Bool("handler_created", knowledgeHandler != nil))

	// 设置知识库管理器到AgentHandler以便记录检索日志
	agentHandler.SetKnowledgeManager(knowledgeManager)

	// 更新 App 中的知识库组件（如果 App 不为 nil，说明是动态初始化）
	if app != nil {
		app.knowledgeManager = knowledgeManager
		app.knowledgeRetriever = knowledgeRetriever
		app.knowledgeIndexer = knowledgeIndexer
		app.knowledgeHandler = knowledgeHandler
		// 如果使用独立数据库，更新 knowledgeDB
		if knowledgeDBPath != "" {
			app.knowledgeDB = knowledgeDBConn
		}
		logger.Info("Knowledge base components updated in App")
	}

	// 扫描知识库并建立索引（异步）
	go func() {
		itemsToIndex, err := knowledgeManager.ScanKnowledgeBase()
		if err != nil {
			logger.Warn("Knowledge base scan failed", zap.Error(err))
			return
		}

		// 检查是否已有索引
		hasIndex, err := knowledgeIndexer.HasIndex()
		if err != nil {
			logger.Warn("Index status check failed", zap.Error(err))
			return
		}

		if hasIndex {
			// 如果已有索引，只索引新添加或更新的项
			if len(itemsToIndex) > 0 {
				logger.Info("Existing knowledge base index detected, starting incremental indexing", zap.Int("count", len(itemsToIndex)))
				ctx := context.Background()
				consecutiveFailures := 0
				var firstFailureItemID string
				var firstFailureError error
				failedCount := 0

				for _, itemID := range itemsToIndex {
					if err := knowledgeIndexer.IndexItem(ctx, itemID); err != nil {
						failedCount++
						consecutiveFailures++

						if consecutiveFailures == 1 {
							firstFailureItemID = itemID
							firstFailureError = err
							logger.Warn("Knowledge item indexing failed", zap.String("itemId", itemID), zap.Error(err))
						}

						// 如果连续失败2次，立即停止增量索引
						if consecutiveFailures >= 2 {
							logger.Error("Too many consecutive indexing failures, stopping incremental indexing immediately",
								zap.Int("consecutiveFailures", consecutiveFailures),
								zap.Int("totalItems", len(itemsToIndex)),
								zap.String("firstFailureItemId", firstFailureItemID),
								zap.Error(firstFailureError),
							)
							break
						}
						continue
					}

					// 成功时重置连续失败计数
					if consecutiveFailures > 0 {
						consecutiveFailures = 0
						firstFailureItemID = ""
						firstFailureError = nil
					}
				}
				logger.Info("Incremental indexing complete", zap.Int("totalItems", len(itemsToIndex)), zap.Int("failedCount", failedCount))
			} else {
				logger.Info("Existing knowledge base index detected, no new or updated items to index")
			}
			return
		}

		// 只有在没有索引时才自动重建
		logger.Info("No knowledge base index detected, starting automatic index build")
		ctx := context.Background()
		if err := knowledgeIndexer.RebuildIndex(ctx); err != nil {
			logger.Warn("Knowledge base index rebuild failed", zap.Error(err))
		}
	}()

	return knowledgeHandler, nil
}

// corsMiddleware CORS中间件
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}
