// Package launcher 启动并监管 dsh.exe 子进程。
//
// 单一职责：
//   1. 找到/释放 dsh.exe 到用户本地目录；
//   2. spawn 子进程并带上 -serve 标志；
//   3. 轮询 /api/v1/console/health/ 直到 200 拿到可用端口；
//   4. 提供 Stop() 关闭子进程。
//
// 跨平台：Windows/macOS/Linux 共用代码路径。
package launcher

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Config 描述一次启动所需的全部参数。
type Config struct {
	// DSHRootDir 是 dsh.exe 的释放根目录。
	// 留空 = 用默认（%LOCALAPPDATA%\DeepSeekHarness 或 ~/.deepseek-harness）。
	DSHRootDir string

	// DSHRawPath 是启动器二进制内嵌的 dsh.exe 物理位置（开发期：相对路径）。
	// 当 EmbedFS 为空时使用。
	DSHRawPath string

	// EmbedFS 非空时使用 //go:embed 提供的 dsh.exe 字节。
	// 仅支持单个文件 "dsh.exe"（命名固定）。
	EmbedFS    fsWrapper
	EmbedName  string

	// Listen 形如 "127.0.0.1:0"（自动选端口）或 ":7777"。
	Listen string

	// AuthToken 非空时通过环境变量传给 dsh。
	AuthToken string

	// ExtraEnv 追加到子进程环境。
	ExtraEnv []string

	// Workspace 子进程的 DSH_AGENT_WORKSPACE_ROOT。
	Workspace string

	// LogWriter 转发 dsh 子进程 stdout/stderr；nil = os.Stdout。
	LogWriter io.Writer

	// ReadyTimeout 等待 /health 的最长时间。
	ReadyTimeout time.Duration
}

// 轻量 fs 接口包装（兼容 embed.FS.Sub 与本地 FS）。
type fsWrapper interface {
	ReadFile(name string) ([]byte, error)
}

// Server 表示一个已启动的 dsh -serve 实例。
type Server struct {
	cmd    *exec.Cmd
	port   int
	listen string
	logf   *os.File
	mu     sync.Mutex
}

// Start 根据 config 准备二进制 + spawn 子进程 + 等待就绪。
func Start(ctx context.Context, cfg Config) (*Server, error) {
	if cfg.ReadyTimeout == 0 {
		cfg.ReadyTimeout = 30 * time.Second
	}

	// 重置全局 listen-address 捕获 channel（避免上一个测试残留）
	listenAddrCh = make(chan string, 1)

	// 1. 解析 / 准备 dsh.exe 物理路径
	binPath, err := prepareBinary(cfg)
	if err != nil {
		return nil, fmt.Errorf("prepare binary: %w", err)
	}

	// 2. 解析监听端口（auto = dsh 内部选）
	listen := cfg.Listen
	if listen == "" {
		listen = "127.0.0.1:0"
	}

	// 3. 准备子进程（dsh -serve 只支持 -serve/-config/-plugin；端口由
	//    DSH_SERVER_LISTEN 环境变量控制）
	args := []string{"-serve"}
	cmd := exec.CommandContext(ctx, binPath, args...)

	env := os.Environ()
	if cfg.AuthToken != "" {
		env = append(env, "DSH_SERVER_AUTH_TOKEN="+cfg.AuthToken)
	}
	if cfg.Workspace != "" {
		env = append(env, "DSH_AGENT_WORKSPACE_ROOT="+cfg.Workspace)
	}
	if listen != "127.0.0.1:0" && listen != ":0" {
		env = append(env, "DSH_SERVER_LISTEN="+listen)
	}
	env = append(env, cfg.ExtraEnv...)
	cmd.Env = env

	// 4. 输出转发 — stdout 和 stderr 都装上 lineCapture，因为 dsh 把
	// "listening on ..." 写到 stderr。
	out := cfg.LogWriter
	if out == nil {
		out = os.Stdout
	}
	cap := &lineCapture{onLine: captureListenAddr}
	cmd.Stdout = io.MultiWriter(out, cap)
	cmd.Stderr = io.MultiWriter(out, cap)

	// 5. 新进程组（Windows: CREATE_NEW_PROCESS_GROUP；POSIX: Setpgid）
	setProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start dsh: %w", err)
	}

	srv := &Server{cmd: cmd}

	// 6. 等待 /health
	port, err := srv.waitReady(ctx, cfg.ReadyTimeout, listen)
	if err != nil {
		_ = srv.Stop()
		return nil, err
	}
	srv.port = port
	srv.listen = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	return srv, nil
}

// Port 返回 dsh 实际监听的端口（自动选模式才有意义）。
func (s *Server) Port() int { return s.port }

// ListenAddr 返回 "127.0.0.1:7777" 形式。
func (s *Server) ListenAddr() string { return s.listen }

// ConsoleURL 返回浏览器地址。
func (s *Server) ConsoleURL() string {
	return "http://" + s.listen + "/console/"
}

// Stop 优雅停止子进程（先 SIGTERM 5s 内 -> Kill）。
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd == nil || s.cmd.Process == nil {
		return nil
	}
	if err := s.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		// 退路：直接 Kill
		_ = s.cmd.Process.Kill()
	}
	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = s.cmd.Process.Kill()
		<-done
	}
	s.cmd = nil
	return nil
}

