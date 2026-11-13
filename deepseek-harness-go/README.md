# DeepSeek Harness Go 版（`dsh`）

参照 `deepseek-harness-java/` 的核心抽象用 Go 实现的 **ReAct agent harness**。
给 LLM 一组工具，让它在有界循环里反复「思考 → 调工具 → 读结果 → 再思考」，直到给出最终答案。

**当前版本**：v3.0.0-dev（`develop` 分支）
**状态**：24 个包测试全绿，`go vet` 0 warning，整体语句覆盖率 71.9%

📖 [架构设计](./docs/ARCHITECTURE.md) · [测试与验收](./docs/TESTING.md)

---

## 特性

- **ReAct 循环**：`LLM → tool_calls → tool result → LLM`，单 turn 内最多 N 轮（默认 16）。
- **流式 LLM**：`ChatStream` + `AssistantDelta` 事件，REPL 实时打字输出。
  - OpenAI 兼容 SSE ✅
  - Anthropic Messages 原生 SSE ✅
  - Ollama ndjson 流 ✅
  - Gemini generateContent SSE ✅
- **多渠道 LLM**：`llm.provider` 支持 `openai`（默认）/ `anthropic` / `ollama` / `gemini`。
- **Skill 系统**：`Markdown + YAML frontmatter`，mention 关键词或 always-on 触发，命中后注入 system prompt。
- **Sub-agent**：`agent_spawn(prompt)` 同步子 agent，共享父级工具，禁止嵌套（max_depth=1）。
- **上下文压缩**：`truncate` / `llm-summary` 两策略；system 永远保留，tool 结果不与调用拆散。
- **可观测性**：`Logger` / `Tracer` / `Span` / `Meter` 接口族，默认 no-op 零开销，OTel opt-in。
- **审计日志**：JSONL 事件流，默认只记参数 SHA-256 摘要（脱敏）。
- **OS 沙箱**：Windows 受限令牌 / Linux namespaces，敏感路径拒绝；失败不静默降级。
- **HTTP+SSE 服务**：`/api/agent/message` + `/api/agent/stream` + `/api/sessions` + `/api/sessions/{id}` + `/healthz`。
- **Session 持久化**：内存 `MapStore` + 磁盘 `SQLiteStore`（纯 Go，无需 CGO）；支持导出/导入。
- **Approval + Shell**：4 种 Approver；shell 工具受 allowlist + workspace 围栏 + 审批 + 沙箱四重约束。
- **gRPC 插件系统**：`--plugin` 可重复；崩溃自动重启；工具经审批关口。
- **MCP stdio**：Initialize + `tools/list` + `tools/call` 高层 Session API。
- **取消传播**：`SIGINT` / `SIGTERM` → `signal.NotifyContext` → 整条链路。
- **错误不冒泡**：工具失败以 `[ERROR]` 角色消息回填给模型，让它带着新信息继续推理。

**三方依赖**：`gopkg.in/yaml.v3` · `modernc.org/sqlite`（纯 Go）· `google.golang.org/grpc` + `protobuf` · `go.opentelemetry.io/otel`（opt-in）

---

## 快速开始

### 1. 准备配置

```bash
cp harness.example.yml harness.yml
```

编辑 `harness.yml`，把 `base-url` 换成你的 OpenAI 兼容端点（如 `http://127.0.0.1:8777/v1`），
把 `model` 换成模型名。

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
dsh 3.0.0-dev — workspace=/path/to/.dsh/workspace model=glm-5.3-flash provider=openai
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

文件工具被限制在 `agent.workspace` 目录内，路径逃逸会被拒绝。

```text
dsh> 请把"明天放假"写到 notes/holiday.txt
  → tool fs_write({"path":"notes/holiday.txt","content":"明天放假"})
  ← fs_write (ok, 1.2ms)
wrote 12 bytes to notes/holiday.txt

dsh> 读 notes/holiday.txt
  → tool fs_read({"path":"notes/holiday.txt"})
  ← fs_read (ok, 215µs)
明天放假
```

### 5. 一次性命令模式

```bash
./dsh -config ./harness.yml -prompt "请使用 greet 工具向 Ada 打招呼"
```

### 6. 调试模式

```bash
./dsh -config ./harness.yml -debug
```

### 7. 取消当前轮

按 `Ctrl+C` 取消**当前轮**（不退出 REPL）；按 `Ctrl+D` 干净退出。

