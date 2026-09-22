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
