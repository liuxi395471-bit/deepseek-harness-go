package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"deepseek-harness-go/internal/llm"
)

// mockOllama 是 /api/chat 的 ndjson fixture 服务器。
func mockOllama(t *testing.T, lines []string, capture *chatRequest) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			http.Error(w, "not found", 404)
			return
		}
		var req chatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if capture != nil {
			*capture = req
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		for _, l := range lines {
			_, _ = w.Write([]byte(l + "\n"))
		}
	}))
}

func TestChatRoundTrip(t *testing.T) {
	var captured chatRequest
	srv := mockOllama(t, []string{
		`{"model":"llama3","message":{"role":"assistant","content":"hello from ollama"},"done":true,"done_reason":"stop","prompt_eval_count":11,"eval_count":7}`,
	}, &captured)
	defer srv.Close()

	c := NewClient(srv.URL, "llama3")
	resp, err := c.Chat(context.Background(), llm.ChatRequest{
		Model: "llama3",
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "sys"},
			{Role: llm.RoleUser, Content: "hi"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "hello from ollama" {
		t.Fatalf("resp = %+v", resp)
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Fatalf("finish = %s", resp.Choices[0].FinishReason)
	}
	if resp.Usage == nil || resp.Usage.PromptTokens != 11 || resp.Usage.CompletionTokens != 7 {
		t.Fatalf("usage = %+v", resp.Usage)
	}
	// 请求映射校验：stream=false、messages 数量一致。
	if captured.Stream {
		t.Fatal("Chat must send stream=false")
	}
	if len(captured.Messages) != 2 || captured.Messages[0].Role != "system" {
		t.Fatalf("messages = %+v", captured.Messages)
	}
}

func TestChatToolCalls(t *testing.T) {
	srv := mockOllama(t, []string{
		`{"model":"m","message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"shell","arguments":{"cmd":"ls"}}}]},"done":true}`,
	}, nil)
	defer srv.Close()

	c := NewClient(srv.URL, "m")
	resp, err := c.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "run ls"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	tcs := resp.Choices[0].Message.ToolCalls
	if len(tcs) != 1 || tcs[0].Function.Name != "shell" {
		t.Fatalf("tool calls = %+v", tcs)
	}
	// arguments 必须是 JSON 字符串（统一协议）。
	if tcs[0].Function.Arguments != `{"cmd":"ls"}` {
		t.Fatalf("args = %q", tcs[0].Function.Arguments)
	}
}

func TestChatStreamDeltas(t *testing.T) {
	srv := mockOllama(t, []string{
		`{"model":"m","message":{"role":"assistant","content":"hel"}}`,
		`{"model":"m","message":{"role":"assistant","content":"lo"}}`,
		`{"model":"m","message":{"role":"assistant","content":" world"},"done":true,"done_reason":"stop","prompt_eval_count":5,"eval_count":3}`,
	}, nil)
	defer srv.Close()

	c := NewClient(srv.URL, "m")
	ch, errCh := c.ChatStream(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	var text strings.Builder
	var finish string
	var usage *llm.Usage
	for chunk := range ch {
		text.WriteString(chunk.Text)
		if chunk.Finish != "" {
			finish = chunk.Finish
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
	}
	if err := <-errCh; err != nil {
		t.Fatalf("stream err: %v", err)
	}
	if text.String() != "hello world" {
		t.Fatalf("text = %q", text.String())
	}
	if finish != "stop" {
		t.Fatalf("finish = %q", finish)
	}
	if usage == nil || usage.PromptTokens != 5 || usage.CompletionTokens != 3 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestStreamSendsToolsAndStreamFlag(t *testing.T) {
	var captured chatRequest
	srv := mockOllama(t, []string{
		`{"model":"m","message":{"role":"assistant","content":"ok"},"done":true}`,
	}, &captured)
	defer srv.Close()

	c := NewClient(srv.URL, "m")
	ch, errCh := c.ChatStream(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Tools: []llm.ToolSpec{{
			Type: "function",
			Function: llm.ToolSpecFunc{
				Name:        "echo",
				Description: "d",
				Parameters:  map[string]any{"type": "object"},
			},
		}},
	})
	for range ch {
	}
	if err := <-errCh; err != nil {
		t.Fatalf("stream err: %v", err)
	}
	if !captured.Stream {
		t.Fatal("ChatStream must send stream=true")
	}
	if len(captured.Tools) != 1 || captured.Tools[0].Function.Name != "echo" {
		t.Fatalf("tools = %+v", captured.Tools)
	}
}

func TestToolMessageCarriesToolName(t *testing.T) {
	var captured chatRequest
	srv := mockOllama(t, []string{`{"message":{"content":"x"},"done":true}`}, &captured)
	defer srv.Close()

	c := NewClient(srv.URL, "m")
	_, err := c.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "u"},
			{Role: llm.RoleTool, Content: "result", Name: "shell", ToolCallID: "c1"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if captured.Messages[1].ToolName != "shell" {
		t.Fatalf("tool_name = %q", captured.Messages[1].ToolName)
	}
}

func TestAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "model not found", 404)
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "m")
	_, err := c.Chat(context.Background(), llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: "x"}}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("err = %v", err)
	}
}

func TestDefaultBaseURL(t *testing.T) {
	c := NewClient("", "m")
	if c.BaseURL != "http://127.0.0.1:11434" {
		t.Fatalf("BaseURL = %q", c.BaseURL)
	}
}

func TestChatStreamEmptyStream(t *testing.T) {
	srv := mockOllama(t, []string{`{"done":true}`}, nil)
	defer srv.Close()
	c := NewClient(srv.URL, "m")
	ch, errCh := c.ChatStream(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	for range ch {
	}
	// done 帧无 content：应发 finish 帧而不是报错。
	if err := <-errCh; err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestChatStreamDoneReasonLength(t *testing.T) {
	srv := mockOllama(t, []string{
		`{"message":{"content":"partial"},"done":true,"done_reason":"length","prompt_eval_count":2,"eval_count":1}`,
	}, nil)
	defer srv.Close()
	c := NewClient(srv.URL, "m")
	ch, errCh := c.ChatStream(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	var finish string
	for chunk := range ch {
		if chunk.Finish != "" {
			finish = chunk.Finish
		}
	}
	if err := <-errCh; err != nil {
		t.Fatalf("err: %v", err)
	}
	if finish != "length" {
		t.Fatalf("finish = %q", finish)
	}
}

func TestChatStreamDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not-json\n"))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "m")
	ch, errCh := c.ChatStream(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	for range ch {
	}
	if err := <-errCh; err == nil {
		t.Fatal("expected decode error")
	}
}

func TestAssistantToolCallArgumentsNullSafe(t *testing.T) {
	// assistant 历史 message 的 tool_calls arguments 非 JSON 对象时不
	// 应 panic（toChatMessages 的 Unmarshal 失败路径）。
	msgs := toChatMessages([]llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
			ID: "x", Function: llm.ToolCallFunc{Name: "t", Arguments: "not-an-object"},
		}}},
	})
	if len(msgs) != 1 || len(msgs[0].ToolCalls) != 1 {
		t.Fatalf("msgs = %+v", msgs)
	}
	// 空 arguments → 空 map（不 panic）。
	msgs2 := toChatMessages([]llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "y", Function: llm.ToolCallFunc{Name: "t"}}}},
	})
	if len(msgs2[0].ToolCalls[0].Function.Arguments) != 0 {
		t.Fatalf("args = %v", msgs2[0].ToolCalls[0].Function.Arguments)
	}
}
