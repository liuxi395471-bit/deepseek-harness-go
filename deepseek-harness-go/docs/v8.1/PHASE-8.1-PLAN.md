# PHASE-8.1-PLAN：本地用户 + Spill + Wails + Schedule/Webhook

> **版本**：PHASE-8.1-PLAN v0.1（2026-09-27）
> **状态**：🚀 启动
> **配套**：`docs/v8.1/DESIGN-v8.1.md`、`docs/v8.1/RELEASE-NOTES.md`、`docs/v8.1/TEST-CASES.md`
> **依赖**：v8.0.0（含 P0 增量）✅

---

## §0 一句话目标

> 把 v8.0 "能用" 的控制台补成 "产品级" 控制台：**多用户登录 + 流式断点续传 + 调度 / Webhook + 真原生桌面壳**。

---

## §1 范围（v8.1）

| ID | 名称 | 子项 | 工时 | 状态 |
|---|---|---|---|---|
| **P9-1** | 本地用户 + JWT + 审计导出增强 | users 表、PBKDF2、JWT 签发、登录页、bearer 中间件兼容、CSV 导出 | 3d | ✅ |
| **P9-2** | Spill 流式断点续传 | `since=seq` 查询、SSE 自动重连 + since | 2d | ✅ |
| **P9-3** | Schedule + Webhook | schedules 表 + cron worker；webhooks 表 + delivery 队列 + HMAC | 3d | ✅ |
| **P9-4** | Wails v2 真原生壳 | Wails CLI 集成、复用 `web/dist/`、嵌入 dsh.exe、Win 验证 | 4d | ✅（脚手架就位；启动器模式兜底） |
| **P9-5** | 收尾 | docs + 全量测试 + tag v8.1.0 | 1d | 🚧 |

---

## §2 后端扩展

### P9-1 用户系统

- `internal/auth/`（新增）：users 表 + bcrypt 哈希 + JWT 签发（HS256，无三方）
  - `users(id, username, password_hash, role, created_at, last_login_at)`
  - 默认 root 用户（从 `DSH_ADMIN_USER` / `DSH_ADMIN_PASSWORD` 注入；空则随机生成并打印到启动日志）
- `internal/console/auth_handler.go`：新增 REST
  - `POST /auth/login` → `{token, expiresAt, user}`
  - `POST /auth/logout` → 黑名单（内存 LRU，TTL 1h）
  - `GET /auth/me` → 当前用户
  - `PUT /auth/users`（仅 admin）→ 创建用户
- 改造 `bearerAuth`：先尝试 JWT（带 user 上下文到 `r.Context()`），fallback 到 `DSH_SERVER_AUTH_TOKEN`（兼容 v8.0 单 token）

### P9-2 Spill 续传

- 新增 `internal/spill/`：`StreamCheckpoint(sid, last_seq, last_event_ts)`
  - 每次 `AppendEvent` 后更新（异步）
  - `GetCheckpoint(sid)` 返回 `{seq, ts}`；SSE handler 接受 `?since_seq=N`，从该位置往后发
- `internal/console/events.go`：SSE 改造
  - 客户端首次连接传 `?since_seq=0`（或 last），服务端读 events 表后 push 历史，然后接 v4 Gateway SSE
  - 前端 SSE handler 用 `EventSource` 原生 + onerror 自动 reconnect，传 last seq
- `internal/store/sqlite.go`：events 表 schema 不变；新增 `events_seq(sid, seq)` 索引已存在

### P9-3 Schedule + Webhook

- `internal/schedule/`：
  - `schedules(id, name, cron_expr, action_json, enabled, created_at, last_run_at, next_run_at)`
  - `Worker`：每 30s 扫一次；命中 cron 触发 `action_json`（type=agent_run / http_post / webhook_dispatch）
  - REST：`/api/v1/console/schedules` CRUD
