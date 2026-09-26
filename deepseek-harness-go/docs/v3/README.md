# v3（deepseek-harness-go v3.0.0）

v3 在 v2 公开 API 不变的前提下，叠加 7 大模块：

- §A Skill System — `internal/skill`
- §B Sub-agent（Sync）— `internal/subagent`
- §C Observability（OTel）— `internal/obs`
- §D Context Compaction — `internal/compaction`
- §E Audit Log — `internal/audit`
- §F OS Sandbox — `internal/sandbox`
- §G 多渠道 LLM（OpenAI/Anthropic/Ollama/Gemini）— `internal/llm/provider`

## 文档

- [`PLAN-v3.md`](./PLAN-v3.md) — v3 整体规划（从 v2 基线出发的演进蓝图）
- [`DESIGN-v3.md`](./DESIGN-v3.md) — v3 详细设计（按 §A–§G 分节）
- [`RELEASE-v3.md`](./RELEASE-v3.md) — v3 发布说明（含验收矩阵）

## 状态

- ✅ 完成；git tag `v3.0.0`（2026-09-26）。
- 24 个 Go 包 `go test ./...` 全绿；`go vet ./...` 干净。

## 上游对齐

v3 设计参考了 `deepseek-harness-java` v0.1.3 的核心抽象（`Agent Harness` /
`sandbox` / `audit`），并把 Java 端的 OTel、JSONL 审计、compaction 策略翻译成 Go。

## 下一版

v3 → v4：事件溯源 + Gateway SSE + 插件库存视图。详见 [`../v4/RELEASE-v4.md`](../v4/RELEASE-v4.md)。