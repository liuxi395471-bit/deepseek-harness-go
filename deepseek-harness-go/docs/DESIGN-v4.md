# DeepSeek Harness Go 版 — 第四版设计文档（v4 / DESIGN-v4）

> **适用版本**：deepseek-harness-go **v4.0.0**（v3.0.0 收官后启动）
> **总体路线图**：[0-ROADMAP.md](./0-ROADMAP.md)（总览）+ 本文档（详细设计）
> **历史快照**：[DESIGN-v3.md](./DESIGN-v3.md)、[DESIGN-v2.md](./DESIGN-v2.md)
> 阅读顺序：§0 原则 → §1 目标与范围 → §2 包增量 → §3 阶段拆分 → §A 事件溯源 → §B Gateway SSE → §C 插件库存视图 → §D 测试用例库 → §E 三方依赖 → §F 验收矩阵 → §G 节奏与风险。

## §0. 设计原则

| # | 原则 | 含义 |
|---|---|---|
| P1 | **不重写 v3** | v3 公开签名 / 接口 / CLI 全部冻结；v4 只在 v3 之上加层 |
| P2 | **事件流优先于直接消息** | v4 强制走事件流 Append + 投影派生，store 不再直接写 messages |
| P3 | **接口即契约** | 新包先定义 interface，再写实现；mock 同 PR 内交付 |
| P4 | **唯一入口收敛** | HTTP 不再 5 个独立端点，全部走 `/api/gateway/stream` |
| P5 | **可观测优先于功能** | 沿用 v3 OTel + audit；v4 让事件流本身可被 Gateway 流式分发 |
| P6 | **零破坏的 Go 升级** | 仍 stdlib 优先；§E 三方依赖白名单内允许引入 |
| P7 | **测试覆盖率 ≥ 80%** | v3 起入 CI；新代码不允许 < 80% |
| P8 | **用例库即设计真相源** | `test-cases.md` 作为人类可读的功能描述，与代码同步维护 |

## §1. 目标与范围

**一句话**：在 v3 已经具备的"生产可用长会话 agent runtime"之上，补齐 **状态可重放、入口可收敛、插件可观测、行为可审计测试** 四件事。

### §1.1 四大目标

1. **事件溯源**：把 v3 的"messages 直接 Append"重构为"event 流 Append + 投影缓存"；任何会话都能从事件流重放出 messages、usage、phase 等派生视图。
2. **统一 Gateway SSE**：5 个独立 HTTP 端点收敛到 `/api/gateway/stream` 单入口；事件流 / 消息流 / 工具调用流 / 子代理流 / 插件视图 全部走同一个 SSE source 路由。
3. **插件库存视图**：本地工具 + gRPC 插件聚合为统一的 InventoryView（名称、来源、参数 schema、权限、健康状态），通过 Gateway 的 `plugins.list` / `tools.list` 两个 SSE source 暴露。
4. **测试用例库**：建立 `test-cases.md`（≥ 50 条），按 v4 章节组织；每条用例对应一个或多个自动化测试；PR review 强制检查新增/修改条目。

### §1.2 v3 → v4 现状

| 域 | v3 实际 | v4 |
|---|---|---|
| ReAct 核心 | ✅ | — |
| REPL / HTTP / SSE | ✅ | 收敛为统一 Gateway |
| Session + 持久化 | ✅ messages Append | 事件流 + 投影（§A） |
| Usage + cost | ✅ | 投影派生 |
| Approval + Shell | ✅ | — |
| Plugin gRPC | ✅ | + InventoryView（§C） |
| Anthropic Stream | ✅ | — |
| MCP Session | ✅ | — |
| Skill loader | ✅（v3 §A） | 降级 P2（v4 仅保持运行） |
| Sub-agent（sync） | ✅（v3 §B） | + 并发 sub-agent（v5+） |
| Obs（OTel） | ✅（v3 §C） | — |
| Compaction | ✅（v3 §D） | — |
| Audit log | ✅（v3 §E） | — |
| OS sandbox | ✅（v3 §F） | 删除（v5+ 视情况） |
| Ollama / Gemini | ✅（v3 §G） | — |
| **事件流 + 投影** | ❌ | ✅ §A |
| **Gateway SSE** | ❌ | ✅ §B |
| **插件库存视图** | ❌ | ✅ §C |
| **测试用例库** | ❌ | ✅ §D |

