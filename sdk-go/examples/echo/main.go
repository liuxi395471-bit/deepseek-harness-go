// echo 是 sdk-go 的最小 demo：连接到 ds-go Gateway，订阅事件流。
//
// 用法：
//
//	go run ./examples/echo --addr http://127.0.0.1:7777 --sid demo
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"deepseek-harness-all/sdk-go/client/http"
	"deepseek-harness-all/sdk-go/types"
)

func main() {
	addr := flag.String("addr", "http://127.0.0.1:7777", "ds-go Gateway base URL")
	sid := flag.String("sid", "demo-session", "session id to subscribe")
	prompt := flag.String("prompt", "ping", "initial prompt to send")
	flag.Parse()

	c := http.New(*addr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1) 订阅事件（异步）
	go func() {
		err := c.SubscribeEvents(ctx, *sid, func(f types.Frame) error {
			fmt.Printf("[frame] %s/%s payload=%v\n", f.Kind, f.Phase, f.Payload)
			return nil
		})
		if err != nil && ctx.Err() == nil {
			fmt.Fprintf(os.Stderr, "subscribe: %v\n", err)
		}
	}()

	// 2) 发送一次 prompt
	resp, err := c.SendSession(ctx, types.SessionSendRequest{SessionID: *sid, Content: *prompt})
	if err != nil {
		fmt.Fprintf(os.Stderr, "send: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("accepted=%v sid=%s\n", resp.Accepted, resp.SessionID)

	// 3) 等 Ctrl-C 或超时
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case <-sig:
	case <-time.After(5 * time.Second):
	}
}
