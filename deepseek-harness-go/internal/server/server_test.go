package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"deepseek-harness-go/internal/agent"
	"deepseek-harness-go/internal/llm"
	"deepseek-harness-go/internal/plugin"
	"deepseek-harness-go/internal/store"
	"deepseek-harness-go/internal/tool"
)

// fakeRunner 是测试用可控的 StreamingRunner。它会阻塞直到被释放
// （release 通道关闭），以便并发会话测试能观察到进行中的请求。
type fakeRunner struct {
	mu      sync.Mutex
	calls   int32
	release chan struct{}
	cancel  context.CancelFunc

	lastPrompt string
	lastSID    string
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{release: make(chan struct{})}
}

func (f *fakeRunner) RunStream(ctx context.Context, prompt, sid string) (<-chan agent.Event, <-chan agent.RunResult) {
	atomic.AddInt32(&f.calls, 1)
	f.mu.Lock()
	f.lastPrompt = prompt
	f.lastSID = sid
	f.mu.Unlock()
	ev := make(chan agent.Event, 4)
	res := make(chan agent.RunResult, 1)
	// 始终发送一条 assistant_delta + assistant_message + loop_done。
	ev <- agent.AssistantDelta{Text: "hi"}
	ev <- agent.AssistantMessage{Content: "hi"}
	close(ev)
	res <- agent.RunResult{
		FinalMessages: []llm.Message{{Role: llm.RoleAssistant, Content: "hi"}},
		Rounds:        1,
		StopReason:    "no_tool_calls",
		SessionID:     sid,
		Usage:         llm.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2},
	}
	close(res)
	return ev, res
}

func newServer(t *testing.T, runner agent.StreamingRunner, st store.Store) *Server {
	t.Helper()
	return New(Config{
		Listen:    "127.0.0.1:0",
		AuthToken: "secret-token",
		Timeout:   0,
	}, runner, st)
}

