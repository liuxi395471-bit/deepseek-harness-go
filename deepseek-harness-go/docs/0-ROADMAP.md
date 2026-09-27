# DeepSeek Harness Go — 🗺️ 整体演进路线图（0-ROADMAP）

> **这是 dsh 的"零号文档"**：从最终目标到当前状态、到未来 4 个版本的整体规划。
> **不在此文档做详细设计**：每阶段启动时，新建独立的 `PHASE-N-PLAN.md` 做实施步骤，必要时 `PHASE-N-DESIGN.md` 做详细规格。
>
> 适用对象：deepseek-harness-go（v4.0.0 完成于 2026-09-26；本规划更新于 2026-09-26）。

---

## 📑 文档组织

```
docs/
├── 0-ROADMAP.md          ← 本文档（唯一）
├── examples/             ← 跨版本示例
├── v1/                   ← v1 历史快照（PLAN.md / DESIGN.md）
│   └── README.md
├── v2/                   ← v2 历史快照（DESIGN-v2.md）
│   └── README.md
├── v3/                   ← v3 完整文档组
│   ├── PLAN-v3.md
│   ├── DESIGN-v3.md
│   ├── RELEASE-v3.md
│   └── README.md
├── v4/                   ← v4 完整文档组
│   ├── DESIGN-v4.md
│   ├── PHASE-1-PLAN.md / PHASE-2-PLAN.md / PHASE-3-PLAN.md / PHASE-4-PLAN.md
│   ├── test-cases.md（TC-0001~TC-0099）
│   ├── RELEASE-v4.md
│   └── README.md
├── v6/                   ← v6 完整文档组（任务 + 工作流）
│   ├── PHASE-6-PLAN.md
│   ├── DESIGN-v6.md
│   ├── RELEASE-NOTES.md
│   └── TEST-CASES.md
├── v7/                   ← v7 完整文档组（生态与协议互通）
│   ├── PHASE-7-PLAN.md
│   ├── DESIGN-v7.md
│   ├── RELEASE-NOTES.md
│   └── TEST-CASES.md
└── common/               ← 跨版本共享文档（保留空）
```

**命名规则**：
- `0-ROADMAP.md` 是**唯一**的整体规划；
- `vN/` 子目录归档该版本的 PLAN / DESIGN / PHASE / RELEASE / test-cases；
- `PHASE-N-PLAN.md` 是单阶段实施步骤（每个阶段一份，启动时创建）；
- `PHASE-N-DESIGN.md` 是单阶段详细规格（按需）；
- `PLAN-vN.md` / `DESIGN-vN.md` 是 vN 整体规划/设计（冻结为历史快照）。

---

## §1 最终目标

### §1.1 项目定位

`deepseek-harness-go` 是 DeepSeek AI 在 **2026 年开源的 `dsh` Agent Harness** 的 Go 语言实现。
上游参考实现位于本仓库同级目录：

| 上游 | 形态 | 关键特征 |
|---|---|---|
| [`../deepseek-harness/`](../deepseek-harness/) | TypeScript（官方主线，60+ packages）| Cordis-based "everything-is-a-plugin"；5 profile（`web` / `headless` / `sdk` / `sdk-minimal` / `acp`）；Web UI + Desktop + SDK + ACP server |
| [`../deepseek-harness-java/`](../deepseek-harness-java/) | Java（v0.1.7，DDD 六边形 8 模块）| Spring Boot + Web Console + 原生 JS；116 用例库；MyBatis + JSONL/H2 |

ds-go 的**最终目标**：

> **成为一个面向开发者 / 嵌入式场景的轻量级 dsh 实现**：
>
> 1. **架构层**：与官方 TS 版对齐"everything-is-a-plugin"思想，但用 Go 的"包 + interface + Registry"实现；
> 2. **能力层**：覆盖 ds-java v0.1.7 的核心域（Agent / Tool / Session / Skill / Sub-agent / Plugin / Sandbox / Audit / Obs / Compaction / LLM provider），并在 v4+ 逐步对齐 ds-java 的任务 / 工作流 / 审批 / 凭据；
> 3. **形态层**：单二进制 + SQLite + HTTP/SSE API；可选 Web Console（v8+）；可选 ACP / SDK 服务端（v7+）；
> 4. **生态层**：通过 v7 的插件工程化支持 Java Native / Node Bridge / MCP 三类插件；
> 5. **可移植层**：在 Linux / macOS / Windows 三大平台均可一键启动；不依赖任何 CGO。

