package index

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/axu4/kuibu/internal/store"
	"github.com/fsnotify/fsnotify"
)

// debounceDelay 是文件事件合并窗口，避免一次保存触发多次索引更新。
const debounceDelay = 200 * time.Millisecond

// Event 是一次防抖后的文件变化汇总，供 App 刷新界面。
//
// Changes/IDs 只包含笔记变化；TodoChanges/TodoIDs 只包含待办变化；
// PeopleChanges/PeopleIDs 只包含组员变化；ReportChanges/ReportIDs 只包含周报
// 变化，四者互不混入，App 可据此分别刷新。
type Event struct {
	// Changes 是每个受影响笔记的变化明细。
	Changes []Change `json:"changes"`
	// IDs 是发生变化（新增、更新或删除）的笔记 ID 列表，已去重。
	IDs []string `json:"ids"`
	// TodoChanges 是每个受影响待办的变化明细。
	TodoChanges []Change `json:"todoChanges,omitempty"`
	// TodoIDs 是发生变化的待办 ID 列表，已去重。
	TodoIDs []string `json:"todoIds,omitempty"`
	// PeopleChanges 是每个受影响组员的变化明细。
	PeopleChanges []Change `json:"peopleChanges,omitempty"`
	// PeopleIDs 是发生变化的组员 ID 列表，已去重。
	PeopleIDs []string `json:"peopleIds,omitempty"`
	// ReportChanges 是每个受影响周报的变化明细。
	ReportChanges []Change `json:"reportChanges,omitempty"`
	// ReportIDs 是发生变化的周报 ID 列表，已去重。
	ReportIDs []string `json:"reportIds,omitempty"`
}

// Watch 监听工作台的 notes/、todos/、people/ 与 reports/ 目录，把外部文件变化
// 增量同步进索引，并在每次同步后调用 onChange 通知调用方。
//
// 只关心非隐藏的 .md 文件，以 "." 开头的临时文件会被忽略（store 的原子写
// 会先写临时文件再 rename，macOS 上表现为 Create/Rename 事件）。reports/ 按
// 周/层级子目录递归监听，.history/ 被忽略。事件在 debounceDelay 内合并处理。
// ctx 取消后 Watch 返回 nil；watcher 自身出错时返回错误。onChange 为 nil 时
// 只同步索引，不做回调。
func Watch(ctx context.Context, ws *store.Workspace, idx *Index, onChange func(Event)) error {
	return watch(ctx, ws, idx, onChange, nil)
}

