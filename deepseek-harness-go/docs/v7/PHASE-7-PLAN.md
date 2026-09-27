# PHASE-7-PLAN — 生态与协议互通（v7）

> **对应阶段**：v7（[0-ROADMAP.md](../../0-ROADMAP.md) §3 v7）
> **对应详细设计**：[DESIGN-v7.md](./DESIGN-v7.md)
> **状态**：✅ 完成（tag v7.0.0，2026-09-27）
> **工时**：6–8 天
> **基线**：v6.0.0 tag
> **主题**：让 ds-go 能接入 Java / Node 插件生态，对外暴露协议
> **用户决策**（2026-09-27）：
> - 范围：全部 8 子 TODO
> - 方向：协议优先（ACP / SDK / MCP multi-transport 优先）
> - MCP transports：HTTP + SSE
> - IDE 目标：任何遵循 ACP 的通用编辑器（不绑特定 IDE）
> - A2A 深度：全量编排（任务路由 + 状态共享）
> - LSP 后端：外接真实 LSP 服务器（gopls / tsserver 等）

## §1 目标

把 ds-go 从"孤岛"变成"互联节点"：
- 能跑来自 Java/Node 生态的插件（Node Bridge）；
- 能被外部 IDE 作为 ACP 协议服务端接入；
- 能暴露 MCP 多 transport 给任何 MCP 客户端；
- 能发布 Go SDK 让第三方集成；
- 能作为 AgentTeam 节点参与多 agent 协作；
- 给 Agent 暴露 LSP 工具做代码智能；
- 用意图分类在 system prompt 注入路由策略。

## §2 子阶段总览

| 编号 | 子阶段 | 包 | 天 | 优先级 | 说明 |
|---|---|---|---|---|---|
| P7-1 | 插件安装工程化 | `internal/plugin/installer/` | 0.5 | 🟢 P2 | installer / status / reconcile |
| P7-2 | Node Bridge 插件 | `internal/plugin/bridge/node/` | 1.0 | 🟡 P1 | JSON-RPC stdio；installRoot 边界 |
| P7-3 | MCP multi-transport | `internal/mcp/` | 1.0 | 🟡 P1 | http + sse transport |
| P7-4 | ACP 服务端 | `internal/acp/` | 1.5 | 🔴 P0 | 通用编辑器；sessions / send / cancel |
| P7-5 | SDK | `internal/sdk/` + `sdk-go/` | 1.0 | 🔴 P0 | JSON-RPC + HTTP client；可独立发布 |
| P7-6 | AgentTeam / A2A | `internal/a2a/` | 1.5 | 🟡 P1 | 多 agent 发现 + 全量编排（路由 + 状态） |
| P7-7 | LSP 工具 | `internal/lsp/` | 1.0 | 🟢 P2 | hover / references / definition；外接真实 LSP |
| P7-8 | 意图分类 | `internal/agent/intent/` | 0.5 | 🟢 P2 | 9 条正则（移植自 Java） |
| **合计** | | | **8d** | | 含 buffer |

### 优先级说明
- **P0**（必须先做、阻塞下游）：P7-4 ACP、P7-5 SDK；
- **P1**（核心交付）：P7-2 Node Bridge、P7-3 MCP multi、P7-6 A2A；
- **P2**（增强 + 收尾）：P7-1 installer、P7-7 LSP、P7-8 意图分类。

### 实施顺序（考虑依赖）
1. P7-5 SDK（先有 SDK，其他子阶段测试可复用）
2. P7-1 installer（P7-2 依赖 installer 的扫描结果）
3. P7-2 Node Bridge（基于 installer + SDK）
4. P7-3 MCP multi（基于 v4 既有 stdio transport 扩展）
5. P7-4 ACP（基于 SDK + LoopRunner + Task 域）
6. P7-6 A2A（基于 ACP 思路 + Task 域 + Workflow 引擎）
7. P7-7 LSP（独立，基于 external_bridge 接 gopls）
8. P7-8 意图分类（独立，移植正则）

## §3 子阶段细节

### P7-1 — 插件安装工程化

**目标**：把 v4 的 plugin 扫描从"启动时一次性"升级为"运行时可查询 + 可对账"。

**新增**：
- `internal/plugin/installer/scanner.go`：扫描 `installRoot` 下的 JAR / `package.json` / `plugin.json`；生成 manifest 索引。
- `internal/plugin/installer/status.go`：每个 plugin 的 5 种状态：`discovered` / `loaded` / `failed` / `disabled` / `uninstalled`。
- `internal/plugin/installer/reconcile.go`：启动时对账（manifest vs 实际加载）；写 `plugin_status.json`。
- `internal/plugin/installer/cli.go`：CLI 子命令 `dsh plugin ls / install / uninstall / status`。

