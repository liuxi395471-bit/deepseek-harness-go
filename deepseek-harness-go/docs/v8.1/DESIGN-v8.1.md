# DESIGN-v8.1：本地用户 + Spill + Wails + Schedule/Webhook

> **版本**：DESIGN-v8.1 v1.0（2026-09-27）
> **状态**：✅ 已交付
> **配套**：`PHASE-8.1-PLAN.md`、`RELEASE-NOTES.md`、`TEST-CASES.md`
> **依赖**：v8.0.0（含 P0 增量）✅

---

## §0 一句话设计目标

把 v8.0 "能用" 的控制台补成 "产品级" 控制台：
- **多用户登录**：本地账号 + JWT，替代 v8.0 的单 token 鉴权
- **流式断点续传**：长任务中途中断（断网 / 浏览器关闭）能继续读历史事件
- **调度与 Webhook**：让 dsh 从 "被动控制台" 升级为 "可自动化触发器"
- **真原生桌面壳**：把 web 控制台塞进 WebView2 / Wails 窗口，体验更紧

---

## §1 设计原则

| 原则 | 含义 |
|---|---|
| **零外部三方** | PBKDF2 / JWT-HS256 / 5 字段 cron 全部用 stdlib 实现；不引入 `bcrypt` / `jwt-go` / `robfig/cron` |
| **向后兼容** | `DSH_SERVER_AUTH_TOKEN` 单 token 鉴权仍可用，新用户系统可关闭 |
| **数据零迁移** | v8.0 → v8.1 所有新表 `CREATE TABLE IF NOT EXISTS`，存量数据 100% 保留 |
| **断点续传优先** | Spill 协议核心：服务端 = 真源（事件表），客户端 = 增量拉（since_seq） |
| **可见性优先** | Schedule / Webhook 失败必落审计；前端列表显式标红 |

---

## §2 体系结构（v8.1 增量）

```
                         ┌──────────────────────────────┐
                         │  v8.0: HTTP console + auth   │
                         │  ┌────────────────────────┐  │
                         │  │ /api/v1/console/*      │  │
                         │  └────────────────────────┘  │
                         └──────────────────────────────┘
                                       ▲
       ┌───────────────────────────────┼───────────────────────────────┐
       │                               │                               │
┌──────┴───────┐               ┌───────┴────────┐               ┌───────┴────────┐
│ v8.1 P9-1    │               │ v8.1 P9-2      │               │ v8.1 P9-3      │
│ Auth         │               │ Spill          │               │ Sched/Webhook  │
│              │               │                │               │                │
│ • users 表   │               │ • since_seq    │               │ • schedules    │
│ • JWT-HS256  │               │ • events 表    │               │ • cron 5-field │
│ • PBKDF2     │               │ • SSE 续传     │               │ • webhook q    │
│ • blacklist  │               │ • 前端自动重连 │               │ • HMAC 签名    │
└──────────────┘               └────────────────┘               └────────────────┘
                                       │
                          ┌────────────┴────────────┐
                          │ v8.1 P9-4 Wails v2      │
                          │ • wails_app scaffold    │
                          │ • embed dsh.exe         │
                          │ • WebView2 渲染         │
                          └─────────────────────────┘
```

---

## §3 P9-1：本地用户 + JWT + 审计导出

### 3.1 用户模型

```sql
CREATE TABLE users (
    id            TEXT PRIMARY KEY,           -- ULID
    username      TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,              -- pbkdf2-sha256$iter$salt$hash
    role          TEXT NOT NULL,              -- admin | user
    created_at    INTEGER NOT NULL,
    last_login_at INTEGER
);
```

- 默认 root 用户在 `cmd/dsh/main.go` 启动时构造：
  - `DSH_ADMIN_USER` / `DSH_ADMIN_PASSWORD` 设置则按值注入
  - 否则随机生成（启动日志一次性打印，运维须立即抄下）

### 3.2 密码哈希（PBKDF2-HMAC-SHA256）

```
password_hash = "pbkdf2-sha256$<iter>$<base64-salt>$<base64-hash>"
```

| 参数 | 值 |
|---|---|
| 算法 | PBKDF2-HMAC-SHA256 |
| 迭代次数 | 100 000 |
| 盐长度 | 16 字节随机 |
| 摘要长度 | 32 字节 |

零外部依赖：`crypto/pbkdf2` + `crypto/sha256`。

### 3.3 JWT（HS256）

- 头部：`{"alg":"HS256","typ":"JWT"}`
- 载荷：`{sub, role, exp, iat}`
- 签名：`HMAC-SHA256(header.payload, secret)`
- secret 来源：
  - `DSH_JWT_SECRET` 环境变量
  - 否则启动时生成 32 字节随机，写入 `<state-dir>/jwt.key`（0600 权限）
