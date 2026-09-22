package llm

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"deepseek-harness-go/internal/config"
)

// NewClient 根据已校验的 LLMConfig 构建 OpenAI 兼容的 Client。
// 调用方应先执行 config.Validate（config.Load 已包含此步骤）。
func NewClient(cfg config.LLMConfig) (Client, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("llm: invalid base-url %q: %w", cfg.BaseURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("llm: base-url scheme must be http(s), got %q", u.Scheme)
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &OpenAICompatibleClient{
		BaseURL: cfg.BaseURL,
		APIKey:  cfg.APIKey,
		HTTPClient: &http.Client{
			Timeout: timeout,
		},
	}, nil
}