**测试**：
- `TC-v7-0001` scanner 识别 JAR / package.json / plugin.json 三种格式。
- `TC-v7-0002` status 持久化（重启可读）。
- `TC-v7-0003` reconcile 报告 orphan（manifest 有但磁盘无）和 drift（磁盘有但 manifest 无）。

---

### P7-2 — Node Bridge 插件

**目标**：让 ds-go 加载和运行 Node 写的插件。

**新增**：
- `internal/plugin/bridge/node/bridge.go`：JSON-RPC stdio；启动 `node main.js`；用 LSP-style framed JSON。
- `internal/plugin/bridge/node/manifest.go`：解析 `package.json` 的 `dshPlugin` 字段。
- `internal/plugin/bridge/node/security.go`：强约束 — 入口 `node main.js` 必须在 installRoot 子目录下。
- `internal/plugin/bridge/node/proxy.go`：把 Node 插件的工具 / 命令代理到 v4 plugin.Inventory。

**测试**：
- `TC-v7-0004` Node 插件提供工具 → Agent 可调用。
- `TC-v7-0005` Node 插件 panic → 状态 failed，不影响其他插件。
- `TC-v7-0006` 安全：入口在 installRoot 外 → 拒绝加载。

---

### P7-3 — MCP multi-transport

**目标**：现有 MCP 仅 stdio，扩展 HTTP + SSE。

**新增**：
- `internal/mcp/server/stdio.go`（已有，保留）。
- `internal/mcp/server/http.go`：HTTP POST /mcp，content-type=application/json。
- `internal/mcp/server/sse.go`：GET /mcp/sse（SSE 推送）+ POST /mcp/sse（上行）。
- `internal/mcp/registry.go`：把多个 MCP server 注册到统一 registry。

**测试**：
- `TC-v7-0007` stdio roundtrip（已有，回归）。
- `TC-v7-0008` HTTP transport 工具调用。
- `TC-v7-0009` SSE 长连接 + 事件推送。

---

### P7-4 — ACP 服务端 🔴 P0

**目标**：实现 Agent Communication Protocol，让任何遵循 ACP 的 IDE / 编辑器接入 ds-go。

**新增**：
- `internal/acp/server/server.go`：HTTP + WebSocket 双端口；处理 `session/create` / `session/send` / `session/cancel` / `session/list`。
- `internal/acp/server/handler.go`：把 ACP session 映射到 v6 Task（task.submit + SSE 转发）。
- `internal/acp/server/auth.go`：token 校验（从 v5 credentials 读取）。
- `internal/acp/types/types.go`：ACP 帧结构（content / tool_call / tool_result / permission_request）。
- `internal/acp/server/permission.go`：把 IDE 的 permission_request 转给 v5 approval 矩阵。

**测试**：
- `TC-v7-0010` session/create + session/send 闭环。
- `TC-v7-0011` WebSocket 多客户端并行 session。
- `TC-v7-0012` ACP token 鉴权失败 → 401。
- `TC-v7-0013` ACP permission_request → ds-go approval 决策。

**E2E**：写一个 mock ACP 客户端（`internal/acp/server/testclient/`）做端到端验证。

---

### P7-5 — SDK 🔴 P0

**目标**：发布 `sdk-go`（独立 module `github.com/yourorg/dsh/sdk-go`），让第三方 Go 应用集成 ds-go。

**新增**：
- `sdk-go/go.mod`：独立 module。
- `sdk-go/client/jsonrpc/client.go`：JSON-RPC over WebSocket；订阅 Gateway SSE 事件。
- `sdk-go/client/http/client.go`：HTTP REST（基于 Gateway `events.subscribe` + `session.send`）。
- `sdk-go/types/types.go`：Event / ToolCall / ToolResult 等共享类型。
- `sdk-go/README.md`：30 行 hello world。
- `sdk-go/examples/echo/main.go`：echo agent demo。

**测试**：
- `TC-v7-0014` JSON-RPC client 订阅 session 事件。
- `TC-v7-0015` HTTP client 提交 session.send。
- `TC-v7-0016` 端到端：echo demo 跑通。

---

### P7-6 — AgentTeam / A2A 🟡 P1（全量编排）

**目标**：多 agent 发现 + 全量协作（任务路由 + 状态共享 + 结果合并）。

