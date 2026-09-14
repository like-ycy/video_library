"""NDJSON 事件协议 —— 与 Go 侧之间的唯一通信通道。

stdout 纪律
-----------
seleniumbase 会往 stdout 打印大量日志。协议流一旦被污染，Go 侧的逐行 JSON
解析立刻失败，表现为「进度卡住」或「整个任务报错」。因此在**任何第三方库被
导入之前**，必须把常规 stdout 让出去，只留下一个私有通道写协议。

print() 在调用时查 sys.stdout，所以下面这个替换对本模块之后执行的所有 print
立即生效 —— 这也是为什么 browser.py 里保留原样的 print() 不会污染协议流。

因此本模块必须最先被导入。cli.py 与 __main__.py 都显式保证了这一点。
"""

from __future__ import annotations

import json
import sys
import threading
from typing import Any

PROTOCOL_VERSION = 1

# 保存真正的 stdout 作为协议通道，随后把所有常规输出改道到 stderr。
# stderr 理论上可能为 None（pythonw 等无控制台场景），此时保持原样不动，
# 宁可日志混进协议流，也不要让所有 print 直接崩掉。
_protocol_stream = sys.stdout
if sys.stderr is not None:
    sys.stdout = sys.stderr

# 进度阶段。Go 侧按这些值做文案映射，新增阶段需同步 Go 侧。
STAGES = frozenset({"search", "detail", "download_cover", "download_shots"})

# 失败原因。Go 侧按这些值决定「可否重试」与 UI 提示，因此必须是固定枚举，
# 不能塞自由文本。
REASONS = frozenset(
    {
        "not_found",  # 站点无此番号，不可重试
        "parse_failed",  # 页面结构变化，提示可能站点改版
        "download_failed",  # 图片下载失败，可重试
        "timeout",  # 页面加载超时，可重试
        "captcha_failed",  # 验证码未通过，可重试且应降低并发
        "chrome_missing",  # 环境缺 Chrome，不可重试
        "driver_failed",  # 驱动初始化失败，不可重试
        "internal_error",  # 未预期的内部错误
    }
)

_EVENT_TYPES = frozenset({"progress", "item_done", "item_failed", "done", "fatal"})

# 多个刮削线程并发写同一行流。CPython 的 GIL 让单次 write 大体安全，但
# 「写 + 换行 + flush」是三步骤，交错会产生半行 JSON 让下游整条流报废。
_write_lock = threading.Lock()


class ProtocolError(RuntimeError):
    """协议事件构造失败。

    宁可在这里抛出，也不要往 stdout 写一行结构不完整的半成品事件 ——
    下游拿到坏数据后更难定位。
    """


def _dump(payload: dict[str, Any]) -> str:
    return json.dumps(payload, ensure_ascii=False, separators=(",", ":"))


def _write_line(line: str) -> None:
    # 每行独立 flush：Go 侧按行读取，缓冲会让进度长时间不可见。
    with _write_lock:
        _protocol_stream.write(line + "\n")
        _protocol_stream.flush()


def emit_object(payload: dict[str, Any]) -> None:
    """写出一行任意 JSON 对象。

    用于 doctor / version 这类一次性查询命令：它们不产生事件流，
    但仍然必须走 stdout 且保持「stdout 只有 JSON」这条不变量。
    """
    _write_line(_dump(payload))


def _write(event_type: str, payload: dict[str, Any]) -> None:
    if event_type not in _EVENT_TYPES:
        raise ProtocolError(f"未知事件类型: {event_type}")
    _write_line(_dump({"v": PROTOCOL_VERSION, "type": event_type, **payload}))


def progress(fanha: str, stage: str, percent: float) -> None:
    """上报单条 job 的进度。percent 取值 0.0–1.0。"""
    if stage not in STAGES:
        raise ProtocolError(f"未知 stage: {stage}")
    _write(
        "progress",
        {
            "fanha": fanha,
            "stage": stage,
            "percent": round(min(max(percent, 0.0), 1.0), 4),
        },
    )


def item_done(fanha: str, data: dict[str, Any]) -> None:
    """单条成功。data 为元数据载荷，不含路径字段（路径由 Go 侧补）。"""
    _write("item_done", {"fanha": fanha, "data": data})


def item_failed(fanha: str, reason: str, detail: str = "") -> None:
    """单条失败。reason 必须是 REASONS 中的值。"""
    if reason not in REASONS:
        raise ProtocolError(f"未知 reason: {reason}")
    _write("item_failed", {"fanha": fanha, "reason": reason, "detail": detail})


def done(summary: dict[str, int]) -> None:
    """全部结束。summary 形如 {"ok": 8, "failed": 2, "skipped": 1}。"""
    _write("done", {"summary": summary})


def fatal(reason: str, detail: str = "") -> None:
    """致命错误：无法继续，进程即将退出。"""
    if reason not in REASONS:
        raise ProtocolError(f"未知 reason: {reason}")
    _write("fatal", {"reason": reason, "detail": detail})
