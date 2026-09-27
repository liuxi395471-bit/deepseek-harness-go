# RELEASE-NOTES v8.1.0（2026-09-27）

> **代号**：Product-Ready Console
> **配套**：`DESIGN-v8.1.md`、`PHASE-8.1-PLAN.md`、`TEST-CASES.md`
> **git tag**：`v8.1.0`

---

## 🎯 一句话

把 v8.0 "能用" 的控制台补成 "产品级" 控制台：**多用户登录 + 流式断点续传 + Schedule/Webhook + Wails v2 真原生壳**。

---

## ✨ 新增能力

### 🔐 P9-1：本地用户 + JWT + 审计导出增强

- **本地用户表** `users(id, username, password_hash, role, ...)` — 无三方依赖
- **PBKDF2-HMAC-SHA256** 密码哈希（100k iter / 16B salt / 32B hash）
- **JWT HS256** 签发与校验（`crypto/hmac` + `crypto/sha256`，零三方）
- **自动 root 用户**：通过 `DSH_ADMIN_USER` / `DSH_ADMIN_PASSWORD` 注入；否则启动时随机生成并打印
- **新 REST**：
  - `POST /api/v1/console/auth/login` → JWT
  - `POST /api/v1/console/auth/logout` → 黑名单
  - `GET /api/v1/console/auth/me` → 当前用户
  - `PUT /api/v1/console/auth/users`（admin）→ 创建用户
  - `GET /api/v1/console/auth/users` → 列表
  - `DELETE /api/v1/console/auth/users/{id}` → 删除
- **双鉴权中间件** `authedHandler`：JWT 优先 → fallback 静态 token（v8.0 兼容）
- **审计导出增强**：
  - `?since=RFC3339` / `?event=name` / `?sid=...` / `?limit=N`
  - `?format=csv` 流式 CSV（RFC 4180 quoting）

### 🔁 P9-2：Spill 流式断点续传

- **真源在服务端**：`GET /api/v1/console/sessions/{sid}/events?since=N&limit=M`
- **前端 SSE 自动重连**（`web/src/utils/sse.ts`）：
  - 退避 1s → 30s
  - since_seq 持久化（断线 → 重连不丢事件）
  - progressBar / Reconnecting 指示
- **零破坏性**：events 表 schema 不变；只在 SSE handler 入口加 since_seq 查询

### ⏰ P9-3：Schedule + Webhook（对齐 ds-ts）

- **5 字段 cron**（自实现，零三方）：`*`, `5`, `*/10`, `1-5`, `1,3,5`
- **schedules 表** + 后台 Worker（30s tick）
- **REST**：`/api/v1/console/schedules` CRUD + run-now
- **webhooks 表** + **5-worker 队列** + 指数退避（1s / 5s / 30s / 300s / 1800s）
- **HMAC-SHA256 签名** header：`X-DSH-Signature: sha256=<hex>`
- **REST**：`/api/v1/console/webhooks` CRUD + `/test` + `/deliveries`
- **前端**：`SchedulesView.vue` + `WebhooksView.vue`（合并 Settings/Notifications）

### 🖥 P9-4：Wails v2 真原生壳（Windows 验证）

- **新模块**：`desktop/wails_app/`
  - `app.go`（`//go:build wails_desktop`）— Wails 入口
  - `wails.json` — 项目配置
  - `README.md` — 构建步骤
- **构建脚本**：`desktop/build-wails-windows.ps1` — 同步 `web/dist/` + `wails build`
- **复用 `web/dist/`** — 单一前端源；避免双份维护
- **保留启动器模式** — `dsh-desktop.exe`（v8.0 既有）作为兜底

---

## 🧪 自动化测试

```
go test ./...                 → 41+ 包，100% pass
go test ./internal/auth       → 4 个测试（JWT 签验 / 过期 / 篡改 / 不同密钥）
go test ./internal/schedule   → 7 个测试（cron 解析 / Store / Worker 触发）
go test ./internal/webhook    → 6 个测试（签名 / CRUD / HTTP 分发 / 重试 / 历史）
go test ./internal/console    → 5 个测试（含 JWT fallback / CSV 导出）
```

前端：
```
vue-tsc --noEmit              → 0 errors
vite build                    → ~137 KB gzip
```

---

## 📦 数据模型

**新增 5 张表**（全部 `CREATE TABLE IF NOT EXISTS`，零迁移）：

| 表 | 用途 |
|---|---|
| `users` | 本地账号 + 角色 |
| `schedules` | cron 计划 + action_json |
| `webhooks` | 出口端点 + secret |
| `webhook_deliveries` | 投递历史 |
| `events` | **不变**（已支持 since_seq 查询） |

---

## 🔌 兼容性

| 场景 | v8.0 → v8.1 |
|---|---|
| `DSH_SERVER_AUTH_TOKEN` 单 token | ✅ 仍可用（fallback） |
| 现有 `audit_events` / `models` / `plugins` 等数据 | ✅ 完全保留 |
| 前端 SPA | ✅ `vue-tsc` 兼容；新增视图（Schedules / Webhooks / Login） |
| CLI 命令 | ✅ 不变 |
| 配置环境变量 | + `DSH_ADMIN_USER` / `DSH_ADMIN_PASSWORD` / `DSH_JWT_SECRET` |

---

## 🚀 升级指引（v8.0 → v8.1）

```bash
# 1. 拉取
git fetch --tags
git checkout v8.1.0

# 2. 编译
cd deepseek-harness-go
go build -o dsh.exe ./cmd/dsh
cd desktop
go build -tags embed_dsh -o dsh-desktop.exe .   # 启动器模式（v8.0 兼容）

# 3. 启动
./dsh.exe
#   → 自动生成 root 账号（控制台日志），复制保存
#   → 访问 http://127.0.0.1:<port>/console/login
```

**Wails 壳（可选）**：
```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@latest
pwsh -File build-wails-windows.ps1
# → build/bin/dsh-wails.exe
```

---

## ⚠️ 已知限制

| # | 描述 | 计划 |
|---|---|---|
| 1 | Wails v2 跨平台 CI 资源消耗大，v8.1 仅 Windows 验证 | mac/linux 留 v8.2 |
| 2 | JWT blacklist 在内存中（重启即失效） | 计划 v8.2 持久化 |
| 3 | 自实现 cron 不支持秒级精度 / 不支持时区 | v8.2 用 `time.FixedZone` 解决 |
| 4 | Webhook 无 IP 白名单 | v8.2 加 ACL |

---

## 🙏 致谢

v8.1 全部 P1 / P2 增量在 0 个新增三方 Go module 的前提下完成（PBKDF2 / JWT / cron 全部用 stdlib 实现）。
