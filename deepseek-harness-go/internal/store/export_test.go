package store

import (
	"context"
	"strings"
	"testing"

	"deepseek-harness-go/internal/llm"
)

// 1. Export → Import 往返保留消息和用量。
func TestExportImport_RoundTrip(t *testing.T) {
	src := NewMapStore()
	defer src.Close()
	ctx := context.Background()

	sess, _ := src.Begin(ctx)
	_ = src.Append(ctx, sess.ID, llm.Message{Role: llm.RoleSystem, Content: "you are x"})
	_ = src.Append(ctx, sess.ID, llm.Message{Role: llm.RoleUser, Content: "hi"})
	_ = src.Append(ctx, sess.ID, llm.Message{Role: llm.RoleAssistant, Content: "hello"})
	_ = src.UpdateUsage(ctx, sess.ID, llm.Usage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8})

	loaded, _ := src.Load(ctx, sess.ID)
	data, err := ExportSession(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "dsh-session/v2") {
		t.Errorf("export missing version: %s", data)
	}

	dst := NewMapStore()
	defer dst.Close()
	imported, err := ImportSession(ctx, dst, data, ImportOptions{PreserveID: true})
	if err != nil {
		t.Fatal(err)
	}
	if imported.ID != sess.ID {
		t.Errorf("ID = %q, want %q", imported.ID, sess.ID)
	}
	if len(imported.Messages) != 3 {
		t.Errorf("Messages = %d, want 3", len(imported.Messages))
	}
	if imported.UsageTotal.TotalTokens != 8 {
		t.Errorf("UsageTotal = %+v", imported.UsageTotal)
	}
}

// 2. 不带 PreserveID 导入时分配新 id。
func TestImport_NewID(t *testing.T) {
	src := NewMapStore()
	defer src.Close()
	ctx := context.Background()
	sess, _ := src.Begin(ctx)
	_ = src.Append(ctx, sess.ID, llm.Message{Role: llm.RoleUser, Content: "hi"})
	loaded, _ := src.Load(ctx, sess.ID)
	data, _ := ExportSession(loaded)

	dst := NewMapStore()
	defer dst.Close()
	imported, err := ImportSession(ctx, dst, data, ImportOptions{PreserveID: false})
	if err != nil {
		t.Fatal(err)
	}
	if imported.ID == sess.ID {
		t.Errorf("ID = %q, want new id", imported.ID)
	}
	if len(imported.Messages) != 1 {
		t.Errorf("Messages = %d", len(imported.Messages))
	}
}

// 3. ResumeSession：ErrNotFound → 包装后的错误。
func TestResumeSession_NotFound(t *testing.T) {
	s := NewMapStore()
	defer s.Close()
	_, err := ResumeSession(context.Background(), s, "missing")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v", err)
	}
}

// 4. ResumeSession：找到时返回完整会话。
func TestResumeSession_Found(t *testing.T) {
	s := NewMapStore()
	defer s.Close()
	ctx := context.Background()
	sess, _ := s.Begin(ctx)
	_ = s.Append(ctx, sess.ID, llm.Message{Role: llm.RoleUser, Content: "hi"})

	loaded, err := ResumeSession(ctx, s, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Messages) != 1 {
		t.Errorf("Messages = %d", len(loaded.Messages))
	}
}

// 5. ExportAll + ImportAll：3 个会话往返。
func TestExportImport_All(t *testing.T) {
	src := NewMapStore()
	defer src.Close()
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		sess, _ := src.Begin(ctx)
		_ = src.Append(ctx, sess.ID, llm.Message{Role: llm.RoleUser, Content: "x"})
	}

	// 导出全部
	list, _ := src.List(ctx, 0, 0)
	data, err := ExportAll(ctx, src)
	if err != nil {
		t.Fatal(err)
	}

	dst := NewMapStore()
	defer dst.Close()
	n, err := ImportAll(ctx, dst, data, ImportOptions{PreserveID: true})
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("imported = %d, want 3", n)
	}
	dstList, _ := dst.List(ctx, 0, 0)
	if len(dstList) != len(list) {
		t.Errorf("dst len = %d, want %d", len(dstList), len(list))
	}
}

// 6. 非法导出 → 错误。
func TestImport_InvalidJSON(t *testing.T) {
	dst := NewMapStore()
	defer dst.Close()
	_, err := ImportSession(context.Background(), dst, []byte(`{not json`), ImportOptions{})
	if err == nil {
		t.Error("invalid json should error")
	}
}

// 7. 版本不匹配 → 错误。
func TestImport_WrongVersion(t *testing.T) {
	dst := NewMapStore()
	defer dst.Close()
	data := []byte(`{"version":"dsh-session/v1","session":{"id":"x","messages":[]}}`)
	_, err := ImportSession(context.Background(), dst, data, ImportOptions{})
	if err == nil || !strings.Contains(err.Error(), "unknown version") {
		t.Errorf("err = %v", err)
	}
}

// 8. SQLite 往返。
func TestExportImport_SQLite(t *testing.T) {
	path := makeTemp(t)
	src, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	ctx := context.Background()
	sess, _ := src.Begin(ctx)
	_ = src.Append(ctx, sess.ID, llm.Message{Role: llm.RoleSystem, Content: "x"})
	_ = src.Append(ctx, sess.ID, llm.Message{Role: llm.RoleUser, Content: "y"})
	loaded, _ := src.Load(ctx, sess.ID)
	data, _ := ExportSession(loaded)

	dst, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	imported, err := ImportSession(ctx, dst, data, ImportOptions{PreserveID: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(imported.Messages) != 2 {
		t.Errorf("Messages = %d", len(imported.Messages))
	}
}
