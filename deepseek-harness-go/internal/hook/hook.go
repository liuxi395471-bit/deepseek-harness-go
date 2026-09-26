// Package hook — 工具执行钩子（v5 P5-6）。
//
// hook.go 提供 PRE_TOOL_USE / POST_TOOL_USE 两类拦截点的实现
// 框架：调用方在工具执行前后获得修改 args / 拦截 / 改写 result 的
// 机会；多个钩子按注册顺序串行执行（first error 终止）。
//
// 设计要点：
//   - 接口：Hook + Event + Registry；
//   - 串行调度：第一个返回 error 的钩子终止后续；
//   - ctx.Done 兼容；
//   - 向后兼容：nil Registry 即 no-op，行为同 v4。
package hook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"deepseek-harness-go/internal/tool"
)

// Event 区分钩子触发位置。
type Event int

const (
	// PreToolUse 在工具执行前触发——可改 args / 拦截。
	PreToolUse Event = iota
	// PostToolUse 在工具执行后触发——可改 result / 追加日志。
	PostToolUse
)

// String 渲染事件名。
func (e Event) String() string {
	switch e {
	case PreToolUse:
		return "pre_tool_use"
	case PostToolUse:
		return "post_tool_use"
	default:
		return fmt.Sprintf("hook.event(%d)", int(e))
	}
}

// PreRequest 是 PreToolUse 钩子的输入/输出。
//
// Args 是 *json.RawMessage：钩子可读 + 可改写。改写时直接赋值；
// 如果钩子只是读，写入新值时分配新 RawMessage。
type PreRequest struct {
	Tool string
	Args *json.RawMessage
}

// PostRequest 是 PostToolUse 钩子的输入/输出。
//
// Result 是 tool.Result 的拷贝：钩子可改写 IsError / Content。
type PostRequest struct {
	Tool   string
	Args   json.RawMessage
	Result tool.Result
}

// Hook 是单个钩子的契约。
//
// Match 决定该钩子是否对 (tool, args) 感兴趣；返回 false 表示该
// 钩子被跳过（Pre/Post 都不调用）。
//
// Pre 在工具执行前触发；返回 error 视为工具被拦截（runner 应当
// 把 err 折叠成 tool.Result.IsError=true 的回填消息）。
//
// Post 在工具执行后触发；通常只读取 / 改写 Result 后记录日志。
type Hook interface {
	Name() string
	Match(toolName string, args json.RawMessage) bool
	Pre(ctx context.Context, req *PreRequest) error
	Post(ctx context.Context, req *PostRequest) error
}

// Registry 收集多个 Hook；Pre/Post 按注册顺序串行执行。
//
// 零值可用：nil registry 调用方应判断；NewRegistry 返回非 nil。
type Registry struct {
	mu    sync.RWMutex
	hooks map[Event][]Hook
}

// NewRegistry 构造空 Registry。
func NewRegistry() *Registry {
	return &Registry{hooks: make(map[Event][]Hook)}
}

// Register 添加钩子。重复 name 注册到同一 event 视为追加（顺序在末尾）。
func (r *Registry) Register(ev Event, h Hook) {
	if h == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hooks[ev] = append(r.hooks[ev], h)
}

// Pre 按注册顺序对每个 hook 调 h.Pre；第一个非 nil err 终止链。
//
// 调用方在使用时根据返回 err 决定是否继续执行工具（通常 err 视为
// 拦截）。
func (r *Registry) Pre(ctx context.Context, req *PreRequest) error {
	if r == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	for _, h := range r.snapshot(PreToolUse) {
		if !h.Match(req.Tool, derefArgs(req.Args)) {
			continue
		}
		if err := h.Pre(ctx, req); err != nil {
			return fmt.Errorf("hook %s pre: %w", h.Name(), err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	return nil
}

// Post 按注册顺序对每个 hook 调 h.Post；错误同样终止链（但通常
// Post 不致命，调用方可自行选择忽略）。
func (r *Registry) Post(ctx context.Context, req *PostRequest) error {
	if r == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	for _, h := range r.snapshot(PostToolUse) {
		if !h.Match(req.Tool, req.Args) {
			continue
		}
		if err := h.Post(ctx, req); err != nil {
			return fmt.Errorf("hook %s post: %w", h.Name(), err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	return nil
}

// snapshot 返回 ev 时刻的 hooks 列表副本，避免迭代中并发修改。
func (r *Registry) snapshot(ev Event) []Hook {
	r.mu.RLock()
	defer r.mu.RUnlock()
	src := r.hooks[ev]
	out := make([]Hook, len(src))
	copy(out, src)
	return out
}

// Size 返回 ev 注册的 hook 数（诊断用）。
func (r *Registry) Size(ev Event) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.hooks[ev])
}

// derefArgs 解 *json.RawMessage；nil 时给空。
func derefArgs(p *json.RawMessage) json.RawMessage {
	if p == nil {
		return nil
	}
	return *p
}

// --- FuncHook 适配器：把单个回调函数包装为 Hook。 ---

// FuncHook 便于测试与小工具；name 用于错误信息与日志。
type FuncHook struct {
	NameStr string
	MatchFn func(string, json.RawMessage) bool
	PreFn   func(context.Context, *PreRequest) error
	PostFn  func(context.Context, *PostRequest) error
}

// Name 实现 Hook。
func (f *FuncHook) Name() string { return f.NameStr }

// Match 实现 Hook。
func (f *FuncHook) Match(name string, args json.RawMessage) bool {
	if f.MatchFn == nil {
		return true
	}
	return f.MatchFn(name, args)
}

// Pre 实现 Hook。
func (f *FuncHook) Pre(ctx context.Context, req *PreRequest) error {
	if f.PreFn == nil {
		return nil
	}
	return f.PreFn(ctx, req)
}

// Post 实现 Hook。
func (f *FuncHook) Post(ctx context.Context, req *PostRequest) error {
	if f.PostFn == nil {
		return nil
	}
	return f.PostFn(ctx, req)
}

// ErrDenied 是钩子常用的拒绝 / 拦截错误。
var ErrDenied = errors.New("hook: denied")
