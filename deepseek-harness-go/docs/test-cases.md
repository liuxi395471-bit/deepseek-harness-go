# dsh v4 测试用例库

> 本文档汇总 v3 + v4 全部验收用例。每个 TC 有唯一编号 `TC-NNNN`、
> 来源（v3 §X / v4 §Y）、触发方式、期望结果，并指向自动化测试函数。

## 用例编号约定

- `TC-0001`~`TC-0049`：v3 阶段用例（[RELEASE-v3.md](./RELEASE-v3.md)）
- `TC-0050`~`TC-0099`：v4 阶段用例
  - `TC-0050`~`TC-0069`：v4 P1 事件溯源
  - `TC-0070`~`TC-0084`：v4 P2 Gateway SSE
  - `TC-0085`~`TC-0099`：v4 P3 库存视图

## v3 §A Skill System（6 用例）

| ID | 标题 | 期望 | 自动化 |
|---|---|---|---|
| TC-0001 | 加载 skill 目录成功 | 列出 N 个 skill | `internal/skill/skill_test.go::TestSkillLoader` |
| TC-0002 | mention 匹配命中 | mention 关键词命中并注入 body | `internal/skill/matcher_test.go` |
| TC-0003 | always-on 注入 | always=true 的 skill 总是注入 | `internal/skill/skill_test.go` |
| TC-0004 | frontmatter 缺字段报错 | 不静默；返回错误 | `internal/skill/loader_test.go` |
| TC-0005 | 目录不存在返回空 | err == nil，len==0 | `internal/skill/loader_test.go` |
| TC-0006 | `/skills` REPL 命令 | 打印已加载列表 | `cmd/dsh/repl/repl_test.go` |

## v3 §B Sub-agent（5 用例）

| ID | 标题 | 期望 | 自动化 |
|---|---|---|---|
| TC-0007 | 子 agent 工具注册 | registry 含 agent_spawn | `internal/subagent/parent_test.go` |
| TC-0008 | 嵌套 agent_spawn 拒绝 | 第二次注册时去掉 | `internal/subagent/subagent_test.go` |
| TC-0009 | 子 agent panic 折叠为 IsError | tool.Result.IsError=true | `internal/subagent/subagent_test.go` |
| TC-0010 | 子 agent 超时 | ctx cancel 后返回 IsError | `internal/subagent/subagent_test.go` |
| TC-0011 | 子 agent 拿到父 messages | 通过 result 返回 | `internal/subagent/result_test.go` |

## v3 §C Observability（4 用例）

| ID | 标题 | 期望 | 自动化 |
|---|---|---|---|
| TC-0012 | obs.Defaults() 零开销 | noop tracer / meter | `internal/obs/obs_test.go` |
| TC-0013 | debug 模式 stderr logger | 输出到 stderr | `internal/obs/stderr_test.go` |
| TC-0014 | otel tracer 端点配置 | endpoint 字段生效 | `internal/obs/otel_test.go` |
| TC-0015 | Runner span instrumentation | agent.run / llm.chat / tool.execute | `internal/agent/runner_test.go` |

## v3 §D Compaction（5 用例）

| ID | 标题 | 期望 | 自动化 |
|---|---|---|---|
| TC-0016 | 触发后消息数减少 | Before > After | `internal/compaction/compaction_test.go` |
| TC-0017 | system 始终保留 | 压缩后 msgs[0] 仍是 system | `internal/compaction/compactor_test.go` |
| TC-0018 | keep-recent 保留 | 最后 N 条不动 | `internal/compaction/truncate_test.go` |
| TC-0019 | llm-summary 摘要 | summary placeholder 替换前面 | `internal/compaction/llmsummary_test.go` |
| TC-0020 | Compacted 事件 | Before/After 字段正确 | `internal/agent/runner_test.go` |

## v3 §E Audit Log（4 用例）

