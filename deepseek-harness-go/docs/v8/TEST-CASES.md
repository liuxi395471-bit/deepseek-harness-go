# TEST-CASES v8.0.0

> 8 个手工 E2E 用例，覆盖 v8 全部核心场景。

## 测试矩阵

| ID | 主题 | 涉及模块 | 自动化 |
|---|---|---|---|
| TC-v8-0001 | Console 鉴权 + health bypass | `internal/console` | go test |
| TC-v8-0002 | Session CRUD + DB 一致性 | `internal/console/store` | go test |
| TC-v8-0003 | Plugin enable/disable 持久化 | `internal/console/installer` | go test |
| TC-v8-0004 | Models ping 真实 HTTP | `internal/console/runtime` | go test |
| TC-v8-0005 | embed.FS SPA 完整性 | `internal/console/embed` | go test |
| TC-v8-0006 | Web SPA 5 页手工 E2E | `web/` | 手工 |
| TC-v8-0007 | 桌面启动器 spawn + 健康检查 | `desktop/launcher` | go test |
| TC-v8-0008 | 桌面启动器嵌入 dsh.exe | `desktop` | 手工 + smoke |

---

## TC-v8-0001：Console 鉴权 + health bypass

**前置**：`dsh.exe -serve`，已设置 `DSH_SERVER_AUTH_TOKEN=xxx`

**步骤**：
1. `curl /api/v1/console/sessions/`（无 token）→ 期望 `401`，错误码 `BAD_REQUEST`，消息 "missing bearer token"
2. `curl -H "Authorization: Bearer wrong" /api/v1/console/sessions/` → 期望 `401`，消息 "invalid bearer token"
3. `curl -H "Authorization: Bearer xxx" /api/v1/console/sessions/` → 期望 `200`
4. `curl /api/v1/console/health/`（无 token）→ 期望 `200`（bypass）

**自动化**：`internal/console/console_test.go::TestBearerAuth_Rejects* / TestHealth_NoAuthRequired`

---

## TC-v8-0002：Session CRUD + DB 一致性

**前置**：tc-0001 环境

**步骤**：
1. POST `/api/v1/console/sessions/` body `{"title":"t1","model":"deepseek-chat"}` → 200，返回 sid
2. GET `/api/v1/console/sessions/{sid}` → 200，包含 title="t1"
3. SQLite 直接查 `dsh.db`：确认 `sessions` 表有对应行
4. DELETE `/api/v1/console/sessions/{sid}` → 200/204
5. GET `/api/v1/console/sessions/{sid}` → 404
6. SQLite 查表：行已删除

**自动化**：`internal/console/console_test.go::TestSessionCRUD`

---

## TC-v8-0003：Plugin enable/disable 持久化

**前置**：tc-0001 环境，初始有 `local` 插件

**步骤**：
1. GET `/api/v1/console/plugins/` → 包含 `local`（state="loaded"）
2. POST `/api/v1/console/plugins/local/disable` → 200
3. 物理读 `<workspace>/plugin_status.json` → 包含 `{"local":"disabled"}`
4. GET `/api/v1/console/plugins/` → `local.state="disabled"`
5. 重启 `dsh.exe -serve`
6. GET `/api/v1/console/plugins/` → `local.state="disabled"`（持久化生效）
7. POST `/api/v1/console/plugins/local/enable` → 200
8. 物理读 → `{"local":"enabled"}`

**自动化**：`internal/console/console_test.go::TestPluginToggle_Persists`

---

## TC-v8-0004：Models ping 真实 HTTP

**前置**：tc-0001 环境 + 至少 1 个已配置渠道

**步骤**：
1. GET `/api/v1/console/models/` → 列出渠道
2. POST `/api/v1/console/models/{channel}/ping` → 200，返回 `{"ok":true,"latencyMs":N}`
3. 故意改坏渠道 baseUrl → POST ping → 返回 `{"ok":false,"error":"..."}`
4. PUT `/api/v1/console/models/{channel}` body `{"active":false}` → 200
5. GET → 渠道 `active=false`

**自动化**：`internal/console/console_test.go::TestModels_Ping*`（mock upstream via httptest）

---

## TC-v8-0005：embed.FS SPA 完整性

**前置**：构建产物 `internal/console/web_dist/index.html` + `assets/*`