## §2. 包增量

```text
internal/
├── store/                         — 已有，v4 加 layer
│   ├── event.go                   — Event / EventType 定义（v4 新增）
│   ├── projection.go              — ProjectionCache 接口 + Memory 实现（v4 新增）
│   ├── projector.go               — Messages/Usage/Phase Projector（v4 新增）
│   └── (existing) store.go / sqlite.go / mem.go / export.go — 兼容层保留
│
├── server/                        — 已有，v4 收敛
│   ├── gateway.go                 — GatewayRequest/Response/Event DTO（v4 新增）
│   └── gateway_handler.go         — source 路由 + emitter（v4 新增）
│
└── plugin/                        — 已有，v4 加 view
    ├── view.go                    — InventoryView 接口 + LocalInventoryView（v4 新增）
    └── grpc_view.go               — 聚合 gRPC plugin hosts（v4 新增）
```

新增文件全部位于 v4 §A / §B / §C；§D 不新增 Go 文件（纯文档）。

## §3. 阶段拆分

| 阶段 | 主题 | 交付 | 依赖 | 工时 |
|---|---|---|---|---|
| P1 | Session 事件溯源 + 投影缓存 | store/event.go + projection.go + projector.go | — | 3 d |
| P2 | 统一 Gateway SSE | server/gateway.go + gateway_handler.go | P1 | 2 d |
| P3 | 插件库存视图 | plugin/view.go + grpc_view.go + Gateway source | P2 | 1.5 d |
| P4 | 用例库 + v4 收尾 | docs/test-cases.md + RELEASE-v4.md + tag | P3 | 2 d |

## §A. Session 事件溯源 + 投影缓存

### §A.1 目标

把 v3 `store.Store.Append(message)` 改为 `store.Store.AppendEvent(event)`，单一存储介质 events 表存放全部状态变化；`messages`、`usage`、`phase` 等运行时字段**仅作投影**——由事件流 + projector 重放派生。

**为什么先做事件流**：
- 重放能力：任意会话可在脱机环境重建 messages
- 多视图：同一事件流可派生 messages（LLM 用）/ usage（账费用）/ phase（前端用）
- 未来兼容性：v5+ 加并发 sub-agent 时，子代理事件自然汇入父流

### §A.2 事件类型

```text
type EventType int

const (
    EventSessionBegin     // 会话开始（写 seq=0）
    EventSystemPrompt     // system 消息建立
    EventUserMessage      // 用户输入
    EventAssistantMessage // 助手完整消息（流式累积完）
    EventToolCall         // 工具调用开始
    EventToolResult       // 工具结果
    EventLLMCall          // llm.chat span 边界
    EventCompaction       // 压缩发生
    EventPhaseChange      // 阶段切换（Init→LLMCall→Tool...）
    EventSessionEnd       // 会话结束（写终止 seq）
)
```

事件 envelope：

```go
type Event struct {
    Sid        string    `json:"sid"`
    Seq        int64     `json:"seq"`         // 单调递增；session 内唯一
    Type       EventType `json:"type"`
    Timestamp  time.Time `json:"ts"`
    Payload    []byte    `json:"payload"`     // JSON 类型化载荷（按 Type 区分）
    Actor      string    `json:"actor,omitempty"` // primary / subagent:X
}
```

### §A.3 Store 接口扩展

```go
// 保留 v3 旧 Append/Load/... 方法，标记 deprecated；v4 新增：
type EventStore interface {
    AppendEvent(ctx context.Context, sid string, ev Event) (int64, error)
    ReadEvents(ctx context.Context, sid string, sinceSeq int64) ([]Event, error)
    GetLastSeq(ctx context.Context, sid string) (int64, error)
}
```

v3 旧方法（`Append(Message)` / `Load(SID)`）内部转发到 `AppendEvent` + `Project`，保留 v3 公开签名冻结。

### §A.4 Schema

