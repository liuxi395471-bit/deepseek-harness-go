// Package llm 提供 OpenAI 兼容的聊天补全客户端，
// 以及与 agent 循环共享的请求/响应类型。
//
// 第一版范围：
//
//   - 仅支持 Chat（非流式）。Stream 布尔字段虽然存在但
//     不会被处理；以 Stream=true 调用会返回 ErrStreamNotSupported。
//   - tool_calls[].function.arguments 保留为 JSON 字符串
//     （不会解码为 map），以便 runner 可以原样传递给工具。
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Role 是 Chat Completions 的消息角色。
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ToolCallFunc 是 ToolCall 中的函数调用载荷。
// ToolCallFunc 描述要调用的 function 工具。
type ToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON 编码的字符串；原样保留
}

// ToolCall 是 assistant 发起的工具调用请求。
//
// Index 字段在流式响应里用于给 tool_call 一个稳定 key（部分
// OpenAI-兼容实现如 DeepSeek 在前几个 delta 里不会发出 id，只
// 给出 index）。重组器（internal/stream）应优先按 ID 合并，若
// ID 为空则按 Index 合并，按 Name 合并作为最后兜底。
//
// Index 用 *int 区分"未设置"与"index=0"（OpenAI 协议下两者的语义
// 不同：第一个 tool_call 必然有 index=0，未设置意味着 delta 没带
// 任何 key 字段）。
type ToolCall struct {
	Index    *int         `json:"index,omitempty"`
	ID       string       `json:"id"`
	Type     string       `json:"type"` // "function"
	Function ToolCallFunc `json:"function"`
}

// Message 是会话历史中的一条记录。
//
// Seq 仅在内部使用（v8.1 用于编辑/删除 API 的消息定位），不属于
// OpenAI wire format；序列化时 json:"-" 屏蔽，避免污染 LLM 请求体。
type Message struct {
	Seq        int64     `json:"seq,omitempty"`
	Role       Role      `json:"role"`
	Content    string    `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string    `json:"tool_call_id,omitempty"` // 仅 role=tool 时使用；对应 ToolCall.ID
	Name       string    `json:"name,omitempty"`         // 可选，role=tool 时表示对应的工具
}

// ToolSpecFunc 向 LLM 描述一个可调用的函数。
type ToolSpecFunc struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"` // JSON Schema 对象
}

// ToolSpec 是单个工具声明的线上格式。
type ToolSpec struct {
	Type     string       `json:"type"` // "function"
	Function ToolSpecFunc `json:"function"`
}

// ChatRequest 是发送到 /chat/completions 的请求体。
type ChatRequest struct {
	Model       string     `json:"model"`
	Messages    []Message  `json:"messages"`
	Tools       []ToolSpec `json:"tools,omitempty"`
	ToolChoice  any        `json:"tool_choice,omitempty"`
	Temperature *float64   `json:"temperature,omitempty"`
	MaxTokens   int        `json:"max_tokens,omitempty"`
	Stream      bool       `json:"stream,omitempty"` // 第一版：必须为 false
}

// Usage 是响应中可选返回的 token 用量统计。
//
// v8.1：拆出 cache 与 reasoning 字段，对齐 DeepSeek V4 报告
// `usage.prompt_tokens_details.cached_tokens`（及旧版
// `prompt_cache_hit_tokens` / `prompt_cache_miss_tokens`）。
//
// json tag 兼容两种字段命名：
//   - `cache_read_tokens` / `cache_write_tokens`（dsh / DeepSeek V4 推荐）
//   - `prompt_cache_hit_tokens`（DeepSeek V3 旧版，会自动归一为 CacheReadTokens）
//
// `reasoning_tokens` 直接对应 OpenAI o1 / DeepSeek-R1 的 output 子集。
//
// cache hit rate = CacheReadTokens / max(1, CacheReadTokens + UncachedInputTokens)
//   其中 UncachedInputTokens = PromptTokens - CacheReadTokens - CacheWriteTokens
// ReasoningTokens 是 CompletionTokens 的子集，永远不再计入 cache% 或 prompt%。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`

	// CacheReadTokens 是从 prefix cache 命中的 token 数（按折扣价计费）。
	// 兼容 prompt_cache_hit_tokens（旧 DeepSeek V3）。
	CacheReadTokens int `json:"cache_read_tokens,omitempty"`
	// CacheWriteTokens 是本次请求写入 cache 的 token 数（首次计费）。
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
	// ReasoningTokens 是 reasoning 模型的思考 token 数（Output 子集）。
	// 兼容 reasoning_tokens（OpenAI o1 / DeepSeek-R1）。
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
}

