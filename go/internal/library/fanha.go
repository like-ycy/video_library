package library

import (
	"regexp"
	"strings"
)

// fanhaPattern 匹配规范番号前缀。
//
// 厂牌缩写允许含数字："1pondo"、"1sdde" 这类是真实存在的番号形态，
// 用 [a-z]+ 会把它们全部漏掉。
//
// 刻意只匹配前缀，后面的修饰一律忽略：-c（字幕版）、-4k、-1（分段）等共享同一份
// 元数据，必须归一到同一个番号 —— 否则同一部作品会被当成两个番号分别刮削、
// 分别入库。
var fanhaPattern = regexp.MustCompile(`^([a-z0-9]+)-(\d+)`)

// NormalizeFanha 把视频文件名主干归一为匹配用番号。
//
// 反例警告：不要写成 strings.ReplaceAll(stem, "-c", "")。
// 那不是一个归一规则，而是一次字符串替换：它只认 -c 这一种修饰，
// 而 -4k、-1、-hd 会原样留在番号里，导致同一部作品产生多个不同的匹配键。
//
// canonical 用于查重与站点匹配；原始 stem 必须另外保留，用于定位磁盘文件。
// 两者混用是一系列路径拼接问题的根源，所以本函数只返回 canonical，不改动 stem。
func NormalizeFanha(stem string) (canonical string, ok bool) {
	matches := fanhaPattern.FindStringSubmatch(strings.ToLower(strings.TrimSpace(stem)))
	if matches == nil {
		return "", false
	}
	return matches[1] + "-" + matches[2], true
}
