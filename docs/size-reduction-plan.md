# 体积瘦身方案

> 目标：安装包尽可能小，同时不牺牲「装完即用」。
> 两档措施，第一档已落地并实测，第二档是设计。

## 0. 现状（实测，macOS arm64，v0.5.0）

| 组成 | 体积 |
|---|---|
| `VideoLib.app` | 17 MB（压缩后 10 MB） |
| `tools/scraper/_internal/libpython3.14.dylib` | 16.9 MiB |
| `tools/scraper/_internal/selenium/` | 12.4 MB（**全部是 selenium-manager 二进制**） |
| `tools/scraper/scraper`（exe，含 PYZ 字节码） | 13.0 MiB |
| 其余（base_library、charset_normalizer、yaml、certifi…） | ≈3 MB |
| **分发目录合计** | **62 MB**（发布包 37–38 MB） |

`scraper` 占整包的 73%，而它 96% 的体积是 CPython 运行时和 selenium 的边角料，
不是我们自己的 1739 行代码。

## 1. 结论

| 阶段 | 分发目录 | 发布包（压缩） | 相对现状 |
|---|---|---|---|
| 现状 | 62 MB | 37–38 MB | — |
| 第一档：PyInstaller 收紧 | **48 MB** | **30 MB** | −21% |
| 第二档：scraper 改为按需下载 | **17 MB** | **10 MB** | −73% |

第二档之后，用户只装观影功能时下载 10 MB；点「开始刮削」再下 20 MB。

---

## 2. 第一档：PyInstaller 收紧（已落地）

改动只有 `python/scraper.spec` 一个文件，共两处。以下每条都实测过。

### 2.1 主因：三份从未被调用的 selenium-manager（−12.4 MB）

selenium 的 wheel 里带了三个平台的 Selenium Manager 二进制：

| 文件 | 体积 |
|---|---|
| `selenium/webdriver/common/linux/selenium-manager` | 5.3 MB |
| `selenium/webdriver/common/windows/selenium-manager.exe` | 3.6 MB |
| `selenium/webdriver/common/macos/selenium-manager` | 3.5 MB |

**它们不可能被调用。** `seleniumbase/core/browser_launcher.py:2073` 是：

```python
service = ChromeService(executable_path=LOCAL_CHROMEDRIVER)
```

显式给了驱动路径，`selenium/webdriver/common/driver_finder.py` 里那条
「没给路径 → 问 Selenium Manager」的分支永不进入。而驱动本来就是我们
让 seleniumbase 自己下到用户目录的（见 `driver_dir.py`）。

**坑：这些文件分两处收集。** 当前平台那份进 `a.binaries`，另外两份进
`a.datas`。只过滤 `a.binaries` 时产物从 45 MB 降到 41 MB，看起来「生效了」，
实际还剩 8.9 MB —— 所以两个都要过滤：

```python
_MANAGER_MARKERS = ("selenium/webdriver/common/", "selenium-manager")

def _drop_selenium_manager(entries):
    kept = []
    for entry in entries:
        dest = entry[0].replace("\\", "/")
        if all(marker in dest for marker in _MANAGER_MARKERS):
            print(f"[scraper.spec] 剔除 {dest}")
            continue
        kept.append(entry)
    return kept

a.binaries = _drop_selenium_manager(a.binaries)
a.datas = _drop_selenium_manager(a.datas)
```

### 2.2 次因：`optimize=2`（exe 13.0 → 11.5 MiB）

去掉 docstring 和 assert。对 seleniumbase 无副作用，已核实：

- `grep -rn "^\s*assert " seleniumbase/ --include=*.py` → **0 条**
- `core/`、`fixtures/` 从不读 `__doc__`

这一档是可选开关（`_OPTIMIZE = 2`）。**线上若出现异常行为，它是第一个该回退的**，
回退到 `0` 即可，不影响其他改动。

### 2.3 明确不做的三项

