package cli

import (
	"strings"
	"testing"
)

// addPersonJSON 添加组员并返回 JSON 对象。
func (e *testEnv) addPersonJSON(args ...string) map[string]any {
	e.t.Helper()
	full := append([]string{"person", "add", "--workspace", e.workspace, "--json"}, args...)
	return e.mustRun("", full...).jsonOut(e.t)
}

func TestPersonAddListShowJSON(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()

	out := e.addPersonJSON("张三", "--alias", "三儿", "--group", "框架组", "--product", "产品线A")
	if out["name"] != "张三" {
		t.Errorf("name = %v", out["name"])
	}
	aliases, _ := out["aliases"].([]any)
	if len(aliases) != 1 || aliases[0] != "三儿" {
		t.Errorf("aliases = %v", out["aliases"])
	}

	e.mustRun("", "person", "add", "李四", "--group", "框架组", "--role", "lead", "--workspace", e.workspace)

	list := e.mustRun("", "person", "list", "--workspace", e.workspace, "--json").jsonArray(t)
	if len(list) != 2 {
		t.Fatalf("person list 条数 = %d，期望 2", len(list))
	}

	show := e.mustRun("", "person", "show", "三儿", "--workspace", e.workspace, "--json").jsonOut(t)
	if show["name"] != "张三" {
		t.Errorf("按别名 show = %v", show["name"])
	}

	// 姓名冲突：在职人员姓名唯一。
	conflict := e.run("", "person", "add", "张三", "--workspace", e.workspace, "--json")
	if conflict.code != 1 || conflict.jsonErrorCode(t) != "CONFLICT" {
		t.Errorf("姓名冲突结果 = %+v，期望 CONFLICT/1", conflict)
	}
}

func TestReportAddListShowStatusConfigJSON(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()

	e.addPersonJSON("张三", "--group", "框架组")
	e.addPersonJSON("李四", "--group", "框架组", "--role", "lead")

	text := "【周报】张三 2026-10-29\n完成核心库\n\n【周报】李四 2026/10/29\n完成 CLI\n"
	added := e.mustRun(text, "report", "add", "--workspace", e.workspace, "--json").jsonArray(t)
	if len(added) != 2 {
		t.Fatalf("report add 条数 = %d，期望 2", len(added))
	}

	list := e.mustRun("", "report", "list", "--week", "2026-W44", "--workspace", e.workspace, "--json").jsonArray(t)
	if len(list) != 2 {
		t.Fatalf("report list 条数 = %d，期望 2", len(list))
	}

	show := e.mustRun("", "report", "show", "2026-W44", "张三", "--workspace", e.workspace, "--json").jsonOut(t)
	if show["week"] != "2026-W44" || show["name"] != "张三" {
		t.Errorf("report show = %v", show)
	}
	if body, _ := show["body"].(string); !strings.Contains(body, "完成核心库") {
		t.Errorf("report show body = %q", show["body"])
	}

	status := e.mustRun("", "report", "status", "--week", "2026-W44", "--workspace", e.workspace, "--json").jsonOut(t)
	entries, _ := status["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("status entries = %d，期望 1（张三）", len(entries))
	}
	extra, _ := status["extra"].([]any)
	if len(extra) != 1 {
		t.Fatalf("status extra = %d，期望 1（李四交 member 层级）", len(extra))
	}
	if status["level"] != "member" || status["week"] != "2026-W44" {
		t.Errorf("status week/level = %v/%v", status["week"], status["level"])
	}

	cfg := e.mustRun("", "report", "config", "--weekday", "3", "--workspace", e.workspace, "--json").jsonOut(t)
	if cfg["weekday"] != float64(3) {
		t.Errorf("report config weekday = %v", cfg["weekday"])
	}

	rm := e.mustRun("", "report", "rm", "2026-W44", "张三", "--workspace", e.workspace, "--json").jsonOut(t)
	if rm["deleted"] == nil || rm["deleted"] == "" {
		t.Errorf("report rm = %v", rm)
	}
}

