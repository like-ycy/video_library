// Package toolpath 定位随 App 分发、或由构建脚本产出的外部工具。
//
// 为什么不能只认一种路径
// ----------------------
// 同一个工具在两种布局下位置不同，而两种布局都真实存在：
//
//	分发布局（tools/package.* 产出，见 docs/packaging-contract.md §4）
//	    <App>/VideoLib.exe
//	    <App>/tools/scraper/scraper.exe
//	    <App>/tools/ffprobe/ffprobe.exe
//
//	开发布局（tools/build.* 组装 dist/<PRODUCT> 后从那里运行）
//	    <repo>/dist/VideoLib/tools/scraper/scraper.exe
//	    <repo>/dist/VideoLib/tools/ffprobe/ffprobe.exe
//
// 只认其中一种，另一种就会静默失效。而「静默」才是要害：找不到时调用方退到
// PATH、再退到空路径，对外表现为「未找到刮削器组件」或「时长字段不显示」，
// 现场看不出跟目录层级有任何关系。开发期仓库里恰好有一份可用的 dist/，一切
// 正常，打包之后才发现 —— 所以这里两种都覆盖，且这段判断只留这一个副本。
//
// 组件一律进同名子目录（tools/<c>/<exe>），即使只有一个文件：运行时契约要求
// 目录形态统一，ffprobe 也因此从 tools/ffprobe 变成 tools/ffprobe/ffprobe。
//
// 为什么从可执行文件出发、逐层向上，而不是相对当前工作目录
// ------------------------------------------------------
// App 通常由快捷方式或双击启动，工作目录是用户主目录、甚至是 system32，
// 不是安装目录。相对路径在这种启动方式下必然落空。可执行文件的位置才是
// 可靠锚点。
//
// 向上走几层，是为了同时覆盖两种深度：分发布局里 tools/ 在 .app 的同级
// （macOS 从 Contents/MacOS 向上 3 层），开发布局里 exe 在
// go/build/bin/*.app/Contents/MacOS，仓库根在上 6 层。
package toolpath

import (
	"os"
	"path/filepath"
)

// ProductDir 是分发目录名，等于 tools/config.env 的 PRODUCT 与 wails.json 的 name。
//
// 开发布局下组件不在 <App> 旁边，而在 <repo>/dist/<ProductDir>/tools/：
// tools/build.sh 每次都把整包装配到 dist/<ProductDir>，App 也从那里运行。
const ProductDir = "VideoLib"

// maxLevels 是从可执行文件目录向上试探的层数（含自身）。
//
// 8 层是为了覆盖 macOS 开发布局：
//
//	go/build/bin/VideoLib.app/Contents/MacOS → Contents → .app → bin →
//	build → go → <repo>
//
// 从 MacOS 目录到仓库根要走 6 层，之后还要在 <repo>/dist/VideoLib/tools/...
// 上命中候选。Windows 开发布局（go/build/bin/VideoLib.exe）只需 3 层，
// 分发布局（.app/Contents/MacOS）只需 3 层。再多走没有收益，只会增加命中
// 无关同名文件的机会。
const maxLevels = 8

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

// DevRel 返回开发布局下的路径片段：dist/<ProductDir>/<rel...>。
//
// 与分发候选（直接 <rel...>）并列交给 Find：哪个先命中取决于从可执行文件
// 向上第几层存在该目录，两条都试才能同时覆盖两种运行方式。
func DevRel(rel ...string) []string {
	return append([]string{"dist", ProductDir}, rel...)
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
