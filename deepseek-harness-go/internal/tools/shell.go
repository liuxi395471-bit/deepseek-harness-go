// Package tools 包含 dsh 内置的工具。shell 工具（v2）通过允许列表
// 和 Approver 执行命令。
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"deepseek-harness-go/internal/approval"
	"deepseek-harness-go/internal/tool"
)

// ShellConfig 是 shell 工具的 YAML 驱动配置。
type ShellConfig struct {
	Enabled         bool          `yaml:"enabled"`
	Approver        string        `yaml:"approver"` // noop | allowlist | terminal | http
	AllowList       []string      `yaml:"allowlist"`
	Timeout         time.Duration `yaml:"timeout"`
	MaxOutputBytes  int           `yaml:"max-output-bytes"`
	WorkspaceRoot   string        `yaml:"workspace"`
}

// ShellTool 在 harness 的策略约束下安全地执行操作系统命令。
//
// 策略（DESIGN-v2 §C.3）：
//
//  1. 只有第一个参数（命令名）会与 AllowList 匹配。
//  2. 每个路径参数都相对于 WorkspaceRoot 解析；目录穿越尝试
//     （逃逸出工作区的相对路径）会被拒绝。
//  3. 审批：执行前会咨询所配置的 Approver。Deny 决定会产生
//     tool.Result{IsError: true}。
//  4. 输出被截断到 MaxOutputBytes（默认 1 MiB），并附带截断标记。
type ShellTool struct {
	cfg     ShellConfig
	approver approval.Approver
}

// NewShellTool 构造一个 ShellTool。approver 可以为 nil；此时工具
// 默认使用 NoopApprover（相当于禁用审批）。
func NewShellTool(cfg ShellConfig, approver approval.Approver) *ShellTool {
	if approver == nil {
		approver = approval.NoopApprover{}
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = 1 << 20 // 1 MiB
	}
	if cfg.WorkspaceRoot == "" {
		cfg.WorkspaceRoot = "."
	}
	return &ShellTool{cfg: cfg, approver: approver}
}

// Name 返回工具的注册名。
func (s *ShellTool) Name() string { return "shell" }

// Description 返回供 LLM 使用的简短人类可读描述。
func (s *ShellTool) Description() string {
	return "Execute a shell command. Args: {cmd, args[], cwd?}. Subject to allowlist and approval."
}

// Parameters 返回工具参数的 JSON Schema。
func (s *ShellTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"cmd":  map[string]any{"type": "string", "description": "Executable name; must be in allowlist."},
			"args": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"cwd":  map[string]any{"type": "string", "description": "Working dir; defaults to workspace root."},
		},
		"required": []string{"cmd"},
	}
}

// shellArgs 是一次性解析的 JSON 结构。
type shellArgs struct {
	Cmd  string   `json:"cmd"`
	Args []string `json:"args"`
	Cwd  string   `json:"cwd"`
}

// Execute 在所有策略约束下运行命令。
func (s *ShellTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	var a shellArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return tool.Err("shell: invalid args: " + err.Error()), nil
	}
	if a.Cmd == "" {
		return tool.Err("shell: cmd required"), nil
	}

	// 1. 允许列表检查。
	if !s.allowed(a.Cmd) {
		return tool.Err(fmt.Sprintf("shell: command %q not in allowlist", a.Cmd)), nil
	}

	// 2. 对看起来像路径的参数做路径解析。
	for _, arg := range a.Args {
		if looksLikePath(arg) {
			if err := s.resolveInWorkspace(arg); err != nil {
				return tool.Err("shell: " + err.Error()), nil
			}
		}
	}

	// 3. 审批。
	dec, err := s.approver.Approve(ctx, approval.Request{
		Tool:   s.Name(),
		Args:   raw,
		Reason: fmt.Sprintf("execute %s with %d args", a.Cmd, len(a.Args)),
	})
	if err != nil {
		return tool.Err("shell: approval error: " + err.Error()), nil
	}
	if dec == approval.Deny {
		return tool.Err("shell: denied by approver"), nil
	}

	// 4. 带超时运行。
	cwd := a.Cwd
	if cwd == "" {
		cwd = s.cfg.WorkspaceRoot
	} else if !filepath.IsAbs(cwd) {
		cwd = filepath.Join(s.cfg.WorkspaceRoot, cwd)
	}
	cmd := exec.CommandContext(ctx, a.Cmd, a.Args...)
	cmd.Dir = cwd
	// 继承一个最小化的环境：unix 下只有 PATH + HOME；保持精简
	// 以避免泄露机密信息。
	cmd.Env = minimalEnv()

	output, runErr := cmd.CombinedOutput()
	if int64(len(output)) > int64(s.cfg.MaxOutputBytes) {
		output = append(output[:s.cfg.MaxOutputBytes], []byte("\n[...truncated]")...)
	}
	if runErr != nil {
		// exec.ExitError：非零退出码；不是 panic，以 Result.IsError 呈现。
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return tool.Result{
				Content: string(output),
				IsError: true,
			}, nil
		}
		// 其他错误（例如 context 被取消）：作为真正的 Go error 返回。
		return tool.Err("shell: " + runErr.Error()), nil
	}
	return tool.Ok(string(output)), nil
}

