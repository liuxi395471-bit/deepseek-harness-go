# Web Console Frontend — Backend Gap Log

> 本文件由 dsh-go 控制台前端持续维护。
> 每次前端实现某个交互时，如果发现**后端未提供对应能力**或**API 不支持**，
> 在本文件追加一条记录，便于后端阶段补齐。
>
> 维护原则：
> - 前端先做 mock / 占位，但**真实可用**——有交互反馈、不报 404
> - 真不能用时，明确写「未实现」+「影响页面」+「建议后端 API 形态」
> - 状态：`[stub]` = 前端占位可交互；`[missing]` = 完全没实现；`[partial]` = 部分实现

---

## 会话 Session

| 能力 | 状态 | 影响的页面 / 按钮 | 说明 / 建议后端 API |
|---|---|---|---|
| 创建会话 | ✅ | SessionsView「新建会话」按钮 | `POST /api/v1/console/sessions` |
| 列表 | ✅ | SessionsView | — |
| 删除会话 | ✅ | SessionsView 行尾「删除」 | `DELETE /api/v1/console/sessions/{sid}` |
| 重命名 | ✅ | SessionsView 行 hover「铅笔」 | `PATCH /api/v1/console/sessions/{sid}` |
| Fork / 派生会话 | `[missing]` | 未来 turn 上下文菜单 | 建议：`POST /api/v1/console/sessions/{sid}/fork` body `{ at_seq: number, title?: string }` |
| **会话导出（md/jsonl）** | ✅ (v8.1 P1) | 顶栏「↓」按钮 → 弹出 md/jsonl 菜单 | `GET /api/v1/console/sessions/{sid}/export?format=md\|jsonl` |

## 消息 Message

| 能力 | 状态 | 影响的页面 / 按钮 | 说明 |
|---|---|---|---|
| 列表 + 增量 resume | ✅ | SessionDetailView | 通过 SSE 持续接收 |
| 删除 | ✅ | 消息 hover「删除」 | `DELETE /sessions/{sid}/messages/{seq}` |
| 编辑 | ✅ | user 消息「编辑」 | `PATCH /sessions/{sid}/messages/{seq}` |
| 重新生成（regenerate） | ✅ (v8.1 P1) | assistant 消息 meta row「↻」按钮 | `POST /sessions/{sid}/messages/{seq}/regenerate`（删后跑，SSE） |
| 复制 | ✅ | 任意消息 hover「复制」 | 仅前端 |
| Fork / 从某条继续 | `[missing]` | turn 上下文菜单 | 建议：fork 后写一条 system「forked from seq=N」 |
| 单消息导出 | `[missing]` | turn 上下文菜单 | 建议：`GET /sessions/{sid}/messages/{seq}/export?format=md` |
| **评分 / 反馈（good/bad）** | ✅ (v8.1 P1) | assistant 消息底部「👍 / 👎」 | `POST /sessions/{sid}/messages/{seq}/feedback` body `{ rating: 1\|0\|null, comment?: string }`；store 落 message_ratings 表 |
| Translate to English | `[missing]` | 上下文菜单 | 客户端调 LLM 翻译，后端无需新接口 |

## 流式 / Spill

| 能力 | 状态 | 影响的页面 / 按钮 | 说明 |
|---|---|---|---|
| SSE 实时下行 | ✅ | SessionDetailView 流式显示 | `/api/v1/console/sessions/{sid}/events?since=N` |
| SSE `since_seq` 续传 | ✅ | 顶栏 entry 后连接态 | 已支持 |
| Spill 重连退避 | ✅ | 顶栏红点 | 客户端策略（1s/2s/4s/8s 上限） |
| 中断 / 取消 | ✅ | composer 圆形 stop | `abort()` + SSE 关闭 |
| **流式 usage 增量推送** | ✅ (v8.1 P1) | composer status bar 流式期间跳 token | agent 事件 UsageUpdate → SSE 帧 usage_update |
| Tool call 入参增量（JSON patch） | `[stub]` | 工具调用 panel | 上游 chunk 中已有，dsh 原生未做 JSON 拼装；先 raw args 显示 |
| 多模态（image/file）attachment | `[missing]` | user 消息附件 / 渲染 | 建议：`POST /sessions/{sid}/attachments` 返回 ref，message 流按 ref 渲染 |

## Composer（输入栏）

