package console

import (
	"context"
	"errors"
	"time"
)

// PluginItem 是控制台插件卡片数据。
type PluginItem struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind"` // "local" | "grpc" | "node" | "mcp" | "java-native"
	Source  string   `json:"source"`
	Version string   `json:"version,omitempty"`
	State   string   `json:"state"`         // "loaded" / "disabled" / "failed" / "discovered"
	Healthy bool     `json:"healthy"`
	Tools   []string `json:"tools"`         // 工具名列表（精简自 ToolSpecView）
	LastError string `json:"lastError,omitempty"`
}

// ModelItem 是控制台模型表格行。
type ModelItem struct {
	Channel   string `json:"channel"`
	Model     string `json:"model"`
	Protocol  string `json:"protocol"` // "openai-compatible" / "anthropic" / "gemini" / "deepseek"
	Active    bool   `json:"active"`
	BaseURL   string `json:"baseUrl,omitempty"`
	TimeoutMs int    `json:"timeoutMs,omitempty"`
	LastTestedAt *time.Time `json:"lastTestedAt,omitempty"`
}

// PingResult 是 /models/{channel}/ping 的响应。
type PingResult struct {
	OK        bool   `json:"ok"`
	LatencyMs int64  `json:"latencyMs"`
	Sample    string `json:"sample,omitempty"`
	Error     string `json:"error,omitempty"`
}

// TaskItem 是控制台任务表格行。
type TaskItem struct {
	ID         string `json:"id"`
	Code       string `json:"code,omitempty"`
	Title      string `json:"title"`
	State      string `json:"state"`
	Profile    string `json:"profile"`
	Owner      string `json:"owner,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Error      string `json:"error,omitempty"`
	Progress   *TaskProgress `json:"progress,omitempty"`
}

// TaskProgress 是 v6 jobs 提供的进度补充（v8 控制台可视化）。
type TaskProgress struct {
	JobID  string `json:"jobId"`
	Lines  int    `json:"lines"`
	Status string `json:"status"`
}

// ApprovalItem 是待审批卡片数据。
type ApprovalItem struct {
	ID         string         `json:"id"`
	Tool       string         `json:"tool"`
	Args       map[string]any `json:"args"`
	Profile    string         `json:"profile"`
	Reason     string         `json:"reason,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
}

// --- 后端接口 ---

// PluginBackend 是 plugin 列表 + 启停抽象。
type PluginBackend interface {
	List(ctx context.Context) ([]PluginItem, error)
	Enable(ctx context.Context, name string) error
	Disable(ctx context.Context, name string) error
}

// ModelBackend 是多渠道模型配置 + 连通性测试抽象。
type ModelBackend interface {
	List(ctx context.Context) ([]ModelItem, error)
	Update(ctx context.Context, channel string, item ModelItem) error
	Ping(ctx context.Context, channel string) (PingResult, error)
}

// TaskBackend 是 task 列表 + 取消抽象。
type TaskBackend interface {
	List(ctx context.Context, state string) ([]TaskItem, error)
	Cancel(ctx context.Context, id string) error
}

// JobsBackend 是 v6 jobs 进度补充。
type JobsBackend interface {
	Get(ctx context.Context, jobID string) (TaskProgress, error)
}

// ApprovalsBackend 是 v8 控制台独立维护的 pending 队列。
//
// Enqueue 由 agent runner 在 PolicyAsk 时调用（v8.1 接入；v8.0 仅在
// Test harness 中使用）。List 列出待办；Decide 把决策写到结果通道，
// 让 HTTPPollApprover resolver 收到。
type ApprovalsBackend interface {
	List(ctx context.Context) ([]ApprovalItem, error)
	Decide(ctx context.Context, id, decision string) error
	// Enqueue 在 v8.0 留 stub；返回 (id, channel)；id 用作 Resolver key。
	Enqueue(ctx context.Context, item ApprovalItem) (id string, result <-chan string, err error)
}

// EventStream 把 v4 Gateway SSE 暴露给控制台前端。
//
// Subscribe 返回 (events chan, cancel func)。events 在 cancel 时被 close。
// sources 与 server.GatewayRequest.Source 同语义。
type EventStream interface {
	Subscribe(ctx context.Context, sources []string) (<-chan EventFrame, func(), error)
}

// EventFrame 是 EventStream 输出的一帧（前端 EventSource 收到一行 data:）。
type EventFrame struct {
	Source    string         `json:"source"`
	Type      string         `json:"type"` // "delta" / "final" / "error" / "update"
	Payload   map[string]any `json:"payload,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

// StateBackend 是 UI 偏好的 KV 持久化（v8.0 in-memory + JSON 文件落盘）。
type StateBackend interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key, value string) error
}

// --- 哨兵错误 ---

// ErrBackendMissing 在 backend 字段为 nil 时返回。
var ErrBackendMissing = errors.New("console: backend not configured")

// ErrPluginNotFound 是 PluginBackend.Enable/Disable 命中未知 name。
var ErrPluginNotFound = errors.New("console: plugin not found")

// ErrModelNotFound 是 ModelBackend.Update/Ping 命中未知 channel。
var ErrModelNotFound = errors.New("console: model channel not found")

// ErrTaskNotFound 是 TaskBackend.Cancel 命中未知 id。
var ErrTaskNotFound = errors.New("console: task not found")

// ErrApprovalNotFound 是 ApprovalsBackend.Decide 命中未知 id。
var ErrApprovalNotFound = errors.New("console: approval not found")
