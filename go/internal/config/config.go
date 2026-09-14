// Package config 管理应用配置的读写。
//
// 配置放在用户配置目录（%APPDATA%\videolib 等）而不是移动硬盘上：
// 硬盘可能只读挂载、换盘符、热拔，把一个需要频繁写入的文件放在上面
// 只是在给自己找故障。
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	appDirName = "videolib"
	fileName   = "config.json"

	// MinConcurrency / MaxConcurrency 限制同时运行的刮削任务数。
	//
	// 每个任务会拉起一个独立的 Chrome 实例，所以上限压得很低：
	// 并发 8 已经意味着 8 个浏览器同时跑，再多只会让验证码失败率上升。
	MinConcurrency = 1
	MaxConcurrency = 8
)

// LibraryRef 指向一个视频库根目录。
type LibraryRef struct {
	// ID 是稳定标识，用于定位该库的索引数据库。见 LibraryID。
	ID string `json:"id"`
	// Root 是库根目录的绝对路径。
	Root string `json:"root"`
}

// Config 是应用配置。
type Config struct {
	Libraries        []LibraryRef `json:"libraries"`
	Concurrency      int          `json:"concurrency"`
	ScrapeTimeoutMin int          `json:"scrape_timeout_min"`
	ScraperPath      string       `json:"scraper_path"`
	FFprobePath      string       `json:"ffprobe_path"`
	PlayerPath       string       `json:"player_path"`
	Theme            string       `json:"theme"`
}

// Defaults 返回带默认值的配置。
func Defaults() Config {
	return Config{
		Concurrency:      2,
		ScrapeTimeoutMin: 30,
		Theme:            "dark",
	}
}

// Dir 返回应用配置目录，不存在则创建。
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("定位用户配置目录: %w", err)
	}
	dir := filepath.Join(base, appDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建配置目录 %s: %w", dir, err)
	}
	return dir, nil
}

// Path 返回配置文件路径。
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

// DBPath 返回某个视频库的索引数据库路径。
func DBPath(libraryID string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "library-"+libraryID+".db"), nil
}

// Load 读取配置。文件不存在时返回默认配置。
func Load() (Config, error) {
	cfg := Defaults()

	path, err := Path()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return cfg, nil
	case err != nil:
		return cfg, fmt.Errorf("读取配置 %s: %w", path, err)
	}

	// 从默认值出发再覆盖：升级后新增的字段自然取到默认值，而不是零值。
	// 反过来（解到零值再补默认）会把「用户显式设成 0」和「字段不存在」混为一谈。
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("解析配置 %s: %w", path, err)
	}
	cfg.Normalize()
	return cfg, nil
}

// Save 原子写入配置。
func (c Config) Save() error {
	path, err := Path()
	if err != nil {
		return err
	}
	c.Normalize()

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置: %w", err)
	}
	data = append(data, '\n')

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("写入 %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("替换 %s: %w", path, err)
	}
	return nil
}

// Normalize 修正越界与空值，并在库缺少 ID 时补上。
func (c *Config) Normalize() {
	if c.Concurrency < MinConcurrency {
		c.Concurrency = MinConcurrency
	}
	if c.Concurrency > MaxConcurrency {
		c.Concurrency = MaxConcurrency
	}
	if c.ScrapeTimeoutMin <= 0 {
		c.ScrapeTimeoutMin = Defaults().ScrapeTimeoutMin
	}
	if c.Theme == "" {
		c.Theme = Defaults().Theme
	}

	seen := make(map[string]bool, len(c.Libraries))
	kept := c.Libraries[:0]
	for _, ref := range c.Libraries {
		root := filepath.Clean(strings.TrimSpace(ref.Root))
		if root == "" || root == "." {
			continue
		}
		if ref.ID == "" {
			ref.ID = LibraryID(root)
		}
		ref.Root = root
		if seen[ref.ID] {
			continue
		}
		seen[ref.ID] = true
		kept = append(kept, ref)
	}
	c.Libraries = kept
}

// LibraryID 为一个库根目录计算稳定标识。
//
// 优先用卷序列号：移动硬盘换盘符（E: → F:）时路径会变而卷序列号不变，
// 索引和用户数据（收藏、播放进度）才能继续对应上。拿不到卷信息时退化为
// 路径 hash —— 这种情况下换盘符会重新建索引，代价是一次扫描而不是数据丢失。
func LibraryID(root string) string {
	clean := filepath.Clean(root)
	if id, ok := volumeID(clean); ok {
		return id
	}
	sum := sha256.Sum256([]byte(clean))
	return "path-" + hex.EncodeToString(sum[:6])
}

// Find 按 ID 取库引用。
func (c Config) Find(libraryID string) (LibraryRef, bool) {
	for _, ref := range c.Libraries {
		if ref.ID == libraryID {
			return ref, true
		}
	}
	return LibraryRef{}, false
}
