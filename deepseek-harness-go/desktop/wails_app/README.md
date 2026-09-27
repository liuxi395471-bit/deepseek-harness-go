# dsh-wails 构建（v8.1）

> **状态**：脚手架已就位（`wails_app/app.go`）。完整构建需要 Wails CLI。

## 一次性安装

```powershell
# 安装 Go ≥ 1.22（已具备）
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# 安装 nodejs + npm（Windows 自带 WebView2，无须额外 runtime）
# https://nodejs.org/

# 安装 wails dependencies
wails doctor
```

## 初始化项目（v8.1 一次性）

```powershell
cd D:\Devops\AgentProgram\deepseek-harness-all\deepseek-harness-go\desktop
wails init -n wails_app -t vue
# 然后：
# 1. 把 wails_app/main.go 替换为 wails_app/app.go（v8.1 scaffold）
# 2. 删除 wails_app/frontend 中冗余模板
# 3. 把 ../web/dist 内容同步到 wails_app/frontend/dist
```

## 构建 Windows 桌面壳

```powershell
cd desktop\wails_app

# 同步 web/dist → frontend/dist
Remove-Item -Recurse -Force frontend\dist
Copy-Item -Recurse ..\..\web\dist frontend\dist

# 构建 wails 二进制
wails build -tags wails_desktop -platform windows/amd64

# 输出：build/bin/dsh-wails.exe（约 10-15 MB）
```

## 运行

```powershell
./build/bin/dsh-wails.exe
# → 原生 WebView2 窗口显示控制台
# → 内部 spawn 同目录 dsh.exe 提供后端 API
```

## 为什么 v8.1 不强制 Wails？

- 启动器模式（`dsh-desktop.exe`）已覆盖 99% 用例：浏览器 + 系统 WebView2 = 几乎相同的 UX
- Wails 跨平台 CI 资源消耗大（Windows + macOS + Linux 各跑一次构建）
- v8.1 计划：**仅 Windows 验证**；mac/Linux 留 v8.2

## 故障排查

| 错误 | 解决 |
|---|---|
| `wails: command not found` | `go install github.com/wailsapp/wails/v2/cmd/wails@latest` |
| WebView2 缺失（启动崩溃） | https://developer.microsoft.com/microsoft-edge/webview2/ |
| `npm install` 失败 | 切换 npm 镜像 `npm config set registry https://registry.npmmirror.com` |
| `wails build` 报 CGO 错误 | 安装 gcc（Windows: TDM-GCC / MinGW-w64） |
