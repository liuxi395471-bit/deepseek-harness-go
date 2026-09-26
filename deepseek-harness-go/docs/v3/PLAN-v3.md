# DeepSeek Harness Go v3 — 实施规划（PLAN-v3）

> **状态**：v3 实际进展已超出原计划。
> - ✅ 原 T2 Skill loader 仍未启动
> - ✅ 原 T3 Sub-agent 仍未启动
> - ✅ 原 T4-T11 Obs/Compaction/Audit/Sandbox/Ollama/Gemini 仍未启动
> - ✅ **新增**：M6 plugin gRPC（T13）、M8 Anthropic ChatStream（T14）、MCP Session（T15-1）已在 v2.0.2 中完成
> **v3 已被 [0-ROADMAP.md](./0-ROADMAP.md) 取代**；本文件作为 v3 时期快照保留。
>
> 对应设计：[DESIGN-v3.md](./DESIGN-v3.md)（§A–§G 大部分已被 v4 借鉴并扩展）
> 状态：v2 收官后启动（v2.0.2 完成于 2026-09-16）
> 原则：**先实现 §A–§G 各包的最小骨架 + 测试，再做集成，最后做验收矩阵**

## 0. 起点与前置

### 0.1 v2 冻结点


```
T0  v1.2 收尾                                          ✅
T1  M5a 流式 LLM (SSE + Recombiner)                     ✅
T2  ChatStream + OpenAI 兼容                            ✅
T3  StreamingRunner + LoopRunner.RunStream              ✅
T4  internal/store (Map + SQLite)                       ✅
T5  Runner × Store 集成                                 ✅
T6  internal/usage Tracker                              ✅
T7  internal/server (HTTP + SSE + Bearer)               ✅
T8  REPL 子命令                                         ✅
T9  M5 端到端 + v2 tag                                  ✅
T10 M7 Approval (4 Approver)                             ✅
T11 M7 Shell (allowlist + pathguard)                     ✅
T12 M7 Resume / Export / Import / branch                 ✅
T13 M6 插件 (gRPC 真落地 + echo demo + --plugin)         ✅ v2.0.1
T14 M8 Anthropic (Chat + ChatStream native SSE)         ✅ v2.0.1
T15-1 MCP Session (Initialize + tools/list + call)       ✅ v2.0.2
T15-2 Skill loader                                      ☐ v3 P1
T15-3 Sub-agent                                         ☐ v3 P1
```

### 0.2 v3 与 v2 不兼容点

无。v3 是纯增量。所有 v2 CLI / API / 配置字段保留。

## 1. v3 TODO 表

每条 TODO 标 ☐ / ✅；只有当前步及之前所有步 ✅ 才能进入下一步。

| # | TODO | 前置 | 对应设计 | 验收 |
|---|---|---|---|---|
| ☐ T1 | v2 release note 收尾（v2.0.2 完成于 2026-09-16） | — | DESIGN-v3 §1.2 | 本文件 |
| ☐ T15-2 | Skill loader | T1 | DESIGN-v3 §A.1–§A.5 | 5 用例 |
| ☐ T15-3 | Sub-agent | T1 | DESIGN-v3 §B.1–§B.3 | 4 用例 |
| ☐ T4 | Obs OTel opt-in（§C） | T1 | DESIGN-v3 §C.1–§C.5 | 3 用例 |
| ☐ T5 | Compaction truncate（§D.2） | T4 | DESIGN-v3 §D.5 | 4 用例 |
| ☐ T6 | Compaction llm-summary（§D.3） | T5 | DESIGN-v3 §D.5 | 1 用例 |
| ☐ T7 | Audit log JSONL（§E） | T1 | DESIGN-v3 §E.5 | 3 用例 |
| ☐ T8 | Sandbox noop + win ACL + linux ns（§F） | T1 | DESIGN-v3 §F.5 | 3 用例 |
| ☐ T9 | Ollama provider（§G.1） | T4 | DESIGN-v3 §G.5 | 1 用例 |
| ☐ T10 | Gemini provider（§G.2） | T4 | DESIGN-v3 §G.5 | 1 用例 |
| ☐ T11 | provider 路由集成到 main.go | T9, T10 | DESIGN-v3 §G.3 | 1 用例 |
| ☐ T12 | v3 端到端 + README 更新 + v3 tag | T11 | DESIGN-v3 §H | 全量回归 + 覆盖率 ≥80% |

