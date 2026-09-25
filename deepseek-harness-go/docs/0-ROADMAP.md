# DeepSeek Harness Go — 🗺️ 整体演进路线图（0-ROADMAP）

> **这是 dsh 的"零号文档"**：整体多阶段演进路线图。
> **不在此文档做详细设计**：每阶段启动时，新建独立的 `PHASE-N-DESIGN.md` 做详细规格、伪码、验收矩阵。
> **不在此文档总结其他版本**：每阶段只点"参考 `../deepseek-harness/...` 或 `../deepseek-harness-java/...` 的具体文件/章节"。
>
> 适用对象：deepseek-harness-go（v2.0.2 完成于 2026-09-16；本规划启动于 2026-09-20）。

## 📑 文档命名约定

| 文档类型 | 命名模式 | 示例 | 职责 |
|---|---|---|---|
| **总纲** | `0-ROADMAP.md` | `0-ROADMAP.md`（本文档） | 整体多阶段路线图（唯一） |
| **新人学习** | `LEARNING-ROADMAP.md` | `LEARNING-ROADMAP.md` | 给新人的 10 个台阶路线（独立于阶段规划） |
| **阶段实施计划** | `PHASE-N-PLAN.md` | `PHASE-1-PLAN.md` | 单阶段的实施步骤（每个阶段一份） |
| **阶段详细设计** | `PHASE-N-DESIGN.md` | `PHASE-1-DESIGN.md` | 单阶段的详细规格、伪码、验收（每个阶段一份，可选） |
| **历史快照** | `PLAN-vN.md` / `DESIGN-vN.md` | `PLAN-v3.md` `DESIGN-v3.md` | 旧版本计划/设计（冻结） |
| **发布说明** | `RELEASE-vN.md` | `RELEASE-v2.md` | 版本发布说明 |

**规则**：
- `0-ROADMAP.md` 是**唯一**的整体规划文档；其他 `PLAN-vN.md` / `PHASE-N-PLAN.md` 都是**单阶段**或**历史**规划
- 阶段开工时先创建 `PHASE-N-PLAN.md`，必要时再写 `PHASE-N-DESIGN.md`
- `PLAN-vN.md` 是 vN 整体规划（旧语义），保留为历史快照
- 新阶段**不允许**再创建 `PLAN-vN.md`，统一使用 `PHASE-N-*` 命名
- 新人入门参考 [LEARNING-ROADMAP.md](./LEARNING-ROADMAP.md)（10 个台阶，与阶段规划正交）

---

## ✅ 完成状态总览（截至 2026-09-26 01:40）

### 阶段状态

| 阶段 | 主题 | 工时 | 状态 | 阶段实施计划 | 阶段详细设计 | 代码 | 测试 | 备注 |
|---|---|---|---|---|---|---|---|---|
| **P0** | 现状冻结 | 0.5d | ✅ 完成（v2.0.2 已收官） | — | — | ✅ | ✅ | 见 [RELEASE-v2.md](./RELEASE-v2.md) |
| **P1** | 事件溯源 | 3d | ☐ 待启动 | ☐ | ☐ | ☐ | ☐ | 下一步开工 |
| **P2** | Gateway SSE | 2d | ☐ 待启动 | ☐ | ☐ | ☐ | ☐ | 依赖 P1 |
| **P3** | 插件库存视图 | 1.5d | ☐ 待启动 | ☐ | ☐ | ☐ | ☐ | 依赖 P2 |
| **P4** | 用例库 + 收尾 | 2d | ☐ 待启动 | ☐ | ☐ | ☐ | ☐ | 依赖 P3 |
| **v3 并行** | Skill+Subagent+Obs+Audit+Compaction+Ollama+Gemini | — | ✅ 完成 | — | [RELEASE-v3.md](./RELEASE-v3.md) | ✅ | ✅ | 与 P1-P4 并行；见 RELEASE-v3 验收矩阵 |

### P0 已完成项明细（v2.0.2 历史）

