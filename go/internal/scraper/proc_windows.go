//go:build windows

package scraper

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"videolib/internal/spawn"
)

func newProcessTree() processTree { return &windowsProcessTree{} }

// windowsProcessTree 用 Job Object 管辖子进程树。
//
// 关键配置是 JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE：句柄一关，Job 内所有进程
// （包括 Python 拉起的 Chrome）全部终止。这同时也是「进程已退出但孙进程残留」
// 场景的兜底。
type windowsProcessTree struct {
	job windows.Handle
}

func (t *windowsProcessTree) attach(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return errors.New("进程尚未启动，无法纳入管辖")
	}
	if t.job == 0 {
		job, err := createKillOnCloseJob()
		if err != nil {
			return err
		}
		t.job = job
	}

	// os/exec 不暴露进程句柄，按 pid 重新打开一个。
	handle, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid),
	)
	if err != nil {
		return fmt.Errorf("打开子进程句柄: %w", err)
	}
	defer windows.CloseHandle(handle)

	if err := windows.AssignProcessToJobObject(t.job, handle); err != nil {
		return fmt.Errorf("把子进程加入 Job Object: %w", err)
	}
	return nil
}

func (t *windowsProcessTree) kill(cmd *exec.Cmd) {
	if t.job != 0 {
		// TerminateJobObject 会连子孙进程一起终止，这正是我们要的。
		if err := windows.TerminateJobObject(t.job, 1); err == nil {
			return
		}
	}
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func (t *windowsProcessTree) release() {
	if t.job != 0 {
		_ = windows.CloseHandle(t.job)
		t.job = 0
	}
}

func createKillOnCloseJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("创建 Job Object: %w", err)
	}

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return 0, fmt.Errorf("配置 Job Object: %w", err)
	}
	return job, nil
}

func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP,
	}
	// GUI 宿主下启动控制台子进程必须显式隐藏窗口，否则每次 doctor/刮削
	// 都闪一个黑框。合并而非覆盖：上面的进程组标志要保留。
	spawn.Hide(cmd)
}
