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
