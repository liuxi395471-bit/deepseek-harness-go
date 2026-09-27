# TEST-CASES v8.1.0（2026-09-27）

> **版本**：TEST-CASES v8.1 v1.0
> **配套**：`DESIGN-v8.1.md`、`PHASE-8.1-PLAN.md`、`RELEASE-NOTES.md`
> **测试基线**：`go test ./...` 全包通过；前端 `vue-tsc --noEmit` 0 errors

---

## §0 总览

| 模块 | TC 数 | 自动化 | 手工 |
|---|---|---|---|
| P9-1 用户 / JWT / 审计导出 | 14 | 14 | 0 |
| P9-2 Spill 续传 | 6 | 4 | 2 |
| P9-3 Schedule + Webhook | 13 | 11 | 2 |
| P9-4 Wails v2 | 3 | 0 | 3 |
| **合计** | **36** | **29** | **7** |

---

## §1 P9-1：本地用户 + JWT + 审计导出

### TC-v8.1-001  JWT 签发 + 校验（happy path）
- **前置**：启动 dsh（启用 JWT）
- **步骤**：`auth.SignToken({sub, role, exp, iat}, secret)`
- **断言**：`VerifyToken` 返回 claims；`exp` 正确；`role` 正确
- **自动化**：`internal/auth/auth_test.go::TestJWTSignAndVerify`

### TC-v8.1-002  JWT 过期拒绝
- **步骤**：构造 `exp = past` 的 token
- **断言**：`VerifyToken` 返回 `ErrExpired`
- **自动化**：`TestJWTExpired`

### TC-v8.1-003  JWT 篡改拒绝（signature 改一位）
- **步骤**：解码后改 signature 段最后 1 字符
- **断言**：`VerifyToken` 返回 `ErrBadSignature`
- **自动化**：`TestJWTTamperedSignature`

### TC-v8.1-004  JWT 不同密钥拒绝
- **步骤**：用 secret-A 签，secret-B 验
- **断言**：返回 `ErrBadSignature`
- **自动化**：`TestJWTDifferentSecret`

### TC-v8.1-005  POST /auth/login → JWT
- **步骤**：root 用户密码登录
- **断言**：200，返回 `{token, expiresAt, user}`
- **自动化**：`TestAuthAndHealth/health_no_token_returns_200` + 集成

### TC-v8.1-006  POST /auth/logout → 黑名单
- **步骤**：登录后立刻 logout，再用同 token 调 `/auth/me`
- **断言**：第二次 401（blacklisted）
- **自动化**：单测覆盖 blacklist 实现；e2e 手工

### TC-v8.1-007  GET /auth/me → 当前用户
- **步骤**：Bearer token，调 `/auth/me`
- **断言**：200，返回 `{username, role}`
- **自动化**：集成测试

### TC-v8.1-008  PUT /auth/users（admin）→ 创建用户
- **步骤**：admin 调 PUT，传 username+password
- **断言**：201，返回新 user id
- **自动化**：handler_v81_test

### TC-v8.1-009  非 admin 创建用户被拒
- **步骤**：user 角色调 PUT
- **断言**：403
- **自动化**：handler_v81_test

### TC-v8.1-010  bearer 中间件：JWT 优先
- **步骤**：同时配 DSH_SERVER_AUTH_TOKEN + JWT_SECRET；发 JWT token
- **断言**：r.Context() 含 user；通过
- **自动化**：`TestJWTFallbackToStaticToken`

### TC-v8.1-011  bearer 中间件：fallback 静态 token
- **步骤**：仅配 DSH_SERVER_AUTH_TOKEN；发 legacy token
- **断言**：通过（但 r.Context() 无 user claims）
- **自动化**：`TestJWTFallbackToStaticToken`

### TC-v8.1-012  audit 导出 ?since=RFC3339
- **步骤**：先制造 N 条 audit，调 `GET /audit?since=2026-09-01`
- **断言**：返回的事件全部 `ts >= since`
- **自动化**：handler_p0_test

