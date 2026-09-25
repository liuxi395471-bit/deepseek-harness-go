// Package gemini 实现 DESIGN-v3 §G.2：Google Gemini provider。
//
// 协议：
//   - 非流式：POST /v1beta/models/{model}:generateContent
//   - 流式：  POST /v1beta/models/{model}:streamGenerateContent?alt=sse
//
// 内部 Message（OpenAI 风格）与 Gemini contents 的映射：
//   - system          → systemInstruction
//   - assistant       → role "model"
//   - tool_calls      → parts[].functionCall
//   - role=tool       → parts[].functionResponse
package gemini

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

const defaultBaseURL = "https://generativelanguage.googleapis.com"

// Client 对接 Gemini generateContent API。
type Client struct {
	BaseURL    string
	APIKey     string
	Model      string // gemini-1.5-pro / gemini-1.5-flash 等
	HTTPClient *http.Client
}

// NewClient 构建一个默认超时 120 秒的客户端。
func NewClient(baseURL, apiKey, model string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		APIKey:     apiKey,
		Model:      model,
		HTTPClient: &http.Client{Timeout: 120 * time.Second},
	}
}

// ---- 线上格式 ----

type part struct {
	Text             string            `json:"text,omitempty"`
	FunctionCall     *functionCall     `json:"functionCall,omitempty"`
	FunctionResponse *functionResponse `json:"functionResponse,omitempty"`
}

type functionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

type functionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type content struct {
	Role  string `json:"role,omitempty"` // "user" | "model"
	Parts []part `json:"parts"`
}

type generationConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
}

type functionDeclaration struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type toolDecl struct {
	FunctionDeclarations []functionDeclaration `json:"functionDeclarations,omitempty"`
}

type generateRequest struct {
	Contents          []content        `json:"contents"`
	SystemInstruction *content         `json:"systemInstruction,omitempty"`
	Tools             []toolDecl       `json:"tools,omitempty"`
	GenerationConfig  generationConfig `json:"generationConfig,omitempty"`
}

type candidate struct {
	Content      content `json:"content"`
	FinishReason string  `json:"finishReason,omitempty"`
}

type usageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount,omitempty"`
	CandidatesTokenCount int `json:"candidatesTokenCount,omitempty"`
	TotalTokenCount      int `json:"totalTokenCount,omitempty"`
}

type generateResponse struct {
	Candidates    []candidate   `json:"candidates"`
	UsageMetadata usageMetadata `json:"usageMetadata,omitempty"`
	Error         *apiErrBody   `json:"error,omitempty"`
}

type apiErrBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

type apiError struct {
	Status int
	Body   string
	URL    string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("gemini: %s: HTTP %d: %s", e.URL, e.Status, e.Body)
}

// ---- 映射 ----

