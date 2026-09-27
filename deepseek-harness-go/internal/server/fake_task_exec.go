package server

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"deepseek-harness-go/internal/task"
)

// fakeTaskExec 是 task.Executor 的内存版测试替身。
//
// 行为：Submit 立即将 state 切到 running，分配一个 ID；不真正运行 loop，
// 由测试按需修改状态机或调用 Cancel。
type fakeTaskExec struct {
	mu    sync.Mutex
	next  int64
	tasks map[string]*task.Task
}

func (f *fakeTaskExec) Submit(_ context.Context, req task.SubmitRequest) (*task.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tasks == nil {
		f.tasks = make(map[string]*task.Task)
	}
	id := fmt.Sprintf("fake-%d", atomic.AddInt64(&f.next, 1))
	t := &task.Task{
		ID:    id,
		Code:  req.Code,
		Title: req.Title,
		Input: req.Input,
		State: task.StateRunning,
	}
	f.tasks[id] = t
	return t, nil
}

func (f *fakeTaskExec) Get(_ context.Context, id string) (*task.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[id]
	if !ok {
		return nil, task.ErrNotFound
	}
	cp := *t
	return &cp, nil
}

func (f *fakeTaskExec) Cancel(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[id]
	if !ok {
		return task.ErrNotFound
	}
	if t.State == task.StateCompleted || t.State == task.StateFailed || t.State == task.StateCanceled {
		return task.ErrAlreadyTerminal
	}
	t.State = task.StateCanceled
	return nil
}

func (f *fakeTaskExec) List(_ context.Context, filter task.Filter) ([]*task.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*task.Task, 0, len(f.tasks))
	for _, t := range f.tasks {
		out = append(out, t)
	}
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}
