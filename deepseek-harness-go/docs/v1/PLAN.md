# DeepSeek Harness Go 版 — 实施规划

> 目标：参照仓库内 `deepseek-harness-java/` 的核心架构，用 Go 实现一个**最小可运行**的 agent harness。
> 原则：**先跑通核心闭环，再谈扩展**。每一阶段都有可执行、可验收的产物。

**架构基调**：以 **Java 版的 `Runner + Registry + Tool` 三件套** 为蓝本；**不引入**上游 TS 的 cordis scope/插件沙箱思路（首版破坏了"最小可运行"原则），也**不引入** Java DDD 的端口-适配器分层（首版工程量过重）。

**目标平台**：开发机为 Windows Go 1.26.4；产出二进制需要 `GOOS=windows` + `GOOS=linux` 双平台验证；REPL 在 Windows Terminal / PowerShell 与 bash 下都能跑。

---

## 0. 文档与术语

| 术语 | 含义 |
| --- | --- |
| **harness** | 本项目产物，对应 Java 版 `deepseek-harness-java` 与上游 TS `dsh`，是一个"让 LLM + 工具跑起来"的运行时外壳 |
| **ReAct 循环** | Reason → Act → Observe 反复迭代：`LLM → tool_calls → tool result → LLM` |
| **内置工具** | Go 代码里直接实现的工具（`tools.FsRead` 等），与"插件"对立 |
| **端到端闭环** | 用户键入一行 prompt → harness 调用 LLM → LLM 调工具 → 结果回填 → LLM 输出最终答复 → REPL 打印 → 等待下一轮 |

---

## 1. 目标与范围

### 1.1 目标
实现一个 CLI 交互式的 agent harness：用户输入 prompt，harness 调用 LLM，LLM 需要时触发内置工具，工具结果回填后继续，直到产出最终答复。

### 1.2 最小可运行 = 什么（首版交付物）
- **交付形态**：CLI 交互式（终端 REPL），无 HTTP、无 Web UI。
- **核心闭环**：`LLM 调用 → 工具调用 → 结果回填 → 再调用`（ReAct 循环，单轮最大 16 步）。
- **工具系统**：3 个内置工具（`greet` / `fs_read` / `fs_write`），无插件系统。
- **错误模型**：工具失败作为 `tool` 角色内容回填（多数模型能据此自我纠正），不冒泡让 REPL 退出。
- **取消模型**：`os.Interrupt` 通过 `signal.NotifyContext` 贯穿 `Runner` → `LLM` → `Tool.Execute`。
- **可观测性**：`--debug` 打印 ReAct 中间事件（assistant delta / tool_calls / tool_result），详见 M4。

### 1.3 明确排除（首版不做，留作后续里程碑）
| 能力 | Java 版对应 | Go 后续里程碑 |
|------|------------|---------------|
| Web UI / HTTP API | `IAgentApi` + static | M5 |
| 插件系统 | `JAVA_NATIVE` / `DSH_NODE_BRIDGE` | M6 |
| Session 持久化 | `PersistingSessionLog` / MySQL | M7 |
| 流式响应 (SSE) | `InMemoryLlmRuntimePort` 流式生成 | M5 |
| Approval / 沙箱 / MCP / Skill | `approval` / `sandbox` / `mcp` / `skill` | M7+ |
| Agent 子进程（codex/claude-code） | `subagent` | M7+ |

排除理由：均为"外围能力"，首版只要保证**ReAct 闭环可观察、可中断、可测试**。

---

## 2. Java 版核心抽象 → Go 映射（修正版）

> 对照表说明：以 Java 版的**端口视角**对齐（参考 `deepseek-harness-java/README.md` §4.6 与 §5.1），而非具体类名。Go 版**不照搬** Java 的包结构，只搬运"语义单元"。

