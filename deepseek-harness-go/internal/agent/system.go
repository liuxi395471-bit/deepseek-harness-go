package agent

import (
	"fmt"
	"strings"

	"deepseek-harness-go/internal/tool"
)

// SystemPromptBuilder 把当前注册表转换为会话的 system 消息。
// 实现必须是可重复调用的（多次调用应保持稳定）。
type SystemPromptBuilder interface {
	Build(reg *tool.Registry) string
}

// DefaultSystemPrompt 输出一个中文的、面向 ReAct 的 prompt，
// 并内嵌一份可用工具清单。用户自定义通过 AgentConfig.SystemPrompt 传入。
type DefaultSystemPrompt struct {
	Override string
}

// NewDefaultSystemPrompt 用可选的用户覆写构造一个 builder。
func NewDefaultSystemPrompt(override string) SystemPromptBuilder {
	return DefaultSystemPrompt{Override: override}
}

// Build 返回给定注册表对应的 system prompt。
func (d DefaultSystemPrompt) Build(reg *tool.Registry) string {
	if d.Override != "" {
		return d.Override
	}
	lines := []string{
		"你是 dsh，一个简洁可靠的 CLI 助手，使用 ReAct 循环与工具协作。",
		"可用工具（按需调用，不需要时不必）：",
	}
	for _, spec := range reg.Specs() {
		lines = append(lines, fmt.Sprintf("- %s: %s", spec.Function.Name, spec.Function.Description))
	}
	lines = append(lines,
		"工具失败（[ERROR] 前缀）时，请把它当作新信息继续推理，而不是重新尝试同一参数。",
		"保持回答简洁，必要时使用工具获取事实。",
	)
	return strings.Join(lines, "\n")
}
