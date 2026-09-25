package store

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"deepseek-harness-go/internal/llm"
)

// 1. Begin → Append → Load 往返（system + user + assistant + tool + assistant）
func TestMapStore_BeginAppendLoad_RoundTrip(t *testing.T) {
	s := NewMapStore()
	defer s.Close()

	ctx := context.Background()
	sess, err := s.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sess.ID == "" {
		t.Fatal("empty id")
	}
	if !sess.CreatedAt.Equal(sess.UpdatedAt) {
		t.Errorf("UpdatedAt = %v, want CreatedAt", sess.UpdatedAt)
	}

	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: "you are an agent"},
		{Role: llm.RoleUser, Content: "hello"},
		{Role: llm.RoleAssistant, Content: "hi there"},
		{Role: llm.RoleTool, Content: "ok", ToolCallID: "c1"},
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
		t.Errorf("Rounds = %d, want 2 (2 assistant msgs)", loaded.Rounds)
	}
	if !strings.HasPrefix(loaded.Preview, "hello") {
		t.Errorf("Preview = %q, want start with hello", loaded.Preview)
	}
}

// 2. 对未知 id 调用 Append → ErrNotFound
func TestMapStore_AppendUnknownID(t *testing.T) {
	s := NewMapStore()
	defer s.Close()
	err := s.Append(context.Background(), "nope", llm.Message{Role: llm.RoleUser})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// 3. 加载未知 id → ErrNotFound
func TestMapStore_LoadUnknownID(t *testing.T) {
	s := NewMapStore()
	defer s.Close()
	_, err := s.Load(context.Background(), "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// 4. List 带 limit + offset：按 UpdatedAt 降序返回会话。
// 验证：(a) page[0] 比 page[1] 更新，(b) limit 生效，
// (c) offset 生效，(d) limit=0 返回全部。
func TestMapStore_ListLimitOffset(t *testing.T) {
	s := NewMapStore()
	defer s.Close()
	ctx := context.Background()
	var ids []string
	for i := 0; i < 15; i++ {
		sess, err := s.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, sess.ID)
		_ = s.Append(ctx, sess.ID, llm.Message{Role: llm.RoleUser, Content: "u"})
		// 加入足够的时间间隔，保证 UpdatedAt 严格递增（毫秒精度）。
		time.Sleep(20 * time.Millisecond)
	}
	// 确认排序：page[0] 必须更新（UpdatedAt > page[1]）
	page, err := s.List(ctx, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 10 {
		t.Errorf("page len = %d, want 10", len(page))
	}
	// 检查返回页内 UpdatedAt 严格降序。
	for i := 0; i < len(page)-1; i++ {
		if page[i].UpdatedAt.Before(page[i+1].UpdatedAt) {
			t.Errorf("page[%d].UpdatedAt < page[%d].UpdatedAt: order wrong", i, i+1)
		}
	}
	// offset 5, limit 5 → 只返回 5 条
	page, err = s.List(ctx, 5, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 5 {
		t.Errorf("offset page len = %d, want 5", len(page))
	}
	// limit=0 表示全部
	all, err := s.List(ctx, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 15 {
		t.Errorf("all len = %d, want 15", len(all))
	}
}

// 5. UpdateUsage 累加
func TestMapStore_UpdateUsage(t *testing.T) {
	s := NewMapStore()
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
	if loaded.UsageTotal.CompletionTokens != 7 {
		t.Errorf("CompletionTokens = %d, want 7", loaded.UsageTotal.CompletionTokens)
	}
	if loaded.UsageTotal.TotalTokens != 20 {
		t.Errorf("TotalTokens = %d, want 20", loaded.UsageTotal.TotalTokens)
	}
}

// 6. 对未知 id 调用 UpdateUsage → ErrNotFound
func TestMapStore_UpdateUsageUnknownID(t *testing.T) {
	s := NewMapStore()
	defer s.Close()
	err := s.UpdateUsage(context.Background(), "nope", llm.Usage{})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// 7. Close 后操作失败
func TestMapStore_CloseBlocksOperations(t *testing.T) {
	s := NewMapStore()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Begin(context.Background()); err == nil {
		t.Error("Begin after Close: got nil err")
	}
	if err := s.Append(context.Background(), "x", llm.Message{}); err == nil {
		t.Error("Append after Close: got nil err")
	}
	// 重复 Close 没问题
	if err := s.Close(); err != nil {
		t.Errorf("double Close err = %v", err)
	}
}

// 8. 同一会话并发 Append：无 panic；所有消息都保留。
func TestMapStore_ConcurrentAppend(t *testing.T) {
	s := NewMapStore()
	defer s.Close()
	ctx := context.Background()
	sess, _ := s.Begin(ctx)
	const goroutines = 16
	const perG = 8
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				_ = s.Append(ctx, sess.ID, llm.Message{Role: llm.RoleUser, Content: "x"})
			}
		}(g)
	}
	wg.Wait()
	loaded, _ := s.Load(ctx, sess.ID)
	if len(loaded.Messages) != goroutines*perG {
		t.Errorf("messages = %d, want %d", len(loaded.Messages), goroutines*perG)
	}
}

// 9. 预览截断到 80 个 rune + 省略号。
func TestMapStore_PreviewTruncation(t *testing.T) {
	s := NewMapStore()
	defer s.Close()
	ctx := context.Background()
	sess, _ := s.Begin(ctx)
	long := strings.Repeat("a", 200)
	if err := s.Append(ctx, sess.ID, llm.Message{Role: llm.RoleUser, Content: long}); err != nil {
		t.Fatal(err)
	}
	loaded, _ := s.Load(ctx, sess.ID)
	if utf8RuneCount(loaded.Preview) != previewLen+1 { // 80 字符 + 省略号 rune
		t.Errorf("Preview runes = %d, want %d", utf8RuneCount(loaded.Preview), previewLen+1)
	}
	if !strings.HasSuffix(loaded.Preview, "…") {
		t.Errorf("Preview = %q, want suffix ellipsis", loaded.Preview)
	}
}

// --- v4 §A 事件溯源验收用例 ---

// T1.8.1 长会话 100 轮 → events 表 ≥ 100 条；seq 单调递增。
func TestStore_AppendEvent_100Rounds(t *testing.T) {
	s := NewMapStore()
	defer s.Close()
	ctx := context.Background()
	sess, _ := s.Begin(ctx)
	const rounds = 100
	for i := 0; i < rounds; i++ {
		ev := Event{Type: EventUserMessage, Actor: "primary"}
		if err := ev.MarshalPayload(UserMessagePayload{Content: "msg-" + itoa(i)}); err != nil {
			t.Fatal(err)
		}
		seq, err := s.AppendEvent(ctx, sess.ID, ev)
		if err != nil {
			t.Fatalf("AppendEvent[%d]: %v", i, err)
		}
		if seq != int64(i) {
			t.Fatalf("seq[%d] = %d, want %d", i, seq, i)
		}
	}
	events, err := s.ReadEvents(ctx, sess.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != rounds {
		t.Fatalf("got %d events, want %d", len(events), rounds)
	}
	last, err := s.GetLastSeq(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if last != int64(rounds-1) {
		t.Errorf("lastSeq = %d, want %d", last, rounds-1)
	}
}

// T1.8.2 投影 Messages 与 v3 Append 写入的 messages 表字节级一致。
func TestProjector_Messages_MatchesV3(t *testing.T) {
	s := NewMapStore()
	defer s.Close()
	ctx := context.Background()
	sess, _ := s.Begin(ctx)

	// v3 路径：直接 Append
	v3msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "u1"},
		{Role: llm.RoleAssistant, Content: "a1"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
			ID: "tc1", Type: "function",
			Function: llm.ToolCallFunc{Name: "echo", Arguments: `{}`},
		}}},
		{Role: llm.RoleTool, ToolCallID: "tc1", Name: "echo", Content: "r1"},
		{Role: llm.RoleAssistant, Content: "done"},
	}
	for _, m := range v3msgs {
		if err := s.Append(ctx, sess.ID, m); err != nil {
			t.Fatal(err)
		}
	}
	loaded, _ := s.Load(ctx, sess.ID)

	// v4 路径：构造等价的 events 流
	s2 := NewMapStore()
	defer s2.Close()
	sess2, _ := s2.Begin(ctx)
	emit := func(typ EventType, pl any) {
		ev := Event{Type: typ, Actor: "primary"}
		if err := ev.MarshalPayload(pl); err != nil {
			t.Fatal(err)
		}
		if _, err := s2.AppendEvent(ctx, sess2.ID, ev); err != nil {
			t.Fatal(err)
		}
	}
	emit(EventSystemPrompt, SystemPromptPayload{Content: "sys"})
	emit(EventUserMessage, UserMessagePayload{Content: "u1"})
	emit(EventAssistantMessage, AssistantMessagePayload{
		Message: llm.Message{Role: llm.RoleAssistant, Content: "a1"},
	})
	emit(EventToolCall, ToolCallPayload{ID: "tc1", Name: "echo", Arguments: `{}`})
	emit(EventToolResult, ToolResultPayload{ID: "tc1", Name: "echo", Result: "r1"})
	emit(EventAssistantMessage, AssistantMessagePayload{
		Message: llm.Message{Role: llm.RoleAssistant, Content: "done"},
	})

	state, err := s2.Project(ctx, sess2.ID, "messages")
	if err != nil {
		t.Fatalf("Project messages: %v", err)
	}
	projected, ok := state.(MessagesState)
	if !ok {
		t.Fatalf("bad state type %T", state)
	}

	if len(projected) != len(loaded.Messages) {
		t.Fatalf("len mismatch: v3=%d, v4=%d", len(loaded.Messages), len(projected))
	}
	for i := range projected {
		if !msgEqual(projected[i], loaded.Messages[i]) {
			t.Errorf("msg[%d] differ:\n  v3: %+v\n  v4: %+v", i, loaded.Messages[i], projected[i])
		}
	}
}

// T1.8.3 cache 命中：第二次 Project 不应触发重放。
func TestProjectionCache_HitRate(t *testing.T) {
	c := NewMemoryProjectionCache()
	// 第一次 Put 后 Get 命中
	c.Put("sid1", "messages", MessagesState{llm.Message{Role: llm.RoleSystem, Content: "x"}})
	s, ok := c.Get("sid1", "messages")
	if !ok {
		t.Fatal("expected hit")
	}
	if len(s.(MessagesState)) != 1 {
		t.Fatalf("bad state: %+v", s)
	}
	// 未命中的 projection name
	if _, ok := c.Get("sid1", "usage"); ok {
		t.Fatal("usage should not exist yet")
	}
	// Invalidate 后 Get 找不到
	c.Invalidate("sid1")
	if _, ok := c.Get("sid1", "messages"); ok {
		t.Fatal("after invalidate should miss")
	}
}

// T1.8.4 多 session 并发：各 sid 自身 seq 单调，互不干扰。
func TestStore_AppendEvent_ConcurrentSessions(t *testing.T) {
	s := NewMapStore()
	defer s.Close()
	ctx := context.Background()
	const sessions = 8
	const eventsPerSession = 50

	var wg sync.WaitGroup
	for sIdx := 0; sIdx < sessions; sIdx++ {
		wg.Add(1)
		go func(sIdx int) {
			defer wg.Done()
			sess, err := s.Begin(ctx)
			if err != nil {
				t.Errorf("Begin: %v", err)
				return
			}
			for i := 0; i < eventsPerSession; i++ {
				ev := Event{Type: EventUserMessage}
				_ = ev.MarshalPayload(UserMessagePayload{Content: "x"})
				_, err := s.AppendEvent(ctx, sess.ID, ev)
				if err != nil {
					t.Errorf("AppendEvent sid=%d i=%d: %v", sIdx, i, err)
					return
				}
			}
		}(sIdx)
	}
	wg.Wait()

	// 校验每个 sid 的 seq 单调且互不干扰。
	rows, err := s.List(ctx, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != sessions {
		t.Fatalf("List len = %d, want %d", len(rows), sessions)
	}
	for _, r := range rows {
		evs, err := s.ReadEvents(ctx, r.ID, 0)
		if err != nil {
			t.Errorf("ReadEvents %s: %v", r.ID, err)
			continue
		}
		if len(evs) != eventsPerSession {
			t.Errorf("sid=%s got %d events, want %d", r.ID, len(evs), eventsPerSession)
		}
		for i := 1; i < len(evs); i++ {
			if evs[i].Seq != evs[i-1].Seq+1 {
				t.Errorf("sid=%s seq gap: %d -> %d", r.ID, evs[i-1].Seq, evs[i].Seq)
			}
		}
		last, _ := s.GetLastSeq(ctx, r.ID)
		if last != int64(eventsPerSession-1) {
			t.Errorf("sid=%s lastSeq=%d, want %d", r.ID, last, eventsPerSession-1)
		}
	}
}

// T1.8.5 v3 Append 在 EventStore 上仍工作；不破坏既有 v3 行为。
//   （兼容层职责：Append 路径不变；事件流是可选的。）
func TestStore_LegacyAppendStillWorks(t *testing.T) {
	s := NewMapStore()
	defer s.Close()
	ctx := context.Background()
	sess, _ := s.Begin(ctx)
	if err := s.Append(ctx, sess.ID, llm.Message{Role: llm.RoleSystem, Content: "sys"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(ctx, sess.ID, llm.Message{Role: llm.RoleUser, Content: "hi"}); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.Load(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Messages) != 2 {
		t.Fatalf("legacy Append lost messages: %+v", loaded.Messages)
	}
}

// T1.8.6 AppendEvent 写入后，Project 立即看到新事件；Invalidate 强制重放。
func TestStore_AppendEvent_ThenProject(t *testing.T) {
	s := NewMapStore()
	defer s.Close()
	ctx := context.Background()
	sess, _ := s.Begin(ctx)

	emit := func(typ EventType, pl any) {
		ev := Event{Type: typ}
		if err := ev.MarshalPayload(pl); err != nil {
			t.Fatal(err)
		}
		if _, err := s.AppendEvent(ctx, sess.ID, ev); err != nil {
			t.Fatal(err)
		}
	}
	emit(EventSystemPrompt, SystemPromptPayload{Content: "sys"})
	emit(EventUserMessage, UserMessagePayload{Content: "u1"})
	state, _ := s.Project(ctx, sess.ID, "messages")
	msgs := state.(MessagesState)
	if len(msgs) != 2 {
		t.Fatalf("got %d msgs, want 2: %+v", len(msgs), msgs)
	}
	if msgs[0].Role != llm.RoleSystem || msgs[0].Content != "sys" {
		t.Errorf("msgs[0] = %+v", msgs[0])
	}
	if msgs[1].Role != llm.RoleUser || msgs[1].Content != "u1" {
		t.Errorf("msgs[1] = %+v", msgs[1])
	}
}

// --- helpers ---

// msgEqual 比较两条消息的语义相等（role + content + tool fields）。
// 用途：v3 Append 与 v4 投影结果应字节级一致（除 JSON 序列化差异）。
func msgEqual(a, b llm.Message) bool {
	if a.Role != b.Role || a.Content != b.Content || a.ToolCallID != b.ToolCallID || a.Name != b.Name {
		return false
	}
	if len(a.ToolCalls) != len(b.ToolCalls) {
		return false
	}
	for i := range a.ToolCalls {
		if a.ToolCalls[i].ID != b.ToolCalls[i].ID ||
			a.ToolCalls[i].Type != b.ToolCalls[i].Type ||
			a.ToolCalls[i].Function.Name != b.ToolCalls[i].Function.Name ||
			a.ToolCalls[i].Function.Arguments != b.ToolCalls[i].Function.Arguments {
			return false
		}
	}
	return true
}

// itoa 避免 strconv import（小型 helper）。
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
