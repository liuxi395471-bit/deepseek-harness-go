// Package plugin 承载 v2 插件传输层（DESIGN-v2 §B）。
//
// 插件是一个外部子进程，在 TCP 套接字上通过明文 HTTP/2 遵循
// tool_provider.proto 中的 gRPC 契约进行通信。宿主的流程：
//
//  1. 在随机的 localhost 端口上监听。
//  2. 通过 DSH_PLUGIN_ADDR 环境变量把地址传给子进程。
//  3. 通过 gRPC 连接；调用 Specs() 枚举工具。
//  4. 将每个工具包装为 tool.Tool 注册到进程内 Registry。
//  5. 将来自 LLM 的工具调用转发给 Execute()。
//
// 子进程崩溃时，Host 的自动重启循环最多重新拉起 MaxRestarts 次
// （DESIGN-v2 §B.5）。之后，插件工具被标记为不健康，
// 工具调用将以结构化错误失败。
package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	pluginpb "deepseek-harness-go/internal/plugin/proto"
	"deepseek-harness-go/internal/tool"
)

// Loader 将外部工具提供方接入进程内 Registry。
//
// 已弃用：仅为与 v2 调用方保持 ABI 稳定而保留；推荐直接使用
// NewClient，它返回可供调用 Register 的 *Client。
type Loader interface {
	Load(ctx context.Context, addr string) (*tool.Registry, error)
}

// Client 包装到单个插件进程的 gRPC 连接。
//
// 生命周期：
//
//	c, _ := NewClient(ctx, addr)            // 拨号 + 获取 Specs
//	reg := c.Registry(ctx)                  // 构建 tool.Registry
//	c.Close()                              // 释放资源
type Client struct {
	addr    string
	conn    *grpc.ClientConn
	stub    pluginpb.ToolProviderClient
	specs   []*pluginpb.ToolSpec
	closeFn func() error
	mu      sync.Mutex
	closed  bool
}

// NewClient 拨号连接 addr（host:port）处的插件并获取其工具规格。
// 连接是惰性的——第一次 RPC 发生在 Specs() 期间。
//
// addr 示例："127.0.0.1:55001"。
func NewClient(ctx context.Context, addr string, opts ...ClientOption) (*Client, error) {
	cfg := defaultClientConfig()
	for _, o := range opts {
		o(&cfg)
	}
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithTimeout(cfg.dialTimeout),
	)
	if err != nil {
		return nil, fmt.Errorf("plugin: dial %s: %w", addr, err)
	}
	c := &Client{
		addr:    addr,
		conn:    conn,
		stub:    pluginpb.NewToolProviderClient(conn),
		closeFn: conn.Close,
	}
	// 提前获取 specs，使调用方可以在插件行为异常时快速失败。
	// 之后通过 Specs() 注册工具反正也需要它。
	specs, err := c.stub.Specs(ctx, &pluginpb.Empty{})
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("plugin: Specs: %w", err)
	}
	c.specs = specs.GetSpecs()
	return c, nil
}

// Specs 返回插件在拨号时声明的工具。
func (c *Client) Specs() []*pluginpb.ToolSpec {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*pluginpb.ToolSpec, len(c.specs))
	copy(out, c.specs)
	return out
}

// Addr 返回该客户端拨号的网络地址。
func (c *Client) Addr() string { return c.addr }

// Close 释放 gRPC 连接。
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.closeFn != nil {
		return c.closeFn()
	}
	return nil
}

// Register 将插件的工具安装到 reg 中。返回新增工具的数量和已安装
// 的名称（供调用方记录日志）。
//
// 每次 Execute 调用前都会征询提供的 approver（依据 DESIGN-v2 §B.5：
// 插件工具默认需要审批，除非插件设置了
// ToolSpec.requires_approval=false）。
func (c *Client) Register(reg *tool.Registry, approver Approver) (added int, names []string, err error) {
	if approver == nil {
		approver = ApproverFunc(func(_ context.Context, _ Request) (Decision, error) {
			// 未配置 approver -> 生产环境默认拒绝。
			// 测试必须注入允许型 approver。
			return Deny, nil
		})
	}
	for _, spec := range c.specs {
		name := spec.GetName()
		if name == "" {
			continue
		}
		parameters := json.RawMessage(spec.GetParameters())
		t := &grpcTool{
			name:         name,
			description:  spec.GetDescription(),
			parameters:   parameters,
			version:      spec.GetVersion(),
			requiresAppr: spec.GetRequiresApproval(),
			client:       c.stub,
			approver:     approver,
		}
		if err := reg.Register(t); err != nil {
			return added, names, fmt.Errorf("plugin: register %q: %w", name, err)
		}
		added++
		names = append(names, name)
	}
	return added, names, nil
}

