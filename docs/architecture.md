# 视频库 App 技术架构

> 本文档定义当前确认的产品基线与技术边界。页面与功能清单见
> [ui-pages-and-features.md](ui-pages-and-features.md)。项目把现有的爬取能力保留在 Python，
> 其余逻辑全部用 Go 重写，并以 Wails 桌面 App 形态交付（不再依赖浏览器打开 HTML）。

---

## 1. 已锁定的设计决策

| # | 决策点 | 结论 | 理由 |
|---|--------|------|------|
| D1 | 桌面框架 | **Go + Wails v2** | 体积小（<20MB）、用系统 WebView，无需打包 Chromium |
| D2 | 刮削语言 | **Python 保留** | 核心难点是 Cloudflare + 验证码对抗，Go 生态无 `solve_captcha()` 等价能力 |
| D3 | Python 交付形态 | **PyInstaller `--onefile` 打包为独立 exe** | Release 只发布一个 scraper.exe；驱动缓存落到用户数据目录 |
| D4 | Go ↔ Python 通信 | **子进程 + stdin/stdout NDJSON** | 不用 CGO（交叉编译会废）；不用本地 HTTP（要管端口与生命周期） |
| D5 | Python 职责边界 | **只做「搜索 + 解析 + 下载图片」，不碰 JSON schema、不碰数据库、不碰 UI** | 单一职责，schema 演进只改 Go 一处 |
| D6 | 数据真相来源 | **文件系统**（mp4 + 图片 + 边车 JSON） | 移动硬盘整目录可拷贝，自包含 |
| D7 | 数据库 | **SQLite，纯 Go 驱动 `modernc.org/sqlite`** | 无 CGO，`GOOS=windows go build` 直接出 exe |
| D8 | DB 定位 | **派生索引，可随时删掉重建** | 索引损坏不是灾难；避免移动硬盘写入风险 |
| D9 | DB 文件位置 | **`%APPDATA%\<app>\library-<卷标识>.db`** | 移动硬盘可能只读挂载／换盘符／热拔；WAL 附属文件是隐患 |
| D10 | 视频供流 | **Wails 自定义 AssetServer + `http.ServeContent`** | 官方支持 Range，免费获得 206 / `If-Modified-Since`，替代 `preview_server.py` |
| D11 | 目标平台 | **Windows x64 优先** | 与现状一致 |
| D12 | 视频库布局 | **固定为 `<库根>/<演员>/<视频文件>`** | 演员目录是一级业务边界，暂不支持多级媒体目录 |
| D13 | 同番号多版本 | **一个番号一个逻辑视频，多个文件作为版本** | 普通版、字幕版、4K 版共享元数据和用户状态 |
| D14 | Release 资产 | **两个独立 exe** | `video-library.exe` 与 `scraper.exe` 可独立更新 |
| D15 | 功能演进 | **分三期实施** | 第一期基础设置与观影；第二期版本与播放体验；第三期多站点、字幕、演员资料、更新 |

### 明确删除的遗留物

以下现有文件在新项目中**不再存在**，相关职责全部归 Go：

| 现有文件/职责 | 去向 |
|---|---|
| `src/generate.py` 的 `export_html()` | 删除。Wails 前端替代 |
| `src/generate.py` 的 `copy_preview_files()` | 删除 |
| `src/index.html` 的「模板 + `__ROOT__` 占位替换」机制 | 删除占位替换，改由媒体 HTTP 路由供图 |
| `preview.bat` | 删除 |
| `preview_server.py`（60 行手写 Range） | 删除。`http.ServeContent` 提供 |
| `videos/index.json`（手工二级索引） | 删除。SQLite 替代 |
| `docs/plan.md` 中「路线 X：uv + http.server + bat」 | 废弃 |

---

## 2. 总体架构

```
┌───────────────────────────────────────────────────────────────┐
│  Wails App（单一 exe）                                        │
│                                                               │
│  ┌─────────────────────────┐  ┌─────────────────────────────┐ │
│  │  刮削模块 (ScrapeView)   │  │  观影模块 (WatchView)        │ │
│  └───────────┬─────────────┘  └──────────────┬──────────────┘ │
│              │  Wails Binding / Events        │                │
│  ┌───────────▼─────────────────────────────────▼─────────────┐ │
│  │                     app.go（绑定层，薄）                   │ │
│  └───────────┬─────────────────────────────────┬─────────────┘ │
│              │                                 │               │
│  ┌───────────▼─────────┐          ┌────────────▼────────────┐  │
│  │ internal/scraper    │          │ internal/index (SQLite) │  │
│  │  runner / events    │          │  videos / user_data     │  │
│  │  proc_windows/unix  │          │  importer（幂等）        │  │
│  └───────────┬─────────┘          └────────────┬────────────┘  │
│              │                                 │               │
│  ┌───────────▼─────────┐          ┌────────────▼────────────┐  │
│  │ internal/library    │◀─────────│ internal/probe (ffprobe)│  │
│  │  scanner / fanha    │          └─────────────────────────┘  │
│  │  sidecar / model    │                                       │
│  └───────────┬─────────┘          ┌─────────────────────────┐  │
│              │                    │ internal/media          │  │
│              │                    │  AssetServer handler    │  │
│              │                    └────────────┬────────────┘  │
└──────────────┼─────────────────────────────────┼───────────────┘
               │ exec + NDJSON(stdin/stdout)     │ http.ServeContent
      ┌────────▼────────┐                        │
      │ scraper.exe     │  ← tools/scraper/      │
      │ (Python/onedir) │                        │
      └────────┬────────┘                        │
               │ 下载图片                        │
               ▼                                 ▼
      ┌─────────────────────────────────────────────────────┐
      │  文件系统（移动硬盘）— 唯一真相来源                  │
      │   <库根>/<演员>/*.mp4                                │
      │   <库根>/<演员>/meta/<演员>.json      ← 边车元数据   │
      │   <库根>/<演员>/meta/<番号>/cover.jpg                │
      │   <库根>/<演员>/meta/<番号>/images/N.jpg             │
      └─────────────────────────────────────────────────────┘
```

**依赖方向严格单向**：`app` → `scraper` / `index` / `media` → `library` → 文件系统。
`library` 是唯一认识文件布局的模块，它不认识 SQLite，也不认识 Wails。

---

## 3. 仓库目录结构

```
<repo>/
├── README.md
├── docs/
│   └── architecture.md              # 本文档
├── go/                              # ─── Wails App（独立 Go module）───
│   ├── go.mod
│   ├── wails.json
│   ├── main.go                      # 入口：装配依赖 + 配置 AssetServer
│   ├── app.go                       # 绑定给前端的 API（薄适配层）
│   ├── internal/
│   │   ├── config/                  # 配置持久化
│   │   ├── library/                 # 视频库领域层（真相访问层）
│   │   ├── probe/                   # ffprobe 封装
│   │   ├── scraper/                 # 调 Python exe
│   │   ├── index/                   # SQLite 索引
│   │   ├── media/                   # AssetServer 自定义 handler
│   │   └── player/                  # 外部播放器调用
│   └── frontend/
│       ├── index.html
│       ├── package.json
│       ├── vite.config.js
│       └── src/
│           ├── main.js
│           ├── views/
│           │   ├── ScrapeView.js    # 刮削模块
│           │   └── WatchView.js     # 观影模块
│           ├── components/
│           └── api.js               # 统一封装 binding 与事件
├── python/                          # ─── 刮削器 ───
│   ├── pyproject.toml
│   ├── scraper.spec                 # PyInstaller 配置
│   ├── src/scraper/
│   │   ├── __init__.py
│   │   ├── __main__.py              # 入口，参数解析
│   │   ├── protocol.py              # NDJSON 事件发射器（★ stdout 纪律）
│   │   ├── cli.py                   # 子命令：scrape / doctor
│   │   ├── browser.py               # seleniumbase 封装（uc / 验证码 / 年龄确认）
│   │   ├── downloader.py            # 图片下载（httpx，含重试）
│   │   ├── models.py                # 输出的元数据 dataclass
│   │   └── sites/
│   │       ├── base.py              # Site 抽象
│   │       └── javlibrary.py        # 站点解析实现
│   └── tests/
├── tools/
│   ├── build.sh                     # macOS 一键：Python + Go + 组装
│   ├── package.{sh,ps1}             # 组装 dist（CI 直接调用）
│   └── bin/                         # ─── 运行时依赖（不提交大文件）───
│       ├── ffprobe.exe
│       └── scraper/                 # PyInstaller onedir 产物
└── dist/                            # 最终分发产物（gitignore）
```

