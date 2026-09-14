"""协议层不变量测试。

这些测试守的是「stdout 只承载 NDJSON」这条纪律。它一旦被破坏，症状会出现在
完全无关的地方（Go 侧解析失败），所以必须用测试钉住。
"""

from __future__ import annotations

import io
import json
import subprocess
import sys
import threading

import pytest

from scraper import protocol


@pytest.fixture
def captured(monkeypatch: pytest.MonkeyPatch) -> io.StringIO:
    """把协议通道换成内存流，避免测试输出污染真实 stdout。"""
    stream = io.StringIO()
    monkeypatch.setattr(protocol, "_protocol_stream", stream)
    return stream


def parse_lines(stream: io.StringIO) -> list[dict]:
    return [json.loads(line) for line in stream.getvalue().splitlines() if line]


def test_stdout_carries_only_protocol_json_in_a_fresh_interpreter() -> None:
    """核心不变量：进程的 stdout 上只能出现协议 JSON。

    必须在子进程里验证。pytest 会把 sys.stdout 与 sys.stderr 换成同一个捕获对象，
    在进程内比较两者身份永远得不出结论 —— 那样写的断言是假的，会一直通过。
    """
    code = (
        "import scraper\n"
        "print('第三方库的日志')\n"
        "from scraper import protocol\n"
        "protocol.emit_object({'type': 'doctor', 'ok': True})\n"
    )
    proc = subprocess.run(
        [sys.executable, "-c", code], capture_output=True, text=True, check=True
    )

    lines = [line for line in proc.stdout.splitlines() if line]
    assert len(lines) == 1, f"stdout 混入了非协议输出: {proc.stdout!r}"
    assert json.loads(lines[0]) == {"type": "doctor", "ok": True}
    assert "第三方库的日志" in proc.stderr


def test_print_does_not_reach_protocol_stream(captured: io.StringIO) -> None:
    """第三方库（如 seleniumbase）的 print 不得进入协议流。"""
    print("seleniumbase 的启动日志")
    assert captured.getvalue() == ""


def test_progress_line_shape(captured: io.StringIO) -> None:
    protocol.progress("ipzz-001", "search", 0.25)
    lines = parse_lines(captured)
    assert len(lines) == 1
    assert lines[0] == {
        "v": protocol.PROTOCOL_VERSION,
        "type": "progress",
        "fanha": "ipzz-001",
        "stage": "search",
        "percent": 0.25,
    }


def test_item_done_carries_payload(captured: io.StringIO) -> None:
    protocol.item_done("ipzz-001", {"title": "标题", "shot_files": []})
    line = parse_lines(captured)[0]
    assert line["type"] == "item_done"
    assert line["data"]["title"] == "标题"
    # ensure_ascii=False：中文必须以原字符输出，否则体积翻倍且不可读。
    assert "标题" in captured.getvalue()


def test_failed_and_fatal_share_reason_enum(captured: io.StringIO) -> None:
    protocol.item_failed("a", "not_found", "站点未收录")
    protocol.fatal("chrome_missing", "未找到 Chrome")
    lines = parse_lines(captured)
    assert [line["type"] for line in lines] == ["item_failed", "fatal"]
    assert lines[0]["reason"] == "not_found"
    assert lines[1]["reason"] == "chrome_missing"


def test_percent_is_clamped(captured: io.StringIO) -> None:
    protocol.progress("a", "detail", 3.5)
    protocol.progress("b", "detail", -1.0)
    percents = [line["percent"] for line in parse_lines(captured)]
    assert percents == [1.0, 0.0]


def test_rejects_unknown_stage(captured: io.StringIO) -> None:
    with pytest.raises(protocol.ProtocolError):
        protocol.progress("a", "不存在的阶段", 0.5)
    assert captured.getvalue() == ""


def test_rejects_unknown_reason(captured: io.StringIO) -> None:
    """自由文本 reason 会让 Go 侧无法分类处理，必须拒绝。"""
    with pytest.raises(protocol.ProtocolError):
        protocol.item_failed("a", "随便写的失败原因")


def test_rejects_unknown_event_type(captured: io.StringIO) -> None:
    with pytest.raises(protocol.ProtocolError):
        protocol._write("未定义事件", {})


def test_emit_object_writes_bare_json(captured: io.StringIO) -> None:
    """doctor / version 走同一通道，但输出的是普通对象而非事件。"""
    protocol.emit_object({"v": 1, "type": "doctor", "chrome": {"found": True}})
    line = parse_lines(captured)[0]
    assert line["type"] == "doctor"
    assert line["chrome"]["found"] is True


def test_concurrent_writes_never_interleave(captured: io.StringIO) -> None:
    """并发线程写同一行流不得产生半行 JSON。

    交错一旦发生，下游整条流报废，而且只在并发时偶现。
    """
    errors: list[Exception] = []

    def worker(index: int) -> None:
        try:
            for _ in range(20):
                protocol.progress(f"fanha-{index}", "search", 0.1)
        except Exception as exc:  # pragma: no cover - 失败时用于报错细节
            errors.append(exc)

    threads = [threading.Thread(target=worker, args=(i,)) for i in range(8)]
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join()

    assert not errors
    lines = parse_lines(captured)  # 解析失败会在这里抛异常
    assert len(lines) == 160
    assert {line["fanha"] for line in lines} == {f"fanha-{i}" for i in range(8)}
