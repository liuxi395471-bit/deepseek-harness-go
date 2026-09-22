# DeepSeek Harness Go 版（`dsh`）

参照 `deepseek-harness-java/` 的核心抽象用 Go 实现的 **ReAct agent harness**。当前对应 [PLAN.md](./docs/PLAN.md)（v2 收官）与 [PLAN-v3.md](./docs/PLAN-v3.md)（v3 实施中）。

**当前版本**：v2.0.2（2026-09-16）—— v2 全部 TODO 完成，含 plugin gRPC、Anthropic native SSE、MCP stdio Session。
**下一版**：v3.0.0 —— Skill loader + Sub-agent + Obs OTel + Compaction + Audit + Sandbox + Ollama/Gemini。

---

## 特性

- **ReAct 循环**：`LLM → tool_calls → tool result → LLM`，单 turn 内最多 N 轮（默认 16）。
- **流式 LLM**：`ChatStream` + `AssistantDelta` 事件，REPL 实时打字输出。
  - OpenAI 兼容 SSE ✅（`internal/stream/`）
  - Anthropic Messages native SSE ✅（`internal/llm/anthropic/stream.go`，v2.0.1+）
- **HTTP+SSE 服务**：`/api/agent/message`（阻塞）+ `/api/agent/stream`（SSE）+ `/api/sessions` + `/api/sessions/{id}` + `/healthz`。
- **Session 持久化**：`MapStore`（内存）+ `SQLiteStore`（默认 `~/.dsh/sessions.db`，pure-Go `modernc.org/sqlite`）；export/import/branch。
- **Approval + Shell**：4 种 Approver（Noop/AllowList/Terminal/HTTP）；shell 工具受 allowlist + workspace pathguard + symlink fail-closed。
- **gRPC 插件系统**（v2.0.1+）：`./dsh -plugin ./plugin-echo.exe`；proto 在 `internal/plugin/proto/`；in-tree `cmd/plugin-echo/` 示例。
- **Anthropic 多渠道**（v2.0.1+）：`provider: anthropic` 即用；Chat + ChatStream 都支持。
- **MCP stdio**（v2.0.2+）：`internal/mcp.Session` 高层 API；Initialize + tools/list + tools/call；in-process fake round-trip 通过 5 用例。
- **Usage 聚合**：`/usage` 打印累计 token + 可选 cost。
- **REPL 子命令**：`/help /clear /model /stream /usage /sessions /history /resume /export /import`。
- **4 个内置工具**：`greet` / `fs_read` / `fs_write` / `shell`（shell 默认关闭，需 `tools.shell.enabled=true`）。
- **取消传播**：`os.Interrupt` / `SIGTERM` → `signal.NotifyContext` → 整条链路（`Runner.Run` / `Client.Chat` / `Tool.Execute`）。
- **错误不冒泡**：工具失败以 `[ERROR]` 角色消息回填给模型。
- **线程安全**：`sync.RWMutex` 工具注册表；SQLite WAL + busy_timeout 并发写。
- **三方依赖**：`gopkg.in/yaml.v3`（配置）+ `modernc.org/sqlite`（pure-Go SQLite）+ `google.golang.org/grpc` + `google.golang.org/protobuf`。

---

## 快速开始

### 1. 准备配置

复制示例配置：

```bash
cp harness.example.yml harness.yml
```

编辑 `harness.yml`，把 `<your-base-url>` 替换成你的 OpenAI 兼容端点（如 `http://127.0.0.1:8777/v1`），把 `<your-model>` 替换成模型名。

设置 API Key（**不要写进 yaml**）：

```bash
export DEEPSEEK_API_KEY="sk-..."
```

### 2. 构建并启动

```bash
go build ./cmd/dsh
./dsh -config ./harness.yml
```

启动后看到：

```text
dsh 0.1.0-dev — workspace=/path/to/.dsh/workspace model=glm-5.3-flash
dsh> ready. type your prompt, 'exit' or Ctrl+D to quit.
dsh>
```

### 3. 跑通第一个工具调用

```text
dsh> 请使用 greet 工具向 Ada 打招呼
  → tool greet({"name":"Ada"})
  ← greet (ok, 312µs)
你好，Ada！我是 dsh。
dsh>
```

### 4. 文件读写

```text
dsh> 请把"明天放假"写到 workspace/notes/holiday.txt
  → tool fs_write({"path":"notes/holiday.txt","content":"明天放假"})
  ← fs_write (ok, 1.2ms)
wrote 12 bytes to notes/holiday.txt

dsh> 读 workspace/notes/holiday.txt
  → tool fs_read({"path":"notes/holiday.txt"})
  ← fs_read (ok, 215µs)
明天放假
```

