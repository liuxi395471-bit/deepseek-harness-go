package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"deepseek-harness-go/internal/config"
)

func TestChat_SendsToolsAndDecodesToolCalls(t *testing.T) {
	var receivedReq ChatRequest
	var gotAuth atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Authorization"))
		if err := json.NewDecoder(r.Body).Decode(&receivedReq); err != nil {
			t.Errorf("server: decode request: %v", err)
		}
		resp := ChatResponse{
			ID:    "test-id",
			Model: "test-model",
			Choices: []Choice{{
				Index: 0, FinishReason: "tool_calls",
				Message: Message{
					Role: RoleAssistant,
					ToolCalls: []ToolCall{{
						ID: "call_1", Type: "function",
						Function: ToolCallFunc{
							Name:      "greet",
							Arguments: `{"name":"Ada"}`,
						},
					}},
				},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewOpenAICompatibleClient(srv.URL, "secret-key")
	temp := 0.5
	resp, err := c.Chat(context.Background(), ChatRequest{
		Model: "test-model",
		Messages: []Message{
			{Role: RoleSystem, Content: "you are dsh"},
			{Role: RoleUser, Content: "hi"},
		},
		Tools: []ToolSpec{{
			Type: "function",
			Function: ToolSpecFunc{
				Name:        "greet",
				Description: "say hi",
				Parameters: map[string]any{
					"type":       "object",
					"properties": map[string]any{"name": map[string]any{"type": "string"}},
					"required":   []string{"name"},
				},
			},
		}},
		Temperature: &temp,
		MaxTokens:   1024,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if receivedReq.Model != "test-model" {
		t.Errorf("Model = %q", receivedReq.Model)
	}
	if len(receivedReq.Tools) != 1 {
		t.Fatalf("Tools len = %d", len(receivedReq.Tools))
	}
	if receivedReq.Tools[0].Function.Name != "greet" {
		t.Errorf("Tool name = %q", receivedReq.Tools[0].Function.Name)
	}
	if receivedReq.Tools[0].Function.Parameters == nil {
		t.Errorf("Tool parameters missing")
	}
	if len(receivedReq.Messages) != 2 {
		t.Errorf("Messages len = %d", len(receivedReq.Messages))
	}
	if receivedReq.Temperature == nil || *receivedReq.Temperature != 0.5 {
		t.Errorf("Temperature not propagated: %v", receivedReq.Temperature)
	}
	if receivedReq.MaxTokens != 1024 {
		t.Errorf("MaxTokens = %d", receivedReq.MaxTokens)
	}

	if got := gotAuth.Load(); got != "Bearer secret-key" {
		t.Errorf("Authorization = %v, want Bearer secret-key", got)
	}

	if len(resp.Choices) != 1 {
		t.Fatalf("Choices len = %d", len(resp.Choices))
	}
	tc := resp.Choices[0].Message.ToolCalls
	if len(tc) != 1 {
		t.Fatalf("ToolCalls len = %d", len(tc))
	}
	if tc[0].Function.Name != "greet" {
		t.Errorf("ToolCall name = %q", tc[0].Function.Name)
	}
	if tc[0].Function.Arguments != `{"name":"Ada"}` {
		t.Errorf("ToolCall arguments = %q", tc[0].Function.Arguments)
	}
}

func TestChat_NoAuthHeaderWhenAPIKeyEmpty(t *testing.T) {
	var gotAuth atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer srv.Close()

	c := NewOpenAICompatibleClient(srv.URL, "")
	if _, err := c.Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got := gotAuth.Load(); got != "" {
		t.Errorf("Authorization = %q, want empty", got)
	}
}

func TestChat_401ReturnsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	c := NewOpenAICompatibleClient(srv.URL, "wrong")
	_, err := c.Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatalf("expected error on 401")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Status != 401 {
		t.Errorf("Status = %d, want 401", apiErr.Status)
	}
	if !strings.Contains(apiErr.Body, "bad key") {
		t.Errorf("Body = %q", apiErr.Body)
	}
}

func TestChat_StreamReturnsErrStreamNotSupported(t *testing.T) {
	c := NewOpenAICompatibleClient("http://x", "k")
	_, err := c.Chat(context.Background(), ChatRequest{Model: "m", Stream: true})
	if !errors.Is(err, ErrStreamNotSupported) {
		t.Errorf("err = %v, want ErrStreamNotSupported", err)
	}
}

func TestChat_ContextCanceledReturnsImmediately(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := NewOpenAICompatibleClient(srv.URL, "")
	c.HTTPClient.Timeout = 5 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 预先取消
	start := time.Now()
	_, err := c.Chat(ctx, ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("expected error")
	}
	if elapsed > 2*time.Second {
		t.Errorf("Chat did not return promptly after cancel: %v", elapsed)
	}
	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestChat_TrailingSlashTrimmed(t *testing.T) {
	var gotPath atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath.Store(r.URL.Path)
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer srv.Close()

	c := NewOpenAICompatibleClient(srv.URL+"/", "")
	if _, err := c.Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got := gotPath.Load(); got != "/chat/completions" {
		t.Errorf("path = %v, want /chat/completions", got)
	}
}

func TestChat_BodyDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "not-json-at-all")
	}))
	defer srv.Close()

	c := NewOpenAICompatibleClient(srv.URL, "")
	_, err := c.Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatalf("expected error on bad body")
	}
	if !strings.Contains(err.Error(), "decode response") {
		t.Errorf("err = %v, want decode failure", err)
	}
}

func TestNewClient_RejectsBadScheme(t *testing.T) {
	cases := []string{"", "ftp://x", "ws://x"}
	for _, base := range cases {
		_, err := NewClient(config.LLMConfig{BaseURL: base, Model: "m"})
		if err == nil {
			t.Errorf("NewClient(%q) error = nil, want non-nil", base)
		}
	}
}

func TestNewClient_AcceptsHTTPAndHTTPS(t *testing.T) {
	cases := []string{"http://x", "https://x"}
	for _, base := range cases {
		c, err := NewClient(config.LLMConfig{BaseURL: base, Model: "m"})
		if err != nil {
			t.Errorf("NewClient(%q): %v", base, err)
			continue
		}
		if c == nil {
			t.Errorf("NewClient(%q) returned nil client", base)
		}
	}
}

func TestNewClient_DefaultsTimeoutWhenZero(t *testing.T) {
	c, err := NewClient(config.LLMConfig{BaseURL: "http://x", Model: "m"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	oc, ok := c.(*OpenAICompatibleClient)
	if !ok {
		t.Fatalf("client type = %T", c)
	}
	if oc.HTTPClient.Timeout != 120*time.Second {
		t.Errorf("Timeout = %v, want 120s", oc.HTTPClient.Timeout)
	}
}

func TestAPIError_ErrorString(t *testing.T) {
	e := &APIError{Status: 429, Body: `{"error":"rate limit"}`, Method: "POST", URL: "http://x/v1/chat/completions"}
	got := e.Error()
	for _, want := range []string{"429", "POST", "rate limit"} {
		if !strings.Contains(got, want) {
			t.Errorf("Error() = %q, missing %q", got, want)
		}
	}
}
