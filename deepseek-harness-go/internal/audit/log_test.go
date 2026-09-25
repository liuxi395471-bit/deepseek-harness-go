package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type memCloser struct {
	strings.Builder
	mu      sync.Mutex
	closed  bool
	closeN  int
	closeSz int
}

func (m *memCloser) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Builder.Write(p)
}

func (m *memCloser) String() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Builder.String()
}

func (m *memCloser) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.closeN++
	return nil
}

func TestLogJSONLRedact(t *testing.T) {
	mem := &memCloser{}
	l := NewWriterLogger(mem, true)
	l.Log(context.Background(), Event{
		SessionID: "s1", Event: EventToolCall, Round: 2,
		Tool: "shell", ArgsRaw: `{"cmd":"ls"}`,
	})
	l.Close()

	line := strings.TrimSpace(mem.String())
	var ev Event
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		t.Fatalf("decode: %v (%s)", err, line)
	}
	if ev.ArgsRaw != "" {
		t.Fatalf("redact mode leaked args_raw: %q", ev.ArgsRaw)
	}
	if ev.ArgsHash == "" {
		t.Fatal("args_hash missing in redact mode")
	}
	if ev.ArgsHash != HashArgs([]byte(`{"cmd":"ls"}`)) {
		t.Fatalf("wrong hash: %s", ev.ArgsHash)
	}
	if ev.Event != "tool_call" || ev.Tool != "shell" || ev.Round != 2 || ev.SessionID != "s1" {
		t.Fatalf("unexpected event: %+v", ev)
	}
	if ev.TS.IsZero() {
		t.Fatal("ts not filled")
	}
	if !mem.closed || mem.closeN != 1 {
		t.Fatal("Close not called exactly once")
	}
}

func TestLogFullModeKeepsRaw(t *testing.T) {
	mem := &memCloser{}
	l := NewWriterLogger(mem, false)
	l.Log(context.Background(), Event{Event: EventToolCall, Tool: "fs", ArgsRaw: `{"path":"a.txt"}`})
	var ev Event
	if err := json.Unmarshal([]byte(strings.TrimSpace(mem.String())), &ev); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ev.ArgsRaw != `{"path":"a.txt"}` {
		t.Fatalf("full mode lost args_raw: %q", ev.ArgsRaw)
	}
}

func TestNewFileLoggerAppends(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	l1, err := NewFileLogger(path, false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	l1.Log(context.Background(), Event{Event: "llm_call", Model: "m", PromptTokens: 10})
	l1.Close()

	l2, err := NewFileLogger(path, false)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	l2.Log(context.Background(), Event{Event: "llm_call", Model: "m", PromptTokens: 20})
	l2.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(lines), data)
	}
	for i, ln := range lines {
		var ev Event
		if err := json.Unmarshal([]byte(ln), &ev); err != nil {
			t.Fatalf("line %d: %v", i, err)
		}
	}
}

func TestConcurrentSessions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	l, err := NewFileLogger(path, true)
	if err != nil {
		t.Fatal(err)
	}
	const sessions = 8
	const perSession = 20
	var wg sync.WaitGroup
	for s := 0; s < sessions; s++ {
		wg.Add(1)
		go func(s int) {
			defer wg.Done()
			sid := fmt.Sprintf("sess-%d", s)
			for i := 0; i < perSession; i++ {
				l.Log(context.Background(), Event{SessionID: sid, Event: EventToolCall, Tool: "t"})
			}
		}(s)
	}
	wg.Wait()
	l.Close()

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	counts := map[string]int{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var ev Event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("bad line %q: %v", sc.Text(), err)
		}
		counts[ev.SessionID]++
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(counts) != sessions {
		t.Fatalf("session count = %d, want %d", len(counts), sessions)
	}
	for sid, n := range counts {
		if n != perSession {
			t.Fatalf("session %s: %d events, want %d", sid, n, perSession)
		}
	}
}

func TestNilLoggerSafe(t *testing.T) {
	var l *FileLogger
	l.Log(context.Background(), Event{Event: "x"})
	if err := l.Close(); err != nil {
		t.Fatalf("nil close: %v", err)
	}
}

func TestHashArgsDeterministic(t *testing.T) {
	a := HashArgs([]byte("hello"))
	b := HashArgs([]byte("hello"))
	c := HashArgs([]byte("world"))
	if a != b || a == c {
		t.Fatal("hash not deterministic/unique")
	}
	if len(a) != 64 {
		t.Fatalf("sha256 hex length = %d", len(a))
	}
}
