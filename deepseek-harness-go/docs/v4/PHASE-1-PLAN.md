# PHASE-1-PLAN — Session 事件溯源 + 投影缓存

> **对应阶段**：v4 P1（[0-ROADMAP.md](./0-ROADMAP.md) §3 P1）
> **对应详细设计**：[DESIGN-v4.md](./DESIGN-v4.md) §A
> **状态**：☐ 待启动 → ☐ 进行中 → ☐ 完成
> **工时**：3 天
> **基线**：v3.0.0 tag（含 130+ 既有测试）

## §1 目标

把 v3 `store.Store.Append(message)` 重构为 v4 双轨：

- **新增** `AppendEvent(Event)` + `ReadEvents(sid, sinceSeq)` + `GetLastSeq(sid)`
- **保留** 全部 v3 公开方法（签名冻结）；v3 `Append(Message)` 内部转发到 `AppendEvent(Event)` + `Projector.Messages()`
- 新增 `events` 表 + `MemoryProjectionCache`
- runner 写入路径切到事件流；v3 测试**不退化**

## §2 完成判定

- [ ] `internal/store/event.go` 创建（Event / EventType 定义）
- [ ] `internal/store/projection.go` 创建（Projection interface + Memory cache）
- [ ] `internal/store/projector.go` 创建（Messages / Usage / Phase 三种投影）
- [ ] `internal/store/store.go` 扩展：`EventStore` 子接口 + v3 Store 保留
- [ ] `internal/store/sqlite.go` 升级：events 表 schema + AppendEvent/ReadEvents/GetLastSeq
- [ ] `internal/store/mem.go` 升级：对应内存实现 + v3 兼容层
- [ ] `internal/agent/runner.go` 切换到事件流（仅在 Store 提供 EventStore 时）
- [ ] 6 个验收用例全部通过
- [ ] `go test -count=1 ./...` 全绿（v3 既有测试不退化）
- [ ] `go test -cover ./internal/store/...` ≥ 80%

## §3 子 TODO 索引

### T1.1 event.go（核心类型）

- [ ] `type EventType int` + iota 常量（10 种）
- [ ] `type Event struct { Sid, Seq, Type, Timestamp, Payload []byte, Actor string }`
- [ ] `func (e Event) MarshalAppendEvent()` / `UnmarshalPayload(v any) error`
- [ ] 预置 payload 结构体：`UserMessagePayload` / `AssistantMessagePayload` / `ToolCallPayload` / `ToolResultPayload` / `LLMCallPayload` / `PhaseChangePayload` / `CompactionPayload`

### T1.2 projection.go

- [ ] `type ProjectionState any`
- [ ] `type Projection interface { Name() string; Apply(ev Event, state ProjectionState) error }`
- [ ] `type ProjectionCache interface { Get/Set/Invalidate }`
- [ ] `type MemoryProjectionCache struct { mu sync.Mutex; data map[string]map[string]ProjectionState }`

### T1.3 projector.go

- [ ] `type MessagesProjector struct{}` —— 把 SessionBegin→System→User→Assistant→Tool 投影为 `[]llm.Message`
- [ ] `type UsageProjector struct{}` —— 累加 LLMCall 事件的 token
- [ ] `type PhaseProjector struct{}` —— 记录最后一次 PhaseChange
- [ ] `type ProjectorSet struct{ Messages, Usage, Phase }` —— 一站式批量 Apply
- [ ] `func ProjectAll(events []Event, set ProjectorSet) (Messages []llm.Message, Usage llm.Usage, Phase string, err error)`

### T1.4 store.go 接口扩展

- [ ] 新增 `type EventStore interface { AppendEvent/ReadEvents/GetLastSeq }`
- [ ] v3 `Store` 不变（签名冻结）
- [ ] `func AsEventStore(Store) (EventStore, bool)` —— 类型断言

### T1.5 sqlite.go 升级

- [ ] `events` 表 schema + `idx_events_session_seq` 索引
- [ ] `AppendEvent` 实现（事务 + seq 自增）
- [ ] `ReadEvents(sid, sinceSeq)` 实现
- [ ] `GetLastSeq(sid)` 实现
- [ ] 兼容层：`Append(message)` → 内部生成 `UserMessage/AssistantMessage/ToolResult` Event → `AppendEvent` → `Projector.Messages` 写入 messages 表（**保留 v3 messages 表写路径**，但驱动源换成事件）

### T1.6 mem.go 升级

- [ ] `events []Event` 字段加入 mapSession
- [ ] `AppendEvent/ReadEvents/GetLastSeq` 实现
- [ ] 兼容层同 T1.5

### T1.7 runner.go 切换

- [ ] LoopRunner 增加字段 `eventStore store.EventStore`（从 `r.Store.(store.EventStore)` 取出）
- [ ] Run 启动时写 `EventSessionBegin` + `EventSystemPrompt` 两条事件
- [ ] 每轮写 `EventLLMCall` + `EventAssistantMessage`（响应）+ `EventPhaseChange`
- [ ] Tool 调用写 `EventToolCall` + `EventToolResult`
- [ ] Store 为 nil 或非 EventStore 时降级到 v3 路径（兼容旧 Store 实现）

### T1.8 验收用例（6 个）

| 用例 | 断言 | 自动化 |
|---|---|---|
| 长会话 100 轮 → events 表 ≥ 100 条 | `len(ReadEvents) >= 100` | ✅ TestStore_AppendEvent_100Rounds |
| 投影 Messages 与原 messages 表一致 | 字节级相同 | ✅ TestProjector_Messages_MatchesV3 |
| cache 命中 | 二次 Project 命中率 100% | ✅ TestProjectionCache_HitRate |
| 多 session 并发互不干扰 | 各 sid 自身 seq 单调 | ✅ TestStore_AppendEvent_ConcurrentSessions |
| legacy messages 仅读不写 | 旧 Append 仍成功但 events 表不增 | ✅ TestStore_LegacyAppendNoEventLeak |
| Runner 走事件流 | 捕获 → 投影 → messages 一致 | ✅ TestRunner_WritesEventStream |

## §4 验收清单

- `go test -count=1 ./...` 全绿
- `go vet ./...` 0 warning
- `go test -cover ./internal/store/...` ≥ 80%
- 6 个 T1.8 验收用例全部通过

## §5 风险与缓解

| 风险 | 缓解 |
|---|---|
| v3 测试因事件流重构退化 | 兼容层转发 Append → AppendEvent+Project；零行为变化 |
| 投影与 messages 表不一致 | P1 期间双写 + T1.8 第二个用例做字节级闸口 |
| Append 事务冲突 | SQLite 默认串行化；Append 与 AppendEvent 锁同一条 sessions 行 |

## §6 完成后

- 更新 0-ROADMAP §3 P1 状态 ☐ → ✅
- 创建 `PHASE-2-PLAN.md`
- 回到 ROADMAP 节奏继续 P2

