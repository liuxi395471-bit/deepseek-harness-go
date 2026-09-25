package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"deepseek-harness-go/internal/agent"
	"deepseek-harness-go/internal/llm"
	"deepseek-harness-go/internal/tool"
)

// SpawnTool 是暴露给 LLM 的内置工具 agent_spawn（§B.1）。
type SpawnTool struct {
	Spawner Spawner
}

// NewSpawnTool 构造 agent_spawn 工具。
func NewSpawnTool(s Spawner) *SpawnTool { return &SpawnTool{Spawner: s} }

// Name 实现 tool.Tool。
func (t *SpawnTool) Name() string { return "agent_spawn" }

// Description 实现 tool.Tool。
func (t *SpawnTool) Description() string {
	return "Spawn a synchronous sub-agent with a self-contained task prompt and return its final answer. Args: {prompt}. The sub-agent shares tools but cannot spawn further sub-agents."
}

// Parameters 实现 tool.Tool。
func (t *SpawnTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{"type": "string", "description": "Complete, self-contained task for the sub-agent."},
		},
		"required": []string{"prompt"},
	}
}

// spawnArgs 是一次性解析的 JSON 结构。
type spawnArgs struct {
	Prompt string `json:"prompt"`
}

// Execute 实现 tool.Tool：同步执行子 agent（§B.3 验收点全部落在这里）。
func (t *SpawnTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	var a spawnArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return tool.Err("agent_spawn: invalid args: " + err.Error()), nil
	}
	if a.Prompt == "" {
		return tool.Err("agent_spawn: prompt required"), nil
	}
	if DepthFromCtx(ctx) >= MaxDepth {
		return tool.Err(ErrMaxDepth.Error()), nil
	}
	var res SubResult
	if sr, ok := t.Spawner.(*SubRunner); ok {
		res = sr.SpawnResult(WithDepth(ctx, DepthFromCtx(ctx)+1), a.Prompt)
	} else {
		text, err := t.Spawner.Spawn(WithDepth(ctx, DepthFromCtx(ctx)+1), a.Prompt)
		res = SubResult{Text: text, Err: err}
	}
	return res.ToToolResult(), nil
}

// SubRunner 持有父 runner 与子 runner（§B.2）。
type SubRunner struct {
	Parent *agent.LoopRunner
	Child  *agent.LoopRunner
}

// compile-time: SubRunner 满足 Spawner。
var _ Spawner = (*SubRunner)(nil)

// SpawnResult 运行子 agent 并返回结构化结果（Spawn 的展开形式，
// 便于测试与审计）。
func (s *SubRunner) SpawnResult(ctx context.Context, prompt string) SubResult {
	events, resCh := s.Child.RunStream(ctx, prompt, "")
	// 排空事件流：子 agent 的中间事件对父 runner 不可见（§B.1），
	// 但必须消费完以防子 runner 阻塞。
	for range events {
	}
	res := <-resCh
	if res.Error != nil {
		// deadline exceeded 与 canceled 都按 canceled 归类（§B.3：
		// 超时 → is_error, msg=canceled）。
		if errors.Is(res.Error, context.Canceled) || errors.Is(res.Error, context.DeadlineExceeded) {
			return SubResult{StopReason: "canceled", Err: context.Canceled}
		}
		return SubResult{StopReason: res.StopReason, Err: res.Error}
	}
	if res.StopReason == "canceled" {
		return SubResult{StopReason: "canceled", Err: context.Canceled}
	}
	return SubResult{
		Text:       finalAssistantText(res),
		Rounds:     res.Rounds,
		StopReason: res.StopReason,
	}
}

// Spawn 实现 Spawner。
func (s *SubRunner) Spawn(ctx context.Context, prompt string) (string, error) {
	res := s.SpawnResult(ctx, prompt)
	if res.Err != nil {
		return "", res.Err
	}
	return res.Text, nil
}

// NewSync 构造同步子 agent（§B.2）。
//
// child 共享 parent 的 LLM client；工具注册表为 reg 的副本但剔除
// agent_spawn 自身，从而在结构上禁止嵌套（max_depth=1 的第一道
// 保险；第二道是 Execute 里的 depth 检查）。
func NewSync(llmClient llm.Client, reg *tool.Registry, model string, maxRounds, maxTokens int, temp *float64) *SubRunner {
	childReg := cloneRegistryWithoutSpawn(reg)
	childSys := agent.NewDefaultSystemPrompt(
		"你是 dsh 的子 agent。用最少的轮次完成给定任务，直接输出最终答案，不要再发起子任务。",
	)
	child := agent.NewLoopRunner(llmClient, childReg, childSys, model, maxRounds, maxTokens, temp)
	return &SubRunner{Child: child}
}

// cloneRegistryWithoutSpawn 复制 reg 中的所有工具（按插入顺序），
// 跳过 agent_spawn。reg 为 nil 时返回空注册表。
func cloneRegistryWithoutSpawn(reg *tool.Registry) *tool.Registry {
	out := tool.NewRegistry()
	if reg == nil {
		return out
	}
	for _, name := range reg.Names() {
		if name == "agent_spawn" {
			continue
		}
		if t, ok := reg.Get(name); ok {
			_ = out.Register(t)
		}
	}
	return out
}

// Attach 把 agent_spawn 工具注册到父 runner 的注册表（parent.go 的
// 父 runner 接入点）。父 runner 之后即可被 LLM 调起子 agent。
func Attach(reg *tool.Registry, llmClient llm.Client, model string, maxRounds, maxTokens int, temp *float64) (*SubRunner, error) {
	s := NewSync(llmClient, reg, model, maxRounds, maxTokens, temp)
	if err := reg.Register(NewSpawnTool(s)); err != nil {
		return nil, fmt.Errorf("subagent: register agent_spawn: %w", err)
	}
	return s, nil
}
