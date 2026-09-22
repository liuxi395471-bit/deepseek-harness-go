// Package e2e 针对内存 mock LLM 测试完整的 v2 技术栈：store + agent
// 流式 + REPL 斜杠命令。它作为 v2 里程碑的权威冒烟测试（DESIGN-v2 §A.8）。
package e2e

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"deepseek-harness-go/internal/agent"
	"deepseek-harness-go/internal/llm"
	"deepseek-harness-go/internal/server"
	"deepseek-harness-go/internal/store"
	"deepseek-harness-go/internal/tool"

	"deepseek-harness-go/cmd/dsh/repl"
)

// mockLLM 是一个可控的 LLM 客户端，在 Chat 和 ChatStream 两种模式下
// 都会先发出一轮工具调用，再给出最终回答。
type mockLLM struct {
	mu          sync.Mutex
	streamCalls int32
	chatCalls   int32
}

func (m *mockLLM) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	atomic.AddInt32(&m.chatCalls, 1)
	m.mu.Lock()
	defer m.mu.Unlock()
	// 如果对话中已存在带 tool_calls 的 assistant 轮次，返回最终回答；
	// 否则返回一次工具调用。
	for _, msg := range req.Messages {
		if msg.Role == llm.RoleTool {
			return llm.ChatResponse{
				Choices: []llm.Choice{{
					Message: llm.Message{
						Role:    llm.RoleAssistant,
						Content: "done with tool",
					},
					FinishReason: "stop",
				}},
				Usage: &llm.Usage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
			}, nil
		}
	}
	return llm.ChatResponse{
		Choices: []llm.Choice{{
			Message: llm.Message{
				ToolCalls: []llm.ToolCall{
					{ID: "c1", Type: "function", Function: llm.ToolCallFunc{
						Name: "echo", Arguments: `{}`,
					}},
				},
			},
			FinishReason: "tool_calls",
		}},
	}, nil
}

func (m *mockLLM) ChatStream(_ context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, <-chan error) {
	atomic.AddInt32(&m.streamCalls, 1)
	ch := make(chan llm.StreamChunk, 4)
	errCh := make(chan error, 1)
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, msg := range req.Messages {
		if msg.Role == llm.RoleTool {
			// 第 2 轮：最终文本
			ch <- llm.StreamChunk{Index: 0, Text: "done with tool"}
			ch <- llm.StreamChunk{Index: 0, Finish: "stop"}
			close(ch)
			close(errCh)
			return ch, errCh
		}
	}
	// 第 1 轮：tool_calls
	ch <- llm.StreamChunk{Index: 0, ToolCalls: []llm.ToolCall{
		{ID: "c1", Type: "function", Function: llm.ToolCallFunc{Name: "echo", Arguments: `{}`}},
	}}
	ch <- llm.StreamChunk{Index: 0, Finish: "tool_calls"}
	close(ch)
	close(errCh)
	return ch, errCh
}

// echoTool 返回固定字符串。
type echoTool struct{}

func (echoTool) Name() string        { return "echo" }
func (echoTool) Description() string { return "echoes" }
func (echoTool) Parameters() any     { return map[string]any{"type": "object"} }
func (echoTool) Execute(_ context.Context, _ json.RawMessage) (tool.Result, error) {
	return tool.Ok("echoed"), nil
}

