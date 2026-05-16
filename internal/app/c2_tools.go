package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/c2"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/mcp/builtin"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// registerC2Tools registers all C2 MCP tools (merged by category to reduce tool count and save context tokens).
// webListenPort is this process's Web/API listen port (config server.port, loaded at startup), used in MCP descriptions to warn against port conflicts with C2 bind_port.
func registerC2Tools(mcpServer *mcp.Server, c2Manager *c2.Manager, logger *zap.Logger, webListenPort int) {
	registerC2ListenerTool(mcpServer, c2Manager, logger, webListenPort)
	registerC2SessionTool(mcpServer, c2Manager, logger)
	registerC2TaskTool(mcpServer, c2Manager, logger)
	registerC2TaskManageTool(mcpServer, c2Manager, logger)
	registerC2PayloadTool(mcpServer, c2Manager, logger, webListenPort)
	registerC2EventTool(mcpServer, c2Manager, logger)
	registerC2ProfileTool(mcpServer, c2Manager, logger)
	registerC2FileTool(mcpServer, c2Manager, logger)
	logger.Info("C2 MCP tools registered (8 unified tools)")
}

func makeC2Result(data interface{}, err error) (*mcp.ToolResult, error) {
	if err != nil {
		return &mcp.ToolResult{
			Content: []mcp.Content{{Type: "text", Text: err.Error()}},
			IsError: true,
		}, nil
	}
	text, _ := json.Marshal(data)
	return &mcp.ToolResult{
		Content: []mcp.Content{{Type: "text", Text: string(text)}},
	}, nil
}

// ============================================================================
// c2_listener — Listener Unified Tool
// ============================================================================

