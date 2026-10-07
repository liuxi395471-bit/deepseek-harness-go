package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestParseUsageBytes_DeepSeekV4：DeepSeek V4 报告
// usage.prompt_tokens_details.cached_tokens
func TestParseUsageBytes_DeepSeekV4(t *testing.T) {
	raw := []byte(`{
		"prompt_tokens": 4000,
		"completion_tokens": 500,
		"total_tokens": 4500,
		"prompt_tokens_details": {"cached_tokens": 3000}
	}`)
	u := ParseUsageBytes(raw)
	if u.PromptTokens != 4000 || u.CompletionTokens != 500 || u.TotalTokens != 4500 {
		t.Fatalf("basic fields wrong: %+v", u)
	}
	if u.CacheReadTokens != 3000 {
		t.Fatalf("CacheReadTokens = %d, want 3000", u.CacheReadTokens)
	}
	hit := u.CacheHitRate()
	if hit < 0.74 || hit > 0.76 {
		t.Fatalf("cache hit rate = %f, want ~0.75 (3000/4000)", hit)
	}
}

// TestParseUsageBytes_DeepSeekV3：DeepSeek V3 旧版
// prompt_cache_hit_tokens / prompt_cache_miss_tokens
func TestParseUsageBytes_DeepSeekV3(t *testing.T) {
	raw := []byte(`{
		"prompt_tokens": 1000,
		"completion_tokens": 200,
		"total_tokens": 1200,
		"prompt_cache_hit_tokens": 800,
		"prompt_cache_miss_tokens": 200
	}`)
	u := ParseUsageBytes(raw)
	if u.CacheReadTokens != 800 {
		t.Fatalf("CacheReadTokens = %d, want 800", u.CacheReadTokens)
	}
	if u.PromptTokens != 1000 {
		t.Fatalf("PromptTokens = %d, want 1000", u.PromptTokens)
	}
	// 命中率 = 800 / (800 + 200) = 0.8
	hit := u.CacheHitRate()
	if hit < 0.79 || hit > 0.81 {
		t.Fatalf("cache hit rate = %f, want ~0.8", hit)
	}
}

// TestParseUsageBytes_ReasoningTokens：OpenAI o1 / DeepSeek-R1
func TestParseUsageBytes_ReasoningTokens(t *testing.T) {
	raw := []byte(`{
		"prompt_tokens": 100,
		"completion_tokens": 200,
		"total_tokens": 300,
		"completion_tokens_details": {"reasoning_tokens": 150}
	}`)
	u := ParseUsageBytes(raw)
	if u.ReasoningTokens != 150 {
		t.Fatalf("ReasoningTokens = %d, want 150", u.ReasoningTokens)
	}
}

// TestParseUsageBytes_OnlyLegacy：极简，只给 prompt/completion
func TestParseUsageBytes_OnlyLegacy(t *testing.T) {
	raw := []byte(`{"prompt_tokens": 50, "completion_tokens": 20, "total_tokens": 70}`)
	u := ParseUsageBytes(raw)
	if u.CacheReadTokens != 0 || u.ReasoningTokens != 0 {
		t.Fatalf("expected zero cache/reasoning, got %+v", u)
	}
	if u.CacheHitRate() != 0 {
		t.Fatalf("cold turn hit rate should be 0")
	}
}

// TestParseUsageBytes_ColdTurn：V4 返回 cached_tokens=0（冷启动）
// 命中率应隐藏（display 用 0%）。
func TestParseUsageBytes_ColdTurn(t *testing.T) {
	raw := []byte(`{
		"prompt_tokens": 500,
		"completion_tokens": 100,
		"total_tokens": 600,
		"prompt_tokens_details": {"cached_tokens": 0}
	}`)
	u := ParseUsageBytes(raw)
	if u.CacheReadTokens != 0 {
		t.Fatalf("CacheReadTokens = %d, want 0", u.CacheReadTokens)
	}
	if u.CacheHitRate() != 0 {
		t.Fatalf("cold turn hit rate should be 0, got %f", u.CacheHitRate())
	}
}

// TestParseUsageBytes_InvalidJSON：坏 JSON 返回零值（不阻塞流）
func TestParseUsageBytes_InvalidJSON(t *testing.T) {
	u := ParseUsageBytes([]byte(`{not json`))
	if u != (Usage{}) {
		t.Fatalf("invalid JSON should return zero Usage, got %+v", u)
	}
}

// TestParseUsageBytes_RoundTrip：normalizeUsage 后 marshal 再解析保持一致
func TestParseUsageBytes_RoundTrip(t *testing.T) {
	raw := []byte(`{
		"prompt_tokens": 1000,
		"completion_tokens": 500,
		"total_tokens": 1500,
		"cache_read_tokens": 700,
		"cache_write_tokens": 50,
		"reasoning_tokens": 200
	}`)
	u := ParseUsageBytes(raw)
	if u.CacheReadTokens != 700 || u.CacheWriteTokens != 50 || u.ReasoningTokens != 200 {
		t.Fatalf("dsh-format fields: %+v", u)
	}
	// re-marshal 应保留 cache_read_tokens（json tag 一致）
	b, _ := json.Marshal(u)
	if !strings.Contains(string(b), `"cache_read_tokens":700`) {
		t.Fatalf("re-marshal missing cache_read_tokens: %s", b)
	}
	if !strings.Contains(string(b), `"reasoning_tokens":200`) {
		t.Fatalf("re-marshal missing reasoning_tokens: %s", b)
	}
}
