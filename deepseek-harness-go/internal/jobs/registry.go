package jobs

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"crypto/rand"
	"encoding/hex"
)

// Buffer 是 Job 的输出行缓冲。
//
// 行为：
//   - Append 追加一行；
//   - Lines 返回当前全部行（按追加顺序）；
//   - LinesSince 返回 from 行号之后的全部行（from=0 表示从头）。
type Buffer struct {
	mu    sync.RWMutex
	lines []string
	cap   int
}

// NewBuffer 构造缓冲；cap<=0 表示 1000 行。
func NewBuffer(cap int) *Buffer {
	if cap <= 0 {
		cap = 1000
	}
	return &Buffer{cap: cap}
}

// Append 写入一行；超过容量时丢最早行（FIFO）。
func (b *Buffer) Append(line string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.lines) >= b.cap {
		// 去掉最早 1/4
		drop := b.cap / 4
		if drop < 1 {
			drop = 1
		}
		b.lines = b.lines[drop:]
	}
	b.lines = append(b.lines, line)
}

// Lines 返回当前所有行（拷贝）。
func (b *Buffer) Lines() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if len(b.lines) == 0 {
		return nil
	}
	out := make([]string, len(b.lines))
	copy(out, b.lines)
	return out
}

// LinesSince 返回 from 行号之后的行。
func (b *Buffer) LinesSince(from int) []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if from >= len(b.lines) {
		return nil
	}
	out := make([]string, len(b.lines)-from)
	copy(out, b.lines[from:])
	return out
}

// Size 返回当前行数。
func (b *Buffer) Size() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.lines)
}

// ShellRunner 用 os/exec 执行 Job 的 Cmd + Args。
//
// onLine 回调在每行 stdout 末尾触发（按 \n 切分）。stderr 也合并到
// stdout（便于简化）。
type ShellRunner struct{}

// NewShellRunner 构造 shell runner。
func NewShellRunner() *ShellRunner { return &ShellRunner{} }

// Run 执行 j.Cmd j.Args；返回 exit code 与 error。
func (r *ShellRunner) Run(ctx context.Context, j *Job, onLine func(string)) (int, error) {
	if j == nil || j.Cmd == "" {
		return 0, errors.New("jobs: empty cmd")
	}
	cmd := exec.CommandContext(ctx, j.Cmd, j.Args...)
	if j.Workdir != "" {
		cmd.Dir = j.Workdir
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, fmt.Errorf("jobs: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return 0, fmt.Errorf("jobs: stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("jobs: start: %w", err)
	}

	// 行扫描：合并 stdout + stderr 到 onLine 回调
	var wg sync.WaitGroup
	scan := func(r io.Reader) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 4096), 1<<20) // 1 MiB / line max
		for sc.Scan() {
			if onLine != nil {
				onLine(sc.Text())
			}
		}
	}
	wg.Add(2)
	go scan(stdout)
	go scan(stderr)
	wg.Wait()

	err = cmd.Wait()
	exit := 0
	if ee, ok := err.(*exec.ExitError); ok {
		exit = ee.ExitCode()
	} else if err != nil {
		return 0, err
	}
	return exit, nil
}

// Registry 是 Job 的运行时入口。
//
// 职责：接受 Submit、调度到 goroutine、跟踪状态、暴露 List / Get / Kill / Output。
type Registry struct {
	store   Store
	runner  Runner
	bufSize int

	mu      sync.Mutex
	running map[string]*runHandle
	outputs map[string]*Buffer
	wg      sync.WaitGroup
	closed  bool
}

type runHandle struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// NewRegistry 构造；store 与 runner 必填。
func NewRegistry(store Store, runner Runner, bufSize int) *Registry {
	if bufSize <= 0 {
		bufSize = 1000
	}
	return &Registry{
		store:   store,
		runner:  runner,
		bufSize: bufSize,
		running: make(map[string]*runHandle),
		outputs: make(map[string]*Buffer),
	}
}

