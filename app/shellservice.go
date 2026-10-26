package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/axu4/kuibu/internal/config"
	"github.com/axu4/kuibu/internal/quicknote"
	"github.com/axu4/kuibu/internal/store"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// 事件名。Go → 前端。
const (
	// quickNoteSavedEvent 在随手记保存成功后发给主窗口。
	quickNoteSavedEvent = "quicknote:saved"
	// openSettingsEvent 让主窗口打开设置面板。
	openSettingsEvent = "shell:open-settings"
	// shortcutChangedEvent 在快捷键状态变化后通知前端刷新。
	shortcutChangedEvent = "shell:shortcut-changed"
	// quickNoteFocusEvent 让已打开的随手记小窗重新聚焦输入框。
	quickNoteFocusEvent = "quicknote:focus"
)

// ShortcutStatus 是随手记全局快捷键的状态，暴露给前端与托盘。
type ShortcutStatus struct {
	// Accelerator 是 Wails 语法的快捷键，例如 "Ctrl+Alt+N"。
	Accelerator string `json:"accelerator"`
	// Display 是 macOS 符号形式，例如 "⌃⌥N"。
	Display string `json:"display"`
	// Registered 表示是否已在系统层注册成功。
	Registered bool `json:"registered"`
	// Error 是注册失败的中文原因，成功时为空。
	Error string `json:"error"`
	// Notice 是「已临时回退但仍需用户处理」的中文提示，例如启动时配置里的
	// 快捷键不合法、已临时改用默认值。Registered 为 true 时也可能非空。
	Notice string `json:"notice"`
}

// QuickNoteSaved 是 quicknote:saved 事件的数据。
type QuickNoteSaved struct {
	// ID 是新建随手记笔记的 ID。
	ID string `json:"id"`
	// InboxID 是随手记收件箱的 ID。
	InboxID string `json:"inboxId"`
	// Title 是新笔记标题。
	Title string `json:"title"`
}

// shortcutManager 是 ShellService 用到的全局快捷键能力，便于单测替换。
// *application.GlobalShortcutManager 满足该接口。
type shortcutManager interface {
	Register(accelerator string, callback func()) error
	Unregister(accelerator string) error
	IsRegistered(accelerator string) bool
}

// shellDesktop 是 ShellService 用到的桌面能力（窗口、托盘、事件），
// 单测里用假实现替换，避免启动 Wails。
type shellDesktop interface {
	// ShowQuickNote 创建或聚焦随手记小窗。
	ShowQuickNote()
	// CloseQuickNote 关闭（销毁）随手记小窗。
	CloseQuickNote()
	// ShowMainWindow 显示并聚焦主窗口，openSettings 为 true 时顺带打开设置。
	ShowMainWindow(openSettings bool)
	// UpdateTrayShortcut 更新托盘菜单项上显示的快捷键。
	UpdateTrayShortcut(accel string)
	// Emit 向前端发事件；app 尚未启动时安全忽略。
	Emit(name string, data any)
}

// ShellService 暴露桌面外壳能力给前端：快捷键状态与修改、随手记保存、窗口控制。
//
// 所有文件读写仍走 internal/store 与 internal/quicknote，本层只做状态、
// 注册与事件转发。
type ShellService struct {
	app *application.App

	// workspace 用于取当前打开的工作台，不另开工作台实例。
	workspace *WorkspaceService

	// todo 用于随手记的待办模式保存，复用同一套与工作台/索引的交互。
	todo *TodoService

	// env / now 可在测试里替换，默认读真实环境与系统时间。
	env func(string) string
	now func() time.Time

	shortcuts shortcutManager
	ui        shellDesktop

	// started 保证 ApplicationStarted 只处理一次。
	startOnce sync.Once

	// mu 保护 current / registered / lastErr，回调与前端调用可能并发。
	mu sync.Mutex
	// current 是当前生效（或尝试生效）的快捷键，规范形式。
	current string
	// registered 表示 current 是否已在系统层注册成功。
	registered bool
	// lastErr 是最近一次注册失败的中文原因。
	lastErr string
	// notice 是启动回退等需要用户知晓、但快捷键仍可用的中文提示。
	notice string
}

