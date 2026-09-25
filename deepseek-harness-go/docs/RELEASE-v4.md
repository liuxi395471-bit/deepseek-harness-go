# dsh v4.0.0 Release Notes

v4 在 v3 之上引入三层新能力：

1. **会话事件溯源 + 投影缓存**（v4 §A / P1）
2. **统一 Gateway SSE**（v4 §B / P2）
3. **插件/工具库存视图**（v4 §C / P3）

加上 v3 的 [RELEASE-v3.md](./RELEASE-v3.md)（Skill / Sub-agent / Obs /
Compaction / Audit / Sandbox / 多 provider）以及测试用例库。

## §1 变更摘要

### 1.1 §A 事件溯源（P1）

- 新增 `internal/store/event.go`、`projection.go`、`projector.go`。
- `store.Store` 拆出 `EventStore` 接口（`AppendEvent` / `ReadEvents` /
  `GetLastSeq`）和 `ProjectionStore` 接口（`Project`）。
- `MemoryStore` / `SQLiteStore` 都实现新接口；SQLite 增加 `events` 表
  与 `idx_events_session` 索引。
- `MemoryProjectionCache` 提供 thread-safe 投影缓存；AppendEvent 时
  失效缓存。
- 投影：`MessagesProjector`（events → []llm.Message）、`UsageProjector`
  （token 累加）、`PhaseProjector`（最新 phase）。`DefaultProjectorSet`
  返回三合一。
- `AsEventStore(Store) (EventStore, bool)`：v3 store 也能适配。

### 1.2 §B Gateway SSE（P2）

- 新增 `internal/server/gateway.go`、`gateway_handler.go`。
- `POST /api/gateway/stream`：单入口 SSE 端点。请求体
  `{"source": "<name>", "params": {...}}`，响应 `data: {json}\n\n`。
- 内置 6 个 source：

  | source | 行为 |
  |---|---|
  | events.subscribe | 读取 sid 历史事件 |
  | session.send | 启动 RunStream |
  | session.snapshot | 取 messages / usage / phase 投影 |
  | tools.list | 列出全部工具（含风险等级） |
  | plugins.list | 列出 plugin entry |
  | llm.call | 直调 Client.Chat |

- 旧端点 `/api/agent/message`、`/api/agent/stream`、`/api/sessions`
  等保持兼容（仍由 Server.Handler 提供）。
- `Server.SetLLMClient` / `SetInventory` 把 client 与 inventory 注入
  Gateway handlers。

### 1.3 §C 插件库存视图（P3）

- 新增 `internal/plugin/view.go`、`grpc_view.go`。
- `Inventory` 接口（List / Get / Health）。
- `LocalInventory`：从 `tool.Registry` 派生。
- `GRPCInventory`：聚合多个 `*Client`。
- `Combined`：多源聚合；同名 plugin 后注册覆盖前注册。
- 风险分级：shell→high、fs.write→medium、其他→low。
- Gateway `tools.list` / `plugins.list` 接入真实 inventory。

## §2 迁移指南（v3 → v4）

### 2.1 不破坏 v3 API

- `agent.StreamingRunner` / `tool.Registry` / `llm.Client` / `store.Store`
  接口签名不变。
- `Server.New(cfg, runner, store)` 签名不变；`SetLLMClient` /
  `SetInventory` 是新增方法，旧调用代码不受影响。
- 配置 `harness.yml` 不需要变更；新能力按默认开关启用。

### 2.2 新增能力使用

- 想用事件溯源：直接 `store.AsEventStore(st).ReadEvents(...)` 即可。
  无需迁移 Session 模型。
- 想用 Gateway SSE：客户端发送 `POST /api/gateway/stream`，body 为
  `{"source":"<name>","params":{...}}`，解析 SSE 帧。
- 想暴露 Inventory：构造 `plugin.Combined` + 注入 `Server.SetInventory`。

### 2.3 main.go 行为变化

- `-serve` 时自动构造 `Combined = LocalInventory + GRPCInventory`
  并注入到 Server。客户端无感；服务端暴露 `/api/gateway/stream`。

## §3 已知限制

- `MemoryProjectionCache` 是简单 LRU-like 缓存（命中即返），未实现
  真正的 LRU 驱逐；后续 v4.x 可补完。
- `GRPCInventory.Health` 当前只验证 gRPC 客户端已创建；未真实发 RPC。
- `tools.list` 摊平所有 plugin 的 tools 为大列表；规模超大时建议
  在客户端分页（v4 暂不分页）。
- 旧端点 `/api/agent/stream` 与 `/api/gateway/stream session.send`
  行为不完全等价：旧端点发的是 agent.Event 帧，新端点是
  GatewayEvent 帧；客户端迁移时需要做适配。

## §4 验收矩阵

| 阶段 | 测试包 | 用例数 | 全绿 |
|---|---|---|---|
| v3 全部 | internal/... + cmd/... | TC-0001 ~ TC-0049 | ✅ |
| v4 P1 | internal/store | TC-0050 ~ TC-0069 | ✅ |
| v4 P2 | internal/server | TC-0070 ~ TC-0084 | ✅ |
| v4 P3 | internal/plugin + server | TC-0085 ~ TC-0099 | ✅ |

详细映射见 [test-cases.md](./test-cases.md)。

## §5 CLI 端到端冒烟（手动）

1. `go build ./cmd/dsh && ./dsh -config harness.example.yml -prompt "hello"`
   → REPL 风格输出
2. `DSH_SERVER_AUTH_TOKEN=tok ./dsh -config harness.example.yml -serve`
   → 服务端监听
3. `curl -X POST http://127.0.0.1:7777/api/gateway/stream -H "Authorization: Bearer tok" -d '{"source":"tools.list","params":{}}'`
   → SSE 帧，含 tool 列表

## §6 三方依赖（v4 未引入新依赖）

- 复用 v3 引入的：
  - `go.opentelemetry.io/otel` + `sdk/trace` + `sdk/metric` + `otlptracegrpc`
  - `golang.org/x/sys`（Windows ACL）
  - `google.golang.org/grpc`（plugin）
  - `modernc.org/sqlite`（纯 Go SQLite）

v4 没有新增三方依赖。

## §7 致谢

v4 在 v3 的基础上由 0-ROADMAP.md 推动；详细设计见
[DESIGN-v4.md](./DESIGN-v4.md)；实施计划见各 PHASE-N-PLAN.md
（[PHASE-1](./PHASE-1-PLAN.md) 已合并，[PHASE-2](./PHASE-2-PLAN.md)、
[PHASE-3](./PHASE-3-PLAN.md)、[PHASE-4](./PHASE-4-PLAN.md)）。