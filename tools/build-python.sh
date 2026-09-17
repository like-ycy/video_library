#!/usr/bin/env bash
# 构建刮削器可执行文件。
#
# PyInstaller 不做交叉编译：要得到 Windows 的 .exe，必须在 Windows 上运行本脚本。
# 在 macOS / Linux 上运行会产出本地可执行文件，用于验证打包配置本身是否可用
# （典型失败：seleniumbase 的资源或 uc_driver 没被收集进去）。
#
# 用法：tools/build-python.sh
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
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
