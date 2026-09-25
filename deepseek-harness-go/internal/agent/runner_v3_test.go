package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"deepseek-harness-go/internal/audit"
	"deepseek-harness-go/internal/compaction"
	"deepseek-harness-go/internal/llm"
	"deepseek-harness-go/internal/skill"
	"deepseek-harness-go/internal/tool"
)

// capturingLLM 记录每次请求的 messages，返回脚本化的响应。
type capturingLLM struct {
	scripted []llm.Message
	reqs     []llm.ChatRequest
}

func (c *capturingLLM) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	c.reqs = append(c.reqs, req)
	if len(c.scripted) == 0 {
		return llm.ChatResponse{Choices: []llm.Choice{{
			Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}}}, nil
	}
	m := c.scripted[0]
	c.scripted = c.scripted[1:]
	return llm.ChatResponse{Choices: []llm.Choice{{Message: m}}}, nil
}

func (c *capturingLLM) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, <-chan error) {
	ch := make(chan llm.StreamChunk, 1)
	errCh := make(chan error, 1)
	go func() {
		defer close(ch)
		defer close(errCh)
		resp, err := c.Chat(ctx, req)
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

// echoTool 原样回显，便于触发 tool_call 审计。
type echoTool struct{}

func (echoTool) Name() string        { return "echo" }
func (echoTool) Description() string { return "echo" }
func (echoTool) Parameters() any     { return map[string]any{} }
func (echoTool) Execute(_ context.Context, args json.RawMessage) (tool.Result, error) {
	return tool.Ok("echo:" + string(args)), nil
}

func TestSkillInjectionIntoSystemPrompt(t *testing.T) {
	llm2 := &capturingLLM{scripted: []llm.Message{
		{Role: llm.RoleAssistant, Content: "ok"},
	}}
	reg := tool.NewRegistry()
	_ = reg.Register(echoTool{})
	r := NewLoopRunner(llm2, reg, NewDefaultSystemPrompt("base system"), "m", 4, 128, nil)
	r.Skills = []skill.Skill{
		{Name: "review", Trigger: "review", Body: "REVIEW_BODY_MARKER"},
		{Name: "deploy", Trigger: "deploy", Body: "DEPLOY_BODY_MARKER"},
	}

	events, resCh := r.Run(context.Background(), "please review this")
	for range events {
	}
	res := <-resCh
	if res.Error != nil {
		t.Fatalf("run error: %v", res.Error)
	}
	if len(llm2.reqs) == 0 {
		t.Fatal("no llm calls")
	}
	sys := llm2.reqs[0].Messages[0].Content
	if !strings.HasPrefix(sys, "base system") {
		t.Fatalf("system prompt lost: %q", sys)
	}
	if !strings.Contains(sys, "REVIEW_BODY_MARKER") {
		t.Fatalf("review skill not injected: %q", sys)
	}
	if strings.Contains(sys, "DEPLOY_BODY_MARKER") {
		t.Fatalf("non-matching skill should not be injected: %q", sys)
	}
}

func TestSkillRerevaluatesPerRoundNoAccumulation(t *testing.T) {
	// B11 修复回归：第二轮 user 提到 "deploy"，首轮注入的 review
	// body 必须从 system 中消失（不能与 deploy body 叠加）。
	reg := tool.NewRegistry()
	r := NewLoopRunner(&capturingLLM{}, reg, NewDefaultSystemPrompt("base"), "m", 4, 128, nil)
	r.Skills = []skill.Skill{
		{Name: "review", Trigger: "review", Body: "REVIEW_BODY"},
		{Name: "deploy", Trigger: "deploy", Body: "DEPLOY_BODY"},
	}
	r.baseSystem = r.System.Build(r.Registry)
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: r.baseSystem},
		{Role: llm.RoleUser, Content: "review this"},
	}
	// 首轮：注入 review
	r.injectSkills(msgs, "review this")
	if !strings.Contains(msgs[0].Content, "REVIEW_BODY") {
		t.Fatalf("review body missing: %q", msgs[0].Content)
	}
	if strings.Contains(msgs[0].Content, "DEPLOY_BODY") {
		t.Fatalf("deploy should not be in round 1: %q", msgs[0].Content)
	}
	// 第二轮：模拟用户在 msgs 里追加 "please deploy"
	msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, Content: "ok-1"})
	msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: "please deploy"})
	r.injectSkills(msgs, "please deploy")
	if strings.Contains(msgs[0].Content, "REVIEW_BODY") {
		t.Fatalf("review body leaked: %q", msgs[0].Content)
	}
	if !strings.Contains(msgs[0].Content, "DEPLOY_BODY") {
		t.Fatalf("deploy body missing: %q", msgs[0].Content)
	}
	// 第三轮：prompt 无 trigger → system 应回到 base
	msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, Content: "ok-2"})
	msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: "unrelated chat"})
	r.injectSkills(msgs, "unrelated chat")
	if strings.Contains(msgs[0].Content, "REVIEW_BODY") ||
		strings.Contains(msgs[0].Content, "DEPLOY_BODY") {
		t.Fatalf("skill leaked: %q", msgs[0].Content)
	}
	if !strings.HasPrefix(msgs[0].Content, "base") {
		t.Fatalf("base lost: %q", msgs[0].Content)
	}
}

