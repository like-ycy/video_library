"""javranking 站点解析。

与 javlibrary 的根本差别
------------------------
这个站没有 Cloudflare 交互验证（只有一层前端年龄确认，状态存在 localStorage，
服务端不参与），页面也不需要 JS 渲染，所以整条链可以用纯 httpx 完成 —— 见
`base.HttpSite`。代价是它不提供「按番号查」的接口：

* 搜索页 `/zh-hans/search/?q=...` 是**客户端渲染**的，HTML 里只有一个空壳，
  真正的结果来自 `/zh-hans/search-index.json`（约 18MB，33k 条，gzip 后 3.7MB）。
* `/zh-hans/videos/<番号>/` 会被 301 到一个不存在的 `/zh-hans/v/` 路由（站点
  的疏漏），因此**不能**用番号直达，必须先从索引里拿到数字 videoId。

所以这里的流程是「一次索引 + 每个番号一次详情页」。索引按 ETag 缓存在磁盘上，
同一台机器上的后续运行只花一次 304 的时间。

索引里已经带了 title / coverUrl / releaseDate / 演员，但那只是列表页的粒度：
分类、时长、截图只在详情页里，所以仍然要取一次详情页 —— 好处是详情页比
javlibrary 的搜索结果页 + 详情页两次浏览器加载便宜得多。

截图质量
--------
`preview-gallery` 里给的是 800×534 的 JPEG（javlibrary 的 previewthumbs 更小），
但个别条目会是 147×200 这类小图，且 `data-pswp-width` 声明的 1600 是假的
（实测文件仍是 800 宽）。按原图地址下载，不做任何缩放或裁剪。

封面只提供 WebP（`static.javranking.cc/video/cover/<id>.webp`，640×480），
所以 `cover_name` 改成 `cover.webp` —— 见 `base.Site.cover_name` 的说明。
"""

from __future__ import annotations

import json
import logging
import re
import threading
import unicodedata
from pathlib import Path
from urllib.parse import urljoin

import httpx
from bs4 import BeautifulSoup, Tag

from .. import driver_dir
from ..models import VideoMeta
from .base import HttpSite, SiteUnavailable

log = logging.getLogger(__name__)

SITE = "https://javranking.cc"
LOCALE = "zh-hans"
INDEX_URL = f"{SITE}/{LOCALE}/search-index.json"
DETAIL_URL = f"{SITE}/{LOCALE}/videos/"

# 索引 18MB（gzip 3.7MB）。用客户端默认的 60 秒不够稳：实测同一个 CDN 上
# 有过 50 秒才下来的情况。只在索引上放宽，详情页仍用客户端默认超时。
INDEX_TIMEOUT = 300.0

CACHE_SUBDIR = "cache"
CACHE_NAME = "javranking-search-index.json"
ETAG_NAME = "javranking-search-index.etag"

# 分类行里混着「有码 / 无码」，它描述的是整部片的分级而不是题材，放进
# genres 会让每一部的标签里都多出一个恒定值，UI 上的分类筛选因此失效。
_SCOPE_TAGS = frozenset({"有码", "无码"})

_BRACKETS = re.compile(r"[\s_\-‐‑‒–—－]+")
_ISO_DURATION = re.compile(r"^PT(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?$")

# 详情页 JSON-LD 的 @type：站点用的是 Movie（不是 schema.org 里视频更常见的
# VideoObject）。两个都认，站点哪天换回后者也不用改代码。
_VIDEO_LD_TYPES = frozenset({"Movie", "VideoObject"})


def normalize(text: str) -> str:
    """把番号归一成可比较的键：全角转半角、去分隔符、转小写。

    `SONE-620` / `sone_620` / `ＳＯＮＥ－６２０` 都会变成 `sone620`。索引里的
    `code` 字段写法并不统一（大小写混杂、偶尔带空格），只有归一再比才靠得住。
    """
    text = unicodedata.normalize("NFKC", text or "")
    return _BRACKETS.sub("", text).lower()


