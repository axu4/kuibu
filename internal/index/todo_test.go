package index

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/axu4/kuibu/internal/store"
)

// writeTodoFile 直接在工作台 todos/ 下写一个待办文件，模拟外部编辑器。
func writeTodoFile(t *testing.T, ws *store.Workspace, id, content string) string {
	t.Helper()
	path := filepath.Join(ws.TodosDir(), id+".md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写入待办文件失败: %v", err)
	}
	return path
}

// frontMatterTodo 生成带 front matter 的待办内容（与 store 的空列表写法一致）。
func frontMatterTodo(id, title, status, due, parent string, order int, body string) string {
	return "---\n" +
		"id: \"" + id + "\"\n" +
		"title: \"" + title + "\"\n" +
		"type: \"todo\"\n" +
		"status: \"" + status + "\"\n" +
		"due: \"" + due + "\"\n" +
		"created: \"2026-01-01T10:00:00+08:00\"\n" +
		"updated: \"2026-01-02T10:00:00+08:00\"\n" +
		"done_at: \"\"\n" +
		"tags: []\n" +
		"parent: \"" + parent + "\"\n" +
		"order: " + itoa(order) + "\n" +
		"links: []\n" +
		"attachments: []\n" +
		"source: \"\"\n" +
		"---\n" + body
}

// mustCreateTodo 用 store 创建一条待办并返回 ID。
func mustCreateTodo(t *testing.T, ws *store.Workspace, in store.NewTodo) string {
	t.Helper()
	todo, err := ws.CreateTodo(in)
	if err != nil {
		t.Fatalf("CreateTodo 失败: %v", err)
	}
	return todo.ID
}

// todoIDs 返回待办元数据列表中的 ID 序列。
func todoIDs(metas []TodoMeta) []string {
	ids := make([]string, len(metas))
	for i, m := range metas {
		ids[i] = m.ID
	}
	return ids
}

// containsHitID 判断搜索命中是否包含指定 ID。
func containsHitID(hits []Hit, id string) bool {
	for _, h := range hits {
		if h.ID == id {
			return true
		}
	}
	return false
}

// equalStrings 比较两个字符串切片。
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestTodoRebuildAndQueries(t *testing.T) {
	ws := newWorkspace(t)
	idx := openIndex(t, ws)

	now := time.Date(2026, 1, 2, 10, 0, 0, 0, store.TodoLocation())
	root := mustCreateTodo(t, ws, store.NewTodo{Title: "写周报", Status: store.StatusTodo,
		Due: &store.Due{Date: "2026-10-30"}, Tags: []string{"工作", "周报"}, Now: now})
	child := mustCreateTodo(t, ws, store.NewTodo{Title: "整理数据", Status: store.StatusDoing,
		Due: &store.Due{Date: "2026-10-29", Time: "15:00"}, Parent: root, Now: now})
	done := mustCreateTodo(t, ws, store.NewTodo{Title: "已完成事项", Status: store.StatusDone, Now: now})

	stats, err := idx.Rebuild(context.Background())
	if err != nil {
		t.Fatalf("Rebuild 失败: %v", err)
	}
	if stats.TodoAdded != 3 || stats.TodoTotal != 3 || stats.Added != 0 || stats.Total != 0 {
		t.Fatalf("Rebuild Stats = %+v，期望 todoAdded=3 todoTotal=3", stats)
	}

	metas, err := idx.ListTodos(TodoFilter{})
	if err != nil {
		t.Fatalf("ListTodos 失败: %v", err)
	}
	if len(metas) != 3 {
		t.Fatalf("ListTodos 数量 = %d，期望 3", len(metas))
	}

	// 默认排序：未完成在前，有截止日期按 due_at 升序；done 在最后。
	want := []string{child, root, done}
	if !equalStrings(todoIDs(metas), want) {
		t.Errorf("默认排序 = %v，期望 %v", todoIDs(metas), want)
	}

	rootMeta, err := idx.GetTodo(root)
	if err != nil {
		t.Fatalf("GetTodo 失败: %v", err)
	}
	if rootMeta.Title != "写周报" || rootMeta.Status != store.StatusTodo {
		t.Errorf("GetTodo = %+v", rootMeta)
	}
	if rootMeta.Due != "2026-10-30" {
		t.Errorf("Due = %q，期望 2026-10-30", rootMeta.Due)
	}
	if rootMeta.DueAt == nil || rootMeta.DueAt.Format("2006-01-02 15:04") != "2026-10-30 23:59" {
		t.Errorf("DueAt = %v", rootMeta.DueAt)
	}
	if rootMeta.DoneAt != nil {
		t.Errorf("未完成待办 DoneAt = %v，期望 nil", rootMeta.DoneAt)
	}
	if len(rootMeta.Tags) != 2 || rootMeta.Tags[0] != "工作" || rootMeta.Tags[1] != "周报" {
		t.Errorf("Tags = %v", rootMeta.Tags)
	}

	children, err := idx.TodoChildren(root)
	if err != nil {
		t.Fatalf("TodoChildren 失败: %v", err)
	}
	if len(children) != 1 || children[0].ID != child {
		t.Errorf("TodoChildren(root) = %v", todoIDs(children))
	}

	doneMeta, err := idx.GetTodo(done)
	if err != nil {
		t.Fatal(err)
	}
	if doneMeta.DoneAt == nil {
		t.Errorf("done 待办 DoneAt = nil")
	}
}

