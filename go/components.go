package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"videolib/internal/component"
	"videolib/internal/config"
	"videolib/internal/probe"
	"videolib/internal/scraper"
	"videolib/internal/toolpath"
)

// 推给前端的组件事件。与 scrape:* 一样是对外契约。
const (
	eventComponentProgress = "component:progress"
	eventComponentDone     = "component:done"
	eventComponentFailed   = "component:failed"
	// eventComponentRefreshed 表示「清单刷新完了，状态可能变了」。
	// 没有它，首次进入页面只能看到内置清单里的版本，后台刚拉回来的新版本
	// 要等用户再进一次页面才出现 —— 而用户没理由再进一次。
	eventComponentRefreshed = "component:refreshed"
)

// componentManifestURL 指向最近一次 Release 附带的组件清单。
//
// 用 /releases/latest/download/ 而不是锚定某个版本：组件有自己的版本号，
// 锚定 App 版本会让「刮削规则修复」必须跟着发一版 App。协议不匹配时
// 后面会退回内置清单，所以拿到更新的清单不会让老 App 装上不兼容的组件。
//
// 可用 VIDEOLIB_COMPONENT_MANIFEST_URL 覆盖（自建发布源、本地联调）。
// 覆盖只改「清单从哪来」：包本身仍按清单里的 sha256 校验，改不了装什么。
var componentManifestURL = "https://github.com/like-ycy/video_library/releases/latest/download/manifest.json"

func init() {
	if key := "VIDEOLIB_COMPONENT_MANIFEST_URL"; os.Getenv(key) != "" {
		componentManifestURL = strings.TrimSpace(os.Getenv(key))
	}
}

// 组件名。同时是落盘目录前缀与清单里的键，改动会让人已装的组件变成「未安装」。
const (
	componentScraper = "scraper"
	componentFFprobe = "ffprobe"
)

// manifestTTL 是在线清单的复用窗口。
//
// 组件更新是低频事件（一个月几次），而设置页每次进入都会问一次状态。
// 窗口内直接用上次的结果，避免每次翻设置页都打一次 GitHub。
const manifestTTL = 6 * time.Hour

// componentDef 描述一个可下载组件在 App 侧的语义。
type componentDef struct {
	Name   string
	Label  string
	Binary string
	// Protocol 是组件与 App 的接口版本。ffprobe 走命令行、没有协议，取 0。
	Protocol int
	// Optional 为真的组件缺失时只降级，不影响核心功能（目前只有 ffprobe）。
	Optional bool
}

// componentDefs 是当前支持按需安装的组件。
//
// 顺序即界面顺序：刮削器在前，因为它决定「能不能刮削」，而 ffprobe 只影响
// 时长与编码两列。
func componentDefs() []componentDef {
	return []componentDef{
		{Name: componentScraper, Label: "刮削器", Binary: "scraper", Protocol: scraper.ProtocolVersion},
		{Name: componentFFprobe, Label: "ffprobe", Binary: "ffprobe", Optional: true},
	}
}

func findComponentDef(name string) (componentDef, bool) {
	for _, def := range componentDefs() {
		if def.Name == name {
			return def, true
		}
	}
	return componentDef{}, false
}

// ComponentDTO 是一个组件的当前状态，供设置页与刮削页展示。
type ComponentDTO struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	// State 取值见 component.State.String()：missing/outdated/ready/corrupt/incompatible。
	State            string `json:"state"`
	InstalledVersion string `json:"installedVersion"`
	AvailableVersion string `json:"availableVersion"`
	Protocol         int    `json:"protocol"`
	// Size 是待下载包的字节数；0 表示清单里没有（还没拿到可用清单）。
	Size int64 `json:"size"`
	// Source 是当前实际生效的来源：config/component/bundled/dev/path/auto，
	// 空表示没有可用的可执行文件。
	Source string `json:"source"`
	// Path 是当前实际使用的可执行文件路径，空表示没有。
	Path string `json:"path"`
	// CanInstall 表示「现在点下载能成功」，依赖清单里是否有本平台的包。
	CanInstall bool `json:"canInstall"`
	// OnDemand 是用户的开关状态；关掉时界面只展示状态、不提供下载入口。
	OnDemand bool `json:"onDemand"`
	Optional bool `json:"optional"`
	// Installing 表示此刻有安装任务在跑。前端切走页面再回来时要靠它恢复
	// 「正在下载」的按钮状态 —— 只靠后续事件的话，重挂载后的界面看起来
	// 像什么都没发生，用户会再点一次下载。
	Installing bool `json:"installing"`
	// ProgressDone/ProgressTotal 是最近一次进度快照，0/0 表示还在解析清单。
	ProgressDone  int64 `json:"progressDone"`
	ProgressTotal int64 `json:"progressTotal"`
	// Message 是给人看的一句话，说明为什么不能装 / 为什么不可用。
	Message string `json:"message"`
}

