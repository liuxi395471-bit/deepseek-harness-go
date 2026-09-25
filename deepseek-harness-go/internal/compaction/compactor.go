package compaction

import (
	"context"

	"deepseek-harness-go/internal/llm"
)

// Compactor 是调度器（§D.1）：判断 message 总 token 估算是否超过
// Trigger，超阈值时委托给 Strategy 压缩到约 Trigger/2。
type Compactor struct {
	Strategy   Strategy
	Trigger    int // tokens 估算超过此值触发
	KeepRecent int // 永远保留最近 N 条
}

// Maybe 检查 msgs；未超阈值时原样返回 (msgs, false, nil)。
// 超阈值时执行压缩并返回 (compacted, true, nil)。
// 压缩失败不致命：返回原消息与 err，由调用方决定是否记日志。
func (c *Compactor) Maybe(ctx context.Context, msgs []llm.Message) ([]llm.Message, bool, error) {
	if c == nil || c.Strategy == nil || c.Trigger <= 0 {
		return msgs, false, nil
	}
	if EstimateTokens(msgs) <= c.Trigger {
		return msgs, false, nil
	}
	target := c.Trigger / 2
	out, err := c.Strategy.Compact(ctx, msgs, target)
	if err != nil {
		return msgs, false, err
	}
	return out, true, nil
}
