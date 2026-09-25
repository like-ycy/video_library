package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"videolib/internal/config"
	"videolib/internal/index"
	"videolib/internal/library"
	"videolib/internal/media"
	"videolib/internal/player"
	"videolib/internal/probe"
	"videolib/internal/scraper"
	"videolib/internal/toolpath"
)

// 推给前端的事件名。前端按这些名字订阅，属于对外契约的一部分。
const (
	eventScrapeProgress   = "scrape:progress"
	eventScrapeItemDone   = "scrape:item-done"
	eventScrapeItemFailed = "scrape:item-failed"
	eventScrapeFinished   = "scrape:finished"
	eventIndexProgress    = "index:progress"
	eventIndexDone        = "index:done"
	eventIndexFailed      = "index:failed"
)

// logRingSize 是保留的刮削日志行数。
//
// 保留最近若干行供 UI 查看，而不是把每一行都推给前端：seleniumbase 的输出量
// 很大，逐行发事件会让 WebView 忙于渲染日志而卡住界面。
const logRingSize = 500

// healthCacheTTL 是 doctor 结果的复用窗口。
//
// doctor 是一次 Python 冷启动（秒级），而设置页与环境诊断页每次进入都会查
// 一遍，连着点开就是两次冷启动。环境不会在半分钟内自己变化；真正会变的
// （scraper 路径）由缓存键兜住，路径一改立刻失效，不必等 TTL。
// 主动刷新走 force，不受这个窗口约束。
const healthCacheTTL = 30 * time.Second

// App 是暴露给前端的绑定对象。
//
// 这里只做四件事：校验参数、调用领域模块、把结果转成前端可用的形状、发事件。
// 任何实际业务逻辑都应落在 internal/ 下 —— 绑定层一旦开始写业务，它就会
// 迅速变成什么都往里塞的地方。
type App struct {
	ctx context.Context

	mu     sync.Mutex
	cfg    config.Config
	stores map[string]*index.Store

	prober *probe.Prober
	runner *scraper.Runner

	// stream 是本地视频流服务。Windows 上不能让大视频走 Wails AssetServer：
	// WebView2 响应体整包进内存，一点播放就可能把进程打崩。
	stream *media.StreamServer

	scrapeMu     sync.Mutex
	scrapeCancel context.CancelFunc
	scraping     bool

	// doctor 结果缓存。单独一把锁：它在 a.mu 之外也可能被读（ScraperHealth
	// 运行期间不会持有 a.mu），混进 a.mu 会让「查环境」这件事被长操作挡住。
	healthMu    sync.Mutex
	healthAt    time.Time
	healthKey   string
	healthValue scraper.Health

	logMu   sync.Mutex
	logRing []string
}

// NewApp 构造 App 并加载配置。
func NewApp() (*App, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if err := cfg.Save(); err != nil {
		// 配置写不进去不致命（只读环境仍能浏览），但要让用户看见。
		log.Printf("警告：无法写入配置文件：%v", err)
	}

	app := &App{
		cfg:     cfg,
		stores:  make(map[string]*index.Store),
		runner:  &scraper.Runner{Site: "javlibrary"},
		logRing: make([]string, 0, logRingSize),
	}
	app.mu.Lock()
	ffprobePath := app.resolveFFprobe()
	app.mu.Unlock()
	if ffprobePath == "" {
		log.Printf("警告：未找到 ffprobe，视频时长与编码信息将不可用")
	}
	return app, nil
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.rebuildRunner()

	stream, err := media.StartStreamServer(a.ResolveLibraryRoot)
	if err != nil {
		// 起不来就退回 Wails /media/ 路径：封面能看，只是大视频仍有崩溃风险。
		log.Printf("警告：本地视频流服务启动失败，内嵌播放可能不稳定：%v", err)
		return
	}
	a.mu.Lock()
	a.stream = stream
	a.mu.Unlock()
}

func (a *App) shutdown(context.Context) {
	a.scrapeMu.Lock()
	if a.scrapeCancel != nil {
		a.scrapeCancel()
		a.scrapeCancel = nil
	}
	a.scrapeMu.Unlock()

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stream != nil {
		_ = a.stream.Close()
		a.stream = nil
	}
	for id, store := range a.stores {
		_ = store.Close()
		delete(a.stores, id)
	}
}

// Close 释放资源。wails.Run 返回后调用。
func (a *App) Close() {
	a.shutdown(context.Background())
}

// rebuildRunner 按当前配置重建刮削器调用器。
func (a *App) rebuildRunner() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.runner.ExePath = a.resolveScraperPath()
	a.runner.Concurrency = a.cfg.Concurrency
	a.runner.IdleTimeout = time.Duration(a.cfg.ScrapeTimeoutMin) * time.Minute
	// 工作目录必须可写：seleniumbase 会在 CWD 下建 downloaded_files/，
	// 而 macOS .app 启动时 CWD 通常是只读的 /。
	if dir, err := config.Dir(); err == nil {
		a.runner.WorkDir = dir
	} else {
		a.runner.WorkDir = os.TempDir()
	}
}