| 手段 | 实测结果 | 结论 |
|---|---|---|
| `strip -S` | libpython3.14.dylib 17.7 MB → 17.7 MB，一分不减（CPython 本身已是 stripped），且会破坏 ad-hoc 签名 | 不做 |
| UPX | 杀软误判风险，启动速度无收益 | 不做（现状已关） |
| 收窄 `hiddenimports` | seleniumbase 动态导入密集，白名单收益 < 1 MB | 不做，保留 `collect_submodules` 全量换安全 |

### 2.4 实测数据

| 项 | 改前 | 改后 |
|---|---|---|
| `scraper` 目录 | 45 MB | **31 MB** |
| exe | 13.0 MiB | 11.5 MiB |
| `_internal/selenium/` | 12.4 MB | 84 KB |
| zip 压缩后 | 28 MB | **20 MB** |
| 构建耗时 | 20 s | 20 s |

### 2.5 验收

已通过（本机 arm64）：`scraper version`、`scraper doctor` 均正常，驱动目录重定向
仍生效（`dir` 指向用户数据目录、`writable: true`）。

**还需人工确认一次**：真实站点走一遍完整刮削（CF 挑战 → 验证码 → 详情页 → 图片下载）。
理由是 `doctor` 只覆盖到导入完整性，不构造 `SB()` 会话。若这一步失败，先回退
`_OPTIMIZE`，再回退 `_drop_selenium_manager`。

CI 断言已落地（`build-windows` 与 `build-macos` 各一步「断言瘦身生效」）：

```bash
stray="$(find python/dist/scraper -name 'selenium-manager*')"
if [ -n "$stray" ]; then echo "产物里出现 selenium-manager" >&2; exit 1; fi
```

用 `find` 而不是 PowerShell 的 `Get-ChildItem -Filter`，是为了两个平台共用同一段
逻辑：`-Filter` 在 Windows 上有 8.3 短名匹配的怪癖，同一份脚本反而更可控。
放在 `version`/`doctor` 自检之后、打包 App 之前 —— 这一步失败得快，不必白跑
几分钟的 wails 构建。

---

## 3. 第二档：scraper 改为按需组件

第一档之后，`scraper` 仍占整包 65%，而绝大多数用户第一次打开 App 只是看视频。
把它挪出安装包是最有效的最后一刀。

### 3.1 目标形态

```
Release v0.6.0
├── VideoLib_0.6.0_macos_arm64.tar.gz      # 只含 App，10 MB  ← 默认下载
├── VideoLib_0.6.0_windows_amd64.zip       # 同上
├── scraper_0.4.3_macos_arm64.zip          # 20 MB，首次刮削时按需
├── scraper_0.4.3_windows_amd64.zip
├── manifest.json                          # 版本/大小/sha256/URL
└── VideoLib_0.6.0_full_windows_amd64.zip  # 整包，离线/内网用户
```

`scraper` 用**独立版本号**，与 App 解耦：改 App 不用重下 20 MB 的刮削器。

### 3.2 manifest

```json
{
  "schema": 1,
  "components": {
    "scraper": {
      "version": "0.4.3",
      "protocol": 1,
      "assets": {
        "windows/amd64": { "url": "…", "size": 21800000, "sha256": "…" },
        "darwin/arm64":  { "url": "…", "size": 20500000, "sha256": "…" },
        "darwin/amd64":  { "url": "…", "size": 20800000, "sha256": "…" }
      }
    }
  }
}
```

App 内嵌一份同构的 `knownComponents` 兜底：manifest 拉不到（断网/被墙）时，
仍能告诉用户「需要 scraper 0.4.3」并给出可手填的下载源。

`protocol` 是 NDJSON 协议版本，与 `go/internal/scraper` 的 `Inspect` 对齐；
组件版本与协议不匹配时拒绝安装并提示升级 App，而不是让它在运行时报解析错误。

