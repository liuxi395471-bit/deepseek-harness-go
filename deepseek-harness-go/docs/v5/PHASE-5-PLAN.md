# PHASE-5-PLAN — 安全与可观测基线（v5）

> **对应阶段**：v5（[0-ROADMAP.md](../../0-ROADMAP.md) §3 v5）
> **对应详细设计**：[DESIGN-v5.md](./DESIGN-v5.md)
> **状态**：☐ 待启动 → ☐ 进行中 → ☐ 完成
> **工时**：5–7 天
> **基线**：v4.0.0 tag（含 99 既有测试 + 事件溯源 + Gateway SSE + 库存视图）

## §1 目标

把 v4 留下的 9 项核心差距（与 ds-java v0.1.7 对齐）收敛到产品级：

1. **P5-1** 会话写租约（防止跨请求并发写导致 seq 错乱）
2. **P5-2** Token 计量域（按 session 聚合 cache / reasoning / completion）
3. **P5-3** 模型渠道配置（运行时按 code 切换 LLM 协议）
4. **P5-4** 凭据抽象（env / file / 多 source 解析）
5. **P5-5** 审批矩阵（profile × tool 二维矩阵）
6. **P5-6** Hook 机制（PRE/POST 工具钩子）

## §2 子阶段依赖

```text
P5-1 Lease (独立)
  │
  ├── P5-2 Meter (依赖事件流；可与 P5-1 并行)
  ├── P5-3 Channel (独立；可与 P5-1 并行)
  ├── P5-4 Credential (独立)
  ├── P5-5 Approval Matrix (依赖 P5-4 + P5-3 配置层)
  └── P5-6 Hook (独立)

所有 P5-N → P5-FINAL（用例库 + RELEASE-v5 + git tag v5.0.0）
```

## §3 各阶段详情

### P5-1 — 会话写租约（Session Write Lease）

**目标**：防止同一 sid 的并发写入导致 seq 不一致。

**新增**：
- `internal/store/lease.go`：`Lease` 接口 + `MemoryLease` 实现 + `NoopLease`（向后兼容）。
- `Lease.Acquire(ctx, sid, owner) (LeaseHandle, error)`：成功返回独占句柄；30s TTL；同 sid 重复调用返回 `ErrLeaseHeld`。
- `LeaseHandle.Release()`：手动释放；超过 TTL 时由后台 reaper 回收。
- `SQLiteStore.AppendEvent` / `SQLiteStore.Append` 在写入前 `Acquire`，完成后 `Release`。

**测试**：
- `TC-v5-0001` Lease 同一 sid 第二次 Acquire 立即返回 ErrLeaseHeld。
- `TC-v5-0002` 不同 sid 并发 Acquire 都成功。
- `TC-v5-0003` Release 后可再次 Acquire。
- `TC-v5-0004` TTL 过期后自动释放。

### P5-2 — Token 计量域（Token Meter）

**目标**：从事件流聚合每个 session 的 token 计量，按类别区分 prompt/completion/cache_read/cache_write/reasoning。

**新增**：
- `internal/usage/meter.go`：`Meter` 接口 + `UsageMeter` 服务。
- `EventStore.OnEvent` 钩子：每次 AppendEvent 后 meter 增量更新。
- `LLMCallPayload` 扩展：增加 `cache_read_tokens` / `cache_write_tokens` / `reasoning_tokens`。
- Gateway 暴露 `usage.meter` source：返回 sid 的 token 计量。

**测试**：
- `TC-v5-0005` Meter 聚合单 session 累计 token。
- `TC-v5-0006` Meter 跨 session 互不串扰。
- `TC-v5-0007` UsageMeter 区分 cache / reasoning 类别。

### P5-3 — 模型渠道配置（Model Channel）

**目标**：运行时按 `channelCode` 切换 LLM 客户端，覆盖 ds-java `harness_model_setting` 表的核心场景。

