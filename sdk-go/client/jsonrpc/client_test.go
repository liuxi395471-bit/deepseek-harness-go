package jsonrpc

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// memTransport 是同进程双向 pipe（in-memory transport）。
type memTransport struct {
	in  chan []byte
	out chan []byte
}

func newMemPair() (*memTransport, *memTransport) {
	a := &memTransport{make(chan []byte, 16), make(chan []byte, 16)}
	b := &memTransport{a.out, a.in}
	return a, b
}

func (m *memTransport) Send(_ context.Context, data []byte) error {
	cp := make([]byte, len(data))
	copy(cp, data)
	m.out <- cp
	return nil
}

func (m *memTransport) Recv(ctx context.Context) ([]byte, error) {
	select {
	case data := <-m.in:
		return data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *memTransport) Close() error { return nil }

// fakeServerPump 模拟服务端 echo。
type fakeServerPump struct {
	t      *memTransport
	calls  int
}

func (s *fakeServerPump) loop() {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		data, err := s.t.Recv(ctx)
		cancel()
		if err != nil {
			return
		}
		s.calls++
		// 把响应原样返回（带 id 0 → 实际是 request，需要补 JSON-RPC）
		var req Request
		_ = decodeJSON(data, &req)
		if req.ID == 0 {
			continue
		}
		resp := Response{JSONRPC: "2.0", ID: req.ID, Result: []byte(`"ok"`)}
		out, _ := encodeJSON(resp)
		_ = s.t.Send(context.Background(), out)
	}
}

func decodeJSON(b []byte, v any) error {
	return json.Unmarshal(b, v)
}
func encodeJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

func TestCall_RoundTrip(t *testing.T) {
	a, b := newMemPair()
	srv := &fakeServerPump{t: b}
	go srv.loop()
	c := New(a)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	resp, err := c.Call(ctx, "echo", map[string]string{"x": "1"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if resp == nil {
		t.Fatal("nil resp")
	}
	if string(resp.Result) != `"ok"` {
		t.Errorf("result = %s, want %q", resp.Result, `"ok"`)
	}
	if srv.calls != 1 {
		t.Errorf("server calls = %d", srv.calls)
	}
}

func TestNotify(t *testing.T) {
	a, b := newMemPair()
	srv := &fakeServerPump{t: b}
	go srv.loop()
	c := New(a)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := c.Notify(ctx, "tick", nil); err != nil {
		t.Fatalf("notify: %v", err)
	}
	// 等待 server 收到
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if srv.calls >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if srv.calls < 1 {
		t.Errorf("server did not receive notify")
	}
}

func TestPushFromServer(t *testing.T) {
	a, b := newMemPair()
	c := New(a)
	defer c.Close()
	// server 直接发一个 notification
	go func() {
		note := Request{JSONRPC: "2.0", Method: "server.push"}
		out, _ := encodeJSON(note)
		_ = b.Send(context.Background(), out)
	}()
	select {
	case got := <-c.Push():
		if got.Method != "server.push" {
			t.Errorf("push method = %s", got.Method)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("no push received")
	}
}
