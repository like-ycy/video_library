# 生成可单独发布的组件包与产物自述（Windows 侧）。
#
# 用法
#   tools\component.ps1                  版本取产物自述
#   tools\component.ps1 -Version 1.2.3   产物自述读不到时用这个版本号
#
# 产物（平台段固定 windows/amd64）
#   dist\release\<PY_COMPONENT>_<ver>_windows_amd64.zip   顶层唯一目录 <PY_COMPONENT>\
#   dist\release\<PY_COMPONENT>_meta_windows_amd64.json   产物 `version` 首行的原样输出
#
# 与 tools/component.sh 同责、同产物。命名与清单生成咬得太紧，所以不塞进
# package.ps1：那份脚本四个仓库逐字相同，分叉从这里开始。
[CmdletBinding()]
param(
    [string]$Version = ''
)

$ErrorActionPreference = 'Stop'

$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$Config = @{}
Get-Content (Join-Path $PSScriptRoot 'config.env') | ForEach-Object {
    $line = $_.Trim()
    if ($line -eq '' -or $line.StartsWith('#')) { return }
    $i = $line.IndexOf('=')
    if ($i -lt 1) { return }
    $key = $line.Substring(0, $i).Trim()
    $value = $line.Substring($i + 1).Trim()
    # config.env 同时喂给 bash，多值只能写成 COMPONENTS="scraper dlre"（不写引号
    # 会被 source 拆成命令）。PowerShell 拿到的是带引号的字面量，不剥掉就会拼出
    # third_party\"scraper"\windows-amd64 这种路径。
    if ($value.Length -ge 2 -and
        (($value.StartsWith('"') -and $value.EndsWith('"')) -or
         ($value.StartsWith("'") -and $value.EndsWith("'")))) {
        $value = $value.Substring(1, $value.Length - 2)
    }
    $Config[$key] = $value
}
$PyComponent = $Config['PY_COMPONENT']
$PyBuildName = $Config['PY_BUILD_NAME']

function Die([string]$Text) { throw $Text }
function Step([string]$Text) { Write-Host ''; Write-Host "==> $Text" -ForegroundColor Cyan }
function Note([string]$Text) { Write-Host "    $Text" }

$src = Join-Path $Root "python\build\pyi\dist\$PyBuildName"
if (-not (Test-Path $src)) { Die "未找到 Python 组件产物：$src（先跑 tools\build.ps1）" }

$rel = Join-Path $Root 'dist\release'
New-Item -ItemType Directory -Path $rel -Force | Out-Null

Step "生成组件包（$PyComponent windows/amd64）"

# 产物自述的第一行就是那行 JSON（{"type":"version","scraper":"..."}），
# 原样落进 meta：manifestgen 读的正是它，不在这里重新拼一份。
$raw = $null
$bin = Join-Path $src "$PyComponent.exe"
if (Test-Path $bin) {
    try {
        $raw = (& $bin version 2>$null | Select-Object -First 1)
    }
    catch { $raw = $null }
}

$ver = $null
if ($raw) {
    try { $ver = ($raw | ConvertFrom-Json).scraper } catch { $ver = $null }
}
if (-not $ver) {
    if (-not $Version) {
        $envVersion = $env:VERSION -replace '^v', ''
        if ($envVersion) {
            $Version = $envVersion
        }
        else {
            $tag = (git -C $Root describe --tags --exact-match 2>$null)
            $Version = if ($tag) { $tag -replace '^v', '' } else { '0.0.0-dev' }
        }
    }
    $ver = $Version
    Note "产物自述不可用，版本回退为 $ver"
}

$meta = Join-Path $rel "${PyComponent}_meta_windows_amd64.json"
if ($raw) {
    Set-Content -Path $meta -Value $raw -Encoding utf8
}
else {
    # 产物跑不起来时也要留下自述，否则 manifestgen 会缺平台。
    Set-Content -Path $meta -Value "{`"scraper`":`"$ver`"}" -Encoding utf8 -NoNewline
}
Note "自述：$(Split-Path -Leaf $meta) → $ver"

$asset = Join-Path $rel "${PyComponent}_${ver}_windows_amd64.zip"
if (Test-Path $asset) { Remove-Item -Force $asset }

# -Path <目录> 保留目录本身作为顶层条目，写 <目录>\* 才会平铺；
# 解压侧按「顶层目录 + 目录内可执行文件」剥壳，平铺的包会找不到可执行文件。
Compress-Archive -Path $src -DestinationPath $asset -Force

$zip = [System.IO.Compression.ZipFile]::OpenRead($asset)
try {
    $names = @($zip.Entries | ForEach-Object { $_.FullName -replace '\\', '/' })
    $stray = @($names | Where-Object { -not ($_ -eq $PyComponent -or $_.StartsWith("$PyComponent/")) })
    if ($stray.Count -gt 0) {
        Die "$(Split-Path -Leaf $asset) 顶层应为 $PyComponent/，实际出现：$($stray[0])"
    }
    Write-Host "    归档结构：$($names.Count) 个条目，全部在 $PyComponent/ 下"
}
finally { $zip.Dispose() }

Step '完成'
Note "归档目录：$rel"
