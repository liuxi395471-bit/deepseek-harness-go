# dsh 学习路线图 — 10 个台阶（LEARNING-ROADMAP）

> **本文档面向"刚到岗 / 想从头理解 dsh"的人**。
> 目标：读完 + 跑完 10 个台阶后，能**自信地读懂** `internal/agent/`、`internal/llm/`、`internal/store/`、`internal/server/`、`internal/plugin/`、`internal/mcp/` 等核心包的真实代码。
>
> **不是**：[0-ROADMAP.md](./0-ROADMAP.md)（那是 v4 的演进路线）。
> **不是**：[PLAN.md](./PLAN.md) / [DESIGN*.md](./DESIGN-v2.md)（那是历史/未来设计规格）。
> **是**：把"9000 LoC + 190 用例 + 16 包"拆成 10 个 1-2 小时的小台阶。
>
> 用法：每台阶按"读什么 → 跑什么 → 改什么 → 自检"的顺序推进。**不要跳**——前后有依赖。

## 阅读顺序

- §1 路线图总览
- §2 前置条件
- §3 10 个台阶（每个独立章节）
- §4 自检清单
- §5 进阶方向

---

## §1 路线图总览

```
难度    时长     台阶
─────────────────────────────────────────────────────────────
★☆☆     1h      T01 仓库与构建（go build / go test）
★★☆     1.5h    T02 配置加载链（YAML + env + 默认值）
★★☆     1.5h    T03 LLM 客户端与 wire 协议
★★★     2h      T04 Tool 接口与注册表
★★★     2h      T05 ReAct 循环（agent.Runner）
★★★★     2h      T06 Session 持久化（store）
★★★★     2h      T07 HTTP Server + SSE
★★★★     2.5h    T08 流式 LLM（ChatStream + Recombiner）
★★★★★   2h      T09 gRPC 插件系统
★★★★★   2h      T10 MCP stdio Session
─────────────────────────────────────────────────────────────
合计                       ~18 小时（一人，约 1 周）
```

**节奏建议**：

- 前 5 个台阶**每天 2 个**（约 5h/天）
- 后 5 个台阶**每天 1 个**（深 2-3h/天）
- 每个台阶完成后立即做自检；不通过不要进下个

**学完后能做的事**：

- 口述 ReAct 循环的事件顺序
- 抄写 `Runner.Run` 的伪码
- 指出新功能应该落在哪个包
- 排查 80% 的"为什么这里没动"问题

---

## §2 前置条件

### §2.1 知识

| 必备 | 备注 |
|---|---|
| Go 语法 | 切片、map、channel、goroutine、`context.Context`、interface |
| Go modules | `go.mod` / `go.sum` / `go get` |
| Go testing | `testing.T` / 表驱动 / `httptest.NewServer` |
| HTTP 基础 | status code / SSE 概念 |
| JSON | OpenAI Messages schema 接触过更好 |

### §2.2 环境

| 工具 | 版本 | 验证 |
|---|---|---|
| Go | 1.26.x | `go version` |
| Git | 任意 | `git status` |
| SQLite CLI | 可选 | `sqlite3 --version` |
| protoc | 仅 T09 用 | 见 T09 §3 |

### §2.3 一句话理解 dsh

> dsh 是一个 **ReAct agent runtime**：用户输入 → 调 LLM → LLM 想调工具就调 → 工具结果回填 → LLM 再生成 → 循环直到 LLM 不再调工具。所有东西（HTTP 流式、SQLite 会话、gRPC 插件、MCP 协议）都绕着这个核心循环。

### §2.4 文档地图（先扫一遍）

| 文档 | 用法 |
|---|---|
| [README.md](../README.md) | 入口；快速开始 |
| [DESIGN.md](./DESIGN.md) | v1.2 设计（M0-M4）—— **T01-T05 必读** |
| [DESIGN-v2.md](./DESIGN-v2.md) | v2 设计（M5-M8+）—— **T06-T10 必读** |
| [PLAN.md](./PLAN.md) | v1→v2 实施节奏（与 DESIGN 对照看） |
| [RELEASE-v2.md](./RELEASE-v2.md) | v2.0.2 已交付清单 |
| [docs/examples/greet-session.md](./examples/greet-session.md) | 端到端示例会话（**T05 必看**） |

