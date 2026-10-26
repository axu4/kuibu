package main

import (
	_ "embed"
	"runtime"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// trayIcon 是 macOS 菜单栏模板图标（44×44，纯黑 + 透明背景）。
//
//go:embed build/tray/tray-template.png
var trayIcon []byte

// shellUI 是 ShellService 的桌面实现：持有主窗口、托盘与小窗。
type shellUI struct {
	app *application.App

	mainWindow *application.WebviewWindow

	// mu 保护 quickNote 与 trayNewNote。
	mu        sync.Mutex
	quickNote *application.WebviewWindow
	trayItem  *application.MenuItem
}

// newShellUI 创建桌面外壳，主窗口由 main 注入。
func newShellUI(app *application.App) *shellUI {
	return &shellUI{app: app}
}

// setupTray 创建菜单栏托盘。左键点击不设置 OnClick，走 Wails 默认行为：
// 没有 attached window 时直接打开菜单（macOS 惯例）。
func (u *shellUI) setupTray() {
	tray := u.app.SystemTray.New()
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(trayIcon)
	} else {
		tray.SetIcon(trayIcon)
	}
	tray.SetTooltip("跬步")

	menu := u.app.NewMenu()
	newNote := menu.Add(trayNewNoteLabel(""))
	newNote.OnClick(func(*application.Context) { u.ShowQuickNote() })
	menu.Add("打开跬步").OnClick(func(*application.Context) { u.ShowMainWindow(false) })
	menu.AddSeparator()
	menu.Add("设置…").OnClick(func(*application.Context) { u.ShowMainWindow(true) })
	menu.AddSeparator()
	menu.Add("退出跬步").OnClick(func(*application.Context) { u.app.Quit() })
	tray.SetMenu(menu)

	u.mu.Lock()
	u.trayItem = newNote
	u.mu.Unlock()
}

// UpdateTrayShortcut 把当前快捷键写进托盘菜单项标签。
//
// 用标签而不是 SetAccelerator：Wails 的菜单加速键在 macOS 上会变成
// 菜单项的 key equivalent（菜单打开时还会抢按键），而这里只需要展示。
func (u *shellUI) UpdateTrayShortcut(accel string) {
	u.mu.Lock()
	item := u.trayItem
	u.mu.Unlock()
	if item != nil {
		item.SetLabel(trayNewNoteLabel(accel))
	}
}

// ShowMainWindow 显示并聚焦主窗口；openSettings 为 true 时通知前端打开设置。
func (u *shellUI) ShowMainWindow(openSettings bool) {
	w := u.mainWindow
	if w == nil {
		return
	}
	if w.IsMinimised() {
		w.UnMinimise()
	}
	w.Show()
	w.Focus()
	if openSettings {
		u.Emit(openSettingsEvent, nil)
	}
}

// ShowQuickNote 创建或聚焦随手记小窗。
//
// 小窗按需创建、关闭时真正销毁（而不是隐藏常驻）：Wails v3.0.0 在
// events_common_darwin.go 里对 Dock reopen 的默认处理是「没有可见窗口时
// Show 所有隐藏窗口」，隐藏常驻的小窗会在点 Dock 图标时被一起弹出来。
// 每次新建、关闭即 Close 销毁可以从根本上避开这个默认行为，代价只是
// 每次打开多一次 WebView 创建（小窗页面很轻，可接受）。
func (u *shellUI) ShowQuickNote() {
	u.mu.Lock()
	if w := u.quickNote; w != nil {
		u.mu.Unlock()
		w.Show()
		w.Focus()
		u.Emit(quickNoteFocusEvent, nil)
		return
	}

	w := u.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:            "quicknote",
		Title:           "随手记",
		Width:           560,
		Height:          300,
		Frameless:       true,
		AlwaysOnTop:     true,
		InitialPosition: application.WindowCentered,
		DisableResize:   true,
		URL:             "/?view=quicknote",
		Mac: application.MacWindow{
			// 用 NSPanel 而不是普通 NSWindow：
			//  - NSPanel 的 canBecomeMainWindow 为 NO（见 webview_panel_darwin.m），
			//    不会出现在「窗口」菜单里，也不会在 Dock 里多一个窗口；
			//  - 仍然是可激活窗口（不用 NonActivating），保证能接收键盘输入。
			WindowClass: application.MacWindowClassPanel,
			PanelPreferences: application.MacPanelPreferences{
				FloatingPanel: true,
			},
			WindowLevel: application.MacWindowLevelFloating,
			CollectionBehavior: application.MacWindowCollectionBehaviorCanJoinAllSpaces |
				application.MacWindowCollectionBehaviorFullScreenAuxiliary,
		},
	})
	u.quickNote = w
	u.mu.Unlock()

	// 关闭后清掉引用；下次快捷键会重新创建。
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		u.mu.Lock()
		if u.quickNote == w {
			u.quickNote = nil
		}
		u.mu.Unlock()
	})

	w.Show()
	w.Focus()
}

// CloseQuickNote 销毁随手记小窗。
func (u *shellUI) CloseQuickNote() {
	u.mu.Lock()
	w := u.quickNote
	u.quickNote = nil
	u.mu.Unlock()
	if w != nil {
		w.Close()
	}
}

// Emit 向前端发事件。
func (u *shellUI) Emit(name string, data any) {
	if data == nil {
		u.app.Event.Emit(name)
		return
	}
	u.app.Event.Emit(name, data)
}

// hideMainWindowOnClose 让主窗口关闭时只隐藏，应用继续常驻菜单栏。
//
// quitting 为 true 时（⌘Q 或托盘「退出跬步」）放行，让 Wails 正常销毁窗口。
func hideMainWindowOnClose(window *application.WebviewWindow, quitting func() bool) {
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if quitting() {
			return
		}
		window.Hide()
		e.Cancel()
	})
}

// setupDockReopen 处理点击 Dock 图标：重新显示并聚焦主窗口。
//
// Wails 默认在 Dock reopen 且没有可见窗口时会 Show 所有隐藏窗口；
// 我们的小窗按需创建、关闭即销毁，因此默认行为不会误弹小窗。这里再
// 注册一个监听，确保主窗口是焦点。
func (u *shellUI) setupDockReopen() {
	u.app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) {
		if u.mainWindow == nil {
			return
		}
		if !u.mainWindow.IsVisible() {
			u.mainWindow.Show()
		}
		u.mainWindow.Focus()
	})
}
