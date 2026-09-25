// Package ollama 实现 DESIGN-v3 §G.1：Ollama provider。
//
// 协议：POST {BaseURL}/api/chat。流式响应是 ndjson（每行一个 JSON
// 对象），不是 SSE，但对外仍以 llm.Client 的 Chat/ChatStream 语义
// 呈现（§G.1 "Ollama 用 ndjson 而不是 SSE，但接口一致"）。
package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"deepseek-harness-go/internal/llm"
)

// Client 对接 Ollama /api/chat。
type Client struct {
	BaseURL    string // 例如 http://127.0.0.1:11434
	Model      string
	HTTPClient *http.Client
}

// NewClient 构建一个默认超时 120 秒的客户端。baseURL 为空时使用
// 本机默认端口。
func NewClient(baseURL, model string) *Client {
	if baseURL == "" {
		baseURL = "http://127.0.0.1:11434"
	}
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Model:      model,
		HTTPClient: &http.Client{Timeout: 120 * time.Second},
	}
}

// chatMessage 是 /api/chat 的消息线上格式。
// Ollama 的 tool 结果放在 role=tool + tool_name；assistant 的
// tool_calls.function.arguments 是 JSON 对象（非字符串）。
type chatMessage struct {
	Role      string      `json:"role"`
	Content   string      `json:"content,omitempty"`
	ToolName  string      `json:"tool_name,omitempty"`
	ToolCalls []toolCall  `json:"tool_calls,omitempty"`
	Images    []string    `json:"images,omitempty"`
}

type toolCall struct {
	Function toolCallFunc `json:"function"`
}

type toolCallFunc struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// chatRequest 是 /api/chat 请求体。
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Tools    []ollamaTool  `json:"tools,omitempty"`
	Stream   bool          `json:"stream"`
}

type ollamaTool struct {
	Type     string       `json:"type"` // "function"
	Function ollamaToolFn `json:"function"`
}

type ollamaToolFn struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// chatResponse 是 /api/chat 单帧响应（流式时每行一帧）。
type chatResponse struct {
	Model     string      `json:"model"`
	CreatedAt string      `json:"created_at"`
	Message   chatMessage `json:"message"`
	Done      bool        `json:"done"`
	// Done 帧上的统计。
	DoneReason      string `json:"done_reason,omitempty"`
	PromptEvalCount int    `json:"prompt_eval_count,omitempty"`
	EvalCount       int    `json:"eval_count,omitempty"`
}

// apiError 在上游返回非 2xx 时返回。
type apiError struct {
	Status int
	Body   string
	URL    string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("ollama: %s: HTTP %d: %s", e.URL, e.Status, e.Body)
}

func toChatMessages(msgs []llm.Message) []chatMessage {
	out := make([]chatMessage, 0, len(msgs))
	for _, m := range msgs {
		cm := chatMessage{Role: string(m.Role), Content: m.Content}
		if m.Role == llm.RoleTool {
			cm.ToolName = m.Name
		}
		for _, tc := range m.ToolCalls {
			args := map[string]any{}
			if tc.Function.Arguments != "" {
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			}
			cm.ToolCalls = append(cm.ToolCalls, toolCall{
				Function: toolCallFunc{Name: tc.Function.Name, Arguments: args},
			})
		}
		out = append(out, cm)
	}
	return out
}

func toOllamaTools(specs []llm.ToolSpec) []ollamaTool {
	if len(specs) == 0 {
		return nil
	}
	out := make([]ollamaTool, 0, len(specs))
	for _, s := range specs {
		params, _ := s.Function.Parameters.(map[string]any)
		out = append(out, ollamaTool{
			Type: "function",
			Function: ollamaToolFn{
				Name:        s.Function.Name,
				Description: s.Function.Description,
				Parameters:  params,
			},
		})
	}
	return out
}

// Chat 发送非流式请求（stream=false），等待完整响应。
func (c *Client) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = c.Model
	}
	body, err := json.Marshal(chatRequest{
		Model:    model,
		Messages: toChatMessages(req.Messages),
		Tools:    toOllamaTools(req.Tools),
		Stream:   false,
	})
	if err != nil {
		return llm.ChatResponse{}, fmt.Errorf("ollama: marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return llm.ChatResponse{}, fmt.Errorf("ollama: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return llm.ChatResponse{}, fmt.Errorf("ollama: do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return llm.ChatResponse{}, &apiError{Status: resp.StatusCode, Body: string(buf), URL: httpReq.URL.String()}
	}
	var cr chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return llm.ChatResponse{}, fmt.Errorf("ollama: decode: %w", err)
	}
	return cr.toLLMResponse(model), nil
}

