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

// 仅嵌入 Vite 构建产物，避免分发源码和 node_modules。
//
//go:embed all:frontend/dist
var embeddedFrontend embed.FS

func main() {
	app, err := NewApp()
	if err != nil {
		log.Fatalf("初始化失败：%v", err)
	}
	defer app.Close()

	frontend, err := fs.Sub(embeddedFrontend, "frontend/dist")
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
