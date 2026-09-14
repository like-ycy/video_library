"""站点抽象。

新增站点只需实现本接口并在 sites/__init__.py 注册，cli.py 与 browser.py
都不需要改动 —— 反爬逻辑与站点解析逻辑是正交的。
"""

from __future__ import annotations

from abc import ABC, abstractmethod

from ..models import VideoMeta


class Site(ABC):
    """一个可刮削的站点。"""

    name: str

    @abstractmethod
    def search_url(self, fanha: str) -> str:
        """构造搜索页 URL。"""

    @abstractmethod
    def pick_detail_url(self, search_html: str) -> str | None:
        """从搜索页 HTML 中挑出详情页的绝对 URL；找不到返回 None。"""

    @abstractmethod
    def parse_detail(self, html: str, fanha: str) -> VideoMeta | None:
        """解析详情页；无法得到可用数据时返回 None。"""
