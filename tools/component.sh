#!/usr/bin/env bash
# 生成可单独发布的组件包与产物自述。
#
# 用法
#   tools/component.sh                 版本取产物自述
#   tools/component.sh -v 1.2.3        产物自述读不到时用这个版本号
#   tools/component.sh --version 1.2.3
#
# 产物（HOST_OS 取 GOOS，HOST_ARCH 取 GOARCH）
#   dist/release/<PY_COMPONENT>_<ver>_<os>_<arch>.zip   顶层唯一目录 <PY_COMPONENT>/
#   dist/release/<PY_COMPONENT>_meta_<os>_<arch>.json   产物 `version` 首行的原样输出
#
# 为什么版本以产物自述为准、参数只作兜底：包名与 meta 必须同源。CI 手里的 tag
# 与产物里编进去的版本一旦分离，用户看到的会是「装完仍提示可更新」，而日志全绿。
#
# 与 tools/component.ps1 同责、同产物。命名与清单生成咬得太紧，所以不塞进
# package.sh：那份脚本四个仓库逐字相同，分叉从这里开始。
set -euo pipefail

# shellcheck source=common.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"
host_platform

version_arg=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        -v | --version)
            [[ $# -ge 2 ]] || die "$1 需要一个版本号"
            version_arg="$2"
            shift
            ;;
        -h | --help)
            sed -n '3,19p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *) die "未知参数：$1（可用：-v/--version）" ;;
    esac
    shift
done

src="$ROOT/python/build/pyi/dist/$PY_BUILD_NAME"
[[ -d "$src" ]] || die "未找到 Python 组件产物：${src}（先跑 tools/build.sh）"

suffix="${HOST_OS}_${HOST_ARCH}"
rel="$ROOT/dist/release"
mkdir -p "$rel"

step "生成组件包（$PY_COMPONENT ${HOST_OS}/${HOST_ARCH}）"

# 产物自述的第一行就是那行 JSON（{"type":"version","scraper":"..."}），
# 原样落进 meta：manifestgen 读的正是它，不在这里重新拼一份。
raw=""
bin="$src/$PY_COMPONENT"
if [[ -x "$bin" ]]; then
    raw="$("$bin" version 2>/dev/null | head -1 || true)"
fi

version=""
if [[ -n "$raw" ]] && command -v python3 >/dev/null 2>&1; then
    version="$(printf '%s' "$raw" | python3 -c '
import json, sys
try:
    print(json.load(sys.stdin).get("scraper", ""))
except Exception:
    pass
' 2>/dev/null || true)"
fi
if [[ -z "$version" ]]; then
    version="$(resolve_version "$version_arg")"
    note "产物自述不可用，版本回退为 $version"
fi

meta="$rel/${PY_COMPONENT}_meta_${suffix}.json"
if [[ -n "$raw" ]]; then
    printf '%s\n' "$raw" > "$meta"
else
    # 产物跑不起来（例如跨架构）时也要留下自述，否则 manifestgen 会缺平台；
    # 版本与包名仍保持一致，只是少了 python/协议字段。
    printf '{"scraper":"%s"}\n' "$version" > "$meta"
fi
note "自述：$(basename "$meta") → $version"

asset="$rel/${PY_COMPONENT}_${version}_${suffix}.zip"
rm -f "$asset"
# 顶层保留同名目录：解压侧按「顶层目录 + 目录内可执行文件」剥壳，多一层或
# 少一层都表现为安装完找不到可执行文件。用 common.sh 的 zip_top 与 package.sh
# 同一套打包规则（-y 保符号链接，-X 不写资源分支）。
zip_top "$ROOT/python/build/pyi/dist" "$PY_BUILD_NAME" "$asset"
assert_zip_top "$asset" "$PY_BUILD_NAME"

step "完成"
note "归档目录：$rel"