### 3.3 Go 侧：`internal/component`（已实现）

```go
// State 是组件在当前机器上的实际状态。
type State int

const (
    Missing State = iota // 没装过
    Outdated             // 装了可用版本，但与期望版本不同（可更新）
    Ready                // 版本与协议都与期望一致，可执行文件在
    Corrupt              // 装了但文件缺失/凭据不可读
    Incompatible         // 协议版本与当前 App 不符
)

func DefaultDir() (string, error)                  // <data>/components
func (m *Manager) Path(name string) string         // 已安装的可执行文件；没有则空
func (m *Manager) State(spec Spec) State
func (m *Manager) Install(ctx, spec, progress func(done, total int64)) (string, error)
func (m *Manager) Remove(name string) error
func (m *Manager) CleanupStale() error             // 启动时清理 .staging-* 与旧版本
func (m *Manager) Verify(name string) error        // 逐文件核对，供诊断用
```

与初版方案的两处差异，都是实现中被真实问题逼出来的：

- **多了 `Outdated`。** 原方案只有四态，但「装了旧版本」和「没装」对用户是两件
  不同的事：前者要显示「可更新」，后者要显示「需要下载」。混在一个状态里，
  UI 要么诱导用户重下 20MB，要么把可更新藏起来。
- **`State` 收的是 `Spec` 而不是组件名。** 判断 `Incompatible` 需要知道期望的
  协议版本，而这个值只存在于调用方手上的 manifest（或内置兜底）里。让 Manager
  自己去猜，等于把同一份事实存两处。

落地规则（这几条是踩过的真实问题，不是风格偏好）：

| 规则 | 为什么 |
|---|---|
| 版本目录隔离：`<data>/components/scraper-0.4.3/` | Windows 上正在运行的 `scraper.exe` 不能被覆盖，同槽位更新会失败；隔离后旧版本留到下次启动再删 |
| 先解到 `<data>/components/.staging-<rand>/`，校验通过再 `os.Rename` | 半成品目录会让 `toolpath` 找到却跑不起来，用户看到的是「刮削器异常退出」而不是「组件损坏」，归因成本极高 |
| staging 必须与目标同盘（同在 data 目录内） | Windows 上跨盘 `os.Rename` 直接失败 |
| 指针文件 `installed.json` 记录当前版本 | 不依赖目录名猜测；也让「手动放回的整包」与「下载的组件」能共存 |
| sha256 流式边下边算 | 20 MB 不该读两遍 |
| 剥顶层目录时**先剥前缀、再去尾斜杠** | `zip -r` 的第一条是 `scraper/` 目录条目；顺序反过来，去完尾斜杠它就不以 `scraper/` 为前缀，会被当成一个名叫 `scraper` 的文件，正好占掉可执行文件的位置 |
| 幂等复用要比对**包校验码**，不能只比版本 | Release 用 `gh release upload --clobber` 覆盖资产，版本号常常不变。只比版本时用户手里那份旧包永远换不掉，而界面显示「已是最新」 |
| `New` 里把 root 解析成真实路径 | root 本身是符号链接时 `os.MkdirAll` 直接失败（它对最后一段用 Lstat，看到的是 symlink 而非目录）；解压时判「链接是否越界」也会因别名与真实路径对不上而误判 |
| 包里主程序必须有可执行位，装之前就拒 | 装完才发现权限不对时，`State` 报 Corrupt 而幂等分支会命中，重装无效，用户永远修不好 |
| 替换旧版本目录先 `rename` 成 `.old-*`，就位后再删 | `RemoveAll` 不原子：Windows 文件被占用、Linux 子目录不可写时删到一半失败，旧安装成了半残而指针还指着它 —— 一次失败的更新把「本来能用」变成「彻底不能用」 |
| 旧目录留到**指针写成功之后**才删，失败则回滚 | 删早了的话，取消或写指针失败时 Install 报告「安装失败」，而磁盘上生效的已是新版、旧版又没了，调用方无法退回原状 |
| 进程内互斥用可取消的信号量，不用 `sync.Mutex` | 一次安装可能在下载阶段停留十几分钟；互斥锁上的等待者既不能取消也不能超时，用户点「取消」界面毫无反应。跨进程那把文件锁本来就支持 ctx，两层行为不该不一致 |

