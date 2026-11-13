// Package webui 提供 Web 控制台的静态资源托管。
//
// 它把前端构建产物（web/dist）用 go:embed 内嵌进 dsh 二进制，
// 使 `dsh -serve` 单进程即可同时提供 API 与前端页面，无需额外部署
// nginx 或起一个前端 dev server。
//
// 鉴权：静态资源（HTML / JS / CSS）不含敏感数据，因此**不要求**
// Bearer token；API 端点仍由 internal/server 的 bearerAuth 保护。
package webui

import (
	"embed"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed all:dist
var embedded embed.FS

// spaFS 返回内嵌的 SPA 文件系统。
//
// embed 把整个目录作为单一项注入（key = "dist"），因此这里 sub 到
// "dist" 这一层，调用方拿到 fs.FS 后可直接 Open("index.html")。
// 若构建产物缺失（尚未执行 npm run build），返回 emptyFS，让 handler
// 给出 404 而不是 panic。
func spaFS() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		return emptyFS{}
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return emptyFS{}
	}
	return sub
}

// emptyFS 是一个永远返回"不存在"的 fs.FS，用于构建产物缺失的降级路径。
type emptyFS struct{}

func (emptyFS) Open(_ string) (fs.File, error) { return nil, fs.ErrNotExist }

// Handler 返回托管前端 SPA 的 http.Handler。
//
// 路由规则（标准 SPA fallback）：
//
//   - 精确命中磁盘文件（如 /assets/index-abc123.js）→ 直接返回该文件。
//   - 其余路径（如 /、/sessions、/login）→ 返回 index.html，
//     交给前端路由处理。
//   - /api/* 与 /healthz 交由调用方处理：本 handler 不应匹配它们，
//     因此调用方必须把它挂在这些路径之后。
//
// 实现说明：这里没有直接用 http.FileServer —— 它对 "/index.html"
// 会发 301 重定向到 "/"（浏览器加载 index.html 时会多一次往返，
// 且 SPA fallback 场景下行为不直观）。改为自行读取文件并用
// http.ServeContent 输出，行为更可控。
func Handler() http.Handler {
	files := spaFS()
	index, indexErr := fs.ReadFile(files, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// API 路径不应落到这里；显式拒绝以免掩盖路由错误。
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" {
			http.NotFound(w, r)
			return
		}

		// 归一化请求路径：去掉前导 "/" 与清理后的 ".." 成分。
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" || name == "index.html" {
			// 首页（或显式请求 /index.html）：直接输出 index.html，
			// 避免 FileServer 的 301 重定向。
			if indexErr != nil {
				http.Error(w, "web console not built", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			_, _ = w.Write(index)
			return
		}

		// 尝试打开磁盘上的真实文件。
		f, err := files.Open(name)
		if err != nil {
			// SPA fallback：任何未命中的路径都返回 index.html。
			if indexErr != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			_, _ = w.Write(index)
			return
		}
		defer f.Close()

		// Vite 产物带内容哈希，可以长期缓存。
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}

		// embed.FS 的文件实现了 io.ReadSeeker，可交给 ServeContent
		// 处理 Range 请求与 Content-Type 推断。
		rs, ok := f.(io.ReadSeeker)
		if !ok {
			data, readErr := fs.ReadFile(files, name)
			if readErr != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", contentType(name))
			_, _ = w.Write(data)
			return
		}
		http.ServeContent(w, r, name, time.Time{}, rs)
	})
}

// contentType 根据扩展名推断 MIME 类型。
func contentType(name string) string {
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

// Available 报告内嵌的前端构建产物是否存在。
// cmd/dsh 用它在启动时给出提示（未构建时提醒用户先跑 npm run build）。
func Available() bool {
	files := spaFS()
	if _, err := fs.Stat(files, "index.html"); err != nil {
		return false
	}
	return true
}
