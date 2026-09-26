# v5 计划（deepseek-harness-go v5.x）

> **状态**：☐ 待启动（v4.0.0 已发布；详见 [`../0-ROADMAP.md`](../0-ROADMAP.md) §"未来演进"）

v5 的主题：**安全与可观测基线**。

目标是把"可观测 + 可控 + 可审计"做到产品级，并把 v4 留下的 9 项核心差距收敛到 ds-java v0.1.7 同等水平。

## 核心子 TODO（与 ds-java 对齐）

| # | 主题 | 对应 Java | 计划 |
|---|---|---|---|
| P5-1 | 会话写租约 | `SessionWriteLeaseService` | [`PHASE-5-PLAN.md`](./PHASE-5-PLAN.md)（待建）|
| P5-2 | Token 计量域 | `SessionTokenMeterService` | 同上 |
| P5-3 | 模型渠道配置 | `ModelSettingService` + DB | 同上 |
| P5-4 | 凭据抽象 | `CredentialService` | 同上 |
| P5-5 | 审批矩阵 | `PermissionPolicyService` | 同上 |
| P5-6 | Hook 机制 | `HookService` | 同上 |

详细设计待 v5 启动时新建 [`./DESIGN-v5.md`](./DESIGN-v5.md)（待建）。

## 验收

- 所有 `TC-v5-xxxx` 新增用例在 `go test ./...` 中通过；
- `go test -race ./...` 无数据竞争；
- 全部 v3/v4 既有测试不回归。

## 下一版

v5 → v6：任务系统与持久化工作流。