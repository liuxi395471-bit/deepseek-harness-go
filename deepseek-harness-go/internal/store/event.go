// Package store — 事件溯源（v4 §A）。
//
// event.go 定义 Session 内状态变化的不可变记录。每条 Event
// 对应一次原子状态变更；运行时字段（messages / usage / phase）
// 仅作为派生视图，由 projector 从事件流重放产生。
package store

import (
	"encoding/json"
	"fmt"
	"time"

	"deepseek-harness-go/internal/llm"
)

// EventType 区分 Session 内不同语义的状态变更。新增类型必须
// 追加在末尾以保持 wire format 兼容；删除或重排必须经过 major bump。
type EventType int

const (
	EventSessionBegin     EventType = iota // 会话开始，写 seq=0
	EventSystemPrompt                      // system 消息建立
	EventUserMessage                       // 用户输入
	EventAssistantMessage                  // 助手完整消息
	EventToolCall                          // 工具调用开始
	EventToolResult                        // 工具结果
	EventLLMCall                           // llm.chat span 边界（携带 usage）
	EventCompaction                        // 压缩发生
	EventPhaseChange                       // 阶段切换
	EventSessionEnd                        // 会话结束
)

// String 返回 EventType 的稳定名称（用于日志、wire format）。
func (t EventType) String() string {
	switch t {
	case EventSessionBegin:
		return "session.begin"
	case EventSystemPrompt:
		return "system.prompt"
	case EventUserMessage:
		return "user.message"
	case EventAssistantMessage:
		return "assistant.message"
	case EventToolCall:
		return "tool.call"
	case EventToolResult:
		return "tool.result"
	case EventLLMCall:
		return "llm.call"
	case EventCompaction:
		return "compaction"
	case EventPhaseChange:
		return "phase.change"
	case EventSessionEnd:
		return "session.end"
	default:
		return fmt.Sprintf("event.unknown(%d)", int(t))
	}
}

// Event 是 Session 内的一次不可变状态变更。
//
// Seq 在单 session 内单调递增；append-only；跨 session 不连续。
// Payload 是 JSON 序列化的类型化载荷（具体结构见各 PayloadXxx 类型）。
// Actor 标识事件来源："primary"（顶层 runner）或 "subagent:<id>"。
type Event struct {
	Sid       string    `json:"sid"`
	Seq       int64     `json:"seq"`
	Type      EventType `json:"type"`
	Timestamp time.Time `json:"ts"`
	Payload   []byte    `json:"payload"`
	Actor     string    `json:"actor,omitempty"`
}

// MarshalPayload 把 v 序列化为 JSON 后写入 Event.Payload。
// 重复设置会覆盖前值；通常由 AppendEvent 内部调用一次。
func (e *Event) MarshalPayload(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("store: marshal payload: %w", err)
	}
	e.Payload = b
	return nil
}

// UnmarshalPayload 把 Event.Payload 反序列化到 v。
// 调用方按 Type 字段决定具体类型；本方法不做类型分发。
func (e Event) UnmarshalPayload(v any) error {
	if len(e.Payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(e.Payload, v); err != nil {
		return fmt.Errorf("store: unmarshal payload (type=%s): %w", e.Type, err)
	}
	return nil
}

// SessionBeginPayload 是 EventSessionBegin 的载荷：会话创建时间。
type SessionBeginPayload struct {
	CreatedAt time.Time `json:"created_at"`
}

// SystemPromptPayload 是 EventSystemPrompt 的载荷。
type SystemPromptPayload struct {
	Content string `json:"content"`
}

// UserMessagePayload 是 EventUserMessage 的载荷。
type UserMessagePayload struct {
	Content  string `json:"content"`
	ToolCall *llm.ToolCall `json:"tool_call,omitempty"`
}

// AssistantMessagePayload 是 EventAssistantMessage 的载荷。
type AssistantMessagePayload struct {
	Message llm.Message `json:"message"`
}

// ToolCallPayload 是 EventToolCall 的载荷。
type ToolCallPayload struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolResultPayload 是 EventToolResult 的载荷。
type ToolResultPayload struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Result  string `json:"result"`
	IsError bool   `json:"is_error,omitempty"`
}

// LLMCallPayload 是 EventLLMCall 的载荷（span 边界 + usage）。
type LLMCallPayload struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// CompactionPayload 是 EventCompaction 的载荷。
type CompactionPayload struct {
	BeforeMsgs int `json:"before_msgs"`
	AfterMsgs  int `json:"after_msgs"`
	Strategy   string `json:"strategy"`
}

// PhaseChangePayload 是 EventPhaseChange 的载荷。
type PhaseChangePayload struct {
	Phase string `json:"phase"`
}

// SessionEndPayload 是 EventSessionEnd 的载荷。
type SessionEndPayload struct {
	StopReason string `json:"stop_reason"`
}
