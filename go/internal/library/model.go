// Package library 是视频库文件布局的唯一权威。
//
// 它只认识文件系统 —— 不认识 SQLite，也不认识 Wails。索引层的导入、媒体层的
// 路径解析都从这里取真相，因此「目录长什么样」这件事在整个程序里只有一处定义，
// 也不会在两种语言里各自漂移。
//
// 目录布局（相对库根 root）：
//
//	<root>/<演员>/<视频文件名>.mp4
//	<root>/<演员>/meta/<演员>.json            汇总元数据（边车 JSON）
//	<root>/<演员>/meta/<stem>/cover.jpg       封面
//	<root>/<演员>/meta/<stem>/images/N.jpg    截图
//
// <stem> 是视频文件名去掉扩展名的原始形式（保留大小写），用于定位磁盘文件；
// 它不是规范化番号，两者的区别见 fanha.go。
package library

import (
	"path"
	"path/filepath"
	"strings"
)

const (
	// MetaDirName 是存放全部刮削产物的子目录名。
	MetaDirName = "meta"

	// SidecarSchema 是边车 JSON 的 schema 版本。
	//
	// Go 是边车 JSON 的唯一写入方，所以这个字段的用途是「将来做迁移」，
	// 而不是「兼容另一个版本的程序并发写入」。
	SidecarSchema = 2
)

// VideoExtensions 是扫描时会被视为视频的扩展名。
//
// 做成变量而不是硬编码 ".mp4"，是为了让「新增容器格式」只改这一处。
// 当前实际使用的只有 .mp4。
var VideoExtensions = []string{".mp4", ".mkv", ".wmv"}

// Video 是边车 JSON 中的一条记录，字段与 docs/architecture.md §7.2 对齐。
type Video struct {
	Fanha         string   `json:"fanha"`
	Stem          string   `json:"stem"`
	Title         string   `json:"title"`
	ReleaseDate   string   `json:"release_date"`
	SiteLengthMin *int     `json:"site_length_min,omitempty"`
	Genres        []string `json:"genres"`
	Cast          []string `json:"cast"`
	Cover         string   `json:"cover"`
	Screenshots   []string `json:"screenshots"`
	VideoFile     string   `json:"video_file"`
	FileSize      int64    `json:"file_size"`
	ScrapedAt     string   `json:"scraped_at"`
}

// ActressSummary 是 <演员目录>/meta/<演员>.json 的内容。
type ActressSummary struct {
	Schema  int     `json:"schema"`
	Actress string  `json:"actress"`
	Videos  []Video `json:"videos"`
}

// ArtFiles 返回边车里记录的图片相对路径：封面在前，截图在后。
//
// 顺序固定是为了让「缺哪个文件」的提示稳定可读。返回空切片表示这条记录里
// 一个图片字段都没有 —— 那不是「图都在」，而是「压根没有图」，调用方必须
// 用 len() 区分这两种情况。
func (v Video) ArtFiles() []string {
	files := make([]string, 0, len(v.Screenshots)+1)
	if v.Cover != "" {
		files = append(files, v.Cover)
	}
	return append(files, v.Screenshots...)
}

// Candidate 是扫描得到的一个待处理视频。
type Candidate struct {
	Actress    string
	Fanha      string
	Stem       string
	ActressDir string
	VideoPath  string // 绝对路径
	VideoRel   string // 相对库根，存进数据库的只有这个
	VideoFile  string
	FileSize   int64
	// FileMtime 是视频文件的修改时间（Unix 秒）。
	//
	// 索引层用它判断「是否需要重新跑 ffprobe」：文件没变就不必再花几百毫秒
	// 起一个外部进程。全库几千条时这个判断决定了导入是几秒还是几分钟。
	FileMtime int64

	// Scraped 表示边车 JSON 中已有该番号的记录。
	Scraped bool
	// MissingArt 表示有记录但图片不在磁盘上 —— 通常是上次刮削中断，
	// 或被单独删掉了图片目录。UI 应把这类条目标为「需补图」。
	//
	// 除了「记录里的文件缺了」，还有一种情况同样为 true：记录里一个图片字段
	// 都没有（边车来自别的工具、或从没刮到过图）。只查「记录里的文件在不在」
	// 会把后者判成齐全。
	MissingArt bool
	// ArtDir 是该视频图片目录的绝对路径（<演员目录>/meta/<stem>）。
	//
	// 只给 UI 展示用：把「缺图」从一句结论变成「程序在找哪个目录」，
	// 用户才能自己判断是路径不对还是文件真没下下来。
	ArtDir string
	// MissingFiles 是边车里记录了、但磁盘上不存在的图片（相对演员目录，正斜杠）。
	// 为空既可能是齐全，也可能是记录里根本没记图片 —— 用 ArtFiles() 区分。
	MissingFiles []string
}

// ── 路径计算 ──────────────────────────────────────────────────────────────
//
// 这里同时用到 filepath 与 path，两者不可互换：
//   filepath → 操作系统真实路径
//   path     → 写进 JSON 的相对路径，必须用正斜杠才能跨平台稳定

// ActressDir 返回演员目录的绝对路径。
func ActressDir(root, actress string) string {
	return filepath.Join(root, actress)
}

// MetaDir 返回演员目录下的 meta 目录。
func MetaDir(actressDir string) string {
	return filepath.Join(actressDir, MetaDirName)
}

// SidecarPath 返回演员汇总 JSON 的路径。
func SidecarPath(actressDir, actress string) string {
	return filepath.Join(MetaDir(actressDir), actress+".json")
}

// ArtDir 返回某个视频的图片目录。
//
// Go 把这个目录作为 job 的 out 交给 Python，因此 Python 完全不需要知道
// 视频库的目录结构。
func ArtDir(actressDir, stem string) string {
	return filepath.Join(MetaDir(actressDir), stem)
}

// ArtRelPrefix 返回图片相对**演员目录**的前缀，与边车 JSON 中
// cover / screenshots 字段的相对基准保持一致。
func ArtRelPrefix(stem string) string {
	return path.Join(MetaDirName, stem)
}

// JobID 返回一条刮削 job 的稳定标识，随 job 下发给 Python 并由事件原样回显。
//
// 用「演员目录名/文件名主干」而不是番号：同一番号可能对应多个文件
// （X.mp4 与 X-c.mp4 经 NormalizeFanha 归一后同号），而事件只能靠这个标识
// 绑回具体文件与其图片目录。同一演员目录内 stem 唯一（大小写不敏感的文件系统
// 决定），加上目录名后全局唯一。
func JobID(actress, stem string) string {
	return actress + "/" + stem
}

// IsVideoFile 判断文件名是否为受支持的视频。
//
// 扩展名比较不区分大小写：Windows 上 .MP4 与 .mp4 同样常见，
// 大小写敏感比较会把一半文件漏掉，而且症状是「扫描不到视频」，很难联想到大小写。
func IsVideoFile(name string) bool {
	ext := filepath.Ext(name)
	for _, candidate := range VideoExtensions {
		if strings.EqualFold(ext, candidate) {
			return true
		}
	}
	return false
}