| 能力 | 状态 | 影响的页面 / 按钮 | 说明 |
|---|---|---|---|
| 发送文本 | ✅ | 圆形 ↑ 按钮 / Enter | — |
| Shift+Enter 换行 | ✅ | textarea keydown | — |
| Stop 流 | ✅ | 红色方形 stop | — |
| 模型切换 | ✅ | footer 模型 pill | 复用 `/api/v1/console/models` |
| 速度档位（High/Med/Low） | `[stub]` | footer 模型 pill 右侧 "High" | 暂无后端支持 speed 参数，前端只切换 UI；建议 LLM channel 加 `speed` 字段 |
| `[+]` 添加附件 | `[stub]` | footer 圆形 + | 打开系统文件选择；尚未实现 multipart 上传 |
| 工作区内修改（workspace pill） | `[stub]` | footer "工作区内修改" | 展示当前 session.cwd; 切换工作区逻辑待补 |
| `@` 提及文件 | `[missing]` | 中间 row「@」 | 弹文件搜索 / 目录树 picker；需要后端：`GET /workspace/files?path=...` |
| 语音输入 | `[missing]` | 中间 row「mic」 | 浏览器 Web Speech API，前端可单独实现 |
| 历史记录 / 历史会话内文本 | `[stub]` | 中间 row「history」 | 仅占位按钮 |
| 网络搜索 toggle | `[stub]` | 中间 row「globe」 | 仅占位 |
| Skills 选择 | `[missing]` | scope 文提及 / 菜单 | 建议：`GET /skills` 列表、`POST /sessions/{sid}/skills` 注入 |
| Composer draft 自动保存 | `[missing]` | 切页面后草稿没了 | 前端可做 localStorage，无需后端 |

## Tools / Function Calling

| 能力 | 状态 | 影响的页面 / 按钮 | 说明 |
|---|---|---|---|
| 工具调用展开（args / result） | ✅ | assistant 消息下「工具」details | 仅 UI |
| tool_call_id ↔ tool_result 关联 | `[partial]` | tool result 渲染 | 部分对齐；建议后端消息存储里加 `tool_call_id` 字段（已在用） |
| 工具权限审批 | ✅ | session 入审批流 | 由后端 approval 流接管 |
| 工具结果可折叠 / 展开 | ✅ | tool section summary | 仅 UI |
| 工具 call 重试 | `[missing]` | tool 上下文菜单 | 建议：`POST /sessions/{sid}/messages/{seq}/retry` |
| 工具 call 撤销 | `[missing]` | tool 上下文菜单 | 工具调用层不支持 undo，状态：未实现 |

## Tokens / Usage

| 能力 | 状态 | 影响的页面 / 按钮 | 说明 |
|---|---|---|---|
| 总 prompt / completion | ✅ | 消息 meta row、composer status bar | session.usage |
| 缓存命中（cache_hit_rate） | ✅ | 同上 | — |
| Reasoning tokens | ✅ | 同上 | 由 usage_parser.go 提供 |
| **tok/s（流式期间跳动）** | ✅ (v8.1 P1) | composer status bar | agent.UsageUpdate 事件 + SSE usage_update 帧 |
| 单 turn 持续时长 | ✅ | status row「用时 N 秒」 | finishedAt - startedAt |
| 上下文占比（context %） | `[stub]` | composer status bar 末位 | 假设 128k 总量；建议后端 session config 暴露 `context_window` 字段 |
| Cost（$ / ¥） | `[missing]` | meta row | 建议后端 model 配置增加 `price_per_1k_input/output` 字段，session 汇总 |

## 会话右键菜单（context menu）

| 项 | 状态 |
|---|---|
| Rename | ✅ |
| Duplicate | `[missing]` |
| Pin to top | `[missing]` |
| Archive | `[missing]` |
| **Export as Markdown** | ✅ (v8.1 P1) | `GET /sessions/{sid}/export?format=md` |
| **Export as JSONL** | ✅ (v8.1 P1) | `GET /sessions/{sid}/export?format=jsonl` |
| Open in new window | 不适用（已嵌入） |
| Delete | ✅ |

## Sidebar（dsh 风格）

| 项 | 状态 | 说明 |
|---|---|---|
| 折叠/展开 | ✅ | 260px ↔ 56px |
| Brand + Logo | ✅ | — |
| New session | ✅ | 跳转 `/sessions?new=1` |
| 7 个一级导航 | ✅ | — |
| 当前工作区显示 | `[missing]` | sidebar 底部 |
| 用户头像 / 角色 | ✅ | 已实现 |
| 主题切换 | ✅ | 浅/深色 |
| 语言切换 | ✅ | zh-CN ↔ en-US |
| 退出登录 | ✅ | — |

## 侧边 Activity Panel（dsh 风格）

| 项 | 状态 | 说明 |
|---|---|---|
| Changes / Tools / Logs tabs | ✅（仅 UI） | — |
| 近期事件（mock） | ✅（mock） | 后端无 /sessions/{sid}/activity 端点；建议增加 |
| Subtasks 进度 | ✅（mock） | — |
| Agent running 数 | ✅（实时) | sending 状态推 |

## SessionsView 列表项

