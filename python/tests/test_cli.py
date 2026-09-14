"""stdin job 解析测试。

输入格式是 Go 与 Python 之间的契约，任何一行不合法都必须立刻失败 ——
静默跳过会让「少刮了几条」伪装成成功。
"""

from __future__ import annotations

import io
import json

import pytest

from scraper import cli


def read(*lines: str) -> list:
    return cli._read_targets(io.StringIO("\n".join(lines)))


def test_parses_ndjson_jobs() -> None:
    targets = read(
        json.dumps({"fanha": "ipzz-001", "out": "/lib/meta/ipzz-001"}),
        "",
        json.dumps({"fanha": "ipzz-002", "out": "/lib/meta/ipzz-002", "force": True}),
    )

    assert [t.fanha for t in targets] == ["ipzz-001", "ipzz-002"]
    assert targets[0].force is False
    assert targets[1].force is True
    assert targets[1].out.name == "ipzz-002"


def test_blank_input_yields_no_targets() -> None:
    assert read("", "   ", "") == []


def test_rejects_malformed_json() -> None:
    with pytest.raises(ValueError, match="不是合法 JSON"):
        read('{"fanha": "ipzz-001"')


def test_rejects_missing_required_fields() -> None:
    with pytest.raises(ValueError, match="缺少必填字段"):
        read(json.dumps({"fanha": "ipzz-001"}))

    with pytest.raises(ValueError, match="缺少必填字段"):
        read(json.dumps({"out": "/lib/meta/ipzz-001"}))


def test_rejects_non_object_line() -> None:
    with pytest.raises(ValueError, match="不是 JSON 对象"):
        read("[1, 2, 3]")


def test_unknown_site_is_a_usage_error() -> None:
    with pytest.raises(ValueError):
        cli.get_site("不存在的站点")
