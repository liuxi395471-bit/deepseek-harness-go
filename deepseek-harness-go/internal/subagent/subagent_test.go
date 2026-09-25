package subagent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"deepseek-harness-go/internal/agent"
	"deepseek-harness-go/internal/llm"
	"deepseek-harness-go/internal/tool"
)

// mockLLM 按 model 名路由预设响应；空 model 返回默认文本。
type mockLLM struct {
	defaultText string
	// byPromptSuffix：prompt 包含关键词时返回对应文本（模拟父子不同响应）。
	byPrompt map[string]string
	chats    int
}

func (m *mockLLM) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	m.chats++
	if err := ctx.Err(); err != nil {
		return llm.ChatResponse{}, err
	}
	text := m.defaultText
	if m.byPrompt != nil {
		for key, v := range m.byPrompt {
			for _, msg := range req.Messages {
				if strings.Contains(msg.Content, key) {
					text = v
					break
				}
			}
		}
	}
	return llm.ChatResponse{
		Choices: []llm.Choice{{
			FinishReason: "stop",
			Message:      llm.Message{Role: llm.RoleAssistant, Content: text},
		}},
	}, nil
}

func (m *mockLLM) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, <-chan error) {
	ch := make(chan llm.StreamChunk, 1)
	errCh := make(chan error, 1)
	go func() {
		defer close(ch)
		defer close(errCh)
		resp, err := m.Chat(ctx, req)
		if err != nil {
			errCh <- err
			return
		}
		if len(resp.Choices) > 0 {
			ch <- llm.StreamChunk{Text: resp.Choices[0].Message.Content, Finish: "stop"}
		}
	}()
	return ch, errCh
}

func newChildRunner(c llm.Client) *agent.LoopRunner {
	reg := tool.NewRegistry()
	_ = reg.Register(stubTool{})
	return agent.NewLoopRunner(c, reg, agent.NewDefaultSystemPrompt(""), "test-model", 4, 512, nil)
}

type stubTool struct{}

func (stubTool) Name() string { return "echo" }
func (stubTool) Description() string {
	return "echo"
}
func (stubTool) Parameters() any { return map[string]any{} }
func (stubTool) Execute(_ context.Context, _ json.RawMessage) (tool.Result, error) {
	return tool.Ok("echo"), nil
}

func TestSpawnReturnsChildText(t *testing.T) {
	m := &mockLLM{defaultText: "子 agent 的最终答案"}
	s := &SubRunner{Child: newChildRunner(m)}
	text, err := s.Spawn(context.Background(), "做点事")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if text != "子 agent 的最终答案" {
		t.Fatalf("text = %q", text)
	}
}

func TestParentRunnerFinalContainsChildResult(t *testing.T) {
	// 父 LLM：第一轮调 agent_spawn；第二轮（收到 tool result）输出最终答案。
	// 子 LLM：返回固定文本。
	childLLM := &mockLLM{defaultText: "CHILD_RESULT_42"}
	s := &SubRunner{Child: newChildRunner(childLLM)}

	reg := tool.NewRegistry()
	_ = reg.Register(stubTool{})
	if err := reg.Register(NewSpawnTool(s)); err != nil {
		t.Fatalf("register: %v", err)
	}

	parentLLM := &scriptedLLM{responses: []llm.Message{
		{Role: llm.RoleAssistant, Content: "", ToolCalls: []llm.ToolCall{{
			ID: "c1", Type: "function",
			Function: llm.ToolCallFunc{Name: "agent_spawn", Arguments: `{"prompt":"task"}`},
		}}},
		{Role: llm.RoleAssistant, Content: "final: CHILD_RESULT_42 received"},
	}}
	parent := agent.NewLoopRunner(parentLLM, reg, agent.NewDefaultSystemPrompt(""), "test-model", 8, 512, nil)

	events, resCh := parent.Run(context.Background(), "delegate please")
	for range events {
	}
	res := <-resCh
	if res.Error != nil {
		t.Fatalf("parent error: %v", res.Error)
	}
	if res.StopReason != "no_tool_calls" {
		t.Fatalf("stop = %s", res.StopReason)
	}
	found := false
	for _, m := range res.FinalMessages {
		if m.Role == llm.RoleTool && strings.Contains(m.Content, "CHILD_RESULT_42") {
			found = true
		}
	}
	if !found {
		t.Fatalf("tool result with child text missing: %+v", res.FinalMessages)
	}
}

// scriptedLLM 依次返回预设响应。
type scriptedLLM struct {
	responses []llm.Message
	i         int
}

func (s *scriptedLLM) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	if s.i >= len(s.responses) {
		return llm.ChatResponse{Choices: []llm.Choice{{
			Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}}}, nil
	}
	m := s.responses[s.i]
	s.i++
	return llm.ChatResponse{Choices: []llm.Choice{{Message: m}}}, nil
}

func (s *scriptedLLM) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, <-chan error) {
	ch := make(chan llm.StreamChunk, 1)
	errCh := make(chan error, 1)
	go func() {
		defer close(ch)
		defer close(errCh)
		resp, err := s.Chat(ctx, req)
		if err != nil {
			errCh <- err
			return
		}
		ch <- llm.StreamChunk{Text: resp.Choices[0].Message.Content, Finish: "stop"}
	}()
	return ch, errCh
}

