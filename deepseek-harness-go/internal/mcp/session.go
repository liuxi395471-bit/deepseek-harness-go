// Package mcp —— 封装 JSON-RPC 传输层的高层会话
// （DESIGN-v2 SD.1）。
//
// MCP 的"高层"操作才是大多数调用者真正需要的：
//   - Initialize 握手
//   - tools/list -> 枚举工具
//   - tools/call -> 调用一个工具
//   - resources/list、prompts/list（尽力而为，缺失时不报错）
//
// mcp.go 中较低层的 Server.Send 只是纯 JSON-RPC。这里的 Session
// 是便捷层，将 MCP 类型映射为 dsh 其余部分无需了解协议细节即可
// 消费的形式。
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Session 用更高层的 MCP 生命周期包装一个 MCP Server。
//
// 生命周期：
//
//	s, err := NewSession(ctx, Config{Command: ...})
//	defer s.Close()
//	tools, err := s.ListTools(ctx)
//	res, err := s.CallTool(ctx, "name", args)
type Session struct {
	srv  *Server
	info ServerInfo
}

// ServerInfo 对应 MCP initialize 结果中的 serverInfo 块。
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Tool 是 MCP 暴露的单个工具的高层表示。
type Tool struct {
	Name        string
	Description string
	// Parameters 是输入 schema（JSON Schema 对象）。
	Parameters json.RawMessage
	// Version 是插件自定义的可选版本字符串。
	Version string
}

// initializeParams 是 initialize 请求的请求体。
type initializeParams struct {
	ProtocolVersion string            `json:"protocolVersion"`
	Capabilities    map[string]any    `json:"capabilities"`
	ClientInfo      map[string]string `json:"clientInfo"`
}

// initializeResult 是 initialize 响应的响应体。
type initializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ServerInfo      ServerInfo     `json:"serverInfo"`
}

// rawTool 是线上返回的单个工具。
type rawTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Version     string          `json:"version,omitempty"`
}

// toolsListResult 对应 MCP tools/list 响应的结构。
type toolsListResult struct {
	Tools []rawTool `json:"tools"`
}

// toolsCallParams 是 tools/call 的请求体。
type toolsCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// toolsCallResult 对应 tools/call 响应的结构。
type toolsCallResult struct {
	Content []contentBlock `json:"content"`
	IsError bool           `json:"isError"`
}

// contentBlock 是工具结果 content 数组中的一个条目。
type contentBlock struct {
	Type string `json:"type"`
	// 文本内容
	Text string `json:"text,omitempty"`
	// 资源内容
	URI      string `json:"uri,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"`
}

// NewSession 启动 MCP 服务器子进程，发送 initialize 握手，
// 并返回一个就绪的 Session。同时启动 Listen goroutine，
// 将响应泵入各 pending 通道。
func NewSession(ctx context.Context, cfg Config) (*Session, error) {
	s, err := NewServer(cfg)
	if err != nil {
		return nil, err
	}
	go s.Listen(ctx)
	return newSessionWithServer(ctx, s)
}

// NewSessionWithServer 是面向测试的构造函数：它接受一个已构建好的
// Server（例如通过 NewServerWithIO 配合 io.Pipe 创建）。
// 它不会替你启动 Listen goroutine；在测试中运行的调用方必须
// 自行启动。
func NewSessionWithServer(ctx context.Context, s *Server) (*Session, error) {
	return newSessionWithServer(ctx, s)
}

func newSessionWithServer(ctx context.Context, s *Server) (*Session, error) {
	// Initialize 握手。我们选择 protocolVersion 2024-11-05，这是
	// 引入工具 API 的版本，大多数现有服务器都在使用。
	// 更旧的服务器会忽略该字段。
	params := initializeParams{
		ProtocolVersion: "2024-11-05",
		Capabilities:    map[string]any{"tools": map[string]any{}},
		ClientInfo:      map[string]string{"name": "dsh", "version": "0.2.2"},
	}
	raw, err := s.Send(ctx, "initialize", params)
	if err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("mcp: initialize: %w", err)
	}
	var ir initializeResult
	if err := json.Unmarshal(raw, &ir); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("mcp: parse initialize result: %w", err)
	}

	// 发送 initialized 通知（不期待响应）。
	if err := s.notify(ctx, "notifications/initialized", map[string]any{}); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("mcp: initialized notification: %w", err)
	}
	return &Session{srv: s, info: ir.ServerInfo}, nil
}