class JavRankingSite(HttpSite):
    name = "javranking"
    cover_name = "cover.webp"

    def __init__(self) -> None:
        self._lock = threading.Lock()
        # 归一化番号 → [(videoId, scope)]。同一番号可能有多个条目（原版与
        # AI 修复/无码版），保留全部，取值时再挑，取第一个会随机。
        self._index: dict[str, list[tuple[str, str]]] | None = None

    # ── 索引 ──────────────────────────────────────────────────────────

    def locate(self, client: httpx.Client, fanha: str) -> str | None:
        entries = self._load_index(client).get(normalize(fanha))
        if not entries:
            return None
        # 优先有码原版：无码/AI 修复条目在中文标题里常被改写过，做封面与
        # 标题的来源不如原版可靠。
        video_id = next(
            (vid for vid, scope in entries if scope == "censored"), entries[0][0]
        )
        return f"{DETAIL_URL}{video_id}/"

    def _load_index(self, client: httpx.Client) -> dict[str, list[tuple[str, str]]]:
        """惰性加载一次索引。

        整段持锁：几十条 job 并发时，不能让每条 job 各下一份 18MB。锁只在
        第一次调用时真正阻塞，之后命中缓存直接返回。
        """
        with self._lock:
            if self._index is not None:
                return self._index
            payload = self._fetch_index_text(client)
            self._index = _build_index(payload)
            log.info(
                "javranking 索引就绪：%d 个番号（缓存 %s）",
                len(self._index),
                _cache_path(),
            )
            return self._index

    def _fetch_index_text(self, client: httpx.Client) -> str:
        cached = _read_cache()
        headers = {}
        # 只有拿得到 ETag 才做条件请求。空串照样会被发出去（httpx 不拦），
        # 服务端只会莫名其妙地回 200 全量，条件请求就白做了。
        if cached is not None and cached[1]:
            headers["If-None-Match"] = cached[1]

        try:
            response = client.get(INDEX_URL, headers=headers, timeout=INDEX_TIMEOUT)
        except httpx.HTTPError as exc:
            if cached is not None:
                log.warning("索引请求失败（%s），改用本地缓存：%s", exc, _cache_path())
                return cached[0]
            raise SiteUnavailable(
                f"搜索索引下载失败：{type(exc).__name__}: {exc}"
            ) from None

        if response.status_code == 304 and cached is not None:
            log.info("索引未变化（ETag 命中），使用本地缓存")
            return cached[0]

        if response.status_code != 200:
            if cached is not None:
                log.warning(
                    "索引 HTTP %d，改用本地缓存：%s",
                    response.status_code,
                    _cache_path(),
                )
                return cached[0]
            raise SiteUnavailable(f"搜索索引 HTTP {response.status_code}")

        # 站点没在 Content-Type 里带 charset，显式钉成 UTF-8：索引里是中文标题，
        # 让它走到「猜编码」上会静默变成乱码。
        response.encoding = "utf-8"
        text = response.text
        if not text.strip():
            raise SiteUnavailable("搜索索引响应为空")
        _write_cache(text, response.headers.get("etag", ""))
        return text

    # ── 详情页 ────────────────────────────────────────────────────────

    def parse_detail(self, html: str, fanha: str) -> VideoMeta | None:
        soup = BeautifulSoup(html, "html.parser")
        ld = _video_json_ld(soup)
        rows = _facts_rows(soup)

        title = _text(ld.get("name")) or _h1(soup)
        release_date, site_length_min = _date_and_length(ld, rows)
        genres = [
            text
            for text in _row_links(rows, "分类")
            if text and text not in _SCOPE_TAGS
        ]
        # 演员行里混着性别标记（span.actor-gender），只取链接文本。
        cast = _row_links(rows, "演员") or [
            _text(actor.get("name"))
            for actor in _as_list(ld.get("actor"))
            if isinstance(actor, dict)
        ]

        cover = _cover_url(soup, ld)
        shots = _preview_shots(soup)

        return VideoMeta(
            fanha=fanha,
            title=title,
            release_date=release_date,
            site_length_min=site_length_min,
            genres=genres,
            cast=cast,
            cover_url=cover,
            shot_urls=shots,
        )


# ── 索引缓存 ──────────────────────────────────────────────────────────────


def _cache_dir() -> Path:
    # 与驱动、下载目录同一套数据目录（driver_dir.data_dir()），因此
    # VIDEOLIB_DATA_DIR 一次就把测试与验证全部隔离到项目内。
    return driver_dir.data_dir() / CACHE_SUBDIR


def _cache_path() -> Path:
    return _cache_dir() / CACHE_NAME


