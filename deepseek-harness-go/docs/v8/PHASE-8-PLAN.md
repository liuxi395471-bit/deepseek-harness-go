# PHASE-8-PLAN — Web Console + Desktop（v8.0）

> **版本**：PHASE-8-PLAN v0.1（2026-09-27）
> **配套设计**：`docs/v8/DESIGN-v8.md`
> **配套路线**：`docs/0-ROADMAP.md §v8`
> **依赖**：v7.0.0（tag `cc1dd87`）+ DESIGN-v8 v0.1
> **目标工时**：8–10 天（单线开发）

---

## §0 阶段概览

| 子阶段 | 主题 | 工时 | 优先级 | 验证 |
|---|---|---|---|---|
| **P8-1** | 后端 Console 域 + REST API + embed.FS | 2.5d | 🔴 必经 | `go test ./internal/console/...` ✅ |
| **P8-2** | 前端 Vue 3 SPA（5 页 + 组件） | 3d | 🔴 必经 | `pnpm build` ✅ + 手工 E2E 8 条 |
| **P8-3** | Desktop（Wails v2）+ 收尾 + RELEASE | 2.5d | 🟡 必经 | `wails build` 三平台 ✅ |

> **用户决策（2026-09-27）**：Web 技术栈 = Vite + Vue 3；可选子项 = + Desktop（不含 Spill / Browser-Use，留 v8.1）。

---

## §1 P8-1 — 后端 Console 域（2.5d）

### §1.1 文件清单

```
internal/console/
├── console.go              ← ConsoleServer 结构 + Start(ctx) + 路由注册
├── embed.go                ← //go:embed all:web/dist → embed.FS
├── auth.go                 ← Token 中间件
├── handler_sessions.go     ← GET/POST/DELETE /sessions + /sessions/:sid/messages
├── handler_plugins.go      ← GET /plugins + /plugins/:name/enable|disable
├── handler_models.go       ← GET/PUT /models + /models/:channel/ping
├── handler_tasks.go        ← GET /tasks + /tasks/:id/cancel
├── handler_approval.go     ← GET /approvals + /approvals/:id/decide
├── handler_events.go       ← GET /events SSE 转发
├── handler_health.go       ← GET /health
├── errors.go               ← 错误码定义
└── *_test.go               ← 单元 + 集成测试
```

### §1.2 ConsoleServer 骨架

```go
type ConsoleServer struct {
    Store    *store.Store
    Plugin   *plugin.Registry
    Runtime  *runtime.ChannelResolver
    Approve  *approval.Service
    Task     *task.Service
    Token    string
    FS       embed.FS
    Logger   *slog.Logger
    Gateway  *server.Gateway    // 复用 v4 Gateway SSE
}

func (s *ConsoleServer) Routes() http.Handler {
    mux := http.NewServeMux()
    mux.HandleFunc("GET /api/v1/console/health", s.health)
    auth := s.authMiddleware
    mux.Handle("GET /api/v1/console/sessions", auth(s.listSessions))
    mux.Handle("POST /api/v1/console/sessions", auth(s.createSession))
    mux.Handle("GET /api/v1/console/sessions/{sid}", auth(s.getSession))
    mux.Handle("DELETE /api/v1/console/sessions/{sid}", auth(s.deleteSession))
    mux.Handle("POST /api/v1/console/sessions/{sid}/messages", auth(s.postMessage))
    // … plugins / models / tasks / approvals / events
    mux.Handle("GET /console/", s.spaHandler())   // SPA fallback to index.html
    mux.Handle("GET /console", http.RedirectHandler("/console/", 301))
    return mux
}
```

### §1.3 关键实现

- **embed.FS**：路径 `web/dist`；开发期可被 `go run -tags devconsole` 替换为本地磁盘（`go:build !devconsole`）。
- **SPA fallback**：`/console/foo/bar` 若文件不存在 → 返回 `index.html`，由前端 router 接管。
- **SSE 转发**：`/api/v1/console/events` 把 `s.Gateway.Subscribe(sources...)` 的 channel 直接 pipe 到 `http.Flusher`。
- **鉴权中间件**：除 `/health` 外全部要求 `Authorization: Bearer <token>`；缺/错返回 401。
- **traceId**：每次请求用 `obs.NewTraceID()` 注入 context 与日志。