// componentService 管理按需组件：清单缓存、状态查询、安装。
//
// 为什么单独一层而不是都塞进 App：清单缓存、安装互斥、后台刷新这三件事
// 与 App 的其他状态无关，混在一起后 a.mu 会被网络请求拖住。
type componentService struct {
	mu      sync.Mutex
	manager *component.Manager
	err     error

	client *http.Client

	manifest    component.Manifest
	manifestAt  time.Time
	manifestOK  bool
	attemptedAt time.Time
}

func newComponentService() *componentService {
	dir, err := component.DefaultDir()
	if err != nil {
		return &componentService{
			client: component.NewHTTPClient(20 * time.Second),
			err:    err,
		}
	}
	return newComponentServiceAt(dir)
}

// newComponentServiceAt 在指定目录上构造服务（测试用；产品侧走 DefaultDir）。
func newComponentServiceAt(dir string) *componentService {
	return &componentService{
		client:  component.NewHTTPClient(20 * time.Second),
		manager: component.New(dir),
	}
}

// available 表示组件目录是否可用（配置目录创建失败时不可用）。
func (s *componentService) available() bool { return s.manager != nil }

// path 返回已安装组件的可执行文件路径，没有则空。
func (s *componentService) path(name string) string {
	if s == nil || s.manager == nil {
		return ""
	}
	return s.manager.Path(name)
}

// cachedManifest 返回 TTL 内的在线清单。
func (s *componentService) cachedManifest() (component.Manifest, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.manifestOK || time.Since(s.manifestAt) > manifestTTL {
		return component.Manifest{}, false
	}
	return s.manifest, true
}

// refreshManifest 拉取并缓存在线清单。
func (s *componentService) refreshManifest(ctx context.Context, mirror string) (component.Manifest, error) {
	manifest, err := component.FetchManifest(ctx, s.client, componentManifestURL, mirror)
	if err != nil {
		return component.Manifest{}, err
	}
	s.mu.Lock()
	s.manifest, s.manifestAt, s.manifestOK = manifest, time.Now(), true
	s.mu.Unlock()
	return manifest, nil
}

// refreshAsync 在后台刷新在线清单，失败只记日志。
//
// 失败不弹窗：设置页只是想知道「有没有可更新的组件」，为此打断用户不合理。
// 真正需要下载时会再拉一次，并把错误摆在下载按钮旁边。
func (s *componentService) refreshAsync(mirror string, onRefreshed func()) {
	// 最小间隔：断网时每次切页面都会走到这里，没有这个门槛就是每次导航打一次
	// GitHub（并在 25 秒后超时），既无意义也让日志刷屏。
	s.mu.Lock()
	if time.Since(s.attemptedAt) < time.Minute {
		s.mu.Unlock()
		return
	}
	s.attemptedAt = time.Now()
	s.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		if _, err := s.refreshManifest(ctx, mirror); err != nil {
			log.Printf("提示：组件清单暂不可用（%v）", err)
			return
		}
		onRefreshed()
	}()
}

