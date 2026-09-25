package compaction

import (
	"context"
	"fmt"

	"deepseek-harness-go/internal/llm"
)

// TruncateStrategy 保留 system + 最近 N 条，在丢弃区间插入 summary
// placeholder，让 LLM 知道上下文被压缩（§D.2）。
type TruncateStrategy struct {
	KeepRecent int
}

// Name 实现 Strategy。
func (TruncateStrategy) Name() string { return "truncate" }

// Compact 实现 Strategy。
func (t TruncateStrategy) Compact(ctx context.Context, msgs []llm.Message, target int) ([]llm.Message, error) {
	return truncate(ctx, msgs, target, t.KeepRecent)
}

// validBoundary 返回 msgs[i] 是否可以作为压缩后剩余部分的起点。
// role=tool 的消息不能作为起点（其 assistant tool_calls 已被丢弃，
// OpenAI 协议要求结果必须紧跟调用）；其余角色都可以。
func validBoundary(m llm.Message) bool {
	return m.Role != llm.RoleTool
}

func truncate(_ context.Context, msgs []llm.Message, target, keepRecent int) ([]llm.Message, error) {
	if len(msgs) == 0 {
		return msgs, nil
	}
	head := 0
	if msgs[0].Role == llm.RoleSystem {
		head = 1 // system 永远保留（§D.5）
	}
	keep := keepRecent
	if keep <= 0 {
		keep = 6
	}

	// 第一步：从末尾向前保留消息，直到达到 target 预算；
	// 但至少保留 keep 条。
	start := len(msgs)
	tokens := 0
	for start > head {
		t := EstimateTokens(msgs[start-1 : start])
		if tokens+t > target && len(msgs)-start >= keep {
			break
		}
		tokens += t
		start--
	}

	// 第二步：结构合法性。start 若落在 tool 消息上，向后（丢得更少）
	// 推进到最近的合法边界。
	for start < len(msgs) && !validBoundary(msgs[start]) {
		start++
	}

	// 保留条数因合法性回退而低于 keep，或没有可丢内容：放弃压缩。
	if start <= head || len(msgs)-start < keep {
		return msgs, nil
	}

	dropped := start - head
	out := make([]llm.Message, 0, len(msgs)-dropped+1)
	out = append(out, msgs[:head]...)
	out = append(out, summaryPlaceholder(dropped))
	out = append(out, msgs[start:]...)
	return out, nil
}

// summaryPlaceholder 让 LLM 知道中间的上下文被压缩了。
func summaryPlaceholder(dropped int) llm.Message {
	return llm.Message{
		Role: llm.RoleUser,
		Content: fmt.Sprintf(
			"[系统提示：为控制上下文长度，此处省略了较早的 %d 条消息；请基于保留的部分继续。]",
			dropped,
		),
	}
}
