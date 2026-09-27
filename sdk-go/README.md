# sdk-go

`ds-go`（deepseek-harness-go）的官方 Go SDK。

**v7.0.0 状态**：✅ 发布（与 ds-go v7.0.0 对齐）

## 安装

```bash
go get deepseek-harness-all/sdk-go
```

## 30 行 Hello

```go
package main

import (
    "context"
    "fmt"
    "os"

    "deepseek-harness-all/sdk-go/client/http"
    "deepseek-harness-all/sdk-go/types"
)

func main() {
    c := http.New("http://127.0.0.1:7777")
    resp, err := c.SendSession(context.Background(), types.SessionSendRequest{
        SessionID: "demo",
        Content:   "ping",
    })
    if err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
    fmt.Println("accepted =", resp.Accepted)
}
```

## 能力

| API | 用途 |
|---|---|
| `SendSession` | 提交一轮 prompt（POST /v1/session/send） |
| `SubscribeEvents` | 订阅事件流（GET /v1/events，SSE） |
| `Permission` | 提交审批决策（POST /v1/permission） |
| `WithToken` | 注入 Bearer token |

JSON-RPC 客户端（`client/jsonrpc`）作为可选项：用 `Transport` 接口注入
你喜欢的 WebSocket / Unix socket / pipe 实现。

## 子包

- `types/` — wire 共享类型（Frame / ToolCall / ToolResult / ...）
- `client/http/` — HTTP / SSE 客户端
- `client/jsonrpc/` — JSON-RPC 2.0 客户端（transport 抽象）
- `examples/echo/` — 最小 demo（单次 send + 订阅）
- `examples/orchestrator/` — 多步演示（拆解 → 分析 → 综合，等每步 done 帧再推下一步）

## 兼容

- Go 1.22+
- 仅 stdlib 依赖（无第三方）
- 与 ds-go Gateway SSE v4 帧结构兼容
- 与 ds-go v7.0.0 同发（tag `v7.0.0`）
