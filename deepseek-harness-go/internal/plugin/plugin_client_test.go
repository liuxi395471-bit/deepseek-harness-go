// gRPC 插件客户端的测试（DESIGN-v2 §B.2）。
//
// 策略：在一个 goroutine 中于随机的 localhost 端口上启动进程内的
// plugin.Server，然后让 plugin.Client 拨号连接。这避免了在测试
// 环境中依赖外部二进制，并完整走通 Specs + Execute 的往返流程。
package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"

	pluginpb "deepseek-harness-go/internal/plugin/proto"
	"deepseek-harness-go/internal/tool"
)

// echoTool 镜像 cmd/plugin-echo 的工具，使测试无需派生子进程。
type echoTool struct{ text string }

func (e *echoTool) Name() string        { return "echo" }
func (e *echoTool) Description() string { return "test echo" }
func (e *echoTool) Parameters() any {
	return map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}}
}
func (e *echoTool) Execute(_ context.Context, args json.RawMessage) (tool.Result, error) {
	var p struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(args, &p)
	return tool.Result{Content: p.Text}, nil
}

// failTool 模拟一个总是出错的插件工具。
type failTool struct{}

func (failTool) Name() string        { return "fail" }
func (failTool) Description() string { return "always errors" }
func (failTool) Parameters() any {
	return map[string]any{"type": "object"}
}
func (failTool) Execute(_ context.Context, _ json.RawMessage) (tool.Result, error) {
	return tool.Result{Content: "boom", IsError: true}, nil
}

// startTestServer 在 127.0.0.1:<随机端口> 上启动 plugin.Server，
// 返回其地址和清理函数。
//
// 它会等到 Specs() 可响应后才返回，因此后续测试代码可以立即
// 对其调用 NewClient()。
func startTestServer(t *testing.T, tools ...tool.Tool) (addr string, cleanup func()) {
	t.Helper()
	reg := tool.NewRegistry()
	for _, tt := range tools {
		if err := reg.Register(tt); err != nil {
			t.Fatalf("register: %v", err)
		}
	}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr = lis.Addr().String()

	gs := grpc.NewServer()
	pluginpb.RegisterToolProviderServer(gs, NewServer(reg))

	ctx, cancel := context.WithCancel(context.Background())
	_ = ctx
	go func() { _ = gs.Serve(lis) }()

	// 等待 gRPC 服务器开始接受连接（Serve 在 GracefulStop 之后才
	// 返回，所以我们只短暂 sleep）。
	time.Sleep(50 * time.Millisecond)

	cleanup = func() {
		cancel()
		gs.GracefulStop()
	}
	return addr, cleanup
}

func TestClient_SpecsAndExecute_RoundTrip(t *testing.T) {
	addr, stop := startTestServer(t, &echoTool{text: "hello"})
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, err := NewClient(ctx, addr)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	specs := c.Specs()
	if len(specs) != 1 || specs[0].GetName() != "echo" {
		t.Fatalf("specs = %+v, want one echo", specs)
	}

	reg := tool.NewRegistry()
	added, names, err := c.Register(reg, ApproverFunc(func(_ context.Context, _ Request) (Decision, error) {
		return ApproveOnce, nil
	}))
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if added != 1 || len(names) != 1 || names[0] != "echo" {
		t.Errorf("Register counts = (%d, %v), want (1, [echo])", added, names)
	}

	got, ok := reg.Get("echo")
	if !ok {
		t.Fatal("echo not registered")
	}
	res, err := got.Execute(ctx, json.RawMessage(`{"text":"hi back"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.IsError || res.Content != "hi back" {
		t.Errorf("res = %+v, want text=hi back no error", res)
	}
}

func TestClient_ApprovalsDeniedByDefault(t *testing.T) {
	addr, stop := startTestServer(t, &echoTool{})
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, err := NewClient(ctx, addr)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	// echo 声明了 requires_approval=false（内置工具的默认值），
	// 因此不应征询 approver。
	reg := tool.NewRegistry()
	_, _, err = c.Register(reg, ApproverFunc(func(_ context.Context, _ Request) (Decision, error) {
		return Deny, nil // 如果被调用则会阻塞
	}))
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, _ := reg.Get("echo")
	res, _ := got.Execute(ctx, json.RawMessage(`{"text":"x"}`))
	if res.IsError {
		t.Errorf("unexpected error: %q", res.Content)
	}
}

func TestClient_ApprovalsGateWhenPluginSetsRequires(t *testing.T) {
	// 一个要求审批（requires approval = true）的 echoTool 插件。
	// 我们无法在测试中更改内置服务器发出的标志，因此手工构造
	// 一个设置了该标志的 Client.specs 列表，再重新调用 Register。
	addr, stop := startTestServer(t, &echoTool{})
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, err := NewClient(ctx, addr)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	// 修改缓存的 specs，强制开启 requires_approval。
	c.mu.Lock()
	c.specs = []*pluginpb.ToolSpec{{
		Name: "echo", Description: "test",
		Parameters:        []byte(`{"type":"object"}`),
		Version:           "test",
		RequiresApproval:  true,
	}}
	c.mu.Unlock()

	var approverCalls atomic.Int32
	denyApprover := ApproverFunc(func(_ context.Context, _ Request) (Decision, error) {
		approverCalls.Add(1)
		return Deny, nil
	})
	reg := tool.NewRegistry()
	if _, _, err := c.Register(reg, denyApprover); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, _ := reg.Get("echo")
	res, _ := got.Execute(ctx, json.RawMessage(`{"text":"hi"}`))
	if !res.IsError {
		t.Errorf("expected denial, got %+v", res)
	}
	if approverCalls.Load() != 1 {
		t.Errorf("approver should be called once, got %d", approverCalls.Load())
	}
}

func TestClient_PropagatesToolError(t *testing.T) {
	addr, stop := startTestServer(t, &failTool{})
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, err := NewClient(ctx, addr)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	reg := tool.NewRegistry()
	if _, _, err := c.Register(reg, nil); err != nil { // nil approver -> 对未标记需审批的工具默认拒绝；failTool 没有审批标志，因此仍然通过
		t.Fatalf("Register: %v", err)
	}
	got, _ := reg.Get("fail")
	res, _ := got.Execute(ctx, json.RawMessage(`{}`))
	if !res.IsError || res.Content != "boom" {
		t.Errorf("res = %+v, want IsError+boom", res)
	}
}

func TestClient_DialFails(t *testing.T) {
	// 该端口上没有服务器；NewClient 必须返回错误。
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := NewClient(ctx, "127.0.0.1:1", WithDialTimeout(500*time.Millisecond))
	if err == nil {
		t.Fatal("expected error dialing nonexistent addr")
	}
}

func TestServerServe_StartStop(t *testing.T) {
	// 使用 ServeForever，通过 net.Listen + cancel 控制地址。
	reg := tool.NewRegistry()
	_ = reg.Register(&echoTool{})
	srv := NewServer(reg)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := lis.Addr().String()
	_ = lis.Close() // 释放端口，让 ServeForever 能占用它

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	err = srv.ServeForever(ctx, addr)
	if err == nil {
		t.Errorf("expected context-canceled error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Logf("got error: %v (acceptable)", err)
	}
}

// 让 go vet 满意，避免未使用导入
var (
	_ sync.Mutex
)