// specLocal 返回不联网就能得到的安装描述。
//
// 优先在线清单（可能比内置的新），协议不匹配则退回内置：清单来自更晚的
// Release 时，它的组件可能是给新协议准备的，装上去也只会报「协议不匹配」。
// 内置清单由同一次构建生成，协议必然一致。
func (s *componentService) specLocal(def componentDef) (component.Spec, string, error) {
	var problems []string
	if manifest, ok := s.cachedManifest(); ok {
		if spec, err := manifest.SpecForCurrent(def.Name); err == nil {
			if spec.Protocol == def.Protocol {
				return spec, "manifest", nil
			}
			problems = append(problems, fmt.Sprintf("在线清单的协议为 v%d（本程序需要 v%d）", spec.Protocol, def.Protocol))
		} else {
			problems = append(problems, err.Error())
		}
	}
	if builtin, err := component.BuiltinManifest(); err == nil {
		if spec, err := builtin.SpecForCurrent(def.Name); err == nil {
			if spec.Protocol == def.Protocol {
				return spec, "builtin", nil
			}
			problems = append(problems, fmt.Sprintf("内置清单的协议为 v%d（本程序需要 v%d）", spec.Protocol, def.Protocol))
		} else {
			problems = append(problems, err.Error())
		}
	} else {
		problems = append(problems, "本构建未内置组件清单")
	}
	return component.Spec{}, "", fmt.Errorf("无法确定 %s 的下载信息：%s", def.Label, strings.Join(problems, "；"))
}

// specForInstall 返回用于安装的描述：先拉最新的在线清单，失败退回本地。
//
// 安装是显式操作，多等几秒换「装上最新的组件」是值得的；但网络不通时不能
// 连装都不让装 —— 内置清单里的包同样能装。
func (s *componentService) specForInstall(ctx context.Context, def componentDef, mirror string) (component.Spec, error) {
	if manifest, err := s.refreshManifest(ctx, mirror); err == nil {
		if spec, err := manifest.SpecForCurrent(def.Name); err == nil && spec.Protocol == def.Protocol {
			return withMirror(spec, mirror), nil
		}
	} else {
		log.Printf("提示：使用内置组件清单（在线清单不可用：%v）", err)
	}
	spec, _, err := s.specLocal(def)
	if err != nil {
		return component.Spec{}, err
	}
	return withMirror(spec, mirror), nil
}

// withMirror 把镜像前缀应用到包下载地址。
//
// 清单里的地址是 GitHub 原始地址，镜像只加在「清单请求」上是不够的：
// 用户填了镜像，清单能取到、包却仍然直连 GitHub，下载依旧卡住 —— 而界面上
// 一切正常，只是永远停在 0%。
func withMirror(spec component.Spec, mirror string) component.Spec {
	if mirror == "" {
		return spec
	}
	spec.Asset.URL = spec.Asset.URLFor(mirror)
	return spec
}

// ── App 上的绑定方法 ──────────────────────────────────────────────────────

// ListComponents 返回所有可下载组件的状态。
//
// 状态查询不联网（用缓存或内置清单），同时按需在后台刷新：设置页要的是
// 立刻能画出来的东西，而不是等 20 秒的 GitHub 响应。
func (a *App) ListComponents() []ComponentDTO {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()

	svc := a.components
	if svc == nil {
		// 返回空切片而不是 nil：Wails 把 nil 序列化成 null，前端 `.map` 会炸。
		return []ComponentDTO{}
	}
	// 组件目录不可用（配置目录建不出来）时也要给出每个组件的条目：
	// 一张写着「组件目录不可用」的卡片，比整个区块消失更好排查。
	if !svc.available() {
		out := make([]ComponentDTO, 0, len(componentDefs()))
		for _, def := range componentDefs() {
			out = append(out, a.componentStatus(def, componentCfg(cfg)))
		}
		return out
	}
	if _, ok := svc.cachedManifest(); !ok {
		svc.refreshAsync(cfg.ComponentMirror, func() { a.emit(eventComponentRefreshed, struct{}{}) })
	}

	out := make([]ComponentDTO, 0, len(componentDefs()))
	for _, def := range componentDefs() {
		out = append(out, a.componentStatus(def, componentCfg(cfg)))
	}
	return out
}

