# v7 设计与范围：生态与协议互通

**版本：** v7.0.0
**状态：** ✅ 已完成
**代号：** Ecosystem & Protocol Interoperability

---

## 1. 目标

让 ds-go 成为可被外部世界直接消费的 agent runtime：

| 维度 | v6 | v7 |
|---|---|---|
| 协议互通 | 单进程 / stdio MCP | HTTP+SSE MCP, ACP, A2A |
| 第三方集成 | 无 | sdk-go（独立 Go 模块） |
| 插件生态 | 仅 JAR | JAR + Node Bridge + native |
| IDE 对接 | 仅 CLI / REPL | ACP 接入任意 IDE |
| 多 agent | 单 agent | AgentTeam / 能力路由 / 共享 KV |
| 工具深度 | 自写工具 | 自写 + 外部 LSP 工具 |

## 2. 子阶段

```
P7-5 SDK           [P0]  sdk-go 模块（types / HTTP / JSON-RPC / echo demo）
P7-1 Plugin install[P2]  installer 包（Scanner / StatusStore / Reconciler）
P7-2 Node Bridge   [P1]  plugin/bridge/node 包（JSON-RPC over stdio）
P7-3 MCP transport [P1]  mcp.HTTPTransport / SSETransport / ServerHandler
                                  + server/mcp_bridge.go (MCPDispatcher 把 JSON-RPC
                                  桥到 Gateway router)
P7-4 ACP Server    [P0]  acp 包（session.create / send / cancel / permission）
P7-6 AgentTeam A2A [P1]  a2a 包（Registry / Orchestrator）
P7-7 LSP Tools     [P2]  lsp 包（hover / references / definition）
P7-8 Intent        [P2]  agent/intent 包（9 类正则）
```

## 3. 设计约束

- **无三方依赖**：所有 transport 用 stdlib（net/http, encoding/json, bufio, io）。
- **协议兼容**：
  - HTTP / SSE 帧结构与 v4 Gateway SSE 兼容；
  - JSON-RPC 2.0 over stdio / WebSocket；
  - LSP standard framed 协议；
  - ACP 接口对齐 dsh-java。
- **安全**：Node Bridge 入口必须位于 installRoot 下；ACP HTTP 鉴权 token 由 v5 credentials 注入。

## 4. 文件清单

### 新增目录
```
sdk-go/                              # 独立 Go module
  go.mod
  README.md
  types/                             # 共享 types
  client/http/                       # HTTP + SSE client
  client/jsonrpc/                    # JSON-RPC 2.0 client
  examples/echo/                     # 最小 demo
deepseek-harness-go/internal/
  acp/                               # Agent Communication Protocol
  a2a/                               # AgentTeam 编排
  lsp/                               # LSP client
  mcp/transport.go                   # MCP HTTP+SSE transport（扩展）
  plugin/installer/                  # 插件安装工程化
  plugin/bridge/node/                # Node Bridge 插件宿主
  agent/intent/                      # 9 类意图分类
```

## 5. 测试

详见 `TEST-CASES.md`。共 21 个 v7 测试用例，覆盖：

- SDK：`TC-v7-0001 ~ TC-v7-0003`
- Installer：`TC-v7-0004 ~ TC-v7-0007`
- Node Bridge：`TC-v7-0008 ~ TC-v7-0011`
- MCP transport：`TC-v7-0012 ~ TC-v7-0013`
- ACP：`TC-v7-0014 ~ TC-v7-0017`
- A2A：`TC-v7-0018 ~ TC-v7-0019`
- LSP：`TC-v7-0020 ~ TC-v7-0021`（实际为 5+）
- Intent：内置测试

## 6. 风险与缓解

| 风险 | 缓解 |
|---|---|
| Node Bridge 在 Windows 上有 stdio 路径问题 | 测试跳过（无 node / 无 roundtrip），生产路径由 debug_bridge 工具验证 |
| 三方依赖膨胀 | 全程 stdlib，仅 sdk-go 引入 `encoding/json` 等标准库 |
| ACP session 与 v6 Task 1:1 映射可能限制并发 | v7.1 改为 1:N（session 持有 task 列表），现阶段够用 |
| LSP server 重启成本高 | Client 设计为单进程进程内复用；批量 hover 才值得起 |

## 7. Wiring：把协议层挂到 `dsh -serve`

子阶段独立单元测试都过了，但 v7.0 收尾还缺**把它们真正串到 Gateway HTTP 上**的一步。commit `1979012` 完成：

```
dsh -serve
├─ /healthz                        无鉴权
├─ /api/gateway/stream             v4 Gateway（Bearer 鉴权）
├─ /api/agent/message / stream     v4 旧端点（Bearer）
├─ /api/sessions[/...]             v4 旧端点（Bearer）
├─ /acp/*                          acp.Server.Handler()（独立鉴权）
│   ├─ /acp/session/create
│   ├─ /acp/session/send
│   ├─ /acp/session/cancel
│   ├─ /acp/session/list
│   └─ /acp/permission/decide
└─ /mcp (POST) + /mcp/sse          mcp.SSEServerHandler（独立鉴权）
                                   ↑ 由 server.MCPDispatcher(router) 提供
                                     JSON-RPC 桥到 Gateway source

adapter:
  task.Executor ──acp.TaskExecutorAdapter──> acp.TaskSubmitter
  Gateway source  ──server.MCPDispatcher──> JSON-RPC result/error
```

### 关键决策

- **ACP / MCP 路由不挂 Bearer 鉴权**：这些协议自带鉴权语义，强行统一会让外部 IDE / MCP client 用不起来。`apiMux` 只包 Bearer 鉴权，顶层 mux 把 ACP/MCP 排前面。
- **MCP 方法名映射**：`tools/list`（MCP 写法）↔ `tools.list`（Gateway 已注册 source）。`mcpMethodToGatewaySource` 处理 slash / dot 两种写法。
- **同步分发**：`Router.DispatchSync` 把 channel-based source handler 同步化成 JSON-RPC 响应，避免在 HTTP 路径上引入 SSE。

## 8. 后续 v7.1 候选

| 方向 | 内容 |
|---|---|
| ACP WebSocket 推送 | 当前只有 HTTP 控制面；ACP-WS 是 v7.1 |
| Node Bridge 真 roundtrip CI | Windows 跳过；v7.1 加 Linux runner |
| LSP 工具注册到 Runner | 当前 LSP 仍是 client 库；v7.1 注册为 `lsp_hover` / `lsp_references` 工具 |
| Intent → system prompt 路由 | 当前 Classify 只是返回类；v7.1 注入 LoopRunner |