| Java 语义 | Go 首版对应 | 备注 |
| --- | --- | --- |
| `ReactLoopAgent`（单 turn ReAct 驱动） | `agent.Runner`（接口）+ `agent.LoopRunner`（默认实现） | 接口可被换：M5 引入 HTTP 流式时给 `agent.StreamRunner` |
| `ToolRegistry` / `IToolCatalogPort` | `tool.Registry` | 线程安全（首版 `sync.RWMutex`）+ 名字索引 |
| `AbstractTool`（`name` / `description` / `parameters` / `run`） | `tool.Tool` 接口 | 见 §B-1 统一签名 |
| `ToolExecutionResult{ ok/fail }` | `tool.Result{ Content string, IsError bool }` | 让工具失败也能回填消息而非冒泡 |
| `FsReadTool` / `FsWriteTool` | `tools.FsRead` / `tools.FsWrite` | 路径被锁在 `workspace.root` |
| （`plugins/sample-tools-plugin` 中的 `greet` 类工具） | `tools.Greet` | 参考 Java 插件命名风格 |
| `IModelExecutionPort` / `DeepSeekAdapter` | `llm.Client`（接口）+ `llm.OpenAICompatibleClient` | OpenAI 兼容 REST；预留 `Stream bool` 字段 |
| `harness.llm.deepseek.*` + env | `config.Config`（YAML + env 覆盖） | 见 §5 优先级 |
| `harness.agent.*`（cwd / workspace / temperature） | `config.Agent` 子结构 | workspace 根目录参数化 |
| `Application.main` | `cmd/dsh/main.go` | 入口：装载配置 → 构建 registry → 启动 REPL |
| `POST /api/agent/message` | `cmd/dsh/repl.go`（`bufio.Scanner` + `signal.NotifyContext`） | 单一来源的交互入口 |

---

## 3. 技术选型（依赖最小化）

| 项 | 选择 | 理由 |
|----|------|------|
| Go 版本 | `go1.26`（本机已装 1.26.4） | 环境已就绪；启用 `slices` / `maps` 标准库、`iter.Seq2` |
| HTTP 客户端 | 标准库 `net/http` | LLM 是 OpenAI 兼容 REST，无需三方库 |
| JSON | 标准库 `encoding/json` | tool calling 参数/结果均为 JSON |
| CLI 交互 | 标准库 `bufio` + `flag` | 最小 REPL，不引入 cobra |
| 流式迭代 | 标准库 `iter` | 预留流式响应接口（M5 用） |
| 测试 | 标准库 `testing` + `net/http/httptest` | 零三方依赖 |
| 配置解析 | `gopkg.in/yaml.v3`（**唯一**三方依赖） | 用于解析 `harness.yml` |
| Module 名 | `deepseek-harness-go`（本地 module） | 非发布，后续可改 |
| 代理 | `goproxy.cn`（已配置） | 拉取 yaml.v3 无障碍 |
| 构建工具 | 不引入 Taskfile / Makefile | 用 `go test ./...` / `go run ./cmd/dsh` 直跑即可；CI 由调用方定 |

> 目标：除 `yaml.v3` 外零第三方依赖。后续里程碑再按需引入（如 sqlite 持久化、cobra 等）。

---

## 4. 目标目录结构

```
deepseek-harness-go/
├── README.md               # 用法说明
├── docs/
│   ├── DESIGN-v2.md        # v2 设计（M5–M8+；详细规格，独立维护）
│   ├── DESIGN.md           # v1.2 设计（冻结；含旧 §11 路线图）
│   ├── PLAN.md             # 本文档（实施规划）
│   └── examples/
│       └── greet-session.md # 第一份端到端示例会话记录
├── go.mod                  # module deepseek-harness-go
├── go.sum
├── harness.example.yml     # 配置示例（默认值见 config.go）
├── cmd/
│   ├── dsh/
│   │   ├── main.go         # 入口：加载配置 → 构建 registry → 启动 REPL
│   │   └── repl/           # REPL 包（repl.go + capture_test.go + repl_test.go）
│   └── llmprobe/
│       └── main.go         # 调试探针：直连 base-url 发一次请求；build tag: probe
├── internal/
│   ├── config/
│   │   ├── config.go       # Config / LLM / Agent 结构 + YAML 加载 + env 覆盖 + 默认值
│   │   └── config_test.go
│   ├── llm/
│   │   ├── types.go        # Role / Message / ToolCall / ToolSpec / ChatRequest / ChatResponse / StreamChunk
│   │   ├── client.go       # Client 接口 + OpenAICompatibleClient 实现（含 httptest 友好的 RoundTripper 注入）
│   │   └── client_test.go
│   ├── tool/
│   │   ├── tool.go         # Tool 接口 + Result 类型 + Schema() 辅助
│   │   ├── registry.go     # 内存注册表（线程安全，name → Tool）
│   │   └── registry_test.go
│   ├── tools/
│   │   ├── greet.go        # 内置工具：greet(name)
│   │   ├── fs_read.go      # 内置工具：fs_read(path)
│   │   ├── fs_write.go     # 内置工具：fs_write(path, content)，强制 workspace.root 内
│   │   └── fs_path.go      # path_guard：路径清洗 + workspace 边界校验（fs_read/fs_write 共享）
│   ├── agent/
│   │   ├── event.go        # 事件类型：AssistantDelta / ToolCallStart / ToolResult / PhaseChange / Error
│   │   ├── runner.go       # Runner 接口 + LoopRunner 实现（ReAct 循环 + ctx 取消 + event 通道）
│   │   ├── system.go       # 系统提示词组装：固定中文模板 + 工具目录拼接
│   │   ├── system_test.go
│   │   └── runner_test.go
│   └── harness/
│       └── harness.go      # 顶层装配：从 config + registry 构造 Runner，并发安全
```

