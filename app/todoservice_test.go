package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axu4/kuibu/internal/index"
	"github.com/axu4/kuibu/internal/store"
)

// fixedNow 是待办测试使用的固定时间：2026-10-26 10:00 周一（Asia/Shanghai）。
func fixedNow() time.Time {
	return time.Date(2026, 10, 26, 10, 0, 0, 0, store.TodoLocation())
}

// newTestTodos 构造一个挂在临时工作台上的 TodoService，并替换系统能力。
func newTestTodos(t *testing.T) (*TodoService, *WorkspaceService, *openedFiles) {
	t.Helper()
	ws := newWorkspaceService()
	openTempWorkspace(t, ws)
	todos := newTodoService(ws)
	todos.now = fixedNow
	files := &openedFiles{}
	todos.openFile = func(path string) error {
		files.opened = append(files.opened, path)
		return nil
	}
	todos.openURL = func(url string) error {
		files.urls = append(files.urls, url)
		return nil
	}
	t.Cleanup(func() { ws.close() })
	return todos, ws, files
}

// openedFiles 记录被打开的文件与链接。
type openedFiles struct {
	opened []string
	urls   []string
}

func TestTodoCreateParsesAndPersists(t *testing.T) {
	todos, ws, _ := newTestTodos(t)

	item, err := todos.Create("明天下午三点 交周报 #工作", "")
	if err != nil {
		t.Fatalf("Create 返回错误: %v", err)
	}
	if item.Title != "交周报" {
		t.Errorf("Title = %q，期望 交周报", item.Title)
	}
	if item.Due != "2026-10-27 15:00" {
		t.Errorf("Due = %q，期望 2026-10-27 15:00", item.Due)
	}
	if len(item.Tags) != 1 || item.Tags[0] != "工作" {
		t.Errorf("Tags = %v", item.Tags)
	}

	// 落盘检查：直接读文件，确认 front matter 与索引一致。
	todo, err := ws.workspace().GetTodo(item.ID)
	if err != nil {
		t.Fatalf("读取落盘待办失败: %v", err)
	}
	if todo.Title != "交周报" || todo.Due.String() != "2026-10-27 15:00" {
		t.Errorf("落盘内容 = %+v", todo)
	}

	// 空文本应报错。
	if _, err := todos.Create("   ", ""); err == nil {
		t.Error("空标题应返回错误")
	}
}

func TestTodoCreateChild(t *testing.T) {
	todos, _, _ := newTestTodos(t)

	parent, err := todos.Create("父任务", "")
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	child, err := todos.Create("子任务", parent.ID)
	if err != nil {
		t.Fatalf("Create 子任务失败: %v", err)
	}
	if child.Parent != parent.ID {
		t.Errorf("Parent = %q，期望 %q", child.Parent, parent.ID)
	}

	detail, err := todos.Get(parent.ID)
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if detail.ChildTotal != 1 || detail.ChildDone != 0 {
		t.Errorf("子任务进度 = %d/%d，期望 0/1", detail.ChildDone, detail.ChildTotal)
	}
	if len(detail.Children) != 1 || detail.Children[0].ID != child.ID {
		t.Errorf("Children = %+v", detail.Children)
	}
}