| 项 | 状态 | 说明 |
|---|---|---|
| 标题 + 预览 + meta | ✅ | — |
| 删除 | ✅ | — |
| 重命名 | ✅ | — |
| 搜索过滤 | ✅ | — |
| 时间相对显示 | ✅ | — |

## ModelsView

| 项 | 状态 | 说明 |
|---|---|---|
| 渠道 CRUD | ✅ | — |
| ping（连通性测试） | ✅ | `/models/{ch}/ping` |
| 启用 / 停用 | ✅ | — |
| 导出审计 | ✅ | `/audit?format=jsonl` |
| 渠道导入（粘贴 JSON） | `[missing]` | 右上角 dropdown |
| 模型详情（max_tokens、context_window） | `[missing]` | 编辑 modal |

## PluginsView

| 项 | 状态 | 说明 |
|---|---|---|
| 列表 / 启用 / 停用 / 卸载 / 安装 | ✅ | — |
| 工具列表展示 | ✅ | — |
| 插件详情 / 参数配置 | `[missing]` | 行 hover 详情侧栏 |
| 插件 search（registry） | `[missing]` | 安装弹窗 dropdown |

## TasksView

| 项 | 状态 | 说明 |
|---|---|---|
| 列表 + 过滤 | ✅ | — |
| 新建 / 取消 / 重试 | ✅ | — |
| Job ID 跳转（行内） | `[missing]` | 跳到 log 详情 |
| 进度条 | `[missing]` | 后端无 progress 数据 |
| 输出查看 | `[missing]` | 详情侧栏 |

## ApprovalsView

| 项 | 状态 | 说明 |
|---|---|---|
| 列表 | ✅ | — |
| 单个 allow / deny / always | ✅ | — |
| 批量 allow / deny | ✅ | — |
| 全选 / 反选 | ✅ | — |
| 拒绝原因输入 | `[missing]` | 拒绝时弹框收集 reason |

## SchedulesView

| 项 | 状态 | 说明 |
|---|---|---|
| 列表 | ✅ | — |
| 创建 / 删除 / 启用 / 立即运行 | ✅ | — |
| Cron 编辑器（图形化） | `[missing]` | 替换当前 textarea |
| 上次运行详情 / 日志 | `[missing]` | 行 hover 详情 |

## WebhooksView

| 项 | 状态 | 说明 |
|---|---|---|
| 列表 + 创建 / 删除 | ✅ | — |
| 测试（手动触发） | ✅ | — |
| 投递历史 | ✅ | — |
| 编辑 / 启用 / 停用 | ✅ | — |
| 重试某次投递 | `[missing]` | 投递历史行 hover「重试」 |

## Login / Auth

| 项 | 状态 | 说明 |
|---|---|---|
| 用户名 / 密码登录 | ✅ | — |
| 错误提示 | ✅ | — |
| 记住我 | `[missing]` | — |
| 忘记密码 | `[missing]` | — |
| 第三方登录（SSO） | `[missing]` | — |
| 注册 | `[missing]` | 后端无注册端点 |

## Theme / Locale

| 项 | 状态 | 说明 |
|---|---|---|
| 浅 / 深主题 | ✅ | localStorage |
| 中文 / 英文 | ✅ | i18n.ts |
| 系统跟随 | `[missing]` | — |
| 字号（13/14/15） | `[missing]` | dsh 原生支持 |

## Topbar / 全局

| 项 | 状态 | 说明 |
|---|---|---|
| 连接状态点 | ✅ | — |
| 版本号 | ✅ | — |
| 语言切换 | ✅ | — |
| 刷新 | ✅ | — |
| 登出 | ✅ | — |
| 全局快捷键（Cmd+K） | `[missing]` | dsh 原生 command palette |
| **在线用户 / presence** | `[missing]` | — |
| **桌面模式 status bar** | ✅ | 「Desktop pid=-」 |

## Notifications / Toast

| 项 | 状态 | 说明 |
|---|---|---|
| 成功 / 错误 / 警告 toast | ✅ | — |
| 后端事件 → toast | `[partial]` | SSE 已有，但未全接入 toast |
| 邮件 / 浏览器原生通知 | `[missing]` | — |

## 数据 / API 通用

| 项 | 状态 | 说明 |
|---|---|---|
| REST + JSON | ✅ | — |
| JWT 鉴权 | ✅ | — |
| SSE 事件流 | ✅ | — |
| WebSocket | — | dsh 原生未启用 |
| 分页 / 搜索（list） | `[partial]` | keyword 已在 sessions 实现 |
| 排序 / 过滤 | `[partial]` | tasks 已支持 |
| Bulk operations | ✅ | approvals 已支持 |

---

## 下次前端实现时建议优先项

