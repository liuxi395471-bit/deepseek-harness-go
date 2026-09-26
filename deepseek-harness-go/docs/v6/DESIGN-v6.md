# DESIGN-v6 — 任务系统 + 持久化工作流 详细设计

> **对应阶段**：v6（[0-ROADMAP.md](../../0-ROADMAP.md) §3 v6）
> **实施计划**：[PHASE-6-PLAN.md](./PHASE-6-PLAN.md)
> **状态**：☐ 待启动
> **作者**：2026-09-27

## §1 总体架构

```
┌─────────────────────────────────────────────────────────────────┐
│              cmd/dsh/main.go (HTTP/SSE Server)                  │
└───────────────┬─────────────────────────────────────────────────┘
                │
   ┌────────────┴────────────────┐
   │ internal/server/gateway.go  │ (v4 SSE)
   └────────────┬────────────────┘
                │
   ┌────────────┴──────────────────────────────────────────────┐
   │  P6-1 task.submit  P6-2 workflow.start  P6-3 jobs.list    │
   │  P6-6 storage.get  P6-5 terminal.list                      │
   └─────┬──────────────┬──────────────┬──────────────┬──────────┘
         │              │              │              │
    ┌────▼────┐   ┌─────▼─────┐  ┌─────▼─────┐  ┌────▼─────┐
    │  Task   │   │ Workflow  │  │   Jobs    │  │ Terminal │
    │ Service │   │  Runner   │  │ Registry  │  │ Session  │
    └────┬────┘   └─────┬─────┘  └─────┬─────┘  └────┬─────┘
         │              │              │              │
         │      ┌───────┴──────┐       │              │
         │      │ Goal/Plan/   │       │              │
         │      │   Todo       │       │              │
         │      └──────┬───────┘       │              │
         │             │               │              │
         ▼             ▼               ▼              ▼
   ┌──────────────────────────────────────────────────────────┐
   │      internal/storage/  (KV: memory/file/chained)        │
   └──────────────────────────────────────────────────────────┘
                              │
                              ▼
   ┌──────────────────────────────────────────────────────────┐
   │   v4 internal/store/  (EventStore + Lease + Projection)   │
   └──────────────────────────────────────────────────────────┘
                              │
                              ▼
   ┌──────────────────────────────────────────────────────────┐
   │   v3 agent/runner  +  v5 approval/matrix  +  v5 hook     │
   └──────────────────────────────────────────────────────────┘
```

## §2 P6-1 Task

### §2.1 数据模型

```go
type Task struct {
    ID         string
    Code       string         // 用户/系统自定义业务编号
    Title      string
    Input      string         // 用户 prompt
    SessionID  string         // 关联 session（可空：纯后台任务）
    State      TaskState      // Pending / Running / Completed / Failed / Canceled
    Profile    string         // 执行 profile（web/headless/sdk/sdk-minimal/acp）
    Permission string         // 凭据引用 / 权限模板
    CreatedAt  time.Time
    UpdatedAt  time.Time
    Owner      string         // user_id
}

type TaskState int
const (
    TaskPending TaskState = iota
    TaskRunning
    TaskCompleted
    TaskFailed
    TaskCanceled
)
```

### §2.2 SQLite schema

```sql
CREATE TABLE IF NOT EXISTS tasks (
    id           TEXT PRIMARY KEY,
    code         TEXT,
    title        TEXT NOT NULL,
    input        TEXT NOT NULL,
    session_id   TEXT,
    state        INTEGER NOT NULL DEFAULT 0,
    profile      TEXT NOT NULL DEFAULT 'headless',
    permission   TEXT,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    owner        TEXT
);
CREATE INDEX IF NOT EXISTS idx_tasks_state ON tasks(state);
CREATE INDEX IF NOT EXISTS idx_tasks_session ON tasks(session_id);
```

### §2.3 Executor

