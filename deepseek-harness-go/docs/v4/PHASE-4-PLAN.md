# PHASE-4-PLAN — 测试用例库 + v4 收尾

> 对应阶段：v4 P4（[0-ROADMAP.md](./0-ROADMAP.md) §3 P4）
> 对应详细设计：[DESIGN-v4.md](./DESIGN-v4.md) §D
> 状态：☐ 待启动
> 工时：1 天
> 依赖：P1 / P2 / P3 已完成

## §1 目标

把 v4 各阶段验收用例集中到 `docs/test-cases.md`，每个用例有：
- 唯一 ID（TC-NNNN）
- 来源（v3 §X / v4 §Y / 旧端点）
- 触发命令或 API 调用
- 期望结果
- 自动化测试函数指针（`*_test.go::TestFunc`）

收尾产出 `docs/RELEASE-v4.md`，记录所有变更、迁移指南、与 v3 的差异。

## §2 完成判定

- [ ] `docs/test-cases.md` 收录 ≥ 50 个 TC，且每个 TC 都可追溯到代码或文档
- [ ] `docs/RELEASE-v4.md` 创建（变更列表 + 迁移指南 + 已知限制）
- [ ] README 更新（增加 Gateway SSE 调用示例）
- [ ] version 常量升到 v4.0.0-dev
- [ ] `go test -count=1 ./...` 全绿
- [ ] `go vet ./...` 全绿
- [ ] git tag v4.0.0

## §3 子 TODO

- T4.1 test-cases.md：聚合 v3/v4 所有验收 + 自动化映射
- T4.2 RELEASE-v4.md
- T4.3 README Gateway 章节
- T4.4 version bump
- T4.5 全量验证
- T4.6 git tag

## §4 风险

- TC 数量不足 → 提前盘点 4 个 phase 已有用例数
- 迁移指南错漏 → 真实跑一次 v3 → v4 升级流程

## §5 完成后

- 更新 0-ROADMAP §3 P4 ☐ → ✅
- 更新 README 主线
- 整体交付给用户