# v5 测试用例目录（TC-v5-xxxx）

> 配套 PHASE-5-PLAN；上一阶段用例 TC-0001 ~ TC-0099 见 v4 test-cases。
> 主题：安全与可观测基线（v5）。

## P5-1 会话写租约

| ID         | 场景                                          | 预期 |
|------------|-----------------------------------------------|------|
| TC-v5-0001 | `MemoryLease.Acquire` 同 sid 第二次立即返回 `ErrLeaseHeld` | 命中 |
| TC-v5-0002 | `MemoryLease` 不同 sid 并发 Acquire 都成功    | 命中 |
| TC-v5-0003 | `MemoryLease.Acquire` 后 `Release`，再次可 Acquire | 命中 |
| TC-v5-0004 | 30s TTL 过期后 Acquire 抢占                   | 命中 |
| TC-v5-0005 | SQLiteStore AppendEvent 持 Lease，第二个 Acquire ErrLeaseHeld | 命中 |

## P5-2 Token 计量域

| ID         | 场景                                          | 预期 |
|------------|-----------------------------------------------|------|
| TC-v5-0006 | `Meter.Account` 累加 prompt/completion/total   | 命中 |
| TC-v5-0007 | `Meter.Account` 区分 cache_read / cache_write / reasoning | 命中 |
| TC-v5-0008 | `Meter.Get(unknown-sid)` 返回零值             | 命中 |
| TC-v5-0009 | `Meter.Snapshot` 跨 session 互不串扰          | 命中 |
| TC-v5-0010 | `Meter.Account` 32-way 并发，合计 == N×each    | 命中 |

## P5-3 模型渠道配置

| ID         | 场景                                          | 预期 |
|------------|-----------------------------------------------|------|
| TC-v5-0011 | `Registry.NewRegistry(channels,cfg)` 构造多 channel | 命中 |
| TC-v5-0012 | `Registry.Resolve(unknown)` 返回 `ErrUnknownChannel` | 命中 |
| TC-v5-0013 | `-channel primary` 切换 LLM 客户端（行为由 MemoryLease 类比） | 命中 |

## P5-4 凭据抽象

| ID         | 场景                                          | 预期 |
|------------|-----------------------------------------------|------|
| TC-v5-0014 | `EnvProvider.Resolve(env-known)` 命中        | 命中 |
| TC-v5-0015 | `FileProvider.Resolve` 从 JSON 读            | 命中 |
| TC-v5-0016 | `Chained.Resolve` env 优先；env 无则 file    | 命中 |
| TC-v5-0017 | `FileProvider.Reload` 拾取磁盘修改          | 命中 |

## P5-5 审批矩阵

| ID         | 场景                                          | 预期 |
|------------|-----------------------------------------------|------|
| TC-v5-0018 | `Matrix.GetPolicy(profile,tool)` 命中 allow   | 命中 |
| TC-v5-0019 | `Matrix.GetPolicy(未知 tool)` 回退 Profile Default | 命中 |
| TC-v5-0020 | `Matrix.GetPolicy(未知 profile)` 回退 Matrix Default | 命中 |
| TC-v5-0021 | `MatrixApprover.Approve PolicyAuto` 返 `ApproveSession` | 命中 |
| TC-v5-0022 | `MatrixApprover.Approve PolicyDeny` 返 Deny + error | 命中 |
| TC-v5-0023 | `MatrixApprover` 跨 profile 隔离            | 命中 |
| TC-v5-0024 | `Chain(Matrix, Noop)` PolicyAsk 透传         | 命中 |

## P5-6 Hook 机制

| ID         | 场景                                          | 预期 |
|------------|-----------------------------------------------|------|
| TC-v5-0025 | `Registry.Pre` 改写 args                     | 命中 |
| TC-v5-0026 | `Registry.Pre` 错误终止后续钩子              | 命中 |
| TC-v5-0027 | `Registry.Match` 过滤不命中的 hook           | 命中 |
| TC-v5-0028 | `Registry` 多 hook 按注册顺序串行            | 命中 |
| TC-v5-0029 | `Registry.Post` 改写 `Result.Content`        | 命中 |
| TC-v5-0030 | `Runner.Hooks.Pre` error 视为拦截并回填 `[ERROR]` | 命中 |

## 回归 / 既有测试

- TC-0001 ~ TC-0099（v4 全部）需保持绿。
- 所有 24 个 v4 包 + 3 个新包（`runtime` / `credentials` / `hook`）+ 2 个
  增强包（`store` 加 Lease / `usage` 加 Meter / `approval` 加 Matrix）共 27 包
  通过 `go test -count=1 ./...` 与 `go vet ./...`。