进度通过 `runtime.EventsEmit("component:progress", …)` 推送，前端只画一条进度条。

**已做的验证**（`go test ./internal/component -race`）：

| 覆盖点 | 用例 |
|---|---|
| 真实 20MB 发布包端到端（安装 → 校验 → 执行 `version`） | `TestInstallRealPackage`（`VIDEOLIB_TEST_COMPONENT_ZIP` 触发） |
| 包内顶层目录剥离、`zip -r` 的目录条目形态 | `TestInstallStripsTopLevelDir`、`TestCommonTopDir` |
| 校验失败 / 取消 / 越界路径 / 缺可执行文件都不留残余 | `TestInstall*` + `assertNoResidue` |
| 四种状态迁移（含协议不匹配优先于版本差异） | `TestState*` |
| 指针损坏时不误删已装组件 | `TestCleanupStaleKeepsVersionsWhenPointerUnreadable` |
| 符号链接重建与绝对链接拒绝 | `TestInstallCreatesSymlink`、`TestInstallRejectsAbsoluteSymlink` |
| 符号链接不能越界、不能占用凭据位置 | `TestInstallRejectsEscapingSymlink`、`TestInstallRejectsReceiptSymlink` |
| 「链接指向链接」也逃不出去（词法检查挡不住） | `TestInstallRejectsSymlinkChainEscape` |
| 「Ready 但文件缺失」可靠重装自愈 | `TestInstallRepairsMissingFile` |
| 可执行位丢失能被发现并修复 | `TestStateDetectsMissingExecBit` |
| 符号链接被删能被诊断发现 | `TestVerifyDetectsMissingSymlink` |
| 卸载不误伤同前缀组件（`scraper` vs `scraper-addon`） | `TestRemoveKeepsSiblingComponent` |
| 另一实例安装期间跳过清理 | `TestCleanupStaleSkipsWhileAnotherInstanceInstalling` |
| 下载末尾取消不会「照样装完」 | `TestInstallAbortsWhenCancelledAtLastByte` |
| 组件根目录本身是符号链接时仍能安装 | `TestInstallWithSymlinkedRoot` |
| 包内主程序无可执行位时拒绝，且不留残余 | `TestInstallRejectsNonExecutableBinary` |
| 同版本号但包内容变了会被替换 | `TestInstallReplacesPackageWithNewChecksum` |
| 权限丢失后 `Path` 返回空，而不是给出跑不了的路径 | `TestPathRejectsNonExecutable` |
| 排队等前一次安装时能响应 ctx 取消 | `TestInstallCancelsWhileWaitingInProcess` |
| 指针写入失败时旧安装原样保留（回滚） | `TestInstallRollsBackWhenPointerWriteFails` |

跨进程互斥用**操作系统文件锁**（`lock_unix.go` 的 `flock`、`lock_windows.go` 的
`LockFileEx`），不用「mkdir 占位 + mtime 过期」：后者在「判断过期 → 删除 → 重建」
这三步上不是原子的，两个实例可能同时判定同一个锁过期，一个刚拿到锁就被另一个
按路径删掉，结果两边都进了临界区。文件锁由内核在进程退出时回收，没有需要人工
清理的残留状态。

进程内互斥（同一实例内并发调用 Install）用容量 1 的 channel 而不是 `sync.Mutex`：
一次安装可能停在下载阶段十几分钟，锁上的等待者必须能响应 ctx 取消，否则用户点了
「取消」界面毫无反应 —— 而跨进程那把锁本来就是支持 ctx 的，两层行为不该不一致。