---

## §3 10 个台阶

### T01 — 仓库与构建

**目标**：能克隆、构建、跑测试、找到代码。

**入口文件**：

- `go.mod`
- `go.sum`
- `cmd/dsh/main.go`

**读什么**：

1. `go.mod` —— 看 module 路径、Go 版本、第三方依赖
2. `cmd/dsh/main.go` 第 1-60 行 —— 只看 `package` / `import` / `var (...)` 的 flag 定义
3. `cmd/dsh/repl/repl.go`（先只看存在性，不深读）

**跑什么**：

```bash
cd d:\Devops\AgentProgram\deepseek-harness-all\deepseek-harness-go
go version                                    # 1.26.x
go build ./...                                # 应该 0 error
go test -count=1 ./internal/config/...        # 应该全绿（约 7 用例）
go test -count=1 ./internal/tool/...           # 应该全绿（约 9 用例）
```

**改什么**：

- 在 `cmd/dsh/main.go` 加一行 `fmt.Println("Hello, learner")`，跑 `./dsh -version` 看输出
- 然后**回滚**这个改动

**自检**：

- [ ] 解释 `go.mod` 中 `// indirect` 的含义
- [ ] 能说出 dsh 是 `command` / `library` / 还是两者？
- [ ] 列出去掉某三方依赖后哪些包会编译失败

**通过标志**：`go build ./...` 0 warning + `go vet ./...` 0 warning

---

### T02 — 配置加载链

**目标**：理解"env > YAML > 默认值"的加载顺序。

**入口文件**：

- `internal/config/config.go`
- `internal/config/config_test.go`
- `harness.example.yml`
- `harness.yml`（本机版本）

**读什么**：

1. `Config` / `LLMConfig` / `AgentConfig` 结构体定义（config.go §开头的 100 行）
2. `Load(path string) (Config, error)` —— 完整读一遍：默认值 → YAML 合并 → env 覆盖
3. 优先级对照表（在 DESIGN.md §2.3 与 PLAN.md §5）

**跑什么**：

```bash
# 看默认
go run ./cmd/llmprobe -tags probe -prompt "hi" 2>&1 | head -5

# 看 env 覆盖
export DEEPSEEK_BASE_URL="http://example.invalid/v1"
export DEEPSEEK_MODEL="test-model"
go run ./cmd/dsh -prompt "x" 2>&1 | head -5
# 应该看到 model=test-model

# 看 YAML 覆盖
cat > /tmp/test.yml <<EOF
llm:
  model: from-yaml
agent:
  debug: true
EOF
go run ./cmd/dsh -config /tmp/test.yml -prompt "x" 2>&1 | head -5
# 应该看到从 YAML 读到 model=from-yaml
```

**改什么**：

- 在 `LLMConfig` 加一个新字段 `Region string \`yaml:"region"\`` + env `DEEPSEEK_REGION`
- 在 `Load` 函数加 env 覆盖
- 写 1 个单测：env 覆盖 YAML 覆盖默认值

**自检**：

- [ ] 说出"修改时只替换 YAML 中出现的字段"是怎么实现的
- [ ] 解释 `*float64` 在 Temperature 字段的作用（与只接受 temp=1 的网关兼容）
- [ ] 找一处 `Validate()` 检查并解释为什么它存在

**通过标志**：3 种加载顺序（默认/YAML/env）的端到端验证可重复

---

### T03 — LLM 客户端与 wire 协议

**目标**：能读写 OpenAI 兼容的 REST 协议；理解 tool_calls 的 JSON 表示。

**入口文件**：

- `internal/llm/types.go`
- `internal/llm/client.go`
- `internal/llm/client_test.go`

**读什么**：

1. `types.go` 全文件 —— 弄清 `Role` / `Message` / `ToolCall` / `ChatRequest` / `ChatResponse` / `Usage`
2. `client.go` 的 `Chat` 方法全文件 —— 看 HTTP 构造、错误处理、JSON 序列化
3. `NewClient(cfg config.LLMConfig)` —— 看 BaseURL 校验
4. `DESIGN.md §3` 完整读一遍

