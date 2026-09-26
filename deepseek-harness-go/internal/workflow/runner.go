package workflow

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"deepseek-harness-go/internal/task"
)

// TaskSubmitter 是 runner 提交 task 节点时依赖的接口。
//
// 通常是 task.LoopExecutor；测试可注入 fake。
type TaskSubmitter interface {
	Submit(ctx context.Context, req task.SubmitRequest) (*task.Task, error)
	Get(ctx context.Context, id string) (*task.Task, error)
}

// RunState 是单次工作流执行的运行时状态。
type RunState struct {
	RunID      string            `json:"run_id"`
	Workflow   string            `json:"workflow"`
	NodeStates map[string]string `json:"node_states"` // Pending/Running/Done/Failed/Skipped
	Vars       Vars              `json:"vars"`
	StartedAt  time.Time         `json:"started_at"`
	FinishedAt *time.Time        `json:"finished_at,omitempty"`
	Error      string            `json:"error,omitempty"`
	StopReason string            `json:"stop_reason"` // "" / "completed" / "failed" / "partial" / "canceled"
}

// DefaultRunnerConfig 是 Runner 的零开销默认配置。
type DefaultRunnerConfig struct {
	WorkerCount int           // 并行节点数；0 = 4
	PollTask    time.Duration // task 节点等待轮询间隔；0 = 50ms
}

func (c *DefaultRunnerConfig) applyDefaults() {
	if c.WorkerCount <= 0 {
		c.WorkerCount = 4
	}
	if c.PollTask <= 0 {
		c.PollTask = 50 * time.Millisecond
	}
}

// Runner 执行工作流。
//
// 并发模型：固定大小 worker pool；ready queue 由 chan string 提供；
// 每个 worker 从 queue 取节点 → 执行 → 把下游入队。
type Runner struct {
	cfg        DefaultRunnerConfig
	tasks      TaskSubmitter
	functions  *FunctionRegistry
	conditions *ConditionEvaluator

	mu sync.Mutex
	id uint64
}

// NewRunner 构造 runner；tasks 与 fns 必填（fns 可为 nil 自动构造空）。
func NewRunner(tasks TaskSubmitter, fns *FunctionRegistry, cfg DefaultRunnerConfig) *Runner {
	cfg.applyDefaults()
	if fns == nil {
		fns = NewFunctionRegistry()
	}
	return &Runner{
		cfg:        cfg,
		tasks:      tasks,
		functions:  fns,
		conditions: NewConditionEvaluator(),
	}
}

// Start 在 goroutine 中启动 workflow；返回 *RunState。
func (r *Runner) Start(ctx context.Context, w *Workflow, input map[string]any) (*RunState, error) {
	if err := w.Validate(); err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.id++
	rid := fmt.Sprintf("run-%d", r.id)
	r.mu.Unlock()

	state := &RunState{
		RunID:      rid,
		Workflow:   w.Code,
		NodeStates: make(map[string]string, len(w.Nodes)),
		Vars:       Vars{},
		StartedAt:  time.Now(),
	}
	for id := range w.Nodes {
		state.NodeStates[id] = "pending"
	}
	for k, v := range input {
		state.Vars[k] = v
	}

	go r.execute(ctx, w, state)
	return state, nil
}

	// execute 主体。
	//
	// 关键结构：
	//   - indeg map：每个节点的剩余入度数；
	//   - ready chan：当前可执行的节点 ID；
	//   - worker pool：固定大小，并发从 ready 取节点；
	//   - 节点完成后，indeg[To]--；归零 → 入队 ready。
	//   - branch 节点：仅把第一个命中 condition 的 To 入队；其他出边的
	//     下游若最终 indeg==0 仍会被入队，此时如果已经被 branch 跳过
	//     则需要另行处理。v6 简化：把未选中出边的下游显式入队 + 标 skipped。
	//   - 退出：所有节点都已经 settled（done / failed / skipped）后，
	//     关闭 doneCh，workers 看到 doneCh 关闭后退出。
