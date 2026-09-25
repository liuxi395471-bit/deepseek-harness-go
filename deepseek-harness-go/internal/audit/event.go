// Package audit 实现 DESIGN-v3 §E：JSONL 审计日志。
//
// 每行一个事件：{ts, session_id, event, ...}。默认 redact 模式下
// args 只记 SHA-256 hash；audit.full=true 才记录原文（§E.2）。
package audit

import "time"

// 事件类型常量（§E.1）。
const (
	EventToolCall        = "tool_call"
	EventApprovalDecision = "approval_decision"
	EventLLMCall         = "llm_call"
	EventHTTPRequest     = "http_request"
)

// Event 是一条审计记录。字段按事件类型选择性填充；未用字段被
// omitempty 省略，保证 JSONL 每行紧凑。
type Event struct {
	TS        time.Time `json:"ts"`
	SessionID string    `json:"session_id,omitempty"`
	Event     string    `json:"event"`

	// tool_call / approval_decision
	Round    int    `json:"round,omitempty"`
	Tool     string `json:"tool,omitempty"`
	ArgsHash string `json:"args_hash,omitempty"` // SHA-256 hex；redact 模式下唯一记录的参数痕迹
	ArgsRaw  string `json:"args_raw,omitempty"`  // 仅 audit.full=true 时记录
	Decision string `json:"decision,omitempty"`  // approve | deny
	Source   string `json:"source,omitempty"`    // 决策来源（approver 名称）

	// llm_call
	Model            string `json:"model,omitempty"`
	PromptTokens     int    `json:"prompt_tokens,omitempty"`
	CompletionTokens int    `json:"completion_tokens,omitempty"`

	// http_request
	Method string  `json:"method,omitempty"`
	Path   string  `json:"path,omitempty"`
	Status int     `json:"status,omitempty"`
	DurMS  float64 `json:"dur_ms,omitempty"`
}
