package compaction

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"deepseek-harness-go/internal/llm"
)

// fakeLLM 记录请求并返回预设摘要。
type fakeLLM struct {
	summary string
	err     error
	reqs    []llm.ChatRequest
}

func (f *fakeLLM) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	f.reqs = append(f.reqs, req)
	if f.err != nil {
		return llm.ChatResponse{}, f.err
	}
	return llm.ChatResponse{Choices: []llm.Choice{{Message: llm.Message{Role: llm.RoleAssistant, Content: f.summary}}}}, nil
}

func (f *fakeLLM) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, <-chan error) {
	ch := make(chan llm.StreamChunk)
	errCh := make(chan error, 1)
	go func() { defer close(ch); _ = ctx; _ = req }()
	return ch, errCh
}

func sysMsg() llm.Message { return llm.Message{Role: llm.RoleSystem, Content: "system prompt"} }

func userMsgs(n, fill int) []llm.Message {
	out := []llm.Message{sysMsg()}
	for i := 0; i < n; i++ {
		out = append(out, llm.Message{
			Role:    llm.RoleUser,
			Content: strings.Repeat("x", fill) + " u" + itoa(i),
		})
	}
	return out
}

func itoa(n int) string { return fmt.Sprint(n) }

func TestEstimateTokens(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: strings.Repeat("a", 30)}, // 10 tokens
		{Role: llm.RoleUser, Content: "你好，世界测试"},           // 8 runes → 3 tokens
		{Role: llm.RoleAssistant, Content: ""},                   // 1 token
	}
	got := EstimateTokens(msgs)
	if got != 14 {
		t.Fatalf("EstimateTokens = %d, want 14", got)
	}
	// tool_calls 计入。
	withTool := []llm.Message{{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
		ID: "1", Function: llm.ToolCallFunc{Name: "shell", Arguments: strings.Repeat("{}", 12)},
	}}}}
	if EstimateTokens(withTool) < 4 {
		t.Fatalf("tool_calls not counted: %d", EstimateTokens(withTool))
	}
}

func TestCompactorNoTrigger(t *testing.T) {
	c := &Compactor{Strategy: TruncateStrategy{KeepRecent: 2}, Trigger: 10000}
	msgs := userMsgs(5, 10)
	out, compacted, err := c.Maybe(context.Background(), msgs)
	if err != nil || compacted {
		t.Fatalf("should not trigger: err=%v compacted=%v", err, compacted)
	}
	if len(out) != len(msgs) {
		t.Fatal("messages changed without trigger")
	}
}

func TestCompactorNilSafe(t *testing.T) {
	var c *Compactor
	msgs := userMsgs(3, 10)
	if _, ok, _ := c.Maybe(context.Background(), msgs); ok {
		t.Fatal("nil compactor triggered")
	}
}

func TestTruncateKeepsSystemAndRecent(t *testing.T) {
	msgs := []llm.Message{sysMsg()}
	for i := 0; i < 20; i++ {
		msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: strings.Repeat("m", 60) + " #" + itoa(i)})
	}
	// ~21 条，每条约 20 tokens；trigger 200 → target 100。
	tr := TruncateStrategy{KeepRecent: 4}
	out, err := tr.Compact(context.Background(), msgs, 100)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if len(out) >= len(msgs) {
		t.Fatalf("message count did not shrink: %d → %d", len(msgs), len(out))
	}
	if out[0].Role != llm.RoleSystem {
		t.Fatalf("first message must be system, got %s", out[0].Role)
	}
	if !strings.Contains(out[1].Content, "省略了较早的") {
		t.Fatalf("missing placeholder: %q", out[1].Content)
	}
	if len(out)-2 < 4 {
		t.Fatalf("keep-recent violated: kept %d", len(out)-2)
	}
	// 最近一条保留。
	if out[len(out)-1].Content != msgs[len(msgs)-1].Content {
		t.Fatal("most recent message dropped")
	}
}

