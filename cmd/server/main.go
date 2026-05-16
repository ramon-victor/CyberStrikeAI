package main

import (
	"context"
	"cyberstrike-ai/internal/app"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/logger"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

func main() {
	var configPath = flag.String("config", "config.yaml", "config file path")
	var httpsBootstrap = flag.Bool("https", false, "enable HTTPS for main site: uses in-memory self-signed cert when tls_cert_path/tls_key_path not configured (local testing); consistent with run.sh default")
	flag.Parse()

	// 环境变量兼容（便于 systemd/docker 等不传参场景）
	if !*httpsBootstrap {
		v := strings.TrimSpace(os.Getenv("CYBERSTRIKE_HTTPS"))
		if v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes") {
			*httpsBootstrap = true
		}
	}

	// 加载配置
	cp := strings.TrimSpace(*configPath)
	if cp == "" {
		cp = "config.yaml"
	}
	if strings.HasPrefix(cp, "-") {
		fmt.Fprintf(os.Stderr, "Invalid -config path %q.\nFor HTTPS with config: ./cyberstrike-ai --https -config config.yaml (-config must be followed by a yaml file path).\n", cp)
		os.Exit(2)
	}
	cfg, err := config.Load(cp)
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		return
	}

	if *httpsBootstrap {
		config.ApplyDevHTTPSBootstrap(cfg)
	}

	port := cfg.Server.Port
	if port <= 0 {
		port = 8080
	}
	scheme := "http"
	if config.MainWebUIUsesHTTPS(&cfg.Server) {
		scheme = "https"
	}
	fmt.Println()
	fmt.Printf("→ Web UI: %s://127.0.0.1:%d/\n", scheme, port)
	if scheme == "https" && cfg.Server.TLSAutoSelfSign {
		fmt.Println("  (Self-signed cert: browser will ask to confirm on first visit)")
	}
	if scheme == "https" && config.ServerHTTPRedirectEnabled(&cfg.Server) {
		fmt.Printf("  (http://127.0.0.1:%d/ will auto-redirect to HTTPS)\n", port)
	}
	fmt.Println()

	// MCP 启用且 auth_header_value 为空时，自动生成随机密钥并写回配置
	if err := config.EnsureMCPAuth(cp, cfg); err != nil {
		fmt.Printf("MCP auth config failed: %v\n", err)
		return
	}
	if cfg.MCP.Enabled {
		config.PrintMCPConfigJSON(cfg.MCP)
	}

	// 初始化日志
	log := logger.New(cfg.Log.Level, cfg.Log.Output)

	// 创建可取消的根 context，用于优雅关闭
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 监听系统信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// 创建应用
	application, err := app.New(cfg, log, cp)
	if err != nil {
		log.Fatal("App initialization failed", "error", err)
	}

	// 在后台监听信号
	go func() {
		sig := <-sigCh
		log.Info("Received system signal, starting graceful shutdown: " + sig.String())
		application.Shutdown()
		cancel()
	}()

	// 启动服务器（传入 context 以支持优雅关闭）
	if err := application.RunWithContext(ctx); err != nil {
		// context 取消导致的关闭不视为错误
		if ctx.Err() != nil {
			log.Info("Server shut down gracefully")
		} else {
			log.Fatal("Server startup failed", "error", err)
		}
	}
}
