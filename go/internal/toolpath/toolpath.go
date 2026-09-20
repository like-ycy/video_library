// Package toolpath 定位随 App 分发、或由构建脚本产出的外部工具。
//
// 为什么不能只认一种路径
// ----------------------
// 同一个工具在两种布局下位置不同，而两种布局都真实存在：
//
//	分发布局（tools/package.ps1 产出，见 docs/architecture.md §9.3）
//	    <App>/VideoLib.exe
//	    <App>/tools/scraper/scraper.exe
//	    <App>/tools/ffprobe.exe
//
//	开发布局（tools/build.sh 产出）
//	    <repo>/tools/bin/scraper/scraper.exe
//	    <repo>/tools/bin/ffprobe.exe
//
// 只认其中一种，另一种就会静默失效。而「静默」才是要害：找不到时调用方退到
// PATH、再退到空路径，对外表现为「未找到刮削器组件」或「时长字段不显示」，
// 现场看不出跟目录层级有任何关系。开发期仓库里恰好有 tools/bin/，一切正常，
// 打包之后才发现 —— 所以这里两种都覆盖，且这段判断只留这一个副本。
//
// 为什么从可执行文件出发、逐层向上，而不是相对当前工作目录
// ------------------------------------------------------
// App 通常由快捷方式或双击启动，工作目录是用户主目录、甚至是 system32，
// 不是安装目录。相对路径在这种启动方式下必然落空。可执行文件的位置才是
// 可靠锚点。
//
// 向上走几层，是为了同时覆盖两种深度：分发布局里 tools/ 是 exe 的同级目录
// （第 1 层就命中），开发布局里 exe 在 go/build/bin/，仓库根在上三层。
package toolpath

import (
	"os"
	"path/filepath"
)

// maxLevels 是从可执行文件目录向上试探的层数（含自身）。
//
// 4 层刚好覆盖开发布局：go/build/bin → go/build → go → <repo>。
// 再多走只会增加命中无关同名文件的机会，没有收益。
const maxLevels = 4

// Find 在标准位置查找文件。rel 是相对于每一层根目录的路径片段。
//
// 返回第一个存在的**普通文件**绝对路径；都不存在时返回空字符串。
//
// 刻意不返回 error：调用方对「没找到」的处理各不相同 —— scraper 要据此报错并
// 引导用户，ffprobe 只是降级少显示两个字段。强行统一成 error 会让两边都要
// 写一遍判断，还不一定都写得对。
func Find(rel ...string) string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return findFrom(filepath.Dir(exe), rel...)
}

// Name 按平台补上可执行文件后缀。
func Name(base string) string {
	if isWindows() {
		return base + ".exe"
	}
	return base
}

// findFrom 是 Find 的实体，单独拆出来是为了可测 —— os.Executable() 在测试
// 里指向临时目录中的测试二进制，没法构造布局，而起始目录一旦可注入就很好造。
func findFrom(dir string, rel ...string) string {
	for level := 0; level < maxLevels; level++ {
		candidate := filepath.Join(append([]string{dir}, rel...)...)
		// 要求是普通文件而不是目录：同名目录摆在候选位置上同样会让
		// 「存在」成立，但拿目录去 exec 会在运行时才炸。
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// 已经到盘根（Windows 的 C:\ 或 Unix 的 /）。
			break
		}
		dir = parent
	}
	return ""
}

func isWindows() bool {
	return os.PathSeparator == '\\'
}
