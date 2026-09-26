# DeepSeek Harness Go 版 — 第一版设计文档（v1.2）

> 配套 [`PLAN.md`](./PLAN.md)（实施规划）。本设计文档回答「**要造的是什么、它由哪些部分组成、各部分如何交互、状态与边界是什么**」；PLAN 回答「**按什么顺序、什么节奏去造**」。
>
> 适用版本：deepseek-harness-go v0.1.0-M4（首个端到端可演示版本）+ v1.2 收尾补丁。
> 阅读顺序建议：§1 全局视图 → §2 配置 → §3 LLM → §4 Tool → §5 Runner → §6 REPL → §7 错误与取消 → §8 验收矩阵。
>
> **v1.1 → v1.2 变更**：基于代码 review 收尾 6 处硬伤（R1–R6）+ 1 处小修（S2）。**第二版（M5 流式 / HTTP API / Session / Usage）见 [§11 第二版路线图](#11-第二版路线图-m5-范围预告)**。
>
> **v1.1 修订**：基于 v1 自评审（H1–H5 硬伤 + M1–M6 编码时吸收 + N1–N4 命名/对齐）后修订；伪码已可直接编译。

---

## 1. 全局视图

### 1.1 分层与依赖

```text
cmd/dsh                     ── 仅做装配与交互，不含业务逻辑
    │
    ▼
internal/agent              ── ReAct 状态机（依赖 llm + tool）
internal/tool               ── Tool 接口 + Registry（依赖 llm：tool.Spec → llm.ToolSpec）
internal/llm                ── OpenAI 兼容 Client（叶子包）
internal/tools              ── 内置工具实现（依赖 tool）
internal/config             ── 配置结构 + 加载（叶子包）
```

依赖约束：
- `cmd/**` → `internal/{agent,tool,llm,config,tools}`
- `internal/tool` **只依赖** `internal/llm`（用于 `tool.Spec` 转换）
- `internal/llm` **不依赖** `internal/tool`（Tool 通过 `Execute(ctx, json.RawMessage) Result` 与外界解耦）
- `internal/agent` 同时依赖 `internal/llm` 与 `internal/tool`，是唯一的"编排放"
- `internal/tools` 依赖 `internal/tool` + `internal/config`（仅类型，不反向）
- `internal/config` 不依赖任何 `internal/*`

### 1.2 关键数据流（一次"LLM 调工具"往返）

```text
REPL prompt
   │
   ▼
Runner.Run(ctx, prompt)
   │
   │  emit PhaseChange(Init)            ─────────┐
   │                                          │  Event 通道
   ▼                                          ▼
构造 messages=[system, user]              ──→ 调用方 / REPL
   │
   ▼
llm.Client.Chat(ctx, ChatRequest{Messages, Tools, …})
   │
   ├── error ──→ emit LoopError ──→ 结束
   │
   ▼
得到 ChatResponse.Choices[0].Message
   │
   ├── 无 ToolCalls ──→ emit AssistantMessage(Content), LoopDone("no_tool_calls")
   │
   ▼
对每个 ToolCall:
   ├─ emit ToolCallStart
   ├─ t, _ := registry.Get(name)
   ├─ result := t.Execute(ctx, parse(Arguments))
   ├─ emit ToolResult{Content, IsError, Took}
   └─ 把 result 作为 message{role:tool, tool_call_id} 回填到 messages
   │
   ▼
rounds += 1；若 rounds > MaxRounds → LoopDone("max_rounds")；否则回到 llm.Chat
```

### 1.3 设计原则

1. **接口先行于实现**：每个包第一份提交就是接口 + 一个 stub，方便换实现（如 LLM 客户端的 mock）。
2. **错误不冒泡**：工具错误 → `Result{IsError}` 回填给模型；LLM 错误 → `LoopError` 事件 → REPL 友好提示。REPL 只在 `ctx.Canceled` 时主动退出。
3. **ctx 贯通**：每一层（Runner / Client / Tool.Execute / 任何 IO）都必须接 `context.Context`，并把 ctx 取消视作第一公民（详见 §7）。
4. **零隐式并发**：`LoopRunner` 单协程跑循环；事件通过 channel 派发；`Registry` 用 `sync.RWMutex`。
5. **测试可注入**：LLM 客户端允许注入 `http.RoundTripper`；Registry 允许注入手工列表；Runner 允许注入 mock client。CI 无需真实 base-url 即可 100% 覆盖。

---

## 2. 配置层 (`internal/config`)

### 2.1 配置结构

```go
type Config struct {
    LLM   LLMConfig   `yaml:"llm"   json:"llm"`
    Agent AgentConfig `yaml:"agent" json:"agent"`
}

type LLMConfig struct {
    BaseURL    string        `yaml:"base-url"  env:"DEEPSEEK_BASE_URL"`
    APIKey     string        `yaml:"api-key"   env:"DEEPSEEK_API_KEY"`
    Model      string        `yaml:"model"     env:"DEEPSEEK_DEFAULT_MODEL"`
    MaxTokens  int           `yaml:"max-tokens" env:"DEEPSEEK_MAX_TOKENS"`
    Timeout    time.Duration `yaml:"timeout"    env:"DEEPSEEK_TIMEOUT"`
    /* 默认 http.Client.Transport 与 UnmarshalYAML 略 */
}

type AgentConfig struct {
    MaxRounds     int     `yaml:"max-rounds"     env:"DSH_AGENT_MAX_ROUNDS"`
    WorkspaceRoot string  `yaml:"workspace"      env:"DSH_AGENT_WORKSPACE"`
    Temperature   *float64 `yaml:"temperature"   env:"DSH_AGENT_TEMPERATURE"`
    SystemPrompt  string  `yaml:"system-prompt"  env:"DSH_AGENT_SYSTEM_PROMPT"`
    Debug         bool    `yaml:"debug"          env:"DSH_AGENT_DEBUG"`
}
```

### 2.2 默认值（用户没写时）

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `LLM.BaseURL` | `http://127.0.0.1:8777/v1` | 与 Java 版对齐 |
| `LLM.Model` | `glm-5.3-flash` | 占位模型，方便联调 |
| `LLM.MaxTokens` | `8192` | |
| `LLM.Timeout` | `120 * time.Second` | |
| `LLM.APIKey` | `""` | 不留默认，必须由用户/环境提供 |
| `Agent.MaxRounds` | `16` | 见 PLAN §5 M0 注：远小于 Java `ReactLoopAgent` 的 50；首版收紧 |
| `Agent.WorkspaceRoot` | `./.dsh/workspace` | 启动时 `os.MkdirAll` |
| `Agent.Temperature` | `0.2` | `*float64`，传 nil 则不发该字段（兼容只接受 temp=1 的网关） |
| `Agent.SystemPrompt` | `""` | 空时使用 `internal/agent/system.go` 的内置模板 |
| `Agent.Debug` | `false` | |

### 2.3 加载优先级与算法

```
Load(path string) (Config, error):
    cfg := applyDefaults()
    if path 存在:
        data := os.ReadFile(path)
        yaml.Unmarshal(data, &cfg)            // 字段级合并：YAML 没写的字段保留 cfg 中的值
    cfg = overrideFromEnv(cfg)                // env 命名规则见上表；非空才覆盖
    cfg.Validate()                            // 必填字段非空、MaxRounds>0、WorkspaceRoot 非空、BaseURL 合法
    return cfg
```

- **合并而非替换**：YAML 解析到结构体只覆盖出现的字段；其它字段保留 `cfg` 当前值（用 `yaml.v3` 默认 unmarshal 行为即可）。
- **env 判定非空**：`os.Getenv(name) != ""` 才覆盖；空字符串视作"未设置"。
- **Validate**：
  - `LLM.BaseURL` 必须能 `url.Parse` 且 scheme ∈ {http, https}
  - `Agent.MaxRounds ∈ [1, 64]`
  - `Agent.WorkspaceRoot` 为绝对路径或当前相对路径

### 2.4 `harness.example.yml`

```yaml
llm:
  base-url: <your-base-url>           # 例如 http://127.0.0.1:8777/v1
  api-key: ""                         # ❗ 不写在这里，用 env: DEEPSEEK_API_KEY
  model: <your-model>                 # 例如 glm-5.3-flash / deepseek-chat / gpt-4o-mini
  max-tokens: 8192
agent:
  max-rounds: 16
  workspace: ./.dsh/workspace
  temperature: 0.2
  system-prompt: |                     # 可选
    你是 dsh，一个简洁可靠的 CLI 助手。
  debug: false
```

---

## 3. LLM 层 (`internal/llm`)

### 3.1 接口

```go
package llm

type Client interface {
    Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
    // M5 增加：ChatStream(ctx, req ChatRequest) iter.Seq2[StreamChunk, error]
}

type APIError struct {
    Status int
    Body   string
    Method string
    URL    string
}
func (e *APIError) Error() string
```

**接口与实现分离的好处**：
- 单测可用 mock client（不需要 httptest）；CI 无网络也能跑通 `Runner` 测试。
- 将来切到 Anthropic Messages 或 Ollama 时只新增实现，Runner 不改。

### 3.2 OpenAI 兼容实现 (`OpenAICompatibleClient`)

```go
type OpenAICompatibleClient struct {
    BaseURL    string        // 不含尾斜杠
    APIKey     string        // 允许为空（某些本地服务不需要鉴权）
    HTTPClient *http.Client  // 可被测试注入 Transport
    // 可选 Logger func(format string, args ...any) — 给 REPL 排错用
}

func New(baseURL, apiKey string) *OpenAICompatibleClient {
    return &OpenAICompatibleClient{
        BaseURL: baseURL,
        APIKey:  apiKey,
        HTTPClient: &http.Client{ Timeout: 120 * time.Second },
    }
}

func (c *OpenAICompatibleClient) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) { ... }
```

实现要点：
1. 构造 URL：`strings.TrimRight(c.BaseURL, "/") + "/chat/completions"`。
2. 序列化请求：`json.Marshal(req)`；若是 `Stream=true`，加 `Accept: text/event-stream`（首版不实现流，留个 `if req.Stream { return ChatResponse{}, errors.New("streaming not implemented yet") }` 兜底）。
3. Header：`Authorization: Bearer <key>`（key 为空时不加 header）。
4. `req, _ := http.NewRequestWithContext(ctx, "POST", url, body)`。
5. 拿到响应：
   - 非 2xx：读 body（截 4 KiB 防爆），封装为 `*APIError` 返回。
   - 2xx：`json.NewDecoder(resp.Body).Decode(&out)`，返回 `out, nil`。
6. **不解析 `tool_calls[].function.arguments`** 字符串为 map，原样保留（让 Runner 解析后传给工具；原因是不同网关 JSON 风格不一致，原样透传最稳）。

### 3.3 关键类型（精简版）

> 完整见 PLAN §5 M1 §1；此处只列"必须有"的字段。

```go
type Role string
const (RoleSystem Role = "system"; RoleUser = "user"; RoleAssistant = "assistant"; RoleTool = "tool")

type ToolCall struct {
    ID       string       `json:"id"`
    Type     string       `json:"type"` // "function"
    Function ToolCallFunc `json:"function"`
}
type ToolCallFunc struct {
    Name      string `json:"name"`
    Arguments string `json:"arguments"` // JSON 串（标量或 object 均可），原样保留
}

type Message struct {
    Role       Role       `json:"role"`
    Content    string     `json:"content,omitempty"`
    ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
    ToolCallID string     `json:"tool_call_id,omitempty"` // role=tool 时使用，对应 ToolCall.ID
}

type ToolSpec struct {
    Type     string       `json:"type"` // "function"
    Function ToolSpecFunc `json:"function"`
}
type ToolSpecFunc struct {
    Name        string `json:"name"`
    Description string `json:"description"`
    Parameters  any    `json:"parameters"` // JSON Schema object；agent.Spec(t) 从 Tool.Parameters() 转
}

type ChatRequest struct {
    Model       string     `json:"model"`
    Messages    []Message  `json:"messages"`
    Tools       []ToolSpec `json:"tools,omitempty"`
    ToolChoice  any        `json:"tool_choice,omitempty"`
    Temperature *float64   `json:"temperature,omitempty"`
    MaxTokens   int        `json:"max_tokens,omitempty"`
    Stream      bool       `json:"stream,omitempty"` // 首版 false
}

type ChatResponse struct {
    ID      string   `json:"id"`
    Object  string   `json:"object"`
    Created int64    `json:"created"`
    Model   string   `json:"model"`
    Choices []Choice `json:"choices"`
    Usage   *Usage   `json:"usage,omitempty"`
}
type Choice struct {
    Index        int     `json:"index"`
    FinishReason string  `json:"finish_reason"`
    Message      Message `json:"message"`
}
type Usage struct {
    PromptTokens     int `json:"prompt_tokens"`
    CompletionTokens int `json:"completion_tokens"`
    TotalTokens      int `json:"total_tokens"`
}
```

### 3.4 客户端构造与生命周期

```go
package llm

import "deepseek-harness-go/internal/config"

/* NewClient 根据 LLMConfig 构造一个 OpenAI 兼容 LLM 客户端。
   返回接口类型，调用方拿到的就是 Client（编译期单值）。 */
func NewClient(cfg config.LLMConfig) (Client, error) {
    u, err := url.Parse(cfg.BaseURL)
    if err != nil { return nil, fmt.Errorf("llm: invalid base-url %q: %w", cfg.BaseURL, err) }
    if u.Scheme != "http" && u.Scheme != "https" {
        return nil, fmt.Errorf("llm: base-url scheme must be http(s): %q", cfg.BaseURL)
    }
    timeout := cfg.Timeout
    if timeout <= 0 { timeout = 120 * time.Second }
    return &OpenAICompatibleClient{
        BaseURL: cfg.BaseURL,
        APIKey:  cfg.APIKey,
        HTTPClient: &http.Client{ Timeout: timeout },
    }, nil
}
```

调用方持有 `Client` 接口，可在 `LoopRunner` 注入。`OpenAICompatibleClient` 的具体方法（`Chat`）对外**不需要**导出——但因其实现必须满足 `Client` 接口（鸭子类型），结构体的字段需要导出。

---

## 4. 工具层 (`internal/tool` + `internal/tools`)

### 4.1 接口与注册表

```go
package tool

type Result struct {
    Content string
    IsError bool
}

func Ok(content string) Result            { return Result{Content: content} }
func Err(content string) Result           { return Result{Content: content, IsError: true} }

type Tool interface {
    Name() string
    Description() string
    Parameters() any                                       // 返回 map[string]any 或 struct；ag 会 Marshal
    Execute(ctx context.Context, args json.RawMessage) (Result, error)
}
```

**错误约定**：
- **绝大多数失败**用 `Result{IsError:true}` 回填给模型，包括：参数缺失、参数非法、文件不存在、路径越界。
- **极少数** `Execute` 返回 `error`：仅用于"工具自身 panic 恢复后"或"`args` JSON 完全无法解析"的情况。
- `IsError==true` 的 result 在消息里前置 `[ERROR] `，让模型区分。

### 4.2 注册表（线程安全）

```go
type Registry struct {
    mu    sync.RWMutex
    tools map[string]Tool
}

func NewRegistry() *Registry
func (r *Registry) Register(t Tool) error                 // 重名 → ErrDuplicate
func (r *Registry) Get(name string) (Tool, bool)
func (r *Registry) Names() []string
func (r *Registry) Specs() []llm.ToolSpec                // 给 LLM 的 tools 字段
func (r *Registry) MustRegister(t Tool)                  // 用于启动期注册 panic 替代
```

### 4.3 内置工具

#### 4.3.1 `greet`（最简工具，纯函数）

```
name:        "greet"
description: "向指定的人打招呼（用于验证 agent 闭环）。"
parameters:  { "type":"object", "properties":{ "name":{"type":"string"} }, "required":["name"] }
behavior:    Execute(ctx, {name}) → Ok("你好，<name>！我是 dsh。")
```

#### 4.3.2 `fs_read`（读取文件）

```
parameters: {
  "type":"object",
  "properties":{
    "path":{"type":"string","description":"文件路径，相对 workspace 或绝对"},
    "max_bytes":{"type":"integer","description":"可选，最大字节数（默认 1 MiB）"}
  },
  "required":["path"]
}
errors (回填 IsError):
  - "path is required"
  - "path escapes workspace: …"
  - "file not found: …"
  - "file too large (>max_bytes)"
```

#### 4.3.3 `fs_write`（写入文件，强制 workspace 内）

```
parameters: {
  "type":"object",
  "properties":{
    "path":{"type":"string"},
    "content":{"type":"string"}
  },
  "required":["path","content"]
}
behavior:
    root := workspaceRoot (绝对路径)
    clean := filepath.Clean(root + "/" + p)
    real := resolveSymlink(clean)                          // 二次校验软链
    if !strings.HasPrefix(real+string(filepath.Separator), root+string(filepath.Separator)) && real != root:
        Err("path escapes workspace: <p>")
    if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil: Err(...)
    if err := os.WriteFile(real, []byte(content), 0o644); err != nil: Err(...)
    Ok("wrote <N> bytes to <relative>")
errors: 同上 + "write failed: ..."
```

### 4.4 路径安全工具 (`internal/tools/fs_path.go`)

```go
package tools

func resolveInWorkspace(root, p string) (string, error) {
    if root == "" { return "", errors.New("workspace root not configured") }
    absRoot, err := filepath.Abs(root)
    if err != nil { return "", err }
    absRoot = filepath.Clean(absRoot)
    candidate := p
    if !filepath.IsAbs(candidate) { candidate = filepath.Join(absRoot, candidate) }
    clean := filepath.Clean(candidate)
    /* 用 evalSymlinks 二次校验；不存在不会报错（让 WriteFile 自己处理） */
    real, err := filepath.EvalSymlinks(clean)
    if err != nil && !errors.Is(err, os.ErrNotExist) { return "", err }
    if err != nil { real = clean }
    rel, err := filepath.Rel(absRoot, real)
    if err != nil || strings.HasPrefix(rel, "..") || strings.Contains(rel, ".."+string(filepath.Separator)) {
        return "", fmt.Errorf("path escapes workspace: %s", p)
    }
    return real, nil
}
```

### 4.5 `Tool` ↔ `llm.ToolSpec` 转换

```go
package tool
import "deepseek-harness-go/internal/llm"

func Spec(t Tool) llm.ToolSpec {
    return llm.ToolSpec{
        Type: "function",
        Function: llm.ToolSpecFunc{
            Name: t.Name(),
            Description: t.Description(),
            Parameters: t.Parameters(),        // any → json.Marshal 时序列化为 JSON Schema object
        },
    }
}
```

> `Tool.Parameters()` 返回 `any`（通常是 `map[string]any` 或匿名 struct），序列化后即为 OpenAI 期望的 JSON Schema object。

---

## 5. Runner 层 (`internal/agent`)

### 5.1 接口

```go
package agent

/* Event 是首版"按值类型 + 类型 switch"的事件接口。
   不引入私有方法 isEvent()：首版不接受跨包注册，所有事件都在本包内实现。
   后续若需要 plugin 自定义事件，再改为 type Event interface{ EventTag() string }。 */
type Event interface{ EventTag() string }

type Runner interface {
    /* Run 启动后立即返回一个 event 通道和一个 result 通道；
       事件持续发送直到 result 关闭（一次执行一次事件流）。 */
    Run(ctx context.Context, prompt string) (events <-chan Event, result <-chan RunResult)
}

type RunResult struct {
    FinalMessages []llm.Message
    Rounds        int
    StopReason    string // "no_tool_calls" | "max_rounds" | "error" | "canceled"
    Error         error  // 当 StopReason == "error"
}
```

> 两个通道都由 Runner 内部 goroutine 负责关闭；**严格顺序**：循环退出时先 `close(events)`、再 `send result`、再 `close(result)`。REPL 用 `for ev := range events { ... }` 消费完所有事件后再 `<-result`，不会出现"先收到 result 但还有事件在路上"或"events 关闭了但 result 没到"的竞态。

### 5.2 事件清单

```go
/* EventTag 返回事件类型名（首版用作格式化和日志）。 */
type PhaseChange struct{ Phase Phase; At time.Time }
func (PhaseChange) EventTag() string { return "phase_change" }

type Phase int
const (PhaseInit Phase = iota; PhaseLLMCall; PhaseToolExec; PhaseLLMDone; PhaseStopped; PhaseError)

type AssistantDelta struct{ Text string }                // 流式时使用（M5）；首版非流则只发整条 AssistantMessage
func (AssistantDelta) EventTag() string { return "assistant_delta" }

type AssistantMessage struct{ Content string; ToolCalls []llm.ToolCall }
func (AssistantMessage) EventTag() string { return "assistant_message" }

type ToolCallStart struct {
    Call llm.ToolCall
}
func (ToolCallStart) EventTag() string { return "tool_call_start" }

type ToolResult struct {
    CallID  string
    Name    string
    Content string
    IsError bool
    Took    time.Duration
}
func (ToolResult) EventTag() string { return "tool_result" }

type LoopDone struct{ Rounds int; Messages []llm.Message }
func (LoopDone) EventTag() string { return "loop_done" }

type LoopError struct{ Err error; Phase Phase }
func (LoopError) EventTag() string { return "loop_error" }
```

### 5.3 LoopRunner 伪码（可执行版本）

```go
package agent

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "strings"
    "time"

    "deepseek-harness-go/internal/llm"
    "deepseek-harness-go/internal/tool"
)

type SystemPromptBuilder interface {
    Build(reg *tool.Registry) string
}

type LoopRunner struct {
    Client    llm.Client
    Registry  *tool.Registry
    System    SystemPromptBuilder

    /* 来自 AgentConfig 的运行参数 */
    Model       string
    Temperature *float64
    MaxTokens   int
    MaxRounds   int

    /* 事件通道缓冲，避免慢消费者阻塞 Runner；32 够覆盖绝大多数 case。 */
    bufSize int
}

/* NewLoopRunner 构造默认配置的 LoopRunner。 */
func NewLoopRunner(c llm.Client, reg *tool.Registry, sys SystemPromptBuilder, model string, maxRounds, maxTokens int, temp *float64) *LoopRunner {
    if maxRounds <= 0 { maxRounds = 16 }
    return &LoopRunner{
        Client: c, Registry: reg, System: sys,
        Model: model, MaxTokens: maxTokens, MaxRounds: maxRounds,
        Temperature: temp, bufSize: 32,
    }
}

func (r *LoopRunner) Run(ctx context.Context, prompt string) (<-chan Event, <-chan RunResult) {
    out := make(chan Event, r.bufSize)
    res := make(chan RunResult, 1)

    go r.loop(ctx, prompt, out, res)
    return out, res
}

func (r *LoopRunner) loop(ctx context.Context, prompt string, out chan<- Event, res chan<- RunResult) {
    /* 关键收尾顺序：即使 panic，也要先把 events 关闭 → result 发出去 → 关闭 result。
       REPL 用 for-range events 消费完事件后再 <-result。 */
    var stopReason string = "error"
    var stopErr error
    var finalMsgs []llm.Message
    rounds := 0

    defer func() {
        if x := recover(); x != nil {
            stopReason = "error"
            stopErr = fmt.Errorf("runner panic: %v", x)
        }
        close(out)                            // 1) 先关闭事件流
        res <- RunResult{                     // 2) 再发最终结果
            FinalMessages: finalMsgs,
            Rounds:        rounds,
            StopReason:    stopReason,
            Error:         stopErr,
        }
        close(res)                            // 3) 最后关闭结果通道
    }()

    msgs := []llm.Message{
        {Role: llm.RoleSystem, Content: r.System.Build(r.Registry)},
        {Role: llm.RoleUser, Content: prompt},
    }
    finalMsgs = msgs

    select {
    case <-ctx.Done():
        stopReason = "canceled"
        return
    default:
    }

    for {
        rounds++
        if rounds > r.MaxRounds {
            stopReason = "max_rounds"
            return
        }

        out <- PhaseChange{Phase: PhaseLLMCall, At: time.Now()}
        resp, err := r.Client.Chat(ctx, llm.ChatRequest{
            Model:       r.Model,
            Messages:    msgs,
            Tools:       r.Registry.Specs(),
            Temperature: r.Temperature,
            MaxTokens:   r.MaxTokens,
            Stream:      false,
        })
        if err != nil {
            if errors.Is(err, context.Canceled) {
                stopReason = "canceled"
                return
            }
            stopReason = "error"
            stopErr = err
            return
        }
        if len(resp.Choices) == 0 {
            stopReason = "error"
            stopErr = errors.New("llm: empty choices")
            return
        }
        assistant := resp.Choices[0].Message
        msgs = append(msgs, assistant)
        out <- AssistantMessage{Content: assistant.Content, ToolCalls: assistant.ToolCalls}

        if len(assistant.ToolCalls) == 0 {
            stopReason = "no_tool_calls"
            finalMsgs = msgs
            return
        }

        /* 工具调用阶段；一次 assistant 可能同时发起多个 tool_calls，依次执行。 */
        for _, tc := range assistant.ToolCalls {
            select {
            case <-ctx.Done():
                stopReason = "canceled"
                finalMsgs = msgs
                return
            default:
            }
            out <- PhaseChange{Phase: PhaseToolExec, At: time.Now()}

            t, ok := r.Registry.Get(tc.Function.Name)
            if !ok {
                /* 未知工具：以 [ERROR] 角色回填，不让循环崩溃。 */
                content := "[ERROR] unknown tool: " + tc.Function.Name
                msgs = append(msgs, llm.Message{
                    Role: llm.RoleTool, Content: content, ToolCallID: tc.ID,
                })
                out <- ToolResult{
                    CallID: tc.ID, Name: tc.Function.Name,
                    Content: content, IsError: true, Took: 0,
                }
                continue
            }

            out <- ToolCallStart{Call: tc}
            start := time.Now()
            /* 工具 args 必须是合法 JSON；非法 JSON 也回填 [ERROR]，不冒泡。 */
            raw := json.RawMessage(tc.Function.Arguments)
            if len(raw) == 0 || string(raw) == "null" {
                raw = json.RawMessage("{}")
            }
            result, execErr := func() (tool.Result, error) {
                defer func() {
                    if x := recover(); x != nil {
                        /* 工具内部 panic → 不会逃出 loop；降级为 Err Result。 */
                    }
                }()
                return t.Execute(ctx, raw)
            }()
            _ = execErr /* Execute 极少返回 error（panic 已被 recover）；此处保留接口 */
            if execErr != nil {
                result = tool.Err(execErr.Error())
            }
            took := time.Since(start)

            if result.IsError && !strings.HasPrefix(result.Content, "[ERROR] ") {
                result.Content = "[ERROR] " + result.Content
            }
            msgs = append(msgs, llm.Message{
                Role: llm.RoleTool, Content: result.Content, ToolCallID: tc.ID,
            })
            out <- ToolResult{
                CallID:  tc.ID,
                Name:    tc.Function.Name,
                Content: result.Content,
                IsError: result.IsError,
                Took:    took,
            }
        }
    }
}
```

实现要点：
- **`rounds` 计数放在循环顶部，逻辑：`rounds >= MaxRounds` 时退出**：第一次循环 `rounds=1`；`MaxRounds=3` 时正好执行 3 次 LLM 调用后停下，`RunResult.Rounds=3`。语义：`MaxRounds=N` = "最多执行 N 次 LLM 调用"。
- **收尾顺序硬约束**：defer 里 `close(out) → res <- result → close(res)`。panic 时也走同一路径，REPL 不会卡死。
- **每轮都是新会话**：首版不持有 history；"上文"挪到 M7。若未来要加 history，只在 `msgs` 外面套一层 `[]llm.Message history` 即可。
- **每个 LLM / 工具循环前 select ctx.Done()**：让 Ctrl+C 立即生效。
- **未知工具 / 非法 args**：以 `[ERROR] …` 回填给模型，**不让循环崩溃**。
- **工具 panic**：用嵌套闭包 recover 兜住（即使工具实现忘了处理 panic），不会逃出 loop 协程。
- **温度字段**：`*float64`，nil 时 `Temperature *float64 \`json:"temperature,omitempty"\`` 自动省略，兼容只接受 temp=1 的网关。

### 5.4 System Prompt 组装

```go
package agent

type SystemPromptBuilder interface {
    Build(reg *tool.Registry) string
}

type DefaultSystemPrompt struct {
    /* Override 来自 AgentConfig.SystemPrompt；为空时用内置模板。 */
    Override string
}

func (d DefaultSystemPrompt) Build(reg *tool.Registry) string {
    if d.Override != "" { return d.Override }
    lines := []string{
        "你是 dsh，一个简洁可靠的 CLI 助手，使用 ReAct 循环与工具协作。",
        "可用工具（按需调用，不需要时不必）：",
    }
    for _, spec := range reg.Specs() {
        lines = append(lines, fmt.Sprintf("- %s: %s", spec.Function.Name, spec.Function.Description))
    }
    lines = append(lines, "工具失败（[ERROR] 前缀）时，请把它当作新信息继续推理，而不是重新尝试同一参数。")
    return strings.Join(lines, "\n")
}
```

### 5.5 构造与生命周期

```
func NewLoopRunner(client llm.Client, reg *tool.Registry, agentCfg config.AgentConfig) *LoopRunner
```

调用方（`cmd/dsh/main.go`）一次性构造并交给 REPL 长期持有；REPL 每次用户输入 → `runner.Run(ctx, prompt)` → 收事件 → 输出。

---

## 6. REPL 层 (`cmd/dsh`)

### 6.1 命令行 flag

```
dsh [-config <path>] [-prompt <string>] [-debug] [-version]

-config   默认 ./harness.yml；不存在则用 defaults
-prompt   非空：一次性跑并打印最终答复（调试用），不走 REPL
-debug    打印中间事件
-version  打印 go build 注入的 version 常量并退出
```

### 6.2 主流程

```
main.go:
  1. flag.Parse()
  2. ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
  3. defer stop()
  4. cfg, err := config.Load(*configPath)
  5. if err != nil { log.Fatalf("config: %v", err) }
  6. if cfg.Agent.WorkspaceRoot != "" { os.MkdirAll(cfg.Agent.WorkspaceRoot, 0o755) }
  7. client, err := llm.NewClient(cfg.LLM)
  8. if err != nil { log.Fatalf("llm: %v", err) }
  9. registry := tool.NewRegistry()
 10. tools.MustRegisterBuiltin(registry, cfg.Agent.WorkspaceRoot)
 11. sys := agent.DefaultSystemPrompt{Override: cfg.Agent.SystemPrompt}
 12. runner := agent.NewLoopRunner(client, registry, sys,
                                       cfg.LLM.Model, cfg.Agent.MaxRounds,
                                       cfg.LLM.MaxTokens, cfg.Agent.Temperature)
 13. if *prompt != "":
        runOnce(ctx, runner, *prompt, *debug)
     else:
        repl.Run(ctx, runner, *debug)
```

### 6.3 `runOnce`（单次对话）

```go
func runOnce(ctx context.Context, runner agent.Runner, prompt string, debug bool) {
    events, result := runner.Run(ctx, prompt)
    for ev := range events {
        if debug { fmt.Fprintln(os.Stderr, formatEvent(ev)) }
    }
    res := <-result
    switch res.StopReason {
    case "no_tool_calls": fmt.Println(res.FinalMessages[len(res.FinalMessages)-1].Content)
    case "max_rounds":    fmt.Fprintln(os.Stderr, "[dsh] reached max-rounds; final assistant content (if any):", tailContent(res.FinalMessages))
    case "error":         fmt.Fprintln(os.Stderr, "[dsh] error:", res.Error)
    case "canceled":      fmt.Fprintln(os.Stderr, "[dsh] canceled")
    }
}
```

### 6.4 REPL

```go
/* REPL.Run 持有 rootCtx（来自 signal.NotifyContext）；
   每轮 prompt 起一个独立子 ctx，Ctrl+C 取消当前轮，不退出 REPL。
   不需要额外 watcher goroutine：每轮子 ctx 自动跟随 rootCtx。 */
func Run(rootCtx context.Context, runner agent.Runner, debug bool) {
    in := bufio.NewScanner(os.Stdin)
    in.Buffer(make([]byte, 64*1024), 1024*1024)
    fmt.Println("dsh> ready. type your prompt, 'exit' to quit.")
    for {
        if !in.Scan() { return }                                  // EOF / Ctrl+D
        line := strings.TrimSpace(in.Text())
        if line == "" { continue }
        if line == "exit" || line == "quit" { return }

        /* 每轮独立子 ctx：WithCancelCause 让 caller 后续能拿到取消原因；
           此处只需 ctx，不用 cause，所以用 WithCancel。 */
        ctx, cancel := context.WithCancel(rootCtx)
        events, result := runner.Run(ctx, line)

        for ev := range events {
            switch e := ev.(type) {
            case agent.AssistantMessage:
                fmt.Print(e.Content)                               /* 实时：首版一次性渲染只看到一次 AssistantMessage */
            case agent.ToolCallStart:
                args := e.Call.Function.Arguments
                if len(args) > 64 { args = args[:64] + "…" }
                fmt.Fprintf(os.Stderr, "\n  → tool %s(%s)\n", e.Call.Function.Name, args)
            case agent.ToolResult:
                tag := "ok"
                if e.IsError { tag = "err" }
                fmt.Fprintf(os.Stderr, "  ← %s (%s, %s)\n", e.Name, tag, e.Took.Round(time.Millisecond))
                if debug { fmt.Fprintf(os.Stderr, "      %s\n", truncate(e.Content, 200)) }
            case agent.PhaseChange:
                if debug { fmt.Fprintf(os.Stderr, "  * phase=%d @ %s\n", e.Phase, e.At.Format(time.RFC3339)) }
            case agent.LoopError:
                fmt.Fprintln(os.Stderr, "[dsh] loop error:", e.Err)
            case agent.AssistantDelta, agent.LoopDone:
                /* M5 启用；首版不发 */
            }
        }

        cancel()                                                   /* 当前轮释放子 ctx；下一轮起新 ctx */
        res := <-result
        if res.StopReason == "max_rounds" {
            fmt.Fprintln(os.Stderr, "\n[dsh] reached max-rounds, continuing with what we have.")
        } else if res.StopReason == "error" {
            fmt.Fprintln(os.Stderr, "[dsh] error:", res.Error)
        } else if res.StopReason == "canceled" {
            fmt.Fprintln(os.Stderr, "[dsh] canceled")
        }
        fmt.Println()
    }
}

func formatEvent(ev agent.Event) string {
    /* debug 模式下打印所有字段。 */
    return fmt.Sprintf("[%s] %+v", ev.EventTag(), ev)
}
```

行为约定：
- `Ctrl+C`（`SIGINT`）：取消**当前轮**，不退出 REPL——rootCtx 还在，下一轮仍可用。
- `Ctrl+D`（EOF）：干净退出，REPL 关闭 stdin 后退出，退出码 0。
- `exit` / `quit`：干净退出。
- `cancel()` 调用时机：每轮事件消费完、result 取出**之后**才 cancel，确保本轮 Runner 协程不会再向已关闭的 channel 写入。

### 6.5 I/O 约定

- **STDOUT**：模型最终答复文本（plain text，无 ANSI 颜色）。
- **STDERR**：所有中间事件（tool call / tool result / 错误 / 调试信息）。
- **输入**：UTF-8，一行一 prompt；空行忽略。

---

## 7. 错误、取消、可观测性

### 7.1 错误分类

| 来源 | 类型 | 处理方式 | 用户感知 |
| --- | --- | --- | --- |
| 工具参数缺失/非法 | `Result{IsError:true}` | 工具内部 err/pre-check，结果回填给模型 | 模型看 `[ERROR]`，可能自纠 |
| 工具 panic | recover() → `Result{Err:"panic: ..."}` | 同上 | 同上 |
| `os` 错误（IO） | `Result{IsError:true}` | 回填 | 同上 |
| LLM 非 2xx | `*APIError` | `Runner.Run` 返回 `RunResult{StopReason:"error"}` | REPL 打印 `[dsh] error: ...` |
| LLM body 解析失败 | `RunResult{StopReason:"error", Error: ...}` | 同上 | 同上 |
| ctx 取消（用户 Ctrl+C） | `ctx.Canceled` | 立即返回 `RunResult{StopReason:"canceled"}` | REPL 打印 `[dsh] canceled` |
| max-rounds 触发 | `RunResult{StopReason:"max_rounds"}` | 截断最后一轮的 assistant content（若有） | REPL 打印提示 |
| REPL 配置错误 | `error` from `Load` | `main` 退出码 2 | 终端日志 |
| 工具注册重名 | `ErrDuplicate` | `main` 启动时 panic | 启动失败，REPL 不进入 |

### 7.2 取消传播链

```
SIGINT/SIGTERM
   │  signal.NotifyContext
   ▼
rootCtx (cmd/dsh.main)
   │
   ▼
REPL: ctx, cancel := context.WithCancel(rootCtx)   // 每轮独立
   │
   ▼
Runner.Run(ctx, prompt)
   │
   ├─ Client.Chat(ctx, req)
   │      └─ http.NewRequestWithContext(ctx, ...) // 标准库在取消时立即终止 HTTP
   │
   └─ Tool.Execute(ctx, args)
          └─ 工具内部如有 io 操作必须接 ctx，否则 ctrl+c 期间该 IO 继续（建议：fs 工具使用 ctx 仅用于"即将被取消时的提前退出门")
```

每轮结束 REPL `cancel()` 释放子 ctx，root ctx 保持有效以支持多轮。

### 7.3 可观测性（首版最小集）

- **`--debug` 开启后**打印所有 `Event` 类型到 stderr。
- 普通模式下：只打印 assistant content（stdout）+ tool call/result 一行（stderr）。
- 不引入第三方 log 库；用 `fmt.Fprintln(os.Stderr, ...)` 即可。
- 不引入 metrics / tracing（M5 之后再加）。

---

## 8. 验收矩阵（详细）

下表把 [PLAN.md §6](./PLAN.md#6-里程碑总览) 的"验收信号"扩展为可勾选的子项。每行对应一个自动或手动的检查点。

| 里程碑 | 自动测试 | 手动端到端 |
| --- | --- | --- |
| **M0** | `go test ./internal/config/... -race`：(a) YAML 全字段；(b) env 全字段；(c) 混合覆盖；(d) 默认值回落；(e) YAML 非法字段报错 | `go run ./cmd/dsh -h` 输出 flag 帮助 |
| **M1** | `go test ./internal/llm/... -race`：(a) 请求携带 `tools`；(b) `tool_calls` 解析（含 nested function.arguments）；(c) 401 返回 `*APIError`；(d) ctx 取消立即返回 | `go run -tags probe ./cmd/llmprobe` 联调真实 base-url |
| **M2** | `go test ./internal/tool ./internal/tools/... -race`：(a) 注册/重名/查找/并发；(b) greet 参数缺失 `IsError`；(c) fs_read 正常+不存在；(d) fs_write 四用例（正常/`../` 拒/绝对路径拒/MkdirAll）| — |
| **M3** | `go test ./internal/agent/... -race`：mock LLM 返回"先 tool_calls(greet)+ 后 assistant 文本"，断言：(a) 事件顺序；(b) tool 回填；(c) `RunResult.Rounds == 2`；(d) ctx 取消时不进入下一轮 | — |
| **M4** | `go test ./cmd/dsh/... -race`：(a) `-prompt` 模式；(b) `-config` 解析；(c) `--debug` 输出含全部事件；(d) EOF 退出 | 实际跑 `(a) greet 演示；(b) fs_read 演示；(c) Ctrl+C 取消当前轮；(d) exit 干净退出；(e) -race` |

### 8.1 端到端示例会话（`docs/examples/greet-session.md`）

```text
$ go run ./cmd/dsh -config ./harness.example.yml -debug
dsh> ready. type your prompt, 'exit' to quit.

dsh> 请使用 greet 工具向 Ada 打招呼
  → tool greet({"name":"Ada"})
  ← greet (ok, 312µs)
你好，Ada！我是 dsh。

dsh> exit
$
```

---

## 9. 附录 A：与上游 / Java 版的差异点

| 点 | 上游 TS（cordis） | Java 版 | Go v1（本设计） |
| --- | --- | --- | --- |
| 包/模块拆分 | `packages/*`（cordis scope） | Maven 10 模块 + DDD 端口 | `internal/{config,llm,tool,agent,harness}` 4 + 1 模块 |
| 容器 | cordis `Context` | Spring `ApplicationContext` | 显式 `Runner` 接口 + 手动装配 |
| 插件 | cordis plugin（含 `.codex-plugin`、`cordis.yml`、`acp`、`sdk`）| JAVA_NATIVE / Node Bridge / MCP | **无**（M6 引入） |
| LLM 多协议 | 多 Provider | 多 Adapter + 渠道管理 | 1 个 OpenAI 兼容实现（M5+ 多协议） |
| 流式 | 全程 SSE | 全程 SSE | 阻塞（M5 转流） |
| 工具结果 | `Content[]` 多模态 | `ToolExecutionResult` | `tool.Result{Content, IsError}` |
| Session | Cordis EventLog + persistent | SessionLog WAL + MySQL | 无（M7 引入 sqlite） |
| 取消 | cordis `dispose` | `AtomicBoolean` + 100ms polling | `context.Context` |
| 工具接口 | Cordis `Tool`（带 `parameters` JSON Schema） | `AbstractTool` | 同形但 `Parameters() any` |
| 错误回填 | 同 Java | `Fail(message, code)` 路径 | `Result{IsError:true}` + `[ERROR]` 前缀 |

---

## 9. v1.2 收尾补丁（v1.1 → v1.2）

> 本节记录 v1.1 落地后代码 review 发现的硬伤与小修；**不破坏任何对外接口**。
>
> 适用版本：deepseek-harness-go v0.1.0-M4（同 v1.1）。

### 9.1 收尾清单

| ID | 类别 | 位置 | 问题 | 修复 |
| --- | --- | --- | --- | --- |
| **R1** | 可观测 | `agent/runner.go:loop` | `max_rounds` 退出前没有发 `PhaseChange{PhaseStopped}`，debug 模式下无法区分"主动停"与"被取消" | 在 `max_rounds` 出口处补一发 `PhaseChange{PhaseStopped}` |
| **R2** | 健壮性 | `agent/runner.go:loop` | 双层 defer panic 防护缺失：若 `res <- RunResult` 时 panic（res 已被关闭），整个进程崩溃 | 在 outer defer 内加 inner defer `_ = recover()` 兜住 |
| **R3** | 死代码 | `cmd/dsh/repl/repl.go` | `var lastAssistant string` + `_ = lastAssistant` 残留；`consumeEvents(..., prompt string)` 的 `prompt` 参数未使用 | 删除 dead var；`consumeEvents` 改为 `(events, debug)` |
| **R4** | 诊断 | `agent/runner.go:safeExecute` | 工具 panic 与"args JSON 解析失败"被统一报为 panic，**但其实只有 panic 会逃出 Execute**——后者本来就是工具自身的 `Result{Err}`，与 shim 无关 | 把 panic 信息改成 `panic in tool X: ...`，保留 `[ERROR]` 前缀 |
| **R5** | 错误处理 | `config/config.go:overrideEnv` | env 类型转换失败（如 `DEEPSEEK_MAX_TOKENS=abc`）**静默吞错**，用户配错毫无线索 | 收集所有转换错误并在 `Load` 返回时 fail-fast 报错 |
| **R6** | 配置一致性 | `cmd/dsh/main.go` | `cfg.Agent.Debug=true` 没有传到 REPL，**配置承诺失真** | CLI flag 优先；未传 flag 时回落到 `cfg.Agent.Debug` |
| **S1** | 测试覆盖 | `agent/runner_test.go` | 没有覆盖 "runner 内部 panic 后仍要送 RunResult" 路径 | 新增 `TestRun_RunnerPanicStillDeliversResult` |
| **S2** | 测试覆盖 | `agent/runner_test.go` | 没有覆盖 "max_rounds 时必须有 PhaseStopped 事件" | 新增 `TestRun_MaxRoundsEmitsPhaseStopped` |
| **S3** | 测试覆盖 | `config/config_test.go` | 没有覆盖 "env 类型错应报错" | 新增 `TestLoad_BadEnvValueFailsLoud`（5 个子用例） |

### 9.2 累计测试

| 包 | v1.1 | v1.2 | 增量 |
| --- | --- | --- | --- |
| `internal/config` | 7 | 12 | +5 |
| `internal/llm` | 11 | 11 | 0 |
| `internal/tool` | 9 | 9 | 0 |
| `internal/tools` | 13 | 13 | 0 |
| `internal/agent` | 14 | 17 | +3 |
| `cmd/dsh/repl` | 5 | 5 | 0 |
| **合计** | **59** | **67** | **+8** |

注：v1.1 表中曾写"59 个测试"，含子用例；v1.2 真实数为 **67**（已重数）。

---

## 10. 附录 B：M5 之后才有意义的扩展点（明示"当前不做"）

| 扩展点 | 当前约束 | 解锁里程碑 |
| --- | --- | --- |
| `Client.ChatStream` | 未实现，调用会返回错 | M5 |
| `StreamRunner` | 未实现 | M5 |
| HTTP API（`/api/agent/message`、`/api/agent/stream`） | 未实现 | M5 |
| 多 LLM 协议（Anthropic Messages、Ollama 原生） | 未实现 | M5+ |
| 插件 / 沙箱 / 审批 / Skill | 未实现 | M6 / M7 |
| Session 持久化 | 每个 prompt 独立会话，不持有 history | M7 |
| 上下文压缩 | 不压缩（依赖 LLM context window） | M7 |
| `usage` 统计 / cost / metrics | 仅 `Usage` 字段透传，不聚合 | M7+ |
| 多语言（cobra、lipgloss 等） | 仅 stdlib | M5 |

---

## 11. 第二版路线图（M5 范围预告）

> **📦 本节已被提升为独立设计文档：[DESIGN-v2.md](./DESIGN-v2.md)（578 行，M5 详细 + M6–M8+ 提纲）。**
> 本节**保留为 v1.2 的历史快照**，不再单独更新；新设计、决策、变更请去 DESIGN-v2。
>
> 下面表格为 v1.2 时期的预告快照，与 DESIGN-v2 不保证逐字段一致。

### 11.1 范围

| 子能力 | 描述 | 价值 | 阻塞依赖 |
| --- | --- | --- | --- |
| **M5a 流式 LLM** | SSE 增量推送；新事件 `AssistantDelta{Text, Index}`；REPL 实时打字 | 当前 5–30s 阻塞体验彻底改变 | `internal/llm` 加 `ChatStream` |
| **M5b HTTP API** | `POST /api/agent/message`、`GET /api/agent/stream`、`GET /api/sessions` | dsh 能被外部进程调用 | M5a |
| **M5c Session 持久化** | sqlite（`~/.dsh/sessions.db`）；Begin/Append/Load/List | 多轮历史；可回放；为 M7 铺路 | 无 |
| **M5d Usage 聚合** | 每轮累计 prompt/completion tokens；可选 cost 计算 | 用户唯一可观测的成本信号 | `llm.Usage` 已透传 |
| **M5e REPL 命令扩展** | `/history`、`/clear`、`/model`、`/usage` | 长会话下必要 | M5c/M5d |

### 11.2 不做（推迟到 v3+）

- MCP（Model Context Protocol）协议适配
- Skill 系统（Markdown-based skill 加载）
- agent 子进程隔离 / sandbox
- Shell 工具 `shell_exec`（安全审批）
- 多 LLM 渠道（Anthropic Messages native、Ollama 原生、Gemini）
- 上下文压缩（prompt 截断 / 摘要）
- 插件 / 进程外扩展

### 11.3 拟新增的包 / 类型

```
internal/stream/                 ── SSE 解析 + chunk 重组（独立可测）
internal/server/                 ── net/http 路由 + 鉴权 + 优雅关闭
internal/session/                ── sqlite + 接口；Begin/Append/Load/List
   ├─ mem.go                     ── in-memory 实现（测试用）
   └─ sqlite.go                  ── modernc.org/sqlite（pure-Go）持久化实现
internal/usage/                  ── Usage 聚合 + cost 计算
```

### 11.4 数据流（流式 + Session）

```text
REPL prompt
   │
   ▼
session.Begin() → session.ID
   │
   ▼
runner.RunStream(ctx, prompt, session.ID)        ← LoopRunner 新方法
   │
   │  emit PhaseChange(Init)
   ▼
for each round ≤ MaxRounds:
   ├─ emit PhaseChange(LLMCall)
   ├─ client.ChatStream(ctx, req) ──→ SSE chunks ──→ emit AssistantDelta{Text}
   ├─ final Choice.Message → session.Append(assistant)
   ├─ no tool_calls → emit AssistantMessage; break
   ├─ for each tool_call:
   │    ├─ emit ToolCallStart
   │    ├─ tool.Execute(ctx, args)
   │    ├─ emit ToolResult
   │    └─ session.Append(tool_message)
   └─ emit UsageUpdate (累计)
   │
   ▼
session.Save()
   │
   ▼
runner returns RunResult{FinalMessages, Rounds, Usage, StopReason}
```

### 11.5 配置层扩展（v2 新增段）

```yaml
server:                              # M5b
  enabled: false                     # --serve 时为 true
  listen: "127.0.0.1:8080"
  auth-token: ""                     # 可选 Bearer；env: DSH_SERVER_AUTH_TOKEN

session:                             # M5c
  enabled: true                      # --no-session 可关闭
  db-path: ~/.dsh/sessions.db        # env: DSH_SESSION_DB
  max-rounds-kept: 100               # 软上限；超出后归档

stream:                              # M5a
  enabled: true                      # --no-stream 可回退到阻塞

usage:                               # M5d
  enabled: true
  cost-per-1k-tokens:                # 可选：模型 → $/1k
    glm-5.3-flash: 0.0001
```

### 11.6 向后兼容承诺

v2 落地**不破坏**以下 v1.x 行为：

- `Config.Load(path)` 签名不变；YAML 旧字段照样工作
- `llm.Client.Chat` 接口不变；`ChatStream` 作为新方法加入
- `agent.Runner.Run` 签名不变；`RunStream` 作为新方法加入
- 所有 v1.x 测试在 v2 下保持通过

### 11.7 验收矩阵（M5 拆分）

| 子能力 | 自动测试 | 手动端到端 |
| --- | --- | --- |
| **M5a 流式** | httptest SSE fixture（`data: {...}\n\n` 多帧）；断言 delta 顺序、最终 message 完整 | `./dsh -stream` 看到打字效果 |
| **M5b HTTP** | `net/http/httptest` 覆盖 `/message`、`/stream`；401/200/500；流式断流 | `curl -N localhost:8080/api/agent/stream` |
| **M5c Session** | in-memory + sqlite 两种 backend 各跑一遍；Begin/Append/RoundTrip 一致 | `--session` 后 `/history` 能看到历史 |
| **M5d Usage** | mock client 返回固定 usage；assert 累计正确；cost 配置命中 | REPL footer 显示本轮 cost |
| **M5e REPL** | REPL 跑 `/usage`、`/history` 不崩溃 | 实际交互验证 |

### 11.8 拟定的实施节奏

| 步骤 | 内容 | 预估 |
| --- | --- | --- |
| 0 | v1.2 收尾补丁（已完成） | ✅ |
| 1 | 把 §11 提升为正式 DESIGN.md v2 章节；写详细伪码 | 半天 |
| 2 | M5a 流式 LLM（SSE 解析 + delta 事件 + REPL 实时渲染） | 1 天 |
| 3 | M5c Session（sqlite + 接口 + Runner 集成） | 半天 |
| 4 | M5d Usage（聚合 + cost 配置 + footer） | 半天 |
| 5 | M5b HTTP API（net/http + 路由 + 鉴权） | 1 天 |
| 6 | M5e REPL 命令扩展 | 半天 |
| 7 | 端到端验收 + README 更新 + PLAN §6 勾选 | 半天 |

合计 **约 4–5 天**。

---

**版本**：v1.2（2026-09-14 第三版：v1.1 + 代码 review 收尾 6 处硬伤 R1–R6 + S2 小修）。第二版（M5 流式/HTTP/Session/Usage）见 [§11](#11-第二版路线图-m5-范围预告)。
**下次更新**：M5 完成后把 §11 提升为正式章节；M7 完成后追加 Session / Approval 章节。
