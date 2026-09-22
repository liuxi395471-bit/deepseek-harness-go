package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"deepseek-harness-go/internal/llm"
	"deepseek-harness-go/internal/tool"
)

type mockClient struct {
	mu      sync.Mutex
	calls   int32
	queue   []llm.ChatResponse
	err     error
	gotReqs []llm.ChatRequest
	// 当队列为空时，由 refill（若设置）返回新的响应。
	// 给 MaxRounds 测试用，使循环能无限跑下去。
	refill func() llm.ChatResponse
}

// ChatStream 从队列中取出下一个 ChatResponse，并把它转换为
// 一连串 StreamChunk（文本 delta、tool-call delta、再加上 finish
// chunk）。贴近 OpenAI/DeepSeek 服务端在线上真正发出的样子。
// 返回前 ch 与 errCh 都会被且只会被关闭一次。
func (m *mockClient) ChatStream(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamChunk, <-chan error) {
	ch := make(chan llm.StreamChunk, 4)
	errCh := make(chan error, 1)
	m.mu.Lock()
	defer m.mu.Unlock()
	var resp llm.ChatResponse
	var emitErr error
	if m.err != nil {
		emitErr = m.err
	} else if len(m.queue) == 0 {
		if m.refill != nil {
			resp = m.refill()
		} else {
			emitErr = errors.New("mockClient: queue empty")
		}
	} else {
		resp = m.queue[0]
		m.queue = m.queue[1:]
	}
	if emitErr != nil {
		errCh <- emitErr
	} else {
		emitMockStream(ch, resp)
	}
	close(ch)
	close(errCh)
	return ch, errCh
}

// emitMockStream translates one ChatResponse into StreamChunks.
func emitMockStream(ch chan<- llm.StreamChunk, resp llm.ChatResponse) {
	if len(resp.Choices) == 0 {
		return
	}
	msg := resp.Choices[0].Message
	if msg.Content != "" {
		ch <- llm.StreamChunk{Index: 0, Text: msg.Content}
	}
	for _, tc := range msg.ToolCalls {
		ch <- llm.StreamChunk{Index: 0, ToolCalls: []llm.ToolCall{tc}}
	}
	finish := ""
	if len(resp.Choices) > 0 {
		finish = resp.Choices[0].FinishReason
	}
	// Usage 放在终止帧上，便于循环排空后由运行器拾取。
	usage := resp.Usage
	ch <- llm.StreamChunk{Index: 0, Finish: finish, Usage: usage}
}

func (m *mockClient) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	atomic.AddInt32(&m.calls, 1)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gotReqs = append(m.gotReqs, req)
	if m.err != nil {
		return llm.ChatResponse{}, m.err
	}
	if len(m.queue) == 0 {
		if m.refill != nil {
			return m.refill(), nil
		}
		return llm.ChatResponse{}, errors.New("mockClient: queue empty")
	}
	out := m.queue[0]
	m.queue = m.queue[1:]
	return out, nil
}

func (m *mockClient) Calls() int { return int(atomic.LoadInt32(&m.calls)) }

func newTestRunner(mc *mockClient, reg *tool.Registry) *LoopRunner {
	sys := DefaultSystemPrompt{Override: "test system"}
	return NewLoopRunner(mc, reg, sys, "test-model", 8, 256, nil)
}

func collectRun(t *testing.T, r Runner, ctx context.Context, prompt string) ([]Event, RunResult) {
	t.Helper()
	events, resultCh := r.Run(ctx, prompt)
	var evs []Event
	for e := range events {
		evs = append(evs, e)
	}
	res := <-resultCh
	return evs, res
}

func TestRun_NoToolCalls_StopsAfterFirstAssistant(t *testing.T) {
	mc := &mockClient{queue: []llm.ChatResponse{
		{Choices: []llm.Choice{{Index: 0, Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}}},
	}}
	r := newTestRunner(mc, tool.NewRegistry())
	evs, res := collectRun(t, r, context.Background(), "hi")

	if res.StopReason != "no_tool_calls" {
		t.Errorf("StopReason = %q", res.StopReason)
	}
	if res.Rounds != 1 {
		t.Errorf("Rounds = %d", res.Rounds)
	}
	if res.Error != nil {
		t.Errorf("Error = %v", res.Error)
	}

	var gotAssistant *AssistantMessage
	for _, e := range evs {
		if am, ok := e.(AssistantMessage); ok {
			gotAssistant = &am
		}
	}
	if gotAssistant == nil || gotAssistant.Content != "done" {
		t.Errorf("no AssistantMessage with content=done: %+v", gotAssistant)
	}
	if mc.Calls() != 1 {
		t.Errorf("LLM calls = %d, want 1", mc.Calls())
	}
}

