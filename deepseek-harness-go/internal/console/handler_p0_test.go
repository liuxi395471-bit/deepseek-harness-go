package console

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- P0 测试：session message edit + delete ---

func TestEditAndDeleteMessage(t *testing.T) {
	stub := stubSessionBackend{items: []SessionItem{{SID: "s1", Title: "T"}}}
	cfg := Config{AuthToken: "tok"}
	srv := New(cfg, Deps{Sessions: stub})
	mux := srv.Handler().(*http.ServeMux)

	t.Run("edit user message returns 200", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/console/sessions/s1/messages/0", nil)
		req.Header.Set("Authorization", "Bearer tok")
		body := map[string]string{"content": "edited"}
		b, _ := json.Marshal(body)
		req.Body = nopCloser(bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("edit code = %d, body=%s", rec.Code, rec.Body.String())
		}
	})
}

func TestDeleteMessage(t *testing.T) {
	stub := stubSessionBackend{items: []SessionItem{{SID: "s1", Title: "T"}}}
	cfg := Config{AuthToken: "tok"}
	srv := New(cfg, Deps{Sessions: stub})
	mux := srv.Handler().(*http.ServeMux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/console/sessions/s1/messages/2", nil)
	req.Header.Set("Authorization", "Bearer tok")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete code = %d, body=%s", rec.Code, rec.Body.String())
	}
}

// --- P0 测试：plugin install/uninstall ---

func TestPluginInstallUninstall(t *testing.T) {
	cfg := Config{AuthToken: "tok"}
	srv := New(cfg, Deps{Plugins: stubPluginBackend{}})
	mux := srv.Handler().(*http.ServeMux)

	t.Run("install", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/console/plugins/echo/install", nil)
		req.Header.Set("Authorization", "Bearer tok")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("install code = %d, body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("uninstall", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/console/plugins/echo/uninstall", nil)
		req.Header.Set("Authorization", "Bearer tok")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("uninstall code = %d, body=%s", rec.Code, rec.Body.String())
		}
	})
}

// --- P0 测试：model create/remove + audit export ---