```sql
CREATE TABLE events (
    sid        TEXT NOT NULL,
    seq        INTEGER NOT NULL,
    type       INTEGER NOT NULL,
    ts         INTEGER NOT NULL,           -- unix nanos
    payload    BLOB NOT NULL,
    actor      TEXT,
    PRIMARY KEY (sid, seq)
);
CREATE INDEX idx_events_sid_seq ON events(sid, seq);
```

向后兼容：保留 v3 `messages` 表，**只读**。v3 session 启动时 loader 检测到 `messages` 行数 > `events` 行数 → 视为 legacy，按需升级（v4.1 一次性迁移）。

### §A.5 投影器

```go
type Projection interface {
    Name() string                                 // "messages" | "usage" | "phase"
    Apply(ev Event, state ProjectionState) error  // 增量；Projector 维护 state
}

type ProjectionCache interface {
    Get(sid, name string) (ProjectionState, bool) // 命中即返回
    Put(sid, name string, state ProjectionState)   // 写回（持久化按需）
    Invalidate(sid string)                          // 强制下次重放
}
```

内置三种投影：
- **Messages**：把 SessionBegin→SystemPrompt→UserMessage→AssistantMessage... 序列化为 llm.Message 列表（v3 输出的等价物）
- **Usage**：累加 LLMCall 事件的 prompt_tokens + completion_tokens
- **Phase**：记录最后一次 PhaseChange 事件

### §A.6 runner.go 改造点

```go
// v4 路径：
func (r *LoopRunner) run(...) {
    sess, _ := r.Store.Begin(ctx)         // Begin 内部写 EventSessionBegin
    msgs := r.Projects.Messages(sid)      // 投影派生（cache 命中即用）
    for {
        ...
        r.Store.AppendEvent(ctx, sid, Event{
            Type: EventAssistantMessage,
            Payload: marshal(assistant),
        })
        ...
    }
}
```

v3 路径保留：`Append(Message)` 内部转 `Event` 再调 `AppendEvent`——保证旧测试不退化。

### §A.7 验收

| 用例 | 断言 |
|---|---|
| 长会话 100 轮 → events 表 100+ 条 | `len(ReadEvents) >= 100` |
| 投影 Messages 与原 messages 表一致 | session 重新 Build 后 msgs 字节级一致 |
| cache 命中返回一致结果 | 第二次 Project 命中率 = 100% |
| 多 session 并发 events 互不干扰 | sid A 的 seq 与 sid B 的 seq 各自单调 |
| legacy messages 仅读不写 | Append(path) 仍成功但表不再增长 |

## §B. 统一 Gateway SSE

### §B.1 目标

v3 有 5 个独立 HTTP 端点：`/api/agent/message`、`/api/agent/stream`、`/api/health`、`/api/sessions/*`、`/api/plugins/*` 等，每个有独立鉴权与 schema。v4 把所有流式 / 一次性端点**收敛为单一 SSE 入口**：

```
POST /api/gateway/stream
Content-Type: application/json
Body: {"source": "<source_name>", "params": {...}}

→ 200 OK
Content-Type: text/event-stream
data: {...}\n\n
data: {...}\n\n
```

### §B.2 Source 协议

```go
type GatewayRequest struct {
    Source string          `json:"source"`           // "events.subscribe" | "tools.list" | ...
    Params json.RawMessage `json:"params,omitempty"`
}

type GatewayEvent struct {
    Source    string          `json:"source"`
    Type      string          `json:"type"`            // "delta" | "final" | "error"
    Payload   json.RawMessage `json:"payload,omitempty"`
    Timestamp time.Time       `json:"ts"`
}
```

注册式 source router：

```go
type SourceHandler func(ctx context.Context, req GatewayRequest, out chan<- GatewayEvent) error

var sourceRegistry = map[string]SourceHandler{}

// 注册：
sourceRegistry["events.subscribe"] = handleEventsSubscribe
sourceRegistry["tools.list"]       = handleToolsList
sourceRegistry["plugins.list"]     = handlePluginsList
sourceRegistry["session.send"]     = handleSessionSend
sourceRegistry["llm.call"]         = handleLLMCall
```

