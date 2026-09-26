package workflow

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"deepseek-harness-go/internal/task"
)

// fakeTaskSubmitter 是测试用 TaskSubmitter：
//   - Submit 总是成功，返回一个 ID 形如 "fake-<n>"；
//   - Get 第一次返回 Running，第二次返回 Completed（计数实现）。
//   - prompt 含 "FAIL" 时 Get 第二次返回 Failed。
type fakeTaskSubmitter struct {
	mu        sync.Mutex
	next      int
	getCount  map[string]int
	completed map[string]bool
	failed    map[string]bool
	calls     int32
	wait      bool // if false, Submit 不要求后续 wait
}

func newFakeTaskSubmitter() *fakeTaskSubmitter {
	return &fakeTaskSubmitter{
		getCount:  make(map[string]int),
		completed: make(map[string]bool),
		failed:    make(map[string]bool),
		wait:      true,
	}
}

func (f *fakeTaskSubmitter) Submit(ctx context.Context, req task.SubmitRequest) (*task.Task, error) {
	atomic.AddInt32(&f.calls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	id := "fake-" + req.Input // 用 input 当作 ID，便于测试断言
	if strings.Contains(req.Input, "FAIL") {
		f.failed[id] = true
	}
	if !f.wait {
		f.completed[id] = true
	}
	return &task.Task{
		ID:        id,
		Title:     req.Title,
		Input:     req.Input,
		State:     task.StateRunning,
		Profile:   req.Profile,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Usage:     task.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
	}, nil
}

func (f *fakeTaskSubmitter) Get(ctx context.Context, id string) (*task.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCount[id]++
	if f.completed[id] {
		return &task.Task{ID: id, State: task.StateCompleted, Usage: task.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3}}, nil
	}
	if f.failed[id] {
		return &task.Task{ID: id, State: task.StateFailed, Error: "fail", Usage: task.Usage{}}, nil
	}
	if f.getCount[id] >= 2 {
		f.completed[id] = true
		return &task.Task{ID: id, State: task.StateCompleted, Usage: task.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3}}, nil
	}
	return &task.Task{ID: id, State: task.StateRunning, Usage: task.Usage{}}, nil
}

func waitRun(t *testing.T, st *RunState, timeout time.Duration, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("workflow run did not satisfy predicate within %s; stop=%s err=%s states=%v",
		timeout, st.StopReason, st.Error, st.NodeStates)
}

