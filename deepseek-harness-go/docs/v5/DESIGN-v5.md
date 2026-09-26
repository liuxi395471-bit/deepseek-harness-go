# DESIGN-v5 — 安全与可观测基线（详细规格）

> **配套**：[0-ROADMAP.md](../../0-ROADMAP.md) §3 v5；[PHASE-5-PLAN.md](./PHASE-5-PLAN.md)
> **基线**：v4.0.0（含事件溯源 + 投影缓存 + Gateway SSE + 库存视图）

## §0 概述

v5 主题：**让 dsh 在"多用户多会话"场景下变得安全、可观测、可审计**。

数据流：

```
┌────────┐       ┌────────────────┐       ┌────────────┐
│ client │──┬──▶│ Gateway SSE    │──┬──▶│ EventStore │
└────────┘  │   │ /api/.../stream│  │   └────────────┘
            │   └────────────────┘  │          │
            │         │ ⑤ event     │ ◀────────┘ ⑥ Append
            │         ▼             │          │
            │   ┌──────────────┐   │   ┌──────────────┐
            │   │ Runner+Hook  │──→│──▶│ Approver     │ ◀── Matrix (P5-5)
            │   │ ChannelClient│   │   └──────────────┘
            │   └──────────────┘   │          │
            │         │ ③ tool     │   ┌──────▼────────┐
            │         ▼             │   │ Credentials   │ ◀── Provider (P5-4)
            │   ┌──────────────┐   │   └───────────────┘
            │   │    Tool      │   │
            │   └──────────────┘   │
            │                      │
            └──────── ⑧ Usage ◀────┴──── Meter (P5-2)
                                   ⑦ Lease (P5-1)
```

## §1 P5-1 Session Write Lease

### §1.1 接口

```go
// internal/store/lease.go

// Lease 提供同 sid 写入互斥（单写者）。
type Lease interface {
    // Acquire 试图持有 sid 的写租约。
    // - 成功：返回非 nil LeaseHandle，调用方必须 Release。
    // - 持有中：返回 ErrLeaseHeld。
    // - sid 不存在：返回 ErrNotFound。
    Acquire(ctx context.Context, sid, owner string) (LeaseHandle, error)
}

// LeaseHandle 是单一 sid 的写租约句柄。
type LeaseHandle interface {
    // Sid 返回被持有的 session id。
    Sid() string
    // Owner 返回 Acquire 的 owner 标识。
    Owner() string
    // Release 主动释放租约。多次调用安全。
    Release()
}

// ErrLeaseHeld 在 sid 已被另一 owner 持有时返回。
var ErrLeaseHeld = errors.New("store: lease held by another owner")
```

### §1.2 Memory 实现

```go
type MemoryLease struct {
    mu     sync.Mutex
    held   map[string]string // sid → owner
    expiry map[string]time.Time
}

func NewMemoryLease() *MemoryLease { ... }

// 默认 30s TTL；后台 goroutine 每 5s 扫描过期项。
const defaultLeaseTTL = 30 * time.Second
const leaseReaperInterval = 5 * time.Second
```

### §1.3 SQLite 集成

```go
// internal/store/sqlite.go (修改)
// 在 Append / AppendEvent 入口处加 lease 获取；失败时返回 ErrLeaseHeld。

func (s *SQLiteStore) Append(ctx context.Context, id string, msg llm.Message) error {
    handle, err := s.lease.Acquire(ctx, id, "append")
    if err != nil { return err }
    defer handle.Release()
    // ... 既有逻辑 ...
}
```

### §1.4 向后兼容

引入 Lease 可选字段：v3 MapStore / v2 v4 SQLite 的旧构造路径仍可用。

`internal/store/store.go` 增加：

```go
// WithLease 把 l 绑到 SQLiteStore。nil = NoopLease（兼容）。
type SQLiteOption func(*SQLiteStore)
func WithLease(l Lease) SQLiteOption { ... }

// NewSQLiteStore 保留旧签名，新增 NewSQLiteStoreWithOptions。
```

## §2 P5-2 Token Meter

### §2.1 LLMCallPayload 扩展

```go
// internal/store/event.go
type LLMCallPayload struct {
    PromptTokens      int `json:"prompt_tokens"`
    CompletionTokens  int `json:"completion_tokens"`
    TotalTokens       int `json:"total_tokens"`
    CacheReadTokens   int `json:"cache_read_tokens,omitempty"`
    CacheWriteTokens  int `json:"cache_write_tokens,omitempty"`
    ReasoningTokens   int `json:"reasoning_tokens,omitempty"`
    Model             string `json:"model,omitempty"` // v5 新增：归因模型
}
```

### §2.2 Meter 接口