func (a *App) componentStatus(def componentDef, cfg componentConfig) ComponentDTO {
	dto := ComponentDTO{
		Name:     def.Name,
		Label:    def.Label,
		Protocol: def.Protocol,
		OnDemand: cfg.onDemand,
		Optional: def.Optional,
	}
	svc := a.components
	if svc == nil || !svc.available() {
		dto.Message = "组件目录不可用，请检查配置目录权限"
		return dto
	}

	spec, _, err := svc.specLocal(def)
	if err != nil {
		dto.Message = err.Error()
	} else {
		dto.AvailableVersion = spec.Version
		dto.Size = spec.Asset.Size
		dto.CanInstall = true
	}

	if version, _, ok := svc.manager.Installed(def.Name); ok {
		dto.InstalledVersion = version
	}
	// 拿不到清单时只判可用性、不判版本：这样已装的组件仍会显示
	// ready/corrupt/incompatible，而不是因为「期望版本未知」被标成可更新。
	if spec.Name == "" {
		dto.State = svc.manager.LocalState(def.Name, def.Protocol).String()
	} else {
		dto.State = svc.manager.State(spec).String()
	}
	dto.Path, dto.Source = a.componentInUse(def.Name)
	dto.Installing, dto.ProgressDone, dto.ProgressTotal = a.installSnapshot(def.Name)
	if dto.Path == "" && dto.State == component.Corrupt.String() {
		dto.Message = "组件文件不完整，重新下载即可修复"
	}
	return dto
}

// installSnapshot 返回某个组件的安装快照（是否在装、装到哪了）。
func (a *App) installSnapshot(name string) (bool, int64, int64) {
	a.scrapeMu.Lock()
	defer a.scrapeMu.Unlock()
	if !a.installs[name] {
		return false, 0, 0
	}
	progress := a.installProgress[name]
	return true, progress[0], progress[1]
}

// componentPath 返回已装组件的可执行文件路径（服务未初始化时为空）。
func (a *App) componentPath(name string) string {
	if a.components == nil {
		return ""
	}
	return a.components.path(name)
}

// componentInUse 返回某个组件当前实际生效的路径与来源。
func (a *App) componentInUse(name string) (string, string) {
	switch name {
	case componentScraper:
		return a.scraperSource()
	case componentFFprobe:
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.ffprobeSourceLocked()
	}
	return "", ""
}

// InstallComponent 启动一次组件安装，立即返回。
//
// 安装包含 20MB 下载与解压，同步等待会让前端请求挂几十秒，期间界面连进度
// 都画不出来。进度走 component:progress 事件，结束走 done/failed。
func (a *App) InstallComponent(name string) error {
	def, ok := findComponentDef(name)
	if !ok {
		return fmt.Errorf("未知组件：%s", name)
	}
	svc := a.components
	if svc == nil || !svc.available() {
		return errors.New("组件目录不可用，请检查配置目录权限")
	}
	if a.ctx == nil {
		return errors.New("应用尚未就绪")
	}
	// 刮削进行中不许换组件：Windows 上正在跑的 scraper.exe 不能被替换，
	// macOS 上虽然能换，但这次刮削的输出与刚装上的版本对不上，出了问题
	// 会归因到错误的版本上。占用与检查在同一个裁决点内完成。
	if err := a.reserveComponentChange(name, "下载或更新"); err != nil {
		return err
	}

	a.mu.Lock()
	mirror := a.cfg.ComponentMirror
	a.mu.Unlock()

	go func() {
		defer a.releaseComponentChange(name)

		// 下载 20MB 在慢网下要几分钟，解压还要几秒。整体超时给足，
		// 具体取消由 ctx 承担：用户关窗口时 a.ctx 会被取消。
		ctx, cancel := context.WithTimeout(a.ctx, 30*time.Minute)
		defer cancel()

		spec, err := svc.specForInstall(ctx, def, mirror)
		if err != nil {
			a.emit(eventComponentFailed, componentFailedDTO{Name: name, Message: err.Error()})
			return
		}

		var lastEmit time.Time
		progress := func(done, total int64) {
			// 限流：下载器每读一块就回调一次，原样转发会把 WebView 的
			// 事件队列塞满，界面反而更卡。末尾那一次必须发出，否则
			// 进度条会停在 99%。
			if done < total && time.Since(lastEmit) < 100*time.Millisecond {
				return
			}
			lastEmit = time.Now()
			a.recordInstallProgress(name, done, total)
			a.emit(eventComponentProgress, componentProgressDTO{Name: name, Done: done, Total: total})
		}

		path, err := svc.manager.Install(ctx, spec, progress)
		if err != nil {
			a.emit(eventComponentFailed, componentFailedDTO{Name: name, Message: friendlyInstallError(err)})
			return
		}

		// 装完立刻让新路径生效：不重建 runner 的话，本次会话仍用旧路径，
		// 用户点「开始刮削」还是报找不到刮削器。
		a.rebuildRunner()
		a.refreshFFprobe()
		a.invalidateHealth()
		log.Printf("组件 %s %s 已安装：%s", def.Name, spec.Version, path)
		a.emit(eventComponentDone, componentDoneDTO{Name: name, Version: spec.Version, Path: path})
	}()
	return nil
}