// newShellService 创建一个尚未注入 app / 快捷键管理器的服务。
func newShellService(workspace *WorkspaceService) *ShellService {
	return &ShellService{
		workspace: workspace,
		env:       os.Getenv,
		now:       time.Now,
	}
}

// ServiceName 返回服务名，供 Wails 日志与绑定使用。
func (s *ShellService) ServiceName() string {
	return "ShellService"
}

// ServiceStartup 在应用启动时读取快捷键配置，并等应用真正启动完成后再注册。
//
// 关键：Wails 的 GlobalShortcut.Register 在 App.Run 之前调用只会排队，
// 系统层注册失败不会返回错误（见 v3.0.0 global_shortcut_manager.go）。
// 因此这里监听 Common.ApplicationStarted（此时 GlobalShortcutManager.started
// 已置位），才能拿到注册失败的真实错误。
func (s *ShellService) ServiceStartup(_ context.Context, _ application.ServiceOptions) error {
	accel, err := config.QuickNoteShortcut(s.env)
	if err != nil {
		log.Printf("kuibu: 读取随手记快捷键配置失败，使用默认值: %v", err)
		accel = config.DefaultQuickNoteShortcut
	}

	// 关键：任何路径都绝不注册未通过 normalizeShortcut 校验的加速键。
	// 配置里是不合法的加速键时（例如用户手写成 "N"），直接回退到默认值，
	// 但改写用户配置是不礼貌的，所以只提示、不落盘。
	notice := ""
	normalized, nerr := normalizeShortcut(accel)
	if nerr != nil {
		log.Printf("kuibu: 配置里的随手记快捷键 %q 不合法: %v", accel, nerr)
		notice = fmt.Sprintf(
			"配置里的随手记快捷键 %q 不合法（%v），已临时改用默认 %s，请在设置里重新设置",
			accel, nerr, shortcutDisplay(config.DefaultQuickNoteShortcut),
		)
		accel = config.DefaultQuickNoteShortcut
		normalized, nerr = normalizeShortcut(accel)
		if nerr != nil {
			// 默认值本身不可用属于代码错误：宁可不注册，也不注册非法加速键。
			log.Printf("kuibu: 默认随手记快捷键 %q 也不可用，本次不注册: %v", accel, nerr)
			s.mu.Lock()
			s.current = ""
			s.registered = false
			s.lastErr = fmt.Sprintf("默认随手记快捷键 %q 不可用：%v", accel, nerr)
			s.mu.Unlock()
			return nil
		}
	}

	s.mu.Lock()
	s.current = normalized
	s.notice = notice
	s.mu.Unlock()

	if s.app != nil {
		s.app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
			s.startOnce.Do(s.registerCurrent)
		})
	}
	return nil
}

// registerCurrent 在应用启动完成后尝试注册当前快捷键，失败只记录状态。
func (s *ShellService) registerCurrent() {
	s.mu.Lock()
	accel := s.current
	s.mu.Unlock()
	if accel == "" {
		return
	}
	if err := s.applyShortcut(accel, false); err != nil {
		log.Printf("kuibu: 随手记快捷键 %s 注册失败: %v", shortcutDisplay(accel), err)
		return
	}
	log.Printf("kuibu: 随手记快捷键 %s 已注册", shortcutDisplay(accel))
}

// GetShortcutStatus 返回当前快捷键与注册状态。
func (s *ShellService) GetShortcutStatus() ShortcutStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked()
}

// statusLocked 组装状态，调用方需持有 s.mu。
func (s *ShellService) statusLocked() ShortcutStatus {
	return ShortcutStatus{
		Accelerator: s.current,
		Display:     shortcutDisplay(s.current),
		Registered:  s.registered,
		Error:       s.lastErr,
		Notice:      s.notice,
	}
}

