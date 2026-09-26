// Package workflow 提供 DAG 工作流引擎（v6 P6-2）。
//
// 一个 Workflow 由节点（Node）和有向边（Edge）组成，节点类型包括：
//
//   - "task"    : 包装并提交一个 Task；产出写到 vars["<nodeID>.output"]
//   - "function": 注册的内置函数（无 LLM 参与）；返回值写入 vars["<nodeID>.result"]
//   - "branch"  : 条件路由：按 Edge.Condition 选择 next
//   - "join"    : 多入边汇聚；可配置 all / any / N-of-M
//
// 执行语义：
//   - 拓扑排序检测环；存在环返回 ErrCycle；
//   - 无依赖节点并行执行；fan-out 同一时刻最多 workerCount 个 goroutine；
//   - 节点失败时按 OnError 策略（fail-fast / partial）决定是否继续。
//
// 与 ds-java v0.1.7+ 的对齐：相当于 cases.workflow.WorkflowService 的
// DAG 版本（v6 决策；v6.1 引入 human-in-loop / parallel-map 等节点）。
package workflow

import (
	"errors"
	"fmt"
)

// NodeType 是节点类型枚举。
type NodeType string

const (
	NodeTask     NodeType = "task"
	NodeFunction NodeType = "function"
	NodeBranch   NodeType = "branch"
	NodeJoin     NodeType = "join"
)

// JoinStrategy 是 join 节点的同步策略。
type JoinStrategy string

const (
	JoinAll     JoinStrategy = "all"     // 全部入边完成
	JoinAny     JoinStrategy = "any"     // 任一入边完成
	JoinNOfM    JoinStrategy = "n_of_m"  // N 个完成（NumRequired 字段）
)

// OnError 是节点失败时整体工作流的策略。
type OnError string

const (
	OnErrorFailFast OnError = "fail_fast" // 立即终止 run
	OnErrorPartial  OnError = "partial"   // 标记失败节点，但允许其他分支继续
)

// Edge 是节点间的有向边。
//
// Condition 是空字符串时无条件转移；否则按 evaluateCondition 解析。
// 当节点是 branch 类型时，多个出边按顺序求值，第一个 true 的获胜。
type Edge struct {
	To        string `json:"to"`
	Condition string `json:"condition,omitempty"`
}

// Node 是 DAG 中的节点。
//
// 字段含义按 Type 变化：
//   - task    : TaskConfig
//   - function: FunctionName + Args
//   - branch  : 无额外字段
//   - join    : JoinStrategy + NumRequired
type Node struct {
	ID         string            `json:"id"`
	Type       NodeType          `json:"type"`
	Task       *TaskNodeConfig   `json:"task,omitempty"`
	Function   *FunctionConfig   `json:"function,omitempty"`
	Join       *JoinConfig       `json:"join,omitempty"`
	OnError    OnError           `json:"on_error,omitempty"`
	Next       []Edge            `json:"next,omitempty"`
}

// TaskNodeConfig 是 task 节点的配置。
type TaskNodeConfig struct {
	Code       string `json:"code,omitempty"`
	Title      string `json:"title,omitempty"`
	Input      string `json:"input,omitempty"`     // 支持 {{.vars.x}} 占位
	Profile    string `json:"profile,omitempty"`
	Owner      string `json:"owner,omitempty"`
	Wait       bool   `json:"wait,omitempty"`       // true=同步等待；false=fire-and-forget
	OutputVar  string `json:"output_var,omitempty"` // 把 Task 写到这个变量；默认 "<nodeID>.task"
}

// FunctionConfig 是 function 节点的配置。
type FunctionConfig struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

// JoinConfig 是 join 节点的配置。
type JoinConfig struct {
	Strategy    JoinStrategy `json:"strategy"`
	NumRequired int          `json:"num_required,omitempty"` // N-of-M 用
}

// Workflow 是 DAG 蓝图。
type Workflow struct {
	Code  string           `json:"code"`
	Entry string           `json:"entry"`
	Nodes map[string]*Node `json:"nodes"`
}

// Validate 检查 Workflow 完整性。
//
// 错误：
//   - Entry 不存在；
//   - 有节点 ID 为空；
//   - 任意 Edge.To 不在 Nodes 中；
//   - DAG 存在环。
func (w *Workflow) Validate() error {
	if w.Entry == "" {
		return errors.New("workflow: entry empty")
	}
	if _, ok := w.Nodes[w.Entry]; !ok {
		return fmt.Errorf("workflow: entry %q not in nodes", w.Entry)
	}
	for id, n := range w.Nodes {
		if id == "" {
			return errors.New("workflow: node with empty id")
		}
		if n.Type == "" {
			return fmt.Errorf("workflow: node %q missing type", id)
		}
		for _, e := range n.Next {
			if _, ok := w.Nodes[e.To]; !ok {
				return fmt.Errorf("workflow: edge %s -> %s: target not found", id, e.To)
			}
		}
	}
	if _, err := w.topoSort(); err != nil {
		return err
	}
	return nil
}

// topoSort 做 Kahn 排序；返回排序后的节点 ID 列表。
// 检测到环时返回 ErrCycle。
func (w *Workflow) topoSort() ([]string, error) {
	inDeg := make(map[string]int)
	for id := range w.Nodes {
		inDeg[id] = 0
	}
	for _, n := range w.Nodes {
		for _, e := range n.Next {
			inDeg[e.To]++
		}
	}
	var queue []string
	for id, d := range inDeg {
		if d == 0 {
			queue = append(queue, id)
		}
	}
	var order []string
	for len(queue) > 0 {
		head := queue[0]
		queue = queue[1:]
		order = append(order, head)
		for _, e := range w.Nodes[head].Next {
			inDeg[e.To]--
			if inDeg[e.To] == 0 {
				queue = append(queue, e.To)
			}
		}
	}
	if len(order) != len(w.Nodes) {
		return nil, ErrCycle
	}
	return order, nil
}

// ErrCycle 在 DAG 存在环时返回。
var ErrCycle = errors.New("workflow: cycle detected")

// Vars 是节点间共享的可变状态。
//
// 注意：vars 是字符串索引；节点输出以 "<nodeID>.result" 或
// "<nodeID>.task" 形式写入。读取通过模板替换或条件表达式。
type Vars map[string]any

// Clone 返回 Vars 的浅拷贝。
func (v Vars) Clone() Vars {
	out := make(Vars, len(v))
	for k, val := range v {
		out[k] = val
	}
	return out
}
