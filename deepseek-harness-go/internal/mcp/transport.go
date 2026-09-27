// Package mcp 提供 MCP 多 transport（v7 P7-3）。
//
// 新增 transport：
//   - HTTPTransport : POST /mcp (application/json)  → JSON-RPC
//   - SSETransport  : GET  /mcp/sse (SSE) + POST /mcp/sse (上行)
//
// stdio 仍由 v2 既有 Server 承载；本包仅扩展 transport 层。
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// HTTPTransport 用 HTTP POST 收 JSON-RPC 请求，返回 JSON-RPC 响应。
//
// 不保持长连接；每请求独立。适合 IDE / Cursor 这类"用完即走"客户端。
type HTTPTransport struct {
	BaseURL string
	Token   string // optional Bearer
	Client  *http.Client
}

// NewHTTPTransport 构造。
func NewHTTPTransport(base string) *HTTPTransport {
	return &HTTPTransport{BaseURL: strings.TrimRight(base, "/"), Client: &http.Client{}}
}

// Send 通过 HTTP POST 发送请求。
func (h *HTTPTransport) Send(ctx context.Context, req []byte) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "POST", h.BaseURL+"/mcp", bytes.NewReader(req))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if h.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+h.Token)
	}
	resp, err := h.Client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mcp http: status=%d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// SSETransport 用 SSE 长连接接收 server 主动推送，并提供 POST 上行。
//
// 行为：
//   - 客户端 GET /mcp/sse → 服务端推 "event: message\ndata: <json>\n\n" 帧；
//   - 客户端 POST /mcp/sse (application/json) → 单次请求响应（用于响应 server notification）。
type SSETransport struct {
	BaseURL string
	Token   string
	Client  *http.Client

	out  chan []byte
	in   chan []byte
	done chan struct{}
}

// NewSSETransport 构造。
func NewSSETransport(base string) *SSETransport {
	return &SSETransport{
		BaseURL: strings.TrimRight(base, "/"),
		Client:  &http.Client{},
		out:     make(chan []byte, 64),
		in:      make(chan []byte, 16),
		done:    make(chan struct{}),
	}
}

// Connect 建立 SSE 长连接；返回 error。
//
// 调用方应另起 goroutine 调 Recv() 处理推流；调用 Send() 提交请求。
func (s *SSETransport) Connect(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/mcp/sse", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	if s.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.Token)
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return fmt.Errorf("mcp sse connect: status=%d", resp.StatusCode)
	}
	go s.readLoop(resp.Body)
	return nil
}

func (s *SSETransport) readLoop(r io.Reader) {
	defer close(s.done)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	var data strings.Builder
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if data.Len() > 0 {
				s.out <- []byte(data.String())
				data.Reset()
			}
		case strings.HasPrefix(line, "data: "):
			data.WriteString(strings.TrimPrefix(line, "data: "))
		}
	}
}

// Recv 从 SSE 流读一条帧（server→client 推）。
func (s *SSETransport) Recv(ctx context.Context) ([]byte, error) {
	select {
	case data := <-s.out:
		return data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.done:
		return nil, errors.New("mcp sse: disconnected")
	}
}

// Send 通过 POST 提交单次 JSON-RPC 请求。
func (s *SSETransport) Send(ctx context.Context, req []byte) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/mcp/sse", bytes.NewReader(req))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if s.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+s.Token)
	}
	resp, err := s.Client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mcp sse send: status=%d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// Close 终止 SSE 长连接。
func (s *SSETransport) Close() error {
	if s.Client != nil {
		s.Client.CloseIdleConnections()
	}
	return nil
}

// jsonRPC 是最小请求 / 响应类型（不依赖 sdk-go，避免循环）。
type jsonRPC struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// SSEServerHandler 把 SSE + POST 接入 dsh 内的 RPC dispatcher。
//
// dispatcher 收到 HTTP GET 帧 → 调用 onPush(数据) 返回响应（可选）；
// 收到 HTTP POST 帧 → 调用 dispatch(请求) 返回响应。
type SSEServerHandler struct {
	dispatch func(ctx context.Context, req []byte) ([]byte, error)
	onPush   func(ctx context.Context, push []byte) error // optional, returns nil to keep connection alive
}

// NewSSEServerHandler 构造；dispatch 是 RPC 调用入口。
func NewSSEServerHandler(dispatch func(ctx context.Context, req []byte) ([]byte, error)) *SSEServerHandler {
	return &SSEServerHandler{dispatch: dispatch}
}

// SetPushHandler 设置可选的 push handler（用于 server→client 主动推送）。
func (h *SSEServerHandler) SetPushHandler(f func(ctx context.Context, push []byte) error) {
	h.onPush = f
}

// ServeHTTP 把 handler 接入 http.Server。
//
//   GET /mcp/sse    → SSE 推
//   POST /mcp/sse   → JSON-RPC 请求响应
//   POST /mcp       → 直接 JSON-RPC（HTTP transport 端点）
func (h *SSEServerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/mcp/sse":
		h.serveSSE(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/mcp/sse":
		h.servePost(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/mcp":
		h.servePost(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *SSEServerHandler) servePost(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resp, err := h.dispatch(r.Context(), body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(resp)
}

func (h *SSEServerHandler) serveSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)
	if flusher == nil {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	// 简单的 "ready" 帧让客户端确认连接
	_, _ = w.Write([]byte("event: ready\ndata: ok\n\n"))
	flusher.Flush()

	<-r.Context().Done()
}