### TC-v8.1-013  audit 导出 ?format=csv
- **步骤**：`GET /audit?format=csv`
- **断言**：Content-Type=text/csv；行首 `id,ts,event,sid,...`；含 RFC 4180 引号转义
- **自动化**：`TestExportAuditCSVFormat`

### TC-v8.1-014  audit 导出过滤 ?event=model.create
- **步骤**：先做多种 audit；过滤 event=model.create
- **断言**：返回全部为 model.create
- **自动化**：handler_p0_test

---

## §2 P9-2：Spill 流式断点续传

### TC-v8.1-015  GET /events?since=0 → 全部历史
- **步骤**：先发 5 个事件，调 `?since=0`
- **断言**：返回 5 个事件，seq 单调递增
- **自动化**：`store.TestEventStoreReadEvents`

### TC-v8.1-016  GET /events?since=N → 仅增量
- **步骤**：再发 3 个事件，调 `?since=5`
- **断言**：返回 3 个事件，seq 起始为 6
- **自动化**：`store.TestEventStoreReadEventsSince`

### TC-v8.1-017  前端 SSE 自动重连（断网 → 复网）
- **前置**：SessionDetailView 打开，长任务进行中
- **步骤**：DevTools → Network → Offline 5s → 取消 Offline
- **断言**：
  - 顶部出现 "Reconnecting…" 黄条
  - 复网后 progressBar 继续填充
  - **不丢任何事件**（对比收到的最大 seq）
- **自动化**：手工（E2E）

### TC-v8.1-018  退避策略：1s → 2s → 4s → 30s 封顶
- **步骤**：连续断网 60s
- **断言**：日志显示重试间隔 `1000ms, 2000ms, 4000ms, 8000ms, 16000ms, 30000ms`（封顶）
- **自动化**：手工（E2E）

### TC-v8.1-019  events 表查询 O(log N) (sid, seq) 索引
- **步骤**：`EXPLAIN QUERY PLAN SELECT ... WHERE sid=? AND seq > ?`
- **断言**：使用 `idx_events_sid_seq`
- **自动化**：sqlite_test

### TC-v8.1-020  SSE 客户端幂等去重
- **步骤**：收到 seq=3 后断网，重连时服务端 `?since=2` 返回 seq=2,3,4,5
- **断言**：客户端按 seq 去重，仅渲染 seq=4,5
- **自动化**：前端 utils/sse.ts 单测

---

## §3 P9-3：Schedule + Webhook

### TC-v8.1-021  cron 解析：`*/5 * * * *`
- **步骤**：`ParseCron("*/5 * * * *")` → NextFire(now) 应在 5 分钟内
- **断言**：`Now() + 4min <= next <= Now() + 5min`
- **自动化**：`TestParseCronAll` / `TestNextFire`

### TC-v8.1-022  cron 解析：列表 `1,3,5`
- **步骤**：每分钟跑 `1,3,5`
- **断言**：第 1/3/5 分钟触发；第 2/4 分钟不触发
- **自动化**：`TestParseCronSpecific`

### TC-v8.1-023  cron 解析：范围 `1-5`
- **步骤**：每分钟跑 `1-5`
- **断言**：第 1-5 分钟触发；第 6+ 分钟不触发
- **自动化**：`TestParseCronStep`

### TC-v8.1-024  cron 解析：非法表达式拒绝
- **步骤**：`ParseCron("not a cron")`
- **断言**：返回 error
- **自动化**：`TestParseCronBadInput`

### TC-v8.1-025  schedules CRUD
- **步骤**：POST → GET → PUT → DELETE
- **断言**：全部 200/201/204
- **自动化**：`TestStoreCRUD`

### TC-v8.1-026  Worker 触发 webhook_dispatch action
- **步骤**：创建 schedule `* * * * *` + action=webhook_dispatch
- **步骤**：mock HTTP server（200 OK）
- **断言**：≤ 60s 内 webhook deliveries ≥ 1 条
- **自动化**：`TestWorkerTriggers`