---

## 5. 分阶段执行步骤

> 每阶段都满足：(a) `go build ./...` 通过；(b) `go test ./...` 通过；(c) 写明"可演示的最小命令"。

### M0 — 项目骨架与配置

**做什么**：
1. `go mod init deepseek-harness-go`，添加唯一三方依赖 `gopkg.in/yaml.v3`。
2. 建目录骨架（§4）。
3. `internal/config`：
   - 类型 `Config{ LLM LLMConfig; Agent AgentConfig }`；`LLMConfig{ BaseURL, APIKey, Model, MaxTokens, Timeout }`；`AgentConfig{ MaxRounds, WorkspaceRoot, SystemPrompt, Debug, Temperature }`。
   - `Load(path string) (Config, error)`：先填默认值 → 文件存在则合并 YAML → env 覆盖。
   - env 名：`DEEPSEEK_BASE_URL` / `DEEPSEEK_API_KEY` / `DEEPSEEK_DEFAULT_MODEL` / `DEEPSEEK_MAX_TOKENS` / `DSH_AGENT_MAX_ROUNDS` / `DSH_AGENT_WORKSPACE`。
   - 默认值：`BaseURL=http://127.0.0.1:8777/v1`、`Model=glm-5.3-flash`、`MaxTokens=8192`、`MaxRounds=16`、`WorkspaceRoot=./.dsh/workspace`。
4. 写 `harness.example.yml`（用占位符 `<your-model>`，**不直接写入真实 API Key**）。
5. 写 README 骨架（参见 §6 验收）。

**配置示例（用户实际写）**：
```yaml
llm:
  base-url: <your-base-url>           # env: DEEPSEEK_BASE_URL
  api-key: ""                         # env: DEEPSEEK_API_KEY（绝不进仓库！）
  model: <your-model>                 # env: DEEPSEEK_DEFAULT_MODEL
  max-tokens: 8192
agent:
  max-rounds: 16                      # 对应 Java ReactLoopAgent 单 turn ≈ 50 步的收紧值
  workspace: ./.dsh/workspace         # 默认值，启动时自动 MkdirAll
  temperature: 0.2
  system-prompt: |                     # 可选；缺省走 internal/agent/system.go 的内置模板
    你是 dsh，一个简洁可靠的 CLI 助手。
  debug: false
```

加载优先级：**env > YAML > 默认值**。

**验收**：
- `go build ./...` 通过；`go test ./internal/config/...` 通过。
- 单测覆盖：(a) 全 YAML 加载；(b) 全 env 覆盖；(c) YAML + env 混合，env 胜出；(d) 缺失字段回落默认值；(e) YAML 字段非法（如 `max-rounds: -1`）应报错而非静默。
- `go run ./cmd/dsh -h`（M4 才接，先留 stub）。

---

### M1 — LLM 客户端