符号链接的最终落点用 `filepath.EvalSymlinks` 解析后再判断包含关系 —— 词法 `Clean`
挡不住「链接指向链接」的组合（`a/up → ..` 加 `leak → a/up/../../victim`），
单看每条都在目录内，合起来就出去了。




### 3.4 路径解析接入点（已实现）

改的是 `go/components.go` 的 `resolveScraperLocked`（`app.go` 的
`resolveScraperPath` 已移过去），顺序：

```
显式配置 → 按需组件（installed.json 指向的版本目录）→ 随包 tools/scraper → 开发 tools/bin → PATH
```

放在「随包」之前，是因为用户装了更新的组件版本时不该被旧的内置副本压过。
`internal/toolpath` 一行没改 —— 组件路径是绝对路径，直接返回即可。

函数同时返回**来源**（config/component/bundled/dev/path），设置页与
「环境诊断」直接显示它。「装了组件却不生效」这类问题，第一眼要看的就是
来源，而不是路径字符串本身（五档里有两档可能指向同名的不同文件）。

ffprobe 走同一套：`probe.LocateWith(configured, componentPath...)`。

### 3.5 前端交互与失败路径（已实现）

设置 → 刮削器：每个组件一张卡片（状态徽标、已装/最新版本、下载大小、
当前生效路径与来源、下载/更新/移除、进度条）。刮削页在组件缺失时顶部出现
「还差一步」提示条，装完自动消失。

**没有做「先弹确认再开始下载」**：用户点击的就是「下载安装」按钮，再弹一次
「需要下载 20 MB」是多余的；体积已经写在按钮旁边的说明里。

失败路径与最终实现：

| 情况 | 表现 |
|---|---|
| 断网 / DNS 失败 | 清单不可达时退回内置清单（版本+大小仍可显示）；点下载后 25 秒内失败，提示换镜像或重试 |
| 下载中断 | 不产生半成品目录（staging 隔离），可直接重试 |
| sha256 不符 | 删 staging，提示「可能被网络中间件改写，请重试或换镜像」 |
| 磁盘不足 | 解压前查可用空间（需 ≥ 2× 包体），不足时提前拒绝 |
| 组件协议不匹配 | 卡片显示「需要更新的 App」，不进入下载；已装但不兼容时报 `incompatible` |
| 用户手动放了整包 | 走「随包」路径，卡片显示「随安装包附带」，不强制下载 |
| 清单里没有本平台的包 | 卡片显示「暂时拿不到下载信息」+ 可去手动指定路径 |

**下载源可配置**（设置 → 刮削器 → 组件下载源）：默认 GitHub，填前缀即可走
镜像；清单请求与包下载用同一个前缀。

**镜像必须 https**（本机回环除外）。原因不是洁癖：清单里的 sha256 与包本身
来自同一个地址，明文传输下能改写响应的一方可以同时替换两者，「校验通过」
于是什么都没证明，而 App 会在环境诊断时直接执行装进来的程序。

即便如此，镜像仍然是一个信任委托 —— 填了镜像就等于把「装什么」的决定权交给
它。设置页对此有一句明确提示。想彻底避免这层委托，用整包（随包分发，不经网络），
或让镜像只做透明加速并自行核对 `SHA256SUMS.txt`。签名清单（Ed25519）是更彻底的
方案，但它引入密钥管理，与本轮「轻量」目标无关，暂不做。

### 3.6 CI 改动（已实现）

原计划的四条，落地时有两处调整，原因写在下面：