func (r *Runner) execute(ctx context.Context, w *Workflow, st *RunState) {
	defer func() {
		end := time.Now()
		st.FinishedAt = &end
	}()

	// incoming map + indeg
	incoming := make(map[string][]string)
	indeg := make(map[string]int)
	for id := range w.Nodes {
		indeg[id] = 0
	}
	for _, n := range w.Nodes {
		for _, e := range n.Next {
			indeg[e.To]++
			incoming[e.To] = append(incoming[e.To], e.To)
		}
	}
	// incoming 是为了未来 join 用；当前 v6 暂不实现 join 同步策略。

	ready := make(chan string, len(w.Nodes))
	skipped := make(map[string]bool)
	var mu sync.Mutex
	var firstErr error
	var failFast bool
	settled := make(map[string]bool)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	enqueue := func(id string) {
		mu.Lock()
		if skipped[id] || settled[id] || st.NodeStates[id] == "skipped" {
			mu.Unlock()
			return
		}
		mu.Unlock()
		select {
		case ready <- id:
		default:
		}
	}

	markSkip := func(id string) {
		mu.Lock()
		skipped[id] = true
		settled[id] = true
		if _, ok := st.NodeStates[id]; ok && st.NodeStates[id] == "pending" {
			st.NodeStates[id] = "skipped"
		}
		mu.Unlock()
	}

	finishNode := func(id string, success bool) {
		mu.Lock()
		if success {
			st.NodeStates[id] = "done"
		} else {
			st.NodeStates[id] = "failed"
		}
		settled[id] = true
		mu.Unlock()

		node := w.Nodes[id]
		for _, e := range node.Next {
			mu.Lock()
			indeg[e.To]--
			left := indeg[e.To]
			mu.Unlock()
			if left == 0 {
				enqueue(e.To)
			}
		}
	}

	stop := func(reason string, err error) {
		mu.Lock()
		if !failFast {
			failFast = true
			st.StopReason = reason
			if err != nil {
				st.Error = err.Error()
				if firstErr == nil {
					firstErr = err
				}
			}
			cancel()
		}
		mu.Unlock()
	}

	// 监视器：当所有节点都已 settled 时关闭 doneCh，让 workers 退出。
	doneCh := make(chan struct{})
	go func() {
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				close(doneCh)
				return
			case <-tick.C:
				mu.Lock()
				done := len(settled) >= len(w.Nodes)
				mu.Unlock()
				if done {
					close(doneCh)
					return
				}
			}
		}
	}()

	// worker
	var wg sync.WaitGroup
	for i := 0; i < r.cfg.WorkerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case <-doneCh:
					return
				case id := <-ready:
					mu.Lock()
					if skipped[id] {
						mu.Unlock()
						continue
					}
					st.NodeStates[id] = "running"
					mu.Unlock()

					if err := r.runNode(ctx, w, w.Nodes[id], st, enqueue, markSkip); err != nil {
						n := w.Nodes[id]
						if n.OnError == OnErrorPartial {
							finishNode(id, false)
							continue
						}
						finishNode(id, false)
						stop("failed", err)
						return
					} else {
						if w.Nodes[id].Type == NodeBranch {
							// branch 节点：手动处理下游 indeg。
							// runBranch 已 markSkip 非选中边；这里
							// 对所有出边的目标 --indeg，让选中边的
							// 目标归零并被 enqueue，未选中边的目标
							// 归零但因 markSkip 不会被 enqueue。
							mu.Lock()
							st.NodeStates[id] = "done"
							settled[id] = true
							mu.Unlock()
							for _, e := range w.Nodes[id].Next {
								mu.Lock()
								indeg[e.To]--
								left := indeg[e.To]
								mu.Unlock()
								if left == 0 {
									enqueue(e.To)
								}
							}
							continue
						}
						finishNode(id, true)
					}
				}
			}
		}()
	}

	// 启动入口
	enqueue(w.Entry)
	wg.Wait()

	mu.Lock()
	if st.StopReason == "" {
		st.StopReason = "completed"
		for _, s := range st.NodeStates {
			if s == "failed" {
				st.StopReason = "failed"
				break
			}
			if s != "done" && s != "skipped" {
				st.StopReason = "partial"
			}
		}
	}
	mu.Unlock()
}

// runNode 执行单个节点。branch 类型需传入 enqueue/markSkip 用于
// 选中边的下游入队 + 未选中边的下游标 skipped。
func (r *Runner) runNode(
	ctx context.Context,
	_ *Workflow,
	n *Node,
	st *RunState,
	enqueue func(string),
	markSkip func(string),
) error {
	switch n.Type {
	case NodeTask:
		return r.runTask(ctx, n, st)
	case NodeFunction:
		return r.runFunction(ctx, n, st)
	case NodeBranch:
		return r.runBranch(n, st, enqueue, markSkip)
	case NodeJoin:
		// v6 简化：join 在 indeg 同步下自然完成；无需特殊执行。
		return nil
	default:
		return fmt.Errorf("workflow: unknown node type %q", n.Type)
	}
}