---

## 命令行 flag

| flag | 默认 | 说明 |
| --- | --- | --- |
| `-config` | `harness.yml` | YAML 配置文件路径；不存在则走默认值 + env |
| `-prompt` | `""` | 非空时一次性执行并退出（不走 REPL） |
| `-serve` | `false` | 启动 HTTP+SSE 服务；需要 `server.auth-token` |
| `-plugin` | `""` | 插件可执行文件路径；**可重复**；spawn 子进程 + gRPC 接入 |
| `-debug` | `false` | 打印所有 agent 事件到 stderr |
| `-audit` | `""` | 审计 JSONL 路径；**优先于** `audit.path`；空 = 关闭 |

---

## Web 控制台

`dsh -serve` 除了提供 HTTP+SSE API，还会在同一端口托管一个
**ChatGPT 风格**的 Web 控制台（前端源码在 `web/`，构建产物通过
`go:embed` 内嵌进二进制）。

### 构建前端

```bash
cd web
npm install
npm run build      # 产物输出到 ../internal/webui/dist
```

未构建时 `-serve` 仍可正常提供 API，只是没有页面（启动日志会提示）。

### 启动

```bash
export DSH_SERVER_AUTH_TOKEN=your-secret-token
./dsh -config ./harness.yml -serve
# 浏览器打开 http://127.0.0.1:8080/
```

首次进入需要输入令牌（对应 `server.auth-token`）。令牌保存在
localStorage，之后刷新无需重新输入。

### 前端开发模式

```bash
cd web
npm run dev        # http://127.0.0.1:5173
```

Vite 会把 `/api` 与 `/healthz` 代理到 `127.0.0.1:8080`，因此
开发时**需要同时**运行 `dsh -serve`（只提供 API）。

### 功能

| 功能 | 说明 |
| --- | --- |
| 会话侧边栏 | 列出已持久化会话；支持搜索、切换、从列表移除 |
| 流式输出 | SSE 实时渲染；`Enter` 发送，`Shift + Enter` 换行 |
| 工具调用卡片 | 展示工具名、入参 JSON、返回结果与耗时，可展开折叠 |
| Markdown 渲染 | GFM 语法 + 代码高亮（输出经 DOMPurify 消毒） |
| 明暗主题 | 跟随系统偏好，可手动切换并记忆 |
| 用量统计 | 每轮显示输入 / 输出 / 总 token 数 |

### 目录结构

```
web/
├── index.html
├── vite.config.ts         # 构建输出到 ../internal/webui/dist
└── src/
    ├── api/
    │   ├── client.ts      # REST + SSE（fetch 流，非 EventSource）
    │   └── types.ts       # 与 Go 侧一一对应的类型定义
    ├── components/
    │   ├── App.vue        # 根组件（布局编排）
    │   ├── Composer.vue   # 输入框
    │   ├── LoginView.vue  # 令牌登录
    │   ├── MessageBubble.vue  # 消息 + 工具卡片
    │   └── Sidebar.vue    # 会话列表
    ├── composables/
    │   └── useChat.ts     # 全局状态与 SSE 事件分发
    ├── styles/            # 设计令牌与全局样式
    └── utils/
        └── markdown.ts    # marked + highlight.js + DOMPurify
```

> **为什么用 fetch 而不是 EventSource？**
> 浏览器原生 `EventSource` 不支持自定义请求头，无法携带
> `Authorization: Bearer <token>`，因此 `api/client.ts` 用
> `fetch` + `ReadableStream` 手动解析 SSE 帧。

---

## REPL 斜杠命令

| 命令 | 说明 |
| --- | --- |
| `/help` | 显示可用命令 |
| `/clear` | 清屏（不删除历史） |
| `/model <name>` | 切换运行时模型 |
| `/stream` | 切换流式模式 |
| `/usage` | 打印累计 token 用量 |
| `/sessions` | 列出已持久化的会话 |
| `/history [N\|<sid>]` | 打印历史 / 切换到指定会话 |
| `/skills` | 列出已加载的 skill |

---

## 配置项

加载优先级：**默认值 → YAML → 环境变量**（后者覆盖前者）。
完整示例见 [harness.example.yml](./harness.example.yml)。

### `llm`