```go
// internal/usage/meter.go

// Meter 为每个 session 累计 token 指标。
type Meter interface {
    // Account 把单次 LLM 调用的 usage 累加到 sid。
    Account(sid string, p LLMCallPayload)
    // Get 返回 sid 的累计指标。session 不存在时返回 zero。
    Get(sid string) Metrics
    // Reset 清空 sid 的累计（用于 /reset 命令）。
    Reset(sid string)
}

type Metrics struct {
    Sid                 string  `json:"sid"`
    PromptTokens        int     `json:"prompt_tokens"`
    CompletionTokens    int     `json:"completion_tokens"`
    TotalTokens         int     `json:"total_tokens"`
    CacheReadTokens     int     `json:"cache_read_tokens"`
    CacheWriteTokens    int     `json:"cache_write_tokens"`
    ReasoningTokens     int     `json:"reasoning_tokens"`
    Model               string  `json:"model,omitempty"`
    UpdatedAt           time.Time `json:"updated_at"`
}

type MemoryMeter struct {
    mu      sync.RWMutex
    metrics map[string]*Metrics
}
```

### §2.3 Gateway 集成

```go
// internal/server/gateway_handler.go (扩展)

// usage.meter source:
//   params: {sid}
//   响应: {final:{metrics: <Metrics>}}
func (h *GatewayHandlers) handleUsageMeter(...) error {
    state, _ := h.Meter.Get(sid)
    return emit(ctx, out, "usage.meter", "final", map[string]any{"metrics": state})
}

// usage.bulk source: 返回全部 session 的 metrics 列表。
func (h *GatewayHandlers) handleUsageBulk(...) error { ... }
```

## §3 P5-3 Model Channel

### §3.1 配置

```yaml
# harness.yml
channels:
  - code: primary
    provider: openai
    base-url: http://127.0.0.1:8777/v1
    model: glm-5.3-flash
  - code: fallback
    provider: anthropic
    model: claude-sonnet
  - code: local
    provider: ollama
    base-url: http://127.0.0.1:11434
    model: qwen2.5
```

### §3.2 注册表

```go
// internal/runtime/channel.go

// ChannelConfig 描述一个命名渠道。
type ChannelConfig struct {
    Code     string        `yaml:"code"     json:"code"`
    Provider string        `yaml:"provider" json:"provider"`
    BaseURL  string        `yaml:"base-url" json:"base_url,omitempty"`
    APIKey   string        `yaml:"api-key"  json:"api_key,omitempty"`
    Model    string        `yaml:"model"    json:"model"`
    Timeout  time.Duration `yaml:"timeout"  json:"timeout,omitempty"`
}

// Resolve 返回 channelCode 对应的 llm.Client。
type Registry interface {
    Resolve(code string) (llm.Client, error)
    Codes() []string
}

var ErrUnknownChannel = errors.New("runtime: unknown channel")
```

### §3.3 runner 接入

```go
// cmd/dsh/main.go (新增)

// -channel primary 切换默认渠道
var channelFlag = flag.String("channel", "", "override LLM channel")

channels, err := runtime.NewRegistry(cfg.Channels)
// 兜底：单一 cfg.LLM → 注册为 "default"
if err != nil { log.Fatal(err) }
client, err := channels.Resolve(channelCode)
```

## §4 P5-4 Credentials

### §4.1 接口

```go
// internal/credentials/provider.go

// Provider 是一组凭据来源；调用方按优先级 Chained。
type Provider interface {
    // Resolve 根据 ref 返回明文凭据。查不到返回 ErrNotFound。
    Resolve(ctx context.Context, ref Ref) (string, error)
}

type Ref struct {
    Name string // "deepseek-api-key"
    Env  string // 环境变量名（优先级最高）
    File string // JSON 文件路径（按 ref.Name 取 val 字段）
}

// EnvProvider 只看环境变量。
type EnvProvider struct{}

// FileProvider 从 JSON 文件读取。
type FileProvider struct {
    Path string // ~/.dsh/credentials.json
}

// Chained 链式查询：env 先、file 后、keyring 再次（v6+）。
type Chained struct {
    Providers []Provider
}
```

### §4.2 文件格式

```json
{
  "deepseek-api-key": "sk-xxx",
  "anthropic-api-key": "sk-ant-xxx"
}
```

### §4.3 集成

```go
// cmd/dsh/main.go
creds := credentials.NewChained(
    credentials.NewEnvProvider(),
    credentials.NewFileProvider(filepath.Join(home, ".dsh", "credentials.json")),
)

apiKey, err := creds.Resolve(ctx, credentials.Ref{Name: "deepseek-api-key", Env: "DEEPSEEK_API_KEY"})
if err != nil { log.Fatalf("...") }
```

## §5 P5-5 Approval Matrix

### §5.1 配置