### §B.3 内置 Source 清单

| Source | 用途 | 输入 params |
|---|---|---|
| `events.subscribe` | 订阅 sid 的事件流（含全部 phase / tool_call / llm_call） | `{sid, since_seq?}` |
| `session.send` | 发送 user prompt（写 user_message 事件） | `{sid, prompt}` |
| `session.snapshot` | 取一次投影快照（messages / usage / phase） | `{sid, projection}` |
| `tools.list` | 本地 + 插件工具清单（snapshot） | `{}` |
| `plugins.list` | gRPC 插件清单（含健康状态） | `{}` |
| `llm.call` | 直接 LLM 调用（无 agent loop，跳过 store） | `ChatRequest` |

### §B.4 路由 + 鉴权

- 旧端点**保留并代理**到 Gateway（v3 client 不退化）：
  - `POST /api/agent/message` → `session.send`
  - `POST /api/agent/stream` → `events.subscribe` 单帧版
  - `GET /api/health` → 固定 health 帧
- 鉴权：Bearer Token（或 `none`），req 透传到 handler；handler 自行决定粒度。
- 超时：默认 300s 可覆盖；ctx cancel 后 emitter 立即关流。
- 流关闭顺序：defer close(out) 必在 err/wait 之后。

### §B.5 序列化与跨语言

- SSE 帧：`data: {json}\n\n`；收尾 `[DONE]` 帧可省略（连接 close 视作结束）。
- 事件内字段命名 snake_case，匹配 v3 store 的 JSONL 习惯。
- 与 v3 OpenAI / Anthropic 协议**不共享**：Gateway 是 dsh 内部协议，外部 SDK 需额外转换。

### §B.6 验收

| 用例 | 断言 |
|---|---|
| 单 source 事件流可消费 | `events.subscribe` 收到 ≥ 1 帧 |
| 多种 source 并存 | 同时订阅两个源不冲突 |
| 旧 `/api/agent/message` 仍工作 | 通过代理路径退化为 Gateway |
| ctx 取消时流立即关 | client disconnect → server goroutine 退出 < 100ms |
| 大负载（10k 帧）无 memory 泄漏 | 全部消费后 goroutine 回收 |

## §C. 插件库存视图

### §C.1 目标

v3 已有 `internal/plugin/` 提供本地 Go plugin 与 gRPC 远程 plugin 的加载机制；v3 register 工具时把 plugin 暴露的工具注入 `tool.Registry`。**但缺一个独立、可查询、可序列化的"插件清单"**。

v4 提供：

1. **Inventory 接口**：聚合本地 Registry + gRPC 远端 plugin hosts 的全部工具/插件元数据。
2. **Gateway source `plugins.list` / `tools.list`**：把清单通过 §B Gateway 暴露。
3. **健康检查**：gRPC 插件在 P3 内置 2 秒超时的 health probe，结果写入清单。

### §C.2 数据模型

```go
type PluginEntry struct {
    Name        string         `json:"name"`
    Kind        string         `json:"kind"`         // "local" | "grpc"
    Source      string         `json:"source"`       // 内置路径 / grpc addr
    Version     string         `json:"version"`
    Healthy     bool           `json:"healthy"`
    LastError   string         `json:"last_error,omitempty"`
    Permissions []string       `json:"permissions"`  // "shell" | "fs" | "net" | ...
    Tools       []ToolSpec     `json:"tools"`        // 该 plugin 暴露的工具
    ProbedAt    time.Time      `json:"probed_at"`
}

type ToolSpec struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    Parameters  json.RawMessage `json:"parameters"`
    Risk        string          `json:"risk"`         // "low" | "medium" | "high"
}
```

### §C.3 Inventory 接口

```go
type Inventory interface {
    List(ctx context.Context) ([]PluginEntry, error)
    Get(name string) (PluginEntry, bool)
    Health(ctx context.Context, name string) (bool, error)
}

type LocalInventory struct {
    Registry *tool.Registry
    Probes   map[string]func(ctx context.Context) error  // 名称 → 健康探测
}

type GRPCInventory struct {
    Hosts []GRPCPluginHost   // 复用 v3 internal/plugin 中的 HostManager
}
```

