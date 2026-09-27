// Package types 包含 sdk-go 共享类型（事件 / 工具调用 / 工具结果等）。
//
// sdk-go 是 ds-go 的对外 SDK 入口：第三方 Go 应用通过此 SDK 调用
// 远端 ds-go Gateway 的能力。本包提供 wire 协议所共用的类型。
//
// 与 v4 Gateway SSE 帧结构保持兼容；JSON-RPC 与 HTTP 客户端共用
// 同一份类型，避免序列化分歧。
package types

import "time"

// FrameKind 标识一帧的类型。
type FrameKind string

const (
	KindEvent       FrameKind = "event"
	KindToolCall    FrameKind = "tool_call"
	KindToolResult  FrameKind = "tool_result"
	KindPermission  FrameKind = "permission"
	KindMessage     FrameKind = "message"
	KindHeartbeat   FrameKind = "heartbeat"
)

// Frame 是 wire 上的统一帧。
type Frame struct {
	Kind     FrameKind   `json:"kind"`
	Source   string      `json:"source,omitempty"`
	Phase    string      `json:"phase,omitempty"`
	Payload  any         `json:"payload,omitempty"`
	At       time.Time   `json:"at"`
}

// ToolCall 是 LLM 发起的工具调用。
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolResult 是工具执行结果。
type ToolResult struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
	IsError bool   `json:"is_error,omitempty"`
}

// SessionSendRequest 是 session.send 的入参。
type SessionSendRequest struct {
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
	Profile   string `json:"profile,omitempty"`
}

// SessionSendResponse 是 session.send 的响应。
type SessionSendResponse struct {
	SessionID string `json:"session_id"`
	Accepted  bool   `json:"accepted"`
}

// PermissionRequest 是工具调用前需要审批的请求。
type PermissionRequest struct {
	ID    string `json:"id"`
	Tool  string `json:"tool"`
	Args  string `json:"args"`
	Why   string `json:"why,omitempty"`
}

// PermissionDecision 是审批决策。
type PermissionDecision struct {
	ID     string `json:"id"`
	Approve bool  `json:"approve"`
	Reason  string `json:"reason,omitempty"`
}

// Event 是流式事件（与 v4 Gateway SSE 帧对齐）。
type Event struct {
	Source  string    `json:"source"`
	Phase   string    `json:"phase"`
	Payload any       `json:"payload,omitempty"`
	At      time.Time `json:"at"`
}