// watch 是 Watch 的实现；ready 非 nil 时会在监听器就绪后关闭它，供测试同步。
func watch(ctx context.Context, ws *store.Workspace, idx *Index, onChange func(Event), ready chan<- struct{}) error {
	if ws == nil || idx == nil {
		return fmt.Errorf("%w: 工作台或索引为空", store.ErrInvalid)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("创建文件监听器失败: %w", err)
	}
	defer watcher.Close()

	notesDir := ws.NotesDir()
	todosDir := ws.TodosDir()
	peopleDir := ws.PeopleDir()
	reportsDir := ws.ReportsDir()

	addDir := func(dir string) error {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		return watcher.Add(dir)
	}

	for _, dir := range []string{notesDir, todosDir, peopleDir, reportsDir} {
		if err := addDir(dir); err != nil {
			return fmt.Errorf("监听工作台目录失败: %w", err)
		}
	}

	// reports/ 按周/层级子目录递归监听，.history 跳过。
	err = filepath.WalkDir(reportsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if d.Name() == store.HistoryDirName || strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if path == reportsDir {
			return nil
		}
		return watcher.Add(path)
	})
	if err != nil {
		return fmt.Errorf("监听周报子目录失败: %w", err)
	}

	if ready != nil {
		close(ready)
	}

	pending := map[string]bool{}
	var timer *time.Timer
	var timerC <-chan time.Time

	stopTimer := func() {
		if timer == nil {
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timerC = nil
	}
	defer stopTimer()

	// flush 把累积的路径同步进索引，并回调变化。
	flush := func() {
		paths := make([]string, 0, len(pending))
		for p := range pending {
			paths = append(paths, p)
		}
		pending = map[string]bool{}
		if len(paths) == 0 {
			return
		}

		event := Event{}
		seenNotes := map[string]bool{}
		seenTodos := map[string]bool{}
		seenPeople := map[string]bool{}
		seenReports := map[string]bool{}
		for _, path := range paths {
			change, err := syncPath(idx, path)
			if err != nil {
				// 文件可能刚被删除或还在写入，忽略单个文件的瞬时错误，
				// 下一次事件或手动 Sync 会修复。
				continue
			}
			if change.Kind == ChangeUnchanged || change.ID == "" {
				continue
			}
			switch fileKind(ws, path) {
			case "todo":
				event.TodoChanges = append(event.TodoChanges, change)
				if !seenTodos[change.ID] {
					seenTodos[change.ID] = true
					event.TodoIDs = append(event.TodoIDs, change.ID)
				}
			case "people":
				event.PeopleChanges = append(event.PeopleChanges, change)
				if !seenPeople[change.ID] {
					seenPeople[change.ID] = true
					event.PeopleIDs = append(event.PeopleIDs, change.ID)
				}
			case "report":
				event.ReportChanges = append(event.ReportChanges, change)
				if !seenReports[change.ID] {
					seenReports[change.ID] = true
					event.ReportIDs = append(event.ReportIDs, change.ID)
				}
			default:
				event.Changes = append(event.Changes, change)
				if !seenNotes[change.ID] {
					seenNotes[change.ID] = true
					event.IDs = append(event.IDs, change.ID)
				}
			}
		}
		if (len(event.Changes) > 0 || len(event.TodoChanges) > 0 ||
			len(event.PeopleChanges) > 0 || len(event.ReportChanges) > 0) && onChange != nil {
			onChange(event)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil

		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			name := filepath.Clean(event.Name)
			// 新建 reports/ 子目录时补充监听。
			if event.Op&fsnotify.Create != 0 {
				if info, statErr := os.Stat(name); statErr == nil && info.IsDir() {
					if isReportSubdir(reportsDir, name) {
						_ = watcher.Add(name)
					}
				}
			}
			if !isIndexableEvent(ws, name) {
				continue
			}
			pending[name] = true
			if timer == nil {
				timer = time.NewTimer(debounceDelay)
			} else {
				stopTimer()
				timer.Reset(debounceDelay)
			}
			timerC = timer.C

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			if errors.Is(err, fsnotify.ErrEventOverflow) {
				// 事件队列溢出时做一次全量同步兜底。
				if _, syncErr := idx.Sync(ctx); syncErr != nil {
					return syncErr
				}
				continue
			}
			return fmt.Errorf("文件监听失败: %w", err)

		case <-timerC:
			timerC = nil
			flush()
		}
	}
}

// syncPath 按路径同步索引；文件不存在时删除索引项。
func syncPath(idx *Index, path string) (Change, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return idx.RemoveFile(path)
	}
	if err != nil {
		return Change{}, err
	}
	if info.IsDir() {
		return Change{Kind: ChangeUnchanged}, nil
	}
	return idx.UpsertFile(path)
}

// isIndexableEvent 判断事件是否是需要处理的 .md 文件变化，并排除 .history。
func isIndexableEvent(ws *store.Workspace, path string) bool {
	name := filepath.Base(path)
	if !isNoteFileName(name) {
		return false
	}
	if fileKind(ws, path) == "" {
		return false
	}
	for _, seg := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		if seg == store.HistoryDirName {
			return false
		}
	}
	return true
}

// isReportSubdir 判断 path 是否是 reports/ 下需要递归监听的子目录。
func isReportSubdir(reportsDir, path string) bool {
	path = filepath.Clean(path)
	reportsDir = filepath.Clean(reportsDir)
	if path == reportsDir {
		return false
	}
	if !strings.HasPrefix(path, reportsDir+string(filepath.Separator)) {
		return false
	}
	for _, seg := range strings.Split(strings.TrimPrefix(path, reportsDir+string(filepath.Separator)), string(filepath.Separator)) {
		if seg == store.HistoryDirName || strings.HasPrefix(seg, ".") {
			return false
		}
	}
	return true
}