| ID | 标题 | 期望 | 自动化 |
|---|---|---|---|
| TC-0021 | tool_call 写入 jsonl | 一行 JSON，包含 tool/args_hash | `internal/audit/log_test.go` |
| TC-0022 | redact 只记 hash | args_raw 缺省 | `internal/audit/log_test.go` |
| TC-0023 | audit.full 记录原文 | 含 args_raw | `internal/audit/log_test.go` |
| TC-0024 | 多 session 并发写 | 不交错 | `internal/audit/log_test.go` |

## v3 §F Sandbox（6 用例）

| ID | 标题 | 期望 | 自动化 |
|---|---|---|---|
| TC-0025 | noop sandbox pass-through | 命令正常运行 | `internal/sandbox/sandbox_test.go` |
| TC-0026 | windows_acl 受限令牌 | CreateRestrictedToken 返回 token | `internal/sandbox/windows_acl_test.go` |
| TC-0027 | System32 路径拒绝 | Apply 返回拒绝 | `internal/sandbox/windows_acl_test.go` |
| TC-0028 | linux_ns 路径前缀拒绝 | /etc/shadow 拒绝 | `internal/sandbox/linux_ns_test.go` |
| TC-0029 | Token 句柄释放 | 每次执行后释放 | `internal/sandbox/windows_acl_test.go` |
| TC-0030 | validateCmd 检查 cmd.Path | 路径为相对路径时拒绝 | `internal/sandbox/sandbox_test.go` |

## v3 §G LLM Provider（6 用例）

| ID | 标题 | 期望 | 自动化 |
|---|---|---|---|
| TC-0031 | provider.NewClient 路由 | 按 provider 字段选择实现 | `internal/llm/provider/provider_test.go` |
| TC-0032 | ollama ndjson round-trip | mock fixture 解析 | `internal/llm/ollama/ollama_test.go` |
| TC-0033 | ollama tool_calls 映射 | response 解析为 tool_calls | `internal/llm/ollama/ollama_test.go` |
| TC-0034 | gemini SSE delta 累积 | 完整文本回复 | `internal/llm/gemini/gemini_test.go` |
| TC-0035 | gemini functionCall 映射 | 转 tool_calls | `internal/llm/gemini/gemini_test.go` |
| TC-0036 | 未支持 provider 报错 | NewClient 返回错误 | `internal/llm/provider/provider_test.go` |

## v3 SSE & Runner（5 用例）

| ID | 标题 | 期望 | 自动化 |
|---|---|---|---|
| TC-0037 | /api/agent/stream 同会话并发 409 | 第二个请求 409 | `internal/server/server_test.go::TestServer_Stream_ConcurrentSameSession_409` |
| TC-0038 | /api/agent/stream 客户端断开 | handler goroutine < 100ms 退出 | `internal/server/server_test.go::TestServer_Stream_ClientDisconnect` |
| TC-0039 | /api/agent/message happy path | 200 + JSON 结构 | `internal/server/server_test.go::TestServer_Message_HappyPath` |
| TC-0040 | skill 注入不重复累积 | 多轮注入后 system 不含重复 | `internal/agent/runner_v3_test.go` |
| TC-0041 | SSE 多行 data 解析 | 含换行的 data 完整发出 | `internal/llm/chatstream_test.go` |

## v3 Store & Subagent（5 用例）

| ID | 标题 | 期望 | 自动化 |
|---|---|---|---|
| TC-0042 | MapStore Begin/Append | session 创建并追加 | `internal/store/mem_test.go` |
| TC-0043 | SQLiteStore AppendEvent | 事件持久化 | `internal/store/sqlite_test.go` |
| TC-0044 | MapStore 并发 Append | 数据竞争不发生 | `internal/store/mem_test.go` |
| TC-0045 | subagent.Attach 注册 tool | registry 含 agent_spawn | `internal/subagent/parent_test.go` |
| TC-0046 | approval 拒绝时工具 IsError | tool.Result.IsError=true | `internal/approval/approval_test.go` |
| TC-0047 | usage.Tracker 累计 | tokens 累加正确 | `internal/usage/usage_test.go` |
| TC-0048 | stream package 提供 fanout | Event 全部发完 | `internal/stream/stream_test.go` |
| TC-0049 | skill NewFileLoader 不存在目录 | 返回空，不报错 | `internal/skill/loader_test.go` |

