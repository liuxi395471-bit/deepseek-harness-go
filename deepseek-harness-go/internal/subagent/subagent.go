// Package subagent 实现 DESIGN-v3 §B：同步子 agent。
//
// 新增内置工具 agent_spawn(prompt) -> string：内部构造一个新的
// LoopRunner（共享父 runner 的 LLM client 与工具注册表），同步执行
// 到完成，把最终 assistant 文本作为 tool result 返回给父 runner。
// 嵌套调用被限制为 max_depth=1（§B.3）。
package subagent

import (
	"context"
	"errors"
)

// MaxDepth 是允许的嵌套层数：父 agent 可 spawn，子 agent 不可再 spawn。
const MaxDepth = 1

// Spawner 是 spawn 能力的契约（§B.2）。
type Spawner interface {
	Spawn(ctx context.Context, prompt string) (string, error)
}

// depthKey 在 ctx 中传递当前嵌套深度。
type depthKey struct{}

// DepthFromCtx 读取当前嵌套深度；无标记时为 0。
func DepthFromCtx(ctx context.Context) int {
	if v, ok := ctx.Value(depthKey{}).(int); ok {
		return v
	}
	return 0
}

// WithDepth 在 ctx 中标记深度（由 SpawnTool 在调用子 runner 前设置）。
func WithDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, depthKey{}, depth)
}

// ErrMaxDepth 在超过 MaxDepth 的 spawn 请求上返回。
var ErrMaxDepth = errors.New("subagent: max spawn depth exceeded")
