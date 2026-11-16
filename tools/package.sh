#!/usr/bin/env bash
# 组装最终分发目录（macOS），对应 docs/architecture.md §9.3 的结构。
#
# 用法
#   tools/package.sh                轻量包：不含 tools/scraper（默认）
#   tools/package.sh --full         整包：含 tools/scraper，离线机器用
#   tools/package.sh --no-scraper   同上「轻量包」，显式声明（保留给旧脚本）
#
# 为什么默认是轻量包：刮削器 31MB，占分发目录 65%，而绝大多数用户第一次打开
# App 只是看视频。改为首次刮削时按需下载后，安装包从 37MB 降到 10MB，见
# docs/size-reduction-plan.md。
#
# 前两件事必须已经做完（通常由 tools/build.sh 串联）：
#   PyInstaller 产出 scraper 并放到 tools/bin/scraper（仅 --full 需要）
#   wails build 产出 go/build/bin/VideoLib.app
#
# 产物：dist/VideoLib/
#   VideoLib.app/
#   tools/scraper/{scraper,_internal/}    ← --no-scraper 时没有这一层
#
# 目录结构是 App 侧路径解析的契约（go/internal/toolpath 从
# VideoLib.app/Contents/MacOS/ 向上探测 tools/scraper/scraper）。
# 与 tools/package.ps1 同责，只服务 macOS 布局；CI 直接调用本脚本。
set -euo pipefail

# 默认轻量；--full 才带刮削器。--no-scraper 与默认等价，保留是为了让调用方
# 显式声明意图 —— 将来默认值若再变，写死参数的脚本行为不会跟着变。
want_scraper=0
saw_full=0
saw_slim=0
for arg in "$@"; do
    case "$arg" in
        --full) saw_full=1 ;;
        --no-scraper) saw_slim=1 ;;
        -h|--help)
            sed -n '4,9p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *)
            # 变量写成 ${arg} 而不是 $arg：后面紧跟全角括号时，bash 会把
            # 括号的 UTF-8 首字节当成变量名的一部分，报「arg?: unbound variable」。
            echo "未知参数：${arg}（可用：--full、--no-scraper）" >&2
            exit 2
            ;;
    esac
done
if [[ "$saw_full" == 1 && "$saw_slim" == 1 ]]; then
    echo "--full 与 --no-scraper 不能同时使用" >&2
    exit 2
fi
[[ "$saw_full" == 1 ]] && want_scraper=1

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
if [[ "$want_scraper" == 1 && ! -e "$scraper" ]]; then
    echo "未找到 ${scraper}，请先运行 tools/build.sh 或打包刮削器到 tools/bin/scraper" >&2
    echo "（默认的轻量包不需要它；要整包时用 --full）" >&2
    exit 1
fi

rm -rf "$dist"
mkdir -p "$dist/tools"

# 保留 .app 内部的可执行位与符号链接。
cp -R "$app" "$dist/VideoLib.app"

if [[ "$want_scraper" == 1 ]]; then
    # 刮削器保持 onedir 结构整体拷贝。目标目录先显式建出来，再灌「源目录的内容」，
    # 而不是把源目录拷到目标路径上 —— 后者在目标不存在时会多出一层目录，
    # 与 package.ps1 / toolpath 的约定不一致。
    mkdir -p "$dist/tools/scraper"
    cp -R "$scraper"/. "$dist/tools/scraper/"
else
    echo "轻量包：不含 tools/scraper，首次刮削时由 App 按需下载" >&2
fi

if [[ -x "$repo/tools/bin/ffprobe" ]]; then
    cp "$repo/tools/bin/ffprobe" "$dist/tools/ffprobe"
else
    echo "警告：未找到 tools/bin/ffprobe —— 视频时长与编码信息将不可用，其余功能不受影响" >&2
fi

echo "分发目录已生成：$dist"
find "$dist" -maxdepth 3 | head -40