**为什么 Go 要单独一个 `go/` 子目录**：Wails CLI 要求 `wails.json` 与 Go module 同根。
把 Wails 项目隔离在 `go/` 下，才能让 Python 与 Go 在同一仓库里互不干扰地构建。

---

## 4. Go 侧模块设计

### 4.1 模块清单与依赖方向

```
app ──┬─▶ scraper ──▶ library   (写边车 JSON)
      ├─▶ index   ──▶ (SQLite，独立)
      │      ▲
      │      └── library         (importer 读边车 JSON)
      ├─▶ media   ──▶ library   (路径解析)
      ├─▶ probe                  (独立，读文件)
      ├─▶ player                 (独立)
      └─▶ config                 (被所有模块读)
```

| 模块 | 职责一句话 | 禁止做的事 |
|---|---|---|
| `config` | 配置读写与默认值 | 不持有业务状态 |
| `library` | 视频库文件布局的唯一权威 | **不认识 SQLite、不认识 Wails** |
| `probe` | 调 ffprobe 读真实媒体信息 | 不缓存（缓存归 `index`） |
| `scraper` | 启动/监控/取消 Python 进程 | 不解析 HTML，不写文件 |
| `index` | SQLite schema、查询、导入 | 不碰文件内容（只信 `library` + `probe` 给的） |
| `media` | 供流（视频/图片 HTTP） | 不做业务筛选 |
| `player` | 调外部播放器 | — |
| `app` | 绑定层：参数校验 + 编排 + 发事件 | **不放业务逻辑** |

### 4.2 各模块详细职责

#### `internal/config`

```go
type Config struct {
    Libraries   []LibraryRef `json:"libraries"`     // 可配置多个视频库根
    Concurrency int          `json:"concurrency"`   // 刮削并发，默认 2
    ScrapeTimeoutMin int     `json:"scrape_timeout_min"`
    ScraperPath string       `json:"scraper_path"`  // 空则自动探测 tools/scraper/scraper.exe
    FFprobePath string       `json:"ffprobe_path"`
    PlayerPath  string       `json:"player_path"`   // 外部播放器，空则走系统默认关联
    Theme       string       `json:"theme"`    // system | dark | light，未设置默认 system（跟随系统）
}

type LibraryRef struct {
    ID   string `json:"id"`    // 稳定标识，建议卷序列号或路径 hash
    Root string `json:"root"`
}
```

- 存 `%APPDATA%\<app>\config.json`
- 提供 `Default()`，字段缺失时逐个补默认值，不要整体覆盖用户配置

#### `internal/library` — 领域层（本方案的核心）

这是**唯一认识目录布局**的模块。

```go
type Video struct {
    Fanha        string   `json:"fanha"`         // 规范化番号，匹配键
    Stem         string   `json:"stem"`          // 原始文件名（去扩展名），用于定位文件
    Title        string   `json:"title"`
    ReleaseDate  string   `json:"release_date"`  // ISO 8601
    SiteLengthMin int     `json:"site_length_min"`
    Genres       []string `json:"genres"`
    Cast         []string `json:"cast"`
    Cover   string   `json:"cover"`        // 相对演员目录
    Shots   []string `json:"screenshots"`  // 相对演员目录
    VideoFile string `json:"video_file"`
    FileSize  int64  `json:"file_size"`
    ScrapedAt string `json:"scraped_at"`
}

type ActressSummary struct {
    Actress string  `json:"actress"`
    Videos  []Video `json:"videos"`
}
```

**`fanha.go` — 番号规范化（修既有 bug）**

现有 `generate.py:138` 是：

```python
fanha = mp4.stem.lower().replace("-c", "")
```

`.replace` 会把番号里**所有** `-c` 删掉，不是只去后缀。`IPZZ-001-c` 之外的形态（如 `1pondo-123456`）也可能被误伤。

新实现应改为「提取规范番号前缀」：

```go
// 匹配 <字母串>-<数字> 的规范番号前缀，忽略后续任何修饰后缀。
var fanhaRe = regexp.MustCompile(`^([a-z]+)-(\d+)`)

// NormalizeFanha 把文件名主干归一为匹配用番号，并返回是否匹配成功。
// 修饰后缀（-c 字幕版、-4k、_1 分段）一律忽略：它们共享同一份元数据。
func NormalizeFanha(stem string) (canonical string, ok bool) {
    m := fanhaRe.FindStringSubmatch(strings.ToLower(strings.TrimSpace(stem)))
    if m == nil {
        return "", false
    }
    return m[1] + "-" + m[2], true
}
```

- **`Stem` 与 `Fanha` 必须分开保存**：`Fanha` 用于匹配与去重，`Stem` 用于定位磁盘上的真实文件名。现有代码把两者混为一谈，是后续一系列路径拼接问题的根源。
- 规范化失败的文件不报错中断，收集为「未识别」列表返回给 UI，由用户决定处理。

**`scanner.go` — 扫描**

```go
type Candidate struct {
    Actress    string
    Fanha      string
    Stem       string
    VideoPath  string   // 绝对路径
    FileSize   int64
    Scraped    bool     // 边车 JSON 中是否已有该番号
    MissingArt bool     // 边车有记录但封面/截图文件缺失
}

func Scan(root string) ([]Candidate, error)
```

- 遍历 `<root>/*/`，找 `*.mp4`（扩展名列表做成常量，预留 `.mkv` / `.wmv`）
- **不读视频内容**，只 stat 拿大小
- 与 `sidecar.go` 交叉比对，标出 `Scraped` / `MissingArt`
- 结果排序稳定（按演员、番号），避免 UI 每次刷新顺序乱跳

**`sidecar.go` — 边车 JSON 读写（★ 必须改 merge 语义）**

现有 `generate.py:196` 是**全量重建**：

```python
summary = {"actress": actor_dir.name, "videos": videos}
```

本次未成功刮削的视频会**从 JSON 中消失**，这是真实的数据丢失。

新实现必须是按番号 merge：

```go
// MergeVideos 按 Fanha upsert，保留 JSON 中已有但本次未涉及的条目。
// 单个视频刮削失败不应导致其历史元数据被清除。
func MergeVideos(path string, actress string, updates []Video) error
```

- 写入用「临时文件 + rename」保证原子性（避免断电/拔盘写坏文件）
- 保留 JSON 中未知字段（前向兼容），不要把结构体直接整体序列化覆盖

#### `internal/probe`

```go
type MediaInfo struct {
    DurationMs int64
    Width, Height int
    VCodec, ACodec string
}

func Probe(ctx context.Context, ffprobePath, videoPath string) (MediaInfo, error)
```

- ffprobe 随 App 分发（`tools/bin/ffprobe.exe`），不依赖用户 PATH
- 外部进程，用信号量限制并发（建议 ≤4）
- **不要在每次扫描时都跑**：只在 `index` 首次入库、或文件 mtime/size 变化时跑，结果缓存在 DB

> **两个「时长」必须分开存**（见 §7.3）：
> 网页标注时长（`site_length_min`，分钟，可能不准）与文件真实时长（`duration_ms`，毫秒）。
> 两者经常不一致，混用一个字段会让排序和展示都出错。

#### `internal/scraper` — 调 Python

```
scraper/
├── runner.go        # 进程生命周期、stdin 投喂、stdout 逐行读、事件分发
├── events.go        # NDJSON 事件类型定义与解码（含协议版本校验）
├── watchdog.go      # 无事件超时检测（seleniumbase 可能永久挂起）
├── proc_windows.go  # Job Object 杀整棵进程树
└── proc_unix.go     # Setpgid + 杀进程组
```

**`runner.go` 关键点**

```go
func (r *Runner) Run(ctx context.Context, jobs []Job, onEvent func(Event)) (Summary, error)
```

- 一次调用**只起一个 Python 进程**，所有 job 走 stdin 投喂（见 §6.1）
- `exec.CommandContext` + `bufio.Scanner` 逐行读 stdout
- `Scanner.Buffer` 要放大（默认 64KB 行上限，`screenshots` 数组可能超）
- 环境变量注入：`PYTHONIOENCODING=utf-8`、`PYTHONUNBUFFERED=1`
- 每个事件即时回调 `app` 层 → `EventsEmit` 推前端

**`watchdog.go`**

seleniumbase 遇到验证码可能长时间无输出。用「最后一次事件时间」做心跳：

```go
// 超过 IdleTimeout 无任何事件，视为挂起：先杀进程树，再上报 fatal。
const DefaultIdleTimeout = 120 * time.Second
```

