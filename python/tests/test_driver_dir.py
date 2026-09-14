"""驱动目录重定向测试。

守的是 R13 的核心不变量：驱动落地目录必须与安装产物解耦。一旦有人把
`driver_dir.activate()` 从 `cli.main()` 里删掉、或挪到 `SB(...)` 之后，
打包产物会静默退回「驱动装进 _internal/」的老路 —— 而这个问题在开发环境
完全看不出来（.venv 里那个目录本来就是可写的）。
"""

from __future__ import annotations

import os
import sys
from pathlib import Path

import pytest

from scraper import driver_dir

APP_DIR_NAME = "videolib"


def _settings() -> object | None:
    """取 seleniumbase 的 settings 对象；不可用时返回 None。"""
    try:
        from seleniumbase import config as sb_config
    except ImportError:  # pragma: no cover - 依赖已声明，正常不会走到
        return None
    return getattr(sb_config, "settings", None)


def test_data_dir_follows_platform_convention() -> None:
    value = driver_dir.data_dir()

    assert value.name == APP_DIR_NAME
    if sys.platform == "darwin":
        assert value == Path.home() / "Library" / "Application Support" / APP_DIR_NAME
    elif sys.platform == "win32":
        # Windows 上刻意用 LOCALAPPDATA：APPDATA 是漫游目录，
        # 把几 MB 的浏览器驱动放进漫游配置会被同步到域服务器。
        raw = os.environ.get("LOCALAPPDATA") or os.environ.get("APPDATA")
        if raw:
            assert str(value).lower().startswith(raw.lower())
    else:
        assert value.name == APP_DIR_NAME


def test_data_dir_honours_env_override(monkeypatch: pytest.MonkeyPatch) -> None:
    """VIDEOLIB_DATA_DIR 必须能整体覆盖数据目录。

    这条能力是为了让打包验证待在项目内。验证需要真的下载一次驱动（约 35MB），
    默认位置会把用户的家目录弄脏 —— 这个环境变量就是为此存在的。
    """
    sandbox = Path("/tmp/videolib-sandbox-test")
    monkeypatch.setenv("VIDEOLIB_DATA_DIR", str(sandbox))

    assert driver_dir.data_dir() == sandbox
    assert driver_dir.target() == sandbox / "drivers"


def test_target_is_drivers_under_data_dir() -> None:
    assert driver_dir.target() == driver_dir.data_dir() / "drivers"


def test_effective_prefers_new_driver_dir(
    monkeypatch: pytest.MonkeyPatch, workdir: Path
) -> None:
    """重定向生效后，effective() 必须报新位置而不是包内默认路径。"""
    settings = _settings()
    if settings is None:  # pragma: no cover
        pytest.skip("seleniumbase 不可用")

    monkeypatch.setattr(settings, "NEW_DRIVER_DIR", str(workdir), raising=False)
    assert driver_dir.effective() == workdir


def test_effective_falls_back_to_package_dir(monkeypatch: pytest.MonkeyPatch) -> None:
    """未重定向时退回 seleniumbase 包内的 drivers 目录。"""
    settings = _settings()
    if settings is None:  # pragma: no cover
        pytest.skip("seleniumbase 不可用")

    monkeypatch.setattr(settings, "NEW_DRIVER_DIR", None, raising=False)
    fallback = driver_dir.effective()

    assert fallback is not None
    assert fallback.name == "drivers"
    assert fallback.parent.name == "seleniumbase"


def test_activate_redirects_to_data_dir(
    monkeypatch: pytest.MonkeyPatch, workdir: Path
) -> None:
    """activate() 必须建出目录并把落地位置换过去。

    两点都是硬要求：
      * 目录必须先存在 —— browser_launcher 与 sb_install 判断 NEW_DRIVER_DIR 时
        都带 os.path.exists，目录不存在就当没设置、静默退回默认路径。
      * 必须在构造 SB(...) 之前调用 —— 晚了 browser_launcher 已经用默认目录
        算好了各驱动的路径。
    """
    settings = _settings()
    if settings is None:  # pragma: no cover
        pytest.skip("seleniumbase 不可用")

    # monkeypatch 会在测试结束把值恢复原状，避免污染同一进程内的其它测试。
    monkeypatch.setattr(settings, "NEW_DRIVER_DIR", None, raising=False)
    monkeypatch.setattr(driver_dir, "data_dir", lambda: workdir)

    landing = driver_dir.activate()

    assert landing is not None
    expected = workdir / "drivers"
    assert expected.is_dir(), "activate() 必须先建出目录，否则重定向会被静默忽略"
    assert landing.resolve() == expected.resolve()
    # effective() 与实际生效位置一致 —— doctor 靠它如实上报。
    assert driver_dir.effective() is not None
    assert driver_dir.effective().resolve() == expected.resolve()


def test_activate_is_idempotent(monkeypatch: pytest.MonkeyPatch, workdir: Path) -> None:
    settings = _settings()
    if settings is None:  # pragma: no cover
        pytest.skip("seleniumbase 不可用")

    monkeypatch.setattr(settings, "NEW_DRIVER_DIR", None, raising=False)
    monkeypatch.setattr(driver_dir, "data_dir", lambda: workdir)

    first = driver_dir.activate()
    second = driver_dir.activate()

    assert first is not None and second is not None
    assert first.resolve() == second.resolve()
