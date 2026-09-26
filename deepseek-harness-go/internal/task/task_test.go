package task

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"deepseek-harness-go/internal/agent"
	"deepseek-harness-go/internal/llm"
)

// fakeRunner 是测试用 Runner：
//   - 收到 RunStream 时立即发出一个 AssistantMessage 与 LoopDone，
//     然后关闭 evCh 并在 resCh 给出 RunResult。
//   - prompt 含 "FAIL" 时返回 StopReason="error"。
//   - prompt 含 "SLEEP" 时延迟 50ms 后再返回；可用于测试 cancel。
type fakeRunner struct {
	calls    int
	lastSID  string
	lastPmt  string
}

func (f *fakeRunner) RunStream(ctx context.Context, prompt string, sid string) (<-chan agent.Event, <-chan agent.RunResult) {
	f.calls++
	f.lastSID = sid
	f.lastPmt = prompt
	evCh := make(chan agent.Event, 8)
	resCh := make(chan agent.RunResult, 1)

	go func() {
		defer close(evCh)
		// 模拟一个"立即产生助手消息"的最小循环。
		evCh <- agent.AssistantMessage{Content: "ok"}
		evCh <- agent.LoopDone{Rounds: 1}
	}()

	if strings.Contains(prompt, "SLEEP") {
		go func() {
			select {
			case <-ctx.Done():
				resCh <- agent.RunResult{StopReason: "canceled", Error: ctx.Err()}
				return
			case <-time.After(80 * time.Millisecond):
				// 模拟完成的"等待"。
			}
			resCh <- agent.RunResult{
				StopReason: "no_tool_calls",
				Usage:      llm.Usage{},
			}
		}()
	} else if strings.Contains(prompt, "FAIL") {
		resCh <- agent.RunResult{StopReason: "error", Error: errFake("runner failed")}
	} else {
		resCh <- agent.RunResult{
			StopReason: "no_tool_calls",
			Usage:      llm.Usage{PromptTokens: 11, CompletionTokens: 7, TotalTokens: 18},
		}
	}
	return evCh, resCh
}

type errFake string

func (e errFake) Error() string { return string(e) }

func TestMemoryStore_InsertGetList(t *testing.T) {
	st := NewMemoryStore()
	ctx := context.Background()

	now := time.Now()
	task1 := &Task{ID: "a", Title: "first", State: StatePending, CreatedAt: now, UpdatedAt: now}
	task2 := &Task{ID: "b", Title: "second", State: StateCompleted, CreatedAt: now, UpdatedAt: now.Add(time.Second)}

	if err := st.Insert(ctx, task1); err != nil {
		t.Fatalf("insert a: %v", err)
	}
	if err := st.Insert(ctx, task2); err != nil {
		t.Fatalf("insert b: %v", err)
	}

	got, err := st.Get(ctx, "a")
	if err != nil {
		t.Fatalf("get a: %v", err)
	}
	if got.Title != "first" {
		t.Errorf("title = %q, want first", got.Title)
	}

	// 不存在 → ErrNotFound
	if _, err := st.Get(ctx, "missing"); err != ErrNotFound {
		t.Errorf("missing → %v, want ErrNotFound", err)
	}

	// 列表按 UpdatedAt 降序：b 在前
	list, err := st.List(ctx, Filter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 || list[0].ID != "b" || list[1].ID != "a" {
		t.Errorf("list order = %+v, want [b a]", list)
	}

	// 按状态过滤
	list, err = st.List(ctx, Filter{States: []State{StatePending}})
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(list) != 1 || list[0].ID != "a" {
		t.Errorf("pending list = %+v, want [a]", list)
	}
}

func TestState_StringJSON(t *testing.T) {
	for _, s := range []State{StatePending, StateRunning, StateCompleted, StateFailed, StateCanceled} {
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("marshal %v: %v", s, err)
		}
		var back State
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatalf("unmarshal %v: %v", b, err)
		}
		if back != s {
			t.Errorf("roundtrip %v → %s → %v", s, b, back)
		}
	}
}

