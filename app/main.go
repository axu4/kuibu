package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// assets 是构建后的前端资源（frontend/dist），由 go:embed 打进二进制。
//
//go:embed all:frontend/dist
var assets embed.FS

func main() {
	service := newWorkspaceService()

	app := application.New(application.Options{
		Name:        "kuibu",
		Description: "跬步工作台",
		Services: []application.Service{
			application.NewService(service),
		},
		Assets: application.AssetOptions{
			Handler:    application.AssetFileServerFS(assets),
			Middleware: service.assetMiddleware(),
		},
		Mac: application.MacOptions{
			// 里程碑 1：关闭最后一个窗口即退出。
			// 托盘、全局快捷键、关窗不退出属于里程碑 2。
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})
	service.app = app

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "跬步",
		Width:  1200,
		Height: 780,
		URL:    "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
