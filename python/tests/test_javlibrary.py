"""站点解析测试。

使用本地 HTML 骨架而不是联网抓取：解析逻辑与反爬逻辑是正交的，
让这部分测试离线可跑、可重复，才能真正防住站点改版导致的回归。
"""

from __future__ import annotations

from pathlib import Path

import pytest

from scraper.sites.javlibrary import SITE, JavLibrarySite

FIXTURE = Path(__file__).parent / "fixtures" / "javlibrary_detail.html"


@pytest.fixture
def site() -> JavLibrarySite:
    return JavLibrarySite()


def test_search_url_lowercases_fanha(site: JavLibrarySite) -> None:
    assert (
        site.search_url("IPZZ-001") == f"{SITE}/cn/vl_searchbyid.php?keyword=ipzz-001"
    )


def test_parse_detail_extracts_every_field(site: JavLibrarySite) -> None:
    meta = site.parse_detail(FIXTURE.read_text(encoding="utf-8"), "ipzz-001")

    assert meta is not None
    assert meta.title == "IPZZ-001 测试标题"
    assert meta.release_date == "2024-03-15"
    # 覆盖 td.text 与 td .text 两条查找分支中的后者
    assert meta.site_length_min == 120
    assert meta.genres == ["类别A", "类别B"]
    assert meta.cast == ["演员A", "演员B"]
    # // 前缀必须补成 https:
    assert meta.cover_url == "https://img.example.com/ipzz-001-cover.jpg"
    assert meta.shot_urls == [
        "https://img.example.com/ipzz-001-1.jpg",
        "https://img.example.com/ipzz-001-2.jpg",
    ]
    assert meta.is_usable()


def test_parse_detail_tolerates_missing_length(site: JavLibrarySite) -> None:
    """时长缺失不该让整条记录失败 —— 元数据与图片仍然有价值。"""
    html = FIXTURE.read_text(encoding="utf-8").replace("120分钟", "N/A")
    meta = site.parse_detail(html, "ipzz-001")

    assert meta is not None
    assert meta.site_length_min is None
    assert meta.is_usable()


def test_parse_detail_on_blank_page_yields_unusable_meta(site: JavLibrarySite) -> None:
    meta = site.parse_detail("<html><body></body></html>", "ipzz-001")

    assert meta is not None
    # 由调用方据此判为 parse_failed
    assert not meta.is_usable()


def test_pick_detail_url_resolves_absolute(site: JavLibrarySite) -> None:
    assert (
        site.pick_detail_url('<a href="/cn/jav/?v=abc">详情</a>')
        == f"{SITE}/cn/jav/?v=abc"
    )


def test_pick_detail_url_returns_none_when_search_empty(site: JavLibrarySite) -> None:
    """搜索页无详情链接意味着站点未收录，调用方据此报 not_found。"""
    assert site.pick_detail_url("<html><body>无结果</body></html>") is None
