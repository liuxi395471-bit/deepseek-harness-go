package hook

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"deepseek-harness-go/internal/tool"
)

// 简单工具：返回 ok。
type okTool struct{}

func (okTool) Name() string                            { return "ok" }
func (okTool) Description() string                     { return "always ok" }
func (okTool) Parameters() any                         { return map[string]any{"type": "object"} }
func (okTool) Execute(_ context.Context, _ json.RawMessage) (tool.Result, error) {
	return tool.Ok("ok"), nil
}

// Event 字符串输出。
func TestEvent_String(t *testing.T) {
	if got := PreToolUse.String(); got != "pre_tool_use" {
		t.Fatalf("PreToolUse.String = %q", got)
	}
	if got := PostToolUse.String(); got != "post_tool_use" {
		t.Fatalf("PostToolUse.String = %q", got)
	}
}

// Registry 空时 Pre/Post 不报错。
func TestRegistry_EmptyIsNoop(t *testing.T) {
	var r *Registry // nil
	if err := r.Pre(context.Background(), &PreRequest{Tool: "x"}); err != nil {
		t.Fatalf("nil registry Pre 应 no-op: %v", err)
	}
	if err := r.Post(context.Background(), &PostRequest{Tool: "x"}); err != nil {
		t.Fatalf("nil registry Post 应 no-op: %v", err)
	}

	r2 := NewRegistry()
	if err := r2.Pre(context.Background(), &PreRequest{}); err != nil {
		t.Fatalf("空 registry Pre: %v", err)
	}
}

// 钩子改 args。
func TestRegistry_Pre_ArgsRewrite(t *testing.T) {
	r := NewRegistry()
	called := false
	h := &FuncHook{
		NameStr: "rewriter",
		PreFn: func(_ context.Context, req *PreRequest) error {
			rewritten := json.RawMessage(`{"v":42}`)
			*req.Args = rewritten
			called = true
			return nil
		},
	}
	r.Register(PreToolUse, h)

	in := json.RawMessage(`{"v":0}`)
	if err := r.Pre(context.Background(), &PreRequest{Tool: "x", Args: &in}); err != nil {
		t.Fatalf("Pre: %v", err)
	}
	if !called {
		t.Fatalf("Pre 未调用")
	}
	if string(in) != `{"v":42}` {
		t.Fatalf("args 未改写: %s", in)
	}
}

// Pre 钩子错误终止链。
func TestRegistry_Pre_ErrorTerminates(t *testing.T) {
	r := NewRegistry()
	var preCalls, postCalls int32

	denyHook := &FuncHook{
		NameStr: "deny",
		PreFn: func(_ context.Context, _ *PreRequest) error {
			atomic.AddInt32(&preCalls, 1)
			return ErrDenied
		},
	}
	secondHook := &FuncHook{
		NameStr: "second",
		PreFn: func(_ context.Context, _ *PreRequest) error {
			atomic.AddInt32(&postCalls, 1)
			return nil
		},
	}
	r.Register(PreToolUse, denyHook)
	r.Register(PreToolUse, secondHook)

	in := json.RawMessage(`{}`)
	err := r.Pre(context.Background(), &PreRequest{Tool: "x", Args: &in})
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("err = %v, want ErrDenied", err)
	}
	if preCalls != 1 {
		t.Fatalf("deny 应调用一次, got %d", preCalls)
	}
	if postCalls != 0 {
		t.Fatalf("second 不应被调用, got %d", postCalls)
	}
}

// Match 不命中跳过。
func TestRegistry_MatchFilters(t *testing.T) {
	r := NewRegistry()
	var called int32
	h := &FuncHook{
		NameStr: "matcher",
		MatchFn: func(tool string, _ json.RawMessage) bool {
			return tool == "include"
		},
		PreFn: func(_ context.Context, _ *PreRequest) error {
			atomic.AddInt32(&called, 1)
			return nil
		},
	}
	r.Register(PreToolUse, h)

	_ = r.Pre(context.Background(), &PreRequest{Tool: "exclude"})
	_ = r.Pre(context.Background(), &PreRequest{Tool: "include"})
	if called != 1 {
		t.Fatalf("预期调用一次, got %d", called)
	}
}

// 多钩子按注册顺序串行。
func TestRegistry_SerialOrdering(t *testing.T) {
	r := NewRegistry()
	var order []string
	mk := func(name string) *FuncHook {
		return &FuncHook{
			NameStr: name,
			PreFn: func(_ context.Context, _ *PreRequest) error {
				order = append(order, name)
				return nil
			},
		}
	}
	r.Register(PreToolUse, mk("a"))
	r.Register(PreToolUse, mk("b"))
	r.Register(PreToolUse, mk("c"))

	_ = r.Pre(context.Background(), &PreRequest{Tool: "x"})
	if len(order) != 3 || order[0] != "a" || order[1] != "b" || order[2] != "c" {
		t.Fatalf("顺序错误: %v", order)
	}
}

// Post 改 result。
func TestRegistry_Post_RewriteResult(t *testing.T) {
	r := NewRegistry()
	r.Register(PostToolUse, &FuncHook{
		NameStr: "tape",
		PostFn: func(_ context.Context, req *PostRequest) error {
			req.Result.Content = "wrapped: " + req.Result.Content
			return nil
		},
	})

	req := &PostRequest{
		Tool:   "t",
		Result: tool.Result{Content: "raw", IsError: false},
	}
	if err := r.Post(context.Background(), req); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if req.Result.Content != "wrapped: raw" {
		t.Fatalf("content = %q", req.Result.Content)
	}
}

// Context 取消。
func TestRegistry_CtxCancelledTerminates(t *testing.T) {
	r := NewRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := r.Pre(ctx, &PreRequest{Tool: "x"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