**`proc_windows.go` — 这是最容易出 bug 的地方**

`exec.CommandContext` 的默认取消**只杀父进程**，`chrome.exe` 会全部残留。

正确做法：创建 Job Object + `CREATE_SUSPENDED` 启动 → 加入 Job → 恢复线程。

```go
// 伪代码，关键是顺序：必须在子进程执行任何代码前加入 Job，
// 否则 Chrome 可能在加入前就被拉起，逃出 Job 管辖。
//
// 1. CreateJobObject，设置 JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
// 2. CreateProcess(CREATE_SUSPENDED | CREATE_NEW_PROCESS_GROUP)
// 3. AssignProcessToJobObject(job, process)
// 4. ResumeThread
// 取消时：TerminateJobObject(job, 1) → 整棵进程树（含 Chrome）一起死
```

依赖 `golang.org/x/sys/windows`。

> 注意：`go 1.20+` 的 `os/exec` 在 Windows 上有 `Cancel` 字段，
> 但它只处理被显式设置的路径依赖；Chrome 这类孙进程仍需 Job Object 兜底。

**进程退出后还要做清理**：seleniumbase 会在临时目录建 Chrome profile。
杀掉进程树后，检查是否残留 `chrome.exe`（可用 `tasklist` 或 WMI），并清理 temp profile 目录。

#### `internal/index` — SQLite

```
index/
├── store.go      # 打开、pragma、schema 迁移（user_version 做版本号）
├── importer.go   # library + probe → DB，幂等 upsert
├── videos.go     # 查询 API
└── userdata.go   # 收藏 / 进度 / 评分
```

**`store.go`**

```go
// 驱动用 modernc.org/sqlite（纯 Go，无 CGO）。
// 不要用 mattn/go-sqlite3：它需要 CGO，会让 Wails 交叉编译变成噩梦。
import _ "modernc.org/sqlite"

const dsn = "file:%s?_pragma=journal_mode(TRUNCATE)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
```

- `journal_mode=TRUNCATE` 而非 WAL：DB 若因任何原因落在移动硬盘上，避免 `-wal`/`-shm` 拔盘隐患
- schema 迁移用 `PRAGMA user_version` 递增，写在事务里
- 连接池 `MaxOpenConns(1)`（本场景单用户，串行最省心）

**`importer.go`**

```go
// ImportLibrary 全量或增量把某视频库同步进索引。必须幂等，可随时重跑。
// 输入：library 扫出的候选列表 + 边车 JSON 元数据 + （可选）probe 结果
// 输出：新增/更新/标记丢失 的计数
func (s *Store) ImportLibrary(ctx context.Context, libID string, cands []library.Candidate) (ImportStats, error)
```

- 用 `INSERT ... ON CONFLICT(library_id, actress, fanha) DO UPDATE` upsert
- 文件已删除但 DB 有记录 → 标记 `missing=1` 而非直接删（用户可能只是盘没插）
- **DB 里永不存绝对路径**，只存相对库根的路径（换盘符/换机器后可重建）

**`videos.go` — 查询 API（观影模块的数据来源）**

```go
type VideoFilter struct {
    LibraryID string
    Actress   string   // 空 = 全部
    Keyword   string   // 匹配 fanha / title
    Genres    []string // AND 语义
    Favorite  *bool
    MinDurationMs int64
}

type SortSpec struct {
    Field string // release_date | file_size | duration_ms | title | fanha
    Desc  bool
}

func (s *Store) QueryVideos(ctx context.Context, f VideoFilter, srt SortSpec, limit, offset int) ([]VideoRow, int, error)
```

#### `internal/media` — AssetServer

```go
// 路由约定
//   /media/<libID>/<相对库根的路径>   → 视频
//   /art/<libID>/<相对库根的路径>     → 图片
//
// http.ServeContent 自带 Range / 206 / If-Modified-Since / Last-Modified，
// 直接替代原 preview_server.py 的 60 行手写实现。
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // 1. 解析 libID → 库根
    // 2. url.PathUnescape → filepath.Clean → 拼绝对路径
    // 3. ★ 校验结果仍在库根内（防目录穿越）
    // 4. os.Open 拿 *os.File（满足 io.ReadSeeker）
    // 5. http.ServeContent(w, r, name, modTime, file)
}
```

**★ 路径安全（必须实现，不可省）**

查询参数来自前端，必须防 `../` 穿越：

```go
func safeJoin(root, rel string) (string, error) {
    full := filepath.Join(root, filepath.Clean("/"+rel))
    if !strings.HasPrefix(full, filepath.Clean(root)+string(os.PathSeparator)) {
        return "", ErrPathEscape
    }
    return full, nil
}
```

另外：拒绝绝对路径、拒绝含 NUL 的路径、符号链接是否允许要显式决定（默认拒绝或校验真实路径）。

#### `internal/player`

```go
// Open 用外部播放器打开。配置了 PlayerPath 就用它，否则走系统默认关联。
// 存在意义：WebView2 的 <video> 对 HEVC / 10bit 支持不可靠，必须留兜底通道。
func Open(playerPath, videoPath string) error

// 也支持跳转播放：PotPlayer 的 /seek=<ms> 之类参数
func OpenAt(playerPath, videoPath string, positionMs int64) error
```

#### `app.go` — 绑定层

只做「校验参数 → 调模块 → 发事件」，不放业务逻辑。

```go
// ── 观影模块 ──
func (a *App) ListLibraries() []config.LibraryRef
func (a *App) ListActresses(libID string) ([]ActressCount, error)
func (a *App) QueryVideos(f VideoFilter, s SortSpec, page int) (Page[VideoRow], error)
func (a *App) GetVideoDetail(videoID int64) (VideoDetail, error)
func (a *App) ToggleFavorite(videoID int64) (bool, error)
func (a *App) SaveProgress(videoID int64, positionMs int64) error
func (a *App) OpenInPlayer(videoID int64, positionMs int64) error

// ── 刮削模块 ──
func (a *App) ScanLibrary(libID string) (ScanResult, error)
func (a *App) StartScrape(libID string, stems []string) error   // 立即返回，进度走事件
func (a *App) CancelScrape() error
func (a *App) ScraperHealth() (Health, error)                   // 调 doctor 子命令
func (a *App) RebuildIndex(libID string) error                  // 删库重建
```

事件名约定（`EventsEmit`）：

| 事件 | 载荷 |
|---|---|
| `scrape:started` | `{total}` |
| `scrape:progress` | `{fanha, stage, percent, message}` |
| `scrape:item_done` | `{fanha, title}` |
| `scrape:item_failed` | `{fanha, reason, detail}` |
| `scrape:finished` | `{ok, failed, skipped, canceled}` |
| `index:imported` | `ImportStats` |

#### `frontend/` — 两个视图

**`ScrapeView.js`（刮削模块）**
1. 选择视频库 → 「扫描」
2. 候选列表：番号 / 演员 / 文件大小 / 状态徽标（未刮削 · 已刮削 · 缺图 · 未识别番号）
3. 筛选（默认只勾未刮削）+ 全选
4. 「开始刮削」→ 总体进度条 + 逐条列表（实时状态）
5. 「取消」→ 调 `CancelScrape`，UI 明确显示「正在终止…」（进程树清理需要时间）
6. 失败项单独成组，支持「重试失败项」

**`WatchView.js`（观影模块）**
1. 演员侧栏 / 顶部 tab（带数量）
2. 搜索框 + 筛选（类别 / 收藏 / 时长）+ 排序
3. 卡片墙（懒加载图片，走 `/art/`）
4. 详情抽屉：封面、截图 lightbox、元数据
5. 播放：默认内嵌 `<video>`（`/media/`），提供「用外部播放器打开」按钮
6. 收藏 / 评分 / 进度

**前端图片与视频 URL 一律指向 `/art/` 与 `/media/`，不再有 `__ROOT__` 占位替换和 `currentActorDir` 拼接。**

---

## 5. Python 侧模块设计

### 5.1 定位

Python 侧是一个**无状态的一次性抓取工具**。它：

- ✅ 接收一份 job 列表（番号 + 输出目录）
- ✅ 用 seleniumbase 绕过 Cloudflare / 验证码，解析详情页
- ✅ 下载封面与截图到指定目录
- ✅ 把结构化元数据以 NDJSON 事件写到 stdout
- ❌ **不写边车 JSON**（schema 归 Go 管）
- ❌ **不写任何索引 / 汇总文件**
- ❌ **不做番号提取**（Go 提取后告知）
- ❌ **不认识 UI、不认识数据库**

### 5.2 目录结构

