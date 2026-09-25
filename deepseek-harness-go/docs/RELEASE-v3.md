# dsh v3.0.0 Release Notes（DESIGN-v3 交付）

对照 [DESIGN-v3.md](./DESIGN-v3.md) 的交付清单。v2 公开签名 / 接口 / CLI 全部冻结（§0 P1），v3 只在 v2 之上加层。

## 新增能力

### §A Skill System — `internal/skill`
- Markdown + YAML frontmatter skill 文件（`name` / `description` / `trigger` / `always` / `body`）。
- 目录：`skills.dir` 配置，默认 `~/.dsh/skills/*.md`。
- 触发：mention 关键词（大小写不敏感）与 always-on；命中 body 注入 system prompt。
- frontmatter 缺字段报错不静默；目录不存在返回空列表。
- REPL 新增 `/skills` 命令列出已加载 skill。

### §B Sub-agent (Sync) — `internal/subagent`
- 内置工具 `agent_spawn(prompt)`：同步子 LoopRunner，共享父 LLM client 与工具注册表。
- 子注册表剔除 `agent_spawn` + ctx depth 检查双重禁止嵌套（max_depth=1）。
- panic / 超时 / 取消统一折叠为 `tool.Result{IsError:true}`。

### §C Observability (OTel) — `internal/obs`
- `Logger / Tracer / Span / Meter` 接口族；默认全 no-op 零开销。
- `obs.provider=otel` 启用 OTLP gRPC tracer + meter（endpoint / service-name / sample-ratio 可配）。
- Runner span：`agent.run`、`llm.chat`（model / tokens attrs）、`tool.execute`（tool name）。
- debug 模式日志走 stderr logger。

### §D Context Compaction — `internal/compaction`
- `Compactor.Maybe` 超阈值触发；`truncate` / `llm-summary` 两策略。
- system 永远保留、最近 `keep-recent` 条保留、tool 结果不与 assistant 调用拆散。
- 压缩位置插入 summary placeholder；`llm-summary` 用同一 LLM client 摘要。
- 新事件 `agent.Compacted{Before, After}`；压缩只影响发给 LLM 的序列，Store 不动。

### §E Audit Log — `internal/audit`
- JSONL 文件日志：`{ts, session_id, event, ...}`。
- 事件：`tool_call`（round/tool/args_hash[/args_raw]）、`llm_call`（model/tokens）。
- 默认 redact 只记 SHA-256 hash；`audit.full=true` 记原文；CLI `--audit` 优先于配置。

### §F OS Sandbox — `internal/sandbox`
- `Sandbox` 接口 + `noop`（与 v2 行为一致）。
- `windows_acl`：`CreateRestrictedToken`（DISABLE_MAX_PRIVILEGE）+ System32 等敏感路径拒绝。
- `linux_ns`：`CLONE_NEWNS|NEWPID|NEWUSER` + uid/gid 映射；`/etc/shadow` 等敏感路径拒绝。
- 接入 shell 工具：执行前 `Apply(cmd)`，失败以 `IsError` 呈现不静默降级。

### §G 多渠道 LLM — `internal/llm/provider`
- provider 路由：`openai`（默认）| `anthropic` | `ollama`（ndjson 流）| `gemini`（SSE 流）。
- Ollama：`/api/chat`，tool_calls 双向映射，prompt/eval token 计数。
- Gemini：`generateContent` / `streamGenerateContent?alt=sse`，system→systemInstruction、
  assistant→model、tool_calls→functionCall/Response 映射，累积文本转 delta。
- 未支持 provider 启动即报错并列出支持列表。

## 配置增量

见 [harness.example.yml](../harness.example.yml)：`llm.provider`、`skills`、`obs`、
`compaction`、`audit`、`sandbox`、`shell`。

## 三方依赖（§J 白名单内）

- `go.opentelemetry.io/otel` + `sdk/trace` + `sdk/metric` + `otlptracegrpc`（opt-in）
- `golang.org/x/sys`（已有，Windows ACL 使用）

## 验收状态（§H 矩阵）

| 子能力 | 自动测试 | 结果 |
|---|---|---|
| Skill loader | 假 skill.md + mention 匹配 | ✅ 94.5% cov |
| Sub-agent (sync) | mock 子 agent（含 panic/超时/嵌套） | ✅ 93.5% cov |
| Obs (OTel) | NewOTelTracer + instrument | ✅ 81.9% cov |
| Compaction | 触发后消息数减少 / system 保留 / keep-recent | ✅ 87.8% cov |
| Audit | tool_call→jsonl / redact / 多 session 并发 | ✅ 90.0% cov |
| Sandbox win | 受限令牌真实子进程 + System32 拒绝 | ✅（linux 交叉编译通过） |
| Ollama | mock ndjson fixture round-trip | ✅ 82.6% cov |
| Gemini | mock SSE fixture delta 累积 + finish | ✅ 85.8% cov |

CLI 端到端冒烟：skill mention 注入（mock 回显验证）、`--audit` JSONL（redact 只记 hash）、
四种 provider 路由错误/端点正确性。全部 24 包 `go test ./...` 通过；`go vet ./...` 干净。
