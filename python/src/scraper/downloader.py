"""图片下载。

复用一条 httpx.Client（线程安全，跨 job 共享连接池）。已存在的文件直接跳过，
这让「中断后重跑」天然变成增量续传。
"""

from __future__ import annotations

import logging
import os
from pathlib import Path
from urllib.parse import urlsplit

import httpx

log = logging.getLogger(__name__)


def _is_absolute_http_url(url: str) -> bool:
    """判断 url 是否是可以直接交给 httpx 的绝对 http(s) 地址。

    站点解析漏了 `urljoin` 时会漏出 `/imgs/x.jpg` 这类站内相对路径，httpx 对它的
    报错是 `UnsupportedProtocol` —— 混在一串重试 warning 里看不出根因。这里显式
    识别出来，日志里就能直接点名「地址不是绝对 URL」，而不是让人去猜网络。
    """
    parts = urlsplit(url)
    return parts.scheme in ("http", "https") and bool(parts.netloc)


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

        成功与跳过都留日志：这两种状态在磁盘上长得一模一样，而排查「日志说
        下载了、盘上没有」时，要区分的恰恰是它们。
        """
        if not overwrite and dest.exists() and dest.stat().st_size > 0:
            log.debug("已存在，跳过 %s（%d 字节）", dest, dest.stat().st_size)
            return True

        if not _is_absolute_http_url(url):
            # 重试对这个错误没有意义，直接判失败并说清原因。
            log.error("图片地址不是绝对 URL，站点解析可能漏了 urljoin：%r → %s", url, dest)
            return False

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

            try:
                dest.parent.mkdir(parents=True, exist_ok=True)
                tmp = dest.with_name(dest.name + ".part")
                tmp.write_bytes(resp.content)
                os.replace(tmp, dest)
            except OSError as exc:
                # 目录不可写、路径过长、被杀软占用……这属于「写不进去」，与
                # 「下载失败」是两类问题，日志必须能区分开。
                last_error = f"写入失败 {type(exc).__name__}: {exc}"
                log.error("无法写入 %s：%s", dest, last_error)
                continue

            # 写完立刻核对：同步盘、杀软、异常文件系统都可能让写入"成功"而文件
            # 不在（或大小不符）。不核对就会把一个幽灵路径报给 Go 写进边车 JSON，
            # 之后全系统都按它找图 —— 症状是永久性的「缺图」，且没有任何线索。
            actual = dest.stat().st_size if dest.exists() else 0
            if actual != len(resp.content):
                last_error = f"落盘校验不符（期望 {len(resp.content)} 字节，实际 {actual}）"
                log.error("写入后校验失败 %s：%s", dest, last_error)
                continue

            log.info("已写入 %s（%d 字节）← %s", dest, actual, url)
            return True

        log.error("放弃下载 %s → %s：%s", url, dest, last_error)
        return False