## v4 §A 事件溯源 P1（20 用例）

| ID | 标题 | 期望 | 自动化 |
|---|---|---|---|
| TC-0050 | AppendEvent 写库 | events 表新增一行 | `internal/store/sqlite_test.go::TestEventStore_AppendRead` |
| TC-0051 | ReadEvents 按 seq 排序 | 升序返回 | `internal/store/sqlite_test.go::TestEventStore_ReadOrder` |
| TC-0052 | GetLastSeq 空会话 | 0 | `internal/store/sqlite_test.go::TestEventStore_EmptySeq` |
| TC-0053 | AppendEvent 同一 (sid,seq) 冲突 | 返回 ErrConflict | `internal/store/sqlite_test.go::TestEventStore_DuplicateSeq` |
| TC-0054 | 并发 AppendEvent | 全部成功 | `internal/store/sqlite_test.go::TestEventStore_Concurrent` |
| TC-0055 | MapStore AppendEvent | in-memory events 追加 | `internal/store/mem_test.go::TestMemEventStore_Append` |
| TC-0056 | MapStore ReadEvents 全部 | 返回顺序正确 | `internal/store/mem_test.go::TestMemEventStore_ReadAll` |
| TC-0057 | MapStore 投影缓存命中 | 第二次 Project 不重算 | `internal/store/mem_test.go::TestMemEventStore_ProjectionCache` |
| TC-0058 | AppendEvent 失效投影缓存 | 投影下次重算 | `internal/store/mem_test.go::TestMemEventStore_CacheInvalidate` |
| TC-0059 | MessagesProjector 派生 | events → []llm.Message | `internal/store/projection_test.go` |
| TC-0060 | UsageProjector 累加 | prompt/completion/total | `internal/store/projection_test.go` |
| TC-0061 | PhaseProjector 当前 phase | 最新 phase 返回 | `internal/store/projection_test.go` |
| TC-0062 | ProjectAll 合并 | 三个投影一致 | `internal/store/projector_test.go` |
| TC-0063 | 投影匹配 v3 Messages | 与 Load.Messages 等价 | `internal/store/sqlite_test.go::TestEventStore_ProjectionMatchesLoad` |
| TC-0064 | AsEventStore 转换 | 接口适配 | `internal/store/store_test.go::TestAsEventStore` |
| TC-0065 | AppendEvent payload marshal | JSON 序列化正确 | `internal/store/event_test.go` |
| TC-0066 | EventType.String | 返回可读名 | `internal/store/event_test.go` |
| TC-0067 | EventStore 投影 cache 命中 | cache hit 返回原值 | `internal/store/mem_test.go` |
| TC-0068 | EventStore 跨会话隔离 | read 不混入 | `internal/store/sqlite_test.go::TestEventStore_SessionIsolation` |
| TC-0069 | Project since_seq 截断 | 从 since_seq 开始 | `internal/store/sqlite_test.go::TestEventStore_ReadSince` |

## v4 §B Gateway SSE P2（15 用例）