### §1.2 终态能力图谱

最终目标的能力图谱（对应 ds-ts 上游子系统）：

| # | 能力 | 上游包（ds-ts） | ds-go 当前 | ds-go 目标 |
|---|---|---|---|---|
| 1 | Agent 执行引擎 | `core/agent` + `core/agent-loop` | ✅ v3 | ✅ |
| 2 | Session 事件 | `core/session` | ✅ v4 | ✅ |
| 3 | Token 计量 | `subsystems/token-meter` | ❌ | v5 |
| 4 | 模型路由 | `subsystems/llm-streaming` | ✅ v3 | ✅ |
| 5 | 工具管线 | `core/tools` | ✅ v3 | ✅ |
| 6 | 沙箱 | `subsystems/sandbox` | ✅ v3 | ✅ |
| 7 | Hook | `subsystems/hooks` | ❌ | v5 |
| 8 | 凭据 | `subsystems/credentials` | ❌（env only）| v5 |
| 9 | 审批 | `subsystems/approval` + `permission-presets` | ⚠ Approval only | v5/v6 |
| 10 | Skill | `packages/skill` | ✅ v3 | ✅ |
| 11 | Sub-agent | `packages/subagent` | ✅ v3 | ✅ |
| 12 | MCP | `packages/mcp` | ⚠ stdio only | v7（多 transport）|
| 13 | 插件 | `packages/extensions` + `host` + `runtime` | ✅ v2 + v3 | v7（多接入）|
| 14 | 凭据加密 | `packages/credentials` | ❌ | v5/v6 |
| 15 | 任务 / 工作流 | `packages/workflow` | ❌ | v6 |
| 16 | Goal / Plan / Todo | `packages/goal` / `plan` / `todo` | ❌ | v6 |
| 17 | Jobs | `packages/jobs` | ❌ | v6 |
| 18 | Terminal | `packages/terminal` | ❌ | v6 |
| 19 | Storage KV | `packages/storage` | ❌ | v6 |
| 20 | LSP | `packages/lsp` | ❌ | **v7 ✅** |
| 21 | SSH | `packages/ssh` | ❌ | v8（可选）|
| 22 | Browser-Use | `packages/browser-use` | ❌ | v8（可选）|
| 23 | Computer-Use | `packages/computer-use` | ❌ | v8（可选）|
| 24 | Web-UI | `apps/web` + `apps/desktop` | ❌（CLI + API）| v8 |
| 25 | ACP | `packages/acp` | ❌ | **v7 ✅** |
| 26 | SDK | `packages/sdk` | ⚠ 部分 | **v7 ✅**（独立 Go module）|
| 27 | Spill | `packages/spill` | ❌ | v8（可选）|
| 28 | Feedback / Deliverables | `packages/feedback` / `deliverables` | ❌ | v8（可选）|
| 29 | PTC-Runtime | `packages/ptc-runtime` | ❌ | v8（可选）|
| 30 | A2A / AgentTeam | `subsystems/agent-team` | ❌ | **v7 ✅** |

**核心域（v5 + v6）**：1–10 + 11–14 + 17–19 = 22 项核心必须完成。
**扩展域（v7 + v8）**：12, 15, 16, 20, 22–30 = 11 项可选扩展。

### §1.3 实施节奏（v5 → v8 = 4 个版本）

