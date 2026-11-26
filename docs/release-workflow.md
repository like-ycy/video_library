# Release 工作流设计

> 本文描述 Release 的自动化构建与分发设计。
> 前置架构见 [architecture.md](architecture.md) §9；本文是其演进，取代其中
> 「整包分发」的产物形态。

## 1. 目标

- 推 tag 触发 CI，自动构建并发布 GitHub Release
- 默认下载项尽量小：用户第一次打开只是为了看视频，不该先背 20 MB 的 Python 运行时
- 刮削器与 App 版本解耦：改一次刮削规则不必重发 App
- 断网 / 拿不到 GitHub 直连的用户也有可用形态
- 全流程在 GitHub Actions 上完成，无需手动操作

## 2. 产物形态

Release 挂载 11 个资产，命名固定：

```
Release v1.2.3
├── videolib_1.2.3_windows_amd64.zip             # 轻量包（默认下载）
├── videolib_1.2.3_darwin_arm64.zip
├── videolib_1.2.3_darwin_amd64.zip
├── videolib_1.2.3_full_windows_amd64.zip        # 整包（离线用，含刮削器）
├── videolib_1.2.3_full_darwin_arm64.zip
├── videolib_1.2.3_full_darwin_amd64.zip
├── scraper_0.4.3_windows_amd64.zip              # 刮削器组件包
├── scraper_0.4.3_darwin_arm64.zip
├── scraper_0.4.3_darwin_amd64.zip
├── manifest.json                                # 组件清单（版本/大小/sha256/URL）
└── SHA256SUMS.txt
```

平台段取 **GOOS**（`darwin` / `windows`），不是 `macos`：更新器按
`runtime.GOOS` 拼后缀找资产，改名会让老版本的自更新静默失效。归档一律 `.zip`
（macOS 用 `zip -qry -X`，`-y` 保符号链接）。

### 为什么分成四组

| 资产 | 体积 | 变更频率 | 给谁 |
|---|---|---|---|
| 轻量包 | ~10 MB | 每次发版 | 默认下载项，绝大多数人 |
| 整包 | ~30 MB | 每次发版 | 拿不到 GitHub 直连、要拷到无网机器 |
| 组件包 | ~20 MB | 只在刮削逻辑改动时 | App 按需下载，用户不直接挑 |
| manifest.json | < 1 KB | 每次发版 | App 用来决定「下哪个、多大、校验码是多少」 |

刮削器带**独立版本号**：`scraper_0.4.3_*` 里的版本来自产物自述
（`scraper version` 的第一行），不是 tag。

### 两种 App 包的目录结构

轻量包（默认）解压后得到唯一目录 `VideoLib/`，内含 App，**不含** `tools/`：

```
VideoLib/VideoLib.exe                # macOS 为 VideoLib/VideoLib.app/
```

整包（`_full_`）顶层同样是 `VideoLib/`，只多一个组件目录：

```
VideoLib/VideoLib.exe
VideoLib/tools/scraper/{scraper.exe,_internal/}
VideoLib/tools/ffprobe/ffprobe.exe   # 可选，来自 third_party/
```

`tools/<组件>/` 这一层由 `tools/package.{sh,ps1}` 定义，App 侧
（`go/internal/toolpath`）按同一路径探测。构建末尾对两种形态各有一道断言：
轻量包断言 `tools/` **不存在**（否则包会悄悄回涨 30 MB），整包断言存在且能
真跑一次 `scraper version`。

### 为什么不再「两个 exe 双击即用」

早期设计是「下载两个独立 exe」，刮削器用 PyInstaller onefile。实施中发现
onefile 每次启动都要把 20 MB 载荷解压到临时目录：启动慢 1–3 秒，且临时目录
每次不同，会把驱动搜索目录也带偏（`driver_dir.py` 因此把驱动重定向到用户
数据目录，与临时目录解耦）。onedir + zip 没有这些副作用，代价是用户解压一次。
现在更进一步：默认连刮削器都不下，第一次刮削时由 App 自己装到数据目录
（同样是 onedir 结构，只是位置换成 `<data>/components/scraper-<ver>/`）。

## 3. 用户首次使用流程

1. 从 Release 下载对应平台的轻量包（`videolib_<ver>_<os>_<arch>.zip`）
2. 解压，得到一个 `VideoLib/` 目录，直接运行其中的 App
3. 启动 App → 添加视频库 → 浏览、播放（这条路径完全不需要刮削器）
4. 第一次点「开始刮削」时，页面顶部出现「刮削器还没安装 · 下载安装」（约 20 MB），
   点一下即装好并立即生效，不需要重启

