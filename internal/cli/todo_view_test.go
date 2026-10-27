package cli

import (
	"strings"
	"testing"
)

func TestTodoDoneWithChildren(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()
	ws := func(a ...string) []string { return append(a, "--workspace", e.workspace) }

	parent := todoID(t, e.addTodo("发版"))
	child := todoID(t, e.mustRun("", ws("todo", "sub", parent, "写说明", "--json")...).jsonOut(t))
	grand := todoID(t, e.mustRun("", ws("todo", "sub", child, "配图", "--json")...).jsonOut(t))

	// 有未完成子任务：不带参数返回 8 并说明数量，且本条不变。
	res := e.run("", ws("todo", "done", parent, "--json")...)
	if res.code != NeedsConfirm || !strings.Contains(res.stderr, "2 个未完成的子任务") {
		t.Fatalf("应返回 8: %+v", res)
	}
	if st := e.mustRun("", ws("todo", "show", parent, "--json")...).jsonOut(t)["status"]; st != "todo" {
		t.Fatalf("未确认时不应改状态: %v", st)
	}
	if res := e.run("", ws("todo", "done", parent, "--with-children", "--only-parent")...); res.code != Error {
		t.Fatalf("两个参数同时给应报用法错误: %+v", res)
	}

	// --only-parent：只完成本条。
	e.mustRun("", ws("todo", "done", parent, "--only-parent")...)
	if st := e.mustRun("", ws("todo", "show", child, "--json")...).jsonOut(t)["status"]; st != "todo" {
		t.Fatalf("--only-parent 不应动子任务: %v", st)
	}

	// --with-children：连同全部后代。
	e.mustRun("", ws("todo", "reopen", parent)...)
	e.mustRun("", ws("todo", "done", parent, "--with-children")...)
	for _, id := range []string{child, grand} {
		if st := e.mustRun("", ws("todo", "show", id, "--json")...).jsonOut(t)["status"]; st != "done" {
			t.Fatalf("%s 应一起完成: %v", id, st)
		}
	}
	// 没有未完成子任务时不需要参数。
	e.mustRun("", ws("todo", "reopen", grand)...)
	e.mustRun("", ws("todo", "done", grand)...)
}

func TestTodoListView(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow() // 2026-10-26 周一
	ws := func(a ...string) []string { return append(a, "--workspace", e.workspace) }

	overdue := todoID(t, e.addTodo("逾期 2026-10-20"))
	today := todoID(t, e.addTodo("今天 交周报"))
	thisWeek := todoID(t, e.addTodo("周五 复盘"))
	todoID(t, e.addTodo("下周三 规划"))
	todoID(t, e.addTodo("没有日期"))
	done := todoID(t, e.addTodo("已完成 今天"))
	e.mustRun("", ws("todo", "done", done)...)

	ids := func(view string) []string {
		var out []string
		for _, it := range e.mustRun("", ws("todo", "list", "--view", view, "--json")...).jsonArray(t) {
			out = append(out, it.(map[string]any)["id"].(string))
		}
		return out
	}
	if got := ids("today"); len(got) != 2 || !contains(got, overdue) || !contains(got, today) {
		t.Fatalf("today（含逾期）= %v", got)
	}
	if got := ids("week"); len(got) != 2 || !contains(got, today) || !contains(got, thisWeek) {
		t.Fatalf("week = %v", got)
	}
	if got := ids("all"); len(got) != 5 {
		t.Fatalf("all = %v", got)
	}
	if got := ids("done"); len(got) != 1 || got[0] != done {
		t.Fatalf("done = %v", got)
	}
	if res := e.run("", ws("todo", "list", "--view", "month")...); res.code != Error {
		t.Fatalf("非法视图应报错: %+v", res)
	}
	if res := e.run("", ws("todo", "list", "--view", "today", "--due", "week")...); res.code != Error {
		t.Fatalf("--view 与 --due 不能同时使用: %+v", res)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
