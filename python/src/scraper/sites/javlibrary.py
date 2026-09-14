"""javlibrary 站点解析。

URL 拼法与选择器迁移自既有实现（video_scraping/src/generate.py），未做改写 ——
站点改版时只需要改这一个文件并重新打包 Python，Go 侧零改动。这是把解析逻辑
留在 Python 的主要收益。

与既有实现的唯一差别：时长由字符串改为 int | None（站点偶尔返回空串或 N/A，
按缺失处理而不是让后续流程拿到脏字符串）。
"""

from __future__ import annotations

from bs4 import BeautifulSoup

from ..models import VideoMeta
from .base import Site

SITE = "https://www.javlibrary.com"
SEARCH_URL = f"{SITE}/cn/vl_searchbyid.php?keyword="


class JavLibrarySite(Site):
    name = "javlibrary"

    def search_url(self, fanha: str) -> str:
        return SEARCH_URL + fanha.lower()

    def pick_detail_url(self, search_html: str) -> str | None:
        soup = BeautifulSoup(search_html, "html.parser")
        link = soup.select_one('a[href*="/cn/jav"]')
        if not link:
            return None
        href = str(link.get("href", ""))
        if not href:
            return None
        return SITE + href

    def parse_detail(self, html: str, fanha: str) -> VideoMeta | None:
        soup = BeautifulSoup(html, "html.parser")

        title_tag = soup.select_one("#video_title h3 a")
        title = title_tag.get_text(strip=True) if title_tag else ""

        def text_after(div_id: str) -> str:
            # 文本可能直接落在 td.text，也可能在外层 td 内包的 span.text
            # （javlibrary 的实际结构）
            node = soup.select_one(f"div#{div_id} td.text") or soup.select_one(
                f"div#{div_id} td .text"
            )
            return node.get_text(strip=True) if node else ""

        release_date = text_after("video_date")
        length_text = text_after("video_length").replace("分钟", "").strip()

        genres = [a.get_text(strip=True) for a in soup.select("#video_genres .genre a")]
        cast = [
            a.get_text(strip=True)
            for a in soup.select("#video_cast span.cast span.star a")
        ]

        cover_tag = soup.select_one("#video_jacket img#video_jacket_img")
        cover = str(cover_tag.get("src", "")) if cover_tag else ""
        if cover.startswith("//"):
            cover = "https:" + cover

        shots: list[str] = []
        for img in soup.select("div.previewthumbs img"):
            src = str(img.get("src", ""))
            if src.startswith("//"):
                src = "https:" + src
            if src:
                shots.append(src)

        return VideoMeta(
            fanha=fanha,
            title=title,
            release_date=release_date,
            site_length_min=_parse_minutes(length_text),
            genres=genres,
            cast=cast,
            cover_url=cover,
            shot_urls=shots,
        )


def _parse_minutes(text: str) -> int | None:
    """站点标注的时长偶尔是空串或 N/A，按缺失处理而不是报错。"""
    return int(text) if text.isdigit() else None
