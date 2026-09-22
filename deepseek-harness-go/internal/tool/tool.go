// Package tool 定义 Tool 接口、Result 包装类型、线程安全的内存 Registry，
// 以及 Tool 与线上格式 llm.ToolSpec 之间的转换。
//
// 工具是纯粹的执行器：它们接收 ctx + 原始 JSON 参数并返回 Result。
// 它们不了解 LLM、runner，也不了解其他工具。
package tool

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"deepseek-harness-go/internal/llm"
)

// ErrDuplicate 在 Registry.Register 发现同名工具已注册时返回。
var ErrDuplicate = errors.New("tool: duplicate name")

// ErrNotFound 在工具不在注册表中时返回。
var ErrNotFound = errors.New("tool: not found")

// Result 是每个工具的返回值。IsError=true 表示失败应作为工具消息
// 反馈给 LLM，而不是向上抛给 runner。
type Result struct {
	Content string
	IsError bool
}

// Ok 是成功路径的便捷构造函数。
func Ok(content string) Result { return Result{Content: content} }

// Err 是失败路径的便捷构造函数。
func Err(content string) Result { return Result{Content: content, IsError: true} }

// Tool 是每个工具都必须实现的契约。
//
// Name() 和 Description() 是稳定的标识符，既用于 LLM 的工具选择，
// 也用于 REPL/--debug 渲染。
//
// Parameters() 返回描述工具参数的 JSON Schema 对象；它会被原样转发给
// LLM，作为 llm.ToolSpecFunc.Parameters。最常见的表示形式是
// map[string]any，但任何 json.Marshal 可序列化的值都可以。
//
// Execute 由 runner 使用 LLM 生成的参数 JSON（通常是
// ToolCall.Function.Arguments）调用。从 Execute 返回非 nil 的 error
// 仅保留给不可恢复的情况；runner 会将其折叠进 Result.IsError，
// 使模型可以继续推理。
type Tool interface {
	Name() string
	Description() string
	Parameters() any
	Execute(ctx context.Context, args json.RawMessage) (Result, error)
}

// Spec 将 Tool 转换为面向 LLM 的线上表示。
func Spec(t Tool) llm.ToolSpec {
	return llm.ToolSpec{
		Type: "function",
		Function: llm.ToolSpecFunc{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		},
	}
}

// Registry 是一个线程安全的 name → Tool 映射。
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
	order []string // 插入顺序，保证 Names()/Specs() 输出稳定
}

// NewRegistry 返回一个空注册表。
func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}}
}

// Register 添加 t。若 name 已被占用则返回 ErrDuplicate。
func (r *Registry) Register(t Tool) error {
	if t == nil {
		return errors.New("tool: nil tool")
	}
	name := t.Name()
	if name == "" {
		return errors.New("tool: empty name")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[name]; exists {
		return ErrDuplicate
	}
	r.tools[name] = t
	r.order = append(r.order, name)
	return nil
}

// MustRegister 是出错时会 panic 的 Register；适用于启动阶段。
func (r *Registry) MustRegister(t Tool) {
	if err := r.Register(t); err != nil {
		panic(err)
	}
}

// Get 返回指定名称的工具，否则返回 ErrNotFound。
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// MustGet 是工具缺失时会 panic 的 Get；适用于确定工具已注册的
// 调用点（通常在 runner 内部）。
func (r *Registry) MustGet(name string) Tool {
	t, ok := r.Get(name)
	if !ok {
		panic(ErrNotFound)
	}
	return t
}

// Names 按插入顺序返回已注册的工具名。
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// Specs 按插入顺序返回所有已注册工具的面向 LLM 的声明。
// 返回的切片是全新副本，可安全修改。
func (r *Registry) Specs() []llm.ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]llm.ToolSpec, 0, len(r.order))
	for _, n := range r.order {
		if t, ok := r.tools[n]; ok {
			out = append(out, Spec(t))
		}
	}
	return out
}

// Len 返回已注册工具的数量。
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.order)
}
