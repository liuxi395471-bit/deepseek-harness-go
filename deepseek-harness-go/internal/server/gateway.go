// Package server — 统一 Gateway SSE（v4 §B）。
//
// gateway.go 实现 /api/gateway/stream 单一入口的请求/响应 DTO
// 和 source router。所有流式 / 一次性请求都通过同一个 SSE 入口，
// 由 source 名字路由到对应 handler。
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// GatewayRequest 是 /api/gateway/stream 的请求体。
//
// Source 决定调用哪个 handler；Params 是 handler 特定的 JSON 参数。
// 例如 `{"source":"session.send","params":{"sid":"abc","prompt":"hi"}}`。
type GatewayRequest struct {
	Source string          `json:"source"`
	Params json.RawMessage `json:"params,omitempty"`
}

// GatewayEvent 是 /api/gateway/stream 输出的一帧。
//
// Source 与请求的 source 字段一致，便于客户端过滤多 source 复用。
// Type 是 "delta"（增量）/ "final"（终止）/ "error"（异常）。
type GatewayEvent struct {
	Source    string          `json:"source"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Timestamp time.Time       `json:"ts"`
}

// SourceHandler 处理单个 source。Handler 通过 out 写出 GatewayEvent
// 帧并返回。返回 error 即代表该 source 终止（产生 error 帧）。
//
// 契约：
//   - 必须在 ctx 取消或 handler 返回后立即停止向 out 写
//   - 不应保留 out 引用——一旦返回，调用方 close(out)
type SourceHandler func(ctx context.Context, req GatewayRequest, out chan<- GatewayEvent) error

// Router 持有 source name → handler 的注册表。
//
// 并发安全：注册通常在 server 启动时一次完成；如运行时注册，
// 调用方需自行加锁（router 内部不加锁以保持零依赖）。
type Router struct {
	sources map[string]SourceHandler
}

// NewRouter 构造空 router。
func NewRouter() *Router {
	return &Router{sources: make(map[string]SourceHandler)}
}

// Register 注册 source handler。同名重复注册将覆盖并返回前一个。
func (r *Router) Register(name string, h SourceHandler) (previous SourceHandler) {
	previous = r.sources[name]
	r.sources[name] = h
	return previous
}

// Sources 返回已注册 source 名字的有序切片（便于调试 / 文档）。
func (r *Router) Sources() []string {
	out := make([]string, 0, len(r.sources))
	for k := range r.sources {
		out = append(out, k)
	}
	return out
}

// ErrUnknownSource 在 source 名字未注册时由 Dispatch 返回。
var ErrUnknownSource = errors.New("gateway: unknown source")

// Dispatch 解析 source 名字并调用对应 handler。ctx 取消时 handler
// 应立即返回；out 由调用方负责 close。
func (r *Router) Dispatch(ctx context.Context, req GatewayRequest, out chan<- GatewayEvent) error {
	h, ok := r.sources[req.Source]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownSource, req.Source)
	}
	return h(ctx, req, out)
}

// writeGatewayFrame 写出单个 GatewayEvent 到 w。flusher 用于立刻
// flush SSE 缓冲。空 Payload 视为 null。
func writeGatewayFrame(w io.Writer, flusher interface{ Flush() }, ev GatewayEvent) error {
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	b, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("gateway: marshal event: %w", err)
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}