package main

import (
	"embed"
	"io/fs"
	"log"
	"runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"

	"videolib/internal/media"
)

// 嵌入整个 frontend 目录。
//
// 用 all: 前缀而不是默认行为：默认规则会跳过以 _ 或 . 开头的文件，
// 而前端资源里一旦出现这类文件，症状是"运行时才 404"，很难联想到打包规则。
//
// 这个项目没有前端构建步骤（不用 npm、不用打包器），frontend/ 里的文件就是
// 最终产物，因此直接嵌入整个目录而不是 frontend/dist。
//
//go:embed all:frontend
var embeddedFrontend embed.FS

func main() {
	app, err := NewApp()
	if err != nil {
		log.Fatalf("初始化失败：%v", err)
	}
	defer app.Close()

	frontend, err := fs.Sub(embeddedFrontend, "frontend")
	if err != nil {
		log.Fatalf("加载前端资源：%v", err)
	}

	opts := &options.App{
		Title:            "视频库",
		Width:            1440,
		Height:           920,
		MinWidth:         1024,
		MinHeight:        680,
		BackgroundColour: &options.RGBA{R: 20, G: 18, B: 29, A: 1},
		AssetServer: &assetserver.Options{
			Assets: frontend,
			// Wails 对 GET 请求先匹配 Assets，匹配不到（fs.ErrNotExist）才落到
			// 这里。所以前端资源不会经过它，而 /media/ 与 /art/ 会 ——
			// 不需要（也不应该）在这里做「回退到默认资源服务」的逻辑。
			Handler: media.New(app.ResolveLibraryRoot),
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind:       []any{app},
	}

	// macOS：用系统原生红绿灯（透明标题栏 + 内容铺满），避免无边框自绘按钮。
	// Windows/Linux：无边框，关闭/最小化/最大化由前端调用 Wails runtime。
	if runtime.GOOS == "darwin" {
		opts.Mac = &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
		}
	} else {
		opts.Frameless = true
	}

	err = wails.Run(opts)
	if err != nil {
		log.Fatalf("应用退出：%v", err)
	}
}