// 1. REPL 流式端到端：stdin 输入 1 条 prompt；我们观察到包含
// "done with tool" 的流式 AssistantMessage。
func TestE2E_REPLStreaming(t *testing.T) {
	registry := tool.NewRegistry()
	if err := registry.Register(echoTool{}); err != nil {
		t.Fatal(err)
	}
	mock := &mockLLM{}
	runner := agent.NewLoopRunner(mock, registry, agent.DefaultSystemPrompt{Override: "test"}, "m", 4, 128, nil)
	runner.Stream = true

	// 捕获 stderr 以屏蔽 REPL 横幅。
	origStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	in := strings.NewReader("hello\n/exit\n")
	origStdin := os.Stdin
	rIn, wIn, _ := os.Pipe()
	_, _ = io.Copy(wIn, in)
	_ = wIn.Close()
	os.Stdin = rIn

	go func() {
		_, _ = io.Copy(io.Discard, r)
	}()
	repl.Run(context.Background(), runner, false)

	os.Stderr = origStderr
	os.Stdin = origStdin
	_ = w.Close()

	if atomic.LoadInt32(&mock.streamCalls) == 0 {
		t.Errorf("ChatStream never called: stream=true but no stream")
	}
}

// 2. HTTP 端到端：server → runner → store。共两轮；session.Messages
// 长度 == 5（system+user+assistant(tool)+tool+assistant(text)）。
func TestE2E_HTTPServer(t *testing.T) {
	registry := tool.NewRegistry()
	if err := registry.Register(echoTool{}); err != nil {
		t.Fatal(err)
	}
	mock := &mockLLM{}
	runner := agent.NewLoopRunner(mock, registry, agent.DefaultSystemPrompt{Override: "test"}, "m", 4, 128, nil)
	storePath := filepath.Join(t.TempDir(), "sessions.db")
	st, err := store.NewSQLiteStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	runner.Store = st

	srv := server.New(server.Config{
		Listen:    "127.0.0.1:0",
		AuthToken: "e2e-token",
	}, runner, st)

	// POST /api/agent/message
	body := strings.NewReader(`{"prompt":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/message", body)
	req.Header.Set("Authorization", "Bearer e2e-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		SessionID  string `json:"session_id"`
		StopReason string `json:"stop_reason"`
		Rounds     int    `json:"rounds"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != "no_tool_calls" {
		t.Errorf("StopReason = %q", resp.StopReason)
	}
	if resp.SessionID == "" {
		t.Errorf("SessionID empty")
	}
	if resp.Rounds != 2 {
		t.Errorf("Rounds = %d, want 2", resp.Rounds)
	}

	// GET /api/sessions/{id} → messages == 5
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/sessions/"+resp.SessionID, nil)
	req2.Header.Set("Authorization", "Bearer e2e-token")
	srv.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("session fetch status = %d", rec2.Code)
	}
	var sess store.Session
	if err := json.Unmarshal(rec2.Body.Bytes(), &sess); err != nil {
		t.Fatal(err)
	}
	if len(sess.Messages) != 5 {
		t.Errorf("Messages len = %d, want 5", len(sess.Messages))
	}

	// 无 token 时返回 401
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost, "/api/agent/message", strings.NewReader(`{"prompt":"x"}`))
	srv.Handler().ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusUnauthorized {
		t.Errorf("no-auth status = %d, want 401", rec3.Code)
	}
}

// 3. SSE 流端到端：GET /api/agent/stream 至少发出一帧 content 为
// "done with tool" 的 assistant_message。
func TestE2E_HTTPSSEStream(t *testing.T) {
	registry := tool.NewRegistry()
	if err := registry.Register(echoTool{}); err != nil {
		t.Fatal(err)
	}
	mock := &mockLLM{}
	runner := agent.NewLoopRunner(mock, registry, agent.DefaultSystemPrompt{Override: "test"}, "m", 4, 128, nil)
	st, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := server.New(server.Config{
		Listen:    "127.0.0.1:0",
		AuthToken: "e2e-token",
	}, runner, st)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/agent/stream?prompt=hello", nil)
	req.Header.Set("Authorization", "Bearer e2e-token")
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "assistant_message") {
		t.Errorf("SSE body missing assistant_message: %s", body)
	}
	if !strings.Contains(body, "done with tool") {
		t.Errorf("SSE body missing 'done with tool': %s", body)
	}
	if !strings.Contains(body, "loop_done") {
		t.Errorf("SSE body missing loop_done: %s", body)
	}
}
