# DeepSeek Harness Go（dsh）— 测试与验收

> 适用版本：`develop` 分支当前实现（v3.0.0-dev）。
> 本文记录**当前真实**的测试规模、覆盖分布与验收方式。

---

## §1. 跑测试

```bash
cd deepseek-harness-go

go build ./...          # 编译
go vet ./...           # 静态检查（当前 0 warning）
go test ./... -count=1 # 全量单测 + 集成测试
go test ./... -cover   # 带覆盖率
```

`-count=1` 用来绕过测试缓存，确保拿到真实结果。

**关于 `-race`**：需要 CGO。本机若未启用 `CGO_ENABLED=1`，用 `-count=1` 代替；
启用 CGO 后建议再跑一轮 `go test ./... -race -count=1`。

---

## §2. 当前测试规模

**285** 个顶层 `Test` 函数，分布在 **24** 个包中（`go test ./... -count=1` 全绿）。

| 包 | 用例数 | 覆盖率 | 覆盖点 |
|---|---|---|---|
| `internal/agent` | 33 | 81.9% | ReAct 完整路径、流式、Store 集成、skill 注入、压缩触发、审计写入、panic 兜底 |
| `internal/store` | 29 | 79.2% | Map + SQLite 双后端、并发写、导出/导入、id 保留与重分配 |
| `internal/llm` | 22 | 89.7% | tools 字段携带、401、ctx 取消、流式 stub、scheme 校验、429 重试 |
| `internal/tools` | 21 | 76.3% | greet / fs_read / fs_write / shell、路径穿越、符号链接逃逸、allowlist、输出截断 |
| `internal/approval` | 15 | 82.4% | 4 种 Approver、会话级缓存、链式审批、HTTPPoll 超时 |
| `internal/skill` | 12 | 94.5% | frontmatter 解析、mention 匹配、always-on、注入格式、缺字段报错 |
| `internal/sandbox` | 12 | 67.8% | provider 路由、敏感路径拒绝、受限令牌构造、平台不匹配报错 |
| `internal/compaction` | 10 | 87.8% | 触发阈值、truncate 窗口、llm-summary、system 保留、tool 不拆散 |
| `internal/plugin` | 9 | 64.4% | Host 生命周期、gRPC 客户端、崩溃重启、审批关口 |
| `internal/tool` | 9 | 87.8% | 注册/重名/并发读、Specs 防御性拷贝、ToolSpec 转换 |
| `internal/mcp` | 8 | 59.7% | JSON-RPC 帧、Session 握手、tools/list、tools/call、method-not-found 降级 |
| `internal/stream` | 8 | 78.6% | SSE 解析、delta 重组、tool_call 片段合并、幂等 Final |
| `internal/usage` | 6 | 88.9% | Tracker 并发累加、价格命中/未命中、格式化 |
| `internal/audit` | 6 | 90.0% | JSONL 行格式、脱敏模式、hash 确定性、并发写入 |
| `internal/obs` | 6 | 81.1% | no-op 兜底、OTel tracer/meter 构造、属性转换 |
| `internal/config` | 8 | 56.9% | YAML/env 优先级、默认值、Validate 各类边界 |
| `internal/subagent` | 9 | 93.5% | 同步 spawn、嵌套禁止、panic/超时折叠、结果回填 |
| `internal/llm/provider` | — | 95.2% | provider 路由、非法 provider 报错、URL 校验 |
| `internal/llm/anthropic` | — | 80.3% | Chat + 原生 SSE + error event + Feed 解析 |
| `internal/llm/ollama` | — | 82.6% | ndjson fixture round-trip、tool_calls 映射、token 计数 |
| `internal/llm/gemini` | — | 85.8% | SSE fixture delta 累积、finish、functionCall 映射 |
| `internal/server` | 7 | 51.5% | 路由、401、409、非法 JSON、SSE 断流、panic 安全 |
| `cmd/dsh/repl` | — | 73.9% | RunOnce、错误 footer、max-rounds、canceled、debug 输出、斜杠命令 |
| `internal/e2e` | 3 | — | REPL + HTTP + session 端到端冒烟 |

**整体语句覆盖率：71.9%**（`go tool cover -func`）。

### 覆盖率偏低的包是有意为之

- `internal/server`（51.5%）、`internal/mcp`（59.7%）、`internal/config`（56.9%）：
  剩余部分主要是错误分支与边界组合，逐条穷举的收益低于维护成本。
- `internal/plugin`（64.4%）：需要真实的插件子进程才能覆盖完整的崩溃重启路径。
- `internal/sandbox`（67.8%）：Windows ACL 与 Linux namespace 的实际生效路径依赖宿主权限，
  CI 中只做交叉编译校验。

---

## §3. 端到端冒烟

`internal/e2e/e2e_test.go` 是权威冒烟测试，用假 LLM 客户端跑通完整链路：

