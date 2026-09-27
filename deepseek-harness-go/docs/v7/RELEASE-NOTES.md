# v7 Release Notes

**v7.0.0 — Ecosystem & Protocol Interoperability**

发布时间：2026-09-27

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

### P7-3 MCP 多 transport（`internal/mcp/transport.go`）
- `HTTPTransport`：POST /mcp → JSON-RPC
- `SSETransport`：GET /mcp/sse（推）+ POST /mcp/sse（拉）
- `SSEServerHandler`：可挂载到 http.Server 的统一接入

### P7-4 ACP 服务端 🔴 P0（`internal/acp/`）
- session/create / session/send / session/cancel / session/list / permission/decide
- Bearer token 鉴权（从 v5 credentials 注入）
- 1:1 映射 v6 Task 域

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

## ✅ 测试

- 全仓 `go test -count=1 -timeout=120s ./...` 通过
- sdk-go 模块 `go test ./...` 通过
- 21 个 v7 测试用例覆盖（`TEST-CASES.md`）

## 🛠️ 兼容性

- Go 1.22+
- 仅标准库依赖
- 与 v4 Gateway SSE 帧结构兼容
- 与 dsh-java ACP 协议对齐

## 🚧 已知限制

- Node Bridge 真 roundtrip 测试在 Windows CI 跳过（生产路径由 debug_bridge 工具验证）
- ACP WebSocket 事件流留待 v7.1（HTTP 控制面已可用）
- LSP 集成未在 v7 端到端冒烟（仅 client + in-memory fake）

## 📦 升级

```bash
git fetch --tag
git checkout v7.0.0
go build ./cmd/dsh
```