刮削器的定位顺序（`go/components.go` 的 `resolveScraperLocked`）：

```
显式配置的 scraper_path → 已下载的组件 → 随包 tools/scraper → 开发 dist/VideoLib/tools/scraper → PATH
```

组件排在随包之前：用户装过新版组件时，旧的内置副本不该压过它。整包用户不会
受影响 —— 他们没装过组件，前三档直接命中随包副本。

其他要点：

- **断网也有说法**。每个平台的构建会把「本平台那一份」组件清单编进 App
  （`go/internal/component/manifest_builtin.json`），所以离线时界面能显示
  「缺 0.4.3，21.0 MB」，而不是「未知」。
- **下载源可换，但必须是 https**。设置 → 刮削器 → 组件下载源 支持填镜像前缀
  （形如 `https://ghproxy.example/`），程序会在其后拼完整 GitHub 地址；清单
  请求与包下载都走同一个前缀。明文地址被拒（本机回环除外），因为清单与包
  同源时，纯 http 下中途改写可以让「校验通过」失去意义。镜像本身仍是信任
  委托，设置页对此有明确提示。
- **手动指定仍然有效**。设置页的「scraper 路径」优先级最高，适配自己编译的
  刮削器或压根不想用组件的用户。

## 4. Workflow 流水线

文件：`.github/workflows/release.yml`

工作流只做四件事：解析版本 → 调 `tools/build.sh` / `tools\build.ps1` →
断言产物 → 传 artifact。构建参数（wails 平台、ldflags、文件名）一律不重复
写在 YAML 里。

### 4.1 触发条件

```yaml
on:
  push:
    tags: ["v*"]
  workflow_dispatch:   # 手动触发，用于冒烟验证
```

tag 格式校验：仅 tag 构建校验 `vMAJOR.MINOR.PATCH`；分支冒烟跳过。

### 4.2 Job 1：build-app（矩阵：darwin/arm64、darwin/amd64、windows/amd64）

步骤序列（两个平台一致，构建收在脚本里）：

```
checkout（fetch-depth 0，脚本靠 git describe 取版本）
  → 解析版本（tag 去掉 v；冒烟用 0.1.0）
  → 安装 Go（按 go.mod 锁定）+ Node 22 + uv + Python 3.14
  → 安装 Wails CLI
  → 非 tag：写空清单 {"schema":1,"components":{}}
  → tools/build.sh --full -v <ver>（Windows：tools\build.ps1 -Full -Version <ver>）
      内部：PyInstaller onedir → wails build → tools/component.sh → 组装整包
  → 仅 tag：manifestgen -only <本平台> → 写 go/internal/component/manifest_builtin.json
  → 仅 tag：再跑一次 --no-python --full，把清单 embed 进 App
  → 断言整包结构（.app/Contents/MacOS/…、tools/scraper/{scraper,_internal}）
     并从最终位置真跑一次 scraper version
  → 断言瘦身生效（无 selenium-manager 残留）
  → tools/package.sh -v <ver>（Windows：tools\package.ps1）产出标准包
  → 断言标准包顶层唯一 VideoLib/ 且不含 tools/
  → 上传 dist/release/*.zip 与 scraper_meta_*.json
```

**为什么 tag 构建跑两遍 `build.sh`**：兜底清单的数据（组件包尺寸/sha256/版本）
由 `tools/component.sh` 在 `build.sh` 末尾才产出，而清单要在 `wails build`
之前存在才能被 `//go:embed` 进去。第二遍用 `--no-python` 复用 Python 产物，
只重编 App；组件包从同一棵未改动的产物树重打，内容一致，内置清单里的
sha256 与最终上传的包对得上。冒烟构建（非 tag）不需要清单里的真实数据，
直接写空清单，只跑一遍。

关键步骤说明：

**兜底清单只在 tag 构建里生成**。清单里的下载地址形如
`https://github.com/<repo>/releases/download/<ref>`，而 Release 只对 tag 存在。
手动在分支上 `workflow_dispatch` 时若照抄这套地址，包里就会带一批
`releases/download/main/...` 的死链接 —— 界面显示「可下载」，点下去必然 404。
所以分支构建走另一条分支：写入 `{"schema":1,"components":{}}` 空清单，组件
安装改走在线清单。想验证「断网也能装」这条路径，必须打 tag。