- 默认 TTL：24 小时

### 3.4 双鉴权中间件（`authedHandler`）

```go
func authedHandler(...) http.Handler {
    if cfg.JWTSecret == "" && cfg.AuthToken == "" {
        return 503
    }
    return http.HandlerFunc(func(w, r) {
        // 1. 解析 Bearer
        tok := bearer(r)
        // 2. JWT 优先
        if claims := jwtVerify(tok, secret); claims != nil && !blacklist.Has(tok) {
            ctx := withUser(r.Context(), claims)
            next.ServeHTTP(w, r.WithContext(ctx))
            return
        }
        // 3. fallback 静态 token（兼容 v8.0）
        if tok == cfg.AuthToken {
            next.ServeHTTP(w, r)  // 注：legacy 用户视为 anonymous
            return
        }
        401
    })
}
```

### 3.5 审计导出增强

| 能力 | Query |
|---|---|
| 时间过滤 | `?since=2026-09-01T00:00:00Z`（RFC3339/Nano） |
| 事件过滤 | `?event=model.create` |
| 会话过滤 | `?sid=01H...` |
| 限制 | `?limit=1000` |
| CSV 流式 | `?format=csv`（RFC 4180 quoting） |

JSONL 导出（v8.0）继续保留；CSV 仅作为可选 format。

---

## §4 P9-2：Spill 流式断点续传

### 4.1 设计动机

v8.0 的 SSE 是 `EventSource` 直连 v4 Gateway。一旦断网 / 关闭浏览器 / 重启 dsh，事件流就**永久丢失**——长任务（10 分钟+）用户体验差。

### 4.2 服务端真源

事件本身由 `store.EventStore` 持久化（v4 即已存在）；v8.1 不另起一套 checkpoint 表，而是让 SSE handler 直接从 events 表读：

```
GET /api/v1/console/sessions/{sid}/events?since=<seq>&limit=<n>
  → 200 application/x-ndjson
    {"seq":N, "event":{...}, "ts":...}
    ...
```

### 4.3 SSE 客户端续传

`web/src/utils/sse.ts` 实现：

```
class ReconnectingSSE {
  - url: string
  - since_seq: number           // 上次收到的最大 seq
  - retry_ms: 1000 → 30000      // 指数退避
  - on_event: (e) => void

  connect():
    - 打开 EventSource(`${url}?since=${since_seq}`)
    - onmessage: 解析 seq，更新 since_seq，调用 on_event
    - onerror:
        退避后重连（since_seq 保持不变，所以不丢事件）
```

**幂等保证**：服务端按 seq 排序返回，客户端按 seq 去重。

### 4.4 前端可见性

`SessionDetailView.vue`：
- 顶部 progressBar：`received_seq / last_seq`
- 断线时显示 "Reconnecting…" 黄条
- 重连后 progressBar 继续填充
- "Last event at HH:MM:SS" 实时刷新

---

## §5 P9-3：Schedule + Webhook

### 5.1 Schedule（5 字段 cron）

```
┌───────── 分钟 (0 - 59)
│ ┌─────── 小时 (0 - 23)
│ │ ┌───── 日 (1 - 31)
│ │ │ ┌─── 月 (1 - 12)
│ │ │ │ ┌─ 周 (0 - 6, 0 = 周日)
│ │ │ │ │
* * * * *
```

支持：`*`, `5`, `*/10`, `1-5`, `1,3,5`。

```
schedules(
    id TEXT PK,
    name TEXT,
    cron_expr TEXT,
    action_json TEXT,           -- {"type":"webhook_dispatch","webhook_id":"..."}
    enabled BOOL,
    created_at INTEGER,
    last_run_at INTEGER,
    next_run_at INTEGER
)
```

`schedule.Worker`：每 30 秒扫一次 enabled schedules；若 `now >= next_run_at`，触发 action_json，更新 `last_run_at` 和 `next_run_at`。

### 5.2 Webhook Dispatcher

```
webhooks(
    id TEXT PK,
    name TEXT,
    url TEXT,
    secret TEXT,                -- 仅创建时返回一次
    enabled BOOL,
    created_at INTEGER
)

webhook_deliveries(
    id TEXT PK,
    webhook_id TEXT,
    payload TEXT,
    status_code INTEGER,
    attempt INTEGER,
    delivered_at INTEGER,
    error TEXT
)
```

**队列**：
- 5 goroutine worker pool
- channel buffer 1000
- 失败重试：1s / 5s / 30s / 300s / 1800s（共 5 次）

**HMAC 签名**：
```
X-DSH-Signature: sha256=<hex(HMAC-SHA256(secret, body))>
```

