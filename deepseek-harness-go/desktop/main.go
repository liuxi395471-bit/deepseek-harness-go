// dsh-desktop：把 dsh.exe 装进桌面启动器。
//
// 用法：
//   dsh-desktop.exe                       # 使用同目录 dsh.exe
//   dsh-desktop.exe -dsh=path/to/dsh.exe  # 指定 dsh 路径
//   dsh-desktop.exe -port=7777            # 显式端口
//   dsh-desktop.exe -no-open              # 不自动开浏览器
//   dsh-desktop.exe -token=xxx            # 给 dsh 注入 token
//
// 嵌入 dsh.exe：把二进制放到 desktop/dsh.exe 后用 `-tags embed_dsh` 构建：
//   go build -tags embed_dsh -o dsh-desktop.exe .

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"deepseek-harness-go/desktop/launcher"
)

func main() {
	var (
		dshPath = flag.String("dsh", "", "dsh.exe 路径；留空 = 使用嵌入或同目录")
		port    = flag.String("port", "", "dsh 监听端口；留空 = 自动")
		noOpen  = flag.Bool("no-open", false, "不自动打开浏览器")
		token   = flag.String("token", "", "Bearer token 注入 dsh")
		wsRoot  = flag.String("workspace", "", "DSH_AGENT_WORKSPACE_ROOT")
	)
	flag.Parse()

	log.SetPrefix("[dsh-desktop] ")
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	listen := "127.0.0.1:" + *port
	if *port == "" {
		listen = "127.0.0.1:0"
	}

	cfg := launcher.Config{
		Listen:       listen,
		AuthToken:    *token,
		Workspace:    *wsRoot,
		ReadyTimeout: 30 * time.Second,
	}

	// 二进制源优先级：flag > 同目录 dsh.exe > 嵌入字节
	switch {
	case *dshPath != "":
		cfg.DSHRawPath = *dshPath
	case fileExists(filepath.Join(".", "dsh.exe")):
		cfg.DSHRawPath = filepath.Join(".", "dsh.exe")
	case len(embeddedDSH) > 0:
		cfg.EmbedFS = &embedWrapper{data: embeddedDSH}
		cfg.EmbedName = "dsh.exe"
	default:
		log.Fatal("no dsh binary: use -dsh or place dsh.exe next to desktop binary or build with embedded (-tags embed_dsh)")
	}

	// 桌面模式：当用户没显式指定 listen 时认为是桌面模式（自动 token 等）
	// isDesktop 影响：自动生成 token、URL 加 ?from=desktop、写入 state meta。
	// 即使 -no-open 也保持 desktop 元数据写入，便于远端 UI 检测。
	isDesktop := *port == ""

	// 桌面模式下自动生成 token（写到 state KV 与 stdout 各一份）
	if isDesktop && *token == "" {
		*token = generateToken()
		log.Printf("desktop: generated bearer token (write-only to dsh env)")
	}

	// Ctrl+C 优雅停止
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	srv, err := launcher.Start(ctx, cfg)
	if err != nil {
		log.Fatalf("start dsh: %v", err)
	}
	defer srv.Stop()

	log.Printf("dsh listening at %s", srv.ListenAddr())
	log.Printf("open: %s", srv.ConsoleURL())

	if !*noOpen {
		url := srv.ConsoleURL()
		if isDesktop {
			// 提示前端走桌面模式 + 注入 token（仅 localhost）
			url += "?from=desktop&token=" + *token
		}
		if err := launcher.OpenBrowser(url); err != nil {
			log.Printf("warning: open browser failed: %v (open URL manually)", err)
		}
	}

	// 桌面模式下向后端 state 写入启动器元数据（前端 footer 读取）
	if isDesktop {
		go writeDesktopMeta(cfg.AuthToken, srv.ListenAddr())
	}

	// 阻塞直到信号
	<-ctx.Done()
	log.Printf("shutting down...")
}

// writeDesktopMeta 把启动器版本 / pid 写到 console state KV，
// 前端 footer 在 ?from=desktop 模式下读取并显示。
func writeDesktopMeta(token, addr string) {
	body := []byte(`{"key":"desktop","value":"{\"version\":\"8.0.0\",\"pid\":\"` + strconv.Itoa(os.Getpid()) + `\"}"}`)
	url := "http://" + addr + "/api/v1/console/state"
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// generateToken 生成一个 32 字节十六进制 token。
func generateToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "dsh-desktop-" + strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}

// embedWrapper 把 embed 字节转成 fsWrapper 接口。
type embedWrapper struct{ data []byte }

func (e *embedWrapper) ReadFile(_ string) ([]byte, error) {
	if len(e.data) == 0 {
		return nil, os.ErrNotExist
	}
	return e.data, nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// 抑制未用导入告警
var _ = fmt.Sprint
