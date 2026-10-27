package main

import (
	"context"
	"fmt"
	"log"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/axu4/kuibu/internal/core"
	"github.com/axu4/kuibu/internal/index"
	"github.com/axu4/kuibu/internal/store"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// 待办列表视图取值。
const (
	// todoViewToday 是「今天」：今天截止 + 已逾期。
	todoViewToday = "today"
	// todoViewWeek 是「本周」：本周一到本周日截止。
	todoViewWeek = "week"
	// todoViewAll 是「全部未完成」。
	todoViewAll = "all"
	// todoViewDone 是「已完成」，含已取消。
	todoViewDone = "done"
)

// TodoLink 是待办链接的前端结构。
type TodoLink struct {
	// URL 是链接地址。
	URL string `json:"url"`
	// Title 是链接标题，可为空。
	Title string `json:"title"`
}

// TodoAttachment 是待办附件的前端结构。
type TodoAttachment struct {
	// Path 是相对待办文件的资源路径，形如 ../assets/xxx。
	Path string `json:"path"`
	// Name 是显示用的文件名。
	Name string `json:"name"`
}

// TodoItem 是给前端的待办结构，元数据来自索引，正文与子任务按需补齐。
type TodoItem struct {
	// ID 是待办标识。
	ID string `json:"id"`
	// Title 是标题。
	Title string `json:"title"`
	// Status 是状态：todo/doing/done/canceled。
	Status string `json:"status"`
	// Due 是截止日期字符串（"2026-10-30" 或 "2026-10-30 15:00"），可空。
	Due string `json:"due"`
	// DueAt 是截止时刻（RFC3339），可空。
	DueAt string `json:"dueAt"`
	// DoneAt 是完成时刻（RFC3339），未完成时为空。
	DoneAt string `json:"doneAt"`
	// Overdue 表示未完成且已过截止时刻。
	Overdue bool `json:"overdue"`
	// Tags 是标签列表。
	Tags []string `json:"tags"`
	// Parent 是父待办 ID，空字符串表示顶层。
	Parent string `json:"parent"`
	// Order 是同级排序序号。
	Order int `json:"order"`
	// Body 是详情 Markdown，列表接口返回空字符串。
	Body string `json:"body"`
	// Links 是链接列表。
	Links []TodoLink `json:"links"`
	// Attachments 是附件列表。
	Attachments []TodoAttachment `json:"attachments"`
	// Source 是来源笔记 ID，可空。
	Source string `json:"source"`
	// SourceTitle 是来源笔记标题，仅在 Get 中填充。
	SourceTitle string `json:"sourceTitle"`
	// Children 是子任务（仅列表的顶层项与 Get 填充）。
	Children []TodoItem `json:"children"`
	// ChildDone 是已完成的直接子任务数。
	ChildDone int `json:"childDone"`
	// ChildTotal 是直接子任务总数。
	ChildTotal int `json:"childTotal"`
	// Created 是创建时刻（RFC3339）。
	Created string `json:"created"`
	// Updated 是最后更新时刻（RFC3339）。
	Updated string `json:"updated"`
}

// TodoListFilter 是待办列表的过滤条件。
type TodoListFilter struct {
	// View 是视图：today/week/all/done，空等同 all。
	View string `json:"view"`
	// Tag 是标签筛选，空表示不过滤。
	Tag string `json:"tag"`
	// Query 是按标题/正文的检索词，空表示不过滤。
	Query string `json:"query"`
}

// TodoTag 是标签及其未完成数量。
type TodoTag struct {
	// Name 是标签名。
	Name string `json:"name"`
	// Count 是带该标签的未完成待办数。
	Count int `json:"count"`
}

// QuickAddPreview 是快速录入的实时预览结果。
type QuickAddPreview struct {
	// Title 是解析后的标题。
	Title string `json:"title"`
	// Due 是解析出的截止日期字符串，可空。
	Due string `json:"due"`
	// Tags 是解析出的标签。
	Tags []string `json:"tags"`
}

// QuickTodoSaved 是随手记待办模式保存成功后的结果。
type QuickTodoSaved struct {
	// ID 是新建待办的 ID。
	ID string `json:"id"`
	// Title 是待办标题。
	Title string `json:"title"`
}

// TodoService 是暴露给前端的待办服务。
//
// 所有读写都走 internal/store 与 internal/index，本层只做参数校验、
// 解析、聚合与系统能力（文件对话框、打开文件/链接）的封装。
type TodoService struct {
	// workspace 用于复用当前打开的工作台与索引实例。
	workspace *WorkspaceService

	// app 在 main 中注入，用于弹原生对话框、打开文件与发事件。
	app *application.App

	// syncMu 保护 lastSync，把「读前同步」做成短时间内的 single-flight：
	// 一次界面刷新会并发发起多个查询，没必要每个查询都跑一遍增量同步。
	syncMu   sync.Mutex
	lastSync time.Time

	// now 可在测试里替换，默认系统时间；使用时统一转换到 store.TodoLocation。
	now func() time.Time
	// openFile / openURL 在测试里替换，默认用系统默认程序打开。
	openFile func(string) error
	openURL  func(string) error
	// pickFiles 在测试里替换，默认弹原生多选文件对话框。
	pickFiles func() ([]string, error)
}

// newTodoService 创建待办服务，复用 WorkspaceService 的工作台与索引。
func newTodoService(workspace *WorkspaceService) *TodoService {
	return &TodoService{
		workspace: workspace,
		now:       time.Now,
	}
}

// ServiceName 返回服务名，供 Wails 日志与绑定使用。
func (s *TodoService) ServiceName() string {
	return "TodoService"
}

// clock 返回转换到待办时区的当前时间。
func (s *TodoService) clock() time.Time {
	now := time.Now
	if s.now != nil {
		now = s.now
	}
	return now().In(store.TodoLocation())
}

// syncInterval 是同一次界面刷新里跳过重复同步的时间窗口。
const syncInterval = 50 * time.Millisecond

// instances 返回当前工作台与索引，并先做一次增量同步。
func (s *TodoService) instances() (*store.Workspace, *index.Index, error) {
	if s.workspace == nil {
		return nil, nil, errNotOpen()
	}
	ws := s.workspace.workspace()
	idx := s.workspace.index()
	if ws == nil || idx == nil {
		return nil, nil, errNotOpen()
	}
	s.syncIndex(idx, false)
	return ws, idx, nil
}

// syncIndex 增量同步索引；force 为 false 时，刚同步过则跳过。
//
// 同步失败不影响本次读取：索引里的旧数据仍可展示，文件是事实来源。
func (s *TodoService) syncIndex(idx *index.Index, force bool) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	if !force && time.Since(s.lastSync) < syncInterval {
		return
	}
	s.lastSync = time.Now()
	if _, err := idx.Sync(context.Background()); err != nil {
		log.Printf("kuibu: 同步索引失败（继续使用旧索引）: %v", err)
	}
}