| ID | 标题 | 期望 | 自动化 |
|---|---|---|---|
| TC-0070 | /api/gateway/stream session.send 完成 | final 帧含 stop_reason | `internal/server/server_test.go::TestGateway_SessionSend_FinalFrame` |
| TC-0071 | events.subscribe 拿到 ≥ 1 帧 | delta + final | `internal/server/server_test.go::TestGateway_EventsSubscribe` |
| TC-0072 | session.snapshot 取 messages | 投影可用 | `internal/server/server_test.go::TestGateway_SessionSnapshot` |
| TC-0073 | unknown source 返回 error 帧 | 错误帧写入 | `internal/server/server_test.go::TestGateway_UnknownSource` |
| TC-0074 | tools.list stub | 空数组 + final | `internal/server/server_test.go::TestGateway_Stubs` |
| TC-0075 | Router.Sources 返回名字列表 | 含已注册名字 | `internal/server/server_test.go::TestRouter_UnknownSource` |
| TC-0076 | Router.Dispatch 未知 source | 返回 ErrUnknownSource | `internal/server/server_test.go::TestRouter_UnknownSource` |
| TC-0077 | emit 在 ctx 取消时返回 | ctx.Err() | `internal/server/server_test.go::TestGateway_EmitCancelledCtx` |
| TC-0078 | Gateway handler 旧端点兼容 | /api/agent/message 等价 | `internal/server/server_test.go::TestServer_Message_HappyPath` |
| TC-0079 | SSE frame 格式正确 | data: {json}\n\n | `internal/server/server_test.go::TestGateway_SessionSend_FinalFrame` |
| TC-0080 | SetLLMClient 后 llm.call 工作 | 200 + content | `internal/server/gateway_handler_test.go` |
| TC-0081 | 无 client 时 llm.call error 帧 | err message | `internal/server/gateway_handler_test.go` |
| TC-0082 | Bearer auth 仍生效 | 401 | `internal/server/server_test.go::TestServer_Message_NoAuth` |
| TC-0083 | /healthz 无需 auth | 200 | `internal/server/server_test.go::TestServer_Healthz_NoAuth` |
| TC-0084 | Router.Register 覆盖同名 | 返回前一个 handler | `internal/server/server_test.go::TestRouter_UnknownSource` |

## v4 §C Plugin Inventory P3（12 用例）

| ID | 标题 | 期望 | 自动化 |
|---|---|---|---|
| TC-0085 | LocalInventory 列出全部工具 | Names == Registry.Names | `internal/plugin/view_test.go::TestLocalInventory_List` |
| TC-0086 | LocalInventory Risk 分级 | shell→high | `internal/plugin/view_test.go::TestLocalInventory_Risk` |
| TC-0087 | LocalInventory Get | 工具名查找成功 | `internal/plugin/view_test.go::TestLocalInventory_Get` |
| TC-0088 | Combined 不同 plugin | 聚合多源 | `internal/plugin/view_test.go::TestCombined_Dedupe` |
| TC-0089 | Combined 同名 plugin 覆盖 | 后注册覆盖前注册 | `internal/plugin/view_test.go::TestCombined_NameOverride` |
| TC-0090 | LocalInventory 空 registry | 1 entry, 0 tools | `internal/plugin/view_test.go::TestLocalInventory_Empty` |
| TC-0091 | LocalInventory nil registry | 返回 nil | `internal/plugin/view_test.go::TestLocalInventory_NilReg` |
| TC-0092 | GRPCInventory 空 | 返回 [] | `internal/plugin/grpc_view_test.go::TestGRPCInventory_Empty` |
| TC-0093 | GRPCInventory Get missing | 返回 false | `internal/plugin/grpc_view_test.go::TestGRPCInventory_GetMissing` |
| TC-0094 | GRPCInventory Health missing | 返回 error | `internal/plugin/grpc_view_test.go::TestGRPCInventory_HealthMissing` |
| TC-0095 | GRPCInventory nil client | Health 报错 | `internal/plugin/grpc_view_test.go::TestGRPCInventory_NilClientHealth` |
| TC-0096 | GRPCInventory zero specs | Healthy=false | `internal/plugin/grpc_view_test.go::TestGRPCInventory_UnhealthyWhenNoSpecs` |

## v4 §C Gateway 接入（3 用例）

| ID | 标题 | 期望 | 自动化 |
|---|---|---|---|
| TC-0097 | tools.list 接入 Inventory | 工具真实可见 | `internal/server/server_test.go::TestGateway_ToolsList_WithInventory` |
| TC-0098 | plugins.list 接入 Inventory | plugin entry 可见 | `internal/server/server_test.go::TestGateway_PluginsList_WithInventory` |
| TC-0099 | main.go build combined | CLI build 通过 | `go build ./cmd/dsh` |

## 自动化映射表

`go test -count=1 ./...` 应覆盖 TC-0001 ~ TC-0099 中由自动化测试
负责的部分。少量端到端用例（CLI smoke / Windows ACL 实跑）需要
手动验证；详见 [RELEASE-v4.md](./RELEASE-v4.md) §验证矩阵。