package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixedTodoTime 返回固定时区（+08:00）的时间，便于 golden 比较。
func fixedTodoTime(y int, m time.Month, d, hh, mm, ss int) time.Time {
	return time.Date(y, m, d, hh, mm, ss, 0, time.FixedZone("CST", 8*3600))
}

func TestCreateTodoTitleOnly(t *testing.T) {
	w := mustInit(t)

	todo, err := w.CreateTodo(NewTodo{Title: "买牛奶"})
	if err != nil {
		t.Fatalf("CreateTodo 失败: %v", err)
	}
	if todo.Title != "买牛奶" {
		t.Errorf("Title = %q", todo.Title)
	}
	if todo.Status != StatusTodo {
		t.Errorf("Status = %q，期望 todo", todo.Status)
	}
	if todo.Order != 0 {
		t.Errorf("Order = %d，期望 0", todo.Order)
	}
	if !noteIDPattern.MatchString(todo.ID) {
		t.Errorf("ID = %q，不符合 yyyymmdd-hhmmss-xxxx", todo.ID)
	}
	if todo.Created.IsZero() || todo.Updated.IsZero() {
		t.Errorf("时间字段为空")
	}

	got, err := w.GetTodo(todo.ID)
	if err != nil {
		t.Fatalf("GetTodo 失败: %v", err)
	}
	if got.Title != "买牛奶" || got.Status != StatusTodo {
		t.Errorf("读取结果 = %+v", got)
	}

	data, err := os.ReadFile(w.todoPath(todo.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `type: "todo"`) {
		t.Errorf("front matter 缺少 type:\n%s", data)
	}
	if !strings.Contains(string(data), `status: "todo"`) {
		t.Errorf("front matter 缺少 status:\n%s", data)
	}
}

func TestCreateTodoValidation(t *testing.T) {
	w := mustInit(t)

	if _, err := w.CreateTodo(NewTodo{Title: "   "}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("空标题错误 = %v，期望 ErrInvalid", err)
	}
	if _, err := w.CreateTodo(NewTodo{Title: "x", Status: "weird"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("非法状态错误 = %v，期望 ErrInvalid", err)
	}
	if _, err := w.CreateTodo(NewTodo{Title: "x", Parent: "nope"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("父不存在错误 = %v，期望 ErrNotFound", err)
	}
}

func TestTodoFrontMatterGolden(t *testing.T) {
	w := mustInit(t)

	todo := &Todo{
		ID:      "20260101-120000-abcd",
		Title:   "写周报",
		Status:  StatusDoing,
		Due:     &Due{Date: "2026-10-30", Time: "15:00"},
		Created: fixedTodoTime(2026, 1, 1, 12, 0, 0),
		Updated: fixedTodoTime(2026, 1, 2, 13, 0, 0),
		Tags:    []string{"工作", "周报"},
		Order:   3,
		Body:    "详情\n",
		Links: []TodoLink{
			{URL: "https://example.com", Title: "示例"},
			{URL: "mailto:a@b.c"},
		},
		Attachments: []string{"../assets/20260101-abcdef12-pic.png"},
		Source:      "mail",
		Priority:    PriorityHigh,
		ExternalID:  "<abc@example.com>",
		Extra:       map[string]any{"estimate": "2h"},
	}
	if err := w.writeTodo(todo); err != nil {
		t.Fatalf("writeTodo 失败: %v", err)
	}

	data, err := os.ReadFile(w.todoPath(todo.ID))
	if err != nil {
		t.Fatal(err)
	}
	want := `---
id: "20260101-120000-abcd"
title: "写周报"
type: "todo"
status: "doing"
due: "2026-10-30 15:00"
created: "2026-01-01T12:00:00+08:00"
updated: "2026-01-02T13:00:00+08:00"
done_at: ""
tags:
    - 工作
    - 周报
parent: ""
order: 3
links:
    - url: "https://example.com"
      title: "示例"
    - url: "mailto:a@b.c"
attachments:
    - ../assets/20260101-abcdef12-pic.png
source: "mail"
priority: "high"
external_id: "<abc@example.com>"
estimate: 2h
---
详情
`
	if string(data) != want {
		t.Errorf("待办文件内容与 golden 不一致:\n got:\n%s\nwant:\n%s", data, want)
	}
}

func TestTodoRoundTripLossless(t *testing.T) {
	w := mustInit(t)

	raw := `---
id: "20260101-120000-abcd"
title: "原始待办"
type: "todo"
status: "doing"
due: "2026-10-30"
created: "2026-01-01T10:00:00+08:00"
updated: "2026-01-02T11:00:00+08:00"
done_at: ""
tags:
  - 旧标签
parent: ""
order: 0
links:
  - url: "https://a.com"
    title: "A"
  - "https://b.com"
attachments: []
source: ""
custom_str: "保留我"
labels: [x, y]
mode: 'fast'
---
正文
`
	path := w.todoPath("20260101-120000-abcd")
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := w.GetTodo("20260101-120000-abcd")
	if err != nil {
		t.Fatalf("GetTodo 失败: %v", err)
	}
	if got.Status != StatusDoing {
		t.Errorf("Status = %q", got.Status)
	}
	if got.Due == nil || got.Due.Date != "2026-10-30" || got.Due.Time != "" {
		t.Errorf("Due = %+v", got.Due)
	}
	if len(got.Links) != 2 || got.Links[0].URL != "https://a.com" || got.Links[1].URL != "https://b.com" {
		t.Errorf("Links = %+v", got.Links)
	}
	if got.Extra["custom_str"] != "保留我" {
		t.Errorf("Extra[custom_str] = %v", got.Extra["custom_str"])
	}

	// 只改标题，未知字段与未修改字段应保持原格式。
	if _, err := w.UpdateTodo(got.ID, fixedTodoTime(2026, 1, 3, 9, 0, 0), func(td *Todo) error {
		td.Title = "改后待办"
		return nil
	}); err != nil {
		t.Fatalf("UpdateTodo 失败: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"custom_str", "保留我", "labels: [x, y]", "mode: 'fast'"} {
		if !strings.Contains(text, want) {
			t.Errorf("写回内容缺少 %q:\n%s", want, text)
		}
	}

	reread, err := w.GetTodo("20260101-120000-abcd")
	if err != nil {
		t.Fatal(err)
	}
	if reread.Title != "改后待办" {
		t.Errorf("Title = %q", reread.Title)
	}
	if reread.Extra["custom_str"] != "保留我" {
		t.Errorf("重新读取 Extra[custom_str] = %v", reread.Extra["custom_str"])
	}
	if reread.Body != "正文\n" {
		t.Errorf("Body = %q", reread.Body)
	}
}

func TestTodoStatusTransition(t *testing.T) {
	w := mustInit(t)

	todo, err := w.CreateTodo(NewTodo{Title: "t", Now: fixedTodoTime(2026, 1, 1, 10, 0, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if todo.DoneAt != nil {
		t.Errorf("初始 DoneAt = %v，期望 nil", todo.DoneAt)
	}

	doneAt := fixedTodoTime(2026, 1, 2, 11, 0, 0)
	got, err := w.SetTodoStatus(todo.ID, StatusDone, doneAt)
	if err != nil {
		t.Fatalf("SetTodoStatus(done) 失败: %v", err)
	}
	if got.DoneAt == nil || !got.DoneAt.Equal(doneAt) {
		t.Errorf("done 后 DoneAt = %v，期望 %v", got.DoneAt, doneAt)
	}
	if got.Status != StatusDone {
		t.Errorf("Status = %q", got.Status)
	}

	// 再次设为 done 不应覆盖已有完成时间。
	later := fixedTodoTime(2026, 1, 3, 12, 0, 0)
	got, err = w.SetTodoStatus(todo.ID, StatusDone, later)
	if err != nil {
		t.Fatalf("再次 SetTodoStatus(done) 失败: %v", err)
	}
	if got.DoneAt == nil || !got.DoneAt.Equal(doneAt) {
		t.Errorf("重复 done 后 DoneAt = %v，期望保持 %v", got.DoneAt, doneAt)
	}

	got, err = w.SetTodoStatus(todo.ID, StatusCanceled, later)
	if err != nil {
		t.Fatalf("SetTodoStatus(canceled) 失败: %v", err)
	}
	if got.DoneAt != nil {
		t.Errorf("canceled 后 DoneAt = %v，期望 nil", got.DoneAt)
	}

	// 落盘后重新读取也应一致。
	reread, err := w.GetTodo(todo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reread.Status != StatusCanceled || reread.DoneAt != nil {
		t.Errorf("重新读取 = status %q doneAt %v", reread.Status, reread.DoneAt)
	}
}

func TestTodoUpdateValidation(t *testing.T) {
	w := mustInit(t)
	todo, err := w.CreateTodo(NewTodo{Title: "t"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := w.UpdateTodo(todo.ID, time.Time{}, func(td *Todo) error {
		td.Status = "weird"
		return nil
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("非法状态错误 = %v，期望 ErrInvalid", err)
	}
	if _, err := w.UpdateTodo(todo.ID, time.Time{}, func(td *Todo) error {
		td.Title = "  "
		return nil
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("空标题错误 = %v，期望 ErrInvalid", err)
	}
	if _, err := w.UpdateTodo("nope", time.Time{}, func(td *Todo) error { return nil }); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在待办错误 = %v，期望 ErrNotFound", err)
	}
}

func TestParseDueAndAt(t *testing.T) {
	cases := []struct {
		in       string
		date     string
		timePart string
		wantErr  bool
	}{
		{in: "2026-10-30", date: "2026-10-30"},
		{in: "2026-10-30 15:00", date: "2026-10-30", timePart: "15:00"},
		{in: "2026-10-30T15:00", date: "2026-10-30", timePart: "15:00"},
		{in: "2026-10-30T15:00:00+08:00", date: "2026-10-30", timePart: "15:00"},
		// UTC 23:00 转上海后是次日 07:00。
		{in: "2026-10-30T23:00:00Z", date: "2026-10-31", timePart: "07:00"},
		{in: "bad", wantErr: true},
	}
	for _, c := range cases {
		d, err := ParseDue(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseDue(%q) 期望报错", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseDue(%q) 失败: %v", c.in, err)
			continue
		}
		if d == nil || d.Date != c.date || d.Time != c.timePart {
			t.Errorf("ParseDue(%q) = %+v，期望 date=%q time=%q", c.in, d, c.date, c.timePart)
		}
	}

	if d, err := ParseDue(""); err != nil || d != nil {
		t.Errorf("ParseDue(\"\") = %v, %v，期望 nil, nil", d, err)
	}

	// At：无时间取 23:59:59；有时间取对应时刻。
	d := &Due{Date: "2026-10-30"}
	if got := d.At(nil).Format("2006-01-02 15:04:05 -07:00"); got != "2026-10-30 23:59:59 +08:00" {
		t.Errorf("At(无时间) = %q", got)
	}
	d.Time = "15:00"
	if got := d.At(nil).Format("2006-01-02 15:04:05 -07:00"); got != "2026-10-30 15:00:00 +08:00" {
		t.Errorf("At(有时间) = %q", got)
	}
	if got := (&Due{}).At(nil); !got.IsZero() {
		t.Errorf("空 Due At = %v，期望零值", got)
	}
}

func TestTodoSubtaskMoveCycleDelete(t *testing.T) {
	w := mustInit(t)

	root, err := w.CreateTodo(NewTodo{Title: "root"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := w.CreateTodo(NewTodo{Title: "child", Parent: root.ID})
	if err != nil {
		t.Fatal(err)
	}
	grand, err := w.CreateTodo(NewTodo{Title: "grand", Parent: child.ID})
	if err != nil {
		t.Fatal(err)
	}
	other, err := w.CreateTodo(NewTodo{Title: "other"})
	if err != nil {
		t.Fatal(err)
	}

	// 子任务查询。
	children, err := w.TodoChildren(root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 || children[0].ID != child.ID {
		t.Fatalf("TodoChildren(root) = %v", children)
	}

	// 移动 other 到 root 的第 0 位。
	if err := w.MoveTodo(other.ID, root.ID, 0); err != nil {
		t.Fatalf("MoveTodo 失败: %v", err)
	}
	children, err = w.TodoChildren(root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 2 || children[0].ID != other.ID || children[1].ID != child.ID {
		t.Errorf("移动后子任务顺序 = %v", children)
	}

	// 成环拒绝。
	if err := w.MoveTodo(root.ID, root.ID, 0); !errors.Is(err, ErrCycle) {
		t.Errorf("移到自己错误 = %v，期望 ErrCycle", err)
	}
	if err := w.MoveTodo(root.ID, child.ID, 0); !errors.Is(err, ErrCycle) {
		t.Errorf("移到子节点错误 = %v，期望 ErrCycle", err)
	}
	if err := w.MoveTodo(root.ID, grand.ID, 0); !errors.Is(err, ErrCycle) {
		t.Errorf("移到孙节点错误 = %v，期望 ErrCycle", err)
	}

	// 非递归删除有子任务应拒绝。
	if err := w.DeleteTodo(root.ID, false); !errors.Is(err, ErrHasChildren) {
		t.Fatalf("非递归删除错误 = %v，期望 ErrHasChildren", err)
	}
	if _, err := w.GetTodo(root.ID); err != nil {
		t.Fatalf("删除失败后 root 应仍存在: %v", err)
	}

	// 先把 other 移回顶层，验证递归删除只删后代。
	if err := w.MoveTodo(other.ID, "", -1); err != nil {
		t.Fatalf("移回顶层失败: %v", err)
	}

	// 递归删除全部后代。
	if err := w.DeleteTodo(root.ID, true); err != nil {
		t.Fatalf("递归删除失败: %v", err)
	}
	for _, id := range []string{root.ID, child.ID, grand.ID} {
		if _, err := w.GetTodo(id); !errors.Is(err, ErrNotFound) {
			t.Errorf("待办 %s 仍存在，错误 = %v", id, err)
		}
	}
	if _, err := w.GetTodo(other.ID); err != nil {
		t.Errorf("other 不应被删除: %v", err)
	}
}

func TestTodoLinkDedupAndInvalidURL(t *testing.T) {
	w := mustInit(t)
	todo, err := w.CreateTodo(NewTodo{Title: "t"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := w.AddTodoLink(todo.ID, TodoLink{URL: "not-a-url"}, time.Time{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("非法 URL 错误 = %v，期望 ErrInvalid", err)
	}

	now := fixedTodoTime(2026, 1, 2, 10, 0, 0)
	got, err := w.AddTodoLink(todo.ID, TodoLink{URL: "https://a.com", Title: "A"}, now)
	if err != nil {
		t.Fatalf("AddTodoLink 失败: %v", err)
	}
	if len(got.Links) != 1 || got.Links[0].URL != "https://a.com" || got.Links[0].Title != "A" {
		t.Errorf("Links = %+v", got.Links)
	}

	// 同 URL 只更新标题，不重复添加。
	got, err = w.AddTodoLink(todo.ID, TodoLink{URL: "https://a.com", Title: "改名"}, now)
	if err != nil {
		t.Fatalf("重复 AddTodoLink 失败: %v", err)
	}
	if len(got.Links) != 1 || got.Links[0].Title != "改名" {
		t.Errorf("同 URL 添加后 Links = %+v", got.Links)
	}

	got, err = w.AddTodoLink(todo.ID, TodoLink{URL: "mailto:a@b.c"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Links) != 2 {
		t.Errorf("添加第二个链接后 Links = %+v", got.Links)
	}

	got, err = w.RemoveTodoLink(todo.ID, "https://a.com", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Links) != 1 || got.Links[0].URL != "mailto:a@b.c" {
		t.Errorf("移除后 Links = %+v", got.Links)
	}
}

func TestTodoAttachmentsDedup(t *testing.T) {
	w := mustInit(t)
	todo, err := w.CreateTodo(NewTodo{Title: "t"})
	if err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(t.TempDir(), "pic.png")
	if err := os.WriteFile(src, []byte("fake png"), 0o644); err != nil {
		t.Fatal(err)
	}

	now := fixedTodoTime(2026, 1, 2, 10, 0, 0)
	got, rel1, err := w.AttachTodoFile(todo.ID, src, now)
	if err != nil {
		t.Fatalf("AttachTodoFile 失败: %v", err)
	}
	if len(got.Attachments) != 1 || got.Attachments[0] != rel1 {
		t.Errorf("Attachments = %v, rel=%q", got.Attachments, rel1)
	}

	// 相同内容再次 attach：资源文件只应有一个，附件列表不重复。
	got, rel2, err := w.AttachTodoFile(todo.ID, src, now)
	if err != nil {
		t.Fatalf("再次 AttachTodoFile 失败: %v", err)
	}
	if rel1 != rel2 {
		t.Errorf("相同内容返回不同路径: %q vs %q", rel1, rel2)
	}
	if len(got.Attachments) != 1 {
		t.Errorf("Attachments 重复: %v", got.Attachments)
	}
	entries, err := os.ReadDir(w.AssetsDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("assets 目录文件数 = %d，期望 1", len(entries))
	}

	// 移除附件只改列表，不删文件。
	got, err = w.DetachTodoFile(todo.ID, rel1, now)
	if err != nil {
		t.Fatalf("DetachTodoFile 失败: %v", err)
	}
	if len(got.Attachments) != 0 {
		t.Errorf("Detach 后 Attachments = %v", got.Attachments)
	}
	if _, err := os.Stat(filepath.Join(w.NotesDir(), rel1)); err != nil {
		t.Errorf("资源文件被删除: %v", err)
	}
}

func TestOldWorkspaceNoTodosDir(t *testing.T) {
	w := mustInit(t)
	if err := os.RemoveAll(w.TodosDir()); err != nil {
		t.Fatal(err)
	}

	todos, err := w.ListTodos()
	if err != nil {
		t.Fatalf("ListTodos 失败: %v", err)
	}
	if len(todos) != 0 {
		t.Errorf("ListTodos = %v，期望空", todos)
	}
	paths, err := w.ListWorkspaceTodos()
	if err != nil {
		t.Fatalf("ListWorkspaceTodos 失败: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("ListWorkspaceTodos = %v，期望空", paths)
	}

	// 写入时按需创建目录。
	todo, err := w.CreateTodo(NewTodo{Title: "t"})
	if err != nil {
		t.Fatalf("无 todos 目录时 CreateTodo 失败: %v", err)
	}
	if _, err := os.Stat(w.todoPath(todo.ID)); err != nil {
		t.Errorf("待办文件不存在: %v", err)
	}
}

func TestNoteListTreeUnaffectedByTodos(t *testing.T) {
	w := mustInit(t)

	note, err := w.Create(NewNote{Title: "笔记"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.CreateTodo(NewTodo{Title: "待办"}); err != nil {
		t.Fatal(err)
	}

	notes, err := w.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].ID != note.ID {
		t.Errorf("List 受 todos 影响: %v", notes)
	}

	notePaths, err := w.ListWorkspaceNotes()
	if err != nil {
		t.Fatal(err)
	}
	if len(notePaths) != 1 || !strings.HasSuffix(notePaths[0], note.ID+".md") {
		t.Errorf("ListWorkspaceNotes 受 todos 影响: %v", notePaths)
	}
	todoPaths, err := w.ListWorkspaceTodos()
	if err != nil {
		t.Fatal(err)
	}
	if len(todoPaths) != 1 || !strings.Contains(todoPaths[0], "todos"+string(filepath.Separator)) {
		t.Errorf("ListWorkspaceTodos = %v", todoPaths)
	}

	roots, err := w.Tree()
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || roots[0].Note.ID != note.ID {
		t.Errorf("Tree 受 todos 影响: %v", roots)
	}
}