// Preview 解析快速录入文本，供输入框实时预览。
func (s *TodoService) Preview(text string) QuickAddPreview {
	qa := core.ParseQuickAdd(text, s.clock())
	return QuickAddPreview{Title: qa.Title, Due: qa.Due.String(), Tags: qa.Tags}
}

// Create 按快速录入规则新建一条待办；parent 为空表示顶层。
func (s *TodoService) Create(text, parent string) (TodoItem, error) {
	ws, idx, err := s.instances()
	if err != nil {
		return TodoItem{}, err
	}
	todo, err := s.createTodo(ws, text, "", parent, s.clock())
	if err != nil {
		return TodoItem{}, err
	}
	return s.getItem(ws, idx, todo.ID)
}

// createTodo 是 Create 与随手记共用的创建逻辑。
//
// 第一段文本按 core.ParseQuickAdd 解析出标题/截止/标签，body 作为详情正文。
func (s *TodoService) createTodo(ws *store.Workspace, text, body, parent string, now time.Time) (*store.Todo, error) {
	qa := core.ParseQuickAdd(text, now)
	title := strings.TrimSpace(qa.Title)
	if title == "" {
		title = strings.TrimSpace(text)
	}
	if title == "" {
		return nil, fmt.Errorf("%w: 待办标题不能为空", store.ErrInvalid)
	}
	return ws.CreateTodo(store.NewTodo{
		Title:  title,
		Body:   body,
		Parent: parent,
		Tags:   qa.Tags,
		Due:    qa.Due,
		Now:    now,
	})
}

