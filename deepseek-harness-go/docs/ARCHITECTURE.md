# DeepSeek Harness Go（dsh）— 架构设计

> 适用版本：`develop` 分支当前实现（v3.0.0-dev）。
> 本文档描述**代码里真实存在的东西**，不是计划。文中每条能力都有对应的包与测试。

---

## §1. 设计目标

dsh 是一个用 Go 实现的 **ReAct agent harness**：给 LLM 一组工具，让它在一个有界循环里反复
"思考 → 调工具 → 读结果 → 再思考"，直到给出最终答案。

四条约束贯穿整个实现：

| # | 原则 | 落地方式 |
|---|---|---|
| P1 | **工具是纯执行器** | `tool.Tool` 只收 `ctx + JSON 参数`，不认识 LLM、Runner 或其他工具 |
| P2 | **错误不冒泡** | 工具失败转成 `Result{IsError:true}` 回填给模型，让它带着新信息继续推理 |
| P3 | **能力默认关闭** | obs / audit / compaction / sandbox / shell 全部 opt-in，默认零开销 |
| P4 | **只依赖标准库** | 除 YAML、SQLite、gRPC、OTel 四个依赖外无三方包 |

---

## §2. 包图

```text
cmd/dsh/                          CLI 入口：装配一切，然后选 REPL / -prompt / -serve
  ├── repl/                       交互式 REPL（斜杠命令、事件渲染）

internal/
  ├── config/                     YAML + env 加载与校验（默认值 → YAML → env）
  │
  ├── llm/                        LLM 适配层
  │   ├── (根)                    OpenAI 兼容 Chat Completions（Chat + ChatStream）
  │   ├── anthropic/              Anthropic Messages API（原生 SSE）
  │   ├── ollama/                 Ollama /api/chat（ndjson 流）
  │   ├── gemini/                 Google generateContent（SSE 流）
  │   └── provider/               provider 路由：按 cfg.Provider 选实现
  │
  ├── tool/                       Tool 接口 + 线程安全 Registry
  ├── tools/                      内置工具：greet / fs_read / fs_write / shell
  │
  ├── agent/                      ReAct 循环运行器（唯一编排点）+ 事件定义
  ├── approval/                   危险工具的人工确认门（4 种 Approver + 链 + 会话缓存）
  ├── sandbox/                    OS 级子进程沙箱（noop / Windows ACL / Linux ns）
  │
  ├── store/                      会话持久化（MapStore 内存 / SQLiteStore 磁盘）
  ├── usage/                      token 聚合与费用计算
  ├── server/                     HTTP + SSE 服务端
  ├── stream/                     SSE 解析与重组
  │
  ├── skill/                      Markdown + YAML frontmatter skill 加载与匹配
  ├── subagent/                   同步子 agent（agent_spawn 工具）
  ├── obs/                        可观测性接缝（Logger / Tracer / Meter，no-op 或 OTel）
  ├── compaction/                 长会话上下文压缩（truncate / llm-summary）
  ├── audit/                      JSONL 审计日志（默认脱敏）
  │
  ├── plugin/                     gRPC 插件宿主与客户端
  │   └── proto/                  tool_provider.proto + 生成代码
  └── mcp/                        MCP stdio 传输层与高层 Session
```

**依赖方向是单向的**：`cmd` → `agent` → `llm`/`tool`/`store`/`obs`。
`tool` 不依赖 `agent`；`llm` 不依赖 `agent`；任何基础包都不反向依赖编排层。

---

## §3. 一次 ReAct 循环的完整数据流

