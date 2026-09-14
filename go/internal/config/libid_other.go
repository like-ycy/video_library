//go:build !windows

package config

// volumeID 在非 Windows 平台没有对应概念，始终返回失败，
// 由 LibraryID 退化为路径 hash。
func volumeID(string) (string, bool) {
	return "", false
}
