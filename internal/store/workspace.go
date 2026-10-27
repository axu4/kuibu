package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// 工作台目录布局常量。
const (
	// notesDirName 是笔记目录名，笔记平铺存放于其中。
	notesDirName = "notes"
	// todosDirName 是待办目录名，待办平铺存放于其中。
	todosDirName = "todos"
	// peopleDirName 是组员目录名，组员平铺存放于其中。
	peopleDirName = "people"
	// reportsDirName 是周报目录名，按周/层级归档。
	reportsDirName = "reports"
	// assetsDirName 是资源目录名，图片等附件存放于此。
	assetsDirName = "assets"
	// metaDirName 是元数据目录名，存放配置与索引。
	metaDirName = ".kuibu"
	// configFileName 是工作台配置文件名。
	configFileName = "config.json"
	// configVersion 是当前工作台配置版本。
	configVersion = 1
)

// config 是 .kuibu/config.json 的内容。
type config struct {
	Version int `json:"version"`
	// ReportWeekday 是周报日（1=周一…7=周日），缺省或 0 视为 4（周四）。
	ReportWeekday int `json:"reportWeekday,omitempty"`
	// ReportMonthDay 是月报收集日（1-31，超过当月天数按月末），缺省或 0 视为 28。
	ReportMonthDay int `json:"reportMonthDay,omitempty"`
	// MeetingWeekday 是周会日（1=周一…7=周日），缺省或 0 视为 1（周一）。
	MeetingWeekday int `json:"meetingWeekday,omitempty"`
}

// Workspace 代表一个已打开的工作台目录。
type Workspace struct {
	// dir 是工作台根目录的绝对路径。
	dir string
}

// Init 在 dir 处初始化一个新工作台。
//
// dir 可以不存在，也可以已存在但必须为空。若 dir 已经是工作台则返回
// ErrAlreadyInit；若非空且不是工作台则返回 ErrInvalid。初始化会创建
// notes/、todos/、assets/ 与 .kuibu/ 目录，并写入 .kuibu/config.json。
func Init(dir string) (*Workspace, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("解析工作台路径失败: %w", err)
	}

	info, statErr := os.Stat(abs)
	switch {
	case statErr == nil:
		if !info.IsDir() {
			return nil, fmt.Errorf("%w: %s 不是目录", ErrInvalid, abs)
		}
		ok, err := isWorkspace(abs)
		if err != nil {
			return nil, err
		}
		if ok {
			return nil, fmt.Errorf("%w: %s", ErrAlreadyInit, abs)
		}
		empty, err := isEmptyDir(abs)
		if err != nil {
			return nil, err
		}
		if !empty {
			return nil, fmt.Errorf("%w: %s 非空且不是工作台", ErrInvalid, abs)
		}
	case errors.Is(statErr, os.ErrNotExist):
		if err := os.MkdirAll(abs, 0o755); err != nil {
			return nil, fmt.Errorf("创建工作台目录失败: %w", err)
		}
	default:
		return nil, fmt.Errorf("检查工作台目录失败: %w", statErr)
	}

	for _, name := range []string{notesDirName, todosDirName, peopleDirName, reportsDirName, assetsDirName, metaDirName} {
		if err := os.MkdirAll(filepath.Join(abs, name), 0o755); err != nil {
			return nil, fmt.Errorf("创建工作台子目录 %s 失败: %w", name, err)
		}
	}

	data, err := json.Marshal(config{Version: configVersion})
	if err != nil {
		return nil, fmt.Errorf("序列化工作台配置失败: %w", err)
	}
	data = append(data, '\n')
	if err := writeFileAtomic(filepath.Join(abs, metaDirName, configFileName), data); err != nil {
		return nil, fmt.Errorf("写入工作台配置失败: %w", err)
	}

	return &Workspace{dir: abs}, nil
}

// Open 打开 dir 处的工作台。dir 必须包含 .kuibu/config.json，否则返回
// ErrNotWorkspace。
func Open(dir string) (*Workspace, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("解析工作台路径失败: %w", err)
	}
	ok, err := isWorkspace(abs)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotWorkspace, abs)
	}
	return &Workspace{dir: abs}, nil
}

// Find 从 start 开始向上逐级查找工作台，返回找到的工作台。
// 找不到时返回 ErrNotWorkspace。
func Find(start string) (*Workspace, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return nil, fmt.Errorf("解析起始路径失败: %w", err)
	}

	dir := abs
	for {
		ok, err := isWorkspace(dir)
		if err != nil {
			return nil, err
		}
		if ok {
			return &Workspace{dir: dir}, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, fmt.Errorf("%w: 从 %s 向上未找到工作台", ErrNotWorkspace, abs)
		}
		dir = parent
	}
}

