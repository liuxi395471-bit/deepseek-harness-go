# DeepSeek Harness 系列项目

本仓库聚合了 **DeepSeek Harness** 的多语言实现与上游版本，按目录并列：

| 子项目 | 语言 / 形态 | 状态 | 一句话定位 |
| --- | --- | --- | --- |
| [`deepseek-harness/`](./deepseek-harness) | TypeScript / Node.js（pnpm workspace） | 上游官方实现，活跃迭代 | 由 [DeepSeek AI](https://deepseek.com) 开发的开源 agent harness，基于 Cordis 的「一切皆插件」架构 |
| [`deepseek-harness-java/`](./deepseek-harness-java) | Java 17 + Spring Boot 3.3 + DDD 六边形 | 已实现，多模块可运行 | 基于 DDD 与端口-适配器的模块化 Agent 运行时，支持插件 / MCP / 审批 / 持久化 |
| [`deepseek-harness-go/`](./deepseek-harness-go) | Go（仅 `PLAN.md`） | 规划中，代码待落地 | 参照 Java 版实现一个最小可运行的 CLI 版 harness，聚焦 ReAct 闭环 |

---

## 1. 关系与定位

三个目录代表同一思路的**三种走向**：

- **上游（TS）**：cordis + plugin-first，覆盖 Web/CLI/Desktop 三端，体量最大、能力最全（session 事件溯源、工具目录、SSE 流式、landlock 沙箱、subagent、ACR/MCP/Codex 子进程桥等），是目前事实上的"参考实现"。
- **Java 版**：把上游思路用 DDD 重写为强分层（app / trigger / api / case / domain / infrastructure / types），把"插件"拆成 JAVA_NATIVE / DSH_NODE_BRIDGE / MCP 三类桥，单实例 Agent Harness 形态，最贴近企业 Java 栈。
- **Go 版（规划中）**：把 Java 版的核心闭环抽到最小可运行形态，**只做 ReAct + 内置工具 + CLI REPL**，作为"练手 + Go 实现参考"。

子项目间的依赖关系：

```text
deepseek-harness (TS, 上游)
        │  设计哲学：cordis / everything-is-a-plugin
        │  公开文档与协议
        ▼
deepseek-harness-java (Java, DDD 重写)
        │  内部架构（ReactLoopAgent / InMemoryToolRegistry / Tool 接口）
        │  直接映射
        ▼
deepseek-harness-go (Go, 计划中，按 1:1 搬运核心抽象)
```

Java 版与 Go 版**不依赖**上游 TS 代码，仅以"理解设计意图"为参照；上层的状态保存、插件协议、模型适配器列表等都会**独立演化**。

---

## 2. 子项目速读

### 2.1 [`deepseek-harness/`](./deepseek-harness)

- **技术栈**：Node.js ≥22.19 / ≥24，TypeScript 6，pnpm 11 workspace，tsdown + tsc 双构建，vitest 全量测试。
- **形态**：CLI / Web / Desktop 三端；Cordis 容器 + 一系列 `packages/*` 能力包（agent / llm / tool / session / sandbox / subagent / cordis / sdk …）。
- **启动**：`pnpm run build && pnpm dsh web`（默认 `http://127.0.0.1:3080`）。
- **关键文档**：`docs/architecture.md`、`docs/development.md`、`AGENTS.md`、`CLAUDE.md`；产物目录 `.dsh-build/`、归档的设计笔记 `.agents/notes/{implemented,proposed,archived}/`。

### 2.2 [`deepseek-harness-java/`](./deepseek-harness-java)

- **技术栈**：JDK 17、Maven 3.9、Spring Boot 3.3、MyBatis、H2（standalone）/ MySQL（默认）、原生 JS Web 控制台。
- **形态**：10 个 Maven 模块，严格按端口-适配器分层：

  ```text
  app → trigger → api/case → domain ← infrastructure
  domain → types(SPI) ← plugins
  ```

- **端口**：`http://localhost:8090/`（Standalone profile = H2；默认 profile = MySQL）。
- **核心能力**：ReAct 循环（`ReactLoopAgent`）、工具系统（`fs_*` / `shell_execute` / `web_*` / `ask_user_question`）、双模式插件（Java Native JAR + Node sidecar JSON-RPC）、MCP stdio 适配、会话事件溯源、SSE 流式对话、任务提交 + 权限矩阵 + 审批、Docker Compose 一键拉起。
- **关键文档**：`README.md` 章节 1–15（含架构图、领域设计、插件开发指南、生产化注意事项、当前边界与已知隐患），`docs/md/{domain-design,domain-modules-reference,domain-course}/` 系列。

### 2.3 [`deepseek-harness-go/`](./deepseek-harness-go)

- **当前文档**：[`PLAN.md`](./deepseek-harness-go/PLAN.md)（实施规划：目标、范围、阶段 M0–M4、技术选型、风险）+ [`DESIGN.md`](./deepseek-harness-go/DESIGN.md)（第一版设计：接口签名、状态机、错误模型、取消链、可执行的 Runner 伪码）。
- **目标**：以 Java 版核心抽象为蓝本，用 Go 实现 CLI 形态的最小可运行 agent harness。
- **规划范围**（首版交付）：项目骨架 + config、`llm.Client`（OpenAI 兼容）、`tool.Registry` + 3 个内置工具（`greet`/`fs_read`/`fs_write`）、`agent.Runner` ReAct 循环、CLI REPL 入口。
- **明确排除**（首版不做）：Web UI / HTTP、插件系统、Session 持久化、Approval / 沙箱 / MCP / Skill、subagent。
- **后续里程碑**：M5 HTTP+最小 Web、M6 插件系统、M7 Session 持久化 + Approval + shell 工具。

---

## 3. 共有的核心抽象

| 抽象 | 上游 TS（`deepseek-harness`） | Java 版 | Go 版（规划） |
| --- | --- | --- | --- |
| ReAct 循环 | `packages/agent` + loop / phase 状态机 | `ReactLoopAgent` | `agent.Runner` |
| 工具接口 | Tool interface（name/parameters/execute） | `AbstractTool` + `ToolDefinition` | `tool.Tool` interface |
| 工具注册表 | `ToolRegistry`（cordis scope） | `InMemoryToolRegistry` | `tool.Registry` |
| LLM 客户端 | `packages/llm`（Cordis Providers） | `DeepSeekAdapter` / `OpenAiCompatibleAdapter` | `llm.Client`（OpenAI 兼容 REST） |
| 配置 | `harness.yml` + env 覆盖 + 数据库 | `harness.yml` + `application.yml` + Spring 注入 | `harness.yml` + env 覆盖 |
| 入口 | `apps/cli`、`apps/web`、`apps/desktop` | Spring Boot `Application.main` | `cmd/dsh/main.go` |

注：上游 TS 的能力远多于 Java / Go 版（如 session 事件溯源、landlock 沙箱、subagent、ACR、Skill、Marketplace、Codex 子进程桥等），其余两版只覆盖子集。

---

## 4. 仓库布局

```text
deepseek-harness-all/
├── README.md                    # 本文档
├── deepseek-harness/            # 上游官方 TS/Node 版
├── deepseek-harness-java/       # Java 17 + Spring Boot 3 + DDD 实现
└── deepseek-harness-go/         # Go 版（仅 PLAN.md，待实施）
```

每个子项目都是**独立工程**：各自有自己的 `package.json` / `pom.xml` / `go.mod`（计划）、独立的依赖、独立构建，互不交叉引用。

---

## 5. 通用开发约定

- 所有子项目均处于 **开发者预览阶段**，接口、协议与目录结构仍在快速迭代；跨语言跟进需以**最新版文档**为准。
- **配置约定**：三个项目的环境变量都以 `LLM_API_KEY` / 模型 base-url 等作为模型凭据入口；写入 yml 时切勿硬编码 API Key，使用 `${LLM_API_KEY}` 占位。
- **运行期风险**（来自 Java 版与上游 README 的共同提示）：
  - 默认未启用运行期审批；高风险工具（`shell_execute` / `fs_write` / `plugin.run` / 子进程）需要先评估审批策略再上线；
  - 任何接受网络访问的入口务必配置 API Key，不要在本地未鉴权时暴露到公网；
  - 模型接口需兼容 OpenAI Chat Completions 或 Anthropic Messages，请按所选模型适配协议。

---

## 6. 快速导航

- 想了解 **DeepSeek 官方 agent harness 是什么** → 上游 [`deepseek-harness/README.md`](./deepseek-harness/README.md) 与 [`docs/architecture.md`](./deepseek-harness/docs/architecture.md)。
- 想跑通 **Java 版 Spring Boot Harness** → [`deepseek-harness-java/README.md`](./deepseek-harness-java/README.md) §2 快速体验（Standalone H2 / MySQL / Docker Compose）。
- 想看 **DDD / 六边形 / 端口-适配器** 怎么落地 Agent → [`deepseek-harness-java/README.md`](./deepseek-harness-java/README.md) §3–5 与 `docs/md/domain-design.md`。
- 想在 **Java 版上做插件** → [`deepseek-harness-java/README.md`](./deepseek-harness-java/README.md) §7（Java Native / Node Bridge / MCP）以及 `docs/md/java-plugin-development.md`。
- 想看 **Go 版的实施规划与设计** → [`deepseek-harness-go/PLAN.md`](./deepseek-harness-go/PLAN.md)（M0–M4 阶段、首版范围、风险）与 [`deepseek-harness-go/DESIGN.md`](./deepseek-harness-go/DESIGN.md)（接口签名、ReAct 状态机、错误/取消模型）。

---

## 7. License

各子项目各自声明 license，请进入对应目录查看 `LICENSE` / `THIRD_PARTY_NOTICES.md`。上游 TS 版为 MIT；Java 版以仓库内声明为准。
