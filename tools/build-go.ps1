# 构建桌面 App（Windows）。
#
# 用法：
#   tools\build-go.ps1                     # 本机平台
#   tools\build-go.ps1 windows/amd64       # 显式指定
#
# Windows 目标会带上 -webview2 download：把 WebView2 引导程序一并打进安装包，
# 覆盖没预装 WebView2 的机器。
$ErrorActionPreference = "Stop"

param([string]$Platform = "")

$repo = Split-Path -Parent $PSScriptRoot
Set-Location (Join-Path $repo "go")

if (-not (Get-Command wails -ErrorAction SilentlyContinue)) {
    Write-Error "未找到 wails CLI。安装：go install github.com/wailsapp/wails/v2/cmd/wails@latest"
}

# 不要用 $args：它是 PowerShell 的自动变量。
$wailsArgs = @("build", "-clean")
if ($Platform) { $wailsArgs += @("-platform", $Platform) }
if ($Platform -like "windows/*") { $wailsArgs += @("-webview2", "download") }

wails @wailsArgs
if ($LASTEXITCODE -ne 0) { throw "wails build 失败" }

Write-Host "产物在 $(Join-Path $repo 'go\build\bin')"
