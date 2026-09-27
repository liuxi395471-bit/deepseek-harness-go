// Package lsp 提供 LSP（Language Server Protocol）客户端（v7 P7-7）。
//
// 设计：
//   - Client 与外部 LSP server（gopls / tsserver 等）通信；
//   - 通讯：JSON-RPC 2.0 over stdio，Content-Length framed；
//   - 支持能力：initialize / hover / references / definition / shutdown。
//
// 安全：仅 stdio；入口命令必须由调用方在 main.go 装配时显式配置。
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
)

// Position 是 LSP 标准的 (line, character) 0-based 坐标。
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Location 是文件路径 + 范围。
type Location struct {
	URI   string `json:"uri"`
	Range struct {
		Start Position `json:"start"`
		End   Position `json:"end"`
	} `json:"range"`
}

// Hover 表示悬停信息。
//
// LSP spec 中 contents 可能是 string / MarkedString / MarkupContent。
// 为简单起见 v7.0 把它当作 string；调用方自行 .(string) 转换。
type Hover struct {
	Contents any `json:"contents"`
	Range    *struct {
		Start Position `json:"start"`
		End   Position `json:"end"`
	} `json:"range,omitempty"`
}

// Client 是单个 LSP server 进程宿主。
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan *json.RawMessage
	closed  chan struct{}
	closeOnce sync.Once
}

// Launch 启动 LSP server；command 是可执行（如 "gopls"），args 是其命令行参数。
func Launch(command string, args ...string) (*Client, error) {
	if command == "" {
		return nil, errors.New("lsp: empty command")
	}
	cmd := exec.Command(command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("lsp: start: %w", err)
	}
	c := &Client{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewReader(stdout),
		pending: make(map[int64]chan *json.RawMessage),
		closed:  make(chan struct{}),
	}
	go c.readPump()
	return c, nil
}

// Close 关闭 client（kill 子进程）。
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
		if c.cmd != nil && c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
			_, _ = c.cmd.Process.Wait()
		}
	})
	return nil
}

// Initialize 发送 initialize 请求。
func (c *Client) Initialize(ctx context.Context, rootURI string) error {
	params := map[string]any{
		"processId": nil,
		"rootUri":   rootURI,
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"hover": map[string]any{
					"contentFormat": []string{"plaintext"},
				},
			},
		},
	}
	_, err := c.call(ctx, "initialize", params)
	return err
}

// Hover 在指定位置查询 hover。
func (c *Client) Hover(ctx context.Context, uri string, pos Position) (*Hover, error) {
	params := map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
	}
	raw, err := c.call(ctx, "textDocument/hover", params)
	if err != nil {
		return nil, err
	}
	if len(*raw) == 0 {
		return nil, nil
	}
	var h Hover
	if err := json.Unmarshal(*raw, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

// References 查询 references。
func (c *Client) References(ctx context.Context, uri string, pos Position) ([]Location, error) {
	params := map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
		"context":      map[string]any{"includeDeclaration": true},
	}
	raw, err := c.call(ctx, "textDocument/references", params)
	if err != nil {
		return nil, err
	}
	if len(*raw) == 0 {
		return nil, nil
	}
	var locs []Location
	if err := json.Unmarshal(*raw, &locs); err != nil {
		return nil, err
	}
	return locs, nil
}

// Definition 查询 definition。
func (c *Client) Definition(ctx context.Context, uri string, pos Position) ([]Location, error) {
	params := map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
	}
	raw, err := c.call(ctx, "textDocument/definition", params)
	if err != nil {
		return nil, err
	}
	if len(*raw) == 0 {
		return nil, nil
	}
	var locs []Location
	if err := json.Unmarshal(*raw, &locs); err != nil {
		return nil, err
	}
	return locs, nil
}

// Shutdown 通知 server 即将关闭。
func (c *Client) Shutdown(ctx context.Context) error {
	_, err := c.call(ctx, "shutdown", nil)
	return err
}

// --- internals ---

func (c *Client) call(ctx context.Context, method string, params any) (*json.RawMessage, error) {
	id := atomic.AddInt64(&c.nextID, 1)
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	body, _ := json.Marshal(req)
	ch := make(chan *json.RawMessage, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	// write frame
	hdr := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	if _, err := io.WriteString(c.stdin, hdr); err != nil {
		return nil, err
	}
	if _, err := c.stdin.Write(body); err != nil {
		return nil, err
	}

	select {
	case raw := <-ch:
		return raw, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closed:
		return nil, errors.New("lsp: client closed")
	}
}

func (c *Client) readPump() {
	for {
		select {
		case <-c.closed:
			return
		default:
		}
		body, err := readFrame(c.stdout)
		if err != nil {
			c.Close()
			return
		}
		var resp struct {
			ID     int64           `json:"id"`
			Result json.RawMessage `json:"result,omitempty"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			continue
		}
		if resp.ID == 0 {
			// notification — ignore for v7.0
			continue
		}
		c.mu.Lock()
		ch, ok := c.pending[resp.ID]
		c.mu.Unlock()
		if ok {
			ch <- &resp.Result
		}
	}
}

func readFrame(r io.Reader) ([]byte, error) {
	br := bufio.NewReader(r)
	var hdrBuf strings.Builder
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		hdrBuf.WriteString(line)
		if line == "\n" || line == "\r\n" {
			break
		}
	}
	var length int
	for _, ln := range strings.Split(hdrBuf.String(), "\n") {
		ln = strings.TrimSpace(strings.TrimRight(ln, "\r"))
		if strings.HasPrefix(ln, "Content-Length:") {
			fmt.Sscanf(strings.TrimPrefix(ln, "Content-Length:"), "%d", &length)
		}
	}
	if length <= 0 {
		return nil, errors.New("lsp: missing content-length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(br, body); err != nil {
		return nil, err
	}
	return body, nil
}