| 版本 | 主题 | 工时 | 优先级 | 状态 |
|---|---|---|---|---|
| v4 | 事件溯源 + Gateway SSE + 库存视图 | — | — | ✅ 完成 |
| **v5** | 安全与可观测基线（写租约 / Token 计量 / 渠道 / 凭据 / 审批矩阵 / Hook）| 5–7d | 🔴 必经 | ✅ 完成（tag v5.0.0） |
| **v6** | 任务系统 + 工作流（Task / Workflow / Goal / Plan / Todo / Jobs / Terminal / Storage）| 5–6d | 🟡 必经 | ✅ 完成（tag v6.0.0） |
| **v7** | 生态与协议互通（Node Bridge / MCP multi-transport / ACP / SDK / LSP / AgentTeam）| 6–8d | 🟢 选做 | ✅ 完成（tag v7.0.0） |
| **v8** | Web Console + Desktop（Vite+Vue 3 + Wails v2）| 8–10d | 🟢 选做 | 🚧 实施中（P8-1 ✅ / P8-2 ✅ / P8-3 ☐）|
**v5 / v6 / v7 均已 ✅ 完成**；核心差距已全部覆盖。
**完整对齐上游 ds-ts 主体能力预计还需 1–2 个版本**（v8 + 任意后续；剩余项均为可选）。

### §1.4 与 ds-java v0.1.7 的差距映射

| ds-java v0.1.7 主线 | ds-go 状态 | 何时补齐 |
|---|---|---|
| 会话事件 v3 | ✅ v4 P1 | v4 |
| 写租约 | ❌ | **v5 P5-1** |
| 投影缓存 | ✅ v4 P1 | v4 |
| Token 计量 | ❌ | **v5 P5-2** |
| 统一 Gateway SSE | ✅ v4 P2 | v4 |
| 插件库存 | ✅ v4 P3 | v4 |
| 工具结果透传 | ⚠ 部分（v3 已有 IsError） | v5 |
| 多模型渠道 | ⚠ 单一 YAML | **v5 P5-3** |
| 凭据管理 | ❌ | **v5 P5-4** |
| 审批矩阵 | ❌（仅 Approval 一维）| **v5 P5-5** |
| Hook 机制 | ❌ | **v5 P5-6** |
| Web Console | ❌ | **v8** |
| 任务 / 工作流 | ❌ | **v6** |
| Goal / Plan / Todo | ❌ | **v6** |
| Jobs / Terminal / Storage | ❌ | **v6** |
| ACP / SDK | ❌ | **v7** |
| AgentTeam / A2A | ❌ | **v7** |
| LSP | ❌ | **v7** |
---

## §2 当前状态（截至 v4.0.0）

### §2.1 已完成阶段

| 阶段 | 主题 | 工时 | 完成日期 | 关键交付 |
|---|---|---|---|---|
| **P0** | 现状冻结 | 0.5d | 2026-09-16 | v2.0.2 tag + branch |
| **v3** | Skill+Sub-agent+Obs+Audit+Compaction+Ollama+Gemini | — | 2026-09-26 | 7 模块 + RELEASE-v3 |
| **P1** | Session 事件溯源 + 投影缓存 | 3d | 2026-09-26 | `internal/store/{event,projection,projector}.go` |
| **P2** | 统一 Gateway SSE | 2d | 2026-09-26 | `internal/server/{gateway,gateway_handler}.go` |
| **P3** | 插件 / 工具库存视图 | 1.5d | 2026-09-26 | `internal/plugin/{view,grpc_view}.go` |
| **P4** | 测试用例库 + 收尾 | 2d | 2026-09-26 | `test-cases.md` + `RELEASE-v4.md` + tag `v4.0.0` |

### §2.2 关键指标

| 指标 | v2.0.2 | v3.0.0 | v4.0.0 |
|---|---:|---:|---:|
| Go 包数 | 19 | 24 | 24 |
| 自动化测试用例 | ~80 | ~130 | 99（统一编号 TC-0001~TC-0099）|
| `go test ./...` | ✅ | ✅ | ✅ |
| `go vet ./...` | ✅ | ✅ | ✅ |
| Git tag | v2.0.2 | v3.0.0 | v4.0.0 |

### §2.3 当前架构（v4）

