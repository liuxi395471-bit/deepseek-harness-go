# End-to-end example: the `greet` session

This is the canonical smoke test for `dsh`. Once it works, your environment is wired correctly end-to-end.

```text
$ go build ./cmd/dsh
$ ./dsh -config ./harness.example.yml -debug
dsh 0.1.0-dev — workspace=/abs/path/.dsh/workspace model=glm-5.3-flash
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

## What this proves

1. ✅ Config loads (no env override needed because `harness.example.yml` carries the demo URL).
2. ✅ LLM endpoint is reachable and tool-calling is enabled.
3. ✅ Tool registry + system prompt composition (model sees `greet` in the tool catalogue).
4. ✅ ReAct loop runs at least one round (LLM → tool_call → tool_result → LLM).
5. ✅ Event channel reaches REPL: `PhaseChange` / `ToolCallStart` / `ToolResult` / `AssistantMessage`.
6. ✅ Final assistant content is printed to stdout.
7. ✅ Clean exit on `exit`.

If any step fails, check (in order):

1. **Config load error** at startup → re-check `harness.yml` syntax and `DEEPSEEK_BASE_URL`.
2. **`phase=llm_call` then immediate `error`** → run `llmprobe` to isolate LLM-vs-Runner.
3. **Model never returns `tool_calls`** → your model may not support tool calling, or the system prompt is too restrictive; try a different model.
4. **`unknown tool: greet`** → internal tools were not registered; rebuild from `cmd/dsh`.