| # | 已完成 | 来源 | 完成日期 |
|---|---|---|---|
| P0.1 | Tag `v2.0.2` 发布 | [RELEASE-v2.md](./RELEASE-v2.md) | 2026-09-16 |
| P0.2 | `docs/RELEASE-v2.md` 写入 | 本仓库 | 2026-09-16 |
| P0.3 | M6 plugin gRPC（T13） | v2.0.0 | 2026-09-16 |
| P0.4 | M8 Anthropic ChatStream（T14） | v2.0.0 | 2026-09-16 |
| P0.5 | MCP Session（T15-1） | v2.0.1 | 2026-09-16 |
| P0.6 | `phase/v4-evolution` branch 创建 | **本规划启动时立即创建** | 2026-09-20 |

### 文档清单（待创建已标 ☐）

| 文档 | 状态 |
|---|---|
| `docs/0-ROADMAP.md` | ✅ 当前（已存在） |
| `docs/PHASE-1-PLAN.md` | ☐ P1 开工时创建 |
| `docs/PHASE-1-DESIGN.md` | ☐ P1 开工时按需创建 |
| `docs/PHASE-2-PLAN.md` | ☐ P2 开工时创建 |
| `docs/PHASE-3-PLAN.md` | ☐ P3 开工时创建 |
| `docs/PHASE-4-PLAN.md` | ☐ P4 开工时创建 |
| `docs/PHASE-5-PLAN.md` | ☐ P5 开工时创建 |
| `docs/test-cases.md` | ☐ P5 期间创建 |
| `docs/RELEASE-v4.md` | ☐ P5 完成时创建 |

---

## 阅读顺序

- §1 总体目标与时间线
- §2 阶段依赖关系
- §3 阶段详情（每阶段：目标 / 不做 / 参考 / 子 TODO 索引 / 工时 / 验收 / 风险 / **完成判定**）
- §4 阶段间交付物与发布节奏
- §5 跨阶段不变量
- §6 风险总表

---

## §1 总体目标与时间线

### §1.1 一句话

把 dsh 从"CLI + HTTP + SQLite + 流式 + 4 Approver + gRPC plugin + Anthropic/MCP"演进为"事件溯源 + 统一 Gateway SSE + 完整可观测 + 审计 + 多渠道"的 **生产可用长会话 agent runtime**。

### §1.2 当前 vs 目标（高层）

| 维度 | 当前（v2.0.2） | 目标（本规划完成时） |
|---|---|---|
| 会话存储 | messages 直接 Append 到 SQLite | 事件流 + 投影缓存 + 写租约 |
| HTTP 流式 | 5 个独立端点 | 统一 Gateway SSE |
| 可观测性 | 仅 `--debug` 打印事件 | OTel opt-in + 全 hook 点 |
| 审计 | 无 | JSONL redact 日志 |
| LLM 渠道 | OpenAI / DeepSeek / Anthropic | + Ollama + Gemini（4 + provider 路由） |
| 上下文管理 | 无压缩 | truncate + llm-summary |
| 测试体系 | 单测 + 集成 | + `test-cases.md` 用例库 |

### §1.3 阶段划分（共 5 个阶段，约 8.5 天）

| 阶段 | 主题 | 工时 | 状态 | 关键交付物 |
|---|---|---|---|---|
| **P0 现状冻结** | 把 v2.0.2 锁为基线 | 0.5 天 | ✅ 完成 | v2.0.2 tag + branch |
| **v3 并行** | Skill+Sub-agent+Obs+Audit+Compaction+Ollama+Gemini | — | ✅ 完成 | 7 大模块（见 [RELEASE-v3.md](./RELEASE-v3.md)） |
| **P1 事件溯源** | Session 事件流 + 投影缓存 | 3 天 | ☐ 待启动 | `internal/store/event.go` + 投影 + 测试 |
| **P2 统一 Gateway SSE** | 5 端点收敛 | 2 天 | ☐ 待启动 | `internal/server/gateway.go` + 路由 |
| **P3 插件库存视图** | Tool/Plugin 可视化 | 1.5 天 | ☐ 待启动 | `internal/plugin/view.go` + HTTP 端点 |
| **P4 用例库 + 收尾** | test-cases.md + v4 tag | 2 天 | ☐ 待启动 | `test-cases.md` + `RELEASE-v4.md` + git tag |
| **合计** | | **~8.5 天**（一人） | | |