## 1.5 v3 实际进展（截至 2026-09-17）

> **重要更新**：原 v3 计划中的 T13/T14/T15-1 已实际在 v2.0.2 中完成。
> 这三项原计划排在 M6/M8 路线图，v2 收官时补齐后顺位调整为 v3 P0/P1。

| TODO | 原 v3 计划 | 实际进展 | 当前优先级 |
|---|---|---|---|
| T13 (M6 plugin gRPC) | v3 P0 | ✅ v2.0.2 完成 | — |
| T14 (M8 Anthropic ChatStream) | v3 P0 | ✅ v2.0.2 完成 | — |
| T15-1 (MCP Session) | v3 P1 | ✅ v2.0.2 完成 | — |
| T15-2 (Skill loader) | v3 P1 | ☐ 未启动 | **v4 P2**（0-ROADMAP §3 P1 降级） |
| T15-3 (Sub-agent sync) | v3 P1 | ☐ 未启动 | **v4 P0**（0-ROADMAP §3 P5） |
| T4 (Obs OTel) | v3 P0 | ☐ 未启动 | **v4 P0**（0-ROADMAP §3 P3） |
| T5/T6 (Compaction) | v3 P0 | ☐ 未启动 | **v4 P0**（0-ROADMAP §3 P4） |
| T7 (Audit) | v3 P1 | ☐ 未启动 | **v4 P1**（0-ROADMAP §3 P3） |
| T8 (Sandbox) | v3 P2 | ☐ 未启动 | **v4 不做**（0-ROADMAP §3 P5 排除） |
| T9/T10 (Ollama/Gemini) | v3 P1 | ☐ 未启动 | **v4 P1**（0-ROADMAP §3 P4） |
| T11 (Provider 路由) | v3 P1 | ☐ 未启动 | **v4 P1**（0-ROADMAP §3 P4） |

**v3 → v4 新增项**（借鉴 Java v0.1.7 + Node v0.1.6-alpha.2）：

| v4 新增 | 借鉴来源 | 0-ROADMAP 对应 |
|---|---|---|
| Session 事件流 v3 + 投影缓存 | Java v0.1.7 §4.1–§4.3 | P1 |
| 统一 Gateway SSE | Java v0.1.7 §5 | P2 |
| 插件库存视图 | Java v0.1.7 §7 | （v4 暂未排，延 v5+） |
| 测试用例库 | Java v0.1.7 §8.1 | P5 |

**结论**：v3 计划被 [0-ROADMAP.md](./0-ROADMAP.md) 取代。v3 本文件保留为历史快照。

## 2. 详细步骤

### T2 — Skill loader（1 天）

**做什么**：
1. `internal/skill/loader.go`：
   - `Skill struct{Name, Description, Trigger, Always, Body string}`
   - `LoadDir(dir string) ([]Skill, error)` 扫描 `*.md`，解析 YAML frontmatter + body
   - frontmatter 用 `gopkg.in/yaml.v3`（已有依赖）
2. `internal/skill/matcher.go`：
   - `Match(skills []Skill, prompt string) []Skill`：trigger 字符串包含（case-insensitive）
3. `internal/agent/runner.go`：
   - `LoopRunner.Skills []Skill` 字段
   - loop body 第 1 步：在构造 system message 前，调 matcher 拼接
4. `cmd/dsh/main.go`：`--skill-dir` flag；加载后赋给 runner.Skills
5. 测试：5 用例（frontmatter 解析 / body 截取 / mention 命中 / 多 mention / 缺字段报错）

**验收**：`go test ./internal/skill/... ./internal/agent/...` 全绿。

### T3 — Sub-agent sync（1 天）

**做什么**：
1. `internal/subagent/subagent.go`：
   - `Spawner` interface
   - `Sync{parent, child *agent.LoopRunner}` 实现
   - `Spawn(ctx, prompt) (string, error)`：child.Run → 等完成 → 返回 final text
2. `internal/tools/agent_spawn.go`：内置工具
   - 参数 `{prompt: string}`
   - 执行：subagent.Spawn → 返回 string
   - 错误：ctx canceled / panic → Result{IsError:true}