func registerC2ListenerTool(s *mcp.Server, m *c2.Manager, l *zap.Logger, webListenPort int) {
	s.RegisterTool(mcp.Tool{
		Name: builtin.ToolC2Listener,
		Description: fmt.Sprintf(`C2 Listener Management. Select operation via action parameter:
- list: List all listeners
- get: Get listener details (requires listener_id)
- create: Create listener (requires name, type, bind_port). On success, returns listener + implant_token (only this once, for X-Implant-Token / oneliner; list/get/start will not return it)
- update: Update listener config (requires listener_id, optional: name/bind_host/bind_port/remark/config/callback_host)
- start: Start listener (requires listener_id)
- stop: Stop listener (requires listener_id)
- delete: Delete listener (requires listener_id)
Listener types: tcp_reverse, http_beacon, https_beacon, websocket
Port constraint: create/update bind_port must NOT be the same as the platform Web/API port. Currently this server port is %d (config server.port, loaded at startup). If bind_port conflicts, the server or listener will fail to bind, and Beacon/oneliner may connect to Web instead of C2. Choose a different free port for the listener.`, webListenPort),
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action":      map[string]interface{}{"type": "string", "description": "Operation: list/get/create/update/start/stop/delete", "enum": []string{"list", "get", "create", "update", "start", "stop", "delete"}},
				"listener_id": map[string]interface{}{"type": "string", "description": "Listener ID (required for get/update/start/stop/delete)"},
				"name":        map[string]interface{}{"type": "string", "description": "Listener name (create/update)"},
				"type":        map[string]interface{}{"type": "string", "description": "Listener type (create)", "enum": []string{"tcp_reverse", "http_beacon", "https_beacon", "websocket"}},
				"bind_host":     map[string]interface{}{"type": "string", "description": "Bind address, default 127.0.0.1; for external listening use 0.0.0.0"},
				"callback_host": map[string]interface{}{"type": "string", "description": "Optional: implant/Payload callback hostname (public IP or domain). Written into config_json; preferred over bind_host when generating oneliner/beacon. Pass empty string in update to clear"},
				"bind_port":   map[string]interface{}{"type": "integer", "description": fmt.Sprintf("Bind port (required for create). Must ≠ %d (current service Web/API port, config server.port)", webListenPort), "minimum": 1, "maximum": 65535},
				"profile_id":  map[string]interface{}{"type": "string", "description": "Malleable Profile ID"},
				"remark":      map[string]interface{}{"type": "string", "description": "Notes"},
				"config":      map[string]interface{}{"type": "object", "description": "Advanced config (beacon path/TLS/OPSEC etc.), available for create/update"},
			},
			"required": []string{"action"},
		},
	}, func(ctx context.Context, params map[string]interface{}) (*mcp.ToolResult, error) {
		action := getString(params, "action")
		id := getString(params, "listener_id")

		switch action {
		case "list":
			listeners, err := m.DB().ListC2Listeners()
			if err != nil {
				return makeC2Result(nil, err)
			}
			for _, li := range listeners {
				li.EncryptionKey = ""
				li.ImplantToken = ""
			}
			return makeC2Result(map[string]interface{}{"listeners": listeners, "count": len(listeners)}, nil)

		case "get":
			listener, err := m.DB().GetC2Listener(id)
			if err != nil {
				return makeC2Result(nil, err)
			}
			if listener == nil {
				return makeC2Result(nil, fmt.Errorf("listener not found"))
			}
			listener.EncryptionKey = ""
			listener.ImplantToken = ""
			return makeC2Result(map[string]interface{}{"listener": listener}, nil)

		case "create":
			var cfg *c2.ListenerConfig
			if cfgRaw, ok := params["config"]; ok && cfgRaw != nil {
				cfgBytes, _ := json.Marshal(cfgRaw)
				cfg = &c2.ListenerConfig{}
				_ = json.Unmarshal(cfgBytes, cfg)
			}
			input := c2.CreateListenerInput{
				Name:         getString(params, "name"),
				Type:         getString(params, "type"),
				BindHost:     getString(params, "bind_host"),
				BindPort:     int(getFloat64(params, "bind_port")),
				ProfileID:    getString(params, "profile_id"),
				Remark:       getString(params, "remark"),
				Config:       cfg,
				CallbackHost: getString(params, "callback_host"),
			}
			listener, err := m.CreateListener(input)
			if err != nil {
				return makeC2Result(nil, err)
			}
			implantToken := listener.ImplantToken
			listener.EncryptionKey = ""
			listener.ImplantToken = ""
			return makeC2Result(map[string]interface{}{
				"listener":      listener,
				"implant_token": implantToken,
			}, nil)

		case "update":
			listener, err := m.DB().GetC2Listener(id)
			if err != nil {
				return makeC2Result(nil, err)
			}
			if listener == nil {
				return makeC2Result(nil, fmt.Errorf("listener not found"))
			}
			if m.IsListenerRunning(id) {
				newHost := getString(params, "bind_host")
				newPort := int(getFloat64(params, "bind_port"))
				if (newHost != "" && newHost != listener.BindHost) || (newPort > 0 && newPort != listener.BindPort) {
					return makeC2Result(nil, fmt.Errorf("cannot modify bind address while listener is running"))
				}
			}
			if v := getString(params, "name"); v != "" {
				listener.Name = v
			}
			if v := getString(params, "bind_host"); v != "" {
				listener.BindHost = v
			}
			if v := int(getFloat64(params, "bind_port")); v > 0 {
				listener.BindPort = v
			}
			if v := getString(params, "profile_id"); v != "" {
				listener.ProfileID = v
			}
			if v, ok := params["remark"]; ok {
				listener.Remark, _ = v.(string)
			}
			if cfgRaw, ok := params["config"]; ok && cfgRaw != nil {
				cfgBytes, _ := json.Marshal(cfgRaw)
				listener.ConfigJSON = string(cfgBytes)
			}
			if _, ok := params["callback_host"]; ok {
				pcfg := &c2.ListenerConfig{}
				raw := strings.TrimSpace(listener.ConfigJSON)
				if raw == "" {
					raw = "{}"
				}
				_ = json.Unmarshal([]byte(raw), pcfg)
				pcfg.CallbackHost = strings.TrimSpace(getString(params, "callback_host"))
				pcfg.ApplyDefaults()
				cfgBytes, err := json.Marshal(pcfg)
				if err != nil {
					return makeC2Result(nil, err)
				}
				listener.ConfigJSON = string(cfgBytes)
			}
			if err := m.DB().UpdateC2Listener(listener); err != nil {
				return makeC2Result(nil, err)
			}
			listener.EncryptionKey = ""
			listener.ImplantToken = ""
			return makeC2Result(map[string]interface{}{"listener": listener}, nil)

		case "start":
			listener, err := m.StartListener(id)
			if err != nil {
				return makeC2Result(nil, err)
			}
			listener.EncryptionKey = ""
			listener.ImplantToken = ""
			return makeC2Result(map[string]interface{}{"listener": listener}, nil)

		case "stop":
			err := m.StopListener(id)
			return makeC2Result(map[string]interface{}{"stopped": err == nil}, err)

		case "delete":
			err := m.DeleteListener(id)
			return makeC2Result(map[string]interface{}{"deleted": err == nil}, err)

		default:
			return makeC2Result(nil, fmt.Errorf("unknown action: %s", action))
		}
	})
}

// ============================================================================
// ============================================================================
// c2_session — Session Unified Tool

