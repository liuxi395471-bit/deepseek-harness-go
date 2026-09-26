// Package usage — Token 计量域（v5 P5-2）。
//
// meter.go 提供按 sid 聚合每次 LLM 调用使用量的服务。和
// usage.Tracker（v2 单会话聚合）不同，Meter 同时维护多 session
// 累计，并额外区分 cache_read / cache_write / reasoning 三类 token。
//
// 设计要点：
//   - 接口：Meter + Metrics + Provider EventHook 三件套；
//   - 进程内实现：MemoryMeter（sync.RWMutex + map）；
//   - 与 EventStore 协作：调用方在 AppendEvent 之后调用 Account；
//   - 跨进程：留给 v6+（Redis / DB 汇总）。
//   - 向后兼容：零值 MemoryMeter{} 即可使用；nil interface 是 no-op。
package usage

import (
	"sync"
	"time"
)

// Meter 为每个 sid 累计 token 指标。
//
// Account 把单次 LLM 调用的使用量累加到 sid。
// Get 返回 sid 的累计指标；不存在或零值均返回零值 Metrics。
// Reset 清空 sid 的累计（管理命令用）。
// Snapshot 返回全部已注册 sid 的 metrics（监控用）。
type Meter interface {
	Account(sid string, a AccountRecord)
	Get(sid string) Metrics
	Reset(sid string)
	Snapshot() []Metrics
}

// AccountRecord 是单次 LLM 调用的 token 计数（v5 P5-2）。
//
// 与 store.LLMCallPayload 的对应字段语义一致，但放在 usage 包内
// 避免循环依赖；调用方负责字段转换。
type AccountRecord struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CacheReadTokens  int
	CacheWriteTokens int
	ReasoningTokens  int
	Model            string
	At               time.Time
}

// Metrics 是单 sid 的累计 token 指标。
type Metrics struct {
	Sid              string    `json:"sid"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	CacheReadTokens  int       `json:"cache_read_tokens"`
	CacheWriteTokens int       `json:"cache_write_tokens"`
	ReasoningTokens  int       `json:"reasoning_tokens"`
	Model            string    `json:"model,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
	Rounds           int       `json:"rounds"` // v5 P5-2 新增：调用次数
}

// MemoryMeter 是进程内 Meter 实现。
type MemoryMeter struct {
	mu      sync.RWMutex
	metrics map[string]*Metrics
}

// NewMemoryMeter 构造一个空 meter。
func NewMemoryMeter() *MemoryMeter {
	return &MemoryMeter{metrics: make(map[string]*Metrics)}
}

// Account 把 a 累加到 sid。sid == "" 静默跳过（测试场景）。
func (m *MemoryMeter) Account(sid string, a AccountRecord) {
	if sid == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	mt, ok := m.metrics[sid]
	if !ok {
		mt = &Metrics{Sid: sid}
		m.metrics[sid] = mt
	}
	mt.PromptTokens += a.PromptTokens
	mt.CompletionTokens += a.CompletionTokens
	mt.TotalTokens += a.TotalTokens
	mt.CacheReadTokens += a.CacheReadTokens
	mt.CacheWriteTokens += a.CacheWriteTokens
	mt.ReasoningTokens += a.ReasoningTokens
	if a.Model != "" {
		mt.Model = a.Model // 最后一次使用的模型
	}
	mt.Rounds++
	if a.At.IsZero() {
		mt.UpdatedAt = time.Now()
	} else {
		mt.UpdatedAt = a.At
	}
}

// Get 返回 sid 的累计指标；不存在时返回零值 Metrics（且 zero=true）。
func (m *MemoryMeter) Get(sid string) Metrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if mt, ok := m.metrics[sid]; ok {
		return *mt
	}
	return Metrics{Sid: sid}
}

// Reset 清空 sid 的累计。
func (m *MemoryMeter) Reset(sid string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.metrics, sid)
}

// Snapshot 返回全部 sid 指标的副本（按 sid 字典序）。
func (m *MemoryMeter) Snapshot() []Metrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Metrics, 0, len(m.metrics))
	for _, mt := range m.metrics {
		out = append(out, *mt)
	}
	return out
}

// NoopMeter 是零开销的 Meter 实现。所有操作都是 no-op。
type NoopMeter struct{}

// Account / Get / Reset / Snapshot 都是 no-op。
func (NoopMeter) Account(string, AccountRecord) {}
func (NoopMeter) Get(sid string) Metrics         { return Metrics{Sid: sid} }
func (NoopMeter) Reset(string)                   {}
func (NoopMeter) Snapshot() []Metrics            { return nil }
