// Package jsonrpc 提供 sdk-go 的 JSON-RPC over WebSocket 客户端。
//
// 协议设计：
//   - Request : {"jsonrpc":"2.0","id":N,"method":"...","params":{...}}
//   - Response: {"jsonrpc":"2.0","id":N,"result":...} 或 {"error":{...}}
//   - Notification (服务端→客户端, id 缺失) 视为 Push 事件。
//
// v7.0 仅暴露在 net 包级别的 wsConn 接口；底层 transport 由调用方
// 注入（避免强耦合 gorilla/websocket 的依赖选择）。
package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

// Transport 是 JSON-RPC 传输抽象（WebSocket / Unix socket / Pipe 都可）。
//
// 行为：
//   - Recv 在连接关闭 / 错误时返回 error；
//   - Send 在写入失败时返回 error；
//   - 收发都是 framed JSON（行分隔或 length-prefixed 均可，由 transport 决定）。
type Transport interface {
	Send(ctx context.Context, data []byte) error
	Recv(ctx context.Context) ([]byte, error)
	Close() error
}

// Client 是 JSON-RPC 2.0 客户端。
type Client struct {
	tr      Transport
	nextID  int64
	mu      sync.Mutex
	pending map[int64]chan *Response
	pushCh  chan *Request
	closed  chan struct{}
	closeOnce sync.Once
}

// New 构造客户端；立刻启动后台 read pump。
func New(tr Transport) *Client {
	c := &Client{
		tr:      tr,
		pending: make(map[int64]chan *Response),
		pushCh:  make(chan *Request, 32),
		closed:  make(chan struct{}),
	}
	go c.readPump()
	return c
}

// Push 返回服务端 push 通知的 channel；连接关闭后 channel 关闭。
func (c *Client) Push() <-chan *Request {
	return c.pushCh
}

// Close 关闭客户端（终止 read pump）。
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
	})
	return c.tr.Close()
}

// Call 同步调用方法；ctx 控制超时。
func (c *Client) Call(ctx context.Context, method string, params any) (*Response, error) {
	id := atomic.AddInt64(&c.nextID, 1)
	req := &Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	respCh := make(chan *Response, 1)
	c.mu.Lock()
	c.pending[id] = respCh
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	if err := c.tr.Send(ctx, body); err != nil {
		return nil, fmt.Errorf("jsonrpc: send: %w", err)
	}
	select {
	case resp := <-respCh:
		if resp.Error != nil {
			return resp, resp.Error
		}
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closed:
		return nil, errors.New("jsonrpc: client closed")
	}
}

// Notify 异步通知（无 response）。
func (c *Client) Notify(ctx context.Context, method string, params any) error {
	req := &Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return c.tr.Send(ctx, body)
}

// readPump 把 transport 的帧分发给 pending 或 push channel。
func (c *Client) readPump() {
	for {
		select {
		case <-c.closed:
			return
		default:
		}
		data, err := c.tr.Recv(context.Background())
		if err != nil {
			c.Close()
			return
		}
		// 先尝试解析为 Response（有 id 且非 0）。
		var resp Response
		if err := json.Unmarshal(data, &resp); err == nil && resp.ID != 0 && resp.JSONRPC != "" {
			c.mu.Lock()
			ch, ok := c.pending[resp.ID]
			c.mu.Unlock()
			if ok {
				ch <- &resp
			}
			continue
		}
		// 否则视为 Notification
		var note Request
		if err := json.Unmarshal(data, &note); err == nil && note.Method != "" {
			select {
			case c.pushCh <- &note:
			default:
				// drop if buffer full
			}
		}
	}
}

// Request 是 JSON-RPC 请求。
type Request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// Response 是 JSON-RPC 响应。
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError 描述 RPC 错误。
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Error 实现 error 接口。
func (e *RPCError) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}
