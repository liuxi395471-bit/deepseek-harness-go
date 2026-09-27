package task

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"deepseek-harness-go/internal/agent"
)

// Runner 是 Executor 执行 task 时依赖的运行契约。
//
// 通常传入 agent.LoopRunner；为测试可传入 fake。Executor 只关心
// RunStream 的事件通道与结果通道，不关心实现细节。
type Runner interface {
	RunStream(ctx context.Context, prompt string, sid string) (<-chan agent.Event, <-chan agent.RunResult)
}

// LoopRunner 是 Executor 实际依赖的 LoopRunner 子集；引入此接口以
// 避免 Executor 与 agent 包的具体实现耦合。
//
// 说明：Executor 接受 agent.StreamingRunner 接口实现（更宽松），
// Runner 仅为类型提示。实际签名等价。
type LoopRunner = agent.StreamingRunner

// LoopExecutor 是 Executor 的 SQLiteStore-backed 默认实现。
//
// 生命周期：构造后即可用；Close 释放底层 db 与活动 goroutine。
//
// 并发安全：所有方法都可并发调用。每个 task 在自己的 goroutine 中
// 运行；Cancel 通过 ctx 取消实现。
type LoopExecutor struct {
	store  Store
	runner Runner

	mu      sync.Mutex
	running map[string]*runHandle // task_id → 当前活动句柄
	wg      sync.WaitGroup
	closed  bool
}

type runHandle struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// NewLoopExecutor 构造执行器；store 必须非 nil。
func NewLoopExecutor(store Store, runner Runner) *LoopExecutor {
	return &LoopExecutor{
		store:   store,
		runner:  runner,
		running: make(map[string]*runHandle),
	}
}

