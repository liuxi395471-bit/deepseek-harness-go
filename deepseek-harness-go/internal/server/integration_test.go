package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"deepseek-harness-go/internal/acp"
	"deepseek-harness-go/internal/agent"
	"deepseek-harness-go/internal/storage"
)

// TestIntegration_ACPMounted 验证 /acp/* 路由挂到了 Server 上。
func TestIntegration_ACPMounted(t *testing.T) {
	srv := newTestServer(t)

	acpSrv := acp.NewServer(acp.NewTaskExecutorAdapter(srv.tasksExec), storage.NewMemoryStorage(), "")
	srv.inner.SetACPServer(acpSrv)

	// POST /acp/session/create
	body, _ := json.Marshal(map[string]any{"profile": "test"})
	r := httptest.NewRequest("POST", "/acp/session/create", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.inner.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "session_id") {
		t.Errorf("missing session_id: %s", w.Body.String())
	}
}

// TestIntegration_MCPHTTPMounted 验证 /mcp (POST) 桥接到 Gateway。
func TestIntegration_MCPHTTPMounted(t *testing.T) {
	srv := newTestServer(t)
	srv.inner.SetMCPDispatcher(MCPDispatcher(srv.inner.Router()))

	// POST /mcp — 调用 tools.list
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools.list",
	})
	r := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.inner.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"id":1`) {
		t.Errorf("missing jsonrpc id: %s", w.Body.String())
	}
}

// TestIntegration_BearerAuthDoesNotBlockACP 验证 ACP 路由不走 Bearer 鉴权。
func TestIntegration_BearerAuthDoesNotBlockACP(t *testing.T) {
	srv := newTestServer(t)
	srv.inner.cfg.AuthToken = "real-token" // 设置 token
	srv.inner.SetACPServer(acp.NewServer(acp.NewTaskExecutorAdapter(srv.tasksExec), storage.NewMemoryStorage(), ""))

	// 没带 Bearer → 仍然能调用 ACP
	body, _ := json.Marshal(map[string]any{})
	r := httptest.NewRequest("POST", "/acp/session/create", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.inner.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("ACP should not require Bearer; got status=%d body=%s", w.Code, w.Body.String())
	}

	// /api/gateway/stream 不带 Bearer → 401
	r2 := httptest.NewRequest("POST", "/api/gateway/stream", bytes.NewReader([]byte("{}")))
	w2 := httptest.NewRecorder()
	srv.inner.Handler().ServeHTTP(w2, r2)
	if w2.Code != http.StatusUnauthorized {
		t.Errorf("gateway should require Bearer; got status=%d", w2.Code)
	}
}

// testServerBundle 是个测试辅助，组合 inner Server 与 v6 task.Executor。
type testServerBundle struct {
	inner    *Server
	tasksExec *fakeTaskExec
}

func newTestServer(t *testing.T) *testServerBundle {
	t.Helper()
	te := &fakeTaskExec{}
	temp := 0.0
	srv := New(Config{AuthToken: ""}, agent.NewLoopRunner(nil, nil, agent.NewDefaultSystemPrompt(""), "test", 1, 100, &temp), nil)
	return &testServerBundle{inner: srv, tasksExec: te}
}

func TestMCP_MethodMapping(t *testing.T) {
	cases := []struct {
		method string
		ok     bool
		source string
	}{
		{"tools/list", true, "tools.list"},
		{"tools.list", true, "tools.list"},
		{"tools/call", true, "tools.call"},
		{"tools.call", true, "tools.call"},
		{"plugins/list", true, "plugins.list"},
		{"initialize", false, ""},
		{"notifications/foo", false, ""},
	}
	for _, c := range cases {
		t.Run(c.method, func(t *testing.T) {
			got, ok := mcpMethodToGatewaySource(c.method)
			if ok != c.ok {
				t.Errorf("mcpMethodToGatewaySource(%q) ok = %v, want %v", c.method, ok, c.ok)
			}
			if ok && got != c.source {
				t.Errorf("source = %q, want %q", got, c.source)
			}
		})
	}
}

func TestMCPDispatcher_ToolsList(t *testing.T) {
	r := NewRouter()
	r.Register("tools.list", func(ctx context.Context, req GatewayRequest, out chan<- GatewayEvent) error {
		_ = emit(ctx, out, req.Source, "final", map[string]any{"tools": []string{"echo"}})
		return nil
	})
	d := MCPDispatcher(r)
	resp, err := d(context.Background(), []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/list"}`))
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if !strings.Contains(string(resp), `"id":7`) {
		t.Errorf("missing id: %s", resp)
	}
	if !strings.Contains(string(resp), `"tools"`) {
		t.Errorf("missing tools: %s", resp)
	}
}

func TestMCPDispatcher_UnknownMethod(t *testing.T) {
	r := NewRouter()
	d := MCPDispatcher(r)
	resp, _ := d(context.Background(), []byte(`{"jsonrpc":"2.0","id":2,"method":"initialize"}`))
	if !strings.Contains(string(resp), `-32601`) {
		t.Errorf("expected -32601 for unknown method: %s", resp)
	}
}

func TestRouter_DispatchSync(t *testing.T) {
	r := NewRouter()
	r.Register("hello", func(ctx context.Context, req GatewayRequest, out chan<- GatewayEvent) error {
		_ = emit(ctx, out, req.Source, "final", map[string]any{"world": true})
		return nil
	})
	events, err := r.DispatchSync(context.Background(), GatewayRequest{Source: "hello"})
	if err != nil {
		t.Fatalf("dispatch sync: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d", len(events))
	}
}

func TestRouter_DispatchSync_Unknown(t *testing.T) {
	r := NewRouter()
	_, err := r.DispatchSync(context.Background(), GatewayRequest{Source: "nope"})
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Errorf("err = %v", err)
	}
}