```go
type TaskExecutor interface {
    Submit(ctx context.Context, t *Task) (LeaseHandle, error)
    Cancel(ctx context.Context, taskID string) error
    Get(ctx context.Context, taskID string) (*Task, error)
    List(ctx context.Context, filter TaskFilter) ([]*Task, error)
}
```

- 内部使用 v5 `store.Lease` 防止同一 task 被并发启动。
- ctx 取消 → `Cancel(ctx, taskID)`。
- 执行通过 `agent.LoopRunner.RunStreaming` 复用对话能力；session_id 注入 ctx。

### §2.4 Gateway SSE source

```
source=task.submit   payload={id, code, input, profile}
source=task.get      payload={task}
source=task.list     payload={tasks:[]}
```

## §3 P6-2 Workflow

### §3.1 DAG 模型

```go
type Node struct {
    ID     string
    Type   string         // "task" / "function" / "branch" / "join"
    Config map[string]any // 节点类型特定
    Next   []Edge         // 出边
}

type Edge struct {
    To        string
    Condition string         // 可选："{{.result}} == ok" / ""（无条件）
}

type Workflow struct {
    Code  string
    Nodes map[string]*Node
    Entry string          // 入口节点 ID
}
```

### §3.2 Runner

- 拓扑排序检测环；不允许 DAG 中出现环。
- 节点类型：
  - `task`：包装 Task 提交；完成后流转。
  - `function`：注册的内置函数（无 LLM 参与）。
  - `branch`：条件路由，按 `Edge.Condition` 评估（使用 expr-lang 或简单 eval）。
  - `join`：多入边汇聚，配置策略（all / any / N-of-M）。
- 并行分支：所有无依赖节点放入 worker pool；`join` 等待同步。

### §3.3 执行状态

```go
type RunState struct {
    RunID      string
    Workflow   string
    NodeStates map[string]string // node_id -> Pending/Running/Done/Skipped/Failed
    StartedAt  time.Time
    FinishedAt *time.Time
    Vars       map[string]any   // 节点间共享变量
}
```

### §3.4 Gateway SSE source

- `workflow.start` payload `{run_id, workflow_code, input}` → 返回 `{run_id}`
- `workflow.status` payload `{run_id}` → 返回 `{state}`
- 增量事件通过现有 `gateway.stream` source，附带 `run_id` 标识。

## §4 P6-3 Jobs

### §4.1 模型

```go
type Job struct {
    ID        string
    Code      string         // 业务编号
    Cmd       string         // shell 命令 / 函数名
    State     JobState       // Pending / Running / Succeeded / Failed / Canceled
    Output    []string       // 行缓冲
    CreatedAt time.Time
    FinishedAt *time.Time
    ExitCode  *int
}
```

### §4.2 Registry

- `JobRegistry` 持有 map[jobID]*Job，sync.RWMutex 保护。
- 可选 SQLite 持久化（v6 内置 SQLiteJobStore）。
- 后台 goroutine 池：默认 4 worker。

### §4.3 工具

| 工具名 | 用途 |
|---|---|
| `jobs_run` | 启动 job，返回 job_id |
| `jobs_list` | 列出 job（可按 state 过滤）|
| `jobs_output` | 读取 job 输出（tail / head / all）|
| `jobs_kill` | 取消 job |

### §4.4 Gateway SSE source

- `jobs.list` payload `{state?: "running"}`
- `jobs.output` payload `{job_id, limit?}` → `{output: []string}`

## §5 P6-4 Goal / Plan / Todo

### §5.1 模型

```go
type Goal struct {
    ID    string
    Title string
    Why   string         // 业务背景
}

type Plan struct {
    ID     string
    GoalID string
    Title  string
    Steps  []string      // step IDs
}

type Todo struct {
    ID       string
    PlanID   string
    Title    string
    State    TodoState    // Pending / InProgress / Done / Blocked
    Order    int
    UpdatedAt time.Time
}
```

### §5.2 plan_mode

- Agent 在 system prompt 注入 `<plan_context>`（当前 Goal + Plan + Todo list）。
- Agent 在每轮可调用 `todo_write` 更新 Todo。
- v6 不引入严格的"plan-only mode"（拒绝一切写入）；只是给 Agent 提示与工具。

