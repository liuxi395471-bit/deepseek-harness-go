# 端到端示例：`greet` 会话

这是 `dsh` 的**权威冒烟测试**。跑通它，说明你的环境已经端到端接好了。

```text
$ go build ./cmd/dsh
$ ./dsh -config ./harness.yml -debug
dsh 3.0.0-dev — workspace=/abs/path/.dsh/workspace model=glm-5.3-flash provider=openai
dsh> ready. type your prompt, 'exit' or Ctrl+D to quit.
dsh>

dsh> 请使用 greet 工具向 Ada 打招呼
  * phase=init @ 2026-09-14T01:18:00+08:00
  * phase=llm_call @ 2026-09-14T01:18:00+08:00
  → tool greet({"name":"Ada"})
  * phase=tool_exec @ 2026-09-14T01:18:01+08:00
  ← greet (ok, 312µs)
      你好，Ada！我是 dsh。
  * phase=llm_call @ 2026-09-14T01:18:01+08:00
你好，Ada！我是 dsh。

dsh> exit
$
```

## 这证明了什么

1. ✅ 配置能加载（`harness.yml` 语法正确，或 env 覆盖生效）。
2. ✅ LLM 端点可达，且支持工具调用。
3. ✅ 工具注册表与 system prompt 组合正确（模型能在工具清单里看到 `greet`）。
4. ✅ ReAct 循环至少跑了一轮（LLM → tool_call → tool_result → LLM）。
5. ✅ 事件通道能到达 REPL：`PhaseChange` / `ToolCallStart` / `ToolResult` / `AssistantMessage`。
6. ✅ 最终助手内容打印到 stdout。
7. ✅ `exit` 能干净退出。

## 排查顺序

若某一步失败，按此顺序检查：

1. **启动即配置报错** → 检查 `harness.yml` 语法与 `DEEPSEEK_BASE_URL`。
2. **`phase=llm_call` 后立刻 `error`** → 用 `-prompt` 单次模式复现，缩小到 LLM 层还是 Runner 层：
   ```bash
   ./dsh -config ./harness.yml -prompt "你好" -debug
   ```
3. **模型从不返回 `tool_calls`** → 该模型可能不支持工具调用，或 system prompt 过于限制；换一个模型试试。
4. **`unknown tool: greet`** → 内置工具没被注册；确认是从 `cmd/dsh` 构建的二进制。

> 提示：`-debug` 会把所有 agent 事件打到 stderr，助手正文仍在 stdout，两者互不干扰。

---

**相关文档**：[README.md](../README.md)（快速开始）· [ARCHITECTURE.md](../ARCHITECTURE.md)（架构设计）· [TESTING.md](../TESTING.md)（测试与验收）
