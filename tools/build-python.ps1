# 构建刮削器可执行文件（Windows）。
#
# PyInstaller 不做交叉编译：要得到 .exe，必须在 Windows 上运行本脚本。
#
# 用法：tools\build-python.ps1
$ErrorActionPreference = "Stop"

$repo = Split-Path -Parent $PSScriptRoot
Set-Location (Join-Path $repo "python")

uv sync
if ($LASTEXITCODE -ne 0) { throw "uv sync 失败" }

uv run pyinstaller scraper.spec --noconfirm
if ($LASTEXITCODE -ne 0) { throw "PyInstaller 打包失败" }

$dest = Join-Path $repo "tools\bin\scraper"
if (Test-Path $dest) { Remove-Item -Recurse -Force $dest }
Move-Item (Join-Path $repo "python\dist\scraper") $dest

Write-Host "刮削器已输出到 $dest"

# 打包后立刻自检：这一步能提前暴露「seleniumbase 资源没被收集进去」
# 这类只在运行时才发作的问题。
Write-Host "--- 自检 ---"
& (Join-Path $dest "scraper.exe") version
& (Join-Path $dest "scraper.exe") doctor
