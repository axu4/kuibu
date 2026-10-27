package main

import (
	"embed"
	"log"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// assets 是构建后的前端资源（frontend/dist），由 go:embed 打进二进制。
//
//go:embed all:frontend/dist
var assets embed.FS

func main() {
	workspace := newWorkspaceService()
	shell := newShellService(workspace)
	todos := newTodoService(workspace)
	updates := newUpdateService()
	reports := newReportService(workspace)
	llms := newLLMService()
	shell.todo = todos
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
			application.NewService(todos),
			application.NewService(updates),
			application.NewService(reports),
			application.NewService(llms),
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
	todos.app = app
	updates.app = app
	updates.quit = app.Quit
	shell.shortcuts = app.GlobalShortcut
	shell.ui = ui
	ui.app = app

	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:   "main",
		Title:  "跬步",
		Width:  1200,
		Height: 780,
		URL:    "/",
		// 隐藏标题栏，红绿灯浮在侧栏左上角；可拖动区域由前端的
		// --wails-draggable 标出（侧栏顶部与各工具栏）。
		Mac: application.MacWindow{
			TitleBar:                application.MacTitleBarHiddenInset,
			InvisibleTitleBarHeight: 40,
		},
		// 允许从 Finder 拖文件到主窗口；只有带 data-file-drop-target 属性的
		// 元素（待办详情面板）会成为目标，事件在下面转发给 TodoService。
		EnableFileDrop: true,
	})
	ui.mainWindow = mainWindow

	// 文件拖放：Wails 在 macOS 上把 DropTargetDetails 里的元素属性原样带回来，
	// 前端在详情面板上写 data-todo-id，这里据此把文件挂到对应待办。
	mainWindow.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {
		files := e.Context().DroppedFiles()
		todoID := ""
		if details := e.Context().DropTargetDetails(); details != nil {
			todoID = details.Attributes["data-todo-id"]
		}
		if todoID == "" || len(files) == 0 {
			return
		}
		if err := todos.HandleDroppedFiles(todoID, files); err != nil {
			log.Printf("kuibu: 添加拖入的附件失败: %v", err)
		}
	})

	hideMainWindowOnClose(mainWindow, quitting.Load)
	ui.setupDockReopen()
	ui.setupTray()

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