**做什么**：
1. `internal/llm/types.go`：
   ```go
   type Role string
   const (RoleSystem Role = "system"; RoleUser = "user"; RoleAssistant = "assistant"; RoleTool = "tool")

   type ToolCall struct { ID string `json:"id"`; Type string `json:"type"` // "function"
       Function ToolCallFunc `json:"function"` }
   type ToolCallFunc struct { Name string `json:"name"`; Arguments string `json:"arguments"` /* JSON 串 */ }

   type Message struct { Role Role `json:"role"`
       Content    string     `json:"content,omitempty"`
       ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
       ToolCallID string     `json:"tool_call_id,omitempty"` /* tool 角色专用 */ }

   type ToolSpec struct { Type string `json:"type"` /* "function" */
       Function ToolSpecFunc `json:"function"` }
   type ToolSpecFunc struct { Name string `json:"name"`; Description string `json:"description"`
       Parameters any `json:"parameters"` /* JSON Schema 片段（object）*/ }

   type ChatRequest struct { Model string `json:"model"`
       Messages    []Message  `json:"messages"`
       Tools       []ToolSpec `json:"tools,omitempty"`
       ToolChoice  any        `json:"tool_choice,omitempty"`
       Temperature *float64   `json:"temperature,omitempty"`
       MaxTokens   int        `json:"max_tokens,omitempty"`
       Stream      bool       `json:"stream,omitempty"` /* 首版恒为 false，但字段保留 */ }

   type ChatResponse struct {
       ID      string   `json:"id"`
       Choices []Choice `json:"choices"`
       Usage   *Usage   `json:"usage,omitempty"`
   }
   type Choice struct { Index int `json:"index"`
       FinishReason string  `json:"finish_reason"`
       Message      Message `json:"message"` }
   type Usage struct { PromptTokens int `json:"prompt_tokens"`; CompletionTokens int `json:"completion_tokens"`; TotalTokens int `json:"total_tokens"` }

   /* 流式预留（M5 用）：StreamChunk = data: {...}\n\n 解析后的一条增量 */
   type StreamChunk struct { Choices []struct{ Delta Message `json:"delta"`; FinishReason string `json:"finish_reason"` } `json:"choices"` }
   ```
2. `internal/llm/client.go`：
   ```go
   type Client interface {
       Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
       /* M5 启用：ChatStream(ctx, req) (iter.Seq2[StreamChunk, error]) */
   }

   type OpenAICompatibleClient struct {
       BaseURL   string
       APIKey    string
       HTTPClient *http.Client /* 允许测试注入 RoundTripper */
   }

   func (c *OpenAICompatibleClient) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) { ... }
   ```
   - 端点：`POST {BaseURL}/chat/completions`。
   - 头：`Authorization: Bearer {APIKey}`、`Content-Type: application/json`。
   - 错误：HTTP 非 2xx → 返回 `*APIError`（含 `Status` / `Body`），让 Runner 可以分类处理（401/429/5xx 首版都不做重试，**直接返回给 REPL 提示用户**）。
3. `internal/llm/client_test.go`：用 `httptest.NewServer` + 自定义 `http.RoundTripper` 验证：
   - 请求体正确携带 `tools` 字段且每个 tool 都是 `function` + `parameters`；
   - `tool_calls` 解析（嵌套 JSON `arguments` 当字符串原样保留，**不解析**为 map）；
   - 401 路径：返回 `*APIError`；
   - ctx 取消路径：请求中途 cancel 时 `Chat` 立刻返回 `ctx.Err()`。

**辅助产物**：`cmd/llmprobe/main.go`（`//go:build probe`），直连 `BaseURL` 发一次 `ChatRequest{Messages:[system,user]}`，打印响应 JSON，作为联调时手动验证工具。

**验收**：
- `go test ./internal/llm/...` 通过。
- `go run -tags probe ./cmd/llmprobe` 能连到真实 base-url（人工执行，本仓验收不强制）。

---

### M2 — 工具系统

**做什么**：
1. `internal/tool/tool.go`：
   ```go
   type Result struct {
       Content string
       IsError bool          /* 失败也回填给模型，让 LLM 自纠 */
   }

   type Tool interface {
       Name() string
       Description() string
       Parameters() any      /* JSON Schema object，序列化为 ToolSpec.Function.Parameters */
       Execute(ctx context.Context, args json.RawMessage) (Result, error)
       /* error 仅在"工具自身 panic 恢复后"或"参数完全无法解析"时才返回——少见 */
   }

   func Spec(t Tool) llm.ToolSpec { /* name/description/parameters 打包 */ }
   ```
2. `internal/tool/registry.go`：线程安全的 `Registry{mu sync.RWMutex; tools map[string]Tool}`，提供 `Register(t Tool) error`（同名返回 `ErrDuplicate`）、`Get(name string) (Tool, bool)`、`Names() []string`、`Specs() []llm.ToolSpec`。
3. `internal/tools/greet.go`：参数 `{name: string}`，中文返回 `"你好，<name>！"`。
4. `internal/tools/fs_path.go`：导出 `resolveInWorkspace(root, p string) (string, error)`，用 `filepath.Clean` + 解析后必须以 `rootAbs` 前缀开头（Windows 大小写不敏感也照样比）。
5. `internal/tools/fs_read.go`：参数 `{path: string}`（相对 workspace 或绝对），读全文，超大文件（默认 > 1 MiB）返回截断 + 标记。
6. `internal/tools/fs_write.go`：参数 `{path: string, content: string}`，强制 `resolveInWorkspace`，自动 `MkdirAll(filepath.Dir)`，拒绝软链逃逸（`os.Lstat` 后再用真实路径再 resolve 一次）。