func TestModelCreateAndAuditQuery(t *testing.T) {
	cfg := Config{AuthToken: "tok"}
	ma := &mockModelAdapter{}
	srv := New(cfg, Deps{Models: ma, Audit: &mockAuditBackend{}})
	mux := srv.Handler().(*http.ServeMux)

	t.Run("create returns 201", func(t *testing.T) {
		rec := httptest.NewRecorder()
		body, _ := json.Marshal(ModelItem{Channel: "c1", Model: "m1", Protocol: "openai"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/console/models", nil)
		req.Header.Set("Authorization", "Bearer tok")
		req.Body = nopCloser(bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create code = %d, body=%s", rec.Code, rec.Body.String())
		}
		if len(ma.created) != 1 || ma.created[0].Channel != "c1" {
			t.Fatalf("unexpected created: %+v", ma.created)
		}
	})

	t.Run("duplicate returns 409", func(t *testing.T) {
		rec := httptest.NewRecorder()
		body, _ := json.Marshal(ModelItem{Channel: "dup", Model: "m1", Protocol: "openai"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/console/models", nil)
		req.Header.Set("Authorization", "Bearer tok")
		req.Body = nopCloser(bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		// first call should succeed
		if rec.Code != http.StatusCreated {
			t.Fatalf("first create code = %d, body=%s", rec.Code, rec.Body.String())
		}
		// second call should conflict
		rec2 := httptest.NewRecorder()
		req2 := httptest.NewRequest(http.MethodPost, "/api/v1/console/models", nil)
		req2.Header.Set("Authorization", "Bearer tok")
		req2.Body = nopCloser(bytes.NewReader(body))
		req2.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusConflict {
			t.Fatalf("duplicate code = %d, want 409", rec2.Code)
		}
	})

	t.Run("audit query returns items", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/console/audit?limit=5", nil)
		req.Header.Set("Authorization", "Bearer tok")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("audit code = %d, body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Items []AuditRecord `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(resp.Items) != 2 {
			t.Fatalf("expected 2 items, got %d", len(resp.Items))
		}
	})

	t.Run("audit export streams JSONL", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/console/audit/export?limit=5", nil)
		req.Header.Set("Authorization", "Bearer tok")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("export code = %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "tool_call") {
			t.Fatalf("missing tool_call in body: %s", rec.Body.String())
		}
	})
}

// --- P0 测试：任务提交/重试 ---

func TestTaskSubmitAndRetry(t *testing.T) {
	cfg := Config{AuthToken: "tok"}
	ta := &mockTaskAdapter{}
	srv := New(cfg, Deps{Tasks: ta})
	mux := srv.Handler().(*http.ServeMux)

	t.Run("submit 201", func(t *testing.T) {
		rec := httptest.NewRecorder()
		body, _ := json.Marshal(map[string]string{"title": "t1", "input": "do x", "profile": "headless"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/console/tasks", nil)
		req.Header.Set("Authorization", "Bearer tok")
		req.Body = nopCloser(bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("submit code = %d, body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("retry 201", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/console/tasks/t-1/retry", nil)
		req.Header.Set("Authorization", "Bearer tok")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("retry code = %d, body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("retry running returns 409", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/console/tasks/t-running/retry", nil)
		req.Header.Set("Authorization", "Bearer tok")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusConflict {
			t.Fatalf("retry running code = %d, want 409", rec.Code)
		}
	})
}

// --- P0 测试：审批批量 ---

func TestApprovalBatchDecide(t *testing.T) {
	cfg := Config{AuthToken: "tok"}
	q := newApprovalQueue()
	id1, _, _ := q.Enqueue(ApprovalItem{Tool: "shell"})
	id2, _, _ := q.Enqueue(ApprovalItem{Tool: "write"})
	srv := New(cfg, Deps{Approvals: &ApprovalsAdapter{Queue: q}})
	mux := srv.Handler().(*http.ServeMux)

	rec := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]any{"ids": []string{id1, id2}, "decision": "deny"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/console/approvals/decide-batch", nil)
	req.Header.Set("Authorization", "Bearer tok")
	req.Body = nopCloser(bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("batch decide code = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Total  int               `json:"total"`
		Failed []map[string]string `json:"failed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Total != 2 {
		t.Fatalf("total = %d, want 2", resp.Total)
	}
	if len(resp.Failed) != 0 {
		t.Fatalf("expected no failures, got %+v", resp.Failed)
	}
	if items := q.List(); len(items) != 0 {
		t.Fatalf("queue should be empty, got %d", len(items))
	}
}

// --- mocks ---

type mockModelAdapter struct {
	created []ModelItem
}

func (m *mockModelAdapter) List(_ context.Context) ([]ModelItem, error) {
	return m.created, nil
}
func (m *mockModelAdapter) Update(_ context.Context, _ string, _ ModelItem) error {
	return nil
}
func (m *mockModelAdapter) Create(_ context.Context, item ModelItem) error {
	for _, c := range m.created {
		if c.Channel == item.Channel {
			return ErrModelExists
		}
	}
	m.created = append(m.created, item)
	return nil
}
func (m *mockModelAdapter) Remove(_ context.Context, channel string) error {
	for i, c := range m.created {
		if c.Channel == channel {
			m.created = append(m.created[:i], m.created[i+1:]...)
			return nil
		}
	}
	return ErrModelNotFound
}
func (m *mockModelAdapter) Ping(_ context.Context, _ string) (PingResult, error) {
	return PingResult{OK: true, LatencyMs: 1}, nil
}

type mockAuditBackend struct{}

func (m *mockAuditBackend) Query(_ context.Context, _ int) ([]AuditRecord, error) {
	return []AuditRecord{
		{Event: "tool_call", Tool: "shell"},
		{Event: "llm_call", Model: "deepseek"},
	}, nil
}
func (m *mockAuditBackend) Export(_ context.Context, w io.Writer, _ int) error {
	_, _ = w.Write([]byte(`{"event":"tool_call"}` + "\n"))
	_, _ = w.Write([]byte(`{"event":"llm_call"}` + "\n"))
	return nil
}

type mockTaskAdapter struct{}

func (m *mockTaskAdapter) List(_ context.Context, _ string) ([]TaskItem, error) {
	return nil, nil
}
func (m *mockTaskAdapter) Cancel(_ context.Context, _ string) error { return nil }
func (m *mockTaskAdapter) Submit(_ context.Context, _, _, _ string) (TaskItem, error) {
	return TaskItem{ID: "t-new", State: "pending"}, nil
}
func (m *mockTaskAdapter) Retry(_ context.Context, id string) (TaskItem, error) {
	if id == "t-running" {
		return TaskItem{}, ErrTaskRunning
	}
	return TaskItem{ID: "t-r" + id, State: "pending"}, nil
}
