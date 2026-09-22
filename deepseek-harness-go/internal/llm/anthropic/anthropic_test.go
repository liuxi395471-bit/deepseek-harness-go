package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"deepseek-harness-go/internal/llm"
)

// TestAnthropicClient_Chat — 基本的非流式路径。
func TestAnthropicClient_Chat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-key" {
			http.Error(w, "bad key", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":   "msg1",
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{"type": "text", "text": "hello from claude"},
			},
			"stop_reason": "end_turn",
			"usage":       map[string]int{"input_tokens": 10, "output_tokens": 5},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key", "claude-3-5-sonnet")
	resp, err := c.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("no choices")
	}
	if resp.Choices[0].Message.Content != "hello from claude" {
		t.Errorf("content = %q", resp.Choices[0].Message.Content)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 15 {
		t.Errorf("Usage = %+v, want TotalTokens=15", resp.Usage)
	}
}

// TestAnthropicClient_ChatStream_HappyPath — 三个文本增量 +
// message_delta + message_stop 产生一段干净的 chunk 序列，
// 并在最后设置 Finish。
func TestAnthropicClient_ChatStream_HappyPath(t *testing.T) {
	body := "" +
		`event: message_start` + "\n" +
		`data: {"type":"message_start","message":{"id":"m1","usage":{"input_tokens":10}}}` + "\n\n" +
		`event: content_block_start` + "\n" +
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n" +
		`event: ping` + "\n\n" +
		`event: content_block_delta` + "\n" +
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}` + "\n\n" +
		`event: content_block_delta` + "\n" +
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":", "}}` + "\n\n" +
		`event: content_block_delta` + "\n" +
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Claude"}}` + "\n\n" +
		`event: content_block_stop` + "\n" +
		`data: {"type":"content_block_stop","index":0}` + "\n\n" +
		`event: message_delta` + "\n" +
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}` + "\n\n" +
		`event: message_stop` + "\n" +
		`data: {"type":"message_stop"}` + "\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/messages") {
			http.Error(w, "bad path", http.StatusNotFound)
			return
		}
		if r.Header.Get("x-api-key") != "k" {
			http.Error(w, "no key", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for _, line := range strings.Split(body, "\n") {
			_, _ = w.Write([]byte(line + "\n"))
		}
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", "claude-3-5-sonnet")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := llm.ChatRequest{
		Stream:   true,
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	}
	chunkCh, errCh := c.ChatStream(ctx, req)

	var (
		got      []string
		finalFin string
	)
	for chunk := range chunkCh {
		if chunk.Text != "" {
			got = append(got, chunk.Text)
		}
		if chunk.Finish != "" {
			finalFin = chunk.Finish
		}
	}
	if e := <-errCh; e != nil {
		t.Fatalf("errCh: %v", e)
	}
	want := "Hello, Claude"
	if strings.Join(got, "") != want {
		t.Errorf("chunks = %q, want %q", got, want)
	}
	if finalFin != "end_turn" {
		t.Errorf("finish = %q, want end_turn", finalFin)
	}
}

// TestAnthropicClient_ChatStream_ErrorEvent — 流中的 `error` 事件
// 通过 errCh 上报，并立即关闭 chunkCh。
func TestAnthropicClient_ChatStream_ErrorEvent(t *testing.T) {
	body := "" +
		`event: message_start` + "\n" +
		`data: {"type":"message_start","message":{"id":"m1","usage":{"input_tokens":2}}}` + "\n\n" +
		`event: error` + "\n" +
		`data: {"type":"error","error":{"type":"rate_limit","message":"too many"}}` + "\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", "m")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	chunkCh, errCh := c.ChatStream(ctx, llm.ChatRequest{
		Stream:   true,
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "x"}},
	})
	// 消费完 chunk 以便 channel 能关闭。
	for range chunkCh {
	}
	err := <-errCh
	if err == nil {
		t.Fatal("expected error from stream")
	}
	if !strings.Contains(err.Error(), "too many") {
		t.Errorf("err = %v, want message about rate limit", err)
	}
}

// TestAnthropicClient_ChatStream_HTTPError — 非 200 响应通过 errCh 上报。
func TestAnthropicClient_ChatStream_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"type":"error"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "k", "m")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	chunkCh, errCh := c.ChatStream(ctx, llm.ChatRequest{Stream: true})
	for range chunkCh {
	}
	err := <-errCh
	if err == nil {
		t.Fatal("expected HTTP error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("err = %v, want HTTP 500 mentioned", err)
	}
}

// TestAnthropicClient_ChatStream_RequiresStreamFlag — 未设置
// req.Stream=true 就调用属于编程错误，必须显式暴露。
func TestAnthropicClient_ChatStream_RequiresStreamFlag(t *testing.T) {
	c := NewClient("http://x", "k", "m")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	chunkCh, errCh := c.ChatStream(ctx, llm.ChatRequest{Stream: false})
	for range chunkCh {
	}
	err := <-errCh
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "Stream=true") {
		t.Errorf("err = %v, want msg about Stream=true", err)
	}
}

// TestFeed_ParsesBasicSequence — 使用固定输入直接驱动解析器；
// 覆盖我们关心的所有事件类型。
func TestFeed_ParsesBasicSequence(t *testing.T) {
	in := strings.NewReader("" +
		`event: message_start` + "\n" +
		`data: {"type":"message_start","message":{"id":"abc","usage":{"input_tokens":7}}}` + "\n\n" +
		`event: content_block_delta` + "\n" +
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}` + "\n\n" +
		`event: message_delta` + "\n" +
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}` + "\n\n" +
		`event: message_stop` + "\n" +
		`data: {"type":"message_stop"}` + "\n\n")
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	var got []Event
	for ev := range Feed(ctx, in) {
		got = append(got, ev)
	}
	if len(got) < 4 {
		t.Fatalf("got %d events, want >=4", len(got))
	}
	if got[0].Type != evMessageStart || got[0].MessageID != "abc" || got[0].InputTok != 7 {
		t.Errorf("message_start = %+v", got[0])
	}
	if got[1].Type != evContentBlockDelta || got[1].DeltaText != "hi" {
		t.Errorf("content_block_delta = %+v", got[1])
	}
	if got[2].Type != evMessageDelta || got[2].StopReason != "end_turn" || got[2].OutputTok != 1 {
		t.Errorf("message_delta = %+v", got[2])
	}
	if got[3].Type != evMessageStop {
		t.Errorf("last = %+v, want message_stop", got[3])
	}
}