```
cmd/dsh/                ← CLI 入口（main.go + repl/）
internal/
├── agent/              ← ReAct LoopRunner
├── llm/                ← LLM 适配层（含 provider 路由）
├── store/              ← v4 起：事件 + 投影 + 缓存
├── server/             ← v4 起：统一 Gateway SSE
├── plugin/             ← v2 起：gRPC 插件；v4 加库存视图
├── skill/              ← v3 Skill
├── subagent/           ← v3 子 Agent
├── obs/                ← v3 OTel
├── audit/              ← v3 JSONL 审计
├── compaction/         ← v3 上下文压缩
├── sandbox/            ← v3 OS 沙箱
├── tool/               ← 工具接口 + Registry
├── tools/              ← 内置工具（shell / fs）
├── stream/             ← SSE helper
├── usage/              ← v3 usage tracker
├── approval/           ← v3 approval
├── config/             ← YAML 配置
└── mcp/                ← MCP stdio
```

---

## §3 阶段详情（v5 → v8）

### v5 — 安全与可观测基线

**状态**：☐ 待启动
**主题**：把"可观测 + 可控 + 可审计"做到产品级
**工时**：5–7 天
**优先级**：🔴 必经
**依赖**：v4.0.0

**目标**：收敛 ds-java v0.1.7 的 9 项核心差距，覆盖 token 计量 / 凭据 / 审批矩阵 / Hook / 模型渠道 / 写租约。

**子 TODO**：
1. **P5-1 会话写租约** — `internal/store/lease.go`，单 sid 写入互斥；30s TTL；与 `EventStore.AppendEvent` 集成。
2. **P5-2 Token 计量域** — `internal/usage/meter.go`，`SessionTokenMeter` 服务；扫描 events 聚合 `cache_read` / `cache_write` / `reasoning`；Gateway 暴露 `usage.meter` source。
3. **P5-3 模型渠道配置** — `internal/runtime/channel.go` + SQLite 表；`channel_code` / `protocol` / `active` 字段；运行时按 channelCode 解析；CLI `-channel` 切换。
4. **P5-4 凭据抽象** — `internal/credentials/provider.go`，从 env / 加密 yml / KMS 解析；`CredentialRef` 引用；与 LLM client 解耦。
5. **P5-5 审批矩阵** — `internal/approval/matrix.go`，profile × tools 二维矩阵；自动 split gated vs autoApproved；与 `SubmitTask` 集成。
6. **P5-6 Hook 机制** — `internal/hook/`，`PRE_TOOL_USE` / `POST_TOOL_USE` 两点；按 hook matcher 调度；用户可注入。

**验收**：
- `TC-v5-0001` ~ `TC-v5-0012`（12 个新用例）通过；
- `go test -race ./...` 无数据竞争；
- 全部 v3/v4 既有测试不回归。

**详细计划**：`v5/PHASE-5-PLAN.md`（启动时创建）；详细规格按需 `v5/DESIGN-v5.md`。

---

### v6 — 任务系统 + 持久化工作流

**状态**：🚧 进行中
**主题**：把"任务执行 + 异步 + 工作流"做到可编排
**工时**：5–6 天
**优先级**：🟡 必经
**依赖**：v5
**用户决策**（2026-09-27）：
- 范围：全部 6 子 TODO
- Task：最小子集（SubmitTask + 状态查询；filter chain 延后 v6.1）
- Workflow：DAG 节点 + 条件路由 + 并行分支（fan-out/join）

**目标**：补齐 ds-java 的"任务 / 目标 / 计划 / 后台任务 / 终端 / 存储"6 个领域。

**子 TODO**：
1. **P6-1 任务领域** — `internal/task/`，`SubmitTaskRequest` / `TaskSubmissionFilter` 链（5 个）/ `ExecutionProfile` / `PermissionPolicyService` / `ApprovalPolicyService`。
2. **P6-2 工作流引擎** — `internal/workflow/`，节点 + 上下文 + 状态机；复用 v4 Gateway SSE 流式输出。
3. **P6-3 后端 Jobs** — `internal/jobs/`，`JobRegistry`（in-memory）+ 4 工具（run / list / output / kill）；与 `internal/stream/` 集成。
4. **P6-4 目标 / 计划 / Todo** — `internal/goal/` / `internal/plan/` / `internal/todo/`；给 Agent 暴露 `plan_mode` / `todo_write` 工具。
5. **P6-5 Terminal 域** — `internal/terminal/`，支持 PTY 长会话；暴露 `terminal_run` / `terminal_read` / `terminal_kill` 工具。
6. **P6-6 KV 存储抽象** — `internal/storage/`，in-memory + file + 可选 Redis 端口。

