package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"deepseek-harness-all/sdk-go/types"
)

func TestSendSession(t *testing.T) {
	var got types.SessionSendRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/session/send" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
		}
		_ = json.NewEncoder(w).Encode(types.SessionSendResponse{SessionID: got.SessionID, Accepted: true})
	}))
	defer srv.Close()

	c := New(srv.URL)
	resp, err := c.SendSession(context.Background(), types.SessionSendRequest{SessionID: "s1", Content: "hi"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if !resp.Accepted || resp.SessionID != "s1" {
		t.Errorf("resp = %+v", resp)
	}
	if got.Content != "hi" {
		t.Errorf("server saw content = %q", got.Content)
	}
}

func TestSendSession_AuthHeader(t *testing.T) {
	got := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(types.SessionSendResponse{Accepted: true})
	}))
	defer srv.Close()

	c := New(srv.URL).WithToken("t0k3n")
	_, _ = c.SendSession(context.Background(), types.SessionSendRequest{SessionID: "s1"})
	if got != "Bearer t0k3n" {
		t.Errorf("auth = %q", got)
	}
}

func TestSubscribeEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "sid=s1") {
			t.Errorf("missing sid: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for i := 0; i < 3; i++ {
			frame := types.Frame{Kind: types.KindEvent, Source: "x", Phase: "delta", At: time.Now()}
			b, _ := json.Marshal(frame)
			_, _ = w.Write([]byte("data: " + string(b) + "\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer srv.Close()

	c := New(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got := 0
	err := c.SubscribeEvents(ctx, "s1", func(f types.Frame) error {
		got++
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if got != 3 {
		t.Errorf("frames = %d, want 3", got)
	}
}

func TestSubscribeEvents_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := New(srv.URL)
	err := c.SubscribeEvents(context.Background(), "s1", func(f types.Frame) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "status=500") {
		t.Errorf("err = %v", err)
	}
}