func toContents(msgs []llm.Message) (systemInstruction *content, contents []content) {
	contents = make([]content, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case llm.RoleSystem:
			// Gemini 只接受一条 systemInstruction；多条 system 合并。
			if systemInstruction == nil {
				systemInstruction = &content{Parts: []part{{Text: m.Content}}}
			} else {
				systemInstruction.Parts[0].Text += "\n\n" + m.Content
			}
		case llm.RoleUser:
			contents = append(contents, content{Role: "user", Parts: []part{{Text: m.Content}}})
		case llm.RoleAssistant:
			c := content{Role: "model"}
			if m.Content != "" {
				c.Parts = append(c.Parts, part{Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				args := map[string]any{}
				if tc.Function.Arguments != "" {
					_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
				}
				c.Parts = append(c.Parts, part{FunctionCall: &functionCall{Name: tc.Function.Name, Args: args}})
			}
			if len(c.Parts) == 0 {
				c.Parts = append(c.Parts, part{Text: ""})
			}
			contents = append(contents, c)
		case llm.RoleTool:
			resp := map[string]any{"result": m.Content}
			// 尝试把 JSON 工具结果还原为对象，便于模型阅读。
			var parsed map[string]any
			if json.Unmarshal([]byte(m.Content), &parsed) == nil {
				resp = parsed
			}
			name := m.Name
			if name == "" {
				name = "tool"
			}
			contents = append(contents, content{Role: "user", Parts: []part{{
				FunctionResponse: &functionResponse{Name: name, Response: resp},
			}}})
		}
	}
	return systemInstruction, contents
}

func toFunctionDeclarations(specs []llm.ToolSpec) []toolDecl {
	if len(specs) == 0 {
		return nil
	}
	decls := make([]functionDeclaration, 0, len(specs))
	for _, s := range specs {
		params, _ := s.Function.Parameters.(map[string]any)
		decls = append(decls, functionDeclaration{
			Name:        s.Function.Name,
			Description: s.Function.Description,
			Parameters:  params,
		})
	}
	return []toolDecl{{FunctionDeclarations: decls}}
}

// candidateToMessage 把 candidate.content 转回统一 Message。
// tool_calls 的 ID 由 name 生成（Gemini 不回传调用 ID；OpenAI 协议
// 要求 tool 消息带 ID，runner 用 ID 关联）。
func candidateToMessage(cand candidate) llm.Message {
	msg := llm.Message{Role: llm.RoleAssistant}
	for _, p := range cand.Content.Parts {
		if p.Text != "" {
			msg.Content += p.Text
		}
		if p.FunctionCall != nil {
			argsJSON, _ := json.Marshal(p.FunctionCall.Args)
			msg.ToolCalls = append(msg.ToolCalls, llm.ToolCall{
				ID:   fmt.Sprintf("call_%s", p.FunctionCall.Name),
				Type: "function",
				Function: llm.ToolCallFunc{
					Name:      p.FunctionCall.Name,
					Arguments: string(argsJSON),
				},
			})
		}
	}
	return msg
}

func finishReason(s string) string {
	switch s {
	case "STOP":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "SAFETY":
		return "content_filter"
	case "", "UNSPECIFIED":
		return "stop"
	default:
		return "stop"
	}
}

func (c *Client) endpoint(stream bool) string {
	suffix := ":generateContent"
	if stream {
		suffix = ":streamGenerateContent?alt=sse"
	}
	return fmt.Sprintf("%s/v1beta/models/%s%s", c.BaseURL, c.Model, suffix)
}

func (c *Client) buildRequest(ctx context.Context, req llm.ChatRequest, stream bool) (*http.Request, error) {
	// runner 允许运行时换模型：req.Model 覆盖端点中的 model。
	effective := *c
	if req.Model != "" {
		effective.Model = req.Model
	}
	sys, contents := toContents(req.Messages)
	body, err := json.Marshal(generateRequest{
		Contents:          contents,
		SystemInstruction: sys,
		Tools:             toFunctionDeclarations(req.Tools),
		GenerationConfig: generationConfig{
			Temperature:     req.Temperature,
			MaxOutputTokens: req.MaxTokens,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("gemini: marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, effective.endpoint(stream), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gemini: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if effective.APIKey != "" {
		httpReq.Header.Set("x-goog-api-key", effective.APIKey)
	}
	return httpReq, nil
}

func decodeBody(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return &apiError{Status: resp.StatusCode, Body: string(buf), URL: resp.Request.URL.String()}
}

// Chat 发送非流式请求。
func (c *Client) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	httpReq, err := c.buildRequest(ctx, req, false)
	if err != nil {
		return llm.ChatResponse{}, err
	}
	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return llm.ChatResponse{}, fmt.Errorf("gemini: do: %w", err)
	}
	defer resp.Body.Close()
	if err := decodeBody(resp); err != nil {
		return llm.ChatResponse{}, err
	}
	var gr generateResponse
	if err := json.NewDecoder(resp.Body).Decode(&gr); err != nil {
		return llm.ChatResponse{}, fmt.Errorf("gemini: decode: %w", err)
	}
	if gr.Error != nil {
		return llm.ChatResponse{}, fmt.Errorf("gemini: api error %d %s: %s", gr.Error.Code, gr.Error.Status, gr.Error.Message)
	}
	out := llm.ChatResponse{Model: c.Model, Object: "chat.completion"}
	if len(gr.Candidates) > 0 {
		cand := gr.Candidates[0]
		out.Choices = []llm.Choice{{
			Index:        0,
			FinishReason: finishReason(cand.FinishReason),
			Message:      candidateToMessage(cand),
		}}
	}
	out.Usage = &llm.Usage{
		PromptTokens:     gr.UsageMetadata.PromptTokenCount,
		CompletionTokens: gr.UsageMetadata.CandidatesTokenCount,
		TotalTokens:      gr.UsageMetadata.TotalTokenCount,
	}
	return out, nil
}

// ChatStream 发送流式 SSE 请求。每次调用一个 goroutine；
// 两个 channel 在 goroutine 退出时关闭。ctx 取消中止读取。
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
	httpReq, err := c.buildRequest(ctx, req, true)
	if err != nil {
		return err
	}
	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("gemini: do: %w", err)
	}
	defer resp.Body.Close()
	if err := decodeBody(resp); err != nil {
		return err
	}

	// SSE：每帧 "data: {json}"，空行分隔。
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var (
		usage    llm.Usage
		finish   string
		sentAny  bool
		lastText string
	)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var gr generateResponse
		if err := json.Unmarshal([]byte(payload), &gr); err != nil {
			return fmt.Errorf("gemini: decode sse frame: %w", err)
		}
		if gr.Error != nil {
			return fmt.Errorf("gemini: api error %d %s: %s", gr.Error.Code, gr.Error.Status, gr.Error.Message)
		}
		sentAny = true
		if len(gr.Candidates) > 0 {
			cand := gr.Candidates[0]
			msg := candidateToMessage(cand)
			if msg.Content != "" && msg.Content != lastText {
				// Gemini SSE 流式帧是累积文本（整段重发）；
				// 只发出新增 delta。
				delta := strings.TrimPrefix(msg.Content, lastText)
				lastText = msg.Content
				select {
				case <-ctx.Done():
					return ctx.Err()
				case chunkCh <- llm.StreamChunk{Text: delta}:
				}
			}
			for _, tc := range msg.ToolCalls {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case chunkCh <- llm.StreamChunk{ToolCalls: []llm.ToolCall{tc}}:
				}
			}
			if cand.FinishReason != "" {
				finish = finishReason(cand.FinishReason)
			}
		}
		if gr.UsageMetadata.TotalTokenCount > 0 {
			usage = llm.Usage{
				PromptTokens:     gr.UsageMetadata.PromptTokenCount,
				CompletionTokens: gr.UsageMetadata.CandidatesTokenCount,
				TotalTokens:      gr.UsageMetadata.TotalTokenCount,
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("gemini: scan: %w", err)
	}
	if !sentAny {
		return fmt.Errorf("gemini: empty stream")
	}
	final := llm.StreamChunk{Finish: finish}
	if finish == "" {
		final.Finish = "stop"
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
