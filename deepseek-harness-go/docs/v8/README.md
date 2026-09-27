# v8 — Web Console + Desktop

> **状态**：📝 设计完成（DESIGN-v8 v0.1 + PHASE-8-PLAN v0.1）
> **主题**：把 ds-go 提升到"产品级"用户体验
> **工时**：8–10 天
> **优先级**：🟢 选做
> **依赖**：v7.0.0（tag `cc1dd87`）
> **用户决策**（2026-09-27）：
> - Web 控制台 = **Vite + Vue 3 SPA**（不上 Wails/Vue 单页原生 JS 路线）
> - 可选子项 = **核心 5 页 + Desktop 打包（Wails v2）**
> - 不做 Spill / Browser-Use（留 v8.1）

---

## 📑 文档清单

```
docs/v8/
├── DESIGN-v8.md         ← 架构 / API / 页面 / Desktop
├── PHASE-8-PLAN.md      ← 三阶段实施步骤（P8-1 后端 / P8-2 前端 / P8-3 Desktop）
├── RELEASE-NOTES.md     ← （P8-3 完成时创建）
└── TEST-CASES.md        ← （P8-3 完成时创建，TC-v8-0001 ~ TC-v8-0008）
```

---

## 🎯 目标

原生 Web 控制台（Vite + Vue 3）+ 可选桌面打包（Wails v2），覆盖：

1. **会话列表 / 详情 / 流式对话**
2. **插件管理**（启用 / 停用）
3. **模型设置**（多渠道 / 协议 / 连通性测试）
4. **任务队列**（v6 任务 + v6 Jobs 进度）
5. **审批**（v5 审批矩阵可视化决策）

---

## 🧱 子 TODO

| # | 子项 | 工时 | 优先级 | 状态 |
|---|---|---|---|---|
| **P8-1** | 后端 Console 域 + REST API + embed.FS | 2.5d | 🔴 必经 | ✅ 完成（commit 2ba5ff2） |
| **P8-2** | 前端 Vue 3 SPA（5 页 + 组件 + 状态管理） | 3d | 🔴 必经 | ✅ 完成（commit fb034e5） |
| **P8-3** | Desktop 启动器 + 收尾 + RELEASE | 2.5d | 🟡 必经 | ✅ 完成；待 tag v8.0.0 |

> 详细规格见 `DESIGN-v8.md`；实施步骤见 `PHASE-8-PLAN.md`。

---

## 🔌 与既有模块的复用

| 既有模块 | v8 怎么用 |
|---|---|
| `internal/store/`（v4）| 会话事件读取 / 列表分页 |
| `internal/plugin/`（v2+v4）| 插件清单 / 启用停用 |
| `internal/runtime/channel.go`（v5）| 模型渠道配置 |
| `internal/approval/matrix.go`（v5）| 审批决策写回 |
| `internal/server/gateway.go`（v4）| SSE 透传 |
| `internal/task/`（v6）| 任务列表 / 取消 |
| `internal/jobs/`（v6）| 任务进度 |
| `internal/usage/meter.go`（v5）| 会话统计 |

**新增**：仅 `internal/console/` + `web/` + `desktop/` 三个新顶层目录；不修改任何既有 internal 包（除可能加 `console_state` 表迁移）。

---

## 🛣️ 路由约定（避免冲突）

| 前缀 | 来源 |
|---|---|
| `/api/v1/console/*` | **v8 新增** |
| `/api/v1/events` | v4 Gateway |
| `/acp/*` | v7 ACP |
| `/mcp` | v7 MCP |
| `/sdk/*` | v7 SDK |
| `/console/*` | v8 SPA 静态资源（embed.FS） |

---

## ⚠️ 关键约束

1. **不引入新三方依赖**（除 Vite + Vue + Wails）；
2. **embed.FS 体积**：Vue 构建产物总 ≤ 1MB（gzip ≤ 300KB）；
3. **Desktop 二进制 ≤ 20MB**（不含系统 WebView）；
4. **鉴权**：v8.0 单 token；v8.1 升级本地用户；
5. **国际化**：v8.0 zh-CN 优先；v8.1 en-US。

---

## ✅ 验收

- [ ] `go test ./...` 全绿
- [ ] `pnpm -C web build` 主 chunk ≤ 300KB gzip
- [ ] `pnpm -C web test` ≥ 6 vitest 通过
- [ ] `wails build` 至少 Windows 平台成功
- [ ] `curl http://127.0.0.1:7777/console/` 返回 200
- [ ] 5 个页面手工 E2E 全通过
- [ ] TC-v8-0001 ~ TC-v8-0008 全通过
- [ ] tag `v8.0.0` 推送成功