// CacheHitRate 返回缓存命中率（0~1）。billed input = cacheRead + cacheWrite + uncached。
// uncached = prompt - cacheRead - cacheWrite（可能为 0 或负数时取 max(0)）。
func (u Usage) CacheHitRate() float64 {
	billed := u.CacheReadTokens + u.CacheWriteTokens + u.UncachedInputTokens()
	if billed <= 0 {
		return 0
	}
	return float64(u.CacheReadTokens) / float64(billed)
}

// UncachedInputTokens 返回真正"没走 cache"、按全价计费的输入 token 数。
func (u Usage) UncachedInputTokens() int {
	v := u.PromptTokens - u.CacheReadTokens - u.CacheWriteTokens
	if v < 0 {
		return 0
	}
	return v
}

// BilledInputTokens 返回计费的输入 token 数（cache read + cache write + uncached）。
func (u Usage) BilledInputTokens() int {
	return u.CacheReadTokens + u.CacheWriteTokens + u.UncachedInputTokens()
}

// Choice 是 ChatResponse.Choices 中的一项（通常长度为 1）。
type Choice struct {
	Index        int     `json:"index"`
	FinishReason string  `json:"finish_reason"`
	Message      Message `json:"message"`
}

// ChatResponse 是解析后的响应体。
type ChatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
}

// APIError 在上游返回非 2xx 响应时返回。
type APIError struct {
	Status int
	Body   string // 前 4 KiB，可能被截断
	Method string
	URL    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("llm: %s %s: HTTP %d: %s", e.Method, e.URL, e.Status, truncate(e.Body, 200))
}

// ErrStreamNotSupported 在 req.Stream 为 true 时由 Chat 返回。
// 流式将在 M5 实现；参见 PLAN §7。
var ErrStreamNotSupported = errors.New("llm: streaming not implemented in v1")

// Client 是 runner 所依赖的契约。
// 生产环境装配：*OpenAICompatibleClient；测试环境：mock。
type Client interface {
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
	// ChatStream 发起流式补全并产出已解码的 chunk。
	// 与 Chat 不同，ChatStream 始终感知流式；服务端必须
	// 支持 SSE。429 响应会触发一次自动重试（退避 200 ms）；
	// 其他非 2xx 响应作为错误返回。context 取消会中止
	// 进行中的请求；已接收的 chunk 仍会发出。
	ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, <-chan error)
}

// StreamChunk 是流式聊天补全中一个已解码的帧。
//
// DESIGN-v2 §A.1.2：调用方只会收到已解码的 delta 值；线上
// 格式（OpenAI choices[].delta）在 llm/sse_feed.go 中处理。
//
// 字段语义：
//
//   - Text:      assistant 消息的文本增量；若该帧仅携带
//                工具调用片段则为空
//   - Index:     该增量所属的 choice（OpenAI 多选；单选
//                服务商始终为 0）
//   - Finish:    仅在某个 choice 的终止帧上非空；
//                取值为 "stop"、"tool_calls"、"length" 或 "content_filter"
//   - ToolCalls: 文本增量期间为 nil；当 Finish == "tool_calls"
//                时在终止帧上填充
type StreamChunk struct {
	Text      string
	Index     int
	Finish    string
	ToolCalls []ToolCall
	// Usage 由部分服务商在终止帧上设置（例如 OpenAI 会将
	// usage 作为单独的尾随 chunk 发送）。中间帧上为 nil。
	Usage *Usage
}

// SSEFrame 是 OpenAI 的线上格式；internal/stream 消费它并产出
// StreamChunk 值。之所以导出是因为 stream 包需要解码它，
// 但调用方（agent 层）绝不应手工构造帧——它们只会看到
// 已解码的 StreamChunk 值。
type SSEFrame struct {
	Choices []struct {
		Delta        SSEDelta `json:"delta"`
		FinishReason string   `json:"finish_reason"`
	} `json:"choices"`
	// UsageRaw 保留原始 JSON，chunk 时再 normalize 成 Usage。
	// 这样能识别嵌套结构（prompt_tokens_details.cached_tokens 等）。
	UsageRaw json.RawMessage `json:"usage,omitempty"`
}

// SSEDelta 是每帧的增量载荷：部分文本和/或部分
// 工具调用片段，两者都可能缺失。
type SSEDelta struct {
	Content   string    `json:"content,omitempty"`
	ToolCalls []SSETool `json:"tool_calls,omitempty"`
	Role      string    `json:"role,omitempty"`
}

