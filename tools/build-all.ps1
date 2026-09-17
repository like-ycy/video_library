# 一键构建本机整包（Windows）：Python 刮削器 + Go 桌面 App + dist\VideoLib。
#
# 用法：
#   tools\build-all.ps1
#   tools\build-all.ps1 -SkipPython
#   tools\build-all.ps1 -SkipGo
#   tools\build-all.ps1 -SkipPackage
#   tools\build-all.ps1 -Cross windows/amd64   # 额外交叉编译（Windows 上通常已是本机）
#
# 硬约束：
#   * PyInstaller 不能交叉编译 —— scraper.exe 只能在 Windows 上打。
#   * Go / Wails 可从 macOS 交叉编译到 Windows，但整包仍建议在本机完成。
#
# 产物：
#   dist\VideoLib\          解压即运行的分发目录
#   go\build\bin\           App 二进制
$ErrorActionPreference = "Stop"

param(
    [switch]$SkipPython,
    [switch]$SkipGo,
    [switch]$SkipPackage,
    [string]$Cross = ""
)

$repo = Split-Path -Parent $PSScriptRoot

Write-Host "==> 目标：Windows $([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture) 整包"
if ($Cross) {
    Write-Host "==> 额外交叉编译 Go：$Cross"
}

if (-not $SkipPython) {
    Write-Host ""
    Write-Host "==> [1/3] 构建 Python 刮削器（本机架构，PyInstaller 不交叉编译）"
    & (Join-Path $repo "tools\build-python.ps1")
    if ($LASTEXITCODE -ne 0) { throw "build-python 失败" }
} else {
    Write-Host ""
    Write-Host "==> [1/3] 跳过 Python（-SkipPython）"
}

if (-not $SkipGo) {
    Write-Host ""
    Write-Host "==> [2/3] 构建 Go 桌面 App（本机平台）"
    & (Join-Path $repo "tools\build-go.ps1")
    if ($LASTEXITCODE -ne 0) { throw "build-go 失败" }
} else {
    Write-Host ""
    Write-Host "==> [2/3] 跳过 Go（-SkipGo）"
}

if ($Cross) {
    Write-Host ""
    Write-Host "==> 额外交叉编译 Go → $Cross"
    Write-Host "    注意：这只是 App 二进制，不会替换本机 scraper，也不进 dist\。"
    & (Join-Path $repo "tools\build-go.ps1") -Platform $Cross
    if ($LASTEXITCODE -ne 0) { throw "交叉编译失败" }
}

if (-not $SkipPackage) {
    Write-Host ""
    Write-Host "==> [3/3] 组装分发目录"
    & (Join-Path $repo "tools\package.ps1")
    if ($LASTEXITCODE -ne 0) { throw "package 失败" }
} else {
    Write-Host ""
    Write-Host "==> [3/3] 跳过组装（-SkipPackage）"
}

Write-Host ""
Write-Host "完成。本机整包：$(Join-Path $repo 'dist\VideoLib')"
if ($Cross) {
    Write-Host "交叉编译产物：$(Join-Path $repo 'go\build\bin')"
}
