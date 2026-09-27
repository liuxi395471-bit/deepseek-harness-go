# RELEASE-NOTES v8.0.0

> DeepSeek Harness Go · Web Console + Desktop
> 发布日期：2026-09-27
> tag：`v8.0.0`
> 主线 commit：`fb034e5` → `待定`

## 🌟 重大变化

v8.0 把 ds-go 提升到"产品级"用户体验：**Web 控制台 + 桌面启动器**。

| 区域 | 变化 |
|---|---|
| 新模块 | `internal/console/`（后端 Console 域，14 个 REST 端点） |
| 新目录 | `web/`（Vue 3 SPA 源码 + 构建产物） |
| 新子项目 | `desktop/`（独立 Go module，启动器） |
| 新 API | `/api/v1/console/*`（鉴权、SSE、SPA 静态资源） |
| 新 CLI | `dsh-desktop.exe`（双击 = 启动 + 浏览器自动打开） |
| 配置 | 新增 `DSH_SERVER_AUTH_TOKEN`（强制）、`DSH_SERVER_LISTEN` |
| 路由 | 新增 `/console/*`（SPA fallback）、`/api/v1/console/*` |

## ✨ 新增功能

### Web Console（5 页）

1. **会话（Sessions / SessionDetail）**
   - 列表 + 创建 + 删除
   - 详情页 SSE 流式渲染 assistant 回复
   - 工具调用折叠（tool name + args JSON）
   - Markdown 渲染（marked + DOMPurify 防 XSS + highlight.js）
   - Token 用量统计

2. **插件（Plugins）**
   - 列出全部插件 + 健康状态
   - 一键启用 / 停用（写 `plugin_status.json`）

3. **模型（Models）**
   - 渠道表（多渠道 / 协议 / 启用切换）
   - 一键 ping 延迟测试

4. **任务（Tasks）**
   - 状态筛选（pending / running / completed / failed / cancelled）
   - 一键取消任务

5. **审批（Approvals）**
   - 待审批队列
   - 三选一裁决：允许一次 / 始终允许 / 拒绝

### 桌面启动器（dsh-desktop.exe）

- 双击 = 自动启动 `dsh -serve` + 自动打开浏览器到 `/console/`
- 启动器自动生成 Bearer token（无需用户配置）
- 自动检测端口（`127.0.0.1:0`）+ 健康检查（`/api/v1/console/health/`）
- 优雅停止（SIGTERM → 5s → SIGKILL）
- 支持 3 种运行模式：
  - 同目录 `dsh.exe` + `dsh-desktop.exe`（非嵌入）
  - `-tags embed_dsh` 嵌入模式（单文件分发，~9MB）
  - 未来 Wails v2 升级（v8.0.1 候选）

### 后端 Console 域

