package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// 仅用于手动调试 node bridge；不应在 CI 中运行。
func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: debug_bridge <path-to-main.js>")
		os.Exit(1)
	}
	cmd := exec.Command("node", os.Args[1])
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		fmt.Println("start:", err)
		os.Exit(1)
	}
	defer cmd.Process.Kill()

	// 读 stderr
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stderr.Read(buf)
			if err != nil {
				return
			}
			fmt.Fprintln(os.Stderr, "[stderr]", string(buf[:n]))
		}
	}()

	// 写 initialize 请求
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	hdr := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	if _, err := stdin.Write([]byte(hdr)); err != nil {
		fmt.Println("write hdr:", err)
		os.Exit(1)
	}
	if _, err := stdin.Write(body); err != nil {
		fmt.Println("write body:", err)
		os.Exit(1)
	}

	// 等响应 5s
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		n, err := stdout.Read(buf)
		if err != nil {
			fmt.Println("read:", err)
			close(done)
			return
		}
		fmt.Println("got:", bytes.TrimSpace(buf[:n]))
		close(done)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case <-done:
	case <-ctx.Done():
		fmt.Println("timeout")
	}
}
