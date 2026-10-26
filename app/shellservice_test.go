package main

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/axu4/kuibu/internal/config"
	"github.com/axu4/kuibu/internal/quicknote"
	"github.com/axu4/kuibu/internal/store"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// fakeShortcuts 是 shortcutManager 的假实现，可模拟注册失败与被占用的键。
type fakeShortcuts struct {
	mu         sync.Mutex
	registered []string
	failWith   map[string]error
}

func newFakeShortcuts() *fakeShortcuts {
	return &fakeShortcuts{failWith: map[string]error{}}
}

func (f *fakeShortcuts) Register(accel string, _ func()) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.failWith[accel]; ok {
		return err
	}
	for _, a := range f.registered {
		if a == accel {
			return errors.New("already registered")
		}
	}
	f.registered = append(f.registered, accel)
	return nil
}

func (f *fakeShortcuts) Unregister(accel string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, a := range f.registered {
		if a == accel {
			f.registered = append(f.registered[:i], f.registered[i+1:]...)
			return nil
		}
	}
	return errors.New("not registered")
}

func (f *fakeShortcuts) IsRegistered(accel string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.registered {
		if a == accel {
			return true
		}
	}
	return false
}

func (f *fakeShortcuts) list() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.registered...)
}

// fakeDesktop 记录 ShellService 对桌面能力的调用。
type fakeDesktop struct {
	quickNotes int
	closed     int
	shown      int
	settings   int
	trayAccels []string
	events     []string
}

func (d *fakeDesktop) ShowQuickNote()  { d.quickNotes++ }
func (d *fakeDesktop) CloseQuickNote() { d.closed++ }
func (d *fakeDesktop) ShowMainWindow(openSettings bool) {
	d.shown++
	if openSettings {
		d.settings++
	}
}
func (d *fakeDesktop) UpdateTrayShortcut(accel string) {
	d.trayAccels = append(d.trayAccels, accel)
}
func (d *fakeDesktop) Emit(name string, _ any) { d.events = append(d.events, name) }

// newTestShell 构造一个使用隔离配置目录与临时工作台的 ShellService。
func newTestShell(t *testing.T) (*ShellService, *fakeShortcuts, *fakeDesktop, *WorkspaceService) {
	t.Helper()

	configDir := t.TempDir()
	ws := newWorkspaceService()
	shell := newShellService(ws)
	shell.env = func(key string) string {
		if key == "KUIBU_CONFIG_DIR" {
			return configDir
		}
		return ""
	}
	shell.now = func() time.Time {
		return time.Date(2026, 3, 4, 9, 30, 0, 0, time.Local)
	}
	shortcuts := newFakeShortcuts()
	ui := &fakeDesktop{}
	shell.shortcuts = shortcuts
	shell.ui = ui
	return shell, shortcuts, ui, ws
}

// openTempWorkspace 在临时目录初始化工作台并挂到 WorkspaceService 上。
func openTempWorkspace(t *testing.T, ws *WorkspaceService) *store.Workspace {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "ws")
	if _, err := store.Init(dir); err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	if err := ws.open(dir); err != nil {
		t.Fatalf("打开临时工作台失败: %v", err)
	}
	t.Cleanup(ws.close)
	return ws.workspace()
}

func TestSaveQuickNoteFallsIntoInbox(t *testing.T) {
	shell, _, ui, ws := newTestShell(t)
	opened := openTempWorkspace(t, ws)

	saved, err := shell.SaveQuickNote("  买牛奶\n- [ ] 顺便买面包  ")
	if err != nil {
		t.Fatalf("SaveQuickNote 返回错误: %v", err)
	}

	inbox, err := quicknote.EnsureInbox(opened)
	if err != nil {
		t.Fatalf("EnsureInbox 失败: %v", err)
	}
	if saved.InboxID != inbox.ID {
		t.Errorf("InboxID = %q，期望 %q", saved.InboxID, inbox.ID)
	}

	note, err := opened.Get(saved.ID)
	if err != nil {
		t.Fatalf("读取新笔记失败: %v", err)
	}
	if note.Parent != inbox.ID {
		t.Errorf("Parent = %q，期望收件箱 %q", note.Parent, inbox.ID)
	}
	if note.Body != "买牛奶\n- [ ] 顺便买面包" {
		t.Errorf("Body = %q", note.Body)
	}
	if saved.Title != "买牛奶" {
		t.Errorf("Title = %q，期望 %q", saved.Title, "买牛奶")
	}

	if ui.closed != 1 {
		t.Errorf("保存后应关闭小窗，实际关闭 %d 次", ui.closed)
	}
	if len(ui.events) != 1 || ui.events[0] != quickNoteSavedEvent {
		t.Errorf("事件 = %v，期望 [%s]", ui.events, quickNoteSavedEvent)
	}
}

