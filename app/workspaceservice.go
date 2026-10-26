package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/axu4/kuibu/internal/config"
	"github.com/axu4/kuibu/internal/index"
	"github.com/axu4/kuibu/internal/store"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// changedEvent 是外部文件变化时发给前端的事件名。
const changedEvent = "workspace:changed"

// todoChangedEvent 是待办文件变化时发给前端的事件名。
const todoChangedEvent = "todo:changed"

// assetPrefix 是工作台图片资源在前端的 URL 前缀。
const assetPrefix = "/workspace-assets/"

// TreeItem 是笔记树的节点，字段带 json tag 以便 wails3 generate bindings 生成 TS 类型。
type TreeItem struct {
	// ID 是笔记标识。
	ID string `json:"id"`
	// Title 是笔记标题。
	Title string `json:"title"`
	// Orphan 表示父笔记不存在，节点被当作根节点。
	Orphan bool `json:"orphan"`
	// Cycle 表示节点因父子环被降为根节点。
	Cycle bool `json:"cycle"`
	// Children 是子节点。
	Children []TreeItem `json:"children"`
}

// NoteData 是一条笔记的元数据与正文。
type NoteData struct {
	// ID 是笔记标识。
	ID string `json:"id"`
	// Title 是笔记标题。
	Title string `json:"title"`
	// Type 是笔记类型。
	Type string `json:"type"`
	// Parent 是父笔记 ID，空字符串表示根节点。
	Parent string `json:"parent"`
	// Order 是同级排序序号。
	Order int `json:"order"`
	// Tags 是标签列表。
	Tags []string `json:"tags"`
	// Created 是创建时间（RFC3339）。
	Created string `json:"created"`
	// Updated 是最后更新时间（RFC3339）。
	Updated string `json:"updated"`
	// Body 是正文。
	Body string `json:"body"`
}

// WorkspaceInfo 是当前工作台的状态。
type WorkspaceInfo struct {
	// Path 是工作台根目录，未打开时为空字符串。
	Path string `json:"path"`
}

// ChangeEvent 是 workspace:changed 事件的数据。
type ChangeEvent struct {
	// IDs 是发生变化的笔记 ID 列表。
	IDs []string `json:"ids"`
}

// TodoChangeEvent 是 todo:changed 事件的数据。
type TodoChangeEvent struct {
	// IDs 是发生变化的待办 ID 列表。
	IDs []string `json:"ids"`
}

// WorkspaceService 是暴露给前端的 Wails service。
//
// 所有文件读写都走 internal/store，索引与监听走 internal/index，
// 本层只做参数校验、状态持有与事件转发。
type WorkspaceService struct {
	// app 在 main 中注入，用于弹原生对话框与发事件。
	app *application.App

	// mu 保护 ws / idx / cancel，避免后台 goroutine 与前端调用并发访问。
	mu  sync.Mutex
	ws  *store.Workspace
	idx *index.Index
	// cancel 取消当前工作台的监听 goroutine。
	cancel context.CancelFunc
	// done 在监听 goroutine 退出后关闭，用于关闭索引前等待它收尾。
	done chan struct{}
}

// newWorkspaceService 创建一个尚未打开工作台的服务。
func newWorkspaceService() *WorkspaceService {
	return &WorkspaceService{}
}

// ServiceName 返回服务名，供 Wails 日志与绑定使用。
func (s *WorkspaceService) ServiceName() string {
	return "WorkspaceService"
}

// ServiceStartup 在应用启动时打开工作台、同步索引并开始监听文件变化。
func (s *WorkspaceService) ServiceStartup(_ context.Context, _ application.ServiceOptions) error {
	dir, err := config.Locate(os.Getenv)
	if err != nil {
		log.Printf("kuibu: 读取默认工作台失败: %v", err)
		return nil
	}
	if dir == "" {
		return nil
	}
	if err := s.open(dir); err != nil {
		// 默认工作台打不开时退回欢迎页，而不是让应用启动失败。
		log.Printf("kuibu: 打开工作台 %s 失败: %v", dir, err)
	}
	return nil
}

// ServiceShutdown 关闭索引与监听。
func (s *WorkspaceService) ServiceShutdown() error {
	s.close()
	return nil
}

// workspace 返回当前打开的工作台，未打开时为 nil。
//
// 仅供同模块的 ShellService（随手记保存）使用，不暴露给前端：
// 复用同一个工作台实例，不另开工作台、不自己读写文件。
func (s *WorkspaceService) workspace() *store.Workspace {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ws
}

// index 返回当前工作台的索引，未打开时为 nil。
//
// 仅供同模块的 TodoService 使用，复用同一个索引实例，不另开数据库。
func (s *WorkspaceService) index() *index.Index {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.idx
}