func TestSkillAlwaysOnInjectsEveryRound(t *testing.T) {
	llm2 := &capturingLLM{scripted: []llm.Message{
		{Role: llm.RoleAssistant, Content: "ok"},
	}}
	reg := tool.NewRegistry()
	r := NewLoopRunner(llm2, reg, NewDefaultSystemPrompt("base"), "m", 4, 128, nil)
	r.Skills = []skill.Skill{{Name: "style", Always: true, Body: "ALWAYS_MARKER"}}

	events, resCh := r.Run(context.Background(), "hello")
	for range events {
	}
	<-resCh
	if len(llm2.reqs) == 0 || !strings.Contains(llm2.reqs[0].Messages[0].Content, "ALWAYS_MARKER") {
		t.Fatal("always-on skill not injected")
	}
}

func TestNoSkillsNoInjection(t *testing.T) {
	llm2 := &capturingLLM{scripted: []llm.Message{
		{Role: llm.RoleAssistant, Content: "ok"},
	}}
	reg := tool.NewRegistry()
	r := NewLoopRunner(llm2, reg, NewDefaultSystemPrompt("base"), "m", 4, 128, nil)
	events, resCh := r.Run(context.Background(), "review this")
	for range events {
	}
	<-resCh
	// 无 Skills 时 system 原样。
	if llm2.reqs[0].Messages[0].Content != "base" {
		t.Fatalf("system changed: %q", llm2.reqs[0].Messages[0].Content)
	}
}

func TestCompactionTriggersBeforeLLMCall(t *testing.T) {
	// 触发一次 tool 调用后第二轮前上下文会变大；用低阈值 Compactor。
	first := llm.Message{Role: llm.RoleAssistant, Content: "", ToolCalls: []llm.ToolCall{{
		ID: "c1", Type: "function",
		Function: llm.ToolCallFunc{Name: "echo", Arguments: `{"x":"` + strings.Repeat("p", 3000) + `"}`},
	}}}
	llm2 := &capturingLLM{scripted: []llm.Message{first}}
	reg := tool.NewRegistry()
	_ = reg.Register(echoTool{})
	r := NewLoopRunner(llm2, reg, NewDefaultSystemPrompt("sys"), "m", 8, 8192, nil)
	// 消息总量估算 > 1000 tokens 触发；target 500。
	r.Compactor = &compaction.Compactor{
		Strategy:   compaction.TruncateStrategy{KeepRecent: 6},
		Trigger:    1000,
		KeepRecent: 6,
	}

	events, resCh := r.Run(context.Background(), strings.Repeat("q", 3000))
	var compactedEvents int
	for ev := range events {
		if _, ok := ev.(Compacted); ok {
			compactedEvents++
		}
	}
	res := <-resCh
	if res.Error != nil {
		t.Fatalf("run error: %v", res.Error)
	}
	if compactedEvents == 0 {
		t.Fatal("expected Compacted event")
	}
	// 发给 LLM 的第二轮请求消息数应明显小于完整序列。
	if len(llm2.reqs) < 2 {
		t.Fatalf("expected >=2 llm calls, got %d", len(llm2.reqs))
	}
}

func TestAuditRecordsToolAndLLMCalls(t *testing.T) {
	llm2 := &capturingLLM{scripted: []llm.Message{
		{Role: llm.RoleAssistant, Content: "", ToolCalls: []llm.ToolCall{{
			ID: "c1", Type: "function",
			Function: llm.ToolCallFunc{Name: "echo", Arguments: `{"k":"v"}`},
		}}},
	}}
	reg := tool.NewRegistry()
	_ = reg.Register(echoTool{})

	mem := &memAuditSink{}
	r := NewLoopRunner(llm2, reg, NewDefaultSystemPrompt("sys"), "test-model", 8, 8192, nil)
	r.Audit = mem

	events, resCh := r.Run(context.Background(), "hi")
	for range events {
	}
	res := <-resCh
	if res.Error != nil {
		t.Fatalf("run error: %v", res.Error)
	}
	var sawTool, sawLLM bool
	for _, ev := range mem.events {
		switch ev.Event {
		case audit.EventToolCall:
			sawTool = true
			if ev.Tool != "echo" || ev.ArgsHash == "" || ev.ArgsRaw != `{"k":"v"}` {
				t.Fatalf("tool_call event wrong: %+v", ev)
			}
		case audit.EventLLMCall:
			sawLLM = true
			if ev.Model != "test-model" {
				t.Fatalf("llm_call model = %q", ev.Model)
			}
		}
	}
	if !sawTool || !sawLLM {
		t.Fatalf("missing events: tool=%v llm=%v (all=%+v)", sawTool, sawLLM, mem.events)
	}
}

func TestNilAuditAndObsNoop(t *testing.T) {
	// Audit nil + Obs 零值：完整跑通（零开销路径）。
	llm2 := &capturingLLM{}
	reg := tool.NewRegistry()
	r := NewLoopRunner(llm2, reg, NewDefaultSystemPrompt(""), "m", 4, 128, nil)
	events, resCh := r.Run(context.Background(), "hi")
	for range events {
	}
	if res := <-resCh; res.Error != nil {
		t.Fatalf("run error: %v", res.Error)
	}
}

// memAuditSink 收集事件的审计 sink。
type memAuditSink struct {
	events []audit.Event
}

func (m *memAuditSink) Log(_ context.Context, ev audit.Event) { m.events = append(m.events, ev) }
func (m *memAuditSink) Close() error                          { return nil }