```
python/
├── pyproject.toml
├── scraper.spec                     # PyInstaller 配置
├── src/scraper/
│   ├── __init__.py
│   ├── __main__.py                  # 入口
│   ├── cli.py                       # 子命令解析与调度
│   ├── protocol.py                  # ★ NDJSON 发射器 + stdout 纪律
│   ├── browser.py                   # seleniumbase 封装
│   ├── downloader.py                # 图片下载
│   ├── models.py                    # VideoMeta dataclass
│   ├── layout.py                    # 输出目录创建与路径规划
│   └── sites/
│       ├── base.py                  # Site 抽象（search / parse_detail）
│       └── javlibrary.py            # 站点实现（★ 现解析逻辑迁入此处）
└── tests/
```

### 5.3 子命令

```bash
# 刮削：job 列表从 stdin 读（NDJSON），配置走 argv
scraper.exe scrape --site javlibrary --concurrency 2 --timeout 60 --retries 2

# 环境自检：供 Go 在用户点「开始刮削」前调用
scraper.exe doctor

# 版本信息：协议协商
scraper.exe version
```

**为什么 job 走 stdin 而不是 argv**：Windows 命令行引号转义规则复杂，
路径含中文/空格/括号极容易出错。NDJSON 走 stdin 完全绕开这个问题。

`stdin` 每行一个 job：

```json
{"fanha":"ipzz-001","out":"D:/videos/演员A/meta/IPZZ-001","force":false,"job":"演员A/IPZZ-001"}
```

`out` 目录由 Go 计算、Python 按需创建（`layout.prepare()`），Python 只负责往里写文件，
因此 Python 完全不需要知道库的目录布局。

`job` 是 Go 给出的不透明标识，Python 必须在每条事件里原样回显（见 §6.4）。
为什么不直接用番号：同一番号可能对应多个文件（`IPZZ-001.mp4` 与 `IPZZ-001-c.mp4`
经 `NormalizeFanha` 归一后同号），而图片是按文件名主干分目录的。早先按番号把事件绑回
job，两个文件的事件会互相覆盖，于是边车 JSON 里记下的是**另一个文件**的输出目录 ——
刮削看着成功、那个目录却从没被写过，表现为永久性「缺图」。该字段可选：不带时按番号
回退（兼容旧版本 Go）。

### 5.4 各模块职责

| 模块 | 职责 |
|---|---|
| `protocol.py` | NDJSON 事件构造与写出；**stdout/stderr 隔离**（见 §6.6） |
| `cli.py` | 子命令解析、读 stdin job、调度并发、汇总统计、退出码 |
| `browser.py` | 封装 `SB(uc=True, headless=True)`；页面加载 + 年龄确认 + `solve_captcha()`；上下文管理器保证异常时退出会话 |
| `downloader.py` | 复用一条 `httpx.Client`；重试与超时；图片写盘 + 写完立即复核（存在且大小相符） |
| `sites/base.py` | `Site` 抽象：`search_url(fanha)` / `parse_detail(html) -> VideoMeta` |
| `sites/javlibrary.py` | 现 `generate.py` 的 `search_detail_url` / `parse_detail` 逻辑迁入，含 `/cn/jav` 链接选择、`previewthumbs` 解析、图片地址统一 `urljoin` 补全（绝对 / `//` / `/imgs` 三种形态都要认） |
| `models.py` | `VideoMeta` dataclass，字段与 §7.2 的边车 JSON 一一对应（但不负责落盘） |
| `layout.py` | 依据 `job.out` 建目录、定文件名（`cover.jpg` / `images/N.jpg`） |

### 5.5 从 `generate.py` 迁移的代码映射

| 现有函数 | 新位置 | 处理 |
|---|---|---|
| `fetch_html` | `browser.py` | 保留逻辑，改为可配置 sleep / 超时 |
| `search_detail_url` | `sites/javlibrary.py` | 迁移 |
| `parse_detail` | `sites/javlibrary.py` | 迁移，返回 `VideoMeta` 而非 dict |
| `download` | `downloader.py` | 迁移，加 retry |
| `process_video` | `cli.py` 的单项处理 | 重构：拆出「抓取」与「下载」两段 |
| `scrape_actress` | `cli.py` 的并发调度 | 重构：输入从目录变成 job 流 |
| `export_html` | — | **删除** |
| `copy_preview_files` | — | **删除** |
| `main()` 的目录遍历 | — | **删除**（归 Go 的 `scanner`） |
| `sys.argv.append("-n")` hack | — | **删除**，改为显式传参 |

> 现有 `generate.py:180` 往 `sys.argv` 里塞 `-n` 给 seleniumbase 用，
> 这在 CLI 化之后必须换成显式参数，否则参数解析会被污染。

---

## 6. Go ↔ Python 接口契约

### 6.1 调用形态

```
Go                                     Python
 │                                        │
 ├─ exec: scraper.exe scrape --site ... ─▶│
 │                                        │
 ├─ stdin ─ {"fanha":"...","out":"..."} ─▶│  (每行一个 job)
 ├─ stdin ─ {"fanha":"...","out":"..."} ─▶│
 ├─ stdin 关闭 ──────────────────────────▶│  (EOF = job 投喂结束)
 │                                        │
 │◀─ stdout ─ {"v":1,"type":"progress"...}│
 │◀─ stdout ─ {"v":1,"type":"item_done"...}│
 │◀─ stdout ─ {"v":1,"type":"done",...} ──│
 │◀─ stderr ─ seleniumbase 日志 ──────────│  (仅记日志，不解析)
 │◀─ exit code ───────────────────────────│
```

**一次调用一个进程**。不要每个番号起一个进程：Python 启动 + chromedriver 初始化成本高，
且并发应由 Python 内部的线程池统一控制（每个 job 一个 Chrome 实例）。

### 6.2 命令行参数

| 参数 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `--site` | string | 必填 | 站点标识，当前 `javlibrary` |
| `--concurrency` | int | 2 | 同时处理的 job 数（1–8） |
| `--timeout` | int | 60 | 单页面加载超时（秒） |
| `--retries` | int | 2 | 单项失败重试次数 |
| `--jobs-from` | string | `-` | job 来源，`-` 表示 stdin |
| `--log-level` | string | `info` | 只影响 stderr |

### 6.3 环境变量

| 变量 | 值 | 原因 |
|---|---|---|
| `PYTHONIOENCODING` | `utf-8` | 中文标题必须不乱码 |
| `PYTHONUNBUFFERED` | `1` | 否则 Go 侧会因 stdout 块缓冲而长时间读不到事件 |
| `PYTHONUTF8` | `1` | 同上，兜底 |

> 这三个变量是**必须**的。缺 `PYTHONUNBUFFERED` 时表现为「进程在跑但 UI 无进度」，
> 极难排查；缺 `PYTHONIOENCODING` 时表现为中文乱码或 `UnicodeEncodeError`。

### 6.4 NDJSON 事件协议

每行一个 JSON 对象，行内不得含换行。`v` 为协议版本。

```jsonc
// 进度：stage ∈ {search, detail, download_cover, download_shots}
{"v":1,"type":"progress","fanha":"ipzz-001","job":"演员A/IPZZ-001","stage":"search","percent":0.25}

// 单项成功：data 只带文件名，不带路径（见下方说明）
{"v":1,"type":"item_done","fanha":"ipzz-001","job":"演员A/IPZZ-001","data":{
  "title":"...","release_date":"2024-03-15","site_length_min":120,
  "genres":["..."],"cast":["..."],
  "cover_file":"cover.jpg",
  "shot_files":["images/1.jpg","images/2.jpg"],
  "total_shots":9
}}

// 单项失败
{"v":1,"type":"item_failed","fanha":"ipzz-001","job":"演员A/IPZZ-001","reason":"not_found","detail":"搜索结果为空"}

// 全部结束
{"v":1,"type":"done","summary":{"ok":8,"failed":2,"skipped":1}}

// 致命错误（无法继续，进程即将退出）
{"v":1,"type":"fatal","reason":"chrome_missing","detail":"未找到 Chrome 浏览器"}
```

**`job` 字段（可选）**：Python 把 job 载荷里的 `job` 原样回显到每条事件里，Go 靠它把
事件绑回自己下发的那条 job、进而绑回具体文件与输出目录。不按番号绑的原因见 §5.3。
它是**纯增量字段**，因此协议版本仍是 v1：

- 旧 Go + 新 Python：多出的键被忽略，行为不变。
- 新 Go + 旧 Python：`job` 为空，Go 退回按番号匹配（同番号多文件时可能绑错，
  但不会崩）。

