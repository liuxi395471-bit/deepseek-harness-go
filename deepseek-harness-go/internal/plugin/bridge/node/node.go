// Package node 提供 Node Bridge 插件宿主（v7 P7-2）。
//
// 通讯：JSON-RPC over stdio，lsp-style framed：
//   - 帧 = "Content-Length: N\r\n\r\n" + body（body 长度 = N 字节）
//   - 每次 Read 一个完整帧才解析
//
// 协议：
//   - "initialize"  → 返回 serverInfo
//   - "tools/list"  → 返回 []ToolSpec
//   - "tools/call"  → 调用 Node 端实现，返回 {content, is_error}
//
// 安全：入口（Node 进程的工作目录 + main 文件）必须位于 installRoot 下；
// 任意越界 → 拒绝加载。
package node

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"deepseek-harness-go/internal/tool"
)

// Manifest 是 package.json 的 dshPlugin 子字段。
type Manifest struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Main        string `json:"main"`         // 入口文件，相对于 packageDir
	Description string `json:"description,omitempty"`
}

// PluginSpec 是 Node 插件提供的工具规格。
type PluginSpec struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters,omitempty"`
}

// rpcRequest / rpcResponse 与 sdk-go JSON-RPC 兼容；这里直接复刻避免
// 跨包依赖。
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("rpc %d: %s", e.Code, e.Message) }

// Bridge 是单个 Node 插件的进程宿主。
type Bridge struct {
	pkgDir     string
	manifest   Manifest
	installRoot string

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr io.ReadCloser

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan *rpcResponse
	pushCh  chan *rpcRequest
	closed  chan struct{}
	closeOnce sync.Once

	tools   []PluginSpec
	alive   atomic.Bool
}

// Load 校验入口在 installRoot 下，然后启动 Node 进程。
func Load(installRoot, pkgDir string) (*Bridge, error) {
	if installRoot == "" {
		return nil, errors.New("node: installRoot empty")
	}
	absRoot, err := filepath.Abs(installRoot)
	if err != nil {
		return nil, err
	}
	absPkg, err := filepath.Abs(pkgDir)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(absRoot, absPkg)
	if err != nil || strings.HasPrefix(rel, "..") || rel == "." {
		return nil, fmt.Errorf("node: pkgDir %s escapes installRoot %s", pkgDir, installRoot)
	}

	// 读取 package.json
	b, err := os.ReadFile(filepath.Join(absPkg, "package.json"))
	if err != nil {
		return nil, err
	}
	var pkg struct {
		DshPlugin Manifest `json:"dshPlugin"`
		Main      string   `json:"main"`
	}
	if err := json.Unmarshal(b, &pkg); err != nil {
		return nil, err
	}
	m := pkg.DshPlugin
	if m.Main == "" {
		m.Main = pkg.Main
	}
	if m.Main == "" {
		return nil, errors.New("node: package.json missing main")
	}

	entry := filepath.Join(absPkg, m.Main)
	if !strings.HasPrefix(entry, absRoot) {
		return nil, errors.New("node: main escapes installRoot")
	}

	cmd := exec.Command("node", entry)
	cmd.Dir = absPkg
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	b2 := &Bridge{
		pkgDir:      absPkg,
		manifest:    m,
		installRoot: absRoot,
		cmd:         cmd,
		stdin:       stdin,
		stdout:      bufio.NewReader(stdout),
		stderr:      stderr,
		pending:     make(map[int64]chan *rpcResponse),
		pushCh:      make(chan *rpcRequest, 32),
		closed:      make(chan struct{}),
	}
	b2.alive.Store(true)

	// initialize + tools/list
	if err := b2.handshake(); err != nil {
		b2.Close()
		return nil, err
	}

	go b2.readPump()
	go b2.drainStderr()
	return b2, nil
}

// Manifest 返回 manifest 副本。
func (b *Bridge) Manifest() Manifest {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.manifest
}

// Tools 返回 tools/list 的结果。
func (b *Bridge) Tools() []PluginSpec {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]PluginSpec, len(b.tools))
	copy(out, b.tools)
	return out
}

// NodePluginTool wraps a Node plugin's tool into a tool.Tool.
type NodePluginTool struct {
	name string
	desc string
	br   *Bridge
}

func (t *NodePluginTool) Name() string        { return t.name }
func (t *NodePluginTool) Description() string { return t.desc }
func (t *NodePluginTool) Parameters() any     { return map[string]any{"type": "object"} }

// AsTools 把 plugin tools 转成 []tool.Tool。
func (b *Bridge) AsTools() []tool.Tool {
	b.mu.Lock()
	specs := make([]PluginSpec, len(b.tools))
	copy(specs, b.tools)
	b.mu.Unlock()
	out := make([]tool.Tool, 0, len(specs))
	for _, sp := range specs {
		spec := sp
		out = append(out, &NodePluginTool{name: spec.Name, desc: spec.Description, br: b})
	}
	return out
}