1. ~~**单 turn 实时 usage delta**~~ ✅ v8.1 P1
2. ~~**消息评分 / good-bad**~~ ✅ v8.1 P1
3. **Composer workspace picker / @提及**
4. **Tool call 重试**
5. ~~**会话 markdown 导出**~~ ✅ v8.1 P1
6. **Cron 图形化编辑器**
7. **Cmd+K command palette**
8. **Skills 列表 + 注入**
9. **Message 评分评论框**（rating ≠ 0 时弹简易 textarea）

每完成一项，删除对应 `[missing]` 行即可。

---

## v8.1 P1 实施详情

### usage_update 流式事件（dsh 实时 status bar）

**后端**：
- `internal/agent/event.go`：新增 `UsageUpdate{ Usage llm.Usage; Round int }`
- `internal/agent/runner.go`：每轮 LLM 调用结束后累计 totalUsage（包含 cache_read / cache_write / reasoning 字段）并 push `UsageUpdate`
- `internal/console/adapter.go`：eventToFrame 把 UsageUpdate 转 SSE 帧 `usage_update`，载荷 = 全套 token 字段（含 cacheHitRate、uncachedInputTokens）

**前端**：
- `web/src/views/SessionDetailView.vue`：send() 处理 `usage_update` 帧，写 `liveUsage.value`；composer status bar 由 `displayUsage` 计算响应（流式期间跳数）。

### message_ratings 表（dsh 风格 👍/👎）

**后端**：
- `internal/store/sqlite.go`：
  - `schemaSQL` 加 `message_ratings` 表（session_id, msg_seq, rating, comment, updated_at）
  - 实现 `SetMessageRating`（rating=0 时清除）+ `ListMessageRatings`
- `internal/store/mem.go`：同等的 `ratings map[int64]MessageRating` + 两个方法
- `internal/store/store.go`：接口新增两个方法 + `MessageRating struct`
- `internal/console/backends.go`：SessionBackend 接口加 `SetMessageRating` + `ListMessageRatings`
- `internal/console/adapter.go`：SessionsAdapter 实现两个方法 + Get() 路径附 ratings 进 SessionDetail
- `internal/console/handler_sessions.go`：`handleMessageFeedback` 端点（POST /feedback）

**前端**：
- `web/src/api/sessions.ts`：`setFeedback()` mutation
- `web/src/views/SessionDetailView.vue`：meta row 加 👍/👎 按钮（hover 才出现），点击 toggle；`ratings` 状态从 `detail.data.value.ratings` 同步；按钮 `.rated` 状态 = 已评分（蓝色 accent + 浅背景）

### regenerate 重生成（dsh 风格 ↻）

**后端**：
- `internal/console/backends.go`：SessionBackend 加 `Regenerate(ctx, sid, msgSeq, out)` 方法
- `internal/console/adapter.go`：实现——删除 msgSeq 之后所有 assistant/tool 消息 + 调 SendStream
- `internal/console/handler_sessions.go`：`handleMessageRegenerate` 端点（POST /regenerate，SSE 流）

**前端**：
- `web/src/api/sessions.ts`：`regenerate()` 函数（fetch+事件）
- `web/src/views/SessionDetailView.vue`：assistant meta row 加 ↻ 按钮（hover 出现），点击调 regenerate；UI 与 send 复用（live msg + scrollToBottom）

### session export md / jsonl

**后端**：
- `internal/console/backends.go`：SessionBackend 加 `ExportMarkdown` + `ExportJSONL`
- `internal/console/adapter.go`：实现——分别产出 Markdown 文本（含 # 标题、usage、消息体、tool_calls 代码块）/ JSONL（每行一条 message）
- `internal/console/handler_sessions.go`：`handleSessionExport` 端点（GET /export?format=md|jsonl）
- `internal/console/console.go`：注册 `GET /sessions/{sid}/export`

**前端**：
- `web/src/api/sessions.ts`：`exportMd()` / `exportJsonl()` 返回 Blob
- `web/src/views/SessionDetailView.vue`：topbar 加 ↓ 按钮 + dropdown 菜单；点击触发下载（downloadBlob → `<a download>`）；demo session 禁止导出（提示 toast）

### 端点清单（v8.1 P1 新增）

| Method | Path | 用途 |
|---|---|---|
| POST | `/api/v1/console/sessions/{sid}/messages/{seq}/feedback` | 评分 good/bad |
| POST | `/api/v1/console/sessions/{sid}/messages/{seq}/regenerate` | 重生成 assistant（SSE） |
| GET  | `/api/v1/console/sessions/{sid}/export?format=md\|jsonl` | 导出 |

### SSE 事件清单（v8.1 P1 新增）

| 帧 | 触发时机 | 载荷 |
|---|---|---|
| `usage_update` | 每轮 LLM 调用结束 | promptTokens / completionTokens / totalTokens / cacheReadTokens / cacheWriteTokens / reasoningTokens / uncachedInputTokens / cacheHitRate / round |

---