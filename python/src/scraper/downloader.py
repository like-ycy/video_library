"""图片下载。

复用一条 httpx.Client（线程安全，跨 job 共享连接池）。已存在的文件直接跳过，
这让「中断后重跑」天然变成增量续传。
"""

from __future__ import annotations

import logging
import os
from pathlib import Path

import httpx

log = logging.getLogger(__name__)


class ImageDownloader:
    def __init__(self, client: httpx.Client, retries: int = 2) -> None:
        self._client = client
        self._retries = max(retries, 1)

    def download(self, url: str, dest: Path, *, overwrite: bool = False) -> bool:
        """把 url 下载到 dest，成功返回 True。

        `overwrite=False` 时已存在的文件直接算成功 —— 这让「中断后重跑」天然
        变成增量续传。`overwrite=True` 用于用户主动要求重新刮削（job 的 force 标志）。

        先写临时文件再 rename：中途被杀不会留下半截文件。否则下次运行会因为
        `dest.exists()` 而跳过它，把一个损坏的图片永久缓存下来。
        """
        if not overwrite and dest.exists() and dest.stat().st_size > 0:
            return True

        last_error = ""
        for attempt in range(1, self._retries + 1):
            try:
                resp = self._client.get(url)
            except httpx.HTTPError as exc:
                last_error = f"{type(exc).__name__}: {exc}"
                log.warning(
                    "下载失败(%d/%d) %s：%s", attempt, self._retries, url, last_error
                )
                continue

            if resp.status_code != 200:
                last_error = f"HTTP {resp.status_code}"
                log.warning(
                    "下载非 200(%d/%d) %s：%s",
                    attempt,
                    self._retries,
                    url,
                    last_error,
                )
                continue

            if not resp.content:
                last_error = "响应体为空"
                log.warning("下载空内容(%d/%d) %s", attempt, self._retries, url)
                continue

            dest.parent.mkdir(parents=True, exist_ok=True)
            tmp = dest.with_name(dest.name + ".part")
            tmp.write_bytes(resp.content)
            os.replace(tmp, dest)
            return True

        log.error("放弃下载 %s：%s", url, last_error)
        return False