**步骤**：
1. `go build -o dsh.exe ./cmd/dsh`
2. `./dsh.exe -serve` + 等待就绪
3. `curl http://127.0.0.1:7777/console/` → 200，body 包含 `<title>DeepSeek Harness · 控制台</title>`
4. `curl http://127.0.0.1:7777/console/assets/<index-*.js>` → 200
5. `curl http://127.0.0.1:7777/console/sessions/abc/foo/bar`（任意深路径）→ 200（SPA fallback 到 index.html）
6. Windows 大小写敏感测试：`curl /Console/` → 404（应该 — 路径区分大小写）

**自动化**：`internal/console/console_test.go::TestEmbedFS_ServesIndex / TestSPAFallback`

---

## TC-v8-0006：Web SPA 5 页手工 E2E

**前置**：浏览器打开 `http://127.0.0.1:7777/console/`，已在右上角输入 token

**步骤**：

### 6.1 Sessions 页
1. 看到 "会话列表"，空状态提示 "还没有会话"
2. 点 "新建会话"，输入 title="smoke test"，点击 "创建"
3. 自动跳转到 SessionDetail
4. 看到 title="smoke test"

### 6.2 SessionDetail 页
1. 在 textarea 输入 "1+1=?"，回车
2. 看到 user 气泡 + assistant 气泡（SSE 流式）
3. 等 done，底部显示 token 用量
4. 返回列表，能看到新会话

### 6.3 Plugins 页
1. 看到 `local` 插件，state="loaded"
2. 点 "停用"
3. badge 变 "disabled"，提示 "插件已停用"
4. 物理 `<workspace>/plugin_status.json` 含 `{"local":"disabled"}`
5. 点 "启用" → 恢复

### 6.4 Models 页
1. 看到渠道表
2. 点 "ping" → 显示延迟 ms 或失败 badge
3. 点 "停用" / "启用" → 切换 active 状态

### 6.5 Tasks 页
1. 看到空状态
2. 切换 "状态" 筛选 → 列表/空状态切换
3. 如果有 running 任务，能点 "取消"

### 6.6 Approvals 页
1. 看到空状态或待审批列表
2. 选一个 → 三按钮可见
3. 点 "拒绝" → 卡片消失

**自动化**：v8.1 候选（Playwright）；v8.0 手工。

---

## TC-v8-0007：桌面启动器 spawn + 健康检查

**前置**：Go 1.22+

**步骤**：
1. `cd desktop && go test ./launcher/...` → 4/4 通过
2. `go build -o dsh-desktop.exe .`
3. `cp ../dsh.exe ./`（同目录）
4. `./dsh-desktop.exe -no-open` → 后台运行
5. 期望 stdout 包含：
   - `desktop: generated bearer token ...`
   - `dsh listening at 127.0.0.1:XXXX`
   - `open: http://127.0.0.1:XXXX/console/`
6. `curl -H "Authorization: Bearer <from stdout>" http://127.0.0.1:XXXX/api/v1/console/health/` → 200
7. `curl http://127.0.0.1:XXXX/api/v1/console/sessions/`（无 token）→ 401
8. Ctrl+C → 5s 内优雅停止；`Get-Process dsh-desktop` 为空

**自动化**：`desktop/launcher/launcher_test.go::TestServer_StartStop_PortAuto`

---

## TC-v8-0008：桌面启动器嵌入 dsh.exe

**前置**：tc-0007 环境

**步骤**：
1. `cd ..`
2. `go build -o dsh.exe ./cmd/dsh`
3. `cd desktop`
4. `pwsh -File build-windows.ps1 -Embed`
5. 期望输出：`done: ./bin/dsh-desktop.exe (XXXXX KB)` ~40MB
6. `bin/dsh-desktop.exe -no-open` → 后台运行
7. 同 tc-0007 第 5-8 步
8. 验证：临时目录 `%LOCALAPPDATA%\DeepSeekHarness\bin\dsh.exe` 被释放

**自动化**：手工（构建脚本 + smoke）

---

## 验证汇总

| TC | 自动化 | 通过 |
|---|---|---|
| 0001 | go test | ✅ |
| 0002 | go test | ✅ |
| 0003 | go test | ✅ |
| 0004 | go test | ✅ |
| 0005 | go test | ✅ |
| 0006 | 手工 | ✅ |
| 0007 | go test | ✅ |
| 0008 | 手工 | ✅ |
