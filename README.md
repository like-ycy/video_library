# video_library

视频库桌面应用。扫描本地视频 → 从站点刮削元数据与图片 → 在 Wails 桌面 App 中浏览与观看。

## 组成

```
./
├── go/                Wails 桌面 App：扫描、索引、媒体供流、UI（Go 模块根）
├── python/            刮削器：只做「搜索 + 解析 + 下载图片」，输出 NDJSON 事件流
│   ├── src/scraper/
│   ├── tests/
│   ├── packaging/     PyInstaller 的 pyinstaller.spec 与 entry.py
│   └── build/pyi/     PyInstaller 产物（不提交）
├── tools/             构建与打包脚本（build/package/component + config.env）
├── third_party/       随包分发的预置第三方二进制 <name>/<os>-<arch>/
├── dist/              组装目录与发布归档（不提交）
└── docs/              设计文档
```

目录与打包契约见 [`docs/packaging-contract.md`](docs/packaging-contract.md)。

两个语言的分工与接口契约见 [`docs/architecture.md`](docs/architecture.md)。要点：

- **Python 不认识 UI、不认识数据库、不写边车 JSON、不做番号提取。**
  它是一个无状态的一次性抓取工具，由 Go 以子进程方式调用。
- **Go 是唯一的编排者与数据所有者。** 文件布局、元数据 schema、索引、供流全归 Go。
- **文件系统是唯一真相来源**，SQLite 只是可随时重建的索引。

## 下载与安装

Release 上有两套 App 包，按需要挑一个：

| 下载项 | 体积 | 适用 |
|---|---|---|
| `videolib_<ver>_<os>_<arch>.zip` | ~10 MB | 默认。第一次刮削时 App 自己下载刮削器（约 20 MB，只下一次） |
| `videolib_<ver>_full_<os>_<arch>.zip` | ~30 MB | 拿不到 GitHub 直连，或要把整个目录拷到无网机器 |

解压后直接运行，不需要安装。刮削器缺失时的行为：

- 浏览、播放完全不受影响（这两条路径不碰刮削器）
- 点「开始刮削」时，刮削页顶部会出现「下载安装」按钮，点一下即装好并生效
- 设置 → 刮削器设置 顶部的组件卡片显示已装版本，可重新下载或更新
- 下载源可在 设置 → 刮削器设置 → 组件下载源 换成镜像前缀（形如 `https://ghproxy.example/`）
- 也可以手动指定自己编译的刮削器：设置 → 刮削器设置 → scraper 路径
- 想彻底关掉下载入口：设置 → 刮削器设置 → 关掉「缺少时允许一键下载安装」

Release 上除两类 App 包外还有 `scraper_<ver>_<os>_<arch>.zip`（组件包）、
`manifest.json`（组件清单）与 `SHA256SUMS.txt`。**这些不用手动挑**，由 App 按清单
下载并校验；手动核验下载物时才用得上校验和。产物细节与 CI 流程见
[`docs/release-workflow.md`](docs/release-workflow.md)。

## 环境要求

| 组件 | 版本 | 说明 |
|---|---|---|
| Python | 3.14 | 由 uv 管理。锁定 `seleniumbase==4.51.2` |
| uv | 0.12+ | |
| Go | 1.26+ | `go/go.mod` 声明的版本 |
| Wails CLI | v2 | `go install github.com/wailsapp/wails/v2/cmd/wails@latest`，装在 `$GOPATH/bin` |
| Node | 20+ | Wails 会调用，但本项目不需要前端构建 |
| Chrome | 任意近期版本 | **不可打包**，目标机必须已安装 |
| ffprobe | 6+ | 可选。预置在 `third_party/ffprobe/<os>-<arch>/`（如 `darwin-arm64/ffprobe`），构建时随整包进 `tools/ffprobe/`；缺失时只是少显示时长与编码 |

**Windows / 另一架构的完整产物走 CI，不在本机交叉打 scraper。**

PyInstaller 不是交叉编译器：它在构建时需要**真的执行目标平台的 Python**
来探测依赖，macOS 上跑不了 Windows 的 Python。在 Mac 上跑只会得到
`Mach-O` 可执行文件，不是 `.exe`。

Go 侧不一样：Wails 的 Windows 后端是纯 Go（不含 cgo），本机可交叉编译出
`PE32+ executable`。**两侧机制不同，别以为一个能交叉编译另一个也能。**

日常本机自测用 `tools/build.sh --full` 一键出 Mac 整包；要拿齐 Windows x64 /
macOS ARM / macOS x64 三套完整产物，用 `.github/workflows/release.yml`
（推 tag `v*` 或手动触发），不必自己准备多台机器。

## 打包与跨平台支持

| 目标 | Go / Wails | Python 刮削器 |
|---|---|---|
| Windows x64 | ✅ 可从 macOS 交叉编译，CI 有 `windows-latest` | ❌ 不能交叉编译，必须在 Windows 上打（CI） |
| macOS ARM | ✅ 本机 / CI `macos-latest` | ✅ 只能在 ARM Mac 上打 |
| macOS x64 | ✅ 可从 ARM Mac 交叉编译，CI 有 `macos-15-intel` | ❌ 不能交叉编译，必须在 Intel Mac 上打 |

### 本机一键打包（macOS）

```bash
# 本机架构整包：scraper + App + dist/VideoLib + 整包 zip
tools/build.sh --full

# 标准包（不含 tools/scraper，与 Release 默认下载项同形）
tools/build.sh

# 复用 python/build/pyi/dist 里已有的刮削器，只重建 App 并打包
tools/build.sh --no-python --full

# 只重新组装与归档，不构建
tools/build.sh --package-only --full
```

产物在 `dist/VideoLib/`（`VideoLib.app` + `tools/scraper/`），归档在
`dist/release/videolib_<ver>[_full]_darwin_<arch>.zip`，组件包在
`dist/release/scraper_<ver>_darwin_<arch>.zip`。

本地刻意打整包（含刮削器），改完代码就能直接刮一刀。想验证标准包形态用
`tools/build.sh`（不带 `--full`），CI 发的默认就是它。

组装逻辑在 `tools/package.sh`（macOS）/ `tools/package.ps1`（Windows），
组件包在 `tools/component.sh` / `tools/component.ps1`；项目差异只在
`tools/config.env`。与 CI 共用同一路径。

完整三平台矩阵由 GitHub Actions 打包（`.github/workflows/release.yml`）：
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

> 测试文件按 `.gitignore` 约定不入库（`**/*_test.go`、`python/tests/`）。
> 从仓库克隆下来跑 `go test ./...` 会显示「no test files」，属预期结果 ——
> 测试只存在于开发者本地。

### 代码检查

两侧各自独立，提交前跑一遍：

```bash
cd python && uv format && uv run ruff check
cd go && gofmt -l . && go vet ./... && go build ./...
```

## 关于 Cloudflare 认证代码

`python/src/scraper/browser.py` 中的 `fetch_html()` 与 `SB(...)` 构造参数，
是从既有生产代码逐字复制的，**未做任何修改**。其中的 sleep 时长与
`solve_captcha()` 调用顺序都是实际站点上验证过的结果。

改动该文件的任何一行都可能导致验证码对抗失效。如需调整，请先在实际站点上重新验证。
文件顶部有同样的警示注释。