### §1.4 时间线图

```
Week 1     Week 2     Week 3
 │          │          │
 ▼          ▼          ▼
 P0 ✅ P1──────────────┤ P2 ├────┤ P3 ├────┤ P4 │
 baseline   events+sse   gateway  plugin   testcases
                                     +v4 tag
```

每阶段内部不重叠；阶段之间允许 1 天 buffer 用于回归 + release note。

---

## §2 阶段依赖关系

```text
P0 (baseline) ✅ ── 必先后置
   │
  v3 并行 ✅（Skill+Subagent+Obs+Audit+Compaction+Ollama+Gemini）
   │
   ▼
P1 (事件溯源) ☐
  │
  ▼
P2 (Gateway SSE) ☐ ──┐
  │                │
  ▼                │
P3 (插件库存视图) ☐──┘
  │
  ▼
P4 (用例库 + 收尾) ☐
```

**关键约束**：

- **P1 → P2**：Gateway SSE 的事件类型需要 P1 的 event 流（避免重复定义）
- **P2 → P3**：插件库存视图需要 Gateway 作为 HTTP 基础
- **P3 → P4**：用例库涵盖所有已实现模块

---

## §3 阶段详情

### P0 — 现状冻结（baseline lock）

**状态**：✅ 已完成（v2.0.2 历史快照已收官）

**已完成项**：
- ✅ v2.0.2 tag 发布（2026-09-16）
- ✅ `docs/RELEASE-v2.md` 写入（约 9 KB，含 130+ 测试用例）
- ✅ M6 plugin gRPC（T13）— v2.0.0 落地
- ✅ M8 Anthropic ChatStream（T14）— v2.0.0 落地
- ✅ MCP Session（T15-1）— v2.0.1 落地
- ✅ `phase/v4-evolution` branch 创建（本规划启动时立即执行）

**目标**：把 v2.0.2 锁定为 v3/v4 演进不可变的起点。

**做什么**：

1. 确认 `docs/RELEASE-v2.md` 已发布
2. 确认 git tag `v2.0.2` 已存在
3. ✅ **本规划启动时立即创建** branch `phase/v4-evolution` 作为所有 P1+ 工作的载体

**不做**：

- 不修改 v2.0.2 任何代码
- 不改 README / 配置示例
- 不动 RELEASE-v2.md

**参考**：

- `../deepseek-harness/CHANGELOG.md` — 看官方 Node 版如何 freeze 每个 minor version
- `../deepseek-harness-java/docs/md/release-v0.1.7-development-notes.md` §11 升级注意（用于借鉴基线锁定策略）

**子 TODO**：无（本阶段只做冻结动作）

**工时**：0.5 天

**验收（完成判定）**：

- ✅ git tag `v2.0.2` 存在
- ☐ `phase/v4-evolution` branch 创建（**本规划启动时立即创建**）
- ☐ `git diff v2.0.2 phase/v4-evolution` 0 行差异（baseline lock）

**风险**：无

---

### P1 — Session 事件溯源 + 投影缓存 ☐

**状态**：☐ 待启动

**目标**：把 v2 的"messages 直接 Append 到 SQLite"重构为"event 流 Append + 派生投影缓存"。

**启动时**：新建 `docs/PHASE-1-PLAN.md`，含实施步骤；按需新建 `docs/PHASE-1-DESIGN.md` 做详细设计。

**完成判定（本阶段全部 ✅ 才能进入 P2）**：