func TestValidate_DAG(t *testing.T) {
	w := &Workflow{
		Code:  "wf1",
		Entry: "a",
		Nodes: map[string]*Node{
			"a": {ID: "a", Type: NodeFunction, Function: &FunctionConfig{Name: "echo"}, Next: []Edge{{To: "b"}}},
			"b": {ID: "b", Type: NodeFunction, Function: &FunctionConfig{Name: "echo"}},
		},
	}
	if err := w.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestValidate_Cycle(t *testing.T) {
	w := &Workflow{
		Code:  "wf-cycle",
		Entry: "a",
		Nodes: map[string]*Node{
			"a": {ID: "a", Type: NodeFunction, Function: &FunctionConfig{Name: "echo"}, Next: []Edge{{To: "b"}}},
			"b": {ID: "b", Type: NodeFunction, Function: &FunctionConfig{Name: "echo"}, Next: []Edge{{To: "a"}}},
		},
	}
	if err := w.Validate(); err != ErrCycle {
		t.Errorf("validate cycle → %v, want ErrCycle", err)
	}
}

func TestValidate_EdgeMissingTarget(t *testing.T) {
	w := &Workflow{
		Code:  "wf-bad",
		Entry: "a",
		Nodes: map[string]*Node{
			"a": {ID: "a", Type: NodeFunction, Function: &FunctionConfig{Name: "echo"}, Next: []Edge{{To: "ghost"}}},
		},
	}
	if err := w.Validate(); err == nil || !strings.Contains(err.Error(), "target not found") {
		t.Errorf("validate → %v, want target-not-found", err)
	}
}

func TestCondition_Eval(t *testing.T) {
	c := NewConditionEvaluator()
	vars := Vars{"x": "ok"}
	cases := []struct {
		expr string
		want bool
		err  bool
	}{
		{"", true, false},
		{"true", true, false},
		{"false", false, false},
		{"vars.x == ok", true, false},
		{"vars.x != ok", false, false},
		{`vars.x contains "k"`, true, false},
		{`vars.x == "nope"`, false, false},
		{"vars.missing == ok", false, false}, // missing → nil == "ok" → false
	}
	for _, c2 := range cases {
		got, err := c.Eval(c2.expr, vars)
		if c2.err && err == nil {
			t.Errorf("%q: expected err", c2.expr)
			continue
		}
		if !c2.err && err != nil {
			t.Errorf("%q: unexpected err: %v", c2.expr, err)
			continue
		}
		if got != c2.want {
			t.Errorf("%q = %v, want %v", c2.expr, got, c2.want)
		}
	}
}

func TestRenderTemplate(t *testing.T) {
	vars := Vars{"name": "alice", "n": 7}
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"hello", "hello"},
		{"hello {{.vars.name}}", "hello alice"},
		{"x={{.vars.n}}", "x=7"},
		{"missing={{.vars.unk}}", "missing={{.vars.unk}}"},
	}
	for _, c := range cases {
		if got := renderTemplate(c.in, vars); got != c.want {
			t.Errorf("render(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRunner_LinearDAG(t *testing.T) {
	tasks := newFakeTaskSubmitter()
	fns := NewFunctionRegistry()
	fns.Register("noop", func(ctx context.Context, vars Vars, args map[string]any) (any, error) { return nil, nil })
	r := NewRunner(tasks, fns, DefaultRunnerConfig{})

	w := &Workflow{
		Code:  "linear",
		Entry: "a",
		Nodes: map[string]*Node{
			"a": {ID: "a", Type: NodeTask, Task: &TaskNodeConfig{Input: "step1"}, Next: []Edge{{To: "b"}}},
			"b": {ID: "b", Type: NodeFunction, Function: &FunctionConfig{Name: "noop"}},
		},
	}
	st, err := r.Start(context.Background(), w, nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitRun(t, st, 2*time.Second, func() bool { return st.StopReason == "completed" })
	if st.NodeStates["a"] != "done" || st.NodeStates["b"] != "done" {
		t.Errorf("states = %+v", st.NodeStates)
	}
	if atomic.LoadInt32(&tasks.calls) != 1 {
		t.Errorf("task submit calls = %d, want 1", tasks.calls)
	}
}

func TestRunner_FanOutJoin(t *testing.T) {
	tasks := newFakeTaskSubmitter()
	fns := NewFunctionRegistry()
	fns.Register("noop", func(ctx context.Context, vars Vars, args map[string]any) (any, error) { return nil, nil })
	r := NewRunner(tasks, fns, DefaultRunnerConfig{WorkerCount: 4})

	w := &Workflow{
		Code:  "fanout",
		Entry: "root",
		Nodes: map[string]*Node{
			"root": {ID: "root", Type: NodeFunction, Function: &FunctionConfig{Name: "noop"}, Next: []Edge{
				{To: "left"}, {To: "right"},
			}},
			"left":  {ID: "left", Type: NodeTask, Task: &TaskNodeConfig{Input: "left"}},
			"right": {ID: "right", Type: NodeTask, Task: &TaskNodeConfig{Input: "right"}},
		},
	}
	st, _ := r.Start(context.Background(), w, nil)
	waitRun(t, st, 2*time.Second, func() bool { return st.StopReason == "completed" })
	if st.NodeStates["left"] != "done" || st.NodeStates["right"] != "done" {
		t.Errorf("states = %+v", st.NodeStates)
	}
}

func TestRunner_Branch(t *testing.T) {
	tasks := newFakeTaskSubmitter()
	fns := NewFunctionRegistry()
	fns.Register("seed", func(ctx context.Context, vars Vars, args map[string]any) (any, error) {
		return "ok", nil
	})
	r := NewRunner(tasks, fns, DefaultRunnerConfig{})

	w := &Workflow{
		Code:  "branch",
		Entry: "seed",
		Nodes: map[string]*Node{
			"seed":    {ID: "seed", Type: NodeFunction, Function: &FunctionConfig{Name: "seed"}},
			"decide":  {ID: "decide", Type: NodeBranch, Next: []Edge{
				{To: "go_ok", Condition: "vars.seed.result == ok"},
				{To: "go_fail", Condition: "vars.seed.result == no"},
			}},
			"go_ok":   {ID: "go_ok", Type: NodeTask, Task: &TaskNodeConfig{Input: "ok_path", Wait: true}},
			"go_fail": {ID: "go_fail", Type: NodeTask, Task: &TaskNodeConfig{Input: "fail_path", Wait: true}},
		},
		// 手动连接 seed → decide（让 decide 知道 vars.seed.result）
	}
	w.Nodes["seed"].Next = []Edge{{To: "decide"}}
	st, _ := r.Start(context.Background(), w, nil)
	waitRun(t, st, 2*time.Second, func() bool { return st.StopReason == "completed" || st.StopReason == "failed" })
	if st.StopReason != "completed" {
		t.Logf("stop=%s err=%s states=%+v", st.StopReason, st.Error, st.NodeStates)
	}
	if st.NodeStates["go_ok"] != "done" {
		t.Errorf("go_ok = %v", st.NodeStates["go_ok"])
	}
	if st.NodeStates["go_fail"] != "skipped" {
		t.Errorf("go_fail = %v, want skipped", st.NodeStates["go_fail"])
	}
}

func TestRunner_TaskFailureFailFast(t *testing.T) {
	tasks := newFakeTaskSubmitter()
	fns := NewFunctionRegistry()
	fns.Register("noop", func(ctx context.Context, vars Vars, args map[string]any) (any, error) { return nil, nil })
	r := NewRunner(tasks, fns, DefaultRunnerConfig{})

	w := &Workflow{
		Code:  "ff",
		Entry: "a",
		Nodes: map[string]*Node{
			"a": {ID: "a", Type: NodeTask, Task: &TaskNodeConfig{Input: "FAIL this", Wait: true}},
		},
	}
	st, _ := r.Start(context.Background(), w, nil)
	waitRun(t, st, 2*time.Second, func() bool { return st.StopReason == "failed" })
	if st.NodeStates["a"] != "failed" {
		t.Errorf("a = %v, want failed", st.NodeStates["a"])
	}
}

func TestRunner_TaskFailurePartial(t *testing.T) {
	tasks := newFakeTaskSubmitter()
	fns := NewFunctionRegistry()
	fns.Register("noop", func(ctx context.Context, vars Vars, args map[string]any) (any, error) { return nil, nil })
	r := NewRunner(tasks, fns, DefaultRunnerConfig{WorkerCount: 4})

	w := &Workflow{
		Code:  "part",
		Entry: "root",
		Nodes: map[string]*Node{
			"root": {ID: "root", Type: NodeFunction, Function: &FunctionConfig{Name: "noop"}, Next: []Edge{
				{To: "bad"}, {To: "good"},
			}},
			"bad":  {ID: "bad", Type: NodeTask, OnError: OnErrorPartial, Task: &TaskNodeConfig{Input: "FAIL me", Wait: true}},
			"good": {ID: "good", Type: NodeTask, Task: &TaskNodeConfig{Input: "good", Wait: true}},
		},
	}
	st, _ := r.Start(context.Background(), w, nil)
	waitRun(t, st, 2*time.Second, func() bool { return st.StopReason == "failed" || st.StopReason == "completed" })
	if st.NodeStates["good"] != "done" {
		t.Errorf("good = %v, want done", st.NodeStates["good"])
	}
}

func TestRunner_InvalidFunction(t *testing.T) {
	tasks := newFakeTaskSubmitter()
	r := NewRunner(tasks, NewFunctionRegistry(), DefaultRunnerConfig{})

	w := &Workflow{
		Code:  "miss",
		Entry: "a",
		Nodes: map[string]*Node{
			"a": {ID: "a", Type: NodeFunction, Function: &FunctionConfig{Name: "ghost"}},
		},
	}
	st, _ := r.Start(context.Background(), w, nil)
	waitRun(t, st, 2*time.Second, func() bool { return st.StopReason == "failed" })
	if !strings.Contains(st.Error, "ghost") {
		t.Errorf("err = %q, want mention ghost", st.Error)
	}
}

func TestRunner_FunctionError(t *testing.T) {
	tasks := newFakeTaskSubmitter()
	fns := NewFunctionRegistry()
	fns.Register("boom", func(ctx context.Context, vars Vars, args map[string]any) (any, error) {
		return nil, errors.New("kaboom")
	})
	r := NewRunner(tasks, fns, DefaultRunnerConfig{})

	w := &Workflow{
		Code:  "boom",
		Entry: "a",
		Nodes: map[string]*Node{
			"a": {ID: "a", Type: NodeFunction, Function: &FunctionConfig{Name: "boom"}},
		},
	}
	st, _ := r.Start(context.Background(), w, nil)
	waitRun(t, st, 2*time.Second, func() bool { return st.StopReason == "failed" })
	if !strings.Contains(st.Error, "kaboom") {
		t.Errorf("err = %q", st.Error)
	}
}
