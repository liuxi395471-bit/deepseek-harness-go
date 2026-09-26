# PHASE-3-PLAN — 插件库存视图

> 对应阶段：v4 P3（[0-ROADMAP.md](./0-ROADMAP.md) §3 P3）
> 对应详细设计：[DESIGN-v4.md](./DESIGN-v4.md) §C
> 状态：☐ 待启动
> 工时：1.5 天
> 依赖：P2 统一 Gateway SSE（已完成）

## §1 目标

提供 plugin.Tool 清单的可查询、可序列化视图，通过 P2 Gateway SSE
`tools.list` 与 `plugins.list` 两个 source 暴露。本地 Registry 与远端
gRPC 插件统一聚合为 InventoryView。

## §2 完成判定

- [ ] `internal/plugin/view.go` 创建（PluginEntry / ToolSpec / Inventory 接口）
- [ ] `internal/plugin/grpc_view.go` 创建（gRPC plugin hosts 聚合）
- [ ] Gateway `tools.list` / `plugins.list` 接入真实 Inventory
- [ ] 3 个验收用例全部通过
- [ ] `go test -cover ./internal/plugin/... ./internal/server/...` ≥ 80%

## §3 子 TODO

- T3.1 PluginEntry / ToolSpec 结构 + JSON schema
- T3.2 Inventory 接口（List / Get / Health）
- T3.3 LocalInventory：从 Registry 派生（不暴露内部代码路径）
- T3.4 GRPCInventory：从 []*Client 派生（已知 spec + 健康探测）
- T3.5 Combined：聚合 Local + GRPC
- T3.6 Gateway handlers.tools.list / plugins.list 替换 P2 stub
- T3.7 3 个验收用例

## §4 验收用例

| 用例 | 断言 |
|---|---|
| LocalInventory 含全部已注册工具 | Names() == Registry.Names() |
| LocalInventory 不暴露参数 schema 私有字段 | ToolSpec.Parameters 是 JSON Schema |
| Combined 去重 + 远端优先 | 同名 plugin → 远端覆盖本地 |

## §5 风险

- gRPC plugin health probe 阻塞 → 2s 超时（DESIGN-v4 §C.3 约定）
- 工具风险字段未声明 → 默认 "low"（DESIGN-v4 §C.5）

## §6 完成后

- 更新 0-ROADMAP §3 P3 ☐ → ✅
- 创建 PHASE-4-PLAN.md