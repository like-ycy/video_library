"""站点抽象。

新增站点只需实现下列两种接口之一并在 sites/__init__.py 注册，cli.py 与
browser.py 都不需要改动 —— 反爬逻辑与站点解析逻辑是正交的。

两种站点
--------
* `BrowserSite`：走 seleniumbase 会话取页面（javlibrary 这类有 Cloudflare 的站）
* `HttpSite`：自己用 httpx 拿页面（javranking 这类直接给 JSON 索引的站）

两者的差别只有「HTML 从哪来」这一件事：都产出同一份 HTML 交给 `parse_detail()`，
因此解析逻辑、下载、事件上报、失败原因映射全部共用一条代码路径。

`HttpSite` 存在的意义不只是省掉浏览器：没有 Cloudflare 时，一次 opensession 的
固定等待（5+8+3 秒）比真正取数据还贵，几十条 job 下来是量级差异。
"""

from __future__ import annotations

from abc import ABC, abstractmethod
from typing import TYPE_CHECKING, ClassVar

from ..models import VideoMeta

if TYPE_CHECKING:
    import httpx


class SiteUnavailable(Exception):
    """站点暂时取不到数据（网络错误、HTTP 非 200、索引下载失败）。

    存在的目的是让站点实现不必依赖 cli 的 `_FetchError`：解析层不应该知道
    协议层怎么给失败分类。cli 收到它以后映射成可重试的 `timeout`。
    """


class Site(ABC):
    """一个可刮削的站点。

    `cover_name` 是封面的落盘文件名，**扩展名必须与字节真实格式一致**：
    javlibrary 给 JPEG，javranking 只给 WebP。名字写错不会报错，只会在
    播放器/查看器里表现成一张打不开的图 —— Go 侧按扩展名推断 Content-Type，
    所以文件名就是它对外的类型声明。
    """

    name: str

    # True 表示必须由 browser.py 的会话取页面（cli 据此决定要不要检查 Chrome）。
    requires_browser: ClassVar[bool] = True
    cover_name: ClassVar[str] = "cover.jpg"

    @abstractmethod
    def parse_detail(self, html: str, fanha: str) -> VideoMeta | None:
        """解析详情页；无法得到可用数据时返回 None。"""


class BrowserSite(Site):
    """由浏览器会话取页面的站点。"""

    requires_browser: ClassVar[bool] = True

    @abstractmethod
    def search_url(self, fanha: str) -> str:
        """构造搜索页 URL。"""

    @abstractmethod
    def pick_detail_url(self, search_html: str) -> str | None:
        """从搜索页 HTML 中挑出详情页的绝对 URL；找不到返回 None。"""


class HttpSite(Site):
    """自己用 httpx 取页面的站点。"""

    requires_browser: ClassVar[bool] = False

    @abstractmethod
    def locate(self, client: httpx.Client, fanha: str) -> str | None:
        """返回详情页的绝对 URL；站点无此番号时返回 None。

        `client` 是 cli 复用的那条 `httpx.Client`（线程安全，共享连接池）。
        """
