"""刮削器的领域数据结构。

字段名与 Go 侧边车 JSON 的 schema 对齐（见 docs/architecture.md §7.2），
但本模块不负责落盘：schema 演进与原子写入都归 Go。
"""

from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path


@dataclass(slots=True)
class Target:
    """一个待处理的刮削目标。

    `out` 目录由 Go 侧计算并创建（`<库根>/<演员>/meta/<番号>`），
    Python 因此完全不需要知道视频库的目录布局。
    """

    fanha: str
    out: Path
    force: bool = False


@dataclass(slots=True)
class VideoMeta:
    """从站点解析出的原始元数据。"""

    fanha: str
    title: str = ""
    release_date: str = ""
    site_length_min: int | None = None
    genres: list[str] = field(default_factory=list)
    cast: list[str] = field(default_factory=list)
    cover_url: str = ""
    shot_urls: list[str] = field(default_factory=list)

    def is_usable(self) -> bool:
        """标题与封面都拿到才算可用；缺任一项按解析失败处理。"""
        return bool(self.title and self.cover_url)


@dataclass(slots=True)
class ScrapeResult:
    """一次成功刮削的完整产出，用于构造 item_done 载荷。

    `shot_files` 是**实际写入成功**的文件名，`total_shots` 是站点给出的总数。
    两者不等时说明部分截图下载失败，UI 可以据此提示「8/9 张」。
    """

    meta: VideoMeta
    cover_file: str
    shot_files: list[str]
    total_shots: int

    def to_payload(self) -> dict[str, object]:
        return {
            "title": self.meta.title,
            "release_date": self.meta.release_date,
            "site_length_min": self.meta.site_length_min,
            "genres": self.meta.genres,
            "cast": self.meta.cast,
            "cover_file": self.cover_file,
            "shot_files": self.shot_files,
            "total_shots": self.total_shots,
        }