- ☐ `docs/PHASE-1-PLAN.md` 创建
- ☐ `docs/PHASE-1-DESIGN.md` 创建（按需）
- ☐ `internal/store/event.go` 创建
- ☐ `internal/store/projection.go` 创建
- ☐ `internal/store/projector.go` 创建
- ☐ `internal/agent/runner.go` 改造完成
- ☐ 6 个验收用例全部通过
- ☐ `go test -count=1 ./...` 全绿（v2 的 130+ 用例不退化）
- ☐ `go test -cover ./internal/store/...` ≥ 80%

**不做（本阶段）**：

- 写租约（JVM 内存限制；Go SQLite WAL+busy_timeout 已够；多实例部署留 v5+）
- 投影缓存 TTL/LRU（先做内存无限增长，v5+ 加淘汰）
- 事件压缩 / archive

**参考（仅引用，详细设计在 PHASE-1-DESIGN）**：

- `../deepseek-harness-java/deepseek-harness-java-domain/src/main/java/cn/xiaofuge/deepseek/harness/domain/session/event/model/entity/SessionHeader.java` — 事件头格式
- `../deepseek-harness-java/deepseek-harness-java-domain/src/main/java/cn/xiaofuge/deepseek/harness/domain/session/event/service/SessionWriteLeaseService.java` — 写租约（Go 版不实现，借鉴思路）
- `../deepseek-harness-java/deepseek-harness-java-domain/src/main/java/cn/xiaofuge/deepseek/harness/domain/session/event/service/InMemorySessionProjectionCache.java` — 投影缓存接口签名
- `../deepseek-harness-java/deepseek-harness-java-domain/src/main/java/cn/xiaofuge/deepseek/harness/domain/session/event/service/SessionRebuilderService.java` — 重建逻辑（先查 lastSeq）
- `../deepseek-harness-java/deepseek-harness-java-infrastructure/src/main/java/cn/xiaofuge/deepseek/harness/infrastructure/adapter/eventstore/JsonlSessionEventStore.java` — JSONL 存储（Go 用 SQLite 替代，借鉴事件序列化/反序列化思路）
- `../deepseek-harness/packages/session/session-event-log/` — Node 版的 event log 设计（实现细节）
- `../deepseek-harness/packages/api/session-controller/src/owned-value.ts` — Node 版的 Session ownership
- `../deepseek-harness/docs/agent-lifecycle.md` — 事件生命周期参考

**子 TODO 索引**（在 PHASE-1-PLAN 内展开）：

- T1.1 `internal/store/event.go`：Event/EventType 定义
- T1.2 `internal/store/store.go`：Store 接口扩展（AppendEvent/ReadEvents/GetLastSeq/Project）
- T1.3 `internal/store/sqlite.go`：events 表 schema + 索引 + 兼容 messages 表
- T1.4 `internal/store/projection.go`：ProjectionCache 接口 + MemoryProjectionCache
- T1.5 `internal/store/projector.go`：MessagesProjector + UsageProjector + PhaseProjector
- T1.6 `internal/agent/runner.go`：改写为 event 流（AppendEvent + Project）
- T1.7 6 个验收用例

**工时**：3 天

**验收**：

- `PHASE-1-PLAN.md` 完成
- `go test -count=1 ./...` 全绿（v2 的 130+ 用例不退化）
- 新增 ≥ 6 用例通过
- `go test -cover ./internal/store/...` ≥ 80%
- 端到端：长会话 100 轮后 `events` 表有 100 条；投影器派生 messages 与原 messages 表一致

**风险**：

- 旧 session 数据格式不兼容 → 回退 messages 表机制
- Projection cache 无容量上限 → 本阶段接受（v5+ 加 LRU）
- 写租约缺失 → 多实例部署未支持（文档注明）

---

### P2 — 统一 Gateway SSE ☐

**状态**：☐ 待启动（依赖 P1）

**目标**：把 v2 的 5 个独立端点收敛到 `/api/gateway/stream` 一个 SSE 入口。

**启动时**：新建 `docs/PHASE-2-PLAN.md`，含实施步骤；按需新建 `docs/PHASE-2-DESIGN.md` 做详细设计。

**完成判定（本阶段全部 ✅ 才能进入 P3）**：