func TestRun_ToolCallThenFinalAnswer(t *testing.T) {
	mc := &mockClient{queue: []llm.ChatResponse{
		{Choices: []llm.Choice{{Index: 0, Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID: "c1", Type: "function",
				Function: llm.ToolCallFunc{Name: "greet", Arguments: `{"name":"Ada"}`},
			}},
		}}}},
		{Choices: []llm.Choice{{Index: 0, Message: llm.Message{Role: llm.RoleAssistant, Content: "向 Ada 问好了"}}}},
	}}

	reg := tool.NewRegistry()
	reg.MustRegister(&stubGreeter{})

	r := newTestRunner(mc, reg)
	evs, res := collectRun(t, r, context.Background(), "hi")

	if res.StopReason != "no_tool_calls" {
		t.Errorf("StopReason = %q, want no_tool_calls", res.StopReason)
	}
	if res.Rounds != 2 {
		t.Errorf("Rounds = %d, want 2", res.Rounds)
	}

	var sawCall, sawResult bool
	for _, e := range evs {
		switch v := e.(type) {
		case ToolCallStart:
			sawCall = true
			if v.Call.Function.Name != "greet" {
				t.Errorf("ToolCall name = %q", v.Call.Function.Name)
			}
		case ToolResult:
			sawResult = true
			if v.Name != "greet" {
				t.Errorf("ToolResult name = %q", v.Name)
			}
			if v.IsError {
				t.Errorf("IsError = true")
			}
			if !strings.Contains(v.Content, "Ada") {
				t.Errorf("Content = %q", v.Content)
			}
		}
	}
	if !sawCall || !sawResult {
		t.Errorf("missing tool events: call=%v result=%v", sawCall, sawResult)
	}

	last := res.FinalMessages[len(res.FinalMessages)-1]
	if last.Role != llm.RoleAssistant || last.Content != "向 Ada 问好了" {
		t.Errorf("last message = %+v", last)
	}
	hasTool := false
	for _, m := range res.FinalMessages {
		if m.Role == llm.RoleTool && m.ToolCallID == "c1" {
			hasTool = true
		}
	}
	if !hasTool {
		t.Errorf("no role=tool message with ToolCallID=c1")
	}
}

func TestRun_UnknownToolBackfillsError(t *testing.T) {
	mc := &mockClient{queue: []llm.ChatResponse{
		{Choices: []llm.Choice{{Index: 0, Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID: "c1", Type: "function",
				Function: llm.ToolCallFunc{Name: "does_not_exist", Arguments: `{}`},
			}},
		}}}},
		{Choices: []llm.Choice{{Index: 0, Message: llm.Message{Role: llm.RoleAssistant, Content: "ok"}}}},
	}}

	r := newTestRunner(mc, tool.NewRegistry())
	evs, res := collectRun(t, r, context.Background(), "hi")

	if res.StopReason != "no_tool_calls" {
		t.Errorf("StopReason = %q", res.StopReason)
	}

	var got ToolResult
	for _, e := range evs {
		if tr, ok := e.(ToolResult); ok && tr.Name == "does_not_exist" {
			got = tr
		}
	}
	if !got.IsError {
		t.Errorf("expected IsError=true")
	}
	if !strings.HasPrefix(got.Content, "[ERROR] unknown tool") {
		t.Errorf("Content = %q", got.Content)
	}
}

func TestRun_ToolFailureBackfillsError(t *testing.T) {
	mc := &mockClient{queue: []llm.ChatResponse{
		{Choices: []llm.Choice{{Index: 0, Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID: "c1", Type: "function",
				Function: llm.ToolCallFunc{Name: "boom", Arguments: `{}`},
			}},
		}}}},
		{Choices: []llm.Choice{{Index: 0, Message: llm.Message{Role: llm.RoleAssistant, Content: "ok"}}}},
	}}

	reg := tool.NewRegistry()
	reg.MustRegister(&failingTool{})

	r := newTestRunner(mc, reg)
	evs, res := collectRun(t, r, context.Background(), "hi")

	if res.StopReason != "no_tool_calls" {
		t.Errorf("StopReason = %q", res.StopReason)
	}
	var got ToolResult
	for _, e := range evs {
		if tr, ok := e.(ToolResult); ok && tr.Name == "boom" {
			got = tr
		}
	}
	if !got.IsError {
		t.Errorf("expected IsError=true")
	}
	if !strings.HasPrefix(got.Content, "[ERROR]") {
		t.Errorf("Content = %q", got.Content)
	}
}

