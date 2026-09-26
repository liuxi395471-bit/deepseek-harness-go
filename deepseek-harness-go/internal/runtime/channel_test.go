package runtime

import (
	"errors"
	"testing"
	"time"

	"deepseek-harness-go/internal/config"
)

func TestNewRegistry_EmptyConfigs(t *testing.T) {
	// 空 configs + 零值 cfg：返回空 Registry
	r, err := NewRegistry(nil, config.LLMConfig{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := r.Codes(); len(got) != 0 {
		t.Fatalf("空 configs + 零值 cfg 应无渠道, got %v", got)
	}
}

func TestNewRegistry_FallbackFromLLMConfig(t *testing.T) {
	cfg := config.LLMConfig{
		Provider: "openai",
		BaseURL:  "http://127.0.0.1:8777/v1",
		APIKey:   "test-key",
		Model:    "glm-5.3-flash",
		Timeout:  30 * time.Second,
	}
	r, err := NewRegistry(nil, cfg)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	codes := r.Codes()
	if len(codes) != 1 || codes[0] != "default" {
		t.Fatalf("codes = %v, want [default]", codes)
	}
	if _, err := r.Resolve("default"); err != nil {
		t.Fatalf("Resolve default: %v", err)
	}
}

func TestNewRegistry_MultipleChannels(t *testing.T) {
	configs := []ChannelConfig{
		{
			Code:     "primary",
			Provider: "openai",
			BaseURL:  "http://127.0.0.1:8777/v1",
			Model:    "glm-5.3-flash",
		},
		{
			Code:     "fallback",
			Provider: "ollama",
			BaseURL:  "http://127.0.0.1:11434",
			Model:    "qwen2.5",
		},
	}
	r, err := NewRegistry(configs, config.LLMConfig{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	codes := r.Codes()
	if len(codes) != 2 {
		t.Fatalf("codes len = %d, want 2", len(codes))
	}
	// 验证顺序
	if codes[0] != "primary" || codes[1] != "fallback" {
		t.Fatalf("codes 顺序错误: %v", codes)
	}
	for _, c := range codes {
		if _, err := r.Resolve(c); err != nil {
			t.Fatalf("Resolve %s: %v", c, err)
		}
	}
}

func TestNewRegistry_DuplicateCodeRejected(t *testing.T) {
	configs := []ChannelConfig{
		{Code: "dup", Provider: "openai", Model: "m1"},
		{Code: "dup", Provider: "openai", Model: "m2"},
	}
	_, err := NewRegistry(configs, config.LLMConfig{})
	if err == nil {
		t.Fatalf("duplicate code 应返回错误")
	}
}

func TestNewRegistry_MissingFields(t *testing.T) {
	cases := []struct {
		name string
		ch   ChannelConfig
	}{
		{"empty code", ChannelConfig{Provider: "openai", Model: "m"}},
		{"empty provider", ChannelConfig{Code: "x", Model: "m"}},
		{"empty model", ChannelConfig{Code: "x", Provider: "openai"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewRegistry([]ChannelConfig{tc.ch}, config.LLMConfig{})
			if err == nil {
				t.Fatalf("%s 应返回错误", tc.name)
			}
		})
	}
}

func TestRegistry_ResolveUnknown(t *testing.T) {
	r, _ := NewRegistry(nil, config.LLMConfig{
		Provider: "openai",
		BaseURL:  "http://127.0.0.1:8777/v1",
		Model:    "m",
	})
	_, err := r.Resolve("nonexistent")
	if err == nil {
		t.Fatalf("Resolve 未知 code 应返回错误")
	}
	if !errors.Is(err, ErrUnknownChannel) {
		t.Fatalf("err = %v, want ErrUnknownChannel", err)
	}
}

func TestRegistry_ResolveModel(t *testing.T) {
	r, _ := NewRegistry([]ChannelConfig{
		{Code: "fast", Provider: "openai", BaseURL: "http://127.0.0.1:8777/v1", Model: "gpt-4o-mini"},
		{Code: "slow", Provider: "openai", BaseURL: "http://127.0.0.1:8777/v1", Model: "gpt-4o"},
	}, config.LLMConfig{})

	if got, _ := r.ResolveModel("fast"); got != "gpt-4o-mini" {
		t.Fatalf("ResolveModel fast = %q", got)
	}
	if got, _ := r.ResolveModel("slow"); got != "gpt-4o" {
		t.Fatalf("ResolveModel slow = %q", got)
	}
	_, err := r.ResolveModel("unknown")
	if !errors.Is(err, ErrUnknownChannel) {
		t.Fatalf("err = %v, want ErrUnknownChannel", err)
	}
}

func TestRegistry_ConcurrentResolve(t *testing.T) {
	r, _ := NewRegistry([]ChannelConfig{
		{Code: "c", Provider: "openai", BaseURL: "http://127.0.0.1:8777/v1", Model: "m"},
	}, config.LLMConfig{})

	const N = 32
	done := make(chan struct{}, N)
	for i := 0; i < N; i++ {
		go func() {
			_, err := r.Resolve("c")
			if err != nil {
				t.Errorf("Resolve: %v", err)
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < N; i++ {
		<-done
	}
}
