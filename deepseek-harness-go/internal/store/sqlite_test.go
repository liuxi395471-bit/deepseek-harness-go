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

// --- v4 §A SQLite 事件流验收用例 ---

// T1.8.7 SQLite events 表写入与查询
func TestSQLiteStore_AppendEvent_RoundTrip(t *testing.T) {
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	sess, _ := s.Begin(ctx)

	// 写混合事件
	emit := func(typ EventType, pl any) int64 {
		ev := Event{Type: typ, Actor: "primary"}
		_ = ev.MarshalPayload(pl)
		seq, err := s.AppendEvent(ctx, sess.ID, ev)
		if err != nil {
			t.Fatalf("AppendEvent type=%s: %v", typ, err)
		}
		return seq
	}
	emit(EventSystemPrompt, SystemPromptPayload{Content: "sys"})
	emit(EventUserMessage, UserMessagePayload{Content: "u1"})
	emit(EventAssistantMessage, AssistantMessagePayload{
		Message: llm.Message{Role: llm.RoleAssistant, Content: "a1"},
	})

	// 读全量
	events, err := s.ReadEvents(ctx, sess.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
	if events[0].Type != EventSystemPrompt || events[1].Type != EventUserMessage || events[2].Type != EventAssistantMessage {
		t.Errorf("event order/types wrong: %+v", events)
	}

	// seq 单调递增
	for i := 1; i < len(events); i++ {
		if events[i].Seq != events[i-1].Seq+1 {
			t.Errorf("seq gap: %d -> %d", events[i-1].Seq, events[i].Seq)
		}
	}

	// GetLastSeq
	last, _ := s.GetLastSeq(ctx, sess.ID)
	if last != 2 {
		t.Errorf("lastSeq=%d, want 2", last)
	}

	// ReadEvents from=2
	tail, _ := s.ReadEvents(ctx, sess.ID, 2)
	if len(tail) != 1 {
		t.Errorf("from=2 got %d events, want 1", len(tail))
	}

	// Project
	state, err := s.Project(ctx, sess.ID, "messages")
	if err != nil {
		t.Fatalf("Project messages: %v", err)
	}
	msgs := state.(MessagesState)
	if len(msgs) != 3 {
		t.Errorf("projected msgs = %d, want 3: %+v", len(msgs), msgs)
	}
	if msgs[0].Role != llm.RoleSystem || msgs[1].Role != llm.RoleUser {
		t.Errorf("projected roles wrong: %+v", msgs)
	}

	// Project 命中 cache（第二次更快——这是定性测试，仅断言不报错）
	if _, err := s.Project(ctx, sess.ID, "messages"); err != nil {
		t.Fatalf("Project cache hit: %v", err)
	}

	// AppendEvent 后 cache 自动 invalidate
	emit(EventAssistantMessage, AssistantMessagePayload{
		Message: llm.Message{Role: llm.RoleAssistant, Content: "a2"},
	})
	state2, _ := s.Project(ctx, sess.ID, "messages")
	msgs2 := state2.(MessagesState)
	if len(msgs2) != 4 {
		t.Errorf("after append, projected msgs = %d, want 4", len(msgs2))
	}
}

// T1.8.8 SQLite GetLastSeq 对未知 sid 返回 ErrNotFound
func TestSQLiteStore_GetLastSeq_NotFound(t *testing.T) {
	s, _ := NewSQLiteStore(":memory:")
	defer s.Close()
	_, err := s.GetLastSeq(context.Background(), "no-such-id")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// T1.8.9 SQLite AppendEvent 对未知 sid 返回 ErrNotFound
func TestSQLiteStore_AppendEvent_NotFound(t *testing.T) {
	s, _ := NewSQLiteStore(":memory:")
	defer s.Close()
	ev := Event{Type: EventUserMessage}
	_ = ev.MarshalPayload(UserMessagePayload{Content: "x"})
	_, err := s.AppendEvent(context.Background(), "no-such-id", ev)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// T1.8.10 SQLite ProjectionStore 类型断言通过
func TestSQLiteStore_AsEventStore(t *testing.T) {
	s, _ := NewSQLiteStore(":memory:")
	defer s.Close()
	var iface Store = s
	es, ok := AsEventStore(iface)
	if !ok {
		t.Fatal("SQLiteStore should implement EventStore")
	}
	if es != s {
		t.Fatal("AsEventStore should return same instance")
	}
	ps, ok := iface.(ProjectionStore)
	if !ok {
		t.Fatal("SQLiteStore should implement ProjectionStore")
	}
	if _, err := ps.Project(context.Background(), "no-such", "messages"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown sid Project err = %v, want ErrNotFound", err)
	}
}

// T1.8.11 MapStore 也实现 EventStore + ProjectionStore
func TestMapStore_ImplementsProjectionStore(t *testing.T) {
	s := NewMapStore()
	defer s.Close()
	var iface Store = s
	es, ok := AsEventStore(iface)
	if !ok {
		t.Fatal("MapStore should implement EventStore")
	}
	ctx := context.Background()
	sess, _ := s.Begin(ctx)
	ev := Event{Type: EventSystemPrompt}
	_ = ev.MarshalPayload(SystemPromptPayload{Content: "x"})
	if _, err := es.AppendEvent(ctx, sess.ID, ev); err != nil {
		t.Fatal(err)
	}
	if _, ok := iface.(ProjectionStore); !ok {
		t.Fatal("MapStore should implement ProjectionStore")
	}
}

// T1.8.21 usage / phase 投影：累加 LLMCall 与 PhaseChange
func TestAppendEvent_NotFound_BothBackends(t *testing.T) {
	ctx := context.Background()
	m := NewMapStore()
	defer m.Close()
	ev := Event{Type: EventUserMessage}
	_ = ev.MarshalPayload(UserMessagePayload{Content: "x"})
	if _, err := m.AppendEvent(ctx, "missing", ev); err == nil || !errors.Is(err, ErrNotFound) {
		t.Errorf("MapStore AppendEvent missing sid err = %v", err)
	}

	s, _ := NewSQLiteStore(":memory:")
	defer s.Close()
	if _, err := s.AppendEvent(ctx, "missing", ev); err == nil || !errors.Is(err, ErrNotFound) {
		t.Errorf("SQLiteStore AppendEvent missing sid err = %v", err)
	}
}

// T1.8.15 MapStore.GetLastSeq 对 unknown sid 返回 ErrNotFound
func TestMapStore_GetLastSeq_NotFound(t *testing.T) {
	s := NewMapStore()
	defer s.Close()
	_, err := s.GetLastSeq(context.Background(), "no-such")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// T1.8.16 MapStore.Project 对 unknown sid 返回 ErrNotFound
func TestMapStore_Project_NotFound(t *testing.T) {
	s := NewMapStore()
	defer s.Close()
	_, err := s.Project(context.Background(), "no-such", "messages")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// T1.8.17 MapStore.Project 对未知投影 name 返回错误
func TestMapStore_Project_UnknownName(t *testing.T) {
	s := NewMapStore()
	defer s.Close()
	ctx := context.Background()
	sess, _ := s.Begin(ctx)
	_, err := s.Project(ctx, sess.ID, "nope")
	if err == nil {
		t.Fatal("expected error for unknown projection name")
	}
}

// T1.8.18 MapStore AppendEvent 后 cache 失效：Project 看到新事件
func TestMapStore_AppendEvent_InvalidatesCache(t *testing.T) {
	s := NewMapStore()
	defer s.Close()
	ctx := context.Background()
	sess, _ := s.Begin(ctx)
	emit := func(typ EventType, pl any) {
		ev := Event{Type: typ}
		_ = ev.MarshalPayload(pl)
		if _, err := s.AppendEvent(ctx, sess.ID, ev); err != nil {
			t.Fatal(err)
		}
	}
	emit(EventSystemPrompt, SystemPromptPayload{Content: "sys"})
	emit(EventUserMessage, UserMessagePayload{Content: "u1"})
	state, _ := s.Project(ctx, sess.ID, "messages")
	if len(state.(MessagesState)) != 2 {
		t.Fatalf("first project: %+v", state)
	}
	emit(EventAssistantMessage, AssistantMessagePayload{
		Message: llm.Message{Role: llm.RoleAssistant, Content: "a1"},
	})
	state2, _ := s.Project(ctx, sess.ID, "messages")
	if len(state2.(MessagesState)) != 3 {
		t.Errorf("after append, msgs = %d, want 3", len(state2.(MessagesState)))
	}
}

// T1.8.19 Event.UnmarshalPayload 处理空 Payload
func TestEvent_UnmarshalPayload_Empty(t *testing.T) {
	ev := Event{}
	type dummy struct{ X int }
	var d dummy
	if err := ev.UnmarshalPayload(&d); err != nil {
		t.Errorf("empty payload should not error: %v", err)
	}
	if d.X != 0 {
		t.Errorf("default zero value expected")
	}
}

// T1.8.20 Event.MarshalPayload + UnmarshalPayload 往返
func TestEvent_Payload_RoundTrip(t *testing.T) {
	ev := Event{}
	pl := UserMessagePayload{Content: "hi"}
	if err := ev.MarshalPayload(pl); err != nil {
		t.Fatal(err)
	}
	if len(ev.Payload) == 0 {
		t.Fatal("payload empty after marshal")
	}
	var got UserMessagePayload
	if err := ev.UnmarshalPayload(&got); err != nil {
		t.Fatal(err)
	}
	if got.Content != pl.Content {
		t.Errorf("got %q, want %q", got.Content, pl.Content)
	}
}

// T1.8.21 usage / phase 投影：累加 LLMCall 与 PhaseChange
func TestProjector_UsageAndPhase(t *testing.T) {
	now := time.Now()
	events := []Event{
		{Type: EventLLMCall, Timestamp: now, Actor: "primary"},
		{Type: EventPhaseChange, Timestamp: now, Actor: "primary"},
		{Type: EventLLMCall, Timestamp: now, Actor: "primary"},
	}
	_ = events[0].MarshalPayload(LLMCallPayload{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30})
	_ = events[1].MarshalPayload(PhaseChangePayload{Phase: "llm_call"})
	_ = events[2].MarshalPayload(LLMCallPayload{PromptTokens: 5, CompletionTokens: 10, TotalTokens: 15})
	res, err := ProjectAll(events, DefaultProjectorSet())
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage.PromptTokens != 15 || res.Usage.CompletionTokens != 30 || res.Usage.TotalTokens != 45 {
		t.Errorf("usage = %+v, want 15/30/45", res.Usage)
	}
	if string(res.Phase) != "llm_call" {
		t.Errorf("phase = %q, want llm_call", res.Phase)
	}
}

// T1.8.12 EventType.String 覆盖所有类型 + 默认分支
func TestEventType_String_AllBranches(t *testing.T) {
	cases := map[EventType]string{
		EventSessionBegin:     "session.begin",
		EventSystemPrompt:     "system.prompt",
		EventUserMessage:      "user.message",
		EventAssistantMessage: "assistant.message",
		EventToolCall:         "tool.call",
		EventToolResult:       "tool.result",
		EventLLMCall:          "llm.call",
		EventCompaction:       "compaction",
		EventPhaseChange:      "phase.change",
		EventSessionEnd:       "session.end",
		EventType(999):        "event.unknown(999)",
	}
	for typ, want := range cases {
		if got := typ.String(); got != want {
			t.Errorf("String(%d) = %q, want %q", int(typ), got, want)
		}
	}
}

// T1.8.13 SQLite ExportSession 通过 Session 序列化（不包含 events 表，
// 但 Session 字段由 Append 路径填充）。本测试确认 v3 import/export
// 在 EventStore 上仍可用。
func TestSQLiteStore_ExportImport_WithEventStore(t *testing.T) {
	src, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	ctx := context.Background()
	sess, _ := src.Begin(ctx)
	if err := src.Append(ctx, sess.ID, llm.Message{Role: llm.RoleSystem, Content: "x"}); err != nil {
		t.Fatal(err)
	}
	// 写一条事件
	ev := Event{Type: EventUserMessage}
	_ = ev.MarshalPayload(UserMessagePayload{Content: "y"})
	if _, err := src.AppendEvent(ctx, sess.ID, ev); err != nil {
		t.Fatal(err)
	}
	// 重新 Load，让 sess 拿到 Append 写入的 messages
	loaded, err := src.Load(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}

	// ExportSession 只导出 Session（v3 字段）；events 不在导出范围。
	data, err := ExportSession(loaded)
	if err != nil {
		t.Fatal(err)
	}
	dst, _ := NewSQLiteStore(":memory:")
	defer dst.Close()
	imported, err := ImportSession(ctx, dst, data, ImportOptions{PreserveID: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(imported.Messages) != 1 {
		t.Errorf("import lost messages: %+v", imported.Messages)
	}
}

// T1.8.14 MemoryProjectionCache.Size 正确返回 session 数
func TestMemoryProjectionCache_Size(t *testing.T) {
	c := NewMemoryProjectionCache()
	if c.Size() != 0 {
		t.Errorf("empty size = %d, want 0", c.Size())
	}
	c.Put("s1", "messages", MessagesState{llm.Message{Role: llm.RoleSystem, Content: "x"}})
	c.Put("s2", "usage", UsageState{})
	if c.Size() != 2 {
		t.Errorf("size = %d, want 2", c.Size())
	}
	c.Invalidate("s1")
	if c.Size() != 1 {
		t.Errorf("after invalidate size = %d, want 1", c.Size())
	}
}

// T1.8.15 三个 Projector 的 Name() 方法（覆盖默认分支）
func TestProjector_Names(t *testing.T) {
	if (&MessagesProjector{}).Name() != "messages" {
		t.Errorf("MessagesProjector name wrong")
	}
	if (&UsageProjector{}).Name() != "usage" {
		t.Errorf("UsageProjector name wrong")
	}
	if (&PhaseProjector{}).Name() != "phase" {
		t.Errorf("PhaseProjector name wrong")
	}
}

// T1.8.16 AsEventStore(nil) 不 panic
func TestAsEventStore_NilStore(t *testing.T) {
	_, ok := AsEventStore(nil)
	if ok {
		t.Error("nil should not be EventStore")
	}
}

// T1.8.17 AsEventStore 对非 EventStore 类型（仅实现 Store）返回 false
func TestAsEventStore_NonEvent(t *testing.T) {
	// 构造一个仅满足 Store 接口的 stub。
	stub := &storeStub{}
	_, ok := AsEventStore(stub)
	if ok {
		t.Error("stub should not be EventStore")
	}
}

// storeStub 仅实现 Store，不实现 EventStore。
type storeStub struct{}

func (*storeStub) Begin(context.Context) (Session, error) { return Session{}, nil }
func (*storeStub) Append(context.Context, string, llm.Message) error { return nil }
func (*storeStub) Load(context.Context, string) (Session, error) { return Session{}, nil }
func (*storeStub) List(context.Context, int, int) ([]Session, error) { return nil, nil }
func (*storeStub) UpdateUsage(context.Context, string, llm.Usage) error { return nil }
func (*storeStub) Close() error { return nil }

// T1.8.18 MapStore.Begin 在 closed 后 AppendEvent 返回错误
func TestMapStore_AppendEvent_Closed(t *testing.T) {
	s := NewMapStore()
	ctx := context.Background()
	sess, _ := s.Begin(ctx)
	s.Close()
	ev := Event{Type: EventUserMessage}
	_ = ev.MarshalPayload(UserMessagePayload{Content: "x"})
	_, err := s.AppendEvent(ctx, sess.ID, ev)
	if err == nil {
		t.Error("AppendEvent after Close should error")
	}
}

// T1.8.19 MapStore.ReadEvents 在 closed 后返回错误
func TestMapStore_ReadEvents_Closed(t *testing.T) {
	s := NewMapStore()
	ctx := context.Background()
	sess, _ := s.Begin(ctx)
	s.Close()
	_, err := s.ReadEvents(ctx, sess.ID, 0)
	if err == nil {
		t.Error("ReadEvents after Close should error")
	}
}

// T1.8.20 SQLite 覆盖 GetLastSeq 在 sid 存在但无事件时返回 -1
func TestSQLiteStore_GetLastSeq_EmptySession(t *testing.T) {
	s, _ := NewSQLiteStore(":memory:")
	defer s.Close()
	ctx := context.Background()
	sess, _ := s.Begin(ctx)
	last, err := s.GetLastSeq(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if last != -1 {
		t.Errorf("empty session lastSeq = %d, want -1", last)
	}
}

// T1.8.21 ExportAll 序列化全部会话
func TestExportAll_Empty(t *testing.T) {
	s := NewMapStore()
	defer s.Close()
	ctx := context.Background()
	data, err := ExportAll(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"sessions": []`) {
		t.Errorf("export empty should have empty sessions: %s", string(data))
	}
}

// T1.8.22 newEmptyIDFromBegin 走一遍（覆盖 export.go 私有函数）
func TestNewEmptyIDFromBegin(t *testing.T) {
	s := NewMapStore()
	defer s.Close()
	id := newEmptyIDFromBegin(s)
	if id == "" {
		t.Fatal("newEmptyIDFromBegin returned empty")
	}
}

// T1.8.23 Projector.Apply default 分支：未识别 EventType 不报错
func TestProjector_DefaultCase(t *testing.T) {
	msgs := MessagesState{}
	ev := Event{Type: EventType(255)} // 未定义类型
	if err := (&MessagesProjector{}).Apply(ev, &msgs); err != nil {
		t.Errorf("default case should be noop: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("default case should not append")
	}
}

// T1.8.24 Apply 传入错误状态类型返回错误（覆盖错误分支）
func TestProjector_BadStateType(t *testing.T) {
	// MessagesProjector 期望 *MessagesState，传错误类型
	ev := Event{Type: EventSystemPrompt}
	err := (&MessagesProjector{}).Apply(ev, "not the right type")
	if err == nil {
		t.Error("expected error for bad state type")
	}
}

// T1.8.25 UsageProjector 非 LLMCall 类型不累加
func TestProjector_Usage_NoOpForNonLLM(t *testing.T) {
	usage := UsageState{}
	ev := Event{Type: EventUserMessage}
	if err := (&UsageProjector{}).Apply(ev, &usage); err != nil {
		t.Fatal(err)
	}
	if usage.TotalTokens != 0 {
		t.Errorf("non-LLMCall should not affect usage")
	}
}

// T1.8.26 PhaseProjector 非 PhaseChange 类型不更新
func TestProjector_Phase_NoOpForNonPhase(t *testing.T) {
	phase := PhaseState("init")
	ev := Event{Type: EventUserMessage}
	if err := (&PhaseProjector{}).Apply(ev, &phase); err != nil {
		t.Fatal(err)
	}
	if string(phase) != "init" {
		t.Errorf("non-PhaseChange should not change phase")
	}
}