// toLLMResponse 把 Ollama 帧转换为统一 ChatResponse。
func (r *chatResponse) toLLMResponse(model string) llm.ChatResponse {
	out := llm.ChatResponse{
		Model:  model,
		Object: "chat.completion",
	}
	msg := llm.Message{Role: llm.RoleAssistant, Content: r.Message.Content}
	for _, tc := range r.Message.ToolCalls {
		argsJSON, _ := json.Marshal(tc.Function.Arguments)
		msg.ToolCalls = append(msg.ToolCalls, llm.ToolCall{
			ID:   fmt.Sprintf("call_%s_%d", tc.Function.Name, time.Now().UnixNano()),
			Type: "function",
			Function: llm.ToolCallFunc{
				Name:      tc.Function.Name,
				Arguments: string(argsJSON),
			},
		})
	}
	finish := "stop"
	if r.Done && r.DoneReason == "length" {
		finish = "length"
	}
	out.Choices = []llm.Choice{{
		Index:        0,
		FinishReason: finish,
		Message:      msg,
	}}
	out.Usage = &llm.Usage{
		PromptTokens:     r.PromptEvalCount,
		CompletionTokens: r.EvalCount,
		TotalTokens:      r.PromptEvalCount + r.EvalCount,
	}
	return out
}

// ChatStream 以 stream=true 发送请求并逐行解码 ndjson。
// ctx 取消中止读取；已收到的 chunk 仍会发出。
// 并发：每次调用一个 goroutine，两个 channel 在退出时关闭。
func (c *Client) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, <-chan error) {
	chunkCh := make(chan llm.StreamChunk, 4)
	errCh := make(chan error, 1)
	go func() {
		defer close(chunkCh)
		defer close(errCh)
		if err := c.doStream(ctx, req, chunkCh); err != nil {
			errCh <- err
		}
	}()
	return chunkCh, errCh
}

func (c *Client) doStream(ctx context.Context, req llm.ChatRequest, chunkCh chan<- llm.StreamChunk) error {
	model := req.Model
	if model == "" {
		model = c.Model
	}
	body, err := json.Marshal(chatRequest{
		Model:    model,
		Messages: toChatMessages(req.Messages),
		Tools:    toOllamaTools(req.Tools),
		Stream:   true,
	})
	if err != nil {
		return fmt.Errorf("ollama: marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ollama: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("ollama: do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &apiError{Status: resp.StatusCode, Body: string(buf), URL: httpReq.URL.String()}
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var (
		text      strings.Builder
		toolCalls []llm.ToolCall
		usage     llm.Usage
		finish    string
		sentAny   bool
	)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var frame chatResponse
		if err := json.Unmarshal(line, &frame); err != nil {
			return fmt.Errorf("ollama: decode ndjson line: %w", err)
		}
		sentAny = true
		if frame.Message.Content != "" {
			text.WriteString(frame.Message.Content)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case chunkCh <- llm.StreamChunk{Text: frame.Message.Content}:
			}
		}
		for _, tc := range frame.Message.ToolCalls {
			argsJSON, _ := json.Marshal(tc.Function.Arguments)
			call := llm.ToolCall{
				ID:   fmt.Sprintf("call_%s_%d", tc.Function.Name, time.Now().UnixNano()),
				Type: "function",
				Function: llm.ToolCallFunc{
					Name:      tc.Function.Name,
					Arguments: string(argsJSON),
				},
			}
			toolCalls = append(toolCalls, call)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case chunkCh <- llm.StreamChunk{ToolCalls: []llm.ToolCall{call}}:
			}
		}
		if frame.PromptEvalCount > 0 || frame.EvalCount > 0 {
			usage = llm.Usage{
				PromptTokens:     frame.PromptEvalCount,
				CompletionTokens: frame.EvalCount,
				TotalTokens:      frame.PromptEvalCount + frame.EvalCount,
			}
		}
		if frame.Done {
			finish = "stop"
			if frame.DoneReason == "length" {
				finish = "length"
			}
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("ollama: scan: %w", err)
	}
	if !sentAny {
		return fmt.Errorf("ollama: empty stream")
	}
	final := llm.StreamChunk{Finish: finish}
	if len(toolCalls) > 0 {
		final.ToolCalls = toolCalls
	}
	if usage.TotalTokens > 0 {
		u := usage
		final.Usage = &u
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case chunkCh <- final:
	}
	return nil
}