```text
用户 prompt → ReAct 循环 → 工具调用 → 工具结果 → 最终答案
```

同时覆盖 REPL 斜杠命令与 HTTP 服务端的基本路径。

### 手工冒烟（需要真实端点）

`docs/examples/greet-session.md` 记录了一次完整的 `greet` 会话。它验证：

1. 配置能加载。
2. LLM 端点可达且支持工具调用。
3. 工具注册表与 system prompt 组合正确（模型能看到 `greet`）。
4. ReAct 循环至少跑一轮。
5. 事件通道能到达 REPL。
6. 最终内容打印到 stdout。
7. `exit` 能干净退出。

失败排查顺序见该文档末尾。

---

## §4. 验收矩阵

按能力逐条列出**当前实现状态**与对应的验证手段。

### 4.1 核心循环

| # | 验收项 | 状态 | 验证 |
|---|---|---|---|
| A1 | ReAct 循环：LLM → tool_calls → 结果 → LLM | ✅ | `internal/agent` |
| A2 | 单 turn 轮次上限可配 | ✅ | `agent.max-rounds`，Validate 限制 [1,64] |
| A3 | 工具失败以 `[ERROR]` 回填给模型，不终止循环 | ✅ | `internal/agent` |
| A4 | 工具 panic 不终止进程 | ✅ | `safeExecute` 兜底 |
| A5 | 所有事件先于 `RunResult` 投递 | ✅ | `internal/agent`（含 panic 分支） |
| A6 | 停止原因四态齐全 | ✅ | `no_tool_calls` / `max_rounds` / `error` / `canceled` |
| A7 | 取消链贯通到 LLM 与工具 | ✅ | `internal/agent`、`internal/llm` |

### 4.2 LLM 多渠道

| # | 验收项 | 状态 | 验证 |
|---|---|---|---|
| B1 | OpenAI 兼容（默认） | ✅ | `internal/llm` |
| B2 | Anthropic Messages + 原生 SSE | ✅ | `internal/llm/anthropic` |
| B3 | Ollama ndjson 流 | ✅ | `internal/llm/ollama` |
| B4 | Gemini generateContent SSE | ✅ | `internal/llm/gemini` |
| B5 | provider 路由 | ✅ | `internal/llm/provider`（95.2%） |
| B6 | 非法 provider 启动即报错并列出支持列表 | ✅ | `config.Validate` + `provider.NewClient` |
| B7 | 非流式 `Chat` 遇 `Stream=true` 拒绝发请求 | ✅ | `ErrStreamNotSupported` |

### 4.3 工具与安全

| # | 验收项 | 状态 | 验证 |
|---|---|---|---|
| C1 | Registry 线程安全 + 重名拒绝 | ✅ | `internal/tool` |
| C2 | 工具清单顺序稳定（LLM 输入可复现） | ✅ | `internal/tool` |
| C3 | fs 工具路径围栏（拒绝逃逸） | ✅ | `internal/tools` |
| C4 | 符号链接逃逸 fail-closed | ✅ | `internal/tools` |
| C5 | shell allowlist：空列表 = 拒绝一切 | ✅ | `internal/tools` |
| C6 | shell 输出截断 + 截断标记 | ✅ | `internal/tools` |
| C7 | 4 种 Approver + 会话级缓存 + 链式 | ✅ | `internal/approval` |
| C8 | Windows ACL 受限令牌 + 敏感路径拒绝 | ✅ | `internal/sandbox`（win-only） |
| C9 | Linux namespaces + 敏感路径拒绝 | ✅ | `internal/sandbox`（linux-only） |
| C10 | 沙箱失败不静默降级 | ✅ | `internal/tools`（shell 装配处） |

### 4.4 v3 新增能力

| # | 验收项 | 状态 | 验证 |
|---|---|---|---|
| D1 | Skill 加载（frontmatter 解析） | ✅ | `internal/skill` |
| D2 | mention 触发 + always-on | ✅ | `internal/skill` |
| D3 | 命中 body 注入 system prompt | ✅ | `internal/skill`、`internal/agent` |
| D4 | skill 目录不存在不报错、缺字段报错 | ✅ | `internal/skill` |
| D5 | `agent_spawn` 同步子 agent | ✅ | `internal/subagent`（93.5%） |
| D6 | 禁止嵌套（max_depth=1） | ✅ | 注册表剔除 + ctx 深度双重保险 |
| D7 | 子 agent panic/超时折叠为 IsError | ✅ | `internal/subagent` |
| D8 | obs 接口族 + no-op 零开销 | ✅ | `internal/obs` |
| D9 | OTel opt-in（OTLP gRPC） | ✅ | `internal/obs` |
| D10 | 三个 span 接入点 | ✅ | `agent.run` / `llm.chat` / `tool.execute` |
| D11 | compaction 阈值触发 | ✅ | `internal/compaction` |
| D12 | truncate / llm-summary 两策略 | ✅ | `internal/compaction` |
| D13 | system 永远保留、tool 不拆散 | ✅ | `internal/compaction` |
| D14 | 审计 JSONL 四类事件 | ✅ | `internal/audit` |
| D15 | 默认脱敏（只记 hash） | ✅ | `internal/audit` |
| D16 | `--audit` 优先于配置 | ✅ | `cmd/dsh/main.go` |