// SetQuickNoteShortcut 校验并尝试注册新快捷键，成功后才注销旧的并持久化。
//
// 失败时旧快捷键继续生效，返回中文错误，前端可直接展示。
func (s *ShellService) SetQuickNoteShortcut(accel string) (ShortcutStatus, error) {
	normalized, err := normalizeShortcut(accel)
	if err != nil {
		return s.GetShortcutStatus(), fmt.Errorf("快捷键不可用：%w", err)
	}
	if err := s.applyShortcut(normalized, true); err != nil {
		return s.GetShortcutStatus(), err
	}
	return s.GetShortcutStatus(), nil
}

// ResetQuickNoteShortcut 恢复默认快捷键（⌃⌥N）。
func (s *ShellService) ResetQuickNoteShortcut() (ShortcutStatus, error) {
	normalized, err := normalizeShortcut(config.DefaultQuickNoteShortcut)
	if err != nil {
		return s.GetShortcutStatus(), fmt.Errorf("默认快捷键不可用：%w", err)
	}
	if err := s.applyShortcut(normalized, false); err != nil {
		return s.GetShortcutStatus(), err
	}
	// 恢复默认是用户明确操作：清掉启动回退留下的提示。
	s.mu.Lock()
	s.notice = ""
	s.mu.Unlock()
	// 写空字符串表示省略该字段，下次读取即默认值。
	if err := config.SaveQuickNoteShortcut(s.env, ""); err != nil {
		log.Printf("kuibu: 恢复默认快捷键后写入配置失败: %v", err)
		return s.GetShortcutStatus(), fmt.Errorf("快捷键已恢复，但保存配置失败：%w", err)
	}
	return s.GetShortcutStatus(), nil
}

// applyShortcut 注册 accel 并在成功后替换旧快捷键；persist 为 true 时写入配置。
//
// 顺序是「先注册新的，成功后再注销旧的」：新快捷键被占用时旧快捷键继续可用。
func (s *ShellService) applyShortcut(accel string, persist bool) error {
	// 防御性校验：唯一的注册入口也自己校验一遍，绝不把非法加速键交给系统，
	// 否则 "N" 这类值会吞掉全局的单键输入。
	normalized, err := normalizeShortcut(accel)
	if err != nil {
		return fmt.Errorf("快捷键不可用：%w", err)
	}
	accel = normalized

	s.mu.Lock()
	old := s.current
	oldRegistered := s.registered
	if old == accel && oldRegistered {
		// 已经是这个快捷键且注册成功：清掉旧的注册错误，只保证配置落盘
		// （用户可能刚重置过配置）。否则上次失败的 lastErr 会一直挂着。
		s.lastErr = ""
		if persist {
			s.notice = ""
		}
		status := s.statusLocked()
		s.mu.Unlock()
		if persist {
			if err := config.SaveQuickNoteShortcut(s.env, accel); err != nil {
				return fmt.Errorf("快捷键已生效，但保存配置失败：%w", err)
			}
		}
		if s.ui != nil {
			s.ui.Emit(shortcutChangedEvent, status)
		}
		return nil
	}
	s.mu.Unlock()

	if s.shortcuts == nil {
		err := fmt.Errorf("快捷键服务不可用")
		s.setFailure(accel, err)
		return err
	}

	if err := s.shortcuts.Register(accel, s.toggleQuickNote); err != nil {
		wrapped := fmt.Errorf("随手记快捷键 %s 注册失败（可能被其他应用占用）：%w", shortcutDisplay(accel), err)
		s.setFailure(accel, wrapped)
		return wrapped
	}

	if oldRegistered && old != "" && old != accel {
		if err := s.shortcuts.Unregister(old); err != nil {
			// 旧快捷键注销失败不影响新快捷键生效，只记录。
			log.Printf("kuibu: 注销旧快捷键 %s 失败: %v", shortcutDisplay(old), err)
		}
	}

	s.mu.Lock()
	s.current = accel
	s.registered = true
	s.lastErr = ""
	if persist {
		s.notice = ""
	}
	status := s.statusLocked()
	s.mu.Unlock()

	if persist {
		if err := config.SaveQuickNoteShortcut(s.env, accel); err != nil {
			log.Printf("kuibu: 保存随手记快捷键失败: %v", err)
			return fmt.Errorf("快捷键已生效，但保存配置失败：%w", err)
		}
	}

	if s.ui != nil {
		s.ui.UpdateTrayShortcut(accel)
		s.ui.Emit(shortcutChangedEvent, status)
	}
	return nil
}

