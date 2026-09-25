//go:build windows

package sandbox

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestHelperChild 是受限令牌子进程测试的 helper：打印标记后返回。
func TestHelperChild(t *testing.T) {
	if os.Getenv("SB_CHILD") == "1" {
		os.Stdout.WriteString("sandbox-ok\n")
		return
	}
	t.Skip("helper only")
}

// TestWindowsACLApplyCreatesRestrictedToken 验收 §F.5（windows）：
// Apply 能创建受限令牌并设置到 SysProcAttr；随后用该令牌真正启动
// 一个子进程（测试二进制自身，不在敏感路径上），验证受限令牌下
// 进程可用。
func TestWindowsACLApplyCreatesRestrictedToken(t *testing.T) {
	s := NewWindowsACL()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("executable: %v", err)
	}
	cmd := exec.Command(exe, "-test.run=^TestHelperChild$")
	cmd.Env = append(os.Environ(), "SB_CHILD=1")
	if err := s.Apply(context.Background(), cmd); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.Token == 0 {
		t.Fatal("SysProcAttr.Token not set")
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run under restricted token: %v (%s)", err, out)
	}
	if !strings.Contains(string(out), "sandbox-ok") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestWindowsACLValidate(t *testing.T) {
	s := NewWindowsACL()
	if err := s.Validate(`C:\Windows\System32\drivers\etc\hosts`); err == nil {
		t.Fatal("sensitive path should be denied")
	}
	if err := s.Validate(t.TempDir()); err != nil {
		t.Fatalf("normal path: %v", err)
	}
}
