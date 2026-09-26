// Package store — 会话写租约（v5 P5-1）。
//
// lease.go 提供同 sid 写入互斥（单写者）。
//
// 为什么需要：
// v4 事件溯源下，SQLiteStore.Append / AppendEvent 接收并发的"同 sid"
// 写入时，会出现 seq 错乱（两个 goroutine 取到相同的
// COALESCE(MAX(seq), -1) + 1，结果抢占同一行）。Lease 抽象在写入入口
// 提供单写者语义；30s TTL 防止句柄泄漏后永久阻塞。
//
// 设计要点：
//   - 接口：Lease + LeaseHandle + NoopLease 三件套；
//   - 进程内实现：MemoryLease（sync.Mutex + map + 后台 reaper）；
//   - 跨进程：留给 v6+（Redis / DB 行锁）；
//   - 向后兼容：NoopLease 总是成功，调用方改一行可恢复 v4 行为。
package store

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrLeaseHeld 在 sid 已被另一 owner 持有时返回。
var ErrLeaseHeld = errors.New("store: lease held by another owner")

// 默认 TTL 与 reaper 周期。
const (
	defaultLeaseTTL        = 30 * time.Second
	defaultLeaseReaperTick = 5  * time.Second
)

// Lease 提供同 sid 的写入互斥。
//
// 语义：
//   - 成功：返回非 nil LeaseHandle，调用方必须 Release；
//   - 持有中：返回 ErrLeaseHeld（不阻塞）；
//   - 默认实现有 TTL，stale holder 会被后台 reaper 回收。
type Lease interface {
	Acquire(ctx context.Context, sid, owner string) (LeaseHandle, error)
}

// LeaseHandle 是已获取的租约句柄。
//
// Release 必须由 Acquire 成功的调用方在 defer 中调用；多次 Release
// 安全（no-op）。
type LeaseHandle interface {
	Sid() string
	Owner() string
	Release()
}

// NoopLease 是零开销的 Lease 实现，总是成功。用于"尚未启用租约"场景。
type NoopLease struct{}

// Acquire 立即返回空句柄。
func (NoopLease) Acquire(_ context.Context, sid, owner string) (LeaseHandle, error) {
	return noopLeaseHandle{sid: sid, owner: owner}, nil
}

type noopLeaseHandle struct{ sid, owner string }

func (h noopLeaseHandle) Sid() string   { return h.sid }
func (h noopLeaseHandle) Owner() string { return h.owner }
func (noopLeaseHandle) Release()        {}

// MemoryLease 是进程内的 Lease 实现。
//
// 并发安全。每次 Acquire 加锁；句柄持有者后台 reaper 周期回收。
//
// 注意：MemoryLease 与 v4 的 SQLiteStore 协作时，同进程多个 goroutine
// 都在争抢同一 sid，第二个调用方立即得到 ErrLeaseHeld——该调用方应
// 自行决定放弃或重试。Lease 不做重试队列。
type MemoryLease struct {
	mu       sync.Mutex
	held     map[string]leaseEntry
	ttl      time.Duration
	reaperT  time.Duration
}

type leaseEntry struct {
	owner    string
	acquired time.Time
	handle   *memoryLeaseHandle
}

// NewMemoryLease 构造空内存租约注册表。后台 reaper goroutine 自动
// 启动，用于回收过期条目。
func NewMemoryLease() *MemoryLease {
	l := &MemoryLease{
		held:    make(map[string]leaseEntry),
		ttl:     defaultLeaseTTL,
		reaperT: defaultLeaseReaperTick,
	}
	go l.reaperLoop()
	return l
}

// Acquire 试图持有 sid 的写租约。
//
// 行为：
//   - 命中空闲条目：返回新句柄；
//   - 已持有且未过期：返回 (nil, ErrLeaseHeld)；
//   - 已持有但已过期：抢占（删除后重新注册）。
func (l *MemoryLease) Acquire(_ context.Context, sid, owner string) (LeaseHandle, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	if e, ok := l.held[sid]; ok {
		if now.Sub(e.acquired) >= l.ttl {
			delete(l.held, sid)
		} else {
			return nil, ErrLeaseHeld
		}
	}
	h := &memoryLeaseHandle{
		lease: l,
		sid:   sid,
		owner: owner,
	}
	l.held[sid] = leaseEntry{
		owner:    owner,
		acquired: now,
		handle:   h,
	}
	return h, nil
}

// ForceRelease 强制释放 sid 上的租约（不考虑 ttl / owner）。
// 主要用于测试或管理命令。生产代码应使用 LeaseHandle.Release。
func (l *MemoryLease) ForceRelease(sid string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.held[sid]
	delete(l.held, sid)
	return ok
}

// Size 返回当前持有的租约数（诊断用）。
func (l *MemoryLease) Size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.held)
}

func (l *MemoryLease) reaperLoop() {
	t := time.NewTicker(l.reaperT)
	defer t.Stop()
	for range t.C {
		l.reapOnce()
	}
}

func (l *MemoryLease) reapOnce() {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for sid, e := range l.held {
		if now.Sub(e.acquired) >= l.ttl {
			delete(l.held, sid)
		}
	}
}

type memoryLeaseHandle struct {
	lease    *MemoryLease
	sid      string
	owner    string
	mu       sync.Mutex
	released bool
}

func (h *memoryLeaseHandle) Sid() string   { return h.sid }
func (h *memoryLeaseHandle) Owner() string { return h.owner }

// Release 释放租约。多次调用安全；幂等。
func (h *memoryLeaseHandle) Release() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.released {
		return
	}
	h.released = true
	h.lease.mu.Lock()
	defer h.lease.mu.Unlock()
	// 仅当句柄仍记录在我的 entry 上时删除；避免覆盖后来的 Acquire。
	if e, ok := h.lease.held[h.sid]; ok && e.handle == h {
		delete(h.lease.held, h.sid)
	}
}