func TestTodoSyncExternalChanges(t *testing.T) {
	ws := newWorkspace(t)
	idx := openIndex(t, ws)

	writeTodoFile(t, ws, "t1", frontMatterTodo("t1", "原始标题", "todo", "2026-10-30", "", 0, "原始正文\n"))
	if _, err := idx.Rebuild(context.Background()); err != nil {
		t.Fatalf("Rebuild 失败: %v", err)
	}

	// 外部改写：状态/截止日期/标题都变化。
	writeTodoFile(t, ws, "t1", frontMatterTodo("t1", "改写标题", "doing", "2026-10-25 10:00", "", 0, "改写正文\n"))
	stats, err := idx.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync 失败: %v", err)
	}
	if stats.TodoUpdated != 1 || stats.TodoTotal != 1 || stats.TodoAdded != 0 {
		t.Fatalf("改写后 Sync = %+v，期望 todoUpdated=1", stats)
	}
	meta, err := idx.GetTodo("t1")
	if err != nil {
		t.Fatalf("GetTodo 失败: %v", err)
	}
	if meta.Title != "改写标题" || meta.Status != "doing" || meta.Due != "2026-10-25 10:00" {
		t.Errorf("改写后元数据 = %+v", meta)
	}
	if hits, err := idx.SearchTodos("改写正文", 10); err != nil || !containsHitID(hits, "t1") {
		t.Errorf("改写后全文索引未更新，结果 = %v err=%v", hits, err)
	}

	// 外部删除文件。
	if err := os.Remove(filepath.Join(ws.TodosDir(), "t1.md")); err != nil {
		t.Fatal(err)
	}
	stats, err = idx.Sync(context.Background())
	if err != nil {
		t.Fatalf("删除后 Sync 失败: %v", err)
	}
	if stats.TodoRemoved != 1 || stats.TodoTotal != 0 {
		t.Fatalf("删除后 Sync = %+v，期望 todoRemoved=1", stats)
	}
	if _, err := idx.GetTodo("t1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("删除后 GetTodo 错误 = %v，期望索引项不存在", err)
	}
}

