package console

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeRunner 模拟 agent.StreamingRunner。
type fakeRunner struct{}

func (fakeRunner) RunStream(_ ctx, _ string, _ string) (<-chan struct{}, <-chan struct{}) {
	return nil, nil
}

// minimalDeps 构造一组可用的 Deps 用于 handler 单测。
// 多数测试只需要其中一两个 backend；其他字段 nil。
func minimalDeps() Deps {
	return Deps{}
}

func writeJSONReq(t *testing.T, w *httptest.ResponseRecorder, r *http.Request, body any) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r.Body = nopCloser(bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
}

// nopCloser 是测试用的 io.ReadCloser（避免 import 完整 io）。
func nopCloser(r interface{ Read(p []byte) (int, error) }) readCloser {
	return readCloser{r: r}
}

type readCloser struct {
	r interface{ Read(p []byte) (int, error) }
}

func (rc readCloser) Read(p []byte) (int, error) { return rc.r.Read(p) }
func (rc readCloser) Close() error                { return nil }

// --- 测试：auth + health ---

func TestAuthAndHealth(t *testing.T) {
	cfg := Config{AuthToken: "secret-token"}
	srv := New(cfg, minimalDeps())
	srv.SetSPAFS(emptyFS{})

	mux := srv.Handler().(*http.ServeMux)

	t.Run("health no token returns 200", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/console/health/", nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("health code = %d", rec.Code)
		}
	})

	t.Run("sessions missing token returns 401", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/console/sessions/", nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("sessions code = %d, want 401", rec.Code)
		}
	})

	t.Run("sessions wrong token returns 401", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/console/sessions/", nil)
		req.Header.Set("Authorization", "Bearer wrong")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("sessions code = %d, want 401", rec.Code)
		}
	})

	t.Run("empty token config rejects all", func(t *testing.T) {
		srv2 := New(Config{}, minimalDeps())
		srv2.SetSPAFS(emptyFS{})
		mux2 := srv2.Handler().(*http.ServeMux)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/console/sessions/", nil)
		req.Header.Set("Authorization", "Bearer anything")
		mux2.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("no-token-config code = %d, want 503", rec.Code)
		}
	})
}

// --- 测试：embed.FS 至少存在 index.html ---

func TestEmbedFSHasIndex(t *testing.T) {
	fsys := spaFS()
	f, err := fsys.Open("index.html")
	if err != nil {
		t.Fatalf("spaFS index.html: %v", err)
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if stat.Size() < 100 {
		t.Fatalf("index.html too small: %d", stat.Size())
	}
}

// --- 测试：sessions handler（带 stub backend） ---

type stubSessionBackend struct {
	items []SessionItem
	err   error
}

func (s stubSessionBackend) List(_ ctx, _ int, _ string) ([]SessionItem, error) {
	return s.items, s.err
}
func (s stubSessionBackend) Get(_ ctx, sid string) (SessionDetail, error) {
	for _, it := range s.items {
		if it.SID == sid {
			return SessionDetail{SessionItem: it}, nil
		}
	}
	return SessionDetail{}, ErrSessionNotFound
}
func (s stubSessionBackend) Create(_ ctx, title, model string) (SessionItem, error) {
	return SessionItem{SID: "new-sid", Title: title, Model: model}, nil
}
func (s stubSessionBackend) Delete(_ ctx, _ string) error { return nil }
func (s stubSessionBackend) Send(_ ctx, _, _ string) (SendResult, error) {
	return SendResult{}, nil
}
func (s stubSessionBackend) SendStream(_ ctx, _, _ string, _ chan<- SessionFrame) error {
	return nil
}
func (s stubSessionBackend) EditMessage(_ ctx, _ string, _ int64, _ string) error {
	return nil
}
func (s stubSessionBackend) DeleteMessage(_ ctx, _ string, _ int64) error {
	return nil
}
func (s stubSessionBackend) EventsSince(_ ctx, _ string, since int64) ([]SessionEvent, int64, error) {
	return nil, since, nil
}
func (s stubSessionBackend) SetMessageRating(_ ctx, _ string, _ int64, _ int, _ string) error {
	return nil
}
func (s stubSessionBackend) ListMessageRatings(_ ctx, _ string) (map[int64]int, error) {
	return nil, nil
}
func (s stubSessionBackend) ExportMarkdown(_ ctx, _ string) (string, error) {
	return "", nil
}
func (s stubSessionBackend) ExportJSONL(_ ctx, _ string) (string, error) {
	return "", nil
}
func (s stubSessionBackend) Regenerate(_ ctx, _ string, _ int64, _ chan<- SessionFrame) error {
	return nil
}

type ctx = context.Context

func TestSessionsListAndCreate(t *testing.T) {
	stub := stubSessionBackend{items: []SessionItem{{SID: "a", Title: "T1"}}}
	cfg := Config{AuthToken: "tok"}
	srv := New(cfg, Deps{Sessions: stub})
	mux := srv.Handler().(*http.ServeMux)

	t.Run("list with token returns items", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/console/sessions", nil)
		req.Header.Set("Authorization", "Bearer tok")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("list code = %d, body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Items []SessionItem `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(resp.Items) != 1 || resp.Items[0].SID != "a" {
			t.Fatalf("unexpected items: %+v", resp.Items)
		}
	})

	t.Run("create returns 201", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/console/sessions/", nil)
		req.Header.Set("Authorization", "Bearer tok")
		writeJSONReq(t, rec, req, map[string]string{"title": "新会话", "model": "deepseek-chat"})
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create code = %d, body=%s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "new-sid") {
			t.Fatalf("body missing sid: %s", rec.Body.String())
		}
	})
}

