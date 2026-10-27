package store

import (
	"errors"
	"testing"
	"time"
)

func TestSetTodoStatusTreeAndAttachmentPath(t *testing.T) {
	ws, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 26, 10, 0, 0, 0, TodoLocation())
	p, _ := ws.CreateTodo(NewTodo{Title: "父", Now: now})
	c1, _ := ws.CreateTodo(NewTodo{Title: "子1", Parent: p.ID, Now: now})
	c2, _ := ws.CreateTodo(NewTodo{Title: "子2", Parent: p.ID, Now: now})
	g, _ := ws.CreateTodo(NewTodo{Title: "孙", Parent: c1.ID, Now: now})
	if _, err := ws.SetTodoStatus(c2.ID, StatusDone, now); err != nil {
		t.Fatal(err)
	}
	if n, err := ws.OpenDescendantCount(p.ID); err != nil || n != 2 {
		t.Fatalf("OpenDescendantCount = %d, %v", n, err)
	}
	_, changed, err := ws.SetTodoStatusTree(p.ID, StatusDone, now)
	if err != nil || len(changed) != 3 {
		t.Fatalf("SetTodoStatusTree changed = %v, %v（已完成的子2 不应重写）", changed, err)
	}
	if got, _ := ws.GetTodo(g.ID); got.Status != StatusDone {
		t.Fatal("孙任务应一起完成")
	}

	src := t.TempDir() + "/a.txt"
	if err := writeFileAtomic(src, []byte("hi")); err != nil {
		t.Fatal(err)
	}
	_, rel, err := ws.AttachTodoFile(p.ID, src, now)
	if err != nil {
		t.Fatal(err)
	}
	full, err := ws.TodoAttachmentPath(p.ID, rel)
	if err != nil || full == "" {
		t.Fatalf("TodoAttachmentPath = %q, %v", full, err)
	}
	if _, err := ws.TodoAttachmentPath(p.ID, "../assets/../../etc/passwd"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("列表外 / 穿越路径应拒绝: %v", err)
	}
}
