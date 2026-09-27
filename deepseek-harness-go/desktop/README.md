# dsh Desktop 启动器

> v8 P8-3：把 `dsh.exe`（含 Web Console SPA）装进一个独立的"桌面启动器"二进制。
> 双击 = 启动 `dsh -serve` + 打开浏览器到 `/console/`。

## 设计

```
desktop/
├── go.mod               ← 独立 Go module（避免污染主仓库）
├── main.go              ← 启动器入口
├── embed.go             ← //go:embed dsh.exe / wails.json（可选）
├── launcher/
│   ├── launcher.go      ← 后端进程管理（启动/停止/health-check）
│   ├── browser.go       ← 调用系统默认浏览器
│   └── launcher_test.go
├── frontend/            ← Wails 兼容骨架（用户装 Wails 后 wails build 升级）
│   ├── package.json
│   ├── index.html
│   └── src/main.js      ← 占位
├── wails.json           ← Wails 项目元数据（v8.0 不强制使用）
├── build-windows.ps1    ← 构建脚本
└── README.md
```

## 两种运行模式

### 模式 A：纯启动器（v8.0 默认，无 Wails 依赖）

- 启动器把内嵌的 `dsh.exe` 释放到
  `%LOCALAPPDATA%\DeepSeekHarness\bin\dsh.exe`（仅首次）
- spawn 子进程 `dsh.exe -serve -port 0`（端口由 ds-go 自动选）
- 轮询 `/api/v1/console/health/` 直到 200
- 调用 Windows ShellExecute 打开默认浏览器到 `/console/`
- 后台运行直到用户关闭终端窗口；`Ctrl+C` 终止整个进程组

### 模式 B：Wails 壳（v8.0.1 候选，可选）

- `cd desktop && wails dev` / `wails build`
- 复用 `web/dist/` 作为 frontend；Wails 内部把请求转给内置 ds-go

## 命令

```bash
# 开发
cd desktop
go build -o dsh-desktop.exe .
./dsh-desktop.exe

# 构建 Windows 桌面包
pwsh -File build-windows.ps1

# 测试
go test ./...
```

## 端口策略

`dsh -serve` 支持 `-port` 显式；启动器用 `0` 让 dsh 自动选可用端口，
然后从 stdout / stderr 解析 `"listening on 127.0.0.1:XXXX"` 拿到端口。
回退：探测 7777-7800 范围。