### 5. 调试模式

加 `--debug` 打印所有事件到 stderr：

```bash
./dsh -config ./harness.yml -debug
```

### 6. 一次性命令模式

```bash
./dsh -config ./harness.yml -prompt "请使用 greet 工具向 Ada 打招呼"
```

### 7. 取消当前轮

按 `Ctrl+C` 取消**当前轮**（不退出 REPL）；按 `Ctrl+D` 干净退出。

---

## 命令行 flag

| flag | 默认 | 说明 |
| --- | --- | --- |
| `-config` | `harness.yml` | YAML 配置文件路径；不存在则走默认值 + env |
| `-prompt` | `""` | 非空时一次性执行并退出（不走 REPL） |
| `-serve` | `false` | 启动 HTTP+SSE 服务（DESIGN-v2 §A.2）；需要 `DSH_SERVER_AUTH_TOKEN` |
| `-plugin` | `""` | 插件可执行文件路径（DESIGN-v2 §B.2，v2.0.1+）；可重复；spawn 子进程 + gRPC 接入 |
| `-debug` | `false` | 打印所有 agent 事件到 stderr |
| `-version` | `false` | 打印版本并退出 |

---

## 配置项

完整配置项见 [harness.example.yml](./harness.example.yml)。加载优先级：**env > YAML > 默认值**。

| 段 | key | env | 默认 |
| --- | --- | --- | --- |
| `llm` | `base-url` | `DEEPSEEK_BASE_URL` | `http://127.0.0.1:8777/v1` |
| `llm` | `api-key` | `DEEPSEEK_API_KEY` | `""` |
| `llm` | `model` | `DEEPSEEK_DEFAULT_MODEL` | `glm-5.3-flash` |
| `llm` | `max-tokens` | `DEEPSEEK_MAX_TOKENS` | `8192` |
| `llm` | `timeout` | `DEEPSEEK_TIMEOUT` | `120s` |
| `agent` | `max-rounds` | `DSH_AGENT_MAX_ROUNDS` | `16` |
| `agent` | `workspace` | `DSH_AGENT_WORKSPACE` | `./.dsh/workspace` |
| `agent` | `temperature` | `DSH_AGENT_TEMPERATURE` | `0.2` |
| `agent` | `system-prompt` | `DSH_AGENT_SYSTEM_PROMPT` | `""`（内置中文模板） |
| `agent` | `debug` | `DSH_AGENT_DEBUG` | `false` |

---

## 调试探针

```bash
go build -tags probe -o llmprobe ./cmd/llmprobe
./llmprobe -prompt "hello"
```

仅在 `//go:build probe` 标签下编译，不会污染主二进制。

---

## 测试

```bash
go test ./...                  # 单元测试
go test ./... -v               # 详细输出
# -race 需要 cgo，本机如未启用 CGO_ENABLED=1，可改用：
go test ./... -count=1
```

测试覆盖（v2.0.2）：

| 包 | 用例数 | 覆盖点 |
| --- | --- | --- |
| `internal/config` | 7 | YAML/env 优先级、默认值、Validate 边界 |
| `internal/llm` | 11 | tools 字段携带、401、ctx 取消、流式 stub、scheme 校验 |
| `internal/llm/anthropic` | 6 | Chat + ChatStream native SSE + error event + Feed parser |
| `internal/tool` | 9 | 注册/重名/并发读、Specs 防御性拷贝 |
| `internal/tools` | 14 | greet/fs_read/fs_write/shell + 路径穿越 + symlink escape |
| `internal/agent` | 17 | ReAct 完整路径 + RunStream + Store 集成 + system prompt |
| `internal/mcp` | 8 | JSON-RPC + Session (Initialize/ListTools/CallTool/Concurrent) |
| `internal/plugin` | 9 | Host + gRPC Client + Server + echo + approval gating |
| `internal/store` | 22 | Map + SQLite + 归档 + 注入 + export/import |
| `internal/server` | 6 | 路由 + 401 + 409 + 断流 + panic-safe |
| `internal/stream` | 5 | SSE 解析 + Recombiner + ToolCallAccum |
| `internal/usage` | 6 | Tracker + cost 命中/miss + 并发 |
| `internal/approval` | 15 | 4 Approver + session scope + HTTPPoll |
| `internal/e2e` | — | REPL + HTTP + session 端到端 |
| `cmd/dsh/repl` | 5 | RunOnce + 错误 footer + max-rounds + canceled + debug |

