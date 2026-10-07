# dsh 桌面端前端 — 功能与视觉设计文档

> 对应 `dsh-desktop` 启动器（`desktop/dsh-desktop.exe`）内置的 Web Console SPA。
> 所有视图位于 `web/src/views/`；组件位于 `web/src/components/`；设计 token 在 `web/src/styles/main.css`。
>
> 适用版本：v8.1.0（2026-10-07）—— 桌面端 React/Vue 视觉与 dsh 原生 web 1:1 对齐。
>
> 关联文档：
> - [`README.md`](./README.md) — Go Harness 简介
> - [`desktop/README.md`](../desktop/README.md) — dsh-desktop 启动器
> - [`desktop/BUILD.md`](../desktop/BUILD.md) — 桌面包构建
> - [`WEB-FRONTEND-GAPS.md`](./WEB-FRONTEND-GAPS.md) — Web 前端 gap 清单

---

## 1. 设计原则

dsh-go 控制台严格对齐 dsh 原生 web 视觉（参考 `deepseek-harness` 的 `packages/client/ui-chat/*` + `SidebarRoot.module.css`），所有元素都"贴源反推"。核心原则：

1. **强烈深色优先**：主背景 `#0a0a0a`；侧栏 `#0e0e0e`；面板 `#141414`。浅色模式作为 fallback。
2. **dsw-alias-\* 命名兼容**：CSS token 与 dsh 原生完全一致，后续可平滑替换。
3. **border-l1 / l2 / l3 / l4 透明度分层**：4 档边框透明度（0.06 / 0.10 / 0.16 / 0.22）。
4. **corner-shape: round**：Windows 11 风圆角（xs=4 / sm=6 / md=8 / lg=12 / xl=16 / pill=9999）。
5. **紧凑 sidebar**：12px 内边距、6px 行间距；hover 仅底色变化，无 box-shadow。
6. **微动效统一**：`cubic-bezier(0.4, 0, 0.2, 1)`；fast=100ms / base=200ms / slow=300ms。

---

## 2. 全局布局（App.vue）

控制台采用 dsh 原生 web 的 `AppFrame` 布局 —— 左侧 Sidebar + 右侧 Topbar + Content，桌面模式额外有底部 Statusbar。

```
┌──────────┬─────────────────────────────────────────────────────────┐
│          │ Topbar（顶栏，46px）                                      │
│  Side    ├─────────────────────────────────────────────────────────┤
│  bar     │                                                         │
│ (260px)  │                                                         │
│          │  Content（router-view，flex 1）                          │
│   ⊙      │   · 紫色径向渐变背景（#1a1424 → #100c1a）                │
│          │   · 装饰：spark ✦ + blob 光晕                              │
│          │                                                         │
├──────────┼─────────────────────────────────────────────────────────┤
│          │  Statusbar（22px，仅 desktop 模式）                       │
└──────────┴─────────────────────────────────────────────────────────┘
```

Sidebar 折叠时网格切换为 `56px 1fr`（rail 模式，仅图标）。

### 2.1 Sidebar（左栏）

| 区域 | 元素 | 备注 |
|---|---|---|
| 顶部 | brand mark（紫色渐变 `#6e59ff → #b06bff` 方块 + 闪电 ⚡ 图标）+ "DeepSeek / Harness" 文字 | 折叠时仅图标可见 |
| 折叠按钮 | 右侧 24×24 图标按钮 | 切换 sidebar 展开 / 折叠 |
| 当前工作区 chip | 圆角 10px、黑色渐变（`#2a2436 → #1f1b2e`）；左侧紫色渐变方块 + 工作区名 + "工作区" 副标题 + 倒三角 | 点击展开工作区菜单浮层 |
| 新建会话按钮 | 紫色实心矩形 32px 高，主色 `#2e90fa`；展开："+ New session"，折叠：圆形 + 图标 | 点击跳 `/sessions?new=1` 立即创建并进入 |
| 导航列表 | 一级导航：Sessions（图标 + 文字）；admin 入口（plugins / models / tasks / approvals / schedules / webhooks）从顶栏移除，仅作为 admin 控制台的内容 |
| 底部 | user-avatar（圆形）+ 用户名 + 角色（Admin / Member）+ 主题切换按钮（月亮 / 太阳图标） | 永久可见；折叠时仅显示主题切换图标 |

