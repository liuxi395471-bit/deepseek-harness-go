# DeepSeek Harness Go — DESIGN-v8（Web Console + Desktop）

> **版本**：DESIGN-v8 v0.1（2026-09-27）
> **状态**：📝 设计稿（待 PHASE-8-PLAN 启动）
> **配套**：`docs/0-ROADMAP.md §v8` · `docs/v8/PHASE-8-PLAN.md`
> **依赖**：v7.0.0（tag `cc1dd87`）

---

## §0 一句话目标

> **把 ds-go 从"能用"提升到"愿意每天打开"**：原生 Web 控制台 + 可选桌面打包，让"会话 / 插件 / 模型 / 审批 / 任务"五件事都能在浏览器里完成。

---

## §1 范围与决策

### §1.1 用户决策（2026-09-27）

| 决策项 | 选择 | 备选 |
|---|---|---|
| Web 控制台技术栈 | **Vite + Vue 3 SPA**（推荐项 #2） | 原生 JS + embed.FS；Go WebAssembly |
| 可选子项 | **核心 5 页 + Desktop 打包（Wails）**（推荐项 #4） | 仅核心 / +Spill / +Browser-Use |
| 数据库 / API | **复用 v4 Gateway + v5 SQLite**（不引入新存储） | — |
| 鉴权 | **绑定 `DSH_TOKEN` env**（最小可用），v8.1 升级到本地用户 | — |
| 国际化 | **zh-CN 优先**（与 ds-java 控制台一致），en-US 留 v8.1 | — |

> 决策依据：ds-ts `apps/web` 已是 Vite + Vue 栈，ds-java 是原生 JS 路线；选择 Vite + Vue 是为了**复用上游心智 + 避免从零搭建组件库**，同时保持前端构建产物可审计。

### §1.2 范围（v8.0）

| 子项 | 包含 | 不包含（v8.1+） |
|---|---|---|
| Web 控制台 | 5 页：会话列表 / 会话详情 / 插件管理 / 模型设置 / 任务队列 | 用户系统 / RBAC / 审计导出 / Webhook 配置 |
| Desktop 打包 | Wails 模板 + 一键构建脚本 | 自动更新 / 多窗口 / Tray / 通知中心 |
| 后端 | 新增 `internal/console/`（与 Gateway 解耦），复用 store / plugin / runtime | 新增独立后端进程 |
| 数据 | SQLite（已有），`console_state` 单表（持久化 UI 偏好） | 全文检索 / 时序指标 |

### §1.3 范围（v8.1 / 候选）

- v8.1：Spill 流式输出增强（事件驱动折叠 / 重连断点续传）
- v8.1：用户系统 + 简单口令登录（`DSH_AUTH=local`）
- v8.1：Browser-Use 可视化（截图回传 / 标注面板）
- v8.1：PTC-Runtime 预览（任务甘特图）

---

## §2 整体架构

### §2.1 系统拓扑

```
┌─────────────────────────────────────────────────────────────┐
│  Desktop（Wails v2 + 系统 WebView）                          │
│   ├─ 打开系统浏览器  →  http://127.0.0.1:7777                │
│   └─ Tray 菜单 → Start / Stop / Open Folder                 │
└──────────────────────────┬──────────────────────────────────┘
                           │ HTTP / SSE
┌──────────────────────────▼──────────────────────────────────┐
│  Browser（Vue 3 SPA，Vite 构建产物 → Go embed.FS）           │
│  ┌──────────┬──────────┬──────────┬──────────┬────────────┐  │
│  │ Sessions │ Plugins  │ Models   │ Tasks    │ Approval*  │  │
│  └────┬─────┴────┬─────┴────┬─────┴────┬─────┴────────────┘  │
│        └──────────┴────┬─────┴──────────┘                     │
│                vue-query / Pinia / Vue Router                │
└──────────────────────────┬──────────────────────────────────┘
                           │ /api/v1/* JSON + /api/v1/events SSE
┌──────────────────────────▼──────────────────────────────────┐
│  ds-go 进程（单二进制）                                       │
│  ┌──────────────────────────────────────────────────────┐   │
│  │ internal/console/  ← v8 新增                            │   │
│  │   ├─ console.go    路由 + 中间件（鉴权 / 日志）         │   │
│  │   ├─ embed.go      //go:embed dist → SPA shell        │   │
│  │   ├─ handler_*.go  5 页对应的 REST 聚合接口           │   │
│  │   └─ ws_proxy.go   WebSocket 升级（SSE fallback）      │   │
│  └──────────────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────────────┐   │
│  │ v4 Gateway  ←─ 复用：/events SSE 流（v7 已稳定）      │   │
│  └──────────────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────────────┐   │
│  │ v5-v7 全部 internal/* 包  ←─ 全部零改动复用           │   │
│  └──────────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────────┘
```