func registerC2SessionTool(s *mcp.Server, m *c2.Manager, l *zap.Logger) {
	s.RegisterTool(mcp.Tool{
		Name: builtin.ToolC2Session,
		Description: `C2 Session Management. Select operation via action parameter:
- list: List sessions (filterable by listener_id/status/os/search)
- get: Get session details and recent task history (requires session_id)
- set_sleep: Set heartbeat interval (requires session_id)
- kill: Send exit task to terminate implant (requires session_id)
- delete: Delete session record (requires session_id)`,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action":         map[string]interface{}{"type": "string", "description": "Operation: list/get/set_sleep/kill/delete", "enum": []string{"list", "get", "set_sleep", "kill", "delete"}},
				"session_id":     map[string]interface{}{"type": "string", "description": "Session ID (required for get/set_sleep/kill/delete)"},
				"listener_id":    map[string]interface{}{"type": "string", "description": "Filter by listener (list)"},
				"status":         map[string]interface{}{"type": "string", "description": "Filter by status: active/sleeping/dead/killed (list)"},
				"os":             map[string]interface{}{"type": "string", "description": "Filter by OS: linux/windows/darwin (list)"},
				"search":         map[string]interface{}{"type": "string", "description": "Fuzzy search hostname/username/IP (list)"},
				"limit":          map[string]interface{}{"type": "integer", "description": "Max results (list)"},
				"sleep_seconds":  map[string]interface{}{"type": "integer", "description": "Heartbeat interval in seconds (set_sleep)"},
				"jitter_percent": map[string]interface{}{"type": "integer", "description": "Jitter percentage 0-100 (set_sleep)"},
			},
			"required": []string{"action"},
		},
	}, func(ctx context.Context, params map[string]interface{}) (*mcp.ToolResult, error) {
		action := getString(params, "action")
		id := getString(params, "session_id")

		switch action {
		case "list":
			filter := database.ListC2SessionsFilter{
				ListenerID: getString(params, "listener_id"),
				Status:     getString(params, "status"),
				OS:         getString(params, "os"),
				Search:     getString(params, "search"),
			}
			if limit := int(getFloat64(params, "limit")); limit > 0 {
				filter.Limit = limit
			}
			sessions, err := m.DB().ListC2Sessions(filter)
			return makeC2Result(map[string]interface{}{"sessions": sessions, "count": len(sessions)}, err)

		case "get":
			session, err := m.DB().GetC2Session(id)
			if err != nil {
				return makeC2Result(nil, err)
			}
			if session == nil {
				return makeC2Result(nil, fmt.Errorf("session not found"))
			}
			tasks, _ := m.DB().ListC2Tasks(database.ListC2TasksFilter{SessionID: id, Limit: 10})
			return makeC2Result(map[string]interface{}{"session": session, "tasks": tasks}, nil)

		case "set_sleep":
			sleep := int(getFloat64(params, "sleep_seconds"))
			jitter := int(getFloat64(params, "jitter_percent"))
			err := m.DB().SetC2SessionSleep(id, sleep, jitter)
			return makeC2Result(map[string]interface{}{"updated": err == nil, "sleep_seconds": sleep, "jitter_percent": jitter}, err)

		case "kill":
			task, err := m.EnqueueTask(c2.EnqueueTaskInput{
				SessionID:      id,
				TaskType:       c2.TaskTypeExit,
				Payload:        map[string]interface{}{},
				Source:         "ai",
				ConversationID: agent.ConversationIDFromContext(ctx),
				UserCtx:        ctx,
			})
			return makeC2Result(map[string]interface{}{"task": task}, err)

		case "delete":
			err := m.DB().DeleteC2Session(id)
			return makeC2Result(map[string]interface{}{"deleted": err == nil}, err)

		default:
			return makeC2Result(nil, fmt.Errorf("unknown action: %s", action))
		}
	})
}

// ============================================================================
// c2_task — Task Dispatch Unified Tool (merged all task types)
// ============================================================================