> **v8.1 P3 设计决策**：Sessions 是 chat 工作区的主入口，其它 admin 视图（plugins / models / tasks / approvals / schedules / webhooks）从左栏移除，避免冗余。它们仍可通过直接 URL 访问，或后续提供独立的 admin 控制台。

### 2.2 Topbar（顶栏）

| 位置 | 元素 | 行为 |
|---|---|---|
| 左侧 | **模型选择器 chip**：紫色渐变方块 + 模型名（如 `deepseek-flash`）+ 渠道标签（如 `default`）+ 倒三角 | 点击展开模型列表（来自 `/models` API，30s 自动刷新）|
| 左侧 | **Tab 栏**：`Chat` / `DeepResearch` / `R1` / `T4` / `Agent` / `Code` / `Docs`，带 lucide 图标 + 紫色下划线指示 | 仅在非 session 详情页显示；点击当前 tab 高亮 |
| 右侧 | 连接状态点（绿 / 红 / 灰）+"已连接 / 认证失败 / 未连接" | 点击刷新 |
| 右侧 | 版本号 `v8.1.0` | 仅显示 |
| 右侧 | 语言切换按钮（"EN" / "中"） | i18n（zh-CN / en-US）切换，持久化 localStorage |
| 右侧 | 刷新按钮（旋转图标） | ping `/health` |
| 右侧 | 注销按钮（仅 web 模式） | 调 `/auth/logout`，跳 `/login` |
| 右侧 | 桌面 pill："Desktop"（紫色背景 + Monitor 图标，仅 desktop 模式显示） | 由 `?from=desktop` 触发 |

### 2.3 Content（主内容区）

```
background: linear-gradient(180deg,
  #1a1424 0%,
  #14101d 35%,
  #100c1a 100%);
[data-theme="light"] →
background: linear-gradient(180deg,
  #faf8ff 0%,
  #f3eef9 35%,
  #ede9f4 100%);
```

装饰元素：

- `deco-spark-1` / `deco-spark-2`：两枚浮动 ✦ 图标（12s 周期上下浮动）
- `deco-blob-1` / `deco-blob-2`：两团紫色径向渐变 blob（18s 周期浮动 + blur(60px)）
- 所有装饰 `pointer-events: none`，不干扰交互

### 2.4 Statusbar（底部状态条，仅桌面模式）

```
<Monitor/> DeepSeek Harness Desktop v8.0.0   pid=12345   已连接
```

22px 高，淡灰色文字，桌面独占。

---

## 3. 视图清单与功能

### 3.1 LoginView（`/login`）

**用途**：本地账号登录控制台。

**布局**：

- 居中卡片（360px 宽），紫色 12px 圆角
- 顶部 brand 区：紫色渐变方块 + ⚡ 图标 + "DeepSeek Harness" 标题 + "使用本地账号登录控制台" 提示
- 表单字段：用户名（必填）/ 密码（必填）
- 错误条（红色 AlertCircle 图标 + 消息）
- "登录"主按钮（紫色全宽，登录中禁用 + "登录中…" 文案）

**行为**：

- 调 `POST /auth/login` 拿 JWT
- 成功后跳 `redirect` query 或 `/sessions`
- 401 时显示错误条
- 已登录用户访问 `/login` 会再被允许（由 router.beforeEach 决定）

### 3.2 SessionsView（`/sessions`，默认页）

**用途**：会话列表（按工作区分组）。

**布局**：

```
┌─────────────────────────────────────────────────┐
│ 会话                            [+] 新建会话    │ ← page header
├─────────────────────────────────────────────────┤
│ 🔍 过滤会话…                                   │ ← 搜索条
├─────────────────────────────────────────────────┤
│ 默认工作区 · 5                                  │ ← 工作区分组
│  ┌─────────────────────────────────────────┐  │
│  │ 会话标题                                  │  │
│  │ 最近一条消息预览…                          │  │
│  │ [deepseek-flash] 3 轮 · 5 分钟前    [🗑] │  │ ← 列表项
│  └─────────────────────────────────────────┘  │
│ 项目 A · 2                                     │
│  ┌─────────────────────────────────────────┐  │
│  │ …                                       │  │
└─────────────────────────────────────────────────┘
```