### §2.2 构建产物分层

```
deepseek-harness-go/
├── web/                          ← v8 新增（Vue 3 源码 + 构建配置）
│   ├── package.json              （vite / vue / pinia / vue-query / marked）
│   ├── vite.config.ts            （base='/console/'，output 'dist'）
│   ├── index.html
│   └── src/
│       ├── main.ts
│       ├── router.ts             （5 路由）
│       ├── stores/               （pinia：sessionList / pluginList / modelConfig / taskQueue）
│       ├── pages/                （5 个 .vue）
│       ├── api/                  （axios 客户端 + types）
│       └── components/           （ChatBubble / MarkdownView / PluginCard / ModelForm / TaskRow）
├── internal/console/             ← v8 新增（Go 端）
│   ├── console.go
│   ├── embed.go                  //go:embed all:dist
│   ├── handler_sessions.go
│   ├── handler_plugins.go
│   ├── handler_models.go
│   ├── handler_tasks.go
│   └── handler_approval.go
├── desktop/                      ← v8 新增（Wails 模板）
│   ├── main.go                   （Wails entry）
│   ├── wails.json
│   └── build/                    （平台图标 / Info.plist）
└── docs/v8/                      ← 本目录
```

> **关键约束**：Vue 构建产物必须是**可审计的静态文件**，不得引入运行时 CDN；CI 在 `web/` 跑 `pnpm build` → 把 `dist/` 提交进仓库 → Go `embed.FS` 打包进二进制。

---

## §3 后端 API 设计

### §3.1 REST 端点（`/api/v1/console/*`）

> **路径前缀统一 `/api/v1/console/`**，与 v4 Gateway 的 `/api/v1/events`、v7 ACP 的 `/acp/*`、v7 MCP 的 `/mcp` 完全隔离，避免路由冲突。

| Method | Path | 说明 | 入参 | 出参 |
|---|---|---|---|---|
| GET | `/sessions` | 会话列表（分页） | `?limit=50&cursor=` | `[{sid, title, model, createdAt, lastEventAt, status}]` |
| GET | `/sessions/:sid` | 会话详情（含事件流摘要） | — | `{sid, title, events: [...], stats}` |
| POST | `/sessions` | 新建会话 | `{title, model}` | `{sid}` |
| DELETE | `/sessions/:sid` | 删除会话 | — | `{ok}` |
| POST | `/sessions/:sid/messages` | 发送用户消息（流式） | `{content}` | `text/event-stream` |
| GET | `/plugins` | 插件清单 | — | `[{name, kind, status, version}]` |
| POST | `/plugins/:name/enable` | 启用插件 | — | `{ok}` |
| POST | `/plugins/:name/disable` | 停用插件 | — | `{ok}` |
| GET | `/models` | 已配置模型清单 | — | `[{channel, model, protocol, active}]` |
| PUT | `/models/:channel` | 更新模型配置 | `{model, protocol, active}` | `{ok}` |
| POST | `/models/:channel/ping` | 连通性测试 | — | `{latencyMs, sample}` |
| GET | `/tasks` | 任务队列 | `?state=` | `[{taskId, state, kind, progress}]` |
| POST | `/tasks/:id/cancel` | 取消任务 | — | `{ok}` |
| GET | `/approvals` | 待审批清单 | — | `[{id, tool, args, profile, createdAt}]` |
| POST | `/approvals/:id/decide` | 审批决策 | `{decision: 'allow'|'deny'\|'always'}` | `{ok}` |
| GET | `/events` | **SSE 转发**（透传 v4 Gateway） | `?sources=...` | `text/event-stream` |
| GET | `/health` | 健康检查 | — | `{ok, version, uptime}` |

### §3.2 错误约定

```json
{ "error": { "code": "PLUGIN_NOT_FOUND", "message": "plugin 'echo' not found", "traceId": "..." } }
```

- HTTP 状态码：`400`（参数）/ `401`（未鉴权）/ `404`（找不到）/ `409`（冲突）/ `500`（内部）。
- `traceId` 由 `internal/obs/` 生成，与日志/审计可关联。

### §3.3 鉴权

v8.0：单 token 模式
- 环境变量 `DSH_CONSOLE_TOKEN` 或 YAML `console.token` 配置；
- HTTP header `Authorization: Bearer <token>`；
- 路由层中间件：除 `/health` 之外全部要求鉴权。

v8.1：本地用户模式
- `console.users: [{name, passwordHash, role}]`；
- 登录页签发 JWT；
- 审计日志记录 user。

---

## §4 前端设计

### §4.1 5 个页面

