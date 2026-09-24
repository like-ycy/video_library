package version

import "strings"

// Version 是当前软件的编译版本号。
//
// 本地/未注入时回落到测试版本；正式发布由 CI 通过
// -ldflags "-X videolib/internal/version.Version=x.y.z" 写入 git tag。
var Version = "0.1.0"

// GetVersion 返回展示用版本号（带 v 前缀）。
func GetVersion() string {
	v := strings.TrimSpace(Version)
	if v == "" {
		v = "0.1.0"
	}
	if !strings.HasPrefix(v, "v") {
		return "v" + v
	}
	return v
}
