// Package mcp 实现 MCP（Model Context Protocol）stdio 客户端
// （DESIGN-v2 SD.1）。
//
// MCP 允许 Cursor AI 代理通过 stdio 连接外部工具。
// 本包实现客户端一侧：我们充当 MCP 宿主，通过 stdin/stdout
// 连接到 MCP 服务器进程。
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Config 用于启动一个 MCP 服务器进程的配置。
type Config struct {
	Command      []string
	Env         []string
	StartupTimeout time.Duration
}

// Server 表示一个正在运行的 MCP 服务器进程。
type Server struct {
	cfg Config
	mu  sync.Mutex

	cmd  *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	reqID  int64
	reqMu  sync.Mutex
	pending map[int64]chan pendingResp
}

// NewServer 启动 MCP 服务器进程。
func NewServer(cfg Config) (*Server, error) {
	if cfg.StartupTimeout == 0 {
		cfg.StartupTimeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.StartupTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, cfg.Command[0], cfg.Command[1:]...)
	cmd.Env = append(os.Environ(), cfg.Env...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp: start: %w", err)
	}
	s := &Server{
		cfg:     cfg,
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		pending:  make(map[int64]chan pendingResp),
	}
	return s, nil
}

// NewServerWithIO 构造一个绑定到调用方提供 I/O 的 Server。
// 适用于通过 io.Pipe 管道对在进程内搭建假 MCP 服务器的测试。
// 此模式下 cfg.Command 会被忽略。
func NewServerWithIO(cfg Config, stdin io.WriteCloser, stdout io.ReadCloser) *Server {
	return &Server{
		cfg:     cfg,
		stdin:   stdin,
		stdout:  stdout,
		pending: make(map[int64]chan pendingResp),
	}
}

// pendingResp 保存一个未完成请求的结果。
type pendingResp struct {
	Result json.RawMessage
	Error *jsonError
}

type jsonError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error 实现 error 接口，使 jsonError 可通过
// errors.As(&jsonError{}) 作为可用的错误类型。
func (e *jsonError) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// Send 发送一个 JSON-RPC 请求并等待响应。
// 可安全地并发使用。
func (s *Server) Send(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := s.nextID()
	req := rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	ch := make(chan pendingResp, 1)

	s.reqMu.Lock()
	s.pending[id] = ch
	s.reqMu.Unlock()
	defer func() {
		s.reqMu.Lock()
		delete(s.pending, id)
		s.reqMu.Unlock()
	}()

	enc := json.NewEncoder(s.stdin)
	if err := enc.Encode(req); err != nil {
		return nil, fmt.Errorf("mcp: encode: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			// 包装一层，以便调用方可以通过 errors.As(&jsonError{})
			// 获取结构化的错误码（如 -32601 方法未找到）。
			return nil, &rpcError{
				Method:  method,
				jsonErr: *resp.Error,
			}
		}
		return resp.Result, nil
	}
}

// rpcError 同时携带方法名和底层 JSON-RPC 错误，
// 这样 errors.As(rpcError{}) 就能取出 code/message。
type rpcError struct {
	Method  string
	jsonErr jsonError
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("mcp: %s: %s (code %d)", e.Method, e.jsonErr.Message, e.jsonErr.Code)
}

func (e *rpcError) Unwrap() error { return &e.jsonErr }

// RPCError 是调用方想检查 JSON-RPC 错误时供 errors.As 使用的公开类型。
// 我们暴露这个别名，使该类型存在于包的公开接口中。
type RPCError = rpcError

// rpcRequest 表示一个 JSON-RPC 请求。
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

// notification 表示一个 JSON-RPC 通知（无 ID，无响应）。
type notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

// notify 发送一个 JSON-RPC 通知。与 Send 不同，不附加 ID，
// 且预期服务器不回复。用于 "notifications/initialized" 等
// 生命周期事件。
func (s *Server) notify(_ context.Context, method string, params any) error {
	enc := json.NewEncoder(s.stdin)
	if err := enc.Encode(notification{JSONRPC: "2.0", Method: method, Params: params}); err != nil {
		return fmt.Errorf("mcp: encode notification: %w", err)
	}
	return nil
}

func (s *Server) nextID() int64 {
	s.reqMu.Lock()
	defer s.reqMu.Unlock()
	s.reqID++
	return s.reqID
}

// Close 终止服务器进程。
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stdin != nil {
		s.stdin.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		return s.cmd.Process.Kill()
	}
	return nil
}

// ErrProcessDone 在进程已退出时由 Send 返回。
var ErrProcessDone = errors.New("mcp: process done")

// Listen 启动一个后台 goroutine，从服务器 stdout 读取 JSON-RPC 响应，
// 并将其分发给等待中的请求方。
func (s *Server) Listen(ctx context.Context) {
	dec := json.NewDecoder(s.stdout)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		var resp rpcResponse
		if err := dec.Decode(&resp); err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			continue
		}
		s.reqMu.Lock()
		ch, ok := s.pending[resp.ID]
		s.reqMu.Unlock()
		if ok {
			ch <- pendingResp{Result: resp.Result, Error: resp.Error}
		}
	}
}

// rpcResponse 表示一个 JSON-RPC 响应。
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error *jsonError       `json:"error,omitempty"`
}

// ToolCall 调用 MCP 服务器上的一个工具。
func (s *Server) ToolCall(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	return s.Send(ctx, "tools/call", map[string]any{
		"name": name, "arguments": args,
	})
}