**uv 缓存**：用 `astral-sh/setup-uv`，它默认按 `uv.lock` 缓存依赖，不需要
额外的 `actions/cache` 步骤。

**版本号注入**：同一份 `VERSION`（tag 去掉 `v`；非 tag 冒烟用 `0.1.0`）交给
build 脚本，由它统一写入三处，避免 UI / 文件属性 / 刮削器自述不一致：

1. `-ldflags "-X videolib/internal/version.Version=${VERSION}"` → Go 二进制
   （前端 `GetAppVersion`、顶栏与关于页读这里）
2. `wails.json` 的 `productVersion` → Windows 文件属性 / macOS Info.plist
3. `python/src/scraper/_version.py` 的 `__version__` → PyInstaller 打包的刮削器
   （`scraper version` / 设置页环境诊断显示这里）

本地覆盖版本号：`tools/build.sh -v 1.2.3`（Windows：`tools\build.ps1 -Version 1.2.3`）。
不传时按 `git describe --tags --exact-match` 取，取不到用 `0.0.0-dev`。

> 组件包与 manifest 里的刮削器版本**不读 tag**，而是读 `scraper version` 的输出。
> 这是刻意的：清单里的版本必须与产物自述一致，否则用户装完会一直停在「可更新」。

**产物自检**：整包断言里会从 `dist/VideoLib/tools/scraper/` 真跑一次
`scraper version`，在 CI 阶段就暴露「_internal 没跟可执行文件同级」这类问题，
而不是等用户拿到产物才发现。

**组件包形状**：`tools/component.sh` 用 `zip -qry -X`（macOS）/ `Compress-Archive`
（Windows）打包，顶层保留 `scraper/`。解压侧靠 `component.TopLevelDir` 剥这一层，
而 manifestgen 的校验是「TopLevelDir + 可执行文件」，平铺的包同样能过 —— 所以
脚本本身再断言一次 zip 顶层只有 `scraper/`。这条契约只由一行命令维持，被改成
通配符时不改等用户装完才发现。

**瘦身断言**：`find python/build/pyi/dist/scraper -name 'selenium-manager*'`
有结果就失败。这三份二进制共 12.4 MB 且永不被调用（`browser_launcher.py` 显式
传了驱动路径），`packaging/pyinstaller.spec` 按名字过滤掉了。PyInstaller 升级会
改变 hook 的收集行为，把它们悄悄装回来时构建照样成功 —— 只有包体积回涨。
断在这里比等用户发现包里多东西早。

### 4.3 Job 2：release（ubuntu runner）

```
checkout + 安装 Go
  → 下载三个 build job 的 artifact
  → 按文件名收集：3 个轻量包、3 个整包、3 个组件包、3 份组件自述
  → go run ./cmd/manifestgen 合成 manifest.json（并自校验）
  → 删掉组件自述（不发布）
  → sha256sum 生成 SHA256SUMS.txt
  → 断言资产数为 11 → gh release create/upload
```

- Release 标题：`v1.2.3`
- Release 正文：由 `--generate-notes` 自动生成
- 资产：见 §2 的 11 个文件

轻量包与整包**分开计数**：两者文件名只差一个 `_full_`，用同一个 `find` 模式
会把整包算进轻量包，数量对得上而「默认下载项」可能是 40 MB 的整包。

使用多个 job 而非单 job 内直接上传 Release 的原因：
- 构建必须在原生 runner（PyInstaller 不能交叉编译），Release 发布用 Linux 更快
- 与 video_cut 项目保持一致的模式
- build job 可独立重试（`workflow_dispatch` 触发时不创建 Release）

### 4.4 组件包与清单的三条约束

这三条都是「构建全绿、用户装不上」那一类问题的防线，全部由
`go/cmd/manifestgen` 在生成清单时强制：

| 约束 | 不满足时的症状 | 拦在哪 |
|---|---|---|
| 包名里的版本 == 产物自述的版本 | 用户下载后看到版本号对不上，或一直提示更新 | 生成器按自述版本拼包名，拼不出就失败 |
| zip 内剥掉顶层目录后正好是可执行文件 | 安装完报「缺少可执行文件」 | `component.TopLevelDir` + 条目名比对 |
| 三个平台的版本一致 | 某个平台的用户永远停在「可更新」 | 比对全部 meta，不一致即失败 |

清单生成后还要用 App 的解析路径（`component.ParseManifest` + `Spec`）读一遍：
字段名拼错、schema 不对、URL 协议不对，都会在构建时失败。