**跑什么**：

```bash
# 1) 工具字段携带校验
go test -count=1 -run TestChat ./internal/llm/... -v

# 2) 401 错误路径
grep -n "StatusUnauthorized" internal/llm/client_test.go

# 3) 流式 stub 路径
go test -count=1 -run TestChatStream ./internal/llm/... -v
```

**改什么**：

- 用 `httptest.NewServer` 写一个 mock：返回固定 `tool_calls=[greet({"name":"x"})]`
- 在 `client_test.go` 加一个 `TestMyMock` 看请求体内容

**自检**：

- [ ] 手画出 `ToolCall` 的 JSON 形状（ID / Type / Function / Name / Arguments）
- [ ] 解释为什么 `Arguments` 是 `string` 而不是 `map[string]any`
- [ ] 解释 `http.NewRequestWithContext` 在 ctx 取消时会发生什么
- [ ] 找出 `client.go` 里"非 2xx → 返回 `*APIError`"的代码位置

**通过标志**：

- 单测中 mock 一个返回 `tool_calls` 的 fixture，能在你的测试里打印出 `req.Messages` 长度

---

### T04 — Tool 接口与注册表

**目标**：理解"工具就是 ctx + JSON args → Result" 的最小契约；写自己的工具。

**入口文件**：

- `internal/tool/tool.go`
- `internal/tool/registry.go`
- `internal/tools/greet.go`
- `internal/tools/fs_read.go`
- `internal/tools/fs_write.go`
- `internal/tools/fs_path.go`

**读什么**：

1. `tool.go` 全文 —— `Result` / `Tool` interface / `Spec(t Tool)` 函数
2. `registry.go` 全文 —— `RWMutex` + `Register/Get/Names/Specs`
3. `greet.go` 全文 —— 最简工具样板
4. `fs_path.go` 全文 —— workspace 路径安全
5. `DESIGN.md §4`

**跑什么**：

```bash
go test -count=1 ./internal/tool/...      # 注册表 + Spec
go test -count=1 ./internal/tools/...     # 4 个内置工具
```

**改什么**（在你自己的电脑上做这个，不提交到主仓库）：

```go
// internal/tools/reverse.go
package tools

import (
    "context"
    "encoding/json"
    "deepseek-harness-go/internal/tool"
)

type Reverse struct{}

func (Reverse) Name() string        { return "reverse" }
func (Reverse) Description() string { return "反转字符串" }
func (Reverse) Parameters() any     { return map[string]any{
    "type": "object",
    "properties": map[string]any{
        "s": map[string]any{"type": "string"},
    },
    "required": []string{"s"},
} }
func (Reverse) Execute(_ context.Context, args json.RawMessage) (tool.Result, error) {
    var p struct{ S string `json:"s"` }
    if err := json.Unmarshal(args, &p); err != nil {
        return tool.Err("invalid args"), nil
    }
    runes := []rune(p.S)
    for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
        runes[i], runes[j] = runes[j], runes[i]
    }
    return tool.Ok(string(runes)), nil
}
```

加注册（main.go）+ 单测 + 用 `-prompt "请用 reverse 工具反转 abc"` 验证

**自检**：

- [ ] 解释 `IsError=true` 与 `Execute` 返回 `error` 的区别
- [ ] 为什么 `Parameters()` 返回 `any` 而不是强类型 struct？
- [ ] 解释 `fs_path.go` 中 `EvalSymlinks` 二次校验防止什么攻击

**通过标志**：

你自己写的 `reverse` 工具在 REPL 中能调起来，且 6 行单测全过（happy + arg missing + arg invalid + 边界 + 并发注册 + Spec JSON 不退化）。

---

### T05 — ReAct 循环（agent.Runner）

**目标**：理解 dsh 的**核心**——一次"LLM → tool → LLM → 工具 → LLM"的事件流。

**入口文件**：

- `internal/agent/event.go`
- `internal/agent/runner.go`
- `internal/agent/system.go`
- `internal/agent/runner_test.go`
- `docs/examples/greet-session.md`

