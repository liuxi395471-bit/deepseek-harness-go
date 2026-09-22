// Package usage 在 agent 循环中聚合 token 消耗，并根据按模型定价表
// 计算费用。
//
// Tracker 是协程安全的；对 Add 的并发调用由内部互斥锁串行化。
// 费用根据价格表和模型报告的 completion_tokens 惰性重算，因此未知
// 的模型名会返回 Cost=0 而不会 panic。
//
// 这是 v2 的 M5d 层；v1 没有用量显示。
package usage

import (
	"fmt"
	"sync"

	"deepseek-harness-go/internal/llm"
)

// PriceMap 将模型名映射为每 1k completion tokens 的美元单价。
// 按惯例，input tokens 按 completion 费率的 1/4 计费；对于没有
// input 成本的模型，调用方可以将值显式设为 0。
type PriceMap map[string]float64

// Snapshot 是 tracker 状态在某一时刻的视图，可安全地共享给
// HTTP/REPL 消费方。
type Snapshot struct {
	Prompt     int
	Completion int
	Total      int
	Cost       float64
	Currency   string // v1 规范中名为 CostCurr；为清晰起见改名为 Currency。
}

// Tracker 跨轮次累积用量。零值即可直接使用，但如需费用计算，
// 应通过 NewTracker 构造。
type Tracker struct {
	mu       sync.Mutex
	prompt   int
	complete int
	total    int
	cost     float64
	currency string
	prices   PriceMap
}

// NewTracker 使用给定的按模型价格表和货币代码构造 tracker。
// currency 会通过 Snapshot.Currency 返回，但不参与费用计算公式
// （该公式假定以美元计）。
func NewTracker(prices PriceMap, currency string) *Tracker {
	return &Tracker{
		currency: currency,
		prices:   prices,
	}
}

// Add 将 delta 累加进运行总量，并使用 delta 隐含的模型更新 Cost
// （这是调用方的职责）。返回新的快照。
//
// llm.Usage 本身不携带模型名；费用归因在调用方边界（Runner）处理，
// 因为那里知道模型是什么。因此这里的 Add() 只更新 token 计数；
// 仅在调用 SetCost 时才重算费用。
//
// 注意：希望按轮更新费用的调用方应另行调用 SetCost 并传入模型的
// 价格。Add() 不改变 Cost。
func (t *Tracker) Add(delta llm.Usage) Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.prompt += delta.PromptTokens
	t.complete += delta.CompletionTokens
	t.total += delta.TotalTokens
	return t.snapshotLocked()
}

// SetCost 直接设置运行总费用。runner 使用它基于当前活动模型
// 应用每轮的费用。
func (t *Tracker) SetCost(c float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cost = c
}

// Reset 清空 tracker 状态。
func (t *Tracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.prompt = 0
	t.complete = 0
	t.total = 0
	t.cost = 0
}

// Snapshot 返回当前状态的副本。
func (t *Tracker) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.snapshotLocked()
}

// snapshotLocked 是不加锁的内部实现；调用方必须持有 t.mu。
func (t *Tracker) snapshotLocked() Snapshot {
	return Snapshot{
		Prompt:     t.prompt,
		Completion: t.complete,
		Total:      t.total,
		Cost:       t.cost,
		Currency:   t.currency,
	}
}

// ComputeCost 返回给定 delta 在指定模型名下的费用（美元）。
// 若模型未知则返回 0 —— 策略是对未知模型绝不报错
// （DESIGN-v2 §A.4.2）。
//
// 公式：cost = (completion_tokens / 1000) * price
//
// 默认不对 input tokens 计费；需要 input 定价的调用方可以扩展
// 此函数。
func ComputeCost(delta llm.Usage, model string, prices PriceMap) float64 {
	price, ok := prices[model]
	if !ok {
		return 0
	}
	return float64(delta.CompletionTokens) / 1000.0 * price
}

// Format 将快照渲染为 REPL 底栏的一行文本：
//
//	[usage] prompt=42 completion=17 total=59 cost=$0.000
//
// 当 Cost 为 0 且 currency 非空时，仍会输出 "$0.000"；
// 当 currency 为空时，输出 "cost=N/A"。
func Format(s Snapshot) string {
	if s.Currency == "" {
		return fmt.Sprintf("[usage] prompt=%d completion=%d total=%d cost=N/A",
			s.Prompt, s.Completion, s.Total)
	}
	return fmt.Sprintf("[usage] prompt=%d completion=%d total=%d cost=$%.4f",
		s.Prompt, s.Completion, s.Total, s.Cost)
}