### §5.3 工具

- `todo_write` 接受 `{add?: [], update?: [{id,state}], delete?: [id]}`。
- `todo_read` 返回当前 Todo 列表。

### §5.4 存储

- 复用 v4 `EventStore`：Goal/Plan/Todo 写入 `kind=goal/plan/todo` 事件。
- 通过 `Projector` 投影到内存视图（`goal_view.go` / `plan_view.go` / `todo_view.go`）。

## §6 P6-5 Terminal

### §6.1 模型

```go
type Session struct {
    ID        string
    Shell     string         // "/bin/sh" / "cmd.exe" / "powershell.exe"
    Args      []string
    Rows      int
    Cols      int
    CreatedAt time.Time
    Buf       *Buffer        // 行缓冲
    cmd       *exec.Cmd      // （可选）底层进程
    mu        sync.Mutex
}
```

### §6.2 工具

- `terminal_run`：启动新会话，返回 `{session_id, initial_output}`。
- `terminal_read`：读取 buffer 增量。
- `terminal_write`：写入 stdin（仅 shell 模式，纯 PTY 留待 v6+）。
- `terminal_kill`：关闭会话。

### §6.3 Buffer

- 行缓冲 + 历史窗口（默认 1000 行）。
- v6 暂时不引入真 PTY（避免 platform-specific `go-pty` 依赖）；用 `bufio.Scanner`
  包 `exec.Cmd.Stdout` 即可。

### §6.4 Gateway SSE source

- `terminal.list` payload `{sessions: [{id, shell, alive}]}`
- 增量事件复用 `gateway.stream` 信封。

## §7 P6-6 Storage

### §7.1 接口

```go
type Storage interface {
    Get(ctx context.Context, key string) ([]byte, bool, error)
    Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
    Delete(ctx context.Context, key string) error
    List(ctx context.Context, prefix string) ([]string, error)
}
```

### §7.2 实现

- `MemoryStorage`：进程内 sync.RWMutex + map。
- `FileStorage`：JSON 行文件，每行 `{key, value, exp}`。
- `ChainedStorage`：读写分别走多级，回退链（写最左，读最右匹配）。

### §7.3 Gateway SSE source

- `storage.get` payload `{key}` → `{value, found}`（二进制 base64）。
- `storage.set` payload `{key, value, ttl_seconds}` → `{ok}`。

## §8 与上游对齐

| ds-java v0.1.7+ | ds-go v6 |
|---|---|
| `cases.task.SubmitTaskRequest` | `task.SubmitTask`（最小子集） |
| `cases.workflow.WorkflowService` | `workflow.Runner`（DAG） |
| `cases.jobs.JobRegistry` | `jobs.Registry` |
| `cases.goal` / `plan` / `todo` | `goal` / `plan` / `todo` 三个包 |
| `cases.terminal.TerminalService` | `terminal.Session` |
| `packages.storage.Storage` | `storage.Storage` |

ds-java 的"完整 filter chain + 5 个 filter"延后到 v6.1；v6 走最小子集。

## §9 风险

| # | 风险 | 应对 |
|---|---|---|
| R1 | Workflow DAG 复杂度爆炸 | v6 仅支持 4 种节点（task/function/branch/join）；其他节点留 v6.1 |
| R2 | Terminal 跨平台 PTY 复杂 | v6 用 `exec.Cmd` + `bufio.Scanner`，不引入 `go-pty` |
| R3 | 多 Job 并发占用资源 | worker pool 默认 4；可配置 |
| R4 | Storage 中间结果写满磁盘 | FileStorage 行文件 + 可配置 max_size；超过自动 rotate |

## §10 后续（v6.1+）

- Task filter chain（5 个 filter）
- 真 PTY（依赖 `go-pty` 或 `creack/pty`）
- Storage 适配 Redis（仅当用户要求）
- Workflow 节点类型扩展：`human-in-the-loop` / `parallel-map`
