package workflow

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// FunctionRegistry 持有 function 节点的可调用函数集合。
//
// 注册的函数接收 vars + args，返回一个值（写入 vars["<nodeID>.result"]）。
// 错误视为节点失败。
type FunctionRegistry struct {
	mu      sync.RWMutex
	entries map[string]Func
}

// Func 是注册到 FunctionRegistry 的可调用单元。
type Func func(ctx context.Context, vars Vars, args map[string]any) (any, error)

// NewFunctionRegistry 构造空注册表。
func NewFunctionRegistry() *FunctionRegistry {
	return &FunctionRegistry{entries: make(map[string]Func)}
}

// Register 注册 name → fn。同名覆盖返回旧值。
func (r *FunctionRegistry) Register(name string, fn Func) (old Func) {
	r.mu.Lock()
	defer r.mu.Unlock()
	old = r.entries[name]
	r.entries[name] = fn
	return
}

// Call 按 name 调用。
func (r *FunctionRegistry) Call(ctx context.Context, name string, vars Vars, args map[string]any) (any, error) {
	r.mu.RLock()
	fn, ok := r.entries[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("workflow: function %q not registered", name)
	}
	return fn(ctx, vars, args)
}

// ConditionEvaluator 是 condition 表达式的求值器。
//
// 极简语法（v6）：
//
//	"<lhs> <op> <rhs>"
//
// op ∈ { ==, !=, contains }。
// lhs / rhs 可以是字面量（数字 / 字符串 / true / false）或 {{.vars.xxx}}。
//
// 单 token 表达式（"true" / "false"）视为布尔常量。
type ConditionEvaluator struct{}

// NewConditionEvaluator 构造求值器。
func NewConditionEvaluator() *ConditionEvaluator { return &ConditionEvaluator{} }

// Eval 按 vars 解析 expr；expr 为空返回 true（无条件）。
func (e *ConditionEvaluator) Eval(expr string, vars Vars) (bool, error) {
	if expr == "" {
		return true, nil
	}
	// 单 token
	if expr == "true" {
		return true, nil
	}
	if expr == "false" {
		return false, nil
	}
	// 双 token 表达式：先尝试 " contains "（带空格、不与 ==/!= 冲突），
	// 然后按 == 与 != 顺序试。
	if idx := indexOf(expr, " contains "); idx >= 0 {
		lhs := trimAll(expr[:idx])
		rhs := trimAll(expr[idx+len(" contains "):])
		lv, err := resolveAtom(lhs, vars)
		if err != nil {
			return false, err
		}
		rv, err := resolveAtom(rhs, vars)
		if err != nil {
			return false, err
		}
		ls, ok := lv.(string)
		if !ok {
			return false, fmt.Errorf("workflow: contains lhs not string: %T", lv)
		}
		rs, ok := rv.(string)
		if !ok {
			return false, fmt.Errorf("workflow: contains rhs not string: %T", rv)
		}
		return containsString(ls, rs), nil
	}
	if idx := indexOf(expr, "=="); idx >= 0 {
		lhs := trimAll(expr[:idx])
		rhs := trimAll(expr[idx+2:])
		lv, err := resolveAtom(lhs, vars)
		if err != nil {
			return false, err
		}
		rv, err := resolveAtom(rhs, vars)
		if err != nil {
			return false, err
		}
		return equalValues(lv, rv), nil
	}
	if idx := indexOf(expr, "!="); idx >= 0 {
		lhs := trimAll(expr[:idx])
		rhs := trimAll(expr[idx+2:])
		lv, err := resolveAtom(lhs, vars)
		if err != nil {
			return false, err
		}
		rv, err := resolveAtom(rhs, vars)
		if err != nil {
			return false, err
		}
		return !equalValues(lv, rv), nil
	}
	return false, fmt.Errorf("workflow: cannot parse condition %q", expr)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func trimAll(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

func resolveAtom(token string, vars Vars) (any, error) {
	token = trimAll(token)
	// vars.xxx
	if len(token) >= 5 && token[:5] == "vars." {
		key := token[5:]
		if v, ok := vars[key]; ok {
			return v, nil
		}
		return nil, nil
	}
	// 字面量
	switch token {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null", "nil":
		return nil, nil
	}
	// 数字
	var n int
	if _, err := fmt.Sscanf(token, "%d", &n); err == nil {
		return n, nil
	}
	// 字符串（去引号）
	if len(token) >= 2 && token[0] == '"' && token[len(token)-1] == '"' {
		return token[1 : len(token)-1], nil
	}
	if len(token) >= 2 && token[0] == '\'' && token[len(token)-1] == '\'' {
		return token[1 : len(token)-1], nil
	}
	// 默认：作为字面量字符串
	return token, nil
}

func equalValues(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	as, aok := a.(string)
	bs, bok := b.(string)
	if aok && bok {
		return as == bs
	}
	af, aok := a.(float64)
	bf, bok := b.(float64)
	if aok && bok {
		return af == bf
	}
	ai, aok := a.(int)
	bi, bok := b.(int)
	if aok && bok {
		return ai == bi
	}
	ab, aok := a.(bool)
	bb, bok := b.(bool)
	if aok && bok {
		return ab == bb
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func containsString(s, sub string) bool {
	if sub == "" {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ErrJoinNotReady 是 join 节点在所有入边完成前被调用时的错误（内部用）。
var ErrJoinNotReady = errors.New("workflow: join not ready")