func TestTodoRebuildAfterDeleteDB(t *testing.T) {
	ws := newWorkspace(t)
	idx := openIndex(t, ws)

	writeTodoFile(t, ws, "a", frontMatterTodo("a", "待办 A", "todo", "2026-10-30", "", 0, "正文 A\n"))
	writeTodoFile(t, ws, "b", frontMatterTodo("b", "待办 B", "doing", "2026-10-25", "", 0, "正文 B\n"))

	if _, err := idx.Rebuild(context.Background()); err != nil {
		t.Fatalf("Rebuild 失败: %v", err)
	}
	before, err := idx.ListTodos(TodoFilter{})
	if err != nil {
		t.Fatal(err)
	}

	if err := idx.Close(); err != nil {
		t.Fatalf("关闭索引失败: %v", err)
	}
	if err := os.Remove(idx.Path()); err != nil {
		t.Fatalf("删除索引文件失败: %v", err)
	}

	reopened, err := Open(ws)
	if err != nil {
		t.Fatalf("重新打开索引失败: %v", err)
	}
	defer reopened.Close()

	stats, err := reopened.Rebuild(context.Background())
	if err != nil {
		t.Fatalf("重建失败: %v", err)
	}
	if stats.TodoTotal != 2 {
		t.Fatalf("重建 Stats = %+v", stats)
	}
	after, err := reopened.ListTodos(TodoFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Fatalf("重建前后数量不同: %d vs %d", len(before), len(after))
	}
	for i := range before {
		if before[i].ID != after[i].ID || before[i].Title != after[i].Title ||
			before[i].Status != after[i].Status || before[i].Due != after[i].Due {
			t.Errorf("重建前后第 %d 项不同: before=%+v after=%+v", i, before[i], after[i])
		}
	}
}

