# RELEASE NOTES — v6.0.0

> 发布日期：2026-09-27
> 基线：v5.0.0
> 配套文档：[PHASE-6-PLAN.md](./PHASE-6-PLAN.md) · [DESIGN-v6.md](./DESIGN-v6.md) · [TEST-CASES.md](./TEST-CASES.md)

## 主题

> **把"任务执行 + 异步 + 工作流"做到可编排**

v6 在 v5 的"安全 + 可观测"基线之上，引入了 6 个新的领域子包，把
agent 从"逐轮 LLM 对话"提升到"长生命周期任务编排"：

1. **Task**：把用户提示包装成可查询、可取消的异步任务；
2. **Workflow**：以 DAG 描述多任务工作流（节点类型 task/function/branch/join）；
3. **Jobs**：后台长进程（运行 / 列表 / 输出 / 取消）；
4. **Goal / Plan / Todo**：3 层任务管理 + plan-mode 系统提示注入；
5. **Terminal**：buffer-backed shell session（cmd / powershell / bash / sh）；
6. **Storage**：KV 抽象（Memory + File + Chained）。

## 新增包

| 包 | 行数（粗略） | 说明 |
|---|---|---|
| `internal/task` | ~600 | Task 模型、SQLite 持久化、LoopExecutor |
| `internal/workflow` | ~700 | DAG 模型、Kahn 拓扑校验、Runner、Condition |
| `internal/jobs` | ~550 | 后台 Job 注册表、Buffer、ShellRunner |
| `internal/goal` | ~350 | Goal/Plan/Todo 三层模型 |
| `internal/terminal` | ~400 | buffer-backed Terminal + Registry |
| `internal/storage` | ~400 | Memory/File/Chained 三种实现 |

## 新增工具

| 工具 | 子阶段 | 说明 |
|---|---|---|
| `jobs_run` / `jobs_list` / `jobs_output` / `jobs_kill` | P6-3 | 后端 Job 管理 |
| `todo_write` / `todo_read` | P6-4 | Todo 增删改查 |
| `terminal_run` / `terminal_read` / `terminal_kill` | P6-5 | Terminal session |
| `kv_set` / `kv_get` / `kv_delete` / `kv_list` | P6-6 | KV 存储 |

## 新增 Gateway SSE source

| source | 子阶段 | 说明 |
|---|---|---|
| `task.submit` / `task.get` / `task.list` / `task.cancel` | P6-1 | Task 管理 |
| `jobs.list` | P6-3 | 后端 Job 列表 |

## 不破坏的接口

- v2/v3/v4/v5 公开 API 冻结（`internal/agent`、`internal/store`、`internal/llm`、`internal/server`、`internal/audit` 等）。
- Gateway SSE 帧格式不变。
- 现有 27 个 v2-v5 包继续全绿。

## 兼容性

- 新工具都通过 `tool.Registry` 注册；现有 REPL / CLI / Gateway 客户端无需任何修改即可发现并调用新工具。
- 新增的 SSE source 均按 v4 B.3 协议；老客户端遇到未知 source 会返回 `unknown-source`，不影响其他 source。

## 已知缺口（留待 v6.1）

- Task：完整 Filter chain（v6.0 仅 Submit + Get/List/Cancel）。
- Jobs：SQLite 持久化（v6.0 用 MemoryStore）。
- Goal/Plan/Todo：与 v4 EventStore 接入（v6.0 内存）。
- Terminal：PTY / 交互式 shell（v6.0 用 cmd 直接调用）。
- Workflow：join 节点的复杂同步策略（v6.0 仅基础 Kahn 拓扑）。
- Storage：WAL / 加密（v6.0 JSON 文件直接写）。

## 验收

```
$ go vet ./...                          # 无 warning
$ go test -count=1 -timeout=120s ./...  # 34 包全绿
```

## 致谢

v6 实现借鉴了 ds-java v0.1.7+ 的 cases.task / cases.workflow / cases.jobs /
cases.goal/plan/todo / cases.terminal / cases.storage 设计语言，并按 Go
的并发模型（goroutine + context）做了重新表达。
