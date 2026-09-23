// Package spawn 统一外部子进程的启动属性。
//
// 目前只有一条规则：不要让子进程闪出控制台窗口。放在独立包里而不是让
// probe / player / scraper 各写一遍，是因为这几处一旦漏掉任何一处，
// 表现都是「某个按钮点一下闪过一个黑框」—— 而黑框不会出现在日志里，
// 只能靠人眼发现。判断只留这一个副本，新增起子进程的地方直接调 Hide。
//
// 只用于**控制台程序**（scraper.exe、ffprobe.exe、cmd）。GUI 程序
// （PotPlayer、Chrome）不要调：实现里的 SW_HIDE 会让它隐藏启动，
// 而它们本来也不会闪窗口。
//
// Hide 的签名与实现在 hide_windows.go / hide_other.go，按平台各自实现。
package spawn