**为什么 `item_done` 报告文件名而不是路径**

Python 只报告它**实际写出了哪些文件**，目录由 Go 侧拼接。这样文件命名规则在
整个系统里只有一处定义（`python/src/scraper/layout.py`），Go 不复制一份 ——
否则改一边忘一边，症状是「刮削成功但封面永远是空白」，在界面上极难归因。

既然这个字段会被 Go 写进边车 JSON、成为全系统找图的唯一依据，Python 在发
`item_done` 之前必须**回到磁盘复核**每个文件确实存在且非空，只把真实存在的报上来；
封面没落盘则整条判 `download_failed`。否则一个幽灵路径会被永久固化：
用户看到「缺图」，而日志里没有任何线索。

`shot_files` 是**磁盘上确实存在**的截图数量，`total_shots` 是站点给出的总数。
两者不等表示部分截图失败：单条仍然算成功（元数据与封面已拿到），但 UI 应显示「8/9 张」。

**一次性查询命令**（`doctor` / `version`）输出的是**单行普通 JSON 对象**，
不是事件流 —— 它们不产生 `v`/`type` 之外的事件。两者的共同约定是
「stdout 只承载 JSON」，所以 Go 侧读第一行即可。

**`reason` 取固定枚举**（不要自由文本，否则 Go 无法分类处理）：

| reason | 含义 | UI 建议动作 |
|---|---|---|
| `not_found` | 站点无此番号 | 标记为「站点无记录」，不重试 |
| `parse_failed` | 页面结构变化 | 提示可能站点改版，建议更新刮削器 |
| `download_failed` | 图片下载失败 | 可重试 |
| `timeout` | 页面加载超时 | 可重试 |
| `captcha_failed` | 验证码未通过 | 可重试，降低并发 |
| `chrome_missing` | 环境缺 Chrome | 引导用户安装，不可重试 |
| `driver_failed` | 驱动初始化失败 | 引导重装刮削组件 |

**协议版本协商**：Go 启动时调 `scraper.exe version` 拿 `{"protocol": 1, "scraper": "0.3.1"}`。
主版本不匹配时直接禁用刮削按钮并提示重新安装刮削组件 —— 不要试图兼容。

### 6.5 退出码

| 码 | 含义 |
|---|---|
| 0 | 全部成功 |
| 1 | 部分失败（仍有 `done` 事件，计数反映真实结果） |
| 2 | 参数错误 / 输入格式错误 |
| 3 | 环境问题（Chrome / 驱动缺失） |
| 4 | 被取消（收到终止信号） |
| 5 | 未捕获异常 |

### 6.6 ★ stdout / stderr 纪律

**这是最容易踩的坑**：seleniumbase 会往 stdout 打印大量日志，
一旦混入协议流，Go 侧 NDJSON 解析立刻崩。

Python 侧必须在**任何库被导入之前**建立隔离：

```python
# protocol.py
import sys

# 保存真正的 stdout 用于协议输出，随后把所有常规 print 重定向到 stderr。
# 必须在 import seleniumbase / httpx 之前执行。
_real_stdout = sys.stdout
sys.stdout = sys.stderr

import json

def emit(payload: dict) -> None:
    """写一行协议事件。flush 是必须的：Go 侧按行读，缓冲会让进度卡住。"""
    _real_stdout.write(json.dumps(payload, ensure_ascii=False, separators=(",", ":")) + "\n")
    _real_stdout.flush()
```

- 所有业务日志走 stderr（`logging` 默认就是 stderr，保持即可）
- `emit` 只接受已知事件类型，内部校验必填字段 —— 宁可抛异常也不要发出半成品行
- **每行必须 `flush`**，不要依赖行缓冲

### 6.7 取消与进程树

```
用户点「取消」
   │
   ├─ Go: ctx.Cancel()
   ├─ Go: watchdog 停止
   ├─ Go: TerminateJobObject(job)  ← Windows
   │      或 kill(-pgid, SIGKILL)   ← Unix
   │      → Chrome 孙进程一并退出
   ├─ Go: 等待进程退出（带 5s 超时，超时则再次强杀）
   ├─ Go: 清理残留 chrome.exe 与 temp profile
   └─ Go: EventsEmit("scrape:finished", {canceled: true})
```

**已知风险点**

1. `exec.CommandContext` 默认取消只杀父进程 → **必须**用 Job Object / 进程组兜底
2. 子进程可能在加入 Job 前就拉起 Chrome → 用 `CREATE_SUSPENDED` 消除竞态
3. 强杀后 temp profile 残留 → 退出后清理
4. UI 上必须有「正在终止…」中间态，进程树清理不是瞬间完成

---

## 7. 数据存储设计

### 7.1 三层结构

| 层 | 位置 | 是否可重建 | 内容 |
|---|---|---|---|
| **真相** | 移动硬盘 | ❌ 不可重建 | mp4、封面/截图、边车 JSON |
| **索引** | `%APPDATA%` | ✅ 完全可重建 | SQLite：查询加速 + 用户数据 |
| **运行时** | 内存 | ✅ | 当前筛选结果、扫描缓存 |

原则：**删掉 SQLite 只会丢失「用户数据」中的收藏/进度**。
如果这点也需要可移植，把 `user_data` 单独拆一个库放到用户指定的位置（可选增强，不在一期范围）。

### 7.2 边车 JSON（`<库根>/<演员>/meta/<演员>.json`）

保持现有布局，schema 由 Go 定义：

```json
{
  "schema": 2,
  "actress": "演员名称",
  "videos": [
    {
      "fanha": "ipzz-001",
      "stem": "IPZZ-001",
      "title": "视频标题",
      "release_date": "2024-03-15",
      "site_length_min": 120,
      "genres": ["类别A"],
      "cast": ["演员A"],
      "cover": "meta/IPZZ-001/cover.jpg",
      "screenshots": ["meta/IPZZ-001/images/1.jpg"],
      "video_file": "IPZZ-001.mp4",
      "file_size": 1234567890,
      "scraped_at": "2026-09-14T10:00:00+08:00"
    }
  ]
}
```

变更说明（相对现版本）：

| 变更 | 原因 |
|---|---|
| 新增 `schema` 字段 | 便于未来迁移，Go 侧可按版本分支处理 |
| 新增 `stem` | 区分「匹配键」与「文件名」，修 `fanha` 被 `.replace("-c","")` 破坏的问题 |
| `length` → `site_length_min` | 明确这是网页标注值，与真实时长区分 |
| 新增 `scraped_at` | 支持「按刮削时间排序」与「过期重刮」 |
| **去掉 `video_path`** | 存绝对路径在换盘符/换机器后失效。路径由 `library` 层实时拼 |

> 关于 `cover` / `screenshots` 的相对基准：**以演员目录为基准**（与现版本一致）。
> 前端不再手工拼 `currentActorDir`，改由 Go 在查询时拼成 `/art/<libID>/<演员>/<相对路径>`。

### 7.3 SQLite schema

```sql
PRAGMA user_version = 1;

CREATE TABLE videos (
  id               INTEGER PRIMARY KEY,
  library_id       TEXT    NOT NULL,
  actress          TEXT    NOT NULL,
  fanha            TEXT    NOT NULL,
  stem             TEXT    NOT NULL,
  title            TEXT,
  release_date     TEXT,               -- ISO 8601，可直接字符串比较排序
  site_length_min  INTEGER,
  duration_ms      INTEGER,            -- ffprobe 真实时长，与上者不同源
  genres           TEXT,               -- JSON 数组
  cast_list        TEXT,               -- JSON 数组
  cover_rel        TEXT,
  shots_rel        TEXT,               -- JSON 数组
  video_rel        TEXT    NOT NULL,   -- 相对库根，永不存绝对路径
  file_size        INTEGER,
  file_mtime       INTEGER,            -- 用于判断是否需要重新 probe
  width            INTEGER,
  height           INTEGER,
  vcodec           TEXT,
  acodec           TEXT,
  missing          INTEGER NOT NULL DEFAULT 0,  -- 文件不在磁盘上
  scraped          INTEGER NOT NULL DEFAULT 0,  -- 边车 JSON 中已有该番号的记录
  scraped_at       TEXT,                        -- 上次刮削时间，仅供展示与排序
  imported_at      TEXT    NOT NULL,
  UNIQUE(library_id, actress, fanha)
);

CREATE INDEX idx_videos_actress ON videos(library_id, actress);
CREATE INDEX idx_videos_date    ON videos(library_id, release_date);
CREATE INDEX idx_videos_fanha   ON videos(library_id, fanha);
CREATE INDEX idx_videos_title   ON videos(library_id, title);
CREATE INDEX idx_videos_missing ON videos(library_id, missing);
CREATE INDEX idx_videos_scraped ON videos(library_id, scraped);

-- 用户数据：与刮削产物无关，即使重建索引也希望能保留
CREATE TABLE user_data (
  library_id       TEXT    NOT NULL,
  actress          TEXT    NOT NULL,
  fanha            TEXT    NOT NULL,
  favorite         INTEGER NOT NULL DEFAULT 0,
  rating           INTEGER,
  watch_position_ms INTEGER NOT NULL DEFAULT 0,
  play_count       INTEGER NOT NULL DEFAULT 0,
  last_played_at   TEXT,
  PRIMARY KEY (library_id, actress, fanha)
);

-- 扫描/导入元信息，用于「是否需要重新扫描」判断
CREATE TABLE libraries (
  id           TEXT PRIMARY KEY,
  root         TEXT NOT NULL,
  last_scan_at TEXT,
  video_count  INTEGER NOT NULL DEFAULT 0
);
```