3. `cmd/dsh/main.go`：默认注册 `agent_spawn` 工具
4. 测试：4 用例（happy / panic / ctx cancel / max_depth=1）

**验收**：`go test ./internal/subagent/... ./internal/tools/...` 全绿。

### T4 — Obs OTel（1 天）

**做什么**：
1. `internal/obs/obs.go`：`Logger`、`Tracer`、`Span`、`Meter` 接口
2. `internal/obs/noop.go`：默认实现（空操作）
3. `internal/obs/otel.go`：OTel 实现
   - `NewOTel(ctx, cfg OTelConfig) (Tracer, error)`
   - 用 `go.opentelemetry.io/otel` + OTLP gRPC exporter
4. 接入点：Runner.Run / Client.Chat / Tool.Execute 三个位置包 span
5. 测试：3 用例（noop 默认 / otel 构造 / span attrs 完整）

**验收**：`go test ./internal/obs/...` 全绿；`go.mod` 加 otel 依赖。

### T5 — Compaction truncate（1.5 天）

**做什么**：
1. `internal/compaction/strategy.go`：`Strategy` interface + TokenCount 工具
2. `internal/compaction/truncate.go`：保留 system + 最近 N 条
3. `internal/compaction/compactor.go`：
   - `Compactor{Strategy, TriggerTokens}`
   - `Maybe(ctx, msgs) (newMsgs, compacted bool)`
4. `internal/agent/runner.go`：loop 每轮开头调 `compactor.Maybe`
5. 测试：4 用例（不触发 / 触发后保留 system / keep-recent 命中 / token count 估算）

### T6 — Compaction llm-summary（0.5 天）

**做什么**：
1. `internal/compaction/llmsummary.go`：
   - 用 LLM client 调一次 Chat
   - prompt: "请用 200 字总结以下对话" + 丢的部分 messages
2. 测试：1 用例（mock LLM 返回摘要；摘要插回原位）

### T7 — Audit log（0.5 天）

**做什么**：
1. `internal/audit/event.go`：Event struct 字段
2. `internal/audit/log.go`：
   - `FileLogger{path, redact bool}`
   - `Log(ctx, ev)` 追加一行 JSONL
   - `Close()` flush + close
3. `internal/audit/redact.go`：SHA-256(args) → hex
4. 接入：runner / tool 在关键点 log
5. 测试：3 用例（写 JSONL / redact vs raw / 多 session 并发）

### T8 — Sandbox（2 天）

**做什么**：
1. `internal/sandbox/sandbox.go`：`Sandbox` interface + NoopSandbox
2. `internal/sandbox/windows_acl.go`：`//go:build windows`；用 `golang.org/x/sys/windows` ACL API
3. `internal/sandbox/linux_ns.go`：`//go:build linux`；用 `syscall.SysProcAttr` 设 namespace flags
4. 接入：`internal/tools/shell.go` 在 exec 前调 sandbox.Apply
5. 测试：3 用例（noop 不变 / win ACL 接口 mock / linux ns 接口 mock）

### T9 — Ollama provider（1 天）

**做什么**：
1. `internal/llm/ollama/ollama.go`：
   - `Client{BaseURL, Model, HTTPClient}`
   - `Chat` + `ChatStream`（Ollama 用 ndjson）
2. 测试：1 用例（httptest fixture）

### T10 — Gemini provider（1 天）

**做什么**：
1. `internal/llm/gemini/gemini.go`：
   - `Client{APIKey, Model, HTTPClient}`
   - `Chat` + `ChatStream`（走 `/v1beta/models/{model}:streamGenerateContent?alt=sse`）
2. 测试：1 用例（httptest fixture）

### T11 — provider 路由（0.5 天）

**做什么**：
1. `internal/llm/client.go`：扩展 `NewClient(cfg)` 按 `cfg.Provider` 路由到 openai/anthropic/ollama/gemini
2. `internal/config/config.go`：`LLMConfig.Provider` 加 `ollama`/`gemini` 验证
3. 测试：1 用例（每种 provider 都能 New）

### T12 — v3 收尾（1 天）

**做什么**：
1. 更新 README（添加 Skill / Sub-agent / Obs / Compaction / Audit / Sandbox / Ollama / Gemini 段）
2. 更新 `harness.example.yml`（所有 v3 新段）
3. `go test ./... -count=1` 全绿 + `go vet` 0 warning
4. 写 `docs/RELEASE-v3.md`
5. `cmd/dsh/main.go` version → `0.3.0-dev`

