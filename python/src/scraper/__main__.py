"""`python -m scraper` 的入口。"""

from __future__ import annotations

import sys
from multiprocessing import freeze_support


def main() -> int:
    # 在导入 cli（会拉进 seleniumbase）之前保证 CWD 可写，避免下载目录被
    # 固化到只读路径。
    from .driver_dir import ensure_writable_cwd

    ensure_writable_cwd()

    # 导入本包已建立 stdout 隔离，此时才能安全导入 cli（它会拉进 seleniumbase）。
    from .cli import main as cli_main

    return cli_main()


if __name__ == "__main__":
    freeze_support()
    sys.exit(main())