- ☐ `docs/PHASE-2-PLAN.md` 创建
- ☐ `internal/server/gateway.go` 创建
- ☐ `internal/server/gateway_handler.go` 创建
- ☐ 路由改造（旧端点保留 + 转发）
- ☐ 5 个验收用例全部通过
- ☐ README 更新（Gateway SSE 调用示例）

**不做（本阶段）**：

- Workflow source（v5+ 引入）
- 多实例广播 / 断点续传
- SSE 重连协议

**参考（仅引用，详细设计在 PHASE-2-DESIGN）**：

- `../deepseek-harness-java/deepseek-harness-java-api/src/main/java/cn/xiaofuge/deepseek/harness/api/gateway/IGatewayStreamApi.java` — Gateway 接口
- `../deepseek-harness-java/deepseek-harness-java-api/src/main/java/cn/xiaofuge/deepseek/harness/api/gateway/dto/GatewayStreamEventDTO.java` — 事件信封 schema
- `../deepseek-harness-java/deepseek-harness-java-trigger/src/main/java/cn/xiaofuge/deepseek/harness/trigger/service/stream/GatewayStreamApi.java` — 实现（线程池、超时、完成顺序）
- `../deepseek-harness-java/deepseek-harness-java-trigger/src/main/java/cn/xiaofuge/deepseek/harness/trigger/http/GatewayStreamController.java` — HTTP 端点
- `../deepseek-harness/docs/api-gateway.md` — Node 版 API gateway 文档

**子 TODO 索引**（在 PHASE-2-PLAN 内展开）：

- T2.1 `internal/server/gateway.go`：GatewayRequest/Response/Event DTO
- T2.2 `internal/server/gateway_handler.go`：source 路由 + emitter
- T2.3 `internal/server/server.go`：路由注册（旧端点保留 + 转发）
- T2.4 5 个验收用例
- T2.5 README 更新

**工时**：2 天

**验收**：

- `PHASE-2-PLAN.md` 完成
- `go test ./internal/server/...` 全绿（旧 6 用例不退化 + 5 新用例）
- 端到端：`curl -N -H "Authorization: Bearer $T" http://127.0.0.1:8080/api/gateway/stream -d '{...}'` 看到 SSE 流
- 旧端点 `/api/agent/message` 与 `/api/agent/stream` 仍能工作

**风险**：

- 旧 v2 client 依赖旧端点 → 旧端点保留 + 转发
- SSE 超时与 HTTP client 不匹配 → 默认 300s

---

### P3 — 插件库存视图 ☐

**状态**：☐ 待启动（依赖 P2）

**目标**：通过 Gateway SSE 暴露 `/api/plugins` 与 `/api/tools`，返回注册表与 gRPC 插件的完整清单（名称 / 来源 / 来源主机 / 参数 schema / 权限 / 健康状态），供运维与 LLM 自发现用。

**启动时**：新建 `docs/PHASE-3-PLAN.md`，含实施步骤。

**完成判定（本阶段全部 ✅ 才能进入 P4）**：

- ☐ `docs/PHASE-3-PLAN.md` 创建
- ☐ `internal/plugin/view.go` 实现（聚合本地 reg + gRPC plugin hosts）
- ☐ Gateway SSE source `plugins.list` 与 `tools.list` 注册
- ☐ 3 个验收用例全部通过
- ☐ `go test -cover ./internal/plugin/...` ≥ 80%

**不做（本阶段）**：

- 插件热加载（v5+）
- 远程插件市场（v5+）
- 插件配置回写（v5+）

**参考（仅引用，详细设计在 PHASE-3-DESIGN）**：

- `../deepseek-harness-java/deepseek-harness-java-domain/src/main/java/cn/xiaofuge/deepseek/harness/domain/plugin/view/` — Java 版 plugin view 接口签名
- `../deepseek-harness/packages/plugin/plugin-registry/` — Node 版 plugin registry

**子 TODO 索引**（在 PHASE-3-PLAN 内展开）：