设计要点：

- `user_data` 用**业务键**（library_id + actress + fanha）而非 `video_id` 外键 —— 重建索引时视频行 id 会变，业务键稳定，用户数据得以幸存
- `missing` 标记而非删除：盘没插、文件临时移走都是常态
- `scraped` 是**独立一列**，由扫描器算出（边车 JSON 里有没有该番号）后带进来。
  不要让调用方从别的字段推断（例如「标题非空」或「scraped_at 非空」）——
  「是否已刮削」在扫描器和统计里必须是同一个定义，否则界面会同时显示
  「扫描说已刮削」和「统计说 0 条已刮削」这种自相矛盾的状态。
  `scraped_at` 只承担展示与排序，不承担判断。
- `genres` / `cast_list` 存 JSON 数组，用 `json_each` 做精确匹配（`LIKE '%"x"%'`
  在类别名互为子串时会误匹配）。**几千条记录完全够用**，不要提前拆表或上 FTS5
- `index` 层不存任何绝对路径

### 7.4 DB 位置策略

```
%APPDATA%\<app>\library-<libID>.db
```

`libID` 生成优先级：
1. Windows 卷序列号（`GetVolumeInformation`，最稳，换盘符不受影响）
2. 库根绝对路径的 hash（兜底）

**为什么不放移动硬盘**

| 风险 | 说明 |
|---|---|
| 只读挂载 | 某些场景硬盘会以只读挂载，写库直接失败 |
| 换盘符 | `E:` → `F:`，写死在配置里的 DB 路径失效 |
| 热拔 | 写入中途拔盘可能损坏 DB |
| WAL 附属文件 | `-wal` / `-shm` 未 checkpoint 就拔盘，是公认的损坏来源 |

代价只有一个：换台电脑要重建一次索引（秒级）。这个代价远小于上述任何一个风险。

### 7.5 导入流程（幂等）

```
ScanLibrary(libID)
  → library.Scan(root)            遍历文件，产出 Candidate（含大小、是否已刮削）
  → library.LoadSidecars(root)    读全部边车 JSON 的元数据
  → index.ImportLibrary(...)      按 (libID, actress, fanha) upsert
  → probe 补齐                     仅对 file_mtime 变化或 duration_ms 为空的行
  → EventsEmit("index:imported", stats)
```

- **必须幂等**：用户随时可以「重建索引」，结果应当一致
- 导入是**单向**的：文件系统 → DB。只有 `user_data` 会反向写回（且不写文件）

---

## 8. 播放与媒体服务

### 8.1 AssetServer 配置

```go
// main.go
wails.Run(&options.App{
    Title:  "视频库",
    AssetServer: &assetserver.Options{
        // 打包进二进制的前端资源
        Assets: frontendFS(),
        // 自定义 handler：只处理 /media/ 与 /art/
        Handler: media.New(frontendFS(), app.ResolveLibraryRoot),
    },
})
```

**Wails 的调度顺序必须搞清楚，否则会写出一段永远不被调用的"回退"逻辑**：

| 请求 | 处理者 |
|---|---|
| GET 且路径能在 `Assets`（`fs.FS`）中命中 | Assets，**不会**进 `Handler` |
| GET 但 `Assets` 返回 `fs.ErrNotExist` | 回落到 `Handler` |
| 非 GET（POST/PUT/...） | 直接进 `Handler` |

也就是说 `Assets` 是**优先**的，`Handler` 是**后备**的。因此：

- 前端自己的文件（`/index.html`、`/src/main.js`）由 `Assets` 服务，不会经过我们
- `/media/`、`/art/` 在前端资源里不存在，天然会落到 `Handler`
- **不需要**在 `Handler` 里写"回退到默认资源服务"的逻辑 —— 那一步 Wails 已经做过了。
  反过来，`Handler` 对不认识的路径应当直接返回 404，而不是试图再去找一遍资源。
- 若把 `Assets` 设为 nil，则所有 GET 请求都会直接进 `Handler`。

`media.Handler` 内部只需两件事：匹配前缀，然后交给 `http.ServeContent`。

### 8.2 `http.ServeContent` 为什么够用

`http.ServeContent(w, r, name, modTime, content io.ReadSeeker)` 本身就实现了：

- `Range` 单区间与多区间 → 206 Partial Content
- 非法 Range → 416
- `If-Modified-Since` / `If-Range` → 304
- `Accept-Ranges: bytes`
- 正确的 `Content-Type` 猜测（可传入自定义 `Content-Type` 覆盖）

**它不依赖 Wails 的 Range 支持**，因此不必关心 `Assets` 的 `fs.FS` 是否满足
`io.ReadSeeker` —— 我们服务的是 `*os.File`，天然满足。

也就是说，**原 `preview_server.py` 里手写的 60 行 `send_head` / `copyfile` 逻辑完全不需要移植**。
这比 Python 版本更短、更正确。

### 8.3 播放策略

| 场景 | 方案 |
|---|---|
| 默认 | WebView2 内嵌 `<video>`，src 指向 `/media/<libID>/...` |
| H.264 / AAC MP4 | 内嵌播放，正常 |
| HEVC / 10bit / 异常封装 | 内嵌可能黑屏或无声 → 提供「用外部播放器打开」按钮 |
| 大文件 | `http.ServeContent` 的 Range 支持拖动，无需额外处理 |

**必须把「用外部播放器打开」做成一等公民功能**，而不是兜底隐藏项。
理由：现有代码里已经保留了给 PotPlayer 用的 `video_path` 字段，
说明真实使用中外部播放器是主力路径。丢掉这个能力会明显降低体验。

外部播放器调用：

```go
// 配置了 PlayerPath 就用它（支持 /seek 之类参数），否则走系统默认关联
func Open(playerPath, videoPath string, positionMs int64) error
```

---

## 9. 构建与分发

### 9.1 Python 打包

#### 必须在一台 Windows 机器上打包（无法交叉编译）

PyInstaller **不是**交叉编译器，官方文档 `doc/building-for-other-platforms.rst` 明确说明
这不可能改变：

> PyInstaller is not a cross compiler. Due to Python's dynamic nature, PyInstaller needs to
> run snippets of code from the target Python environment at build time and it can only do that
> if said environment is runable on (i.e. built for) the current platform. Hence, PyInstaller
> ever becoming a cross compiler is **impossible**.

原因是机制上的：PyInstaller 属于「装配」而非「编译」——它在构建时**真的执行目标平台的
Python**（跑 hook、导入模块、读 `sys` 信息）来推断依赖。macOS 上跑不了 Windows 的 Python，
所以它连"要打包什么"都算不出来。此外二进制格式（Mach-O vs PE/COFF）与扩展模块链接的
运行时（`libpython3.14.dylib` vs `python314.dll` + `vcruntime140.dll`）也都不同。

在 macOS 上运行 PyInstaller 得到的是 `Mach-O 64-bit executable arm64`，**不是** `.exe`。
那些看起来能跨平台的参数也不管用：`--target-arch` 只影响 CPU 架构探测（官方标注
"not for cross-compilation"），`--platform` 在 6.x 中根本不存在。

**注意与 Go 侧的差别**：Wails/Go 是静态编译，`GOOS=windows` 可以直接在 macOS 上产出
`.exe`（§9.2 已验证）。两侧机制不同，不要以为「一个能交叉编译另一个也能」。

**三条可行路径**：

