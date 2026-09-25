// Package store 为 agent 会话提供持久化存储。
//
// Session 是单次对话的持久化记录：到目前为止交换的消息、累计用量，
// 以及用于列表展示的预览。Store 接口支持两种后端——MapStore（内存，
// 仅测试用）和 SQLiteStore（默认磁盘存储）——在构造时选择。
//
// 迁移/淘汰策略（DESIGN-v2 §A.3.7）：v1 没有历史记录；v2 在
// session.enabled=true 时加入持久化。当 session.enabled=false（或 v1
// 接线中 Store==nil）时，行为与 v1 完全一致。
package store

import (
	"context"
	"errors"
	"time"

	"deepseek-harness-go/internal/llm"
)

// ErrNotFound 在 Load/Append/UpdateUsage 中当 session id 不存在时返回。
// 调用方必须用 errors.Is 而非 == 判断。
var ErrNotFound = errors.New("store: session not found")

// Session 是一次对话的持久化状态。
//
// Messages 是有序的：索引 0 为 system 提示，之后 user/assistant/tool
// 交替。Append 不会重排；Load 总是按追加顺序返回。
//
// UsageTotal 是会话中所有轮次的累计和；调用方通过 UpdateUsage 传入
// 正的增量来累加。
//
// Preview 在 Begin 时从第一条 user 消息设置（截断到 previewLen 个
// rune）。供 List 展示使用。
type Session struct {
	ID         string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Rounds     int
	Preview    string
	Messages   []llm.Message
	UsageTotal llm.Usage
}

// Store 是支撑 Begin/Append/Load/List/UpdateUsage/Close 的契约。
//
// 所有方法都可安全地被多个 goroutine 并发使用，包括跨会话（不同 ID）
// 和单会话内部（不同 goroutine 对同一 ID 调用 Append）。map 后端使用
// 每会话互斥锁；SQLite 后端依赖数据库驱动自身的串行化。
//
// 实现：
//   - MapStore：进程内 map；零外部依赖；用于测试。
//   - SQLiteStore：通过 modernc.org/sqlite（纯 Go）的磁盘 SQLite。
//
// 调用方应在构造后 defer Close。
type Store interface {
	// Begin 分配新的 session id（UUID），插入空行，并返回初始化后的
	// Session。预期第一次 Append 添加 system 消息，第二次添加用户提示。
	Begin(ctx context.Context) (Session, error)

	// Append 将 msg 追加到 session id。若 id 不存在则返回 ErrNotFound。
	// seq 由后端按每会话单调递增的顺序分配。
	Append(ctx context.Context, id string, msg llm.Message) error

	// Load 返回包含所有消息的完整 Session。若 id 不存在则返回
	// ErrNotFound。
	Load(ctx context.Context, id string) (Session, error)

	// List 返回从 offset 开始的至多 limit 个会话，按 UpdatedAt 降序
	// 排列。limit <= 0 表示"全部"。返回切片中的会话只带元数据
	// （ID/CreatedAt/UpdatedAt/Rounds/Preview），Messages 为空——调用方
	// 必须调用 Load 获取历史。
	List(ctx context.Context, limit, offset int) ([]Session, error)

	// UpdateUsage 将 delta 加到会话的 UsageTotal。若 id 不存在则返回
	// ErrNotFound。
	UpdateUsage(ctx context.Context, id string, delta llm.Usage) error

	// Close 释放资源。可安全地多次调用。
	Close() error
}

// EventStore 是 v4 §A 引入的事件溯源扩展接口。实现 AppendEvent/
// ReadEvents/GetLastSeq 三个方法，向 AppendEvent 调用方提供完整的
// Session 事件序列；GetLastSeq 返回当前已分配的最大 seq（用于断点
// 续传或投影重建时的 since 起点）。
//
// 兼容策略：v3 Store 的实现未必支持事件流；调用方用 AsEventStore
// 做类型断言降级，未实现时回退到 v3 Load + Append 路径。
type EventStore interface {
	Store

	// AppendEvent 把事件追加到 sid 对应会话的事件流。返回分配的
	// seq（从 0 开始，单调递增）。若 id 不存在返回 ErrNotFound。
	AppendEvent(ctx context.Context, sid string, ev Event) (int64, error)

	// ReadEvents 返回 sid 的事件序列，from 起始 seq 之后的事件按 seq
	// 升序排列。from=0 表示从头开始。事件类型取决于实现，可能在
	// Append 之外补出 system_prompt 等额外事件；调用方应按 Event.Type
	// 分发。找不到 sid 返回 ErrNotFound。
	ReadEvents(ctx context.Context, sid string, from int64) ([]Event, error)

	// GetLastSeq 返回 sid 已分配的最大 seq；若 sid 未知返回
	// ErrNotFound。AppendEvent 之前调用得到 -1（无事件）；AppendEvent
	// 之后得到该 sid 当前的最高 seq。
	GetLastSeq(ctx context.Context, sid string) (int64, error)
}

// AsEventStore 把 s 转换为 EventStore；s 未实现事件流接口时返回
// (nil, false)，调用方应回退到 v3 Load/Append 路径。
func AsEventStore(s Store) (EventStore, bool) {
	if s == nil {
		return nil, false
	}
	if es, ok := s.(EventStore); ok {
		return es, true
	}
	return nil, false
}

// ProjectionStore 是 v4 §A.5 引入的可选接口：让 Store 持有自己的
// 投影缓存，使 cache 命中由 Store 内部维护，避免调用方每次手动
// 传 cache。v4 阶段不强制实现；Runner 在 EventStore 提供时自动使用，
// 否则构造一个进程内的 MemoryProjectionCache。
type ProjectionStore interface {
	EventStore

	// Project 派生 sid 的指定投影。name 为 "messages" / "usage" /
	// "phase" 等。命中 cache 时直接返回；未命中则从 events 重放
	// 并 Put 回 cache。
	Project(ctx context.Context, sid, name string) (ProjectionState, error)
}

// previewLen 是预览的最大长度（rune 数）。
const previewLen = 80

// truncatePreview 返回 s 的前 n 个 rune，截断时附加省略号。
// 两种后端都用它，保证行为一致。
func truncatePreview(s string) string {
	if utf8RuneCount(s) <= previewLen {
		return s
	}
	return truncateRunes(s, previewLen) + "…"
}
