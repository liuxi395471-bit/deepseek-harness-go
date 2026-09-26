# TC-v6 — v6 测试用例清单

> 适用范围：v6.0.0（Task / Workflow / Jobs / Goal-Plan-Todo / Terminal / Storage）。
> 用例编号顺序与 docs/v6/PHASE-6-PLAN.md §3 子阶段一致。

| 编号 | 子阶段 | 用例 | 验收标准 | 落地 |
|---|---|---|---|---|
| TC-v6-0001 | P6-1 | Task Submit → Get → List | Submit 后 Task 立刻出现；Get 按 ID 命中；List 反映 Filter | `internal/task/task_test.go` |
| TC-v6-0002 | P6-1 | Task Cancel | Cancel 后 State 变为 Canceled；Get 不再变更 | `internal/task/task_test.go` |
| TC-v6-0003 | P6-1 | Task Submit + Cancel via Gateway | `task.submit` / `task.cancel` SSE 端到端 | `internal/server/task_gateway_test.go` |
| TC-v6-0004 | P6-2 | Workflow 线性 DAG | a→b 串行执行；全部 done | `internal/workflow/workflow_test.go` |
| TC-v6-0005 | P6-2 | Workflow fan-out + join | 两条 Task 分支并行；join 节点 seen done | `internal/workflow/workflow_test.go` |
| TC-v6-0006 | P6-2 | Workflow 条件 branch | Branch 节点选第一条命中边；其他边 skipped | `internal/workflow/workflow_test.go` |
| TC-v6-0007 | P6-2 | Workflow cycle detection | Kahn 校验 ErrCycle | `internal/workflow/workflow_test.go` |
| TC-v6-0008 | P6-2 | Workflow 失败 fail-fast / partial | OnError=partial 时 sibling 仍 done | `internal/workflow/workflow_test.go` |
| TC-v6-0009 | P6-3 | Jobs Submit / List / Output / Kill | submit→completed；output 返回多行；kill idempotent | `internal/jobs/jobs_test.go` |
| TC-v6-0010 | P6-3 | Jobs 并发 / 隔离 | 多个 Job 并行；filter 正确返回 | `internal/jobs/jobs_test.go` |
| TC-v6-0011 | P6-3 | Jobs Gateway `jobs.list` | SSE 端到端 | `internal/server` (手动) |
| TC-v6-0012 | P6-4 | Todo CRUD | add / list / update / delete；State 正确 | `internal/goal/goal_test.go` |
| TC-v6-0013 | P6-4 | Plan 拆解 | 1 Goal → 1 Plan → N Todo | `internal/goal/goal_test.go` |
| TC-v6-0014 | P6-4 | Plan mode 系统提示注入 | Snapshot.Render() 输出 Markdown | `internal/goal/goal_test.go` |
| TC-v6-0015 | P6-5 | Terminal echo | `echo hi` 写入 buffer | `internal/terminal/terminal_test.go` |
| TC-v6-0016 | P6-5 | Terminal 并发互斥 | busy 时第二次 Run 返回 ErrBusy | `internal/terminal/terminal_test.go` |
| TC-v6-0017 | P6-5 | Terminal buffer cap | 超过 cap 丢最早 | `internal/terminal/terminal_test.go` |
| TC-v6-0018 | P6-6 | MemoryStorage CRUD | Get/Set/Delete/List | `internal/storage/storage_test.go` |
| TC-v6-0019 | P6-6 | FileStorage 持久化 | 重建实例仍可读 | `internal/storage/storage_test.go` |
| TC-v6-0020 | P6-6 | ChainedStorage 路由 | L1 miss → L2 命中；Set 写最后一层 | `internal/storage/storage_test.go` |

## 回归基线

| 来源 | 包数 | 状态 |
|---|---|---|
| v5 基线 | 27 包 | 全绿 |
| v6 新增 | 7 包（task / workflow / jobs / goal / terminal / storage / tools） | 全绿 |

总包数 34；`go test -count=1 -timeout=120s ./...` 全部 PASS。
