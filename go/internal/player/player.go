// Package player 负责用外部播放器打开视频。
//
// 存在的理由：WebView2 的 <video> 对 HEVC、10bit 等编码的支持很不可靠 ——
// 可能黑屏、可能只有声音、可能干脆不出控件。把「用外部播放器打开」做成一等
// 公民功能（而不是藏在菜单里的兜底项），用户才不会在看不了的文件上卡住。
package player

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"videolib/internal/spawn"
)

// Open 打开视频。
//
// playerPath 为空时走系统默认关联（Windows 的 start、macOS 的 open、
// Linux 的 xdg-open），此时无法指定起始位置。
func Open(playerPath, videoPath string) error {
	return OpenAt(playerPath, videoPath, 0)
}

// OpenAt 打开视频并尝试从指定位置续播。
//
// 起始位置靠播放器自身的命令行参数实现，而各家参数不统一，因此这里按可执行
// 文件名做识别。识别不出来时退化为「从头打开」——不报错，因为用户的期望是
// 能看就行，起始位置是加分项。
func OpenAt(playerPath, videoPath string, positionMs int64) error {
	if playerPath == "" {
		return openWithSystemDefault(videoPath)
	}

	args := seekArgs(playerPath, positionMs)
	args = append(args, videoPath)

	cmd := exec.Command(playerPath, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动播放器 %s: %w", playerPath, err)
	}
	// 不 Wait：播放器是长期运行的独立进程，用户应当能关掉 App 之后继续看。
	// 这里只做一次回收，避免留下僵尸进程。
	go func() { _ = cmd.Wait() }()
	return nil
}

// seekArgs 按播放器返回续播参数。
//
// 只识别确实支持命令行跳转的几个常见播放器。宁可不传参数，也不要给不认识
// 的播放器塞一个它无法解析的参数 —— 那会让它直接报错退出，比从头播放更糟。
func seekArgs(playerPath string, positionMs int64) []string {
	if positionMs <= 0 {
		return nil
	}
	name := strings.ToLower(filepath.Base(playerPath))
	seconds := positionMs / 1000

	switch {
	case strings.Contains(name, "potplayer"), strings.Contains(name, "potplayermini"):
		// PotPlayer 接受 /seek=hh:mm:ss
		return []string{fmt.Sprintf("/seek=%02d:%02d:%02d",
			seconds/3600, seconds%3600/60, seconds%60)}
	case strings.Contains(name, "mpv"), strings.Contains(name, "mpc-hc"),
		strings.Contains(name, "vlc"):
		return []string{fmt.Sprintf("--start=+%d", seconds)}
	default:
		return nil
	}
}

func openWithSystemDefault(videoPath string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// 空的窗口标题参数是必需的：start 会把第一个带引号的参数当作标题，
		// 否则含空格的路径会被当成标题，视频根本不会打开。
		cmd = exec.Command("cmd", "/c", "start", "", videoPath)
		// cmd 是控制台程序，不隐藏必闪窗口；start 起的目标本身不受影响。
		// 只对这里调 Hide：上面的播放器是 GUI 程序，SW_HIDE 会让它隐藏启动。
	case "darwin":
		cmd = exec.Command("open", videoPath)
	default:
		cmd = exec.Command("xdg-open", videoPath)
	}
	spawn.Hide(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("用系统默认程序打开 %s: %w", filepath.Base(videoPath), err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