func TestTodoFilters(t *testing.T) {
	ws := newWorkspace(t)
	idx := openIndex(t, ws)

	now := time.Date(2026, 1, 2, 10, 0, 0, 0, store.TodoLocation())
	a := mustCreateTodo(t, ws, store.NewTodo{Title: "写周报", Status: store.StatusTodo,
		Due: &store.Due{Date: "2026-10-30"}, Tags: []string{"工作"}, Now: now})
	b := mustCreateTodo(t, ws, store.NewTodo{Title: "处理紧急问题", Status: store.StatusDoing,
		Due: &store.Due{Date: "2026-10-25", Time: "10:00"}, Tags: []string{"工作", "紧急"}, Now: now})
	c := mustCreateTodo(t, ws, store.NewTodo{Title: "买菜", Status: store.StatusTodo, Tags: []string{"生活"}, Now: now})
	d := mustCreateTodo(t, ws, store.NewTodo{Title: "已完成的活", Status: store.StatusDone,
		Due: &store.Due{Date: "2026-10-20"}, Tags: []string{"工作"}, Now: now})
	e := mustCreateTodo(t, ws, store.NewTodo{Title: "子任务", Status: store.StatusTodo, Tags: []string{"工作"}, Parent: a, Now: now})

	if _, err := idx.Rebuild(context.Background()); err != nil {
		t.Fatalf("Rebuild 失败: %v", err)
	}

	// 状态过滤：只要未完成。
	metas, err := idx.ListTodos(TodoFilter{Statuses: []string{store.StatusTodo, store.StatusDoing}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{b, a, c, e} // b 截止早于 a；无截止的 c/e 排后，c 顶层先于 e。
	if !equalStrings(todoIDs(metas), want) {
		t.Errorf("状态过滤结果 = %v，期望 %v", todoIDs(metas), want)
	}

	// 标签过滤：全部包含。
	metas, err = idx.ListTodos(TodoFilter{Tags: []string{"工作"}})
	if err != nil {
		t.Fatal(err)
	}
	wantTags := []string{b, a, e, d}
	if !equalStrings(todoIDs(metas), wantTags) {
		t.Errorf("标签过滤结果 = %v，期望 %v", todoIDs(metas), wantTags)
	}

	// 父任务过滤：只要顶层。
	top := ""
	metas, err = idx.ListTodos(TodoFilter{Parent: &top})
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 4 {
		t.Fatalf("顶层过滤数量 = %d，期望 4", len(metas))
	}
	for _, m := range metas {
		if m.ID == e {
			t.Errorf("顶层过滤不应包含子任务 e")
		}
	}

	// 到期区间过滤：>= 2026-10-26 排除无截止与更早的。
	metas, err = idx.ListTodos(TodoFilter{DueFrom: "2026-10-26"})
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(todoIDs(metas), []string{a}) {
		t.Errorf("DueFrom 结果 = %v，期望 [a]", todoIDs(metas))
	}

	// 逾期过滤：未完成且截止早于 2026-10-26 00:00。
	overdue := time.Date(2026, 10, 26, 0, 0, 0, 0, store.TodoLocation())
	metas, err = idx.ListTodos(TodoFilter{OverdueAt: overdue})
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(todoIDs(metas), []string{b}) {
		t.Errorf("逾期结果 = %v，期望 [b]", todoIDs(metas))
	}

	// 全文检索：三字及以上走 FTS，两字走 LIKE 回退。
	for _, q := range []string{"写周报", "紧急", "买菜"} {
		hits, err := idx.SearchTodos(q, 10)
		if err != nil {
			t.Fatalf("SearchTodos(%q) 失败: %v", q, err)
		}
		if len(hits) == 0 {
			t.Errorf("SearchTodos(%q) 无结果", q)
		}
	}
	if hits, err := idx.SearchTodos("处理", 10); err != nil || len(hits) == 0 {
		t.Errorf("两字 LIKE 回退失败: hits=%v err=%v", hits, err)
	}

	// ListTodos 的 Query 过滤：三字走 FTS EXISTS，两字走 LIKE。
	metas, err = idx.ListTodos(TodoFilter{Query: "写周报"})
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(todoIDs(metas), []string{a}) {
		t.Errorf("ListTodos(Query=写周报) = %v，期望 [a]", todoIDs(metas))
	}
	metas, err = idx.ListTodos(TodoFilter{Query: "处理"})
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(todoIDs(metas), []string{b}) {
		t.Errorf("ListTodos(Query=处理) = %v，期望 [b]", todoIDs(metas))
	}
}

func TestWatchTodoChanges(t *testing.T) {
	ws := newWorkspace(t)
	idx := openIndex(t, ws)
	if _, err := idx.Rebuild(context.Background()); err != nil {
		t.Fatalf("Rebuild 失败: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan Event, 16)
	done := make(chan error, 1)
	ready := make(chan struct{})
	go func() {
		done <- watch(ctx, ws, idx, func(e Event) {
			select {
			case events <- e:
			default:
			}
		}, ready)
	}()
	<-ready

	waitEvent := func(t *testing.T, predicate func(Event) bool) Event {
		t.Helper()
		timeout := time.After(5 * time.Second)
		for {
			select {
			case e := <-events:
				if predicate(e) {
					return e
				}
			case <-timeout:
				t.Fatalf("等待事件超时")
			}
		}
	}

	// 待办文件变化：应带 TodoIDs，且 IDs 为空。
	writeTodoFile(t, ws, "t1", frontMatterTodo("t1", "待办", "todo", "2026-10-30", "", 0, "正文\n"))
	e := waitEvent(t, func(ev Event) bool { return contains(ev.TodoIDs, "t1") })
	if len(e.IDs) != 0 {
		t.Errorf("待办事件 IDs = %v，期望空", e.IDs)
	}
	if !contains(e.TodoIDs, "t1") {
		t.Errorf("待办事件 TodoIDs = %v，期望包含 t1", e.TodoIDs)
	}
	if len(e.TodoChanges) == 0 {
		t.Errorf("待办事件 TodoChanges 为空")
	}

	// 笔记文件变化：应带 IDs，且 TodoIDs 为空。
	writeNoteFile(t, ws, "n1", frontMatterNote("n1", "笔记", "", 0, "笔记正文\n"))
	e = waitEvent(t, func(ev Event) bool { return contains(ev.IDs, "n1") })
	if len(e.TodoIDs) != 0 {
		t.Errorf("笔记事件 TodoIDs = %v，期望空", e.TodoIDs)
	}
	if !contains(e.IDs, "n1") {
		t.Errorf("笔记事件 IDs = %v，期望包含 n1", e.IDs)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Watch 返回错误: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("取消后 Watch 未返回")
	}
}
