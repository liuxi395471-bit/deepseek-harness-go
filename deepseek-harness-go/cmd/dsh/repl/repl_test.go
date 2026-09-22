package repl

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"deepseek-harness-go/internal/agent"
	"deepseek-harness-go/internal/llm"
	"deepseek-harness-go/internal/tool"
)

type mockClient struct {
	mu      sync.Mutex
	calls   int32
	queue   []llm.ChatResponse
	err     error
	gotReqs []llm.ChatRequest
	refill  func() llm.ChatResponse
}

func (m *mockClient) ChatStream(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamChunk, <-chan error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	errCh := make(chan error, 1)
	errCh <- errors.New("ChatStream not used in T2 tests; see T3")
	return ch, errCh
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

func newRunner(mc *mockClient) *agent.LoopRunner {
	reg := tool.NewRegistry()
	sys := agent.NewDefaultSystemPrompt("")
	return agent.NewLoopRunner(mc, reg, sys, "m", 4, 64, nil)
}

func TestRunOnce_PrintsFinalContent(t *testing.T) {
	mc := &mockClient{queue: []llm.ChatResponse{
		{Choices: []llm.Choice{{Index: 0, Message: llm.Message{Role: llm.RoleAssistant, Content: "hello"}}}},
	}}
	r := newRunner(mc)

	out, errOut := captureStd(t, func() {
		RunOnce(context.Background(), r, "hi", false)
	})

	if !strings.Contains(out, "hello") {
		t.Errorf("stdout missing 'hello': %q", out)
	}
	if strings.Contains(errOut, "[dsh] error") {
		t.Errorf("unexpected error footer: %q", errOut)
	}
}

func TestRunOnce_ErrorFooter(t *testing.T) {
	mc := &mockClient{err: errors.New("boom")}
	r := newRunner(mc)

	out, errOut := captureStd(t, func() {
		RunOnce(context.Background(), r, "hi", false)
	})
	_ = out
	if !strings.Contains(errOut, "[dsh] error: boom") {
		t.Errorf("errOut missing error footer: %q", errOut)
	}
}

func TestRunOnce_MaxRoundsFooter(t *testing.T) {
	loop := llm.ChatResponse{
		Choices: []llm.Choice{{Index: 0, Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
			ID: "c1", Type: "function",
			Function: llm.ToolCallFunc{Name: "g", Arguments: `{}`},
		}}}},
		},
	}
	mc := &mockClient{queue: []llm.ChatResponse{loop}, refill: func() llm.ChatResponse { return loop }}
	reg := tool.NewRegistry()
	reg.MustRegister(&tinyTool{})
	r := agent.NewLoopRunner(mc, reg, agent.NewDefaultSystemPrompt(""), "m", 2, 64, nil)

	out, errOut := captureStd(t, func() {
		RunOnce(context.Background(), r, "hi", false)
	})
	_ = out
	if !strings.Contains(errOut, "max-rounds") {
		t.Errorf("errOut missing max-rounds footer: %q", errOut)
	}
}

func TestRunOnce_ContextCanceled(t *testing.T) {
	mc := &mockClient{err: context.Canceled}
	r := newRunner(mc)
	out, errOut := captureStd(t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		RunOnce(ctx, r, "hi", false)
	})
	_ = out
	if !strings.Contains(errOut, "canceled") {
		t.Errorf("errOut missing canceled footer: %q", errOut)
	}
}

func TestRunOnce_DebugShowsToolCalls(t *testing.T) {
	mc := &mockClient{queue: []llm.ChatResponse{
		{Choices: []llm.Choice{{Index: 0, Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID: "c1", Type: "function",
				Function: llm.ToolCallFunc{Name: "tiny", Arguments: `{}`},
			}},
		}}}},
		{Choices: []llm.Choice{{Index: 0, Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}}},
	}}
	reg := tool.NewRegistry()
	reg.MustRegister(&tinyTool{})
	r := agent.NewLoopRunner(mc, reg, agent.NewDefaultSystemPrompt(""), "m", 4, 64, nil)

	out, errOut := captureStd(t, func() {
		RunOnce(context.Background(), r, "hi", true)
	})
	if !strings.Contains(out, "done") {
		t.Errorf("stdout = %q", out)
	}
	for _, want := range []string{"→ tool tiny", "← tiny (ok", "phase=llm_call", "AssistantMessage"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("errOut missing %q: %q", want, errOut)
		}
	}
}

type tinyTool struct{}

func (*tinyTool) Name() string                                  { return "tiny" }
func (*tinyTool) Description() string                           { return "ok" }
func (*tinyTool) Parameters() any                               { return nil }
func (*tinyTool) Execute(_ context.Context, _ json.RawMessage) (tool.Result, error) {
	return tool.Ok("done"), nil
}
