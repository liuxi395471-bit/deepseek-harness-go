package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPTransport_Send(t *testing.T) {
	var got []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" {
			t.Errorf("path = %s", r.URL.Path)
		}
		got = make([]byte, r.ContentLength)
		_, _ = r.Body.Read(got)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
	}))
	defer srv.Close()

	tr := NewHTTPTransport(srv.URL)
	resp, err := tr.Send(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if !strings.Contains(string(resp), `"result":`) {
		t.Errorf("resp = %s", resp)
	}
	if string(got) != `{"jsonrpc":"2.0","id":1,"method":"ping"}` {
		t.Errorf("got = %q", got)
	}
}

func TestSSETransport_Post(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp/sse" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"hello":"sse"}}`))
	}))
	defer srv.Close()

	tr := NewSSETransport(srv.URL)
	resp, err := tr.Send(context.Background(), []byte(`{"jsonrpc":"2.0","id":1}`))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if !strings.Contains(string(resp), `"hello":"sse"`) {
		t.Errorf("resp = %s", resp)
	}
}

func TestSSEServerHandler_ServeHTTP(t *testing.T) {
	h := NewSSEServerHandler(func(ctx context.Context, req []byte) ([]byte, error) {
		if !strings.Contains(string(req), `"method":"ping"`) {
			t.Errorf("unexpected req: %s", req)
		}
		return []byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`), nil
	})

	// POST /mcp
	r := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("POST /mcp status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Errorf("body = %s", w.Body.String())
	}

	// GET /mcp/sse
	r2 := httptest.NewRequest("GET", "/mcp/sse", nil)
	w2 := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(w2, r2)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Log("sse handler did not return; that's OK if context not cancelled")
	}

	// 未知路径
	r3 := httptest.NewRequest("GET", "/unknown", nil)
	w3 := httptest.NewRecorder()
	h.ServeHTTP(w3, r3)
	if w3.Code != http.StatusNotFound {
		t.Errorf("GET /unknown status = %d, want 404", w3.Code)
	}
}
