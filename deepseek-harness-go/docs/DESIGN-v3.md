# DeepSeek Harness Go 版 — 第三版设计文档（v3 / DESIGN-v3）

> **历史快照**。本文档是 [DESIGN-v2.md](./DESIGN-v2.md) 的**继任设计**，但已被 v4 借鉴并扩展。
> - **v4 实施规划**：[0-ROADMAP.md](./0-ROADMAP.md)（替代 [PLAN-v3.md](./PLAN-v3.md)）
> - v4 在 v3 基础上**新增**：§A 事件流 + 投影缓存（借鉴 Java v0.1.7 §4）、§B 统一 Gateway SSE（借鉴 Java v0.1.7 §5）、§C 插件库存视图（借鉴 Java v0.1.7 §7）、§D 测试用例库（借鉴 Java v0.1.7 §8.1）
> - v4 把 v3 原 §F Sandbox **删除**（借鉴 Java 不上 OS sandbox 而是 Python runtime 隔离；Go 版 v5+ 再考虑）
> - v4 把 v3 原 §A Skill **降级 P2**
> 适用版本：deepseek-harness-go **v3.0.0**（v2 收官后启动；**v3 已被 v4 取代**）。
> 阅读顺序建议：§0 原则 → §1 目标与差距 → §2 包增量 → §A Skill → §B Sub-agent → §C Obs（OTel） → §D Compaction → §E Audit → §F Sandbox → §G 多渠道 LLM → §H 验收 → §I 节奏。

## §0. 设计原则

| # | 原则 | 含义 |
|---|---|---|
| P1 | **不重写 v2** | v2 公开签名/接口/CLI 全部冻结；v3 只在 v2 之上加层 |
| P2 | **v2 优先于新功能** | v2 留存的"⚠️"项先补齐，再开 v3 |
| P3 | **接口即契约** | 新包必须先定义 interface，再写实现；mock 同 PR 内交付 |
| P4 | **可观测优先于功能** | 所有 I/O 必须接 `internal/obs/`；v3 让 obs 从 no-op 升级为 OTel 真实现 |
| P5 | **零破坏的 Go 升级** | 仍 stdlib 优先；§7 三方依赖白名单内允许引入 |
| P6 | **每个里程碑有 demo 命令** | 与 PLAN §5 同样要求 |
| P7 | **测试覆盖率 ≥ 80%** | v3 起 `go test -cover` 入 CI；新代码不允许 < 80% |

## §1. v3 目标

**一句话**：把 dsh 从"可被 HTTP 调用、能跑工具的 agent runtime"升级为"生产可用的长会话 agent runtime，对齐官方 Node 版 60%+ 能力"。

### §1.1 五个目标

1. **Skill system**：Markdown + YAML frontmatter 文件注入 system prompt；mention 触发；热加载
2. **Sub-agent**：同步子 LoopRunner；父子事件汇总；v4 并发版
3. **Observability**：`internal/obs/` 真正落地（OTel SDK）+ hook 点覆盖
4. **Context compaction**：超阈值触发；`truncate` / `llm-summary` 两策略
5. **Audit log + Sandbox + 多渠道**：补齐 v2 留底

### §1.2 v2 → v3 现状

| 域 | v2 实际 | v3 |
|---|---|---|
| ReAct 核心 | ✅ | — |
| REPL / HTTP / SSE | ✅ | — |
| Session + 持久化 | ✅ | — |
| Usage + cost | ✅ | — |
| Approval + Shell | ✅ | — |
| Plugin gRPC | ✅ | — |
| Anthropic Stream | ✅ | — |
| MCP Session | ✅ | — |
| **Skill loader** | ❌ | ✅ §A |
| **Sub-agent (sync)** | ❌ | ✅ §B |
| **Obs (OTel)** | ❌ | ✅ §C |
| **Compaction** | ❌ | ✅ §D |
| **Audit log** | ❌ | ✅ §E |
| **OS sandbox** | ❌ | ✅ §F |
| **Ollama / Gemini** | ❌ | ✅ §G |
| Plan mode / Persona / Schedule | ❌ | v4+ |

## §2. 包增量