| 路径 | 说明 |
|---|---|
| GitHub Actions `windows-latest` | 官方推荐做法。见 `.github/workflows/build.yml` |
| Windows 机器直接打 | 在 `python/` 下 `uv run pyinstaller scraper.spec` |
| Windows 虚拟机 | 官方推荐做法，但需装完整 Python 环境 |

macOS 上跑 `tools/build.sh`（或其中的 PyInstaller 步骤）仍然有价值：它能验证**打包配置本身**是否成立
（seleniumbase 资源收集、`freeze_support`、stdout/stderr 分离、驱动目录重定向），
这些与目标平台无关。但产物只能在 macOS 上跑。

```bash
pyinstaller --onefile --name scraper \
  --collect-data=seleniumbase \
  --add-binary "<site-packages>/seleniumbase/drivers/uc_driver;seleniumbase/drivers" \
  --hidden-import=seleniumbase \
  --hidden-import=seleniumbase.core \
  src/scraper/__main__.py
```

**为什么 Release 使用 `--onefile`**

| 问题 | 对策 |
|---|---|
| 启动时解压 | 可接受，刮削任务本身远长于解压时间 |
| 驱动缓存 | `driver_dir.py` 固定到 `%LOCALAPPDATA%/videolib/drivers`（macOS/Linux 为 `~/.videolib/drivers`），不依赖临时目录 |
| 杀软误报 | Release 提供校验值；若仍误报，后续再增加安装包签名 |
| 排障 | `version`、`doctor` 输出协议、环境和实际驱动目录 |

**驱动落地目录：必须重定向，见 §10 的 R13**

seleniumbase 会在需要时自行下载驱动，但它的默认落地目录是**包内**的
`drivers/`，打包后即为 `<产物>/_internal/seleniumbase/drivers/`。装在受保护
位置时不可写，覆盖安装时驱动又会随着产物一起丢。

所以由 `src/scraper/driver_dir.py` 在启动时把落地目录重定向到用户数据目录
（`%LOCALAPPDATA%\videolib\drivers` 等），走 seleniumbase 自己的
`override_driver_dir()` / `settings.NEW_DRIVER_DIR` 入口，不硬改路径。

**不要**用 `SB(uc=True, driver_executable_path=...)`：那要改动 `browser.py` 里
验证过的 `SB(...)` 构造参数，不在允许范围内。

**Python 版本**

现有项目 `requires-python = ">=3.14"`（`.python-version` = 3.14）。
打包前需确认 PyInstaller 与 seleniumbase 依赖链对该版本的支持。
**建议降到 3.12**：生态兼容性最好，seleniumbase 与 undetected-chromedriver 在该版本上验证充分。

**undetected-chromedriver 在 PyInstaller 下的已知问题**

需在入口最顶部加 `freeze_support()`：

```python
from multiprocessing import freeze_support

if __name__ == "__main__":
    freeze_support()
    main()
```

否则 uc 模式在多进程下可能死循环。

### 9.2 Go 构建

```bash
cd go
wails build -platform windows/amd64 -webview2 download -clean
```

- **可以从 macOS / Linux 直接交叉编译出 Windows 的 .exe**，不需要 mingw：
  Wails v2 的 Windows 后端是纯 Go 实现（内置 Go WebView2Loader），不含 cgo。
  实测在 Apple Silicon Mac 上一条命令产出 15MB 的 `PE32+ executable (GUI) x86-64`。
  因此 **Windows 机器只在 PyInstaller 那一步是必需的**。
- `-webview2 download`：把 WebView2 引导程序打进安装包，覆盖未预装 WebView2 的机器
- 需要 NSIS 生成安装器（可选）

### 9.3 最终 Release 资产

```
Release v1.2.3/
├── video-library.exe                  # Go + Wails（含前端资源）
└── scraper.exe                        # Python 刮削器
```

Release 上传两个独立文件。用户将它们放在同一目录，App 支持自动发现 scraper.exe，
也支持在设置页手动选择路径。ffprobe 作为 App 配套运行时文件处理，不改变两个主资产的命名。

**为什么不把 scraper embed 进 Go 二进制**

| 方案 | 评价 |
|---|---|
| `//go:embed` 打进 exe | ❌ Go 二进制膨胀几十 MB；首次运行仍要解压到 temp，onefile 的问题原样复现；调试痛苦 |
| 同级目录分发 | ✅ 简单、可单独更新、可单独排障 |

**但必须做到「App 启动不依赖 Python」**：
App 启动时**不要**检查 scraper 是否存在。
只有用户进入刮削模块并点「开始刮削」时，才通过 `doctor` 检测并给出明确引导。

### 9.4 环境自检与版本协商

Go 侧启动刮削前的流程：

```
用户点「开始刮削」
  → scraper.exe version   检查协议版本（主版本必须匹配）
  → scraper.exe doctor    检查 Chrome / 驱动 / 写权限
     ├─ ok                    → 继续
     ├─ chrome_missing        → 弹窗引导安装 Chrome，附下载地址
     ├─ driver_failed         → 提示重新安装刮削组件
     └─ scraper_not_found     → 提示刮削组件缺失 / 路径配置
```

`doctor` 返回：

```json
{
  "scraper": "0.3.1",
  "protocol": 1,
  "python": "3.12.7",
  "chrome": {"found": true, "version": "140.0.7339.81"},
  "driver": {"ready": true, "source": "bundled"},
  "writable": true
}
```

### 9.5 版本号与兼容

- 协议版本（`protocol`）变化 = 破坏性变更，Go 拒绝运行并要求更新刮削组件
- 刮削器版本（`scraper`）可独立演进，只要协议不变
- 站点改版只影响 `sites/javlibrary.py`，重新打包 Python 即可，Go 侧零改动 ——这是把解析逻辑留在 Python 的重要收益

---

## 10. 风险清单与对策

| # | 风险 | 影响 | 对策 |
|---|---|---|---|
| R1 | seleniumbase + PyInstaller 打包失败 | **方案根本不可行** | **Phase 0 冒烟验证**（§11）。在干净 Windows 上跑通一个番号再写 Go |
| R2 | 目标机无 Chrome，且无法打包进 exe | 刮削功能不可用 | `doctor` 提前检测 + 明确引导；文档写明前置依赖 |
| R3 | `exec.CommandContext` 取消后 Chrome 残留 | 后台堆积僵尸进程，内存爆 | Job Object + `CREATE_SUSPENDED`；退出后清理残留（§6.7） |
| R4 | seleniumbase 日志污染 stdout，NDJSON 解析失败 | 进度功能直接崩 | §6.6 的 stdout/stderr 隔离，在建协议层时就做对 |
| R5 | 站点改版导致解析失效 | 刮削全部失败 | `parse_failed` 明确上报；解析隔离在 `sites/`，单文件修复后重打包 |
| R6 | WebView2 不支持 HEVC / 10bit | 内嵌播放黑屏 | 外部播放器作为一等公民功能（§8.3） |
| R7 | AssetServer 路径穿越 | 任意文件读取 | `safeJoin` 强校验（§4.2 media）；拒绝绝对路径与 NUL |
| R8 | 移动硬盘只读/换盘符/热拔 | 索引损坏或不可用 | DB 放 `%APPDATA%`；DB 内只存相对路径 |
| R9 | Python 3.14 打包兼容性未验证 | 打包受阻 | 降级到 3.12（§9.1） |
| R10 | ffprobe 对大量文件过慢 | 首次导入卡顿 | 仅在 mtime 变化时 probe；并发限 4；导入过程发进度事件 |
| R11 | 单个视频刮削失败导致历史元数据丢失 | 静默数据丢失（**现有 bug**） | 边车 JSON 改 merge 语义（§4.2 sidecar） |
| R12 | onefile 模式驱动目录每次都是新的临时路径 | 驱动无法复用，且极易被杀软误报 | 用 `--onedir`（§9.1） |
| R13 | 打包后驱动落在产物内部的 `_internal/seleniumbase/drivers/` | 装在受保护位置时不可写 → 驱动装不进去；覆盖安装时驱动随产物一起丢 | 启动时重定向到用户数据目录，与产物解耦（见下方专项说明） |

#### R13 专项说明：驱动落地目录必须重定向

seleniumbase 默认把驱动写进它自己包内的目录：

```python
# browser_launcher.py:44
DRIVER_DIR = os.path.dirname(os.path.realpath(drivers.__file__))
```

源码运行时那是 `.venv/.../seleniumbase/drivers/`，可写，一切正常。**所以这个
问题在开发环境完全看不出来。** 但 PyInstaller onedir 打包后，那里变成
`<产物>/_internal/seleniumbase/drivers/`，于是：

