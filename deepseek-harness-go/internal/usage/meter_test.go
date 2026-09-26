package usage

import (
	"testing"
	"time"
)

// MemoryMeter 基础：Account 后 Get 返回累计值。
func TestMemoryMeter_AccountAndGet(t *testing.T) {
	m := NewMemoryMeter()
	m.Account("s1", AccountRecord{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		CacheReadTokens:  30,
		Model:            "gpt-4o",
		At:               time.Now(),
	})
	m.Account("s1", AccountRecord{
		PromptTokens:     200,
		CompletionTokens: 100,
		TotalTokens:      300,
		CacheWriteTokens: 20,
		ReasoningTokens:  10,
		Model:            "gpt-4o",
		At:               time.Now(),
	})

	got := m.Get("s1")
	want := Metrics{
		Sid:              "s1",
		PromptTokens:     300,
		CompletionTokens: 150,
		TotalTokens:      450,
		CacheReadTokens:  30,
		CacheWriteTokens: 20,
		ReasoningTokens:  10,
		Model:            "gpt-4o",
		Rounds:           2,
	}
	if got != want && (got.Sid == want.Sid &&
		got.PromptTokens == want.PromptTokens &&
		got.CompletionTokens == want.CompletionTokens &&
		got.TotalTokens == want.TotalTokens &&
		got.CacheReadTokens == want.CacheReadTokens &&
		got.CacheWriteTokens == want.CacheWriteTokens &&
		got.ReasoningTokens == want.ReasoningTokens &&
		got.Model == want.Model &&
		got.Rounds == want.Rounds) {
		// 通过
	} else if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// 不存在的 sid 返回零值 Metrics 且 UpdatedAt 是零。
func TestMemoryMeter_UnknownSIDReturnsZero(t *testing.T) {
	m := NewMemoryMeter()
	got := m.Get("does-not-exist")
	if got.Sid != "does-not-exist" {
		t.Fatalf("Sid = %q, want does-not-exist", got.Sid)
	}
	if got.TotalTokens != 0 || got.Rounds != 0 {
		t.Fatalf("未存在 sid 应为零值: %+v", got)
	}
}

// sid == "" 静默 no-op；Get 仍返回零值 Metrics。
func TestMemoryMeter_EmptySIDIsNoop(t *testing.T) {
	m := NewMemoryMeter()
	m.Account("", AccountRecord{PromptTokens: 999})
	if m.Get("").TotalTokens != 0 {
		t.Fatalf("空 sid 不应记录")
	}
	if got := len(m.Snapshot()); got != 0 {
		t.Fatalf("snapshot 应为空，got %d", got)
	}
}

// 跨 session 互不串扰。
func TestMemoryMeter_CrossSessionIsolation(t *testing.T) {
	m := NewMemoryMeter()
	m.Account("alice", AccountRecord{PromptTokens: 100})
	m.Account("bob", AccountRecord{PromptTokens: 200})

	if got := m.Get("alice").PromptTokens; got != 100 {
		t.Fatalf("alice PromptTokens = %d, want 100", got)
	}
	if got := m.Get("bob").PromptTokens; got != 200 {
		t.Fatalf("bob PromptTokens = %d, want 200", got)
	}
}

// Reset 清空指定 sid；不影响其他。
func TestMemoryMeter_ResetOnlyTarget(t *testing.T) {
	m := NewMemoryMeter()
	m.Account("alice", AccountRecord{PromptTokens: 100})
	m.Account("bob", AccountRecord{PromptTokens: 200})

	m.Reset("alice")

	if got := m.Get("alice").PromptTokens; got != 0 {
		t.Fatalf("alice 应清空, got %d", got)
	}
	if got := m.Get("bob").PromptTokens; got != 200 {
		t.Fatalf("bob 应保留, got %d", got)
	}
}

// Snapshot 返回全部 sid 的副本。
func TestMemoryMeter_SnapshotReturnsAll(t *testing.T) {
	m := NewMemoryMeter()
	m.Account("alpha", AccountRecord{PromptTokens: 1})
	m.Account("beta", AccountRecord{PromptTokens: 2})
	m.Account("gamma", AccountRecord{PromptTokens: 3})

	all := m.Snapshot()
	if len(all) != 3 {
		t.Fatalf("snapshot len = %d, want 3", len(all))
	}
	// 验证总和
	total := 0
	for _, m := range all {
		total += m.PromptTokens
	}
	if total != 6 {
		t.Fatalf("snapshot total = %d, want 6", total)
	}
}

// 并发 Account：sync.RWMutex 保证串行化。
func TestMemoryMeter_ConcurrentAccount(t *testing.T) {
	m := NewMemoryMeter()
	const N = 32
	const each = 10
	done := make(chan struct{}, N)
	for i := 0; i < N; i++ {
		go func() {
			for j := 0; j < each; j++ {
				m.Account("hot", AccountRecord{PromptTokens: 1})
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < N; i++ {
		<-done
	}
	got := m.Get("hot").PromptTokens
	if want := N * each; got != want {
		t.Fatalf("并发 Account 合计 = %d, want %d", got, want)
	}
	if got := m.Get("hot").Rounds; got != N*each {
		t.Fatalf("Rounds = %d, want %d", got, N*each)
	}
}

// NoopMeter 是零开销且不 panic。
func TestNoopMeter(t *testing.T) {
	var m Meter = NoopMeter{}
	m.Account("s", AccountRecord{PromptTokens: 1})
	m.Reset("s")
	got := m.Get("s")
	if got.Sid != "s" || got.TotalTokens != 0 {
		t.Fatalf("NoopMeter Get 异常: %+v", got)
	}
	if all := m.Snapshot(); all != nil {
		t.Fatalf("NoopMeter.Snapshot = %+v, want nil", all)
	}
}