**验收（单测全覆盖）**：
- `registry_test`：注册、重复报错、按名查找、并发安全（`-race`）。
- `greet_test`：基本调用 + 参数缺失 → 返回 `Result{IsError:true}` 不冒泡。
- `fs_read_test`：正常读 + 不存在路径 → IsError + 错误消息含原文。
- `fs_write_test`（**必须覆盖四个用例**）：
  - (a) 在 workspace 内正常写文件；
  - (b) `../etc/passwd` 路径穿越被拒；
  - (c) workspace 外的绝对路径被拒；
  - (d) workspace 不存在时 `MkdirAll` 成功。

---

### M3 — Agent 循环（核心）

**做什么**：
1. `internal/agent/event.go`：
   ```go
   type Event interface{ isEvent() }
   type PhaseChange struct{ Phase Phase; At time.Time }
   type Phase int
   const (PhaseInit Phase = iota; PhaseLLMCall; PhaseToolExec; PhaseLLMDone; PhaseStopped; PhaseError)

   type AssistantDelta struct{ Text string }
   type AssistantMessage struct{ Content string; ToolCalls []llm.ToolCall }

   type ToolCallStart struct{ Call llm.ToolCall }
   type ToolResult struct{ CallID string; Name string; Content string; IsError bool; Took time.Duration }

   type LoopDone struct{ Rounds int; Messages []llm.Message }
   type LoopError struct{ Err error; Phase Phase }

   type Error struct{ Err error }
   ```
2. `internal/agent/system.go`：
   - 内置中文模板："你是 dsh 的内置助手，可以调用以下工具：…（拼接 `Registry.Specs()` 的 name + description）"
   - 用户可在 `AgentConfig.SystemPrompt` 覆盖；为空时用内置模板。
3. `internal/agent/runner.go`：
   ```go
   type Runner interface {
       Run(ctx context.Context, prompt string) (<-chan Event, <-chan error)
       /* REPL 等收尾主结果：<-chan RunResult{} 含 final Messages 与 Rounds 计数 */
   }

   type LoopRunner struct {
       Client   llm.Client
       Registry *tool.Registry
       System   SystemPromptBuilder
       MaxRounds int
   }

   type RunResult struct {
       FinalMessages []llm.Message
       Rounds        int
       StopReason    string /* "no_tool_calls" | "max_rounds" | "error" | "canceled" */ }
   ```

   循环逻辑（伪码）：
   ```
   loop:
     msgs = [system] ++ history ++ [user(prompt)]            // history 来自 Run() 多次复用？不——首版每轮新会话
                                                            // 若要做"记忆"挪到 M7；这里只跑单轮对话
     req = ChatRequest{ Messages: msgs, Tools: registry.Specs(), Temperature: cfg.Temp, MaxTokens: cfg.MaxTokens }
     resp, err := client.Chat(ctx, req)                     // 一次性非流式
     if err != nil: emit LoopError{Phase: PhaseLLMCall}; return
     emit AssistantMessage(resp.Choices[0].Message)
     if len(resp.Choices[0].Message.ToolCalls) == 0:
        emit LoopDone{Rounds, Messages}; return
     for _, tc := range ToolCalls:
        emit ToolCallStart(tc)
        result := registry.Get(tc.Function.Name).Execute(ctx, parse(tc.Function.Arguments))
        emit ToolResult{...}
        msgs = msgs ++ [{role:tool, content:result.Content, tool_call_id:tc.ID, IsError?result.IsError}]
     rounds++
     if rounds > MaxRounds: emit LoopDone{Reason:"max_rounds"}; return
     goto loop
   ```
   - **ctx 取消**：`Chat` 与 `Execute` 都传 ctx；REPL 收到 SIGINT → cancel → `Chat` 返回 `ctx.Err()` → `LoopError` 发出。
   - **错误回填**：工具返回 `Result{IsError:true}` 时，message content 前缀 `"[ERROR] "`（或 `"[tool error] "`），让模型区分。
   - **每轮 history 重置？**：**首版每个 prompt 是独立会话**（不持有 session state，简化到极致）；"记住上文"挪到 M7。