```yaml
# harness.yml
approval:
  matrix:
    default: auto                 # profile 默认策略
    profiles:
      dev:
        - tool: shell
          policy: ask              # ask | auto | deny
        - tool: fs_write
          policy: ask
      prod:
        - tool: shell
          policy: deny
        - tool: fs_write
          policy: ask
        - tool: fs_read
          policy: auto
```

### §5.2 实现

```go
// internal/approval/matrix.go

type Policy int
const (
    PolicyAsk Policy = iota  // 询问人
    PolicyAuto               // 自动批准
    PolicyDeny               // 拒绝
)

type Rule struct {
    Tool   string `yaml:"tool"`
    Policy Policy `yaml:"policy"`
}

type Profile struct {
    Name  string `yaml:"name"`
    Rules []Rule `yaml:"rules"`
}

type Matrix struct {
    Default Policy    `yaml:"default"`
    Profiles []Profile `yaml:"profiles"`
}

// GetPolicy 返回 (profile, tool) 的 Policy；profile 不存在回退 default。
func (m *Matrix) GetPolicy(profile, tool string) Policy

// MatrixApprover 把 Matrix 包装为 approval.Approver。
type MatrixApprover struct {
    Matrix Matrix
    Profile string
}

func (a *MatrixApprover) Approve(ctx context.Context, req Request) (Decision, error) {
    switch a.Matrix.GetPolicy(a.Profile, req.Tool) {
    case PolicyAuto: return ApproveSession, nil
    case PolicyDeny: return Deny, fmt.Errorf("matrix deny")
    default: return /* 委托 Chain 下一层 *\/ }
}
```

### §5.3 集成

```go
// cmd/dsh/main.go
mat, err := approval.LoadMatrix(cfg.Approval.MatrixFile)
mApprover := &approval.MatrixApprover{Matrix: mat, Profile: cfg.Agent.Profile}

// 给 shell 工具包裹：先 Matrix 再 fallback 到原 approver。
shellTool.Approver = approval.NewChain(mApprover, terminalApprover)
```

## §6 P5-6 Hooks

### §6.1 接口

```go
// internal/hook/hook.go

type Event int
const (
    PreToolUse Event = iota
    PostToolUse
)

type Hook interface {
    Name() string
    Match(req MatchRequest) bool
    Pre(ctx context.Context, req *PreRequest) error  // 改 args / deny
    Post(ctx context.Context, req *PostRequest) error // 改 result / log
}

type MatchRequest struct {
    Tool string
    Args json.RawMessage
}

type PreRequest struct {
    Tool   string
    Args   *json.RawMessage // 可改写
}

type PostRequest struct {
    Tool   string
    Args   json.RawMessage
    Result tool.Result
}

// Registry 收集多个 Hook；Pre/Post 按注册顺序串行执行。
type Registry struct {
    mu sync.RWMutex
    hooks map[Event][]Hook
}

func (r *Registry) Pre(ctx, req *PreRequest) error
func (r *Registry) Post(ctx, req *PostRequest) error
```

### §6.2 runner 接入

```go
// internal/agent/runner.go (修改)
type LoopRunner struct {
    // ...
    Hooks *hook.Registry  // v5 新增；nil = NoopRegistry
}

// 在调用 tool.Execute 之前：
if r.Hooks != nil {
    preReq := &hook.PreRequest{Tool: tc.Function.Name, Args: &raw}
    if err := r.Hooks.Pre(ctx, preReq); err != nil {
        // 拦截：把 err 折叠成 tool Result{IsError:true}
    }
    raw = *preReq.Args
}
// 执行
// ...

// 执行后：
if r.Hooks != nil {
    postReq := &hook.PostRequest{Tool: tc.Function.Name, Args: raw, Result: result}
    _ = r.Hooks.Post(ctx, postReq)
}
```

### §6.3 集成

```go
// cmd/dsh/main.go
hreg := hook.NewRegistry()
// 暂时只暴露 NoopRegistry；后续允许 -hook 路径
runner.Hooks = hreg
```

## §7 跨域约束

- **不为 P5-1..P5-6 引入第三方依赖**（仅复用现有 json/sync/context）。
- **所有新接口的零值 = no-op**（如 `runner.Hooks == nil` = `NoopRegistry`）。
- **测试用 mock + 表驱动**，不走网络。
- **Wire 兼容**：JSON Event.Payload 多余字段允许；缺字段给零值。

## §8 已知边界

- P5-1 Lease 是进程内（MemoryLease）；跨进程需 Redis lease——v6+。
- P5-2 Meter 仅聚合 LLMCall；tool 用量需在 v6+ 引入 cost attribution。
- P5-3 Channel 不做故障转移（fallback 需 v6+）。
- P5-4 Credentials 不支持 KMS / 加密 yml——v6+。
- P5-5 Matrix 仅扁平 profile；继承层级 v6+。
- P5-6 Hooks 不支持异步回调——v6+。