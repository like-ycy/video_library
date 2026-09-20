"""命令行入口。

子命令
------
scrape    刮削。job 列表从 stdin 读（NDJSON），一行一个 job。
doctor    环境自检，输出一行 JSON。
version   版本与协议信息，输出一行 JSON。

输出约定
--------
stdout 只承载 JSON（事件流或一次性查询结果），stderr 承载全部日志。
这个隔离由 scraper/__init__.py 在导入任何子模块之前建立，因此本文件可以
直接 import browser，不需要关心顺序。
"""

from __future__ import annotations

import argparse
import json
import logging
import platform
import signal
import sys
import threading
from concurrent.futures import ThreadPoolExecutor, as_completed
from contextlib import suppress
from dataclasses import dataclass
from pathlib import Path
from typing import TextIO

import httpx

from . import __version__, driver_dir, env_check, layout, protocol
from .browser import fetch_html, open_session
from .downloader import ImageDownloader
from .models import ScrapeResult, Target, VideoMeta
from .sites import get_site
from .sites.base import Site

log = logging.getLogger("scraper")

EXIT_OK = 0
EXIT_PARTIAL = 1
EXIT_USAGE = 2
EXIT_ENV = 3
EXIT_CANCELED = 4
EXIT_INTERNAL = 5

# 单条 job 各阶段在总进度中的位置。搜索页与详情页含固定的验证码等待，
# 是最慢的部分，所以权重明显偏向前面；下载只是陪跑。
_PCT_SEARCH = 0.05
_PCT_DETAIL = 0.40
_PCT_COVER = 0.55
_PCT_SHOTS_FROM = 0.55
_PCT_SHOTS_TO = 1.0


class _FetchError(Exception):
    """抓取阶段失败。reason 必须是 protocol.REASONS 中的值。"""

    def __init__(self, reason: str, detail: str = "") -> None:
        super().__init__(detail or reason)
        self.reason = reason
        self.detail = detail


class _CanceledError(Exception):
    """上层请求取消。"""


@dataclass(slots=True)
class _Outcome:
    """单条 job 的最终结果，用于汇总计数。"""

    status: str  # ok | failed | skipped
    fanha: str


# ── scrape ────────────────────────────────────────────────────────────────


def cmd_scrape(args: argparse.Namespace) -> int:
    try:
        site = get_site(args.site)
    except ValueError as exc:
        log.error("%s", exc)
        return EXIT_USAGE

    try:
        targets = _read_targets(sys.stdin)
    except ValueError as exc:
        log.error("%s", exc)
        return EXIT_USAGE

    if not targets:
        log.warning("stdin 没有收到任何 job")
        protocol.done({"ok": 0, "failed": 0, "skipped": 0})
        return EXIT_OK

    health = env_check.check()
    if not health.chrome.found:
        protocol.fatal("chrome_missing", health.chrome.hint())
        return EXIT_ENV

    cancel = threading.Event()
    _install_signal_handlers(cancel)

    counters = {"ok": 0, "failed": 0, "skipped": 0}
    log.info(
        "开始刮削 %d 条，站点=%s，并发=%d", len(targets), site.name, args.concurrency
    )

    with httpx.Client(follow_redirects=True, timeout=args.timeout) as client:
        downloader = ImageDownloader(client, retries=args.retries)
        workers = max(1, min(args.concurrency, len(targets)))
        with ThreadPoolExecutor(max_workers=workers) as pool:
            futures = [
                pool.submit(_scrape_one, target, site, downloader, cancel)
                for target in targets
            ]
            for future in as_completed(futures):
                outcome = future.result()
                counters[outcome.status] += 1

    protocol.done(counters)
    log.info("结束：%s", counters)

    if cancel.is_set():
        return EXIT_CANCELED
    return EXIT_PARTIAL if counters["failed"] else EXIT_OK


def _read_targets(stream: TextIO) -> list[Target]:
    """从 stdin 逐行读 job。任何一行不合法都直接失败，不做静默跳过 ——
    参数错误应该立刻暴露，而不是少刮几条却报成功。
    """
    targets: list[Target] = []
    for lineno, raw in enumerate(stream, 1):
        line = raw.strip()
        if not line:
            continue
        try:
            obj = json.loads(line)
        except json.JSONDecodeError as exc:
            raise ValueError(f"stdin 第 {lineno} 行不是合法 JSON：{exc}") from None
        if not isinstance(obj, dict):
            raise ValueError(f"stdin 第 {lineno} 行不是 JSON 对象")
        fanha = str(obj.get("fanha", "")).strip()
        out = str(obj.get("out", "")).strip()
        if not fanha or not out:
            raise ValueError(f"stdin 第 {lineno} 行缺少必填字段 fanha / out")
        targets.append(
            Target(fanha=fanha, out=Path(out), force=bool(obj.get("force", False)))
        )
    return targets


