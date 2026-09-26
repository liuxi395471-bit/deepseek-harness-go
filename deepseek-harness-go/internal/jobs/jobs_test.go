package jobs

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeRunner 是测试用 Runner：根据 prompt 返回不同结果。
type fakeRunner struct {
	calls int32
	delay time.Duration
	fail  bool
	out   []string
}

func (f *fakeRunner) Run(ctx context.Context, j *Job, onLine func(string)) (int, error) {
	atomic.AddInt32(&f.calls, 1)
	for _, l := range f.out {
		if onLine != nil {
			onLine(l)
		}
	}
	if f.delay > 0 {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(f.delay):
		}
	}
	if f.fail {
		return 1, errors.New("fake fail")
	}
	return 0, nil
}

func waitState(t *testing.T, r *Registry, id string, want State, timeout time.Duration) *Job {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		j, err := r.Get(context.Background(), id)
		if err == nil && j.State == want {
			return j
		}
		time.Sleep(5 * time.Millisecond)
	}
	j, _ := r.Get(context.Background(), id)
	t.Fatalf("job %s did not reach %s within %s; current state=%v err=%q",
		id, want, timeout, j.State, j.Error)
	return nil
}

func TestRegistry_SubmitCompletes(t *testing.T) {
	store := NewMemoryStore()
	runner := &fakeRunner{out: []string{"hello", "world"}}
	reg := NewRegistry(store, runner, 100)
	defer reg.Close(context.Background())

	j, err := reg.Submit(context.Background(), SubmitRequest{Cmd: "echo", Args: []string{"hi"}})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if j.State != StatePending {
		t.Errorf("submitted state = %v, want Pending", j.State)
	}
	final := waitState(t, reg, j.ID, StateSucceeded, 1*time.Second)
	if final.ExitCode != 0 {
		t.Errorf("exit = %d, want 0", final.ExitCode)
	}
	lines, err := reg.Output(j.ID, 0, 0)
	if err != nil {
		t.Fatalf("output: %v", err)
	}
	if len(lines) != 2 || lines[0] != "hello" || lines[1] != "world" {
		t.Errorf("output = %v", lines)
	}
}

func TestRegistry_Fail(t *testing.T) {
	store := NewMemoryStore()
	runner := &fakeRunner{fail: true}
	reg := NewRegistry(store, runner, 100)
	defer reg.Close(context.Background())

	j, _ := reg.Submit(context.Background(), SubmitRequest{Cmd: "false"})
	final := waitState(t, reg, j.ID, StateFailed, 1*time.Second)
	if final.ExitCode != 1 {
		t.Errorf("exit = %d, want 1", final.ExitCode)
	}
}

func TestRegistry_Cancel(t *testing.T) {
	store := NewMemoryStore()
	runner := &fakeRunner{delay: 100 * time.Millisecond}
	reg := NewRegistry(store, runner, 100)
	defer reg.Close(context.Background())

	j, _ := reg.Submit(context.Background(), SubmitRequest{Cmd: "sleep", Args: []string{"1"}})
	time.Sleep(10 * time.Millisecond)
	if err := reg.Kill(context.Background(), j.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	final := waitState(t, reg, j.ID, StateCanceled, 1*time.Second)
	_ = final
}

func TestRegistry_KillAlreadyTerminal(t *testing.T) {
	store := NewMemoryStore()
	runner := &fakeRunner{}
	reg := NewRegistry(store, runner, 100)
	defer reg.Close(context.Background())

	j, _ := reg.Submit(context.Background(), SubmitRequest{Cmd: "true"})
	waitState(t, reg, j.ID, StateSucceeded, 1*time.Second)

	if err := reg.Kill(context.Background(), j.ID); err != ErrAlreadyTerminal {
		t.Errorf("kill terminal → %v, want ErrAlreadyTerminal", err)
	}
}

func TestRegistry_ListFilter(t *testing.T) {
	store := NewMemoryStore()
	runner := &fakeRunner{}
	reg := NewRegistry(store, runner, 100)
	defer reg.Close(context.Background())

	// 启动 3 个立即完成的
	for i := 0; i < 3; i++ {
		reg.Submit(context.Background(), SubmitRequest{Cmd: "ok", Owner: "alice"})
	}

	// 等到 3 个都完成
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		list, _ := reg.List(context.Background(), Filter{States: []State{StateSucceeded}})
		if len(list) == 3 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// 现在启动 1 个失败的
	runner.fail = true
	failJob, _ := reg.Submit(context.Background(), SubmitRequest{Cmd: "ko", Owner: "bob"})
	waitState(t, reg, failJob.ID, StateFailed, 1*time.Second)

	list, err := reg.List(context.Background(), Filter{States: []State{StateSucceeded}})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("succeeded list = %d, want 3", len(list))
	}

	list, err = reg.List(context.Background(), Filter{Owner: "bob"})
	if err != nil {
		t.Fatalf("list owner: %v", err)
	}
	if len(list) != 1 || list[0].Owner != "bob" {
		t.Errorf("bob list = %+v", list)
	}
}

func TestRegistry_NoRunner(t *testing.T) {
	reg := NewRegistry(NewMemoryStore(), nil, 100)
	_, err := reg.Submit(context.Background(), SubmitRequest{Cmd: "x"})
	if !errors.Is(err, ErrNoRunner) {
		t.Errorf("submit no runner → %v, want ErrNoRunner", err)
	}
}

func TestRegistry_EmptyCmd(t *testing.T) {
	reg := NewRegistry(NewMemoryStore(), &fakeRunner{}, 100)
	_, err := reg.Submit(context.Background(), SubmitRequest{})
	if err == nil || !strings.Contains(err.Error(), "empty cmd") {
		t.Errorf("submit empty → %v, want 'empty cmd'", err)
	}
}

func TestBuffer_AppendAndCap(t *testing.T) {
	b := NewBuffer(3)
	for i := 0; i < 5; i++ {
		b.Append(string(rune('a' + i)))
	}
	lines := b.Lines()
	// cap=3, drop=cap/4=0 → drop=1. After 5 appends with cap=3:
	// append 1: [a]
	// append 2: [a,b]
	// append 3: [a,b,c]
	// append 4: drop 1 → [b,c,d]
	// append 5: drop 1 → [c,d,e]
	if len(lines) != 3 {
		t.Errorf("lines len = %d, want 3 (cap)", len(lines))
	}
	if lines[0] != "c" || lines[2] != "e" {
		t.Errorf("lines = %v, want [c d e]", lines)
	}
}

func TestBuffer_LinesSince(t *testing.T) {
	b := NewBuffer(10)
	for i := 0; i < 5; i++ {
		b.Append(string(rune('a' + i)))
	}
	after := b.LinesSince(2)
	if len(after) != 3 || after[0] != "c" {
		t.Errorf("LinesSince(2) = %v", after)
	}
	// 越界
	if b.LinesSince(100) != nil {
		t.Errorf("LinesSince(100) should be nil")
	}
}