### §1.4 测试（≥ 5 个）

- `TC-P8-1-01` `auth_test`：缺 token → 401；错 token → 401；正确 token → 200
- `TC-P8-1-02` `sessions_test`：list / create / get / delete 全链路 + DB 一致
- `TC-P8-1-03` `plugins_test`：enable/disable 改 `plugin_status` 表 + 内存 Registry 同步
- `TC-P8-1-04` `models_test`：PUT 改 `channels` 表；ping 走真实 HTTP（用 `httptest.Server` mock upstream）
- `TC-P8-1-05` `embed_test`：`embed.FS` 至少存在 `index.html` 且可读

### §1.5 验收

```bash
cd deepseek-harness-go
go test ./internal/console/...        # ✅
go vet ./internal/console/...         # ✅
go build ./...                        # ✅
```

---

## §2 P8-2 — 前端 Vue 3 SPA（3d）

### §2.1 目录骨架

```
web/
├── package.json
├── pnpm-lock.yaml
├── vite.config.ts
├── tsconfig.json
├── index.html
└── src/
    ├── main.ts                 ← createApp + pinia + router + vue-query
    ├── router.ts
    ├── api/
    │   ├── client.ts           ← axios 实例 + interceptor
    │   ├── sessions.ts
    │   ├── plugins.ts
    │   ├── models.ts
    │   ├── tasks.ts
    │   └── approvals.ts
    ├── stores/
    │   ├── useUI.ts            ← 主题 / 侧栏折叠
    │   └── useToken.ts
    ├── pages/
    │   ├── Sessions.vue
    │   ├── SessionDetail.vue
    │   ├── Plugins.vue
    │   ├── Models.vue
    │   ├── Tasks.vue
    │   └── Approvals.vue
    ├── components/
    │   ├── AppShell.vue        ← 顶栏 + 侧栏 + 内容区
    │   ├── ChatBubble.vue
    │   ├── MarkdownView.vue
    │   ├── PluginCard.vue
    │   ├── ModelForm.vue
    │   ├── TaskRow.vue
    │   └── ApprovalCard.vue
    └── styles/
        ├── tokens.css          ← CSS 变量（深浅主题）
        └── base.css
```

### §2.2 关键依赖

```jsonc
{
  "dependencies": {
    "vue": "^3.4",
    "vue-router": "^4.3",
    "pinia": "^2.1",
    "@tanstack/vue-query": "^5.40",
    "axios": "^1.7",
    "marked": "^12.0",
    "dompurify": "^3.1",
    "highlight.js": "^11.9",
    "lucide-vue-next": "^0.400"
  },
  "devDependencies": {
    "vite": "^5.2",
    "@vitejs/plugin-vue": "^5.0",
    "typescript": "^5.4",
    "vue-tsc": "^2.0",
    "vitest": "^1.6",
    "@vue/test-utils": "^2.4",
    "happy-dom": "^14.0"
  }
}
```

### §2.3 路由表

| Path | Page | 备注 |
|---|---|---|
| `/` | 重定向到 `/sessions` | — |
| `/sessions` | Sessions.vue | 列表 |
| `/sessions/:sid` | SessionDetail.vue | 详情 + 发送 |
| `/plugins` | Plugins.vue | — |
| `/models` | Models.vue | — |
| `/tasks` | Tasks.vue | — |
| `/approvals` | Approvals.vue | — |

### §2.4 实现顺序

1. **骨架**（半天）：vite + vue + ts + router + pinia + axios + AppShell。
2. **Sessions + SessionDetail**（1d）：列表 / 详情 / 发送 / SSE 流式渲染。
3. **Plugins + Models**（0.5d）：卡片网格 / 表格编辑。
4. **Tasks + Approvals**（0.5d）：进度条 / 三按钮审批。
5. **打磨**（半天）：loading / empty / error / 暗色主题 / 响应式。

### §2.5 测试（≥ 6 个，vitest）