**读什么**：

1. `event.go` 全文 —— 事件类型（`PhaseChange` / `AssistantMessage` / `ToolCallStart` / `ToolResult` / `LoopDone` / `LoopError`）
2. `runner.go` 的 `LoopRunner.loop()` —— **逐行读**（核心算法，约 60 行）
3. `system.go` —— system prompt 组装
4. `DESIGN.md §5.3` 的伪码配合读
5. `runner_test.go` 第一个测试（mock LLM）

**跑什么**：

```bash
go test -count=1 -v -run TestRun_Happy ./internal/agent/...   # 关键单测
go test -count=1 -v -run TestRun_RunnerPanic ./internal/agent/...
go test -count=1 -v -run TestRun_MaxRounds ./internal/agent/...

# 看真实事件流（用 mock LLM）
go test -count=1 -v -run TestRun ./internal/agent/... 2>&1 | head -80
```

**手画**（必做，**不画不算会**）：

在纸上画出"用户输入一句 prompt → 走完 ReAct → 输出"的事件序列（包括 phase 编号、tool_call_start 的字段、tool_result 的字段、loop_done 的字段）。然后对照 `runner_test.go` 里的 `TestRun_Happy` 看实际是否吻合。

**改什么**：

- 在 `runner.go` 的 `loop()` 中加一个 `fmt.Fprintf(os.Stderr, "[trace] round=%d\n", rounds)` 行
- 跑一个测试看 trace
- 然后**回滚**

**自检**：

- [ ] 解释"defer 里 `close(out) → res <- result → close(res)`"的顺序对竞态的影响
- [ ] 解释 `MaxRounds=N` 是"N 次 LLM 调用"还是"N 轮 LLM+tool 循环"？
- [ ] 解释为什么工具 panic 不应逃出 `loop` 协程？
- [ ] 解释 `RunResult.Rounds` 的语义（首版 vs 重构）

**通过标志**：

能在不看代码的情况下复述"为什么工具 `IsError=true` 时要在 message 前加 `[ERROR] ` 前缀"。

---

### T06 — Session 持久化（store）

**目标**：理解 SQLite 怎么存"一次对话"——表结构、并发写入、回放。

**入口文件**：

- `internal/store/store.go`
- `internal/store/mem.go`
- `internal/store/sqlite.go`
- `internal/store/sqlite_test.go`
- `internal/store/export.go`

**读什么**：

1. `store.go` —— `Session` struct / `Store` interface / `ErrNotFound` 定义
2. `mem.go` —— in-memory 实现（最容易懂先读）
3. `sqlite.go` 的表 schema（DDL 部分） + `Begin` / `Append` / `Load`
4. `export.go` —— JSON 序列化/反序列化
5. `DESIGN-v2.md §A.3`

**跑什么**：

```bash
go test -count=1 ./internal/store/...        # 22 个用例
go test -count=1 -v -run TestSQLite ./internal/store/... 2>&1 | head -40

# 看真表结构
DSH_SESSION_DB=/tmp/learn.db go run ./cmd/dsh -prompt "x"
sqlite3 ~/.dsh/sessions.db ".schema"
sqlite3 ~/.dsh/sessions.db "SELECT id, rounds, length(messages_json) FROM sessions LIMIT 5;"
```

**改什么**：

- 写一个 `TestMyStoreAppend`：用 SQLiteStore，连续 100 次 Append 同 session_id，读出来检查 Messages 顺序与数量

**自检**：

- [ ] 解释 SQLite WAL + busy_timeout 怎么做到的并发写安全
- [ ] 解释 `Session.Preview` 在哪里被设置、用于什么场景
- [ ] 解释 `UpdateUsage(ctx, sid, delta)` 为什么不直接接受总和
- [ ] 说出 `Export` 的 JSON 顶层字段（id/created_at/rounds/...）

**通过标志**：

- 你的测试用例通过
- 能口述"Append 时写 messages_json 列，是 JSON 数组，整个 Session 序列化一次"

---

### T07 — HTTP Server + SSE

**目标**：理解 net/http 路由 + Bearer 鉴权 + 流式写 SSE。