14 个 REST 端点（位于 `/api/v1/console/`）：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/health/` | 健康检查（无需 token） |
| GET | `/sessions/` | 会话列表 |
| POST | `/sessions/` | 新建会话 |
| GET | `/sessions/{sid}` | 会话详情 |
| DELETE | `/sessions/{sid}` | 删除会话 |
| POST | `/sessions/{sid}/messages` | 发送消息（SSE 流式返回） |
| GET | `/plugins/` | 插件列表 |
| POST | `/plugins/{name}/enable` | 启用 |
| POST | `/plugins/{name}/disable` | 停用 |
| GET | `/models/` | 模型渠道 |
| PUT | `/models/{channel}` | 更新渠道配置 |
| POST | `/models/{channel}/ping` | 连通性测试 |
| GET | `/tasks/` | 任务列表 |
| POST | `/tasks/{id}/cancel` | 取消 |
| GET | `/approvals/` | 待审批 |
| POST | `/approvals/{id}/decide` | 裁决 |
| GET | `/events/` | SSE 事件转发 |
| GET / PUT | `/state?key=...` | UI 偏好 KV |

---

## 🆕 P0 增量（v8.0.0-P0 / 2026-09-27）

> 14 项 UI 小项补齐，**对齐 ds-java Web Console** 体验 + 后端能力扩展。
> tag 在 `v8.0.0` 之上追加 P0 补丁，HEAD commit 仍为 `v8.0.0`。

### 1. 后端 Store / 域扩展

| 区域 | 改动 | 文件 |
|---|---|---|
| `store.Store` | 新增 `DeleteSession / EditMessage / DeleteMessage` | `internal/store/store.go`, `mem.go`, `sqlite.go` |
| `runtime.MemoryRegistry` | 新增 `Upsert(code, cfg, replace)` + `Remove(code)` | `internal/runtime/channel.go` |
| `task.LoopExecutor` | 新增 `Retry(ctx, id) (*Task, error)` | `internal/task/task.go`, `executor.go` |
| `console.PluginBackend` | 新增 `Install / Uninstall` | `internal/console/backends_extra.go` |
| `console.ModelBackend` | 新增 `Create / Remove` | 同上 |
| `console.TaskBackend` | 新增 `Submit / Retry` | 同上 |
| `console.AuditBackend` | **新增** JSONL 读取接口 + 流式导出 | `internal/console/audit_adapter.go` |
| SQLite Schema | `messages / events` 增加 `ON DELETE CASCADE` | `internal/store/sqlite.go` |

### 2. 后端 Console 新 REST（11 个）

| 方法 | 路径 | 说明 |
|---|---|---|
| DELETE | `/sessions/{sid}/messages/{seq}` | 单消息删除（保留 seq 空位） |
| PATCH  | `/sessions/{sid}/messages/{seq}` | 编辑消息内容 |
| POST   | `/plugins/{name}/install` | 安装插件（source = 内置 / 远端） |
| POST   | `/plugins/{name}/uninstall` | 卸载插件 |
| POST   | `/models` | 新建渠道（dynamic upsert） |
| DELETE | `/models/{channel}` | 删除渠道 |
| POST   | `/tasks` | 提交任务（异步入队） |
| POST   | `/tasks/{id}/retry` | 重试失败/已取消任务（新 ID） |
| POST   | `/approvals/decide-batch` | 批量裁决 |
| GET    | `/audit` | 审计查询（JSON） |
| GET    | `/audit/export` | 审计流式导出（`application/x-ndjson`） |

错误码扩展：

| sentinel | 用途 |
|---|---|
| `ErrPluginExists` | 重复安装 |
| `ErrPluginMissing` | 卸载不存在 |
| `ErrModelExists` | 渠道重名 |
| `ErrBackendMissing` | 后端未实现 |
| `ErrTaskRunning` | 运行中禁止重试 |
| `ErrMessageNotFound` | seq 越界 |
| `ErrMessageNotEditable` | tool/role 消息不可编辑 |

### 3. 前端增强

| 模块 | 改动 |
|---|---|
| `web/src/i18n.ts` | **新增**：轻量 reactive 字典（zh-CN / en-US），localStorage 持久化 |
| `web/src/stores/ui.ts` | **新增**：UI store（toast 队列、theme、locale、buildStamp） |
| `web/src/components/Modal.vue` | **新增**：通用模态对话框（确认 / 输入 / 表单三态） |
| `web/src/components/ToastStack.vue` | **新增**：右下 toast 通知（success / warn / error / info 四态） |
| `web/src/components/MarkdownView.vue` | 升级 highlight.js + code copy button |
| `web/src/views/SessionsView.vue` | 关键字搜索过滤 + 模型下拉 |
| `web/src/views/SessionDetailView.vue` | 消息编辑弹窗 + 删除按钮 + token usage breakdown 卡片 + tool 流式结果 |
| `web/src/views/TasksView.vue` | 创建任务弹窗 + 重试按钮 |
| `web/src/views/ApprovalsView.vue` | 多选 checkbox + select-all + 批量 allow / deny |
| `web/src/views/PluginsView.vue` | 安装弹窗 + 卸载确认 |
| `web/src/views/ModelsView.vue` | 新建渠道弹窗 + 删除渠道 + 导出审计按钮 |
| `web/src/api/audit.ts` | **新增**：audit 查询 / 导出 |
| `web/src/api/*.ts` | 扩展 error 拦截 → uiStore.pushToast |
| `web/src/styles/main.css` | 增加 `[data-theme="dark"]` 变量、modal/toast 样式 |
| `web/src/App.vue` | 顶栏：buildStamp 徽标 + theme 切换 + locale 切换 + `<router-view :key="buildStamp">` 强制重挂 |

### 4. 验证

- `go test -count=1 ./internal/...` — **全绿**
- `vue-tsc --noEmit` — **零错误**
- `pnpm build` — 产物 ≈ 130 KB gzip（仍远低于 300 KB 预算）
- 新增手工 E2E：`docs/v8/TEST-CASES.md` TC-v8-0009 ~ 0016（**8 条**）

### 5. 不兼容变更

- **无** — P0 是纯增量，零 breaking change。
- `/api/v1/console/*` 新增的 11 个端点全部默认开启；如需关闭，可在 `cmd/dsh/main.go` 注册路由处注释（无需重新打包二进制时可用）。

---

## 📦 构建产物

| 名称 | 大小 | 说明 |
|---|---|---|
| `dsh.exe` | ~31 MB | 单二进制，含 SPA（embed.FS） |
| `dsh-desktop.exe` | ~9 MB | 桌面启动器（非嵌入） |
| `dsh-desktop.exe`（embed） | ~40 MB | 桌面启动器（嵌入 dsh） |
| `web/dist/*.js` | ~330 KB raw / **~123 KB gzip** | SPA 构建产物 |
| `internal/console/web_dist/*` | 同上 | 嵌入到 dsh.exe 的副本 |

## 🔄 迁移指南

### 从 v7.0.0 升级

```bash
git fetch origin
git checkout v8.0.0
cd deepseek-harness-go
go build -o dsh.exe ./cmd/dsh
```

**配置变更**（必须）：

- `DSH_SERVER_AUTH_TOKEN` **强制**（启动 `-serve` 时）
- v8 Console 的 `/api/v1/console/*` 走与 v4 Gateway 不同的 token 路径
- 如使用 `DSH_AGENT_WORKSPACE_ROOT`，保持不变

**新增子模块**（可选）：

```bash
# 桌面启动器（独立 Go module）
cd desktop
go build -o dsh-desktop.exe .
# 或嵌入模式（需要 dsh.exe 在 desktop 父目录）
pwsh -File build-windows.ps1 -Embed
```

### 不兼容变更

- `dsh -serve` 启动时**必须**有 `DSH_SERVER_AUTH_TOKEN`（v7 仅在 cfg.server.enabled=true 时需要）
- 新增 `/console/` 路径（占用顶级 prefix）；如有反向代理需排除此路径
- Console 路由 `/api/v1/console/*` 与 v4 Gateway 的 `/api/v1/*` 并存（不冲突）

## 🐛 已修复

- (无 — v8.0 是纯增量版本)

## ⚠️ 已知限制

1. **单 Bearer token** — v8.0 不支持多用户/角色；v8.1 引入本地用户。
2. **国际化** — v8.0 仅 zh-CN；**v8.0-P0 增量**：新增 en-US 切换（顶栏 🌐）。
3. **Wails 桌面壳** — v8.0 仅提供启动器模式（浏览器 + 系统 WebView）；原生 Wails 壳留 v8.0.1。
4. **移动端** — 不优化手机屏幕（仅 1024px+ 桌面）。
5. **Tailwind** — v8.0 用原生 CSS + Vars；v8.1 引入 Tailwind。

## ✅ 验证

### v8.0.0 基础
- `go test ./...` — **41 个 internal 包 + 1 个 sdk-go 包 + 1 个 desktop 包全绿**
- `vue-tsc --noEmit` — **零错误**
- `pnpm build` / `npx vite build` — **总 gzip ~123 KB**（远低于 300 KB 预算）
- 手工 E2E：见 `docs/v8/TEST-CASES.md`（8 条全通过）

### v8.0.0-P0 增量
- `go test -count=1 ./internal/...` — **全绿（含新增 handler_p0_test）**
- `vue-tsc --noEmit` — **零错误**
- `pnpm build` — gzip ≈ **130 KB**（仅 +7 KB）
- 手工 E2E：`docs/v8/TEST-CASES.md` **TC-v8-0009 ~ 0016**（8 条新增）

## 📚 文档

- 设计：`docs/v8/DESIGN-v8.md`
- 计划：`docs/v8/PHASE-8-PLAN.md`
- 测试：`docs/v8/TEST-CASES.md`
- 启动器：`desktop/README.md` + `desktop/BUILD.md`
- 路线：`docs/0-ROADMAP.md` v0.8
