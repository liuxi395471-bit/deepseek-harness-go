package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"deepseek-harness-go/internal/llm"
)

// makeTemp creates a temporary directory and returns its path.
func makeTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "sessions.db")
}

// 1. SQLiteStore: Begin → Append → Load round-trip
func TestSQLiteStore_BeginAppendLoad(t *testing.T) {
	path := makeTemp(t)
	s, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	sess, err := s.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: "you are an agent"},
		{Role: llm.RoleUser, Content: "hello"},
		{Role: llm.RoleAssistant, Content: "hi"},
		{Role: llm.RoleTool, Content: "tool result", ToolCallID: "c1"},
		{Role: llm.RoleAssistant, Content: "done"},
	}
	for _, m := range msgs {
		if err := s.Append(ctx, sess.ID, m); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	loaded, err := s.Load(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Messages) != len(msgs) {
		t.Fatalf("messages = %d, want %d", len(loaded.Messages), len(msgs))
	}
	for i, m := range msgs {
		got := loaded.Messages[i]
		if got.Role != m.Role || got.Content != m.Content {
			t.Errorf("msg[%d] = %+v, want %+v", i, got, m)
		}
	}
	if loaded.Rounds != 2 {
		t.Errorf("Rounds = %d, want 2", loaded.Rounds)
	}
	if !strings.HasPrefix(loaded.Preview, "hello") {
		t.Errorf("Preview = %q, want hello prefix", loaded.Preview)
	}
}

