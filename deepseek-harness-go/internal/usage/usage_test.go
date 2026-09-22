package usage

import (
	"sync"
	"sync/atomic"
	"testing"

	"deepseek-harness-go/internal/llm"
)

// 1. Add 正确累积 token 计数。
func TestTracker_AddAccumulates(t *testing.T) {
	tr := NewTracker(nil, "USD")
	tr.Add(llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15})
	tr.Add(llm.Usage{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5})
	s := tr.Snapshot()
	if s.Prompt != 13 {
		t.Errorf("Prompt = %d, want 13", s.Prompt)
	}
	if s.Completion != 7 {
		t.Errorf("Completion = %d, want 7", s.Completion)
	}
	if s.Total != 20 {
		t.Errorf("Total = %d, want 20", s.Total)
	}
}

// 2. ComputeCost 命中价格表。
func TestComputeCost_Hit(t *testing.T) {
	prices := PriceMap{
		"deepseek-chat":     0.00014,
		"deepseek-reasoner": 0.00055,
	}
	delta := llm.Usage{CompletionTokens: 1000}
	c := ComputeCost(delta, "deepseek-chat", prices)
	if c != 0.00014 {
		t.Errorf("cost = %v, want 0.00014", c)
	}
	delta2 := llm.Usage{CompletionTokens: 2000}
	c = ComputeCost(delta2, "deepseek-reasoner", prices)
	if c != 0.00110 {
		t.Errorf("cost = %v, want 0.00110", c)
	}
}

// 3. ComputeCost 未命中 → 0（不 panic）。
func TestComputeCost_Miss(t *testing.T) {
	prices := PriceMap{"deepseek-chat": 0.00014}
	c := ComputeCost(llm.Usage{CompletionTokens: 1000}, "unknown-model", prices)
	if c != 0 {
		t.Errorf("cost = %v, want 0", c)
	}
	// 价格表为 nil：仍返回 0，不 panic。
	c = ComputeCost(llm.Usage{CompletionTokens: 1000}, "x", nil)
	if c != 0 {
		t.Errorf("cost = %v, want 0", c)
	}
}

// 4. Reset 清空状态。
func TestTracker_Reset(t *testing.T) {
	tr := NewTracker(nil, "USD")
	tr.Add(llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15})
	tr.Reset()
	s := tr.Snapshot()
	if s.Prompt != 0 || s.Completion != 0 || s.Total != 0 {
		t.Errorf("after Reset: %+v, want all zero", s)
	}
}

// 5. Format 渲染出预期的底栏行。
func TestFormat(t *testing.T) {
	s := Snapshot{Prompt: 42, Completion: 17, Total: 59, Cost: 0.0012, Currency: "USD"}
	got := Format(s)
	want := "[usage] prompt=42 completion=17 total=59 cost=$0.0012"
	if got != want {
		t.Errorf("Format = %q, want %q", got, want)
	}
	// currency 为空 → cost=N/A
	s = Snapshot{Prompt: 1, Completion: 1, Total: 2, Currency: ""}
	got = Format(s)
	want = "[usage] prompt=1 completion=1 total=2 cost=N/A"
	if got != want {
		t.Errorf("Format = %q, want %q", got, want)
	}
}

// 6. 并发 Add：race 检测器无告警；最终总量等于累加和。
func TestTracker_ConcurrentAdd(t *testing.T) {
	tr := NewTracker(nil, "USD")
	const goroutines = 16
	const perG = 100
	var wg sync.WaitGroup
	var totalAdds int64
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				tr.Add(llm.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2})
				atomic.AddInt64(&totalAdds, 1)
			}
		}()
	}
	wg.Wait()
	s := tr.Snapshot()
	want := int64(goroutines * perG)
	if int64(s.Total) != want*2 || int64(s.Total) != atomic.LoadInt64(&totalAdds)*2 {
		t.Errorf("Total = %d, want %d", s.Total, want*2)
	}
	if int64(s.Prompt) != want {
		t.Errorf("Prompt = %d, want %d", s.Prompt, want)
	}
	if int64(s.Completion) != want {
		t.Errorf("Completion = %d, want %d", s.Completion, want)
	}
}
