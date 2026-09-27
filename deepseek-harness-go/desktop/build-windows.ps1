<#
.SYNOPSIS
  构建 dsh-desktop.exe（Windows 启动器）。

.DESCRIPTION
  三种构建模式：
    1. 不嵌入（默认）— 仅启动器，依赖同目录 dsh.exe
    2. 嵌入（-embed）— 把 ../dsh.exe 复制到 ./dsh.exe，再 go build -tags embed_dsh
    3. CI / Wails — 留扩展点（未来 wails build）

.PARAMETER Embed
  把 ../dsh.exe 嵌入到启动器二进制里；v8.0 推荐用法。

.PARAMETER Output
  最终产物路径，默认 ./bin/dsh-desktop.exe

.EXAMPLE
  pwsh -File build-windows.ps1 -Embed
#>

[CmdletBinding()]
param(
    [switch]$Embed,
    [string]$Output = "./bin/dsh-desktop.exe"
)

$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$repoRoot  = Resolve-Path "$scriptDir/.." | Select-Object -ExpandProperty Path
$desktopDir = $scriptDir

Push-Location $desktopDir

try {
    Write-Host "[1/4] check Go toolchain..." -ForegroundColor Cyan
    $goVersion = go version 2>&1
    if (-not $goVersion) {
        throw "go not in PATH"
    }
    Write-Host "       $goVersion"

    if ($Embed) {
        $srcExe = Join-Path $repoRoot "dsh.exe"
        if (-not (Test-Path $srcExe)) {
            throw "no dsh.exe at $srcExe; run 'go build -o dsh.exe ./cmd/dsh' first"
        }
        Write-Host "[2/4] embed dsh.exe -> $srcExe" -ForegroundColor Cyan
        Copy-Item -Force $srcExe "$desktopDir/dsh.exe"

        Write-Host "[3/4] build with -tags embed_dsh ..." -ForegroundColor Cyan
        go build -tags embed_dsh -ldflags "-s -w" -o $Output .
        Remove-Item "$desktopDir/dsh.exe" -ErrorAction SilentlyContinue
    } else {
        Write-Host "[3/4] build (no embed) ..." -ForegroundColor Cyan
        go build -ldflags "-s -w" -o $Output .
    }

    if (-not (Test-Path $Output)) {
        throw "build produced no output"
    }
    $size = (Get-Item $Output).Length
    Write-Host "[4/4] done: $Output ($([math]::Round($size/1KB, 1)) KB)" -ForegroundColor Green
}
finally {
    Pop-Location
}
