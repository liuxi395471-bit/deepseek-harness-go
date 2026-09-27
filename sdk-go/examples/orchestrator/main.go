// orchestrator 是 sdk-go 的多步演示。
//
// 流程：
//  1. 订阅 session 事件流；
//  2. 顺序发 3 个 prompt（拆解 → 分析 → 综合）；
//  3. 每步收到 "done" 帧就推下一步；
//  4. 总超时 30s。
//
// 用法：
//
//	go run ./examples/orchestrator --addr http://127.0.0.1:7777 --sid orch-demo
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"deepseek-harness-all/sdk-go/client/http"
	"deepseek-harness-all/sdk-go/types"
)

func main() {
	addr := flag.String("addr", "http://127.0.0.1:7777", "ds-go Gateway base URL")
	sid := flag.String("sid", "orch-demo", "session id")
	token := flag.String("token", "", "optional Bearer token")
	flag.Parse()

	c := http.New(*addr).WithToken(*token)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Ctrl-C 立即退出
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "[orch] interrupt")
		cancel()
	}()

	var (
		mu      sync.Mutex
		gotDone bool
	)

	// 1) 订阅事件流
	subDone := make(chan struct{})
	go func() {
		defer close(subDone)
		err := c.SubscribeEvents(ctx, *sid, func(f types.Frame) error {
			fmt.Printf("[frame] kind=%s phase=%s payload=%v\n", f.Kind, f.Phase, f.Payload)
			if f.Kind == "loop" && f.Phase == "done" {
				mu.Lock()
				gotDone = true
				mu.Unlock()
			}
			return nil
		})
		if err != nil && ctx.Err() == nil {
			fmt.Fprintf(os.Stderr, "subscribe: %v\n", err)
		}
	}()

	steps := []string{
		"Step 1: list the workspace directory.",
		"Step 2: read the largest file inside.",
		"Step 3: summarize its first 20 lines.",
	}

	// 2) 顺序发送，每步等上一个 done 才推下一个
	for i, prompt := range steps {
		mu.Lock()
		gotDone = false
		mu.Unlock()

		fmt.Printf("[orch] sending step %d: %q\n", i+1, prompt)
		resp, err := c.SendSession(ctx, types.SessionSendRequest{
			SessionID: *sid,
			Content:   prompt,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "[orch] step %d send: %v\n", i+1, err)
			os.Exit(1)
		}
		if !resp.Accepted {
			fmt.Fprintf(os.Stderr, "[orch] step %d rejected (sid=%s)\n", i+1, resp.SessionID)
			os.Exit(1)
		}

		// 等 done 或超时
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "[orch] ctx cancelled")
			cancel()
			<-subDone
			return
		case <-time.After(8 * time.Second):
			fmt.Fprintf(os.Stderr, "[orch] step %d timeout (no done frame)\n", i+1)
			cancel()
			<-subDone
			return
		case <-loopDoneTicker(&mu, &gotDone):
			fmt.Printf("[orch] step %d done\n", i+1)
		}
	}

	fmt.Println("[orch] all steps completed")
	cancel()
	<-subDone
}

// loopDoneTicker 每 50ms 检查 gotDone 是否被置位。
func loopDoneTicker(mu *sync.Mutex, flag *bool) <-chan struct{} {
	ch := make(chan struct{}, 1)
	go func() {
		t := time.NewTicker(50 * time.Millisecond)
		defer t.Stop()
		for range t.C {
			mu.Lock()
			done := *flag
			mu.Unlock()
			if done {
				ch <- struct{}{}
				return
			}
		}
	}()
	return ch
}