// setFailure 记录注册失败状态，同时保留旧快捷键继续生效。
//
// 首次注册（启动时）没有旧快捷键，current 直接设为失败的那个；
// 修改快捷键失败时 current 仍是旧的可用快捷键，避免界面显示成失效的新值。
func (s *ShellService) setFailure(accel string, err error) {
	s.mu.Lock()
	if !s.registered {
		s.current = accel
	}
	s.lastErr = err.Error()
	status := s.statusLocked()
	s.mu.Unlock()

	if s.ui != nil {
		s.ui.UpdateTrayShortcut(s.current)
		s.ui.Emit(shortcutChangedEvent, status)
	}
}

// toggleQuickNote 是全局快捷键的回调：已有小窗则聚焦，否则创建。
func (s *ShellService) toggleQuickNote() {
	if s.ui == nil {
		return
	}
	s.ui.ShowQuickNote()
}

// SaveQuickNote 把 text 保存到当前工作台的随手记收件箱。
//
// 未打开工作台时返回明确错误；保存成功后关闭小窗并发 quicknote:saved。
func (s *ShellService) SaveQuickNote(text string) (QuickNoteSaved, error) {
	if s.workspace == nil {
		return QuickNoteSaved{}, fmt.Errorf("尚未打开工作台：请先在主窗口打开工作台")
	}
	ws := s.workspace.workspace()
	if ws == nil {
		return QuickNoteSaved{}, fmt.Errorf("尚未打开工作台：请先在主窗口打开工作台")
	}

	note, err := quicknote.Save(ws, text, s.now())
	if err != nil {
		if errors.Is(err, quicknote.ErrEmpty) {
			return QuickNoteSaved{}, fmt.Errorf("随手记内容为空")
		}
		return QuickNoteSaved{}, fmt.Errorf("保存随手记失败：%w", err)
	}
	inbox, err := quicknote.EnsureInbox(ws)
	if err != nil {
		return QuickNoteSaved{}, fmt.Errorf("定位随手记收件箱失败：%w", err)
	}

	saved := QuickNoteSaved{ID: note.ID, InboxID: inbox.ID, Title: note.Title}
	if s.ui != nil {
		s.ui.Emit(quickNoteSavedEvent, saved)
		s.ui.CloseQuickNote()
	}
	return saved, nil
}

// SaveQuickTodo 把随手记内容按「待办模式」保存：第一行快速录入，其余行作正文。
//
// 保存成功后关闭小窗，并发 todo:changed，让主窗口的待办列表刷新。
func (s *ShellService) SaveQuickTodo(text string) (QuickTodoSaved, error) {
	if s.todo == nil {
		return QuickTodoSaved{}, fmt.Errorf("待办服务不可用")
	}
	todo, err := s.todo.createQuickTodo(text)
	if err != nil {
		if errors.Is(err, store.ErrInvalid) {
			return QuickTodoSaved{}, err
		}
		return QuickTodoSaved{}, fmt.Errorf("保存待办失败：%w", err)
	}

	saved := QuickTodoSaved{ID: todo.ID, Title: todo.Title}
	if s.ui != nil {
		s.ui.Emit(todoChangedEvent, TodoChangeEvent{IDs: []string{todo.ID}})
		s.ui.CloseQuickNote()
	}
	return saved, nil
}

// CloseQuickNote 关闭随手记小窗（销毁，不常驻隐藏）。
func (s *ShellService) CloseQuickNote() {
	if s.ui != nil {
		s.ui.CloseQuickNote()
	}
}

// OpenMainWindow 显示并聚焦主窗口。
func (s *ShellService) OpenMainWindow() {
	if s.ui != nil {
		s.ui.ShowMainWindow(false)
	}
}