4. 单测用 `httptest.Server` 打桩一个 LLM：第一次返回 `tool_calls=[greet({"name":"Ada"})]`，第二次返回纯文本 `"已向 Ada 问好"`。断言：
   - assistant delta 顺序正确；
   - tool_result 内容是中文；
   - 最终 `RunResult.Messages[-1]` 是 assistant 文本；
   - `Rounds == 2`。

**验收**：核心闭环单测通过；**跑通此阶段，MVP 即完成**。

---

### M4 — CLI 入口与联调

**做什么**：
1. `cmd/dsh/main.go`：
   ```
   - 解析 -config / -debug / -prompt（一次性命令模式，调试用）
   - signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
   - config.Load
   - registry := tool.NewRegistry(); 注册 greet / fs_read / fs_write
   - llmClient := llm.OpenAICompatibleClient{...}
   - runner := agent.LoopRunner{Client: llmClient, Registry: registry, ...}
   - if -prompt != "": 一次性跑并打印最终答复 + (--debug) 中间事件
                     else 进入 REPL
   ```
2. `cmd/dsh/repl.go`：
   - `bufio.Scanner` 读 stdin，UTF-8 整行。
   - 每行 prompt → `runner.Run(ctx, prompt)` → 等 `<-chan Event` 中的 `AssistantMessage.Content` 累加显示，`ToolCallStart/ToolResult` 用浅色前缀（`> tool greet(name=Ada)` 等）打印，`LoopDone` 收尾。
   - `--debug` 时打印所有事件（含 AssistantMessage 之前的所有中间状态）。
   - 空行 / `exit` / `quit` / EOF → 退出。
   - `Ctrl+C` 取消**当前轮**（不退出 REPL，用户可继续键入下一轮）；`Ctrl+D` 退出。
3. 端到端示例脚本 `docs/examples/greet-session.md`：描述「执行 `go run ./cmd/dsh` → 键入『请使用 greet 工具向 Ada 打招呼』 → 期望输出」。

**验收（端到端）**：
- `go build ./...` 通过；`go test ./... -race` 通过。
- 实际启动：`go run ./cmd/dsh -config ./harness.example.yml -debug` 后：
  - 输入「请使用 greet 工具向 Ada 打招呼」→ `--debug` 看到 `tool greet(name=Ada)` 被调，结果回填，最终回复包含中文"你好，Ada"。
  - 输入 `exit` → 干净退出（返回码 0）。
  - 跑 `greet` 后再跑 `fs_read` 读刚写好的文件，验证工具目录正确。
  - 在一次对话中途按 `Ctrl+C` → 当前轮中断，REPL 不退出，可继续输入。

---

## 6. 里程碑总览

| 里程碑 | 交付物 | 验收信号 | 关键命令 | 状态 |
| --- | --- | --- | --- | --- |
| M0 | 骨架 + config | `go build` / `go test` / 配置优先级正确 | `go test ./internal/config/...` | ✅ |
| M1 | `llm.Client` | 打桩单测 + 探针联调 | `go test ./internal/llm/...` | ✅ |
| M2 | tool 系统 + 3 内置工具 | 注册表单测 + `fs_write` 四用例 | `go test ./internal/tool ./internal/tools/...` | ✅ |
| M3 | `agent.Runner` | 打桩 LLM 的循环单测通过 | `go test ./internal/agent/...` | ✅ |
| M4 | CLI REPL | 端到端「greet」演示成功 | `go run ./cmd/dsh -config ./harness.example.yml -debug` | ✅ |
| **v1.2** | 代码 review 收尾 | R1–R6 硬伤 + S1–S3 测试覆盖 | `go test ./...` 67 用例全绿 | ✅ |
| M5a | 流式 LLM（SSE） | delta 事件 + REPL 实时渲染 | `./dsh -stream` 看到打字效果 | ⏳ 待启动 |
| M5b | HTTP API | 路由 + 鉴权 + 优雅关闭 | `curl -N localhost:8080/api/agent/stream` | ⏳ |
| M5c | Session 持久化 | sqlite + Begin/Append/Load | `--session` 后 `/history` 可见 | ⏳ |
| M5d | Usage 聚合 | prompt/completion 累计 + cost | REPL footer 显示本轮 cost | ⏳ |
| M5e | REPL 命令扩展 | `/history`、`/clear`、`/usage` | 实际交互验证 | ⏳ |

> **v1.2 累计**：67 个测试通过（config 12 + llm 11 + tool 9 + tools 13 + agent 17 + repl 5）。
>
> 每个里程碑**必须**先编译、再单测、再 demo；任意一个挂了才能进入下一个。

---

