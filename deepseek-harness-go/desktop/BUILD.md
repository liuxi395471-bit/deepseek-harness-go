# dsh-desktop 构建

## 快速开始

```bash
# 1. 编译主 dsh.exe（如果还没有）
cd ..
go build -o dsh.exe ./cmd/dsh
cd desktop

# 2. 方式 A — 嵌入模式（推荐，单文件分发）
pwsh -File build-windows.ps1 -Embed

# 方式 B — 非嵌入（要求桌面二进制和 dsh.exe 同目录）
go build -o dsh-desktop.exe .
Copy-Item ../dsh.exe ./dsh.exe  # 与 dsh-desktop.exe 同目录

# 3. 跑
./dsh-desktop.exe
# → 浏览器自动打开 http://127.0.0.1:<port>/console/
```

## 命令行选项

```
dsh-desktop.exe [选项]

  -dsh=path/to/dsh.exe    指定 dsh 路径
  -port=7777              显式端口（默认 = 自动选）
  -no-open                不自动打开浏览器
  -token=xxx              给 dsh 注入 Bearer token
  -workspace=path         DSH_AGENT_WORKSPACE_ROOT
```

## 内嵌 dsh.exe（推荐分发）

把 `dsh.exe` 复制到 `desktop/` 目录，然后：

```bash
go build -tags embed_dsh -ldflags "-s -w" -o dsh-desktop.exe .
```

最终产物：
- 单文件 `dsh-desktop.exe`（≈ 9MB on Windows）
- 双击 = 释放 `dsh.exe` 到 `%LOCALAPPDATA%\DeepSeekHarness\bin\` + 启动 + 打开浏览器
- `Ctrl+C` 优雅停止（SIGTERM → 5s 超时 → SIGKILL）

## 测试

```bash
cd desktop
go test ./...
```

测试覆盖：
- `TestLineCapture_DetectsListen` — 行捕获能从 stderr 抓 listening 地址
- `TestPrepareBinary_FromEmbed` — embed 字节正确写入并幂等
- `TestPrepareBinary_RawPath` — 从物理路径复制
- `TestServer_StartStop_PortAuto` — 真实 spawn + 健康检查 + 优雅停止

## 未来：Wails v2 升级（v8.0.1）

如果以后装上了 Wails v2（`npm install -g wails`），可以：

```bash
cd desktop
wails init -n desktop -t vue  # 一次性
# 然后把 web/dist 拷贝到 frontend/dist
wails build
```

Wails 模式：
- 原生 WebView2 窗口（无浏览器）
- 体积更小、UX 更紧
- Tray 图标 / 系统通知 / 系统菜单

但 v8.0 不强制 Wails — 启动器模式已经覆盖 99% 用例。
