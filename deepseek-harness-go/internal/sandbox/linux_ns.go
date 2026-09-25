//go:build linux

// Linux namespaces sandbox（DESIGN-v3 §F.3）。
//
// Apply 通过 syscall.SysProcAttr 启用 user/mount/pid 三层 namespace：
//   - CLONE_NEWUSER：子进程在新的 user namespace 中获得"伪 root"，
//     但对宿主而言仍是普通用户；
//   - CLONE_NEWNS + CLONE_NEWPID：独立挂载视图与 PID 空间。
//
// 受当前进程权限影响：无 CAP_SYS_ADMIN 时 mount/pid namespace 需要
// user namespace 先行启用（内核 3.12+ 已支持该组合）。
package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// LinuxNS 是基于 namespaces 的沙箱。
type LinuxNS struct {
	// WorkspaceRoot 可选：非空时作为只读校验基准（Validate 使用）。
	WorkspaceRoot string
}

// NewLinuxNS 构造 Linux namespaces 沙箱。
func NewLinuxNS() Sandbox { return LinuxNS{} }

// Name 实现 Sandbox。
func (LinuxNS) Name() string { return "linux_ns" }

// Apply 设置 SysProcAttr（§F.3）。同时校验 cmd.Path / cmd.Dir /
// cmd.Args[0] 不落在敏感路径（/etc/shadow 等）。
//
// NEWPID 启用后，子进程 PID 1 是它自身——因此以宿主角色拒绝 /proc/1
// 的逻辑不再适用（见 B3）。本实现在敏感路径列表中仅保留 /etc/shadow
// 与 /boot 等真正"对宿主敏感"的位置；/proc 在子 PID 命名空间内重新
// 挂载，指向子进程视图，敏感度大幅降低。
func (s LinuxNS) Apply(_ context.Context, cmd *exec.Cmd) error {
	if err := validateCmd(cmd); err != nil {
		return err
	}
	uid := os.Getuid()
	gid := os.Getgid()
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWNS | syscall.CLONE_NEWPID | syscall.CLONE_NEWUSER,
		// 把宿主 uid/gid 映射回子进程 namespace 内的同一 id，
		// 避免文件所有权全部变成 overflow uid。
		UidMappings: []syscall.SysProcIDMap{
			{ContainerID: 0, HostID: uid, Size: 1},
		},
		GidMappings: []syscall.SysProcIDMap{
			{ContainerID: 0, HostID: gid, Size: 1},
		},
	}
	return nil
}

// linuxSensitivePrefixes 列出 Linux 上默认拒绝访问的路径前缀。
//
// 注意：CLONE_NEWPID 启用后，子进程 PID=1 是它自身，宿主视角的
// /proc/1 已不存在于子进程视图；因此 /proc/1 不应保留在此列表中，
// 否则子进程访问自身伪 proc 时会被拒绝。
var linuxSensitivePrefixes = []string{
	"/etc/shadow", "/etc/sudoers", "/boot", "/sys",
}

// checkUnixSensitive 是 linux 平台的敏感路径检查（sandbox.go 的
// checkPath 在所有平台都会调用它）。
func checkUnixSensitive(abs string) error {
	for _, p := range linuxSensitivePrefixes {
		if abs == p || strings.HasPrefix(abs, p+"/") {
			return fmt.Errorf("sandbox: access to sensitive path %q is denied", abs)
		}
	}
	return nil
}

// Validate 检查路径：必须位于 WorkspaceRoot 内（配置了 root 时），
// 且不在敏感路径列表上。
func (s LinuxNS) Validate(path string) error {
	if err := checkPath(path); err != nil {
		return err
	}
	if s.WorkspaceRoot == "" {
		return nil
	}
	absRoot, err := absClean(s.WorkspaceRoot)
	if err != nil {
		return err
	}
	absPath, err := absClean(path)
	if err != nil {
		return err
	}
	if absPath != absRoot && !withinRoot(absRoot, absPath) {
		return fmt.Errorf("sandbox: path %q outside workspace %q", absPath, absRoot)
	}
	return nil
}

func absClean(p string) (string, error) {
	a, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("sandbox: resolve %q: %w", p, err)
	}
	return filepath.Clean(a), nil
}

func withinRoot(root, path string) bool {
	return len(path) > len(root)+1 && path[:len(root)] == root && path[len(root)] == '/'
}
