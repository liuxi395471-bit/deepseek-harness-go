// dsh-wails — Wails v2 桌面壳（v8.1）
//
// 这是 Wails v2 真原生壳的脚手架文件。
// 用户首次需要：
//   1. 安装 Wails CLI：go install github.com/wailsapp/wails/v2/cmd/wails@latest
//   2. 在 desktop/ 目录跑：wails init -n wails_app -t vue
//   3. 把 wails_app/main.go 替换为本文件
//   4. 把 web/dist/ 内容同步到 wails_app/frontend/dist/（build 脚本处理）
//   5. wails build -platform windows/amd64 → build/bin/dsh-wails.exe
//
// 运行时（v8.1 简化）：
//   - 启动时 spawn 内嵌 / 同目录 dsh.exe（与启动器一致）
//   - 渲染 web/dist/ 到 WebView2（系统 WebView，无需自带）
//   - Tray 菜单：Show / Hide / Quit
//
// 注释：
//   - 本文件**依赖 wails CLI 生成的项目**；在没有 `wails_app/` 生成项目时
//     它不会被编译。build script `desktop/build-wails-windows.ps1` 调用
//     `wails build`；普通 `go build ./desktop/...` 仍能编译 launcher。
//   - 为避免误将 wails 引入主项目，本文件用 build tag `wails_desktop` 隔离。

//go:build wails_desktop
// +build wails_desktop

package wails_app

import (
	"context"
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// assets 是嵌入的前端 SPA 资源；wails build 时由 wails_app/frontend/dist
// 填充进 build/bin。
//
//go:embed all:frontend/dist
var assets embed.FS

// App 是 wails runtime 的 entry struct。所有绑定方法都能被前端 JS 调用。
type App struct {
	ctx context.Context
}

// startup is called by wails after the webview window is created.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	log.Printf("[dsh-wails] startup: ctx ready")
}

// shutdown is called by wails when the app exits.
func (a *App) shutdown(ctx context.Context) {
	log.Printf("[dsh-wails] shutdown")
}

// Run 是 wails_app/main.go 调用的入口。
func Run() error {
	app := &App{}
	return wails.Run(&options.App{
		Title:  "DeepSeek Harness Desktop",
		Width:  1280,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
	})
}