// RemoveComponent 卸载按需安装的组件。
//
// 只删组件目录，不动用户手工配置的路径与随包副本：那两者不归这里管，
// 删了之后 resolveScraper 会自然回落到它们。
func (a *App) RemoveComponent(name string) error {
	def, ok := findComponentDef(name)
	if !ok {
		return fmt.Errorf("未知组件：%s", name)
	}
	svc := a.components
	if svc == nil || !svc.available() {
		return errors.New("组件目录不可用")
	}
	// 卸载也要走同一个占用点：正在安装时删目录，会让那次安装在半途失去
	// 目标文件，最后报一个和真正原因无关的解压错误。刮削检查也在里面 ——
	// 两处各查一次，就会出现「都通过、然后同时发生」。
	if err := a.reserveComponentChange(name, "卸载"); err != nil {
		return err
	}
	defer a.releaseComponentChange(name)
	if err := svc.manager.Remove(def.Name); err != nil {
		return err
	}
	a.rebuildRunner()
	// 卸载后必须重算：Prober 里存的是启动时那条路径，文件已经没了，
	// 不重算就会继续 exec 一个不存在的文件（表现为「探测一直失败」）。
	a.refreshFFprobe()
	a.invalidateHealth()
	return nil
}

type componentProgressDTO struct {
	Name  string `json:"name"`
	Done  int64  `json:"done"`
	Total int64  `json:"total"`
}

type componentDoneDTO struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Path    string `json:"path"`
}

type componentFailedDTO struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

// friendlyInstallError 把底层错误换成用户能行动的说法。
//
// 安装失败最常见的两种是「下不动」与「校验不符」。原样抛出 net/http 的
// 错误串，用户只能看到一堆英文，既不知道能不能重试，也不知道该改什么。
func friendlyInstallError(err error) string {
	msg := err.Error()
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "下载超时。可换用镜像后重试（设置 → 刮削器 → 组件下载源）。"
	case errors.Is(err, context.Canceled):
		return "下载已取消。"
	case strings.Contains(msg, "校验"):
		return "下载的组件包校验不符，可能被网络中间件改写。请重试，或换用镜像。"
	}
	return msg
}

// ── 路径解析 ──────────────────────────────────────────────────────────────

// configSnapshot 是解析路径时需要的那部分配置，避免在持有 a.mu 期间
// 被 SaveConfig 改掉。
// componentCfg 从完整配置里取出状态展示需要的那一部分。
func componentCfg(cfg config.Config) componentConfig {
	return componentConfig{onDemand: cfg.ComponentsOnDemand}
}

// componentConfig 是状态展示需要的那部分配置。
//
// 单独取一份而不是把 a.cfg 传下去：状态计算不该持有 a.mu，也不该看到
// 与组件无关的字段（那会让「为什么改播放器路径会影响组件卡片」变成真问题）。
type componentConfig struct {
	onDemand bool
}

// resolveScraperPath 定位刮削器可执行文件。
func (a *App) resolveScraperPath() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	path, _ := a.resolveScraperLocked()
	return path
}

// resolveScraperLocked 定位刮削器，同时返回来源。调用方必须持有 a.mu。
//
// 顺序：显式配置 → 按需组件 → 随包 → 开发目录 → PATH。
//
// 两种布局都要认，且分发布局必须排在开发布局之前：
//
//	分发  <App>/tools/scraper/scraper.exe   ← tools/package.ps1 产出
//	开发  <repo>/tools/bin/scraper/scraper.exe
//
// 只认开发布局是曾经的 bug：开发期仓库里恰好有 tools/bin/，一切正常，
// 而 user 装好分发包后必然找不到刮削器，且报错完全指不到目录层级上。
//
// 组件排在随包之前：用户装过新版组件时，旧的内置副本不该压过它。整包用户
// （随包副本、没装过组件）不受影响，因为那时组件路径是空的。
func (a *App) resolveScraperLocked() (path, source string) {
	if a.cfg.ScraperPath != "" {
		return a.cfg.ScraperPath, "config"
	}
	if found := a.componentPath(componentScraper); found != "" {
		return found, "component"
	}

	name := toolpath.Name("scraper")
	// 分发布局优先于开发布局：正式分发物里不该出现 tools/bin/，
	// 万一出现也说明那是残留，不该压过随包分发的正本。
	for _, candidate := range []struct {
		rel    []string
		source string
	}{
		{[]string{"tools", "scraper", name}, "bundled"},
		{[]string{"tools", "bin", "scraper", name}, "dev"},
	} {
		if found := toolpath.Find(candidate.rel...); found != "" {
			return found, candidate.source
		}
	}

	if found, err := exec.LookPath(name); err == nil {
		return found, "path"
	}
	return "", ""
}

