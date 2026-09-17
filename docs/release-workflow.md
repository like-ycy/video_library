# Release 工作流设计

> 本文描述 Release 的自动化构建与分发设计。
> 前置架构见 [architecture.md](architecture.md) §9；本文是其演进，取代其中
> 「整包分发」的产物形态。

## 1. 目标

- 推 tag 触发 CI，自动构建并发布 GitHub Release
- Release 包含两个独立的可执行文件，用户下载后双击即用
- 文件名固定，便于后续实现自动更新
- 全流程在 GitHub Actions 上完成，无需手动操作

## 2. 产物形态

Release 挂载两个资产，命名固定：

```
Release v1.2.3
├── video-library.exe    # Go 桌面主程序（含前端资源）
└── scraper.exe          # Python 刮削器（PyInstaller onefile）
```

### 为什么是两个文件

Go 与 Python 是不同的运行时，无法编译进同一个可执行文件。现有方案（onedir
目录整包分发）要求用户下载并解压一个 zip，内部含多层目录结构。改为两个独立
exe 后：

- 用户只需下载两个文件，不需要解压
- 两者可以独立更新（换 scraper.exe 不影响主程序，反之亦然）
- 文件名固定，自动更新器可以精确地下载、替换、校验

### 为什么 scraper 用 onefile

PyInstaller 有两种打包模式：

| 模式 | 产物 | 启动行为 |
|------|------|----------|
| onedir（旧） | `scraper.exe` + `_internal/` 目录 | 直接启动，无需解压 |
| onefile（新） | 单个 `scraper.exe` | 启动时解压载荷到临时目录再运行 |

选 onefile 的原因：Release 只挂两个 exe，用户不需要处理目录结构。

**onefile 的已知副作用及对策：**

| 副本 | 原因 | 对策 |
|------|------|------|
| 启动变慢（1-3 秒） | 每次启动解压内嵌文件到临时目录 | 可接受；刮削本身耗时远大于此 |
| 驱动重复下载 | seleniumbase 默认去产物内部目录找驱动，临时目录每次不同 | `driver_dir.py` 已将驱动重定向到 `%LOCALAPPDATA%\videolib\drivers\`，与临时目录解耦 |
| 杀软误报 | 单文件 + 拉起浏览器 + 联网是高危特征组合 | 驱动不在 exe 内部，特征已弱化；若仍被误杀，用户加白名单即可 |

> **注意**：`scraper.spec` 顶部注释仍保留 onedir 时代的警告（"必须是 onedir，
> 不能用 onefile"）。切换到 onefile 后该注释需要同步更新，否则后来者会被误导。

## 3. 用户首次使用流程

1. 从 Release 下载 `video-library.exe` 和 `scraper.exe`，放到任意目录
2. 双击 `video-library.exe` 启动应用
3. 在刮削页面点「选择刮削器」按钮，在文件对话框中指向 `scraper.exe`
4. 路径持久化到用户配置（`%APPDATA%\videolib\config.json` 的 `scraper_path` 字段）
5. 之后启动自动读取配置，无需重复选择

## 4. Workflow 流水线

文件：`.github/workflows/build.yml`

### 4.1 触发条件

```yaml
on:
  push:
    tags: ["v*"]
  workflow_dispatch:   # 手动触发，用于冒烟验证
```

tag 格式校验：`vMAJOR.MINOR.PATCH`，不匹配则立即失败。

### 4.2 Job 1：build（Windows runner）

```
checkout
  → 安装 uv + Go
  → uv sync（缓存 ~/.cache/uv）
  → PyInstaller onefile 打包 scraper
  → scraper.exe version + doctor 自检
  → 设置 wails.json productVersion = tag 去掉 v 前缀
  → wails build（ldflags 注入版本号）
  → 重命名 VideoLib.exe → video-library.exe
  → 上传 artifact
```

关键步骤说明：

**uv 缓存**：通过 `actions/cache` 缓存 uv 的包缓存目录，加速后续构建的依赖
安装。缓存 key 基于 `uv.lock` 的 hash，依赖变更时自动失效。

```yaml
- uses: actions/cache@v4
  with:
    path: ~/.cache/uv
    key: uv-${{ runner.os }}-${{ hashFiles('python/uv.lock') }}
```

**版本号注入**：构建时通过 `-ldflags "-X .../version.Version=${GITHUB_REF_NAME}"`
把 tag 写入 Go 二进制。同时更新 `wails.json` 的 `productVersion`（Wails 用它
写 Windows 版本资源，影响文件属性对话框中显示的版本）。

**产物自检**：打包完成后立即运行 `scraper.exe version` 和 `scraper.exe doctor`，
在 CI 阶段就暴露环境问题（如 seleniumbase 资源缺失），而不是等用户拿到产物
才发现。

### 4.3 Job 2：release（ubuntu runner）

```
下载 build job 的 artifact
  → gh release create/upload
```

- Release 标题：`v1.2.3`
- Release 正文：由 `--generate-notes` 自动生成
- 资产：`video-library.exe` + `scraper.exe`

使用两个 job 而非单 job 内直接上传 Release 的原因：
- 构建在 Windows（PyInstaller 不能交叉编译），Release 发布用 Linux 更快
- 与 video_cut 项目保持一致的模式
- build job 可独立重试（`workflow_dispatch` 触发时不创建 Release）

### 4.4 不做的事

| 不做 | 原因 |
|------|------|
| 预装 ChromeDriver 到产物 | 驱动版本必须与用户机器上的 Chrome 匹配；CI 上预装一个固定版本反而会不匹配。SeleniumBase 首次使用时自动下载匹配版本 |
| 打 zip 压缩包 | 用户需要的是"下载即用"，不是"下载再解压" |
| 打 MSI/安装包 | 目前是绿色软件，双击即用；未来如需安装包再考虑 |

## 5. Go 侧改动

### 5.1 新增配置读写

`config.Config.ScraperPath` 字段已存在，`resolveScraperPath()` 已优先读取
（`app.go:141`）。需要新增的是一个绑定方法供前端调用：

```
SetScraperPath(path string) error
```

职责：校验路径存在且是可执行文件 → 写入 `cfg.ScraperPath` → 保存配置 →
调用 `rebuildRunner()` 使路径立即生效。

### 5.2 前端 UI

在刮削页面工具栏（`ScrapeView.js` 的 `renderBar`）加一个「选择刮削器」按钮：

- 点击后打开 Wails 原生文件选择对话框（`runtime.OpenFileDialog`）
- 文件过滤器：`*.exe`（Windows）
- 选中后调用 `SetScraperPath`，成功后 toast 提示并刷新环境状态

当 `ScraperHealth` 报错"未找到刮削器组件"时，提示语中引导用户点此按钮选择
scraper.exe，而不是提示"运行 build-python"。

## 6. 版本号规则

- Git tag：`v1.2.3`（语义化版本）
- Go 二进制内嵌版本：`1.2.3`（通过 ldflags）
- wails.json productVersion：`1.2.3`（构建时写入）
- Release 标题：`v1.2.3`
- 资产文件名：固定 `video-library.exe` / `scraper.exe`（不含版本号，便于自动更新器按固定名下载）

## 7. 自动更新（未来）

文件名固定为自动更新铺路。后续可实现：

1. App 启动时向 GitHub Releases API 请求最新 tag
2. 比较内嵌版本号
3. 有新版时提示用户，或自动下载对应文件到临时目录
4. 替换正在运行的 exe（Windows 上需要 rename-swap：先改旧文件名，再放新文件）

当前阶段不实现，仅保证文件名和版本号的注入方式为此做好准备。