// resolveScraperPath 定位刮削器可执行文件。
//
// 顺序：显式配置 → 标准位置（见 internal/toolpath）→ PATH。
//
// 两种布局都要认，且分发布局必须排在最前：
//
//	分发  <App>/tools/scraper/scraper.exe   ← tools/package.ps1 产出
//	开发  <repo>/tools/bin/scraper/scraper.exe
//
// 只认开发布局是曾经的 bug：开发期仓库里恰好有 tools/bin/，一切正常，
// 而 user 装好分发包后必然找不到刮削器，且报错完全指不到目录层级上。
func (a *App) resolveScraperPath() string {
	if a.cfg.ScraperPath != "" {
		return a.cfg.ScraperPath
	}

	name := toolpath.Name("scraper")

	// 分发布局优先于开发布局：正式分发物里不该出现 tools/bin/，
	// 万一出现也说明那是残留，不该压过随包分发的正本。
	for _, rel := range [][]string{
		{"tools", "scraper", name},
		{"tools", "bin", "scraper", name},
	} {
		if found := toolpath.Find(rel...); found != "" {
			return found
		}
	}

	if found, err := exec.LookPath(name); err == nil {
		return found
	}
	return ""
}

// resolveFFprobe 按当前配置重新定位 ffprobe 并重建 Prober，返回所用路径。
// 调用方必须持有 a.mu。
//
// 构造 Prober 只有这一处入口（启动、保存设置、环境诊断都走它）：
// ffprobe 的实际路径是「配置 → 标准位置 → PATH」一次完整解析的结果，
// 直接拿 cfg.FFprobePath 构造就是第二套状态源。设置页没有 ffprobe 输入框，
// 这一项恒为空，于是用户每保存一次任意设置，PATH 里找到的那份就被丢掉，
// 诊断显示「未找到」—— 而 scraper 每次都完整解析，同一个页面上两个工具
// 的表现因此对不上。
func (a *App) resolveFFprobe() string {
	path := probe.Locate(a.cfg.FFprobePath)
	// 路径没变就不重建：Prober 内含并发信号量，导入进行中被替换会让
	// 新旧两个各带 4 个额度的信号量同时生效，瞬时并发翻倍。
	if a.prober == nil || a.prober.Path() != path {
		a.prober = probe.New(path, 4)
	}
	return path
}

// ── 媒体服务 ──────────────────────────────────────────────────────────────

// ResolveLibraryRoot 供 media.Handler 把库标识解析为根目录。
func (a *App) ResolveLibraryRoot(libraryID string) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	ref, ok := a.cfg.Find(libraryID)
	if !ok {
		return "", false
	}
	return ref.Root, true
}

// ── 库管理 ────────────────────────────────────────────────────────────────

// LibraryDTO 是给前端的库信息。
type LibraryDTO struct {
	ID        string `json:"id"`
	Root      string `json:"root"`
	Available bool   `json:"available"`
}

func (a *App) ListLibraries() []LibraryDTO {
	a.mu.Lock()
	defer a.mu.Unlock()

	result := make([]LibraryDTO, 0, len(a.cfg.Libraries))
	for _, ref := range a.cfg.Libraries {
		info, err := os.Stat(ref.Root)
		result = append(result, LibraryDTO{
			ID:        ref.ID,
			Root:      ref.Root,
			Available: err == nil && info.IsDir(),
		})
	}
	return result
}

// PickLibraryRoot 弹出目录选择对话框，返回用户选中的路径。
func (a *App) PickLibraryRoot() (string, error) {
	if a.ctx == nil {
		return "", errors.New("应用尚未就绪")
	}
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:                "选择视频库根目录（其下应是「演员名/视频文件.mp4」结构）",
		CanCreateDirectories: false,
	})
}

// AddLibrary 添加一个视频库。重复添加同一目录是幂等的。
func (a *App) AddLibrary(root string) (LibraryDTO, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return LibraryDTO{}, errors.New("未选择目录")
	}
	info, err := os.Stat(root)
	if err != nil {
		return LibraryDTO{}, fmt.Errorf("目录不可访问：%w", err)
	}
	if !info.IsDir() {
		return LibraryDTO{}, fmt.Errorf("%s 不是目录", root)
	}

	ref := config.LibraryRef{ID: config.LibraryID(root), Root: root}

	a.mu.Lock()
	replaced := false
	for i, existing := range a.cfg.Libraries {
		if existing.ID == ref.ID {
			a.cfg.Libraries[i] = ref
			replaced = true
			break
		}
	}
	if !replaced {
		a.cfg.Libraries = append(a.cfg.Libraries, ref)
	}
	cfg := a.cfg
	a.mu.Unlock()

	if err := cfg.Save(); err != nil {
		return LibraryDTO{}, fmt.Errorf("保存配置：%w", err)
	}
	return LibraryDTO{ID: ref.ID, Root: ref.Root, Available: true}, nil
}