**新增**：
- `internal/a2a/registry/registry.go`：注册 / 注销 agent capability（"code" / "chat" / "research" 等）。
- `internal/a2a/discovery/discovery.go`：本地（配置文件）+ 远程（HTTP registry）。
- `internal/a2a/orchestrator/orchestrator.go`：接收任务 → 按 capability 路由 → 跨 agent 状态共享（用 v6 storage 作为共享 KV）→ 收集结果。
- `internal/a2a/protocol/protocol.go`：A2A 帧格式（task / status / result / cancel）。
- `internal/a2a/server/server.go`：暴露本地 agent 到 A2A 网络。

**测试**：
- `TC-v7-0017` registry 注册 + 注销。
- `TC-v7-0018` orchestrator 把"research + write_code"任务路由到两个本地 agent。
- `TC-v7-0019` 状态共享（agent A 写入 storage / agent B 读取）。

---

### P7-7 — LSP 工具 🟢 P2（external bridge）

**目标**：给 Agent 提供 hover / references / definition 工具，外接 gopls 等真实 LSP 服务器。

**新增**：
- `internal/lsp/client/client.go`：JSON-RPC over stdio；启动 `gopls`。
- `internal/lsp/client/protocol.go`：LSP 帧结构（initialize / hover / references / definition）。
- `internal/tools/lsp_*.go`：4 个工具 `lsp_hover` / `lsp_references` / `lsp_definition` / `lsp_diagnostics`。

**测试**：
- `TC-v7-0020` lsp_hover 在 Go 源码上返回类型信息（需要 gopls 环境；CI skip）。

**注意**：CI 中若无 gopls，把测试标 `t.Skip("gopls not installed")`，保证 go test ./... 仍绿。

---

### P7-8 — 意图分类 🟢 P2

**目标**：把 Java 的 9 条意图正则移植到 Go，用于 system prompt 路由。

**新增**：
- `internal/agent/intent/intent.go`：9 条意图（如 `code.generate` / `code.explain` / `research.web` 等）。
- `internal/agent/intent/classify.go`：`Classify(input string) (intent string, confidence float64)`。
- `internal/agent/intent/prompt.go`：根据 intent 注入 system prompt 段落。

**测试**：
- `TC-v7-0021` 9 条正则覆盖率（每个意图给一个典型输入验证）。

---

## §4 跨阶段不变量

- v2/v3/v4/v5/v6 公开 API 冻结。
- 现有 34 包 + 新 5+ 包（acp / sdk / a2a / lsp / intent）≥ 80% 测试覆盖。
- 新增 4 个三方依赖允许：LSP client（仅 stdio）、gorilla/websocket（ACP HTTP）、go-reverse-rpc 候选。
- 不引入 React / Vite / Webpack（这些给 v8）。
- ACP / SDK / A2A 都基于 v6 Storage（共享 KV）做状态协调。
- LSP / MCP multi-transport 都基于 v4 plugin.Inventory 风格的注册表。

## §5 验收

- `go vet ./...` 无 warning
- `go test -count=1 ./...` 全绿（34 + 新 ≈ 39+ 包；LSP 测在 gopls 缺失时 skip）
- `TC-v7-0001` ~ `TC-v7-0021` 全部通过（含 LSP 的 skip 优雅降级）
- sdk-go 模块独立 `go build ./...` 通过
- ACP mock 客户端 E2E 通过
- git tag `v7.0.0`

## §6 文档交付

- `DESIGN-v7.md`（架构设计）
- `TEST-CASES.md` 增到 `TC-v7-0021`
- `RELEASE-NOTES.md` 发布说明
- `sdk-go/README.md` SDK 入门
- `docs/0-ROADMAP.md` v7 标完成

## §7 风险与降级

| 风险 | 缓解 |
|---|---|
| gopls 在 CI 不可用 | LSP 测试 skip；不影响 `go test ./...` |
| WebSocket 库选型 | 用 gorilla/websocket（最成熟），不引入 nhooyr.io |
| ACP 协议版本 | 锁定 v1 spec（参考 dsh-java 的 `acp.proto`）；不追 draft |
| A2A 跨进程通信 | v7 限定为同进程多 agent；跨进程留 v7.1 |
| Node Bridge Node 版本 | 文档要求 Node 18+；CI 用 Node 20 |

## §8 与上游 ds-ts / ds-java 对齐

| 能力 | ds-java v0.1.7 | ds-go v7 |
|---|---|---|
| Node Bridge | ✅ | ✅ |
| ACP server | ✅ | ✅ |
| MCP multi-transport | ✅ | ✅（http + sse） |
| AgentTeam | ✅ | ✅（全量） |
| LSP | ✅ | ✅（external bridge） |
| SDK | ✅ Java/TS | ✅ Go |
| 意图分类 | ✅ | ✅（9 条正则） |
| 插件安装工程化 | ✅ | ✅ |