**验收**：
- `TC-v6-0001` ~ `TC-v6-0018`（18 个新用例）通过；
- Gateway 暴露 `task.submit` / `workflow.start` / `jobs.list` 等 source；
- 全部 v3/v4/v5 既有测试不回归。

**详细计划**：`v6/PHASE-6-PLAN.md`（启动时创建）。

---

### v7 — 生态与协议互通

**状态**：✅ 完成（tag v7.0.0，wiring commit `1979012`）
**主题**：让 ds-go 能接入 Java / Node 插件生态，对外暴露协议
**工时**：6–8 天
**优先级**：🟢 选做
**依赖**：v6

**目标**：让 ds-go 不再是"孤岛"，可以接入其他生态的插件、可以被其他客户端作为协议服务端。

**子 TODO**：
1. **P7-1 插件安装工程化** — `internal/plugin/installer.go`；扫描 `installRoot`、识别 JAR / `package.json` / `plugin.json`；持久化 `plugin_status`；状态对账。
2. **P7-2 Node Bridge 插件** — `internal/plugin/bridge/node/`，JSON-RPC stdio；入口限 `installRoot` 内。
3. **P7-3 MCP multi-transport** — `internal/mcp/`，增加 http / sse 两种 transport；wiring 时把 MCP JSON-RPC 桥到 Gateway source。
4. **P7-4 ACP 服务端** — `internal/acp/`，`AcpServerService`；让外部 IDE / 编辑器接入；wiring 时挂到 `/acp/*`。
5. **P7-5 SDK** — `internal/sdk/`，统一 JSON-RPC / HTTP / WS 客户端；Go SDK 也可独立发布。
6. **P7-6 AgentTeam / A2A** — `internal/a2a/`，参考 Java AgentTeam；多 agent 发现 + 协作。
7. **P7-7 LSP 工具** — `internal/lsp/`，暴露给 Agent 的 hover / references 工具。
8. **P7-8 意图分类** — `internal/agent/intent.go`，9 条正则（移植自 Java）。

**验收**：
- `TC-v7-0001` ~ `TC-v7-0030`（30 个新用例）通过；
- 端到端：Node 插件接入 ds-go；ACP IDE 接入 ds-go；
- `dsh -serve` 暴露 `/acp/*` + `/mcp`，curl 实测返回 200；
- `sdk-go` 模块独立 `go build ./...` 通过。

**详细计划**：`v7/PHASE-7-PLAN.md`（已完成）。

---

### v8 — Web Console + Desktop

**状态**：🚧 实施中（2026-09-27）；P8-1 ✅ / P8-2 ✅ / P8-3 ☐ 待启动
**主题**：把 ds-go 提升到"产品级"用户体验
**工时**：8–10 天
**优先级**：🟢 选做
**依赖**：v7.0.0（tag `cc1dd87`）
**用户决策**（2026-09-27）：
- Web 控制台 = **Vite + Vue 3 SPA**（不上 Wails / 原生 JS / Go WASM）
- 可选子项 = **核心 5 页 + Desktop 打包（Wails v2）**
- 不做 Spill / Browser-Use / PTC-Runtime（留 v8.1）

**目标**：原生 Web 控制台（参考 ds-ts `apps/web` 与 ds-java 控制台），把"会话 / 插件 / 模型 / 任务 / 审批"5 件事都在浏览器里完成；并提供 Wails 桌面打包。

