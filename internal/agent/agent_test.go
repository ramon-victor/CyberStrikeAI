package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/storage"

	"go.uber.org/zap"
)

// setupTestAgent creates a test Agent.
func setupTestAgent(t *testing.T) (*Agent, *storage.FileResultStorage) {
	logger := zap.NewNop()
	mcpServer := mcp.NewServer(logger)

	openAICfg := &config.OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: "https://api.test.com/v1",
		Model:   "test-model",
	}

	agentCfg := &config.AgentConfig{
		MaxIterations:        10,
		LargeResultThreshold: 100, // set a small threshold for testing
		ResultStorageDir:     "",
	}

	agent := NewAgent(openAICfg, agentCfg, mcpServer, nil, logger, 10)

	// Create test storage
	tmpDir := filepath.Join(os.TempDir(), "test_agent_storage_"+time.Now().Format("20060102_150405"))
	testStorage, err := storage.NewFileResultStorage(tmpDir, logger)
	if err != nil {
		t.Fatalf("failed to create test storage: %v", err)
	}

	agent.SetResultStorage(testStorage)

	return agent, testStorage
}

func TestAgent_FormatMinimalNotification(t *testing.T) {
	agent, testStorage := setupTestAgent(t)
	_ = testStorage // avoid unused variable warning

	executionID := "test_exec_001"
	toolName := "nmap_scan"
	size := 50000
	lineCount := 1000
	filePath := "tmp/test_exec_001.txt"

	notification := agent.formatMinimalNotification(executionID, toolName, size, lineCount, filePath)

	// Verify the notification contains required information
	if !strings.Contains(notification, executionID) {
		t.Errorf("notification should contain execution ID: %s", executionID)
	}

	if !strings.Contains(notification, toolName) {
		t.Errorf("notification should contain tool name: %s", toolName)
	}

	if !strings.Contains(notification, "50000") {
		t.Errorf("notification should contain size information")
	}

	if !strings.Contains(notification, "1000") {
		t.Errorf("notification should contain line count information")
	}

	if !strings.Contains(notification, "query_execution_result") {
		t.Errorf("notification should contain query tool usage instructions")
	}
}

func TestAgent_ExecuteToolViaMCP_LargeResult(t *testing.T) {
	agent, _ := setupTestAgent(t)

	// Create simulated MCP tool result (large result)
	largeResult := &mcp.ToolResult{
		Content: []mcp.Content{
			{
				Type: "text",
				Text: strings.Repeat("This is a test line with some content.\n", 1000), // 约50KB
			},
		},
		IsError: false,
	}

	// Simulate MCP server returning a large result
	// Since we need to simulate CallTool behavior, we would need a mock or use the actual MCP server
	// To simplify testing, we directly test the result processing logic

	// Set threshold
	agent.mu.Lock()
	agent.largeResultThreshold = 1000 // 设置较小的阈值
	agent.mu.Unlock()

	// Create execution ID
	executionID := "test_exec_large_001"
	toolName := "test_tool"

	// Format result
	var resultText strings.Builder
	for _, content := range largeResult.Content {
		resultText.WriteString(content.Text)
		resultText.WriteString("\n")
	}

	resultStr := resultText.String()
	resultSize := len(resultStr)

	// Detect large result and save
	agent.mu.RLock()
	threshold := agent.largeResultThreshold
	storage := agent.resultStorage
	agent.mu.RUnlock()

	if resultSize > threshold && storage != nil {
		// Save large result
		err := storage.SaveResult(executionID, toolName, resultStr)
		if err != nil {
			t.Fatalf("failed to save large result: %v", err)
		}

		// Generate notification
		lines := strings.Split(resultStr, "\n")
		filePath := storage.GetResultPath(executionID)
		notification := agent.formatMinimalNotification(executionID, toolName, resultSize, len(lines), filePath)

		// Verify notification format
		if !strings.Contains(notification, executionID) {
			t.Errorf("notification should contain execution ID")
		}

		// Verify result was saved
		savedResult, err := storage.GetResult(executionID)
		if err != nil {
			t.Fatalf("failed to get saved result: %v", err)
		}

		if savedResult != resultStr {
			t.Errorf("saved result does not match original result")
		}
	} else {
		t.Fatal("large result should be detected and saved")
	}
}