```text
用户 prompt
   │
   ├─► agent.LoopRunner.run()
   │      ├─ 建立 "agent.run" span（obs）
   │      ├─ 准备消息序列：
   │      │    ├─ Store == nil  → [system]
   │      │    ├─ Store != nil 且 sid=="" → Begin 新会话，追加 system
   │      │    └─ Store != nil 且 sid!="" → Load 恢复历史
   │      ├─ 追加 user 消息，并注入命中的 skill body 到 system
   │      └─ 写入 PhaseChange{PhaseInit}
   │
   └─► for 循环（最多 MaxRounds 轮）
          │
          ├─ PhaseChange{PhaseLLMCall}
          ├─ compaction.Maybe()      ← 超阈值则压缩（只影响发给 LLM 的序列）
          ├─ 重新评估 skill 注入      ← 每轮按最新 user prompt 重算
          │
          ├─ llm.Client.Chat / ChatStream
          │      ├─ 建立 "llm.chat" span（model / tokens 属性）
          │      └─ 返回 assistant message
          ├─ AssistantMessage 事件 + 持久化 + audit(llm_call)
          │
          ├─ 若无 tool_calls → stopReason="no_tool_calls"，发 LoopDone，退出
          │
          └─ 对每个 tool_call：
                 ├─ PhaseChange{PhaseToolExec}
                 ├─ 查 Registry；找不到 → 回填 "[ERROR] unknown tool"
                 ├─ ToolCallStart 事件
                 ├─ 建立 "tool.execute" span
                 ├─ audit(tool_call)（参数脱敏为 SHA-256）
                 ├─ safeExecute()  ← panic 兜底
                 ├─ 结果回填为 role=tool 消息（失败加 "[ERROR] " 前缀）
                 └─ ToolResult 事件
```

### 关键不变式

- **所有事件先于 `RunResult` 投递**。`loop()` 的 defer 保证先 `close(out)`、再发结果、最后 `close(res)`，
  即使发生 panic 也如此。订阅者可以放心地 `for range events` 后再 `<-resultCh`。
- **工具失败不终止循环**。失败被写成 `[ERROR] ...` 的 tool 消息，模型看到后继续推理。
- **工具 panic 不终止进程**。`safeExecute` 捕获 panic 并折叠成 `Result{IsError:true}`。
- **压缩只影响发给 LLM 的序列**，Store 里的完整历史不动。

---

## §4. 停止原因（StopReason）

| 值 | 含义 |
|---|---|
| `no_tool_calls` | 助手给出了不带工具调用的最终消息（正常结束） |
| `max_rounds` | 达到 `agent.max-rounds` 上限 |
| `error` | 致命错误（LLM 调用失败、空 choices、panic）；`RunResult.Error` 有值 |
| `canceled` | context 被取消（Ctrl+C、SIGTERM、请求断开） |

---

## §5. 会话与持久化

`store.Store` 是一个六方法的契约：`Begin` / `Append` / `Load` / `List` / `UpdateUsage` / `Close`。
两种实现：

| 实现 | 后端 | 用途 |
|---|---|---|
| `MapStore` | 进程内 map + 每会话互斥锁 | 测试；REPL / `-prompt` 模式的默认 |
| `SQLiteStore` | `modernc.org/sqlite`（纯 Go，无需 CGO），WAL + busy_timeout | `-serve` 模式的默认，落在 `<workspace>/dsh.db` |

`runner.RunStream(ctx, prompt, sid)` 的 `sid` 语义：

- `sid == ""` 且 `Store == nil` → 不持久化（纯内存路径）。
- `sid == ""` 且 `Store != nil` → 开新会话，`Store.Begin` 分配 UUID。
- `sid != ""` → 从 `Store.Load(sid)` 恢复历史，继续对话。

会话还支持导出/导入（`store/export.go`）：单会话与全量两种格式，版本号 `dsh-session/v2`，
导入时可选择保留原 id（幂等覆盖）或分配新 id。

### 5.1 用量与成本

`usage.Tracker` 在 runner 每一轮后累加 token 计数（prompt / completion / total），
并可按模型价格表折算费用：

```go
cost = (completion_tokens / 1000) * price[model]
```

**未知模型名返回 0 而不是报错**——这是刻意的：价格表缺失不该让会话失败。
`Tracker` 是协程安全的（内部互斥锁串行化 `Add`）。`Snapshot` 返回值可安全共享给 HTTP / REPL 消费方。

注意分工：`llm.Usage` 本身不携带模型名，费用归因发生在 runner 边界（那里知道当前模型是什么），
因此 `Tracker.Add` 只更新 token 计数，费用由 `SetCost` 单独设置。

---

## §6. LLM 多渠道

`internal/llm/provider.NewClient(cfg)` 按 `cfg.Provider` 路由：

| provider | 端点 | 协议 |
|---|---|---|
| `openai`（默认，含空值） | `{base-url}/chat/completions` | Chat Completions 兼容 |
| `anthropic` | `{base-url}/messages` | Anthropic Messages API（原生 SSE） |
| `ollama` | `{base-url}/api/chat` | Ollama ndjson 流 |
| `gemini` | `{base-url}/v1beta/models/{model}:generateContent` | Google generateContent（SSE 流） |

