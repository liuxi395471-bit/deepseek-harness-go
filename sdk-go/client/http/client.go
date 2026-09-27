// Package http 提供 sdk-go 的 HTTP 客户端。
//
// 通讯模式：
//   - session.send : POST /v1/session/send
//   - events.subscribe : GET /v1/events?sid=... （SSE 流）
//   - task.submit / task.cancel 等 : 与 Gateway source 一一对应
//
// 不引入 gorilla/websocket 之类的三方依赖（v7.0 仅 HTTP/SSE）。
// 未来 v7.1 可加 WS 客户端。
package http

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"deepseek-harness-all/sdk-go/types"
)

// Client 是 SDK 的 HTTP 入口。
type Client struct {
	base string // e.g. http://127.0.0.1:7777
	hc   *http.Client
	token string // optional Bearer
}

// New 构造客户端；base 是 ds-go Gateway 的根地址。
func New(base string) *Client {
	return &Client{
		base: strings.TrimRight(base, "/"),
		hc:   &http.Client{Timeout: 30 * time.Second},
	}
}

// WithToken 设置 Bearer token（从 v5 credentials 注入）。
func (c *Client) WithToken(token string) *Client {
	c.token = token
	return c
}

// SendSession 提交一轮 prompt。
func (c *Client) SendSession(ctx context.Context, req types.SessionSendRequest) (*types.SessionSendResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, "POST", "/v1/session/send", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sdk: send status=%d", resp.StatusCode)
	}
	var out types.SessionSendResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SubscribeEvents 订阅一个 session 的事件流（SSE）。
//
// 每行 data: <json> 触发一次 callback；流关闭或 ctx 取消时返回。
func (c *Client) SubscribeEvents(ctx context.Context, sessionID string, cb func(types.Frame) error) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/v1/events?sid="+sessionID, nil)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sdk: subscribe status=%d", resp.StatusCode)
	}
	return readSSE(resp.Body, cb)
}

// Permission 提交审批决策（POST /v1/permission）。
func (c *Client) Permission(ctx context.Context, d types.PermissionDecision) error {
	body, _ := json.Marshal(d)
	resp, err := c.do(ctx, "POST", "/v1/permission", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sdk: permission status=%d", resp.StatusCode)
	}
	return nil
}

func (c *Client) do(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Content-Type", "application/json")
	return c.hc.Do(req)
}

func readSSE(r io.Reader, cb func(types.Frame) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	var data strings.Builder
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			if data.Len() > 0 {
				var f types.Frame
				if err := json.Unmarshal([]byte(data.String()), &f); err != nil {
					// skip malformed frame
					data.Reset()
					continue
				}
				if err := cb(f); err != nil {
					return err
				}
				data.Reset()
			}
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			data.WriteString(strings.TrimPrefix(line, "data: "))
		}
	}
	return sc.Err()
}
