# 组装最终分发目录，对应 docs/architecture.md §9.3 的结构。
#
# 前两件事必须已经做完：
#   tools\build-python.ps1
#   tools\build-go.ps1 windows/amd64
#
# 产物：dist\VideoLib\
$ErrorActionPreference = "Stop"

$repo = Split-Path -Parent $PSScriptRoot
$dist = Join-Path $repo "dist\VideoLib"

$appExe = Join-Path $repo "go\build\bin\VideoLib.exe"
$scraper = Join-Path $repo "tools\bin\scraper"

if (-not (Test-Path $appExe)) {
    throw "未找到 $appExe，请先运行 tools\build-go.ps1 windows/amd64"
}
if (-not (Test-Path $scraper)) {
    throw "未找到 $scraper，请先运行 tools\build-python.ps1"
}

if (Test-Path $dist) { Remove-Item -Recurse -Force $dist }
New-Item -ItemType Directory -Path (Join-Path $dist "tools") -Force | Out-Null

Copy-Item $appExe $dist

# 刮削器保持 onedir 结构整体拷贝。不要试图把它塞进 App 可执行文件里：
# 那样不仅要解压到临时目录（重新引入 onefile 的驱动重复下载问题），
# 也没法单独更新或排查。
Copy-Item $scraper (Join-Path $dist "tools\scraper") -Recurse

$ffprobe = Join-Path $repo "tools\bin\ffprobe.exe"
if (Test-Path $ffprobe) {
    Copy-Item $ffprobe (Join-Path $dist "tools\ffprobe.exe")
} else {
    Write-Warning "未找到 tools\bin\ffprobe.exe —— 视频时长与编码信息将不可用，其余功能不受影响"
}

Write-Host "分发目录已生成：$dist"
Get-ChildItem $dist -Recurse -Depth 1 | Select-Object -First 20 FullName
