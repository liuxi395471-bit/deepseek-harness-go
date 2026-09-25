// Package llm 是 dsh 的 LLM 适配层（DESIGN-v2 §A.1）。
//
// v3 起，所有 provider 路由都应使用 internal/llm/provider.NewClient
// （DESIGN-v3 §G.3）；它对 openai / anthropic / ollama / gemini 做统一
// 调度，对 openai 路径会回退到本包的 NewClient 兼容实现。本包的
// NewClient 保留为公开 API（v2 冻结，P1）但不再扩展；新代码请走
// provider 包。
package llm

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"deepseek-harness-go/internal/config"
)

// NewClient 根据已校验的 LLMConfig 构建 OpenAI 兼容的 Client。
// 多 provider 路由（anthropic / ollama / gemini，DESIGN-v3 §G.3）
// 见 internal/llm/provider 包——它会在 provider=openai 时转发到这里。
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