**REST**：
| Method | Path | 功能 |
|---|---|---|
| GET | `/webhooks` | list |
| POST | `/webhooks` | create（返回 secret 一次） |
| PUT | `/webhooks/{id}` | update |
| DELETE | `/webhooks/{id}` | delete |
| POST | `/webhooks/{id}/test` | 立即触发 test payload |
| GET | `/webhooks/{id}/deliveries` | history（最近 100） |

### 5.3 Schedule 触发 Webhook

```json
{
  "type": "webhook_dispatch",
  "webhook_id": "01H..."
}
```

Worker 命中后调用 `webhook.Dispatcher.Dispatch(webhookID, payload)`，payload 包含：

```json
{
  "schedule_id": "01H...",
  "schedule_name": "...",
  "fired_at": "2026-09-27T11:00:00Z"
}
```

---

## §6 P9-4：Wails v2 真原生壳

### 6.1 决策

| 方案 | 选择 |
|---|---|
| **保留启动器模式** | ✅ `desktop/dsh-desktop.exe`（v8.0 起就有） |
| **新增 Wails v2 壳** | ✅ `desktop/wails_app/`（v8.1） |
| **强制 Wails** | ❌ 仅 Windows 验证；mac/linux 留 v8.2 |

### 6.2 启动器模式（v8.0 既有）

```
dsh-desktop.exe
  → spawn dsh.exe
  → 打开浏览器到 http://127.0.0.1:<port>/console/
```

**优点**：跨平台零依赖、UX 接近原生
**缺点**：浏览器 tab 多时会忘

### 6.3 Wails v2 模式（v8.1 新增）

```
dsh-wails.exe  (build via Wails CLI)
  → embed frontend/dist/ (web/dist 拷贝)
  → embed dsh.exe (Go embed + runtime extract)
  → spawn dsh.exe → http://127.0.0.1:<port>/
  → 渲染 frontend 到 WebView2 窗口
  → Tray 菜单：Show / Hide / Quit
```

**优点**：原生窗口、Tray、单一可执行文件
**缺点**：跨平台 CI 资源消耗大

### 6.4 复用 web/dist/

构建脚本（`desktop/build-wails-windows.ps1`）：
```powershell
Copy-Item -Recurse -Force ..\..\web\dist frontend\dist
wails build -tags wails_desktop -platform windows/amd64
```

frontend 只保留 Wails 运行时 wrapper（`window.go.main.App`），其它代码全部从 `web/` 维护，避免双份维护。

---

## §7 兼容性矩阵

| 场景 | v8.0 | v8.1 |
|---|---|---|
| 单 token 鉴权（`DSH_SERVER_AUTH_TOKEN`） | ✅ | ✅（fallback） |
| 多用户 JWT | ❌ | ✅ |
| SSE 断线即丢 | ✅ | ❌（since_seq 续传） |
| 定时任务 | ❌ | ✅（cron + action_json） |
| Webhook 推送 | ❌ | ✅（HMAC + retry） |
| 控制台 | Web（浏览器） | Web（浏览器）/ Wails v2 |
| 审计导出 | JSONL | JSONL + CSV + since/limit |

---

## §8 数据模型变更总览

| 表 | 动作 | 字段 |
|---|---|---|
| `users` | 新增 | id, username, password_hash, role, created_at, last_login_at |
| `schedules` | 新增 | id, name, cron_expr, action_json, enabled, created_at, last_run_at, next_run_at |
| `webhooks` | 新增 | id, name, url, secret, enabled, created_at |
| `webhook_deliveries` | 新增 | id, webhook_id, payload, status_code, attempt, delivered_at, error |
| `events` | 不变（已有 seq 主键） | — |
| `audit_events` | 不变 | — |

所有新表：`CREATE TABLE IF NOT EXISTS`，v8.0 → v8.1 零迁移。

---

## §9 风险与缓解

| # | 风险 | 缓解 |
|---|---|---|
| R1 | 自实现 JWT / PBKDF2 易出错 | 单测覆盖签名 / 篡改 / 过期 / 不同密钥；OWASP 推荐参数 |
| R2 | Spill 续传期间 events 表快速膨胀 | 仅记录增量 seq；events 已有 (sid, seq) 索引 |
| R3 | 自实现 5 字段 cron 边界（夏令时 / 月末） | 单测覆盖 * / 步长 / 范围 / 列表；不依赖时区（统一 UTC） |
| R4 | Wails v2 跨平台 CI 资源消耗大 | v8.1 仅 Windows；mac/linux 留 v8.2 |
| R5 | Webhook secret 泄漏 | UI 仅首次创建时展示一次；库内 hash 储存 |
| R6 | Schedule 误触发（高频 cron） | cron 表达式校验拒绝 `* * * * *` 之外 + 显式 enable 确认 |