// RemoveLibrary 移除一个视频库。
//
// 只从配置里移除，不动磁盘上的任何文件，也不删除索引数据库 ——
// 用户误删之后重新添加即可拿回全部数据（包括收藏与进度）。
func (a *App) RemoveLibrary(libraryID string) error {
	a.mu.Lock()
	kept := a.cfg.Libraries[:0]
	for _, ref := range a.cfg.Libraries {
		if ref.ID != libraryID {
			kept = append(kept, ref)
		}
	}
	a.cfg.Libraries = kept
	cfg := a.cfg
	a.mu.Unlock()

	return cfg.Save()
}

// ── 观影模块 ──────────────────────────────────────────────────────────────

// videoDTO 是给前端的视频记录。
//
// 媒体地址由 Go 生成而不是前端拼接：相对路径的基准（演员目录）、中文目录名的
// 转义、以及 /media/ 与 /art/ 的路由规则都属于后端知识，前端拼错的表现是
// 「封面永远是空白」，很难归因。
type videoDTO struct {
	index.Row
	CoverURL string   `json:"coverUrl"`
	ShotURLs []string `json:"shotUrls"`
	VideoURL string   `json:"videoUrl"`
	// Playable 为 false 表示文件已不在磁盘上（row.Missing）。
	Playable bool `json:"playable"`
}

type videoPage struct {
	Items    []videoDTO `json:"items"`
	Total    int        `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"pageSize"`
}

func (a *App) decorate(libraryID string, row index.Row) videoDTO {
	dto := videoDTO{Row: row, Playable: !row.Missing}
	if row.CoverRel != "" {
		// 边车 JSON 中的相对路径以演员目录为基准；补上演员目录名才是相对库根的路径。
		dto.CoverURL = media.ArtURL(libraryID, path.Join(row.Actress, row.CoverRel))
	}
	for _, shot := range row.ShotsRel {
		dto.ShotURLs = append(dto.ShotURLs, media.ArtURL(libraryID, path.Join(row.Actress, shot)))
	}
	if row.VideoRel != "" {
		dto.VideoURL = a.videoURL(libraryID, row.VideoRel)
	}
	return dto
}

// videoURL 优先走本地流媒体服务；服务未就绪时退回 Wails 的 /media/ 路径。
func (a *App) videoURL(libraryID, rel string) string {
	a.mu.Lock()
	stream := a.stream
	a.mu.Unlock()
	if stream != nil {
		return stream.VideoURL(libraryID, rel)
	}
	return media.VideoURL(libraryID, rel)
}

func (a *App) store(libraryID string) (*index.Store, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if opened, ok := a.stores[libraryID]; ok {
		return opened, nil
	}
	if _, ok := a.cfg.Find(libraryID); !ok {
		return nil, fmt.Errorf("未找到视频库：%s", libraryID)
	}
	dbPath, err := config.DBPath(libraryID)
	if err != nil {
		return nil, err
	}
	opened, err := index.Open(dbPath)
	if err != nil {
		return nil, err
	}
	a.stores[libraryID] = opened
	return opened, nil
}

