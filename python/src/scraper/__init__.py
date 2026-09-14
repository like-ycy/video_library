"""视频库刮削器。

导入顺序纪律
------------
本包必须在任何会 import seleniumbase 的子模块被导入之前建立 stdout 隔离，
否则 seleniumbase 的启动日志会写进 stdout、污染 NDJSON 协议流。

这个隔离放在 __init__.py，而不是靠「在 cli.py 里把某个 import 写在最前面」，
是因为导入重排工具（ruff 的 isort 规则等）会正确地把它挪到字母序位置 ——
一次 `ruff check --fix` 就能悄悄破坏协议，而且症状（下游解析失败）离原因很远。
放在包初始化里，顺序由 Python 的导入机制保证，格式化工具改不动。
"""

from . import protocol  # noqa: F401  必须最先执行：建立 stdout 隔离

__version__ = "0.1.0"

__all__ = ["__version__", "protocol"]