| key | env | 默认 |
| --- | --- | --- |
| `provider` | `DSH_LLM_PROVIDER` | `openai` |
| `base-url` | `DEEPSEEK_BASE_URL` | `http://127.0.0.1:8777/v1` |
| `api-key` | `DEEPSEEK_API_KEY` | `""` |
| `model` | `DEEPSEEK_DEFAULT_MODEL` | `glm-5.3-flash` |
| `max-tokens` | `DEEPSEEK_MAX_TOKENS` | `8192` |
| `timeout` | `DEEPSEEK_TIMEOUT` | `120s` |

### `agent`

| key | env | 默认 |
| --- | --- | --- |
| `max-rounds` | `DSH_AGENT_MAX_ROUNDS` | `16` |
| `workspace` | `DSH_AGENT_WORKSPACE` | `./.dsh/workspace` |
| `temperature` | `DSH_AGENT_TEMPERATURE` | `0.2` |
| `system-prompt` | `DSH_AGENT_SYSTEM_PROMPT` | `""`（内置中文模板） |
| `debug` | `DSH_AGENT_DEBUG` | `false` |

### `server`

| key | env | 默认 |
| --- | --- | --- |
| `enabled` | `DSH_SERVER_ENABLED` | `false` |
| `listen` | `DSH_SERVER_LISTEN` | `127.0.0.1:8080` |
| `auth-token` | `DSH_SERVER_AUTH_TOKEN` | `""`（`enabled=true` 时必填） |
| `request-timeout` | `DSH_SERVER_TIMEOUT` | `0`（不超时） |
| `max-concurrent-sessions` | `DSH_SERVER_MAX_SESSIONS` | `16` |

### v3 新增段

| 段 | key | env | 默认 |
| --- | --- | --- | --- |
| `skills` | `dir` | `DSH_SKILLS_DIR` | `""`（= `~/.dsh/skills`） |
| `obs` | `provider` | `DSH_OBS_PROVIDER` | `noop` |
| `obs` | `endpoint` | `DSH_OBS_OTEL_ENDPOINT` | `localhost:4317` |
| `obs` | `service-name` | `DSH_OBS_OTEL_SERVICE_NAME` | `dsh` |
| `obs` | `sample-ratio` | `DSH_OBS_OTEL_SAMPLE_RATIO` | `1.0` |
| `compaction` | `enabled` | `DSH_COMPACTION_ENABLED` | `false` |
| `compaction` | `trigger-tokens` | `DSH_COMPACTION_TRIGGER_TOKENS` | `16000` |
| `compaction` | `strategy` | `DSH_COMPACTION_STRATEGY` | `truncate` |
| `compaction` | `keep-recent` | `DSH_COMPACTION_KEEP_RECENT` | `6` |
| `audit` | `path` | `DSH_AUDIT_PATH` | `""`（关闭） |
| `audit` | `full` | `DSH_AUDIT_FULL` | `false`（脱敏） |
| `sandbox` | `provider` | `DSH_SANDBOX_PROVIDER` | `noop` |
| `shell` | `enabled` | `DSH_SHELL_ENABLED` | `false` |
| `shell` | `allowlist` | `DSH_SHELL_ALLOWLIST` | `[]`（拒绝一切） |
| `shell` | `timeout` | `DSH_SHELL_TIMEOUT` | `30s` |

---

## v3 能力详解

### Skill 系统

skill 是 `Markdown + YAML frontmatter` 文件，放在 `~/.dsh/skills/*.md`（或 `skills.dir` 指定的目录）：

```markdown
---
name: code-review
description: 用户要求 review 代码时注入
trigger: "review"
always: false
---

# 代码风格
- 函数 < 50 行
- 错误不冒泡到 Runner
```

`trigger` 作为子串出现在用户输入中即命中（大小写不敏感）；`always: true` 则每轮命中。
命中的 body 以 `<skill name="...">` 拼接到 system prompt 尾部，每轮重新评估。

### Sub-agent

模型可调用 `agent_spawn(prompt)` 起一个同步子 agent：共享父级的 LLM client 与工具注册表，
返回最终答案作为一次 tool result。嵌套被双重禁止（子注册表剔除 `agent_spawn` + ctx 深度检查）。

### 上下文压缩

```yaml
compaction:
  enabled: true
  trigger-tokens: 16000
  strategy: llm-summary   # truncate | llm-summary
  keep-recent: 6
```

