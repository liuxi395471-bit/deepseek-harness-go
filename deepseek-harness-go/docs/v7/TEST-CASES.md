# v7 测试用例（TC-v7-xxxx）

| 编号 | 子阶段 | 用例 | 期望 |
|---|---|---|---|
| TC-v7-0001 | SDK types | Frame / ToolCall / PermissionDecision 序列化 | JSON roundtrip 一致 |
| TC-v7-0002 | SDK http | SendSession + auth header | 收到 Authorization |
| TC-v7-0003 | SDK http | SubscribeEvents SSE 流 | 解析出 3 帧 |
| TC-v7-0004 | SDK jsonrpc | Call roundtrip | 服务端收到 1 次 |
| TC-v7-0005 | SDK jsonrpc | Notify | 服务端收到 notify |
| TC-v7-0006 | SDK jsonrpc | Server push notification | Push channel 收到 |
| TC-v7-0007 | Installer | Scan 3 种格式 + 跳过隐藏 | 3 个 entry |
| TC-v7-0008 | Installer | StatusStore roundtrip | 重启后状态保留 |
| TC-v7-0009 | Installer | Reconcile drift + orphan + healthy | 三类正确分组 |
| TC-v7-0010 | Node Bridge | Load 拒绝越界 pkgDir | 返回 "escapes" 错误 |
| TC-v7-0011 | Node Bridge | Load 不存在 pkg | 返回错误 |
| TC-v7-0012 | MCP http | POST /mcp 路由 | 服务端收到 JSON-RPC |
| TC-v7-0013 | MCP sse | POST /mcp/sse | 响应正常 |
| TC-v7-0014 | ACP server | Create + Send + List | 1 session / 1 task |
| TC-v7-0015 | ACP server | Cancel | state=canceled |
| TC-v7-0016 | ACP http | Bearer token 鉴权 | unauthorized 401 |
| TC-v7-0017 | ACP http | session not found | 返回 500 + 错误信息 |
| TC-v7-0018 | A2A registry | Register / Unregister / Duplicate | 注册失败 → error |
| TC-v7-0019 | A2A orchestrator | Run + shared state 跨步骤 | 各步骤 Updates 持久化 |
| TC-v7-0020 | LSP frame | readFrame 标准帧 | 解析正确 |
| TC-v7-0021 | LSP client | Initialize + Hover | Hover contents = "fake-hover" |
| TC-v7-0022 | LSP client | References | 返回 1 个 location |
| TC-v7-0023 | Intent | 9 类全覆盖 | 全部正确归类 |
| TC-v7-0024 | Intent | Empty input | 兜底 chat |
| TC-v7-0025 | Intent | First-hit wins | CodeFix 优先于 CodeRefactor |
| TC-v7-0026 | Server-ACP | /acp/session/create 挂载 | 返回 session_id |
| TC-v7-0027 | Server-ACP | Bearer 不阻 ACP（独立鉴权） | 200 |
| TC-v7-0028 | Server-MCP | POST /mcp tools/list 桥接到 Gateway | 返回 tools 数组 |
| TC-v7-0029 | Server-MCP | unknown method | JSON-RPC -32601 |
| TC-v7-0030 | Router | DispatchSync | 同步收集事件 |

## 执行

```bash
go test -count=1 -timeout=120s ./...
```

测试结果（v7.0.0）：

```
ok  	deepseek-harness-go/cmd/dsh/repl	0.814s
ok  	deepseek-harness-go/internal/a2a	1.131s
ok  	deepseek-harness-go/internal/acp	1.219s
ok  	deepseek-harness-go/internal/agent	0.470s
ok  	deepseek-harness-go/internal/agent/intent	0.910s
ok  	deepseek-harness-go/internal/approval	0.935s
ok  	deepseek-harness-go/internal/audit	0.920s
ok  	deepseek-harness-go/internal/compaction	0.901s
ok  	deepseek-harness-go/internal/config	0.991s
ok  	deepseek-harness-go/internal/credentials	0.894s
ok  	deepseek-harness-go/internal/e2e	0.987s
ok  	deepseek-harness-go/internal/goal	0.975s
ok  	deepseek-harness-go/internal/hook	0.894s
ok  	deepseek-harness-go/internal/jobs	0.976s
ok  	deepseek-harness-go/internal/llm	3.164s
ok  	deepseek-harness-go/internal/llm/anthropic	2.799s
ok  	deepseek-harness-go/internal/llm/gemini	2.569s
ok  	deepseek-harness-go/internal/llm/ollama	2.255s
ok  	deepseek-harness-go/internal/llm/provider	2.677s
ok  	deepseek-harness-go/internal/lsp	0.833s
ok  	deepseek-harness-go/internal/mcp	2.639s
ok  	deepseek-harness-go/internal/obs	10.838s
ok  	deepseek-harness-go/internal/plugin	1.647s
ok  	deepseek-harness-go/internal/plugin/bridge/node	1.051s
ok  	deepseek-harness-go/internal/plugin/installer	0.915s
ok  	deepseek-harness-go/internal/runtime	1.024s
ok  	deepseek-harness-go/internal/sandbox	0.998s
ok  	deepseek-harness-go/internal/server	0.585s
ok  	deepseek-harness-go/internal/skill	0.971s
ok  	deepseek-harness-go/internal/storage	0.832s
ok  	deepseek-harness-go/internal/store	2.753s
ok  	deepseek-harness-go/internal/stream	1.399s
ok  	deepseek-harness-go/internal/subagent	0.904s
ok  	deepseek-harness-go/internal/task	0.987s
ok  	deepseek-harness-go/internal/terminal	3.336s
ok  	deepseek-harness-go/internal/tool	1.219s
ok  	deepseek-harness-go/internal/tools	1.419s
ok  	deepseek-harness-go/internal/usage	1.162s
ok  	deepseek-harness-go/internal/workflow	0.707s

# sdk-go 模块
ok  	deepseek-harness-all/sdk-go/client/http	1.401s
ok  	deepseek-harness-all/sdk-go/client/jsonrpc	0.686s
ok  	deepseek-harness-all/sdk-go/types	0.668s
```

所有测试通过 ✅。

## 已知限制

- Node Bridge 的真 roundtrip 测试在 Windows 跳过（依赖机器装 Node；CI 用 Linux 跑覆盖）。
- ACP WS 事件流留待 v7.1（HTTP 路径已实现）。
