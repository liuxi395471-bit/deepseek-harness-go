# DeepSeek Harness Go 版 — 第二版设计文档（v2 / DESIGN-v2）

> 本文档是 [DESIGN.md](./DESIGN.md) v1.2 的**继任设计**：负责规划 v1 之后的所有里程碑（M5 → M8+），并把 v1 留下的范围预告 [DESIGN.md §11](./DESIGN.md#11-第二版路线图-m5-范围预告) 提升为正式设计。
>
> 适用版本：deepseek-harness-go **v2.0.0**（M8+ 完成后）
> 阅读顺序建议：§0 原则与兼容承诺 → §1 全局架构 → §A M5 详细设计 → §B M6 插件 → §C M7 Session/Approval/Shell → §D M8+ → §E 配置迁移 → §F 验收矩阵 → §G 节奏
>
> **v2 增量目标**：把 dsh 从"单进程 CLI 玩具"升级为"可被外部调用的长会话 agent 运行时"，同时**不破坏**任何 v1.x 行为。

---

## §0. 设计原则与向后兼容承诺

### §0.1 八条原则（v2 写作时遵守）

| # | 原则 | 含义 |
| --- | --- | --- |
| P1 | **接口只增不破** | v1 公开签名一律保留；新能力通过新方法/新类型/新配置段加入。 |
| P2 | **阻塞路径仍是默认** | 流式/插件/Session 默认开启，但 `Stream=false`/无插件/无历史也能跑；老用户的 YAML 不改可直接跑。 |
| P3 | **包按层切，禁止循环** | `cmd/*` → `internal/{agent,tool,llm,config,store,server,stream,usage}`；新增包不引入反向依赖。 |
| P4 | **错误不冒泡到 Runner** | 沿用 v1 规则：tool 失败 → `Result.IsError=true` 回模型；网络/SSE 失败 → 拆分 *recoverable*（重试 N 次）与 *fatal*（断流回 canceled）。 |
| P5 | **取消语义统一** | v1 的 `context.Canceled → StopReason="canceled"` 在每个新模块都保留；HTTP 断连 → cancel ctx。 |
| P6 | **可观测性是基础设施** | 所有 I/O（HTTP/SSE/SQL）走 `internal/obs` 的统一接口，但**默认实现为 no-op**，不强制依赖第三方日志库。 |
| P7 | **最小三方依赖** | 新增三方库只允许：`modernc.org/sqlite`（pure-Go，强制）；M6 gRPC 时允许 `google.golang.org/grpc` + `protobuf`。日志/JSON/重试库一律使用 stdlib。 |
| P8 | **测试不绕开接口** | 新接口必须配套 `*_test.go`；端到端加 `e2e/` 子包；不再允许"先合并后补测试"。 |

### §0.2 不破坏清单（v1.x → v2.x 兼容承诺）

| v1 公开元素 | v2 行为 |
| --- | --- |
| `config.Load(path) (Config, error)` | 签名不变；缺新段走默认值 |
| `llm.Client.Chat(ctx, ChatRequest) (ChatResponse, error)` | 签名不变；`Stream=true` 由 v1 的 `ErrStreamNotSupported` 升级为真正流式（行为变更，**但仅在 v2 build tag 下默认开启**——见 §A.6） |
| `agent.Runner.Run(ctx, prompt) (<-chan Event, <-chan RunResult)` | 签名不变；事件类型集合**只增不删** |
| `agent.Runner` interface | v1 仅声明 `Run`；v2 新增 `StreamingRunner` interface 声明 `RunStream`；`LoopRunner` 双实现两者。v1 mock Runner 在 v2 下编译不会报错。 |
| `tool.Tool` 接口（4 方法） | 完全不变；插件形态由 `tool.Registry.Register` 扩展 |
| `tool.Registry` | v1 方法（`Register/Get/Names/Specs/MustRegister/MustGet/Len`）全部保留；v2 加 `RegisterFunc` 等价便捷方法 |
| `agent.RunResult` 字段 | `FinalMessages/Rounds/StopReason/Error` 全保留；v2 加 `Usage SessionID` 字段（仅新增） |
| `llm.Message` struct | v1 字段全保留；v2 仅允许新增字段（如 `Usage *llm.Usage`、`Timestamp time.Time`） |
| REPL 命令 `/exit` `Ctrl+C` `Ctrl+D` 行为 | 完全保留 |
| `harness.yml` 已写字段 | 全部生效；缺新段走默认值 |
| 所有 v1 单测 | 在 v2 下必须维持 `go test ./...` 全绿 |

### §0.3 显式破坏清单（仅在 v2 build 下生效，默认关）

- v2 build 标志：当前所有 v1.x 分支保持 build 不变；v2 整合后切到单一 `cmd/dsh` 二进制时启用下述行为。
- `llm.Client.Chat` 的 `Stream=true` 在 v1 默认返回 `ErrStreamNotSupported`；v2 默认真流式。**v1 行为可通过 `-no-stream` 显式回退**。
- CLI flag 新增分两类：
  - **配置覆盖**（同名覆盖 YAML）：`-base-url` `-model` `-max-rounds`
  - **开关**：`--serve` `--session` `--no-stream`
- YAML 配置：`server:` `session:` `stream:` `usage:` 段均为**可选**，缺省全部 false / 默认值；旧 YAML 不报错。

---

## §1. v2 全局架构

### §1.1 包图（v1 + v2 增量）

```text
cmd/dsh/                          ── v1：装配 + REPL；v2：装配 + REPL + 服务模式
├── main.go                            ── flag 解析 → config → 装配 → REPL 或 server
└── repl/                              ── v1：IO 事件消费；v2：加 /history /usage /model 子命令

cmd/llmprobe/                      ── v1：调试探针；v2：加 --stream 演示

internal/
├── config/                        ── v1：YAML + env + default；v2：加 server/session/stream/usage 段
├── llm/                           ── v1：OpenAI 兼容 Chat；v2：加 ChatStream（interface）+ SSE wire-format
│   ├── client.go                       ── OpenAICompatibleClient 加 Streaming(bool) 选项
│   ├── types.go                        ── 加 StreamChunk / AssistantDeltaEvent
│   └── sse.go                          ── 新增：SSE 帧解析（独立可测，stdlib bufio.Scanner）
├── tool/                          ── v1：Tool interface + Registry；v2：兼容，Registry 加可选 Loader
├── tools/                         ── v1：3 个内置；v2：加 shell（受 Approval 管控）+ history（v2 旁路）
├── agent/                         ── v1：ReAct loop；v2：保留 Run，新加 RunStream
│   ├── event.go                        ── v1 事件不变；加 AssistantDelta{Index} 字段
│   ├── runner.go                       ── LoopRunner 加 RunStream（共享 loop body）
│   ├── system.go                       ── v1 prompt 组装；v2 拼接历史摘要（如有 session）
│   └── prompt.go                       ── 新增：构造 messages slice（统一 Run 与 RunStream）
├── store/                         ── v2 新增（取代 v1 §11 暂定名 internal/session/）
│   ├── store.go                        ── interface Store{ Begin/Append/Load/List/Close }
│   ├── mem.go                          ── InMemory 实现（测试用）
│   └── sqlite.go                       ── modernc.org/sqlite 实现（生产用）
├── stream/                        ── v2 新增；SSE chunk → AssistantDelta 重组（独立可测）
│   └── stream.go                       ── Recombiner{deltas []byte} + Final()
├── server/                        ── v2 新增；net/http 路由 + Bearer 鉴权 + 优雅关停
│   ├── server.go                       ── NewServer + Run(ctx) + ListenAndServe
│   ├── auth.go                         ── Bearer token middleware
│   ├── handlers_agent.go               ── POST /api/agent/message / GET /api/agent/stream
│   └── handlers_session.go             ── GET /api/sessions / GET /api/sessions/:id
├── usage/                         ── v2 新增；Usage 聚合 + 可选 cost
│   ├── tracker.go                      ── per-session token 累计
│   └── cost.go                         ── 用 YAML 配置做 cost 计算（默认无 cost）
└── obs/                           ── v2 新增；统一 logger/Hook 接口（默认 no-op）
    └── obs.go
```

依赖图（v2）：

```text
cmd/*  →  internal/{agent,tool,llm,config,store,server,stream,usage,obs}
internal/agent      →  {llm, tool, store*, usage*, stream*}
internal/store      →  obs
internal/server     →  {agent, store, obs}
internal/stream     →  {llm(只取 types), obs}
internal/usage      →  {llm(只取 types), obs}
internal/llm        →  obs
internal/tool       →  llm(只取 types)
internal/config     →  （无）
internal/obs        →  （无）

(* 表示可选依赖：loop 不带 history / usage 时这两个接口为 nil)
```

### §1.2 数据流全景（v2 顶级）

```text
            ┌────────────── CLI REPL ──────────────┐
            │                                      │
            │   prompt   ┌── /api ──┐   prompt      │
user ───────►            │          │  ───────────► │
            │     ──HTTP─►│   server │              │
            │            │          │              │
            │            └────┬─────┘              │
            │                 │                    │
            │                 ▼                    │
            │       agent.Runner.RunStream         │
            │                 │                    │
            │   ┌─────────────┼─────────────────┐  │
            │   │             ▼                 │  │
            │   │   ┌──────────────────┐        │  │
            │   │   │  llm.Client      │        │  │
            │   │   │  .ChatStream()   │        │  │
            │   │   └────────┬─────────┘        │  │
            │   │            ▼                  │  │
            │   │     stream.Recombiner        │  │
            │   │            ▼                  │  │
            │   │      AssistantDelta           │  │
            │   │            ▼                  │  │
            │   │       (tool calls?)           │  │
            │   │        │       │              │  │
            │   │       no      yes             │  │
            │   │        ▼       ▼              │  │
            │   │    done    tool.Exec ──► store.Append│
            │   │             ▲                │  │
            │   │             └── loop ────────┘  │  │
            │   └─────────────────────────────────┘  │
            │                 │                    │
            │                 ▼                    │
            │   RunResult{Usage, SessionID, …}     │
            │                 │                    │
            │            usage.Tracker               │
            │                 │                    │
            │            REPL render                 │
            │            HTTP response              │
            └──────────────────────────────────────┘
```

### §1.3 进程模型

| 进程 | 何时启动 | 职责 |
| --- | --- | --- |
| `dsh --serve` | 显式 `-serve` 或 `server.enabled=true` | HTTP 服务；可单独 daemon |
| `dsh` (REPL) | 默认 | 交互式；可同时启用 session |
| `llmprobe` | 调试 | 与 v1 同 |

## §A. M5 详细设计

### §A.0 M5 总目标

把 dsh 从"5–30s 静默阻塞"升级为"打字机式流式响应、可被 HTTP 调用、可选历史持久化、可观测 token 成本"。M5 拆为 5 个子能力：M5a 流式 LLM、M5b HTTP API、M5c Session 持久化、M5d Usage 聚合、M5e REPL 命令扩展。

### §A.1 M5a — 流式 LLM（SSE）

### §A.1.1 目标

- `llm.Client` 接口加 `ChatStream(ctx, ChatRequest) (<-chan StreamChunk, <-chan error)`。
- `agent.Runner` 加 `RunStream(ctx, prompt, sessionID)`，行为等价于 `Run` 但每个 round 把 LLM 返回拆为 `AssistantDelta` 增量事件。
- REPL 收到 `AssistantDelta` 直接 `fmt.Print` 拼接（**不换行**），round 结束收到 `AssistantMessage` 时换行。
- 兼容 v1：`Run` 仍可用；`Stream=false` 时一条 `AssistantMessage` 完整抵达。

### §A.1.2 接口草案

```go
// internal/llm/types.go（v2 新增）
type StreamChunk struct {
    Text      string // 当前帧增量文本
    Index     int    // choice index（OpenAI 多 choice 时）
    Finish    string // "stop" / "tool_calls" / ""；最后一帧必填
    ToolCalls []ToolCall // 最后一帧可能携带；增量阶段为 nil
}

// internal/llm/client.go（v2 扩展 interface）
type Client interface {
    Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
    ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, <-chan error)
}

// internal/agent/event.go（v2 字段增量）
type AssistantDelta struct {
    Text  string
    Index int
}
```

### §A.1.3 流式数据流

```text
Runner.RunStream(ctx, prompt, sid)
  │
  ├─ Store.Begin/Load → sessionID = sid
  ├─ msgs := session.Messages ∪ {RoleUser, Content: prompt}      ← store=nil 时仅 user
  ├─ emit PhaseChange{Init}
  │
  for round in 1..MaxRounds:
      │
      ├─ emit PhaseChange{LLMCall}
      │
      ├─ chatErrCh, chunkCh := Client.ChatStream(ctx, req)
      │
      ├─ for chunk := range chunkCh:
      │     │
      │     ├─ chunk.Text != ""          → emit AssistantDelta{Text: chunk.Text, Index: chunk.Index}
      │     ├─ chunk.ToolCalls partial   → Recombiner.ToolCallAccum.Append(...)
      │     └─ chunk.Finish != ""        → break
      │
      ├─ if chatErrCh 收到 fatal         → emit LoopError; return RunResult{StopReason:"error"}
      ├─ if ctx cancel mid-stream        → 已发 deltas 仍 emit; return RunResult{StopReason:"canceled"}
      │
      ├─ assistant := Recombiner.Final()               // 完整 AssistantMessage
      ├─ msgs = append(msgs, assistant)
      ├─ emit AssistantMessage{Content, ToolCalls}
      ├─ Store.Append(assistant)
      │
      ├─ len(assistant.ToolCalls) == 0:
      │     ├─ emit LoopDone
      │     └─ return RunResult{StopReason:"no_tool_calls"}
      │
      ├─ for each tool_call:
      │     ├─ emit ToolCallStart
      │     ├─ tool.Exec → emit ToolResult
      │     ├─ Store.Append(tool_message)
      │     └─ emit UsageUpdate{DeltaUsage}             ← M5d 接入
      │
      └─ if round >= MaxRounds:
            ├─ emit PhaseChange{Stopped}
            └─ return RunResult{StopReason:"max_rounds"}
```

关键不变式：

- **delta 顺序**：同一 `Index` 的 delta 按抵达顺序 emit；不同 Index 并行时不交织。
- **错误折叠**：tool panic → Result{IsError:true}；SSE fatal → LoopError + StopReason="error"。
- **取消语义**：ctx cancel 时已 send 的 chunks 不会被丢弃；runner 仍把它们 emit。
- **partial tool_calls**：用 `Recombiner.ToolCallAccum` 按 `(index, tool_call_id)` 聚合 `arguments` 片段；末帧 `Finish=tool_calls` 时一次性产出完整 `[]ToolCall`。

### §A.1.4 错误与恢复

| 场景 | 行为 |
| --- | --- |
| chunk 解析失败（非法 JSON 帧） | 丢弃该帧，记 `obs.Warn`；**不**终止 loop |
| stream 中途 5xx | 整流判为 **fatal**；`StopReason="error"` |
| 流中途 429 | **recoverable**：退避重试 N 次（默认 1，间隔 200ms），仍失败再 fatal |
| 流中途 ctx cancel | 已发 chunks 仍 emit；`StopReason="canceled"` |
| SSE keep-alive comment（`: ping`） | 忽略 |
| tool_calls 跨多帧重组失败 | **fatal**；`StopReason="error"` 含 `"tool call assembly"` |
| 整体不支持 SSE（首帧 400/405） | 自动降级到 B 档；不视为错误（见 §A.6） |

### §A.1.5 配置与开关

```yaml
stream:
  enabled: true                   # 默认开；--no-stream 回退
  retry-on-429: 1                 # 429 退避重试次数（0 表示不重试）
  idle-timeout: 30s               # chunk 间最大间隔；超时即断流
```

### §A.1.6 测试矩阵

| 用例 | 断言 |
| --- | --- |
| 单 frame 多 chunk（`data: {...}\n\n` ×3） | delta 文本按序拼接 == 一次性 `Chat` 的 `Content` |
| 空流（服务端立刻断连） | `StopReason="error"` |
| 中途 200ms 间隔 `:ping\n\n` | 不影响 delta 顺序，不触发超时 |
| 末帧只含 `finish_reason` | 重组出完整 message，不多发 chunk |
| ctx 取消前已收 2 chunk | 这 2 个 chunk 仍 emit；不再触发下一 round |
| 429 后服务端 200 | retry 后正常出 message，调用次数 == 2 |
| tool_calls 跨 3 帧拼装 | Recombiner.ToolCallAccum 完整聚合 `arguments` |
| 首帧 400（不支持 SSE） | 自动降级到 B 档；后续走阻塞 Chat |

### §A.2 M5b — HTTP API

#### §A.2.1 路由总表

| Method | Path | 说明 | 入参 | 出参 |
| --- | --- | --- | --- | --- |
| `POST` | `/api/agent/message` | 阻塞；返回最终 `RunResult` JSON | `{prompt, session_id?}` | `{session_id, messages, usage, stop_reason}` |
| `GET` | `/api/agent/stream` | SSE；与 REPL 流式同源 | `?prompt=...&session_id=...` | text/event-stream，每帧 `event:` / `data:` |
| `GET` | `/api/sessions` | 列出会话 | — | `[{id, created_at, rounds, preview}]` |
| `GET` | `/api/sessions/{id}` | 取完整历史 | — | `{id, messages[], usage_total}` |
| `GET` | `/healthz` | 健康检查 | — | `{ok: true}` |
| (无) | (无) | **v2 不提供 DELETE/PUT**；session 清理走 §A.3.5 `archive-path` | — | — |

#### §A.2.2 鉴权

**默认 Bearer Token**，从 `server.auth-token` 或 env `DSH_SERVER_AUTH_TOKEN` 读取。空 token = **不允许启动 server**（防止误暴露）。除了 `/healthz`，所有 `/api/*` 都强制校验。

```go
// internal/server/auth.go
func bearerMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path == "/healthz" { next.ServeHTTP(w, r); return }
        h := r.Header.Get("Authorization")
        if !strings.HasPrefix(h, "Bearer ") { http.Error(w, "unauthorized", 401); return }
        tok := strings.TrimPrefix(h, "Bearer ")
        if subtle.ConstantTimeCompare([]byte(tok), []byte(serverToken)) != 1 {
            http.Error(w, "unauthorized", 401); return
        }
        next.ServeHTTP(w, r)
    })
}
```

#### §A.2.3 SSE 帧协议（与 REPL 事件流对齐）

每个 event 用 v1 已有 `Event` 类型序列化：

```text
event: phase_change
data: {"phase":"llm_call","at":"2026-09-14T10:00:00Z"}

event: assistant_delta
data: {"text":"你好","index":0}

event: assistant_message
data: {"content":"你好，Ada！","tool_calls":null}

event: tool_call_start
data: {"call":{...}}

event: tool_result
data: {"call_id":"...","name":"greet","content":"...","is_error":false,"took_ms":0.312}

event: usage_update
data: {"prompt_tokens":12,"completion_tokens":3,"total":15}

event: loop_done
data: {"rounds":1,"stop_reason":"no_tool_calls"}
```

客户端断连即取消请求 ctx → Runner 进入 `StopReason="canceled"`。

#### §A.2.4 错误与限额

| 情况 | 状态码 |
| --- | --- |
| missing/invalid JSON 入参 | 400 |
| 未带 Bearer | 401 |
| token 不匹配 | 401 |
| 同一 session 并发请求（POST 与 GET stream 撞 ID） | 409 |
| 服务端 panic | 500 + `event: loop_error\ndata: {...}` |
| ctx 超时（默认 0 = 不超时） | 504 |

#### §A.2.5 配置

```yaml
server:
  enabled: false                 # --serve 或设 true 才启
  listen: "127.0.0.1:8080"
  auth-token: ""                 # env: DSH_SERVER_AUTH_TOKEN
  request-timeout: 0             # 0 = 跟随 ctx；非 0 强制 cancel
  max-concurrent-sessions: 16
```

#### §A.2.6 测试矩阵

| 用例 | 断言 |
| --- | --- |
| `httptest` 跑 `POST /api/agent/message`（mock LLM） | 返回 `{messages, usage, stop_reason}` |
| 不带 `Authorization` | 401 |
| 错 token | 401 |
| 并发 `POST` + `GET /stream` 同 session | 后到者 409 |
| SSE client 断连 | 后端 ctx cancel，Runner `StopReason="canceled"` |
| panic in tool | server 仍 200，body 含 `event: tool_result` `is_error=true` |

### §A.3 M5c — Session 持久化

#### §A.3.1 包结构调整

v1 §11 暂定 `internal/session/`，v2 改名 **`internal/store/`**——因为它最终承载的不只是 sessions，而是任何"状态可序列化、可恢复"的抽象（未来可能加 `store/casbin/` `store/queue/` 等）。

#### §A.3.2 接口

```go
// internal/store/store.go
type Session struct {
    ID         string
    CreatedAt  time.Time
    UpdatedAt  time.Time
    Rounds     int
    Preview    string         // 第一条 user msg 截断 80 chars
    Messages   []llm.Message  // 完整对话，含 system/user/assistant/tool
    UsageTotal llm.Usage      // 累计
}

type Store interface {
    Begin(ctx context.Context) (Session, error)        // 分配 UUID，初始化 row
    Append(ctx context.Context, id string, msg llm.Message) error
    Load(ctx context.Context, id string) (Session, error)
    List(ctx context.Context, limit, offset int) ([]Session, error)
    UpdateUsage(ctx context.Context, id string, delta llm.Usage) error   // 多轮使用；v2 仅增加 UsageTotal
    Close() error
}
```

#### §A.3.3 两套实现

| 文件 | 用途 | 依赖 |
| --- | --- | --- |
| `store/mem.go` | `MapStore`，所有数据在内存；测试用 | 无 |
| `store/sqlite.go` | `SQLiteStore`，默认路径 `~/.dsh/sessions.db` | `modernc.org/sqlite`（pure-Go） |

SQLite schema：

```sql
CREATE TABLE IF NOT EXISTS sessions (
    id          TEXT PRIMARY KEY,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    preview     TEXT,
    rounds      INTEGER DEFAULT 0
);
CREATE TABLE IF NOT EXISTS messages (
    session_id  TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    seq         INTEGER NOT NULL,
    role        TEXT NOT NULL,
    content     TEXT,
    tool_call_id TEXT,
    tool_calls  TEXT,           -- JSON
    PRIMARY KEY (session_id, seq)
);
CREATE TABLE IF NOT EXISTS usage (
    session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    prompt_tokens     INTEGER DEFAULT 0,
    completion_tokens INTEGER DEFAULT 0,
    total_tokens      INTEGER DEFAULT 0
);
```

#### §A.3.4 Runner 接入

```go
// internal/agent/runner.go（v2）
type LoopRunner struct {
    // ... v1 fields ...
    Store  store.Store   // 可为 nil；nil 则不持久化
    Usage  *usage.Tracker // 可为 nil
}

func (r *LoopRunner) RunStream(ctx context.Context, prompt string, sid string) (...) {
    // 1. 若 sid == "" 且 Store != nil → Store.Begin → sid
    // 2. 若 sid != "" → Store.Load → msgs = sess.Messages
    // 3. 走原 loop body（v1 同款）
    // 4. round 结束：msg → Store.Append；usage → Usage.Add
}
```

#### §A.3.5 配置

```yaml
session:
  enabled: true                              # --no-session 关
  db-path: ~/.dsh/sessions.db               # env: DSH_SESSION_DB
  max-rounds-kept: 100                       # 软上限；超出归档
  archive-path: ~/.dsh/sessions.archive/     # 归档目录
```

#### §A.3.6 测试矩阵

| 用例 | 断言 |
| --- | --- |
| `MapStore` Begin/Append/Load/List | round-trip 一致 |
| `SQLiteStore` 同上（t.TempDir() 创 DB） | round-trip 一致 |
| Append 后 List，`limit=10 offset=0` | 只返回前 10 条 |
| 关闭 store 后再 Open 旧 DB | 数据持久 |
| Runner 与 MapStore 集成跑 2 轮 | session.Messages 长度 == system+user+assistant+tool+assistant = 5 |
| 触发 `max-rounds-kept` 归档 | 旧 row 移到 archive 目录 |
| SQL 注入式 session id | 参数化绑定，不爆 |

#### §A.3.7 迁移/淘汰策略

v2 引入 store 后，v1.2 的"无历史"行为仍保留——只要 `Store=nil` 或 `session.enabled=false`。**不写迁移脚本**，因为 v1 本来就没有历史。

### §A.4 M5d — Usage 聚合与 cost

#### §A.4.1 接口

```go
// internal/usage/tracker.go
type Tracker struct {
    mu      sync.Mutex
    Per     llm.Usage            // 当前轮
    Total   llm.Usage            // session 累计
    Cost    float64              // 可选；cost 配置为空时恒为 0
}

func (t *Tracker) Add(delta llm.Usage)
func (t *Tracker) Snapshot() UsageSnapshot
type UsageSnapshot struct {
    Prompt     int
    Completion int
    Total      int
    Cost       float64
    CostCurr   string            // "USD" 等
}
```

#### §A.4.2 Cost 配置

```yaml
usage:
  enabled: true
  cost-per-1k:
    deepseek-chat: 0.00014       # USD/1k output，input 通常免费或更低
    deepseek-reasoner: 0.00055
  currency: USD
```

未匹配模型名的 cost 留空，**不报错**；只输出 token 数字。

#### §A.4.3 展示位置

- REPL：每轮结束打印一行 `[usage] prompt=42 completion=17 total=59 cost=$0.000`
- HTTP：`RunResult.Usage` 字段 + `/api/sessions/{id}` 的 `usage_total`
- `--usage` REPL 命令：自上次启动累计

#### §A.4.4 测试矩阵

| 用例 | 断言 |
| --- | --- |
| Mock client 返回固定 Usage | Tracker.Total 对应字段累加正确 |
| cost 配置命中模型 | Cost 计算 == (output_tokens/1000) * price |
| cost 配置 miss 模型 | Cost=0；不 panic |
| 并发 Add | race detector 干净 |

### §A.5 M5e — REPL 命令扩展

#### §A.5.1 新增子命令

| 命令 | 说明 | Session 依赖 |
| --- | --- | --- |
| `/history` | 列**当前 session**的最近 N 条 message（默认 20） | `session.enabled=true` |
| `/history <id>` | **切换**当前 session 为 `<id>`（若存在） | 同上 |
| `/clear` | 清屏，**不**删历史 | — |
| `/model <name>` | 切换运行时模型（REPL 本地 trust，不走 server 白名单） | — |
| `/usage` | 打印自启动累计 usage | `usage.enabled=true` |
| `/stream on|off` | 切流式/阻塞 | — |
| `/sessions` | 列**全部**历史 session（id / created / preview） | `session.enabled=true` |
| `/help` | 列命令清单 | — |

#### §A.5.2 与 v1 兼容

- 所有 v1 行为（直接输入文本即 prompt、`exit`、`Ctrl+C` `Ctrl+D`）保留。
- `/` 开头的输入被识别为命令；不含 `/` 的输入照常作为 prompt 发送。
- 命令解析在 `repl/repl.go` 增加一个 `parseLine` 函数；不动 v1 的 IO 路径。

#### §A.5.3 测试矩阵

| 用例 | 断言 |
| --- | --- |
| `/history` 未启用 session | 打印 `[history] disabled (set session.enabled=true)` |
| `/model gpt-x` 切模型 | 后续轮使用新模型；repl event 中可见 model 字段 |
| `/stream off` | 下一轮走阻塞路径，单独 AssistantMessage 一次性打印 |
| 输入 `/` | 视为空命令，不崩溃 |
| 并发 `/history` 与新 prompt | 不死锁 |

### §A.6 流式默认与回退策略（关键决策）

#### §A.6.1 三档流式状态

| 档位 | 含义 | 触发 |
| --- | --- | --- |
| **A. full** | 流式 + SSE + delta 增量 | `stream.enabled=true` 且 CLI 未传 `--no-stream` |
| **B. buffered** | 阻塞但带 timeout 与早期 flush | `--no-stream` 或服务端不支持 SSE |
| **C. blocking** | v1 行为：等全量一并返回 | `stream.enabled=false`（YAML）或 `--stream=blocking` |

#### §A.6.2 默认档位

**默认 A**——因为 v2 build 已经过去了一年，主流 OpenAI 兼容端都支持 SSE。如果命中不支持 SSE 的端，**自动降级到 B**（检测方法：第一次 SSE 请求 400/405 即视为不支持）。

#### §A.6.3 与 v1 行为对齐

v1 用户的 YAML 没有 `stream:` 段 → 默认 A。
v1 用户的代码调 `llm.Client.Chat` 不调 `ChatStream` → 走 C，与 v1 完全一致。
v1 的单测在 v2 下不加 `stream.enabled=true` 就仍走 C。**这是 v1 测试在 v2 仍能 pass 的关键。**

### §A.7 /model 切模型的实现

```go
// internal/agent/runner.go（v2 扩展）
type LoopRunner struct {
    // ...
    modelMu sync.RWMutex
    model   string
}

func (r *LoopRunner) SetModel(name string) {
    r.modelMu.Lock(); defer r.modelMu.Unlock()
    r.model = name
}

func (r *LoopRunner) currentModel() string {
    r.modelMu.RLock(); defer r.modelMu.RUnlock()
    return r.model
}
```

- `/model foo` 只改 runner.model，不动 config（重启回原值）。
- HTTP `POST /api/agent/message` 入参带 `?model=` 优先于 runner 当前值（per-request override）。
- server 对此字段强制白名单：`server.allowed-models: ["deepseek-chat"]`；缺省 = 不限制。
- **REPL `/model` 不受 `server.allowed-models` 约束**（本地 trust boundary）；只有 HTTP `?model=` 走白名单。两者语义不同。

### §A.8 M5 总验收

- `./dsh -config harness.yml` 默认进 A 档；输入 prompt 看到打字机效果。
- `./dsh -serve` 起 HTTP；`curl -N .../api/agent/stream?prompt=hi` 看到 SSE 帧。
- 同一 session 跑 3 轮，`/history` 列出 3 条；`/api/sessions` 也能看到。
- 跑 `--no-stream` 仍能跑（落到 B/C）；`stream.enabled=false` 走 C。
- 杀掉服务端，正在进行的 SSE 客户端读 EOF，runner StopReason=canceled。
- 所有 v1 单测维持绿色。

## §B. M6 — 插件系统（outline）

### §B.1 二选一：gRPC vs wasm

| 维度 | gRPC（外部进程） | wasm（in-process） |
| --- | --- | --- |
| 部署 | 子进程；语言无关 | 单文件 `.wasm`；需 Go wasm runtime |
| 性能 | IPC 开销，几百 µs | ~µs，但启动大 |
| 隔离 | 进程级；可以挂掉不杀主 | 沙箱需 runtime 支持 |
| 工具编写 | 任意语言 stub | Rust/AssemblyScript/TinyGo |
| 测试 | 端到端真实 | 难以跨 runtime 测 |

**v2 倾向 gRPC**——理由：(1) 隔离天然；(2) 第三方能用任何语言写工具；(3) Go 生态成熟 (`google.golang.org/grpc`)；唯一代价是第一次引第三方依赖。

### §B.2 接口草案

```go
// internal/plugin/plugin.proto
service ToolProvider {
    rpc Specs(Empty) returns (ToolList);
    rpc Execute(ExecuteRequest) returns (ExecuteResponse);
}

message Empty {}
message ToolList { repeated ToolSpec specs = 1; }
message ToolSpec {
    string name = 1;
    string description = 2;
    bytes  parameters_json = 3;  // JSON Schema
    string version = 4;
}
message ExecuteRequest {
    string name = 1;
    bytes  args_json = 2;
    string call_id = 3;
}
message ExecuteResponse {
    string content = 1;
    bool   is_error = 2;
}
```

### §B.3 插件形态

```go
// internal/plugin/loader.go
type Loader interface {
    Load(path string) (*tool.Registry, error)
}

// 内置：本地子进程 gRPC client 实现 (cmd: ./my-tool-plugin --socket ...)
```

### §B.4 注册时机

- `--plugin ./myplugin` 启动时 spawn 子进程 → gRPC handshake → Specs() → registry.Register
- `llm.Client` 加 per-tool RPC mapping

### §B.5 v2 范围与不做

**v2 做**：
- `internal/plugin/` 包 + gRPC client/server stub
- 一个样板 `examples/plugin/echo/`
- `--plugin` flag
- 子进程看门狗（崩溃自动重启上限 3 次）

**不做**：
- wasm runtime
- 远程插件（HTTP plugin registry）
- 权限模型（由 M7 Approval 处理）
- 插件工具走 Approval 路径：**v2 插件工具默认需 Approval**；插件作者可在 gRPC `ToolSpec` 元数据中声明 `requires_approval=false` 跳过

---

## §C. M7 — Session/Approval/Shell（详细到能落地）

### §C.1 Session 增强

M5c 已有 history 持久化；M7 加：
- **跨进程恢复**：`--resume <id>` 从 sqlite 装载历史继续聊
- **分支**：message 上加 `parent_seq`，允许从一个 user prompt 派生多个 assistant 路线
- **导出**：`/export <id> --format json|md` → 写文件
- **导入**：JSON → sqlite

### §C.2 Approval：人对危险工具的二次确认

```go
// internal/approval/approval.go
type Request struct {
    Tool    string          `json:"tool"`
    Args    json.RawMessage `json:"args"`
    Reason  string          `json:"reason"`           // 由工具主动说明
}
type Decision int
const (
    ApproveOnce Decision = iota       // 仅本次
    ApproveSession                     // 整 session 通行
    Deny
)

type Approver interface {
    Approve(ctx context.Context, req Request) (Decision, error)
}
```

**内置 4 个 Approver**：

| 类型 | 何时用 | 默认 |
| --- | --- | --- |
| `NoopApprover` | 测试 / 无需审批 | — |
| `AllowListApprover` | 工具白名单 | 包含 `greet/fs_read` |
| `TerminalApprover` | REPL 模式下 `--approval terminal` 弹问 | yes/no/session/no |
| `HTTPPollApprover` | 服务模式下轮询 `/api/approval/{id}` | — |

### §C.3 Shell 工具

```yaml
# harness.yml（v2 新工具段）
tools:
  shell:
    enabled: true
    approver: terminal           # noop / allowlist / terminal / http
    allowlist: ["ls", "cat", "git", "go"]      # 仅 match 这些命令前缀
    timeout: 30s
    max-output-bytes: 1048576    # 1 MiB
```

工具实现 `internal/tools/shell.go`：
1. 拼接 `cmd := exec.CommandContext(ctx, args[0], args[1:]...)`
2. `Approver.Approve(ctx, req)` —— deny 直接返回 `Result{IsError:true}`
3. `CombinedOutput()`，超 `max-output-bytes` 截断 + 标记
4. **强制**沙箱：禁用 `cd /`、对路径参数 `resolveInWorkspace`；与 fs_read/fs_write 同源校验

### §C.4 v2 不做

- 完整 RBAC（仅 allowlist + session 维度）
- 跨进程审批（v2 是同进程为主；HTTP 审批是 stretch）
- Audit log（v3+）

---

## §D. M8+ —— MCP / Skill / Sub-agent / 多渠道（outline）

### §D.1 MCP（Model Context Protocol）

- 新增 `internal/mcp/`
- 起 stdio 子进程跑 MCP server；通过 JSON-RPC 收集 tools
- Registry.Register 适配
- v2 范围：stdio MCP，单 server；多 server + 远端 HTTP MCP 推 v3

### §D.2 Skill

- `~/.dsh/skills/*.md` —— Markdown with YAML frontmatter
- title / description / body
- Runner 根据 model mention 自动注入 `system` prompt 段落
- v2 提供 `SkillLoader` + `skills_dir` 配置；不做热加载

### §D.3 Sub-agent

- 工具 `agent_spawn` 调子 LoopRunner（同一进程，不同 LoopRunner 实例，共享 Registry）
- 父子通信：events 上行汇总 → 父 runner 当 final answer
- v2 范围：同步子 agent 一次性返回；并发子 agent 推 v3

### §D.4 多渠道 LLM

| 渠道 | 协议 | v2 范围 |
| --- | --- | --- |
| OpenAI Chat Completions | https+json / SSE | ✅ 主线 |
| Anthropic Messages native | https+json / SSE | v2 实现 Client（`internal/llm/anthropic/`） |
| Ollama native | http+json | v3 |
| Gemini | http+json | v3 |

- `llm.Client` 不变；实现新增，靠 `config.llm.provider` 字段路由
- `llm.provider: openai` （默认） | `anthropic`
- env: `DSH_LLM_PROVIDER`
- v1 user YAML 不写 provider → 默认 openai，**绝不静默切到 anthropic**

### §D.5 上下文压缩（v3+）

- `internal/compaction/`：超 `max-context-tokens` 触发摘要压缩老 message
- v3 提供 `summary:truncate` + `summary:llm` 两种策略

## §E. 配置演进与迁移路径

### §E.1 v1 → v2 配置映射

| v1 字段 | v2 字段 | 说明 |
| --- | --- | --- |
| `llm.{base-url,api-key,model,max-tokens,timeout}` | 不变 | 完全兼容 |
| (v2 新) | `llm.provider` (`openai` \| `anthropic`) | v1 不写默认 openai |
| `agent.{max-rounds,workspace,temperature,system-prompt,debug}` | 不变 | 完全兼容 |
| (无) | `server: {enabled, listen, auth-token, request-timeout, max-concurrent-sessions, allowed-models}` | 新增；缺省值 |
| (无) | `session: {enabled, db-path, max-rounds-kept, archive-path}` | 新增；缺省 enabled=true |
| (无) | `stream: {enabled, retry-on-429, idle-timeout}` | 新增；缺省 enabled=true |
| (无) | `usage: {enabled, cost-per-1k:{...}, currency}` | 新增；缺省 enabled=true |
| (无) | `tools: {shell: {enabled, approver, allowlist, timeout, max-output-bytes}}` | 新增；缺省 enabled=false |

### §E.2 Env 变量映射

| v1 env | v2 env | 说明 |
| --- | --- | --- |
| `DEEPSEEK_BASE_URL` `DEEPSEEK_API_KEY` `DEEPSEEK_DEFAULT_MODEL` `DEEPSEEK_MAX_TOKENS` `DEEPSEEK_TIMEOUT` | 不变 | |
| `DSH_AGENT_MAX_ROUNDS` `DSH_AGENT_WORKSPACE` `DSH_AGENT_TEMPERATURE` `DSH_AGENT_SYSTEM_PROMPT` `DSH_AGENT_DEBUG` | 不变；兼容历史 `DEEPSEEK_AGENT_*` 命名（若读不到 `DSH_AGENT_*` 则回退 `DEEPSEEK_AGENT_*`） | |
| (无) | `DSH_SERVER_AUTH_TOKEN` `DSH_SERVER_LISTEN` | |
| (无) | `DSH_SESSION_DB` `DSH_NO_SESSION` | |
| (无) | `DSH_NO_STREAM` | |

### §E.3 优先级

单一规则：**CLI flag > env > YAML 字段 > 内置默认**。这条规则在 v1 已有，v2 不变。

### §E.4 不会做的事

- 不引入 `config:v2` 这种语义版本字段
- 不写"自动迁移脚本"——v1 → v2 升级只需 `cp v1-harness.yml harness.yml` 不报错即可
- 不删 v1 字段；只新增

---

## §F. 验收矩阵（M5–M8）

| 子能力 | 自动测试 | 手动端到端 |
| --- | --- | --- |
| **M5a 流式 LLM** | httptest SSE fixture 多帧；断言 delta 顺序；最终 message == 一次性 Chat 的 Content | `./dsh` 输入 prompt 看到打字效果 |
| **M5b HTTP API** | `httptest` 覆盖 /message /stream /sessions；401/200/500；流式断流 | `curl -N localhost:8080/api/agent/stream?prompt=hi` |
| **M5c Session** | in-memory + sqlite 各跑一遍；Begin/Append/RoundTrip 一致 | `--session` 后 `/history` 能看到历史 |
| **M5d Usage** | mock client 返回固定 usage；累计正确；cost 配置命中 | REPL footer 显示本轮 cost |
| **M5e REPL** | `/usage` `/history` 不崩 | 实际交互 |
| **M6 插件** | in-process mock PluginProvider 返回 echo 工具 | `./myplugin --socket ...` → ./dsh --plugin ./myplugin → tool echo 跑通 |
| **M7 Approval** | mock Approver；deny 时 Result.IsError=true；session scope 不再问 | `--approval terminal` 跑 shell 工具，弹问 |
| **M7 Resume** | `--resume <id>` 后能继续上轮 | 杀 dsh，重启，`/history` 看到上轮 |
| **M8 MCP** | stdio mock MCP server；tools 注册成功 | 拉一个公开 MCP server，tools 出现在 system prompt |
| **M8 Anthropic** | mock Anthropic SSE fixture；delta/text 一致 | 配 `provider: anthropic`，同 greet 任务完成 |
| **M8 Skill** | 假 skill.md 注入 system；body 不丢字 | `/skills` 列已加载；mention "review" 注入 review skill |
| **M8 Sub-agent** | 子 agent mock 返回固定文本；父 runner final 含子结果 | 提示模型 `agent_spawn` 调起子 agent |

---

## §G. 节奏与工时

| 步 | 内容 | 预估 | 前置 |
| --- | --- | --- | --- |
| 0 | v1.2 收尾 | ✅ | — |
| 1 | M5a 流式 + SSE 解析 + delta 事件 + REPL 渲染 | 1.5 天 | — |
| 2 | M5c Session store（MapStore + SQLite）+ Runner 接入 | 1 天 | 1 |
| 3 | M5d Usage tracker + cost 配置 + footer | 0.5 天 | 2 |
| 4 | M5b HTTP API + Bearer + 路由 + 优雅关停 | 1.5 天 | 1, 2 |
| 5 | M5e REPL 命令扩展 + /model + /stream | 0.5 天 | 3 |
| 6 | M5 端到端验收 + README 更新 | 0.5 天 | 5 |
| 7 | M7 Approval + Shell 工具 | 1.5 天 | 6 |
| 8 | M7 Resume / Export / Branch | 1 天 | 7 |
| 9 | M6 插件（gRPC） + 示例 | 1.5 天 | 6 |
| 10 | M8 Anthropic provider | 1 天 | 6 |
| 11 | M8 MCP stdio + Skill loader + Sub-agent | 2 天 | 9 |
| 12 | 跨里程碑回归 + 文档 + v2 release note | 1 天 | 11 |

**合计 ~12.5 天**（一人；不含 v1.x 维护）。

### §G.1 风险与缓解

| 风险 | 缓解 |
| --- | --- |
| 各家 LLM SSE 帧格式微差 | `internal/stream` 抽出 adapter；至少覆盖 OpenAI + DeepSeek + zhipu 三家做 fixture |
| SQLite 并发 | 单进程内 store 用一处 mutex；不让两个 LoopRunner 同时写同 session；DB 层 PRAGMA journal_mode=WAL |
| HTTP server 内存涨 | `max-concurrent-sessions`；每 session ctx cancel 即关闭；定期 GC 旧 store |
| Cost 数据漂移 | cost 表内置查表；不调外部 API |
| v1 测试在 v2 跑挂 | CI 加 `go test ./... -count=1` 双套（v1 走 `stream.enabled=false`，v2 默认全开） |

---

**版本**：v2.0-draft（2026-09-14 起；与 v1.2 并行维护）
**入口**：[DESIGN.md](./DESIGN.md)（v1.2）保持冻结；本文件为 v2 主入口
**下次更新**：M5 完成后写 §A 的"实施回顾"附录；M7 完成后扩 §C