// createQuickTodo 把随手记内容保存成待办：第一行快速录入，其余行作为正文。
func (s *TodoService) createQuickTodo(text string) (*store.Todo, error) {
	ws, _, err := s.instances()
	if err != nil {
		return nil, err
	}
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.TrimSpace(normalized)
	if normalized == "" {
		return nil, fmt.Errorf("%w: 随手记内容为空", store.ErrInvalid)
	}
	lines := strings.Split(normalized, "\n")
	first := strings.TrimSpace(lines[0])
	body := strings.TrimSpace(strings.Join(lines[1:], "\n"))
	if first == "" {
		// 第一行为空时，把正文第一段当标题，避免生成空标题待办。
		first = body
		body = ""
	}
	return s.createTodo(ws, first, body, "", s.clock())
}

// List 按视图/标签/检索词返回待办列表。
func (s *TodoService) List(filter TodoListFilter) ([]TodoItem, error) {
	_, idx, err := s.instances()
	if err != nil {
		return nil, err
	}

	view := filter.View
	if view == "" {
		view = todoViewAll
	}
	metaFilter := index.TodoFilter{Statuses: core.TodoViewStatuses(view), Query: filter.Query}
	if filter.Tag != "" {
		metaFilter.Tags = []string{filter.Tag}
	}
	metas, err := idx.ListTodos(metaFilter)
	if err != nil {
		return nil, err
	}

	// 再按视图做日期过滤（今天含逾期、本周），规则在核心库。
	matched := make([]index.TodoMeta, 0, len(metas))
	now := s.clock()
	for _, m := range metas {
		ok, err := core.InTodoView(view, m.Due, now)
		if err != nil {
			return nil, err
		}
		if ok {
			matched = append(matched, m)
		}
	}

	// 子任务进度与缩进展示需要全部待办的父子关系。
	all, err := idx.ListTodos(index.TodoFilter{})
	if err != nil {
		return nil, err
	}
	allChildren := map[string][]index.TodoMeta{}
	for _, m := range all {
		allChildren[m.Parent] = append(allChildren[m.Parent], m)
	}

	matchedIDs := make(map[string]bool, len(matched))
	for _, m := range matched {
		matchedIDs[m.ID] = true
	}
	matchedChildren := map[string][]index.TodoMeta{}
	for _, m := range matched {
		matchedChildren[m.Parent] = append(matchedChildren[m.Parent], m)
	}

	var roots []TodoItem
	for _, m := range matched {
		if m.Parent != "" && matchedIDs[m.Parent] {
			continue
		}
		roots = append(roots, s.listItem(m, matchedChildren, allChildren))
	}
	if roots == nil {
		roots = []TodoItem{}
	}
	return roots, nil
}

// listItem 把一条索引元数据转成列表项，并递归挂上匹配到的子任务。
func (s *TodoService) listItem(meta index.TodoMeta, matched, all map[string][]index.TodoMeta) TodoItem {
	item := s.itemFromMeta(meta)
	done, total := 0, 0
	for _, c := range all[meta.ID] {
		total++
		if c.Status == store.StatusDone || c.Status == store.StatusCanceled {
			done++
		}
	}
	item.ChildDone = done
	item.ChildTotal = total
	for _, c := range matched[meta.ID] {
		item.Children = append(item.Children, s.listItem(c, matched, all))
	}
	if item.Children == nil {
		item.Children = []TodoItem{}
	}
	return item
}