**总计 ~190 用例（含 subtests），`go test ./... -count=1` 全绿，`go vet` 0 warning**。

---

## 目录结构

```
deepseek-harness-go/
├── README.md              # 本文件
├── docs/
│   ├── DESIGN-v2.md         # v2 设计（M5–M8+；详细规格；已冻结）
│   ├── DESIGN-v3.md         # v3 设计（Skill / Sub-agent / Obs / Compaction / Audit / Sandbox / 多渠道）
│   ├── DESIGN.md            # v1.2 设计（冻结）
│   ├── PLAN.md              # v2 实施规划（已收官）
│   ├── PLAN-v3.md           # v3 实施规划
│   ├── RELEASE-v2.md        # v2.0.2 release note
│   └── examples/
│       └── greet-session.md   # 第一份端到端示例会话
├── go.mod / go.sum
├── harness.example.yml    # 配置示例
├── cmd/
│   ├── dsh/               # 主 CLI（v2.0.2：+--plugin +--serve）
│   │   ├── main.go
│   │   └── repl/          # REPL 包
│   ├── plugin-echo/       # gRPC 示范插件（DESIGN-v2 §B.5）
│   └── llmprobe/          # 调试探针（build tag: probe）
├── tools/                 # 一键安装 protoc / protoc-gen-go / protoc-gen-go-grpc
├── internal/
│   ├── config/            # 配置加载（YAML + env + default）
│   ├── llm/               # OpenAI 兼容 + anthropic（M8）
│   │   └── anthropic/          ── 含 ChatStream native SSE（v2.0.1）
│   ├── tool/              # Tool 接口 + 注册表
│   ├── tools/             # 内置：greet / fs_read / fs_write / shell
│   ├── agent/             # ReAct Runner + StreamingRunner
│   ├── approval/          # 4 Approver + CacheChain + HTTPPoll（DESIGN-v2 §C.2）
│   ├── stream/            # SSE 解析 + Recombiner + ToolCallAccum
│   ├── store/             # Session 持久化（Map + SQLite + export/import/branch）
│   ├── usage/             # Token 聚合 + cost（DESIGN-v2 §A.4）
│   ├── server/            # HTTP + SSE server（DESIGN-v2 §A.2）
│   ├── plugin/            # gRPC plugin host + client + server + proto（v2.0.1）
│   │   └── proto/              ── tool_provider.proto + 生成的 .pb.go
│   └── mcp/               # MCP stdio JSON-RPC + Session（v2.0.2）
```

---

## 当前边界（v2.0.2）

- ReAct 循环、Session 持久化、HTTP+SSE、Approval+Shell、Usage 全部 ✅
- 插件系统 ✅（gRPC + proto + 示范 echo 插件 + `--plugin` CLI）
- 多渠道 LLM：OpenAI 兼容 ✅、Anthropic native SSE ✅；Ollama/Gemini 待 v3
- MCP stdio Session ✅（Initialize + tools/list + tools/call + ListResources/Prompts）
- CLI REPL + HTTP 双前端；无 Web UI
- 无 OS 沙箱（仅用户态 allowlist + pathguard）；Windows ACL / Linux ns 待 v3
- 无 Skill loader / Sub-agent / Obs (OTel) / Compaction / Audit；均待 v3
- 无 Plan mode / Persona / Schedule；v4+

详见 [RELEASE-v2.md](./docs/RELEASE-v2.md)。

---

## 路线图

**v2 已完成**：参见 [RELEASE-v2.md](./docs/RELEASE-v2.md) 与 [PLAN.md §7.1](./docs/PLAN.md#71-v2-实施-todo按-design-v2-g-节奏展开)。

**v3 实施中**（[PLAN-v3.md](./docs/PLAN-v3.md) + [DESIGN-v3.md](./docs/DESIGN-v3.md)）：

- **§A Skill loader**（T2）：Markdown + YAML frontmatter；mention 触发
- **§B Sub-agent sync**（T3）：`agent_spawn` 工具；同步子 LoopRunner
- **§C Obs OTel**（T4）：`internal/obs/` OTel SDK；opt-in
- **§D Compaction**（T5/T6）：`truncate` + `llm-summary` 两策略
- **§E Audit log**（T7）：JSONL；redact 默认
- **§F Sandbox**（T8）：Windows ACL + Linux namespaces
- **§G 多渠道**（T9/T10/T11）：Ollama + Gemini + provider 路由

**v4+**：Plan mode / Persona / Schedule / Web UI / Desktop。

---

## License

内部实现，与同仓库 `deepseek-harness`（MIT）和 `deepseek-harness-java` 保持各自 license。
