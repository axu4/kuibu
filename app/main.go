package main

import (
	"embed"
	"log"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// assets 是构建后的前端资源（frontend/dist），由 go:embed 打进二进制。
//
//go:embed all:frontend/dist
var assets embed.FS

func main() {
	workspace := newWorkspaceService()
	shell := newShellService(workspace)
	ui := newShellUI(nil)

	// quitting 在「真正退出」的路径上置位：WindowClosing 钩子看到它就放行，
	// 否则主窗口关窗只是隐藏（关窗不退出）。
	//
	// 为什么需要它：Wails 的 App.Quit 走 NSApp terminate，最终会调用
	// applicationShouldTerminate → shouldQuit() → cleanup()，cleanup 里对每个
	// 窗口调用 Close()，而 Close() 会 emit WindowClosing 事件，钩子照样执行。
	// 也就是说 WindowClosing 钩子拦得住退出流程，必须在退出前显式放行。
	var quitting atomic.Bool

	app := application.New(application.Options{
		Name:        "kuibu",
		Description: "跬步工作台",
		Services: []application.Service{
			application.NewService(workspace),
			application.NewService(shell),
		},
		Assets: application.AssetOptions{
			Handler:    application.AssetFileServerFS(assets),
			Middleware: workspace.assetMiddleware(),
		},
		ShouldQuit: func() bool {
			// 只在这里置位：表示用户确实要退出（⌘Q、托盘「退出跬步」、
			// 系统注销/关机触发的 terminate）。
			quitting.Store(true)
			return true
		},
		Mac: application.MacOptions{
			// 里程碑 2：关掉最后一个窗口不退出，应用常驻菜单栏。
			ApplicationShouldTerminateAfterLastWindowClosed: false,
			// 保留 Dock 图标与正常激活行为（默认值，显式写出来更清楚）。
			ActivationPolicy: application.ActivationPolicyRegular,
		},
	})
	app.OnShutdown(func() { quitting.Store(true) })

	workspace.app = app
	shell.app = app
	shell.shortcuts = app.GlobalShortcut
	shell.ui = ui
	ui.app = app

	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:   "main",
		Title:  "跬步",
		Width:  1200,
		Height: 780,
		URL:    "/",
	})
	ui.mainWindow = mainWindow

	hideMainWindowOnClose(mainWindow, quitting.Load)
	ui.setupDockReopen()
	ui.setupTray()

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