// itemFromMeta 转换元数据字段，正文留空。
func (s *TodoService) itemFromMeta(m index.TodoMeta) TodoItem {
	tags := m.Tags
	if tags == nil {
		tags = []string{}
	}
	links := make([]TodoLink, 0, len(m.Links))
	for _, l := range m.Links {
		links = append(links, TodoLink{URL: l.URL, Title: l.Title})
	}
	attachments := make([]TodoAttachment, 0, len(m.Attachments))
	for _, a := range m.Attachments {
		attachments = append(attachments, TodoAttachment{Path: a, Name: path.Base(a)})
	}
	item := TodoItem{
		ID:          m.ID,
		Title:       m.Title,
		Status:      m.Status,
		Due:         m.Due,
		Tags:        tags,
		Parent:      m.Parent,
		Order:       m.Order,
		Body:        "",
		Links:       links,
		Attachments: attachments,
		Source:      m.Source,
		Children:    []TodoItem{},
		Created:     formatTime(m.Created),
		Updated:     formatTime(m.Updated),
	}
	if m.DueAt != nil {
		item.DueAt = formatTime(*m.DueAt)
		item.Overdue = core.TodoOverdue(m.Status, m.DueAt, s.clock())
	}
	if m.DoneAt != nil {
		item.DoneAt = formatTime(*m.DoneAt)
	}
	return item
}

// Get 返回一条待办的完整详情：正文、子任务、链接、附件与来源笔记标题。
func (s *TodoService) Get(id string) (TodoItem, error) {
	ws, idx, err := s.instances()
	if err != nil {
		return TodoItem{}, err
	}
	return s.getItem(ws, idx, id)
}

// getItem 是 Get 的内部实现，复用已取到的工作台与索引。
//
// 索引可能落后于刚写入的文件（监听有 200ms 防抖），索引里查不到时
// 先做一次增量同步再重试，避免「刚创建就打开」拿不到数据。
func (s *TodoService) getItem(ws *store.Workspace, idx *index.Index, id string) (TodoItem, error) {
	// 刚写完的文件要等监听防抖（200ms）才进索引，这里强制同步一次，
	// 保证「创建/修改后立即读取」拿到的是最新元数据。
	s.syncIndex(idx, true)
	meta, err := idx.GetTodo(id)
	if err != nil {
		return TodoItem{}, err
	}
	todo, err := ws.GetTodo(id)
	if err != nil {
		return TodoItem{}, err
	}
	item := s.itemFromMeta(meta)
	item.Body = todo.Body

	children, err := idx.TodoChildren(id)
	if err != nil {
		return TodoItem{}, err
	}
	for _, c := range children {
		item.Children = append(item.Children, s.itemFromMeta(c))
	}
	item.ChildTotal = len(children)
	item.ChildDone = 0
	for _, c := range children {
		if c.Status == store.StatusDone || c.Status == store.StatusCanceled {
			item.ChildDone++
		}
	}

	if item.Source != "" {
		if src, err := idx.Get(item.Source); err == nil {
			item.SourceTitle = src.Title
		}
	}
	return item, nil
}

// Tags 返回未完成待办的标签及数量，按数量降序、名称升序。
func (s *TodoService) Tags() ([]TodoTag, error) {
	_, idx, err := s.instances()
	if err != nil {
		return nil, err
	}
	metas, err := idx.ListTodos(index.TodoFilter{Statuses: []string{store.StatusTodo, store.StatusDoing}})
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, m := range metas {
		for _, t := range m.Tags {
			counts[t]++
		}
	}
	tags := make([]TodoTag, 0, len(counts))
	for name, count := range counts {
		tags = append(tags, TodoTag{Name: name, Count: count})
	}
	// 数量降序，同数量按名称升序，保证界面稳定。
	for i := 1; i < len(tags); i++ {
		for j := i; j > 0; j-- {
			a, b := tags[j-1], tags[j]
			if a.Count > b.Count || (a.Count == b.Count && a.Name <= b.Name) {
				break
			}
			tags[j-1], tags[j] = tags[j], tags[j-1]
		}
	}
	return tags, nil
}

// UpdateTitle 修改标题。
func (s *TodoService) UpdateTitle(id, title string) (TodoItem, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return TodoItem{}, fmt.Errorf("%w: 待办标题不能为空", store.ErrInvalid)
	}
	return s.update(id, func(t *store.Todo) error {
		t.Title = title
		return nil
	})
}