func TestTodoListViews(t *testing.T) {
	todos, _, _ := newTestTodos(t)

	mustCreate := func(text, due string) TodoItem {
		t.Helper()
		item, err := todos.Create(text, "")
		if err != nil {
			t.Fatalf("Create(%q) 失败: %v", text, err)
		}
		if due != "" {
			item, err = todos.UpdateDue(item.ID, due)
			if err != nil {
				t.Fatalf("UpdateDue 失败: %v", err)
			}
		}
		return item
	}

	overdue := mustCreate("逾期", "2026-10-25")
	today := mustCreate("今天", "2026-10-26")
	thisWeek := mustCreate("本周晚些", "2026-10-29")
	nextWeek := mustCreate("下周", "2026-11-05")
	noDue := mustCreate("无截止", "")
	done := mustCreate("已完成", "")

	if _, err := todos.SetStatus(done.ID, store.StatusDone, false); err != nil {
		t.Fatalf("SetStatus 失败: %v", err)
	}
	canceled := mustCreate("已取消", "")
	if _, err := todos.SetStatus(canceled.ID, store.StatusCanceled, false); err != nil {
		t.Fatalf("SetStatus 失败: %v", err)
	}

	ids := func(items []TodoItem) map[string]bool {
		out := map[string]bool{}
		for _, it := range items {
			out[it.ID] = true
		}
		return out
	}

	todayItems, err := todos.List(TodoListFilter{View: "today"})
	if err != nil {
		t.Fatalf("List today 失败: %v", err)
	}
	got := ids(todayItems)
	if !got[overdue.ID] || !got[today.ID] {
		t.Errorf("今天组应含今天与逾期，实际 %v", got)
	}
	if got[thisWeek.ID] || got[nextWeek.ID] || got[noDue.ID] || got[done.ID] {
		t.Errorf("今天组混入了不该出现的项: %v", got)
	}
	for _, it := range todayItems {
		if it.ID == overdue.ID && !it.Overdue {
			t.Errorf("逾期待办应标记 overdue: %+v", it)
		}
	}

	weekItems, err := todos.List(TodoListFilter{View: "week"})
	if err != nil {
		t.Fatalf("List week 失败: %v", err)
	}
	got = ids(weekItems)
	if !got[today.ID] || !got[thisWeek.ID] {
		t.Errorf("本周组应含本周内的项，实际 %v", got)
	}
	if got[overdue.ID] || got[nextWeek.ID] || got[noDue.ID] {
		t.Errorf("本周组混入了本周外或无截止项: %v", got)
	}

	allItems, err := todos.List(TodoListFilter{View: "all"})
	if err != nil {
		t.Fatalf("List all 失败: %v", err)
	}
	got = ids(allItems)
	if !got[noDue.ID] || !got[nextWeek.ID] {
		t.Errorf("全部未完成应含无截止与下周项: %v", got)
	}
	if got[done.ID] || got[canceled.ID] {
		t.Errorf("全部未完成不应含已完成/已取消: %v", got)
	}

	doneItems, err := todos.List(TodoListFilter{View: "done"})
	if err != nil {
		t.Fatalf("List done 失败: %v", err)
	}
	got = ids(doneItems)
	if !got[done.ID] || !got[canceled.ID] {
		t.Errorf("已完成组应含已完成与已取消: %v", got)
	}
	if got[today.ID] {
		t.Errorf("已完成组混入了未完成项: %v", got)
	}
}

func TestTodoListTagAndSearchFilter(t *testing.T) {
	todos, _, _ := newTestTodos(t)

	work, err := todos.Create("写周报 #工作", "")
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	life, err := todos.Create("买菜 #生活", "")
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if err := todos.UpdateBody(life.ID, "记得买西红柿和鸡蛋"); err != nil {
		t.Fatalf("UpdateBody 失败: %v", err)
	}

	tagged, err := todos.List(TodoListFilter{View: "all", Tag: "工作"})
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(tagged) != 1 || tagged[0].ID != work.ID {
		t.Errorf("标签筛选结果 = %+v", tagged)
	}

	found, err := todos.List(TodoListFilter{View: "all", Query: "西红柿"})
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(found) != 1 || found[0].ID != life.ID {
		t.Errorf("正文检索结果 = %+v", found)
	}

	found, err = todos.List(TodoListFilter{View: "all", Query: "周报"})
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(found) != 1 || found[0].ID != work.ID {
		t.Errorf("标题检索结果 = %+v", found)
	}

	tags, err := todos.Tags()
	if err != nil {
		t.Fatalf("Tags 失败: %v", err)
	}
	if len(tags) != 2 {
		t.Errorf("标签数 = %d，期望 2: %+v", len(tags), tags)
	}
}

func TestTodoSetStatusWithChildren(t *testing.T) {
	todos, _, _ := newTestTodos(t)

	parent, _ := todos.Create("父", "")
	child, _ := todos.Create("子", parent.ID)
	grand, _ := todos.Create("孙", child.ID)

	// 不带子任务：只改父。
	if _, err := todos.SetStatus(parent.ID, store.StatusDone, false); err != nil {
		t.Fatalf("SetStatus 失败: %v", err)
	}
	detail, _ := todos.Get(parent.ID)
	if detail.Status != store.StatusDone {
		t.Errorf("父状态 = %q", detail.Status)
	}
	if detail.Children[0].Status != store.StatusTodo {
		t.Errorf("不带子任务时子任务不应变化: %+v", detail.Children[0])
	}

	count, err := todos.OpenChildrenCount(parent.ID)
	if err != nil {
		t.Fatalf("OpenChildrenCount 失败: %v", err)
	}
	if count != 2 {
		t.Errorf("未完成子任务数 = %d，期望 2", count)
	}

	// 带子任务：父与全部后代一起完成。
	if _, err := todos.SetStatus(parent.ID, store.StatusDone, true); err != nil {
		t.Fatalf("SetStatus 失败: %v", err)
	}
	for _, id := range []string{child.ID, grand.ID} {
		item, err := todos.Get(id)
		if err != nil {
			t.Fatalf("Get(%s) 失败: %v", id, err)
		}
		if item.Status != store.StatusDone {
			t.Errorf("%s 状态 = %q，期望 done", id, item.Status)
		}
	}
	if n, _ := todos.OpenChildrenCount(parent.ID); n != 0 {
		t.Errorf("全部完成后未完成子任务数 = %d", n)
	}
}