// grpcTool 将一个 gRPC 工具适配为 tool.Tool。
type grpcTool struct {
	name, description, version string
	parameters                 json.RawMessage
	requiresAppr               bool
	client                     pluginpb.ToolProviderClient
	approver                   Approver
}

// Name 实现 tool.Tool。
func (t *grpcTool) Name() string { return t.name }

// Description 实现 tool.Tool。
func (t *grpcTool) Description() string { return t.description }

// Parameters 实现 tool.Tool。
func (t *grpcTool) Parameters() any {
	// JSON Schema 对象先反序列化成通用 map 再透传，
	// 使其能干净地经过 json.Marshal 往返。
	var v any
	if len(t.parameters) == 0 {
		// 最小有效 schema
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	if err := json.Unmarshal(t.parameters, &v); err != nil {
		// 回退为字符串形式的字节，让调用方能看到原始 schema。
		return map[string]any{"type": "object", "raw": string(t.parameters)}
	}
	return v
}

// Execute 实现 tool.Tool。
func (t *grpcTool) Execute(ctx context.Context, args json.RawMessage) (tool.Result, error) {
	// 审批关口（DESIGN-v2 §B.5）。只有当插件作者通过
	// ToolSpec.requires_approval=false 显式 opting out 时才跳过。
	if t.requiresAppr {
		dec, err := t.approver.Approve(ctx, Request{
			Tool:   t.name,
			Args:   args,
			Reason: "plugin tool",
		})
		if err != nil {
			return tool.Err("plugin: approver: " + err.Error()), nil
		}
		if dec != ApproveOnce && dec != ApproveSession {
			return tool.Err("plugin: denied by approver"), nil
		}
	}
	callID := newCallID()
	resp, err := t.client.Execute(ctx, &pluginpb.ExecuteRequest{
		Name:     t.name,
		ArgsJson: []byte(args),
		CallId:   callID,
	})
	if err != nil {
		// 将 gRPC status 转换为我们工具错误的表述方式。
		if st, ok := status.FromError(err); ok {
			return tool.Result{
				Content: fmt.Sprintf("plugin: %s: %s", st.Code(), st.Message()),
				IsError: true,
			}, nil
		}
		return tool.Result{Content: "plugin: " + err.Error(), IsError: true}, nil
	}
	return tool.Result{
		Content: resp.GetContent(),
		IsError: resp.GetIsError(),
	}, nil
}

// newCallID 是一个快速的唯一 ID；我们不想仅为此引入 uuid，
// 而是依赖时间 + 计数器。
var (
	callMu sync.Mutex
	callN  int64
)

func newCallID() string {
	callMu.Lock()
	callN++
	n := callN
	callMu.Unlock()
	return fmt.Sprintf("pcall_%d_%d", time.Now().UnixNano(), n)
}

// ----- 客户端选项 ---------------------------------------------------------

// ClientOption 用于配置 Client。
type ClientOption func(*clientConfig)

type clientConfig struct {
	dialTimeout time.Duration
}

func defaultClientConfig() clientConfig {
	return clientConfig{dialTimeout: 10 * time.Second}
}

// WithDialTimeout 覆盖 gRPC 拨号超时（默认 10 秒）。
func WithDialTimeout(d time.Duration) ClientOption {
	return func(c *clientConfig) { c.dialTimeout = d }
}

// ----- 审批（DESIGN-v2 §C.2） ---------------------------------------------
//
// 插件审批委托给与 shell 工具相同的 Approver 接口。插件适配器
// 将 tool.Tool 调用转换为 approval.Request，并遵循
// ApproveOnce / ApproveSession / Deny。

// Decision 通过下方的 Approver 从 approval 包的词汇中重新导出。
type Decision int

const (
	Deny Decision = iota
	ApproveOnce
	ApproveSession
)

// Request 镜像 approval.Request，使本包避免导入循环
// （approval -> tool 允许；tool -> approval 不允许）。
type Request struct {
	Tool   string
	Args   json.RawMessage
	Reason string
}

// Approver 决定一次插件工具调用是否被允许。
type Approver interface {
	Approve(ctx context.Context, req Request) (Decision, error)
}

// ApproverFunc 将普通函数适配为 Approver 接口。
type ApproverFunc func(ctx context.Context, req Request) (Decision, error)

func (f ApproverFunc) Approve(ctx context.Context, req Request) (Decision, error) {
	return f(ctx, req)
}

// ----- 错误 --------------------------------------------------------------

// ErrPluginUnreachable 在 gRPC 拨号失败时返回。
var ErrPluginUnreachable = errors.New("plugin: unreachable")