func TestRun_MaxRounds(t *testing.T) {
	// 始终回复工具调用，但不能让队列跑空。
	loop := llm.ChatResponse{
		Choices: []llm.Choice{{Index: 0, Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID: "c1", Type: "function",
				Function: llm.ToolCallFunc{Name: "g", Arguments: `{}`},
			}},
		}}},
	}
	mc := &mockClient{queue: []llm.ChatResponse{loop}}
	// 包一层 Chat，在队列清空时自动补单（让循环能持续跑）。
	mc.refill = func() llm.ChatResponse { return loop }
	reg := tool.NewRegistry()
	reg.MustRegister(&stubGreeter{})

	r := NewLoopRunner(mc, reg, DefaultSystemPrompt{}, "m", 3, 128, nil)
	_, res := collectRun(t, r, context.Background(), "hi")

	if res.StopReason != "max_rounds" {
		t.Errorf("StopReason = %q, want max_rounds", res.StopReason)
	}
	if res.Rounds != 3 {
		t.Errorf("Rounds = %d, want 3", res.Rounds)
	}
}

func TestRun_ContextCanceled(t *testing.T) {
	block := make(chan struct{})
	mc := &blockingClient{block: block}
	reg := tool.NewRegistry()
	r := NewLoopRunner(mc, reg, DefaultSystemPrompt{}, "m", 8, 128, nil)

	ctx, cancel := context.WithCancel(context.Background())
	evCh, resCh := r.Run(ctx, "hi")
	time.AfterFunc(20*time.Millisecond, cancel)
	for range evCh {
	}
	res := <-resCh

	if res.StopReason != "canceled" {
		t.Errorf("StopReason = %q, want canceled", res.StopReason)
	}
	close(block)
}

func TestRun_LLMError(t *testing.T) {
	mc := &mockClient{err: errors.New("boom")}
	r := newTestRunner(mc, tool.NewRegistry())
	_, res := collectRun(t, r, context.Background(), "hi")
	if res.StopReason != "error" {
		t.Errorf("StopReason = %q", res.StopReason)
	}
	if res.Error == nil || res.Error.Error() != "boom" {
		t.Errorf("Error = %v", res.Error)
	}
}

func TestRun_LLMErrorsCanceledAsCanceled(t *testing.T) {
	mc := &mockClient{err: context.Canceled}
	r := newTestRunner(mc, tool.NewRegistry())
	_, res := collectRun(t, r, context.Background(), "hi")
	if res.StopReason != "canceled" {
		t.Errorf("StopReason = %q, want canceled", res.StopReason)
	}
}

func TestRun_EmptyChoices(t *testing.T) {
	mc := &mockClient{queue: []llm.ChatResponse{{Choices: nil}}}
	r := newTestRunner(mc, tool.NewRegistry())
	_, res := collectRun(t, r, context.Background(), "hi")
	if res.StopReason != "error" {
		t.Errorf("StopReason = %q", res.StopReason)
	}
	if res.Error == nil || !strings.Contains(res.Error.Error(), "empty choices") {
		t.Errorf("Error = %v", res.Error)
	}
}

func TestRun_ToolPanicIsRecovered(t *testing.T) {
	mc := &mockClient{queue: []llm.ChatResponse{
		{Choices: []llm.Choice{{Index: 0, Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID: "c1", Type: "function",
				Function: llm.ToolCallFunc{Name: "panicker", Arguments: `{}`},
			}},
		}}}},
		{Choices: []llm.Choice{{Index: 0, Message: llm.Message{Role: llm.RoleAssistant, Content: "ok"}}}},
	}}

	reg := tool.NewRegistry()
	reg.MustRegister(&panickingTool{})

	r := newTestRunner(mc, reg)
	_, res := collectRun(t, r, context.Background(), "hi")
	if res.StopReason != "no_tool_calls" {
		t.Errorf("StopReason = %q, want no_tool_calls (panic should not crash loop)", res.StopReason)
	}
}

// TestRun_RunnerPanicStilDeliversResult 覆盖内层 defer 守卫：
// 如果循环内部发生 panic（用一个在首次调用时 panic 的客户端模拟），
// 运行器仍然必须送出 RunResult，避免 REPL 在 <-resultCh 上死锁。
func TestRun_RunnerPanicStillDeliversResult(t *testing.T) {
	r := NewLoopRunner(&panickingClient{}, tool.NewRegistry(), DefaultSystemPrompt{Override: "test"}, "m", 8, 256, nil)

	// 用一个有界的 context，避免关闭路径有 bug 时测试永远卡住。
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	var res RunResult
	go func() {
		evCh, resCh := r.Run(ctx, "hi")
		for range evCh {
		}
		res = <-resCh
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("runner did not deliver RunResult after panic (REPL would deadlock)")
	}
	if res.StopReason != "error" {
		t.Errorf("StopReason = %q, want error", res.StopReason)
	}
	if res.Error == nil {
		t.Errorf("Error = nil, want panic-derived error")
	}
}