## 7. 后续里程碑（不在首版，先记录）

> 详细规格、节奏、风险与验收见 [DESIGN-v2.md](./DESIGN-v2.md)。
> 本节保留为高层摘要，与 v2 设计文档交叉引用。

- **M5**：HTTP API（`/api/agent/message` / `/api/agent/stream` SSE）+ SQLite Session 持久化 + Usage 聚合 + REPL 子命令；`llm.Client` 加 `ChatStream`，`Runner` 加 `RunStream`（共享 v1 `loop` 状态机）。详见 [DESIGN-v2 §A](./DESIGN-v2.md#a-m5-详细设计)。
- **M6**：插件系统（gRPC 子进程，跨平台；候选还有 Go `plugin`/WASM，二选一定下后写设计）。详见 [DESIGN-v2 §B](./DESIGN-v2.md#b-m6--插件系统outline)。
- **M7**：Session 跨进程恢复 + 分支 + 导入导出；Approval 四种 Approver（Noop/AllowList/Terminal/HTTP）；终端工具 `shell_exec`（allowlist + Approver + workspace resolveInWorkspace）。详见 [DESIGN-v2 §C](./DESIGN-v2.md#c-m7--sessionapprovalshell详细到能落地)。
- **M8+**：MCP（stdio 适配） / Skill（`~/.dsh/skills/*.md` loader） / Sub-agent（同步子 LoopRunner） / 多渠道（Anthropic native；Ollama/Gemini 推 v3） / 上下文压缩。详见 [DESIGN-v2 §D](./DESIGN-v2.md#d-m8-----mcp--skill--sub-agent--多渠道outline)。

### 7.1 v2 实施 TODO（按 [DESIGN-v2 §G](./DESIGN-v2.md#g-节奏与工时) 节奏展开）

每条 TODO 标 ☐ / ✅；只有当前步及之前所有步 ✅ 才能进入下一步。验收对照 DESIGN-v2 §F。

| # | TODO | 前置 | 对应设计 | 验收 |
| --- | --- | --- | --- | --- |
| ✅ T0 | v1.2 收尾补丁（6 处硬伤 R1–R6 + S2） | — | DESIGN-v2 §G | ✅ 已完成（v1.2 tag） |
| ✅ T1 | M5a：`internal/stream/` 包（SSE 解析 + Recombiner + ToolCallAccum） | T0 | DESIGN-v2 §A.1.1–§A.1.6 | 单测覆盖 §A.1.6 表格 8 行（8/8 通过 2026-09-15） |
| ✅ T2 | M5a：`llm.Client` 加 `ChatStream`；OpenAI 兼容客户端实现流式 | T1 | §A.1.2 interface | fixture 覆盖 OpenAI + DeepSeek |
| ✅ T3 | M5a：`agent.Runner` 加 `StreamingRunner`；`LoopRunner.RunStream` 共享 loop body | T1, T2 | §A.3.4 / §0.2 兼容 | v1 测试 `-stream.enabled=false` 全绿 |
| ✅ T4 | M5c：`internal/store/` 包；`MapStore` + `SQLiteStore` + `Begin/Append/Load/List/UpdateUsage/Close` | T0 | §A.3.2–§A.3.6 | round-trip + 归档 + 注入测试（22/22 通过） |
| ✅ T5 | M5c：Runner 与 Store 集成；session_id 透传 | T3, T4 | §A.3.4 | session.Messages 长度 = 5（2 轮 + system）通过 |
| ✅ T6 | M5d：`internal/usage/` 包；`Tracker` + cost 配置 + REPL footer | T5 | §A.4 | cost 命中/miss + 并发 Add 干净（6/6 通过） |
| ✅ T7 | M5b：`internal/server/` 包；Bearer 鉴权 + 路由 + SSE + 优雅关停 | T3, T4 | §A.2 | §A.2.6 6 行测试全过 |
| ✅ T8 | M5e：REPL 子命令 `/history /model /usage /stream /sessions /help /clear` | T6, T7 | §A.5 | §A.5.3 5 行测试（10 通过） |
| ✅ T9 | M5 端到端 + README 更新 + v2 tag | T8 | §A.8 | REPL 流式 + HTTP SSE + session 5 messages 通过 |
| ✅ T10 | M7 Approval：`internal/approval/` 包；4 个 Approver | T9 | §C.2 | deny → IsError；session-scope 持久化（15/15 通过） |
| ✅ T11 | M7 Shell：`internal/tools/shell.go`；allowlist + pathguard + Approver | T10 | §C.3 | approve/deny 路径全覆盖（7/7 通过） |
| ✅ T12 | M7 Resume：`--resume <id>`、`/export`、`/import`、branch | T10 | §C.1 | 杀 dsh 重启能续聊（8/8 通过） |
| ✅ T13 | M6 插件：proto + `internal/plugin/` + `examples/plugin/echo/` + `--plugin` flag | T9 | §B.2–§B.5 | 子进程 crash 自动重启 ≤3；9/9 测试通过（**v2.0.1 落地**） |
| ✅ T14 | M8 Anthropic provider：`internal/llm/anthropic/` + 独立 SSE adapter + ChatStream 真实现 | T9 | §D.4 | mock fixture + native SSE Feed 解析（6/6 测试通过；**v2.0.1 落地**） |
| ✅ T15-1 | M8 MCP stdio Session：`Initialize` + `tools/list` + `tools/call` + `ListResources/Prompts` | T13 | §D.1 | in-process fake MCP server round-trip（5/5 测试通过；**v2.0.2 落地**） |
| ☐ T15-2 | M8 Skill loader | T1 | §D.2 | 见 [PLAN-v3](./PLAN-v3.md) T2 |
| ☐ T15-3 | M8 Sub-agent sync | T1 | §D.3 | 见 [PLAN-v3](./PLAN-v3.md) T3 |
| ✅ T16 | v2 release note + 跨里程碑回归 | T15-1 | §G | v2.0.2 release note + 全量测试 |

**v2.0.2 完成**：v2 全部里程碑 ✅，详见 [docs/RELEASE-v2.md](./RELEASE-v2.md)。

**关键依赖关系**：
- T7 必须在 T5 之后（HTTP 路由需要 session_id 字段）
- T11 必须在 T10 之后（Shell 必须挂 Approver）
- T13 必须在 T15-1 之后（MCP/Skill 也通过插件层）—— **已满足**

**v4 规划**：[0-ROADMAP.md](./0-ROADMAP.md)（整体路线图；唯一总纲）+ [PLAN-v3.md](./PLAN-v3.md)（**已被 0-ROADMAP 取代**；v3 是历史快照）+ [DESIGN-v3.md](./DESIGN-v3.md)。


---

## 8. 风险与注意点

1. **工具参数 JSON Schema**：`Parameters() any` + `json.Marshal` 生成。类型严格性弱，但首版够用。后续引入 `invopop/jsonschema` 校验。
2. **流式响应**：首版 `Stream=false`，REPL 体验略顿（最长受 LLM 120s 超时影响）。流式留到 M5，**接口已预留 `iter.Seq2`**。
3. **fs 工具安全**：`fs_write` 必须限制在 workspace 根内（`filepath.Clean` + 前缀校验 + 软链解析二次校验），防穿越。四用例单测必过。
4. **路径分隔符**：Go `path` 是 URL 风格，**首版不允许**；所有 fs 工具必须用 `path/filepath`。Windows 上 `\\` 和 `/` 都接受，但解析后的路径必须落在 workspace 内。
5. **Cancellation 串联**：`Runner.Run(ctx)` 把 ctx 一路传到 `llm.Client.Chat` 与 `Tool.Execute`；任一阶段被取消，整条链路都应该快速返回 `ctx.Err()`，不能让 REPL 假死到超时。`signal.NotifyContext(os.Interrupt, SIGTERM)` 是唯一信号入口。
6. **工具错误回填**：工具失败作为 `Result{IsError:true}` 回填**给模型**，不是给用户；用户看到的是模型"已经知道失败了"之后给出的回复；除非模型也放弃（`max_rounds` 触发），用户才会看到原始错误上下文。
7. **API Key 安全**：`harness.example.yml` 必须用占位符；README 必须强调"用 env 或本地未提交 yml"。任何 PR 含真实 key 一律拒收。
8. **HTTP 客户端与超时**：OpenAI 兼容网关单轮可能 60–120s；`http.Client.Timeout = cfg.LLM.Timeout`（默认 120s）。`fs_write` 这种本地工具单独 30s 超时。
9. **同平台差异**：开发机 Windows + 目标 Linux 都跑过 `go test ./... -race` 一次；CI 上应有 `windows-latest` + `ubuntu-latest` 双 job（首版不强制 PR 检查，但手动验过）。
10. **`config.Config` 序列化**：`YAML` 解析时大小写敏感（`yaml.v3` 默认）；字段名务必与 `yaml:"base-url"` 一致。