- `internal/webhook/`：
  - `webhooks(id, name, url, secret, enabled, created_at)` + `webhook_deliveries(id, webhook_id, payload, status_code, attempt, delivered_at)`
  - 队列：内存 channel + worker 池（5 goroutine），失败指数退避（1s / 5s / 30s / 300s）；HMAC-SHA256 签名 header `X-DSH-Signature`
  - REST：`/api/v1/console/webhooks` CRUD + `/api/v1/console/webhooks/{id}/test`（立即触发）

### P9-4 Wails

- `desktop/wails_app/`（新建 Wails 项目）：
  - `wails init -n wails_app -t vue`
  - 修改 `frontend/src/main.js` 复用 `web/dist/` 的 build
  - `wails_app/main.go`：embed `dsh.exe`，spawn 进程 + open in WebView
- `desktop/build-windows-wails.ps1`：CI 脚本
- `desktop/wails.json` 已存在；保留为配置参考

---

## §3 前端扩展

### P9-1 Login

- `web/src/views/LoginView.vue`（新）：用户名 + 密码表单
- `web/src/stores/user.ts`（新）：JWT 存储（localStorage）+ user info
- `web/src/api/client.ts`：token 拦截器改造（JWT 优先；过期自动跳转 /login）
- `web/src/App.vue`：顶栏用户菜单（用户名 + 注销）

### P9-2 Spill 重连

- `web/src/views/SessionDetailView.vue`：SSE 客户端加 `since_seq`，重连时拉断点
- `web/src/utils/sse.ts`（新）：通用 EventSource with since_seq 包装器

### P9-3 Schedule / Webhook

- `web/src/views/SchedulesView.vue`（新）：cron 表达式输入 + 列表 + enable/disable + run now
- `web/src/views/WebhooksView.vue`（新）：CRUD + delivery history + test 按钮

### P9-4 Wails frontend

- `desktop/wails_app/frontend/` 由 Wails 生成；指向 `web/dist/` 静态文件
- 顶栏去 URL 输入（原生壳内无需 token 输入）

---

## §4 数据迁移

- v8.0 → v8.1：**零迁移**（无破坏性 schema 变更）
- 新增表：`users`、`schedules`、`webhooks`、`webhook_deliveries`、`stream_checkpoints` — 全部 `CREATE TABLE IF NOT EXISTS`

---

## §5 验证

| TC | 主题 | 自动化 |
|---|---|---|
| TC-v8-1001 | JWT 登录 + 多用户 CRUD | go test |
| TC-v8-1002 | bcrypt + token 过期 + logout 黑名单 | go test |
| TC-v8-1003 | Spill since_seq 断点续传 | go test |
| TC-v8-1004 | SSE 自动重连 + since | 手工 |
| TC-v8-1005 | cron 调度 + 触发 webhook | go test |
| TC-v8-1006 | Webhook 队列 + 重试 + HMAC 签名 | go test |
| TC-v8-1007 | Wails build（Windows）→ 启动 → 嵌入 dsh.exe | 手工 smoke |

---

## §6 风险

| # | 风险 | 应对 |
|---|---|---|
| R1 | JWT 无三方库 → 需手写 HS256 | 借用 `crypto/hmac` + `crypto/sha256`；小代码量、无 CVE |
| R2 | Spill 续传与 v4 Gateway SSE 转发冲突 | Console SSE 走自己 events 表；v4 Gateway 仅作 fallback |
| R3 | cron 解析复杂 | 用 `robfig/cron/v3`（已是 Go 生态最成熟，仅 1 个新三方） |
| R4 | Wails 跨平台 CI 资源消耗大 | v8.1 仅 Windows 验证；mac/linux 留 v8.2 |
| R5 | Webhook HMAC 密钥泄漏 | 写库前 hash；UI 仅显示一次（首次创建时） |

---

## §7 工期

- Day 1-3：P9-1（users + JWT + login UI）
- Day 4-5：P9-2（spill + SSE 重连）
- Day 6-8：P9-3（schedule + webhook）
- Day 9-12：P9-4（Wails 集成）
- Day 13：P9-5（文档 + tag）