// TestRun_MaxRoundsEmitsPhaseStopped 覆盖 R1：当 max_rounds 触发时，
// 循环必须发出 PhaseChange{PhaseStopped}，让调试消费者能看出
// 是正常退出（max_rounds）还是 cancel/error。
func TestRun_MaxRoundsEmitsPhaseStopped(t *testing.T) {
	loop := llm.ChatResponse{
		Choices: []llm.Choice{{Index: 0, Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
			ID: "c1", Type: "function",
			Function: llm.ToolCallFunc{Name: "g", Arguments: `{}`},
		}}}},
		},
	}
	mc := &mockClient{queue: []llm.ChatResponse{loop}, refill: func() llm.ChatResponse { return loop }}
	reg := tool.NewRegistry()
	reg.MustRegister(&stubGreeter{})

	r := NewLoopRunner(mc, reg, DefaultSystemPrompt{}, "m", 2, 128, nil)
	evs, res := collectRun(t, r, context.Background(), "hi")

	if res.StopReason != "max_rounds" {
		t.Errorf("StopReason = %q", res.StopReason)
	}
	var sawStopped bool
	for _, e := range evs {
		if pc, ok := e.(PhaseChange); ok && pc.Phase == PhaseStopped {
			sawStopped = true
		}
	}
	if !sawStopped {
		t.Errorf("no PhaseChange{PhaseStopped} on max_rounds exit (debug observability broken)")
	}
}

// panickingClient 在第一次 Chat 调用时 panic。用于验证
// 运行器的 defer 守卫。
type panickingClient struct{}

func (panickingClient) ChatStream(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamChunk, <-chan error) {
	ch := make(chan llm.StreamChunk)
	errCh := make(chan error, 1)
	errCh <- errors.New("ChatStream not used in panicking tests")
	close(ch)
	close(errCh)
	return ch, errCh
}

func (panickingClient) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	panic("runner panic test")
}

func TestRun_PassesToolSpecsToLLM(t *testing.T) {
	mc := &mockClient{queue: []llm.ChatResponse{
		{Choices: []llm.Choice{{Index: 0, Message: llm.Message{Role: llm.RoleAssistant, Content: "ok"}}}},
	}}
	reg := tool.NewRegistry()
	reg.MustRegister(&miniTool{name: "alpha", desc: "first"})

	r := newTestRunner(mc, reg)
	collectRun(t, r, context.Background(), "hi")

	if len(mc.gotReqs) != 1 {
		t.Fatalf("LLM calls = %d", len(mc.gotReqs))
	}
	specs := mc.gotReqs[0].Tools
	if len(specs) != 1 || specs[0].Function.Name != "alpha" {
		t.Errorf("Tools = %+v", specs)
	}
	if mc.gotReqs[0].Messages[0].Role != llm.RoleSystem {
		t.Errorf("first message not system: %+v", mc.gotReqs[0].Messages[0])
	}
}

type stubGreeter struct{}

func (*stubGreeter) Name() string        { return "greet" }
func (*stubGreeter) Description() string { return "stub" }
func (*stubGreeter) Parameters() any     { return nil }
func (*stubGreeter) Execute(_ context.Context, args json.RawMessage) (tool.Result, error) {
	var p struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(args, &p)
	if p.Name == "" {
		return tool.Ok("hi"), nil
	}
	return tool.Ok("你好，" + p.Name + "！"), nil
}

type failingTool struct{}

func (*failingTool) Name() string        { return "boom" }
func (*failingTool) Description() string { return "always fails" }
func (*failingTool) Parameters() any     { return nil }
func (*failingTool) Execute(_ context.Context, _ json.RawMessage) (tool.Result, error) {
	return tool.Err("intentional failure"), nil
}

type panickingTool struct{}

func (*panickingTool) Name() string        { return "panicker" }
func (*panickingTool) Description() string { return "panics" }
func (*panickingTool) Parameters() any     { return nil }
func (*panickingTool) Execute(_ context.Context, _ json.RawMessage) (tool.Result, error) {
	panic("nope")
}

type blockingClient struct{ block chan struct{} }

func (b *blockingClient) ChatStream(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamChunk, <-chan error) {
	ch := make(chan llm.StreamChunk)
	errCh := make(chan error, 1)
	errCh <- errors.New("ChatStream not used in blocking tests")
	close(ch)
	close(errCh)
	return ch, errCh
}

func (b *blockingClient) Chat(ctx context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	select {
	case <-b.block:
		return llm.ChatResponse{}, errors.New("client unblocked")
	case <-ctx.Done():
		return llm.ChatResponse{}, ctx.Err()
	}
}
