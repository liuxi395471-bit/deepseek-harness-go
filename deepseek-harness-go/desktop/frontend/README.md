# dsh-desktop frontend（Wails v2 占位）

> v8.0：此目录为空 — 当前桌面启动器直接复用 `../web/dist/`。
> v8.0.1：若启用 Wails v2，可 `wails init` 在此生成 Vue 模板，
> 然后让 Wails 复用我们已有的 `web/dist/`，从而避免重复维护两套前端。

## Wails 启用步骤（v8.0.1 候选）

```bash
cd desktop
# 1. 装 Wails CLI
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# 2. 初始化前端（一次性）
wails init -n frontend -t vue

# 3. 把我们已有的 web/dist 拷到 frontend/dist
cp -r ../web/dist frontend/dist

# 4. 修改 frontend/src/main.js，让 Wails 知道用我们的 backend
#    （参考 Wails 文档的 OnStartup / OnDomReady）

# 5. 构建
wails build
```

输出在 `desktop/build/bin/dsh-desktop.exe`。
