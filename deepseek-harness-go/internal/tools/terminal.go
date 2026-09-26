// Package tools 包含 dsh 内置的工具；v6 P6-5 增加 terminal_* 工具。
//
//   - terminal_run   : 创建（或指定 id）Terminal 并执行一条命令
//   - terminal_read  : 读取 Terminal buffer
//   - terminal_kill  : 取消 Terminal 当前命令
//
// 工具共享同一个 terminal.Registry（main 启动时通过 SetTerminalRegistry 注入）。
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"deepseek-harness-go/internal/terminal"
	"deepseek-harness-go/internal/tool"
)

// terminalRegistryHolder 持有 terminal.Registry（v6 P6-5）。
type terminalRegistryHolder struct {
	mu sync.RWMutex
	r  *terminal.Registry
}

func (h *terminalRegistryHolder) Get() *terminal.Registry {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.r
}

func (h *terminalRegistryHolder) Set(r *terminal.Registry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.r = r
}

// SetTerminalRegistry 注入 terminal.Registry。
func SetTerminalRegistry(r *terminal.Registry) {
	globalTerminalRegistry.Set(r)
}

var globalTerminalRegistry = &terminalRegistryHolder{}

// TerminalRunTool 在 Terminal 上执行一条命令。
type TerminalRunTool struct{}

func NewTerminalRunTool() *TerminalRunTool { return &TerminalRunTool{} }
func (*TerminalRunTool) Name() string      { return "terminal_run" }
func (*TerminalRunTool) Description() string {
	return "在 Terminal 上执行一条命令；如果 id 留空则新建。返回 {id, exit, lines}。"
}
func (*TerminalRunTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":       map[string]any{"type": "string", "description": "留空 = 创建新 Terminal"},
			"shell":    map[string]any{"type": "string", "description": "cmd / powershell / bash / sh"},
			"command":  map[string]any{"type": "string"},
			"workdir":  map[string]any{"type": "string"},
			"owner":    map[string]any{"type": "string"},
			"timeout":  map[string]any{"type": "integer", "description": "超时毫秒数；0 表示不限"},
		},
		"required": []string{"command"},
	}
}

func (*TerminalRunTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	var a struct {
		ID      string `json:"id"`
		Shell   string `json:"shell"`
		Command string `json:"command"`
		Workdir string `json:"workdir"`
		Owner   string `json:"owner"`
		Timeout int    `json:"timeout"`
	}
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return tool.Err("invalid args: " + err.Error()), nil
	}
	if a.Command == "" {
		return tool.Err("command required"), nil
	}
	reg := globalTerminalRegistry.Get()
	if reg == nil {
		return tool.Err("terminal registry unavailable"), nil
	}

	var t *terminal.Terminal
	if a.ID != "" {
		tt, err := reg.Get(a.ID)
		if err != nil {
			return tool.Err(fmt.Sprintf("get: %s", err.Error())), nil
		}
		t = tt
	} else {
		shell := a.Shell
		if shell == "" {
			shell = "cmd"
		}
		tt, err := reg.Create(shell, nil, a.Workdir, a.Owner)
		if err != nil {
			return tool.Err(fmt.Sprintf("create: %s", err.Error())), nil
		}
		t = tt
	}

	timeout := time.Duration(a.Timeout) * time.Millisecond
	readOffBefore := 0 // 取全部新输出
	exit, err := t.Run(ctx, a.Command, timeout)
	if err != nil {
		// 即便错误也尝试返回 buffer
		lines := t.Read(0)
		b, _ := json.Marshal(map[string]any{"id": t.ID, "err": err.Error(), "lines": lines})
		return tool.Err(string(b)), nil
	}
	lines := t.Read(readOffBefore)
	b, _ := json.Marshal(map[string]any{"id": t.ID, "exit": exit, "lines": lines})
	return tool.Ok(string(b)), nil
}

// TerminalReadTool 读取 Terminal buffer。
type TerminalReadTool struct{}

func NewTerminalReadTool() *TerminalReadTool { return &TerminalReadTool{} }
func (*TerminalReadTool) Name() string       { return "terminal_read" }
func (*TerminalReadTool) Description() string {
	return "读取 Terminal 的输出 buffer；since 起始行号，limit 最多返回多少行。"
}
func (*TerminalReadTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":    map[string]any{"type": "string"},
			"since": map[string]any{"type": "integer"},
			"limit": map[string]any{"type": "integer"},
		},
		"required": []string{"id"},
	}
}

func (*TerminalReadTool) Execute(_ context.Context, raw json.RawMessage) (tool.Result, error) {
	var a struct {
		ID    string `json:"id"`
		Since int    `json:"since"`
		Limit int    `json:"limit"`
	}
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return tool.Err("invalid args: " + err.Error()), nil
	}
	if a.ID == "" {
		return tool.Err("id required"), nil
	}
	reg := globalTerminalRegistry.Get()
	if reg == nil {
		return tool.Err("terminal registry unavailable"), nil
	}
	t, err := reg.Get(a.ID)
	if err != nil {
		return tool.Err(fmt.Sprintf("get: %s", err.Error())), nil
	}
	lines := t.Read(a.Since)
	if a.Limit > 0 && len(lines) > a.Limit {
		lines = lines[len(lines)-a.Limit:]
	}
	b, _ := json.Marshal(map[string]any{"id": t.ID, "lines": lines, "count": len(lines)})
	return tool.Ok(string(b)), nil
}

// TerminalKillTool 取消 Terminal 当前命令。
type TerminalKillTool struct{}

func NewTerminalKillTool() *TerminalKillTool { return &TerminalKillTool{} }
func (*TerminalKillTool) Name() string       { return "terminal_kill" }
func (*TerminalKillTool) Description() string {
	return "取消 Terminal 当前正在运行的命令；幂等。"
}
func (*TerminalKillTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{"type": "string"},
		},
		"required": []string{"id"},
	}
}

func (*TerminalKillTool) Execute(_ context.Context, raw json.RawMessage) (tool.Result, error) {
	var a struct {
		ID string `json:"id"`
	}
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return tool.Err("invalid args: " + err.Error()), nil
	}
	if a.ID == "" {
		return tool.Err("id required"), nil
	}
	reg := globalTerminalRegistry.Get()
	if reg == nil {
		return tool.Err("terminal registry unavailable"), nil
	}
	if err := reg.Kill(a.ID); err != nil {
		return tool.Err(fmt.Sprintf("kill: %s", err.Error())), nil
	}
	return tool.Ok(`{"ok":true}`), nil
}