**入口文件**：

- `internal/server/server.go`
- `internal/server/util.go`
- `internal/server/server_test.go`

**读什么**：

1. `server.go` 的 `Server` struct + `Handler()` + 每个路由 handler
2. `util.go` 的 Bearer 鉴权中间件
3. SSE 响应部分：`Content-Type: text/event-stream`、`writer.Flush()`
4. `DESIGN-v2.md §A.2`

**跑什么**：

```bash
# 1) 起服务（独立 token）
export DSH_SERVER_ENABLED=true
export DSH_SERVER_AUTH_TOKEN=learn
./dsh -serve &
SERVER_PID=$!

# 2) 健康检查（无 token → 401）
curl -i http://127.0.0.1:8080/healthz                    # 200 with auth
curl -i http://127.0.0.1:8080/api/sessions               # 401
curl -i -H "Authorization: Bearer learn" http://127.0.0.1:8080/api/sessions  # 200 []

# 3) 列表 / 取详情
curl -H "Authorization: Bearer learn" http://127.0.0.1:8080/api/sessions | python -m json.tool

# 4) 流式
curl -N -H "Authorization: Bearer learn" "http://127.0.0.1:8080/api/agent/stream?prompt=hi"

# 5) 优雅关停
kill -TERM $SERVER_PID
wait $SERVER_PID
```

**改什么**：

- 加一个 `/api/info` 路由：返回 `{version, uptime_seconds, session_count}`
- 不需要鉴权；写 2 个单测：401 / 200 内容含 uptime

**自检**：

- [ ] 解释为什么 `Authorization: Bearer <token>` 是简单但有效的鉴权
- [ ] 解释 SSE 与"chunked HTTP"的关系
- [ ] 解释 `http.Flusher` 在 SSE 中何时被调
- [ ] 解释"优雅关停"如何避免半截响应

**通过标志**：

- 你的 `/api/info` 路由可以独立测试
- 你能手动跑上述 5 步 curl 命令并得到预期输出

---

### T08 — 流式 LLM（ChatStream + Recombiner）

**目标**：理解从 SSE chunk 到 `AssistantDelta` 事件的重组过程。

**入口文件**：

- `internal/stream/stream.go`
- `internal/stream/parser.go`
- `internal/stream/recombiner.go`
- `internal/llm/client.go` 里的 `ChatStream`（OpenAI 部分）

**读什么**：

1. `stream.go` —— 顶层 `StreamChunk` 类型
2. `parser.go` —— `data: {...}\n\n` 解析
3. `recombiner.go` —— 把同一 index 的多个 chunk 累积成完整 delta
4. `DESIGN-v2.md §A.1`

**跑什么**：

```bash
go test -count=1 -v ./internal/stream/...   # 5 用例

# 看真实流（用 REPL 实时打字）
./dsh -prompt "write a haiku about go" -debug 2>&1 | head -30
```

**改什么**：

- 写一个工具函数 `CollectDelta(events <-chan agent.Event) string`：把所有 `AssistantDelta{Text}` 串起来
- 跑一个单测，验证输出 == 完整 assistant message

**自检**：

- [ ] 解释为什么把 SSE chunk 直接转给 LLM 是不行的
- [ ] 解释 `Recombiner` 为什么要按 index 分组
- [ ] 解释"delta"与"完整 message"的语义区别
- [ ] 说出 `DESIGN-v2.md §A.1` 的 8 个测试用例名

**通过标志**：

- 你能口述"客户端收到 `data: {...}\n\n` → parser 解析为 1 个 StreamChunk → Recombiner 按 index 累积 → 输出 AssistantDelta{Text}"的整条链路

---

### T09 — gRPC 插件系统

**目标**：理解"用户写一个独立二进制，dsh 启动它，调它的工具"的机制。

**入口文件**：

- `internal/plugin/plugin.go`（Host 管理）
- `internal/plugin/client.go`（连到插件）
- `internal/plugin/proto/tool_provider.proto`（接口定义）
- `cmd/plugin-echo/main.go`（示范插件）
- `internal/plugin/plugin_client_test.go`

**读什么**：

