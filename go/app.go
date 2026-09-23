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

	scrapeMu     sync.Mutex
	scrapeCancel context.CancelFunc
	scraping     bool

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
		dto.VideoURL = media.VideoURL(libraryID, row.VideoRel)
	}
	return dto
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
			Actress:    candidate.Actress,
			Fanha:      candidate.Fanha,
			Stem:       candidate.Stem,
			VideoFile:  candidate.VideoFile,
			FileSize:   candidate.FileSize,
			Scraped:    candidate.Scraped,
			MissingArt: candidate.MissingArt,
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
func (a *App) ScraperHealth() (scraper.Health, error) {
	a.rebuildRunner()
	if a.runner.ExePath == "" {
		return scraper.Health{}, errors.New(
			"未找到刮削器组件。请先运行 tools/build.sh 完成打包，" +
				"或在配置中指定 scraper_path")
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
	return health, nil
}

// ── 内部实现 ──────────────────────────────────────────────────────────────

type scrapePlan struct {
	ref     config.LibraryRef
	jobs    []scraper.Job
	byFanha map[string]library.Candidate
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
		byFanha: make(map[string]library.Candidate, len(stems)),
	}
	for _, stem := range stems {
		candidate, ok := byStem[stem]
		if !ok {
			// 用户界面上选中的条目在扫描之后被移动或改名了。跳过而不是报错：
			// 一个条目失效不该让整批刮削无法开始。
			continue
		}
		plan.byFanha[candidate.Fanha] = candidate
		plan.jobs = append(plan.jobs, scraper.Job{
			Fanha: candidate.Fanha,
			// 输出目录由 Go 计算，Python 因此不需要知道库的目录布局。
			Out:   library.ArtDir(candidate.ActressDir, candidate.Stem),
			Force: force,
		})
	}
	if len(plan.jobs) == 0 {
		return scrapePlan{}, errors.New("选中的条目已不存在，请重新扫描")
	}
	return plan, nil
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
	)

	callbacks := scraper.Callbacks{
		Progress: func(fanha, stage string, percent float64) {
			a.emit(eventScrapeProgress, map[string]any{
				"fanha": fanha, "stage": stage, "percent": percent,
			})
		},
		ItemDone: func(fanha string, data scraper.ItemData) {
			candidate, ok := plan.byFanha[fanha]
			if !ok {
				return
			}
			video := buildVideoRecord(candidate, data)
			collectedMu.Lock()
			collected[candidate.Actress] = append(collected[candidate.Actress], video)
			collectedMu.Unlock()

			a.emit(eventScrapeItemDone, map[string]any{
				"fanha": fanha, "title": data.Title,
				"shots": len(data.ShotFiles), "totalShots": data.TotalShots,
			})
		},
		ItemFailed: func(fanha, reason, detail string) {
			a.emit(eventScrapeItemFailed, map[string]any{
				"fanha": fanha, "reason": reason,
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
	byActressDir := make(map[string]library.Candidate)
	for _, candidate := range plan.byFanha {
		byActressDir[candidate.Actress] = candidate
	}

	var (
		merged  []string
		failure error
	)
	for actress, videos := range collected {
		candidate, ok := byActressDir[actress]
		if !ok {
			continue
		}
		sidecarPath := library.SidecarPath(candidate.ActressDir, actress)
		if _, err := library.MergeVideos(sidecarPath, actress, videos); err != nil {
			failure = errors.Join(failure, fmt.Errorf("写入 %s: %w", actress, err))
			continue
		}
		merged = append(merged, actress)
	}
	return merged, failure
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
