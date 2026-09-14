"""浏览器会话与反爬对抗。

================================================================================
★★★  不要修改本文件中 fetch_html() 与 open_session() 的内容  ★★★
================================================================================

这两个函数是从既有生产代码（video_scraping/src/generate.py）**逐字复制**的，
未做任何修改。其中的每一个 sleep 时长与 solve_captcha() 的调用顺序，都是在
实际站点上反复验证得出的结果。

修改任何一个数值（包括「只是把 5 秒改成 3 秒」）都可能让验证码流程失效，
而且失效时表现为「偶发失败」，极难归因。

如需调整，请先在实际站点上重新验证完整流程，再同步更新本注释与
docs/architecture.md §10 的风险条目。
================================================================================

关于日志
--------
fetch_html() 内的 print() 保持原样。协议层（protocol.py）在导入本模块之前
已把 sys.stdout 指向 stderr，所以这些 print 不会污染 NDJSON 协议流，
只会出现在 stderr 日志里。
"""

from __future__ import annotations

import sys
from collections.abc import Iterator
from contextlib import contextmanager

from seleniumbase import BaseCase, SB


def fetch_html(sb: BaseCase, url: str) -> str | None:
    try:
        sb.activate_cdp_mode(url)
        sb.sleep(5)
        sb.solve_captcha()
        sb.sleep(8)
        sb.solve_captcha()
        sb.sleep(3)
        try:
            sb.click('input[value="我同意"]', timeout=6)
            sb.sleep(3)
        except Exception:
            pass
        return sb.get_page_source()
    except Exception as exc:
        print(f"[ERROR] 无法加载页面 ({url}): {exc}")
        return None


@contextmanager
def open_session() -> Iterator[SB]:
    """打开一个已具备反爬能力的浏览器会话。

    一次会话内应先访问搜索页、再访问详情页（与原实现一致）——重建会话会
    丢失已验证的 Cloudflare 通行状态。

    `sys.argv` 补 `-n` 与 SB(uc=True, test=True, ...) 的参数组合同样属于验证过的
    用法：`-n` 让 seleniumbase 不进入需要交互的模式，缺了它验证码流程行为会变。
    """
    if "-n" not in sys.argv:
        sys.argv.append("-n")

    with SB(uc=True, test=True, locale="en", headless=True) as sb:
        yield sb