// Dir 返回工作台根目录的绝对路径。
func (w *Workspace) Dir() string {
	return w.dir
}

// NotesDir 返回笔记目录的绝对路径。
func (w *Workspace) NotesDir() string {
	return filepath.Join(w.dir, notesDirName)
}

// TodosDir 返回待办目录的绝对路径。
func (w *Workspace) TodosDir() string {
	return filepath.Join(w.dir, todosDirName)
}

// PeopleDir 返回组员目录的绝对路径。
func (w *Workspace) PeopleDir() string {
	return filepath.Join(w.dir, peopleDirName)
}

// ReportsDir 返回周报目录的绝对路径。
func (w *Workspace) ReportsDir() string {
	return filepath.Join(w.dir, reportsDirName)
}

// AssetsDir 返回资源目录的绝对路径。
func (w *Workspace) AssetsDir() string {
	return filepath.Join(w.dir, assetsDirName)
}

// MetaDir 返回 .kuibu 元数据目录的绝对路径。
func (w *Workspace) MetaDir() string {
	return filepath.Join(w.dir, metaDirName)
}

// isWorkspace 判断 dir 是否包含 .kuibu/config.json。
func isWorkspace(dir string) (bool, error) {
	path := filepath.Join(dir, metaDirName, configFileName)
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("检查工作台配置失败: %w", err)
	}
	if info.IsDir() {
		return false, nil
	}
	return true, nil
}

// isEmptyDir 判断目录是否为空。
func isEmptyDir(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("读取目录失败: %w", err)
	}
	return len(entries) == 0, nil
}

// ListWorkspaceNotes 返回笔记目录下所有 .md 文件的绝对路径，按文件名排序。
// 这是索引增量重建时枚举文件的入口。
func (w *Workspace) ListWorkspaceNotes() ([]string, error) {
	entries, err := os.ReadDir(w.NotesDir())
	if err != nil {
		return nil, fmt.Errorf("读取笔记目录失败: %w", err)
	}
	var paths []string
	for _, e := range entries {
		// 跳过隐藏文件（如 macOS 的 ._xxx.md、编辑器临时文件）。
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !strings.HasSuffix(e.Name(), noteExt) {
			continue
		}
		paths = append(paths, filepath.Join(w.NotesDir(), e.Name()))
	}
	sort.Strings(paths)
	return paths, nil
}

// ListWorkspaceTodos 返回待办目录下所有 .md 文件的绝对路径，按文件名排序。
// 老工作台可能还没有 todos/ 目录，此时视为空列表而不是报错。
func (w *Workspace) ListWorkspaceTodos() ([]string, error) {
	entries, err := os.ReadDir(w.TodosDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("读取待办目录失败: %w", err)
	}
	var paths []string
	for _, e := range entries {
		// 跳过隐藏文件与非 .md 文件，规则与笔记一致。
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !strings.HasSuffix(e.Name(), noteExt) {
			continue
		}
		paths = append(paths, filepath.Join(w.TodosDir(), e.Name()))
	}
	sort.Strings(paths)
	return paths, nil
}

// ListWorkspacePeople 返回组员目录下所有 .md 文件的绝对路径，按文件名排序。
// 老工作台可能还没有 people/ 目录，此时视为空列表而不是报错。
func (w *Workspace) ListWorkspacePeople() ([]string, error) {
	entries, err := os.ReadDir(w.PeopleDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("读取组员目录失败: %w", err)
	}
	var paths []string
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !strings.HasSuffix(e.Name(), noteExt) {
			continue
		}
		paths = append(paths, filepath.Join(w.PeopleDir(), e.Name()))
	}
	sort.Strings(paths)
	return paths, nil
}

// ListWorkspaceReports 返回 reports/ 下所有周报 .md 文件的绝对路径（递归），
// 按路径排序；.history/ 与隐藏文件不参与。老工作台可能还没有 reports/ 目录，
// 此时视为空列表而不是报错。
func (w *Workspace) ListWorkspaceReports() ([]string, error) {
	var paths []string
	err := filepath.WalkDir(w.ReportsDir(), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			if d.Name() == historyDirName || strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || !strings.HasSuffix(d.Name(), noteExt) {
			return nil
		}
		paths = append(paths, filepath.Clean(path))
		return nil
	})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("读取周报目录失败: %w", err)
	}
	sort.Strings(paths)
	return paths, nil
}
