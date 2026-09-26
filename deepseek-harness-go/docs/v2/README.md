# v2 历史快照（deepseek-harness-go v2.0.2）

v2 引入 plugin gRPC（T13）、Anthropic ChatStream（T14）、MCP Session
（T15-1）。本目录是历史快照，新工作请参阅 [`../0-ROADMAP.md`](../0-ROADMAP.md)。

## 文档

- [`DESIGN-v2.md`](./DESIGN-v2.md) — v2 详细设计

## 状态

- 已冻结，对应 git tag `v2.0.2`（2026-09-16）。
- 上游 baseline lock：v3 / v4 演进从该 tag 出发。

## 升级路径

v2 → v3（Skill+Sub-agent+Obs+Audit+Compaction+Ollama+Gemini）
v2 → v4（事件溯源 + Gateway SSE + 库存视图）
详见 [`../v3/RELEASE-v3.md`](../v3/RELEASE-v3.md) 与 [`../v4/RELEASE-v4.md`](../v4/RELEASE-v4.md)。