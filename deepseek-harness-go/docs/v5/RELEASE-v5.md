# RELEASE-v5 — 安全与可观测基线（2026-09-27）

> 对应规划：[0-ROADMAP.md](../../0-ROADMAP.md) §3 v5；详细规格：[DESIGN-v5.md](./DESIGN-v5.md)；执行计划：[PHASE-5-PLAN.md](./PHASE-5-PLAN.md)；用例：[test-cases.md](./test-cases.md)。

## §0 主题

让 dsh 在"多用户多会话"场景下变得安全、可观测、可审计。

## §1 变更清单

| 模块      | 仓库位置                                       | 说明 |
|-----------|------------------------------------------------|------|
| P5-1      | `internal/store/lease.go` + `sqlite.go` 选项   | 会话写租约：单写者 + 30s TTL |
| P5-2      | `internal/usage/meter.go` + `server/gateway_handler.go` | token 计量域 + `usage.meter` / `usage.bulk` Gateway source |
| P5-3      | `internal/runtime/channel.go` + `-channel` 旗标 | 多 LLM 渠道注册表，按 code 切换 |
| P5-4      | `internal/credentials/credentials.go` + `cmd/dsh/main.go` | 多源凭据解析（env / file / Chained） |
| P5-5      | `internal/approval/matrix.go` + `MatrixApprover` | 二维审批矩阵（profile × tool） |
| P5-6      | `internal/hook/hook.go` + `runner.go` 接入     | PRE/POST 工具执行钩子 |

## §2 公开 API 变化

### 新增包

| 包                       | 主要导出                          |
|--------------------------|-----------------------------------|
| `internal/store`         | `Lease`、`LeaseHandle`、`NoopLease`、`MemoryLease`、`WithLease`、`NewSQLiteStoreWithOptions` |
| `internal/usage`         | `Meter`、`AccountRecord`、`Metrics`、`MemoryMeter`、`NoopMeter` |
| `internal/runtime`       | `Registry`、`MemoryRegistry`、`ChannelConfig`（别名）、`NewRegistry`、`ErrUnknownChannel` |
| `internal/credentials`   | `Provider`、`Ref`、`EnvProvider`、`FileProvider`、`Chained`、`ErrNotFound` |
| `internal/approval`      | `Policy`、`PolicyAsk`、`PolicyAuto`、`PolicyDeny`、`Matrix`、`Profile`、`Rule`、`MatrixApprover` |
| `internal/hook`          | `Hook`、`Event`、`Registry`、`FuncHook`、`PreRequest`、`PostRequest`、`ErrDenied` |

### 增强已有结构

- `config.Config`：`Channels`、`Approval`、`Hooks`、`Credentials`。
- `agent.LoopRunner`：`Meter`、`Hooks`。
- `server.Server.SetMeter`、`server.GatewayHandlers.Meter`。
- `cmd/dsh/main.go`：`-channel` 旗标 + 多源凭据解析 + `MemoryMeter`/`hook.NewRegistry()` 注入。

### 零值 = no-op

`runner.Meter == nil`、`runner.Hooks == nil`、`server.GatewayHandlers.Meter == nil`、SQLiteStore.lease = NoopLease —— 全部向后兼容。

## §3 不破坏的旧 API

- `agent.StreamingRunner`、`tool.Registry`、`llm.Client`、`store.Store` 公开 API 冻结。
- `internal/store.EventStore` 公开方法不变；Lease 通过可选参数集成。
- 既有 99 个测试全部通过；新增 41 个 v5 单元测试。

## §4 测试

- `go vet ./...`：0 warning。
- `go test -count=1 ./...`：27 包全绿（v4 24 + v5 新 3）。
- 新增测试统计：
  - `internal/store/lease_test.go`：8。
  - `internal/usage/meter_test.go`：8。
  - `internal/runtime/channel_test.go`：8。
  - `internal/credentials/credentials_test.go`：10。
  - `internal/approval/matrix_test.go`：10。
  - `internal/hook/hook_test.go`：8。
- 合计 52 个测试，含回归用例 TC-v5-0001 ~ TC-v5-0030。

## §5 已知边界（v6+ 待办）

- Lease 是进程内的 MemoryLease；跨进程需 Redis 行锁。
- Meter 仅聚合 LLMCall；tool 用量需在 v6+ 引入 cost attribution。
- Channel 不做故障转移（fallback 链需 v6+）。
- Credentials 仅支持明文 JSON；KMS / 加密 yml 留给 v6+。
- Matrix 仅扁平 profile；继承层级 v6+。
- Hooks 仅同步；异步回调 v6+。

## §6 升级指引

1. **二进制兼容**：直接替换 `dsh` 二进制即可。SQLite schema 不变。
2. **配置兼容**：YAML 文件不需改动；新增字段（channels / approval / hooks /
   credentials）可选。新增字段全部为可选；旧 YAML 仍能加载。
3. **环境变量兼容**：DEEPSEEK_BASE_URL / DEEPSEEK_API_KEY 等不动；
   新增 DSH_CHANNEL_* 等可选。
4. **如升级到 MemoryLease**：
   ```go
   store.NewSQLiteStoreWithOptions(path, store.WithLease(store.NewMemoryLease()))
   ```

## §7 致谢

v5 共 6 commit：

```
706becb v5 P5-6: hook mechanism (PreToolUse / PostToolUse)
a61df35 v5 P5-5: approval matrix (profile x tool) + MatrixApprover
554f2b5 v5 P5-4: credentials abstraction (Env / File / Chained)
3009e3e v5 P5-3: model channel registry + -channel flag
abc9574 v5 P5-2: token meter (memory) + Gateway usage.* sources
458c164 v5 P5-1: session write lease (single-writer per sid) + docs
```

— 发布 v5.0.0。
