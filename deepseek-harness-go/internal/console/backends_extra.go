package console

import (
	"context"
	"errors"
	"io"
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

// AuditRecord 是审计导出的一行（与 audit.Event 字段对齐）。
//
// 字段集与 audit.Event 完全相同（避免 console 反向依赖 audit 包）。
type AuditRecord struct {
	TS        time.Time `json:"ts"`
	SessionID string    `json:"sessionId,omitempty"`
	Event     string    `json:"event"`
	Round     int       `json:"round,omitempty"`
	Tool      string    `json:"tool,omitempty"`
	ArgsHash  string    `json:"argsHash,omitempty"`
	ArgsRaw   string    `json:"argsRaw,omitempty"`
	Decision  string    `json:"decision,omitempty"`
	Source    string    `json:"source,omitempty"`
	Model     string    `json:"model,omitempty"`
	Method    string    `json:"method,omitempty"`
	Path      string    `json:"path,omitempty"`
	Status    int       `json:"status,omitempty"`
	DurMS     float64   `json:"durMs,omitempty"`
}

// --- 后端接口 ---

// PluginBackend 是 plugin 列表 + 启停 + 安装/卸载抽象。
type PluginBackend interface {
	List(ctx context.Context) ([]PluginItem, error)
	Enable(ctx context.Context, name string) error
	Disable(ctx context.Context, name string) error
	// Install 安装 installRoot 下的插件（installer.Scan 一遍）；name 必填。
	// 已存在同 name 返回 ErrPluginExists。
	Install(ctx context.Context, name, source string) error
	// Uninstall 卸载 name（删除 status + 文件）；不存在返回 ErrPluginNotFound。
	Uninstall(ctx context.Context, name string) error
}

// ModelBackend 是多渠道模型配置 + 连通性测试 + 新增/卸载抽象。
type ModelBackend interface {
	List(ctx context.Context) ([]ModelItem, error)
	Update(ctx context.Context, channel string, item ModelItem) error
	// Create 新增渠道；channel 已存在返回 ErrModelExists。
	Create(ctx context.Context, item ModelItem) error
	// Remove 卸载渠道；不存在返回 ErrModelNotFound。
	Remove(ctx context.Context, channel string) error
	Ping(ctx context.Context, channel string) (PingResult, error)
}

// TaskBackend 是 task 列表 / 取消 / 提交 / 重试抽象。
type TaskBackend interface {
	List(ctx context.Context, state string) ([]TaskItem, error)
	Cancel(ctx context.Context, id string) error
	Submit(ctx context.Context, title, input, profile string) (TaskItem, error)
	// Retry 重跑已终止任务。Running/Pending 返回 ErrTaskRunning。
	Retry(ctx context.Context, id string) (TaskItem, error)
}

// JobsBackend 是 v6 jobs 进度补充。
type JobsBackend interface {
	Get(ctx context.Context, jobID string) (TaskProgress, error)
}

// AuditBackend 提供审计日志查询 + 导出（v8 P0）。
//
// AuditFilter 是 v8.1 扩展的审计查询参数（since / event / sid）。
type AuditFilter struct {
	Since time.Time // 仅返回 ts > Since；零值 = 不过滤
	Event string    // event 名精确匹配；空 = 不过滤
	SID   string    // sessionId 精确匹配；空 = 不过滤
	Limit int       // > 0 时生效
}

// Query 返回 limit 条记录（按时间倒序；since 留 v8.1）。
// QueryWith 应用 AuditFilter；Limit==0 时默认 200。
// Export 写出最近的 exportLimit 条 JSONL 到 w（HTTP 由 handler 流式写出）。
// ExportCSV 写出 CSV 表头 + 行（流式）。
type AuditBackend interface {
	Query(ctx context.Context, limit int) ([]AuditRecord, error)
	QueryWith(ctx context.Context, f AuditFilter) ([]AuditRecord, error)
	Export(ctx context.Context, w io.Writer, exportLimit int) error
	ExportCSV(ctx context.Context, w io.Writer, exportLimit int) error
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

// ScheduleItem 是 ScheduleBackend 的单条记录（v8.1）。
type ScheduleItem struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Cron       string    `json:"cron"`
	Action     string    `json:"action"` // JSON: {"type":"agent_run","prompt":"..."} | {"type":"webhook","webhookId":N}
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"createdAt"`
	LastRunAt  time.Time `json:"lastRunAt,omitempty"`
	NextRunAt  time.Time `json:"nextRunAt,omitempty"`
	LastStatus string    `json:"lastStatus,omitempty"` // "ok" | "failed" | ""
	LastError  string    `json:"lastError,omitempty"`
}

// ScheduleBackend 提供 schedule CRUD + run-now + worker（v8.1）。
//
// 接口形态匹配 internal/schedule.Store：
//   - 接受 individual 字段，避免 FrontEnd 直接绑死 struct tag。
//   - 每方法额外接受 context；Run 返回 schedule item 含最新 lastRunAt/Status。
type ScheduleBackend interface {
	List(ctx context.Context) ([]ScheduleItem, error)
	Create(ctx context.Context, item ScheduleItem) (ScheduleItem, error)
	// Update 接收 patch item（id 字段可空；空字段保留原值；enabled 用 *bool）。
	Update(ctx context.Context, id string, item ScheduleItem) (ScheduleItem, error)
	Delete(ctx context.Context, id string) error
	Run(ctx context.Context, id string) (ScheduleItem, error)
}

// WebhookItem 是 WebhookBackend 的单条记录（v8.1）。
type WebhookItem struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	URL        string    `json:"url"`
	Secret     string    `json:"secret,omitempty"` // 仅创建/更新时返回
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"createdAt"`
	LastStatus int       `json:"lastStatus,omitempty"`
	LastError  string    `json:"lastError,omitempty"`
	LastDeliveredAt time.Time `json:"lastDeliveredAt,omitempty"`
}

// WebhookDelivery 是 webhook 投递历史的一行（v8.1）。
type WebhookDelivery struct {
	ID          string    `json:"id"`
	WebhookID   string    `json:"webhookId"`
	Payload     string    `json:"payload"`
	StatusCode  int       `json:"statusCode"`
	Attempt     int       `json:"attempt"`
	OK          bool      `json:"ok"`
	DeliveredAt time.Time `json:"deliveredAt"`
	Error       string    `json:"error,omitempty"`
}

// WebhookBackend 提供 webhook CRUD + delivery + test（v8.1）。
//
// 接口形态匹配 internal/webhook.Dispatcher。
type WebhookBackend interface {
	List(ctx context.Context) ([]WebhookItem, error)
	Create(ctx context.Context, item WebhookItem) (WebhookItem, error)
	Update(ctx context.Context, id string, item WebhookItem) (WebhookItem, error)
	Delete(ctx context.Context, id string) error
	Dispatch(ctx context.Context, id, payload string) (WebhookDelivery, error)
	ListDeliveries(ctx context.Context, webhookID string, limit int) ([]WebhookDelivery, error)
}

// --- 哨兵错误 ---

// ErrBackendMissing 在 backend 字段为 nil 时返回。
var ErrBackendMissing = errors.New("console: backend not configured")

// ErrPluginNotFound 是 PluginBackend.Enable/Disable 命中未知 name。
var ErrPluginNotFound = errors.New("console: plugin not found")

// ErrPluginExists 是 PluginBackend.Install 命中已存在 name。
var ErrPluginExists = errors.New("console: plugin already exists")

// ErrModelNotFound 是 ModelBackend.Update/Ping 命中未知 channel。
var ErrModelNotFound = errors.New("console: model channel not found")

// ErrModelExists 是 ModelBackend.Create 命中已存在 channel。
var ErrModelExists = errors.New("console: model channel already exists")

// ErrTaskNotFound 是 TaskBackend.Cancel 命中未知 id。
var ErrTaskNotFound = errors.New("console: task not found")

// ErrTaskRunning 是 TaskBackend.Retry 命中仍 Running/Pending 的 task。
var ErrTaskRunning = errors.New("console: task already running")

// ErrApprovalNotFound 是 ApprovalsBackend.Decide 命中未知 id。
var ErrApprovalNotFound = errors.New("console: approval not found")

// ErrMessageNotFound 是 SessionBackend.EditMessage/DeleteMessage 命中未知 seq。
var ErrMessageNotFound = errors.New("console: message not found")

// ErrMessageNotEditable 是 SessionBackend.EditMessage 收到 role≠user/system。
var ErrMessageNotEditable = errors.New("console: message role not editable")
