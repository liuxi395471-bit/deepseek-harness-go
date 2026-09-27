# dsh-wails 构建脚本（v8.1）
#
# 用法：
#   pwsh -File build-wails-windows.ps1               # 默认构建
#   pwsh -File build-wails-windows.ps1 -Dev          # 开发模式（hot reload）
#
# 前置：
#   - Wails CLI: go install github.com/wailsapp/wails/v2/cmd/wails@latest
#   - 已执行 wails init 生成 wails_app/（首次手动；后续脚本不变）
#
# 失败时直接抛错退出。

[CmdletBinding()]
param(
    [switch]$Dev,
    [switch]$SkipSync
)

$ErrorActionPreference = "Stop"
Set-Location -LiteralPath (Split-Path -Parent $MyInvocation.MyCommand.Path)

function Require-Wails {
    if (-not (Get-Command wails -ErrorAction SilentlyContinue)) {
        Write-Error "Wails CLI not found. Run: go install github.com/wailsapp/wails/v2/cmd/wails@latest"
    }
    Write-Host "✓ Wails CLI present: $(wails version 2>&1)"
}

function Sync-WebDist {
    param([string]$From, [string]$To)
    if (Test-Path $To) {
        Remove-Item -Recurse -Force $To
    }
    Copy-Item -Recurse -Force $From $To
    Write-Host "✓ Synced $From → $To"
}

Require-Wails

if (-not $SkipSync) {
    $from = Join-Path (Resolve-Path "../web/dist") ""
    $to   = Join-Path (Resolve-Path "./frontend/dist") ""
    if (-not (Test-Path $from)) {
        Write-Error "web/dist not found; run `npm run build` in web/ first"
    }
    Sync-WebDist -From $from -To $to
}

Set-Location -LiteralPath "wails_app"

if ($Dev) {
    Write-Host "→ Wails dev mode (live reload)"
    wails dev -tags wails_desktop
} else {
    Write-Host "→ Wails build (windows/amd64)"
    wails build -tags wails_desktop -platform windows/amd64
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    Write-Host "✓ Output: wails_app/build/bin/dsh-wails.exe"
}