### 4.5 不做的事

| 不做 | 原因 |
|------|------|
| 预装 ChromeDriver 到产物 | 驱动版本必须与用户机器上的 Chrome 匹配；CI 上预装一个固定版本反而会不匹配。SeleniumBase 首次使用时自动下载匹配版本 |
| 打 MSI/安装包 | 目前是绿色软件，解压即用；未来如需安装包再考虑 |
| 把组件自述（`scraper_meta_*.json`）也发到 Release | 它是清单的输入，信息已在 manifest 里；多三个 json 只会让下载列表更难挑 |

## 5. App 侧的按需组件

### 5.1 配置项

`config.Config` 新增两个字段（`go/internal/config/config.go`）：

| 字段 | 默认 | 作用 |
|---|---|---|
| `components_on_demand` | `true` | 缺少刮削器时是否提供「下载安装」入口。只管入口：已装的组件永远生效，关掉开关不会让它突然找不到 |
| `component_mirror` | 空 | 组件下载源前缀，清单请求与包下载都走它 |

### 5.2 绑定方法

`go/components.go`：

```
ListComponents()  []ComponentDTO   // 状态：state / 已装版本 / 可装版本 / 大小 / 当前生效路径与来源
InstallComponent(name string) error // 立即返回，进度走 component:progress 事件
RemoveComponent(name string) error  // 只删组件目录，不动手动配置与随包副本
```

事件：`component:progress`（`{name,done,total}`，按 100ms 限流）、
`component:done`、`component:failed`。

### 5.3 清单来源

1. 在线清单 `https://github.com/<repo>/releases/latest/download/manifest.json`
   （TTL 6 小时，页面打开时后台刷新）；
2. 编译进 App 的兜底清单（CI 用 `manifestgen -only <本平台>` 生成）。

协议不匹配时退回兜底清单：线上清单可能来自更晚的 Release、组件是给新协议
准备的，装上去只会报「协议不匹配」。

本地联调可用 `VIDEOLIB_COMPONENT_MANIFEST_URL` 指定清单地址；它只改「清单从
哪来」，包本身仍按清单里的 sha256 校验。

### 5.4 前端

- 设置 → 刮削器：每个组件一张卡片（状态徽标、已装/最新版本、下载大小、
  当前生效路径与来源、下载/移除按钮、进度条）
- 刮削页：组件缺失且开关为开时，顶部出现「还差一步」提示条，装完即消失
- 环境诊断：多一行「scraper 来源」，说明这份可执行文件来自配置 / 组件 /
  随包 / 开发目录 / PATH 中的哪一个

## 6. 版本号规则

- Git tag：`v1.2.3`（语义化版本）
- Go 二进制内嵌版本：`1.2.3`（通过 ldflags）
- wails.json productVersion：`1.2.3`（构建时写入）
- scraper `__version__`：`1.2.3`（构建时写入 `_version.py`）
- 前端顶栏 / 关于页：读 `GetAppVersion`（来自上述 Go 内嵌版本）
- Release 标题：`v1.2.3`
- App 轻量包：`videolib_<appver>_<os>_<arch>.zip`
- App 整包：`videolib_<appver>_full_<os>_<arch>.zip`
- 组件包文件名：`scraper_<scrver>_<os>_<arch>.zip`
- manifest 里的 `version` / `protocol`：来自组件包内可执行文件的 `version` 输出
- 本地手动打包：App 默认 `0.1.0`，scraper 默认 `0.4.3`（测试版本号）

App 版本与组件版本是**两个独立的号**：前四个来自 tag，后两个来自产物自述。
当前 CI 把 tag 同时注入 `_version.py`，所以两者数值相同；但 manifest 的取值
路径与 tag 无关，停止注入即可让刮削器版本独立演进，CI 不需要跟着改。

## 7. 自动更新（未来）

文件名固定为自动更新铺路。后续可实现：

1. App 启动时向 GitHub Releases API 请求最新 tag
2. 比较内嵌版本号
3. 有新版时提示用户，或自动下载对应文件到临时目录
4. 替换正在运行的 exe（Windows 上需要 rename-swap：先改旧文件名，再放新文件）

组件（刮削器 / ffprobe）的更新路径已经就绪：`manifest.json` 给出每个平台的
版本、大小与 sha256，`internal/component` 负责下载、校验与原子落地。App 自身
的更新仍需另做（要处理「替换正在运行的程序」这个平台差异），当前阶段不实现。