def _install_signal_handlers(cancel: threading.Event) -> None:
    """收到终止信号时温和退出。

    注意这只是尽力而为：Go 侧取消用的是终止整棵进程树，Python 不一定有机会
    跑到清理逻辑。真正保证「不留残留 Chrome」的是 Go 侧的 Job Object。
    """

    def handler(signum: int, _frame: object) -> None:
        if cancel.is_set():
            raise SystemExit(EXIT_CANCELED)
        cancel.set()
        log.warning("收到信号 %s，正在结束当前任务…", signum)

    for sig in (signal.SIGINT, signal.SIGTERM):
        # 非主线程或平台不支持该信号时忽略。
        with suppress(ValueError, OSError):
            signal.signal(sig, handler)


def _scrape_one(
    target: Target,
    site: Site,
    downloader: ImageDownloader,
    cancel: threading.Event,
) -> _Outcome:
    if cancel.is_set():
        return _Outcome("skipped", target.fanha)

    try:
        meta = _fetch_meta(target, site, cancel)
        _download_images(target, meta, downloader, cancel)
    except _CanceledError:
        return _Outcome("skipped", target.fanha)
    except _FetchError as exc:
        log.error("[%s] %s：%s", target.fanha, exc.reason, exc.detail)
        protocol.item_failed(target.fanha, exc.reason, exc.detail)
        return _Outcome("failed", target.fanha)

    return _Outcome("ok", target.fanha)


def _fetch_meta(target: Target, site: Site, cancel: threading.Event) -> VideoMeta:
    """用一次浏览器会话取回元数据。

    搜索页与详情页必须共用同一个会话：重建会话会丢掉已验证的 Cloudflare 通行
    状态，从而触发额外验证。这与原实现的行为一致。
    """
    protocol.progress(target.fanha, "search", _PCT_SEARCH)
    search_url = site.search_url(target.fanha)

    with open_session() as sb:
        if cancel.is_set():
            raise _CanceledError

        search_html = fetch_html(sb, search_url)
        if not search_html:
            raise _FetchError("timeout", f"搜索页加载失败：{search_url}")

        detail_url = site.pick_detail_url(search_html)
        if not detail_url:
            # 搜索页没有详情链接，即站点无此记录 —— 重试也不会变好。
            raise _FetchError("not_found", f"站点未收录：{target.fanha}")

        protocol.progress(target.fanha, "detail", _PCT_DETAIL)
        detail_html = fetch_html(sb, detail_url)
        if not detail_html:
            raise _FetchError("timeout", f"详情页加载失败：{detail_url}")

    meta = site.parse_detail(detail_html, target.fanha)
    if meta is None or not meta.is_usable():
        raise _FetchError(
            "parse_failed", "详情页解析不到标题或封面，站点结构可能已变化"
        )
    return meta


def _download_images(
    target: Target,
    meta: VideoMeta,
    downloader: ImageDownloader,
    cancel: threading.Event,
) -> None:
    layout.prepare(target.out)

    protocol.progress(target.fanha, "download_cover", _PCT_COVER)
    if not downloader.download(
        meta.cover_url, layout.cover_path(target.out), overwrite=target.force
    ):
        raise _FetchError("download_failed", f"封面下载失败：{meta.cover_url}")

    total = len(meta.shot_urls)
    shot_files: list[str] = []
    for index, url in enumerate(meta.shot_urls, 1):
        if cancel.is_set():
            raise _CanceledError
        protocol.progress(
            target.fanha,
            "download_shots",
            _PCT_SHOTS_FROM + (_PCT_SHOTS_TO - _PCT_SHOTS_FROM) * index / total,
        )
        if downloader.download(
            url, layout.shot_path(target.out, index), overwrite=target.force
        ):
            shot_files.append(layout.shot_name(index))

    # 部分截图下载失败不算整条失败：元数据与封面已经拿到，仍然有价值。
    # 数量上的差异通过 total_shots 与 shot_files 长度的对比暴露给 UI。
    if not shot_files and total:
        log.warning("[%s] %d 张截图全部下载失败", target.fanha, total)

    result = ScrapeResult(
        meta=meta,
        cover_file=layout.COVER_NAME,
        shot_files=shot_files,
        total_shots=total,
    )
    protocol.item_done(target.fanha, result.to_payload())


# ── doctor / version ──────────────────────────────────────────────────────