- T3.1 `internal/plugin/view.go`：PluginView 接口 + LocalPluginView 实现
- T3.2 `internal/plugin/grpc_view.go`：聚合 gRPC plugin hosts 的元数据
- T3.3 `internal/server/gateway.go`：注册 `plugins.list` 与 `tools.list` 两个 SSE source
- T3.4 3 个验收用例

**工时**：1.5 天

**验收**：

- `PHASE-3-PLAN.md` 完成
- `go test ./internal/plugin/...` 全绿
- 端到端：`curl -N http://127.0.0.1:8080/api/gateway/stream -d '{"source":"plugins.list"}'` 看到插件清单

**风险**：

- gRPC plugin 元数据获取阻塞 → 设置 2s 超时 + 缓存
- 插件来源不可信 → 仅返回 metadata，不暴露 plugin 内部代码路径

---

### P4 — 测试用例库 + 收尾 ☐

**状态**：☐ 待启动（依赖 P3）

**目标**：建立 `docs/test-cases.md`（≥ 50 条覆盖 v3 + v4 全部能力），发 `RELEASE-v4.md`，打 `v4.0.0` tag。

**启动时**：新建 `docs/PHASE-4-PLAN.md`，含实施步骤。

**完成判定（v4 发布）**：

- ☐ `docs/PHASE-4-PLAN.md` 创建
- ☐ `docs/test-cases.md` 创建（≥ 50 条，覆盖 Skill / Sub-agent / Obs / Audit / Compaction / Ollama / Gemini / 事件流 / Gateway SSE / 插件视图）
- ☐ `docs/RELEASE-v4.md` 创建（含实际交付清单 + 已知限制 + 升级指南）
- ☐ README 更新（v4 功能段落 + 4 provider 配置 + Gateway SSE 用法 + 事件流概念）
- ☐ cmd/dsh version → v4.0.0
- ☐ git tag `v4.0.0` 存在
- ☐ `go test -count=1 ./...` 全绿（合计 ≥ 220 用例）
- ☐ `go vet ./...` 0 warning
- ☐ `go test -cover ./...` ≥ 80%

**不做（本阶段）**：

- 并发 sub-agent（v5+）
- Plan mode / Persona / Schedule（v5+）
- OS sandbox（v5+：Java 不上 sandbox，Go 版同上）

**参考（仅引用，详细设计在 PHASE-4-DESIGN）**：

- `../deepseek-harness-java/docs/md/test-cases.md` — Java 版用例库（结构参考）
- `../deepseek-harness-java/docs/md/release-v0.1.7-development-notes.md` §8.1 — Java 版用例库章节
- `../deepseek-harness/docs/development.md` — Node 版开发指南

**子 TODO 索引**（在 PHASE-4-PLAN 内展开）：

- T4.1 收集 v3 已通过的 30+ 验收用例 + v4 P1/P2/P3 新增用例
- T4.2 `docs/test-cases.md`：分类组织（按 v4 章节 / 用例编号 / 自动化状态）
- T4.3 `docs/RELEASE-v4.md`：实际交付清单 + 已知限制 + v2/v3 → v4 升级指南
- T4.4 README 更新
- T4.5 cmd/dsh version → v4.0.0
- T4.6 git tag v4.0.0
- T4.7 全量回归 + 覆盖率检查

**工时**：2 天

**验收**：

- `PHASE-4-PLAN.md` 完成
- `go test -count=1 ./...` 全绿
- `go vet ./...` 0 warning
- `go test -cover ./...` ≥ 80%
- `docs/test-cases.md` 存在且 ≥ 50 条
- `docs/RELEASE-v4.md` 已发布
- `git tag v4.0.0` 存在

**风险**：

- 用例库维护成本 → 单一真相源；PR review 强制
- 升级指南不准确 → 给出 v3 → v4 的最小升级操作清单

---

## §4 阶段间交付物与发布节奏

### §4.1 每阶段交付物