// --- 测试：plugins enable/disable ---

type stubPluginBackend struct {
	items []PluginItem
}

func (s stubPluginBackend) List(_ ctx) ([]PluginItem, error) { return s.items, nil }
func (s stubPluginBackend) Enable(_ ctx, name string) error {
	if name == "missing" {
		return ErrPluginNotFound
	}
	return nil
}
func (s stubPluginBackend) Disable(_ ctx, name string) error {
	if name == "missing" {
		return ErrPluginNotFound
	}
	return nil
}
func (s stubPluginBackend) Install(_ ctx, _ string, _ string) error { return nil }
func (s stubPluginBackend) Uninstall(_ ctx, _ string) error      { return nil }

func TestPluginEnable(t *testing.T) {
	cfg := Config{AuthToken: "tok"}
	srv := New(cfg, Deps{Plugins: stubPluginBackend{items: []PluginItem{
		{Name: "echo", Kind: "grpc", State: "loaded"},
	}}})
	mux := srv.Handler().(*http.ServeMux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/console/plugins/echo/enable", nil)
	req.Header.Set("Authorization", "Bearer tok")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable code = %d, body=%s", rec.Code, rec.Body.String())
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/console/plugins/missing/enable", nil)
	req2.Header.Set("Authorization", "Bearer tok")
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("missing enable code = %d, want 404", rec2.Code)
	}
}

// --- 测试：approvals queue ---

func TestApprovalQueueEnqueueAndDecide(t *testing.T) {
	q := newApprovalQueue()
	id, resultCh, err := q.Enqueue(ApprovalItem{Tool: "shell", Args: map[string]any{"cmd": "rm -rf /"}})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if id == "" {
		t.Fatalf("empty id")
	}

	items := q.List()
	if len(items) != 1 {
		t.Fatalf("list len = %d, want 1", len(items))
	}

	if err := q.Decide(id, "deny"); err != nil {
		t.Fatalf("decide: %v", err)
	}
	select {
	case dec := <-resultCh:
		if dec != "deny" {
			t.Fatalf("dec = %q, want deny", dec)
		}
	default:
		t.Fatalf("result channel empty after decide")
	}

	// 二次 decide 应返回 ErrApprovalNotFound
	if err := q.Decide(id, "allow"); err == nil {
		t.Fatalf("second decide: expected error, got nil")
	}
}

// --- 测试：SPA fallback ---

func TestSPAFallback(t *testing.T) {
	cfg := Config{AuthToken: "tok"}
	srv := New(cfg, minimalDeps())
	srv.SetSPAFS(spaFS())
	mux := srv.Handler().(*http.ServeMux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/console/", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/console/ code = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "DeepSeek Harness") {
		t.Fatalf("body missing app shell text: %s", rec.Body.String())
	}

	// /console/foo → fallback to index.html
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/console/foo/bar/baz", nil)
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("/console/foo/bar/baz code = %d, body=%s", rec2.Code, rec2.Body.String())
	}
	if !strings.Contains(rec2.Body.String(), "DeepSeek Harness") {
		t.Fatalf("fallback body missing app shell: %s", rec2.Body.String())
	}
}

// --- 测试：state KV ---

func TestStateStore(t *testing.T) {
	s, err := newStateStore("")
	if err != nil {
		t.Fatalf("newStateStore: %v", err)
	}
	if _, ok, _ := s.Get(nil, "k1"); ok {
		t.Fatalf("empty store should return not found")
	}
	if err := s.Set(nil, "k1", "v1"); err != nil {
		t.Fatalf("set: %v", err)
	}
	v, ok, err := s.Get(nil, "k1")
	if err != nil || !ok || v != "v1" {
		t.Fatalf("get k1: v=%q ok=%v err=%v", v, ok, err)
	}
}

// --- 测试：approve queue resolver ---

func TestApprovalResolver(t *testing.T) {
	q := newApprovalQueue()
	res := q.Resolver()
	id, _, err := q.Enqueue(ApprovalItem{Tool: "shell"})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	// 启动 goroutine 异步 Decide
	go func() {
		_ = q.Decide(id, "allow")
	}()
	dec, err := res(context.Background(), id)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if dec != "allow" {
		t.Fatalf("dec = %q", dec)
	}
}
