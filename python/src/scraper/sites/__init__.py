"""站点注册表。

cli.py 通过名字取站点实现，不认识任何具体站点。

`--site` 接受逗号分隔的**站点链**，按书写顺序逐个尝试，前一个失败（且失败
原因不是「不可回退」）就换下一个。默认链在 Go 侧拼：`javlibrary,javranking`。
"""

from __future__ import annotations

from .base import BrowserSite, HttpSite, Site, SiteUnavailable
from .javlibrary import JavLibrarySite
from .javranking import JavRankingSite

SITES: dict[str, type[Site]] = {
    JavLibrarySite.name: JavLibrarySite,
    JavRankingSite.name: JavRankingSite,
}


class UnknownSite(ValueError):
    def __init__(self, name: str) -> None:
        super().__init__(f"未知站点: {name}（可选：{', '.join(sorted(SITES))}）")
        self.name = name


def get_sites(spec: str) -> list[Site]:
    """把 `--site` 的值解析成有序站点列表。

    去重但保留顺序：重复写同一个站点只会让它被重试两遍，那不是用户想要
    「回退到另一个站点」的意思。
    """
    names = [part.strip() for part in spec.split(",") if part.strip()]
    if not names:
        raise UnknownSite("")

    sites: list[Site] = []
    seen: set[str] = set()
    for name in names:
        if name in seen:
            continue
        seen.add(name)
        try:
            sites.append(SITES[name]())
        except KeyError:
            raise UnknownSite(name) from None
    return sites


def get_site(name: str) -> Site:
    """取单个站点。保留给只需要一个站点的调用方（测试、手工验证）。"""
    return get_sites(name)[0]


__all__ = [
    "SITES",
    "BrowserSite",
    "HttpSite",
    "Site",
    "SiteUnavailable",
    "UnknownSite",
    "get_site",
    "get_sites",
]
