"""`python -m scraper` 的入口。"""

from __future__ import annotations

import sys
from multiprocessing import freeze_support


def main() -> int:
    # 导入本包已建立 stdout 隔离，此时才能安全导入 cli（它会拉进 seleniumbase）。
    from .cli import main as cli_main

    return cli_main()


if __name__ == "__main__":
    freeze_support()
    sys.exit(main())