func registerC2TaskTool(s *mcp.Server, m *c2.Manager, l *zap.Logger) {
	s.RegisterTool(mcp.Tool{
		Name: builtin.ToolC2Task,
		Description: `Send task to C2 session. All task types specified via task_type parameter:
- exec: Execute command (requires command)
- shell: Interactive command, keeps cwd (requires command)
- pwd/ps/screenshot/socks_stop: No extra params
- cd/ls: Requires path
- kill_proc: Requires pid
- upload: Requires remote_path + file_id
- download: Requires remote_path
- port_fwd: Requires action(start/stop) + local_port + remote_host + remote_port
- socks_start: Requires port (default 1080)
- load_assembly: Requires data(base64) or file_id, optional args
- persist: Optional method(auto/cron/bashrc/launchagent/registry/schtasks)
Returns task_id. Use c2_task_manage wait/get_result to fetch results.`,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"session_id":      map[string]interface{}{"type": "string", "description": "C2 Session ID (s_xxx)"},
				"task_type":       map[string]interface{}{"type": "string", "description": "Task type", "enum": []string{"exec", "shell", "pwd", "cd", "ls", "ps", "kill_proc", "upload", "download", "screenshot", "port_fwd", "socks_start", "socks_stop", "load_assembly", "persist"}},
				"command":         map[string]interface{}{"type": "string", "description": "Command (exec/shell)"},
				"path":            map[string]interface{}{"type": "string", "description": "Path (cd/ls)"},
				"pid":             map[string]interface{}{"type": "integer", "description": "Process ID (kill_proc)"},
				"remote_path":     map[string]interface{}{"type": "string", "description": "Remote path (upload/download)"},
				"file_id":         map[string]interface{}{"type": "string", "description": "Server-side file ID (upload/load_assembly)"},
				"data":            map[string]interface{}{"type": "string", "description": "Base64 data (load_assembly)"},
				"args":            map[string]interface{}{"type": "string", "description": "Command-line arguments (load_assembly)"},
				"action":          map[string]interface{}{"type": "string", "description": "start/stop (port_fwd)"},
				"local_port":      map[string]interface{}{"type": "integer", "description": "Local port (port_fwd)"},
				"remote_host":     map[string]interface{}{"type": "string", "description": "Remote host (port_fwd)"},
				"remote_port":     map[string]interface{}{"type": "integer", "description": "Remote port (port_fwd)"},
				"port":            map[string]interface{}{"type": "integer", "description": "SOCKS5 port (socks_start), default 1080"},
				"method":          map[string]interface{}{"type": "string", "description": "Persistence method (persist): auto/cron/bashrc/launchagent/registry/schtasks"},
				"timeout_seconds": map[string]interface{}{"type": "integer", "description": "Timeout in seconds, default 60"},
			},
			"required": []string{"session_id", "task_type"},
		},
	}, func(ctx context.Context, params map[string]interface{}) (*mcp.ToolResult, error) {
		sessionID := getString(params, "session_id")
		taskTypeStr := getString(params, "task_type")
		taskType := c2.TaskType(taskTypeStr)
		timeout := getFloat64(params, "timeout_seconds")

		payload := map[string]interface{}{"timeout_seconds": timeout}

		switch taskType {
		case c2.TaskTypeExec, c2.TaskTypeShell:
			payload["command"] = getString(params, "command")
		case c2.TaskTypeCd, c2.TaskTypeLs:
			payload["path"] = getString(params, "path")
		case c2.TaskTypeKillProc:
			payload["pid"] = params["pid"]
		case c2.TaskTypeUpload:
			payload["remote_path"] = getString(params, "remote_path")
			payload["file_id"] = getString(params, "file_id")
		case c2.TaskTypeDownload:
			payload["remote_path"] = getString(params, "remote_path")
		case c2.TaskTypePortFwd:
			payload["action"] = getString(params, "action")
			payload["local_port"] = params["local_port"]
			payload["remote_host"] = getString(params, "remote_host")
			payload["remote_port"] = params["remote_port"]
		case c2.TaskTypeSocksStart:
			payload["port"] = params["port"]
		case c2.TaskTypeLoadAssembly:
			payload["data"] = getString(params, "data")
			payload["file_id"] = getString(params, "file_id")
			payload["args"] = getString(params, "args")
		case c2.TaskTypePersist:
			payload["method"] = getString(params, "method")
		case c2.TaskTypePwd, c2.TaskTypePs, c2.TaskTypeScreenshot, c2.TaskTypeSocksStop:
			// no extra params
		default:
			return makeC2Result(nil, fmt.Errorf("unsupported task_type: %s", taskTypeStr))
		}

		input := c2.EnqueueTaskInput{
			SessionID:      sessionID,
			TaskType:       taskType,
			Payload:        payload,
			Source:         "ai",
			ConversationID: agent.ConversationIDFromContext(ctx),
			UserCtx:        ctx,
		}
		task, err := m.EnqueueTask(input)
		if err != nil {
			return makeC2Result(nil, err)
		}
		return makeC2Result(map[string]interface{}{"task_id": task.ID, "status": task.Status}, nil)
	})
}

// ============================================================================
// c2_task_manage — Task Management Tool (query/wait/cancel)
// ============================================================================

