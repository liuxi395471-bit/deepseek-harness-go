// Package plugin —— 内置插件服务器脚手架（DESIGN-v2 §B.3）。
//
// 内置插件以子命令的形式存在于 dsh 二进制中，在 exec 进入 gRPC
// 服务循环之前，通过 NewServer().Main() 注册其工具。示例：
//
//	cmd/plugin-echo/main.go 使用 plugin.NewServer() 注册一个
//	echo 工具，然后调用 ServeForever()。
//
// Server 将 tool.Registry 包装为 gRPC ToolProvider 契约，
// 使插件作者不必编写 protobuf 胶水代码。
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net"

	"google.golang.org/grpc"

	pluginpb "deepseek-harness-go/internal/plugin/proto"
	"deepseek-harness-go/internal/tool"
)

// Server 将进程内 tool.Registry 适配为 gRPC ToolProvider。
type Server struct {
	pluginpb.UnimplementedToolProviderServer
	registry *tool.Registry
}

// NewServer 包装 reg。返回的服务器可以在通过 DSH_PLUGIN_ADDR
// 传入的地址上（宿主约定）进行 ServeForever。
func NewServer(reg *tool.Registry) *Server {
	return &Server{registry: reg}
}

// Specs 实现 pluginpb.ToolProviderServer。
func (s *Server) Specs(_ context.Context, _ *pluginpb.Empty) (*pluginpb.ToolList, error) {
	specs := s.registry.Specs()
	out := make([]*pluginpb.ToolSpec, 0, len(specs))
	for _, sp := range specs {
		params, err := json.Marshal(sp.Function.Parameters)
		if err != nil {
			return nil, fmt.Errorf("plugin server: marshal params for %s: %w", sp.Function.Name, err)
		}
		out = append(out, &pluginpb.ToolSpec{
			Name:        sp.Function.Name,
			Description: sp.Function.Description,
			Parameters:  params,
			Version:     "1.0.0",
			// 内置工具是可信的 → 默认跳过审批。
			// 作者如有需要可针对单个工具开启。
			RequiresApproval: false,
		})
	}
	return &pluginpb.ToolList{Specs: out}, nil
}

// Execute 实现 pluginpb.ToolProviderServer。
func (s *Server) Execute(ctx context.Context, req *pluginpb.ExecuteRequest) (*pluginpb.ExecuteResponse, error) {
	t, ok := s.registry.Get(req.GetName())
	if !ok {
		return &pluginpb.ExecuteResponse{
			Content: fmt.Sprintf("plugin server: tool %q not found", req.GetName()),
			IsError: true,
		}, nil
	}
	args := req.GetArgsJson()
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	res, err := t.Execute(ctx, args)
	if err != nil {
		return &pluginpb.ExecuteResponse{
			Content: "plugin server: " + err.Error(),
			IsError: true,
		}, nil
	}
	return &pluginpb.ExecuteResponse{
		Content: res.Content,
		IsError: res.IsError,
	}, nil
}

// ServeForever 从 DSH_PLUGIN_ADDR 获取地址，启动绑定到该地址的
// gRPC 服务器，并阻塞直到 ctx 被取消。
//
// 约定（与 Host.launch 一致）：
//   - 环境变量 DSH_PLUGIN_ADDR = "127.0.0.1:NNN"
//   - 插件监听该地址。
//
// 创建监听器，构造 gRPC.NewServer，注册 ToolProviderServer，
// 然后 Serve() 阻塞。ctx 取消时，服务器被优雅停止
// （5 秒宽限期）。
func (s *Server) ServeForever(ctx context.Context, addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("plugin server: listen %s: %w", addr, err)
	}
	gs := grpc.NewServer()
	pluginpb.RegisterToolProviderServer(gs, s)

	// ctx 取消时优雅关闭。
	errCh := make(chan error, 1)
	go func() {
		errCh <- gs.Serve(lis)
	}()
	select {
	case <-ctx.Done():
		stopped := make(chan struct{})
		go func() {
			gs.GracefulStop()
			close(stopped)
		}()
		<-stopped
		return ctx.Err()
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("plugin server: serve: %w", err)
		}
	}
	return nil
}