func TestTodoUpdateDueClearAndQuickExpression(t *testing.T) {
	todos, _, _ := newTestTodos(t)

	item, err := todos.Create("交方案", "")
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}

	item, err = todos.UpdateDue(item.ID, "明天下午三点")
	if err != nil {
		t.Fatalf("UpdateDue 失败: %v", err)
	}
	if item.Due != "2026-10-27 15:00" {
		t.Errorf("快速表达解析 Due = %q", item.Due)
	}

	item, err = todos.UpdateDue(item.ID, "2026-11-02")
	if err != nil {
		t.Fatalf("UpdateDue 失败: %v", err)
	}
	if item.Due != "2026-11-02" {
		t.Errorf("标准格式 Due = %q", item.Due)
	}

	item, err = todos.UpdateDue(item.ID, "")
	if err != nil {
		t.Fatalf("UpdateDue 清空失败: %v", err)
	}
	if item.Due != "" || item.DueAt != "" || item.Overdue {
		t.Errorf("清空后 = %+v", item)
	}

	if _, err := todos.UpdateDue(item.ID, "不是日期"); err == nil {
		t.Error("无法解析的截止应返回错误")
	}
}

func TestTodoLinks(t *testing.T) {
	todos, _, files := newTestTodos(t)

	item, _ := todos.Create("带链接", "")
	item, err := todos.AddLink(item.ID, "https://example.com/doc", "文档")
	if err != nil {
		t.Fatalf("AddLink 失败: %v", err)
	}
	if len(item.Links) != 1 || item.Links[0].URL != "https://example.com/doc" || item.Links[0].Title != "文档" {
		t.Fatalf("Links = %+v", item.Links)
	}

	// 同 URL 只更新标题。
	item, err = todos.AddLink(item.ID, "https://example.com/doc", "文档 v2")
	if err != nil {
		t.Fatalf("AddLink 失败: %v", err)
	}
	if len(item.Links) != 1 || item.Links[0].Title != "文档 v2" {
		t.Errorf("同 URL 应去重，Links = %+v", item.Links)
	}

	if err := todos.OpenLink("https://example.com/doc"); err != nil {
		t.Fatalf("OpenLink 失败: %v", err)
	}
	if len(files.urls) != 1 || files.urls[0] != "https://example.com/doc" {
		t.Errorf("打开的链接 = %v", files.urls)
	}
	if err := todos.OpenLink("file:///etc/passwd"); err == nil {
		t.Error("file:// 链接应被拒绝")
	}

	item, err = todos.RemoveLink(item.ID, "https://example.com/doc")
	if err != nil {
		t.Fatalf("RemoveLink 失败: %v", err)
	}
	if len(item.Links) != 0 {
		t.Errorf("删除后 Links = %+v", item.Links)
	}

	if _, err := todos.AddLink(item.ID, "没有 scheme", ""); err == nil {
		t.Error("非法 URL 应返回错误")
	}
}

func TestTodoAttachAndOpenAttachment(t *testing.T) {
	todos, ws, files := newTestTodos(t)

	item, _ := todos.Create("带附件", "")

	src := filepath.Join(t.TempDir(), "报告.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o600); err != nil {
		t.Fatalf("写临时文件失败: %v", err)
	}
	item, err := todos.AttachPaths(item.ID, []string{src})
	if err != nil {
		t.Fatalf("AttachPaths 失败: %v", err)
	}
	if len(item.Attachments) != 1 {
		t.Fatalf("Attachments = %+v", item.Attachments)
	}
	rel := item.Attachments[0].Path
	if !strings.HasPrefix(rel, "../assets/") {
		t.Errorf("附件相对路径 = %q，期望 ../assets/ 前缀", rel)
	}
	if item.Attachments[0].Name == "" || !strings.Contains(item.Attachments[0].Name, "报告") {
		t.Errorf("附件名 = %q", item.Attachments[0].Name)
	}

	// 正常打开：解析到 assets/ 下的真实文件。
	if err := todos.OpenAttachment(item.ID, rel); err != nil {
		t.Fatalf("OpenAttachment 失败: %v", err)
	}
	if len(files.opened) != 1 {
		t.Fatalf("打开的文件 = %v", files.opened)
	}
	want := filepath.Join(ws.workspace().AssetsDir(), item.Attachments[0].Name)
	if files.opened[0] != want {
		t.Errorf("打开路径 = %q，期望 %q", files.opened[0], want)
	}

	// 非法路径：路径穿越、列表外路径、绝对路径都应被拒绝。
	bad := []string{
		"../../etc/passwd",
		"../assets/../../../etc/passwd",
		"../assets/不存在.txt",
		"/etc/passwd",
	}
	for _, rel := range bad {
		if err := todos.OpenAttachment(item.ID, rel); err == nil {
			t.Errorf("OpenAttachment(%q) 应被拒绝", rel)
		}
	}
	if len(files.opened) != 1 {
		t.Errorf("被拒绝的路径不应被打开: %v", files.opened)
	}

	// 移除附件不删除 assets 里的文件。
	item, err = todos.Detach(item.ID, rel)
	if err != nil {
		t.Fatalf("Detach 失败: %v", err)
	}
	if len(item.Attachments) != 0 {
		t.Errorf("移除后 Attachments = %+v", item.Attachments)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("移除附件后资源文件应保留: %v", err)
	}
}

