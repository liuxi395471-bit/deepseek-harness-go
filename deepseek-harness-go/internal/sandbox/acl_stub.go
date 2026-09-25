//go:build !windows

package sandbox

// NewWindowsACL 在非 Windows 平台不可用；New() 已按 runtime.GOOS
// 提前拒绝，这里只为让 sandbox.go 在所有平台都能编译。
func NewWindowsACL() Sandbox { return NoopSandbox{} }