def cmd_doctor(_args: argparse.Namespace) -> int:
    """环境自检。

    stdout 恒为一行 JSON（供 Go 解析），stderr 另给人一份可读摘要。
    因此不需要 `--json` 这类开关 —— 一个什么都不改变的 flag 只会误导调用方。
    """
    health = env_check.check()
    protocol.emit_object(
        {
            "v": protocol.PROTOCOL_VERSION,
            "type": "doctor",
            "scraper": __version__,
            "python": platform.python_version(),
            "platform": sys.platform,
            **health.to_payload(),
        }
    )

    if health.ok():
        log.info(
            "环境就绪：chrome=%s driver=%s(%s) 临时目录可写",
            health.chrome.path,
            health.driver.ready,
            health.driver.source,
        )
        return EXIT_OK

    log.error(
        "环境不可用：chrome=%s driver=%s(%s) temp_writable=%s",
        health.chrome.found,
        health.driver.ready,
        health.driver.source,
        health.temp_writable,
    )
    for hint in (health.chrome.hint(), health.driver.hint()):
        if hint:
            log.error("%s", hint)
    return EXIT_ENV


def cmd_version(_args: argparse.Namespace) -> int:
    """版本与协议信息。stdout 恒为一行 JSON，stderr 给人一份可读摘要。"""
    payload = {
        "v": protocol.PROTOCOL_VERSION,
        "type": "version",
        "scraper": __version__,
        "python": platform.python_version(),
        "platform": sys.platform,
        "frozen": bool(getattr(sys, "frozen", False)),
    }
    protocol.emit_object(payload)
    log.info(
        "scraper %s（协议 v%d，Python %s，打包运行=%s）",
        payload["scraper"],
        payload["v"],
        payload["python"],
        payload["frozen"],
    )
    return EXIT_OK


# ── 入口 ──────────────────────────────────────────────────────────────────


def _build_parser() -> argparse.ArgumentParser:
    # --log-level 放在各子命令上，这样 `scraper scrape --log-level debug` 和
    # `scraper --log-level debug scrape` 都能用，Go 侧不必关心参数顺序。
    common = argparse.ArgumentParser(add_help=False)
    common.add_argument(
        "--log-level",
        default="info",
        choices=("debug", "info", "warning", "error"),
        help="日志级别（只影响 stderr）",
    )

    parser = argparse.ArgumentParser(prog="scraper", description="视频库刮削器")
    sub = parser.add_subparsers(dest="command", required=True)

    scrape = sub.add_parser(
        "scrape", parents=[common], help="刮削，job 从 stdin 读 NDJSON"
    )
    scrape.add_argument("--site", default="javlibrary", help="站点标识")
    scrape.add_argument(
        "--concurrency",
        type=int,
        default=2,
        choices=range(1, 9),
        metavar="N",
        help="同时处理的 job 数（1-8，默认 2）",
    )
    scrape.add_argument("--timeout", type=float, default=60.0, help="单请求超时（秒）")
    scrape.add_argument(
        "--retries",
        type=int,
        default=2,
        choices=range(1, 6),
        metavar="N",
        help="单个图片下载的重试次数",
    )
    scrape.add_argument(
        "--jobs-from",
        default="-",
        help="job 来源，'-' 表示 stdin",
    )
    scrape.set_defaults(handler=cmd_scrape)

    doctor = sub.add_parser("doctor", parents=[common], help="环境自检")
    doctor.set_defaults(handler=cmd_doctor)

    version = sub.add_parser("version", parents=[common], help="版本与协议信息")
    version.set_defaults(handler=cmd_version)

    return parser


def _configure_logging(level: str) -> None:
    # 显式指向 stderr：协议占用了真正的 stdout，日志绝不能混进去。
    logging.basicConfig(
        level=level.upper(),
        stream=sys.stderr,
        format="%(asctime)s %(levelname)s %(name)s: %(message)s",
    )


def main(argv: list[str] | None = None) -> int:
    parser = _build_parser()
    args = parser.parse_args(argv)
    _configure_logging(args.log_level)

    # 只有 scrape 需要真正重定向：它必须在任何 SB(...) 构造之前完成，否则
    # browser_launcher 已经用默认目录算好了各驱动的路径。
    #
    # doctor 刻意不调 activate()：它是只读诊断，自己算出「驱动将来会落在哪里」即可。
    # 让它创建目录是个真实的问题 —— 用户只想看一眼环境，主目录里就多出一个目录。
    if args.command == "scrape":
        log.debug("驱动目录：%s", driver_dir.activate())
        log.debug("下载目录：%s", driver_dir.redirect_downloads())

    if args.command == "scrape" and args.jobs_from != "-":
        log.error("暂不支持的 job 来源：%s（只支持 '-' 即 stdin）", args.jobs_from)
        return EXIT_USAGE

    try:
        return int(args.handler(args))
    except KeyboardInterrupt:
        log.warning("被中断")
        return EXIT_CANCELED
    except SystemExit:
        raise
    except Exception as exc:
        log.exception("未捕获异常")
        protocol.fatal("internal_error", f"{type(exc).__name__}: {exc}")
        return EXIT_INTERNAL
