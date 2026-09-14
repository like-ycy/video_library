"""驱动落地目录的重定向。

为什么必须重定向
----------------
seleniumbase 默认把驱动写进它自己包内的 `drivers/` 目录：

    # browser_launcher.py
    DRIVER_DIR = os.path.dirname(os.path.realpath(drivers.__file__))

源码运行时那是 `.venv/.../seleniumbase/drivers/`，可写，一切正常。但 PyInstaller
onedir 打包后，那里变成 `<产物>/_internal/seleniumbase/drivers/`，于是冒出两个
真实问题：

1. **装在受保护位置时不可写。** 例如装在 `Program Files` 下，驱动装不进去，
   刮削直接失败，而错误现场看不出跟安装位置有关。
2. **驱动会随产物一起丢。** 覆盖安装或重新打包时，`_internal/` 被替换，
   已下载的驱动一并消失，用户下次还得等一次下载。

把驱动固定到用户数据目录下，就与产物解耦了。

seleniumbase 提供的入口
-----------------------
`browser_launcher.override_driver_dir()` 会设置 `settings.NEW_DRIVER_DIR`，
而 `browser_launcher` 与 `sb_install` 在「该设置存在**且目录已存在**」时都会改用它：

    # browser_launcher.py:4130 / sb_install.py:385 两处同构的判断
    if (... getattr(sb_config.settings, "NEW_DRIVER_DIR", None)
            and os.path.exists(sb_config.settings.NEW_DRIVER_DIR)):
        driver_dir = sb_config.settings.NEW_DRIVER_DIR

「目录已存在」是硬条件：目录不存在它就当没设置，静默退回默认路径。所以本模块
必须先 mkdir 再设置，顺序不能反。

调用时机同样是硬要求：必须在构造 `SB(...)` **之前**完成，否则 browser_launcher
已经用默认目录算好了各个驱动的路径。

版本耦合说明
------------
这里用的是 seleniumbase 的内部接口，与版本强耦合。pyproject.toml 已锁死
`seleniumbase==4.51.2`；升级版本时必须重新验证本模块（尤其是 `override_driver_dir`
是否还在、`NEW_DRIVER_DIR` 是否仍被两处判断采用）。
"""

from __future__ import annotations

import logging
import os
import sys
import tempfile
from pathlib import Path

log = logging.getLogger("scraper")

# 与 Go 侧的应用名保持一致，方便用户在一个地方找到应用的数据。
_APP_DIR_NAME = "videolib"

# 覆盖数据目录的环境变量名。
#
# 存在的意义是让验证与测试能待在项目内，不去写用户目录 —— 打包验证需要真的下载
# 一次驱动（约 35MB），默认位置会把用户主目录弄脏。设置它就完全隔离了：
#
#     VIDEOLIB_DATA_DIR=./tmp uv run scraper doctor
_ENV_DATA_DIR = "VIDEOLIB_DATA_DIR"


def data_dir() -> Path:
    """返回应用的用户数据目录。

    环境变量 `VIDEOLIB_DATA_DIR` 可整体覆盖，用于测试与本地验证。

    否则按平台惯例选择位置。Windows 上刻意用 `%LOCALAPPDATA%` 而不是 `%APPDATA%`：
    后者是漫游目录，在企业域环境下会被同步到服务器，把几 MB 的浏览器驱动放进
    漫游配置里是错的。Go 侧的配置文件用 `%APPDATA%`（那才是配置该待的地方），
    两者分开是刻意的，不是遗漏。
    """
    override = os.environ.get(_ENV_DATA_DIR)
    if override:
        return Path(override)

    if sys.platform == "win32":
        raw = os.environ.get("LOCALAPPDATA") or os.environ.get("APPDATA")
        if raw:
            return Path(raw) / _APP_DIR_NAME
    elif sys.platform == "darwin":
        return Path.home() / "Library" / "Application Support" / _APP_DIR_NAME
    else:
        raw = os.environ.get("XDG_DATA_HOME")
        base = Path(raw) if raw else Path.home() / ".local" / "share"
        return base / _APP_DIR_NAME

    # 环境变量缺失（极少见）时退到临时目录：驱动能重新下载，不是不可替代的数据。
    return Path(tempfile.gettempdir()) / _APP_DIR_NAME


def target() -> Path:
    """返回我们希望的驱动目录。"""
    return data_dir() / "drivers"


def fallback() -> Path | None:
    """返回 seleniumbase 的默认驱动目录，即重定向失败时会用的位置。"""
    try:
        import seleniumbase
    except ImportError:
        return None
    return Path(seleniumbase.__file__).resolve().parent / "drivers"


def effective() -> Path | None:
    """返回实际会被使用的驱动目录；seleniumbase 不可用时返回 None。

    优先读 `NEW_DRIVER_DIR`（说明重定向已生效），否则是 seleniumbase 的默认位置。
    `doctor` 靠它如实报出实际情况，而不是报我们的意图。
    """
    try:
        from seleniumbase import config as sb_config
    except ImportError:
        return None

    override = getattr(getattr(sb_config, "settings", None), "NEW_DRIVER_DIR", None)
    if override:
        return Path(override)
    return fallback()


def activate() -> Path | None:
    """把驱动目录重定向到用户数据目录，返回生效的目录。

    幂等，可重复调用。必须在构造 `SB(...)` 之前调用。
    """
    destination = target()
    try:
        destination.mkdir(parents=True, exist_ok=True)
    except OSError as exc:
        # 连用户数据目录都建不出来（磁盘满、权限异常）。不抛异常：
        # 退回默认目录在「产物装在可写位置」时仍然能用，直接崩掉反而更糟。
        log.warning("无法创建驱动目录 %s：%s。将使用默认位置", destination, exc)
        return effective()

    try:
        from seleniumbase.core.browser_launcher import override_driver_dir
    except ImportError as exc:
        log.warning("无法导入 seleniumbase 的驱动目录接口（%s），将使用默认位置", exc)
        return effective()

    try:
        override_driver_dir(str(destination))
    except Exception as exc:
        # 这里的失败不值得让整个刮削挂掉：默认目录在多数情况下可用。
        # doctor 会报出实际生效的目录，出问题时能看出来。
        log.warning("重定向驱动目录到 %s 失败（%s），将使用默认位置", destination, exc)
        return effective()

    return effective()
