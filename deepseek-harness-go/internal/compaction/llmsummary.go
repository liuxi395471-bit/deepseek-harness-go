package compaction

import (
	"context"
	"fmt"
	"strings"

	"deepseek-harness-go/internal/llm"
)

// LLMSummaryStrategy 用同一个 LLM client 把被丢弃的部分总结为一段
// 摘要并插回原位（§D.3）。不引入新模型。
type LLMSummaryStrategy struct {
	Client     llm.Client
	Model      string
	KeepRecent int
	// MaxSummaryChars 是摘要长度上限提示；默认 200 字。
	MaxSummaryChars int
}

// Name 实现 Strategy。
func (LLMSummaryStrategy) Name() string { return "llm-summary" }

// Compact 实现 Strategy：先用 truncate 决定保留窗口（保证结构合法），
// 再把丢弃区间交给 LLM 做摘要，替换掉 placeholder。
func (s LLMSummaryStrategy) Compact(ctx context.Context, msgs []llm.Message, target int) ([]llm.Message, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("compaction: llm-summary requires an LLM client")
	}
	base, err := truncate(ctx, msgs, target, s.KeepRecent)
	if err != nil {
		return nil, err
	}
	// 找到 truncate 插入的 placeholder。
	idx := -1
	for i, m := range base {
		if m.Role == llm.RoleUser && strings.HasPrefix(m.Content, "[系统提示：") {
			idx = i
			break
		}
	}
	if idx < 0 {
		return base, nil // 没丢任何消息
	}
	dropped := extractDropped(msgs, base, idx)
	summary, err := summarize(ctx, s.Client, s.Model, dropped, s.MaxSummaryChars)
	if err != nil {
		return nil, err
	}
	base[idx] = llm.Message{
		Role: llm.RoleUser,
		Content: fmt.Sprintf(
			"[系统提示：为控制上下文长度，较早的 %d 条消息已压缩为摘要：%s]",
			len(dropped), summary,
		),
	}
	return base, nil
}

// extractDropped 反推被丢弃的原始消息区间：
// compacted[:idx] 是保留的头部（system），compacted[idx+1:] 的条数
// 与原始尾部一致，因此丢弃数 = len(orig) - 头部 - 尾部。
func extractDropped(orig, compacted []llm.Message, phIdx int) []llm.Message {
	head := phIdx
	tail := len(compacted) - phIdx - 1
	droppedCount := len(orig) - head - tail
	if droppedCount <= 0 || head+droppedCount > len(orig) {
		return nil
	}
	return orig[head : head+droppedCount]
}

func summarize(ctx context.Context, c llm.Client, model string, dropped []llm.Message, maxChars int) (string, error) {
	if maxChars <= 0 {
		maxChars = 200
	}
	if len(dropped) == 0 {
		return "", nil
	}
	var b strings.Builder
	for _, m := range dropped {
		b.WriteString(string(m.Role))
		b.WriteString(": ")
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	req := llm.ChatRequest{
		Model: model,
		Messages: []llm.Message{
			{
				Role:    llm.RoleSystem,
				Content: fmt.Sprintf("你是对话摘要器。请用不超过 %d 字总结以下对话的关键信息，保留事实、决定与未完成事项。只输出摘要本身。", maxChars),
			},
			{Role: llm.RoleUser, Content: b.String()},
		},
	}
	resp, err := c.Chat(ctx, req)
	if err != nil {
		return "", fmt.Errorf("compaction: summary call: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("compaction: summary call returned no choices")
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}