def _etag_path() -> Path:
    return _cache_dir() / ETAG_NAME


def _read_cache() -> tuple[str, str] | None:
    """读磁盘缓存，返回 (正文, ETag)。读不到或损坏都当没有缓存。"""
    path = _cache_path()
    try:
        if not path.is_file() or path.stat().st_size == 0:
            return None
        text = path.read_text(encoding="utf-8")
    except OSError as exc:
        log.warning("读取索引缓存失败（%s）：%s", path, exc)
        return None
    try:
        etag = _etag_path().read_text(encoding="utf-8").strip()
    except OSError:
        etag = ""
    return text, etag


def _write_cache(text: str, etag: str) -> None:
    """原子落盘。

    半截的索引比没有索引更坏：json.loads 报错，而「删除缓存重跑」不是用户
    想得到的操作。先写 .part 再 replace，进程被杀也不会留下坏文件。
    """
    directory = _cache_dir()
    try:
        directory.mkdir(parents=True, exist_ok=True)
        tmp = directory / (CACHE_NAME + ".part")
        tmp.write_text(text, encoding="utf-8")
        tmp.replace(_cache_path())
        if etag:
            tmp_etag = directory / (ETAG_NAME + ".part")
            tmp_etag.write_text(etag, encoding="utf-8")
            tmp_etag.replace(_etag_path())
    except OSError as exc:
        # 缓存写不进去只是下次要重新下载，不该让本次刮削失败。
        log.warning("写入索引缓存失败（%s）：%s", directory, exc)


def _build_index(text: str) -> dict[str, list[tuple[str, str]]]:
    try:
        payload = json.loads(text)
    except json.JSONDecodeError as exc:
        raise SiteUnavailable(f"搜索索引不是合法 JSON：{exc}") from None

    videos = payload.get("videos") if isinstance(payload, dict) else None
    if not isinstance(videos, list):
        raise SiteUnavailable("搜索索引结构与预期不符（缺少 videos 数组）")

    index: dict[str, list[tuple[str, str]]] = {}
    for video in videos:
        if not isinstance(video, dict):
            continue
        key = normalize(_text(video.get("code")))
        video_id = video.get("videoId")
        if not key or video_id is None:
            continue
        index.setdefault(key, []).append((str(video_id), _text(video.get("scope"))))
    return index


# ── 详情页解析辅助 ────────────────────────────────────────────────────────


def _text(value: object) -> str:
    return value.strip() if isinstance(value, str) else ""


def _as_list(value: object) -> list:
    if value is None:
        return []
    if isinstance(value, list):
        return value
    return [value]


def _absolute(url: str) -> str:
    """把站点给的图片地址补成绝对 URL。

    与 javlibrary 同一条纪律：空串不能交给 urljoin，否则会得到站点首页这种
    「合法的垃圾地址」，`is_usable()` 便会误判成已拿到封面。
    """
    url = (url or "").strip()
    if not url:
        return ""
    return urljoin(SITE + "/", url)


def _h1(soup: BeautifulSoup) -> str:
    tag = soup.select_one("h1")
    return tag.get_text(strip=True) if tag else ""


def _video_json_ld(soup: BeautifulSoup) -> dict:
    """取页面里的结构化数据。

    站点的 @type 是 `Movie` 而不是常见的 `VideoObject`（两者都收）。这份数据
    比 DOM 稳定得多 —— 它是给搜索引擎看的，改版时不会动 —— title /
    datePublished / duration / image / actor 全在里面，所以优先用它。
    """
    found: dict = {}
    for tag in soup.select('script[type="application/ld+json"]'):
        raw = tag.string or tag.get_text() or ""
        try:
            data = json.loads(raw)
        except json.JSONDecodeError, TypeError:
            continue
        for obj in _flatten_ld(data):
            if obj.get("@type") in _VIDEO_LD_TYPES:
                found = obj
                break
        if found:
            break
    return found


def _flatten_ld(data: object) -> list[dict]:
    """展开 @graph 与数组形态。"""
    if isinstance(data, list):
        return [obj for item in data for obj in _flatten_ld(item)]
    if isinstance(data, dict):
        graph = data.get("@graph")
        if isinstance(graph, list):
            return _flatten_ld(graph)
        return [data]
    return []


