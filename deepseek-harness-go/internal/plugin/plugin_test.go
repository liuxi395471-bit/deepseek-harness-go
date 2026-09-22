package plugin

import (
	"context"
	"os"
	"testing"
	"time"
)

// 1. Start 启动一个进程。
func TestHost_Start(t *testing.T) {
	// 使用 Go 二进制本身作为一个简单的"插件"。
	h := NewHost(Config{
		Command:      []string{os.Args[0], "--version"},
		MaxRestarts:  0,
		StartupWait:  2 * time.Second,
	})
	if err := h.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.Process() == nil {
		t.Error("Process() nil after Start")
	}
	if err := h.Close(); err != nil {
		t.Error(err)
	}
}

// 2. 重复 Start 返回错误。
func TestHost_DoubleStart(t *testing.T) {
	h := NewHost(Config{
		Command:     []string{os.Args[0], "--version"},
		MaxRestarts: 0,
	})
	if err := h.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := h.Start(context.Background()); err == nil {
		t.Error("double Start should error")
	}
	h.Close()
}

// 3. 未 Start 时 Close 是安全的。
func TestHost_CloseWithoutStart(t *testing.T) {
	h := NewHost(Config{})
	if err := h.Close(); err != nil {
		t.Error(err)
	}
}