func registerC2TaskManageTool(s *mcp.Server, m *c2.Manager, l *zap.Logger) {
	s.RegisterTool(mcp.Tool{
		Name: builtin.ToolC2TaskManage,
		Description: `C2 Task Management. Select operation via action parameter:
- get_result: Get task details and results (requires task_id)
- wait: Block until task completes and return result (requires task_id)
- list: List tasks (filterable by session_id/status)
- cancel: Cancel queued task (requires task_id)`,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action":          map[string]interface{}{"type": "string", "description": "Operation: get_result/wait/list/cancel", "enum": []string{"get_result", "wait", "list", "cancel"}},
				"task_id":         map[string]interface{}{"type": "string", "description": "Task ID (required for get_result/wait/cancel)"},
				"session_id":      map[string]interface{}{"type": "string", "description": "Filter by session (list)"},
				"status":          map[string]interface{}{"type": "string", "description": "Filter by status: queued/sent/running/success/failed/cancelled (list)"},
				"limit":           map[string]interface{}{"type": "integer", "description": "Max results (list)"},
				"timeout_seconds": map[string]interface{}{"type": "integer", "description": "Wait timeout in seconds (wait), default 60"},
			},
			"required": []string{"action"},
		},
	}, func(ctx context.Context, params map[string]interface{}) (*mcp.ToolResult, error) {
		action := getString(params, "action")

		switch action {
		case "get_result":
			id := getString(params, "task_id")
			task, err := m.DB().GetC2Task(id)
			if err != nil {
				return makeC2Result(nil, err)
			}
			if task == nil {
				return makeC2Result(nil, fmt.Errorf("task not found"))
			}
			return makeC2Result(map[string]interface{}{"task": task}, nil)

		case "wait":
			id := getString(params, "task_id")
			timeout := int(getFloat64(params, "timeout_seconds"))
			if timeout <= 0 {
				timeout = 60
			}
			deadline := time.Now().Add(time.Duration(timeout) * time.Second)
			for time.Now().Before(deadline) {
				task, err := m.DB().GetC2Task(id)
				if err != nil {
					return makeC2Result(nil, err)
				}
				if task == nil {
					return makeC2Result(nil, fmt.Errorf("task not found"))
				}
				if task.Status == "success" || task.Status == "failed" || task.Status == "cancelled" {
					return makeC2Result(map[string]interface{}{"task": task}, nil)
				}
				select {
				case <-time.After(500 * time.Millisecond):
				case <-ctx.Done():
					return makeC2Result(nil, ctx.Err())
				}
			}
			return makeC2Result(nil, fmt.Errorf("timeout waiting for task completion"))

		case "list":
			filter := database.ListC2TasksFilter{
				SessionID: getString(params, "session_id"),
				Status:    getString(params, "status"),
			}
			if limit := int(getFloat64(params, "limit")); limit > 0 {
				filter.Limit = limit
			}
			tasks, err := m.DB().ListC2Tasks(filter)
			return makeC2Result(map[string]interface{}{"tasks": tasks, "count": len(tasks)}, err)

		case "cancel":
			id := getString(params, "task_id")
			err := m.CancelTask(id)
			return makeC2Result(map[string]interface{}{"cancelled": err == nil}, err)

		default:
			return makeC2Result(nil, fmt.Errorf("unknown action: %s", action))
		}
	})
}

// ============================================================================
// c2_payload — Payload Unified Tool
// ============================================================================

