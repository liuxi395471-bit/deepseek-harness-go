// Package runtime — 模型渠道注册表（v5 P5-3）。
//
// channel.go 提供多 LLM 渠道的注册与解析。运行期按 channelCode
// 切换 LLM 客户端，覆盖 ds-java `harness_model_setting` 表的核心
// 场景（运行时按渠道切换 base-url / 模型 / API key）。
//
// 设计要点：
//   - 接口：Registry + Resolve(code)；
//   - 实现：MemoryRegistry（sync.RWMutex + map）；
//   - 加载：从 YAML（cfg.Channels）和兜底（cfg.LLM）的统一解析；
//   - 跨进程：留给 v6+。
//   - 向后兼容：当 cfg.Channels 为空时仍支持 cfg.LLM 单渠道。
package runtime

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"deepseek-harness-go/internal/config"
	"deepseek-harness-go/internal/llm"
	"deepseek-harness-go/internal/llm/provider"
)

// ErrUnknownChannel 在 Resolve 收到未注册的 code 时返回。
var ErrUnknownChannel = errors.New("runtime: unknown channel")

// ChannelConfig 复用 config.ChannelConfig；本别名仅为兼容早期设计。
type ChannelConfig = config.ChannelConfig

// channelEntry 是注册表内部项：解析后保留 cfg + 已构造的 client 缓存。
type channelEntry struct {
	cfg    config.LLMConfig
	client llm.Client
}

// Registry 是渠道的查找接口。
type Registry interface {
	// Resolve 返回 code 对应的 LLM 客户端。未知 code 返回 ErrUnknownChannel。
	Resolve(code string) (llm.Client, error)
	// Codes 返回已注册的 code 列表。
	Codes() []string
}

// MemoryRegistry 是进程内的 Registry 实现。
//
// 并发安全：构造时一次性把全部 channel 解析为 client 并缓存；
// Resolve 只做 map 查询 + 单飞构造（首次 Resolve 时构造）。
type MemoryRegistry struct {
	mu      sync.RWMutex
	entries map[string]*channelEntry
	order   []string
}

// NewRegistry 从 configs 构造注册表。如果 configs 为空，自动
// 从 cfg（兜底）注册一个 code="default" 的渠道。
//
// 行为：
//   - 空 configs + cfg == zero：返回空 Registry；
//   - 空 configs + cfg 非零：注册 code="default"；
//   - 任一 channel 解析失败：返回错误（不让启动）；
//   - 重复 code：返回错误。
func NewRegistry(configs []ChannelConfig, cfg config.LLMConfig) (*MemoryRegistry, error) {
	r := &MemoryRegistry{entries: make(map[string]*channelEntry)}
	if len(configs) == 0 {
		// 兜底：单一 cfg.LLM → 注册 "default"
		if isZeroLLM(cfg) {
			return r, nil
		}
		if err := r.add("default", cfg); err != nil {
			return nil, err
		}
		return r, nil
	}
	for _, ch := range configs {
		if ch.Code == "" {
			return nil, errors.New("runtime: channel.code required")
		}
		if ch.Provider == "" {
			return nil, fmt.Errorf("runtime: channel %q provider required", ch.Code)
		}
		if ch.Model == "" {
			return nil, fmt.Errorf("runtime: channel %q model required", ch.Code)
		}
		entry := config.LLMConfig{
			Provider:  ch.Provider,
			BaseURL:   ch.BaseURL,
			APIKey:    ch.APIKey,
			Model:     ch.Model,
			MaxTokens: ch.MaxTokens,
		}
		if ch.Timeout != "" {
			d, err := time.ParseDuration(ch.Timeout)
			if err != nil {
				return nil, fmt.Errorf("runtime: channel %q timeout: %w", ch.Code, err)
			}
			entry.Timeout = d
		} else {
			entry.Timeout = 120 * time.Second
		}
		if err := r.add(ch.Code, entry); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// isZeroLLM 返回 cfg 是否为零值。
func isZeroLLM(cfg config.LLMConfig) bool {
	return cfg.Provider == "" && cfg.BaseURL == "" && cfg.Model == ""
}

func (r *MemoryRegistry) add(code string, cfg config.LLMConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.entries[code]; dup {
		return fmt.Errorf("runtime: duplicate channel code %q", code)
	}
	client, err := provider.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("runtime: channel %q: %w", code, err)
	}
	r.entries[code] = &channelEntry{cfg: cfg, client: client}
	r.order = append(r.order, code)
	return nil
}

// Resolve 返回 code 对应的 LLM 客户端。未知 code 返回 ErrUnknownChannel。
func (r *MemoryRegistry) Resolve(code string) (llm.Client, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[code]
	if !ok {
		return nil, fmt.Errorf("%w: %q (have %v)", ErrUnknownChannel, code, r.order)
	}
	return e.client, nil
}

// Codes 返回已注册的 code 列表（按注册顺序）。
func (r *MemoryRegistry) Codes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// ResolveModel 返回 code 渠道绑定的 model 名（用于 runner.Model）。
// 当 code 未注册时返回兜底 model。
func (r *MemoryRegistry) ResolveModel(code string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[code]
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownChannel, code)
	}
	return e.cfg.Model, nil
}