`Combined` 包装两者并按需去重（远端 plugin 同名时优先 gRPC）。

### §C.4 Gateway Source

```go
// 注册：
sourceRegistry["tools.list"]   = inventoryToolsListSource(inv)
sourceRegistry["plugins.list"] = inventoryPluginsListSource(inv)

// tools.list 返回单帧 final，含全部 ToolSpec；plugins.list 类似 + Healthy。
```

### §C.5 权限字段来源

- 本地工具：v3 `tool.Tool` 不内置权限声明。v4 引入**可选** `Risk() string` 方法（默认 "low"）；已知高危工具（shell / fs / net）在 v4 内置工具侧实现此方法，返回 "high"。
- gRPC 插件：从 plugin manifest 读 `permissions` 字段（proto 已存在 v0.1 metadata）；缺省按 "low" 处理并打 warn 日志。

### §C.6 验收

| 用例 | 断言 |
|---|---|
| 本地工具清单正确 | tools 数组包含 echo/shell 等已知工具 |
| gRPC 插件清单正确 | stub server 注册 2 个 gRPC 插件 → 清单含 2 条 |
| 健康状态实时 | stub server 重启后下次 List 反映 unhealthy |
| 高危工具 Risk=high | shell 在清单中 risk="high" |
| Gateway source 单帧 | tools.list 仅触发 1 次 source 调用 + 1 帧 final |

## §D. 测试用例库

### §D.1 目标

建立 `docs/test-cases.md` 作为人类可读、与代码同步维护的**功能目录**。每条用例：
- 唯一编号 TC-NNNN
- 描述（人类语言）
- 对应自动化测试函数（Go test 函数名）
- 章节归属（§A 事件流 / §B Gateway / §C 插件视图 / §E ~ §G 复用 v3）
- 自动化状态：⏳ 待写 / 🟡 部分 / ✅ 自动 / ⊘ 手动

### §D.2 结构

```text
# DeepSeek Harness Go — 测试用例库（v4 / test-cases）

## §A Session 事件溯源 + 投影缓存
- TC-0001 begin 事件是 seq=0：[evt]Begin → seq=0  ✅ TestStore_AppendEvent_AssignsSeqZero
- TC-0002 ...                              ✅ ...

## §B 统一 Gateway SSE
- TC-0010 source 路由能识别 tools.list  ✅ TestGateway_SourceRouter_ToolsList
...

## §C 插件库存视图
- TC-0020 local + grpc 聚合：...         ✅ TestInventory_Combined_Aggregates

## §E ~ §G 复用 v3 验收
（链接 v3 RELEASE-v3.md 的验收矩阵）

## 自动化覆盖率
- 自动化：NN/50
- 手动：M/50
```

### §D.3 流程

1. P1 启动时：先把已存在的 30+ 验收用例迁入 TC-NNNN 条目
2. P1/P2/P3 每个新增验收：先写测试函数 → 再在 tc 文档登记 → 再合 PR
3. PR review 模板强制项：
   - ☐ 修改 / 新增代码对应的 TC-NNNN 已登记
   - ☐ 该 TC 的 `对应 Go test` 字段非空（或标 ⊘ 手动）
4. v4.0.0 发布时该文档的 自动化 列全部绿

### §D.4 验收

| 项 | 断言 |
|---|---|
| 用例数量 | `wc -l test-cases.md` ≥ 50 |
| 自动化比例 | 自动化项 / 总数 ≥ 80% |
| 章节覆盖 | 至少含 §A / §B / §C / §E（v3 复核） |
| PR template | `.github/PULL_REQUEST_TEMPLATE.md` 含 TC-NNNN checkbox |

## §E. 三方依赖

v4 仍坚持**stdlib 优先**，增量依赖严格白名单。

