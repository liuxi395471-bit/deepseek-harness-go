// Package sandbox 实现 DESIGN-v3 §F：OS 级子进程沙箱。
//
// Sandbox 在 shell 工具执行前对 *exec.Cmd 应用 OS 约束（§F.4）。
// 平台实现用 build tag 隔离：
//
//	windows_acl.go  //go:build windows   — 受限令牌 + 敏感路径拒绝
//	linux_ns.go     //go:build linux     — namespaces（user/mount/pid）
//
// 默认 NoopSandbox 与 v2 行为一致（§F.5）。
package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Sandbox 是平台沙箱契约（§F.1）。
type Sandbox interface {
	Name() string
	// Apply 在 cmd.Start 之前设置 cmd.SysProcAttr 等 OS 约束。
	// 返回 error 时工具应以 Result{IsError:true} 呈现。
	Apply(ctx context.Context, cmd *exec.Cmd) error
	// Validate 检查路径是否允许在沙箱内访问。
	Validate(path string) error
}

// TokenReleaser 是可选的资源回收接口：实现 Apply 时若分配了需要
// 在 cmd 退出后显式释放的 OS 资源（如 windows_acl 的受限 Token
// 句柄），应同时实现此接口。shell 工具在 cmd.CombinedOutput 之后
// 调用 ReleaseToken 回收。Sandbox 不实现此接口时不释放任何资源。
type TokenReleaser interface {
	ReleaseToken(cmd *exec.Cmd)
}

// NoopSandbox 是默认实现：与 v2 行为一致，不做任何额外约束。
type NoopSandbox struct{}

// Name 实现 Sandbox。
func (NoopSandbox) Name() string { return "noop" }

// Apply 不做任何事。
func (NoopSandbox) Apply(context.Context, *exec.Cmd) error { return nil }

// Validate 不做任何事。
func (NoopSandbox) Validate(string) error { return nil }

// New 按 provider 构造沙箱（§F.4）。"auto" 按编译目标平台选择
// windows_acl / linux_ns；未知 provider 返回错误。
func New(provider string) (Sandbox, error) {
	switch provider {
	case "", "noop":
		return NoopSandbox{}, nil
	case "auto":
		if runtime.GOOS == "windows" {
			return NewWindowsACL(), nil
		}
		if runtime.GOOS == "linux" {
			return NewLinuxNS(), nil
		}
		return NoopSandbox{}, nil
	case "windows_acl":
		if runtime.GOOS != "windows" {
			return nil, fmt.Errorf("sandbox: provider windows_acl requires GOOS=windows (got %s)", runtime.GOOS)
		}
		return NewWindowsACL(), nil
	case "linux_ns":
		if runtime.GOOS != "linux" {
			return nil, fmt.Errorf("sandbox: provider linux_ns requires GOOS=linux (got %s)", runtime.GOOS)
		}
		return NewLinuxNS(), nil
	default:
		return nil, fmt.Errorf("sandbox: unknown provider %q (supported: noop, auto, windows_acl, linux_ns)", provider)
	}
}

// sensitivePathPrefixes 列出 Windows 上默认拒绝访问的路径前缀。
var sensitivePathPrefixes = []string{
	`c:\windows`,
	`c:\program files`,
	`c:\program files (x86)`,
	`c:\programdata`,
}

// checkPath 是平台相关的敏感路径检查：Windows 前缀在所有平台
// 检查（路径均归一化为小写）；Unix 敏感路径只在 linux 平台检查
// （其他平台上以 / 开头的路径会被 filepath.Abs 加盘符前缀，
// 语义已不同）。
func checkPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("sandbox: resolve %q: %w", path, err)
	}
	lower := strings.ToLower(abs)
	for _, p := range sensitivePathPrefixes {
		if strings.HasPrefix(lower, p) {
			return fmt.Errorf("sandbox: access to sensitive path %q is denied", abs)
		}
	}
	return checkUnixSensitive(abs)
}

// validateCmd 对 cmd 涉及的所有可执行路径做敏感路径校验（覆盖
// cmd.Path / cmd.Dir / cmd.Args[0] 的绝对路径形式）。非平台特定
// 实现：所有平台都复用，避免 sandbox provider 各自重复实现时
// 行为漂移。
func validateCmd(cmd *exec.Cmd) error {
	if cmd == nil {
		return nil
	}
	if cmd.Path != "" {
		if err := checkPath(cmd.Path); err != nil {
			return err
		}
	}
	if cmd.Dir != "" {
		if err := checkPath(cmd.Dir); err != nil {
			return err
		}
	}
	if len(cmd.Args) > 0 {
		arg0 := cmd.Args[0]
		if arg0 != "" && isAbsolutePath(arg0) {
			cleanArg := filepath.Clean(arg0)
			cleanPath := filepath.Clean(cmd.Path)
			if !pathEqual(cleanArg, cleanPath) {
				if err := checkPath(arg0); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// isAbsolutePath 跨平台判断路径是否为绝对路径。
func isAbsolutePath(p string) bool {
	if len(p) >= 2 && p[1] == ':' {
		return true // Windows 盘符（C:, D:）
	}
	if len(p) >= 1 && (p[0] == '/' || p[0] == '\\') {
		return true // Unix / UNC
	}
	return false
}

// pathEqual 在 Windows 上做大小写不敏感比较。
func pathEqual(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