func TestTodoChooseAndAttachUsesPicker(t *testing.T) {
	todos, _, _ := newTestTodos(t)
	item, _ := todos.Create("选择文件", "")

	src := filepath.Join(t.TempDir(), "picker.txt")
	if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
		t.Fatalf("写临时文件失败: %v", err)
	}
	todos.pickFiles = func() ([]string, error) { return []string{src}, nil }

	item, err := todos.ChooseAndAttach(item.ID)
	if err != nil {
		t.Fatalf("ChooseAndAttach 失败: %v", err)
	}
	if len(item.Attachments) != 1 {
		t.Errorf("Attachments = %+v", item.Attachments)
	}

	// 用户取消（返回空列表）不报错，也不改附件。
	todos.pickFiles = func() ([]string, error) { return nil, nil }
	item, err = todos.ChooseAndAttach(item.ID)
	if err != nil {
		t.Fatalf("取消时不应报错: %v", err)
	}
	if len(item.Attachments) != 1 {
		t.Errorf("取消后 Attachments = %+v", item.Attachments)
	}
}

func TestTodoGetIncludesSourceTitle(t *testing.T) {
	todos, ws, _ := newTestTodos(t)

	note, err := ws.workspace().Create(store.NewNote{Title: "会议纪要"})
	if err != nil {
		t.Fatalf("创建笔记失败: %v", err)
	}
	todo, err := ws.workspace().CreateTodo(store.NewTodo{
		Title:  "来自笔记的待办",
		Source: note.ID,
		Now:    fixedNow(),
	})
	if err != nil {
		t.Fatalf("创建待办失败: %v", err)
	}
	if _, err := ws.index().Sync(t.Context()); err != nil {
		t.Fatalf("同步索引失败: %v", err)
	}

	item, err := todos.Get(todo.ID)
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if item.Source != note.ID {
		t.Errorf("Source = %q，期望 %q", item.Source, note.ID)
	}
	if item.SourceTitle != "会议纪要" {
		t.Errorf("SourceTitle = %q，期望 会议纪要", item.SourceTitle)
	}
}

func TestTodoPreview(t *testing.T) {
	todos, _, _ := newTestTodos(t)
	preview := todos.Preview("明天下午三点 交周报 #工作")
	if preview.Title != "交周报" || preview.Due != "2026-10-27 15:00" {
		t.Errorf("Preview = %+v", preview)
	}
	if len(preview.Tags) != 1 || preview.Tags[0] != "工作" {
		t.Errorf("Preview.Tags = %v", preview.Tags)
	}
}

func TestSaveQuickTodo(t *testing.T) {
	shell, _, ui, ws := newTestShell(t)
	opened := openTempWorkspace(t, ws)
	todos := newTodoService(ws)
	todos.now = fixedNow
	shell.todo = todos
	t.Cleanup(ws.close)

	saved, err := shell.SaveQuickTodo("明天下午三点 交周报 #工作\n把上周的数据补上\n- [ ] 附上截图")
	if err != nil {
		t.Fatalf("SaveQuickTodo 返回错误: %v", err)
	}
	if saved.Title != "交周报" {
		t.Errorf("Title = %q，期望 交周报", saved.Title)
	}

	todo, err := opened.GetTodo(saved.ID)
	if err != nil {
		t.Fatalf("读取新待办失败: %v", err)
	}
	if todo.Title != "交周报" {
		t.Errorf("落盘标题 = %q", todo.Title)
	}
	if todo.Due.String() != "2026-10-27 15:00" {
		t.Errorf("落盘截止 = %q", todo.Due.String())
	}
	if todo.Body != "把上周的数据补上\n- [ ] 附上截图" {
		t.Errorf("落盘正文 = %q", todo.Body)
	}
	if ui.closed != 1 {
		t.Errorf("保存后应关闭小窗，实际 %d 次", ui.closed)
	}
	if len(ui.events) != 1 || ui.events[0] != todoChangedEvent {
		t.Errorf("事件 = %v，期望 [%s]", ui.events, todoChangedEvent)
	}

	// 空内容报错，且不关闭小窗。
	if _, err := shell.SaveQuickTodo("   \n  "); err == nil {
		t.Error("空内容应返回错误")
	}
	if ui.closed != 1 {
		t.Errorf("失败时不应关闭小窗，实际 %d 次", ui.closed)
	}
}