func TestLoopExecutor_SubmitCompletes(t *testing.T) {
	store := NewMemoryStore()
	fake := &fakeRunner{}
	exec := NewLoopExecutor(store, fake)
	defer exec.Close(context.Background())

	ctx := context.Background()
	submitted, err := exec.Submit(ctx, SubmitRequest{
		Title:  "test",
		Input:  "hello",
		Owner:  "alice",
		Code:   "t-001",
		Profile: "headless",
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if submitted.State != StatePending {
		t.Errorf("submitted.State = %v, want Pending (同步快照)", submitted.State)
	}

	// 等待完成
	deadline := time.Now().Add(500 * time.Millisecond)
	var final *Task
	for time.Now().Before(deadline) {
		got, err := exec.Get(ctx, submitted.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.State == StateCompleted {
			final = got
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if final == nil {
		t.Fatal("task never reached Completed")
	}
	if final.Usage.PromptTokens != 11 || final.Usage.CompletionTokens != 7 || final.Usage.TotalTokens != 18 {
		t.Errorf("usage = %+v, want 11/7/18", final.Usage)
	}
	if fake.lastSID != "" && fake.lastSID != submitted.SessionID {
		t.Errorf("fake lastSID = %q", fake.lastSID)
	}
	if fake.lastPmt != "hello" {
		t.Errorf("fake lastPmt = %q", fake.lastPmt)
	}
}

func TestLoopExecutor_SubmitFails(t *testing.T) {
	store := NewMemoryStore()
	fake := &fakeRunner{}
	exec := NewLoopExecutor(store, fake)
	defer exec.Close(context.Background())

	submitted, err := exec.Submit(context.Background(), SubmitRequest{
		Title: "fail", Input: "please FAIL this",
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		got, _ := exec.Get(context.Background(), submitted.ID)
		if got.State == StateFailed {
			if !strings.Contains(got.Error, "runner failed") {
				t.Errorf("Error = %q, want 'runner failed'", got.Error)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("task did not reach Failed")
}

func TestLoopExecutor_Cancel(t *testing.T) {
	store := NewMemoryStore()
	fake := &fakeRunner{}
	exec := NewLoopExecutor(store, fake)
	defer exec.Close(context.Background())

	submitted, err := exec.Submit(context.Background(), SubmitRequest{
		Title: "slow", Input: "SLEEP please",
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	// 切到 Running 后再取消
	time.Sleep(10 * time.Millisecond)
	if err := exec.Cancel(context.Background(), submitted.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		got, _ := exec.Get(context.Background(), submitted.ID)
		if got.State == StateCanceled {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	got, _ := exec.Get(context.Background(), submitted.ID)
	t.Fatalf("task did not reach Canceled; state=%v err=%q", got.State, got.Error)
}

func TestLoopExecutor_CancelAlreadyTerminal(t *testing.T) {
	store := NewMemoryStore()
	fake := &fakeRunner{}
	exec := NewLoopExecutor(store, fake)
	defer exec.Close(context.Background())

	submitted, _ := exec.Submit(context.Background(), SubmitRequest{Input: "ok"})

	// 等到完成
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		got, _ := exec.Get(context.Background(), submitted.ID)
		if got.State == StateCompleted {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if err := exec.Cancel(context.Background(), submitted.ID); err != ErrAlreadyTerminal {
		t.Errorf("cancel completed → %v, want ErrAlreadyTerminal", err)
	}
}

func TestLoopExecutor_CancelNotFound(t *testing.T) {
	store := NewMemoryStore()
	exec := NewLoopExecutor(store, &fakeRunner{})
	defer exec.Close(context.Background())

	if err := exec.Cancel(context.Background(), "nope"); err != ErrNotFound {
		t.Errorf("cancel missing → %v, want ErrNotFound", err)
	}
}

func TestLoopExecutor_ListFilter(t *testing.T) {
	store := NewMemoryStore()
	fake := &fakeRunner{}
	exec := NewLoopExecutor(store, fake)
	defer exec.Close(context.Background())

	// 提交 3 个不同 owner
	for _, owner := range []string{"alice", "bob", "bob"} {
		_, err := exec.Submit(context.Background(), SubmitRequest{Input: "ok", Owner: owner})
		if err != nil {
			t.Fatalf("submit: %v", err)
		}
	}

	time.Sleep(100 * time.Millisecond)

	list, err := exec.List(context.Background(), Filter{Owner: "bob"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("bob list len = %d, want 2", len(list))
	}
	for _, tk := range list {
		if tk.Owner != "bob" {
			t.Errorf("filter leaked: %+v", tk)
		}
	}
}

func TestSQLiteStore_BasicRoundTrip(t *testing.T) {
	dbPath := t.TempDir() + "/tasks.db"
	st, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	now := time.Now().Truncate(time.Millisecond)
	tk := &Task{
		ID:        "id-1",
		Code:      "code-x",
		Title:     "title",
		Input:     "hello",
		State:     StatePending,
		Profile:   "headless",
		Owner:     "alice",
		CreatedAt: now,
		UpdatedAt: now,
		Usage:     Usage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7},
	}
	if err := st.Insert(ctx, tk); err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := st.Get(ctx, "id-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != tk.Title || got.Code != tk.Code || got.State != StatePending {
		t.Errorf("roundtrip mismatch:\n got=%+v\n want=%+v", got, tk)
	}
	if !reflect.DeepEqual(got.Usage, tk.Usage) {
		t.Errorf("usage: got=%+v want=%+v", got.Usage, tk.Usage)
	}

	// Update state
	got.State = StateRunning
	got.UpdatedAt = now.Add(time.Second)
	if err := st.Update(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	back, _ := st.Get(ctx, "id-1")
	if back.State != StateRunning {
		t.Errorf("after update state = %v", back.State)
	}

	// List
	all, err := st.List(ctx, Filter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("list len = %d, want 1", len(all))
	}

	// Update 不存在 → ErrNotFound
	miss := &Task{ID: "nope", Title: "x"}
	if err := st.Update(ctx, miss); err != ErrNotFound {
		t.Errorf("update missing → %v, want ErrNotFound", err)
	}
}