**功能点**：
  - 按 5s 轮询 `GET /sessions?limit=50`
  - 按工作区分组展示（默认工作区永远在第一位）
  - 跨工作区搜索（标题 / 预览 / 模型名模糊匹配）
  - "+ New session"按钮：立即调 `POST /sessions` 创建并跳 `/sessions/:sid`；左栏 "+ New session" 等价入口
  - 单条 hover 显示删除按钮（🗑）；点击二次确认后调 `DELETE /sessions/:sid`
  - 空态：MessageSquare 图标 + "暂无会话 / 点击右上角「新建会话」开始"
  - 工作区 chip 切换会过滤显示，但展示所有工作区

### 3.3 SessionDetailView（`/sessions/:sid`）

**用途**：聊天主界面（dsh-go 控制台的核心）。

```
┌─────────────────────────────────────────────────────┐
│ [←] 会话标题 · X 条 · Y 轮       🔄 ⬇ 📋 [Panel] │ ← topbar（38px）
├──────────────────────────────────────┬──────────────┤
│ Messages（max-width 748px）          │ Activity     │
│  ┌─[user]──────────┐                 │ ┌──────────┐ │
│  │ 用户消息（右对齐 bubble）          │ │ Changes   │ │
│  │ ⏰ 14:32                          │ ├──────────┤ │
│  └──────────────────────────────────┘  │ ∎ x.ts    │ │
│                                       │ ∎ go run  │ │
│  ⚡ 助手消息                          │ ├──────────┤ │
│  Markdown 渲染的 markdown…           │ Tools │ Logs│ │
│  ┌─ [tool calls] details ──┐         │ ├──────────┤ │
│  │ 2 个工具调用                       │ │ 4 / 4     │ │
│  │  ├ fs_write [unread args]           │ │ subtasks │ │
│  │  └ terminal [unread args]          │ │ 0 agent   │ │
│  └───────────────────────────────────┘ └──────────────┘ │
│                                       │              │
│  ┌─ StepChain ──────────────────────┐ │              │
│  │ ⚙ 4. hello.go（40 行 · 新建）       │ │              │
│  │ ⚙ 3. terminal: go run hello.go    │ │              │
│  │ ⚙ 2. file_search: *.go            │ │              │
│  └───────────────────────────────────┘ │              │
│                                       │              │
│  [🖫 cpu] [↑ 1k] [↓ 0.5k] [cache 85%] │              │
│  [🧠 0] [🕐 1.2s] [⏰ 14:33]          │              │
│  ✓ 已完成 · 用时 1.2s                 │              │
│  [📋] [👍] [👎] [🔄]                  │              │
├──────────────────────────────────────┴──────────────┤
│ Composer                                          │
│ ┌──────────────────────────────────────────────┐ │
│ │ 输入消息，AI 会自动感知工作区内容…              │ │ ← textarea
│ ├──────────────────────────────────────────────┤ │
│ │ 🔍 📎 🌐 @ 🎤 🕘                              │ │ ← mid row（工具图标）
│ ├──────────────────────────────────────────────┤ │
│ │ [+] [工作区内修改 ▾]    [cpu ▾] [↑]           │ │ ← foot
│ ├──────────────────────────────────────────────┤ │
│ │ 轮 · 步 · tok/s · tok · 缓存 · %              │ │ ← status bar
│ └──────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────┘
```

**核心元素详解**：

- **顶栏（38px）**：
  - 返回按钮（ArrowLeft）
  - 标题块：标题文字（pin 图标预留）+ 副标题（消息数 · 轮数 · spill 连接点）
  - 刷新（RefreshCw）
  - 导出下拉（Download → Markdown / JSONL）
  - 右侧 Activity 面板开关（PanelRight / PanelRightClose）

