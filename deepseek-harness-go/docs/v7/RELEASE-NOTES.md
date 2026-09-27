# v7 Release Notes

**v7.0.0 — Ecosystem & Protocol Interoperability**

发布时间：2026-09-27

> 🏷 Git tag: `v7.0.0`（commit `80bd7b6`）
> 🧩 Wiring commit: `1979012`（把协议层挂到 `dsh -serve`）
> 📦 SDK commit: 见 `sdk-go/`

---

## 🎉 新增

### P7-5 sdk-go 模块（独立 Go module）
- 路径：`sdk-go/`（仓库根）
- `types/` — Frame / ToolCall / ToolResult / Permission* / Session* 共用类型
- `client/http/` — SendSession / SubscribeEvents（SSE）/ Permission
- `client/jsonrpc/` — JSON-RPC 2.0 客户端 + 自定义 Transport 抽象
- `examples/echo/` — 最小接入 demo（30 行 SDK demo）

### P7-1 插件安装工程化（`internal/plugin/installer/`）
- `Scanner`：扫描 installRoot，识别 JAR / package.json / plugin.json
- `StatusStore`：5 状态 + JSON 原子持久化
- `Reconciler`：磁盘 vs status 对账，输出 Drift / Orphans / Healthy

### P7-2 Node Bridge（`internal/plugin/bridge/node/`）
- JSON-RPC over stdio（Content-Length framed）
- `initialize` / `tools/list` / `tools/call`
- 安全：入口必须位于 installRoot 内
- `debug_bridge/main.go` 提供手动 roundtrip 工具

### P7-3 MCP 多 transport（`internal/mcp/transport.go` + `internal/server/mcp_bridge.go`）
- `HTTPTransport`：POST /mcp → JSON-RPC
- `SSETransport`：GET /mcp/sse（推）+ POST /mcp/sse（拉）
- `SSEServerHandler`：可挂载到 http.Server 的统一接入
- **`server.MCPDispatcher(router)`**：把 MCP JSON-RPC 桥到 Gateway source（method 映射 slash ↔ dot）

### P7-4 ACP 服务端 🔴 P0（`internal/acp/`）
- session/create / session/send / session/cancel / session/list / permission/decide
- Bearer token 鉴权（从 v5 credentials 注入）
- 1:1 映射 v6 Task 域
- **`acp.TaskExecutorAdapter`**：把 `task.Executor` 桥到 ACP 内部 `TaskSubmitter`

### P7-6 AgentTeam / A2A（`internal/a2a/`）
- `Registry`：capability → agents 注册表
- `Orchestrator`：顺序执行 steps，跨 agent 共享 state 写入 v6 KV（storage）

### P7-7 LSP 工具（`internal/lsp/`）
- JSON-RPC over stdio（标准 LSP framed）
- Initialize / Hover / References / Definition / Shutdown
- 集成测试用 in-memory pipe（不依赖 gopls）

### P7-8 意图分类（`internal/agent/intent/`）
- 9 类：code_generation / code_fix / code_refactor / code_explain / file_read / file_write / shell_exec / search / chat
- 顺序正则匹配，CHAT 兜底

---

## 🔌 Wiring：把协议层挂到 `dsh -serve`（commit `1979012`）

v7 子阶段原本只交付了"独立单元"，commit `1979012` 把它们真正串到 Gateway 上：

| 路径 | 协议 | 鉴权 |
|---|---|---|
| `/healthz` | 健康检查 | 无 |
| `/api/gateway/stream` | v4 Gateway SSE | Bearer |
| `/api/agent/message` / `stream` | v4 旧端点 | Bearer |
| `/api/sessions[/...]` | v4 旧端点 | Bearer |
| `/acp/*` | ACP HTTP | Bearer（独立） |
| `/mcp` (POST) / `/mcp/sse` | MCP JSON-RPC / SSE | 无 |

新增关键组件：

- `internal/acp/task_adapter.go` — `TaskExecutorAdapter`
- `internal/server/mcp_bridge.go` — `MCPDispatcher` + `mcpMethodToGatewaySource`
- `internal/server/gateway.go` — `Router.DispatchSync`
- `internal/server/server.go` — `SetACPServer` / `SetMCPDispatcher` / `Router()`
- `internal/server/integration_test.go` — 5 个端到端集成测试
- `internal/server/fake_task_exec.go` — 测试替身

### smoke test 实测（live `dsh -serve`）

```
$ curl -X POST http://127.0.0.1:8080/acp/session/create \
       -H "Authorization: Bearer tok-test" \
       -H "Content-Type: application/json" \
       -d '{"profile":"smoke"}'
→ 200 {"session_id":"acp-1"}

$ curl -X POST http://127.0.0.1:8080/acp/session/send \
       -H "Authorization: Bearer tok-test" -H "Content-Type: application/json" \
       -d '{"session_id":"acp-1","content":"hello world"}'
→ 200 {"session_id":"acp-1","task_id":"762e3720dadb18c3dc0fdc355dd150df","state":"pending"}

$ curl -X POST http://127.0.0.1:8080/acp/session/list \
       -H "Authorization: Bearer tok-test" -H "Content-Type: application/json" \
       -d '{"session_id":"acp-1"}'
→ 200 {"tasks":[{"id":"762e3720dadb18c3dc0fdc355dd150df","state":"pending"}]}

$ curl -X POST http://127.0.0.1:8080/acp/permission/decide \
       -H "Authorization: Bearer tok-test" -H "Content-Type: application/json" \
       -d '{"id":"p1","approve":true}'
→ 200 {"ok":true}

$ curl -X POST http://127.0.0.1:8080/mcp \
       -H "Content-Type: application/json" \
       -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
→ 200 {"jsonrpc":"2.0","id":1,"result":{"plugin_count":1,"tools":[{"name":"greet",...}]}}
```

---

## ✅ 测试

- 全仓 `go test -count=1 -timeout=120s ./...` 通过（41 packages）
- sdk-go 模块 `go test ./...` 通过（3 packages）
- 30 个 v7 测试用例覆盖（`TEST-CASES.md`）：0001 ~ 0030
- 5 个端到端 integration 测试（Server-ACP / Server-MCP / DispatchSync）

## 🛠️ 兼容性

- Go 1.22+
- 仅标准库依赖
- 与 v4 Gateway SSE 帧结构兼容
- 与 dsh-java ACP 协议对齐
- /acp/* 与 /mcp 是**新增**路由，不影响既有 /api/* 端点

## 🚧 已知限制

- Node Bridge 真 roundtrip 测试在 Windows CI 跳过（生产路径由 debug_bridge 工具验证）
- ACP WebSocket 事件流留待 v7.1（HTTP 控制面已可用）
- LSP 集成未在 v7 端到端冒烟（仅 client + in-memory fake）
- MCP HTTP 当前仅 `tools/list` / `tools/call` 桥到 Gateway source；其他 method 由 MCP 客户端自行处理（返回 -32601）

## 📦 升级

```bash
git fetch --tag
git checkout v7.0.0
go build ./cmd/dsh

# 验证 wiring（启动 dsh 后另开 terminal）
DSH_SERVER_AUTH_TOKEN=tok ./dsh -serve &
curl -X POST http://127.0.0.1:8080/healthz                                # {"ok":true}
curl -X POST http://127.0.0.1:8080/mcp -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' \
     -H "Content-Type: application/json"
```