```text
internal/
├── skill/                  ── 新包
│   ├── loader.go                 — 加载 ~/.dsh/skills/*.md
│   ├── matcher.go                — mention 触发
│   └── registry.go               — Skill struct + Register/Resolve
│
├── subagent/               ── 新包
│   ├── subagent.go               — Sync 子 LoopRunner
│   ├── parent.go                 — 父 runner 接入点
│   └── result.go                 — SubResult → parent
│
├── obs/                    ── 从空目录升级
│   ├── obs.go                    — Logger/Tracer 接口（保持）
│   ├── noop.go                   — 默认实现
│   └── otel.go                   — OTel Tracer + MeterProvider（opt-in）
│
├── compaction/             ── 新包
│   ├── strategy.go               — Strategy interface
│   ├── truncate.go               — 简单截断
│   ├── llmsummary.go             — LLM 摘要
│   └── compactor.go              — 调度器
│
├── audit/                  ── 新包
│   ├── log.go                    — JSONL 写入
│   ├── event.go                  — Event types
│   └── redact.go                 — 脱敏（仅记 hash）
│
├── sandbox/                ── 新包
│   ├── sandbox.go                — Sandbox interface + noop
│   ├── windows_acl.go            — Windows ACL（//go:build windows）
│   └── linux_ns.go               — Linux namespaces（//go:build linux）
│
└── llm/
    ├── ollama/                  ── 新包
    └── gemini/                  ── 新包
```

## §A. Skill System

### §A.1 文件格式

```markdown
---
name: code-review
description: 当用户请求 review 代码时注入；包含项目代码风格与检查清单
body: |
  ## 代码风格
  - 函数 < 50 行
  - 错误不冒泡到 Runner
  ...
---

# 完整 skill 正文（也会拼到 system prompt）
```

路径：`~/.dsh/skills/<name>.md` 或 `harness.yml` 里 `skills.dir` 指定。

### §A.2 触发机制

两种：
1. **Mention trigger**：skill frontmatter 写 `trigger: "review"`；用户输入含 `review` 关键词 → 注入
2. **Always-on**：frontmatter `always: true` → 每轮都注入（仅小 body）

### §A.3 接口

```go
type Skill struct {
    Name        string
    Description string
    Trigger     string
    Always      bool
    Body        string
}

type Loader interface {
    Load(dir string) ([]Skill, error)
}

type Matcher interface {
    Match(skills []Skill, userPrompt string) []Skill
}
```

### §A.4 接入 Runner

```go
runner := agent.NewLoopRunner(...)
runner.Skills = []Skill{...}   // 或 ResolveFromDir

// loop 每轮：
// 1. user prompt 进来
// 2. matcher.Match(skills, prompt) → [s1, s2]
// 3. system = system + "\n\n" + s1.Body + "\n\n" + s2.Body
// 4. 走原 loop body
```

### §A.5 验收

| 用例 | 断言 |
|---|---|
| 假 skill.md 注入 system | body 出现在 system prompt |
| mention "review" 触发 review skill | Match 返回对应 skill |
| 多个 mention 全部命中 | 都注入 |
| YAML frontmatter 缺字段 | 报错，不静默 |
| 不存在目录 | 返回空 list，不报错 |

## §B. Sub-agent (Sync)

### §B.1 工具

新增内置工具 `agent_spawn(prompt: string) -> string`：
- 内部 new LoopRunner（共享 parent 的 LLM client + tool registry）
- 同步执行 Run 直到完成
- 返回 final assistant 文本
- 父 runner 当成一次 tool result

### §B.2 接口

```go
// internal/subagent/subagent.go
type Spawner interface {
    Spawn(ctx context.Context, prompt string) (string, error)
}

type SubRunner struct {
    Parent  *agent.LoopRunner
    Child   *agent.LoopRunner
}

func NewSync(llm llm.Client, reg *tool.Registry, model string) *SubRunner {
    child := agent.NewLoopRunner(llm, reg, ..., model, ...)
    return &SubRunner{Child: child}
}
```

### §B.3 验收

| 用例 | 断言 |
|---|---|
| mock 子 agent 返回固定文本 | 父 runner final 含子结果 |
| 子 agent panic | 父 tool_result is_error=true |
| 子 agent 超时（ctx cancel） | 父 tool_result is_error=true, msg=canceled |
| 子 agent 调起自己（嵌套） | 限制 max_depth=1 |

### §B.4 v4 升级

并发 sub-agent：spawn N 个 goroutine + errgroup，结果聚合；保留 sync 接口为 v4 默认。

## §C. Observability (OTel)

### §C.1 接口（保持 v2 stub）

```go
// internal/obs/obs.go
type Logger interface {
    Debug(ctx context.Context, msg string, attrs ...Attr)
    Info(ctx context.Context, msg string, attrs ...Attr)
    Warn(ctx context.Context, msg string, attrs ...Attr)
    Error(ctx context.Context, msg string, attrs ...Attr)
}

type Tracer interface {
    Start(ctx context.Context, name string) (context.Context, Span)
}

type Span interface {
    End()
    SetAttr(key string, val any)
    RecordError(err error)
}

type Meter interface {
    Counter(name string) Counter
    Histogram(name string) Histogram
}
```

### §C.2 OTel 实现（opt-in）

