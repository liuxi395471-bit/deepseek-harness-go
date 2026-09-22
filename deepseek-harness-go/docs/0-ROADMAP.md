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

## ✅ 完成状态总览（截至 2026-09-20 02:38）

### 阶段状态

| 阶段 | 主题 | 工时 | 状态 | 阶段实施计划 | 阶段详细设计 | 代码 | 测试 | 备注 |
|---|---|---|---|---|---|---|---|---|
| **P0** | 现状冻结 | 0.5d | ✅ 完成（v2.0.2 已收官） | — | — | ✅ | ✅ | 见 [RELEASE-v2.md](./RELEASE-v2.md) |
| **P1** | 事件溯源 | 3d | ☐ 待启动 | ☐ | ☐ | ☐ | ☐ | 下一步开工 |
| **P2** | Gateway SSE | 2d | ☐ 待启动 | ☐ | ☐ | ☐ | ☐ | 依赖 P1 |
| **P3** | Obs + Audit | 2.5d | ☐ 待启动 | ☐ | ☐ | ☐ | ☐ | 依赖 P2 |
| **P4** | Compaction + 多渠道 | 4d | ☐ 待启动 | ☐ | ☐ | ☐ | ☐ | 依赖 P3 |
| **P5** | Sub-agent + 用例库 + 收尾 | 3d | ☐ 待启动 | ☐ | ☐ | ☐ | ☐ | 依赖 P4 |

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

### §1.3 阶段划分（共 6 个阶段，约 6 周）

| 阶段 | 主题 | 工时 | 状态 | 关键交付物 |
|---|---|---|---|---|
| **P0 现状冻结** | 把 v2.0.2 锁为基线 | 0.5 天 | ✅ 完成 | v2.0.2 tag + branch |
| **P1 事件溯源** | Session 事件流 + 投影缓存 | 3 天 | ☐ 待启动 | `internal/store/event.go` + 投影 + 测试 |
| **P2 统一 Gateway SSE** | 5 端点收敛 | 2 天 | ☐ 待启动 | `internal/server/gateway.go` + 路由 |
| **P3 可观测性 + 审计** | OTel + Audit log | 2.5 天 | ☐ 待启动 | `internal/obs/otel.go` + `internal/audit/` |
| **P4 上下文压缩 + 多渠道** | Compaction + Ollama + Gemini | 4 天 | ☐ 待启动 | `internal/compaction/` + 多渠道 |
| **P5 子代理 + 用例库 + 收尾** | Sub-agent + 用例库 + v4 tag | 3 天 | ☐ 待启动 | `internal/subagent/` + `test-cases.md` + RELEASE-v4 |
| **合计** | | **~15 天**（一人） | | |

### §1.4 时间线图

```
Week 1     Week 2     Week 3     Week 4     Week 5     Week 6
 │          │          │          │          │          │
 ▼          ▼          ▼          ▼          ▼          ▼
 P0 ✅ P1─────┤ P2 ├──────┤ P3 ├──────┤ P4 ├──────┤ P5 │
 baseline   events+sse obs+audit  compact    sub-agent
                       +otel      +ollama    +testcases
                                  +gemini    +v4 tag
```

每阶段内部不重叠；阶段之间允许 1 天 buffer 用于回归 + release note。

---

## §2 阶段依赖关系

```text
P0 (baseline) ✅ ── 必先后置
  │
  ▼
P1 (事件溯源) ☐ ──────────┐
  │                    │
  ▼                    │
P2 (Gateway SSE) ☐ ──┐   │
  │                │   │
  ▼                ▼   │
P3 (Obs+Audit) ☐ ────┐   │
  │                │   │
  ▼                ▼   ▼
P4 (Compaction + 多渠道) ☐  ←── 依赖 P3（hook 点）
  │
  ▼
P5 (Sub-agent + 用例库 + 收尾) ☐ ←── 依赖 P1（事件流用于子代理事件）
```

**关键约束**：

- **P1 → P2**：Gateway SSE 的事件类型需要 P1 的 event 流（避免重复定义）
- **P3 → P4**：Compaction 触发需要 hook 到 OTel span
- **P1 → P5**：Sub-agent 通过事件流汇总子代理输出
- **P0 → 其余**：必须先把 v2.0.2 tag 锁为不可变基线（✅ 已完成）

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

### P3 — 可观测性 + 审计日志 ☐

**状态**：☐ 待启动（依赖 P2）

**目标**：引入 OTel opt-in 实现 + JSONL audit log。

**启动时**：新建 `docs/PHASE-3-PLAN.md`，含实施步骤；按需新建 `docs/PHASE-3-DESIGN.md` 做详细设计。

