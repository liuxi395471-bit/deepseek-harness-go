package launcher

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeFs 模拟 embed.FS.ReadFile。
type fakeFs struct{ data map[string][]byte }

func (f *fakeFs) ReadFile(name string) ([]byte, error) {
	v, ok := f.data[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return v, nil
}

// TestLineCapture_DetectsListen 验证能从日志行捕获端口。
func TestLineCapture_DetectsListen(t *testing.T) {
	lc := &lineCapture{onLine: func(s string) {
		if !strings.Contains(s, "listening on 127.0.0.1:7778") {
			t.Errorf("expected line capture, got %q", s)
		}
	}}
	io.WriteString(lc, "2026/09/27 17:17:01 [dsh] serve: listening on 127.0.0.1:7778 (model=x)\n")
	if len(lc.buf) != 0 {
		t.Errorf("buf should be empty after newline, got %q", string(lc.buf))
	}
}

// TestPrepareBinary_FromEmbed 验证嵌入式二进制能写到 dst 并去重。
func TestPrepareBinary_FromEmbed(t *testing.T) {
	tmp := t.TempDir()
	exe := "dsh"
	if isWindows() {
		exe = "dsh.exe"
	}
	dst := filepath.Join(tmp, exe)
	ff := &fakeFs{data: map[string][]byte{exe: []byte("FAKE-BIN")}}
	cfg := Config{DSHRootDir: tmp, EmbedFS: ff, EmbedName: exe}

	path, err := prepareBinary(cfg)
	if err != nil {
		t.Fatalf("prepareBinary: %v", err)
	}
	if path != dst {
		t.Fatalf("path = %s, want %s", path, dst)
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, []byte("FAKE-BIN")) {
		t.Fatalf("dst content wrong")
	}
	// 二次写入应幂等（不会报错）
	if _, err := prepareBinary(cfg); err != nil {
		t.Fatalf("second prepare: %v", err)
	}
}

// TestPrepareBinary_RawPath 验证从物理路径复制。
func TestPrepareBinary_RawPath(t *testing.T) {
	tmp := t.TempDir()
	exe := "dsh"
	if isWindows() {
		exe = "dsh.exe"
	}
	src := filepath.Join(tmp, "src-"+exe)
	os.WriteFile(src, []byte("SRC-BIN"), 0o755)

	dstRoot := filepath.Join(tmp, "dst")
	cfg := Config{DSHRootDir: dstRoot, DSHRawPath: src}

	path, err := prepareBinary(cfg)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	want := filepath.Join(dstRoot, exe)
	if path != want {
		t.Fatalf("path=%s want %s", path, want)
	}
	got, _ := os.ReadFile(want)
	if string(got) != "SRC-BIN" {
		t.Fatalf("content wrong: %s", got)
	}
}

// TestServer_StartStop_PortAuto 验证 spawn 子进程 + 健康检查 + 优雅停止。
// 使用一个真实 HTTP echo 服务器作为"dsh"（通过二进制代理）。
func TestServer_StartStop_PortAuto(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain")
	}
	// 1. 编译一个临时 echo server（监听 127.0.0.1:0，返回 200）
	tmp := t.TempDir()
	src := filepath.Join(tmp, "fake_dsh.go")
	code := `package main
import ("fmt"; "net"; "net/http"; "os"; "time")
func main() {
  addr := os.Getenv("FAKE_ADDR")
  if addr == "" { addr = "127.0.0.1:0" }
  mux := http.NewServeMux()
  mux.HandleFunc("/api/v1/console/health/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
  ln, _ := net.Listen("tcp", addr)
  realAddr := ln.Addr().String()
  fmt.Fprintln(os.Stderr, "listening on "+realAddr)
  os.Stderr.Sync()
  time.Sleep(50 * time.Millisecond)
  srv := &http.Server{Handler: mux}
  srv.Serve(ln)
}
`
	os.WriteFile(src, []byte(code), 0o644)

	exe := "fake_dsh"
	if isWindows() {
		exe += ".exe"
	}
	bin := filepath.Join(tmp, exe)
	cmd := exec.Command("go", "build", "-o", bin, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	// 2. 用 launcher 包准备（DSHRawPath）
	root := filepath.Join(tmp, "root")
	cfg := Config{
		DSHRootDir:   root,
		DSHRawPath:   bin,
		Listen:       "127.0.0.1:0",
		ExtraEnv:     []string{"FAKE_ADDR=127.0.0.1:0"},
		ReadyTimeout: 10 * time.Second,
		LogWriter:    io.Discard,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	srv, err := Start(ctx, cfg)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if srv.Port() == 0 {
		t.Fatal("port should be non-zero")
	}
	if !strings.HasPrefix(srv.ConsoleURL(), "http://127.0.0.1:") {
		t.Fatalf("bad console URL: %s", srv.ConsoleURL())
	}
	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func isWindows() bool { return os.PathSeparator == '\\' }

// 防数据竞争：lineCapture 测试并行时 channel 全局，需串行
var lineCaptureMu sync.Mutex
