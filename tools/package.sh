#!/usr/bin/env bash
# 组装最终分发目录（macOS），对应 docs/architecture.md §9.3 的结构。
#
# 前两件事必须已经做完：
#   tools/build-python.sh
#   tools/build-go.sh          # 本机平台，或 -platform darwin/arm64|amd64
#
# 产物：dist/VideoLib/
#   VideoLib.app/
#   tools/scraper/{scraper,_internal/}
#
# 目录结构是 App 侧路径解析的契约（go/internal/toolpath 从
# VideoLib.app/Contents/MacOS/ 向上探测 tools/scraper/scraper）。
# 与 tools/package.ps1 同责，只服务 macOS 布局。
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dist="$repo/dist/VideoLib"
app="$repo/go/build/bin/VideoLib.app"
scraper="$repo/tools/bin/scraper"

if [[ ! -d "$app" ]]; then
    echo "未找到 $app，请先运行 tools/build-go.sh" >&2
    exit 1
fi
if [[ ! -e "$scraper" ]]; then
    echo "未找到 $scraper，请先运行 tools/build-python.sh" >&2
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
