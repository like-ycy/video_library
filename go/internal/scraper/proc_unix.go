//go:build !windows

package scraper

import (
	"errors"
	"os/exec"
	"syscall"

	"videolib/internal/spawn"
)

func newProcessTree() processTree { return unixProcessTree{} }

// unixProcessTree 依赖 setpgid：子进程自成进程组，向 -pgid 发信号即杀掉整组。
type unixProcessTree struct{}

// attach 在类 Unix 上是空操作 —— 进程组的建立发生在 fork 时（见 setProcAttr），
// 没有「先启动后纳管」的窗口。
func (unixProcessTree) attach(*exec.Cmd) error { return nil }

func (unixProcessTree) kill(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	// 负 pid 表示整个进程组。
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil &&
		!errors.Is(err, syscall.ESRCH) {
		// 进程组已经不存在（ESRCH）之外的情况，退化为只杀单进程。
		_ = cmd.Process.Kill()
	}
}

func (unixProcessTree) release() {}

func setProcAttr(cmd *exec.Cmd) {
	// Setpgid 让子进程成为新进程组的组长，父进程因此可以一次信号杀掉全组。
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// 非 Windows 上是空操作；调用点只写一次，避免两套启动属性逻辑。
	spawn.Hide(cmd)
}