超出阈值时压缩到约一半：`truncate` 插入占位提示，`llm-summary` 用同一个 LLM 摘要被丢弃的区间。
system 永远保留；tool 结果不会与它的 assistant 调用拆散。压缩只影响发给 LLM 的序列，落盘历史不变。

### 审计日志

```bash
./dsh -config ./harness.yml --audit audit.jsonl
```

每行一个事件：`{"ts":..., "session_id":..., "event":"tool_call", "tool":..., "args_hash":...}`。
默认只记参数的 SHA-256 摘要；`audit.full=true` 才记录原文。

### OS 沙箱

```yaml
sandbox:
  provider: auto     # noop | windows_acl | linux_ns | auto
```

Windows 用受限令牌（`CreateRestrictedToken`）并拒绝敏感路径；Linux 用 namespaces 隔离并做
uid/gid 映射。沙箱在 shell 工具执行前施加约束，**失败绝不静默降级**为无沙箱执行。

---

## 目录结构

```text
deepseek-harness-go/
├── README.md
├── go.mod / go.sum
├── harness.example.yml        # 配置示例（可直接加载）
├── cmd/
│   └── dsh/                   # 主 CLI
│       ├── main.go            # 装配：config → llm → tools → subagent → runner
│       └── repl/              # REPL（斜杠命令 + 事件渲染）
├── docs/
│   ├── ARCHITECTURE.md        # 架构设计（本文档集的核心）
│   ├── TESTING.md             # 测试与验收
│   └── examples/
│       └── greet-session.md   # 端到端冒烟示例
├── internal/
│   ├── config/                # 配置加载（YAML + env + 校验）
│   ├── llm/                   # LLM 适配层
│   │   ├── anthropic/         # Anthropic Messages + 原生 SSE
│   │   ├── gemini/            # Google generateContent
│   │   ├── ollama/            # Ollama /api/chat
│   │   └── provider/          # provider 路由
│   ├── tool/                  # Tool 接口 + 线程安全 Registry
│   ├── tools/                 # 内置：greet / fs_read / fs_write / shell
│   ├── agent/                 # ReAct Runner + 事件定义
│   ├── approval/              # 4 种 Approver + 链 + 会话缓存
│   ├── sandbox/               # noop / Windows ACL / Linux ns
│   ├── store/                 # Map + SQLite + export/import
│   ├── usage/                 # token 聚合 + cost
│   ├── server/                # HTTP + SSE 服务端
│   ├── stream/                # SSE 解析 + 重组
│   ├── skill/                 # skill 加载与匹配
│   ├── subagent/              # 同步子 agent
│   ├── obs/                   # Logger / Tracer / Span / Meter（no-op 或 OTel）
│   ├── compaction/            # truncate / llm-summary
│   ├── audit/                 # JSONL 审计日志
│   ├── plugin/                # gRPC 插件宿主 + 客户端
│   │   └── proto/             # tool_provider.proto + 生成代码
│   ├── mcp/                   # MCP stdio 传输 + Session
│   └── e2e/                   # 端到端冒烟测试
└── tools/                     # protoc 工具链（本地）
```

---

## 测试

```bash
go test ./... -count=1            # 全量测试
go test ./... -count=1 -cover     # 带覆盖率
go vet ./...                      # 静态检查
```

当前：**285** 个测试用例、**24** 个包全绿，整体覆盖率 **71.9%**。
详见 [docs/TESTING.md](./docs/TESTING.md)。

---

## 当前边界

- ReAct 循环、Session 持久化、HTTP+SSE、Approval+Shell、Usage、多渠道 LLM ✅
- Skill / Sub-agent / Obs(OTel) / Compaction / Audit / Sandbox ✅
- MCP stdio Session ✅ · gRPC 插件系统 ✅
- CLI REPL + HTTP 双前端；**无 Web UI**
- REPL 的 `/history <N>` 尚未真正打印历史消息（`/history <sid>` 会话切换可用）
- `cmd/dsh` 当前装配空价格表，`usage` 的成本恒为 0
- `server.request-timeout` / `max-concurrent-sessions` 是软上限，不强制拒绝
- Linux namespace 沙箱需相应权限；CI 只做交叉编译校验
- 插件协议无版本协商

---

## License

内部实现，与同仓库 `deepseek-harness`（MIT）和 `deepseek-harness-java` 保持各自 license。