func TestReportAddDryRunJSON(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()
	e.addPersonJSON("张三", "--group", "框架组")

	preview := e.mustRun("【周报】张三 2026-10-29\n内容\n", "report", "add", "--dry-run", "--workspace", e.workspace, "--json").jsonArray(t)
	if len(preview) != 1 {
		t.Fatalf("dry-run 条数 = %d", len(preview))
	}
	item := preview[0].(map[string]any)
	if item["week"] != "2026-W44" || item["personName"] != "张三" {
		t.Errorf("dry-run 预览 = %v", item)
	}
	if item["reason"] != nil && item["reason"] != "" {
		t.Errorf("dry-run 不应有 reason: %v", item["reason"])
	}
}

func TestReportAddDuplicateWritesLast(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()
	e.addPersonJSON("张三", "--group", "框架组")

	text := "【周报】张三 2026-10-29\n第一份\n【周报】张三 2026/10/29\n第二份\n"

	preview := e.mustRun(text, "report", "add", "--dry-run", "--workspace", e.workspace, "--json").jsonArray(t)
	if len(preview) != 2 {
		t.Fatalf("dry-run 条数 = %d，期望 2", len(preview))
	}
	first := preview[0].(map[string]any)
	if first["duplicate"] != true {
		t.Errorf("第 1 份 duplicate = %v，期望 true", first["duplicate"])
	}
	if first["reason"] == nil || first["reason"] == "" {
		t.Errorf("第 1 份应给出重复原因: %v", first["reason"])
	}
	second := preview[1].(map[string]any)
	if second["duplicate"] != nil && second["duplicate"] != false {
		t.Errorf("第 2 份 duplicate = %v，期望 false/nil", second["duplicate"])
	}

	added := e.mustRun(text, "report", "add", "--workspace", e.workspace, "--json").jsonArray(t)
	if len(added) != 1 {
		t.Fatalf("report add 条数 = %d，期望 1（只写最后一份）", len(added))
	}

	list := e.mustRun("", "report", "list", "--week", "2026-W44", "--workspace", e.workspace, "--json").jsonArray(t)
	if len(list) != 1 {
		t.Fatalf("report list 条数 = %d，期望 1", len(list))
	}

	show := e.mustRun("", "report", "show", "2026-W44", "张三", "--workspace", e.workspace, "--json").jsonOut(t)
	body, _ := show["body"].(string)
	if !strings.Contains(body, "第二份") || strings.Contains(body, "第一份") {
		t.Errorf("应只写最后一份: %q", body)
	}
}

func TestReportAddErrors(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()
	e.addPersonJSON("张三", "--group", "框架组")

	// 无头行且未指定 --person。
	res := e.run("没有头行\n", "report", "add", "--workspace", e.workspace, "--json")
	if res.code != 1 || res.jsonErrorCode(t) != "PARSE_FAILED" {
		t.Errorf("无头行结果 = %+v，期望 PARSE_FAILED/1", res)
	}

	// 姓名不匹配。
	res = e.run("【周报】不存在的人 2026-10-29\n内容\n", "report", "add", "--workspace", e.workspace, "--json")
	if res.code != 1 || res.jsonErrorCode(t) != "PERSON_NOT_FOUND" {
		t.Errorf("姓名不匹配结果 = %+v，期望 PERSON_NOT_FOUND/1", res)
	}

	// 多份文本指定 --person。
	res = e.run("【周报】张三 2026-10-29\nA\n【周报】张三 2026-10-29\nB\n", "report", "add", "--person", "张三", "--workspace", e.workspace, "--json")
	if res.code != 1 || res.jsonErrorCode(t) != "USAGE" {
		t.Errorf("多份 --person 结果 = %+v，期望 USAGE/1", res)
	}
}
