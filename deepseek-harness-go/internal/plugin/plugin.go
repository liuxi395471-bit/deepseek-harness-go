// Package plugin 实现工具插件宿主（DESIGN-v2 S-B.2）。
//
// v2 使用 gRPC 进行进程间通信。宿主充当 gRPC 客户端连接到插件进程；
// 每个插件通过 ToolService 注册其工具。崩溃的插件会被自动重启，
// 最多 MaxRestarts 次。
package plugin

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// Config 控制插件的启动与管理方式。
type Config struct {
	Command      []string // 二进制路径 + 参数
	MaxRestarts  int      // 崩溃恢复尝试次数；0 表示禁用
	StartupWait  time.Duration
}

// Host 管理一个插件进程。它启动该进程，按照 MaxRestarts 的限制
// 维持其存活，并暴露插件的工具。
type Host struct {
	cfg Config
	mu  sync.Mutex

	started  bool
	cmd      *exec.Cmd
	restarts int32
	addr     string // 通过 DSH_PLUGIN_ADDR 传给插件的 gRPC 地址
}

// NewHost 根据命令行构建一个 Host。
func NewHost(cfg Config) *Host {
	if cfg.MaxRestarts < 0 {
		cfg.MaxRestarts = 0
	}
	if cfg.StartupWait == 0 {
		cfg.StartupWait = 5 * time.Second
	}
	return &Host{cfg: cfg}
}

// Start 启动插件进程并等待其就绪。
func (h *Host) Start(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.started {
		return errors.New("plugin: already started")
	}
	if err := h.launch(ctx); err != nil {
		return err
	}
	h.started = true
	return nil
}

// launch 派生进程并为其设置地址。
func (h *Host) launch(ctx context.Context) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("plugin: listen: %w", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	cmd := exec.CommandContext(ctx, h.cfg.Command[0], h.cfg.Command[1:]...)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("DSH_PLUGIN_ADDR=%s", addr))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("plugin: start %v: %w", h.cfg.Command[0], err)
	}
	h.cmd = cmd
	h.addr = addr
	return nil
}

// Addr 返回插件正在监听的 gRPC 地址（在 launch 期间设置）。
// 在 Start() 之前返回 ""。
func (h *Host) Addr() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.addr
}

// Watch 启动一个后台 goroutine 监控插件进程，在其崩溃时最多重启
// cfg.MaxRestarts 次。Watch 在首次成功启动后立即返回。
func (h *Host) Watch(ctx context.Context) error {
	if err := h.Start(ctx); err != nil {
		return err
	}
	stop := make(chan struct{})
	go h.runWatchLoop(ctx, stop)
	return nil
}

// runWatchLoop 监控插件并在崩溃时重启它。
func (h *Host) runWatchLoop(ctx context.Context, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		default:
		}
		h.mu.Lock()
		if h.cmd != nil && h.cmd.Process != nil {
			done := make(chan error, 1)
			go func() { done <- h.cmd.Wait() }()
			select {
			case <-stop:
				h.mu.Unlock()
				return
			case <-ctx.Done():
				h.mu.Unlock()
				return
			case <-done:
				if int(atomic.LoadInt32(&h.restarts)) >= h.cfg.MaxRestarts {
					h.mu.Unlock()
					return
				}
				atomic.AddInt32(&h.restarts, 1)
				if err := h.launch(ctx); err != nil {
					h.mu.Unlock()
					return
				}
			}
		}
		h.mu.Unlock()
		time.Sleep(100 * time.Millisecond)
	}
}

// RestartCount 返回该宿主重启其插件的次数。
func (h *Host) RestartCount() int {
	return int(atomic.LoadInt32(&h.restarts))
}

// Close 终止插件进程。
func (h *Host) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cmd != nil && h.cmd.Process != nil {
		return h.cmd.Process.Kill()
	}
	return nil
}

// Process 返回当前进程句柄（未启动时为 nil）。
func (h *Host) Process() *os.Process {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cmd == nil {
		return nil
	}
	return h.cmd.Process
}


