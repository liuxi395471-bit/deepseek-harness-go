package console

import (
	"context"
	"errors"
	"time"
)

// SessionItem 是控制台会话列表项（精简字段）。
type SessionItem struct {
	SID         string    `json:"sid"`
	Title       string    `json:"title"`
	Model       string    `json:"model"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Rounds      int       `json:"rounds"`
	Preview     string    `json:"preview"`
}

// SessionDetail 是 /sessions/{sid} 的完整响应。
type SessionDetail struct {
	SessionItem
	Messages []any `json:"messages"`
	Usage    struct {
		PromptTokens     int `json:"promptTokens"`
		CompletionTokens int `json:"completionTokens"`
		TotalTokens      int `json:"totalTokens"`
		// v8.1: cache + reasoning 细分
		CacheReadTokens     int     `json:"cacheReadTokens,omitempty"`
		CacheWriteTokens    int     `json:"cacheWriteTokens,omitempty"`
		ReasoningTokens     int     `json:"reasoningTokens,omitempty"`
		CacheHitRate        float64 `json:"cacheHitRate,omitempty"`
		UncachedInputTokens int     `json:"uncachedInputTokens,omitempty"`
	} `json:"usage"`
	// Ratings 是 v8.1 P1 引入的 msgSeq → rating 映射（+1 good / -1 bad）。
	Ratings map[int64]int `json:"ratings,omitempty"`
}

// SendResult 是发送消息的同步返回（仅当 source 一次性返回时使用）。
type SendResult struct {
	SessionID string `json:"sid"`
	Rounds    int    `json:"rounds"`
	StopReason string `json:"stopReason"`
}

// SessionBackend 是控制台与 sessions 之间的抽象。
//
// 接口契约：
//   - List: 倒序；limit<=0 表示全部；cursor 由调用方生成（按 sid）。
//   - Get: 返回完整 messages + usage；sid 不存在返回 ErrSessionNotFound。
//   - Create: 分配新 sid 并返回。
//   - Delete: 幂等。
//   - Send: 阻塞直到 loop 结束；调用方把返回再写入 Store。
//   - SendStream: 流式推送（SendResult + 多个中间帧）。
//   - EditMessage: 替换 user/system 消息的 content（v8 P0）。
//   - DeleteMessage: 删除消息（v8 P0）。
type SessionBackend interface {
	List(ctx context.Context, limit int, cursor string) ([]SessionItem, error)
	Get(ctx context.Context, sid string) (SessionDetail, error)
	Create(ctx context.Context, title, model string) (SessionItem, error)
	Delete(ctx context.Context, sid string) error
	Send(ctx context.Context, sid, content string) (SendResult, error)
	// SendStream 推送消息到 sid；out 是 SSE frame 通道；调用方负责 close。
	SendStream(ctx context.Context, sid, content string, out chan<- SessionFrame) error
	// EditMessage 修改 sid 中 seq=msgSeq 的 user/system 消息内容。
	EditMessage(ctx context.Context, sid string, msgSeq int64, newContent string) error
	// DeleteMessage 删除 sid 中 seq=msgSeq 的消息（保留空位）。
	DeleteMessage(ctx context.Context, sid string, msgSeq int64) error
	// EventsSince 返回 sid 中 seq > since 的 events（升序）；未实现时返回
	// nil, nil（callers 应 fall back to SSE）。v8.1 Spill 续传接口。
	EventsSince(ctx context.Context, sid string, since int64) (events []SessionEvent, lastSeq int64, err error)

	// SetMessageRating 评分（+1 good / -1 bad / 0 clear）；v8.1 P1。
	SetMessageRating(ctx context.Context, sid string, msgSeq int64, rating int, comment string) error

	// ListMessageRatings 返回 sid 全部评分（seq → rating）。
	ListMessageRatings(ctx context.Context, sid string) (map[int64]int, error)

	// ExportMarkdown 把 sid 的对话导出为 Markdown。
	ExportMarkdown(ctx context.Context, sid string) (string, error)

	// ExportJSONL 导出为 JSONL（每行一个 message json）。
	ExportJSONL(ctx context.Context, sid string) (string, error)

	// Regenerate 重生成 sid 中 seq=msgSeq 的 user 消息对应的 assistant 回复。
	// 流程：删除从 msgSeq 起所有 assistant/tool 消息，然后按 user 消息内容重新跑 Send。
	Regenerate(ctx context.Context, sid string, msgSeq int64, out chan<- SessionFrame) error
}

// SessionEvent 是 EventsSince 返回的单条事件（投影自 store.Event）。
type SessionEvent struct {
	Seq     int64          `json:"seq"`
	Type    int            `json:"type"`
	TS      time.Time      `json:"ts"`
	Payload map[string]any `json:"payload"`
	Actor   string         `json:"actor,omitempty"`
}

// SessionFrame 是 SendStream 输出的一帧（与 server/eventToFrame 同形）。
type SessionFrame struct {
	Event string         // "assistant_delta" / "tool_call_start" / ...
	Data  map[string]any // JSON-可序列化载荷
}

// ErrSessionNotFound 在 Get/Delete 命中未知 sid 时由 backend 返回。
//
// backend 实现应当返回 errors.Is(err, console.ErrSessionNotFound) 可
// 识别的错误；console 包在 handler 内做映射。
var ErrSessionNotFound = errors.New("console: session not found")

// ErrSessionMissing 在 SessionBackend 字段为 nil 时返回。
var ErrSessionMissing = errors.New("console: session backend missing")
