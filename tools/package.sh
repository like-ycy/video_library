#!/usr/bin/env bash
# 组装最终分发目录（macOS），对应 docs/architecture.md §9.3 的结构。
#
# 前两件事必须已经做完（通常由 tools/build.sh 串联）：
#   PyInstaller 产出 scraper 并放到 tools/bin/scraper
#   wails build 产出 go/build/bin/VideoLib.app
#
# 产物：dist/VideoLib/
#   VideoLib.app/
#   tools/scraper/{scraper,_internal/}
#
# 目录结构是 App 侧路径解析的契约（go/internal/toolpath 从
# VideoLib.app/Contents/MacOS/ 向上探测 tools/scraper/scraper）。
# 与 tools/package.ps1 同责，只服务 macOS 布局；CI 直接调用本脚本。
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dist="$repo/dist/VideoLib"
app="$repo/go/build/bin/VideoLib.app"
scraper="$repo/tools/bin/scraper"

# wails.json 的 name 是 "videolib"，outputfilename 是 "VideoLib"。
# macOS 上 .app 目录名跟 name，可执行文件名跟 outputfilename，于是本机产物是
# videolib.app/Contents/MacOS/VideoLib。分发目录与 CI 断言约定的是 VideoLib.app，
# 这里在找不到精确路径时再兜底认一下任意 *.app，避免大小写敏感卷或路径检查踩坑。
if [[ ! -d "$app" ]]; then
    shopt -s nullglob
    candidates=("$repo/go/build/bin/"*.app)
    shopt -u nullglob
    if [[ ${#candidates[@]} -eq 1 ]]; then
        app="${candidates[0]}"
    else
        echo "未找到 $repo/go/build/bin/VideoLib.app，请先运行 tools/build.sh 或 wails build" >&2
        exit 1
    fi
fi
if [[ ! -e "$scraper" ]]; then
    echo "未找到 $scraper，请先运行 tools/build.sh 或打包刮削器到 tools/bin/scraper" >&2
    exit 1
fi

rm -rf "$dist"
mkdir -p "$dist/tools"

# 保留 .app 内部的可执行位与符号链接。
cp -R "$app" "$dist/VideoLib.app"

# 刮削器保持 onedir 结构整体拷贝。目标目录先显式建出来，再灌「源目录的内容」，
# 而不是把源目录拷到目标路径上 —— 后者在目标不存在时会多出一层目录，
# 与 package.ps1 / toolpath 的约定不一致。
mkdir -p "$dist/tools/scraper"
cp -R "$scraper"/. "$dist/tools/scraper/"

if [[ -x "$repo/tools/bin/ffprobe" ]]; then
    cp "$repo/tools/bin/ffprobe" "$dist/tools/ffprobe"
else
    echo "警告：未找到 tools/bin/ffprobe —— 视频时长与编码信息将不可用，其余功能不受影响" >&2
fi

echo "分发目录已生成：$dist"
find "$dist" -maxdepth 3 | head -40