// SSETool 是 OpenAI 的增量工具调用格式。Function.Arguments 是
// JSON 的*片段*（通常是单个属性，甚至是不完整的字符串）。
//
// Index 字段在多数 OpenAI-兼容实现里稳定给出（0-based），是
// "无 ID 时"的合并 key。这里用 *int 以区分"未提供"与"index=0"。
type SSETool struct {
	Index    *int   `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

// truncate 返回 s 的前最多 n 个字节，若被截断则以省略号结尾。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// OpenAICompatibleClient 对接 Chat Completions REST API。
//
// 字段是导出的，以便测试无需构造函数即可构建；
// 生产环境装配应使用 NewOpenAICompatibleClient（它会设置
// 合理的默认 HTTP 超时）或 client.go 中的 NewClient。
type OpenAICompatibleClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

// NewOpenAICompatibleClient 构建一个默认超时为 120 秒的客户端。
func NewOpenAICompatibleClient(baseURL, apiKey string) *OpenAICompatibleClient {
	return &OpenAICompatibleClient{
		BaseURL:    baseURL,
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: 120 * time.Second},
	}
}

// Chat 将 req POST 到 {BaseURL}/chat/completions 并解码响应。
//
// 行为：
//   - req.Stream=true → 返回 ErrStreamNotSupported（不发起网络调用）。
//   - 非 2xx → 返回包含状态码和截断响应体的 *APIError。
//   - 2xx 但响应体格式错误 → 返回包装后的解码错误。
func (c *OpenAICompatibleClient) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if req.Stream {
		return ChatResponse{}, ErrStreamNotSupported
	}

	body, err := json.Marshal(req)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("llm: marshal request: %w", err)
	}

	endpoint := strings.TrimRight(c.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("llm: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("llm: do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return ChatResponse{}, &APIError{
			Status: resp.StatusCode,
			Body:   string(buf),
			Method: httpReq.Method,
			URL:    httpReq.URL.String(),
		}
	}

	var out ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return ChatResponse{}, fmt.Errorf("llm: decode response: %w", err)
	}
	return out, nil
}

// ChatStream 以 stream=true 发送 req 并流式读取 SSE 帧。
//
// 429 → 退避 200 ms 后重试一次；仍失败 → errCh 收到 *APIError。
// ctx 取消 → 中止 HTTP 请求；已接收的 chunk 仍会发出。
// 4xx（非 429）→ *APIError。
//
// 并发：ChatStream 每次调用启动一个 goroutine 并立即返回。
// goroutine 退出时两个返回的 channel 都会被关闭。
func (c *OpenAICompatibleClient) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, <-chan error) {
	chunkCh := make(chan StreamChunk, 4)
	errCh := make(chan error, 1)
	go func() {
		defer close(chunkCh)
		defer close(errCh)
		// 429 重试循环：遇到 429 再尝试一次，然后放弃。
		var lastErr error
		for attempt := 0; attempt < 2; attempt++ {
			if attempt > 0 {
				// 按 DESIGN-v2 §A.1.4 退避 200 ms。
				select {
				case <-ctx.Done():
					return
				case <-time.After(200 * time.Millisecond):
				}
			}
			lastErr = c.doStream(ctx, req, chunkCh)
			if lastErr == nil {
				return
			}
			var apiErr *APIError
			if errors.As(lastErr, &apiErr) && apiErr.Status == 429 {
				continue // 重试
			}
			// 致命错误：非 429 错误或重试已耗尽。
			errCh <- lastErr
			return
		}
		// 重试已耗尽。
		errCh <- lastErr
	}()
	return chunkCh, errCh
}

// doStream 执行一次 HTTP 请求并将 chunk 送入 chunkCh。
// 正常 EOF 时返回 nil，出现致命/不可恢复的错误时返回 error。
func (c *OpenAICompatibleClient) doStream(ctx context.Context, req ChatRequest, chunkCh chan<- StreamChunk) error {
	req.Stream = true
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("llm: marshal request: %w", err)
	}
	endpoint := strings.TrimRight(c.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("llm: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("llm: do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &APIError{
			Status: resp.StatusCode,
			Body:   string(buf),
			Method: httpReq.Method,
			URL:    httpReq.URL.String(),
		}
	}
	for frame := range sseFeed(ctx, resp.Body) {
		if frame.err != nil {
			return frame.err
		}
		chunk := frame.Chunk(0)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case chunkCh <- chunk:
		}
	}
	return nil
}