#### P1 会话列表 `/sessions`

```
┌──────────────────────────────────────────────────────┐
│  DeepSeek Harness · 控制台          [+ 新建会话]      │
├────────────┬─────────────────────────────────────────┤
│ 会话       │  选中会话：sid-7f3a...                    │
│ ├ 会话 A   │  ┌────────────────────────────────────┐ │
│ ├ 会话 B ★ │  │ User: 帮我打开 ~/.zshrc            │ │
│ ├ 会话 C   │  │ Assistant: 好的，下面是内容...     │ │
│ └ 会话 D   │  │   [markdown 渲染]                   │ │
│            │  │ Tool: shell.run "cat ~/.zshrc"      │ │
│ 模型       │  │   ✓ exit 0                         │ │
│ 插件       │  └────────────────────────────────────┘ │
│ 任务       │  [输入框: 发送]                          │
│ 审批 (3)   │                                         │
└────────────┴─────────────────────────────────────────┘
```

#### P2 插件管理 `/plugins`

- 卡片网格：每卡片显示插件名 / 类型（gRPC / Node / MCP / Java Native）/ 版本 / 状态；
- 按钮：启用 / 停用 / 查看 manifest / 卸载（v8.1）；
- 状态徽标：✅ enabled / ⏸ disabled / ⚠ error。

#### P3 模型设置 `/models`

- 表格：channel / model / protocol / active / lastTested；
- 行内编辑：`active` 切换 / model 改名 / protocol 改 `openai-compatible` / `anthropic` / `gemini`；
- "连通性测试" 按钮：触发 `/models/:channel/ping` → 显示 latency + 抽样回复。

#### P4 任务队列 `/tasks`

- 列表 + 进度条；
- 状态：queued / running / completed / failed / cancelled；
- 行内按钮：取消 / 查看日志（v8.1 跳到会话详情）；
- 实时刷新：SSE 监听 `task.update`。

#### P5 审批 `/approvals`

- 待审批卡片：工具名 / 参数摘要 / 触发 profile / 创建时间；
- 三按钮：允许 / 拒绝 / 永久允许（写 v5 approval matrix）；
- 空状态：✅ "没有待审批的请求"。

### §4.2 技术栈

| 角色 | 选型 | 理由 |
|---|---|---|
| 构建 | Vite 5 | 上游同款；HMR 体验好 |
| 框架 | Vue 3 + `<script setup>` | 上游同款；TypeScript |
| 状态 | Pinia | 官方推荐；类型友好 |
| 数据 | @tanstack/vue-query | 缓存 + 自动 refetch + SSE 友好 |
| 路由 | Vue Router 4 | 标准 |
| HTTP | axios + interceptor | 拦截器统一注入 token |
| 渲染 | marked + DOMPurify + highlight.js | 与 ds-java 控制台一致 |
| 图标 | lucide-vue-next | 树摇友好 |
| 样式 | 原生 CSS + CSS Vars | 不引入 Tailwind，控制包体 |

### §4.3 包体目标

- 主 chunk ≤ **300 KB**（gzipped）；
- vendor 分包：vue / marked / highlight 单独 chunk；
- 首屏 LCP ≤ **1.5s**（局域网内）。

---

## §5 Desktop 打包（Wails v2）

### §5.1 选型理由

| 方案 | 优点 | 缺点 |
|---|---|---|
| **Wails v2** ✅ | 单二进制 / 系统 WebView / 体积小（~10MB）/ 无 Electron | Windows 需 WebView2 Runtime |
| Electron | 跨平台一致 | 体积 80MB+ / 内存高 |
| Tauri | 更小 | Rust 工具链；增加仓库复杂度 |

Wails v2 与 ds-go "**单二进制、不引入 CGO**" 的整体风格最契合。

### §5.2 打包结构

```
desktop/
├── main.go              ← Wails 入口
│                         - 启动 ds-go headless 模式（或外部 ds-go 进程）
│                         - 打开系统浏览器指向 http://127.0.0.1:7777
│                         - 提供 Tray：Start / Stop / Open Folder
├── wails.json           ← 构建配置
└── build/
    ├── appicon.png      ← 跨平台图标
    ├── windows/         ← .ico + manifest
    ├── darwin/          ← Info.plist
    └── linux/           ← .desktop 文件
```

### §5.3 命令约定

```bash
# 开发
cd desktop && wails dev

# 构建（输出到 desktop/build/bin/）
cd desktop && wails build

# 产物
deepseek-harness-desktop-amd64.exe     ← Windows
DeepSeek Harness Desktop.app           ← macOS
deepseek-harness-desktop-amd64.AppImage ← Linux
```

### §5.4 与 ds-go 二进制关系

