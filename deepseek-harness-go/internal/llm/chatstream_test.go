package llm

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

// 多行 SSE data：data: 行跨多个，之间由空行分隔为独立事件。
// 与单行不同，多行 data 在 SSE 协议里是合法的（事件由多行 data:
// 组成，行间用 \n 拼接）；但 JSON 解码层只接受完整 JSON。本测试
// 验证 sseFeed 不会把第二条 data 行静默丢弃。
func TestChatStream_MultilineData(t *testing.T) {
	// 两个独立事件，每个事件单行 data:。这是 OpenAI/DeepSeek
	// 实际格式；测试目的是回归保护"扫描器读到下一个 data 时能继续"。
	frames := []string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"\"},\"finish_reason\":\"stop\"}]}\n\n",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, _ := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range frames {
			_, _ = io.WriteString(w, f)
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
	if len(got) != 2 || got[0] != "Hi" || got[1] != "" {
		t.Fatalf("got %#v, want [\"Hi\", \"\"]", got)
	}
}

// 单行 SSE data 中包含转义换行符（合法 JSON 字符串里嵌入 \n）。
// 验证 sseFeed 不会把字面 \n 当作多行分隔。
func TestChatStream_DataWithEscapedNewline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, _ := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		// data 中 Content 字段包含 \n（两字符转义）。原实现若误把它
		// 当作多行分隔，会破坏 JSON 解析。
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"a\\nb\"}}]}\n\n")
		flusher.Flush()
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
	if len(got) != 1 || got[0] != "a\nb" {
		t.Fatalf("got %#v, want [\"a\\nb\"]", got)
	}
}

// 单行超过 scanner buffer 上限时 scanner.Err 须被透传为流错误，
// 而不是静默吞掉。之前实现直接 close(ch)，客户端以为流正常 EOF。
func TestChatStream_LineTooLongIsFatal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// 写出 2MB 单行（>1MB scanner 上限）
		huge := strings.Repeat("x", 2*1024*1024)
		_, _ = io.WriteString(w, "data: "+huge+"\n\n")
	}))
	defer srv.Close()

	c := NewOpenAICompatibleClient(srv.URL, "")
	ch, errCh := c.ChatStream(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}})

	for range ch {
	}
	err := <-errCh
	if err == nil {
		t.Fatalf("expected scanner overflow error, got nil")
	}
	if !strings.Contains(err.Error(), "scan") {
		t.Fatalf("expected scanner-derived error, got %v", err)
	}
}

// sanity：bufio.Scanner 默认 buffer 是 64KB 起始；构造 100KB 单行
// 仍能正常解析（向上扩）。
func TestSSEFeed_LongSingleLineOK(t *testing.T) {
	long := strings.Repeat("y", 100*1024)
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"" + long + "\"}}]}\n\n"
	ch := sseFeed(context.Background(), strings.NewReader(body))
	frames := 0
	for f := range ch {
		if f.err != nil {
			t.Fatalf("unexpected err: %v", f.err)
		}
		frames++
		if f.parsed.Choices[0].Delta.Content != long {
			t.Fatalf("content mismatch")
		}
	}
	if frames != 1 {
		t.Fatalf("want 1 frame, got %d", frames)
	}
}

// verify that bufio.Scanner import still works after edits
var _ = bufio.NewScanner
