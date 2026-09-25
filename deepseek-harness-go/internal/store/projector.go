// Package store — 投影器（v4 §A.5）。
//
// projector.go 提供三种内置投影：
//   - MessagesProjector：把 SessionBegin→SystemPrompt→UserMessage→
//     AssistantMessage→ToolCall/ToolResult 投影为 []llm.Message
//   - UsageProjector：累加 LLMCall 事件的 token
//   - PhaseProjector：记录最后一次 PhaseChange
//
// ProjectAll 是便捷入口，遍历 events 一次并把每条事件分发给三
// 个投影。Messages 必须以 ToolCall 消息 + 后续 Tool 结果消息的形式
// 输出（与 v3 Append 的格式一致），保证 Runner 投影结果与原 messages
// 表字节级等价。
package store

import (
	"fmt"

	"deepseek-harness-go/internal/llm"
)

// MessagesState 是 MessagesProjector 的派生状态，等价于 v3 Session.Messages。
type MessagesState []llm.Message

// Apply 把事件转换为 llm.Message 追加到状态。
func (p *MessagesProjector) Apply(ev Event, state ProjectionState) error {
	msgs, ok := state.(*MessagesState)
	if !ok {
		return fmt.Errorf("projector messages: bad state type %T (want *MessagesState)", state)
	}
	switch ev.Type {
	case EventSessionBegin, EventSessionEnd, EventLLMCall, EventCompaction:
		// 这些事件不直接产生 llm.Message，跳过。
		return nil
	case EventSystemPrompt:
		var pl SystemPromptPayload
		if err := ev.UnmarshalPayload(&pl); err != nil {
			return err
		}
		*msgs = append(*msgs, llm.Message{Role: llm.RoleSystem, Content: pl.Content})
	case EventUserMessage:
		var pl UserMessagePayload
		if err := ev.UnmarshalPayload(&pl); err != nil {
			return err
		}
		*msgs = append(*msgs, llm.Message{Role: llm.RoleUser, Content: pl.Content})
	case EventAssistantMessage:
		var pl AssistantMessagePayload
		if err := ev.UnmarshalPayload(&pl); err != nil {
			return err
		}
		*msgs = append(*msgs, pl.Message)
	case EventToolCall:
		var pl ToolCallPayload
		if err := ev.UnmarshalPayload(&pl); err != nil {
			return err
		}
		*msgs = append(*msgs, llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID:   pl.ID,
				Type: "function",
				Function: llm.ToolCallFunc{
					Name:      pl.Name,
					Arguments: pl.Arguments,
				},
			}},
		})
	case EventToolResult:
		var pl ToolResultPayload
		if err := ev.UnmarshalPayload(&pl); err != nil {
			return err
		}
		*msgs = append(*msgs, llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: pl.ID,
			Name:       pl.Name,
			Content:    pl.Result,
		})
	case EventPhaseChange:
		// 阶段变化不发 llm.Message；phase 由 PhaseProjector 单独维护。
		return nil
	default:
		// 未知类型视为 noop，保留前向兼容。
		return nil
	}
	return nil
}

// Name 返回投影名称。
func (p *MessagesProjector) Name() string { return "messages" }

// UsageState 是 UsageProjector 的派生状态。
type UsageState llm.Usage

// Apply 累加 LLMCall 事件的 token。
func (p *UsageProjector) Apply(ev Event, state ProjectionState) error {
	if ev.Type != EventLLMCall {
		return nil
	}
	var pl LLMCallPayload
	if err := ev.UnmarshalPayload(&pl); err != nil {
		return err
	}
	if u, ok := state.(*UsageState); ok {
		u.PromptTokens += pl.PromptTokens
		u.CompletionTokens += pl.CompletionTokens
		u.TotalTokens += pl.TotalTokens
	}
	return nil
}

// Name 返回投影名称。
func (p *UsageProjector) Name() string { return "usage" }

// PhaseState 是 PhaseProjector 的派生状态：当前 phase 字符串。
type PhaseState string

// Apply 覆盖当前 phase。
func (p *PhaseProjector) Apply(ev Event, state ProjectionState) error {
	if ev.Type != EventPhaseChange {
		return nil
	}
	var pl PhaseChangePayload
	if err := ev.UnmarshalPayload(&pl); err != nil {
		return err
	}
	if s, ok := state.(*PhaseState); ok {
		*s = PhaseState(pl.Phase)
	}
	return nil
}

// Name 返回投影名称。
func (p *PhaseProjector) Name() string { return "phase" }

// MessagesProjector 把事件流投影为 messages。
type MessagesProjector struct{}

// UsageProjector 把事件流投影为 usage。
type UsageProjector struct{}

// PhaseProjector 把事件流投影为当前 phase。
type PhaseProjector struct{}

// ProjectorSet 聚合三个内置投影，方便一次遍历多次 Apply。
type ProjectorSet struct {
	Messages *MessagesProjector
	Usage    *UsageProjector
	Phase    *PhaseProjector
}

// DefaultProjectorSet 返回带三个内置投影的 set。
func DefaultProjectorSet() ProjectorSet {
	return ProjectorSet{Messages: &MessagesProjector{}, Usage: &UsageProjector{}, Phase: &PhaseProjector{}}
}

// ProjectResult 是 ProjectAll 的输出：派生 messages + usage + phase。
type ProjectResult struct {
	Messages MessagesState
	Usage    UsageState
	Phase    PhaseState
}

// ProjectAll 把 events 应用到 set 内的全部投影。events 顺序敏感。
// 任何投影 Apply 失败立即返回（不部分提交）。
func ProjectAll(events []Event, set ProjectorSet) (ProjectResult, error) {
	res := ProjectResult{
		Messages: MessagesState{},
		Usage:    UsageState{},
		Phase:    PhaseState(""),
	}
	for i, ev := range events {
		if set.Messages != nil {
			if err := set.Messages.Apply(ev, &res.Messages); err != nil {
				return ProjectResult{}, fmt.Errorf("project events[%d] type=%s messages: %w", i, ev.Type, err)
			}
		}
		if set.Usage != nil {
			if err := set.Usage.Apply(ev, &res.Usage); err != nil {
				return ProjectResult{}, fmt.Errorf("project events[%d] type=%s usage: %w", i, ev.Type, err)
			}
		}
		if set.Phase != nil {
			if err := set.Phase.Apply(ev, &res.Phase); err != nil {
				return ProjectResult{}, fmt.Errorf("project events[%d] type=%s phase: %w", i, ev.Type, err)
			}
		}
	}
	return res, nil
}
