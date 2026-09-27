// Package task 为 dsh 提供任务编排原语（v6 P6-1）。
//
// 一个 Task 是把"一条用户 prompt"包装成可在后台调度、可取消、可查询
// 状态的工作单元。Task 与 Session 是松耦合关系：Task 可以关联一个
// 已存在的 session（沿用其消息历史），也可以独立存在（纯后台任务）。
//
// 设计要点：
//   - Task 自带 SQLite 表（独立 db 文件或共享 db 通过注入 *sql.DB）；
//   - 状态机：Pending → Running → (Completed | Failed | Canceled)；
//   - 调度复用 v4 agent.LoopRunner（已有 ctx 取消 + 事件流）；
//   - 用 v5 store.Lease 防止同 task 被并发重复启动。
//
// 与 ds-java v0.1.7 的对齐：相当于 cases.task.SubmitTaskRequest 的
// 最小子集（v6 决策），完整 filter chain 延后到 v6.1。
package task

import (
	"context"
	"errors"
	"time"
)

// State 是 Task 生命周期状态。
//
// 状态迁移由 Executor 内部驱动，调用方只读：
//
//	Pending → Running → Completed        正常完成
//	Pending → Running → Failed            执行异常
//	Pending → Running → Canceled          ctx 取消或显式 Cancel
//	Pending → Canceled                    排队阶段取消（v6 暂无队列语义）
type State int

const (
	StatePending State = iota
	StateRunning
	StateCompleted
	StateFailed
	StateCanceled
)

// String 返回状态名（用于日志 / JSON 序列化）。
func (s State) String() string {
	switch s {
	case StatePending:
		return "pending"
	case StateRunning:
		return "running"
	case StateCompleted:
		return "completed"
	case StateFailed:
		return "failed"
	case StateCanceled:
		return "canceled"
	default:
		return "unknown"
	}
}

// MarshalJSON / UnmarshalJSON 让 State 直接在 JSON 中以字符串形式出现。
func (s State) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

// UnmarshalJSON 反向解析。
func (s *State) UnmarshalJSON(b []byte) error {
	switch string(b) {
	case `"pending"`:
		*s = StatePending
	case `"running"`:
		*s = StateRunning
	case `"completed"`:
		*s = StateCompleted
	case `"failed"`:
		*s = StateFailed
	case `"canceled"`:
		*s = StateCanceled
	default:
		return errors.New("task: unknown state " + string(b))
	}
	return nil
}

// Task 是单个调度单元。
//
// 输入：
//   - Input：用户 prompt（必填）；
//   - Profile：执行 profile（默认 "headless"）；
//   - Permission：权限模板名（可空，使用默认）；
//   - SessionID：复用已有 session 的消息历史（可空：纯后台任务）。
//
// 输出（运行时填充）：
//   - State / Error / FinishedAt / Usage。
type Task struct {
	ID         string    `json:"id"`
	Code       string    `json:"code,omitempty"`
	Title      string    `json:"title"`
	Input      string    `json:"input"`
	SessionID  string    `json:"session_id,omitempty"`
	State      State     `json:"state"`
	Profile    string    `json:"profile"`
	Permission string    `json:"permission,omitempty"`
	Owner      string    `json:"owner,omitempty"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Usage      Usage     `json:"usage"`
}

// Usage 是该 Task 的累计 token 用量（与 llm.Usage 同形）。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

	// Filter 是 List 的查询条件。
	//
	// 零值 Filter{} 表示"全部"。要按状态过滤用 States；Filter.State
	// 保留为单状态简写（与 States 互斥，States 优先）。
	type Filter struct {
	State    State   // 单状态过滤（StatePending == 零值时也可能误命中；推荐用 States）
	States   []State // 多状态 IN 过滤；优先于 State
	Owner    string
	SessionID string
	Code     string
	Limit    int // 0 = 默认 50
	Offset   int
}

// SubmitRequest 是 Executor.Submit 的入参。
type SubmitRequest struct {
	Code       string
	Title      string
	Input      string
	SessionID  string
	Profile    string
	Permission string
	Owner      string
}

// Executor 是 Task 的运行时契约。
//
// 并发安全：调用方可并发 Submit / Cancel / Get / List / Retry。
type Executor interface {
	// Submit 创建并启动一个 Task（同步等待状态切到 Running 后立即返回）。
	// 内部启动 goroutine 跑 loop；调用方通过 Get / List 跟踪进度。
	Submit(ctx context.Context, req SubmitRequest) (*Task, error)

	// Cancel 中止正在运行（Pending / Running）的 Task；幂等。
	// 若 Task 已处于终止态（Completed / Failed / Canceled）则返回 nil。
	Cancel(ctx context.Context, taskID string) error

	// Retry 重跑已终止的 Task（Failed / Canceled），复用其 Input。
	// 实际行为：以原 Task 为基础分配新 id，Insert 为 Pending，
	// 启动 goroutine 执行（=一次"重新提交"）。
	// 已处于 Running / Pending 返回 ErrAlreadyRunning；找不到返回 ErrNotFound。
	Retry(ctx context.Context, taskID string) (*Task, error)

	// Get 返回 Task 当前快照。taskID 不存在返回 ErrNotFound。
	Get(ctx context.Context, taskID string) (*Task, error)

	// List 按 filter 返回 Task 列表（按 UpdatedAt 降序）。
	List(ctx context.Context, filter Filter) ([]*Task, error)
}

// ErrAlreadyRunning 在 Retry 一个仍在 Running/Pending 的 Task 时返回。
var ErrAlreadyRunning = errors.New("task: already running")

// ErrNotFound 在 taskID 未找到时返回。
var ErrNotFound = errors.New("task: not found")

// ErrAlreadyTerminal 在 Cancel 一个已终止 Task 时返回（仍视为成功）。
var ErrAlreadyTerminal = errors.New("task: already terminal")

// Store 是 Executor 依赖的持久化接口。
//
// 实现：SQLiteStore（见 sqlite_store.go）；测试用 MemoryStore。
type Store interface {
	Insert(ctx context.Context, t *Task) error
	Update(ctx context.Context, t *Task) error
	Get(ctx context.Context, id string) (*Task, error)
	List(ctx context.Context, filter Filter) ([]*Task, error)
}
