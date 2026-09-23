//go:build !windows

package spawn

import "os/exec"

// Hide 是空操作：macOS/Linux 上从 GUI 启动控制台程序本来就不会新建窗口，
// 子进程的 stdout/stderr 也都由调用方接管成管道，没有窗口这回事。
func Hide(_ *exec.Cmd) {}
