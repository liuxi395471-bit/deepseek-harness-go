package a2a

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"deepseek-harness-go/internal/storage"
)

// Orchestrator 负责接收复合任务（按顺序列出多个 capability 步骤），
// 按 capability 路由到对应 agent，跨步骤共享 state（写入 v6 storage）。
//
// 与 v6 Workflow 的区别：
//   - Workflow 是 DAG 节点，节点类型 task/function/branch/join；
//   - Orchestrator 是顺序列表 + 共享 KV（agent 间通信）；
//   - Workflow 强调并行 / 条件，Orchestrator 强调多 agent 串联。
type Orchestrator struct {
	reg   *Registry
	store storage.Storage

	mu     sync.Mutex
	nextID int
}

// NewOrchestrator 构造。
func NewOrchestrator(reg *Registry, store storage.Storage) *Orchestrator {
	return &Orchestrator{reg: reg, store: store}
}

// Step 是 orchestrator 的一个步骤。
type Step struct {
	Capability string `json:"capability"`
	Input      string `json:"input"`
	// SharedKey 是把 input 注入 shared state 的键（可选）。
	SharedKey string `json:"shared_key,omitempty"`
}

// OrchestrationRequest 是 Run 的入参。
type OrchestrationRequest struct {
	Steps []Step `json:"steps"`
	// Shared 初始共享状态（写入 storage "a2a.shared" namespace）。
	Shared map[string]any `json:"shared,omitempty"`
}

// OrchestrationResult 是 Run 的出参。
type OrchestrationResult struct {
	Outputs []Result `json:"outputs"`
	Shared  map[string]any `json:"shared"`
}

// Run 按顺序执行 steps；每步失败立即返回（fail-fast）。
//
// 跨步骤状态：每步 result.Updates 写入 storage "a2a.shared" namespace。
// 下一步 task 的 SharedState 从 storage 读出（拷贝）。
func (o *Orchestrator) Run(ctx context.Context, req OrchestrationRequest) (*OrchestrationResult, error) {
	if len(req.Steps) == 0 {
		return nil, errors.New("a2a: empty steps")
	}
	if o.reg == nil {
		return nil, errors.New("a2a: registry nil")
	}
	// 初始化 shared state
	shared := make(map[string]any)
	for k, v := range req.Shared {
		shared[k] = v
	}
	if o.store != nil {
		for k, v := range shared {
			if err := storage.JSONSet(o.store, "a2a.shared", k, v); err != nil {
				return nil, fmt.Errorf("a2a: seed shared state: %w", err)
			}
		}
	}

	out := make([]Result, 0, len(req.Steps))
	for i, st := range req.Steps {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		agent, ok := o.reg.Lookup(st.Capability)
		if !ok {
			return nil, fmt.Errorf("a2a: step %d: no agent for capability %q", i, st.Capability)
		}
		// 注入 shared state
		ts := make(map[string]any, len(shared))
		for k, v := range shared {
			ts[k] = v
		}
		// 注入 SharedKey → 当前 step Input 写入 shared
		if st.SharedKey != "" {
			ts[st.SharedKey] = st.Input
		}
		o.mu.Lock()
		o.nextID++
		id := fmt.Sprintf("a2a-%d", o.nextID)
		o.mu.Unlock()
		t := Task{ID: id, Capability: st.Capability, Input: st.Input, SharedState: ts}

		res, err := agent.Handle(ctx, t)
		if err != nil {
			return nil, fmt.Errorf("a2a: step %d (%s): %w", i, agent.Name, err)
		}
		// 写 Updates 到 shared
		if len(res.Updates) > 0 {
			for k, v := range res.Updates {
				shared[k] = v
				if o.store != nil {
					if err := storage.JSONSet(o.store, "a2a.shared", k, v); err != nil {
						return nil, fmt.Errorf("a2a: persist shared: %w", err)
					}
				}
			}
		}
		out = append(out, res)
	}
	return &OrchestrationResult{Outputs: out, Shared: shared}, nil
}

// SharedState 从 storage 读出当前 shared state（用于跨进程 / 测试断言）。
func (o *Orchestrator) SharedState() map[string]any {
	out := make(map[string]any)
	if o.store == nil {
		return out
	}
	keys, err := o.store.List("a2a.shared")
	if err != nil {
		return out
	}
	for _, k := range keys {
		var v any
		if err := storage.JSONGet(o.store, "a2a.shared", k, &v); err == nil {
			out[k] = v
		}
	}
	return out
}
