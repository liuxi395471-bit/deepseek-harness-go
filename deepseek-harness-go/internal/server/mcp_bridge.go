// Package server — MCP JSON-RPC → Gateway source 桥接（v7 P7-3）。
//
// MCP 协议（stdio/HTTP/SSE 都用）：
//   - request  : {"jsonrpc":"2.0","id":N,"method":"tools/list",...}
//   - response : {"jsonrpc":"2.0","id":N,"result":{...}} 或 {"error":{...}}
//
// Gateway 协议：
//   - source   : 字符串 (如 "tools.list")
//   - params   : 任意 JSON
//
// 桥接：把 MCP JSON-RPC 直接映射为 Gateway source（method → source）。
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// allowedMCPSource 把 MCP method 名 → Gateway source 名。
//
// MCP 协议：tools/list、tools/call（slash）；
// Gateway 已注册：tools.list、tools.call（dot）。
var allowedMCPSource = map[string]string{
	"tools/list":   "tools.list",
	"tools/call":   "tools.call",
	"plugins/list": "plugins.list",
}

// 把 MCP method 映射到 Gateway source。
//
// 允许两种写法：
//   - MCP 风格：tools/list / tools/call / plugins/list
//   - 兼容 dot：tools.list / tools.call / plugins.list
func mcpMethodToGatewaySource(method string) (string, bool) {
	if s, ok := allowedMCPSource[method]; ok {
		return s, true
	}
	if _, known := allowedMCPSource[strings.ReplaceAll(method, ".", "/")]; known {
		return method, true
	}
	return "", false
}

// MCPDispatcher 把 MCP HTTP POST 帧转给 GatewayRouter，组装 JSON-RPC 响应。
//
// 返回 []byte 是 JSON-RPC 响应（成功 result / 失败 error）。
func MCPDispatcher(router *Router) func(ctx context.Context, req []byte) ([]byte, error) {
	return func(ctx context.Context, body []byte) ([]byte, error) {
		var rpcReq struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      *int64          `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(body, &rpcReq); err != nil {
			return jsonRPCErrorResp(nil, -32700, "parse error: "+err.Error()), nil
		}
		if rpcReq.Method == "" {
			return jsonRPCErrorResp(rpcReq.ID, -32600, "method required"), nil
		}
		gwSource, ok := mcpMethodToGatewaySource(rpcReq.Method)
		if !ok {
			return jsonRPCErrorResp(rpcReq.ID, -32601, "method not bridged: "+rpcReq.Method), nil
		}
		events, err := router.DispatchSync(ctx, GatewayRequest{
			Source: gwSource,
			Params: rpcReq.Params,
		})
		if err != nil {
			return jsonRPCErrorResp(rpcReq.ID, -32603, err.Error()), nil
		}
		// 拼接 result：取最后一条非 error 事件的 payload。
		var result json.RawMessage = json.RawMessage("null")
		for _, ev := range events {
			if ev.Type == "error" && len(ev.Payload) > 0 {
				return jsonRPCErrorResp(rpcReq.ID, -32000, string(ev.Payload)), nil
			}
			if len(ev.Payload) > 0 {
				result = ev.Payload
			}
		}
		return jsonRPCResultResp(rpcReq.ID, result), nil
	}
}

func jsonRPCResultResp(id *int64, result json.RawMessage) []byte {
	if id == nil {
		return []byte(fmt.Sprintf(`{"jsonrpc":"2.0","result":%s}`, string(result)))
	}
	return []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":%s}`, *id, string(result)))
}

func jsonRPCErrorResp(id *int64, code int, msg string) []byte {
	if id == nil {
		return []byte(fmt.Sprintf(`{"jsonrpc":"2.0","error":{"code":%d,"message":%q}}`, code, msg))
	}
	return []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"error":{"code":%d,"message":%q}}`, *id, code, msg))
}