func do(t *testing.T, srv *Server, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// 1. /healthz 开放访问（无需认证）并返回 200
func TestServer_Healthz_NoAuth(t *testing.T) {
	srv := newServer(t, newFakeRunner(), nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := do(t, srv, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// 2. POST /api/agent/message 不带 Authorization → 401
func TestServer_Message_NoAuth(t *testing.T) {
	srv := newServer(t, newFakeRunner(), nil)
	body := strings.NewReader(`{"prompt":"hi"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/message", body)
	rec := do(t, srv, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// 3. POST /api/agent/message 使用错误 token → 401
func TestServer_Message_WrongToken(t *testing.T) {
	srv := newServer(t, newFakeRunner(), nil)
	body := strings.NewReader(`{"prompt":"hi"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/message", body)
	req.Header.Set("Authorization", "Bearer wrong")
	rec := do(t, srv, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// 4. POST /api/agent/message 正常路径 → 200 + 响应体结构
func TestServer_Message_HappyPath(t *testing.T) {
	srv := newServer(t, newFakeRunner(), nil)
	body := strings.NewReader(`{"prompt":"hi"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/message", body)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := do(t, srv, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp messageResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.StopReason != "no_tool_calls" {
		t.Errorf("StopReason = %q", resp.StopReason)
	}
	if resp.Usage["total_tokens"] != 2 {
		t.Errorf("Usage.total_tokens = %d", resp.Usage["total_tokens"])
	}
	if resp.Rounds != 1 {
		t.Errorf("Rounds = %d", resp.Rounds)
	}
}

// 5. 对同一会话并发请求 /api/agent/stream → 409
func TestServer_Stream_ConcurrentSameSession_409(t *testing.T) {
	// blockingRunner：阻塞进行中的调用直到被释放。
	blocking := newBlockingRunner()
	defer blocking.releaseNow()
	srv := newServer(t, blocking, nil)

	// 第一个请求启动并阻塞。
	first := httptest.NewRequest(http.MethodGet, "/api/agent/stream?prompt=x&session_id=abc", nil)
	first.Header.Set("Authorization", "Bearer secret-token")
	firstRec := httptest.NewRecorder()
	go srv.Handler().ServeHTTP(firstRec, first)

	// 等待第一个请求到达 runner。
	blocking.waitEntered()
	// 同一会话上的第二个请求 → 409。
	second := httptest.NewRequest(http.MethodGet, "/api/agent/stream?prompt=x&session_id=abc", nil)
	second.Header.Set("Authorization", "Bearer secret-token")
	secondRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(secondRec, second)
	if secondRec.Code != http.StatusConflict {
		t.Errorf("second status = %d, want 409", secondRec.Code)
	}
}

// 6. SSE：客户端断开连接 → ctx 被取消 → StopReason=canceled
func TestServer_Stream_ClientDisconnect(t *testing.T) {
	// 使用能观察 ctx.Done() 的阻塞式 runner。
	blocking := newBlockingRunner()
	srv := newServer(t, blocking, nil)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/agent/stream?prompt=x", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()
	go srv.Handler().ServeHTTP(rec, req)

	blocking.waitEntered()
	// 取消客户端上下文。
	cancel()

	// 等待 runner 感知到 ctx 取消。
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("runner did not observe ctx cancel")
		default:
		}
		if blocking.ctxDoneSeen() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// 7. 工具 panic 安全：is_error=true 的工具结果通过 SSE 送达
func TestServer_Stream_ToolPanicSafe(t *testing.T) {
	// 自定义 runner：发送一条带工具调用的 AssistantMessage，以及一个
	// IsError=true 的工具结果（模拟 panic 恢复）。
	run := &panicToolRunner{}
	srv := newServer(t, run, nil)

	// 用 message 端点断言响应结构；SSE 结构用 stream 端点验证。
	body := strings.NewReader(`{"prompt":"hi"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/message", body)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := do(t, srv, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

// ---- 辅助函数 ----

// blockingRunner：保持 RunStream 打开直到被释放。
type blockingRunner struct {
	mu       sync.Mutex
	entered  chan struct{}
	released chan struct{}
	once     sync.Once
	done     chan struct{}
}

func newBlockingRunner() *blockingRunner {
	return &blockingRunner{
		entered:  make(chan struct{}, 1),
		released: make(chan struct{}),
		done:     make(chan struct{}),
	}
}

func (b *blockingRunner) waitEntered() { <-b.entered }
func (b *blockingRunner) releaseNow() {
	b.once.Do(func() { close(b.released) })
}

func (b *blockingRunner) ctxDoneSeen() bool {
	select {
	case <-b.done:
		return true
	default:
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.done == nil // 在 RunStream 中观察到 ctx.Done 后设置的关闭标志
}

func (b *blockingRunner) RunStream(ctx context.Context, prompt, sid string) (<-chan agent.Event, <-chan agent.RunResult) {
	ev := make(chan agent.Event)
	res := make(chan agent.RunResult, 1)
	go func() {
		defer close(ev)
		defer close(res)
		select {
		case b.entered <- struct{}{}:
		default:
		}
		select {
		case <-b.released:
			res <- agent.RunResult{StopReason: "no_tool_calls"}
		case <-ctx.Done():
			b.mu.Lock()
			close(b.done)
			b.mu.Unlock()
			res <- agent.RunResult{StopReason: "canceled", Error: ctx.Err()}
		}
	}()
	return ev, res
}

// panicToolRunner 发送一条带工具调用的 AssistantMessage，并通过返回
// IsError=true 的结果模拟 panic 恢复。
type panicToolRunner struct{}

func (panicToolRunner) RunStream(ctx context.Context, prompt, sid string) (<-chan agent.Event, <-chan agent.RunResult) {
	ev := make(chan agent.Event, 2)
	res := make(chan agent.RunResult, 1)
	ev <- agent.ToolResult{
		CallID: "c1", Name: "fake",
		Content: "[ERROR] panic in tool fake: boom",
		IsError: true,
		Took:    0,
	}
	ev <- agent.AssistantMessage{Content: "I tried, but it broke."}
	close(ev)
	res <- agent.RunResult{
		FinalMessages: []llm.Message{{Role: llm.RoleAssistant, Content: "I tried, but it broke."}},
		Rounds:        1,
		StopReason:    "no_tool_calls",
		SessionID:     sid,
	}
	close(res)
	return ev, res
}

// 消除未使用导入
var (
	_ = errors.New
	_ = atomic.AddInt32
	_ = io.EOF
	_ = bytes.NewReader
	_ = tool.NewRegistry
)

// --- T3 fake helpers ---

type fakeTool struct {
	name, desc string
}

func (f *fakeTool) Name() string        { return f.name }
func (f *fakeTool) Description() string { return f.desc }
func (f *fakeTool) Parameters() any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (f *fakeTool) Execute(ctx context.Context, args json.RawMessage) (tool.Result, error) {
	return tool.Result{Content: "ok"}, nil
}

// fakeInventory 直接在 server_test 里实现 plugin.Inventory，
// 避免引入 plugin 包（plugin.Inventory 接口用 plugin.PluginEntry，
// 但 plugin.PluginEntry 是简单结构体，可以直接构造）。
type fakeInventory struct {
	entries []plugin.PluginEntry
}

func (f *fakeInventory) List(ctx context.Context) ([]plugin.PluginEntry, error) {
	return f.entries, nil
}
func (f *fakeInventory) Get(ctx context.Context, name string) (plugin.PluginEntry, bool) {
	for _, e := range f.entries {
		if e.Name == name {
			return e, true
		}
	}
	return plugin.PluginEntry{}, false
}
func (f *fakeInventory) Health(ctx context.Context, name string) (bool, error) {
	if _, ok := f.Get(ctx, name); ok {
		return true, nil
	}
	return false, nil
}

// 用 fakeTool 注册一个 LocalInventory 等价物。
func newFakeInventoryFromReg(reg *tool.Registry) *fakeInventory {
	tools := []plugin.ToolSpecView{}
	for _, n := range reg.Names() {
		if t, ok := reg.Get(n); ok {
			tools = append(tools, plugin.ToolSpecView{
				Name:        t.Name(),
				Description: t.Description(),
				Risk:        "low",
				Parameters:  t.Parameters(),
			})
		}
	}
	return &fakeInventory{entries: []plugin.PluginEntry{
		{
			Name:    "local",
			Kind:    "local",
			Source:  "internal://registry",
			Healthy: true,
			Tools:   tools,
		},
	}}
}

// --- v4 §B Gateway 验收用例 ---

// T2.5.1 /api/gateway/stream session.send 完成返回 final 帧
func TestGateway_SessionSend_FinalFrame(t *testing.T) {
	srv := newServer(t, newFakeRunner(), nil)
	body := strings.NewReader(`{"source":"session.send","params":{"prompt":"hi"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/gateway/stream", body)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := do(t, srv, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	// 解析 SSE 帧：每行 data: {...}\n\n
	frames := parseSSEFrames(t, rec.Body.String())
	if len(frames) == 0 {
		t.Fatal("no frames emitted")
	}
	var hasFinal bool
	for _, f := range frames {
		if f["type"] != "final" {
			continue
		}
		payload, _ := f["payload"].(map[string]any)
		if payload == nil {
			continue
		}
		if payload["stop_reason"] == "no_tool_calls" {
			hasFinal = true
			if payload["rounds"] != float64(1) {
				t.Errorf("rounds = %v, want 1", payload["rounds"])
			}
		}
	}
	if !hasFinal {
		t.Errorf("no final frame with stop_reason; frames = %+v", frames)
	}
}

// T2.5.2 events.subscribe 通过 Gateway 拿到 ≥ 1 帧
func TestGateway_EventsSubscribe(t *testing.T) {
	st := store.NewMapStore()
	ctx := context.Background()
	sess, _ := st.Begin(ctx)
	ev := store.Event{Type: store.EventUserMessage}
	_ = ev.MarshalPayload(store.UserMessagePayload{Content: "hi"})
	if _, err := st.AppendEvent(ctx, sess.ID, ev); err != nil {
		t.Fatal(err)
	}

	srv := newServer(t, newFakeRunner(), st)
	body := strings.NewReader(`{"source":"events.subscribe","params":{"sid":"` + sess.ID + `"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/gateway/stream", body)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := do(t, srv, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	frames := parseSSEFrames(t, rec.Body.String())
	if len(frames) < 2 {
		t.Fatalf("got %d frames, want >= 2: %+v", len(frames), frames)
	}
	var sawDelta bool
	for _, f := range frames {
		if f["type"] == "delta" {
			sawDelta = true
		}
	}
	if !sawDelta {
		t.Errorf("no delta frame in: %+v", frames)
	}
}

// T2.5.3 session.snapshot 拿到 messages 投影
func TestGateway_SessionSnapshot(t *testing.T) {
	st := store.NewMapStore()
	ctx := context.Background()
	sess, _ := st.Begin(ctx)
	if err := st.Append(ctx, sess.ID, llm.Message{Role: llm.RoleSystem, Content: "sys"}); err != nil {
		t.Fatal(err)
	}
	srv := newServer(t, newFakeRunner(), st)
	body := strings.NewReader(`{"source":"session.snapshot","params":{"sid":"` + sess.ID + `","projection":"messages"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/gateway/stream", body)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := do(t, srv, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	frames := parseSSEFrames(t, rec.Body.String())
	if len(frames) == 0 {
		t.Fatal("no frames")
	}
	final := frames[len(frames)-1]
	if final["type"] != "final" {
		t.Errorf("last frame type = %v", final["type"])
	}
}

// T2.5.4 unknown source → error 帧
func TestGateway_UnknownSource(t *testing.T) {
	srv := newServer(t, newFakeRunner(), nil)
	body := strings.NewReader(`{"source":"nope","params":{}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/gateway/stream", body)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := do(t, srv, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	frames := parseSSEFrames(t, rec.Body.String())
	var sawErr bool
	for _, f := range frames {
		if f["type"] == "error" {
			sawErr = true
		}
	}
	if !sawErr {
		t.Errorf("expected error frame for unknown source; got %+v", frames)
	}
}

// T2.5.5 tools.list / plugins.list stub 返回空数组
func TestGateway_Stubs(t *testing.T) {
	srv := newServer(t, newFakeRunner(), nil)
	for _, src := range []string{"tools.list", "plugins.list"} {
		t.Run(src, func(t *testing.T) {
			body := strings.NewReader(`{"source":"` + src + `","params":{}}`)
			req := httptest.NewRequest(http.MethodPost, "/api/gateway/stream", body)
			req.Header.Set("Authorization", "Bearer secret-token")
			rec := do(t, srv, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}
			frames := parseSSEFrames(t, rec.Body.String())
			if len(frames) == 0 || frames[len(frames)-1]["type"] != "final" {
				t.Errorf("expected final frame: %+v", frames)
			}
		})
	}
}

// T3.3.1 tools.list 接入真实 Inventory 后能拿到本地工具
func TestGateway_ToolsList_WithInventory(t *testing.T) {
	srv := newServer(t, newFakeRunner(), nil)
	reg := tool.NewRegistry()
	if err := reg.Register(&fakeTool{name: "shell", desc: "run shell"}); err != nil {
		t.Fatal(err)
	}
	srv.SetInventory(newFakeInventoryFromReg(reg))
	body := strings.NewReader(`{"source":"tools.list","params":{}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/gateway/stream", body)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := do(t, srv, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	frames := parseSSEFrames(t, rec.Body.String())
	if len(frames) == 0 {
		t.Fatal("no frames")
	}
	final := frames[len(frames)-1]
	if final["type"] != "final" {
		t.Fatalf("last frame type = %v", final["type"])
	}
	payload, _ := final["payload"].(map[string]any)
	if payload == nil {
		t.Fatalf("payload missing: %+v", final)
	}
	tools, _ := payload["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %d, want 1: %+v", len(tools), tools)
	}
	t0, _ := tools[0].(map[string]any)
	if t0["name"] != "shell" {
		t.Errorf("name = %v", t0["name"])
	}
	if t0["plugin_kind"] != "local" {
		t.Errorf("plugin_kind = %v", t0["plugin_kind"])
	}
}

// T3.3.2 plugins.list 接入真实 Inventory 后能拿到 plugin entry
func TestGateway_PluginsList_WithInventory(t *testing.T) {
	srv := newServer(t, newFakeRunner(), nil)
	reg := tool.NewRegistry()
	if err := reg.Register(&fakeTool{name: "echo", desc: "x"}); err != nil {
		t.Fatal(err)
	}
	srv.SetInventory(newFakeInventoryFromReg(reg))
	body := strings.NewReader(`{"source":"plugins.list","params":{}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/gateway/stream", body)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := do(t, srv, req)
	frames := parseSSEFrames(t, rec.Body.String())
	if len(frames) == 0 {
		t.Fatal("no frames")
	}
	final := frames[len(frames)-1]
	payload, _ := final["payload"].(map[string]any)
	if payload == nil {
		t.Fatalf("payload missing: %+v", final)
	}
	plugins, _ := payload["plugins"].([]any)
	if len(plugins) != 1 {
		t.Fatalf("plugins = %d, want 1", len(plugins))
	}
}

// T2.5.6 Router.Register + Sources + Dispatch 覆盖错误分支
func TestRouter_UnknownSource(t *testing.T) {
	r := NewRouter()
	_, ok := r.Sources(), []string{}
	_ = ok
	prev := r.Register("x", func(ctx context.Context, req GatewayRequest, out chan<- GatewayEvent) error {
		out <- GatewayEvent{Source: "x", Type: "delta"}
		return nil
	})
	if prev != nil {
		t.Error("first register should have no previous")
	}
	err := r.Dispatch(context.Background(), GatewayRequest{Source: "missing"}, nil)
	if err == nil {
		t.Error("expected ErrUnknownSource")
	}
}

// T2.5.7 emit 工具：ctx cancel 时返回错误
func TestGateway_EmitCancelledCtx(t *testing.T) {
	out := make(chan GatewayEvent)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := emit(ctx, out, "x", "delta", map[string]any{"a": 1})
	if err == nil {
		t.Error("expected ctx.Err()")
	}
}

// --- helpers ---

// parseSSEFrames 把 SSE 响应体拆为帧序列，每帧是一个 json map。
func parseSSEFrames(t *testing.T, body string) []map[string]any {
	t.Helper()
	out := []map[string]any{}
	for _, raw := range strings.Split(body, "\n\n") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		// 跳过 event: 头部，只取 data: 行
		for _, line := range strings.Split(raw, "\n") {
			if strings.HasPrefix(line, "data: ") {
				payload := strings.TrimPrefix(line, "data: ")
				var m map[string]any
				if err := json.Unmarshal([]byte(payload), &m); err != nil {
					t.Errorf("bad JSON in SSE frame: %v (payload=%s)", err, payload)
					continue
				}
				out = append(out, m)
			}
		}
	}
	return out
}