func TestHandleWatchEventEmitsOnlyNonEmpty(t *testing.T) {
	var events []struct {
		name string
		data any
	}
	emit := func(name string, data any) {
		events = append(events, struct {
			name string
			data any
		}{name, data})
	}

	// 只有笔记变化。
	handleWatchEvent(index.Event{IDs: []string{"n1"}}, emit)
	if len(events) != 1 || events[0].name != changedEvent {
		t.Fatalf("笔记变化事件 = %+v", events)
	}
	if got := events[0].data.(ChangeEvent); len(got.IDs) != 1 || got.IDs[0] != "n1" {
		t.Errorf("笔记事件数据 = %+v", got)
	}

	// 只有待办变化。
	events = nil
	handleWatchEvent(index.Event{TodoIDs: []string{"t1", "t2"}}, emit)
	if len(events) != 1 || events[0].name != todoChangedEvent {
		t.Fatalf("待办变化事件 = %+v", events)
	}
	if got := events[0].data.(TodoChangeEvent); len(got.IDs) != 2 {
		t.Errorf("待办事件数据 = %+v", got)
	}

	// 两者都有：各发一次，互不混入。
	events = nil
	handleWatchEvent(index.Event{IDs: []string{"n1"}, TodoIDs: []string{"t1"}}, emit)
	if len(events) != 2 {
		t.Fatalf("事件数 = %d，期望 2: %+v", len(events), events)
	}
	if events[0].name != changedEvent || events[1].name != todoChangedEvent {
		t.Errorf("事件顺序 = %+v", events)
	}
	if len(events[0].data.(ChangeEvent).IDs) != 1 {
		t.Errorf("笔记事件混入了待办: %+v", events[0].data)
	}

	// 空事件：什么都不发。
	events = nil
	handleWatchEvent(index.Event{}, emit)
	if len(events) != 0 {
		t.Errorf("空事件不应发任何事件: %+v", events)
	}
}

func TestHandleDroppedFiles(t *testing.T) {
	todos, _, _ := newTestTodos(t)
	item, _ := todos.Create("拖入附件", "")

	src := filepath.Join(t.TempDir(), "dropped.txt")
	if err := os.WriteFile(src, []byte("d"), 0o600); err != nil {
		t.Fatalf("写临时文件失败: %v", err)
	}
	if err := todos.HandleDroppedFiles(item.ID, []string{src}); err != nil {
		t.Fatalf("HandleDroppedFiles 失败: %v", err)
	}
	detail, err := todos.Get(item.ID)
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if len(detail.Attachments) != 1 {
		t.Errorf("Attachments = %+v", detail.Attachments)
	}

	// 没有目标待办或没有文件时静默忽略。
	if err := todos.HandleDroppedFiles("", []string{src}); err != nil {
		t.Errorf("空待办 ID 应静默忽略: %v", err)
	}
	if err := todos.HandleDroppedFiles(item.ID, nil); err != nil {
		t.Errorf("空文件列表应静默忽略: %v", err)
	}
}

func TestTodoDeleteAndMove(t *testing.T) {
	todos, _, _ := newTestTodos(t)

	parent, _ := todos.Create("父", "")
	child, _ := todos.Create("子", parent.ID)

	if err := todos.Delete(parent.ID, false); err == nil {
		t.Error("有子任务时非递归删除应报错")
	}
	if err := todos.Delete(parent.ID, true); err != nil {
		t.Fatalf("递归删除失败: %v", err)
	}
	for _, id := range []string{parent.ID, child.ID} {
		if _, err := todos.Get(id); err == nil {
			t.Errorf("%s 应已被删除", id)
		}
	}

	a, _ := todos.Create("A", "")
	if err := todos.Move(a.ID, "", 0); err != nil {
		t.Fatalf("Move 失败: %v", err)
	}
}