### TC-v8.1-027  webhook HMAC 签验
- **步骤**：dispatcher.Sign(body) → 发送；服务端 Verify(body, sig)
- **断言**：Verify 返回 true
- **自动化**：`TestSignAndVerify`

### TC-v8.1-028  webhook HMAC 篡改拒绝
- **步骤**：发送时改 body 1 字节
- **断言**：服务端 Verify 返回 false
- **自动化**：`TestSignAndVerify`（负例）

### TC-v8.1-029  webhook Dispatch 200 → delivery 记录成功
- **步骤**：mock server 返回 200
- **断言**：`ListDeliveries` 返回 status_code=200
- **自动化**：`TestDispatchWithHTTPServer`

### TC-v8.1-030  webhook Dispatch 失败 → retry
- **步骤**：mock server 始终返回 500；disable retry 后断言 attempt=1
- **断言**：`TestWorkerRetryDisabled`；retry=on 时 attempt=6 后放弃
- **自动化**：`TestWorkerRetryDisabled` / `TestDispatchBadURL`

### TC-v8.1-031  webhook POST /webhooks 返回 secret 一次
- **步骤**：POST 创建
- **断言**：响应 body 含 `secret`；之后 GET /webhooks 不含 `secret`
- **自动化**：adapter_webhook_test

### TC-v8.1-032  webhook POST /webhooks/{id}/test
- **步骤**：mock server 200；调 /test
- **断言**：≤ 1s 内 deliveries 增加 1 条
- **自动化**：adapter_webhook_test

### TC-v8.1-033  schedule enable=false 不触发
- **步骤**：schedule `* * * * *`，但 enabled=false
- **断言**：60s 后 deliveries=0
- **自动化**：`TestStoreCRUD`（enable toggle）

---

## §4 P9-4：Wails v2 真原生壳

### TC-v8.1-034  Wails build（windows/amd64）成功
- **步骤**：`wails build -tags wails_desktop -platform windows/amd64`
- **断言**：`build/bin/dsh-wails.exe` 存在；≈ 10-15 MB
- **自动化**：手工（依赖 Wails CLI + Node）

### TC-v8.1-035  Wails app 启动 → WebView2 窗口出现
- **步骤**：双击 `dsh-wails.exe`
- **断言**：窗口出现，标题 "DeepSeek Harness Desktop"，控制台渲染
- **自动化**：手工 smoke

### TC-v8.1-036  Wails Tray 菜单：Show / Hide / Quit
- **步骤**：最小化到 Tray → 右键菜单 → Show
- **断言**：窗口恢复；Quit 干净退出（无僵尸进程）
- **自动化**：手工 smoke

---

## §5 端到端验证脚本

```bash
# 后端
cd deepseek-harness-go
go test ./...
# 预期：所有包 ok

# 前端
cd web
npx vue-tsc --noEmit   # 预期：0 errors
npm run build          # 预期：dist 产物 ≈ 137KB gzip

# 启动器（v8.0 兼容）
cd desktop
go build -tags embed_dsh -o dsh-desktop.exe .
./dsh-desktop.exe      # 浏览器应自动打开

# 集成 smoke（手工）
# 1. /console/login 登录 root
# 2. 触发一次 model.create → 审计列表可见
# 3. ?format=csv 下载 → Excel 可打开
# 4. 创建 webhook + schedule → 1 分钟内 delivery 出现
# 5. 关闭 SessionDetail → 重开 → 进度条继续（不丢事件）
```

---

## §6 验证报告模板

每次发版前由 release manager 填写：

```
[v8.1.0]
- 单元测试：PASS（41 包 / 350+ 测试）
- 集成测试：PASS（JWT / 审计 / Spill / Schedule / Webhook）
- E2E（手工）：PASS（启动器 / Wails 壳 / 浏览器断网重连）
- 回归：v8.0 单 token 仍可用 ✅
- 数据迁移：零迁移 ✅
```
