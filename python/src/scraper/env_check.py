"""环境自检。

Go 侧在用户点「开始刮削」之前调用，用于把「环境不可用」与「站点抓取失败」
区分开：前者要引导用户装软件，后者才是刮削器自身的问题。

这一步不能省。Chrome 无法打包进 exe，目标机是否装了 Chrome 只能在运行时探测。
没有这个检查，用户看到的会是一连串莫名其妙的抓取失败。
"""

from __future__ import annotations

import os
import shutil
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path

from . import driver_dir

_WINDOWS_CHROME = (
    r"%ProgramFiles%\Google\Chrome\Application\chrome.exe",
    r"%ProgramFiles(x86)%\Google\Chrome\Application\chrome.exe",
    r"%LOCALAPPDATA%\Google\Chrome\Application\chrome.exe",
)
_MACOS_CHROME = (
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    "/Applications/Chromium.app/Contents/MacOS/Chromium",
)
_LINUX_CHROME = (
    "google-chrome",
    "google-chrome-stable",
    "chromium",
    "chromium-browser",
)

_DRIVER_NAMES = ("uc_driver.exe", "uc_driver")


@dataclass(slots=True)
class ChromeStatus:
    found: bool = False
    path: str = ""

    def hint(self) -> str:
        if self.found:
            return ""
        return "未找到 Chrome 浏览器。刮削需要依赖本机已安装的 Chrome，请安装后重试。"


@dataclass(slots=True)
class DriverStatus:
    """浏览器驱动状态。

    `ready` 与 `on_demand` 是两件事，不要合并：

    - `ready=True` 表示驱动文件已经在落地目录里，可以直接工作。
    - `on_demand=True` 表示驱动文件不在，但 seleniumbase 会在需要时自行下载
      （内部走 browser_launcher → sb_install 的安装路径）。

    `dir` / `writable` 是为诊断打包环境而存在的：驱动目录已被重定向到用户数据
    目录（见 driver_dir.py）。如果那里不可写，或者重定向没生效而退回了产物内部的
    默认路径，驱动就装不进去 —— 而错误现场和打包规则看不出任何关系。
    """

    ready: bool = False
    source: str = ""
    on_demand: bool = False
    dir: str = ""
    writable: bool = False

    def hint(self) -> str:
        if self.ready:
            return ""
        if self.on_demand and self.writable:
            # 实测首次下载 chromedriver + uc_driver 约 90 秒，把这个数字写出来，
            # 用户才不会以为界面卡死了。
            return (
                "浏览器驱动尚未就绪，seleniumbase 会在首次刮削时自行获取"
                "（约 1 分钟，取决于网络）。"
            )
        if self.on_demand:
            return (
                f"浏览器驱动尚未就绪，且其落地目录不可写（{self.dir}）。"
                "若程序安装在受保护位置（如 Program Files），请改装到可写目录。"
            )
        return "浏览器驱动不可用。请重新安装刮削组件。"


@dataclass(slots=True)
class Health:
    chrome: ChromeStatus
    driver: DriverStatus
    temp_writable: bool

    def ok(self) -> bool:
        return (
            self.chrome.found
            and (self.driver.ready or self.driver.on_demand)
            and self.temp_writable
        )

    def to_payload(self) -> dict[str, object]:
        return {
            "chrome": {"found": self.chrome.found, "path": self.chrome.path},
            "driver": {
                "ready": self.driver.ready,
                "source": self.driver.source,
                "on_demand": self.driver.on_demand,
                "dir": self.driver.dir,
                "writable": self.driver.writable,
            },
            "temp_writable": self.temp_writable,
        }


def check() -> Health:
    return Health(
        chrome=_find_chrome(),
        driver=_find_driver(),
        temp_writable=_temp_writable(),
    )


def _find_chrome() -> ChromeStatus:
    if sys.platform == "win32":
        for raw in _WINDOWS_CHROME:
            path = Path(os.path.expandvars(raw))
            if path.exists():
                return ChromeStatus(found=True, path=str(path))
    elif sys.platform == "darwin":
        for candidate in _MACOS_CHROME:
            if Path(candidate).exists():
                return ChromeStatus(found=True, path=candidate)
    else:
        for name in _LINUX_CHROME:
            found = shutil.which(name)
            if found:
                return ChromeStatus(found=True, path=found)
    return ChromeStatus(found=False, path="")


def _find_driver() -> DriverStatus:
    """探测驱动状态。

    **只读**：绝不创建目录。doctor 是诊断命令，不该有副作用 —— 否则用户只是想
    看一眼环境，主目录里就凭空多出一个 videolib 目录。
    """
    landing = driver_dir.target()
    status = DriverStatus(dir=str(landing), writable=_dir_writable(landing))

    for name in _DRIVER_NAMES:
        candidate = landing / name
        if candidate.exists():
            status.ready = True
            status.source = str(candidate)
            return status

    # 刻意不查 PATH：本项目固定用 uc=True，而 seleniumbase 的 UC 路径是从
    # driver_dir 拼出 uc_driver 的位置，PATH 上的 chromedriver 并不会被采用。
    # 把 PATH 也算作「就绪」会让 doctor 报出一个实际跑不起来的绿色状态。
    status.on_demand = True
    status.source = "seleniumbase 具备自行获取能力"
    return status


def _dir_writable(path: Path) -> bool:
    """判断该目录是否可写；目录不存在时看它的父目录。

    seleniumbase 下载前会 os.makedirs()，所以「目录不存在但父目录可写」仍然可用
    （打包产物里 drivers/ 通常正是这种情况：它只含 __init__.py，整个进了 PYZ，
    磁盘上不落地）。而父目录也不可写（例如装在 Program Files 下）就直接失败。
    """
    target = path
    while not target.exists() and target != target.parent:
        target = target.parent
    return os.access(target, os.W_OK)


def _temp_writable() -> bool:
    """seleniumbase 需要在临时目录建 Chrome profile，不可写则必然失败。"""
    try:
        with tempfile.NamedTemporaryFile(prefix="scraper-probe-", delete=True):
            return True
    except OSError:
        return False