**完成判定（本阶段全部 ✅ 才能进入 P4）**：

- ☐ `docs/PHASE-3-PLAN.md` 创建
- ☐ `internal/obs/otel.go` 创建（OTel 真实现）
- ☐ `internal/audit/log.go` 创建
- ☐ 3 个 hook 点接入（runner / client / tool）
- ☐ 7 个验收用例全部通过（4 obs + 3 audit）
- ☐ `go.mod` 加 otel 依赖

**不做（本阶段）**：

- OTel metrics（先 trace）
- Audit log 远程发送（先本地文件）
- 集成 Prometheus / Grafana

**参考（仅引用，详细设计在 PHASE-3-DESIGN）**：

- `../deepseek-harness/packages/obs/` — Node 版 OTel 实现
- `../deepseek-harness/packages/obs/noop.ts` + `../deepseek-harness/packages/obs/otel.ts` — 实现参考
- `../deepseek-harness/docs/development.md` — Node 版 obs 配置
- `../deepseek-harness-java/docs/md/runtime-diagnostics/` — Java 版启动诊断（不是 OTel，但借鉴"分级日志"思路）

**子 TODO 索引**（在 PHASE-3-PLAN 内展开）：

- T3.1 `internal/obs/obs.go`：接口（保持 v3 草案）
- T3.2 `internal/obs/noop.go`：默认实现
- T3.3 `internal/obs/otel.go`：OTel 实现（OTLP gRPC exporter）
- T3.4 `internal/obs/hook.go`：hook 点封装
- T3.5 `internal/agent/runner.go`：runner.Run 包 span
- T3.6 `internal/llm/client.go`：client.Chat/ChatStream 包 span
- T3.7 `internal/tool/tool.go`：tool.Execute 包 span
- T3.8 `internal/audit/event.go`：Event schema + redact
- T3.9 `internal/audit/log.go`：FileLogger
- T3.10 `internal/audit/redact.go`：SHA-256 脱敏
- T3.11 CLI flag `--audit` 与 `--audit-full`
- T3.12 7 个验收用例

**工时**：2.5 天

**验收**：

- `PHASE-3-PLAN.md` 完成
- `go test ./internal/obs/... ./internal/audit/...` 全绿
- 端到端：`obs.provider=otel` 时向 OTLP collector 发 trace
- 端到端：`--audit /tmp/audit.jsonl` 触发 tool_call → 文件含对应行

**风险**：

- OTel SDK 体积大 → build tag `obs_otel` 隔离；默认仍 noop
- Audit log 体积 → logrotate 由用户自管

---

### P4 — 上下文压缩 + 多渠道 ☐

**状态**：☐ 待启动（依赖 P3）

**目标**：实现 Compaction + Ollama + Gemini + Provider 路由。

**启动时**：新建 `docs/PHASE-4-PLAN.md`，含实施步骤；按需新建 `docs/PHASE-4-DESIGN.md` 做详细设计。

**完成判定（本阶段全部 ✅ 才能进入 P5）**：

- ☐ `docs/PHASE-4-PLAN.md` 创建
- ☐ `internal/compaction/` 完整（strategy + truncate + llmsummary + token + compactor）
- ☐ `internal/llm/ollama/` 完整
- ☐ `internal/llm/gemini/` 完整
- ☐ `internal/llm/client.go` Provider 路由
- ☐ `internal/agent/runner.go` loop 接入 compactor
- ☐ 9 个验收用例全部通过（5 compaction + 2 ollama + 2 gemini + 1 路由）

**不做（本阶段）**：

- Anthropic thinking / budget control（v5+）
- 模型自动 fallback（v5+）
- 多实例 session 共享

**参考（仅引用，详细设计在 PHASE-4-DESIGN）**：

- `../deepseek-harness/packages/compaction/` — Node 版 BasicCompactionEngine
- `../deepseek-harness/packages/compaction/strategy.ts` + `truncate.ts` + `summary.ts` — 策略模式
- `../deepseek-harness/packages/llm/llm-deepseek/src/common/messages-api.ts` — DeepSeek Messages（Ollama 不一样，但 URL 模式参考）
- `../deepseek-harness/docs/deepseek-llm-api-wire-extensions.md` — wire extensions
- `../deepseek-harness-java/deepseek-harness-java-domain/src/main/java/cn/xiaofuge/deepseek/harness/domain/agent/service/compaction/BasicCompactionEngine.java` — Java 版压缩思路

**子 TODO 索引**（在 PHASE-4-PLAN 内展开）：

