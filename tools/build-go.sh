#!/usr/bin/env bash
# 构建桌面 App。
#
# 平台参数直接透传给 wails；不传则构建本机平台。
#   tools/build-go.sh                        # 本机
#   tools/build-go.sh windows/amd64          # 交叉编译到 Windows
#
# 交叉编译到 Windows **不需要 mingw**：Wails v2 的 Windows 后端是纯 Go 实现
# （内置 Go WebView2Loader），不含 cgo。因此 Windows 机器只在 PyInstaller
# 那一步是必需的。
#
# Windows 目标会带上 -webview2 download：把 WebView2 引导程序一起打进安装包，
# 覆盖没预装 WebView2 的机器。这个项目本身不依赖 WebView2 之外的运行时，
# 但缺了它应用会直接起不来。
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
platform="${1:-}"

cd "$repo/go"

if ! command -v wails >/dev/null 2>&1; then
    echo "未找到 wails CLI。安装：" >&2
    echo "  go install github.com/wailsapp/wails/v2/cmd/wails@latest" >&2
    exit 1
fi

wails_args=(build -clean)
if [[ -n "$platform" ]]; then
    wails_args+=(-platform "$platform")
fi
if [[ "$platform" == windows/* ]]; then
    wails_args+=(-webview2 download)
fi

wails "${wails_args[@]}"

echo "产物在 $repo/go/build/bin/"
