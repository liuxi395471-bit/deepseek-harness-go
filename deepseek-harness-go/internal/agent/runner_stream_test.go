package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"deepseek-harness-go/internal/llm"
	"deepseek-harness-go/internal/tool"
)

// stubTool 是用于 round-trip 测试的最小工具。
type stubTool struct{}

func (stubTool) Name() string        { return "echo" }
func (stubTool) Description() string { return "echo" }
func (stubTool) Parameters() any     { return struct{ X int }{X: 1} }
func (stubTool) Execute(_ context.Context, _ json.RawMessage) (tool.Result, error) {
	return tool.Ok("ok"), nil
}

// RunStream 快乐路径：1 个 ChatResponse → ChatStream 发 1 个文本 chunk + finish chunk。
// 期望 1 个 AssistantDelta("Hello") 和 Content="Hello" 的 AssistantMessage。
func TestRunStream_EmitsAssistantDeltaPerChunk(t *testing.T) {
	mc := &mockClient{
		queue: []llm.ChatResponse{
			{Choices: []llm.Choice{{Message: llm.Message{Content: "Hello"}, FinishReason: "stop"}}},
		},
	}
	r := NewLoopRunner(mc, tool.NewRegistry(), DefaultSystemPrompt{Override: "x"}, "m", 4, 64, nil)

	events, result := r.RunStream(context.Background(), "hi", "")
	var deltas []string
	var final AssistantMessage
	for ev := range events {
		switch v := ev.(type) {
		case AssistantDelta:
			deltas = append(deltas, v.Text)
		case AssistantMessage:
			final = v
		}
	}
	res := <-result
	if res.StopReason != "no_tool_calls" {
		t.Errorf("StopReason = %q, want no_tool_calls", res.StopReason)
	}
	if len(deltas) != 1 || deltas[0] != "Hello" {
		t.Errorf("deltas = %#v, want [Hello]", deltas)
	}
	if final.Content != "Hello" {
		t.Errorf("final.Content = %q, want Hello", final.Content)
	}
}

// RunStream：工具调用轮次后接最终文本轮次。
// 第 1 轮：tool_calls → 0 个 delta，AssistantMessage 含 1 个 tool call。
// 第 2 轮：text → 1 个 delta，AssistantMessage 的 Content="done"。
func TestRunStream_ToolCallRound(t *testing.T) {
	mc := &mockClient{
		queue: []llm.ChatResponse{
			{Choices: []llm.Choice{{
				Message: llm.Message{
					ToolCalls: []llm.ToolCall{
						{ID: "c1", Type: "function", Function: llm.ToolCallFunc{Name: "echo", Arguments: `{"x":1}`}},
					},
				},
				FinishReason: "tool_calls",
			}}},
			{Choices: []llm.Choice{{Message: llm.Message{Content: "done"}, FinishReason: "stop"}}},
		},
	}
	reg := tool.NewRegistry()
	_ = reg.Register(stubTool{})
	r := NewLoopRunner(mc, reg, DefaultSystemPrompt{Override: "x"}, "m", 4, 64, nil)

	events, result := r.RunStream(context.Background(), "hi", "")
	var deltas []string
	var msgs []AssistantMessage
	for ev := range events {
		switch v := ev.(type) {
		case AssistantDelta:
			deltas = append(deltas, v.Text)
		case AssistantMessage:
			msgs = append(msgs, v)
		}
	}
	res := <-result
	if res.StopReason != "no_tool_calls" {
		t.Errorf("StopReason = %q, want no_tool_calls", res.StopReason)
	}
	if len(deltas) != 1 || deltas[0] != "done" {
		t.Errorf("deltas = %#v, want [done]", deltas)
	}
	if len(msgs) != 2 {
		t.Fatalf("msgs = %d, want 2", len(msgs))
	}
	if msgs[0].ToolCalls[0].Function.Name != "echo" {
		t.Errorf("tool name = %q", msgs[0].ToolCalls[0].Function.Name)
	}
	if msgs[1].Content != "done" {
		t.Errorf("round2 content = %q", msgs[1].Content)
	}
}

