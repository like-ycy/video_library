#!/usr/bin/env bash
# 一键构建 macOS 本机整包：Python 刮削器 + Go 桌面 App + dist/VideoLib。
#
# 用法：
#   tools/build.sh          # 完整构建：scraper + App + dist/VideoLib
#   tools/build.sh --go     # 只重建 Go App 并组装 dist（跳过 PyInstaller）
#
# --go 适用于改样式/改 Go 后快速查看：复用 tools/bin/scraper 里已有的刮削器，
# 不再每次跑一遍 Python 打包。首次完整构建仍用默认无参方式。
#
# 版本号（本地测试版本，与 CI 推 tag 注入无关）：
#   App     → go/internal/version/version.go 的 Version（默认 0.1.0）
#             可选：VIDEOLIB_VERSION=1.2.3 tools/build.sh
#             或 wails build -ldflags "-X videolib/internal/version.Version=1.2.3"
#   scraper → python/src/scraper/_version.py（默认 0.4.3）
#             可选：SCRAPER_VERSION=1.2.3 tools/build.sh
#
# 硬约束（与 CI / architecture.md §9 一致）：
#   * PyInstaller 不能交叉编译 —— scraper 只能是本机架构。
#   * 本机自测出当前架构整包即可；Windows / 另一架构走 CI。
#
# 产物：
#   dist/VideoLib/          本机可直接运行的分发目录
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# 本地可选注入；未设置时保持源码里的测试版本号。
app_version="${VIDEOLIB_VERSION:-}"
scraper_version="${SCRAPER_VERSION:-}"

build_python=1
for arg in "$@"; do
    case "$arg" in
        --go)
            build_python=0
            ;;
        -h | --help)
            sed -n '2,23p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *)
            echo "未知参数：$arg" >&2
            echo "用法：tools/build.sh [--go]" >&2
            exit 1
            ;;
    esac
done

# 整包 3 步；--go 跳过 Python，剩 2 步（Go App + 组装）。
if ((build_python)); then
    step_total=3
else
    step_total=2
fi
step=0

host_os="$(uname -s)"
host_arch="$(uname -m)"
if ((build_python)); then
    echo "==> 目标：${host_os} ${host_arch} 整包"
else
    echo "==> 目标：${host_os} ${host_arch}（仅 Go App，跳过 Python）"
fi

if ((build_python)); then
    step=$((step + 1))
    echo
    echo "==> [$step/$step_total] 构建 Python 刮削器（本机架构）"
    if [[ -n "$scraper_version" ]]; then
        printf '__version__ = "%s"\n' "$scraper_version" \
            > "$repo/python/src/scraper/_version.py"
        echo "刮削器版本号已设为 $scraper_version"
    fi
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
else
    dest="$repo/tools/bin/scraper"
    if [[ ! -e "$dest" ]]; then
        echo "未找到已有刮削器 ${dest}。请先跑一次完整构建：tools/build.sh" >&2
        exit 1
    fi
    echo
    echo "==> 跳过 Python 刮削器打包（复用 ${dest}）"
fi

step=$((step + 1))
echo
echo "==> [$step/$step_total] 构建 Go 桌面 App（本机平台）"
cd "$repo/go"
if ! command -v wails >/dev/null 2>&1; then
    echo "未找到 wails CLI。安装：" >&2
    echo "  go install github.com/wailsapp/wails/v2/cmd/wails@latest" >&2
    exit 1
fi
wails_args=(-clean)
if [[ -n "$app_version" ]]; then
    wails_args+=(-ldflags "-X videolib/internal/version.Version=${app_version}")
    echo "App 版本号已设为 $app_version"
fi
wails build "${wails_args[@]}"
echo "产物在 $repo/go/build/bin/"

step=$((step + 1))
echo
echo "==> [$step/$step_total] 组装分发目录"
"$repo/tools/package.sh"

echo
if ((build_python)); then
    echo "完成。本机整包：$repo/dist/VideoLib"
else
    echo "完成。仅刷新 App 的分发目录：$repo/dist/VideoLib"
fi
