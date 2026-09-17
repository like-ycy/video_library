#!/usr/bin/env bash
# 一键构建本机平台整包：Python 刮削器 + Go 桌面 App + dist/VideoLib。
#
# 用法：
#   tools/build-all.sh                          # 本机整包（默认）
#   tools/build-all.sh --cross windows/amd64    # 额外交叉编译 Go（不组装进 dist）
#   tools/build-all.sh --cross darwin/amd64     # 同上，macOS Intel
#   tools/build-all.sh --skip-python            # 只重建 Go 并重新组装
#   tools/build-all.sh --skip-go                # 只重建 Python 并重新组装
#   tools/build-all.sh --skip-package           # 只构建两侧，不组装
#
# 硬约束（与 CI / architecture.md §9 一致）：
#   * PyInstaller 不能交叉编译 —— scraper 只能是本机架构。
#   * Go / Wails 可交叉编译到 windows/amd64 与另一 darwin 架构，无需 mingw。
#   * 要拿 Windows 的 scraper.exe 或 Intel Mac 的 scraper，必须去对应机器或 CI。
#
# 产物：
#   dist/VideoLib/          本机可直接运行的分发目录
#   go/build/bin/           交叉编译时额外产出（若传了 --cross）
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

build_python=1
build_go=1
do_package=1
cross_platform=""

while [[ $# -gt 0 ]]; do
    case "$1" in
        --cross)
            if [[ $# -lt 2 ]]; then
                echo "--cross 需要平台参数，例如 windows/amd64 或 darwin/amd64" >&2
                exit 2
            fi
            cross_platform="$2"
            shift 2
            ;;
        --skip-python)  build_python=0; shift ;;
        --skip-go)      build_go=0; shift ;;
        --skip-package) do_package=0; shift ;;
        -h|--help)
            sed -n '2,18p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *)
            echo "未知参数：$1（-h 查看用法）" >&2
            exit 2
            ;;
    esac
done

host_os="$(uname -s)"
host_arch="$(uname -m)"
case "$host_os" in
    Darwin) host_label="macOS ${host_arch}" ;;
    Linux)  host_label="Linux ${host_arch}" ;;
    *)      host_label="${host_os} ${host_arch}" ;;
esac

echo "==> 目标：${host_label} 整包"
if [[ -n "$cross_platform" ]]; then
    echo "==> 额外交叉编译 Go：${cross_platform}"
fi

if [[ "$build_python" -eq 1 ]]; then
    echo
    echo "==> [1/3] 构建 Python 刮削器（本机架构，PyInstaller 不交叉编译）"
    "$repo/tools/build-python.sh"
else
    echo
    echo "==> [1/3] 跳过 Python（--skip-python）"
fi

if [[ "$build_go" -eq 1 ]]; then
    echo
    echo "==> [2/3] 构建 Go 桌面 App（本机平台）"
    "$repo/tools/build-go.sh"
else
    echo
    echo "==> [2/3] 跳过 Go（--skip-go）"
fi

if [[ -n "$cross_platform" ]]; then
    echo
    echo "==> 额外交叉编译 Go → ${cross_platform}"
    echo "    注意：这只是 App 二进制，不会替换本机 scraper，也不进 dist/。"
    "$repo/tools/build-go.sh" "$cross_platform"
fi

if [[ "$do_package" -eq 1 ]]; then
    echo
    echo "==> [3/3] 组装分发目录"
    "$repo/tools/package.sh"
else
    echo
    echo "==> [3/3] 跳过组装（--skip-package）"
fi

echo
echo "完成。本机整包：$repo/dist/VideoLib"
if [[ -n "$cross_platform" ]]; then
    echo "交叉编译产物：$repo/go/build/bin/"
    ls -1 "$repo/go/build/bin" 2>/dev/null || true
fi