func (r *Runner) runTask(ctx context.Context, n *Node, st *RunState) error {
	if n.Task == nil {
		return fmt.Errorf("workflow: task node %q missing config", n.ID)
	}
	input := renderTemplate(n.Task.Input, st.Vars)
	t, err := r.tasks.Submit(ctx, task.SubmitRequest{
		Code:    n.Task.Code,
		Title:   n.Task.Title,
		Input:   input,
		Profile: n.Task.Profile,
		Owner:   n.Task.Owner,
	})
	if err != nil {
		return fmt.Errorf("workflow: task submit %q: %w", n.ID, err)
	}
	outVar := n.Task.OutputVar
	if outVar == "" {
		outVar = n.ID + ".task"
	}
	st.Vars[outVar] = t.ID

	if !n.Task.Wait {
		return nil
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		got, err := r.tasks.Get(ctx, t.ID)
		if err != nil {
			return fmt.Errorf("workflow: task get %q: %w", t.ID, err)
		}
		if got.State == task.StateCompleted || got.State == task.StateFailed || got.State == task.StateCanceled {
			st.Vars[outVar] = got
			if got.State != task.StateCompleted {
				return fmt.Errorf("workflow: task %q ended %s: %s", t.ID, got.State, got.Error)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(r.cfg.PollTask):
		}
	}
}

func (r *Runner) runFunction(ctx context.Context, n *Node, st *RunState) error {
	if n.Function == nil {
		return fmt.Errorf("workflow: function node %q missing config", n.ID)
	}
	out, err := r.functions.Call(ctx, n.Function.Name, st.Vars, n.Function.Args)
	if err != nil {
		return fmt.Errorf("workflow: function %q: %w", n.Function.Name, err)
	}
	st.Vars[n.ID+".result"] = out
	return nil
}

// runBranch 选择命中 condition 的第一条出边。
//
// 关键点：与 finishNode 不同的是，branch 不调用 finishNode（避免把
// 所有出边的下游入队）。worker 在 runNode 返回 nil 后识别 NodeBranch
// 并直接把节点标 done。
//
// 但 branch 仍然需要 decrement 出边目标的 indeg（否则下游永远
// 不会被 enqueue）。所以这里：
//   - 选中边：indeg 减 1 → enqueue
//   - 未选中边：markSkip + indeg 减 1
func (r *Runner) runBranch(n *Node, st *RunState, enqueue func(string), markSkip func(string)) error {
	if len(n.Next) == 0 {
		return fmt.Errorf("workflow: branch %q has no next edges", n.ID)
	}
	chosen := ""
	for _, e := range n.Next {
		ok, err := r.conditions.Eval(e.Condition, st.Vars)
		if err != nil {
			return fmt.Errorf("workflow: branch %q condition %q: %w", n.ID, e.Condition, err)
		}
		if ok {
			chosen = e.To
			break
		}
	}
	if chosen == "" {
		return fmt.Errorf("workflow: branch %q: no edge satisfied", n.ID)
	}
	st.Vars["__branch_"+n.ID] = chosen
	for _, e := range n.Next {
		if e.To == chosen {
			// 选中边：indeg 由 caller（worker）减 1 并 enqueue。
			continue
		}
		// 未选中边：标 skipped，caller 仍会减 indeg，但 skip 状态防止
		// 节点被入队执行。
		markSkip(e.To)
	}
	return nil
}

// renderTemplate 把 {{.vars.x}} 占位符替换为 vars[x]。
//
// 缺失 key 时保留原占位符；仅支持 vars.xxx 这一种路径。
func renderTemplate(s string, vars Vars) string {
	if s == "" {
		return s
	}
	const prefix = "{{.vars."
	out := make([]byte, 0, len(s))
	i := 0
	for i < len(s) {
		if i+len(prefix) <= len(s) && s[i:i+len(prefix)] == prefix {
			j := i + len(prefix)
			for j+1 < len(s) {
				if s[j:j+2] == "}}" {
					key := s[i+len(prefix) : j]
					if v, ok := vars[key]; ok {
						out = append(out, fmt.Sprintf("%v", v)...)
					} else {
						out = append(out, s[i:j+2]...)
					}
					i = j + 2
					goto next
				}
				j++
			}
			out = append(out, s[i:]...)
			return string(out)
		}
		out = append(out, s[i])
		i++
	next:
	}
	return string(out)
}

// Errors used by tests / callers.
var (
	ErrUnknownFunction = errors.New("workflow: unknown function")
)
