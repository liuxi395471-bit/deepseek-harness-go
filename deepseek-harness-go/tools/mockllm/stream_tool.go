package main

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// respondToolStream 以 SSE 形式返回一个 greet 工具调用。
//
// 客户端（agent 层）在流式模式下会累积 tool_calls 分片，
// 因此这里按 OpenAI 的协议分两帧发送：第一帧给出 id/name，
// 第二帧补齐 arguments。
func respondToolStream(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no flusher", http.StatusInternalServerError)
		return
	}

	// 第一帧：文本 + 工具调用的 id 与 name。
	frame1 := map[string]any{
		"id":      "mock-stream-2",
		"object":  "chat.completion.chunk",
		"created": 1700000000,
		"model":   "mock-model",
		"choices": []any{
			map[string]any{
				"index": 0,
				"delta": map[string]any{
					"content": "好的，我先调用 greet 工具。",
					"tool_calls": []any{
						map[string]any{
							"index":    0,
							"id":       "call_mock_1",
							"type":     "function",
							"function": map[string]any{"name": "greet", "arguments": ""},
						},
					},
				},
			},
		},
	}
	writeChunk(w, flusher, frame1)

	// 第二帧：补齐 arguments。
	frame2 := map[string]any{
		"id":      "mock-stream-2",
		"object":  "chat.completion.chunk",
		"created": 1700000000,
		"model":   "mock-model",
		"choices": []any{
			map[string]any{
				"index": 0,
				"delta": map[string]any{
					"tool_calls": []any{
						map[string]any{
							"index":    0,
							"id":       "call_mock_1",
							"type":     "function",
							"function": map[string]any{"name": "greet", "arguments": "{\"name\":\"前端联调\"}"},
						},
					},
				},
			},
		},
	}
	writeChunk(w, flusher, frame2)

	// 终止帧。
	frame3 := map[string]any{
		"id":      "mock-stream-2",
		"object":  "chat.completion.chunk",
		"created": 1700000000,
		"model":   "mock-model",
		"choices": []any{
			map[string]any{
				"index":         0,
				"delta":         map[string]any{},
				"finish_reason": "tool_calls",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     13,
			"completion_tokens": 17,
			"total_tokens":      30,
		},
	}
	writeChunk(w, flusher, frame3)

	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// writeChunk 写出一个 SSE data 帧。
func writeChunk(w http.ResponseWriter, flusher http.Flusher, payload map[string]any) {
	b, _ := json.Marshal(payload)
	fmt.Fprintf(w, "data: %s\n\n", b)
	flusher.Flush()
}