// emit 向前端发事件；app 尚未注入时安全忽略。
func (s *WorkspaceService) emit(name string, data any) {
	if s.app == nil {
		return
	}
	if data == nil {
		s.app.Event.Emit(name)
		return
	}
	s.app.Event.Emit(name, data)
}

// handleWatchEvent 把一次文件变化转换成前端事件。
//
// 抽成纯函数便于单测：只有对应 ID 列表非空时才发事件，笔记与待办互不混入。
func handleWatchEvent(e index.Event, emit func(name string, data any)) {
	if len(e.IDs) > 0 {
		emit(changedEvent, ChangeEvent{IDs: e.IDs})
	}
	if len(e.TodoIDs) > 0 {
		emit(todoChangedEvent, TodoChangeEvent{IDs: e.TodoIDs})
	}
}

// Current 返回当前工作台路径，未打开时为空字符串。
func (s *WorkspaceService) Current() WorkspaceInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ws == nil {
		return WorkspaceInfo{}
	}
	return WorkspaceInfo{Path: s.ws.Dir()}
}

// ChooseWorkspace 弹出目录选择对话框，打开或初始化工作台。
//
// 选中的目录不是工作台时，会询问用户是否在此初始化；成功后写入用户配置
// 作为默认工作台。用户取消时返回空路径且不报错。
func (s *WorkspaceService) ChooseWorkspace() (WorkspaceInfo, error) {
	dir, err := s.app.Dialog.OpenFile().
		CanChooseFiles(false).
		CanChooseDirectories(true).
		CanCreateDirectories(true).
		SetTitle("选择或新建工作台文件夹").
		SetButtonText("选择").
		PromptForSingleSelection()
	if err != nil {
		return WorkspaceInfo{}, fmt.Errorf("打开目录选择框失败: %w", err)
	}
	if dir == "" {
		return WorkspaceInfo{}, nil
	}

	if _, err := store.Open(dir); err != nil {
		if !errors.Is(err, store.ErrNotWorkspace) {
			return WorkspaceInfo{}, err
		}
		if !s.confirmInit(dir) {
			return WorkspaceInfo{}, nil
		}
		if _, err := store.Init(dir); err != nil {
			return WorkspaceInfo{}, err
		}
	}

	if err := s.open(dir); err != nil {
		return WorkspaceInfo{}, err
	}
	if err := config.SaveDefaultWorkspace(os.Getenv, dir); err != nil {
		log.Printf("kuibu: 保存默认工作台失败: %v", err)
	}

	s.mu.Lock()
	info := WorkspaceInfo{Path: s.ws.Dir()}
	s.mu.Unlock()
	return info, nil
}

// confirmInit 询问用户是否把非工作台目录初始化为工作台。
//
// Wails 的原生对话框是异步的（Show 只负责排队），这里用带缓冲的 channel
// 等待用户点击，避免在按钮回调触发前就返回 false。
func (s *WorkspaceService) confirmInit(dir string) bool {
	result := make(chan bool, 1)
	answer := func(v bool) {
		select {
		case result <- v:
		default:
		}
	}
	dialog := s.app.Dialog.Question().
		SetTitle("初始化工作台").
		SetMessage(fmt.Sprintf("%s 还不是跬步工作台，要在这里新建吗？", dir))
	dialog.AddButton("新建工作台").OnClick(func() { answer(true) })
	dialog.AddButton("取消").SetAsCancel().OnClick(func() { answer(false) })
	dialog.Show()
	return <-result
}

// Tree 返回嵌套的笔记树。
func (s *WorkspaceService) Tree() ([]TreeItem, error) {
	s.mu.Lock()
	ws := s.ws
	s.mu.Unlock()
	if ws == nil {
		return []TreeItem{}, nil
	}
	roots, err := ws.Tree()
	if err != nil {
		return nil, err
	}
	return toTreeItems(roots), nil
}

// Get 按 ID 返回笔记元数据与正文。
func (s *WorkspaceService) Get(id string) (NoteData, error) {
	s.mu.Lock()
	ws := s.ws
	s.mu.Unlock()
	if ws == nil {
		return NoteData{}, errNotOpen()
	}
	note, err := ws.Get(id)
	if err != nil {
		return NoteData{}, err
	}
	return toNoteData(note), nil
}

// Create 新建一条笔记，parent 为空表示根节点，返回新笔记。
func (s *WorkspaceService) Create(title, parent string) (NoteData, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "未命名笔记"
	}
	s.mu.Lock()
	ws := s.ws
	s.mu.Unlock()
	if ws == nil {
		return NoteData{}, errNotOpen()
	}
	note, err := ws.Create(store.NewNote{Title: title, Parent: parent})
	if err != nil {
		return NoteData{}, err
	}
	return toNoteData(note), nil
}

// SaveBody 保存笔记正文。
func (s *WorkspaceService) SaveBody(id, body string) error {
	s.mu.Lock()
	ws := s.ws
	s.mu.Unlock()
	if ws == nil {
		return errNotOpen()
	}
	_, err := ws.UpdateBody(id, body)
	return err
}