// scraperSource 返回刮削器的路径与来源（外部调用，自己加锁）。
func (a *App) scraperSource() (string, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.resolveScraperLocked()
}

// scraperPath 返回当前刮削器路径，没有则空。
func (a *App) scraperPath() string {
	path, _ := a.scraperSource()
	return path
}

// resolveFFprobe 按当前配置重新定位 ffprobe 并重建 Prober，返回所用路径。
// 调用方必须持有 a.mu。
func (a *App) resolveFFprobe() string {
	path, _ := a.resolveFFprobeLocked()
	return path
}

// resolveFFprobeLocked 同 resolveFFprobe，另返回来源（component/auto）。
//
// 构造 Prober 只有这一处入口（启动、保存设置、环境诊断都走它）：
// ffprobe 的实际路径是「配置 → 组件 → 标准位置 → PATH」一次完整解析的结果，
// 直接拿 cfg.FFprobePath 构造就是第二套状态源。设置页没有 ffprobe 输入框，
// 这一项恒为空，于是用户每保存一次任意设置，PATH 里找到的那份就被丢掉，
// 诊断显示「未找到」—— 而 scraper 每次都完整解析，同一个页面上两个工具
// 的表现因此对不上。
func (a *App) resolveFFprobeLocked() (string, string) {
	var preferred []string
	if found := a.componentPath(componentFFprobe); found != "" {
		preferred = append(preferred, found)
	}
	path := probe.LocateWith(a.cfg.FFprobePath, preferred...)
	// 路径没变就不重建：Prober 内含并发信号量，导入进行中被替换会让
	// 新旧两个各带 4 个额度的信号量同时生效，瞬时并发翻倍。
	if a.prober == nil || a.prober.Path() != path {
		a.prober = probe.New(path, 4)
	}
	source := "auto"
	if len(preferred) > 0 && path == preferred[0] {
		source = "component"
	}
	return path, source
}

// ffprobeSourceLocked 返回 ffprobe 的路径与来源。调用方必须持有 a.mu。
func (a *App) ffprobeSourceLocked() (string, string) {
	if a.prober == nil {
		return a.resolveFFprobeLocked()
	}
	path := a.prober.Path()
	source := "auto"
	if a.componentPath(componentFFprobe) == path && path != "" {
		source = "component"
	}
	return path, source
}

// refreshFFprobe 在组件变化后重新解析 ffprobe 并重建 Prober。
func (a *App) refreshFFprobe() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.resolveFFprobeLocked()
}

// invalidateHealth 让下一次 ScraperHealth 重新跑 doctor。
//
// 组件换掉之后缓存里的 Chrome/驱动信息仍然有效，但 scraper 版本与协议必须
// 重新读 —— 否则设置页会显示「已是最新」而实际刚装完。
func (a *App) invalidateHealth() {
	a.healthMu.Lock()
	defer a.healthMu.Unlock()
	a.healthKey = ""
	a.healthValue = scraper.Health{}
	a.healthAt = time.Time{}
}

// cleanupComponents 清理上次留下的半成品与旧版本目录。
//
// 启动时在后台跑：失败只影响磁盘占用，不该拦住应用启动。
func (a *App) cleanupComponents() {
	svc := a.components
	if svc == nil || !svc.available() {
		return
	}
	go func() {
		if err := svc.manager.CleanupStale(); err != nil {
			log.Printf("提示：清理旧组件目录失败（下次启动会再试）：%v", err)
		}
	}()
}
