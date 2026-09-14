package scraper

import "fmt"

// ProtocolVersion 是 NDJSON 协议版本，必须与 Python 侧 protocol.PROTOCOL_VERSION 一致。
//
// 主版本不匹配时直接拒绝运行，而不是尝试兼容：半兼容的协议只会产生难以归因的
// 诡异行为，而重新安装刮削组件只是一次操作。
const ProtocolVersion = 1

// 事件类型。
const (
	EventProgress   = "progress"
	EventItemDone   = "item_done"
	EventItemFailed = "item_failed"
	EventDone       = "done"
	EventFatal      = "fatal"
)

// 失败原因，与 Python 侧 protocol.REASONS 一一对应。
const (
	ReasonNotFound       = "not_found"
	ReasonParseFailed    = "parse_failed"
	ReasonDownloadFailed = "download_failed"
	ReasonTimeout        = "timeout"
	ReasonCaptchaFailed  = "captcha_failed"
	ReasonChromeMissing  = "chrome_missing"
	ReasonDriverFailed   = "driver_failed"
	ReasonInternalError  = "internal_error"
)

// Event 是 Python 侧 NDJSON 事件流的一行。
//
// 所有事件类型共用一个结构体而不是各自一个类型：事件是流式到达的，先解析出
// Type 再二次解码会让代码多一层且没有收益，字段少且互不冲突。
type Event struct {
	V       int       `json:"v"`
	Type    string    `json:"type"`
	Fanha   string    `json:"fanha"`
	Stage   string    `json:"stage"`
	Percent float64   `json:"percent"`
	Data    *ItemData `json:"data"`
	Reason  string    `json:"reason"`
	Detail  string    `json:"detail"`
	Summary *Summary  `json:"summary"`
}

// ItemData 是 item_done 的载荷。
//
// 这里只有文件名（CoverFile / ShotFiles），没有路径：Python 只报告它实际写出
// 了哪些文件，目录由 Go 侧拼接，命名规则因此只有一处定义。
//
// ShotFiles 是下载成功的数量，TotalShots 是站点给出的总数，两者不等表示部分
// 截图失败 —— 单条仍然算成功，但 UI 应该显示「8/9 张」。
type ItemData struct {
	Title         string   `json:"title"`
	ReleaseDate   string   `json:"release_date"`
	SiteLengthMin *int     `json:"site_length_min"`
	Genres        []string `json:"genres"`
	Cast          []string `json:"cast"`
	CoverFile     string   `json:"cover_file"`
	ShotFiles     []string `json:"shot_files"`
	TotalShots    int      `json:"total_shots"`
}

// Summary 是 done 事件的计数。
type Summary struct {
	OK      int `json:"ok"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

// Info 是 version 子命令的输出。
type Info struct {
	V        int    `json:"v"`
	Type     string `json:"type"`
	Scraper  string `json:"scraper"`
	Python   string `json:"python"`
	Platform string `json:"platform"`
	Frozen   bool   `json:"frozen"`
}

// ChromeInfo 是 doctor 报告的 Chrome 探测结果。
type ChromeInfo struct {
	Found bool   `json:"found"`
	Path  string `json:"path"`
}

// DriverInfo 是 doctor 报告的驱动状态。
//
// OnDemand 表示驱动文件尚未就绪，但 seleniumbase 会在需要时自行下载并落地到 Dir。
// Dir 已被重定向到用户数据目录（见 python/src/scraper/driver_dir.py），与安装
// 产物解耦 —— 覆盖安装不会丢驱动，装在受保护位置也不会因不可写而失败。
//
// Dir 是**实际生效**的目录（读 seleniumbase 的 NEW_DRIVER_DIR），不是我们的意图；
// Writable 表示它是否可写。这两个字段是「重定向没生效」或「目录不可写」的直接线索。
type DriverInfo struct {
	Ready    bool   `json:"ready"`
	Source   string `json:"source"`
	OnDemand bool   `json:"on_demand"`
	Dir      string `json:"dir"`
	Writable bool   `json:"writable"`
}

// Health 是 doctor 子命令的输出。
//
// Chrome / Driver 用命名类型而不是内联匿名结构体：Wails 的 TS 绑定生成器解析
// 不了匿名嵌套结构体，每次构建都会打印 "Not found: struct { ... }" ——
// 那种噪音会让人习惯性忽略构建输出。
type Health struct {
	V            int        `json:"v"`
	Type         string     `json:"type"`
	Scraper      string     `json:"scraper"`
	Python       string     `json:"python"`
	Platform     string     `json:"platform"`
	TempWritable bool       `json:"temp_writable"`
	Chrome       ChromeInfo `json:"chrome"`
	Driver       DriverInfo `json:"driver"`
}

// Healthy 判断环境是否可用。
//
// 这里刻意允许「驱动未就绪但仍然放行」：现有的 Windows 用法里驱动是常驻的，
// 而在全新环境里 seleniumbase 是否能自行补齐取决于它的内部判断，Go 侧无从得知。
// 直接拦下来会让本来能跑的环境用不了，所以放行并把判断交给 UI —— 由它把
// Driver.Ready / Dir / Writable 明确展示出来。真出问题时日志里也能看到原因。
func (h Health) Healthy() bool {
	return h.Chrome.Found && (h.Driver.Ready || h.Driver.OnDemand) && h.TempWritable
}

// Retryable 判断某个失败原因是否值得重试。
//
// not_found（站点没收录）重试一百次结果一样；chrome_missing / driver_failed
// 必须引导用户装软件，重试同样没有意义。UI 靠这个判断决定「重试失败项」
// 按钮是否可用，否则用户只会对着必然失败的按钮白等。
func Retryable(reason string) bool {
	switch reason {
	case ReasonDownloadFailed, ReasonTimeout, ReasonCaptchaFailed:
		return true
	default:
		return false
	}
}

// ReasonText 给出面向用户的中文说明。
func ReasonText(reason string) string {
	switch reason {
	case ReasonNotFound:
		return "站点未收录此番号"
	case ReasonParseFailed:
		return "页面结构解析失败，站点可能已改版"
	case ReasonDownloadFailed:
		return "图片下载失败"
	case ReasonTimeout:
		return "页面加载超时"
	case ReasonCaptchaFailed:
		return "验证码未通过"
	case ReasonChromeMissing:
		return "未找到 Chrome 浏览器"
	case ReasonDriverFailed:
		return "浏览器驱动不可用"
	case ReasonInternalError:
		return "刮削器内部错误"
	default:
		return fmt.Sprintf("未知失败原因：%s", reason)
	}
}
