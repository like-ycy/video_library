package main

import (
	"errors"
	"fmt"
	goRuntime "runtime"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"videolib/internal/appearance"
	"videolib/internal/component"
	"videolib/internal/config"
	"videolib/internal/index"
	"videolib/internal/scraper"
	"videolib/internal/version"
)

// ConfigDTO 是给前端的可编辑配置（不含 libraries，库单独管理）。
type ConfigDTO struct {
	Concurrency        int    `json:"concurrency"`
	ScrapeTimeoutMin   int    `json:"scrapeTimeoutMin"`
	ScraperPath        string `json:"scraperPath"`
	FFprobePath        string `json:"ffprobePath"`
	PlayerPath         string `json:"playerPath"`
	Theme              string `json:"theme"`
	ComponentsOnDemand bool   `json:"componentsOnDemand"`
	ComponentMirror    string `json:"componentMirror"`
	// SiteOrder 是优先站点，SiteFallback 决定失败时是否换另一个站点。
	// 前端只认这两个字段，站点链由后端拼 —— 见 scraper.Chain。
	SiteOrder    string `json:"siteOrder"`
	SiteFallback bool   `json:"siteFallback"`
}

// PathsDTO 展示配置与数据目录位置。
type PathsDTO struct {
	ConfigDir  string `json:"configDir"`
	ConfigFile string `json:"configFile"`
	AppData    string `json:"appData"`
}

func (a *App) dtoFromConfig(cfg config.Config) ConfigDTO {
	return ConfigDTO{
		Concurrency:        cfg.Concurrency,
		ScrapeTimeoutMin:   cfg.ScrapeTimeoutMin,
		ScraperPath:        cfg.ScraperPath,
		FFprobePath:        cfg.FFprobePath,
		PlayerPath:         cfg.PlayerPath,
		Theme:              cfg.Theme,
		ComponentsOnDemand: cfg.ComponentsOnDemand,
		ComponentMirror:    cfg.ComponentMirror,
		SiteOrder:          cfg.SiteOrder,
		SiteFallback:       cfg.SiteFallback,
	}
}

// GetConfig 读取当前可编辑配置。
func (a *App) GetConfig() ConfigDTO {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.dtoFromConfig(a.cfg)
}

// SaveConfig 保存可编辑配置字段（不触碰 libraries）。
func (a *App) SaveConfig(dto ConfigDTO) (ConfigDTO, error) {
	// 镜像前缀会拼进下载地址。协议不对时在这里拒绝，而不是等到用户点下载：
	// 那时失败信息是「只支持 http(s)」，用户未必想到是自己填的这一栏。
	mirror := strings.TrimSpace(dto.ComponentMirror)
	if mirror != "" {
		if err := component.ValidateURL(mirror); err != nil {
			return ConfigDTO{}, fmt.Errorf("组件下载源不可用：%w", err)
		}
	}

	// 站点名同样在入口处拒绝：不认识的站点传给 Python 只会得到一条「未知站点」
	// 的致命错误，而那时用户已经按下了开始刮削。空值表示「回到默认」。
	order := strings.ToLower(strings.TrimSpace(dto.SiteOrder))
	if order == "" {
		order = scraper.DefaultSiteOrder
	}
	if !scraper.IsKnownSite(order) {
		return ConfigDTO{}, fmt.Errorf("未知站点：%s（可选：%s）", dto.SiteOrder, scraper.SiteNames())
	}

	a.mu.Lock()
	if dto.Concurrency > 0 {
		a.cfg.Concurrency = dto.Concurrency
	}
	if dto.ScrapeTimeoutMin > 0 {
		a.cfg.ScrapeTimeoutMin = dto.ScrapeTimeoutMin
	}
	a.cfg.ScraperPath = dto.ScraperPath
	a.cfg.FFprobePath = dto.FFprobePath
	a.cfg.PlayerPath = dto.PlayerPath
	a.cfg.ComponentsOnDemand = dto.ComponentsOnDemand
	a.cfg.ComponentMirror = mirror
	a.cfg.SiteOrder = order
	a.cfg.SiteFallback = dto.SiteFallback
	switch dto.Theme {
	case "system", "dark", "light":
		a.cfg.Theme = dto.Theme
	}
	cfg := a.cfg
	a.mu.Unlock()

	if err := cfg.Save(); err != nil {
		return ConfigDTO{}, fmt.Errorf("保存配置: %w", err)
	}

	a.mu.Lock()
	a.resolveFFprobe()
	a.mu.Unlock()
	a.rebuildRunner()
	// 换路径/开关之后设置页显示的是旧结果，缓存必须失效。
	a.invalidateHealth()

	return a.dtoFromConfig(cfg), nil
}

// ScrapeNeedsBrowser 报告当前站点链是否必须有 Chrome。
//
// 前端在开始刮削前会查一次硬件环境并要求装 Chrome。那条检查原本是硬编码的
// 结论，现在站点可选，它必须先问清楚「这条链到底要不要浏览器」—— 只用
// javranking 的用户没装 Chrome 也应该能刮。
func (a *App) ScrapeNeedsBrowser() bool {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	return scraper.ChainNeedsBrowser(scraper.Chain(cfg.SiteOrder, cfg.SiteFallback))
}