| 计划 | 实际 | 为什么改 |
|---|---|---|
| 独立的 `build-scraper` job | 各平台 build job 内顺带产出 | PyInstaller 必须在原生 runner 上跑，独立 job 只能把三个平台再各启一遍机器，白等一次 `uv sync` 与几分钟的 PyInstaller |
| `<ver>` 取自 `_version.py` | 取自 `scraper version` 的输出 | `_version.py` 是输入，产物自述是结果。两者不一致时（漏注入、打包缓存），清单会与包对不上，症状是用户永远停在「可更新」 |
| `--no-scraper` / `--full` | 已加，并在第 6 步把默认切成轻量包 | 切换与 App 接入分两次提交：先让两条路并存跑一个版本，确认清单与组件下载没问题，再改默认值 |
| 轻量包启动验证 | 改成结构断言，不启动 GUI | CI 跑不了 GUI。改成「断言轻量包里没有 `tools/scraper`」+「断言整包里结构正确且能跑 `scraper version`」，能覆盖的失败面是同一个（打包脚本改错） |
| 每平台产出两份 App 包 | 第 6 步补上（轻量 + `_full_`） | 只留按需下载的话，拿不到 GitHub 直连的用户就没有任何可用形态 |

具体落点：

- `go/cmd/manifestgen`：读 `scraper_meta_<os>_<arch>.json` 与对应的 zip，算 sha256
  与大小，生成 `manifest.json`；生成后用 `component.ParseManifest` + `Spec` 再读
  一遍，读不懂就失败。**它是唯一实现**，build job 打完包也用它自校验一次。
- 每个 build job 打包完立刻跑 `scraper version | head -1 > scraper_meta_*.json`，
  再把 zip 与自述一起上传。自述是构建中间产物，release job 用完即删，不进 Release。
- 每个 build job 在 `wails build` 之前用 `manifestgen -only <本平台>` 生成
  `go/internal/component/manifest_builtin.json`（`//go:embed` 进 App），
  断网时界面仍能说出「缺哪个版本、多大」。**只在 tag 构建里做**：清单里的
  地址是 `releases/download/<ref>`，Release 只对 tag 存在，分支构建写进去
  就是一批必然 404 的死链接（分支构建改为写空清单，走在线清单）。
- `release` job 生成清单与 `SHA256SUMS.txt`，断言资产总数为 11
  （3 轻量 + 3 整包 + 3 组件 + manifest + 校验和）。
- 收集时轻量与整包分开匹配（`! -name '*_full_*'`）：两者文件名只差一个
  `_full_`，混在一起数会让「默认下载项」变成 40 MB 的整包。

生成器强制的不只是「字段写对」，还有三条一致性约束：

| 约束 | 不满足时的症状 |
|---|---|
| 包名里的版本 == 产物自述的版本 | 包名是给人看的，自述是给程序看的；两者分叉时用户看到版本号对不上 |
| 三个平台的版本一致 | `ManifestComponent` 只有一个 version 字段，必然有一个平台被标错 |
| 三个平台的**协议**一致 | 更隐蔽：清单写 v1、包是 v2，凭据记的也是 v1，兼容性检查一路放行 |
| zip 内主程序带可执行位 | 装完报 Corrupt；发布侧不拦就只能等用户发现 |

用 Go 程序而不是 shell + jq 生成清单，是因为字段名必须与 App 侧解析端逐字一致：
用 shell 拼 JSON 时字段名成了第二份事实，改一边忘另一边只会在用户机器上暴露
（「组件清单版本不支持」），而 CI 一片绿。现在生成端与解析端共用同一个 struct，
生成器还会用 `component.ParseManifest` + `Spec` 把结果读一遍。

### 3.7 兼容与回退

- `tools/package.sh` 的现有布局完全保留，`toolpath` 的五级查找不变 ——
  整包、开发布局、手动放置三种情况全部继续可用。
- 开关：`cfg.ComponentsOnDemand`。**最终默认 `true`**：默认发行形态就是轻量包，
  关掉它等于让「下载即用」的用户没有任何可走的路。原来打算先默认 `false`
  灰度，但那要求同时发两种 App 包并让用户自己分辨，成本比收益高 ——
  真正的回退手段是「整包仍然发布」，而不是开关默认值。