// UpdateDue 修改截止：raw 为空表示清空，否则先按快速录入解析，再退回 ParseDue。
func (s *TodoService) UpdateDue(id, raw string) (TodoItem, error) {
	text := strings.TrimSpace(raw)
	var due *store.Due
	if text != "" {
		qa := core.ParseQuickAdd(text, s.clock())
		if qa.Due != nil {
			due = qa.Due
		} else {
			parsed, err := store.ParseDue(text)
			if err != nil {
				return TodoItem{}, err
			}
			due = parsed
		}
	}
	return s.update(id, func(t *store.Todo) error {
		t.Due = due
		return nil
	})
}

// UpdateTags 覆盖待办标签（去空白、去重、保序）。
func (s *TodoService) UpdateTags(id string, tags []string) (TodoItem, error) {
	clean := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, t := range tags {
		t = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t), "#"))
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		clean = append(clean, t)
	}
	return s.update(id, func(t *store.Todo) error {
		t.Tags = clean
		return nil
	})
}

// UpdateBody 保存详情正文。
func (s *TodoService) UpdateBody(id, body string) error {
	_, err := s.update(id, func(t *store.Todo) error {
		t.Body = body
		return nil
	})
	return err
}

// update 用 store.UpdateTodo 修改并返回最新详情。
func (s *TodoService) update(id string, fn func(*store.Todo) error) (TodoItem, error) {
	ws, idx, err := s.instances()
	if err != nil {
		return TodoItem{}, err
	}
	if _, err := ws.UpdateTodo(id, s.clock(), fn); err != nil {
		return TodoItem{}, err
	}
	return s.getItem(ws, idx, id)
}

// SetStatus 修改状态；withChildren 为 true 时连同未完成的子任务一起修改。
func (s *TodoService) SetStatus(id, status string, withChildren bool) (TodoItem, error) {
	if !store.ValidTodoStatus(status) {
		return TodoItem{}, fmt.Errorf("%w: 非法待办状态 %q", store.ErrInvalid, status)
	}
	ws, idx, err := s.instances()
	if err != nil {
		return TodoItem{}, err
	}
	now := s.clock()
	if withChildren {
		if _, _, err := ws.SetTodoStatusTree(id, status, now); err != nil {
			return TodoItem{}, err
		}
	} else if _, err := ws.SetTodoStatus(id, status, now); err != nil {
		return TodoItem{}, err
	}
	return s.getItem(ws, idx, id)
}

// OpenChildrenCount 返回未完成的子任务数（含全部后代）。
func (s *TodoService) OpenChildrenCount(id string) (int, error) {
	ws, _, err := s.instances()
	if err != nil {
		return 0, err
	}
	return ws.OpenDescendantCount(id)
}

// AddLink 添加链接（同 URL 去重，已有则更新标题）。
func (s *TodoService) AddLink(id, url, title string) (TodoItem, error) {
	ws, idx, err := s.instances()
	if err != nil {
		return TodoItem{}, err
	}
	url = strings.TrimSpace(url)
	title = strings.TrimSpace(title)
	if _, err := ws.AddTodoLink(id, store.TodoLink{URL: url, Title: title}, s.clock()); err != nil {
		return TodoItem{}, err
	}
	return s.getItem(ws, idx, id)
}

// RemoveLink 按 URL 移除链接。
func (s *TodoService) RemoveLink(id, url string) (TodoItem, error) {
	ws, idx, err := s.instances()
	if err != nil {
		return TodoItem{}, err
	}
	if _, err := ws.RemoveTodoLink(id, url, s.clock()); err != nil {
		return TodoItem{}, err
	}
	return s.getItem(ws, idx, id)
}

// OpenLink 用系统默认浏览器打开链接，只允许 http/https/mailto。
func (s *TodoService) OpenLink(url string) error {
	url = strings.TrimSpace(url)
	lower := strings.ToLower(url)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") &&
		!strings.HasPrefix(lower, "mailto:") {
		return fmt.Errorf("%w: 不支持的链接协议 %q", store.ErrInvalid, url)
	}
	return s.open(url, true)
}

