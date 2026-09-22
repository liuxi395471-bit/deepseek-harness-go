package llm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// 401 为致命错误（不重试）；errCh 收到 *APIError。
func TestChatStream_401Fatal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	c := NewOpenAICompatibleClient(srv.URL, "wrong")
	ch, errCh := c.ChatStream(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	for range ch {
	}
	err := <-errCh
	if err == nil {
		t.Fatalf("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("got %T, want *APIError", err)
	}
	if apiErr.Status != 401 {
		t.Errorf("Status = %d, want 401", apiErr.Status)
	}
}

// 两次请求均返回 429 → 重试后仍为致命错误。
func TestChatStream_429Exhausted(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":"still rate limited"}`))
	}))
	defer srv.Close()

	c := NewOpenAICompatibleClient(srv.URL, "")
	ch, errCh := c.ChatStream(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	for range ch {
	}
	err := <-errCh
	if err == nil {
		t.Fatalf("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 429 {
		t.Fatalf("got %v, want 429", err)
	}
	if hits != 2 {
		t.Errorf("hits = %d, want 2", hits)
	}
}

// 流中途取消 ctx：进行中的请求被中止；不返回错误。
func TestChatStream_ContextCanceledMidStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"content":"a"}}]}`+"\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		// 阻塞直到客户端取消。
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := NewOpenAICompatibleClient(srv.URL, "")
	c.HTTPClient.Timeout = 5 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, errCh := c.ChatStream(ctx, ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}})

	// 至少消费一个 chunk，然后取消。
	for chunk := range ch {
		if chunk.Text == "a" {
			cancel()
			break
		}
	}
	// errCh 可能收到也可能收不到值；按设计两者皆可接受。
	<-errCh
}

// 逐帧增量语义：每帧只携带自身的增量。
// 重组逻辑（stream 包中）负责拼装。
func TestChatStream_ToolCallDeltaFrames(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		frames := []string{
			`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"greet","arguments":"{\"name\""}}]}}]}`,
			`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\":\"Ada\"}"}}]}}]}`,
			`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
			`data: [DONE]`,
		}
		for _, f := range frames {
			_, _ = io.WriteString(w, f+"\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	c := NewOpenAICompatibleClient(srv.URL, "")
	ch, errCh := c.ChatStream(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}})

	var got []StreamChunk
	for chunk := range ch {
		got = append(got, chunk)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("errCh: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d chunks, want 3: %#v", len(got), got)
	}
	// 帧 1：tool_calls 携带 id+名称+参数前缀。
	f1 := got[0]
	if len(f1.ToolCalls) != 1 {
		t.Fatalf("f1 ToolCalls = %d, want 1", len(f1.ToolCalls))
	}
	if f1.ToolCalls[0].ID != "call_1" {
		t.Errorf("f1 id = %q", f1.ToolCalls[0].ID)
	}
	if f1.ToolCalls[0].Function.Name != "greet" {
		t.Errorf("f1 name = %q", f1.ToolCalls[0].Function.Name)
	}
	// 帧 2：tool_calls 携带参数续接片段。
	f2 := got[1]
	if len(f2.ToolCalls) != 1 {
		t.Fatalf("f2 ToolCalls = %d, want 1", len(f2.ToolCalls))
	}
	if f2.ToolCalls[0].Function.Arguments != `":"Ada"}` {
		t.Errorf("f2 args = %q", f2.ToolCalls[0].Function.Arguments)
	}
	// 帧 3：仅含 finish_reason。
	f3 := got[2]
	if f3.Finish != "tool_calls" {
		t.Errorf("f3 Finish = %q", f3.Finish)
	}
	if len(f3.ToolCalls) != 0 {
		t.Errorf("f3 should carry no tool_calls: %d", len(f3.ToolCalls))
	}
}
func TestChatStream_DeepSeekStyleStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		frames := []string{
			`data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"Deep"}}]}`,
			`data: {"choices":[{"index":0,"delta":{"content":"Seek"}}]}`,
			`data: {"choices":[{"index":0,"delta":{"content":"!"},"finish_reason":"stop"}]}`,
			`data: [DONE]`,
		}
		for _, f := range frames {
			_, _ = io.WriteString(w, f+"\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	c := NewOpenAICompatibleClient(srv.URL, "")
	ch, errCh := c.ChatStream(context.Background(), ChatRequest{Model: "deepseek-chat", Messages: []Message{{Role: RoleUser, Content: "hi"}}})

	var text string
	for chunk := range ch {
		text += chunk.Text
	}
	if err := <-errCh; err != nil {
		t.Fatalf("errCh: %v", err)
	}
	if text != "DeepSeek!" {
		t.Errorf("text = %q, want %q", text, "DeepSeek!")
	}
}
func TestChatStream_Retries429ThenSucceeds(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"error":"rate limit"}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: [DONE]`+"\n\n")
	}))
	defer srv.Close()

	c := NewOpenAICompatibleClient(srv.URL, "")
	ch, errCh := c.ChatStream(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}})

	var got []string
	for chunk := range ch {
		got = append(got, chunk.Text)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("errCh: %v", err)
	}
	if hits != 2 {
		t.Errorf("hits = %d, want 2", hits)
	}
	if len(got) != 1 || got[0] != "ok" {
		t.Errorf("got %#v, want [ok]", got)
	}
}
func TestChatStream_OpenAITextHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		frames := []string{
			`data: {"choices":[{"index":0,"delta":{"content":"Hello"}}]}`,
			`data: {"choices":[{"index":0,"delta":{"content":", "}}]}`,
			`data: {"choices":[{"index":0,"delta":{"content":"world"},"finish_reason":"stop"}]}`,
			`data: [DONE]`,
		}
		for _, f := range frames {
			_, _ = io.WriteString(w, f+"\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	c := NewOpenAICompatibleClient(srv.URL, "")
	ch, errCh := c.ChatStream(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}})

	var got []string
	for chunk := range ch {
		got = append(got, chunk.Text)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("errCh: %v", err)
	}
	want := []string{"Hello", ", ", "world"}
	if len(got) != len(want) {
		t.Fatalf("got %d chunks, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chunk %d = %q, want %q", i, got[i], want[i])
		}
	}
}