| 阶段 | 实施计划 | 代码 | 测试 | 文档 |
|---|---|---|---|---|
| P0 ✅ | — | — | — | `phase/v4-evolution` branch + v2.0.2 tag |
| v3 并行 ✅ | — | 7 大模块 | 验收矩阵 | `RELEASE-v3.md` |
| P1 ☐ | `PHASE-1-PLAN.md` | `internal/store/{event,projection,projector}.go` | 6 用例 | — |
| P2 ☐ | `PHASE-2-PLAN.md` | `internal/server/gateway*.go` | 5 用例 | README |
| P3 ☐ | `PHASE-3-PLAN.md` | `internal/plugin/view.go` | 3 用例 | — |
| P4 ☐ | `PHASE-4-PLAN.md` | — | — | `test-cases.md` + `RELEASE-v4.md` |

### §4.2 Release 节奏

- P0 ✅ 不发版（baseline lock，已是 v2.0.2）
- v3 ✅ → 已发 `v3.0.0`（含全部 v3 功能）
- P1 完成 → 可发 `v3.1-preview`（可选）
- P2 完成 → 可发 `v3.2-preview`（可选）
- P3 完成 → 可发 `v3.3-preview`（可选）
- **P4 完成 → 发 `v4.0.0`（正式版）**

---

## §5 跨阶段不变量

每阶段必须遵守的规则：

| # | 规则 | 含义 |
|---|---|---|
| **I1** | 不删 v2 公开 API | `cmd/dsh` CLI、`internal/server` HTTP 路由、`tool.Tool` 接口签名全部冻结 |
| **I2** | 不改 v2 默认行为 | 无新配置段 → 行为与 v2.0.2 一致 |
| **I3** | 不破坏现有测试 | 每步前必须先跑 v2 回归（130+ 用例不退化） |
| **I4** | 新增功能 opt-in | obs / audit / compaction / sandbox 默认 noop |
| **I5** | 三方依赖最小化 | 仅 OTel 三个包（v4 增量）；其余 stdlib |
| **I6** | 测试覆盖率 ≥ 80% | `go test -cover` 入 CI；新代码不允许 < 80% |
| **I7** | PR 模板强制 PHASE-N-PLAN 引用 | 每 PR 必须引用所属阶段的实施计划章节 |
| **I8** | 文档命名规范 | 见 §📑 文档命名约定 |

---

## §6 风险总表

| # | 风险 | 缓解 | 触发阶段 |
|---|---|---|---|
| R1 | 事件流回放慢 | 投影缓存 + 增量写 | P1 |
| R2 | 旧 v2 client 依赖旧端点 | 旧端点保留 + 转发 | P2 |
| R3 | Gateway SSE 超时与 HTTP client 不匹配 | 默认 300s | P2 |
| R4 | 插件库存视图性能 | 缓存 TTL | P3 |
| R5 | 用例库维护成本 | 单一真相源；PR review 强制 | P4 |
| R6 | SQLite 单写者限制 | v2 已用 WAL + busy_timeout | P1 |

---

## §7 启动指令

如确认按本规划执行：

1. **v3 收尾**（今天）：确认 `RELEASE-v3.md` 完整 → 打 tag `v3.0.0`
2. **P1 启动**（下次开工）：先写 `docs/PHASE-1-PLAN.md` → 按需写 `PHASE-1-DESIGN.md` → 开始写代码
3. 每阶段流程：**PHASE-N-PLAN.md** → 代码 → 测试 → commit → **更新本 ROADMAP 状态** → 下阶段

每阶段开工前请先告知，我会：
- 在新对话中创建 `PHASE-N-PLAN.md`
- 启动该阶段的实施
- 完成验收后再回到本 ROADMAP 更新状态

---

**版本**：0-ROADMAP v0.3（2026-09-26 更新：v3 并行完成，5 阶段缩减为 4 阶段）
**配套文档**：每个阶段的 `PHASE-N-PLAN.md`（启动时创建）；`DESIGN-v4.md`（v4 详细设计）
**配套 release**：`docs/RELEASE-v4.md`（P4 完成时创建）
