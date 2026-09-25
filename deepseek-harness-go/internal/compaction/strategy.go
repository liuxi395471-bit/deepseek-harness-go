// Package compaction 实现 DESIGN-v3 §D：长会话上下文压缩。
//
// Compactor 在 message 总 token 估算超过 Trigger 时调用 Strategy，
// 把历史压缩到 target 附近；system 消息与最近 KeepRecent 条消息
// 永远保留（§D.5 验收）。
package compaction

import (
	"context"

	"deepseek-harness-go/internal/llm"
)

// Strategy 是压缩策略契约（§D.1）。
type Strategy interface {
	Name() string
	// Compact 把 msgs 压缩到约 target token 估算值以内。
	// 实现必须保证第一条 system 消息（若存在）被保留，且压缩后的
	// 消息序列满足 OpenAI 协议的结构约束（tool 结果必须紧跟其
	// assistant tool_calls）。
	Compact(ctx context.Context, msgs []llm.Message, target int) ([]llm.Message, error)
}

// EstimateTokens 用纯字符估算 token 数（§D.5："tiktoken-go 或纯字符
// 估算"）。取保守估算：每 3 个 rune 记 1 token，最低每条消息 1 token。
// tool_calls 的 name + arguments 也计入。
func EstimateTokens(msgs []llm.Message) int {
	total := 0
	for _, m := range msgs {
		n := len([]rune(m.Content))
		for _, tc := range m.ToolCalls {
			n += len([]rune(tc.Function.Name)) + len([]rune(tc.Function.Arguments))
		}
		if n == 0 {
			n = 1
		}
		total += (n + 2) / 3
	}
	return total
}