| 库 | 状态 | 用途 | 版本 |
|---|---|---|---|
| `github.com/anthropics/anthropic-sdk-go` | v3 已选 | Anthropic | 与 v3 一致 |
| `go.opentelemetry.io/otel` + 三个子包 | v3 已选 | obs | v3 锁定 |
| `github.com/charmbracelet/bubbletea` 等 | v2 已选 | TUI | 与 v3 一致 |
| **新增：HTTP/JSON** | **stdlib 已够** | Gateway 协议用 `net/http` + `encoding/json` | — |
| **新增：UUID** | **stdlib 已够** | sid 用 `crypto/rand` + base64url | — |

**绝对不引入的依赖**：
- 任何 OTel 之外的 metrics SDK（v3 已上 OTel metrics，无需再加一个）
- 任何 ORM / SQL builder（v3 直用 `database/sql` + 手写 SQL）
- 任何 SSE 第三方库（v4 gateway 用 `bufio.Scanner` + SSE 行格式手写）
- 任何 UI 框架（Bubble Tea 已够）

## §F. v4 总体验收矩阵

| 维度 | 指标 |
|---|---|
| `go test -count=1 ./...` | 全绿（含 v4 新增 ≥ 30 用例） |
| `go vet ./...` | 0 warning |
| `go test -cover ./...` | ≥ 80% |
| `cmd/dsh --version` | v4.0.0 |
| `git tag` 含 v4.0.0 | 是 |
| `docs/test-cases.md` | ≥ 50 条，§A/§B/§C 各 ≥ 6 条 |
| `docs/RELEASE-v4.md` | 发布；含 v3→v4 升级指南 |
| 端到端：旧 HTTP client 通过 Gateway 代理可用 | curl /api/agent/message 200 |
| 端到端：长会话 100 轮事件流重放 | messages 投影与原 messages 表字节级一致 |
| 端到端：插件清单通过 SSE 可消费 | curl 拿到的 JSON 含 2 个 ToolSpec |

## §G. 节奏与风险

### §G.1 节奏

| 阶段 | 工期 | buffer | 关键产出 |
|---|---|---|---|
| P1 事件溯源 | 3 d | +0.5 d 回归 | store/event.go + tests + tc.md |
| P2 Gateway SSE | 2 d | +0.5 d 回归 | server/gateway*.go + 5 用例 |
| P3 插件视图 | 1.5 d | +0 d | plugin/view.go + 3 用例 |
| P4 用例库 + 收尾 | 2 d | +0.5 d release | tc.md 收尾 + RELEASE-v4 + tag |
| **合计** | **8.5 d + 1.5 d buffer = 10 d** | | |

### §G.2 风险与缓解

| 编号 | 风险 | 缓解 |
|---|---|---|
| R1 | 投影与原 messages 表不一致 | P1 期间双写 + 增量对比脚本；P1 收尾前完成一致性闸口 |
| R2 | 旧 client 依赖旧端点 | Gateway 代理旧端点；v4.0.0 release 注明兼容窗口到 v4.2.0 |
| R3 | gRPC 插件 health probe 阻塞 | 2s 超时 + 并发 probe + 缓存 5s |
| R4 | SSE 多 source 反压 | 每 source 一个 chan(256)，handler goroutine 退出时 close；client 慢时 handler 自动 backpressure |
| R5 | SQLite 单写者限制 | v3 已用 WAL + busy_timeout；v4 仍单实例写入 events |
| R6 | 用例库维护成本 | PR template 强约束；CI 检查 TC-NNNN 引用一致性 |
| R7 | PR template 漏配 | v4 P1 第一个 PR 就 setup `.github/PULL_REQUEST_TEMPLATE.md` |

### §G.3 v5+ 路线（不在 v4 范围）

- 并发 sub-agent（errgroup + 多 goroutine）
- Plan mode（任务拆分 + 用户确认）
- Persona（多身份切换）
- Schedule（cron 调度）
- OS sandbox 重新评估（参考 Java 不上 sandbox 决策）
- Workflow source（P2 不做）
- SSE 重连协议（P2 不做）

---

**版本**：DESIGN-v4 v0.1（2026-09-26 起）
**配套文档**：[0-ROADMAP.md](./0-ROADMAP.md)（总览）+ 各 `PHASE-N-PLAN.md`（实施步骤，阶段开工时创建）
**配套 release**：[RELEASE-v4.md](./RELEASE-v4.md)（P4 完成时创建）
