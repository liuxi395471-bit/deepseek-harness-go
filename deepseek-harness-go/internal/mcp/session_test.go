// 高层 MCP Session 的测试（DESIGN-v2 §D.1）。
//
// 策略：在一个 goroutine 中搭建假 MCP 服务器，通过 io.Pipe
// 连接到真实的 Session。假服务器从管道一端读取请求，查找方法，
// 并写回预置的响应。
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeServer 是一个由 goroutine 驱动的 MCP 响应器。
type fakeServer struct {
	in  io.Reader
	out io.WriteCloser

	// initialized 记录 notifications/initialized 是否已到达。
	initializedMu sync.Mutex
	initialized   bool
}

// run 从 in 读取 JSON-RPC 请求，向 out 写入 JSON-RPC 响应。
// 每行为一条 JSON 消息（换行符分隔）。
func (f *fakeServer) run(t *testing.T, ctx context.Context, tools []Tool, serverInfo ServerInfo) {
	t.Helper()
	dec := json.NewDecoder(f.in)
	for {
		if ctx.Err() != nil {
			return
		}
		var req map[string]any
		if err := dec.Decode(&req); err != nil {
			if errors.Is(err, io.EOF) || ctx.Err() != nil {
				return
			}
			continue
		}
		method, _ := req["method"].(string)
		// 通知没有 ID。
		id, hasID := req["id"]
		if !hasID {
			if method == "notifications/initialized" {
				f.initializedMu.Lock()
				f.initialized = true
				f.initializedMu.Unlock()
			}
			continue
		}
		// 数字 ID 可能被反序列化为 float64。
		var idNum int64
		switch v := id.(type) {
		case float64:
			idNum = int64(v)
		case int64:
			idNum = v
		}
		resp := f.handle(ctx, idNum, method, req["params"], tools, serverInfo)
		if err := json.NewEncoder(f.out).Encode(resp); err != nil {
			return
		}
	}
}

func (f *fakeServer) handle(_ context.Context, id int64, method string, params any, tools []Tool, info ServerInfo) map[string]any {
	switch method {
	case "initialize":
		return map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"result": map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      info,
			},
		}
	case "tools/list":
		raws := make([]map[string]any, 0, len(tools))
		for _, t := range tools {
			r := map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": t.Parameters,
			}
			if t.Version != "" {
				r["version"] = t.Version
			}
			raws = append(raws, r)
		}
		return map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"result":  map[string]any{"tools": raws},
		}
	case "tools/call":
		// 回显：将参数原样作为 JSON 文本内容返回。
		paramsBytes, _ := json.Marshal(params)
		return map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"result": map[string]any{
				"content": []map[string]any{{
					"type": "text",
					"text": "echo: " + string(paramsBytes),
				}},
				"isError": false,
			},
		}
	case "resources/list", "prompts/list":
		return map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"error":   map[string]any{"code": -32601, "message": "method not found"},
		}
	default:
		return map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"error":   map[string]any{"code": -32601, "message": "method not found"},
		}
	}
}

// startFakeMCP 通过 io.Pipe 将假服务器连接到真实 Session。
// 返回会话、一个清理函数以及假服务器的状态（以便测试
// 对 initialized 通知进行断言）。
func startFakeMCP(t *testing.T, tools []Tool, info ServerInfo) (*Session, func(), *fakeServer) {
	t.Helper()
	// 管道方向：客户端写 -> 服务器读；服务器写 -> 客户端读。
	clientToServerR, clientToServerW := io.Pipe()
	serverToClientR, serverToClientW := io.Pipe()

	srv := NewServerWithIO(Config{}, clientToServerW, serverToClientR)
	ctx, cancel := context.WithCancel(context.Background())
	go srv.Listen(ctx)

	fake := &fakeServer{in: clientToServerR, out: serverToClientW}
	fakeDone := make(chan struct{})
	go func() {
		defer close(fakeDone)
		fake.run(t, ctx, tools, info)
	}()

	sess, err := NewSessionWithServer(ctx, srv)
	if err != nil {
		cancel()
		t.Fatalf("NewSessionWithServer: %v", err)
	}
	teardown := func() {
		cancel()
		_ = sess.Close()
		// 关闭管道以解除假服务器的阻塞。
		_ = clientToServerW.Close()
		_ = serverToClientR.Close()
		<-fakeDone
	}
	return sess, teardown, fake
}

func TestSession_InitializeAndListTools(t *testing.T) {
	want := []Tool{
		{
			Name:        "add",
			Description: "add two numbers",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"a":{"type":"number"},"b":{"type":"number"}}}`),
			Version:     "1.0.0",
		},
		{
			Name:        "echo",
			Description: "echo args",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
	}
	sess, stop, fake := startFakeMCP(t, want, ServerInfo{Name: "fake-mcp", Version: "0.1.0"})
	defer stop()

	if got := sess.ServerInfo(); got.Name != "fake-mcp" || got.Version != "0.1.0" {
		t.Errorf("ServerInfo = %+v", got)
	}
	got, err := sess.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(got) != 2 || got[0].Name != "add" || got[1].Name != "echo" {
		t.Errorf("ListTools = %+v, want add+echo", got)
	}
	if string(got[0].Parameters) != string(want[0].Parameters) {
		t.Errorf("add params = %s, want %s", got[0].Parameters, want[0].Parameters)
	}

	// 必须已发送 initialized 通知。
	fake.initializedMu.Lock()
	defer fake.initializedMu.Unlock()
	if !fake.initialized {
		t.Error("notifications/initialized was not sent")
	}
}

func TestSession_CallTool(t *testing.T) {
	sess, stop, _ := startFakeMCP(t, []Tool{{Name: "echo"}}, ServerInfo{Name: "fake", Version: "0"})
	defer stop()

	blocks, isErr, err := sess.CallTool(context.Background(), "echo", map[string]any{"text": "hi"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if isErr {
		t.Error("unexpected isError")
	}
	if len(blocks) != 1 || blocks[0].Type != "text" || !strings.Contains(blocks[0].Text, "echo:") {
		t.Errorf("blocks = %+v", blocks)
	}
}

func TestSession_ListResourcesSwallowsMethodNotFound(t *testing.T) {
	sess, stop, _ := startFakeMCP(t, nil, ServerInfo{Name: "fake", Version: "0"})
	defer stop()

	got, err := sess.ListResources(context.Background())
	if err != nil {
		t.Fatalf("ListResources should swallow method-not-found, got: %v", err)
	}
	if got != nil {
		t.Errorf("got = %v, want nil", got)
	}
}

func TestSession_ListPromptsSwallowsMethodNotFound(t *testing.T) {
	sess, stop, _ := startFakeMCP(t, nil, ServerInfo{Name: "fake", Version: "0"})
	defer stop()

	got, err := sess.ListPrompts(context.Background())
	if err != nil {
		t.Fatalf("ListPrompts should swallow method-not-found, got: %v", err)
	}
	if got != nil {
		t.Errorf("got = %v, want nil", got)
	}
}

func TestSession_ConcurrentSend(t *testing.T) {
	// 5 个并发 ListTools 调用全部成功；验证 Send 是并发安全的。
	sess, stop, _ := startFakeMCP(t, []Tool{{Name: "t"}}, ServerInfo{Name: "f", Version: "0"})
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	errCh := make(chan error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := sess.ListTools(ctx); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent ListTools: %v", err)
	}
}