// slowMockClient 同步发出一个 chunk，然后在某个 channel 关闭前一直阻塞
// （或 ctx 被取消）。用于确定性地演练中途取消路径。
type slowMockClient struct {
	mu       sync.Mutex
	hold     chan struct{}
	emitted  chan struct{}
	once     sync.Once
}

func (s *slowMockClient) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, errors.New("Chat not used")
}
func (s *slowMockClient) ChatStream(ctx context.Context, _ llm.ChatRequest) (<-chan llm.StreamChunk, <-chan error) {
	ch := make(chan llm.StreamChunk, 2)
	errCh := make(chan error, 1)
	go func() {
		defer close(ch)
		defer close(errCh)
		ch <- llm.StreamChunk{Index: 0, Text: "a"}
		s.once.Do(func() { close(s.emitted) })
		select {
		case <-s.hold:
		case <-ctx.Done():
			errCh <- ctx.Err()
		}
	}()
	return ch, errCh
}
func (s *slowMockClient) Calls() int { return 0 }

// RunStream：收到一个 chunk 后取消，必须呈现 StopReason=canceled。
func TestRunStream_CancelMidRound(t *testing.T) {
	smc := &slowMockClient{hold: make(chan struct{}), emitted: make(chan struct{})}
	r := NewLoopRunner(smc, tool.NewRegistry(), DefaultSystemPrompt{Override: "x"}, "m", 4, 64, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, result := r.RunStream(ctx, "hi", "")
	for ev := range events {
		if _, ok := ev.(AssistantDelta); ok {
			cancel()
			close(smc.hold)
			break
		}
	}
	res := <-result
	if res.StopReason != "canceled" {
		t.Errorf("StopReason = %q, want canceled", res.StopReason)
	}
}

// v1 路径：Stream=false（默认）时 Run 使用 Chat()，从不使用 ChatStream。
// 由 mockClient.Calls() == 1 且 AssistantDelta 事件数为 0 验证。
func TestRun_StreamFalse_UsesChat(t *testing.T) {
	mc := &mockClient{
		queue: []llm.ChatResponse{
			{Choices: []llm.Choice{{Message: llm.Message{Content: "ok"}, FinishReason: "stop"}}},
		},
	}
	r := NewLoopRunner(mc, tool.NewRegistry(), DefaultSystemPrompt{Override: "x"}, "m", 4, 64, nil)
	events, result := r.Run(context.Background(), "hi")
	var deltas int
	for ev := range events {
		if _, ok := ev.(AssistantDelta); ok {
			deltas++
		}
	}
	<-result
	if deltas != 0 {
		t.Errorf("deltas = %d, want 0 (Stream=false)", deltas)
	}
	if mc.Calls() != 1 {
		t.Errorf("Chat calls = %d, want 1", mc.Calls())
	}
}

// v2 path: Run with Stream=true uses ChatStream and emits AssistantDelta.
func TestRun_StreamTrue_UsesChatStream(t *testing.T) {
	mc := &mockClient{
		queue: []llm.ChatResponse{
			{Choices: []llm.Choice{{Message: llm.Message{Content: "streamed"}, FinishReason: "stop"}}},
		},
	}
	r := NewLoopRunner(mc, tool.NewRegistry(), DefaultSystemPrompt{Override: "x"}, "m", 4, 64, nil)
	r.Stream = true
	events, result := r.Run(context.Background(), "hi")
	var deltas []string
	for ev := range events {
		if d, ok := ev.(AssistantDelta); ok {
			deltas = append(deltas, d.Text)
		}
	}
	<-result
	if len(deltas) != 1 || deltas[0] != "streamed" {
		t.Errorf("deltas = %#v, want [streamed]", deltas)
	}
}

// LoopRunner 同时满足 Runner 和 StreamingRunner 两个接口（编译期检查）。
func TestLoopRunner_ImplementsBothInterfaces(t *testing.T) {
	var _ Runner = (*LoopRunner)(nil)
	var _ StreamingRunner = (*LoopRunner)(nil)
}