未支持的 provider 在启动时报错并列出支持列表——不会静默回退到默认渠道。

`llm.Client` 契约有两个方法：

- `Chat(ctx, req)` —— 非流式。`req.Stream=true` 时直接返回 `ErrStreamNotSupported`，不发网络请求。
- `ChatStream(ctx, req)` —— 流式，返回 chunk 通道与 error 通道。OpenAI 兼容实现对 HTTP 429
  会退避 200 ms 后自动重试一次；其他非 2xx 直接以 `*APIError` 返回。

---

## §7. 工具系统

`tool.Tool` 是四个方法的契约：

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() any
    Execute(ctx context.Context, args json.RawMessage) (Result, error)
}
```

`tool.Registry` 是线程安全的 `name → Tool` 映射，维护插入顺序，保证 `Names()` / `Specs()`
输出稳定（LLM 看到的工具清单顺序因此可复现）。重名注册返回 `ErrDuplicate`。

内置工具：

| 工具 | 默认 | 说明 |
|---|---|---|
| `greet` | ✅ | 回一句问候，用于冒烟测试 |
| `fs_read` | ✅ | 读工作区内的文件；路径逃逸与符号链接逃逸都会被拒绝 |
| `fs_write` | ✅ | 写工作区内的文件；同样受路径围栏保护 |
| `shell` | ❌ 需 `shell.enabled=true` | 跑操作系统命令，受 allowlist + 审批 + 沙箱三重约束 |
| `agent_spawn` | ✅ | 同步子 agent（见 §8） |

### shell 的四道防线

1. **allowlist**：只匹配命令名（`args[0]` 的 basename）；空列表 = 拒绝一切。
2. **路径围栏**：看起来像路径的参数解析后必须落在 `agent.workspace` 内；符号链接按 fail-closed 处理。
3. **审批**：`approval.Approver` 裁决，允许一次 / 允许整个会话 / 拒绝。
4. **沙箱**：`sandbox.Sandbox.Apply` 在 `cmd.Start` 前施加 OS 级约束；**失败绝不静默降级**为无沙箱执行。

此外：子进程环境被裁剪为最小集（`PATH` + `HOME`），输出截断到 1 MiB。

### 7.1 审批

`approval.Approver` 是危险工具的人工确认门。裁决结果有三档：

| 裁决 | 含义 |
|---|---|
| `ApproveOnce` | 仅允许本次调用 |
| `ApproveSession` | 允许当前会话内后续所有同名工具的调用 |
| `Deny` | 拒绝 |

四种内置实现：

| 实现 | 行为 |
|---|---|
| `NoopApprover` | 总是批准（关闭审批 / 可信工具） |
| `AllowListApprover` | 工具名在固定集合中则批准，并授予会话级批准 |
| `TerminalApprover` | 在 stderr 提示，从 stdin 读一行：`y`/`yes`/回车 → 一次；`s`/`session` → 会话级；其他 → 拒绝 |
| `HTTPPollApprover` | 阻塞轮询远程决策器直到给出结论 |

**故障安全**：`TerminalApprover` 在 EOF 或读取错误时裁决为 `Deny`（而非放行）。
`ctx` 取消时返回 `Deny` + `ctx.Err()`。

`Chain` 可以把多个 Approver 串起来：依次咨询，第一个给出非 pending 结论的生效；
出现硬错误立即终止链条；全部 pending 则拒绝。`CachingApprover` 包装内部审批者，
缓存 `ApproveSession` 的裁决，使会话内后续同名调用跳过审批。

---

## §8. 子 agent

`agent_spawn(prompt)` 是暴露给 LLM 的内置工具。它同步创建一个子 `LoopRunner`：

- **共享**父级的 LLM client 与工具注册表。
- **禁止嵌套**——两道保险：子注册表在结构上剔除 `agent_spawn` 本身，且 ctx 里带深度标记，
  超过 `MaxDepth = 1` 直接拒绝。
- **中间事件不可见**：子 agent 的事件流被排空，父 runner 只看到最终答案作为一次 tool result。
- **失败统一折叠**：panic、超时、取消都变成 `tool.Result{IsError:true}`。

---

## §9. Skill 系统

skill 是 `Markdown + YAML frontmatter` 文件：

```markdown
---
name: code-review
description: 用户要求 review 代码时注入
trigger: "review"     # mention 关键词；大小写不敏感
always: false         # true 则每轮都注入
---