func TestSaveQuickNoteWithoutWorkspace(t *testing.T) {
	shell, _, ui, _ := newTestShell(t)

	_, err := shell.SaveQuickNote("随便写点什么")
	if err == nil {
		t.Fatal("未打开工作台时应返回错误")
	}
	if got := err.Error(); got == "" || !contains(got, "工作台") {
		t.Errorf("错误信息 = %q，期望提到工作台", got)
	}
	if ui.closed != 0 || len(ui.events) != 0 {
		t.Errorf("失败时不应关闭小窗或发事件: closed=%d events=%v", ui.closed, ui.events)
	}
}

func TestSaveQuickNoteRejectsEmpty(t *testing.T) {
	shell, _, _, ws := newTestShell(t)
	openTempWorkspace(t, ws)

	if _, err := shell.SaveQuickNote("   \n\t "); err == nil {
		t.Fatal("空内容应返回错误")
	}
}

func TestSetQuickNoteShortcutPersists(t *testing.T) {
	shell, shortcuts, ui, _ := newTestShell(t)
	// 模拟启动态：默认快捷键已注册。
	if err := shell.applyShortcut("Ctrl+Alt+N", false); err != nil {
		t.Fatalf("初始注册失败: %v", err)
	}

	status, err := shell.SetQuickNoteShortcut(" ctrl + shift + k ")
	if err != nil {
		t.Fatalf("SetQuickNoteShortcut 返回错误: %v", err)
	}
	if status.Accelerator != "Ctrl+Shift+K" || !status.Registered || status.Error != "" {
		t.Errorf("状态 = %+v", status)
	}
	if status.Display != "⌃⇧K" {
		t.Errorf("Display = %q，期望 ⌃⇧K", status.Display)
	}
	if got := shortcuts.list(); len(got) != 1 || got[0] != "Ctrl+Shift+K" {
		t.Errorf("已注册快捷键 = %v，期望只剩新快捷键", got)
	}
	if len(ui.trayAccels) == 0 || ui.trayAccels[len(ui.trayAccels)-1] != "Ctrl+Shift+K" {
		t.Errorf("托盘应同步新快捷键，实际 %v", ui.trayAccels)
	}

	// 持久化生效：重新读取配置应得到新值。
	reloaded := newShellService(nil)
	reloaded.env = shell.env
	accel, err := configShortcut(reloaded)
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	if accel != "Ctrl+Shift+K" {
		t.Errorf("配置里的快捷键 = %q，期望 Ctrl+Shift+K", accel)
	}
}

func TestSetQuickNoteShortcutKeepsOldOnFailure(t *testing.T) {
	shell, shortcuts, _, _ := newTestShell(t)
	if err := shell.applyShortcut("Ctrl+Alt+N", false); err != nil {
		t.Fatalf("初始注册失败: %v", err)
	}
	shortcuts.failWith["Ctrl+Shift+K"] = errors.New("hotkey taken")

	status, err := shell.SetQuickNoteShortcut("Ctrl+Shift+K")
	if err == nil {
		t.Fatal("被占用的快捷键应返回错误")
	}
	if !contains(err.Error(), "注册失败") {
		t.Errorf("错误信息 = %q，期望可读的中文提示", err.Error())
	}
	if status.Accelerator != "Ctrl+Alt+N" || !status.Registered {
		t.Errorf("失败后应保留旧快捷键，状态 = %+v", status)
	}
	if status.Error == "" {
		t.Error("失败后应带上错误信息")
	}
	if got := shortcuts.list(); len(got) != 1 || got[0] != "Ctrl+Alt+N" {
		t.Errorf("已注册快捷键 = %v，期望仍是旧快捷键", got)
	}
}

func TestSetQuickNoteShortcutRejectsInvalid(t *testing.T) {
	shell, shortcuts, _, _ := newTestShell(t)
	if err := shell.applyShortcut("Ctrl+Alt+N", false); err != nil {
		t.Fatalf("初始注册失败: %v", err)
	}

	for _, input := range []string{"", "N", "Ctrl"} {
		status, err := shell.SetQuickNoteShortcut(input)
		if err == nil {
			t.Errorf("SetQuickNoteShortcut(%q) 应返回错误", input)
		}
		if status.Accelerator != "Ctrl+Alt+N" {
			t.Errorf("非法输入后快捷键被改动: %+v", status)
		}
	}
	if got := shortcuts.list(); len(got) != 1 || got[0] != "Ctrl+Alt+N" {
		t.Errorf("非法输入不应影响已注册快捷键: %v", got)
	}
}

