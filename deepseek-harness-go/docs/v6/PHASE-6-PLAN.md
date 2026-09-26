# PHASE-6-PLAN — 任务系统 + 持久化工作流（v6）

> **对应阶段**：v6（[0-ROADMAP.md](../../0-ROADMAP.md) §3 v6）
> **对应详细设计**：[DESIGN-v6.md](./DESIGN-v6.md)
> **状态**：✅ 完成（tag v6.0.0）
> **工时**：5–6 天
> **基线**：v5.0.0 tag（含安全与可观测基线）
> **主题**：把"任务执行 + 异步 + 工作流"做到可编排
> **用户决策**（2026-09-27）：
> - 范围：全部 6 子 TODO
> - Task：最小子集（SubmitTask + 状态查询）
> - Workflow：DAG 节点（条件路由 + 并行分支）

## §1 目标

把 ds-java v0.1.7 的「任务 / 目标 / 计划 / 后台任务 / 终端 / 存储」6 个核心域落地到 ds-go，
让 dsh 从"对话式 REPL"演化为"任务编排服务端"。

能力增量：
1. **任务** — 把一条 prompt 包装成任务，关联 execution profile / permission / approval；
2. **工作流** — 把多个任务组装成 DAG，按节点 + 条件 + 并行分支推进；
3. **Jobs** — 后台长任务（编译 / 索引 / 监控）的状态查询与取消；
4. **Goal/Plan/Todo** — 让 Agent 拥有"目标 → 计划 → 待办"语义，引入 `todo_write` 工具；
5. **Terminal** — PTY 长会话的 shell 交互，支持 read / write / kill；
6. **Storage** — KV 抽象（in-memory + file + 可选 Redis），给 workflow 中间结果 / 配置中心用。

## §2 子阶段依赖

```text
P6-1 Task (依赖 v5 Hook/Matrix + Lease)
  │
  ├── P6-2 Workflow (依赖 P6-1 Task + Gateway SSE)
  ├── P6-3 Jobs (依赖 Stream；可独立)
  ├── P6-4 Goal/Plan/Todo (依赖 v4 EventStore；可独立)
  ├── P6-5 Terminal (依赖 v5 Sandbox；可独立)
  └── P6-6 Storage (独立)

所有 P6-N → P6-FINAL（用例库 + RELEASE-v6 + git tag v6.0.0）
```

## §3 各阶段详情

### P6-1 — 任务领域（Task）

**目标**：把"一 prompt"包装为带元数据的 Task；任务可被 Gateway 提交，
状态可查；可选 execution profile / permission policy。

**新增**：
- `internal/task/task.go`：`Task` 结构 + `TaskState` (Pending/Running/Completed/Failed/Canceled)；
- `internal/task/store.go`：`SQLiteTaskStore`：CRUD + 状态机。
- `internal/task/executor.go`：执行 Task → 复用 v4 LoopRunner；ctx 取消传播。
- `internal/server/gateway_handler.go`：`task.submit` / `task.get` / `task.list` source。

**测试**：
- `TC-v6-0001` Submit 新 task → state=Running→Completed。
- `TC-v6-0002` 取消 task → state=Canceled。
- `TC-v6-0003` Task 与 session 关联（session_id 写入 context）。

### P6-2 — 工作流引擎（Workflow）

**目标**：DAG 节点 + 边 + 条件路由 + 并行分支；复用 v4 Gateway SSE
流式输出每节点 delta。

**新增**：
- `internal/workflow/dag.go`：`Node` / `Edge` / `Condition`。
- `internal/workflow/registry.go`：`Workflow` 注册表（按 code）。
- `internal/workflow/runner.go`：拓扑排序 + 并行执行 + 失败回退策略（fail-fast / partial）。
- `internal/server/gateway_handler.go`：`workflow.start` / `workflow.status` source。

**测试**：
- `TC-v6-0004` 线性 DAG (3 节点) → 顺序完成。
- `TC-v6-0005` 并行分支 (fan-out 2 → join) → 全部完成。
- `TC-v6-0006` 条件路由 (input==ok → A else B) → 命中分支。

### P6-3 — 后端 Jobs