def _facts_rows(soup: BeautifulSoup) -> dict[str, Tag]:
    """详情页右侧的事实面板：dt 是标签，dd 是值。"""
    rows: dict[str, Tag] = {}
    for row in soup.select("div.facts-panel__row"):
        label = row.select_one("dt")
        value = row.select_one("dd")
        if label is None or value is None:
            continue
        rows[label.get_text(strip=True)] = value
    return rows


def _row_links(rows: dict[str, Tag], label: str) -> list[str]:
    node = rows.get(label)
    if node is None:
        return []
    return [a.get_text(strip=True) for a in node.select("a") if a.get_text(strip=True)]


def _date_and_length(ld: dict, rows: dict[str, Tag]) -> tuple[str, int | None]:
    """发布日期与时长：JSON-LD 优先，DOM 兜底。

    DOM 上的写法是「2025年3月12日 · 120 分钟」——同一格里塞两个字段，只能
    靠正则拆；JSON-LD 给的是 ISO 8601（2025-03-12 / PT120M），因此它是首选。
    """
    date = _text(ld.get("datePublished"))
    minutes = _parse_iso_duration(_text(ld.get("duration")))

    node = rows.get("日期&时长") or rows.get("日期")
    if node is not None:
        text = node.get_text(" ", strip=True)
        if not date:
            match = re.search(r"(\d{4})\s*年\s*(\d{1,2})\s*月\s*(\d{1,2})\s*日", text)
            if match:
                year, month, day = match.groups()
                date = f"{year}-{int(month):02d}-{int(day):02d}"
            else:
                match = re.search(r"(\d{4})[-/](\d{1,2})[-/](\d{1,2})", text)
                if match:
                    year, month, day = match.groups()
                    date = f"{year}-{int(month):02d}-{int(day):02d}"
        if minutes is None:
            match = re.search(r"(\d+)\s*分钟", text)
            if match:
                minutes = int(match.group(1))
    return date, minutes


def _parse_iso_duration(text: str) -> int | None:
    """`PT120M` / `PT1H20M` → 分钟。站点目前只给到分钟粒度，秒直接舍去。"""
    match = _ISO_DURATION.match(text or "")
    if not match:
        return None
    hours, minutes, _seconds = match.groups()
    total = int(hours or 0) * 60 + int(minutes or 0)
    return total or None


def _og_image(soup: BeautifulSoup) -> str:
    tag = soup.select_one('meta[property="og:image"]')
    return _absolute(_text(tag.get("content")) if tag else "")


def _cover_url(soup: BeautifulSoup, ld: dict) -> str:
    """挑封面地址，顺序有讲究。

    1. JSON-LD 的 `image`：`/video/cover/<id>.webp`，640×480 的海报。
    2. 页面上的海报 `img`：同一个地址，作用是在 JSON-LD 缺失时兜底。
    3. `og:image`：**最后才用**。它指向 `/video/og/<id>.webp`（1200×630 的
       分享卡片，带标题文字），实测在多数片子上是 404 —— 站点只给部分片子
       生成。把它排在前面会让大量条目以「封面下载失败」收尾。
    """
    return _absolute(_text(ld.get("image"))) or _hero_cover(soup) or _og_image(soup)


def _hero_cover(soup: BeautifulSoup) -> str:
    """页面里的海报图。按文档顺序取第一个，相关推荐卡片在它之后。"""
    tag = soup.select_one('img[src*="/video/cover/"]')
    return _absolute(_text(tag.get("src")) if tag else "")


def _preview_shots(soup: BeautifulSoup) -> list[str]:
    """预览图取 `<a>` 的 href 而不是 `<img>` 的 src。

    `img` 上挂的是缩略图（站点会按容器给不同尺寸），`a` 的 href 才是原图。
    """
    shots: list[str] = []
    seen: set[str] = set()
    for anchor in soup.select("#preview-gallery a.preview-gallery__trigger"):
        url = _absolute(_text(anchor.get("href")))
        if url and url not in seen:
            seen.add(url)
            shots.append(url)
    if not shots:
        # 结构小改版时的兜底：整页里所有 samples 原图。
        for img in soup.select('img[src*="/samples/"]'):
            url = _absolute(_text(img.get("src")))
            if url and url not in seen:
                seen.add(url)
                shots.append(url)
    return shots
