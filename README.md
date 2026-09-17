# video_library

视频库桌面应用。扫描本地视频 → 从站点刮削元数据与图片 → 在 Wails 桌面 App 中浏览与观看。

## 组成

```
./
├── python/    刮削器：只做「搜索 + 解析 + 下载图片」，输出 NDJSON 事件流
├── go/        Wails 桌面 App：扫描、索引、媒体供流、UI
├── tools/     构建脚本与运行时依赖（ffprobe、打包后的 scraper）
└── docs/      设计文档
```

两个语言的分工与接口契约见 [`docs/architecture.md`](docs/architecture.md)。要点：

- **Python 不认识 UI、不认识数据库、不写边车 JSON、不做番号提取。**
  它是一个无状态的一次性抓取工具，由 Go 以子进程方式调用。
- **Go 是唯一的编排者与数据所有者。** 文件布局、元数据 schema、索引、供流全归 Go。
- **文件系统是唯一真相来源**，SQLite 只是可随时重建的索引。

## 环境要求

| 组件 | 版本 | 说明 |
|---|---|---|
| Python | 3.14 | 由 uv 管理。锁定 `seleniumbase==4.51.2` |
| uv | 0.12+ | |
| Go | 1.23+ | |
| Wails CLI | v2 | `go install github.com/wailsapp/wails/v2/cmd/wails@latest`，装在 `$GOPATH/bin` |
| Node | 20+ | Wails 会调用，但本项目不需要前端构建 |
| Chrome | 任意近期版本 | **不可打包**，目标机必须已安装 |
| ffprobe | 6+ | 可选。放在 `tools/bin/ffprobe.exe`，缺失时只是少显示时长与编码 |

**Windows 机器是必需的，但只需要一台 —— 而且只为 PyInstaller 服务。**

PyInstaller 不是交叉编译器，官方文档明确说明这不可能改变：它在构建时需要**真的执行
目标平台的 Python** 来探测依赖，而 macOS 上跑不了 Windows 的 Python。在 Mac 上跑
只会得到 `Mach-O` 可执行文件，不是 `.exe`。

Go 侧不一样：Wails 的 Windows 后端是纯 Go（不含 cgo），所以
`tools/build-go.sh windows/amd64` 在 macOS 上就能产出真正的 `PE32+ executable`。
**两侧机制不同，别以为一个能交叉编译另一个也能。**

日常本机自测用下面的 `build-all` 一键出整包；要拿齐 Windows x64 / macOS ARM /
macOS x64 三套完整产物，用 `.github/workflows/build.yml`（推 tag `v*` 或手动触发），
不必自己准备多台机器。

## 打包与跨平台支持

| 目标 | Go / Wails | Python 刮削器 |
|---|---|---|
| Windows x64 | ✅ 可从 macOS 交叉编译，CI 有 `windows-latest` | ❌ 不能交叉编译，必须在 Windows 上打（本机或 CI） |
| macOS ARM | ✅ 本机 / CI `macos-latest` | ✅ 只能在 ARM Mac 上打 |
| macOS x64 | ✅ 可从 ARM Mac 交叉编译，CI 有 `macos-15-intel` | ❌ 不能交叉编译，必须在 Intel Mac 上打 |

### 本机一键整包

```bash
# macOS / Linux：本机架构的 scraper + App + dist/VideoLib
tools/build-all.sh

# 可选：额外交叉编译 Go 到另一平台（只出 App 二进制，不进 dist）
tools/build-all.sh --cross windows/amd64
tools/build-all.sh --cross darwin/amd64
```

```powershell
# Windows：本机 scraper.exe + App + dist\VideoLib
tools\build-all.ps1
```

### 分步打包（需要更细粒度控制时）

```bash
# 1) 目标平台上：出刮削器（PyInstaller 只能本机跑）
tools/build-python.sh          # macOS / Linux
tools\build-python.ps1         # Windows

# 2) 任意平台：出桌面 App（macOS 也能交叉编译到 Windows）
tools/build-go.sh                       # 本机
tools/build-go.sh windows/amd64         # 交叉编译到 Windows x64
tools/build-go.sh darwin/amd64          # ARM Mac 交叉编译到 Intel Mac

# 3) 目标平台上：组装分发目录
tools/package.sh               # macOS
tools\package.ps1              # Windows
```

产物在 `dist/VideoLib/`：Windows 解压即 `VideoLib.exe` + `tools/scraper/`；
macOS 解压即 `VideoLib.app` + `tools/scraper/`。

完整三平台矩阵由 GitHub Actions 打包（`.github/workflows/build.yml`）：
推 tag `v*` 或在 Actions 页手动触发，产出 Windows x64、macOS ARM、macOS x64 三套。

## 快速开始

### 刮削器（Python）

```bash
cd python
uv sync

# 环境自检（stdout 输出 JSON，stderr 另给可读摘要）
uv run scraper doctor

# 刮削：job 从 stdin 读（NDJSON）
echo '{"fanha":"ipzz-001","out":"/tmp/out/ipzz-001"}' | uv run scraper scrape --site javlibrary
```

### 桌面 App（Go）

```bash
cd go
go mod tidy
wails dev          # 开发
wails build        # 打包
```

## 关于 Cloudflare 认证代码

`python/src/scraper/browser.py` 中的 `fetch_html()` 与 `SB(...)` 构造参数，
是从既有生产代码逐字复制的，**未做任何修改**。其中的 sleep 时长与
`solve_captcha()` 调用顺序都是实际站点上验证过的结果。

改动该文件的任何一行都可能导致验证码对抗失效。如需调整，请先在实际站点上重新验证。
文件顶部有同样的警示注释。
