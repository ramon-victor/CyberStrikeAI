package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/logger"
	"cyberstrike-ai/internal/mcp"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run cmd/test-external-mcp/main.go <config.yaml>")
		os.Exit(1)
	}

	configPath := os.Args[1]
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	if cfg.ExternalMCP.Servers == nil || len(cfg.ExternalMCP.Servers) == 0 {
		fmt.Println("No external MCP servers configured")
		os.Exit(0)
	}

	fmt.Printf("Found %d external MCP server(s)\n\n", len(cfg.ExternalMCP.Servers))

	// 创建日志
	log := logger.New("info", "stdout")

	// 创建外部MCP管理器
	manager := mcp.NewExternalMCPManager(log.Logger)
	manager.LoadConfigs(&cfg.ExternalMCP)

	// 显示配置
	fmt.Println("=== Configuration ===")
	for name, srv := range cfg.ExternalMCP.Servers {
		fmt.Printf("\n%s:\n", name)
		fmt.Printf("  Transport: %s\n", getTransport(srv))
		if srv.Command != "" {
			fmt.Printf("  Command: %s\n", srv.Command)
			fmt.Printf("  Args: %v\n", srv.Args)
		}
		if srv.URL != "" {
			fmt.Printf("  URL: %s\n", srv.URL)
		}
		fmt.Printf("  Description: %s\n", srv.Description)
		fmt.Printf("  Timeout: %d seconds\n", srv.Timeout)
		fmt.Printf("  ExternalMCPEnable: %v\n", srv.ExternalMCPEnable)
	}

	// 获取统计信息
	fmt.Println("\n=== Statistics ===")
	stats := manager.GetStats()
	fmt.Printf("Total: %d\n", stats["total"])
	fmt.Printf("Enabled: %d\n", stats["enabled"])
	fmt.Printf("Disabled: %d\n", stats["disabled"])
	fmt.Printf("Connected: %d\n", stats["connected"])

	// 测试启动（仅测试启用的）
	fmt.Println("\n=== Test Start ===")
	for name, srv := range cfg.ExternalMCP.Servers {
		if srv.ExternalMCPEnable {
			fmt.Printf("\nAttempting to start %s...\n", name)
			// 注意：实际启动可能会失败，因为需要真实的MCP服务器
			err := manager.StartClient(name)
			if err != nil {
				fmt.Printf("  Start failed (expected if no real MCP server): %v\n", err)
			} else {
				fmt.Printf("  Start successful\n")
				// 获取客户端状态
				if client, exists := manager.GetClient(name); exists {
					fmt.Printf("  Status: %s\n", client.GetStatus())
					fmt.Printf("  Connected: %v\n", client.IsConnected())
				}
			}
		}
	}

	// 等待一下
	time.Sleep(2 * time.Second)

	// 测试获取工具列表
	fmt.Println("\n=== Test: Get Tool List ===")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tools, err := manager.GetAllTools(ctx)
	if err != nil {
		fmt.Printf("Failed to get tool list: %v\n", err)
	} else {
		fmt.Printf("Got %d tools\n", len(tools))
		for i, tool := range tools {
			if i < 5 { // 只显示前5个
				fmt.Printf("  - %s: %s\n", tool.Name, tool.Description)
			}
		}
		if len(tools) > 5 {
			fmt.Printf("  ... %d more tools\n", len(tools)-5)
		}
	}

	// 测试停止
	fmt.Println("\n=== Test: Stop ===")
	for name := range cfg.ExternalMCP.Servers {
		fmt.Printf("\nStopping %s...\n", name)
		err := manager.StopClient(name)
		if err != nil {
			fmt.Printf("  Stop failed: %v\n", err)
		} else {
			fmt.Printf("  Stop successful\n")
		}
	}

	// 最终统计
	fmt.Println("\n=== Final Statistics ===")
	stats = manager.GetStats()
	fmt.Printf("Total: %d\n", stats["total"])
	fmt.Printf("Enabled: %d\n", stats["enabled"])
	fmt.Printf("Disabled: %d\n", stats["disabled"])
	fmt.Printf("Connected: %d\n", stats["connected"])

	fmt.Println("\n=== Test Complete ===")
}

func getTransport(srv config.ExternalMCPServerConfig) string {
	t := srv.GetTransportType()
	if t == "" {
		return "unknown"
	}
	return t
}

