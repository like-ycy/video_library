# -*- mode: python ; coding: utf-8 -*-
"""PyInstaller 配置。

★ 必须是 onedir，不能用 onefile
--------------------------------
onefile 每次启动都把载荷解压到一个新的临时目录。seleniumbase 会去那个临时
目录查驱动版本，发现"没有"于是重新下载 —— 每次刮削前都要白等一次下载。
此外「单文件 + 拉起浏览器 + 自动联网下载」是 Windows Defender 的高危特征组合，
产物很容易被直接删除。

构建：
    cd python
    uv add --dev pyinstaller          # 首次
    uv run pyinstaller scraper.spec --noconfirm

产物：
    dist/scraper/scraper.exe 与同级 _internal/

注意
----
Chrome 浏览器本身无法打包，目标机必须已安装。这一点由 `scraper doctor` 在
用户点「开始刮削」之前探测并给出引导，见 docs/architecture.md §9.4。
"""

from PyInstaller.utils.hooks import collect_data_files, collect_submodules

# seleniumbase 的配置与资源是运行时按路径查找的，必须整体带上。
datas = collect_data_files("seleniumbase")

# seleniumbase 大量使用动态导入，静态分析抓不全，必须显式收集子模块。
hiddenimports = collect_submodules("seleniumbase")

# 浏览器驱动不由本 spec 打包。
#
# seleniumbase 会在首次需要驱动时自行下载（browser_launcher → sb_install），
# 并在此后自行更新，所以这里不预置任何驱动文件 —— 预置反而会与它的自更新逻辑
# 打架。
#
# 但它的默认落地目录来自
#
#     DRIVER_DIR = os.path.dirname(os.path.realpath(drivers.__file__))
#
# 打包后那是 <产物>/_internal/seleniumbase/drivers/，带来两个真实问题：
#   * 装在受保护位置（如 Program Files）时不可写，驱动装不进去，刮削失败
#   * 产物被覆盖安装或重新打包时，驱动连目录一起丢，下次还要重下一次
#
# 所以 scraper 启动时会调 driver_dir.activate() 把落地目录重定向到用户数据目录
# （见 src/scraper/driver_dir.py），让驱动与产物解耦。
#
# 打包完成后应当确认重定向确实生效：从冻结产物运行 `scraper doctor`，
# 报出的 driver.dir 应当指向用户数据目录，而不是产物内部的 _internal/ 路径。

a = Analysis(  # noqa: F821
    ["scraper_main.py"],
    pathex=["src"],
    binaries=[],
    datas=datas,
    hiddenimports=hiddenimports,
    hookspath=[],
    runtime_hooks=[],
    # 只排除确定用不到的 GUI 工具包。不要顺手排除 numpy/pandas 之类：
    # seleniumbase 的部分功能会可选地用到它们，误删会让验证码流程在运行时才炸。
    excludes=["tkinter"],
    noarchive=False,
)

pyz = PYZ(a.pure)  # noqa: F821

exe = EXE(  # noqa: F821
    pyz,
    a.scripts,
    [],
    exclude_binaries=True,
    name="scraper",
    debug=False,
    bootloader_ignore_signals=False,
    strip=False,
    # 关闭 UPX：压缩后更容易被杀软误判，且对启动速度没什么帮助。
    upx=False,
    console=True,
    disable_windowed_traceback=False,
    argv_emulation=False,
    target_arch=None,
    codesign_identity=None,
    entitlements_file=None,
)

coll = COLLECT(  # noqa: F821
    exe,
    a.binaries,
    a.datas,
    strip=False,
    upx=False,
    name="scraper",
)
