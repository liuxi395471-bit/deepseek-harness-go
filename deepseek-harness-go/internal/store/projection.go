// Package store — 投影缓存（v4 §A.5）。
//
// projection.go 定义派生视图的契约。运行时字段（messages /
// usage / phase）不再直接写入 store，而是由 Projector 从事件流
// Apply 出来的 ProjectionState 持有。
package store

import "sync"

// ProjectionState 是单个投影的内部状态类型。实现可以是任意 Go
// 值（[]llm.Message / llm.Usage / string 等）。零值必须表示
// "尚未应用任何事件"。
type ProjectionState any

// Projection 把事件流应用到自己的状态上。state 是 in-out：
// 实现需读取并原地修改（append / 累加 / 覆盖）。
//
// 实现必须满足：
//   - 同事件顺序 Apply 两次结果一致（幂等：f(f(s, e), e) = f(s, e)）
//   - 对未识别 EventType 视为 noop，不报错
//   - 错误来自载荷解析（如 Payload JSON 损坏）
type Projection interface {
	Name() string
	Apply(ev Event, state ProjectionState) error
}

// ProjectionCache 跨 session 缓存派生状态；命中即直接返回。
//
// 实现可以是 MemoryProjectionCache（v4 默认）或持久化版本（v5+）。
// 语义：Get 找不到时返回 (nil, false)；Put 覆盖已有；Invalidate 强制
// 下次重放。
type ProjectionCache interface {
	Get(sid, name string) (ProjectionState, bool)
	Put(sid, name string, state ProjectionState)
	Invalidate(sid string)
}

// MemoryProjectionCache 是 ProjectionCache 的进程内实现。
//
// 结构：outer key = sid，inner key = projection name；同 session 不同
// 投影相互独立。所有方法可并发使用。
//
// v4 阶段不设容量上限（接受内存增长），LRU 留 v5+。
type MemoryProjectionCache struct {
	mu   sync.RWMutex
	data map[string]map[string]ProjectionState
}

// NewMemoryProjectionCache 构造一个空 cache。
func NewMemoryProjectionCache() *MemoryProjectionCache {
	return &MemoryProjectionCache{data: make(map[string]map[string]ProjectionState)}
}

// Get 返回 (state, true) 表示命中；找不到返回 (nil, false)。
func (c *MemoryProjectionCache) Get(sid, name string) (ProjectionState, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	per, ok := c.data[sid]
	if !ok {
		return nil, false
	}
	s, ok := per[name]
	return s, ok
}

// Put 写入或覆盖 sid 的 name 投影。
func (c *MemoryProjectionCache) Put(sid, name string, state ProjectionState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	per, ok := c.data[sid]
	if !ok {
		per = make(map[string]ProjectionState)
		c.data[sid] = per
	}
	per[name] = state
}

// Invalidate 清空 sid 的全部投影。下次 Get 返回 false 触发重放。
func (c *MemoryProjectionCache) Invalidate(sid string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, sid)
}

// Size 返回 cache 当前持有的 session 数（用于诊断 / 测试）。
func (c *MemoryProjectionCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.data)
}
