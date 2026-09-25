# PHASE-2-PLAN — 统一 Gateway SSE

> 对应阶段：v4 P2（[0-ROADMAP.md](./0-ROADMAP.md) §3 P2）
> 对应详细设计：[DESIGN-v4.md](./DESIGN-v4.md) §B
> 状态：☐ 待启动
> 工时：2 天
> 依赖：P1 事件溯源（已完成）

## §1 目标

把所有流式 / 一次性 HTTP 端点收敛为单一 SSE 入口：

```
POST /api/gateway/stream
Content-Type: application/json
Body: {"source": "<name>", "params": {...}}
```

旧端点 `/api/agent/message`、`/api/agent/stream`、`/api/sessions` 等
改为 **代理到 Gateway**——v3 client 不退化。

## §2 完成判定

- [ ] `internal/server/gateway.go` 创建（DTO + 路由器）
- [ ] `internal/server/gateway_handler.go` 创建（内置 source 实现）
- [ ] 旧 5 端点保持兼容：handler 改为代理到 Gateway
- [ ] 5 个验收用例通过
- [ ] `go test ./internal/server/...` 全绿
- [ ] README 更新（Gateway SSE 调用示例）

## §3 内置 Source 清单

| Source | params | 行为 |
|---|---|---|
| `events.subscribe` | `{sid, since_seq?}` | 订阅 sid 的事件流（P1 EventStore） |
| `session.send` | `{sid, prompt}` | 启动 RunStream；返回 final 帧 |
| `session.snapshot` | `{sid, projection}` | 取一次 messages / usage / phase 投影 |
| `tools.list` | `{}` | 列出本地工具（stub，真实集成 P3） |
| `plugins.list` | `{}` | 列出插件（stub，真实集成 P3） |
| `llm.call` | `{prompt, model}` | 直接 LLM 调用，跳过 agent loop |

P2 阶段实现核心 sources（events.subscribe / session.send /
session.snapshot / llm.call）。tools.list / plugins.list 提供 stub
返回空数组（让 P3 接入）；不返回 error。

## §4 子 TODO

- T2.1 `gateway.go`：`GatewayRequest` / `GatewayEvent` / `SourceHandler`
- T2.2 `gateway.go`：`Router` + `Register(source, handler)` + `Dispatch`
- T2.3 `gateway_handler.go`：实现 4 个核心 source + 2 个 stub
- T2.4 `server.go`：注册 `/api/gateway/stream`，改写旧 5 端点为代理
- T2.5 5 个验收用例

## §5 验收用例

| 用例 | 断言 |
|---|---|
| 单 source 事件流可消费 | events.subscribe 收到 ≥ 1 帧 |
| session.send 完成返回 final | 终止帧含 stop_reason / rounds |
| session.snapshot 拿到 messages | 与 Load 等价 |
| ctx 取消时流立即关 | client disconnect 后 handler goroutine 退出 < 100ms |
| 旧 /api/agent/message 仍工作 | 旧端点返回与 Gateway session.send 等价结果 |

## §6 风险

- 旧端点行为变化 → 代理路径保持字节级兼容
- SSE 多 source 反压 → 每 source 一个 chan(256)，handler 退出时 close
- 大负载 → 流式写出，不用 `bytes.Buffer` 累积

## §7 完成后

- 更新 0-ROADMAP §3 P2 ☐ → ✅
- 创建 PHASE-3-PLAN.md