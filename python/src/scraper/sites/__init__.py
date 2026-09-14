"""站点注册表。

cli.py 通过名字取站点实现，不认识任何具体站点。
"""

from __future__ import annotations

from .base import Site
from .javlibrary import JavLibrarySite

SITES: dict[str, type[Site]] = {
    JavLibrarySite.name: JavLibrarySite,
}


class UnknownSite(ValueError):
    def __init__(self, name: str) -> None:
        super().__init__(f"未知站点: {name}（可选：{', '.join(sorted(SITES))}）")
        self.name = name


def get_site(name: str) -> Site:
    try:
        return SITES[name]()
    except KeyError:
        raise UnknownSite(name) from None


__all__ = ["SITES", "Site", "UnknownSite", "get_site"]
