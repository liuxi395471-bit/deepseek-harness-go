package console

import (
	"embed"
	"io/fs"
)

//go:embed all:web_dist/index.html
var embeddedIndex embed.FS

// spaFS 返回 embed.FS 子树给 ConsoleServer。
//
// 路径约定：调用方把 web/dist 软链或拷贝到 internal/console/web_dist/，
// 或在 go build 时通过 -overlay 注入。当前实现嵌入一个占位
// index.html（P8-2 替换为真实 Vue 构建产物）。
//
// 返回的 fs.FS 已经是子树的根（调用 http.FS 后能直接 Stat "index.html"）。
func spaFS() fs.FS {
	sub, err := fs.Sub(embeddedIndex, "web_dist")
	if err != nil {
		// embed 失败说明目录不在；返回空 FS，让 ConsoleServer 在 /console/
		// 上返回 404 + 清晰错误。
		return emptyFS{}
	}
	return sub
}

type emptyFS struct{}

func (emptyFS) Open(_ string) (fs.File, error) { return nil, fs.ErrNotExist }