```go
// internal/obs/otel.go
// 仅当 obs.provider=otel 时启用
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/sdk/trace"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace"
)

func NewOTelTracer(ctx context.Context, cfg OTelConfig) (Tracer, error) {
    exp, err := otlptrace.New(ctx, otlptrace.WithEndpoint(cfg.Endpoint))
    tp := trace.NewTracerProvider(trace.WithBatcher(exp))
    otel.SetTracerProvider(tp)
    return &otelTracer{tp: tp}, nil
}
```

### §C.3 接入点

每个 Runner / Tool / LLM call 自动 span：
- `runner.Run` → span "agent.run"
- `client.Chat` → span "llm.chat" (model, tokens attrs)
- `tool.Execute` → span "tool.execute" (tool name)

### §C.4 配置

```yaml
obs:
  provider: noop   # noop | otel
  otel:
    endpoint: "localhost:4317"   # OTLP gRPC
    service-name: dsh
    sample-ratio: 0.1
```

### §C.5 验收

| 用例 | 断言 |
|---|---|
| OTLP exporter 起本地 collector | 看到 trace |
| provider=noop | 零开销，无外部依赖 |
| Span 属性完整 | model/prompt_tokens/completion_tokens 都到位 |

## §D. Context Compaction

### §D.1 接口

```go
// internal/compaction/strategy.go
type Strategy interface {
    Name() string
    Compact(ctx context.Context, msgs []llm.Message, target int) ([]llm.Message, error)
}

type Compactor struct {
    Strategy Strategy
    Trigger  int    // tokens 超此值触发
}

func (c *Compactor) Maybe(ctx, msgs) ([]llm.Message, bool) {
    if tokenCount(msgs) > c.Trigger {
        out, err := c.Strategy.Compact(ctx, msgs, c.Trigger/2)
        return out, true
    }
    return msgs, false
}
```

### §D.2 truncate 策略

保留 system + 最近 N 条；中间丢：
- N 由 `target` 算出
- 在丢的部分中间插一条"summary placeholder"让 LLM 知道上下文被压缩

### §D.3 llm-summary 策略

调一次 LLM 让它把丢的部分做摘要，结果插回原位：
- 用同一个 LLM client（不引入新模型）
- prompt: "请用 200 字总结以下对话"

### §D.4 配置

```yaml
compaction:
  enabled: true
  trigger-tokens: 16000
  strategy: llm-summary   # truncate | llm-summary
  keep-recent: 6          # 永远保留最近 N 条
```

### §D.5 验收

| 用例 | 断言 |
|---|---|
| 触发后 message 数减少 | 数量明显小于原数 |
| 语义保留 | LLM 后续能继续对话不出错 |
| system 永远保留 | compaction 后第一条还是 system |
| keep-recent 命中 | 最近 N 条不被丢 |
| token count 工具 | 用 tiktoken-go 或纯字符估算 |

## §E. Audit Log

### §E.1 格式

JSONL：`{ts, session_id, event, ...}`，每行一个事件。

事件类型：
- `tool_call`: `{ts, session_id, round, tool, args_hash, args_raw?}`
- `approval_decision`: `{ts, session_id, tool, decision, source}`
- `llm_call`: `{ts, session_id, model, prompt_tokens, completion_tokens}`
- `http_request`: `{ts, method, path, status, dur_ms}`

### §E.2 脱敏

默认 redact 模式：args 只记 SHA-256 hash；`audit.full=true` 才记 raw content。

### §E.3 接口

```go
// internal/audit/log.go
type Logger interface {
    Log(ctx context.Context, ev Event)
    Close() error
}

func NewFileLogger(path string, redact bool) (*FileLogger, error)
```

### §E.4 接入点

Runner 在每个 round 调 `audit.Log(...)` 一次；tool.Execute 同理。

### §E.5 验收

| 用例 | 断言 |
|---|---|
| 触发 tool_call | audit.jsonl 含对应行 |
| redact=true | args_raw 字段为空，只有 hash |
| 多 session 并发 | 各 session_id 准确 |

## §F. OS Sandbox

### §F.1 接口

```go
// internal/sandbox/sandbox.go
type Sandbox interface {
    Name() string
    Apply(ctx context.Context, cmd *exec.Cmd) error   // 设置 cmd.SysProcAttr 等
    Validate(path string) error                       // 路径检查
}

type NoopSandbox struct{}
```

### §F.2 Windows ACL（//go:build windows）

用 `golang.org/x/sys/windows`:
- 给子进程 token 加 restricted SID
- 拒绝对 `C:\Windows\System32` 等敏感路径的访问

### §F.3 Linux namespaces（//go:build linux）

用 `syscall.SysProcAttr`:
- `Cloneflags: syscall.CLONE_NEWNS | syscall.CLONE_NEWPID | syscall.CLONE_NEWUSER`
- 把 workspace bind-mount 到 `/workspace`
- 限制可见路径