// Execute implements tool.Tool.
func (t *NodePluginTool) Execute(ctx context.Context, args json.RawMessage) (tool.Result, error) {
	return t.br.Call(ctx, t.name, args)
}

// Call 调用 Node 端的工具实现。
func (b *Bridge) Call(ctx context.Context, name string, args json.RawMessage) (tool.Result, error) {
	if !b.alive.Load() {
		return tool.Err("node plugin not alive"), nil
	}
	params, _ := json.Marshal(map[string]any{"name": name, "args": args})
	resp, err := b.callRPC(ctx, "tools/call", params)
	if err != nil {
		return tool.Err(err.Error()), nil
	}
	var out struct {
		Content string `json:"content"`
		IsError bool   `json:"is_error"`
	}
	if err := json.Unmarshal(resp.Result, &out); err != nil {
		return tool.Err("decode: " + err.Error()), nil
	}
	if out.IsError {
		return tool.Err(out.Content), nil
	}
	return tool.Ok(out.Content), nil
}

// Push 返回 server→client notification（无 id）。
func (b *Bridge) Push() <-chan *rpcRequest { return b.pushCh }

// Close 终止 Node 进程。
func (b *Bridge) Close() error {
	b.closeOnce.Do(func() {
		close(b.closed)
		b.alive.Store(false)
		if b.cmd != nil && b.cmd.Process != nil {
			_ = b.cmd.Process.Kill()
			_, _ = b.cmd.Process.Wait()
		}
	})
	return nil
}

// --- internals ---

func (b *Bridge) handshake() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := b.callRPC(ctx, "initialize", nil)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	if len(resp.Result) == 0 {
		return errors.New("initialize: empty result")
	}
	// tools/list
	resp2, err := b.callRPC(ctx, "tools/list", nil)
	if err != nil {
		return fmt.Errorf("tools/list: %w", err)
	}
	var tools []PluginSpec
	if err := json.Unmarshal(resp2.Result, &tools); err != nil {
		return fmt.Errorf("tools/list decode: %w", err)
	}
	b.mu.Lock()
	b.tools = tools
	b.mu.Unlock()
	return nil
}

func (b *Bridge) callRPC(ctx context.Context, method string, params json.RawMessage) (*rpcResponse, error) {
	id := atomic.AddInt64(&b.nextID, 1)
	req := rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	body, _ := json.Marshal(req)

	ch := make(chan *rpcResponse, 1)
	b.mu.Lock()
	b.pending[id] = ch
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
	}()

	// write frame
	hdr := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	b.mu.Lock()
	w := b.stdin
	b.mu.Unlock()
	if w == nil {
		return nil, errors.New("stdin closed")
	}
	if _, err := io.WriteString(w, hdr); err != nil {
		return nil, err
	}
	if _, err := w.Write(body); err != nil {
		return nil, err
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return resp, resp.Error
		}
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-b.closed:
		return nil, errors.New("node plugin closed")
	}
}

func (b *Bridge) readPump() {
	for {
		select {
		case <-b.closed:
			return
		default:
		}
		body, err := readFrame(b.stdout)
		if err != nil {
			b.Close()
			return
		}
		// 是 Response（有 id）？
		var resp rpcResponse
		if err := json.Unmarshal(body, &resp); err == nil && resp.ID != 0 && resp.JSONRPC == "2.0" {
			b.mu.Lock()
			ch, ok := b.pending[resp.ID]
			b.mu.Unlock()
			if ok {
				ch <- &resp
			}
			continue
		}
		// 否则视为 notification
		var note rpcRequest
		if err := json.Unmarshal(body, &note); err == nil && note.Method != "" {
			select {
			case b.pushCh <- &note:
			default:
			}
		}
	}
}

func (b *Bridge) drainStderr() {
	_, _ = io.Copy(io.Discard, b.stderr)
}

// readFrame 读一个 Content-Length frame。
func readFrame(r *bufio.Reader) ([]byte, error) {
	header := make([]byte, 0, 64)
	for {
		line, err := readLine(r)
		if err != nil {
			return nil, err
		}
		if line == "" {
			break
		}
		header = append(header, line...)
		header = append(header, '\n')
	}
	// parse Content-Length
	var length int
	for _, line := range strings.Split(string(header), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Content-Length:") {
			var n int
			_, _ = fmt.Sscanf(line, "Content-Length: %d", &n)
			length = n
		}
	}
	if length <= 0 {
		return nil, errors.New("frame: missing content-length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