1. `plugin.go` —— `Host{Start, Stop, AllSpecs, Execute}`；子进程管理 + auto-restart
2. `proto/tool_provider.proto` —— service ToolProvider { rpc Specs / rpc Execute }
3. `cmd/plugin-echo/main.go` —— 服务端实现（约 50 行）
4. `DESIGN-v2.md §B` —— 整体设计

**跑什么**：

```bash
go build -o plugin-echo ./cmd/plugin-echo/
./dsh -plugin ./plugin-echo.exe -prompt "echo 三次 hello"
# 应该看到 "echoed" 工具被调

# 看 spec
./dsh -plugin ./plugin-echo.exe -debug < /dev/null 2>&1 | grep -i "spec\|echo"

go test -count=1 -v ./internal/plugin/...
```

**改什么**：

- 写一个 `cmd/plugin-reverse/`（用前面 T04 写的 `reverse` 工具）
- 启动 dsh 加载它，REPL 中调 `reverse`

**自检**：

- [ ] 解释 gRPC 与 HTTP 相比在"插件进程内通讯"场景下的优缺点
- [ ] 解释 auto-restart 的退避策略（看 plugin.go 的代码）
- [ ] 解释 `internal/plugin/proto/` 下的 `.pb.go` 与 `.proto` 的关系（git 生成步骤）
- [ ] 解释 `--plugin` flag 怎么支持 repeatable

**通过标志**：

你的 `plugin-reverse` 二进制被 dsh 加载，且 `reverse({"s":"abc"})` 工具在 REPL 中可用。

---

### T10 — MCP stdio Session

**目标**：理解 dsh 怎么与外部 MCP server 进程对话。

**入口文件**：

- `internal/mcp/mcp.go`（JSON-RPC 底层）
- `internal/mcp/session.go`（高层 Session）
- `internal/mcp/session_test.go`

**读什么**：

1. `mcp.go` —— `Server` struct + request/response 派发 + rpcError
2. `session.go` —— `NewSession` / `ListTools` / `CallTool` / `ListResources` / `ListPrompts`
3. `DESIGN-v2.md §D.1` —— MCP 协议要点
4. JSON-RPC 2.0 规范（外部资料）

**跑什么**：

```bash
go test -count=1 -v ./internal/mcp/...   # 8 用例（含 in-process fake）

# 看 JSON-RPC 帧
grep -rn "jsonrpc" internal/mcp/ | head -10
```

**改什么**（可选，挑战题）：

- 用 mcp.Session 写一个测试：构造一个 fake MCP server（返回硬编码 tools/list 响应），验证 `ListTools` 正确解析

**自检**：

- [ ] 解释 JSON-RPC 2.0 的核心字段（jsonrpc / id / method / params / result / error）
- [ ] 解释 MCP 协议中 `notifications/initialized` 的作用
- [ ] 解释 `errors.As` 与 `errors.Is` 在 mcp.jsonError 处的用途
- [ ] 解释"method not found"为什么 ListResources/Prompts 选择吞错而不是冒泡

**通过标志**：

你能口述"一个 JSON-RPC 调用从用户代码 → session.go → mcp.go → 标准库 stdin pipe → 外部 MCP server → 响应路径"的完整往返。

---

## §4 自检清单

学完所有台阶后，对照此清单：

### §4.1 知识

- [ ] 能手画"REPL prompt → REPL → Runner → LLM → Tool → 结果"的完整数据流（不看书）
- [ ] 能指出"加一个新 LLM 字段"应该改哪些文件
- [ ] 能区分 `cfg.Provider=openai` 与 `cfg.Provider=anthropic` 在 dsh 内的不同代码路径
- [ ] 能解释"为什么 dsh 的工具 panic 不会让进程崩溃"
- [ ] 能解释"为什么 HTTP server 必须用 SSE 而不是 WebSocket"

### §4.2 动手

- [ ] 在不查文档的情况下，从 0 写一个 `cmd/dsh/mini-runner/main.go`，能完成 1 轮 LLM + 1 个工具调用（用 mock）
- [ ] 能用 `sqlite3` 命令行查 `~/.dsh/sessions.db` 的内容
- [ ] 能用 `protoc --go_out=. --go-grpc_out=. internal/plugin/proto/*.proto` 重生成 proto 代码

