package cli

import (
	"strings"
	"testing"

	"github.com/axu4/kuibu/internal/store"
)

func TestReportTriageProjectFlow(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()

	lead := e.addPersonJSON("组长", "--group", "框架组", "--role", "lead")
	zhang := e.addPersonJSON("张三", "--group", "框架组")
	li := e.addPersonJSON("李四", "--group", "框架组")
	_ = lead

	ws, err := store.Open(e.workspace)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []struct {
		id, name, body string
	}{
		{zhang["id"].(string), "张三", "## 本周完成\n- 【支付】退款接口联调\n## 下周计划\n- 【支付】补齐用例\n"},
		{li["id"].(string), "李四", "## 本周完成\n- 【数据】报表导出 Excel\n## 风险与求助\n- 测试环境不足\n"},
	} {
		if _, err := ws.SaveGeneratedDraft(store.GeneratedDraft{Person: m.id, Name: m.name, Week: "2026-W44", Body: m.body}, false); err != nil {
			t.Fatal(err)
		}
	}

	// 项目配置。
	first := e.mustRun("", "report", "project", "add", "云平台项目", "--workspace", e.workspace, "--json").jsonOut(t)
	second := e.mustRun("", "report", "project", "add", "数据中台项目", "--workspace", e.workspace, "--json").jsonOut(t)
	p1 := first["project"].(map[string]any)["id"].(string)
	p2 := second["project"].(map[string]any)["id"].(string)
	list := e.mustRun("", "report", "projects", "--workspace", e.workspace, "--json").jsonArray(t)
	if len(list) != 2 {
		t.Fatalf("项目数 = %d", len(list))
	}

	triage := e.mustRun("", "report", "triage", "list", "--week", "2026-W44", "--workspace", e.workspace, "--json").jsonOut(t)
	items, _ := triage["items"].([]any)
	if len(items) != 4 {
		t.Fatalf("筛选项 = %d: %v", len(items), triage)
	}
	byText := map[string]string{}
	for _, raw := range items {
		it := raw.(map[string]any)
		byText[it["text"].(string)] = it["id"].(string)
	}
	for text, p := range map[string]string{
		"退款接口联调": p1, "补齐用例": p1, "报表导出 Excel": p2, "测试环境不足": p1,
	} {
		e.mustRun("", "report", "triage", "set", byText[text], "--accept", "--project", p, "--week", "2026-W44", "--workspace", e.workspace)
	}

	out := e.mustRun("", "report", "triage", "list", "--week", "2026-W44", "--workspace", e.workspace, "--json").jsonOut(t)
	counts := out["counts"].(map[string]any)
	if counts["undecided"].(float64) != 0 || counts["accepted"].(float64) != 4 {
		t.Fatalf("counts = %v", counts)
	}

	e.mustRun("", "report", "group", "assemble", "--project", p1, "--week", "2026-W44", "--workspace", e.workspace)
	asm := e.mustRun("", "report", "group", "show", "--project", p1, "--week", "2026-W44", "--workspace", e.workspace, "--json").jsonOut(t)
	body, _ := asm["body"].(string)
	for _, want := range []string{"## 本周总结", "## 下周计划", "## 其他事项", "退款接口联调"} {
		if !strings.Contains(body, want) {
			t.Errorf("p1 组报缺少 %q: %s", want, body)
		}
	}
	for _, no := range []string{"张三", "李四", "报表导出"} {
		if strings.Contains(body, no) {
			t.Errorf("p1 组报不应出现 %q", no)
		}
	}
	groups := e.mustRun("", "report", "group", "list", "--week", "2026-W44", "--workspace", e.workspace, "--json").jsonArray(t)
	if len(groups) != 2 {
		t.Fatalf("group list = %v", groups)
	}
	e.mustRun("", "report", "group", "finalize", "--project", p1, "--week", "2026-W44", "--workspace", e.workspace)
	exported := e.mustRun("", "report", "export", "group", "--project", p1, "--week", "2026-W44", "--workspace", e.workspace)
	if !strings.Contains(exported.stdout, "## 本周总结") || strings.Contains(exported.stdout, "张三") {
		t.Fatalf("导出 = %q", exported.stdout)
	}
}
