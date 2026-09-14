# scraper — 视频库刮削器

一个无状态的一次性抓取工具。由 Go 侧以子进程方式调用，通过 stdin 收 job、
通过 stdout 回 NDJSON 事件。

## 边界

**做**：绕过站点反爬、解析详情页、下载封面与截图、输出结构化元数据。

**不做**：写边车 JSON、写任何索引或汇总文件、提取番号、读数据库、认识 UI。
这些全部归 Go 侧。因此本模块不依赖视频库的目录布局知识——它只认一个 `out` 目录。

## 用法

```bash
uv sync

# 环境自检（stdout 恒为一行 JSON，stderr 另给人一份可读摘要）
uv run scraper doctor
uv run scraper version

# 刮削：job 列表从 stdin 读，一行一个 JSON 对象
printf '%s\n' \
  '{"fanha":"ipzz-001","out":"/tmp/lib/演员A/meta/ipzz-001"}' \
  '{"fanha":"ipzz-002","out":"/tmp/lib/演员A/meta/ipzz-002"}' \
  | uv run scraper scrape --site javlibrary --concurrency 2
```

## 退出码

| 码 | 含义 |
|---|---|
| 0 | 全部成功 |
| 1 | 部分失败（仍会输出 `done` 事件，计数反映真实结果） |
| 2 | 参数或输入格式错误 |
| 3 | 环境问题（Chrome / 驱动缺失） |
| 4 | 被取消 |
| 5 | 未捕获异常 |

## stdout / stderr 纪律

**stdout 只承载 NDJSON 协议，其余一切输出走 stderr。**

`seleniumbase` 会往 stdout 打印大量日志。协议流一旦被污染，Go 侧的逐行 JSON
解析立刻失败。`protocol.py` 在任何第三方库被导入之前把 `sys.stdout` 指向 `stderr`，
并保留私有通道用于协议输出。

因此 `protocol` 模块**必须最先被导入**。`cli.py` 与 `__main__.py` 都显式保证了这一点。

## 关于 `browser.py`

`fetch_html()` 与 `SB(...)` 构造参数逐字复制自既有生产代码，未做任何修改。
改动其中任何一个 sleep 数值都可能让验证码流程失效。详见该文件顶部注释。

## 驱动落地目录（`driver_dir.py`）

seleniumbase 会自行下载浏览器驱动，但它默认写到**自己包内**的 `drivers/`：

```python
DRIVER_DIR = os.path.dirname(os.path.realpath(drivers.__file__))
```

源码运行时那是 `.venv/.../seleniumbase/drivers/`，可写，没问题。但打包后那是
`<产物>/_internal/seleniumbase/drivers/`，两个后果：

- 装在受保护位置（如 `Program Files`）时不可写 → 驱动装不进去，刮削失败
- 覆盖安装或重新打包时驱动随产物一起丢 → 下次还要重下一次

所以 `driver_dir.activate()` 在启动时把它重定向到用户数据目录：

| 平台 | 目录 |
|---|---|
| Windows | `%LOCALAPPDATA%\videolib\drivers` |
| macOS | `~/Library/Application Support/videolib/drivers` |
| Linux | `$XDG_DATA_HOME/videolib/drivers` |

走 seleniumbase 自己的入口（`override_driver_dir()` → `settings.NEW_DRIVER_DIR`），
不硬改路径。两个硬约束：

- **先建目录再设置**：判断里带 `os.path.exists`，目录不存在就当没设置、静默退回
- **必须在构造 `SB(...)` 之前**：晚了就按默认目录算好了路径

`scraper doctor` 报出的 `driver.dir` 是**实际生效**的位置（读 `NEW_DRIVER_DIR`），
可直接用它确认重定向是否成功。

`doctor` 本身是**只读**的：它只报出驱动将来会落在哪里，不创建任何目录。
创建目录只发生在 `scrape`（真正要用驱动时）。诊断命令不该有副作用 —— 否则用户
只是想看一眼环境，主目录里就凭空多出一个目录。

### 本地验证不要污染主目录

验证驱动流程需要真的下载一次（约 35MB，耗时约 1 分钟）。用环境变量把数据目录
指到项目内，就不会写到 `~/Library/Application Support` 或 `%LOCALAPPDATA%`：

```bash
# PowerShell
$env:VIDEOLIB_DATA_DIR = "$pwd\..\.probe\sandbox"; uv run scraper doctor

# bash
VIDEOLIB_DATA_DIR=../.probe/sandbox uv run scraper doctor
```

## 打包

```bash
uv run pyinstaller scraper.spec --noconfirm
```

产物为 `dist/scraper/`（**onedir，不是 onefile**）。onefile 会让驱动目录每次落在
新的临时路径，且极易被杀软误报。详见 `docs/architecture.md` §9.1。