1. **装在受保护位置时不可写。** 例如装在 `Program Files` 下，驱动下载会失败，
   刮削直接报错，而错误现场看不出跟安装位置有任何关系。
2. **驱动会随产物一起丢。** 覆盖安装或重新打包时 `_internal/` 被替换，
   已下载的驱动一并消失，用户下次还得等一次下载。

**做法**：启动时把落地目录重定向到用户数据目录，由
`src/scraper/driver_dir.py` 负责：

| 平台 | 驱动目录 |
|---|---|
| Windows | `%LOCALAPPDATA%\videolib\drivers`（缺省回退 `%APPDATA%`） |
| macOS | `~/.videolib/drivers` |
| Linux | `~/.videolib/drivers` |

Windows 上刻意用 `%LOCALAPPDATA%` 而不是 `%APPDATA%`：后者是漫游目录，
企业域环境下会同步到服务器，把几 MB 的浏览器驱动放进漫游配置是错的。
Go 侧的配置文件用 `%APPDATA%`，两者分开是刻意的。

**走的是 seleniumbase 自己的入口**，不是硬改路径：

```python
# browser_launcher.py:4130 与 sb_install.py:385 两处同构的判断
if (... getattr(sb_config.settings, "NEW_DRIVER_DIR", None)
        and os.path.exists(sb_config.settings.NEW_DRIVER_DIR)):
    driver_dir = sb_config.settings.NEW_DRIVER_DIR
```

因此有两个硬约束，顺序和时机都不能错：

- **先 mkdir 再设置**：判断里带 `os.path.exists`，目录不存在它就当没设置、
  静默退回默认路径。
- **必须在构造 `SB(...)` 之前设置**：晚了 browser_launcher 已经用默认目录算好了
  各驱动的路径。落点在 `cli.main()`，只对 `scrape` / `doctor` 生效。

**版本耦合**：这里用的是 seleniumbase 的内部接口。`pyproject.toml` 已锁死
`seleniumbase==4.51.2`；升级时必须重新验证 `override_driver_dir` 是否还在、
`NEW_DRIVER_DIR` 是否仍被那两处采用。

**不要**用 `SB(uc=True, driver_executable_path=...)`：那要改动 `browser.py` 里
验证过的 `SB(...)` 构造参数，不在允许范围内。

`scraper doctor` 报出的 `driver.dir` 是**实际生效**的目录（读
`NEW_DRIVER_DIR`），不是我们的意图，因此可以直接用它确认重定向是否成功。

---

## 11. 产品分期与里程碑

按「最快看到可用产品」排序。

| Phase | 内容 | 产出 | 验收标准 |
|---|---|---|---|
| **P0** | Python 打包冒烟验证 | `scraper.spec` + 打包脚本 | 干净 Windows 上 exe 跑通一个番号，图片落盘正确；**且明确回答 R13：驱动从哪来** |
| **P1** | Python 剥离为 CLI | `python/` 全部模块 | `echo '{...}' \| scraper.exe scrape` 能输出合规 NDJSON；`doctor` 正常；Ctrl-C 能干净退出 |
| **P2** | Go 骨架 + 观影模块 | `config` / `library` / `media` / 前端 WatchView | 手工放一份边车 JSON，App 能扫描、展示卡片墙、内嵌播放并可拖动进度 |
| **P3** | SQLite 索引 | `index` 全部 | 「重建索引」幂等；搜索/筛选/排序可用；删库后能完整重建 |
| **P4** | 刮削编排 | `scraper` runner + ScrapeView | 点开始有实时逐条进度；点取消后 `tasklist` 无残留 chrome.exe |
| **P5** | 增强 | `probe` / `player` / `user_data` | 真实时长入库；外部播放器可跳转续播；收藏与进度持久 |
| **P6 / 第一期** | 设置与基础 Release | 设置页、双 exe Release、环境诊断、评分、继续观看、最近播放 | 用户下载两个 exe 后可配置并完成扫描 → 刮削 → 观看 |
| **P7 / 第二期** | 多版本与播放体验 | 同番号多版本、播放列表、自动下一部、播放失败提示优化 | 同番号版本合并正确，播放状态稳定 |
| **P8 / 第三期** | 扩展能力 | 多站点、字幕管理、演员资料、自动更新、安装包 | 扩展能力不破坏一期数据与协议 |

**P0 是唯一的 go / no-go 关卡**。它不通，后续所有设计都需要重新评估（可能的退路：Python 改成
长期驻留服务、或刮削改为在用户本地 Python 环境运行而非打包）。

---

## 12. 明确不做的事

| 不做 | 原因 |
|---|---|
| CGO 嵌 CPython | 交叉编译会废掉；与 PyInstaller 冲突 |
| 引入 Electron / 打包 Chromium | Wails 用系统 WebView，体积差一个量级 |
| Python 写 SQLite | 职责污染，且 Python 侧只做一次性抓取，无索引需求 |
| Python 写边车 JSON | schema 演进会变成两处维护 |
| 恢复 `index.json` / `index.html` / `preview.bat` / `preview_server.py` | 全部被 Go 侧能力覆盖 |
| DB 里存绝对路径 | 换盘符/换机器即失效 |
| 一期就上 FTS5 / 拆分 genre 表 | 几千条记录的规模用 `LIKE` 完全够，避免过度设计 |
| 兼容旧版协议 | 协议主版本不匹配直接拒绝，提示更新刮削组件 |
| App 启动时强依赖 Python | 观影模块必须能独立于刮削器运行 |
| 视频转码 / 内嵌转码 | 不在范围；解码交给 WebView2 或外部播放器 |

---

## 附录 A：新增/变更的关注点速查

**必须实现（缺失即功能缺陷）**

- [ ] `NormalizeFanha` 用正则提取规范番号，不再 `.replace("-c","")`
- [ ] 边车 JSON merge 语义（按 fanha upsert，不清除历史条目）
- [ ] 边车 JSON 原子写入（临时文件 + rename）
- [ ] Python 侧 stdout/stderr 隔离 + 每行 flush
- [ ] Go 侧注入 `PYTHONIOENCODING` / `PYTHONUNBUFFERED`
- [ ] Windows Job Object 杀进程树（`CREATE_SUSPENDED` 消除竞态）
- [ ] `media.Handler` 的 `safeJoin` 路径穿越校验
- [ ] `bufio.Scanner.Buffer` 放大（协议行可能超 64KB）
- [ ] `freeze_support()` 在 Python 入口顶部
- [ ] PyInstaller `--onefile` + `--collect-data=seleniumbase` + `uc_driver` 一并打包

**产品约束**

- [ ] 目录格式固定为 `<库根>/<演员>/<视频文件>`，演员名直接来自目录名
- [ ] 同番号多个文件归并为一个逻辑视频，文件版本单独保存并可选择播放
- [ ] 用户状态按逻辑番号保存，切换版本不丢失收藏、评分和播放进度
- [ ] Release 发布 `video-library.exe` 与 `scraper.exe` 两个独立资产
- [ ] 设置页提供 scraper、播放器、并发、超时和环境诊断

**必须验证（Phase 0）**

已在 macOS 上验证（`--onedir` 产物，非 Windows）：

- [x] 打包本身成功，产物可从命令行运行
- [x] 冻结产物里 `sys.frozen` 为真，入口脚本与 `freeze_support()` 生效
- [x] stdout/stderr 分离正确：stdout 仅 1 行合法 JSON，stderr 仅日志
      （用独立管道验证，与 Go 的 `StdoutPipe`/`StderrPipe` 同机制）
- [x] `doctor` 能报出 `driver.dir` / `driver.exists` / `driver.writable`
- [x] 确认打包后 `DRIVER_DIR` 会落在产物内部（这正是 R13 要解决的问题）

必须在 Windows 上验证：

- [ ] 干净 Windows 上 exe 可运行（PyInstaller 不做交叉编译，只能在这里做）
- [ ] **驱动目录重定向生效**：从冻结产物运行 `scraper doctor`，
      `driver.dir` 应指向 `%LOCALAPPDATA%\videolib\drivers` 而非 `_internal\...`
- [ ] 首次刮削能成功下载驱动到上述目录（观察目录里出现 `uc_driver.exe`）
- [ ] 覆盖安装产物后驱动仍在（这是重定向要换来的效果）
- [ ] Chrome 缺失时 `doctor` 能正确上报
- [ ] 任务管理器无残留 `chrome.exe`（含取消场景）
- [ ] 中文路径与中文标题不乱码
- [ ] Windows Defender 不拦截
