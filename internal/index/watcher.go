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
// Changes/IDs 只包含笔记变化；TodoChanges/TodoIDs 只包含待办变化，
// 两者互不混入，App 可据此分别刷新笔记树与待办列表。
type Event struct {
	// Changes 是每个受影响笔记的变化明细。
	Changes []Change `json:"changes"`
	// IDs 是发生变化（新增、更新或删除）的笔记 ID 列表，已去重。
	IDs []string `json:"ids"`
	// TodoChanges 是每个受影响待办的变化明细。
	TodoChanges []Change `json:"todoChanges,omitempty"`
	// TodoIDs 是发生变化的待办 ID 列表，已去重。
	TodoIDs []string `json:"todoIds,omitempty"`
}

// Watch 监听工作台的 notes/ 与 todos/ 目录，把外部文件变化增量同步进索引，
// 并在每次同步后调用 onChange 通知调用方。
//
// 只关心非隐藏的 .md 文件，以 "." 开头的临时文件会被忽略（store 的原子写
// 会先写临时文件再 rename，macOS 上表现为 Create/Rename 事件）。事件在
// debounceDelay 内合并处理。ctx 取消后 Watch 返回 nil；watcher 自身出错时
// 返回错误。onChange 为 nil 时只同步索引，不做回调。
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
	// 老工作台可能还没有 todos/ 目录，先建出来以便监听。
	if err := os.MkdirAll(todosDir, 0o755); err != nil {
		return fmt.Errorf("创建待办目录失败: %w", err)
	}
	if err := watcher.Add(notesDir); err != nil {
		return fmt.Errorf("监听笔记目录失败: %w", err)
	}
	if err := watcher.Add(todosDir); err != nil {
		return fmt.Errorf("监听待办目录失败: %w", err)
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
			if isTodoPath(ws, path) {
				event.TodoChanges = append(event.TodoChanges, change)
				if !seenTodos[change.ID] {
					seenTodos[change.ID] = true
					event.TodoIDs = append(event.TodoIDs, change.ID)
				}
			} else {
				event.Changes = append(event.Changes, change)
				if !seenNotes[change.ID] {
					seenNotes[change.ID] = true
					event.IDs = append(event.IDs, change.ID)
				}
			}
		}
		if (len(event.Changes) > 0 || len(event.TodoChanges) > 0) && onChange != nil {
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
			if !isWatchedEvent(event, notesDir, todosDir) {
				continue
			}
			pending[filepath.Clean(event.Name)] = true
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

// isWatchedEvent 判断事件是否是需要处理的笔记或待办文件变化。
func isWatchedEvent(event fsnotify.Event, notesDir, todosDir string) bool {
	// 只处理直接位于 notes/ 或 todos/ 下的文件，忽略 Chmod 之类的纯元数据事件。
	if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
		return false
	}
	name := filepath.Base(event.Name)
	if !isNoteFileName(name) {
		return false
	}
	// 兼容监听目录内的子路径（正常情况下 notes/ 与 todos/ 都是平铺的）。
	dir := filepath.Dir(filepath.Clean(event.Name))
	if strings.EqualFold(dir, filepath.Clean(notesDir)) {
		return true
	}
	return strings.EqualFold(dir, filepath.Clean(todosDir))
}