func TestResetQuickNoteShortcut(t *testing.T) {
	shell, shortcuts, _, _ := newTestShell(t)
	if err := shell.applyShortcut("Ctrl+Shift+K", true); err != nil {
		t.Fatalf("初始注册失败: %v", err)
	}

	status, err := shell.ResetQuickNoteShortcut()
	if err != nil {
		t.Fatalf("ResetQuickNoteShortcut 返回错误: %v", err)
	}
	if status.Accelerator != "Ctrl+Alt+N" || !status.Registered {
		t.Errorf("恢复默认后状态 = %+v", status)
	}
	if got := shortcuts.list(); len(got) != 1 || got[0] != "Ctrl+Alt+N" {
		t.Errorf("已注册快捷键 = %v，期望默认值", got)
	}

	reloaded := newShellService(nil)
	reloaded.env = shell.env
	accel, err := configShortcut(reloaded)
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	if accel != "Ctrl+Alt+N" {
		t.Errorf("配置里的快捷键 = %q，期望默认值", accel)
	}
}

func TestStartupFallsBackToDefaultOnInvalidConfig(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{name: "缺少修饰键", input: "N"},
		{name: "纯修饰键", input: "Ctrl+Alt"},
		{name: "未知修饰键", input: "Foo+N"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			shell, shortcuts, _, _ := newTestShell(t)
			// 模拟用户手改配置写入了不合法的快捷键。
			if err := config.SaveQuickNoteShortcut(shell.env, tc.input); err != nil {
				t.Fatalf("写配置失败: %v", err)
			}

			if err := shell.ServiceStartup(context.Background(), application.ServiceOptions{}); err != nil {
				t.Fatalf("ServiceStartup 返回错误: %v", err)
			}
			// 单测里没有真实的 App，直接模拟 ApplicationStarted 后触发注册。
			shell.registerCurrent()

			// 假管理器只能收到默认快捷键，绝不能收到非法加速键。
			got := shortcuts.list()
			if len(got) != 1 || got[0] != config.DefaultQuickNoteShortcut {
				t.Errorf("已注册快捷键 = %v，期望只有默认值 %s", got, config.DefaultQuickNoteShortcut)
			}
			for _, accel := range got {
				if accel == tc.input {
					t.Errorf("假管理器收到了不合法的加速键 %q", accel)
				}
				if _, err := normalizeShortcut(accel); err != nil {
					t.Errorf("假管理器收到了未通过校验的加速键 %q: %v", accel, err)
				}
			}

			status := shell.GetShortcutStatus()
			if !status.Registered {
				t.Errorf("回退后应注册成功: %+v", status)
			}
			if status.Accelerator != config.DefaultQuickNoteShortcut {
				t.Errorf("Accelerator = %q，期望默认值", status.Accelerator)
			}
			if status.Notice == "" {
				t.Error("回退到默认值后应带上中文提示")
			}
			if !contains(status.Notice, "不合法") || !contains(status.Notice, "临时改用默认") {
				t.Errorf("Notice = %q，期望说明不合法并已改用默认值", status.Notice)
			}

			// 不改写用户配置：下次启动仍会提醒用户。
			accel, err := config.QuickNoteShortcut(shell.env)
			if err != nil {
				t.Fatalf("读取配置失败: %v", err)
			}
			if accel != tc.input {
				t.Errorf("配置里的快捷键 = %q，期望保持用户写入的 %q", accel, tc.input)
			}
		})
	}
}

func TestStartupValidConfigHasNoNotice(t *testing.T) {
	shell, shortcuts, _, _ := newTestShell(t)
	if err := config.SaveQuickNoteShortcut(shell.env, "ctrl+shift+k"); err != nil {
		t.Fatalf("写配置失败: %v", err)
	}

	if err := shell.ServiceStartup(context.Background(), application.ServiceOptions{}); err != nil {
		t.Fatalf("ServiceStartup 返回错误: %v", err)
	}
	shell.registerCurrent()

	status := shell.GetShortcutStatus()
	if !status.Registered || status.Accelerator != "Ctrl+Shift+K" {
		t.Errorf("合法配置应直接注册：%+v", status)
	}
	if status.Notice != "" {
		t.Errorf("合法配置不应带提示，实际 %q", status.Notice)
	}
	if status.Error != "" {
		t.Errorf("合法配置不应带错误，实际 %q", status.Error)
	}
	if got := shortcuts.list(); len(got) != 1 || got[0] != "Ctrl+Shift+K" {
		t.Errorf("已注册快捷键 = %v", got)
	}
}

