package scraper

import "strings"

// 站点的「可选集合」与「站点链拼装」都在这个文件里。
//
// 站点名是 Python 侧 `--site` 的取值，只有这套名字有效；Go 既不解析页面也不
// 关心站点怎么实现，只负责把设置页里的两个选项译成一条链。把这份名单放在
// scraper 包而不是 config 包，是因为它属于「怎么调刮削器」，不属于「怎么存
// 配置」。

const (
	// DefaultSiteOrder 是默认优先站点。
	//
	// javlibrary 收录最全但有 Cloudflare，每条 job 都要过一遍验证码等待；
	// javranking 没有反爬、直接读 JSON 索引，收录少一些。默认先走前者。
	DefaultSiteOrder = "javlibrary"

	// DefaultSite 是默认站点链，按顺序回退。
	//
	// 刻意保留字面量而不是 Chain(...)：这条链是给人读的说明书，改站点集合时
	// 应该先看到它。sites_test.go 里有断言保证它与 Chain 的默认结果一致，
	// 所以两者不会悄悄分叉。
	DefaultSite = "javlibrary,javranking"
)

// SiteOption 是一个可选的刮削站点。
type SiteOption struct {
	// Name 是传给 Python `--site` 的标识，也是配置里存的值。
	Name string
	// Label 是设置页显示的名字。
	Label string
	// Note 用一句话说明取舍，设置页靠它解释「为什么值得选这个」。
	Note string
	// NeedsBrowser 表示该站点要靠浏览器会话取页面，也就是必须有 Chrome。
	//
	// 这条信息只在「没装 Chrome 该不该拦下任务」这一个判断里用到，但必须由
	// 后端给出：站点实现细节属于 Python，前端猜不出来。
	NeedsBrowser bool
}

// SiteOptions 是全部可选站点，**声明顺序即默认优先级**。
var SiteOptions = []SiteOption{
	{
		Name:         "javlibrary",
		Label:        "JavLibrary",
		Note:         "收录最全，但每条 job 都要过 Cloudflare 验证",
		NeedsBrowser: true,
	},
	{
		Name:  "javranking",
		Label: "JavRanking",
		Note:  "无反爬、直接读索引，速度快但收录较少",
	},
}

// IsKnownSite 判断 name 是否是受支持的站点。
func IsKnownSite(name string) bool {
	for _, opt := range SiteOptions {
		if opt.Name == name {
			return true
		}
	}
	return false
}

// SiteNames 返回全部站点名（逗号分隔），用于错误信息。
func SiteNames() string {
	names := make([]string, 0, len(SiteOptions))
	for _, opt := range SiteOptions {
		names = append(names, opt.Name)
	}
	return strings.Join(names, "、")
}

// Chain 把「优先站点 + 失败是否换站」拼成 Python 需要的 `--site` 值。
//
// order 不认识时按默认优先级处理：配置是手写的，多打一个字母不该表现成整批
// 任务报「未知站点」、一条也刮不下来。
func Chain(order string, fallback bool) string {
	if !IsKnownSite(order) {
		order = DefaultSiteOrder
	}
	if !fallback {
		// 只用这一个站点。
		return order
	}
	parts := []string{order}
	for _, opt := range SiteOptions {
		if opt.Name != order {
			parts = append(parts, opt.Name)
		}
	}
	return strings.Join(parts, ",")
}

// ChainNeedsBrowser 判断这条站点链是否必须有 Chrome。
//
// 与 Python 侧的前置校验同一个判据（链上任一站需要浏览器就要 Chrome）：只写
// javranking 时没装浏览器也能刮，UI 不该再拿「缺少 Chrome」拦人。不认识的站点
// 名按「需要浏览器」处理 —— 宁可多拦一次，也不要放一个注定失败的进程出去。
func ChainNeedsBrowser(chain string) bool {
	for _, name := range strings.Split(chain, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		needs := true
		for _, opt := range SiteOptions {
			if opt.Name == name {
				needs = opt.NeedsBrowser
				break
			}
		}
		if needs {
			return true
		}
	}
	// 空链不会发生（配置经 Normalize 兜底），但空链等同于「什么都不会跑」，
	// 没有理由因此拦住用户。
	return false
}
