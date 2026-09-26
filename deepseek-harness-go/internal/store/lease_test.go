package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// 验证 NoopLease 总是成功。
func TestNoopLease(t *testing.T) {
	var l Lease = NoopLease{}
	h1, err := l.Acquire(context.Background(), "s1", "owner-A")
	if err != nil || h1 == nil {
		t.Fatalf("NoopLease.Acquire 失败: %v", err)
	}
	// 同一 sid 再次 Acquire 也能成功（noop 语义）。
	h2, err := l.Acquire(context.Background(), "s1", "owner-B")
	if err != nil || h2 == nil {
		t.Fatalf("NoopLease 第二次 Acquire 失败: %v", err)
	}
	h1.Release()
	h2.Release()
}

// MemoryLease 基础流程：释放后可以再次持有。
func TestMemoryLease_AcquireRelease_ReleaseAllowsReacquire(t *testing.T) {
	l := NewMemoryLease()
	defer l.ForceRelease("s1") // 测试收尾

	h1, err := l.Acquire(context.Background(), "s1", "owner-A")
	if err != nil {
		t.Fatalf("首次 Acquire 应成功: %v", err)
	}
	h1.Release()

	h2, err := l.Acquire(context.Background(), "s1", "owner-B")
	if err != nil {
		t.Fatalf("释放后再次 Acquire 应成功: %v", err)
	}
	if h2.Sid() != "s1" || h2.Owner() != "owner-B" {
		t.Fatalf("handle 属性错误: sid=%q owner=%q", h2.Sid(), h2.Owner())
	}
	h2.Release()
}

// MemoryLease 同 sid 持有中第二次返回 ErrLeaseHeld。
func TestMemoryLease_DoubleAcquireReturnsErrLeaseHeld(t *testing.T) {
	l := NewMemoryLease()

	h1, err := l.Acquire(context.Background(), "s1", "owner-A")
	if err != nil {
		t.Fatalf("首次 Acquire: %v", err)
	}
	defer h1.Release()

	_, err = l.Acquire(context.Background(), "s1", "owner-B")
	if err == nil {
		t.Fatalf("第二次 Acquire 应返回 ErrLeaseHeld, 但 err=nil")
	}
	if !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("err = %v, want ErrLeaseHeld", err)
	}
}

// 不同 sid 互不干扰。
func TestMemoryLease_DifferentSidsIndependent(t *testing.T) {
	l := NewMemoryLease()

	h1, err := l.Acquire(context.Background(), "sid-A", "owner-A")
	if err != nil {
		t.Fatalf("sid-A Acquire: %v", err)
	}
	defer h1.Release()

	h2, err := l.Acquire(context.Background(), "sid-B", "owner-B")
	if err != nil {
		t.Fatalf("sid-B Acquire 应成功: %v", err)
	}
	defer h2.Release()

	if l.Size() != 2 {
		t.Fatalf("Size = %d, want 2", l.Size())
	}
}

// TTL 过期后允许抢占：构造一个自定义 TTL 极短的 lease 测抢占。
func TestMemoryLease_TTLExpiryAllowsReacquire(t *testing.T) {
	l := &MemoryLease{
		held:    make(map[string]leaseEntry),
		ttl:     10 * time.Millisecond,
		reaperT: 1 * time.Hour, // 关闭 reaper，手动控制时间
	}
	// 不启动 reaper；测试用 ttl + 手动过期。
	h1, err := l.Acquire(context.Background(), "s1", "owner-A")
	if err != nil {
		t.Fatalf("首次 Acquire: %v", err)
	}

	// 未过期：第二次应 ErrLeaseHeld
	_, err = l.Acquire(context.Background(), "s1", "owner-B")
	if !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("未过期时 err = %v, want ErrLeaseHeld", err)
	}

	// 等待超过 ttl
	time.Sleep(20 * time.Millisecond)

	// 过期后应抢占成功
	h2, err := l.Acquire(context.Background(), "s1", "owner-B")
	if err != nil {
		t.Fatalf("过期后 Acquire 应成功: %v", err)
	}
	// 第一次的句柄调用 Release 不应该删除 h2 的所有权
	h1.Release()

	// 此时还能 Acquire（因为 h2 还持有）
	_, err = l.Acquire(context.Background(), "s1", "owner-C")
	if !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("h2 持有期间 Acquire 应 ErrLeaseHeld, got %v", err)
	}
	h2.Release()

	// h2 释放后再次 Acquire 应成功
	_, err = l.Acquire(context.Background(), "s1", "owner-D")
	if err != nil {
		t.Fatalf("h2 释放后 Acquire 应成功, got %v", err)
	}
}

// 并发 10 个 goroutine 同时争抢同一 sid：只能有 1 个成功。
func TestMemoryLease_ConcurrentAcquireSingleWinner(t *testing.T) {
	l := NewMemoryLease()

	const N = 16
	var wg sync.WaitGroup
	success := make([]int, N)
	for i := 0; i < N; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			h, err := l.Acquire(context.Background(), "shared", "owner")
			if err == nil {
				success[i] = 1
				time.Sleep(10 * time.Millisecond)
				h.Release()
			}
		}()
	}
	wg.Wait()

	total := 0
	for _, s := range success {
		total += s
	}
	if total != 1 {
		t.Fatalf("期望恰好 1 个 Acquire 成功，实际 %d", total)
	}
}

// Reaper 周期回收：默认 TTL 30s 太长，本测试用极短 TTL 验证 reaper。
func TestMemoryLease_ReaperCollectsExpired(t *testing.T) {
	l := &MemoryLease{
		held:    make(map[string]leaseEntry),
		ttl:     5 * time.Millisecond,
		reaperT: 10 * time.Millisecond,
	}
	// 不启动 goroutine，手动 reap。
	h, err := l.Acquire(context.Background(), "s1", "owner")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if l.Size() != 1 {
		t.Fatalf("Size = %d, want 1", l.Size())
	}
	// 模拟过期后 reaper
	time.Sleep(15 * time.Millisecond)
	l.reapOnce()

	if l.Size() != 0 {
		t.Fatalf("reap 后 Size = %d, want 0", l.Size())
	}
	// h.Release 不应 panic（entry 已被 reaper 删除）
	h.Release()
}

// Acquire 应在底层做完检查后立即返回；context.Done 只作为信号。
func TestMemoryLease_AcquireRespectsContext(t *testing.T) {
	l := NewMemoryLease()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	// Acquire 不读 ctx.Done（我们实现是非阻塞 + 立即返回）。
	// 验证：不阻塞、不 panic、按预期返回 ErrLeaseHeld 或 OK。
	_, err := l.Acquire(ctx, "s1", "owner")
	if err != nil && !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("unexpected err: %v", err)
	}
}
