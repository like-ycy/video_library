"""命名规则一致性测试。

上报给 Go 的文件名必须与实际写盘路径完全一致。两者一旦不一致，Go 拼出来的
路径会指向不存在的文件，表现为「刮削成功但封面永远是空白」—— 这种问题在 UI
上很难归因，所以在单元测试里钉死。
"""

from __future__ import annotations

from pathlib import Path

from scraper import layout


def test_shot_name_matches_shot_path() -> None:
    # 纯路径运算，不触碰文件系统；用真实的中文路径确认不受编码影响。
    out = Path("/library/演员A/meta/ipzz-001")
    for index in (1, 2, 99):
        written = layout.shot_path(out, index)
        assert written.relative_to(out).as_posix() == layout.shot_name(index)


def test_cover_name_matches_cover_path() -> None:
    assert layout.cover_path(Path("/library/meta/ipzz-001")).name == layout.COVER_NAME


def test_prepare_is_idempotent(workdir: Path) -> None:
    layout.prepare(workdir)
    layout.prepare(workdir)
    assert (workdir / layout.IMAGES_DIR).is_dir()