- `TC-P8-2-01` `AppShell.test`：渲染顶栏 + 侧栏
- `TC-P8-2-02` `ChatBubble.test`：user/assistant/tool 三种角色
- `TC-P8-2-03` `MarkdownView.test`：marked + DOMPurify 不注入 `<script>`
- `TC-P8-2-04` `PluginCard.test`：status 徽标切换
- `TC-P8-2-05` `ModelForm.test`：编辑保存触发 axios
- `TC-P8-2-06` `useToken.test`：本地存取

### §2.6 验收

```bash
cd web
pnpm install
pnpm build                # → dist/（≤ 300KB gzip 主 chunk）
pnpm test                 # ≥ 6 个 vitest 通过
pnpm typecheck            # vue-tsc 0 error
```

### §2.7 包体预算

| chunk | 预算（gzip） |
|---|---|
| main | ≤ 60 KB |
| vendor-vue | ≤ 90 KB |
| vendor-marked | ≤ 30 KB |
| vendor-highlight | ≤ 40 KB |
| vendor-axios | ≤ 15 KB |
| 单页面（按需） | ≤ 25 KB |
| **首屏总请求** | **≤ 300 KB** |

CI 校验：`pnpm build --mode analyze` 输出 budget 报告，超阈值 PR 阻断。

---

## §3 P8-3 — Desktop + 收尾（2.5d）

### §3.1 Wails 骨架

```
desktop/
├── main.go                  ← Wails entry（启动 ds-go headless + 打开浏览器）
├── wails.json
├── go.mod                   ← module deepseek-harness-go/desktop
├── build/
│   ├── appicon.png
│   ├── windows/info.json
│   ├── darwin/Info.plist
│   └── linux/main.desktop
└── README.md
```

### §3.2 main.go 核心

```go
package main

import (
    "embed"
    "github.com/wailsapp/wails/v2"
    "github.com/wailsapp/wails/v2/pkg/options"
)

type App struct {
    dshCmd *exec.Cmd
    cancel context.CancelFunc
}

//go:embed all:build/appicon.png
var iconFS embed.FS

func (a *App) startup(ctx context.Context) {
    a.startDSH(ctx)  // 释放嵌入的 dsh 二进制，--serve 启动
}

func (a *App) shutdown(ctx context.Context) {
    a.cancel()
    a.dshCmd.Wait()
}

func main() {
    app := &App{}
    err := wails.Run(&options.App{
        Title:  "DeepSeek Harness",
        Width:  1280, Height: 800,
        AssetServer: &assetserver.Options{Assets: assetFS},
        OnStartup:   app.startup,
        OnShutdown:  app.shutdown,
        Bind:        []interface{}{app},
    })
    if err != nil { log.Fatal(err) }
}
```

### §3.3 打包产物

| 平台 | 命令 | 产物 |
|---|---|---|
| Windows | `wails build -platform windows/amd64` | `build/bin/deepseek-harness-desktop-amd64.exe` |
| macOS | `wails build -platform darwin/universal` | `build/bin/DeepSeek Harness Desktop.app` |
| Linux | `wails build -platform linux/amd64` | `build/bin/deepseek-harness-desktop-amd64.AppImage` |

> **二进制嵌入**：`desktop/dsh_embedded.go` 用 `//go:embed all:../dsh/dist/dsh.exe`（Windows）/ `dsh`（Linux）/ `dsh-darwin`（macOS）把 v8 构建的 ds-go 二进制塞进 Wails 二进制。

### §3.4 收尾交付

| 任务 | 文件 |
|---|---|
| 文档 | `docs/v8/RELEASE-NOTES.md`（含变更 / 迁移 / 已知限制） |
| 文档 | `docs/v8/TEST-CASES.md`（≥ 8 用例） |
| 文档 | `docs/v8/README.md`（5 页截图 + Desktop 三平台截图） |
| 文档 | 更新根 `README.md` 增加 v8 徽标 + Desktop 段落 |
| 路线 | 更新 `docs/0-ROADMAP.md §v8` 状态 ✅ |
| 示例 | `web/src/api/types.ts` 导出 SDK 类型（供下游复用） |
| CI | `.github/workflows/ci.yml` 增加 `pnpm install && pnpm build` 步骤 |