**新增**：
- `internal/runtime/channel.go`：`ChannelConfig` + `Registry`（内存映射）；`LoadFromConfig` 从 YAML 读列表；`Resolve(code)` 返回 `llm.Client`。
- `config.LLMConfig` 增加 `Channels []ChannelConfig`；`cfg.LLM.Provider` 作为 fallback。
- `cmd/dsh/main.go`：构造 ChannelRegistry；`-channel <code>` 切换 runner 模型客户端。

**测试**：
- `TC-v5-0008` ChannelRegistry.Resolve 命中已注册渠道。
- `TC-v5-0009` ChannelRegistry 加载 YAML 多渠道。
- `TC-v5-0010` 未知 code 返回 ErrUnknownChannel。

### P5-4 — 凭据抽象（Credentials）

**目标**：把 `DEEPSEEK_API_KEY` 单一 env 来源升级为可插入式凭据 provider（env / file / 明文 yml）。

**新增**：
- `internal/credentials/provider.go`：`Provider` 接口 + `EnvProvider` / `FileProvider` / `Chained` 实现。
- `CredentialRef{Name, Key}` 引用；`Resolve(ctx, ref)` 返回明文。
- `cmd/dsh/main.go`：构造 Chained（env 优先，file fallback）。

**测试**：
- `TC-v5-0011` EnvProvider 命中已设 env。
- `TC-v5-0012` FileProvider 读取明文 JSON。
- `TC-v5-0013` Chained 优先级：env 优先。

### P5-5 — 审批矩阵（Approval Matrix）

**目标**：profile × tool 二维矩阵，自动 split `Gated` vs `AutoApproved`。

**新增**：
- `internal/approval/matrix.go`：`Matrix` 结构；`Matrix.GetPolicy(profile, tool)` 返回 `ApproveOnce` / `Deny` / `AutoApproved`。
- `MatrixApprover` 把矩阵包装为 Approver 实现。
- `cmd/dsh/main.go`：从 YAML 读矩阵并注入 shell 工具 Approver。

**测试**：
- `TC-v5-0014` Matrix 命中允许项返回 ApproveSession。
- `TC-v5-0015` Matrix 未命中返回 Deny。
- `TC-v5-0016` Matrix 跨 profile 隔离。

### P5-6 — Hook 机制

**目标**：在工具调用前后注入回调点，供用户做审计 / 转换 / 拦截。

**新增**：
- `internal/hook/hook.go`：`Hook` 接口 + `Registry` + `PreToolUse` / `PostToolUse` 两类。
- `agent.LoopRunner` 增加 `Hooks HooksField` 字段，在工具调用前后回调。
- `cmd/dsh/main.go`：构造默认 NoopRegistry；预留 `-hook` 接入点。

**测试**：
- `TC-v5-0017` PreToolUse 钩子拦截并改 args。
- `TC-v5-0018` PostToolUse 钩子在结果后追加内容。
- `TC-v5-0019` 多钩子按优先级调度。

## §4 跨阶段不变量

- v2/v3/v4 公开 API 全部冻结（`agent.StreamingRunner` / `tool.Registry` / `llm.Client` / `store.Store`）。
- `internal/store.EventStore` 公开方法不变；新 Lease 通过可选参数集成。
- 测试用 `t.TempDir()` + memstore + 无网络；不引入新三方依赖。

## §5 验收

- `go vet ./...` 无 warning
- `go test -count=1 ./...` 全绿（24+ 新增包）
- `go test -race ./...` 无数据竞争
- 既有 99 个测试不回归
- 新增 `TC-v5-0001` ~ `TC-v5-0019` 全部通过
- git tag `v5.0.0` 发布

## §6 文档交付

- [`DESIGN-v5.md`](./DESIGN-v5.md) 详细规格（包含伪码 / 表结构 / 配置示例）
- `test-cases.md` 增加 `TC-v5-xxxx` 19 条
- `RELEASE-v5.md` 发布说明