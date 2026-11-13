package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"deepseek-harness-go/internal/llm"
	"deepseek-harness-go/internal/store"
)

// TestServer_SessionDetail_JSONShape 断言 GET /api/sessions/{id} 返回
// snake_case 字段，与 /api/sessions 的列表契约保持一致。
//
// 历史上该端点直接序列化 store.Session，输出的是 Go 字段名
// （ID / CreatedAt / Messages…），客户端需要写两套解析逻辑。
func TestServer_SessionDetail_JSONShape(t *testing.T) {
	st := store.NewMapStore()
	srv := newServer(t, newFakeRunner(), st)

	// 造一个带消息的会话。
	sess, err := st.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := st.Append(t.Context(), sess.ID, llm.Message{
		Role: llm.RoleUser, Content: "hello",
	}); err != nil {
		t.Fatalf("append: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sess.ID, nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := do(t, srv, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// 必须存在这些 snake_case 键。
	for _, key := range []string{"id", "created_at", "updated_at", "rounds", "preview", "messages", "usage"} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing key %q in session detail JSON", key)
		}
	}
	// 旧的 Go 字段名不应再出现。
	for _, key := range []string{"ID", "CreatedAt", "UpdatedAt", "Messages", "UsageTotal"} {
		if _, ok := got[key]; ok {
			t.Errorf("unexpected legacy key %q in session detail JSON", key)
		}
	}
}

// TestServer_StreamLoopDoneHasSessionID 断言 SSE 的 loop_done 帧带有
// session_id 与 usage —— 前端需要 session_id 才能在新建会话后继续对话。
func TestServer_StreamLoopDoneHasSessionID(t *testing.T) {
	srv := newServer(t, newFakeRunner(), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/agent/stream?prompt=hi", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := do(t, srv, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "session_id") {
		t.Errorf("loop_done frame missing session_id:\n%s", body)
	}
	if !strings.Contains(body, "usage") {
		t.Errorf("loop_done frame missing usage:\n%s", body)
	}
	// runner 自己发出的 LoopDone 不应被转发（否则客户端会收到两个
	// 语义重复的终止帧）。
	if n := strings.Count(body, "event: loop_done"); n != 1 {
		t.Errorf("loop_done frames = %d, want exactly 1:\n%s", n, body)
	}
}
