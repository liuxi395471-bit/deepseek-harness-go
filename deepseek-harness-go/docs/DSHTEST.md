# dshtest —— 回归测试运行器

> 不用交互输入，直接用 YAML fixtures 批量跑回归测试。

## 快速开始

```bash
# 构建（一次性）
cd d:\Devops\AgentProgram\deepseek-harness-all\deepseek-harness-go
go build -o dshtest.exe ./cmd/dshtest

# 配 key
$env:DSH_API_KEY  = "sk-..."
$env:DSH_BASE_URL = "http://127.0.0.1:8777/v1"
$env:DSH_MODEL    = "glm-5.3-flash"

# 跑全部
.\dshtest.exe -fixtures fixtures/

# 只跑某个用例（子串匹配）
.\dshtest.exe -fixtures fixtures/ -filter greet

# 看完整事件流
.\dshtest.exe -fixtures fixtures/ -filter greet -verbose
```

## YAML 用例格式

```yaml
cases:
  - name: "greet 工具调通"               # 必填；显示名
    input:
      prompt: "请使用 greet 工具向 Ada 打招呼"  # 必填
      max_rounds: 8                  # 可选；不写取默认值
      stream: true                    # 可选；不写取默认值
    expect:
      stop_reason: no_tool_calls      # 必填；最终停止原因
      rounds: 2                       # 可选；不写则跳过检查
      tools_called: [greet]           # 可选；期望被调用的工具名列表
      no_tool_calls: true             # 可选；期望无 tool_calls
      final_content_contains: "Ada"   # 可选；最终消息包含某子串
      final_content_equals: "..."     # 可选；与上一条互斥
      events:                         # 可选；事件流顺序匹配
        - phase_change: llm_call      # phase 值：init/llm_call/tool_exec/stopped/error
        - assistant_message:          # 软匹配：字段全空=忽略该字段
            content: ""
            tool_calls:
              - name: greet
                arguments_contains: "Ada"
        - tool_result:
            name: greet
            content_contains: "Ada"
            is_error: false
        - loop_done:
            rounds: 2
    tags: [greet, smoke]             # 可选；标签，用于过滤
```

### phase 值速查

| YAML 值 | agent.Phase | 含义 |
|---|---|---|
| `init` | PhaseInit | 循环进入 |
| `llm_call` | PhaseLLMCall | 调用 LLM |
| `tool_exec` | PhaseToolExec | 执行工具 |
| `llm_done` | PhaseLLMDone | （保留） |
| `stopped` | PhaseStopped | 正常退出（max_rounds） |
| `error` | PhaseError | 异常退出 |

### stop_reason 值速查

| 值 | 含义 |
|---|---|
| `no_tool_calls` | LLM 输出了最终文本（不调工具） |
| `max_rounds` | 达到最大轮数 |
| `error` | 运行器内部错误 |
| `canceled` | context 取消（Ctrl+C） |

## 命令行 flag

| flag | 默认 | 说明 |
|---|---|---|
| `-fixtures` | `fixtures` | fixtures 目录或单个文件 |
| `-filter` | `""` | 子串过滤（用例 name 或 tag） |
| `-strict` | `false` | 严格模式：所有 expect 字段未匹配都 FAIL |
| `-verbose` | `false` | 打印完整事件 tag 列表 |
| `-parallel` | `1` | 并发数（>1 后台并行） |
| `-update` | `false` | 把实际值写回 YAML（生成期望用） |
| `-max-rounds` | `8` | 全局默认 max rounds |
| `-stream` | `true` | 全局默认流式开关 |
| `-color` | `true` | 色彩开关 |

## 事件流匹配

`expect.events` 里的每一项与实际事件的**顺序子序列**匹配（非严格连续）。字段全空或不存在则忽略该字段。

### 软匹配 vs 硬匹配

- **软匹配**（默认）：`tools_called` / `rounds` / `events` 未匹配时只打印警告，不 FAIL
- **硬匹配**（`-strict`）：未匹配直接 FAIL

## fixtures 目录结构

```
fixtures/
├── smoke.yml       # 冒烟测试（greet + fs_read + 无工具）
├── llm.yml         # LLM 响应路径（stop_reason/error/max_rounds）
├── tools.yml       # 工具注册表行为（重名/未知工具/panic）
└── session.yml     # Session 持久化（resume/export）
```

每个文件独立可跑：

```bash
.\dshtest.exe -fixtures fixtures/smoke.yml
.\dshtest.exe -fixtures fixtures/llm.yml
```

## 用例命名约定

```
<功能> <行为>    # 例如 "greet 工具调通"
<边界> <条件>    # 例如 "未知工具返回 [ERROR]"
<异常> <路径>    # 例如 "panic 不崩溃"
```

## 典型开发流程

### 1. 新增功能，先写用例

```bash
# 在 fixtures/ 下加用例
# 运行看实际结果
.\dshtest.exe -fixtures fixtures/ -filter my-case -verbose
```

### 2. 写期望值（先用 -strict=false 观察）

```bash
# 不写 expect.events；只验证 stop_reason
# 通过后再补充 events 精确断言
```

### 3. CI / 回归

```bash
# 全部跑一遍
.\dshtest.exe -fixtures fixtures/

# 并发加速
.\dshtest.exe -fixtures fixtures/ -parallel 4
```

## 已知限制

| 限制 | 说明 |
|---|---|
| 需要真实 API key | 不含 mock；想 mock 要自己在 fixtures 里配 mock server |
| `dshtest` 不进主二进制 | 单独构建 `go build -o dshtest.exe ./cmd/dshtest` |
| workspace 在 `.\.dshtest-workspace` | 工具的 fs_* 会写到这；gitignore 里忽略 |