func TestSubagentCanceledIsError(t *testing.T) {
	// 子 LLM 挂起直到 ctx 取消。
	m := &blockingLLM{}
	s := &SubRunner{Child: newChildRunner(m)}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := s.Spawn(ctx, "slow task")
	if err == nil {
		t.Fatal("expected cancel error")
	}
	if !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("err = %v", err)
	}
	// ToToolResult 语义。
	r := SubResult{StopReason: "canceled", Err: context.Canceled}.ToToolResult()
	if !r.IsError || !strings.Contains(r.Content, "canceled") {
		t.Fatalf("ToToolResult = %+v", r)
	}
}

// blockingLLM 在 Chat 上阻塞直到 ctx 取消。
type blockingLLM struct{}

func (blockingLLM) Chat(ctx context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	<-ctx.Done()
	return llm.ChatResponse{}, ctx.Err()
}

func (blockingLLM) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, <-chan error) {
	ch := make(chan llm.StreamChunk)
	errCh := make(chan error, 1)
	go func() { defer close(ch); <-ctx.Done(); errCh <- ctx.Err(); close(errCh) }()
	return ch, errCh
}

func TestSpawnToolErrorsAreToolResults(t *testing.T) {
	// panic 场景：spawner panic → Execute 的 result 是 is_error。
	panicky := spawnerFunc(func(ctx context.Context, prompt string) (string, error) {
		panic("child exploded")
	})
	t2 := NewSpawnTool(panicky)
	// SpawnTool.Execute 不含 recover（panic 由 runner 的 safeExecute 兜底，
	// 这正是 §B.3 "子 agent panic → 父 tool_result is_error=true" 的路径）。
	defer func() {
		if x := recover(); x == nil {
			t.Fatal("expected panic to propagate to runner's safeExecute")
		}
	}()
	_, _ = t2.Execute(context.Background(), json.RawMessage(`{"prompt":"x"}`))
}

// spawnerFunc 让闭包满足 Spawner。
type spawnerFunc func(ctx context.Context, prompt string) (string, error)

func (f spawnerFunc) Spawn(ctx context.Context, prompt string) (string, error) {
	return f(ctx, prompt)
}

func TestSpawnToolMaxDepth(t *testing.T) {
	// depth 已达上限时 Execute 返回 is_error 而不是再 spawn。
	called := false
	s := spawnerFunc(func(ctx context.Context, prompt string) (string, error) {
		called = true
		return "", nil
	})
	t2 := NewSpawnTool(s)
	res, err := t2.Execute(WithDepth(context.Background(), MaxDepth), json.RawMessage(`{"prompt":"nested"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "max spawn depth") {
		t.Fatalf("res = %+v", res)
	}
	if called {
		t.Fatal("spawner should not be invoked at max depth")
	}
}

func TestChildRegistryExcludesSpawn(t *testing.T) {
	reg := tool.NewRegistry()
	_ = reg.Register(stubTool{})
	if err := reg.Register(NewSpawnTool(&SubRunner{})); err != nil {
		t.Fatal(err)
	}
	s := NewSync(&mockLLM{defaultText: "x"}, reg, "m", 4, 128, nil)
	for _, name := range s.Child.Registry.Names() {
		if name == "agent_spawn" {
			t.Fatal("child registry must not contain agent_spawn")
		}
	}
	// 父注册表不受影响。
	if _, ok := reg.Get("agent_spawn"); !ok {
		t.Fatal("parent registry should still have agent_spawn")
	}
}

func TestAttachRegistersTool(t *testing.T) {
	reg := tool.NewRegistry()
	s, err := Attach(reg, &mockLLM{defaultText: "x"}, "m", 4, 128, nil)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if s.Child == nil {
		t.Fatal("nil child")
	}
	if _, ok := reg.Get("agent_spawn"); !ok {
		t.Fatal("agent_spawn not registered")
	}
	// 重复 Attach 报 ErrDuplicate。
	if _, err := Attach(reg, &mockLLM{defaultText: "x"}, "m", 4, 128, nil); err == nil {
		t.Fatal("duplicate attach should fail")
	}
}

func TestSpawnToolInvalidArgs(t *testing.T) {
	t2 := NewSpawnTool(spawnerFunc(func(_ context.Context, _ string) (string, error) { return "", nil }))
	res, err := t2.Execute(context.Background(), json.RawMessage(`not json`))
	if err != nil || !res.IsError {
		t.Fatalf("invalid json: %+v %v", res, err)
	}
	res, err = t2.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil || !res.IsError || !strings.Contains(res.Content, "prompt required") {
		t.Fatalf("empty prompt: %+v %v", res, err)
	}
}

func TestFinalAssistantText(t *testing.T) {
	res := agent.RunResult{FinalMessages: []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "u"},
		{Role: llm.RoleAssistant, Content: "the answer"},
		{Role: llm.RoleTool, Content: "ignored", ToolCallID: "t"},
	}}
	if got := finalAssistantText(res); got != "the answer" {
		t.Fatalf("got %q", got)
	}
	if got := finalAssistantText(agent.RunResult{}); got != "" {
		t.Fatalf("empty: %q", got)
	}
}
