package agent

import (
	"context"
	"testing"

	"deepseek-harness-go/internal/llm"
	"deepseek-harness-go/internal/store"
	"deepseek-harness-go/internal/tool"
)

// 1. Runner + MapStore 集成：2 轮（工具调用轮后接最终答案轮）。
// 验证 session.Messages 长度为 5（system+user+assistant+tool+assistant）。
func TestRunner_MapStore_TwoRounds(t *testing.T) {
	s := store.NewMapStore()
	defer s.Close()
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
	r.Store = s

	events, result := r.RunStream(context.Background(), "hi", "")
	for ev := range events {
		_ = ev
	}
	res := <-result
	if res.StopReason != "no_tool_calls" {
		t.Errorf("StopReason = %q, want no_tool_calls", res.StopReason)
	}
	if res.SessionID == "" {
		t.Errorf("SessionID empty, want a value")
	}
	loaded, err := s.Load(context.Background(), res.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	// system + user + assistant(tool_call) + tool + assistant(text) = 5
	if len(loaded.Messages) != 5 {
		t.Errorf("Messages len = %d, want 5", len(loaded.Messages))
	}
	if loaded.Rounds != 2 {
		t.Errorf("Rounds = %d, want 2", loaded.Rounds)
	}
}

// 2. Resume: RunStream with existing sid → messages are loaded + new user
// message appended + 1 round runs.
func TestRunner_MapStore_ResumeSession(t *testing.T) {
	s := store.NewMapStore()
	defer s.Close()
	ctx := context.Background()

	// 预埋 2 条消息（system + user）
	sess, _ := s.Begin(ctx)
	_ = s.Append(ctx, sess.ID, llm.Message{Role: llm.RoleSystem, Content: "you are x"})
	_ = s.Append(ctx, sess.ID, llm.Message{Role: llm.RoleUser, Content: "first"})

	mc := &mockClient{
		queue: []llm.ChatResponse{
			{Choices: []llm.Choice{{Message: llm.Message{Content: "second"}, FinishReason: "stop"}}},
		},
	}
	reg := tool.NewRegistry()
	r := NewLoopRunner(mc, reg, DefaultSystemPrompt{Override: "NEW SYS"}, "m", 4, 64, nil)
	r.Store = s

	events, result := r.RunStream(ctx, "second prompt", sess.ID)
	for ev := range events {
		_ = ev
	}
	res := <-result
	if res.SessionID != sess.ID {
		t.Errorf("SessionID = %q, want %q", res.SessionID, sess.ID)
	}
	loaded, _ := s.Load(ctx, sess.ID)
	// 预埋的 2 + 新 user msg + assistant = 4
	if len(loaded.Messages) != 4 {
		t.Errorf("Messages len = %d, want 4", len(loaded.Messages))
	}
	// 最后一条消息应该是新的 assistant 轮次。
	last := loaded.Messages[len(loaded.Messages)-1]
	if last.Role != llm.RoleAssistant || last.Content != "second" {
		t.Errorf("last = %+v", last)
	}
}

// 3. Store == nil path: RunStream works exactly like v1; SessionID empty.
func TestRunner_NilStore_NoPersistence(t *testing.T) {
	mc := &mockClient{
		queue: []llm.ChatResponse{
			{Choices: []llm.Choice{{Message: llm.Message{Content: "ok"}, FinishReason: "stop"}}},
		},
	}
	reg := tool.NewRegistry()
	r := NewLoopRunner(mc, reg, DefaultSystemPrompt{Override: "x"}, "m", 4, 64, nil)
	// r.Store left nil.

	events, result := r.RunStream(context.Background(), "hi", "")
	for ev := range events {
		_ = ev
	}
	res := <-result
	if res.SessionID != "" {
		t.Errorf("SessionID = %q, want empty (no Store)", res.SessionID)
	}
}

// 4. Usage 跨轮聚合后持久化到 Store。
// 使用 2 轮 tool-call+answer 来驱动 2 次 LLM 调用。
func TestRunner_MapStore_Usage(t *testing.T) {
	s := store.NewMapStore()
	defer s.Close()
	mc := &mockClient{
		queue: []llm.ChatResponse{
			{Choices: []llm.Choice{{
				Message: llm.Message{
					ToolCalls: []llm.ToolCall{
						{ID: "c1", Type: "function", Function: llm.ToolCallFunc{Name: "echo", Arguments: `{"x":1}`}},
					},
				},
				FinishReason: "tool_calls",
			}}, Usage: &llm.Usage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8}},
			{Choices: []llm.Choice{{Message: llm.Message{Content: "b"}, FinishReason: "stop"}}, Usage: &llm.Usage{PromptTokens: 7, CompletionTokens: 2, TotalTokens: 9}},
		},
	}
	reg := tool.NewRegistry()
	_ = reg.Register(stubTool{})
	r := NewLoopRunner(mc, reg, DefaultSystemPrompt{Override: "x"}, "m", 4, 64, nil)
	r.Store = s

	events, result := r.RunStream(context.Background(), "hi", "")
	for ev := range events {
		_ = ev
	}
	res := <-result
	if res.Rounds != 2 {
		t.Errorf("Rounds = %d, want 2", res.Rounds)
	}
	if res.Usage.TotalTokens != 17 {
		t.Errorf("RunResult.Usage.TotalTokens = %d, want 17", res.Usage.TotalTokens)
	}
	loaded, _ := s.Load(context.Background(), res.SessionID)
	if loaded.UsageTotal.TotalTokens != 17 {
		t.Errorf("Store UsageTotal.TotalTokens = %d, want 17", loaded.UsageTotal.TotalTokens)
	}
}