# 正文（frontmatter 之后的全部内容）
```

加载目录默认为 `~/.dsh/skills/*.md`，可用 `skills.dir` 覆盖。匹配规则：

- `always: true` → 每轮命中。
- `trigger` 非空且作为子串出现在用户输入中 → 命中（大小写不敏感）。

命中的 skill body 以 `<skill name="...">` 标签拼接到 system prompt 尾部。
**每轮都会重新评估**：注入前先把 system 重置为基线，避免多轮累积。

异常约定：目录不存在返回空列表（不报错）；frontmatter 缺必需字段则报错（不静默）。

---

## §10. 上下文压缩

`compaction.Compactor.Maybe(ctx, msgs)` 在每轮 LLM 调用前检查消息的 token 估算：

- 未超过 `trigger-tokens` → 原样返回。
- 超过 → 委托给 `Strategy` 压缩到约 `trigger/2`，并发出 `agent.Compacted{Before, After}` 事件。

两种策略：

| 策略 | 行为 |
|---|---|
| `truncate` | 保留 system + 最近 N 条，在丢弃区间插入一条 summary placeholder |
| `llm-summary` | 同上，但把丢弃区间交给**同一个** LLM client 摘要，替换 placeholder |

两条硬规则：**system 永远保留**；**tool 结果不与它的 assistant 调用拆散**（压缩边界不会落在
`role=tool` 上，否则 OpenAI 协议会因结果与调用不配对而报错）。

---

## §11. 可观测性与审计

这是两条独立的通道：

- **obs**（`internal/obs`）—— 面向指标与链路。`Logger` / `Tracer` / `Span` / `Meter` 四个接口族，
  默认全部是 no-op（零开销）；`obs.provider=otel` 时切换到 OTel SDK（OTLP gRPC 导出）。
  接入点：`agent.run`、`llm.chat`、`tool.execute` 三个 span。
- **audit**（`internal/audit`）—— 面向合规。JSONL 文件，每行一个事件
  `{ts, session_id, event, ...}`。事件类型：`tool_call`、`approval_decision`、`llm_call`、`http_request`。

**脱敏默认开启**：参数只记 SHA-256 摘要；只有 `audit.full=true`（或 CLI `--audit` 配合配置）
才记录原文。审计写失败被静默吞掉——审计不应拖垮主流程。

---

## §12. 沙箱

`internal/sandbox` 用 build tag 隔离平台实现：

| provider | 平台 | 机制 |
|---|---|---|
| `noop` | 全平台 | 不做任何约束（与早期行为一致） |
| `windows_acl` | Windows | `CreateRestrictedToken`（DISABLE_MAX_PRIVILEGE）+ 敏感路径拒绝 |
| `linux_ns` | Linux | `CLONE_NEWNS｜NEWPID｜NEWUSER` + uid/gid 映射 + 敏感路径拒绝 |
| `auto` | 按 `runtime.GOOS` | 命中上述平台实现，否则回落到 noop |

Windows 默认拒绝访问 `C:\Windows`、`C:\Program Files`、`C:\Program Files (x86)`、`C:\ProgramData`；
Linux 拒绝 `/etc/shadow` 等敏感路径。

沙箱实现了可选的 `TokenReleaser` 接口：shell 工具在 `cmd.CombinedOutput()` 之后调用它释放
受限令牌句柄。

---

## §13. HTTP 服务端

`internal/server` 暴露 5 个端点，除 `/healthz` 外全部要求 `Authorization: Bearer <token>`
（常量时间比较）：

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/healthz` | 健康检查，无需认证 |
| `POST` | `/api/agent/message` | 阻塞执行一次 prompt，返回最终结果 JSON |
| `GET` | `/api/agent/stream` | SSE 流式；query 参数 `prompt`（必填）、`session_id`（可选） |
| `GET` | `/api/sessions` | 列出会话元数据 |
| `GET` | `/api/sessions/{id}` | 获取完整会话记录 |

同一 `session_id` 上的并发请求返回 **409**（每会话互斥锁）。

### §13.1 Web 控制台

`internal/webui` 用 `go:embed` 内嵌前端构建产物（`web/` → `internal/webui/dist`），
由 `internal/server` 在根路径 `/` 提供托管。SPA fallback：未命中磁盘文件的路径一律
返回 `index.html`，交由前端路由处理。

**鉴权边界**：静态资源（HTML / JS / CSS）**不要求** Bearer token —— 浏览器加载
index.html 时无法携带自定义请求头，且页面本身不含敏感数据。`/api/*` 仍全部受
`bearerAuth` 保护。

**SSE 帧契约**（`internal/server/util.go` 的 `eventToFrame`）：

| 事件 | 负载 | 说明 |
|---|---|---|
| `phase_change` | `{phase, at}` | 阶段转换（`init` / `llm_call` / `tool_exec` / `stopped`） |
| `assistant_delta` | `{text, index}` | 流式文本增量 |
| `assistant_message` | `{content, tool_calls}` | 本轮助手消息定稿 |
| `tool_call_start` | `{call}` | 即将执行工具 |
| `tool_result` | `{call_id, name, content, is_error, took_ms}` | 工具执行结果 |
| `loop_error` | `{err, phase}` | 循环致命错误 |
| `loop_done` | `{rounds, stop_reason, session_id, usage}` | 终止帧；**唯一**的终止事件 |

`loop_done` 由服务端统一发送（runner 自身的 `LoopDone` 事件不转发），保证客户端
只收到一个语义完整的终止帧。`session_id` 必须带上：新建会话时前端只能从这里拿到 id，
后续轮次才能续用同一会话。

**流式 usage**：`llm.SSEFrame` 解析顶层 `usage` 字段（与 `choices` 平级，出现在终止帧），
经 `StreamChunk.Usage` 透传到 agent 层并累加到 `RunResult.Usage`。

**工具调用合并**：OpenAI 协议会把一次工具调用的 name / arguments 拆到多个 SSE 帧，
`agent` 层用 `internal/stream.Recombiner` 按 `call_id` 合并，避免产生重复的工具调用。

---

## §14. 插件系统

`internal/plugin` 用 gRPC 做进程间通信。宿主启动插件子进程，通过环境变量 `DSH_PLUGIN_ADDR`
告知其 gRPC 地址，然后建立客户端连接并把插件的工具合并进进程内的 `tool.Registry`。

- 插件进程崩溃时自动重启，最多 `MaxRestarts` 次。
- 插件工具在执行前会征询 `Approver`。
- `--plugin` 参数可重复，每个参数一个插件进程。
- 插件启动失败是**致命错误**——静默丢弃用户明确要求的工具比拒绝启动更糟。

---

## §15. MCP

`internal/mcp` 封装 MCP 的 JSON-RPC stdio 传输。高层 `Session` 覆盖：

- `initialize` 握手（protocol version `2024-11-05`）+ `notifications/initialized`。
- `tools/list` → `tools/call`。
- `resources/list`、`prompts/list` —— 尽力而为：服务器不支持（method-not-found）时返回空列表而非报错。

---

## §16. 取消与错误模型

**取消链**：`os.Interrupt` / `SIGTERM` → `signal.NotifyContext` → rootCtx → 每轮 REPL 轮次派生的子 context
→ `Runner.Run` → `Client.Chat` / `Tool.Execute`。任何一环都能中止整条链。

**错误分层**：

| 层级 | 处理方式 |
|---|---|
| 工具参数错误 / 业务失败 | `Result{IsError:true}`，回填给模型 |
| 工具 panic | `safeExecute` 捕获，折叠成 `Result{IsError:true}` |
| LLM 调用失败 | 终止本轮，`StopReason="error"`，发 `LoopError` |
| 配置非法 | 启动即失败，打印全部问题项 |
| 未知 provider / 未知 sandbox | 启动即失败并列出支持列表 |

---

## §17. 已知边界

- REPL 的 `/history <N>` 尚未真正打印历史消息（当前提示后续版本实现）；`/history <session-id>` 的会话切换可用。
- `usage.Tracker` 已实现并在 REPL 中可查，但 `cmd/dsh` 目前装配的是空价格表，成本恒为 0。
- `server.request-timeout` 与 `max-concurrent-sessions` 目前是软上限，不做强制拒绝。
- 沙箱的 Linux namespace 实现需要在具备相应权限的环境下运行；CI 中仅做交叉编译校验。
- 插件协议未做版本协商，仅支持当前 proto 定义。

---

**相关文档**：[README.md](../README.md)（快速开始与配置）· [TESTING.md](./TESTING.md)（测试与验收）