### §4.3 实验

- [ ] 在不同 `harness.yml` 配置下启动 dsh，看到 system prompt 文案不同
- [ ] 用 SIGINT（Ctrl+C）打断一个长会话，看 ctx 取消的传播
- [ ] 把 LLM 端点改成不存在的地址，看错误如何呈现

---

## §5 进阶方向

完成后，想深入可以选：

| 方向 | 入口 |
|---|---|
| **Session 事件溯源** | [0-ROADMAP.md](./0-ROADMAP.md) P1 + `internal/store/` |
| **Gateway SSE 收敛** | [0-ROADMAP.md](./0-ROADMAP.md) P2 + `internal/server/` |
| **OTel 可观测性** | [0-ROADMAP.md](./0-ROADMAP.md) P3 + 新建 `internal/obs/otel.go` |
| **Compaction 上下文压缩** | [0-ROADMAP.md](./0-ROADMAP.md) P4 + 新建 `internal/compaction/` |
| **Sub-agent** | [0-ROADMAP.md](./0-ROADMAP.md) P5 + 新建 `internal/subagent/` |
| **审计日志** | [0-ROADMAP.md](./0-ROADMAP.md) P3 + 新建 `internal/audit/` |
| **Ollama/Gemini 协议** | [0-ROADMAP.md](./0-ROADMAP.md) P4 + `internal/llm/` |
| **Web UI / Desktop** | [0-ROADMAP.md](./0-ROADMAP.md) v5+ 路线 |

---

## 附录 A：常见入门陷阱

| 陷阱 | 解释 | 避免方法 |
|---|---|---|
| 一上来就读 `runner.go` 全文件 | 该文件 800 行，新人会迷失 | T01-T04 先做 T05 才到 runner.go |
| 把 `cfg := applyDefaults()` 与 `unmarshalYAML` 顺序搞反 | 设计上是"先默认后 YAML" | T02 验证三种加载顺序 |
| 觉得 `Result.IsError=true` 是错误冒泡 | 设计是"回填给 LLM 让它自纠" | T04 自检题 |
| 误以为 SSE 需要 HTTP/2 | SSE 在 HTTP/1.1 chunked 上也能跑 | T07 自检题 |
| 把事件当成"必须严格顺序" | channel 是按发送顺序读的，所以是 | T05 手画时体会 |

---

## 附录 B：推荐的辅助资料

| 资料 | 备注 |
|---|---|
| `../deepseek-harness/`（Node 官方版） | 对照看架构选型差异 |
| `../deepseek-harness-java/`（Java DDD 版） | 对照看端口-适配器思路 |
| Go by Example (gobyexample.com) | 速查 channel / interface / context |
| OpenAI Chat Completions API 文档 | tool_calls schema 来源 |
| JSON-RPC 2.0 规范 | MCP / LSP 都基于此 |

---

## 附录 C：每台阶的速查表

| 台阶 | 入口文件数 | 新增代码量 | 单测新增 |
|---|---|---|---|
| T01 | 4 | 0（只读 + 一行回滚） | 0 |
| T02 | 4 | +5（Region 字段） | +1 |
| T03 | 3 | +30（mock httptest） | +1 |
| T04 | 6 | +50（reverse 工具） | +6 |
| T05 | 5 | 0（trace 后回滚） | 0 |
| T06 | 5 | +50（Append 100 次测试） | +1 |
| T07 | 3 | +30（/api/info 路由） | +2 |
| T08 | 4 | +30（CollectDelta） | +1 |
| T09 | 5 | +100（plugin-reverse） | 0 |
| T10 | 3 | +60（mock MCP server 测试） | +1 |
| **合计** | | **~355 行新代码** | **+13 用例** |

---

**版本**：LEARNING-ROADMAP v0.1（2026-09-22 起）
**配套文档**：[0-ROADMAP.md](./0-ROADMAP.md)（v4 演进路线） / [DESIGN-v2.md](./DESIGN-v2.md)（v2 设计规格）