// Rename 修改笔记标题。
func (s *WorkspaceService) Rename(id, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("%w: 标题不能为空", store.ErrInvalid)
	}
	s.mu.Lock()
	ws := s.ws
	s.mu.Unlock()
	if ws == nil {
		return errNotOpen()
	}
	_, err := ws.Update(id, func(n *store.Note) { n.Title = title })
	return err
}

// Move 把笔记移到 parent 下的第 pos 个位置；parent 为空表示根层级。
func (s *WorkspaceService) Move(id, parent string, pos int) error {
	s.mu.Lock()
	ws := s.ws
	s.mu.Unlock()
	if ws == nil {
		return errNotOpen()
	}
	return ws.Move(id, parent, pos)
}

// Delete 删除笔记；recursive 为 true 时连同子笔记一起删除。
func (s *WorkspaceService) Delete(id string, recursive bool) error {
	s.mu.Lock()
	ws := s.ws
	s.mu.Unlock()
	if ws == nil {
		return errNotOpen()
	}
	return ws.Delete(id, recursive)
}

// errNotOpen 表示尚未打开工作台。
func errNotOpen() error {
	return fmt.Errorf("%w: 尚未打开工作台", store.ErrNotWorkspace)
}

// open 打开工作台、建立索引并启动监听。已打开时会先关闭旧工作台。
func (s *WorkspaceService) open(dir string) error {
	ws, err := store.Open(dir)
	if err != nil {
		return err
	}
	idx, err := index.Open(ws)
	if err != nil {
		return err
	}
	if _, err := idx.Sync(context.Background()); err != nil {
		log.Printf("kuibu: 同步索引失败（不影响数据）: %v", err)
	}

	s.close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.mu.Lock()
	s.ws = ws
	s.idx = idx
	s.cancel = cancel
	s.done = done
	s.mu.Unlock()

	go func() {
		defer close(done)
		err := index.Watch(ctx, ws, idx, func(e index.Event) {
			handleWatchEvent(e, s.emit)
		})
		if err != nil && ctx.Err() == nil {
			log.Printf("kuibu: 监听工作台失败: %v", err)
		}
	}()
	return nil
}

// close 停止监听并关闭索引。
func (s *WorkspaceService) close() {
	s.mu.Lock()
	cancel := s.cancel
	idx := s.idx
	done := s.done
	s.ws = nil
	s.idx = nil
	s.cancel = nil
	s.done = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	if idx != nil {
		_ = idx.Close()
	}
}

// assetMiddleware 把 /workspace-assets/<file> 映射到 <工作台>/assets/<file>。
//
// 只接受 assets 目录下的普通文件名，拒绝任何路径分隔符与 .. 片段，
// 避免路径穿越读取工作台以外的文件。
func (s *WorkspaceService) assetMiddleware() application.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, assetPrefix) {
				next.ServeHTTP(w, r)
				return
			}
			name := strings.TrimPrefix(r.URL.Path, assetPrefix)
			if !validAssetName(name) {
				http.NotFound(w, r)
				return
			}
			s.mu.Lock()
			ws := s.ws
			s.mu.Unlock()
			if ws == nil {
				http.NotFound(w, r)
				return
			}
			file := filepath.Join(ws.AssetsDir(), filepath.FromSlash(name))
			if !strings.HasPrefix(file, ws.AssetsDir()+string(filepath.Separator)) {
				http.NotFound(w, r)
				return
			}
			// 允许浏览器缓存，资源文件名带内容哈希前缀，内容变化即换名。
			w.Header().Set("Cache-Control", "private, max-age=3600")
			http.ServeFile(w, r, file)
		})
	}
}

// validAssetName 判断资源名是否安全：非空、无路径分隔符、无 ..、不是绝对路径。
func validAssetName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return false
	}
	return path.Base(name) == name
}

// toTreeItems 递归把 store 树节点转为前端结构。
func toTreeItems(nodes []*store.TreeNode) []TreeItem {
	out := make([]TreeItem, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, TreeItem{
			ID:       n.Note.ID,
			Title:    n.Note.Title,
			Orphan:   n.Orphan,
			Cycle:    n.Cycle,
			Children: toTreeItems(n.Children),
		})
	}
	return out
}

// toNoteData 把 store.Note 转为前端结构。
func toNoteData(n *store.Note) NoteData {
	tags := n.Tags
	if tags == nil {
		tags = []string{}
	}
	return NoteData{
		ID:      n.ID,
		Title:   n.Title,
		Type:    n.Type,
		Parent:  n.Parent,
		Order:   n.Order,
		Tags:    tags,
		Created: formatTime(n.Created),
		Updated: formatTime(n.Updated),
		Body:    n.Body,
	}
}

// formatTime 把时间格式化为 RFC3339，零值输出空字符串。
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
