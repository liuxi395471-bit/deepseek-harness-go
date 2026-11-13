// Package main 是一个用于本地联调的 OpenAI 兼容 mock LLM 服务。
//
// 它实现 /v1/chat/completions（非流式与流式 SSE），返回一段固定文本，
// 并在被要求时返回一个 greet 工具调用，用于验证前端的工具卡片渲染。
//
// 仅用于开发联调，不属于 dsh 正式代码路径。
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
)

type chatReq struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	Stream bool `json:"stream"`
	Tools  []struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	} `json:"tools"`
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8777", "listen address")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", handleChat)
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"object":"list","data":[{"id":"mock-model","object":"model"}]}`)
	})

	log.Printf("[mock-llm] listening on %s", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}

// lastUser 返回最后一条用户消息内容。
func lastUser(req chatReq) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			return req.Messages[i].Content
		}
	}
	return ""
}

// wantsTool 判断本次请求是否应当返回工具调用（提示里含 "TOOL:" 即触发）。
func wantsTool(prompt string) bool {
	return strings.Contains(strings.ToUpper(prompt), "TOOL:")
}

// hasToolResult 判断请求历史里是否已经含有工具结果（role=tool）。
//
// 用于模拟真实模型的行为：拿到工具结果后就不再重复调用工具，
// 而是给出最终回答。否则 agent 会陷入"调用工具 → 再调用工具"的死循环。
func hasToolResult(req chatReq) bool {
	for _, m := range req.Messages {
		if m.Role == "tool" {
			return true
		}
	}
	return false
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	var req chatReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	prompt := lastUser(req)

	// 需要工具调用时，返回一个 greet 工具调用。
	// 只有在"还没拿到过工具结果"时才返回工具调用 —— 真实模型在拿到
	// 工具结果后就会给出最终回答，不会无限重复调用同一个工具。
	if wantsTool(prompt) && !hasToolResult(req) {
		if req.Stream {
			respondToolStream(w)
		} else {
			respondTool(w, req)
		}
		return
	}

	if req.Stream {
		respondStream(w, prompt)
		return
	}
	respondNonStream(w, prompt)
}

// respondNonStream 返回一次性的完整响应。
func respondNonStream(w http.ResponseWriter, prompt string) {
	w.Header().Set("Content-Type", "application/json")
	body := map[string]any{
		"id":      "mock-chatcmpl-1",
		"object":  "chat.completion",
		"created": 1700000000,
		"model":   "mock-model",
		"choices": []any{
			map[string]any{
				"index":         0,
				"finish_reason": "stop",
				"message": map[string]any{
					"role":    "assistant",
					"content": "你好！我是 mock 模型。\n\n你发的消息是：**" + prompt + "**\n\n```go\nfmt.Println(\"hello\")\n```",
				},
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     11,
			"completion_tokens": 22,
			"total_tokens":      33,
		},
	}
	_ = json.NewEncoder(w).Encode(body)
}

// respondTool 返回一个 greet 工具调用。
func respondTool(w http.ResponseWriter, req chatReq) {
	w.Header().Set("Content-Type", "application/json")
	body := map[string]any{
		"id":      "mock-chatcmpl-2",
		"object":  "chat.completion",
		"created": 1700000000,
		"model":   "mock-model",
		"choices": []any{
			map[string]any{
				"index":         0,
				"finish_reason": "tool_calls",
				"message": map[string]any{
					"role":    "assistant",
					"content": "好的，我先调用 greet 工具。",
					"tool_calls": []any{
						map[string]any{
							"id":   "call_mock_1",
							"type": "function",
							"function": map[string]any{
								"name":      "greet",
								"arguments": `{"name":"前端联调"}`,
							},
						},
					},
				},
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     5,
			"completion_tokens": 7,
			"total_tokens":      12,
		},
	}
	_ = json.NewEncoder(w).Encode(body)
}

// respondStream 以 SSE 形式分块返回文本。
func respondStream(w http.ResponseWriter, prompt string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no flusher", http.StatusInternalServerError)
		return
	}

	parts := []string{
		"你好，我是 ",
		"**流式** ",
		"mock 模型。",
		"\n\n你发的消息是：",
		"\n\n```js\nconsole.log('hi')\n```",
	}

	for _, p := range parts {
		chunk := map[string]any{
			"id":      "mock-stream-1",
			"object":  "chat.completion.chunk",
			"created": 1700000000,
			"model":   "mock-model",
			"choices": []any{
				map[string]any{
					"index": 0,
					"delta": map[string]any{"content": p},
				},
			},
		}
		b, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}

	// 结束帧
	final := map[string]any{
		"id":      "mock-stream-1",
		"object":  "chat.completion.chunk",
		"created": 1700000000,
		"model":   "mock-model",
		"choices": []any{
			map[string]any{
				"index":         0,
				"delta":         map[string]any{},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     9,
			"completion_tokens": 14,
			"total_tokens":      23,
		},
	}
	b, _ := json.Marshal(final)
	fmt.Fprintf(w, "data: %s\n\n", b)
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// 保留 bufio 引用（避免 import 未使用告警，mock 预留流式缓冲读取）。
var _ = bufio.NewReader