func registerC2PayloadTool(s *mcp.Server, m *c2.Manager, l *zap.Logger, webListenPort int) {
	s.RegisterTool(mcp.Tool{
		Name: builtin.ToolC2Payload,
		Description: fmt.Sprintf(`C2 Payload Generation. Select operation via action parameter:
- oneliner: Generate one-liner payload. kind must match listener protocol, otherwise it will fail:
  • tcp_reverse: Raw TCP reverse shell, available kinds: bash, nc, nc_mkfifo, python, perl, powershell (bash refers to /dev/tcp style, not HTTP).
  • http_beacon / https_beacon / websocket: HTTP(S) Beacon polling only, oneliner can only use kind: curl_beacon (script uses bash+curl, different from TCP bash). curl_beacon output ends with " &" to background the entire bash -c; if using exec/execute synchronously, copy the entire string verbatim (including trailing &). Removing & causes the internal while loop to run forever in foreground, blocking the call until timeout/kill.
  • To get a classic bash reverse shell: first c2_listener create type=tcp_reverse, then use kind=bash on that listener.
  • When kind is omitted, the first compatible type is auto-selected based on listener type (HTTP defaults to curl_beacon).
- build: Cross-compile beacon binary. Supports http_beacon / https_beacon / websocket / tcp_reverse (tcp_reverse: implant sends magic CSB1 after connect, then uses same AES-GCM JSON semantics as HTTP; connections without magic are treated as classic interactive shell).
The listener's bind_port must avoid this service's web port %d (config server.port, consistent with c2_listener description), otherwise Beacon cannot connect back correctly.`, webListenPort),
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action":         map[string]interface{}{"type": "string", "description": "Operation: oneliner/build", "enum": []string{"oneliner", "build"}},
				"listener_id":    map[string]interface{}{"type": "string", "description": "Listener ID (required). Before oneliner, confirm listener type then choose compatible kind"},
				"kind":           map[string]interface{}{"type": "string", "description": "Only for action=oneliner. tcp_reverse: bash|nc|nc_mkfifo|python|perl|powershell; http_beacon|https_beacon|websocket: only curl_beacon"},
				"host":           map[string]interface{}{"type": "string", "description": "oneliner/build optional override: non-empty forces implant callback host. When empty: callback_host (from create/update) → bind_host (when 0.0.0.0, attempts external IP detection)"},
				"os":             map[string]interface{}{"type": "string", "description": "Target OS (build): linux/windows/darwin", "default": "linux"},
				"arch":           map[string]interface{}{"type": "string", "description": "Target architecture (build): amd64/arm64/386/arm", "default": "amd64"},
				"sleep_seconds":  map[string]interface{}{"type": "integer", "description": "Default heartbeat interval (build)"},
				"jitter_percent": map[string]interface{}{"type": "integer", "description": "Default jitter percentage (build)"},
			},
			"required": []string{"action", "listener_id"},
		},
	}, func(ctx context.Context, params map[string]interface{}) (*mcp.ToolResult, error) {
		action := getString(params, "action")
		listenerID := getString(params, "listener_id")

		switch action {
		case "oneliner":
			listener, err := m.DB().GetC2Listener(listenerID)
			if err != nil {
				return makeC2Result(nil, err)
			}
			if listener == nil {
				return makeC2Result(nil, fmt.Errorf("listener not found"))
			}
			host := c2.ResolveBeaconDialHost(listener, getString(params, "host"), l, listenerID)
			kind := c2.OnelinerKind(getString(params, "kind"))
			if kind == "" {
				compatible := c2.OnelinerKindsForListener(listener.Type)
				if len(compatible) > 0 {
					kind = compatible[0]
				}
			}
			if !c2.IsOnelinerCompatible(listener.Type, kind) {
				compatible := c2.OnelinerKindsForListener(listener.Type)
				names := make([]string, len(compatible))
				for i, k := range compatible {
					names[i] = string(k)
				}
				return makeC2Result(nil, fmt.Errorf("listener type %s does not support %s, compatible kinds: %v", listener.Type, kind, names))
			}
			input := c2.OnelinerInput{
				Kind:         kind,
				Host:         host,
				Port:         listener.BindPort,
				HTTPBaseURL:  fmt.Sprintf("http://%s:%d", host, listener.BindPort),
				ImplantToken: listener.ImplantToken,
			}
			oneliner, err := c2.GenerateOneliner(input)
			if err != nil {
				return makeC2Result(nil, err)
			}
			out := map[string]interface{}{
				"oneliner": oneliner, "kind": input.Kind, "host": host, "port": listener.BindPort,
			}
			if kind == c2.OnelinerCurl {
				out["usage_note"] = "Sync exec/execute: execute entire string verbatim (must have trailing \" &\"). Removing it causes infinite while loop, blocking the tool indefinitely."
			}
			return makeC2Result(out, nil)

		case "build":
			builder := c2.NewPayloadBuilder(m, l, "", "")
			input := c2.PayloadBuilderInput{
				ListenerID:    listenerID,
				OS:            getString(params, "os"),
				Arch:          getString(params, "arch"),
				SleepSeconds:  int(getFloat64(params, "sleep_seconds")),
				JitterPercent: int(getFloat64(params, "jitter_percent")),
				Host:          strings.TrimSpace(getString(params, "host")),
			}
			result, err := builder.BuildBeacon(input)
			if err != nil {
				return makeC2Result(nil, err)
			}
			return makeC2Result(map[string]interface{}{
				"payload_id": result.PayloadID, "download_path": result.DownloadPath,
				"os": result.OS, "arch": result.Arch, "size_bytes": result.SizeBytes,
			}, nil)

		default:
			return makeC2Result(nil, fmt.Errorf("unknown action: %s", action))
		}
	})
}

// ============================================================================
// c2_event — Event Query Tool
// ============================================================================

