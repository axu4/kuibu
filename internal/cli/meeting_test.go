package cli

import (
	"strings"
	"testing"
)

func TestMeetingAndExtractTodos(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()
	ws := func(a ...string) []string { return append(a, "--workspace", e.workspace) }

	first := e.mustRun("", ws("meeting", "open", "--json")...).jsonOut(t)
	if first["created"] != true || first["week"] != "2026-W44" || first["title"] != "周会 2026-W44（10-26）" {
		t.Fatalf("meeting open = %v", first)
	}
	again := e.mustRun("", ws("meeting", "open", "--json")...).jsonOut(t)
	if again["id"] != first["id"] || again["created"] != false {
		t.Fatalf("同周再次 open 应幂等: %v", again)
	}
	id := first["id"].(string)
	show := e.mustRun("", ws("note", "show", id, "--json")...).jsonOut(t)
	if show["type"] != "meeting" || show["parent"] == "" {
		t.Fatalf("周会笔记 = %v", show)
	}
	tree := e.mustRun("", ws("tree")...).stdout
	if !strings.Contains(tree, "周会") {
		t.Fatalf("笔记树应有「周会」节点:\n%s", tree)
	}

	body := "## 待办\n- [ ] 张三 明天 发版说明 #发布\n  - [ ] 李四 补单测\n- [x] 已完成的不抽\n- [ ] \n"
	e.mustRun(body, ws("note", "edit", id, "--stdin")...)

	dry := e.mustRun("", ws("note", "extract-todos", id, "--dry-run", "--json")...).jsonArray(t)
	if len(dry) != 2 {
		t.Fatalf("应识别 2 条: %v", dry)
	}
	c0 := dry[0].(map[string]any)
	tags, _ := c0["tags"].([]any)
	if c0["title"] != "张三 发版说明" || c0["due"] == nil || len(tags) != 2 {
		t.Fatalf("候选 = %v", c0)
	}
	if list := e.mustRun("", ws("todo", "list", "--json")...).jsonArray(t); len(list) != 0 {
		t.Fatalf("--dry-run 不应创建: %v", list)
	}

	created := e.mustRun("", ws("note", "extract-todos", id, "--json")...).jsonArray(t)
	if len(created) != 2 {
		t.Fatalf("应创建 2 条: %v", created)
	}
	if created[0].(map[string]any)["source"] != id {
		t.Fatalf("source 应为笔记 ID: %v", created[0])
	}
	again2 := e.mustRun("", ws("note", "extract-todos", id, "--json")...).jsonArray(t)
	if len(again2) != 0 {
		t.Fatalf("再执行不应重复创建: %v", again2)
	}
	// 新增一行后只抽新行。
	e.mustRun(body+"- [ ] 王五 整理会议纪要\n", ws("note", "edit", id, "--stdin")...)
	if n := e.mustRun("", ws("note", "extract-todos", id, "--json")...).jsonArray(t); len(n) != 1 {
		t.Fatalf("应只抽新行: %v", n)
	}
	if list := e.mustRun("", ws("meeting", "list", "--json")...).jsonArray(t); len(list) != 1 {
		t.Fatalf("meeting list = %v", list)
	}
}
