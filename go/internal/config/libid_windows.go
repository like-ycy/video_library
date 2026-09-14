//go:build windows

package config

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows"
)

// volumeID 返回 root 所在卷的序列号，形如 "vol-1A2B3C4D"。
//
// 用它而不是路径 hash：移动硬盘换盘符（E: → F:）路径会变，卷序列号不变，
// 索引与用户数据才能继续对应上。
func volumeID(root string) (string, bool) {
	// GetVolumeInformation 需要卷根（"F:\"），不是任意目录。
	volumeRoot := root
	if len(volumeRoot) >= 2 && volumeRoot[1] == ':' {
		volumeRoot = volumeRoot[:2] + `\`
	} else {
		volumeRoot = `\\?\` + strings.TrimRight(volumeRoot, `\`)
	}

	pathPtr, err := windows.UTF16PtrFromString(volumeRoot)
	if err != nil {
		return "", false
	}

	var serial uint32
	if err := windows.GetVolumeInformation(pathPtr, nil, 0, &serial, nil, nil, nil, 0); err != nil {
		return "", false
	}
	return fmt.Sprintf("vol-%08X", serial), true
}
