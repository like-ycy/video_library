"""载荷契约测试。"""

from __future__ import annotations

from scraper.models import ScrapeResult, VideoMeta


def test_is_usable_requires_title_and_cover() -> None:
    assert not VideoMeta(fanha="a").is_usable()
    assert not VideoMeta(fanha="a", title="标题").is_usable()
    assert not VideoMeta(fanha="a", cover_url="https://x/y.jpg").is_usable()
    assert VideoMeta(fanha="a", title="标题", cover_url="https://x/y.jpg").is_usable()


def test_payload_carries_names_not_paths() -> None:
    """载荷里只放文件名，绝对路径由 Go 侧拼。

    把目录布局知识留给 Go 一处，避免两种语言各存一份约定然后各自漂移。
    """
    meta = VideoMeta(fanha="a", title="标题", cover_url="https://x/y.jpg")
    payload = ScrapeResult(
        meta=meta,
        cover_file="cover.jpg",
        shot_files=["images/1.jpg"],
        total_shots=1,
    ).to_payload()

    assert payload["cover_file"] == "cover.jpg"
    assert payload["shot_files"] == ["images/1.jpg"]
    assert "out" not in payload
    assert "path" not in payload


def test_payload_exposes_partial_download() -> None:
    """下载失败若干张截图时不算整条失败，但 UI 要能看出差异。"""
    meta = VideoMeta(fanha="a", title="标题", cover_url="https://x/y.jpg")
    payload = ScrapeResult(
        meta=meta,
        cover_file="cover.jpg",
        shot_files=["images/1.jpg"],
        total_shots=5,
    ).to_payload()

    assert len(payload["shot_files"]) == 1
    assert payload["total_shots"] == 5


def test_payload_field_names_match_sidecar_schema() -> None:
    """字段名必须与 docs/architecture.md §7.2 的边车 JSON 对齐。

    这里钉住名字：改名会被 Ruff/IDE 静默接受，但要到 Go 侧解析时才会暴露。
    """
    meta = VideoMeta(
        fanha="a",
        title="t",
        release_date="2024-01-01",
        site_length_min=120,
        genres=["x"],
        cast=["y"],
        cover_url="https://x/y.jpg",
    )
    payload = ScrapeResult(
        meta=meta, cover_file="cover.jpg", shot_files=[], total_shots=0
    ).to_payload()

    assert set(payload) == {
        "title",
        "release_date",
        "site_length_min",
        "genres",
        "cast",
        "cover_file",
        "shot_files",
        "total_shots",
    }
