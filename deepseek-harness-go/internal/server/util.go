package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"deepseek-harness-go/internal/agent"
	"deepseek-harness-go/internal/llm"
)

// writeJSON 将 v 以给定状态码序列化为 JSON 写出。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// mustJSON 序列化 v，出错时返回 "{}"（仅在程序 bug 时发生）。
func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// sseFrame 是一条 Server-Sent Events 消息。
type sseFrame struct {
	Event string
	Data  string
}

// writeSSE 写出单个 SSE 帧。
func writeSSE(w http.ResponseWriter, flusher http.Flusher, f sseFrame) {
	if f.Event != "" {
		fmt.Fprintf(w, "event: %s\n", f.Event)
	}
	fmt.Fprintf(w, "data: %s\n\n", f.Data)
	flusher.Flush()
}

// eventToFrame 将 agent.Event 转换为 SSE 帧。
// 只有非平凡事件才会生成帧；phase_change 会保留，但 v2 尚未覆盖所有
// 事件类型（留待扩展）。
func eventToFrame(ev agent.Event) sseFrame {
	switch v := ev.(type) {
	case agent.AssistantDelta:
		return sseFrame{
			Event: "assistant_delta",
			Data:  mustJSON(map[string]any{"text": v.Text, "index": 0}),
		}
	case agent.AssistantMessage:
		return sseFrame{
			Event: "assistant_message",
			Data: mustJSON(map[string]any{
				"content":    v.Content,
				"tool_calls": v.ToolCalls,
			}),
		}
	case agent.ToolCallStart:
		return sseFrame{
			Event: "tool_call_start",
			Data:  mustJSON(map[string]any{"call": v.Call}),
		}
	case agent.ToolResult:
		return sseFrame{
			Event: "tool_result",
			Data: mustJSON(map[string]any{
				"call_id": v.CallID,
				"name":    v.Name,
				"content": v.Content,
				"is_error": v.IsError,
				"took_ms": v.Took.Milliseconds(),
			}),
		}
	case agent.LoopError:
		return sseFrame{
			Event: "loop_error",
			Data:  mustJSON(map[string]any{"err": v.Err.Error(), "phase": v.Phase.String()}),
		}
	case agent.PhaseChange:
		return sseFrame{
			Event: "phase_change",
			Data:  mustJSON(map[string]any{"phase": v.Phase.String(), "at": v.At.UTC().Format("2006-01-02T15:04:05Z")}),
		}
	case agent.LoopDone:
		return sseFrame{
			Event: "loop_done",
			Data:  mustJSON(map[string]any{"rounds": v.Rounds, "stop_reason": "no_tool_calls"}),
		}
	default:
		// 未知事件类型 —— 以原始 JSON 输出便于调试。
		return sseFrame{
			Event: "unknown",
			Data:  mustJSON(map[string]any{"type": fmt.Sprintf("%T", ev)}),
		}
	}
}

// msgsAsAny 将 []llm.Message 转换为 []any 以便 JSON 序列化。
func msgsAsAny(msgs []llm.Message) []any {
	out := make([]any, len(msgs))
	for i, m := range msgs {
		out[i] = m
	}
	return out
}

// secondsToDuration 将整数秒的配置值转换为 time.Duration。
// Server.Config.Timeout 字段为兼容 YAML 使用 int 秒；此辅助函数负责转换。
func secondsToDuration(s int) time.Duration {
	return time.Duration(s) * time.Second
}