func registerC2EventTool(s *mcp.Server, m *c2.Manager, l *zap.Logger) {
	s.RegisterTool(mcp.Tool{
		Name:        builtin.ToolC2Event,
		Description: "Get C2 events (connect/disconnect/task/error), filterable by level/category/session/task/time",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"level":      map[string]interface{}{"type": "string", "description": "Level filter: info/warn/critical"},
				"category":   map[string]interface{}{"type": "string", "description": "Category filter: listener/session/task/payload/opsec"},
				"session_id": map[string]interface{}{"type": "string", "description": "Filter by session"},
				"task_id":    map[string]interface{}{"type": "string", "description": "Filter by task"},
				"since":      map[string]interface{}{"type": "string", "description": "Start time (RFC3339 format, e.g. 2025-01-01T00:00:00Z)"},
				"limit":      map[string]interface{}{"type": "integer", "default": 50, "description": "Max results"},
			},
		},
	}, func(ctx context.Context, params map[string]interface{}) (*mcp.ToolResult, error) {
		filter := database.ListC2EventsFilter{
			Level:     getString(params, "level"),
			Category:  getString(params, "category"),
			SessionID: getString(params, "session_id"),
			TaskID:    getString(params, "task_id"),
			Limit:     int(getFloat64(params, "limit")),
		}
		if filter.Limit <= 0 {
			filter.Limit = 50
		}
		if since := getString(params, "since"); since != "" {
			if t, err := time.Parse(time.RFC3339, since); err == nil {
				filter.Since = &t
			}
		}
		events, err := m.DB().ListC2Events(filter)
		return makeC2Result(map[string]interface{}{"events": events, "count": len(events)}, err)
	})
}

// ============================================================================
// c2_profile — Malleable Profile Management Tool (new)
// ============================================================================

func registerC2ProfileTool(s *mcp.Server, m *c2.Manager, l *zap.Logger) {
	s.RegisterTool(mcp.Tool{
		Name: builtin.ToolC2Profile,
		Description: `C2 Malleable Profile Management (controls beacon communication disguise). Select operation via action parameter:
- list: List all Profiles
- get: Get Profile details (requires profile_id)
- create: Create Profile (requires name, optional: user_agent/uris/request_headers/response_headers/body_template/jitter_min_ms/jitter_max_ms)
- update: Update Profile (requires profile_id)
- delete: Delete Profile (requires profile_id)`,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action":           map[string]interface{}{"type": "string", "description": "Operation: list/get/create/update/delete", "enum": []string{"list", "get", "create", "update", "delete"}},
				"profile_id":       map[string]interface{}{"type": "string", "description": "Profile ID (required for get/update/delete)"},
				"name":             map[string]interface{}{"type": "string", "description": "Profile name"},
				"user_agent":       map[string]interface{}{"type": "string", "description": "User-Agent string"},
				"uris":             map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "List of beacon request URIs"},
				"request_headers":  map[string]interface{}{"type": "object", "description": "Custom request headers"},
				"response_headers": map[string]interface{}{"type": "object", "description": "Custom response headers"},
				"body_template":    map[string]interface{}{"type": "string", "description": "Response body template"},
				"jitter_min_ms":    map[string]interface{}{"type": "integer", "description": "Minimum jitter (milliseconds)"},
				"jitter_max_ms":    map[string]interface{}{"type": "integer", "description": "Maximum jitter (milliseconds)"},
			},
			"required": []string{"action"},
		},
	}, func(ctx context.Context, params map[string]interface{}) (*mcp.ToolResult, error) {
		action := getString(params, "action")
		id := getString(params, "profile_id")

		switch action {
		case "list":
			profiles, err := m.DB().ListC2Profiles()
			return makeC2Result(map[string]interface{}{"profiles": profiles, "count": len(profiles)}, err)

		case "get":
			profile, err := m.DB().GetC2Profile(id)
			if err != nil {
				return makeC2Result(nil, err)
			}
			if profile == nil {
				return makeC2Result(nil, fmt.Errorf("profile not found"))
			}
			return makeC2Result(map[string]interface{}{"profile": profile}, nil)

		case "create":
			profile := &database.C2Profile{
				ID:           "p_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:14],
				Name:         getString(params, "name"),
				UserAgent:    getString(params, "user_agent"),
				BodyTemplate: getString(params, "body_template"),
				JitterMinMS:  int(getFloat64(params, "jitter_min_ms")),
				JitterMaxMS:  int(getFloat64(params, "jitter_max_ms")),
				CreatedAt:    time.Now(),
			}
			if uris, ok := params["uris"]; ok {
				if arr, ok := uris.([]interface{}); ok {
					for _, u := range arr {
						if s, ok := u.(string); ok {
							profile.URIs = append(profile.URIs, s)
						}
					}
				}
			}
			if rh, ok := params["request_headers"]; ok {
				if m, ok := rh.(map[string]interface{}); ok {
					profile.RequestHeaders = make(map[string]string)
					for k, v := range m {
						profile.RequestHeaders[k], _ = v.(string)
					}
				}
			}
			if rh, ok := params["response_headers"]; ok {
				if m, ok := rh.(map[string]interface{}); ok {
					profile.ResponseHeaders = make(map[string]string)
					for k, v := range m {
						profile.ResponseHeaders[k], _ = v.(string)
					}
				}
			}
			if err := m.DB().CreateC2Profile(profile); err != nil {
				return makeC2Result(nil, err)
			}
			return makeC2Result(map[string]interface{}{"profile": profile}, nil)

		case "update":
			profile, err := m.DB().GetC2Profile(id)
			if err != nil {
				return makeC2Result(nil, err)
			}
			if profile == nil {
				return makeC2Result(nil, fmt.Errorf("profile not found"))
			}
			if v := getString(params, "name"); v != "" {
				profile.Name = v
			}
			if v := getString(params, "user_agent"); v != "" {
				profile.UserAgent = v
			}
			if v := getString(params, "body_template"); v != "" {
				profile.BodyTemplate = v
			}
			if v := int(getFloat64(params, "jitter_min_ms")); v > 0 {
				profile.JitterMinMS = v
			}
			if v := int(getFloat64(params, "jitter_max_ms")); v > 0 {
				profile.JitterMaxMS = v
			}
			if uris, ok := params["uris"]; ok {
				if arr, ok := uris.([]interface{}); ok {
					profile.URIs = nil
					for _, u := range arr {
						if s, ok := u.(string); ok {
							profile.URIs = append(profile.URIs, s)
						}
					}
				}
			}
			if rh, ok := params["request_headers"]; ok {
				if mp, ok := rh.(map[string]interface{}); ok {
					profile.RequestHeaders = make(map[string]string)
					for k, v := range mp {
						profile.RequestHeaders[k], _ = v.(string)
					}
				}
			}
			if rh, ok := params["response_headers"]; ok {
				if mp, ok := rh.(map[string]interface{}); ok {
					profile.ResponseHeaders = make(map[string]string)
					for k, v := range mp {
						profile.ResponseHeaders[k], _ = v.(string)
					}
				}
			}
			if err := m.DB().UpdateC2Profile(profile); err != nil {
				return makeC2Result(nil, err)
			}
			return makeC2Result(map[string]interface{}{"profile": profile}, nil)

		case "delete":
			err := m.DB().DeleteC2Profile(id)
			return makeC2Result(map[string]interface{}{"deleted": err == nil}, err)

		default:
			return makeC2Result(nil, fmt.Errorf("unknown action: %s", action))
		}
	})
}

