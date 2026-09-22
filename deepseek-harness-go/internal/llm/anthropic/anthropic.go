// Package anthropic 实现 Anthropic Messages API 客户端（DESIGN-v2 SD.4）。
//
// 与 OpenAI 兼容客户端不同，Anthropic 使用不同的线上格式
// （"messages" 端点），其流式事件也采用不同的 SSE 形状。
package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"deepseek-harness-go/internal/llm"
)

// Client 实现 Anthropic Messages API 的 llm.Client。
type Client struct {
	BaseURL  string
	APIKey   string
	Model    string
	HTTPClient *http.Client
}

// NewClient 构建一个 Anthropic 客户端。
func NewClient(baseURL, apiKey, model string) *Client {
	return &Client{
		BaseURL:    baseURL,
		APIKey:     apiKey,
		Model:      model,
		HTTPClient: http.DefaultClient,
	}
}

// Chat 调用 Anthropic messages 端点并返回 ChatResponse。
func (c *Client) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	if req.Stream {
		return llm.ChatResponse{}, errors.New("anthropic: use ChatStream for streaming")
	}

	anthropicReq := toAnthropicReq(req, c.Model)
	body, err := json.Marshal(anthropicReq)
	if err != nil {
		return llm.ChatResponse{}, fmt.Errorf("anthropic: marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimSuffix(c.BaseURL, "/")+"/v1/messages", strings.NewReader(string(body)))
	if err != nil {
		return llm.ChatResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return llm.ChatResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return llm.ChatResponse{}, fmt.Errorf("anthropic: HTTP %d: %s", resp.StatusCode, body)
	}

	var ar anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return llm.ChatResponse{}, fmt.Errorf("anthropic: decode: %w", err)
	}
	return fromAnthropicResp(ar)
}

// ChatStream 在 Anthropic Messages API 之上实现 SSE 流式
// （DESIGN-v2 §D.4）。它会响应 ctx 取消，通过 errCh 上报致命的
// 传输错误，并为每个 content_block_delta 发出一个 llm.StreamChunk。
// 当 message_delta 到达时，最后一个 chunk 会设置 Finish="stop"
// （或 "tool_use"）。
//
// 线上格式说明：Anthropic 使用 `event: <type>\ndata: <json>`，
// 以空行分隔，而非 OpenAI 的 `data: <json>\n\n`。我们使用自己的
// 解析器（stream.go 中的 Feed），而非 internal/stream 中面向
// OpenAI 形状的 SSEFeed。
func (c *Client) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, <-chan error) {
	ch := make(chan llm.StreamChunk)
	errCh := make(chan error, 1)

	if req.Stream == false {
		// 强制流式语义：调用方想用流式却忘了设置 req.Stream。
		// 保持严格——直接报错，而不是静默回退到 Chat。
		errCh <- errors.New("anthropic: ChatStream requires req.Stream=true")
		close(ch)
		return ch, errCh
	}

	go func() {
		defer close(ch)
		defer close(errCh)

		// 转换请求；在线上格式中强制 stream=true。
		anthropicReq := toAnthropicReq(req, c.Model)
		anthropicReq.Stream = true
		body, err := json.Marshal(anthropicReq)
		if err != nil {
			errCh <- fmt.Errorf("anthropic: marshal: %w", err)
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
			strings.TrimSuffix(c.BaseURL, "/")+"/v1/messages", strings.NewReader(string(body)))
		if err != nil {
			errCh <- err
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("x-api-key", c.APIKey)
		httpReq.Header.Set("anthropic-version", "2023-06-01")
		httpReq.Header.Set("Accept", "text/event-stream")

		resp, err := c.HTTPClient.Do(httpReq)
		if err != nil {
			errCh <- err
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			errCh <- fmt.Errorf("anthropic: HTTP %d: %s", resp.StatusCode, body)
			return
		}

		var (
			finish string
			out    = ch
		)
		events := Feed(ctx, resp.Body)
		for ev := range events {
			switch ev.Type {
			case evContentBlockDelta:
				if ev.DeltaType == "text_delta" && ev.DeltaText != "" {
					select {
					case <-ctx.Done():
						return
					case out <- llm.StreamChunk{Text: ev.DeltaText}:
					}
				}
			case evMessageDelta:
				if ev.StopReason != "" {
					finish = ev.StopReason
				}
			case evMessageStop:
				if finish == "" {
					finish = "stop"
				}
				select {
				case <-ctx.Done():
					return
				case out <- llm.StreamChunk{Finish: finish}:
				}
				return
			case evError:
				errCh <- fmt.Errorf("anthropic: stream error: %s", ev.ErrMessage)
				return
			}
		}
		// 流在 message_stop 之前结束（EOF）。若尚未发出终止
		// finish，则补发一次，避免消费方一直等待。
		if finish != "" {
			select {
			case <-ctx.Done():
			case out <- llm.StreamChunk{Finish: finish}:
			}
		}
	}()
	return ch, errCh
}

// ---- 内部类型 ----

type anthropicRequest struct {
	Model       string         `json:"model"`
	Messages    []msgPart      `json:"messages"`
	MaxTokens  int            `json:"max_tokens"`
	Stream     bool           `json:"stream"`
}

type msgPart struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type anthropicResponse struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Role     string       `json:"role"`
	Content  []contentBlock `json:"content"`
	Model    string       `json:"model"`
	StopReason string    `json:"stop_reason"`
	Usage    anthropicUsage `json:"usage"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func toAnthropicReq(req llm.ChatRequest, model string) anthropicRequest {
	msgs := make([]msgPart, 0, len(req.Messages))
	for _, m := range req.Messages {
		content := m.Content
		if m.Role == llm.RoleTool {
			// Anthropic 的工具结果以 content block 形式返回。
			content = m.Content
		}
		role := string(m.Role)
		if role == "system" {
			role = "user" // Anthropic 没有 system 角色
		}
		msgs = append(msgs, msgPart{Role: role, Content: content})
	}
	return anthropicRequest{
		Model:      model,
		Messages:   msgs,
		MaxTokens:  req.MaxTokens,
		Stream:     req.Stream,
	}
}

func fromAnthropicResp(ar anthropicResponse) (llm.ChatResponse, error) {
	var content string
	var tcs []llm.ToolCall
	for _, block := range ar.Content {
		if block.Type == "text" {
			content += block.Text
		}
	}
	finish := ar.StopReason
	if finish == "" {
		finish = "stop"
	}
	return llm.ChatResponse{
		Choices: []llm.Choice{{
			Message: llm.Message{
				Role:       llm.RoleAssistant,
				Content:    content,
				ToolCalls:  tcs,
			},
			FinishReason: finish,
		}},
		Usage: &llm.Usage{
			PromptTokens:     ar.Usage.InputTokens,
			CompletionTokens: ar.Usage.OutputTokens,
			TotalTokens:      ar.Usage.InputTokens + ar.Usage.OutputTokens,
		},
	}, nil
}