// allowed 返回 cmd 是否在配置的允许列表中。空允许列表意味着
// "默认拒绝所有命令"。
func (s *ShellTool) allowed(cmd string) bool {
	if len(s.cfg.AllowList) == 0 {
		return false
	}
	for _, allowed := range s.cfg.AllowList {
		if cmd == allowed || cmd == filepath.Base(allowed) {
			return true
		}
	}
	return false
}

// looksLikePath 当 arg 以 `/`、`./`、`../`、`~\` 开头，或包含路径
// 分隔符且当前操作系统将其视为路径分隔符时，返回 true。
func looksLikePath(arg string) bool {
	if arg == "" {
		return false
	}
	if arg[0] == '/' || arg[0] == '~' {
		return true
	}
	if strings.HasPrefix(arg, "./") || strings.HasPrefix(arg, "../") {
		return true
	}
	return false
}

// resolveInWorkspace 确保（相对）path 解析后位于 WorkspaceRoot 内。
// 位于工作区之外的绝对路径会被拒绝。
func (s *ShellTool) resolveInWorkspace(path string) error {
	root, err := filepath.Abs(s.cfg.WorkspaceRoot)
	if err != nil {
		return fmt.Errorf("workspace root: %w", err)
	}
	var abs string
	if filepath.IsAbs(path) {
		abs = filepath.Clean(path)
	} else {
		abs, err = filepath.Abs(filepath.Join(root, path))
		if err != nil {
			return fmt.Errorf("resolve %q: %w", path, err)
		}
	}
	// 出于安全考虑解析符号链接；若最终目标位于 root 之外则拒绝。
	// EvalSymlinks 的错误被区分处理：
	//   - ErrNotExist：对尚不存在的参数路径来说没问题（写场景由
	//     fs_path.go 处理）；对 shell 参数而言路径可能合法地不存在
	//     （例如重定向目标）。此时保持 abs 原样。
	//   - 其他错误（权限、悬空链接）：按 fail-closed 处理，而不是
	//     静默放行未解析的路径。
	target, err := filepath.EvalSymlinks(abs)
	switch {
	case err == nil:
		abs = target
	case errors.Is(err, fs.ErrNotExist):
		// 保持 abs —— 调用方（shell）只需知道*意图*指向工作区内
	default:
		return fmt.Errorf("resolve symlink %q: %w", path, err)
	}
	// 通过 rel 与 root 比较；在 Windows 上两者都已归一化为正斜杠。
	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
		return fmt.Errorf("path %q escapes workspace", path)
	}
	// 验证工作区根目录确实存在。
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("workspace root %q does not exist", root)
		}
		return err
	}
	return nil
}

// minimalEnv 返回一个供子进程使用的精简环境。
func minimalEnv() []string {
	env := []string{"PATH=/usr/local/bin:/usr/bin:/bin"}
	if home, err := userHome(); err == nil {
		env = append(env, "HOME="+home)
	}
	return env
}

// userHome 对 os.UserHomeDir 做了一层抽象，便于测试。
var userHome = func() (string, error) { return os.UserHomeDir() }