- 开关的语义收窄为「是否显示下载入口」：已装的组件永远生效，关掉开关不会让
  刮削器突然找不到。这点必须如此，否则用户点一下开关就以为组件丢了。

### 3.8 顺带：ffprobe 纳入同一机制（机制已就绪，尚未发布资产）

`component` 与 `go/components.go` 从一开始就不认识「scraper」这个词：组件名、
可执行文件名、协议都是参数。ffprobe 因此直接复用整套机制 ——

- 路径解析：`probe.LocateWith(configured, <components>/ffprobe-<ver>/ffprobe)`
- 状态与安装：`componentDefs()` 里多一条 `{ffprobe, protocol: 0}`，界面自动出现
  一张卡片（`Optional` 为真，缺失只降级，不拦任何流程）

**但 CI 目前不发布 ffprobe 组件**，原因有两条：

1. 现在就不发布它：今天的整包里也没有 ffprobe（缺失只影响时长与编码两列），
   所以发布 ffprobe 组件等于新增 60–80 MB/平台的下载，与「轻量」目标相反；
2. 需要额外的外部二进制来源（gyan.dev / BtbN 的静态构建），这是新的 CI 依赖
   与新的信任面，值得单独一次改动。

真要启用：CI 里把对应平台的 ffprobe 打成
`ffprobe_<ver>_<os>_<arch>.zip` + `ffprobe_meta_*.json` 丢进 `out/`，
`manifestgen` 加一个 `-name ffprobe -binary ffprobe` 的调用即可；App 侧不用改。

---

## 4. 落地顺序

1. **第一档**（已完成）：`python/scraper.spec` 两处改动 + 一次真实刮削确认。
   > 仍待你在真实站点跑一次完整刮削验证：`optimize=2` 的回退开关在
   > `scraper.spec` 顶部的 ②。
2. **CI 断言**（已完成）：`find python/dist/scraper -name 'selenium-manager*'`
   有结果即失败，防止 PyInstaller 升级回潮。
3. **组件骨架**（已完成）：`internal/component`，含清单拉取与内置兜底。
4. **CI 拆资产 + manifest**（已完成）：见 §3.6。
5. **接 UI + 路径解析**（已完成）：`go/components.go`、设置页组件卡片、
   刮削页提示条、环境诊断的来源一行。`ComponentsOnDemand` 默认 `true`（见 §3.7）。
6. **切默认值**（已完成）：`tools/package.{sh,ps1}` 默认轻量包，`--full` 出整包；
   CI 每平台出两份（轻量 + 整包），Release 共 11 个资产。
7. **ffprobe**（机制已完成，未发布资产）：见 §3.8。

## 5. 风险清单

| 风险 | 影响 | 对策 |
|---|---|---|
| 真实刮削因 `optimize=2` 出现异常 | 高 | 已确认 seleniumbase 无 assert/`__doc__` 依赖；出问题先回退该开关 |
| GitHub 下载不可达 | 高（等于功能不可用） | 下载源可配镜像；保留整包下载通道 |
| 首次使用多一步下载 | 中（体验） | 只在首次刮削时触发；观影路径零影响 |
| 组件目录与内置副本混用导致版本漂移 | 中 | `installed.json` 单指针 + 设置页显示实际使用路径 |
| PyInstaller 升级改变 hook 收集方式 | 低 | CI 残留断言（已实现） |
| 清单与组件包版本不同源 | 低 | 清单版本取自产物自述，包名按它拼；不一致时构建失败 |
| 内置兜底清单与线上清单版本漂移 | 低 | 状态卡片会显示「可更新」；协议不一致时以兜底清单为准 |
| 轻量包组装脚本默认值被人改回整包 | 中（悄悄回涨 31 MB） | CI 断言轻量包里不含 `tools/scraper` |
| 分支构建的组件地址 404 | 中（手动触发时才遇到） | 兜底清单只在 tag 构建生成；分支构建写空清单 |
