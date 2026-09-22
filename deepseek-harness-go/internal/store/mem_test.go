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