- T4.1 `internal/compaction/strategy.go`：Strategy interface
- T4.2 `internal/compaction/truncate.go`：truncate 实现
- T4.3 `internal/compaction/llmsummary.go`：llm-summary 实现
- T4.4 `internal/compaction/token.go`：token 估算（tiktoken-go 可选）
- T4.5 `internal/compaction/compactor.go`：调度器
- T4.6 `internal/agent/runner.go`：loop 每轮调 compactor.Maybe
- T4.7 `internal/llm/ollama/ollama.go`：Ollama Client + Chat + ChatStream
- T4.8 `internal/llm/gemini/gemini.go`：Gemini Client + Chat + ChatStream
- T4.9 `internal/llm/client.go`：Provider 路由 switch
- T4.10 `internal/config/config.go`：LLMConfig.Provider 验证
- T4.11 9 个验收用例
- T4.12 README 更新（4 provider 配置示例）

**工时**：4 天

**验收**：

- `PHASE-4-PLAN.md` 完成
- `go test ./internal/compaction/... ./internal/llm/ollama/... ./internal/llm/gemini/...` 全绿
- 端到端：长对话 50 轮后 `/history` 看到 summary
- 端到端：本地 `ollama serve` + `provider: ollama` → dsh 可用
- 端到端：`provider: gemini` + 真实 API key → dsh 可用

**风险**：

- Ollama / Gemini 协议变动 → 抽 adapter 层
- Compaction 让 LLM"失忆" → prompt 注入压缩说明 + 保留 system + 保留最近 N

---

### P5 — 子代理 + 测试用例库 + 收尾 ☐

**状态**：☐ 待启动（依赖 P4 + P1）

**目标**：实现 Sub-agent sync + 建立 test-cases.md + v4 tag 发布。

**启动时**：新建 `docs/PHASE-5-PLAN.md`，含实施步骤；按需新建 `docs/PHASE-5-DESIGN.md` 做详细设计。

**完成判定（v4 发布）**：

- ☐ `docs/PHASE-5-PLAN.md` 创建
- ☐ `internal/subagent/sync.go` 创建
- ☐ `internal/tools/agent_spawn.go` 创建
- ☐ `internal/agent/runner.go` SubSpawner 字段接入
- ☐ `docs/test-cases.md` 创建（≥ 50 条）
- ☐ `docs/RELEASE-v4.md` 创建
- ☐ README 更新
- ☐ cmd/dsh version → 0.4.0
- ☐ git tag v0.4.0
- ☐ 5 个验收用例全部通过
- ☐ `go test -count=1 ./...` 全绿（合计 ~170 用例）
- ☐ `go vet ./...` 0 warning
- ☐ `go test -cover ./...` ≥ 80%

**不做（本阶段）**：

- 并发 sub-agent（v5+）
- Skill loader（P2，v5+）
- OS sandbox（v5+）
- Plan mode / Persona / Schedule（v5+）

**参考（仅引用，详细设计在 PHASE-5-DESIGN）**：

- `../deepseek-harness/packages/subagent/subagent.ts` — Node 版 sub-agent 接口
- `../deepseek-harness/packages/subagent/fork.ts` + `../deepseek-harness/packages/subagent/spawn.ts` — Fork vs Spawn 思路
- `../deepseek-harness-java/deepseek-harness-java-domain/src/main/java/cn/xiaofuge/deepseek/harness/domain/agent/service/subagent/SyncSpawner.java` — Java 版 sync spawner（如存在）
- `../deepseek-harness-java/deepseek-harness-java-domain/src/main/java/cn/xiaofuge/deepseek/harness/domain/agent/service/subagent/ForkInProcessProvider.java` — Java 版 in-process fork
- `../deepseek-harness-java/docs/md/test-cases.md` — Java 版用例库（直接借鉴结构）
- `../deepseek-harness-java/docs/md/release-v0.1.7-development-notes.md` §8.1 — Java 版测试用例库章节
- `../deepseek-harness/docs/development.md` — Node 版开发指南

**子 TODO 索引**（在 PHASE-5-PLAN 内展开）：

- T5.1 `internal/subagent/sync.go`：Spawner interface + SyncSpawner
- T5.2 `internal/tools/agent_spawn.go`：内置工具
- T5.3 `internal/agent/runner.go`：SubSpawner 字段 + 嵌套深度保护
- T5.4 `docs/test-cases.md`：用例库（≥ 50 条）
- T5.5 `docs/RELEASE-v4.md`：实际交付清单 + 已知限制 + 升级指南
- T5.6 README 更新（v4 功能段落 + 4 provider 配置 + Gateway SSE）
- T5.7 cmd/dsh version → 0.4.0
- T5.8 git tag v0.4.0
- T5.9 5 个验收用例 + 全量回归 + 覆盖率 ≥ 80%

