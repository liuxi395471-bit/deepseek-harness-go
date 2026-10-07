package console

import (
	"embed"
	"io/fs"
)

//go:embed all:web_dist
var embeddedSPA embed.FS

// spaFS 返回 embed.FS 子树给 ConsoleServer。
//
// embed.go 通过 //go:embed all:web_dist 把整个 web_dist 目录（含
// index.html + 全部 chunks）烧进二进制。Go embed 把目录作为单一项
// 注入（key = "web_dist"），因此这里 sub 到 web_dist 这一层，调用方
// 拿到 fs.FS 后可直接 Open("index.html") / Open("assets/xxx.js")。
// 如果 web_dist 缺失，则返回 emptyFS 让 handler 给出 404。
func spaFS() fs.FS {
	sub, err := fs.Sub(embeddedSPA, "web_dist")
	if err != nil {
		return emptyFS{}
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return emptyFS{}
	}
	return sub
}

type emptyFS struct{}

func (emptyFS) Open(_ string) (fs.File, error) { return nil, fs.ErrNotExist }
