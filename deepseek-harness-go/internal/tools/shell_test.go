package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"deepseek-harness-go/internal/approval"
)

// 1. 允许列表拒绝未列出的命令。
func TestShell_AllowlistDenies(t *testing.T) {
	tmp := t.TempDir()
	st := NewShellTool(ShellConfig{
		WorkspaceRoot: tmp,
		AllowList:     []string{"ls"},
	}, nil)
	res, err := st.Execute(context.Background(), []byte(`{"cmd":"rm"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Errorf("expected IsError, got %+v", res)
	}
	if !strings.Contains(res.Content, "not in allowlist") {
		t.Errorf("content = %q, want allowlist message", res.Content)
	}
}

// 2. 审批：Deny → IsError 为 true。
func TestShell_ApprovalDeny(t *testing.T) {
	tmp := t.TempDir()
	st := NewShellTool(ShellConfig{
		WorkspaceRoot: tmp,
		AllowList:     []string{"ls"},
	}, approval.AllowListDenyAll())
	res, err := st.Execute(context.Background(), []byte(`{"cmd":"ls"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Errorf("deny should produce IsError, got %+v", res)
	}
}

// 3. ApproveSession 被缓存后 → 无需再次提示即可执行。
func TestShell_ApprovalSessionCached(t *testing.T) {
	tmp := t.TempDir()
	inner := &countingApprover{}
	cached := approval.NewCachingApprover(inner)
	st := NewShellTool(ShellConfig{
		WorkspaceRoot: tmp,
		AllowList:     []string{"ls"},
	}, cached)
	// 第一次调用：会咨询 inner，返回 ApproveSession → 被缓存。
	// 在一个真实存在的目录上使用 "ls"。
	subdir := filepath.Join(tmp, "subdir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"cmd":"ls","args":["subdir"]}`)
	if _, err := st.Execute(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	if inner.calls != 1 {
		t.Errorf("inner called %d times, want 1", inner.calls)
	}
	// 第二次调用：命中缓存；inner 不再被调用。
	if _, err := st.Execute(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	if inner.calls != 1 {
		t.Errorf("inner called %d times after cache, want 1", inner.calls)
	}
}

// 4. 路径解析：逃逸工作区的路径被拒绝。
func TestShell_PathEscapeRejected(t *testing.T) {
	tmp := t.TempDir()
	st := NewShellTool(ShellConfig{
		WorkspaceRoot: tmp,
		AllowList:     []string{"ls"},
	}, nil)
	res, _ := st.Execute(context.Background(), []byte(`{"cmd":"ls","args":["../etc"]}`))
	if !res.IsError {
		t.Errorf("path escape should fail: %+v", res)
	}
	if !strings.Contains(res.Content, "escapes workspace") {
		t.Errorf("content = %q", res.Content)
	}
}

// 5. 无效的参数 JSON。
func TestShell_InvalidArgs(t *testing.T) {
	st := NewShellTool(ShellConfig{WorkspaceRoot: t.TempDir(), AllowList: []string{"x"}}, nil)
	res, err := st.Execute(context.Background(), []byte(`{not json`))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Errorf("invalid args should produce IsError")
	}
}

// 6. 缺少 cmd。
func TestShell_MissingCmd(t *testing.T) {
	st := NewShellTool(ShellConfig{WorkspaceRoot: t.TempDir(), AllowList: []string{"x"}}, nil)
	res, _ := st.Execute(context.Background(), []byte(`{}`))
	if !res.IsError {
		t.Errorf("missing cmd should produce IsError")
	}
}

// 7. 非零退出码 → IsError 为 true，但不返回 Go error。
func TestShell_NonZeroExit(t *testing.T) {
	tmp := t.TempDir()
	st := NewShellTool(ShellConfig{
		WorkspaceRoot: tmp,
		AllowList:     []string{"go"},
	}, approval.NoopApprover{})
	// 运行不带参数的 "go" 以触发非零退出码。
	res, err := st.Execute(context.Background(), []byte(`{"cmd":"go"}`))
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Errorf("non-zero exit should be IsError: %+v", res)
	}
}

// 8. 通过链接目标逃逸出工作区的路径符号链接。
// 针对 P0 符号链接 fail-open 问题的回归测试：参数中的悬空链接
// 绝不能静默通过工作区检查。
func TestShell_SymlinkEscapeRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevation on Windows in CI")
	}
	tmp := t.TempDir()
	// 创建一个指向 tmp 之外的符号链接。
	outside := filepath.Join(filepath.Dir(tmp), "outside-"+filepath.Base(tmp))
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tmp, "leak")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	st := NewShellTool(ShellConfig{
		WorkspaceRoot: tmp,
		AllowList:     []string{"ls"},
	}, nil)
	res, err := st.Execute(context.Background(), []byte(`{"cmd":"ls","args":["leak"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("symlink escape should fail: %+v", res)
	}
	if !strings.Contains(res.Content, "escapes workspace") &&
		!strings.Contains(res.Content, "exists") {
		// EvalSymlinks 可能解析出真实路径并导致 rel 检查失败，
		// 也可能对悬空链接报告"文件不存在"；两者都是可接受的
		// 失败模式。
		t.Errorf("expected escape/exists error, got %q", res.Content)
	}
}

// --- 辅助工具 ---

type countingApprover struct {
	calls int
}

func (c *countingApprover) Approve(_ context.Context, _ approval.Request) (approval.Decision, error) {
	c.calls++
	return approval.ApproveSession, nil
}
