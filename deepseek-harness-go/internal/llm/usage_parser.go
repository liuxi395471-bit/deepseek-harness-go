package llm

// usage_parser.go — 兼容多种 provider 的 usage 字段解码
//
// DeepSeek / OpenAI 兼容接口在 SSE 终止帧或非流式响应里附带
// `usage` 对象，但字段名随 provider 演进而变化：
//
//   - DeepSeek V4 (推荐): usage.prompt_tokens_details.cached_tokens
//   - DeepSeek V3 (旧):   usage.prompt_cache_hit_tokens /
//                         usage.prompt_cache_miss_tokens
//   - OpenAI 通用:        usage.prompt_tokens（含 cache 字段子对象）
//   - 极简:              只给 prompt_tokens / completion_tokens
//
// reasoning_tokens 仅 reasoning model 报告（OpenAI o1 / DeepSeek-R1）。
//
// 我们用一个内嵌 rawUsage JSON 兜底，把"未知字段"放到 raw 里，
// 然后按上述顺序回退读取 cache 数 + reasoning 数。

import "encoding/json"

// rawUsage 是 OpenAI / DeepSeek 报告的 usage 对象原始结构。
// 字段全部为 omitempty（即便没有也照常解码），以兼容不同 provider。
type rawUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`

	// DeepSeek V4 / OpenAI 新版：把 cache 放到子对象里
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitempty"`

	// DeepSeek V3 旧版
	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens,omitempty"`

	// DeepSeek V4 也兼容字段（按 dsh-deepseek-usage-monitor README）
	CacheReadTokens  int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`

	// OpenAI o1 / DeepSeek-R1 reasoning_tokens（在 completion_tokens_details 里）
	CompletionTokensDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details,omitempty"`
	// reasoning_tokens 顶层字段（部分 provider 直接给）
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
}

// normalizeUsage 把 rawUsage 转成 llm.Usage，按以下顺序回退：
//   1. cacheRead = max(cached_tokens, prompt_cache_hit_tokens, cache_read_tokens)
//   2. uncached  = prompt - cacheRead - cacheWrite
//      （OpenAI 有时给 prompt_cache_miss_tokens 字段，可作为更精确的 uncached）
//   3. reasoning = completion_tokens_details.reasoning_tokens
//                 或 reasoning_tokens 顶层字段
// 任何字段缺失都视为 0，绝不返回 error（usage 是 best-effort）。
func normalizeUsage(raw rawUsage) Usage {
	u := Usage{
		PromptTokens:     raw.PromptTokens,
		CompletionTokens: raw.CompletionTokens,
		TotalTokens:      raw.TotalTokens,
	}

	// Cache read：三种来源取最大
	if raw.PromptTokensDetails != nil && raw.PromptTokensDetails.CachedTokens > u.CacheReadTokens {
		u.CacheReadTokens = raw.PromptTokensDetails.CachedTokens
	}
	if raw.PromptCacheHitTokens > u.CacheReadTokens {
		u.CacheReadTokens = raw.PromptCacheHitTokens
	}
	if raw.CacheReadTokens > u.CacheReadTokens {
		u.CacheReadTokens = raw.CacheReadTokens
	}

	// Cache write：先取显式字段，否则用 prompt_cache_miss 推算
	// DeepSeek V3: prompt_cache_miss = 真正未命中（不计入 cache）
	// 老的 dsh-deepseek-usage-monitor 用 (prompt - hit) 作为 cache miss
	u.CacheWriteTokens = raw.CacheWriteTokens
	if raw.PromptCacheMissTokens > 0 && u.CacheWriteTokens == 0 {
		// 若 provider 明确给 prompt_cache_miss，按 README 推荐：
		// cache 命中率 = hit / (hit + miss)，但 cache_write 此时视为 0
		// 后面 UncachedInputTokens 会基于 prompt - cacheRead - cacheWrite 计算
		// 这里我们把 miss 当成 uncached，cacheWrite 保持 0
		_ = raw.PromptCacheMissTokens // 通过 UncachedInputTokens 隐式处理
	}

	// Reasoning tokens
	if raw.CompletionTokensDetails != nil && raw.CompletionTokensDetails.ReasoningTokens > u.ReasoningTokens {
		u.ReasoningTokens = raw.CompletionTokensDetails.ReasoningTokens
	}
	if raw.ReasoningTokens > u.ReasoningTokens {
		u.ReasoningTokens = raw.ReasoningTokens
	}

	// 若 total_tokens 没给，临时算一个（很多 provider 都会给，这里只兜底）
	if u.TotalTokens == 0 {
		u.TotalTokens = u.PromptTokens + u.CompletionTokens
	}
	return u
}

// ParseUsageBytes 从 SSE 终止帧的 usage JSON 解析出标准化 Usage。
// 任何错误都返回零值 Usage（不阻塞流）。
func ParseUsageBytes(data []byte) Usage {
	var raw rawUsage
	if err := json.Unmarshal(data, &raw); err != nil {
		return Usage{}
	}
	return normalizeUsage(raw)
}

// ParseUsageFromMap 从已 map 化的 SSEFrame.Usage 反向解析（用
// 重新 marshal 的方式）。当上游把 usage 字段在 OpenAICompatible
// 客户端里直接打到 *Usage 时，这条路径会跳过（直接使用 *Usage）。
// 但 *Usage 的字段还是旧版 3 字段，需要补 cache。
func ParseUsageFromMap(m map[string]any) Usage {
	b, err := json.Marshal(m)
	if err != nil {
		return Usage{}
	}
	return ParseUsageBytes(b)
}