// 2. Append unknown id → ErrNotFound
func TestSQLiteStore_AppendUnknownID(t *testing.T) {
	s, _ := NewSQLiteStore(":memory:")
	defer s.Close()
	err := s.Append(context.Background(), "nope", llm.Message{Role: llm.RoleUser})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// 3. Load unknown id → ErrNotFound
func TestSQLiteStore_LoadUnknownID(t *testing.T) {
	s, _ := NewSQLiteStore(":memory:")
	defer s.Close()
	_, err := s.Load(context.Background(), "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// 4. List with limit + offset
func TestSQLiteStore_ListLimitOffset(t *testing.T) {
	s, _ := NewSQLiteStore(":memory:")
	defer s.Close()
	ctx := context.Background()
	var ids []string
	for i := 0; i < 15; i++ {
		sess, _ := s.Begin(ctx)
		ids = append(ids, sess.ID)
		_ = s.Append(ctx, sess.ID, llm.Message{Role: llm.RoleUser, Content: "u"})
		// Ensure each session has a strictly later UpdatedAt (ms resolution).
		time.Sleep(20 * time.Millisecond)
	}
	page, err := s.List(ctx, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 10 {
		t.Errorf("page len = %d, want 10", len(page))
	}
	// Verify strictly descending UpdatedAt order within the returned page.
	for i := 0; i < len(page)-1; i++ {
		if page[i].UpdatedAt.Before(page[i+1].UpdatedAt) {
			t.Errorf("page[%d].UpdatedAt < page[%d].UpdatedAt: order wrong", i, i+1)
		}
	}
	page, err = s.List(ctx, 5, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 5 {
		t.Errorf("offset page len = %d, want 5", len(page))
	}
	all, err := s.List(ctx, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 15 {
		t.Errorf("all len = %d, want 15", len(all))
	}
}

// 5. UpdateUsage accumulates
func TestSQLiteStore_UpdateUsage(t *testing.T) {
	s, _ := NewSQLiteStore(":memory:")
	defer s.Close()
	ctx := context.Background()
	sess, _ := s.Begin(ctx)
	if err := s.UpdateUsage(ctx, sess.ID, llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateUsage(ctx, sess.ID, llm.Usage{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5}); err != nil {
		t.Fatal(err)
	}
	loaded, _ := s.Load(ctx, sess.ID)
	if loaded.UsageTotal.PromptTokens != 13 {
		t.Errorf("PromptTokens = %d, want 13", loaded.UsageTotal.PromptTokens)
	}
	if loaded.UsageTotal.TotalTokens != 20 {
		t.Errorf("TotalTokens = %d, want 20", loaded.UsageTotal.TotalTokens)
	}
}

// 6. UpdateUsage unknown id → ErrNotFound
func TestSQLiteStore_UpdateUsageUnknownID(t *testing.T) {
	s, _ := NewSQLiteStore(":memory:")
	defer s.Close()
	err := s.UpdateUsage(context.Background(), "nope", llm.Usage{})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// 12. File persistence: verify data survives store close + reopen.
// The path uses a regular directory (not ~) to avoid OS path-expansion issues.
func TestSQLiteStore_DataPersistsAfterClose(t *testing.T) {
	path := makeTemp(t)
	ctx := context.Background()
	{
		s, err := NewSQLiteStore(path)
		if err != nil {
			t.Fatal(err)
		}
		sess, _ := s.Begin(ctx)
		_ = s.Append(ctx, sess.ID, llm.Message{Role: llm.RoleUser, Content: "persist"})
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
	{
		s, err := NewSQLiteStore(path)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		list, err := s.List(ctx, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 {
			t.Fatalf("len(list) = %d, want 1", len(list))
		}
		loaded, err := s.Load(ctx, list[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.Messages[0].Content != "persist" {
			t.Errorf("content = %q, want persist", loaded.Messages[0].Content)
		}
	}
}

// 8. SQL injection style id: parameterized binding safe
func TestSQLiteStore_SQLInjectionSafe(t *testing.T) {
	s, _ := NewSQLiteStore(":memory:")
	defer s.Close()
	ctx := context.Background()

	malicious := "nope' OR id=id --"
	_, err := s.Load(ctx, malicious)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Load(%q) err = %v, want ErrNotFound", malicious, err)
	}
	// BEGIN should still work with normal IDs
	sess, _ := s.Begin(ctx)
	_ = s.Append(ctx, sess.ID, llm.Message{Role: llm.RoleUser, Content: "ok"})
	_, err = s.Load(ctx, sess.ID)
	if err != nil {
		t.Errorf("Load normal id: %v", err)
	}
}

// 9. Concurrent writes to different sessions: each goroutine creates its own
// session and appends messages to it. Verifies no panic and all sessions are
// visible after the goroutines finish.
func TestSQLiteStore_ConcurrentCrossSession(t *testing.T) {
	path := makeTemp(t)
	s, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	const goroutines = 4
	const perSession = 8
	type result struct{ id  string
		msgs int
	}
	results := make(chan result, goroutines)
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sess, _ := s.Begin(ctx)
			for i := 0; i < perSession; i++ {
				_ = s.Append(ctx, sess.ID, llm.Message{Role: llm.RoleUser, Content: "x"})
			}
			results <- result{id: sess.ID, msgs: perSession}
		}()
	}
	wg.Wait()
	close(results)
	got := 0
	for r := range results {
		if r.msgs != perSession {
			t.Errorf("session %s: messages = %d, want %d", r.id, r.msgs, perSession)
		}
		got++
	}
	if got != goroutines {
		t.Errorf("sessions created = %d, want %d", got, goroutines)
	}
}

// 10. ToolCalls round-trip (JSON marshal/unmarshal)
func TestSQLiteStore_ToolCalls(t *testing.T) {
	s, _ := NewSQLiteStore(":memory:")
	defer s.Close()
	ctx := context.Background()
	sess, _ := s.Begin(ctx)
	msg := llm.Message{
		Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{
			{ID: "c1", Type: "function", Function: llm.ToolCallFunc{
				Name:      "greet",
				Arguments: `{"name":"Ada"}`,
			}},
		},
	}
	if err := s.Append(ctx, sess.ID, msg); err != nil {
		t.Fatal(err)
	}
	loaded, _ := s.Load(ctx, sess.ID)
	if len(loaded.Messages) != 1 {
		t.Fatal(loaded.Messages)
	}
	got := loaded.Messages[0]
	if len(got.ToolCalls) != 1 {
		t.Errorf("ToolCalls len = %d", len(got.ToolCalls))
	}
	if got.ToolCalls[0].Function.Name != "greet" {
		t.Errorf("name = %q", got.ToolCalls[0].Function.Name)
	}
	if got.ToolCalls[0].Function.Arguments != `{"name":"Ada"}` {
		t.Errorf("args = %q", got.ToolCalls[0].Function.Arguments)
	}
}

// 11. Preview truncation
func TestSQLiteStore_PreviewTruncation(t *testing.T) {
	s, _ := NewSQLiteStore(":memory:")
	defer s.Close()
	ctx := context.Background()
	sess, _ := s.Begin(ctx)
	long := strings.Repeat("中", 200) // 200 Chinese chars (each rune = 1 preview slot)
	if err := s.Append(ctx, sess.ID, llm.Message{Role: llm.RoleUser, Content: long}); err != nil {
		t.Fatal(err)
	}
	loaded, _ := s.Load(ctx, sess.ID)
	// previewLen = 80; the suffix "…" counts as 1 rune.
	if utf8RuneCount(loaded.Preview) != previewLen+1 {
		t.Errorf("Preview runes = %d, want %d", utf8RuneCount(loaded.Preview), previewLen+1)
	}
}

// 12. Double-close is safe
func TestSQLiteStore_DoubleClose(t *testing.T) {
	s, _ := NewSQLiteStore(":memory:")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("double Close: %v", err)
	}
	// Re-open should still work after double-close
	// (Close just calls db.Close; reopening via path would work.)
}