## 3. 验收清单（do-not-merge-each-step）

- 每步完成时 `go test -count=1 ./...` 必须全绿
- 每步 `go vet ./...` 必须 0 warning
- 新代码 `go test -cover` 必须 ≥ 80%
- `RELEASE-v3.md` 必须含每项功能的"实际"状态

## 4. 风险与缓解

| 风险 | 缓解 |
|---|---|
| OTel SDK 体积大 | 默认 noop；OTel 在 build tag 后才链入 |
| Windows ACL 编译要求 win | build tag；其他平台 stub |
| Linux ns 编译要求 cgo | build tag；其他平台 stub |
| Ollama/Gemini 协议变动 | 抽 adapter |
| Compaction 让 LLM"失忆" | prompt 注入压缩说明 + 保留 system + 保留最近 N |
| Audit log 体积 | 默认 redact；logrotate 由用户自管 |

## 5. 与官方 Node 版差距（v3 后预计）

| 能力 | 上游 Node | dsh Go v3 |
|---|---|---|
| ReAct 核心 | ✅ | ✅ |
| REPL / HTTP / SSE | ✅ | ✅ |
| Session + 持久化 | ✅ | ✅ |
| Usage + cost | ✅ | ✅ |
| Approval + Shell | ✅ | ✅ |
| Plugin 系统 | ✅（100+） | ✅（3 in-tree + proto） |
| Anthropic Stream | ✅ | ✅ |
| MCP | ✅ | ✅（stdio 单 server） |
| Skill | ✅ | ✅ |
| Sub-agent | ✅（并发） | ✅（sync） |
| Obs | ✅ | ✅（OTel opt-in） |
| Compaction | ✅ | ✅ |
| Audit | ✅ | ✅ |
| OS sandbox | ✅（3 平台） | ⚠️ win + linux |
| 多渠道 | ✅（4+） | ✅（4 渠道） |
| Plan mode | ✅ | ❌（v4） |
| Persona | ✅ | ❌（v4） |
| Schedule | ✅ | ❌（v4） |
| Web UI / Desktop | ✅ | ❌（v5+） |

**v3 后总体对齐官方 Node 版约 55-60% 能力**（v2.0.2 是 25-30%，v3 翻一倍）。

## 6. 与 v2 的边界检查

- **不删 v2 任何公开 API**
- **不修改 v2 默认行为**（无新段 → v2 行为）
- **不破坏现有 14 包测试**：每步前必须先跑回归
- **不增加 v2 必修项**：v3 全是 opt-in（obs.provider=otel 等）

## 7. 时间线

```
Day 1:   T2 Skill loader
Day 2:   T3 Sub-agent
Day 3-4: T4 Obs OTel
Day 5-6: T5 Compaction truncate
Day 6.5: T6 Compaction llm-summary
Day 7:   T7 Audit log
Day 8-9: T8 Sandbox win+linux
Day 10:  T9 Ollama
Day 11:  T10 Gemini
Day 11.5:T11 provider 路由
Day 12:  T12 v3 收尾 + release note + README
```

合计 **12 天**（一人）。

---

## 7. v3 → v4 迁移指引

> **本文件已被 [0-ROADMAP.md](./0-ROADMAP.md) 取代**。
> - v4 在 v3 基础上**新增 4 项**（T1 事件流、T2 Gateway SSE、T3 插件库存、T4 测试用例库），借鉴 Java v0.1.7 与 Node v0.1.6-alpha.2
> - v4 把 v3 原 T8 (Sandbox) **删除**（借鉴 Java 不上 OS sandbox 而是 Python runtime 隔离思路；Go 版 v5+ 再考虑）
> - v4 把 v3 原 T15-2 (Skill) **降级 P2**
> - v4 把 v3 原 T13/T14/T15-1 标记为 ✅（已在 v2.0.2 中完成）
>
> **详细 v4 规划请直接阅读 [0-ROADMAP.md](./0-ROADMAP.md)**。

---

**v3.0.0-dev** — 2026-09-16 起 — **已被 v4 取代**
**v4 入口**：[0-ROADMAP.md](./0-ROADMAP.md)