- **消息区（messages-col）**：
  - `max-width: 748px`（对齐 dsh 原生 `--dsh-chat-content-width`）
  - 消息类型：
    - **user**：82% max-width 右对齐 bubble，圆角 20px，灰底
    - **assistant**：左对齐无 bubble，26×26 Sparkles 紫色渐变 avatar；数据是 `MarkdownView`（marked + DOMPurify + highlight.js，支持代码块 + 复制按钮 + 9 种语言高亮）
    - **tool**：灰色 tool-row 区块，左侧终端图标 + 工具名 + "失败"红标 + 输出内容（pre）
    - **system**：灰色提示行
  - 工具调用：`<details>` 折叠面板（圆点 + 工具名 + 嵌套 args pre）
  - 文件卡片：`FileCard` 组件（已编辑 / 已创建 + 行号:列 + 描述 + 代码预览 + View diff + 打开 + 撤销）
  - 步骤链：`StepChain` 组件（每个步骤：edit/terminal/web/search/file/tool 图标 + 标题 + 详情 + 可选预览；圆点带连接线）
  - meta 行：模型 / ↑prompt / ↓completion / cache 命中%（带颜色：≥0.7 绿 / ≥0.4 黄 / 否则灰）/ reasoning tokens / 用时 / 时间 + hover 显示 [📋 👍 👎 🔄] 操作
  - status 行：✓ 已完成 · 用时 Xs（dsh 风格）
  - 流式输出：实时光标 `▍`（530ms 闪烁），`assistant_delta` SSE 增量追加
  - **回到最新**浮动按钮：仅当用户上滑且正在发送时显示
  - 空态：⚡ logo + "开始一个对话" + 当前模型名

- **Composer（输入区）**：
  - **textarea**：自增长（max 220px），Enter 发送，Shift+Enter 换行
  - **mid row**：6 个 icon-only 工具按钮（搜索 / 附加 / 网络搜索 / @ 提及 / 语音 / 历史）
  - **foot row**：
    - 左：`[+]` 添加附件 + "工作区内修改" workspace pill
    - 右：模型选择器 pill（当前模型 + 速度档 high/medium/low）+ send / stop 切换（运行时显示方块停止按钮）
  - **status bar**（6 维）：
    - 轮（GitBranch）/ 步（ListChecks）/ tok/s（Gauge）/ total tok（Zap）/ 缓存命中 %（Database，带颜色）/ 上下文 %（CircleDot，按 128k 估算）
    - 实时更新（usage_update SSE）

- **Activity Side Panel**（240px，可关）：
  - 顶：Activity 标题 + 计数
  - 标签页：Changes / Tools / Logs（默认 Changes）
  - 列表：每条 {icon + text + time + ›}
  - 底部：X / Y subtasks + N agent running

- **Demo 模式**（`/sessions/demo`）：
  - 不发任何 API 请求，纯前端 mock 数据
  - 用于未登录用户预览产品；router.beforeEach 单独放行

### 3.4 PluginsView（`/plugins`）

**用途**：插件管理（启停 / 安装 / 卸载）。

| 列 | 字段 |
|---|---|
| 名称 | plugin.name |
| 元数据 | tag: kind / version / source |
| 状态 | badge（enabled / disabled / failed / unhealthy）|
| 工具列表 | tag 列表（plugin.tools）|
| 操作 | 启用 / 停用 / 卸载 |

- 顶部工具栏：刷新 + 安装
- 安装弹窗：插件名称（必填）+ 来源（可选）
- 卸载带 confirm 对话框
- 失败 / unhealthy 显示 `lastError` 红色区块

### 3.5 ModelsView（`/models`）

**用途**：模型渠道管理。

表格列：渠道 / 模型 / 协议 / 状态 / 连通性 / 操作。

- "新增渠道"弹窗：渠道 ID（必填）+ 模型名 + 协议（openai-compatible / anthropic / gemini / deepseek）+ Base URL
- 行操作：ping（实时测连通性 + 延迟）/ 启用 / 停用 / 删除
- ping 结果：成功显示绿色 badge `XXms`；失败显示红色 badge + tooltip 错误
- 顶部"导出审计"按钮：下载 JSONL 审计日志

### 3.6 TasksView（`/tasks`）

**用途**：任务调度。

- 顶部状态过滤（全部 / pending / running / completed / failed / cancelled）
- 列表项：标题 + profile + 状态 badge + code + 创建/更新时间 + progress
- 操作：失败 / 已取消可重试；running / pending 可取消
- "新建任务"弹窗：标题（可选）/ 输入（必填，textarea）/ profile（headless / default / restricted）

### 3.7 ApprovalsView（`/approvals`）

**用途**：待审批请求管理。