// Submit 创建并启动一个 Task；立即返回（不等待 Task 完成）。
//
// 流程：
//  1. 分配 ID + Pending 状态入库；
//  2. 启动 goroutine：切到 Running、调用 Runner.RunStream、等待完成；
//  3. 根据 StopReason 切到 Completed / Failed / Canceled，更新 Usage。
//
// 如果 store.Insert 失败，返回错误，task 不会被创建。
func (e *LoopExecutor) Submit(ctx context.Context, req SubmitRequest) (*Task, error) {
	if req.Input == "" {
		return nil, errors.New("task: submit: empty input")
	}
	if req.Title == "" {
		// 给个默认值；调用方通常会传 title。
		req.Title = truncate(req.Input, 40)
	}
	if req.Profile == "" {
		req.Profile = "headless"
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	t := &Task{
		ID:        id,
		Code:      req.Code,
		Title:     req.Title,
		Input:     req.Input,
		SessionID: req.SessionID,
		State:     StatePending,
		Profile:   req.Profile,
		Permission: req.Permission,
		Owner:     req.Owner,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := e.store.Insert(ctx, t); err != nil {
		return nil, err
	}
	e.spawn(t)
	return e.store.Get(ctx, id)
}

// spawn 在新 goroutine 中执行 t。ctx 是 Executor 内部派生（受 Cancel 控制）。
func (e *LoopExecutor) spawn(t *Task) {
	runCtx, cancel := context.WithCancel(context.Background())
	handle := &runHandle{cancel: cancel, done: make(chan struct{})}
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		cancel()
		return
	}
	e.running[t.ID] = handle
	e.wg.Add(1)
	e.mu.Unlock()

	go func() {
		defer e.wg.Done()
		defer close(handle.done)
		e.execute(runCtx, t)
		e.mu.Lock()
		delete(e.running, t.ID)
		e.mu.Unlock()
	}()
}

// execute 在 Task 自己的 goroutine 内执行；通过 runCtx 感知取消。
func (e *LoopExecutor) execute(ctx context.Context, t *Task) {
	// 切到 Running
	t.State = StateRunning
	t.UpdatedAt = time.Now()
	start := t.UpdatedAt
	t.StartedAt = &start
	if err := e.store.Update(ctx, t); err != nil {
		// 状态写不回：记 Error 并退出（不让 task 永远卡 Running）。
		t.State = StateFailed
		t.Error = "update running: " + err.Error()
		end := time.Now()
		t.FinishedAt = &end
		t.UpdatedAt = end
		_ = e.store.Update(ctx, t)
		return
	}

	evCh, resCh := e.runner.RunStream(ctx, t.Input, t.SessionID)

	// 后台排空事件通道（不消费，只是防止 Runner 写阻塞）。
	go func() {
		for range evCh {
		}
	}()

	var res agent.RunResult
	select {
	case <-ctx.Done():
		// 取消：尝试 cancel runner 的 ctx；等待 done 后置 Canceled。
		res = agent.RunResult{StopReason: "canceled", Error: ctx.Err()}
	case res = <-resCh:
		// 正常完成或失败。
	}

	if ctx.Err() != nil {
		// 二次确认 — cancel 中途又出现。
		t.State = StateCanceled
		t.Error = ctx.Err().Error()
	} else {
		switch res.StopReason {
		case "error", "":
			t.State = StateFailed
			if res.Error != nil {
				t.Error = res.Error.Error()
			} else {
				t.Error = "runner: stop reason error"
			}
		case "canceled":
			t.State = StateCanceled
			if res.Error != nil {
				t.Error = res.Error.Error()
			}
		default:
			// no_tool_calls / max_rounds：都视为完成（max_rounds 算 partial）。
			t.State = StateCompleted
			if res.StopReason == "max_rounds" {
				t.Error = "max_rounds"
			}
		}
	}
	t.Usage = Usage{
		PromptTokens:     res.Usage.PromptTokens,
		CompletionTokens: res.Usage.CompletionTokens,
		TotalTokens:      res.Usage.TotalTokens,
	}
	end := time.Now()
	t.FinishedAt = &end
	t.UpdatedAt = end
	_ = e.store.Update(ctx, t)
}

// Cancel 中止 taskID；幂等。
//
// 语义：
//   - 找不到 → ErrNotFound；
//   - 已终止（Completed / Failed / Canceled） → ErrAlreadyTerminal；
//   - Pending / Running → 取消内部 ctx。
func (e *LoopExecutor) Cancel(ctx context.Context, taskID string) error {
	t, err := e.store.Get(ctx, taskID)
	if err != nil {
		return err
	}
	if t.State == StateCompleted || t.State == StateFailed || t.State == StateCanceled {
		return ErrAlreadyTerminal
	}
	e.mu.Lock()
	handle, ok := e.running[taskID]
	e.mu.Unlock()
	if !ok {
		// 不在 running map：可能是 Pending 排队（v6 暂无）或刚完成；
		// 标记 Canceled 即可。
		t.State = StateCanceled
		t.UpdatedAt = time.Now()
		end := t.UpdatedAt
		t.FinishedAt = &end
		return e.store.Update(ctx, t)
	}
	handle.cancel()
	// 不立即写 store；execute() 会负责更新。
	select {
	case <-handle.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// Get 返回任务快照。
func (e *LoopExecutor) Get(ctx context.Context, taskID string) (*Task, error) {
	return e.store.Get(ctx, taskID)
}

// Retry 重新提交一个已终止的任务；分配新 id、复用 Input/Profile 等。
//
// 语义：
//   - 找不到原始 task → ErrNotFound
//   - 原始 task 仍在 Running/Pending → ErrAlreadyRunning
//   - 已终止 → 新建 Task（Pending）+ spawn goroutine
//
// 副作用：通过 Retry 链回的新 task 自身可独立 Cancel/List。
func (e *LoopExecutor) Retry(ctx context.Context, taskID string) (*Task, error) {
	src, err := e.store.Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if src.State == StatePending || src.State == StateRunning {
		return nil, ErrAlreadyRunning
	}
	newID, err := newID()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	t := &Task{
		ID:         newID,
		Code:       src.Code,
		Title:      src.Title,
		Input:      src.Input,
		SessionID:  src.SessionID,
		State:      StatePending,
		Profile:    src.Profile,
		Permission: src.Permission,
		Owner:      src.Owner,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := e.store.Insert(ctx, t); err != nil {
		return nil, err
	}
	e.spawn(t)
	return e.store.Get(ctx, newID)
}

// List 返回按 UpdatedAt 降序的 Task 列表。
func (e *LoopExecutor) List(ctx context.Context, filter Filter) ([]*Task, error) {
	return e.store.List(ctx, filter)
}

// Close 等待所有活动 task 完成；超时通过 ctx 控制。
func (e *LoopExecutor) Close(ctx context.Context) error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	// 不取消活动 task；让其自然完成。调用方应先 Cancel 所有 task。
	e.mu.Unlock()

	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// newID 返回 16 字节随机 hex ID。
func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("task: id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// truncate 截断到 n 个 rune（用于默认 title）。
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		count++
		if count > n {
			return s[:i] + "…"
		}
	}
	return s
}
