package main

import (
	"errors"
	"fmt"
	goRuntime "runtime"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"videolib/internal/appearance"
	"videolib/internal/config"
	"videolib/internal/index"
	"videolib/internal/scraper"
)

// ConfigDTO 是给前端的可编辑配置（不含 libraries，库单独管理）。
type ConfigDTO struct {
	Concurrency      int    `json:"concurrency"`
	ScrapeTimeoutMin int    `json:"scrapeTimeoutMin"`
	ScraperPath      string `json:"scraperPath"`
	FFprobePath      string `json:"ffprobePath"`
	PlayerPath       string `json:"playerPath"`
	Theme            string `json:"theme"`
}

// PathsDTO 展示配置与数据目录位置。
type PathsDTO struct {
	ConfigDir  string `json:"configDir"`
	ConfigFile string `json:"configFile"`
	AppData    string `json:"appData"`
}

func (a *App) dtoFromConfig(cfg config.Config) ConfigDTO {
	return ConfigDTO{
		Concurrency:      cfg.Concurrency,
		ScrapeTimeoutMin: cfg.ScrapeTimeoutMin,
		ScraperPath:      cfg.ScraperPath,
		FFprobePath:      cfg.FFprobePath,
		PlayerPath:       cfg.PlayerPath,
		Theme:            cfg.Theme,
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

	return a.dtoFromConfig(cfg), nil
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
	GoVersion      string          `json:"goVersion"`
	Platform       string          `json:"platform"`
	FFprobePath    string          `json:"ffprobePath"`
	FFprobeOK      bool            `json:"ffprobeOk"`
	ScraperExePath string          `json:"scraperExePath"`
	ScraperExeOK   bool            `json:"scraperExeOk"`
	ScraperHealth  *scraper.Health `json:"scraperHealth,omitempty"`
	ScraperError   string          `json:"scraperError,omitempty"`
	PlayerPath     string          `json:"playerPath"`
	ConfigDir      string          `json:"configDir"`
	CheckedAt      string          `json:"checkedAt"`
	TempWritable   bool            `json:"tempWritable"`
}

// DiagnoseEnv 运行环境自检。
//
// force 透传给 ScraperHealth，语义一致：主动刷新跳过缓存。
func (a *App) DiagnoseEnv(force bool) EnvReport {
	report := EnvReport{
		GoVersion:      goRuntime.Version(),
		Platform:       goRuntime.GOOS + "/" + goRuntime.GOARCH,
		CheckedAt:      time.Now().Format(time.RFC3339),
		ScraperExePath: a.resolveScraperPath(),
		PlayerPath:     a.playerPath(),
		TempWritable:   true,
	}
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
