"""输出文件的命名规则。

这是 Python 与 Go 之间唯一的文件级约定。Python 按这里的规则写文件，并把
**实际写出的文件名**通过 item_done 事件报给 Go。

Go 不复制这套命名规则，只做 `out` 目录与文件名的拼接 —— 否则同一套约定
会在两种语言里各自漂移，改一边忘一边。
"""

from __future__ import annotations

from pathlib import Path

COVER_NAME = "cover.jpg"
IMAGES_DIR = "images"


def prepare(out: Path) -> None:
    """建立输出目录树。幂等，可对同一目录重复调用。"""
    (out / IMAGES_DIR).mkdir(parents=True, exist_ok=True)


def cover_path(out: Path) -> Path:
    return out / COVER_NAME


def shot_path(out: Path, index: int) -> Path:
    return out / IMAGES_DIR / f"{index}.jpg"


def shot_name(index: int) -> str:
    """截图相对 out 目录的文件名，与 shot_path() 保持一致。"""
    return f"{IMAGES_DIR}/{index}.jpg"
