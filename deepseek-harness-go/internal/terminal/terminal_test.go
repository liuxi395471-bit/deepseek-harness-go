package terminal

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTerminal_RunSimple(t *testing.T) {
	tr := New("t1", "cmd", nil, "", "test", 100)
	exit, err := tr.Run(context.Background(), "echo hello", 5*time.Second)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if exit != 0 {
		t.Errorf("exit = %d, want 0", exit)
	}
	all := tr.All()
	if len(all) == 0 || !strings.Contains(strings.Join(all, "|"), "hello") {
		t.Errorf("output = %v", all)
	}
}

func TestTerminal_RunFailingCommand(t *testing.T) {
	tr := New("t2", "cmd", nil, "", "test", 100)
	exit, err := tr.Run(context.Background(), "cmd /c exit 7", 5*time.Second)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if exit != 7 {
		t.Errorf("exit = %d, want 7", exit)
	}
}

func TestTerminal_BusyRejected(t *testing.T) {
	tr := New("t3", "cmd", nil, "", "test", 100)
	// 启动一个长命令并 await
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = tr.Run(context.Background(), "ping 127.0.0.1 -n 3 >nul", 5*time.Second)
	}()
	// 等到 running
	for i := 0; i < 50; i++ {
		tr.mu.Lock()
		r := tr.running
		tr.mu.Unlock()
		if r {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, err := tr.Run(context.Background(), "echo busy", 1*time.Second)
	if err != ErrBusy {
		t.Errorf("busy err = %v, want ErrBusy", err)
	}
	wg.Wait()
}

func TestTerminal_BufferCap(t *testing.T) {
	tr := New("t4", "cmd", nil, "", "test", 3)
	tr.appendLine("a")
	tr.appendLine("b")
	tr.appendLine("c")
	tr.appendLine("d")
	all := tr.All()
	// cap=3, drop=cap/4=0 → drop=1. After 4 appends:
	// [a] → [a,b] → [a,b,c] → drop 1 → [b,c,d]
	if len(all) != 3 {
		t.Errorf("all len = %d, want 3", len(all))
	}
	if all[0] != "b" || all[2] != "d" {
		t.Errorf("all = %v, want [b c d]", all)
	}
}

func TestTerminal_ReadSince(t *testing.T) {
	tr := New("t5", "cmd", nil, "", "test", 10)
	tr.appendLine("1")
	tr.appendLine("2")
	tr.appendLine("3")
	lines := tr.Read(1)
	if len(lines) != 2 || lines[0] != "2" || lines[1] != "3" {
		t.Errorf("Read(1) = %v", lines)
	}
	// 越界
	if tr.Read(100) == nil {
		t.Error("Read(100) should be empty slice, not nil")
	}
}

func TestRegistry_CreateGetList(t *testing.T) {
	r := NewRegistry(100)
	t1, err := r.Create("cmd", nil, "", "alice")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if t1.ID == "" {
		t.Error("empty id")
	}
	got, err := r.Get(t1.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != t1 {
		t.Error("get returned different ptr")
	}
	list := r.List()
	if len(list) != 1 || list[0].ID != t1.ID {
		t.Errorf("list = %v", list)
	}
}

func TestRegistry_NotFound(t *testing.T) {
	r := NewRegistry(100)
	if _, err := r.Get("nope"); err != ErrTerminalNotFound {
		t.Errorf("get nope = %v, want ErrTerminalNotFound", err)
	}
	if err := r.Kill("nope"); err != ErrTerminalNotFound {
		t.Errorf("kill nope = %v, want ErrTerminalNotFound", err)
	}
}

func TestRegistry_CreateEmptyShell(t *testing.T) {
	r := NewRegistry(100)
	if _, err := r.Create("", nil, "", ""); err == nil {
		t.Error("empty shell should error")
	}
}