**工时**：3 天

**验收**：

- `PHASE-5-PLAN.md` 完成
- `go test -count=1 ./...` 全绿（合计 ~170 用例）
- `go vet ./...` 0 warning
- `go test -cover ./...` ≥ 80%
- `docs/test-cases.md` 存在且 ≥ 50 条用例
- `docs/RELEASE-v4.md` 发布
- `git tag v0.4.0` 存在

**风险**：

- 子代理复用父 Registry 工具 → `agent_spawn` 自身不应在子 Runner 中注册（避免无限递归）
- 用例库维护成本 → 单一真相源；PR review 强制检查

---

## §4 阶段间交付物与发布节奏

### §4.1 每阶段交付物

| 阶段 | 实施计划 | 代码 | 测试 | 文档 |
|---|---|---|---|---|
| P0 ✅ | — | — | — | `phase/v4-evolution` branch + v2.0.2 tag |
| P1 ☐ | `PHASE-1-PLAN.md` | `internal/store/{event,projection,projector}.go` | 6 用例 | — |
| P2 ☐ | `PHASE-2-PLAN.md` | `internal/server/gateway*.go` | 5 用例 | README |
| P3 ☐ | `PHASE-3-PLAN.md` | `internal/obs/*.go` + `internal/audit/*.go` | 7 用例 | — |
| P4 ☐ | `PHASE-4-PLAN.md` | `internal/compaction/*.go` + `internal/llm/{ollama,gemini}/*.go` | 9 用例 | README |
| P5 ☐ | `PHASE-5-PLAN.md` | `internal/subagent/*.go` + `internal/tools/agent_spawn.go` | 5 用例 + 用例库 | `test-cases.md` + `RELEASE-v4.md` |

### §4.2 Release 节奏

- P0 ✅ 不发版（baseline lock，已是 v2.0.2）
- P1 完成 → 可发 `v3.1-preview`（可选）
- P2 完成 → 可发 `v3.2-preview`（可选）
- P3 完成 → 可发 `v3.3-preview`（可选）
- P4 完成 → 可发 `v3.4-preview`（可选）
- **P5 完成 → 发 `v4.0.0`（正式版）**

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
| R2 | OTel SDK 体积大 | build tag `obs_otel` 隔离 | P3 |
| R3 | Ollama / Gemini 协议变动 | 抽 adapter 层 | P4 |
| R4 | Compaction 让 LLM"失忆" | prompt 注入压缩说明 + 保留 system + 保留最近 N | P4 |
| R5 | Audit log 体积 | logrotate 由用户自管；默认 redact | P3 |
| R6 | 投影缓存无容量上限 | v5+ 加 LRU | P1 |
| R7 | 旧 v2 client 依赖旧端点 | 旧端点保留 + 转发 | P2 |
| R8 | 测试用例库维护成本 | 单一真相源；PR review 强制 | P5 |
| R9 | Provider 路由错配 | 启动时校验 provider + 真实可达 | P4 |
| R10 | 子代理无限递归 | agent_spawn 不在子 Runner 注册；嵌套深度保护 | P5 |
| R11 | SQLite 单写者限制 | v2 已用 WAL + busy_timeout；P1 不再上 lease | P1 |
| R12 | 用户对阶段粒度不满意 | 本文档每阶段可拆分（如 P3 拆 obs / audit） | 全部 |

---

## §7 启动指令

如确认按本规划执行：

1. **P0 收尾**（今天）：创建 `phase/v4-evolution` branch（v2.0.2 tag 已存在）
2. **P1 启动**（下次开工）：先写 `docs/PHASE-1-PLAN.md`（实施步骤）→ 按需写 `PHASE-1-DESIGN.md`（详细设计）→ 开始写代码
3. 每阶段流程：**PHASE-N-PLAN.md** → 代码 → 测试 → commit → **更新本 ROADMAP 状态** → 下阶段

每阶段开工前请先告知，我会：
- 在新对话中创建 `PHASE-N-PLAN.md`
- 启动该阶段的实施
- 完成验收后再回到本 ROADMAP 更新状态

---

**版本**：0-ROADMAP v0.2（2026-09-20 起）
**配套文档**：每个阶段的 `PHASE-N-PLAN.md`（启动时创建）；`PHASE-N-DESIGN.md`（按需创建）
**配套 release**：`docs/RELEASE-v4.md`（P5 完成时创建）