// Submit 启动一个 Job；立即返回（不等待完成）。
//
// 错误：ID 生成失败 / store 写入失败 → 返回错误。
func (r *Registry) Submit(ctx context.Context, req SubmitRequest) (*Job, error) {
	if r.runner == nil {
		return nil, ErrNoRunner
	}
	if req.Cmd == "" {
		return nil, errors.New("jobs: empty cmd")
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	j := &Job{
		ID:        id,
		Code:      req.Code,
		Cmd:       req.Cmd,
		Args:      req.Args,
		Workdir:   req.Workdir,
		State:     StatePending,
		Owner:     req.Owner,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := r.store.Insert(ctx, j); err != nil {
		return nil, err
	}
	r.spawn(j)
	return r.store.Get(ctx, id)
}

// SubmitRequest 是 Registry.Submit 的入参。
type SubmitRequest struct {
	Code    string
	Cmd     string
	Args    []string
	Workdir string
	Owner   string
}

func (r *Registry) spawn(j *Job) {
	runCtx, cancel := context.WithCancel(context.Background())
	h := &runHandle{cancel: cancel, done: make(chan struct{})}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		cancel()
		return
	}
	r.running[j.ID] = h
	r.outputs[j.ID] = NewBuffer(r.bufSize)
	r.wg.Add(1)
	r.mu.Unlock()

	go func() {
		defer r.wg.Done()
		defer close(h.done)
		r.execute(runCtx, j, h)
		r.mu.Lock()
		delete(r.running, j.ID)
		r.mu.Unlock()
	}()
}

func (r *Registry) execute(ctx context.Context, j *Job, h *runHandle) {
	// 切到 Running
	j.State = StateRunning
	j.UpdatedAt = time.Now()
	start := j.UpdatedAt
	j.StartedAt = &start
	if err := r.store.Update(ctx, j); err != nil {
		j.State = StateFailed
		j.Error = "update running: " + err.Error()
		end := time.Now()
		j.FinishedAt = &end
		j.UpdatedAt = end
		_ = r.store.Update(ctx, j)
		return
	}

	r.mu.Lock()
	buf := r.outputs[j.ID]
	r.mu.Unlock()

	exit, runErr := r.runner.Run(ctx, j, func(line string) {
		buf.Append(line)
	})

	now := time.Now()
	j.FinishedAt = &now
	j.UpdatedAt = now
	j.ExitCode = exit
	if ctx.Err() != nil || runErr != nil && errors.Is(runErr, context.Canceled) {
		j.State = StateCanceled
		if runErr != nil {
			j.Error = runErr.Error()
		}
	} else if runErr != nil || exit != 0 {
		j.State = StateFailed
		if runErr != nil {
			j.Error = runErr.Error()
		}
	} else {
		j.State = StateSucceeded
	}
	_ = r.store.Update(ctx, j)
}

// Kill 取消一个 Job；幂等（已终止返回 ErrAlreadyTerminal）。
func (r *Registry) Kill(ctx context.Context, jobID string) error {
	j, err := r.store.Get(ctx, jobID)
	if err != nil {
		return err
	}
	if j.State == StateSucceeded || j.State == StateFailed || j.State == StateCanceled {
		return ErrAlreadyTerminal
	}
	r.mu.Lock()
	h, ok := r.running[jobID]
	r.mu.Unlock()
	if ok {
		h.cancel()
	}
	return nil
}

// Get 返回 Job 快照。
func (r *Registry) Get(ctx context.Context, jobID string) (*Job, error) {
	return r.store.Get(ctx, jobID)
}

// List 按 filter 返回 Job 列表。
func (r *Registry) List(ctx context.Context, filter Filter) ([]*Job, error) {
	return r.store.List(ctx, filter)
}

// Output 返回 Job 的输出行缓冲。
//
// limit<=0 表示返回全部。
func (r *Registry) Output(jobID string, since, limit int) ([]string, error) {
	r.mu.Lock()
	buf, ok := r.outputs[jobID]
	r.mu.Unlock()
	if !ok {
		return nil, ErrNotFound
	}
	var lines []string
	if since > 0 {
		lines = buf.LinesSince(since)
	} else {
		lines = buf.Lines()
	}
	if limit > 0 && len(lines) > limit {
		// 取最后 limit 行
		lines = lines[len(lines)-limit:]
	}
	return lines, nil
}

// Close 等待所有活动 Job 完成；超时由 ctx 控制。
func (r *Registry) Close(ctx context.Context) error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.mu.Unlock()

	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func newID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("jobs: id: %w", err)
	}
	return hex.EncodeToString(b), nil
}
