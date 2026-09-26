package credentials

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// EnvProvider 命中。
func TestEnvProvider_Resolve(t *testing.T) {
	os.Setenv("DSH_TEST_KEY", "env-value")
	defer os.Unsetenv("DSH_TEST_KEY")

	p := NewEnvProvider()
	v, err := p.Resolve(context.Background(), Ref{Name: "test-key", Env: "DSH_TEST_KEY"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if v != "env-value" {
		t.Fatalf("v = %q, want env-value", v)
	}
}

// EnvProvider 缺 Env 时按 Name 自动拼 DSH_<UPPER_SNAKE>。
func TestEnvProvider_AutoEnvFromName(t *testing.T) {
	os.Setenv("DSH_AUTO_NAME", "auto-value")
	defer os.Unsetenv("DSH_AUTO_NAME")

	p := NewEnvProvider()
	v, err := p.Resolve(context.Background(), Ref{Name: "auto-name"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if v != "auto-value" {
		t.Fatalf("v = %q", v)
	}
}

// EnvProvider 找不到返回 ErrNotFound。
func TestEnvProvider_NotFound(t *testing.T) {
	p := NewEnvProvider()
	_, err := p.Resolve(context.Background(), Ref{Name: "no-such-key", Env: "DSH_NO_SUCH_KEY"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// FileProvider 读取 JSON 文件。
func TestFileProvider_Resolve(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	if err := os.WriteFile(path, []byte(`{
		"deepseek-api-key": "file-key-1",
		"anthropic-api-key": "file-key-2"
	}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	fp, err := NewFileProvider(path)
	if err != nil {
		t.Fatalf("NewFileProvider: %v", err)
	}

	v, err := fp.Resolve(context.Background(), Ref{Name: "deepseek-api-key"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if v != "file-key-1" {
		t.Fatalf("v = %q", v)
	}

	v, err = fp.Resolve(context.Background(), Ref{Name: "anthropic-api-key"})
	if err != nil || v != "file-key-2" {
		t.Fatalf("anthropic = %q, err=%v", v, err)
	}
}

// FileProvider 文件不存在：构造不报错，所有 Resolve 返回 ErrNotFound。
func TestFileProvider_MissingFile(t *testing.T) {
	fp, err := NewFileProvider("/path/does/not/exist")
	if err != nil {
		t.Fatalf("NewFileProvider: %v", err)
	}
	_, err = fp.Resolve(context.Background(), Ref{Name: "x"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// FileProvider 找不到的 name 返回 ErrNotFound。
func TestFileProvider_NotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	if err := os.WriteFile(path, []byte(`{"a":"1"}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	fp, _ := NewFileProvider(path)
	_, err := fp.Resolve(context.Background(), Ref{Name: "b"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

// Chained：env 命中就不查 file。
func TestChained_EnvPriority(t *testing.T) {
	os.Setenv("DSH_CHAIN_KEY", "env-wins")
	defer os.Unsetenv("DSH_CHAIN_KEY")

	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	if err := os.WriteFile(path, []byte(`{"chain-key":"file-value"}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	fp, _ := NewFileProvider(path)

	chain := NewChained(NewEnvProvider(), fp)
	v, err := chain.Resolve(context.Background(), Ref{Name: "chain-key"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if v != "env-wins" {
		t.Fatalf("v = %q, want env-wins (env 应优先)", v)
	}
}

// Chained：env 没有则回退到 file。
func TestChained_FallbackToFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	if err := os.WriteFile(path, []byte(`{"fallback-key":"file-value"}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	fp, _ := NewFileProvider(path)

	chain := NewChained(NewEnvProvider(), fp)
	v, err := chain.Resolve(context.Background(), Ref{Name: "fallback-key"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if v != "file-value" {
		t.Fatalf("v = %q", v)
	}
}

// Chained：全部查不到返回 ErrNotFound。
func TestChained_AllNotFound(t *testing.T) {
	fp, _ := NewFileProvider("/nonexistent")
	chain := NewChained(NewEnvProvider(), fp)
	_, err := chain.Resolve(context.Background(), Ref{Name: "missing-everywhere"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

// FileProvider Reload：磁盘文件被修改后 Reload 重新读取。
func TestFileProvider_Reload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	if err := os.WriteFile(path, []byte(`{"k":"v1"}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	fp, _ := NewFileProvider(path)
	if v, _ := fp.Resolve(context.Background(), Ref{Name: "k"}); v != "v1" {
		t.Fatalf("v1 = %q", v)
	}

	if err := os.WriteFile(path, []byte(`{"k":"v2"}`), 0o600); err != nil {
		t.Fatalf("WriteFile v2: %v", err)
	}
	// 不 Reload 还是 v1（cache 在内存）。
	if v, _ := fp.Resolve(context.Background(), Ref{Name: "k"}); v != "v1" {
		t.Fatalf("before reload = %q", v)
	}

	if err := fp.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if v, _ := fp.Resolve(context.Background(), Ref{Name: "k"}); v != "v2" {
		t.Fatalf("after reload = %q, want v2", v)
	}
}

func TestToUpperSnake(t *testing.T) {
	cases := map[string]string{
		"deepseek-api-key": "DEEPSEEK_API_KEY",
		"api-key":          "API_KEY",
		"single":           "SINGLE",
		"a-b-c":            "A_B_C",
	}
	for in, want := range cases {
		if got := toUpperSnake(in); got != want {
			t.Fatalf("toUpperSnake(%q) = %q, want %q", in, got, want)
		}
	}
}
