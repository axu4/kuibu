package cli

import (
	"strings"
	"testing"
)

const testMessageLink = "message://%3C20261029.abc123@mail.example.com%3E"

func TestTodoAddAllInOne(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()

	out := e.addTodo("回复客户报价", "--raw", "--status", "doing", "--priority", "high",
		"--link", testMessageLink+" 原邮件", "--link", "https://example.com",
		"--source", "mail", "--external-id", "<abc@mail.example.com>")
	if out["action"] != "created" {
		t.Errorf("action = %v，期望 created", out["action"])
	}
	todo := todoOut(t, out)
	if todo["status"] != "doing" || todo["priority"] != "high" || todo["source"] != "mail" || todo["externalId"] != "<abc@mail.example.com>" {
		t.Fatalf("字段不对: %v", todo)
	}
	links, _ := todo["links"].([]any)
	if len(links) != 2 {
		t.Fatalf("links = %v", todo["links"])
	}
	first, _ := links[0].(map[string]any)
	if first["url"] != testMessageLink || first["title"] != "原邮件" {
		t.Errorf("第一条链接 = %v", first)
	}
}

func TestTodoLinkMessageURL(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()
	id := todoID(t, e.addTodo("看邮件"))
	e.mustRun("", "todo", "link", id, testMessageLink, "--workspace", e.workspace)
	show := e.mustRun("", "todo", "show", id, "--workspace", e.workspace, "--json").jsonOut(t)
	links, _ := show["links"].([]any)
	if len(links) != 1 || links[0].(map[string]any)["url"] != testMessageLink {
		t.Fatalf("links = %v", show["links"])
	}

	res := e.run("", "todo", "link", id, "javascript:alert(1)", "--workspace", e.workspace, "--json")
	if res.code == 0 || res.jsonErrorCode(t) != "INVALID" {
		t.Fatalf("javascript: 应被拒绝: code=%d stderr=%s", res.code, res.stderr)
	}
}

func TestTodoExternalIDUpsertAndQuery(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()

	id := todoID(t, e.addTodo("回复 A", "--raw", "--source", "mail", "--external-id", "<a@x>"))
	e.addTodo("别的事", "--raw")

	res := e.run("", "todo", "add", "回复 A", "--raw", "--external-id", "<a@x>", "--workspace", e.workspace, "--json")
	if res.code != 1 || res.jsonErrorCode(t) != "DUPLICATE" {
		t.Fatalf("重复 external_id: code=%d stderr=%s", res.code, res.stderr)
	}
	if res := e.run("", "todo", "add", "x", "--upsert", "--workspace", e.workspace, "--json"); res.jsonErrorCode(t) != "USAGE" {
		t.Fatalf("--upsert 缺 --external-id 应报 USAGE: %s", res.stderr)
	}

	out := e.addTodo("回复 A（改）", "--raw", "--external-id", "<a@x>", "--upsert", "--priority", "low", "--link", testMessageLink)
	if out["action"] != "updated" || todoID(t, out) != id {
		t.Fatalf("upsert 应更新原待办: %v", out)
	}
	todo := todoOut(t, out)
	if todo["title"] != "回复 A（改）" || todo["priority"] != "low" || todo["source"] != "mail" {
		t.Fatalf("upsert 结果: %v", todo)
	}

	// 已完成的也能按 external_id 查到。
	e.mustRun("", "todo", "done", id, "--workspace", e.workspace)
	list := e.todoListJSON("--external-id", "<a@x>")
	if len(list) != 1 || list[0].(map[string]any)["id"] != id {
		t.Fatalf("按 external-id 查询 = %v", list)
	}
	if list := e.todoListJSON("--source", "mail"); len(list) != 1 {
		t.Fatalf("按 source 查询 = %v", list)
	}
	if list := e.todoListJSON("--external-id", "<none>"); len(list) != 0 {
		t.Fatalf("不存在的 external-id 应为空: %v", list)
	}
	if out := e.addTodo("新邮件", "--raw", "--external-id", "<b@x>", "--upsert"); out["action"] != "created" {
		t.Fatalf("upsert 无匹配应新建: %v", out)
	}
}

func TestTodoPriorityEditListSort(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()

	a := todoID(t, e.addTodo("A", "--raw"))
	b := todoID(t, e.addTodo("B", "--raw", "--priority", "low"))
	c := todoID(t, e.addTodo("C", "--raw"))
	e.mustRun("", "todo", "edit", c, "--priority", "high", "--workspace", e.workspace)

	ids := func(list []any) []string {
		var out []string
		for _, v := range list {
			out = append(out, v.(map[string]any)["id"].(string))
		}
		return out
	}
	if got := ids(e.todoListJSON()); strings.Join(got, ",") != strings.Join([]string{a, b, c}, ",") {
		t.Errorf("默认排序不应受优先级影响: %v", got)
	}
	if got := ids(e.todoListJSON("--sort", "priority")); strings.Join(got, ",") != strings.Join([]string{c, b, a}, ",") {
		t.Errorf("--sort priority = %v", got)
	}
	if got := ids(e.todoListJSON("--priority", "high,none")); strings.Join(got, ",") != strings.Join([]string{a, c}, ",") {
		t.Errorf("--priority high,none = %v", got)
	}
	e.mustRun("", "todo", "edit", c, "--priority", "none", "--workspace", e.workspace)
	if got := ids(e.todoListJSON("--priority", "high")); len(got) != 0 {
		t.Errorf("清除优先级后不应再命中 high: %v", got)
	}
	if res := e.run("", "todo", "edit", c, "--priority", "urgent", "--workspace", e.workspace, "--json"); res.jsonErrorCode(t) != "USAGE" {
		t.Errorf("非法优先级应报 USAGE: %s", res.stderr)
	}
}

func TestSubcommandHelp(t *testing.T) {
	e := newTestEnv(t)
	for _, args := range [][]string{
		{"todo", "add", "--help"},
		{"todo", "link", "-h"},
		{"todo", "--help"},
		{"note", "new", "-h"},
		{"report", "group", "--help"},
		{"report", "month", "show", "--help"},
		{"llm", "add", "-h"},
		{"--help"},
	} {
		res := e.run("", args...)
		if res.code != 0 || !strings.Contains(res.stdout, "用法") {
			t.Errorf("%v: code=%d stdout=%q stderr=%q", args, res.code, res.stdout, res.stderr)
		}
	}
	res := e.run("", "todo", "add", "--help")
	for _, flag := range []string{"--status", "--link", "--priority", "--source", "--external-id", "--upsert"} {
		if !strings.Contains(res.stdout, flag) {
			t.Errorf("todo add --help 缺少 %s:\n%s", flag, res.stdout)
		}
	}
}
