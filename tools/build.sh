#!/usr/bin/env bash
# 一键构建 macOS 本机整包：Python 刮削器 + Go 桌面 App + dist/VideoLib。
#
# 用法：
#   tools/build.sh
#
# 硬约束（与 CI / architecture.md §9 一致）：
#   * PyInstaller 不能交叉编译 —— scraper 只能是本机架构。
#   * 本机自测出当前架构整包即可；Windows / 另一架构走 CI。
#
# 产物：
#   dist/VideoLib/          本机可直接运行的分发目录
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

host_os="$(uname -s)"
host_arch="$(uname -m)"
echo "==> 目标：${host_os} ${host_arch} 整包"

echo
echo "==> [1/3] 构建 Python 刮削器（本机架构）"
cd "$repo/python"
uv sync
uv run pyinstaller scraper.spec --noconfirm

dest="$repo/tools/bin/scraper"
if [[ -e "$dest" ]]; then
    rm -rf -- "$dest"
fi
mv dist/scraper "$dest"
echo "刮削器已输出到 $dest"
echo "--- 自检 ---"
"$dest/scraper" version || true

echo
echo "==> [2/3] 构建 Go 桌面 App（本机平台）"
cd "$repo/go"
if ! command -v wails >/dev/null 2>&1; then
    echo "未找到 wails CLI。安装：" >&2
    echo "  go install github.com/wailsapp/wails/v2/cmd/wails@latest" >&2
    exit 1
fi
wails build -clean
echo "产物在 $repo/go/build/bin/"

echo
echo "==> [3/3] 组装分发目录"
"$repo/tools/package.sh"

echo
echo "完成。本机整包：$repo/dist/VideoLib"