func TestTruncateNothingToDrop(t *testing.T) {
	msgs := []llm.Message{sysMsg(), {Role: llm.RoleUser, Content: "short"}}
	out, err := TruncateStrategy{KeepRecent: 6}.Compact(context.Background(), msgs, 1)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("should give up, got %d", len(out))
	}
}

func TestTruncateToolBoundary(t *testing.T) {
	// 构造：system, u0, assistant(toolcall), tool, tool, u1..u5
	// 若 start 落在 tool 消息上，必须回退到合法边界。
	msgs := []llm.Message{sysMsg(), {Role: llm.RoleUser, Content: strings.Repeat("y", 90)}}
	msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, Content: "", ToolCalls: []llm.ToolCall{{ID: "t1", Function: llm.ToolCallFunc{Name: "x", Arguments: "{}"}}}})
	msgs = append(msgs, llm.Message{Role: llm.RoleTool, Content: "result1", ToolCallID: "t1"})
	for i := 0; i < 6; i++ {
		msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: strings.Repeat("z", 90) + " #" + itoa(i)})
	}
	out, err := TruncateStrategy{KeepRecent: 4}.Compact(context.Background(), msgs, 120)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	// 压缩后的序列必须合法：没有以 tool 消息开头的窗口。
	for i, m := range out {
		if i > 0 && m.Role == llm.RoleTool && out[i-1].Role != llm.RoleAssistant {
			t.Fatalf("orphan tool message at %d", i)
		}
	}
	if out[0].Role != llm.RoleSystem {
		t.Fatal("system lost")
	}
}

func TestLLMSummaryStrategy(t *testing.T) {
	fake := &fakeLLM{summary: "用户在讨论压缩策略。"}
	msgs := []llm.Message{sysMsg()}
	for i := 0; i < 15; i++ {
		msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: strings.Repeat("d", 60) + " #" + itoa(i)})
	}
	s := LLMSummaryStrategy{Client: fake, Model: "test-model", KeepRecent: 4}
	out, err := s.Compact(context.Background(), msgs, 120)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if len(out) >= len(msgs) {
		t.Fatalf("did not shrink: %d → %d", len(msgs), len(out))
	}
	if out[0].Role != llm.RoleSystem {
		t.Fatal("system lost")
	}
	if !strings.Contains(out[1].Content, "压缩为摘要：用户在讨论压缩策略。") {
		t.Fatalf("summary not injected: %q", out[1].Content)
	}
	if len(fake.reqs) != 1 {
		t.Fatalf("expected 1 summary call, got %d", len(fake.reqs))
	}
	// 摘要 prompt 携带被丢弃内容。
	joined := fake.reqs[0].Messages[1].Content
	if !strings.Contains(joined, "#0") {
		t.Fatalf("summary prompt missing dropped content prefix: %q", joined[:100])
	}
}

func TestLLMSummaryNoClient(t *testing.T) {
	if _, err := (LLMSummaryStrategy{}).Compact(context.Background(), userMsgs(10, 40), 50); err == nil {
		t.Fatal("expected error without client")
	}
}

func TestLLMSummaryUpstreamError(t *testing.T) {
	fake := &fakeLLM{err: errors.New("upstream down")}
	msgs := userMsgs(15, 60)
	_, err := (LLMSummaryStrategy{Client: fake, KeepRecent: 4}).Compact(context.Background(), msgs, 120)
	if err == nil {
		t.Fatal("expected upstream error")
	}
}

func TestMaybeWithLLMSummary(t *testing.T) {
	fake := &fakeLLM{summary: "s"}
	c := &Compactor{Strategy: LLMSummaryStrategy{Client: fake, KeepRecent: 2}, Trigger: 300}
	msgs := userMsgs(20, 60)
	out, compacted, err := c.Maybe(context.Background(), msgs)
	if err != nil || !compacted {
		t.Fatalf("trigger failed: err=%v compacted=%v", err, compacted)
	}
	if len(out) >= len(msgs) {
		t.Fatal("did not shrink")
	}
}