// waitReady 轮询 /health 直到 200 或超时。
// 若 listen 显式给了端口，直接用；否则依赖子进程 stderr 中捕获的 "listening on X.X.X.X:PORT"。
func (s *Server) waitReady(ctx context.Context, timeout time.Duration, listen string) (int, error) {
	deadline := time.Now().Add(timeout)
	probeAddr := listen
	if listen == "127.0.0.1:0" || listen == ":0" {
		// 等捕获端口
		select {
		case captured := <-listenAddrCh:
			probeAddr = captured
			parts := strings.LastIndex(captured, ":")
			if parts < 0 {
				return 0, fmt.Errorf("bad captured addr %q", captured)
			}
			p, err := strconv.Atoi(captured[parts+1:])
			if err != nil {
				return 0, fmt.Errorf("parse port from %q: %w", captured, err)
			}
			return p, pollHealth(ctx, captured, deadline)
		case <-time.After(timeout):
			return 0, fmt.Errorf("timeout waiting for dsh listen addr")
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	// 显式端口：直接探测
	parts := strings.LastIndex(probeAddr, ":")
	if parts < 0 {
		return 0, fmt.Errorf("bad addr %q", probeAddr)
	}
	p, _ := strconv.Atoi(probeAddr[parts+1:])
	if err := pollHealth(ctx, probeAddr, deadline); err != nil {
		return 0, err
	}
	return p, nil
}

func pollHealth(ctx context.Context, addr string, deadline time.Time) error {
	url := "http://" + addr + "/api/v1/console/health/"
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("dsh did not become healthy within %s (url=%s)", time.Until(deadline), url)
}

// prepareBinary 把 dsh.exe 释放到用户本地目录并返回最终路径。
// 优先用 cfg.EmbedFS，否则用 cfg.DSHRawPath。
func prepareBinary(cfg Config) (string, error) {
	root := cfg.DSHRootDir
	if root == "" {
		root = defaultRootDir()
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}

	exeName := "dsh.exe"
	if runtime.GOOS != "windows" {
		exeName = "dsh"
	}
	dst := filepath.Join(root, exeName)

	// 已经存在：检查是否需要重写（嵌入式字节变化时强制覆盖）
	if cfg.EmbedFS == nil && cfg.DSHRawPath == "" {
		return "", errors.New("neither EmbedFS nor DSHRawPath set")
	}

	if cfg.EmbedFS != nil {
		name := cfg.EmbedName
		if name == "" {
			name = exeName
		}
		data, err := cfg.EmbedFS.ReadFile(name)
		if err != nil {
			return "", fmt.Errorf("read embedded %s: %w", name, err)
		}
		if !fileEqual(dst, data) {
			if err := os.WriteFile(dst, data, 0o755); err != nil {
				return "", err
			}
		}
		return dst, nil
	}

	// 直接复制
	src := cfg.DSHRawPath
	srcData, err := os.ReadFile(src)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", src, err)
	}
	if !fileEqual(dst, srcData) {
		if err := os.WriteFile(dst, srcData, 0o755); err != nil {
			return "", err
		}
	}
	return dst, nil
}

func fileEqual(path string, data []byte) bool {
	cur, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	if len(cur) != len(data) {
		return false
	}
	for i := range cur {
		if cur[i] != data[i] {
			return false
		}
	}
	return true
}

func defaultRootDir() string {
	if runtime.GOOS == "windows" {
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			return filepath.Join(v, "DeepSeekHarness", "bin")
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".deepseek-harness", "bin")
	}
	return filepath.Join(os.TempDir(), "deepseek-harness")
}

// ---------- listen-address 捕获 ----------

var listenAddrCh = make(chan string, 1)

// captureListenAddr 全局钩子：在新 Server 启动前重置 channel。
var captureListenAddr = func(line string) {
	line = strings.TrimSpace(line)
	// dsh -serve 日志格式：
	//   "2026/09/27 17:17:01 [dsh] serve: listening on 127.0.0.1:7778 (model=...)"
	idx := strings.Index(line, "listening on ")
	if idx < 0 {
		return
	}
	rest := line[idx+len("listening on "):]
	// 切到空格或括号
	end := strings.IndexAny(rest, " (")
	if end < 0 {
		end = len(rest)
	}
	addr := strings.TrimSpace(rest[:end])
	if addr == "" {
		return
	}
	// 非阻塞发送
	select {
	case listenAddrCh <- addr:
	default:
	}
}

// lineCapture 把每行输出回调 onLine。
type lineCapture struct {
	onLine func(string)
	buf    []byte
}

func (l *lineCapture) Write(p []byte) (int, error) {
	l.buf = append(l.buf, p...)
	for {
		idx := -1
		for i, b := range l.buf {
			if b == '\n' {
				idx = i
				break
			}
		}
		if idx < 0 {
			break
		}
		line := string(l.buf[:idx])
		l.onLine(strings.TrimRight(line, "\r"))
		l.buf = l.buf[idx+1:]
	}
	return len(p), nil
}

// ---------- 跨平台进程属性 ----------

// setProcAttr 配置子进程属性，使其：
//   - Windows：隐藏窗口（CREATE_NO_WINDOW）
//   - POSIX  ：新进程组（Setpgid=true），让本进程收到的 SIGTERM 不传到子进程
func setProcAttr(cmd *exec.Cmd) {
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	} else {
		cmd.SysProcAttr = newSysProcAttr()
	}
}

// ---------- 兼容：dsh 不接受 -listen，从 env 拿；为简洁允许 -listen 被忽略 ----------

// HelperContains 仅用于测试。
func HelperContains(haystack, needle string) bool { return strings.Contains(haystack, needle) }

// NewScanner 暴露 bufio 给测试用。
func NewScanner(r io.Reader) *bufio.Scanner { return bufio.NewScanner(r) }