// GetAppVersion 返回当前软件版本号。
// 本地默认测试版本；CI 打 tag 时经 -ldflags 注入。
func (a *App) GetAppVersion() string {
	return strings.TrimSpace(version.Version)
}

// GetSystemAppearance 返回操作系统当前外观。
// macOS 读原生 AppleInterfaceStyle；其他平台返回空字符串，由前端用 prefers-color-scheme。
func (a *App) GetSystemAppearance() string {
	return appearance.Get()
}

// Paths 返回配置目录信息，供设置页展示。
func (a *App) Paths() (PathsDTO, error) {
	dir, err := config.Dir()
	if err != nil {
		return PathsDTO{}, err
	}
	file, err := config.Path()
	if err != nil {
		return PathsDTO{}, err
	}
	return PathsDTO{
		ConfigDir:  dir,
		ConfigFile: file,
		AppData:    dir,
	}, nil
}

// PickExecutable 弹出文件选择对话框，返回可执行文件路径。
func (a *App) PickExecutable(title string) (string, error) {
	if a.ctx == nil {
		return "", errors.New("应用尚未就绪")
	}
	if title == "" {
		title = "选择可执行文件"
	}
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: title,
	})
}

// EnvReport 是环境诊断结果。
type EnvReport struct {
	GoVersion      string `json:"goVersion"`
	Platform       string `json:"platform"`
	FFprobePath    string `json:"ffprobePath"`
	FFprobeOK      bool   `json:"ffprobeOk"`
	ScraperExePath string `json:"scraperExePath"`
	ScraperExeOK   bool   `json:"scraperExeOk"`
	// ScraperSource 说明这份可执行文件是从哪来的（config/component/bundled/dev/path），
	// 「装了却不生效」时这是第一个要看的信息。
	ScraperSource string          `json:"scraperSource"`
	ScraperHealth *scraper.Health `json:"scraperHealth,omitempty"`
	ScraperError  string          `json:"scraperError,omitempty"`
	PlayerPath    string          `json:"playerPath"`
	ConfigDir     string          `json:"configDir"`
	CheckedAt     string          `json:"checkedAt"`
	TempWritable  bool            `json:"tempWritable"`
}

// DiagnoseEnv 运行环境自检。
//
// force 透传给 ScraperHealth，语义一致：主动刷新跳过缓存。
func (a *App) DiagnoseEnv(force bool) EnvReport {
	report := EnvReport{
		GoVersion:    goRuntime.Version(),
		Platform:     goRuntime.GOOS + "/" + goRuntime.GOARCH,
		CheckedAt:    time.Now().Format(time.RFC3339),
		PlayerPath:   a.playerPath(),
		TempWritable: true,
	}
	report.ScraperExePath, report.ScraperSource = a.scraperSource()
	if dir, err := config.Dir(); err == nil {
		report.ConfigDir = dir
	}

	// 每次诊断都重新解析：PATH 可能是 App 启动之后才改的，
	// 「重新检查」按钮若只读上次启动的结果，会把新改好的 PATH 报成未找到。
	a.mu.Lock()
	ffprobe := a.resolveFFprobe()
	a.mu.Unlock()
	report.FFprobePath = ffprobe
	report.FFprobeOK = ffprobe != ""

	report.ScraperExeOK = report.ScraperExePath != ""
	if report.ScraperExeOK {
		health, err := a.ScraperHealth(force)
		if err != nil {
			report.ScraperError = err.Error()
			if health.Scraper != "" {
				h := health
				report.ScraperHealth = &h
			}
		} else {
			h := health
			report.ScraperHealth = &h
			report.TempWritable = health.TempWritable
		}
	}
	return report
}

// ── 观影记录 ──────────────────────────────────────────────────────────────

// ListContinueWatching 返回未看完的视频。
func (a *App) ListContinueWatching(libraryID string) ([]videoDTO, error) {
	store, err := a.store(libraryID)
	if err != nil {
		return nil, err
	}
	rows, err := store.ListContinue(a.ctx, libraryID, 80)
	if err != nil {
		return nil, err
	}
	return a.decorateAll(libraryID, rows), nil
}

// ListRecentPlays 返回最近播放。
func (a *App) ListRecentPlays(libraryID string) ([]videoDTO, error) {
	store, err := a.store(libraryID)
	if err != nil {
		return nil, err
	}
	rows, err := store.ListRecent(a.ctx, libraryID, 80)
	if err != nil {
		return nil, err
	}
	return a.decorateAll(libraryID, rows), nil
}

func (a *App) decorateAll(libraryID string, rows []index.Row) []videoDTO {
	items := make([]videoDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, a.decorate(libraryID, row))
	}
	return items
}

// ClearProgress 清除单条播放进度。
func (a *App) ClearProgress(libraryID string, videoID int64) error {
	store, err := a.store(libraryID)
	if err != nil {
		return err
	}
	row, err := store.GetVideo(a.ctx, libraryID, videoID)
	if err != nil {
		return err
	}
	return store.ClearProgress(a.ctx, libraryID, row.Actress, row.Fanha)
}

// ClearRecentHistory 清空最近播放记录。
func (a *App) ClearRecentHistory(libraryID string) error {
	store, err := a.store(libraryID)
	if err != nil {
		return err
	}
	return store.ClearRecent(a.ctx, libraryID)
}