func TestAgent_ExecuteToolViaMCP_SmallResult(t *testing.T) {
	agent, _ := setupTestAgent(t)

	// Create small result
	smallResult := &mcp.ToolResult{
		Content: []mcp.Content{
			{
				Type: "text",
				Text: "Small result content",
			},
		},
		IsError: false,
	}

	// Set a larger threshold
	agent.mu.Lock()
	agent.largeResultThreshold = 100000 // 100KB
	agent.mu.Unlock()

	// Format result
	var resultText strings.Builder
	for _, content := range smallResult.Content {
		resultText.WriteString(content.Text)
		resultText.WriteString("\n")
	}

	resultStr := resultText.String()
	resultSize := len(resultStr)

	// Detect large result
	agent.mu.RLock()
	threshold := agent.largeResultThreshold
	storage := agent.resultStorage
	agent.mu.RUnlock()

	if resultSize > threshold && storage != nil {
		t.Fatal("small result should not be saved")
	}

	// Small result should be returned directly
	if resultSize <= threshold {
		// 这是预期的行为
		if resultStr == "" {
			t.Fatal("small result should be returned directly, should not be empty")
		}
	}
}

func TestAgent_SetResultStorage(t *testing.T) {
	agent, _ := setupTestAgent(t)

	// Create new storage
	tmpDir := filepath.Join(os.TempDir(), "test_new_storage_"+time.Now().Format("20060102_150405"))
	newStorage, err := storage.NewFileResultStorage(tmpDir, zap.NewNop())
	if err != nil {
		t.Fatalf("failed to create new storage: %v", err)
	}

	// Set new storage
	agent.SetResultStorage(newStorage)

	// Verify storage was updated
	agent.mu.RLock()
	currentStorage := agent.resultStorage
	agent.mu.RUnlock()

	if currentStorage != newStorage {
		t.Fatal("storage was not correctly updated")
	}

	// Cleanup
	os.RemoveAll(tmpDir)
}

func TestAgent_NewAgent_DefaultValues(t *testing.T) {
	logger := zap.NewNop()
	mcpServer := mcp.NewServer(logger)

	openAICfg := &config.OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: "https://api.test.com/v1",
		Model:   "test-model",
	}

	// Test default configuration
	agent := NewAgent(openAICfg, nil, mcpServer, nil, logger, 0)

	if agent.maxIterations != 30 {
		t.Errorf("default iteration count mismatch. expected: 30, actual: %d", agent.maxIterations)
	}

	agent.mu.RLock()
	threshold := agent.largeResultThreshold
	agent.mu.RUnlock()

	if threshold != 50*1024 {
		t.Errorf("default threshold mismatch. expected: %d, actual: %d", 50*1024, threshold)
	}
}

func TestAgent_NewAgent_CustomConfig(t *testing.T) {
	logger := zap.NewNop()
	mcpServer := mcp.NewServer(logger)

	openAICfg := &config.OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: "https://api.test.com/v1",
		Model:   "test-model",
	}

	agentCfg := &config.AgentConfig{
		MaxIterations:        20,
		LargeResultThreshold: 100 * 1024, // 100KB
		ResultStorageDir:     "custom_tmp",
	}

	agent := NewAgent(openAICfg, agentCfg, mcpServer, nil, logger, 15)

	if agent.maxIterations != 15 {
		t.Errorf("iteration count mismatch. expected: 15, actual: %d", agent.maxIterations)
	}

	agent.mu.RLock()
	threshold := agent.largeResultThreshold
	agent.mu.RUnlock()

	if threshold != 100*1024 {
		t.Errorf("threshold mismatch. expected: %d, actual: %d", 100*1024, threshold)
	}
}
