"""测试夹具。

不使用 pytest 内置的 tmp_path：它建在系统临时目录（macOS 的
/private/var/folders/...）下，在受限环境里写入可能被拒绝，测试会以 error 收场
而不是给出真实结论 —— 那比失败更糟，因为它看起来像测试的问题而不是代码的问题。

这里把临时目录放在仓库内的 tests/.tmp/ 下，任何环境都能跑。
"""

from __future__ import annotations

import itertools
import os
import shutil
from collections.abc import Iterator
from pathlib import Path

import pytest

_TMP_ROOT = Path(__file__).parent / ".tmp"
_case_counter = itertools.count()


@pytest.fixture
def workdir() -> Iterator[Path]:
    """一个干净的、位于仓库内的临时目录，测试结束即清理。"""
    path = _TMP_ROOT / f"case-{os.getpid()}-{next(_case_counter)}"
    path.mkdir(parents=True, exist_ok=True)
    try:
        yield path
    finally:
        shutil.rmtree(path, ignore_errors=True)
