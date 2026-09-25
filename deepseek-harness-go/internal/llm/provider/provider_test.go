package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"deepseek-harness-go/internal/config"
	"deepseek-harness-go/internal/llm"
)

func TestRouteOpenAI(t *testing.T) {
	c, err := NewClient(config.LLMConfig{Provider: "openai", BaseURL: "http://127.0.0.1:1/v1", Model: "m"})
	if err != nil {
		t.Fatalf("openai: %v", err)
	}
	if _, ok := c.(*llm.OpenAICompatibleClient); !ok {
		t.Fatalf("want OpenAICompatibleClient, got %T", c)
	}
	// 空 provider 默认 openai。
	c, err = NewClient(config.LLMConfig{BaseURL: "http://127.0.0.1:1/v1", Model: "m"})
	if err != nil {
		t.Fatalf("default: %v", err)
	}
	if _, ok := c.(*llm.OpenAICompatibleClient); !ok {
		t.Fatalf("default should be openai, got %T", c)
	}
}

func TestRouteOpenAIBadURL(t *testing.T) {
	if _, err := NewClient(config.LLMConfig{Provider: "openai", BaseURL: "ftp://x", Model: "m"}); err == nil {
		t.Fatal("expected scheme error")
	}
}

func TestRouteUnsupported(t *testing.T) {
	_, err := NewClient(config.LLMConfig{Provider: "claude-3", BaseURL: "http://x", Model: "m"})
	if err == nil {
		t.Fatal("expected unsupported provider error")
	}
	for _, p := range SupportedProviders {
		if !strings.Contains(err.Error(), p) {
			t.Fatalf("error must list supported providers: %v", err)
		}
	}
}

func TestRouteOllama(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"via provider"},"done":true}` + "\n"))
	}))
	defer srv.Close()
	c, err := NewClient(config.LLMConfig{Provider: "ollama", BaseURL: srv.URL, Model: "llama3"})
	if err != nil {
		t.Fatalf("ollama route: %v", err)
	}
	resp, err := c.Chat(context.Background(), llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Choices[0].Message.Content != "via provider" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestRouteGemini(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"gemini via provider"}]},"finishReason":"STOP"}]}`))
		_ = r
	}))
	defer srv.Close()
	c, err := NewClient(config.LLMConfig{Provider: "gemini", BaseURL: srv.URL, APIKey: "k", Model: "gemini-1.5-flash"})
	if err != nil {
		t.Fatalf("gemini route: %v", err)
	}
	resp, err := c.Chat(context.Background(), llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Choices[0].Message.Content != "gemini via provider" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestRouteAnthropic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// anthropic 包的响应格式。
		_, _ = w.Write([]byte(`{"id":"1","type":"message","role":"assistant","content":[{"type":"text","text":"via anthropic"}],"stop_reason":"end_turn"}`))
	}))
	defer srv.Close()
	c, err := NewClient(config.LLMConfig{Provider: "anthropic", BaseURL: srv.URL, APIKey: "k", Model: "claude-3-5-sonnet"})
	if err != nil {
		t.Fatalf("anthropic route: %v", err)
	}
	resp, err := c.Chat(context.Background(), llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if !strings.Contains(resp.Choices[0].Message.Content, "via anthropic") {
		t.Fatalf("resp = %+v", resp)
	}
	_ = fmt.Sprint()
	_ = json.Marshal
}
