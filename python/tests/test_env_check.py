"""环境自检测试。

重点守一条：**doctor 必须只读**。

它曾经会在探测驱动时创建数据目录。用户只是想看一眼环境，主目录里就凭空多出
一个 `videolib/` 目录，而且这种副作用几乎不可能让人联想到是 doctor 干的。
探测归探测，创建目录只在真正要下载驱动时（也就是 scrape）才做。
"""

from __future__ import annotations

import os
from pathlib import Path

import pytest

from scraper import driver_dir, env_check


def _driver_name() -> str:
    return "uc_driver.exe" if os.name == "nt" else "uc_driver"


def test_check_creates_no_directory(
    monkeypatch: pytest.MonkeyPatch, workdir: Path
) -> None:
    sandbox = workdir / "should-not-be-created"
    monkeypatch.setattr(driver_dir, "data_dir", lambda: sandbox)

    health = env_check.check()

    assert not sandbox.exists(), "check() 创建了目录 —— doctor 不再是只读的"
    # 尽管不创建，仍要如实报出「将来会落在哪里」
    assert health.driver.dir == str(sandbox / "drivers")
    assert health.driver.ready is False
    assert health.driver.on_demand is True


def test_reports_ready_when_driver_present(
    monkeypatch: pytest.MonkeyPatch, workdir: Path
) -> None:
    landing = workdir / "drivers"
    landing.mkdir(parents=True)
    driver = landing / _driver_name()
    driver.write_bytes(b"stub")
    monkeypatch.setattr(driver_dir, "data_dir", lambda: workdir)

    health = env_check.check()

    assert health.driver.ready is True
    assert health.driver.source == str(driver)
    assert health.driver.on_demand is False
    assert health.driver.hint() == ""


def test_missing_directory_under_writable_parent_is_not_fatal(
    monkeypatch: pytest.MonkeyPatch, workdir: Path
) -> None:
    """目录还不存在但父目录可写 —— 这仍算可用，不该报成致命错误。

    打包产物里 drivers/ 通常就是这种状态（它只含 __init__.py，整个进了 PYZ，
    磁盘上不落地），所以这条判断直接决定了打包后能不能正常下载驱动。
    """
    monkeypatch.setattr(driver_dir, "data_dir", lambda: workdir / "not-yet")

    health = env_check.check()

    assert health.driver.writable is True
    # 提示要说明「首次会自行获取」并给出耗时预期，否则用户会以为界面卡死了。
    hint = health.driver.hint()
    assert hint != ""
    assert "首次" in hint


def test_payload_shape_matches_documented_contract(
    monkeypatch: pytest.MonkeyPatch, workdir: Path
) -> None:
    """doctor 的 JSON 字段是给 Go 解析的，改名会静默破坏契约。"""
    monkeypatch.setattr(driver_dir, "data_dir", lambda: workdir)

    payload = env_check.check().to_payload()

    assert set(payload) == {"chrome", "driver", "temp_writable"}
    assert set(payload["driver"]) == {
        "ready",
        "source",
        "on_demand",
        "dir",
        "writable",
    }
    assert set(payload["chrome"]) == {"found", "path"}