- **方案 A（推荐）**：Desktop 二进制 **内嵌** ds-go 二进制；启动时释放到临时目录并 fork。
- **方案 B**：Desktop 二进制只做"壳"，外部依赖 `dsh` 命令在 PATH 里。
- v8.0 采用 A；v8.1 允许通过 `DSH_BINARY` 切换为 B。

---

## §6 与既有功能的集成

### §6.1 复用清单

| 既有模块 | v8 怎么用 |
|---|---|
| `internal/store/` | 会话事件读取 / 列表分页 |
| `internal/plugin/` | 插件清单 / 启用停用 |
| `internal/runtime/channel.go`（v5）| 模型渠道配置 |
| `internal/approval/matrix.go`（v5）| 审批决策写回 |
| `internal/server/gateway.go`（v4）| SSE 透传到 `/api/v1/console/events` |
| `internal/task/`（v6）| 任务列表 / 取消 |
| `internal/jobs/`（v6）| 任务进度 |
| `internal/usage/meter.go`（v5）| 会话统计 |

### §6.2 新增存储

仅 1 张表 `console_state`，存 UI 偏好：

```sql
CREATE TABLE console_state (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);
```

不引入新依赖（用现有 SQLite 句柄）。

---

## §7 测试策略

### §7.1 后端单元 / 集成

- `internal/console/handler_sessions_test.go`：handler 层（mock store）
- `internal/console/embed_test.go`：embed.FS 索引正确性
- `internal/console/auth_test.go`：鉴权中间件（缺 token / 错误 token / 正确 token）

### §7.2 前端单元 / 组件

- vitest + @vue/test-utils
- 每个页面至少 1 个 smoke test（渲染 + 关键交互）
- 覆盖率 ≥ 70%

### §7.3 端到端

- **手工 E2E**：`test-cases.md` 中 8 条用例覆盖全流程
- **自动 E2E（v8.1 候选）**：Playwright，仅在 CI 跑

### §7.4 桌面打包

- `wails build` 必须成功（Windows + Linux + macOS 三平台）
- 启动 smoke：headless 启动后 5s 内 HTTP 200

---

## §8 风险与回退

| # | 风险 | 影响 | 缓解 |
|---|---|---|---|
| R1 | Vite 产物体积失控 | 首屏慢 | 分包 + 体积预算（300KB gzip）+ CI 强制 |
| R2 | Wails 与系统 WebView 版本差异 | 渲染不一致 | 在 `package.json` 锁版本；README 注明最低要求 |
| R3 | embed.FS 与 Windows 大小写 | 404 | CI 在 Windows 上跑 `go build`，再 `curl /console/` 实测 |
| R4 | 鉴权简单导致误用 | 安全 | v8.0 文档明确"仅本机/内网使用"；v8.1 加本地用户 |
| R5 | 复用 v4 Gateway SSE 性能瓶颈 | 流式卡顿 | 增加缓冲 + 重连；前端 SSE 客户端用 `@microsoft/fetch-event-source` |
| R6 | Desktop 打包首次失败 | 阻断 v8.0 | 允许 v8.0 仅发布 Web 控制台 + 文档说明桌面步骤，v8.0.1 补齐 |

### §8.1 回退策略

若 Wails 在某平台 24h 内无法解决打包问题：
- v8.0.0 仅打 tag，文档注明"Desktop 步骤见 v8.0.1"；
- 不影响 Web 控制台（其产物是独立 chunk）。

---

## §9 与上游 ds-ts / ds-java 对齐

| 能力 | ds-ts | ds-java | ds-go v8 |
|---|---|---|---|
| Web 控制台 | `apps/web` Vite+Vue | 原生 JS 单页 | **Vite+Vue（v8.0）** ✅ |
| Desktop 打包 | Electron | ❌ | **Wails（v8.0）** ✅ |
| 鉴权 | 用户系统 | Token | Token（v8.1 用户） |
| 国际化 | i18n 完整 | zh-CN only | zh-CN only（v8.1 en-US） |

> v8.0 不追求"全覆盖 ds-ts"，只覆盖"ds-go 用户每天会用到的 5 件事"。

---

## §10 v8 后续版本（v8.1 / v8.2 候选）

- v8.1：用户系统 + JWT + 审计导出 CSV
- v8.1：Spill（流式输出断点续传）
- v8.1：Browser-Use 可视化面板
- v8.2：PTC-Runtime 任务甘特图
- v8.2：Webhook + 飞书/钉钉通知
- v8.2：插件市场（在线浏览 + 一键安装）

---

**设计完成**：本 DESIGN-v8 是 v8 的实施依据；启动 PHASE-8 时按本设计开 P8-1 / P8-2 / P8-3 三个子阶段。