// ============================================================================
// c2_file — File Management Tool (new)
// ============================================================================

func registerC2FileTool(s *mcp.Server, m *c2.Manager, l *zap.Logger) {
	s.RegisterTool(mcp.Tool{
		Name: builtin.ToolC2File,
		Description: `C2 File Management. Select operation via action parameter:
- list: List file transfer records for a session (requires session_id)
- get_result: Get task result file path (screenshots etc., requires task_id)`,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action":     map[string]interface{}{"type": "string", "description": "Operation: list/get_result", "enum": []string{"list", "get_result"}},
				"session_id": map[string]interface{}{"type": "string", "description": "Session ID (required for list)"},
				"task_id":    map[string]interface{}{"type": "string", "description": "Task ID (required for get_result)"},
			},
			"required": []string{"action"},
		},
	}, func(ctx context.Context, params map[string]interface{}) (*mcp.ToolResult, error) {
		action := getString(params, "action")

		switch action {
		case "list":
			sessionID := getString(params, "session_id")
			if sessionID == "" {
				return makeC2Result(nil, fmt.Errorf("session_id required"))
			}
			files, err := m.DB().ListC2FilesBySession(sessionID)
			return makeC2Result(map[string]interface{}{"files": files, "count": len(files)}, err)

		case "get_result":
			taskID := getString(params, "task_id")
			task, err := m.DB().GetC2Task(taskID)
			if err != nil {
				return makeC2Result(nil, err)
			}
			if task == nil {
				return makeC2Result(nil, fmt.Errorf("task not found"))
			}
			if task.ResultBlobPath == "" {
				return makeC2Result(map[string]interface{}{"has_file": false, "task_id": taskID}, nil)
			}
			return makeC2Result(map[string]interface{}{
				"has_file":  true,
				"task_id":   taskID,
				"file_path": task.ResultBlobPath,
			}, nil)

		default:
			return makeC2Result(nil, fmt.Errorf("unknown action: %s", action))
		}
	})
}

// ============================================================================
// Utility Functions
// ============================================================================

func getString(params map[string]interface{}, key string) string {
	if v, ok := params[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getFloat64(params map[string]interface{}, key string) float64 {
	if v, ok := params[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		case string:
			if f, err := strconv.ParseFloat(n, 64); err == nil {
				return f
			}
		}
	}
	return 0
}