// QueryVideos 分页查询视频。
func (a *App) QueryVideos(
	libraryID string,
	filter index.Filter,
	sort index.Sort,
	page, pageSize int,
) (videoPage, error) {
	store, err := a.store(libraryID)
	if err != nil {
		return videoPage{}, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 500 {
		pageSize = 120
	}

	rows, total, err := store.QueryVideos(a.ctx, libraryID, filter, sort, pageSize, (page-1)*pageSize)
	if err != nil {
		return videoPage{}, err
	}

	items := make([]videoDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, a.decorate(libraryID, row))
	}
	return videoPage{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func (a *App) GetVideo(libraryID string, videoID int64) (videoDTO, error) {
	store, err := a.store(libraryID)
	if err != nil {
		return videoDTO{}, err
	}
	row, err := store.GetVideo(a.ctx, libraryID, videoID)
	if err != nil {
		return videoDTO{}, err
	}
	return a.decorate(libraryID, row), nil
}

func (a *App) ListActresses(libraryID string) ([]index.ActressCount, error) {
	store, err := a.store(libraryID)
	if err != nil {
		return nil, err
	}
	return store.ListActresses(a.ctx, libraryID)
}

func (a *App) ListGenres(libraryID string) ([]string, error) {
	store, err := a.store(libraryID)
	if err != nil {
		return nil, err
	}
	return store.ListGenres(a.ctx, libraryID)
}

func (a *App) LibraryStats(libraryID string) (index.Stats, error) {
	store, err := a.store(libraryID)
	if err != nil {
		return index.Stats{}, err
	}
	return store.LibraryStats(a.ctx, libraryID)
}

// ToggleFavorite 切换收藏。
func (a *App) ToggleFavorite(libraryID string, videoID int64) (bool, error) {
	store, err := a.store(libraryID)
	if err != nil {
		return false, err
	}
	row, err := store.GetVideo(a.ctx, libraryID, videoID)
	if err != nil {
		return false, err
	}
	return store.ToggleFavorite(a.ctx, libraryID, row.Actress, row.Fanha)
}

// SetRating 设置评分，rating 为 nil 表示清除。
func (a *App) SetRating(libraryID string, videoID int64, rating *int) error {
	store, err := a.store(libraryID)
	if err != nil {
		return err
	}
	row, err := store.GetVideo(a.ctx, libraryID, videoID)
	if err != nil {
		return err
	}
	return store.SetRating(a.ctx, libraryID, row.Actress, row.Fanha, rating)
}

// SaveProgress 保存播放位置。
func (a *App) SaveProgress(libraryID string, videoID int64, positionMs int64) error {
	store, err := a.store(libraryID)
	if err != nil {
		return err
	}
	row, err := store.GetVideo(a.ctx, libraryID, videoID)
	if err != nil {
		return err
	}
	return store.SaveProgress(a.ctx, libraryID, row.Actress, row.Fanha, positionMs)
}

// OpenInPlayer 用外部播放器打开视频。
//
// resume 为 true 时尝试从上次位置续播。这条路径必须存在：WebView2 对 HEVC、
// 10bit 等编码支持不可靠，内嵌播放可能黑屏。
func (a *App) OpenInPlayer(libraryID string, videoID int64, resume bool) error {
	store, err := a.store(libraryID)
	if err != nil {
		return err
	}
	ref, ok := a.cfgAt(libraryID)
	if !ok {
		return fmt.Errorf("未找到视频库：%s", libraryID)
	}
	row, err := store.GetVideo(a.ctx, libraryID, videoID)
	if err != nil {
		return err
	}

	// VideoRel 存的是跨平台的正斜杠形式，拼成本机路径前必须转换。
	full := filepath.Join(ref.Root, filepath.FromSlash(row.VideoRel))
	if _, statErr := os.Stat(full); statErr != nil {
		return fmt.Errorf("视频文件不存在：%s", row.VideoRel)
	}

	var positionMs int64
	if resume {
		if data, dataErr := store.GetUserData(a.ctx, libraryID, row.Actress, row.Fanha); dataErr == nil {
			positionMs = data.WatchPositionMs
		}
	}

	if err := player.OpenAt(a.playerPath(), full, positionMs); err != nil {
		return err
	}
	return store.RecordPlay(a.ctx, libraryID, row.Actress, row.Fanha)
}

// PlayEmbedded 标记一次内嵌播放开始（计入最近播放）。
func (a *App) PlayEmbedded(libraryID string, videoID int64) error {
	store, err := a.store(libraryID)
	if err != nil {
		return err
	}
	row, err := store.GetVideo(a.ctx, libraryID, videoID)
	if err != nil {
		return err
	}
	return store.RecordPlay(a.ctx, libraryID, row.Actress, row.Fanha)
}

// ── 刮削模块 ──────────────────────────────────────────────────────────────

// CandidateDTO 是扫描得到的待刮削条目。
type CandidateDTO struct {
	Actress    string `json:"actress"`
	Fanha      string `json:"fanha"`
	Stem       string `json:"stem"`
	VideoFile  string `json:"videoFile"`
	FileSize   int64  `json:"fileSize"`
	Scraped    bool   `json:"scraped"`
	MissingArt bool   `json:"missingArt"`
	// ArtDir 是该条目的图片目录，MissingFiles 是边车里记了但磁盘上没有的图片。
	//
	// 两者存在的唯一目的是把「缺图」这句话补完整：用户能看到程序实际在找哪个
	// 目录、缺的是哪几个文件，而不是对着一个红色标签猜。
	ArtDir       string   `json:"artDir"`
	MissingFiles []string `json:"missingFiles"`
}

// ScanResultDTO 是扫描结果。
type ScanResultDTO struct {
	Candidates []CandidateDTO `json:"candidates"`
	Issues     []IssueDTO     `json:"issues"`
}

// IssueDTO 是扫描中发现的问题。
type IssueDTO struct {
	Kind    string `json:"kind"`
	Actress string `json:"actress"`
	Subject string `json:"subject"`
	Message string `json:"message"`
}

// ScanLibrary 扫描视频库，返回待刮削条目。
//
// 只读文件系统，不碰索引 —— 它同时服务于刮削模块的选择列表和观影模块的
// 「重新扫描」。索引更新走 ImportLibrary。
func (a *App) ScanLibrary(libraryID string) (ScanResultDTO, error) {
	ref, ok := a.cfgAt(libraryID)
	if !ok {
		return ScanResultDTO{}, fmt.Errorf("未找到视频库：%s", libraryID)
	}

	scan, err := library.Scan(ref.Root)
	if err != nil {
		return ScanResultDTO{}, err
	}

	result := ScanResultDTO{Candidates: make([]CandidateDTO, 0, len(scan.Candidates))}
	for _, candidate := range scan.Candidates {
		result.Candidates = append(result.Candidates, CandidateDTO{
			Actress:      candidate.Actress,
			Fanha:        candidate.Fanha,
			Stem:         candidate.Stem,
			VideoFile:    candidate.VideoFile,
			FileSize:     candidate.FileSize,
			Scraped:      candidate.Scraped,
			MissingArt:   candidate.MissingArt,
			ArtDir:       candidate.ArtDir,
			MissingFiles: candidate.MissingFiles,
		})
	}
	for _, issue := range scan.Issues {
		result.Issues = append(result.Issues, IssueDTO{
			Kind: string(issue.Kind), Actress: issue.Actress,
			Subject: issue.Subject, Message: issue.Message,
		})
	}
	return result, nil
}

// ImportLibrary 把视频库同步进索引（可重跑的幂等操作）。
func (a *App) ImportLibrary(libraryID string) (index.ImportStats, error) {
	ref, ok := a.cfgAt(libraryID)
	if !ok {
		return index.ImportStats{}, fmt.Errorf("未找到视频库：%s", libraryID)
	}
	store, err := a.store(libraryID)
	if err != nil {
		return index.ImportStats{}, err
	}

	emit := func(done, total int) {
		a.emit(eventIndexProgress, map[string]any{"done": done, "total": total})
	}
	return index.Import(a.ctx, store, libraryID, ref.Root, a.prober, emit)
}

// RebuildIndex 清空并重建索引。
func (a *App) RebuildIndex(libraryID string) (index.ImportStats, error) {
	store, err := a.store(libraryID)
	if err != nil {
		return index.ImportStats{}, err
	}
	if err := store.Reset(a.ctx); err != nil {
		return index.ImportStats{}, err
	}
	return a.ImportLibrary(libraryID)
}

// StartScrape 启动一次刮削。
//
// 立即返回，进度通过事件推送 —— 一次批量刮削可能持续几十分钟，
// 同步等待会把前端请求挂死。
func (a *App) StartScrape(libraryID string, stems []string, force bool) error {
	if len(stems) == 0 {
		return errors.New("没有选中任何条目")
	}
	if !a.prober.Available() {
		// 只提示，不阻断：没有 ffprobe 只影响时长展示。
		log.Printf("提示：未找到 ffprobe，刮削后不会写入时长与编码信息")
	}

	ref, ok := a.cfgAt(libraryID)
	if !ok {
		return fmt.Errorf("未找到视频库：%s", libraryID)
	}
	store, err := a.store(libraryID)
	if err != nil {
		return err
	}

	plan, err := a.planScrape(ref, stems, force)
	if err != nil {
		return err
	}

	a.scrapeMu.Lock()
	if a.scraping {
		a.scrapeMu.Unlock()
		return errors.New("已有刮削任务正在进行，请先取消或等待完成")
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.scrapeCancel = cancel
	a.scraping = true
	a.scrapeMu.Unlock()

	go a.runScrape(ctx, store, plan)
	return nil
}

// CancelScrape 取消正在进行的刮削。返回 false 表示当时没有任务在跑。
func (a *App) CancelScrape() bool {
	a.scrapeMu.Lock()
	defer a.scrapeMu.Unlock()
	if a.scrapeCancel == nil {
		return false
	}
	a.scrapeCancel()
	return true
}

// ScrapeStatus 返回当前是否在刮削。
func (a *App) ScrapeStatus() bool {
	a.scrapeMu.Lock()
	defer a.scrapeMu.Unlock()
	return a.scraping
}

// GetScrapeLog 返回最近的刮削日志。
func (a *App) GetScrapeLog() []string {
	a.logMu.Lock()
	defer a.logMu.Unlock()
	out := make([]string, len(a.logRing))
	copy(out, a.logRing)
	return out
}

// ScraperHealth 检查刮削器环境，供 UI 在开始前引导用户。
//
// force 为 true 时绕过 TTL 缓存（设置页的「重新自检」「重新检查」按钮）；
// 为 false 时命中窗口内的上次结果，避免每次进页面都冷启动一次 Python。
// 缓存键是 scraper 路径：换了路径不再命中，即便仍在 TTL 内。
// 出错不缓存 —— 异常状态必须每次都被看见，不能被上一次的结果盖住。
func (a *App) ScraperHealth(force bool) (scraper.Health, error) {
	a.rebuildRunner()
	exePath := a.runner.ExePath
	if exePath == "" {
		return scraper.Health{}, errors.New(
			"未找到刮削器组件。请先运行 tools/build.sh 完成打包，" +
				"或在配置中指定 scraper_path")
	}
	if !force {
		if cached, ok := a.cachedHealth(exePath); ok {
			return cached, nil
		}
	}

	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()

	var health scraper.Health
	if err := a.runner.Inspect(ctx, "doctor", &health); err != nil {
		return scraper.Health{}, err
	}
	if health.V != scraper.ProtocolVersion {
		return health, fmt.Errorf("刮削器协议版本为 v%d，本程序需要 v%d，请重新打包刮削组件",
			health.V, scraper.ProtocolVersion)
	}
	a.storeHealth(exePath, health)
	return health, nil
}

// cachedHealth 取仍在 TTL 内、且键匹配的 doctor 结果。
func (a *App) cachedHealth(key string) (scraper.Health, bool) {
	a.healthMu.Lock()
	defer a.healthMu.Unlock()
	if a.healthKey == key && time.Since(a.healthAt) < healthCacheTTL {
		return a.healthValue, true
	}
	return scraper.Health{}, false
}

func (a *App) storeHealth(key string, health scraper.Health) {
	a.healthMu.Lock()
	defer a.healthMu.Unlock()
	a.healthKey = key
	a.healthAt = time.Now()
	a.healthValue = health
}

// ── 内部实现 ──────────────────────────────────────────────────────────────

type scrapePlan struct {
	ref  config.LibraryRef
	jobs []scraper.Job

	// byID 是 job 标识 → 候选。事件靠标识绑回具体文件与其输出目录，
	// 见 library.JobID 的说明。
	byID map[string]library.Candidate

	// byFanha 只用于兼容不带 job 标识的旧刮削器：那时只能按番号找。
	//
	// 同一番号有多个文件时它必然绑错（后写入的候选把先写入的覆盖掉会被刻意
	// 避开，但「取哪一个」本身就没有正确答案），所以这条路径只保证「不崩」，
	// 不保证「对」。真要修好旧组件，办法是升级刮削器。
	byFanha map[string]library.Candidate
}

// at 按事件携带的标识取回候选。标识优先，番号是旧组件的回退。
func (p scrapePlan) at(item scraper.Item) (library.Candidate, bool) {
	if item.ID != "" {
		candidate, ok := p.byID[item.ID]
		return candidate, ok
	}
	candidate, ok := p.byFanha[item.Fanha]
	return candidate, ok
}

// actresses 返回本次涉及的全部演员目录，用于写边车与事后复核。
func (p scrapePlan) actresses() map[string]library.Candidate {
	byActress := make(map[string]library.Candidate, len(p.byID))
	for _, candidate := range p.byID {
		byActress[candidate.Actress] = candidate
	}
	return byActress
}

func (a *App) planScrape(ref config.LibraryRef, stems []string, force bool) (scrapePlan, error) {
	scan, err := library.Scan(ref.Root)
	if err != nil {
		return scrapePlan{}, err
	}

	byStem := make(map[string]library.Candidate, len(scan.Candidates))
	for _, candidate := range scan.Candidates {
		byStem[candidate.Stem] = candidate
	}

	plan := scrapePlan{
		ref:     ref,
		byID:    make(map[string]library.Candidate, len(stems)),
		byFanha: make(map[string]library.Candidate, len(stems)),
	}
	var vanished []string
	for _, stem := range stems {
		candidate, ok := byStem[stem]
		if !ok {
			// 用户界面上选中的条目在扫描之后被移动或改名了。跳过而不是报错：
			// 一个条目失效不该让整批刮削无法开始。
			vanished = append(vanished, stem)
			continue
		}
		id := library.JobID(candidate.Actress, candidate.Stem)
		plan.byID[id] = candidate
		if _, exists := plan.byFanha[candidate.Fanha]; !exists {
			plan.byFanha[candidate.Fanha] = candidate
		}
		plan.jobs = append(plan.jobs, scraper.Job{
			ID:    id,
			Fanha: candidate.Fanha,
			// 输出目录由 Go 计算，Python 因此不需要知道库的目录布局。
			Out:   library.ArtDir(candidate.ActressDir, candidate.Stem),
			Force: force,
		})
	}
	for _, stem := range vanished {
		a.appendLog(fmt.Sprintf("跳过 %s：扫描之后被移动或改名了", stem))
	}
	if len(plan.jobs) == 0 {
		return scrapePlan{}, errors.New("选中的条目已不存在，请重新扫描")
	}
	a.logPlan(plan)
	return plan, nil
}

// logPlan 把本次的 job 清单写进刮削日志。
//
// 以前整份日志里看不到「哪个番号写到哪个目录」—— 而排查「刮削成功但没有图」
// 时，要回答的第一个问题就是它。顺带点出同番号的条目，说明它们共享一份元数据，
// 免得用户以为有一条没刮。
func (a *App) logPlan(plan scrapePlan) {
	seen := make(map[string]string, len(plan.jobs))
	for _, job := range plan.jobs {
		a.appendLog(fmt.Sprintf("刮削目标 %s（番号 %s）→ %s", job.ID, job.Fanha, job.Out))
		if first, ok := seen[job.Fanha]; ok {
			a.appendLog(fmt.Sprintf(
				"提示：%s 与 %s 番号相同（%s），共享同一份元数据，边车 JSON 只保留一条记录",
				first, job.ID, job.Fanha))
			continue
		}
		seen[job.Fanha] = job.ID
	}
}

func (a *App) runScrape(ctx context.Context, store *index.Store, plan scrapePlan) {
	defer func() {
		a.scrapeMu.Lock()
		a.scraping = false
		a.scrapeCancel = nil
		a.scrapeMu.Unlock()
	}()

	a.rebuildRunner()

	var (
		collectedMu sync.Mutex
		collected   = make(map[string][]library.Video)
		failuresMu  sync.Mutex
		failures    []string
	)

	callbacks := scraper.Callbacks{
		Progress: func(item scraper.Item, stage string, percent float64) {
			a.emit(eventScrapeProgress, map[string]any{
				"fanha": item.Fanha, "stage": stage, "percent": percent,
			})
		},
		ItemDone: func(item scraper.Item, data scraper.ItemData) {
			candidate, ok := plan.at(item)
			if !ok {
				// 认不出这条事件属于哪个文件。以前这种情况会被静默丢掉，用户只看到
				// 「少了一条」；写进日志才能发现是刮削器没回显 job 标识、还是标识
				// 对不上。
				a.appendLog(fmt.Sprintf(
					"警告：收到无法归属的完成事件（job=%q 番号=%q），已忽略", item.ID, item.Fanha))
				return
			}
			video := buildVideoRecord(candidate, data)
			collectedMu.Lock()
			collected[candidate.Actress] = append(collected[candidate.Actress], video)
			collectedMu.Unlock()

			// 把「写到哪个目录」与「记下哪些文件名」打进日志。Windows 上出问题时
			// 这两条是判断「Go 记的路径」与「Python 实际写的路径」是否一致的唯一依据。
			a.appendLog(fmt.Sprintf(
				"完成 %s：封面=%s，截图=%d/%d 张",
				item, video.Cover, len(data.ShotFiles), data.TotalShots))

			a.emit(eventScrapeItemDone, map[string]any{
				"fanha": item.Fanha, "title": data.Title,
				"shots": len(data.ShotFiles), "totalShots": data.TotalShots,
			})
		},
		ItemFailed: func(item scraper.Item, reason, detail string) {
			failuresMu.Lock()
			failures = append(failures, fmt.Sprintf("%s（%s）", item, scraper.ReasonText(reason)))
			failuresMu.Unlock()

			a.emit(eventScrapeItemFailed, map[string]any{
				"fanha": item.Fanha, "reason": reason,
				"message":   scraper.ReasonText(reason),
				"detail":    detail,
				"retryable": scraper.Retryable(reason),
			})
		},
		Log: a.appendLog,
	}

	result, runErr := a.runner.Run(ctx, plan.jobs, callbacks)

	// 无论成功、部分失败还是被取消，都要把已拿到的结果写进边车 JSON ——
	// 用户取消了后 80 条，前 20 条的成功结果是有效的，丢掉它们等于白干。
	merged, mergeErr := a.persistSidecars(plan, collected)

	// 失败项完全不碰边车 JSON：如果这一条以前刮过，旧记录会原样留着，
	// 而它的图片可能早就没了。扫描页于是显示「缺图」，却没有任何地方解释
	// 为什么 —— 这句话就是解释。
	failuresMu.Lock()
	failedCount := len(failures)
	failureLines := append([]string(nil), failures...)
	failuresMu.Unlock()
	if failedCount > 0 {
		a.appendLog(fmt.Sprintf(
			"本次有 %d 条失败，边车 JSON 未被修改：%s —— 之前刮过的条目会保留旧记录，扫描页可能显示缺图",
			failedCount, strings.Join(failureLines, "、")))
	}

	// 复核刚落盘的边车：JSON 里记的图片是不是真的在磁盘上。
	a.verifyPersisted(plan, merged)

	payload := map[string]any{
		"ok":           result.Summary.OK,
		"failed":       result.Summary.Failed,
		"skipped":      result.Summary.Skipped,
		"canceled":     result.Canceled,
		"fatal":        result.FatalReason,
		"fatalMessage": fatalMessage(result),
	}
	if mergeErr != nil {
		payload["error"] = mergeErr.Error()
	}
	if runErr != nil && result.FatalReason == "" {
		payload["error"] = runErr.Error()
	}
	a.emit(eventScrapeFinished, payload)

	if len(merged) == 0 {
		return
	}
	// 刮削只改了元数据与图片，没有改视频文件，因此重新导入几乎不会触发
	// ffprobe（mtime 未变），代价只是一次目录扫描。
	if _, err := a.ImportLibrary(plan.ref.ID); err != nil {
		a.emit(eventIndexFailed, map[string]any{"message": err.Error()})
		return
	}
	a.emit(eventIndexDone, map[string]any{"actresses": len(merged)})
}

// buildVideoRecord 把刮削结果转成边车 JSON 的记录。
//
// 图片路径在这里拼出来：Python 只报告它写出了哪些文件名，
// 相对路径的基准（演员目录）是 Go 侧的知识。
func buildVideoRecord(candidate library.Candidate, data scraper.ItemData) library.Video {
	prefix := library.ArtRelPrefix(candidate.Stem)

	shots := make([]string, 0, len(data.ShotFiles))
	for _, name := range data.ShotFiles {
		shots = append(shots, path.Join(prefix, name))
	}

	video := library.Video{
		Fanha:         candidate.Fanha,
		Stem:          candidate.Stem,
		Title:         data.Title,
		ReleaseDate:   data.ReleaseDate,
		SiteLengthMin: data.SiteLengthMin,
		Genres:        data.Genres,
		Cast:          data.Cast,
		Screenshots:   shots,
		VideoFile:     candidate.VideoFile,
		FileSize:      candidate.FileSize,
		ScrapedAt:     time.Now().Format(time.RFC3339),
	}
	if data.CoverFile != "" {
		video.Cover = path.Join(prefix, data.CoverFile)
	}
	return video
}

// persistSidecars 按演员合并写入边车 JSON，返回受影响的演员名。
//
// 按演员聚合后再写，而不是每个视频写一次：一个演员目录共享同一份 JSON，
// 逐个写会造成大量重复的读-改-写。
func (a *App) persistSidecars(
	plan scrapePlan,
	collected map[string][]library.Video,
) ([]string, error) {
	byActressDir := plan.actresses()

	var (
		merged  []string
		failure error
	)
	for actress, videos := range collected {
		candidate, ok := byActressDir[actress]
		if !ok {
			continue
		}
		// 同一番号的多个文件共享一份元数据，边车按番号就地覆盖，最后一条留下。
		// 排序让「文件名主干就等于番号」的基准文件胜出：否则同一批刮削的两次
		// 运行可能记下不同的 stem，图片目录随之漂移，另一份图就成了孤儿。
		sort.SliceStable(videos, func(i, j int) bool {
			baseI := strings.EqualFold(videos[i].Stem, videos[i].Fanha)
			baseJ := strings.EqualFold(videos[j].Stem, videos[j].Fanha)
			if baseI != baseJ {
				return !baseI
			}
			return videos[i].Stem < videos[j].Stem
		})

		sidecarPath := library.SidecarPath(candidate.ActressDir, actress)
		if _, err := library.MergeVideos(sidecarPath, actress, videos); err != nil {
			failure = errors.Join(failure, fmt.Errorf("写入 %s: %w", actress, err))
			a.appendLog(fmt.Sprintf("写入边车失败 %s：%v", sidecarPath, err))
			continue
		}
		a.appendLog(fmt.Sprintf("已写入边车 %s（本次 %d 条）", sidecarPath, len(videos)))
		merged = append(merged, actress)
	}
	return merged, failure
}

// verifyPersisted 复核刚落盘的边车：JSON 里记的图片是不是真的在磁盘上。
//
// 这是「有 json、没有图片」这类问题的照妖镜。以前只有用户自己翻资源管理器才会
// 发现，而且日志里没有任何一行能解释为什么 —— 现在刮削结束就会把期望路径与
// 缺失文件直接写进日志，Windows 上不必再靠猜。
func (a *App) verifyPersisted(plan scrapePlan, actresses []string) {
	byActressDir := plan.actresses()
	for _, actress := range actresses {
		candidate, ok := byActressDir[actress]
		if !ok {
			continue
		}
		dir := candidate.ActressDir
		summary, err := library.ReadSummary(library.SidecarPath(dir, actress))
		if err != nil {
			a.appendLog(fmt.Sprintf("无法复核边车 %s：%v", library.SidecarPath(dir, actress), err))
			continue
		}
		for _, video := range summary.Videos {
			files := video.ArtFiles()
			if len(files) == 0 {
				a.appendLog(fmt.Sprintf(
					"警告：%s/%s 的边车记录里没有任何图片字段，扫描页会显示缺图（图片目录应为 %s）",
					actress, video.Stem, library.ArtDir(dir, video.Stem)))
				continue
			}
			if missing := library.MissingArtFiles(dir, video); len(missing) > 0 {
				a.appendLog(fmt.Sprintf(
					"警告：%s/%s 有 %d 个图片文件不在磁盘上（%s），扫描页会显示缺图",
					actress, video.Stem, len(missing), strings.Join(missing, "、")))
			}
		}
	}
}

func fatalMessage(result scraper.Result) string {
	if result.FatalReason == "" {
		return ""
	}
	message := scraper.ReasonText(result.FatalReason)
	if result.FatalDetail != "" {
		message += "：" + result.FatalDetail
	}
	return message
}

func (a *App) cfgAt(libraryID string) (config.LibraryRef, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg.Find(libraryID)
}

func (a *App) playerPath() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg.PlayerPath
}

func (a *App) emit(event string, payload any) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, event, payload)
}

func (a *App) appendLog(line string) {
	a.logMu.Lock()
	defer a.logMu.Unlock()

	if len(a.logRing) >= logRingSize {
		// 环形覆盖。保留最近的行就够定位问题了，不必无限增长。
		copy(a.logRing, a.logRing[1:])
		a.logRing = a.logRing[:logRingSize-1]
	}
	a.logRing = append(a.logRing, line)
}