// ChooseAndAttach 弹原生文件对话框选择文件并归档为附件。
func (s *TodoService) ChooseAndAttach(id string) (TodoItem, error) {
	paths, err := s.chooseFiles()
	if err != nil {
		return TodoItem{}, err
	}
	if len(paths) == 0 {
		// 用户取消：返回当前详情，不报错。
		return s.Get(id)
	}
	return s.AttachPaths(id, paths)
}

// chooseFiles 调原生多选文件对话框；测试可替换 pickFiles。
func (s *TodoService) chooseFiles() ([]string, error) {
	if s.pickFiles != nil {
		return s.pickFiles()
	}
	if s.app == nil {
		return nil, fmt.Errorf("文件对话框不可用")
	}
	paths, err := s.app.Dialog.OpenFile().
		CanChooseFiles(true).
		CanChooseDirectories(false).
		SetTitle("选择要添加的附件").
		PromptForMultipleSelection()
	if err != nil {
		return nil, fmt.Errorf("打开文件选择框失败: %w", err)
	}
	return paths, nil
}

// AttachPaths 把给定路径的文件归档到 assets/ 并加入附件列表。
func (s *TodoService) AttachPaths(id string, paths []string) (TodoItem, error) {
	ws, idx, err := s.instances()
	if err != nil {
		return TodoItem{}, err
	}
	now := s.clock()
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		if _, _, err := ws.AttachTodoFile(id, p, now); err != nil {
			return TodoItem{}, err
		}
	}
	return s.getItem(ws, idx, id)
}

// Detach 从附件列表移除相对路径（不删除 assets 里的文件）。
func (s *TodoService) Detach(id, rel string) (TodoItem, error) {
	ws, idx, err := s.instances()
	if err != nil {
		return TodoItem{}, err
	}
	if _, err := ws.DetachTodoFile(id, rel, s.clock()); err != nil {
		return TodoItem{}, err
	}
	return s.getItem(ws, idx, id)
}

// OpenAttachment 用系统默认程序打开待办附件。
//
// 只允许打开该待办附件列表里、且落在工作台 assets/ 目录内的文件，
// 拒绝任何路径穿越与列表外的路径。
func (s *TodoService) OpenAttachment(id, rel string) error {
	ws, _, err := s.instances()
	if err != nil {
		return err
	}
	full, err := ws.TodoAttachmentPath(id, rel)
	if err != nil {
		return err
	}
	return s.open(full, false)
}

// open 调用系统默认程序；openURL 为 true 时用浏览器语义（默认实现相同）。
func (s *TodoService) open(target string, isURL bool) error {
	if isURL {
		if s.openURL != nil {
			return s.openURL(target)
		}
		if s.app == nil {
			return fmt.Errorf("打开链接不可用")
		}
		return s.app.Browser.OpenURL(target)
	}
	if s.openFile != nil {
		return s.openFile(target)
	}
	if s.app == nil {
		return fmt.Errorf("打开文件不可用")
	}
	return s.app.Browser.OpenFile(target)
}

// Move 把待办移到 parent 下的第 position 个位置；parent 为空表示顶层。
func (s *TodoService) Move(id, parent string, position int) error {
	ws, _, err := s.instances()
	if err != nil {
		return err
	}
	return ws.MoveTodo(id, parent, position)
}

// HandleDroppedFiles 处理拖进主窗口的文件：挂到 data-todo-id 指向的待办上。
//
// 拖放事件由 main.go 的 WindowFilesDropped 监听器转发到这里，抽成方法便于单测。
func (s *TodoService) HandleDroppedFiles(todoID string, files []string) error {
	if todoID == "" || len(files) == 0 {
		return nil
	}
	if _, err := s.AttachPaths(todoID, files); err != nil {
		return err
	}
	if s.workspace != nil {
		s.workspace.emit(todoChangedEvent, TodoChangeEvent{IDs: []string{todoID}})
	}
	return nil
}

// Delete 删除待办；recursive 为 true 时连同子任务一起删除。
func (s *TodoService) Delete(id string, recursive bool) error {
	ws, _, err := s.instances()
	if err != nil {
		return err
	}
	return ws.DeleteTodo(id, recursive)
}
