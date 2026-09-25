// Package agent 实现 ReAct 循环运行器（runner）。
//
// 运行器是唯一的编排点：它持有会话历史、与 LLM 客户端通信、调度工具调用，
// 并向外发出事件供 REPL 渲染。它不持有任何终端/UI 状态。
package agent

import (
	"deepseek-harness-go/internal/llm"
	"time"
)

// Phase 是作为事件发出的粗粒度状态指示。
// 订阅者可以借此实现进度 UI；循环的正确性
// 不依赖于消费者是否读取这些事件。
type Phase int

const (
	PhaseInit     Phase = iota // 循环进入
	PhaseLLMCall              // 即将调用 LLM
	PhaseToolExec             // 即将执行工具
	PhaseLLMDone              // （保留）助手消息已完全读取
	PhaseStopped              // 循环正常退出
	PhaseError                // 循环异常退出
)

func (p Phase) String() string {
	switch p {
	case PhaseInit:
		return "init"
	case PhaseLLMCall:
		return "llm_call"
	case PhaseToolExec:
		return "tool_exec"
	case PhaseLLMDone:
		return "llm_done"
	case PhaseStopped:
		return "stopped"
	case PhaseError:
		return "error"
	default:
		return "unknown"
	}
}

// Event 是运行器在处理单个 prompt 期间发出的任何事件。
// 初版：运行器不会在本包之外产生事件，
// 因此无需私有 seal 方法。EventTag 用于便于调试的格式化，
// 后续可能演化为判别字段。
type Event interface {
	EventTag() string
}

// PhaseChange 通知一次阶段转换。并非每次转换都会被发出
// （例如 LLMCall → LLMCall 的重试会被合并）。
type PhaseChange struct {
	Phase Phase
	At    time.Time
}

func (PhaseChange) EventTag() string { return "phase_change" }

// AssistantDelta 为 M5 流式保留；初版不会发出。
type AssistantDelta struct {
	Text string
}

func (AssistantDelta) EventTag() string { return "assistant_delta" }

// AssistantMessage 是非流式下的"最终助手轮次"事件。
type AssistantMessage struct {
	Content   string
	ToolCalls []llm.ToolCall
}

func (AssistantMessage) EventTag() string { return "assistant_message" }

// ToolCallStart 通知运行器即将调用某个工具。
type ToolCallStart struct {
	Call llm.ToolCall
}

func (ToolCallStart) EventTag() string { return "tool_call_start" }

// ToolResult 报告工具返回了什么（或调用失败）。
type ToolResult struct {
	CallID  string
	Name    string
	Content string
	IsError bool
	Took    time.Duration
}

func (ToolResult) EventTag() string { return "tool_result" }

// LoopDone 在成功路径上最后发出。
type LoopDone struct {
	Rounds   int
	Messages []llm.Message
}

func (LoopDone) EventTag() string { return "loop_done" }

// LoopError 通知循环发生致命失败；仅当循环以 "error" StopReason 退出时发出。
type LoopError struct {
	Err   error
	Phase Phase
}

func (LoopError) EventTag() string { return "loop_error" }

// Compacted 通知一次上下文压缩（DESIGN-v3 §D）。Before/After 是
// 压缩前后的消息条数。压缩只影响本轮发给 LLM 的消息序列，
// 会话存储保持完整。
type Compacted struct {
	Before int
	After  int
}

func (Compacted) EventTag() string { return "compacted" }
