# 组装最终分发目录，对应 docs/architecture.md §9.3 的结构。
#
# 用法
#   tools\package.ps1                轻量包：不含 tools\scraper（默认）
#   tools\package.ps1 -Full          整包：含 tools\scraper，离线机器用
#   tools\package.ps1 -NoScraper     同上「轻量包」，显式声明（保留给旧脚本）
#
# 为什么默认是轻量包：刮削器 31MB，占分发目录 65%，而绝大多数用户第一次打开
# App 只是看视频。改为首次刮削时按需下载后，安装包从 37MB 降到 10MB，见
# docs/size-reduction-plan.md。
#
# 前两件事必须已经做完（通常由 CI 内联步骤完成）：
#   PyInstaller 产出 scraper 并放到 tools\bin\scraper（仅 -Full 需要）
#   wails build 产出 go\build\bin\VideoLib.exe
#
# 产物：dist\VideoLib\
param(
    [switch]$Full,
    [switch]$NoScraper
)
$ErrorActionPreference = "Stop"

if ($Full -and $NoScraper) {
    throw "-Full 与 -NoScraper 不能同时使用"
}
# 默认轻量；-Full 才带刮削器。-NoScraper 与默认等价，保留是为了让调用方
# 显式声明意图 —— 将来默认值若再变，写死参数的脚本行为不会跟着变。
$wantScraper = [bool]$Full

$repo = Split-Path -Parent $PSScriptRoot
$dist = Join-Path $repo "dist\VideoLib"

$appExe = Join-Path $repo "go\build\bin\VideoLib.exe"
$scraper = Join-Path $repo "tools\bin\scraper"

if (-not (Test-Path $appExe)) {
    throw "未找到 $appExe，请先运行 wails build -platform windows/amd64"
}
if ($wantScraper -and -not (Test-Path $scraper)) {
    throw "未找到 $scraper，请先把 PyInstaller 产物放到 tools\bin\scraper（默认的轻量包不需要它）"
}

if (Test-Path $dist) { Remove-Item -Recurse -Force $dist }
New-Item -ItemType Directory -Path (Join-Path $dist "tools") -Force | Out-Null

Copy-Item $appExe $dist

# 刮削器保持 onedir 结构整体拷贝。不要试图把它塞进 App 可执行文件里：
# 那样不仅要解压到临时目录（重新引入 onefile 的驱动重复下载问题），
# 也没法单独更新或排查。
#
# 目标目录先显式建出来，再拷「源目录的内容」进去，而不是把源目录拷到目标路径上。
# 后者在目标不存在与已存在两种情况下的落点不同（是「成为源目录的副本」还是
# 「源目录整个被塞进去」），差出一层目录 —— 而 Go 侧按固定路径找刮削器
# （见 go/internal/toolpath），差一层就等于找不到。
# 要的就是 dist\tools\scraper\{scraper.exe,_internal\}，与
# docs/architecture.md §9.3 一致。
if ($wantScraper) {
    $scraperDest = Join-Path $dist "tools\scraper"
    New-Item -ItemType Directory -Path $scraperDest -Force | Out-Null
    Copy-Item (Join-Path $scraper "*") $scraperDest -Recurse
} else {
    Write-Host "轻量包：不含 tools\scraper，首次刮削时由 App 按需下载"
}

$ffprobe = Join-Path $repo "tools\bin\ffprobe.exe"
if (Test-Path $ffprobe) {
    Copy-Item $ffprobe (Join-Path $dist "tools\ffprobe.exe")
} else {
    Write-Warning "未找到 tools\bin\ffprobe.exe —— 视频时长与编码信息将不可用，其余功能不受影响"
}

Write-Host "分发目录已生成：$dist"
Get-ChildItem $dist -Recurse -Depth 1 | Select-Object -First 20 FullName
