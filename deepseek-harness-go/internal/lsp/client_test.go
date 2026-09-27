package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// fakeLSPServer 是一个内存版 LSP server（用 in-memory pipe 模拟 stdio）。
//
// 协议：
//   - 读 initialize → 回 result { capabilities: {} }
//   - 读 textDocument/hover → 回 result { contents: "fake-hover" }
//   - 读 textDocument/references → 回 [{uri, range}]
//   - 读 textDocument/definition → 回 []
//   - 读 shutdown → 回 null
type fakeLSPServer struct {
	in  io.Reader
	out io.Writer
}

func (f *fakeLSPServer) loop() {
	sc := bufio.NewReader(f.in)
	for {
		body, err := readFrame(sc)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				// 错误时退出
			}
			return
		}
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			continue
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"capabilities": map[string]any{}}
		case "textDocument/hover":
			result = map[string]any{"contents": "fake-hover"}
		case "textDocument/references":
			result = []map[string]any{{"uri": "file:///a.go", "range": map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": map[string]any{"line": 0, "character": 5}}}}
		case "textDocument/definition":
			result = []any{}
		case "shutdown":
			result = nil
		default:
			result = nil
		}
		respBody, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		hdr := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(respBody))
		f.out.Write([]byte(hdr))
		f.out.Write(respBody)
	}
}

// TestReadFrame 是 LSP frame parser 的单元测试（不需要真 LSP server）。
func TestReadFrame(t *testing.T) {
	var buf bytes.Buffer
	body := []byte(`{"hello":"world"}`)
	fmt.Fprintf(&buf, "Content-Length: %d\r\n\r\n", len(body))
	buf.Write(body)
	rd := bufio.NewReader(&buf)
	got, err := readFrame(rd)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	if string(got) != string(body) {
		t.Errorf("frame = %q", got)
	}
}

// TestReadFrame_Bad tests missing Content-Length.
func TestReadFrame_Bad(t *testing.T) {
	rd := bufio.NewReader(bytes.NewReader([]byte("\r\n")))
	_, err := readFrame(rd)
	if err == nil || !strings.Contains(err.Error(), "missing content-length") {
		t.Errorf("err = %v", err)
	}
}

// 内存双向 pipe + 用 fake LSPServer + Client。
func newFakeClient(t *testing.T) *Client {
	t.Helper()
	// 我们手动构造 Client：绕开 Launch()
	pr1, pw1 := io.Pipe() // server 读, client 写
	pr2, pw2 := io.Pipe() // client 读, server 写
	srv := &fakeLSPServer{in: pr1, out: pw2}
	go srv.loop()

	c := &Client{
		cmd:     nil,
		stdin:   pw1,
		stdout:  bufio.NewReader(pr2),
		pending: make(map[int64]chan *json.RawMessage),
		closed:  make(chan struct{}),
	}
	go c.readPump()
	return c
}

func TestClient_Initialize(t *testing.T) {
	c := newFakeClient(t)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Initialize(ctx, "file:///root"); err != nil {
		t.Fatalf("init: %v", err)
	}
}

func TestClient_Hover(t *testing.T) {
	c := newFakeClient(t)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Initialize(ctx, "file:///root"); err != nil {
		t.Fatal(err)
	}
	h, err := c.Hover(ctx, "file:///a.go", Position{Line: 0, Character: 4})
	if err != nil {
		t.Fatalf("hover: %v", err)
	}
	if h == nil {
		t.Fatal("hover nil")
	}
	if h.Contents != "fake-hover" {
		t.Errorf("hover contents = %v", h.Contents)
	}
}

func TestClient_References(t *testing.T) {
	c := newFakeClient(t)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Initialize(ctx, "file:///root"); err != nil {
		t.Fatal(err)
	}
	locs, err := c.References(ctx, "file:///a.go", Position{Line: 1, Character: 2})
	if err != nil {
		t.Fatalf("refs: %v", err)
	}
	if len(locs) != 1 || locs[0].URI != "file:///a.go" {
		t.Errorf("locs = %+v", locs)
	}
}

func TestLaunch_MissingCommand(t *testing.T) {
	_, err := Launch("")
	if err == nil {
		t.Error("expected error for empty command")
	}
}