// ServerInfo 返回 initialize 握手中 MCP 服务器的名称/版本。
func (sess *Session) ServerInfo() ServerInfo { return sess.info }

// ListTools 返回 MCP 服务器声明的工具列表。
func (sess *Session) ListTools(ctx context.Context) ([]Tool, error) {
	raw, err := sess.srv.Send(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var lr toolsListResult
	if err := json.Unmarshal(raw, &lr); err != nil {
		return nil, fmt.Errorf("mcp: parse tools/list: %w", err)
	}
	out := make([]Tool, 0, len(lr.Tools))
	for _, rt := range lr.Tools {
		out = append(out, Tool{
			Name:        rt.Name,
			Description: rt.Description,
			Parameters:  rt.InputSchema,
			Version:     rt.Version,
		})
	}
	return out, nil
}

// CallTool 按名称调用一个工具并返回其内容块。
//
// 只有在 MCP 传输本身失败时才返回非 nil 错误。
// 如果工具本身返回 IsError=true，则 err 为 nil，
// 调用方需检查返回的 isError 标志和内容。
func (sess *Session) CallTool(ctx context.Context, name string, args map[string]any) ([]contentBlock, bool, error) {
	raw, err := sess.srv.Send(ctx, "tools/call", toolsCallParams{Name: name, Arguments: args})
	if err != nil {
		return nil, false, err
	}
	var cr toolsCallResult
	if err := json.Unmarshal(raw, &cr); err != nil {
		return nil, false, fmt.Errorf("mcp: parse tools/call: %w", err)
	}
	return cr.Content, cr.IsError, nil
}

// Close 终止 MCP 服务器子进程。
func (sess *Session) Close() error {
	return sess.srv.Close()
}

// ListResources 调用 resources/list。如果服务器不支持资源
// （不支持该能力的 MCP 服务器会返回 method-not-found，我们将其
// 吞掉），则返回空列表且不报错。
func (sess *Session) ListResources(ctx context.Context) ([]map[string]any, error) {
	raw, err := sess.srv.Send(ctx, "resources/list", map[string]any{})
	if err != nil {
		if isMethodNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var out struct {
		Resources []map[string]any `json:"resources"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("mcp: parse resources/list: %w", err)
	}
	return out.Resources, nil
}

// ListPrompts 是 ListResources 的姊妹方法；同样采用吞掉错误的语义。
func (sess *Session) ListPrompts(ctx context.Context) ([]map[string]any, error) {
	raw, err := sess.srv.Send(ctx, "prompts/list", map[string]any{})
	if err != nil {
		if isMethodNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var out struct {
		Prompts []map[string]any `json:"prompts"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("mcp: parse prompts/list: %w", err)
	}
	return out.Prompts, nil
}

// isMethodNotFound 探测 JSON-RPC 错误码（-32601）或
// mcp.go 的 Send 产生错误时包装的 "method not found" 消息。
func isMethodNotFound(err error) bool {
	if err == nil {
		return false
	}
	var je *jsonError
	if errors.As(err, &je) {
		return je.Code == -32601
	}
	msg := err.Error()
	return strings.Contains(msg, "method not found") ||
		strings.Contains(msg, "(code -32601)")
}

// DefaultTimeout 是调用方传入没有截止时间的 ctx 时使用的
// 单次 RPC 超时。便于使用 context.Background() 的测试。
const DefaultTimeout = 30 * time.Second

// WithTimeout 在 ctx 未设置截止时间时为其附加 DefaultTimeout 并返回。
func WithTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, DefaultTimeout)
}