### 4.5 服务端与插件

| # | 验收项 | 状态 | 验证 |
|---|---|---|---|
| E1 | 5 个端点路由 | ✅ | `internal/server` |
| E2 | Bearer 鉴权（常量时间比较） | ✅ | `internal/server` |
| E3 | 同会话并发返回 409 | ✅ | `internal/server` |
| E4 | SSE 流式输出 | ✅ | `internal/server` |
| E5 | gRPC 插件：启动 + 注册工具 | ✅ | `internal/plugin` |
| E6 | 插件崩溃自动重启（上限内） | ✅ | `internal/plugin` |
| E7 | 插件工具经审批关口 | ✅ | `internal/plugin` |
| E8 | MCP Session 握手 + tools/list + tools/call | ✅ | `internal/mcp` |
| E9 | MCP resources/prompts 缺失时降级 | ✅ | `internal/mcp` |

### 4.6 会话与配置

| # | 验收项 | 状态 | 验证 |
|---|---|---|---|
| F1 | 会话持久化双后端 | ✅ | `internal/store` |
| F2 | 会话导出/导入（含 id 保留与重分配） | ✅ | `internal/store/export.go` |
| F3 | 配置优先级：默认 → YAML → env | ✅ | `internal/config` |
| F4 | 非法配置启动即失败并列出全部问题 | ✅ | `config.Validate` |
| F5 | 示例配置可直接加载 | ✅ | `harness.example.yml` |
| F6 | REPL 斜杠命令 | ✅ | `cmd/dsh/repl` |

---

## §5. 手工验收清单

需要真实 LLM 端点时按此顺序走：

```bash
# 1. 配置
cp harness.example.yml harness.yml
# 编辑 harness.yml：填 base-url / model
export DEEPSEEK_API_KEY="sk-..."

# 2. 构建
go build ./cmd/dsh

# 3. 冒烟（最简路径）
./dsh -config ./harness.yml -prompt "请使用 greet 工具向 Ada 打招呼"

# 4. REPL + 调试输出
./dsh -config ./harness.yml -debug
```

在 REPL 里逐项确认：

| # | 操作 | 期望 |
|---|---|---|
| G1 | `/help` | 列出全部斜杠命令 |
| G2 | `/skills` | 列出已加载 skill（或提示目录为空） |
| G3 | `/usage` | 打印 token 用量 |
| G4 | `/sessions` | 列出会话 |
| G5 | `/model <name>` | 切换运行时模型 |
| G6 | `/stream` | 切换流式开关 |
| G7 | `/clear` | 清屏 |
| G8 | `Ctrl+C` | 取消当前轮，REPL 继续运行 |
| G9 | `Ctrl+D` | 干净退出 |

### v3 专项手工验收

| # | 场景 | 期望 |
|---|---|---|
| H1 | 在 `skills.dir` 放一个含 `trigger` 的 skill，用该关键词提问 | 该 skill 的 body 出现在 system prompt 里 |
| H2 | 提问触发 `agent_spawn` | 父回复中含子 agent 的最终答案 |
| H3 | `--audit audit.jsonl` 后跑一次工具调用 | JSONL 中有 `tool_call` 行，且只有 `args_hash` 无 `args_raw` |
| H4 | `audit.full=true` | JSONL 中出现 `args_raw` 原文 |
| H5 | `compaction.enabled=true` + `trigger-tokens` 调小 | 长对话中触发压缩，事件流出现 `Compacted` |
| H6 | `obs.provider=otel` + 本地 collector | 看到 `agent.run` / `llm.chat` / `tool.execute` span |
| H7 | `sandbox.provider=windows_acl` | 访问敏感路径被拒 |
| H8 | `shell.enabled=true` + allowlist | 白名单外命令被拒；白名单内命令执行 |

---

## §6. 覆盖率目标与现状

| 指标 | 目标 | 现状 |
|---|---|---|
| 全量测试通过 | 必须 | ✅ 24 包全绿 |
| `go vet` | 0 warning | ✅ 0 warning |
| 整体覆盖率 | ≥ 70% | ✅ 71.9% |
| 新增模块覆盖率 | ≥ 80% | ✅ skill 94.5%、subagent 93.5%、provider 95.2%、audit 90.0%、compaction 87.8%、obs 81.1% |

---

**相关文档**：[README.md](../README.md)（快速开始）· [ARCHITECTURE.md](./ARCHITECTURE.md)（架构设计）