- 列表项：tool + profile + "待审" badge + 创建时间 + args（代码块 pre）+ 原因
- 单条操作：[允许] [始终] [拒绝]
- 批量：勾选 + 批量允许 / 批量拒绝 / 全选
- 左侧 3px 黄色 warning 边框强调

### 3.8 SchedulesView（`/schedules`）

**用途**：定时任务（Cron）。

表格列：名称 / Cron 表达式（mono 高亮）/ 状态（enabled / disabled）/ 最近（lastStatus badge）/ 操作。

- 操作：▶ 立即运行 / 启用·停用 / 删除
- "新建调度"弹窗：名称 + Cron 表达式 + 动作（JSON）

### 3.9 WebhooksView（`/webhooks`）

**用途**：Webhook 通知管理。

表格列：名称 / URL / 状态 / 最近（HTTP status code badge）/ 操作。

- 操作：投递历史（弹出 modal 表格：时间 / 状态 / attempt / error）/ 测试 / 删除
- "新建 Webhook"弹窗：名称 + URL（必填）+ HMAC-SHA256 密钥（可选）
- 测试结果 toast：✓ HTTP 200 (1 attempt) / ✗ HTTP 500

---

## 4. 组件清单

| 组件 | 用途 | 关键 prop |
|---|---|---|
| `MarkdownView.vue` | Markdown 渲染（marked + DOMPurify + highlight.js） | `source: string` |
| `StepChain.vue` | 步骤链（圆点 + 连接线）| `steps: Step[]` |
| `FileCard.vue` | 文件编辑/创建卡片 | `kind` / `path` / `lineCol` / `description` / `diff` / `preview` / `language` |
| `Modal.vue` | 通用对话框 | `open` / `title` / `@close` / `@confirm` + `#footer` slot |
| `ToastStack.vue` | 全局 toast 通知栈 | 由 `useUIStore().toasts` 驱动 |

---

## 5. Store 体系（Pinia）

| Store | 状态 | 持久化 | 用途 |
|---|---|---|---|
| `user.ts` | `token` / `user{id, username, role, createdAt}` | localStorage | JWT 认证 + 当前用户 |
| `token.ts` | （预留） | localStorage | 旧 static token 兼容 |
| `ui.ts` | `theme` / `toasts` / `locale` | localStorage（theme + locale）| 主题 + 多语言 + 全局 toast |
| `workspace.ts` | `workspaces[]` / `currentId` / `sessionMeta{}` | localStorage | 多工作区管理 + session → 工作区映射 |

---

## 6. API 层（axios + fetch）

所有 `/api/v1/console/*` 请求走单一 axios `client` 实例
- 请求拦截器注入 `Authorization: Bearer <JWT>`
- 响应拦截器：401 清 token + 跳 `/login`；其它错误通过 `useUIStore().reportError` 弹 toast

`client.ts` 关键 API：

```ts
client.get('/sessions', { params: { limit, cursor } })  // ListSessions
client.post('/sessions', { title, model })               // CreateSession
client.get(`/sessions/${sid}`)                           // SessionDetail
client.delete(`/sessions/${sid}`)                        // Delete
client.patch(`/sessions/${sid}/messages/${seq}`, { content }) // EditMessage
client.delete(`/sessions/${sid}/messages/${seq}`)          // DeleteMessage
client.post(`/sessions/${sid}/messages/${seq}/feedback`, { rating, comment }) // 👍/👎
client.get(`/sessions/${sid}/export?format=md`, { responseType: 'blob' })     // ExportMd
client.get(`/sessions/${sid}/export?format=jsonl`, { responseType: 'blob' })  // ExportJsonl
client.get(`/sessions/${sid}/events?since=N`)            // EventsSince (Spill 续传)
```

流式 API 走 `fetch`（不走 axios）：
- `POST /api/v1/console/sessions/:sid/messages` — sendStream（SSE，按 `\n\n` 切帧）
- `POST /api/v1/console/sessions/:sid/messages/:seq/regenerate` — regenerate（同 SSE）

`models.ts` / `plugins.ts` / `tasks.ts` / `approvals.ts` / `schedules.ts` / `webhooks.ts` / `audit.ts` / `auth.ts` 各自封装对应 REST 端点。

---

## 7. 国际化（i18n）

轻量自研 i18n（无 vue-i18n 依赖），zh-CN / en-US 双语。

