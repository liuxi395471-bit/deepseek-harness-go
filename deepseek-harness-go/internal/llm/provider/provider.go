// Package provider 实现 DESIGN-v3 §G.3：provider 路由。
//
// 单独成包是因为 anthropic/ollama/gemini 子包都依赖 internal/llm
// 的类型（Message / ChatRequest 等），路由放在 llm 包内会构成
// import cycle。
package provider

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"deepseek-harness-go/internal/config"
	"deepseek-harness-go/internal/llm"
	"deepseek-harness-go/internal/llm/anthropic"
	"deepseek-harness-go/internal/llm/gemini"
	"deepseek-harness-go/internal/llm/ollama"
)

// SupportedProviders 列出可用的 provider（§G.5：未支持 provider
// 启动时报错并列出支持列表）。
var SupportedProviders = []string{"openai", "anthropic", "ollama", "gemini"}

// NewClient 按 cfg.Provider 构建 LLM Client（§G.3）。
//   - openai（默认，含空值）：OpenAI Chat Completions 兼容端点
//   - anthropic：internal/llm/anthropic
//   - ollama：本机 /api/chat（base-url 可覆盖）
//   - gemini：Google generateContent（base-url 可覆盖，便于 mock）
func NewClient(cfg config.LLMConfig) (llm.Client, error) {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	httpClient := &http.Client{Timeout: timeout}

	switch cfg.Provider {
	case "", "openai":
		u, err := url.Parse(cfg.BaseURL)
		if err != nil {
			return nil, fmt.Errorf("llm: invalid base-url %q: %w", cfg.BaseURL, err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, fmt.Errorf("llm: base-url scheme must be http(s), got %q", u.Scheme)
		}
		return &llm.OpenAICompatibleClient{
			BaseURL:    cfg.BaseURL,
			APIKey:     cfg.APIKey,
			HTTPClient: httpClient,
		}, nil
	case "anthropic":
		c := anthropic.NewClient(cfg.BaseURL, cfg.APIKey, cfg.Model)
		c.HTTPClient = httpClient
		return c, nil
	case "ollama":
		c := ollama.NewClient(cfg.BaseURL, cfg.Model)
		c.HTTPClient = httpClient
		return c, nil
	case "gemini":
		c := gemini.NewClient(cfg.BaseURL, cfg.APIKey, cfg.Model)
		c.HTTPClient = httpClient
		return c, nil
	default:
		return nil, fmt.Errorf("llm: provider %q not supported (supported: %v)", cfg.Provider, SupportedProviders)
	}
}
