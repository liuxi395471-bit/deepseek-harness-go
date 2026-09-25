//go:build windows

// Windows ACL sandbox（DESIGN-v3 §F.2）。
//
// Apply 做两件事：
//  1. 用 advapi32!CreateRestrictedToken 从当前进程主令牌派生一个
//     DISABLE_MAX_PRIVILEGE 的受限令牌，并把它设为子进程令牌
//     （exec.Cmd.SysProcAttr.Token）——子进程将失去所有特权。
//  2. 校验 cmd.Path / cmd.Dir / cmd.Args[0] 不落在敏感路径
//     （System32 等）上。
//
// 受限令牌创建失败（例如被组策略禁止）时 Apply 返回 error，
// 由调用方以 Result{IsError:true} 呈现，绝不静默降级。
package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WindowsACL 是 Windows 受限令牌沙箱。
type WindowsACL struct{}

// tokenRegistry 跟踪 Apply 创建的受限 Token 句柄，由 shell 工具在
// cmd 结束后通过 ReleaseToken 回收。键为 *exec.Cmd 指针（每个子进程
// 命令对象唯一）。
var tokenRegistry sync.Map // map[*exec.Cmd]windows.Token

// RegisterToken 登记 token 与 cmd 的绑定关系，shell 工具执行后
// 通过 ReleaseToken(cmd) 关闭句柄。
func RegisterToken(cmd *exec.Cmd, tok windows.Token) {
	if cmd == nil || tok == 0 {
		return
	}
	tokenRegistry.Store(cmd, tok)
}

// ReleaseToken 关闭 cmd 对应的受限 token 句柄并解除绑定。重复调用
// 安全；不存在时返回 false。
func ReleaseToken(cmd *exec.Cmd) bool {
	if cmd == nil {
		return false
	}
	v, ok := tokenRegistry.LoadAndDelete(cmd)
	if !ok {
		return false
	}
	tok, _ := v.(windows.Token)
	if tok != 0 {
		_ = tok.Close()
	}
	return true
}

// NewWindowsACL 构造 Windows ACL 沙箱。
func NewWindowsACL() Sandbox { return WindowsACL{} }

// Name 实现 Sandbox。
func (WindowsACL) Name() string { return "windows_acl" }

// Apply 设置受限令牌与敏感路径校验。
//
// 路径校验覆盖 cmd.Path / cmd.Dir / cmd.Args[0]（绝对路径形式时），
// 防止 allowlist 仅按名称放行、却被 Args 引导到敏感路径的命令。
//
// 句柄回收：受限 Token 必须 Close，否则每次子进程泄漏一个 handle。
// 本实现在 Apply 成功后通过 sandbox.RegisterToken(cmd, token) 注册；
// shell 工具在 cmd.Wait 后调用 sandbox.ReleaseToken(cmd) 回收。
func (w WindowsACL) Apply(_ context.Context, cmd *exec.Cmd) error {
	if err := validateCmd(cmd); err != nil {
		return err
	}
	token, err := restrictedToken()
	if err != nil {
		return fmt.Errorf("sandbox: restricted token: %w", err)
	}
	// x/sys/windows 的 SysProcAttr 是 syscall.SysProcAttr 的别名，
	// 其 Token 字段是 syscall.Token；两者底层类型一致，需显式转换。
	cmd.SysProcAttr = &windows.SysProcAttr{
		Token:         syscall.Token(token),
		HideWindow:    true,
		CreationFlags: windows.CREATE_DEFAULT_ERROR_MODE,
	}
	RegisterToken(cmd, token)
	return nil
}

// Validate 实现路径检查。
func (WindowsACL) Validate(path string) error { return checkPath(path) }

// ReleaseToken 关闭 cmd 对应的受限 token 句柄并解除绑定。
// 满足 sandbox.TokenReleaser。重复调用安全；无登记时 no-op。
func (WindowsACL) ReleaseToken(cmd *exec.Cmd) { ReleaseToken(cmd) }

const (
	// TokenRestricted 是 CreateRestrictedToken 的 flags：
	// DISABLE_MAX_PRIVILEGE。
	tokDisableMaxPrivilege = 0x1
)

var (
	advapi32                = windows.NewLazySystemDLL("advapi32.dll")
	procCreateRestrictedToken = advapi32.NewProc("CreateRestrictedToken")
)

// restrictedToken 从当前进程主令牌派生一个去掉全部特权的受限令牌。
// 返回的 Token 由调用方负责 Close（exec.Cmd 在启动后不再引用它时）。
func restrictedToken() (windows.Token, error) {
	var primary windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_DUPLICATE|windows.TOKEN_ASSIGN_PRIMARY|windows.TOKEN_QUERY, &primary); err != nil {
		return 0, fmt.Errorf("open process token: %w", err)
	}
	defer primary.Close()

	// CreateRestrictedToken(
	//   PrimaryToken, Flags, NbrOfSidToDisable, SidsToDisable,
	//   DeletePrivilegeCount, PrivilegesToDelete,
	//   RestrictedSidCount, SidsToRestrict, &NewToken)
	var restricted windows.Token
	ret, _, callErr := procCreateRestrictedToken.Call(
		uintptr(primary),
		tokDisableMaxPrivilege,
		0, 0, // SIDs to disable
		0, 0, // privileges to delete
		0, 0, // SIDs to restrict（空 = 仅限 Everyone/受限组）
		uintptr(unsafe.Pointer(&restricted)),
	)
	if ret == 0 {
		return 0, fmt.Errorf("CreateRestrictedToken: %v", callErr)
	}
	return restricted, nil
}