func TestApplyShortcutRejectsInvalidAccelerator(t *testing.T) {
	shell, shortcuts, _, _ := newTestShell(t)

	for _, input := range []string{"N", "Ctrl+Alt", "Foo+N"} {
		if err := shell.applyShortcut(input, true); err == nil {
			t.Errorf("applyShortcut(%q) 应返回错误", input)
		}
	}
	if got := shortcuts.list(); len(got) != 0 {
		t.Errorf("非法加速键不应被注册，实际 %v", got)
	}
}

func TestSetShortcutSuccessClearsStaleError(t *testing.T) {
	shell, shortcuts, _, _ := newTestShell(t)
	if err := shell.applyShortcut("Ctrl+Alt+N", false); err != nil {
		t.Fatalf("初始注册失败: %v", err)
	}

	// 失败一次：旧快捷键仍生效，但 lastErr 带上了失败信息。
	shortcuts.failWith["Ctrl+Shift+K"] = errors.New("hotkey taken")
	status, err := shell.SetQuickNoteShortcut("Ctrl+Shift+K")
	if err == nil {
		t.Fatal("被占用的快捷键应返回错误")
	}
	if !status.Registered || status.Error == "" {
		t.Errorf("失败后旧键仍有效且带错误信息：%+v", status)
	}

	// 下一次成功设置必须清掉旧错误。
	delete(shortcuts.failWith, "Ctrl+Shift+K")
	status, err = shell.SetQuickNoteShortcut("Ctrl+Shift+K")
	if err != nil {
		t.Fatalf("解除占用后应能设置成功: %v", err)
	}
	if status.Error != "" {
		t.Errorf("设置成功后应清空旧错误，实际 %q", status.Error)
	}
	if !status.Registered || status.Accelerator != "Ctrl+Shift+K" {
		t.Errorf("设置成功后状态 = %+v", status)
	}
}

func TestResetClearsStaleError(t *testing.T) {
	shell, shortcuts, _, _ := newTestShell(t)
	if err := shell.applyShortcut("Ctrl+Alt+N", false); err != nil {
		t.Fatalf("初始注册失败: %v", err)
	}
	shortcuts.failWith["Ctrl+Shift+K"] = errors.New("hotkey taken")
	if _, err := shell.SetQuickNoteShortcut("Ctrl+Shift+K"); err == nil {
		t.Fatal("被占用的快捷键应返回错误")
	}

	// 恢复默认：默认键已注册，仍应清掉旧错误。
	status, err := shell.ResetQuickNoteShortcut()
	if err != nil {
		t.Fatalf("ResetQuickNoteShortcut 返回错误: %v", err)
	}
	if status.Error != "" {
		t.Errorf("恢复默认后应清空旧错误，实际 %q", status.Error)
	}
	if !status.Registered || status.Accelerator != config.DefaultQuickNoteShortcut {
		t.Errorf("恢复默认后状态 = %+v", status)
	}
}

func TestStartupFailureIsReported(t *testing.T) {
	shell, shortcuts, ui, _ := newTestShell(t)
	shortcuts.failWith["Ctrl+Alt+N"] = errors.New("hotkey taken")

	// 直接模拟启动完成后的注册。
	shell.current = "Ctrl+Alt+N"
	shell.registerCurrent()

	status := shell.GetShortcutStatus()
	if status.Registered {
		t.Errorf("注册失败时不应标记为已注册: %+v", status)
	}
	if status.Accelerator != "Ctrl+Alt+N" {
		t.Errorf("Accelerator = %q", status.Accelerator)
	}
	if !contains(status.Error, "注册失败") {
		t.Errorf("Error = %q，期望中文失败原因", status.Error)
	}
	if len(ui.events) == 0 || ui.events[len(ui.events)-1] != shortcutChangedEvent {
		t.Errorf("应通知前端刷新快捷键状态，实际事件 %v", ui.events)
	}
}

func TestOpenMainWindowAndCloseQuickNote(t *testing.T) {
	shell, _, ui, _ := newTestShell(t)

	shell.OpenMainWindow()
	shell.CloseQuickNote()
	if ui.shown != 1 {
		t.Errorf("OpenMainWindow 应显示主窗口，实际 %d 次", ui.shown)
	}
	if ui.closed != 1 {
		t.Errorf("CloseQuickNote 应关闭小窗，实际 %d 次", ui.closed)
	}
}

// configShortcut 读取（可能被覆盖过的）配置里的快捷键。
func configShortcut(shell *ShellService) (string, error) {
	return config.QuickNoteShortcut(shell.env)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