- `useI18n()` composable 暴露 `locale` / `setLocale` / `t(key)`
- 持久化：`localStorage 'dsh.console.locale'`
- 字典集中在 `src/i18n.ts`（key 用点号连接，如 `sessions.empty`）
- App.vue 顶部栏"EN / 中"按钮切换

---

## 8. 主题

- 默认深色（`#0a0a0a` 系），由 `useUIStore().theme` 控制
- 浅色 fallback（Content 区会改成 `#faf8ff → #ede9f4` 渐变）
- 应用方式：`document.documentElement.dataset.theme = theme`
- 持久化：`localStorage 'dsh.console.theme'`
- App.vue 底部 user card 的月亮 / 太阳图标切换

---

## 9. 构建与打包

```ts
// web/vite.config.ts
base: '/console/'                    // 所有产物 url 带 /console/ 前缀
server.proxy['/api/v1/console']      // dev 时转发到 dsh -serve :7777
build.rollupOptions.output.manualChunks = {
  vendor:  ['vue', 'vue-router', 'pinia', 'axios'],
  query:   ['@tanstack/vue-query'],
  render:  ['marked', 'dompurify', 'highlight.js'],
}
chunkSizeWarningLimit: 320            // KB（≈ gzip 300KB + 余量）
```

构建产物 `web/dist/` 由 `internal/console/embed.go` 通过 `//go:embed` 注入主二进制；桌面启动器（`desktop/dsh-desktop.exe`）再把 `dsh.exe` 嵌进去做单文件分发。

---

## 10. 关键交互流

### 10.1 新建并发送一条消息

```
左栏 / 顶栏 New session
  → router.push('/sessions?new=1')
  → SessionsView 监听 ?new=1
  → sessionsApi.create() 立即 POST /sessions
  → workspaceStore.bind(sid) 标记当前工作区
  → router.push('/sessions/<sid>')
  → SessionDetailView mount
  → 用户在 composer 输入文本 → Enter
  → sessionsApi.sendStream() 发 SSE
  → 接收 assistant_delta 实时追加
  → tool_call_start / tool_result 渲染工具调用面板
  → usage_update 更新 composer status bar
  → loop_done 结束 → detail.refetch() 持久化
```

### 10.2 Spill 断点续传

```
SessionDetailView onMounted
  → sessionsApi.eventsSince(sid, -1) 拿 lastSeq
  → openSSE('/api/v1/console/sessions/:sid/events?since=lastSeq')
  → onMessage → applySpillEvent
    · assistant_delta → 追加到最后一个 pending 助手消息
    · tool_result     → 新增 tool 消息
  → 断线自动重连（onReconnect 计数）
```

### 10.3 审批流程

```
agent 工具调用遇到 allowlist 外 → POST /approvals
  → ApprovalsView header 计数 3s 轮询更新
  → 用户点 [允许] / [始终] / [拒绝]
  → POST /approvals/:id/decide 或 decideBatch
  → 后端恢复 tool 调用
```

---

## 11. 桌面模式特殊处理

`desktop/dsh-desktop.exe` 启动后：

1. 释放 `dsh.exe` 到 `%LOCALAPPDATA%\DeepSeekHarness\bin\`
2. spawn `dsh.exe -serve -port 0`
3. 轮询 `/api/v1/console/health` 直到 200
4. ShellExecute 打开默认浏览器到 `/console/?from=desktop&token=<jwt>`
5. App.vue `onMounted` 读 `?from=desktop` + `?token` → 标记桌面模式 + 自动登录
7. 显示 Statusbar 状态条（`pid + version`）

桌面模式 ≠ Wails 模式：v8 默认纯浏览器壳（launcher 模式），Wails 升级为 v8.0.1 候选。

---

## 12. 已知 gap

详见 [`WEB-FRONTEND-GAPS.md`](./WEB-FRONTEND-GAPS.md)。

主要缺口：
- 顶栏 admin 入口未挂到 sidebar（plugins / models / tasks / approvals / schedules / webhooks）
- 工作区管理只在 sidebar chip 弹出菜单，未单独成页
- 审计日志 UI 未单独成页（ModelsView 顶部有"导出审计"按钮）
- 凭据（Credentials）/ 计量（Meter）视图未配置