**子 TODO**：
1. **P8-1 后端 Console 域** — `internal/console/`：路由 + auth + 5 类 handler + SSE 转发 + `//go:embed` SPA；新增 1 张 `console_state` 表存 UI 偏好。
2. **P8-2 前端 Vue 3 SPA** — `web/`：Vite + Vue 3 + Pinia + vue-query；5 个页面（会话 / 插件 / 模型 / 任务 / 审批）；主 chunk ≤ 300KB gzip。
3. **P8-3 Desktop + 收尾** — `desktop/`：Wails v2 模板 + Tray；嵌入 ds-go 二进制；`docs/v8/{RELEASE-NOTES.md,TEST-CASES.md}`；tag `v8.0.0`。

**验收**：
- `TC-v8-0001` ~ `TC-v8-0008`（8 个新用例）通过；
- `curl http://127.0.0.1:7777/console/` 返回 200；
- `wails build` 至少 Windows 平台成功；
- 全部 v3/v4/v5/v6/v7 既有测试不回归。

**详细设计**：`v8/DESIGN-v8.md`（已完成）；**详细计划**：`v8/PHASE-8-PLAN.md`（已完成）。

---

## §4 跨版本不变量

不论 v5 → v8 如何演进，下列不变量必须保持：

1. **v2/v3 公开 API 冻结**：`agent.StreamingRunner` / `tool.Registry` / `llm.Client` / `store.Store` 接口签名不变。
2. **不引入新三方依赖**：除非 v5 明确说明（如 KMS SDK）。
3. **测试 ≥ 80% 覆盖率**：每个新包至少 80%。
4. **跨平台一致**：Linux / macOS / Windows 三大平台 `go test ./...` 全绿。
5. **RELEASE-vN.md 必备**：每个版本必须含变更 / 迁移 / 已知限制 / 验证矩阵。
6. **文档目录约定**：每版本独立 `vN/` 子目录；`0-ROADMAP.md` 是唯一整体规划。

---

## §5 风险总表

| # | 风险 | 应对 | 阶段 |
|---|---|---|---|
| R1 | v5 写租约可能影响性能 | 仅在 AppendEvent 时获取；30s TTL；提供 metric | v5 |
| R2 | v6 工作流可能状态机爆炸 | 借鉴 Java `WorkflowService` + `WorkflowRun` | v6 |
| R3 | v7 ACP 协议复杂度 | ✅ 已实现最小子集（HTTP 控制面 + permission/decide）；WebSocket 留 v7.1 | v7 |
| R4 | v8 Web Console 工作量大 | 已选"Vite+Vue SPA + Desktop"（不上原生 JS）；UI 组件复用 ds-ts 上游心智 | v8 |
| R5 | SQLite 单写者限制 | 已用 WAL + busy_timeout；v5 写租约在 EventStore 层再加强 | v5 |
| R6 | 上游 ds-ts 演进速度可能比 ds-go 快 | 每季度 diff 一次 ds-ts subsystems；评估对齐优先级 | 全周期 |

---

## §6 启动指令

如确认按本规划执行：

1. **v5 启动**（下次开工）：先写 `docs/v5/PHASE-5-PLAN.md` → 按需写 `docs/v5/DESIGN-v5.md` → 开始写代码；
2. **每阶段流程**：`PHASE-N-PLAN.md` → 代码 → 测试 → commit → 更新本 ROADMAP 状态 → 下阶段；
3. **新阶段允许重命名**：若后续阶段主题与计划不符，可在 `vN/` 下重建。

每阶段开工前请先告知：

- 在新对话中创建 `PHASE-N-PLAN.md`；
- 启动该阶段的实施；
- 完成验收后再回到本 ROADMAP 更新状态。

---

**版本**：0-ROADMAP v0.8（2026-09-27：v5/v6/v7 全部 ✅；v8 实施中 — P8-1/P8-2 ✅ / P8-3 ☐）
**配套文档**：每个阶段的 `vN/PHASE-N-PLAN.md`（启动时创建）；`vN/DESIGN-vN.md`（按需）
**配套 release**：`vN/RELEASE-NOTES.md`（vN 完成时创建；v7 见 `v7/RELEASE-NOTES.md`）