### §F.4 接入

Shell 工具执行前调 `sandbox.Apply(cmd)`；默认 `noop`，配置 `sandbox.provider=windows_acl | linux_ns` 时启用。

### §F.5 验收

| 用例 | 断言 |
|---|---|
| noop sandbox | 与 v2 行为一致 |
| Windows ACL 启用 | 访问 System32 被拒（需 win） |
| Linux ns 启用 | `unshare -n` 生效（需 linux） |
| sandbox.Apply 失败 | 工具返回 Result{IsError:true} |

## §G. 多渠道 LLM

### §G.1 Ollama provider

```go
// internal/llm/ollama/ollama.go
type Client struct {
    BaseURL string  // http://127.0.0.1:11434
    Model   string
}

// /api/chat + SSE（Ollama 用 ndjson 而不是 SSE，但接口一致）
```

### §G.2 Gemini provider

```go
// internal/llm/gemini/gemini.go
type Client struct {
    APIKey string
    Model  string  // gemini-1.5-pro / gemini-1.5-flash
}

// /v1beta/models/{model}:streamGenerateContent?alt=sse
```

### §G.3 provider 路由

```go
func NewClient(cfg config.LLMConfig) (llm.Client, error) {
    switch cfg.Provider {
    case "openai":    // default
        return openai.NewClient(...)
    case "anthropic":
        return anthropic.NewClient(...)
    case "ollama":
        return ollama.NewClient(...)
    case "gemini":
        return gemini.NewClient(...)
    }
}
```

### §G.4 配置

```yaml
llm:
  provider: openai   # openai | anthropic | ollama | gemini
  base-url: ...
  api-key: ...
  model: ...
```

### §G.5 验收

| 用例 | 断言 |
|---|---|
| mock Ollama fixture | round-trip 一致 |
| mock Gemini SSE fixture | delta 累积 + finish |
| provider=openai（默认） | 与 v2 行为一致 |
| 未支持 provider | 启动时报错列出支持列表 |

## §H. 验收矩阵（v3 摘要）

| 子能力 | 自动测试 | 手动 demo |
|---|---|---|
| Skill loader | 假 skill.md + mention 匹配 | `/skills` 列已加载 |
| Sub-agent (sync) | mock 子 agent | `agent_spawn` 调起子 agent |
| Obs (OTel) | OTLP exporter → collector | 看 stdout trace |
| Compaction | 触发后 message 数减少 | 长对话 50 轮后 `/history` 看到 summary |
| Audit | 触发 tool_call → audit.jsonl | `dsh --audit /tmp/audit.jsonl` |
| Sandbox win | （仅 windows）System32 访问被拒 | （需 win） |
| Sandbox linux | （仅 linux）`unshare -n` 生效 | （需 linux） |
| Ollama | mock fixture | 本地 `ollama serve` 后跑 |
| Gemini | mock fixture | `provider: gemini` |

## §I. 节奏与工时

| 步 | 内容 | 预估 | 前置 |
|---|---|---|---|
| 0 | v2 release note 收尾 | 0.5d | — |
| 1 | Skill loader (§A) | 1d | 0 |
| 2 | Sub-agent sync (§B) | 1d | 0 |
| 3 | Obs OTel opt-in (§C) | 1d | 0 |
| 4 | Compaction (§D) | 1.5d | 3 |
| 5 | Audit log (§E) | 0.5d | 0 |
| 6 | Sandbox win+linux (§F) | 2d | 0 |
| 7 | Ollama + Gemini providers (§G) | 2d | 0 |
| 8 | 跨里程碑回归 + release note v3 | 1d | 7 |

**合计 ~10.5 天**（一人）。

### §I.1 风险

| 风险 | 缓解 |
|---|---|
| OTel SDK 体积大 | 默认仍 noop，OTel opt-in |
| Windows ACL 编译要求 `GOOS=windows` | build tag 隔离 |
| Linux ns 编译要求 cgo | build tag 隔离 |
| Ollama / Gemini 协议变动 | 抽 adapter 层 |
| Compaction 让 LLM"失忆" | prompt 注入压缩说明 + 保留 system |

## §J. 三方依赖白名单（v3 增量）

| 库 | 用途 | 必须 |
|---|---|---|
| `go.opentelemetry.io/otel` | OTel SDK | opt-in |
| `golang.org/x/sys/windows` | Windows ACL | win-only |
| `golang.org/x/sys/unix` | Linux ns | linux-only |
| `github.com/pknot/tiktoken-go`（可选）| token 计数 | no |

---

**版本**：v3.0-draft（2026-09-16 起）
**入口**：[DESIGN-v2.md](./DESIGN-v2.md) 收官；本文件为 v3 主入口
**配套实施**：[PLAN-v3.md](./PLAN-v3.md)
