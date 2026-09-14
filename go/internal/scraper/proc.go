package scraper

import "os/exec"

// processTree 负责把子进程及其全部后代纳入管辖，并在取消时一并终止。
//
// 为什么必须做这件事：exec.CommandContext 的默认取消只杀直接子进程。
// Python 侧每个刮削任务都会拉起一个 Chrome，杀掉 Python 之后那些 Chrome
// 会全部变成孤儿进程留在后台 —— 用户点几次取消就能把内存耗光，而且
// 这些残留进程不会有任何提示。
//
// Windows 用 Job Object；类 Unix 用进程组（setpgid）。
type processTree interface {
	// attach 必须在 cmd.Start() 返回后立刻调用。
	//
	// 越早纳入管辖，「子孙进程在纳入之前就被拉起、从而逃出管辖」的时间窗口越小。
	// Python 启动到拉起 Chrome 之间有数百毫秒（导入 seleniumbase、初始化驱动），
	// 而 attach 只需要微秒级，因此这个窗口在实践中可以忽略。
	attach(cmd *exec.Cmd) error

	// kill 终止整棵进程树。
	kill(cmd *exec.Cmd)

	// release 释放平台资源。Windows 上会顺带终止仍在 Job 中的进程。
	release()
}
