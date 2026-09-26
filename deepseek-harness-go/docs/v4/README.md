# v4（deepseek-harness-go v4.0.0）

v4 在 v3 基础上引入三层新能力：

1. **会话事件溯源 + 投影缓存**（v4 §A / P1）
2. **统一 Gateway SSE**（v4 §B / P2）
3. **插件 / 工具库存视图**（v4 §C / P3）

并交付 [测试用例库](./test-cases.md) 与 [RELEASE 文档](./RELEASE-v4.md)。

## 文档

- [`DESIGN-v4.md`](./DESIGN-v4.md) — v4 详细设计（§A–§G）
- [`PHASE-1-PLAN.md`](./PHASE-1-PLAN.md) — P1 事件溯源实施计划
- [`PHASE-2-PLAN.md`](./PHASE-2-PLAN.md) — P2 Gateway SSE 实施计划
- [`PHASE-3-PLAN.md`](./PHASE-3-PLAN.md) — P3 库存视图实施计划
- [`PHASE-4-PLAN.md`](./PHASE-4-PLAN.md) — P4 测试用例库 + 收尾实施计划
- [`test-cases.md`](./test-cases.md) — TC-0001 ~ TC-0099 用例库（含 v3 + v4 全量映射）
- [`RELEASE-v4.md`](./RELEASE-v4.md) — v4 发布说明（变更 / 迁移 / 已知限制 / 验证矩阵）

## 状态

- ✅ 完成；git tag `v4.0.0`（2026-09-26）。
- 24 个 Go 包 `go test -count=1 ./...` 全绿；`go vet ./...` 干净。

## 主要代码

- 事件溯源：`internal/store/{event,projection,projector}.go`
- 投影缓存：`MemoryProjectionCache`
- Gateway SSE：`internal/server/{gateway,gateway_handler}.go`
- 库存视图：`internal/plugin/{view,grpc_view}.go`
- Server 接入：`server.Server.SetLLMClient / SetInventory`
- CLI 接线：`cmd/dsh/main.go` 自动构造 Combined Inventory 并注入 Server

## 上游对齐

v4 的 §A / §B / §C 与 ds-java v0.1.7 的 6 大主线基本对齐：
- 会话事件 v3 → v4 P1（无写租约）
- 写租约 → v5 P1
- 投影缓存 → v4 P1（等价 Java InMemorySessionProjectionCache）
- Token 计量 → v5 P2
- 统一 Gateway SSE → v4 P2
- 插件库存 → v4 P3（Go 元数据较弱）

## 下一版

v4 → v5：安全与可观测基线（写租约 / Token 计量 / 模型渠道 / 凭据 / 审批矩阵 / Hook）。详见 [`../0-ROADMAP.md`](../0-ROADMAP.md)。