**目标**：让 Agent 能"启动一个长任务，回来继续问进度"，覆盖编译、索引、
监控类用例。

**新增**：
- `internal/jobs/registry.go`：`JobRegistry`（内存 + SQLite 持久化）。
- `internal/jobs/runner.go`：goroutine 池 + 生命周期管理。
- `internal/tools/jobs_run.go` / `jobs_list.go` / `jobs_output.go` / `jobs_kill.go`：4 个内置工具。
- `internal/server/gateway_handler.go`：`jobs.list` source。

**测试**：
- `TC-v6-0007` jobs.run 启动 → state=Running → 完成 → state=Succeeded。
- `TC-v6-0008` jobs.kill 中断正在运行的 job → state=Canceled。

### P6-4 — Goal / Plan / Todo

**目标**：让 Agent 用 "plan mode" 工作，可以写 / 读 / 更新 todo。

**新增**：
- `internal/goal/`：Goal 域（高层目标）。
- `internal/plan/`：Plan 域（多步计划，按 Goal 拆）。
- `internal/todo/`：Todo 域（具体待办项）。
- `internal/tools/todo_write.go` / `todo_read.go`：工具暴露。
- `internal/agent/runner.go`：检测 plan_mode → 在每轮 LLM 前注入 plan 系统段。

**测试**：
- `TC-v6-0009` Todo CRUD（add / list / update / delete）。
- `TC-v6-0010` Plan 拆解：1 Goal → 3 Plan → 9 Todo。

### P6-5 — Terminal 域

**目标**：PTY 长会话，支持 read / write / kill；让 Agent 能做"交互式 shell"
（REPL 环境、psql、python REPL 等）。

**新增**：
- `internal/terminal/session.go`：`Session` 抽象 + 内存实现（可选 PTY）。
- `internal/terminal/buffer.go`：行缓冲 + 历史记录。
- `internal/tools/terminal_run.go` / `terminal_read.go` / `terminal_kill.go`：工具。
- `internal/server/gateway_handler.go`：`terminal.list` source。

**测试**：
- `TC-v6-0011` terminal_run 启动 echo → 写入字符串 → 读取 echo 回显。
- `TC-v6-0012` terminal_kill 关闭会话 → 后续 read 返回 EOF。

### P6-6 — KV 存储抽象

**目标**：跨 session 持久化的 KV；给 workflow 中间结果 / 用户偏好 / 配置
中心用。

**新增**：
- `internal/storage/storage.go`：`Storage` 接口（Get / Set / Delete / List）。
- `internal/storage/memory.go`：进程内实现（sync.RWMutex + map）。
- `internal/storage/file.go`：JSON 文件（每 key 一行）。
- `internal/storage/chained.go`：Chained（回退链）。
- `internal/server/gateway_handler.go`：`storage.get` / `storage.set` source。

**测试**：
- `TC-v6-0013` MemoryStorage Get/Set/Delete。
- `TC-v6-0014` FileStorage 持久化（重启可读）。
- `TC-v6-0015` Chained 优先级。

## §4 跨阶段不变量

- v2/v3/v4/v5 公开 API 冻结。
- 现有 27 包 + 新 6 包（task / workflow / jobs / goal / plan / todo / terminal / storage） ≥ 80% 测试覆盖。
- 不引入新三方依赖（除非实现需要 go-pty；当前 v6 用 pure-Go shim 实现 terminal_buffer）。
- Task / Workflow / Jobs / Terminal 全部利用 v5 Lease 防止并发破坏。
- Gateway SSE 帧格式不变（v4 已定）。

## §5 验收

- `go vet ./...` 无 warning ✅
- `go test -count=1 ./...` 全绿（34 包：27 既有 + 7 新增）✅
- 既有 99 + 30 = 129 测试不回归 ✅
- 新增 `TC-v6-0001` ~ `TC-v6-0020` 全部通过 ✅
- git tag `v6.0.0` ✅

## §6 文档交付

- `DESIGN-v6.md`（已完成）
- `TEST-CASES.md` 列出 20 条 `TC-v6-xxxx` 用例
- `RELEASE-NOTES.md` 发布说明