### §3.5 验收

- `go test ./internal/console/...` ✅
- `pnpm -C web test && pnpm -C web build` ✅
- `cd desktop && wails build -platform windows/amd64` ✅（macOS / Linux 由 CI 跑）
- `wails build` 产物启动后 5s 内 `curl http://127.0.0.1:7777/console/` 返回 200
- 全部 v3/v4/v5/v6/v7 既有测试不回归
- `go test -race ./...` 无数据竞争

---

## §4 用例清单（TC-v8-0001 ~ TC-v8-0008）

| ID | 类型 | 描述 |
|---|---|---|
| TC-v8-0001 | 后端 | GET /health 无 token 仍返回 200 |
| TC-v8-0002 | 后端 | GET /sessions 缺 token 返回 401 |
| TC-v8-0003 | 后端 | POST /sessions 创建后能立即 GET 到 |
| TC-v8-0004 | 后端 | POST /sessions/:sid/messages 触发 SSE 流 |
| TC-v8-0005 | 后端 | POST /plugins/:name/enable 写 plugin_status 并 reload |
| TC-v8-0006 | 后端 | PUT /models/:channel 修改后 GET 立即生效 |
| TC-v8-0007 | 前端 | Sessions 页面加载 → 列表渲染 → 详情 → 发送 → 流式回复 |
| TC-v8-0008 | 端到端 | `wails build` 三平台产物启动 → `/console/` 返回 200 + 健康检查 200 |

---

## §5 风险与回退

| # | 风险 | 触发条件 | 应对 |
|---|---|---|---|
| R1 | Wails 跨平台打包失败 | 任一平台 24h 不解决 | v8.0.0 仅打 tag，Desktop 文档降级；v8.0.1 补齐 |
| R2 | Vite 主 chunk 超 300KB | CI budget 阻断 | 移除 highlight.js（运行时按需加载）或换成 shiki |
| R3 | embed.FS 在 Windows 大小写 | `curl /Console/` 404 | 强制 lowercase + CI 实测 |
| R4 | SSE 转发性能差 | 单前端 tab CPU > 30% | 改为 WebSocket（v8.1） |
| R5 | 鉴权弱导致误用 | 用户公网部署 | README 警告 + v8.1 强鉴权 |

---

## §6 时间线（建议）

```
Day 1 ── P8-1 启动 / 路由 + embed.FS / auth / health
Day 2 ── P8-1 sessions / plugins / models handlers
Day 3 ── P8-1 tasks / approvals / SSE / 测试 ✅
Day 4 ── P8-2 vite + vue 骨架 + AppShell + Sessions
Day 5 ── P8-2 SessionDetail 流式 + Plugins + Models
Day 6 ── P8-2 Tasks + Approvals + 打磨 + 测试 ✅
Day 7 ── P8-3 Wails 骨架 + main.go + 嵌入 dsh 二进制
Day 8 ── P8-3 三平台 wails build + 修复 + RELEASE-NOTES
Day 9 ── P8-3 README + 路线图 + CI + 收尾
Day 10 ── 缓冲 / PR review / tag v8.0.0
```

---

## §7 验收总览

- [ ] `go test ./...` 全绿（含 v3/v4/v5/v6/v7/v8）
- [ ] `pnpm -C web build` 成功且主 chunk ≤ 300KB gzip
- [ ] `pnpm -C web test` ≥ 6 个 vitest 通过
- [ ] `cd desktop && wails build` 至少 Windows 平台成功
- [ ] `curl http://127.0.0.1:7777/console/` 返回 200
- [ ] 5 个页面手工 E2E 全通过
- [ ] `docs/v8/RELEASE-NOTES.md` 完成
- [ ] `docs/v8/TEST-CASES.md` ≥ 8 用例
- [ ] `docs/0-ROADMAP.md` v8 状态 ✅
- [ ] tag `v8.0.0` 推送成功

---

**PHASE-8-PLAN 完成**：本计划是 v8 实施依据，启动时按 P8-1 → P8-2 → P8-3 顺序执行。
