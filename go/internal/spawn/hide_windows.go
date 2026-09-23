//go:build windows

package spawn

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// Hide 抑制子进程的控制台窗口。只用于控制台程序，见包文档。
//
// Wails 桌面 App 是 GUI 进程、自身没有控制台。用它启动控制台程序时
// （scraper.exe 是 PyInstaller console 版，ffprobe.exe 是控制台工具，
// cmd /c start 更是必然），Windows 会为子进程现场分配一个新控制台，
// 表现为每次诊断、每次刮削、每个 ffprobe 都闪过一个黑框。
//
// 两个标志一起设：CREATE_NO_WINDOW 让进程不拥有可见控制台；HideWindow
// 把 ShowWindow 设为 SW_HIDE，兜住不认前者的启动路径。必须合并而不是
// 覆盖 SysProcAttr —— scraper 的进程组属性（CREATE_NEW_PROCESS_GROUP）
// 由它自己设置，我们只往里加位。
func Hide(cmd *exec.Cmd) {
	attr := cmd.SysProcAttr
	if attr == nil {
		attr = &syscall.SysProcAttr{}
		cmd.SysProcAttr = attr
	}
	attr.HideWindow = true
	attr.CreationFlags |= windows.CREATE_NO_WINDOW
}
