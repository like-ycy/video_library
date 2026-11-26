"""PyInstaller 的入口脚本。

为什么放在包外：PyInstaller 要求入口是一个可执行脚本文件，而
`scraper/__main__.py` 使用包内相对导入，直接当入口会破坏导入上下文。

★ freeze_support() 必须在任何重活之前调用。undetected-chromedriver 在
PyInstaller 打包后会启用 multiprocessing，缺这一步会导致进程反复重启。
"""

from multiprocessing import freeze_support

freeze_support()

# seleniumbase 在 import 时按 CWD 固化 downloaded_files 路径。macOS .app
# 启动时 CWD 往往是只读的 /，必须先切到可写目录再导入 cli / seleniumbase。
from scraper.driver_dir import ensure_writable_cwd  # noqa: E402

ensure_writable_cwd()

from scraper.__main__ import main  # noqa: E402

if __name__ == "__main__":
    raise SystemExit(main())
