package cli

import (
	"strings"
	"testing"

	"github.com/axu4/kuibu/internal/store"
)

func TestReportSEProjectFlow(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()
	w := []string{"--workspace", e.workspace}

	me := e.addPersonJSON("勇旭", "--group", "框架组", "--role", "lead")
	e.addPersonJSON("老陈", "--group", "存储组", "--role", "lead")
	pa := e.mustRun("", append([]string{"report", "project", "add", "A 项目", "--json"}, w...)...).jsonOut(t)["project"].(map[string]any)["id"].(string)
	pb := e.mustRun("", append([]string{"report", "project", "add", "B 项目", "--json"}, w...)...).jsonOut(t)["project"].(map[string]any)["id"].(string)

	if res := e.run("", append([]string{"report", "project", "se", "A 项目"}, w...)...); res.code == 0 {
		t.Fatal("不给 --mine / --se 应该报错")
	}
	e.mustRun("", append([]string{"report", "project", "se", "A 项目", "--mine", "--groups", "框架组,存储组"}, w...)...)
	e.mustRun("", append([]string{"report", "project", "se", "B 项目", "--se", "老王"}, w...)...)
	out := e.mustRun("", append([]string{"report", "projects"}, w...)...).stdout
	if !strings.Contains(out, "我是 SE") || !strings.Contains(out, "交给 老王") {
		t.Fatalf("项目列表 = %s", out)
	}
	mine := e.mustRun("", append([]string{"report", "se", "projects", "--json"}, w...)...).jsonArray(t)
	if len(mine) != 1 || mine[0].(map[string]any)["id"] != pa {
		t.Fatalf("我当 SE 的项目 = %v", mine)
	}

	// 框架组 A、B 两份项目组报，定稿。
	ws, err := store.Open(e.workspace)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{pa, pb} {
		if _, err := ws.SaveGeneratedDraft(store.GeneratedDraft{
			Kind: store.GroupReportKind(p), Group: "框架组", Person: me["id"].(string), Name: "勇旭",
			Week: "2026-W44", Body: "## 本周总结\n- " + p + " 进展\n## 下周计划\n- 继续\n",
		}, false); err != nil {
			t.Fatal(err)
		}
		e.mustRun("", append([]string{"report", "group", "finalize", "--week", "2026-W44", "--group", "框架组", "--project", p}, w...)...)
	}

	// B 交给别人：标记已交；A 是我自己的不能标。
	dj := e.mustRun("", append([]string{"report", "group", "deliver", "--week", "2026-W44", "--group", "框架组", "--project", "B 项目", "--json"}, w...)...).jsonOut(t)
	if dj["delivery"].(map[string]any)["deliveredAt"] == nil {
		t.Fatalf("标记已交 = %v", dj)
	}
	if res := e.run("", append([]string{"report", "group", "deliver", "--week", "2026-W44", "--group", "框架组", "--project", "A 项目"}, w...)...); res.code == 0 {
		t.Fatal("我是 SE 的项目不能标记已交")
	}
	if res := e.run("", append([]string{"report", "group", "submit", "--week", "2026-W44", "--group", "框架组", "--project", "A 项目"}, w...)...); res.code == 0 || !strings.Contains(res.stderr, "report se") {
		t.Fatalf("按项目 submit 应提示改用 report se: %+v", res)
	}

	// A 的 SE：框架组自动引用，存储组没交 → 生成需确认（退出 7）。
	st := e.mustRun("", append([]string{"report", "se", "status", "--project", "A 项目", "--week", "2026-W44", "--json"}, w...)...).jsonOut(t)
	if st["counts"].(map[string]any)["received"].(float64) != 1 {
		t.Fatalf("SE 收件 = %v", st)
	}
	if res := e.run("", append([]string{"report", "se", "generate", "--project", "A 项目", "--week", "2026-W44"}, w...)...); res.code != 7 {
		t.Fatalf("缺组生成退出码 = %d，期望 7", res.code)
	}
	e.mustRun("本周：\n- 3.2 发布\n下周：\n- 3.3 冻结\n", append([]string{"report", "se", "add", "--project", "A 项目", "--group", "存储组", "--week", "2026-W44"}, w...)...)
	show := e.mustRun("", append([]string{"report", "se", "show", "--project", "A 项目", "--group", "框架组", "--week", "2026-W44", "--json"}, w...)...).jsonOut(t)
	if show["source"] != "linked" {
		t.Fatalf("框架组来源 = %v", show)
	}
	e.mustRun("", append([]string{"report", "se", "generate", "--project", "A 项目", "--week", "2026-W44"}, w...)...)
	rep := e.mustRun("", append([]string{"report", "se", "report", "--project", "A 项目", "--week", "2026-W44", "--json"}, w...)...).jsonOut(t)
	body := rep["body"].(string)
	if !strings.Contains(body, "### 框架组") || !strings.Contains(body, "### 存储组") || !strings.Contains(body, "- 3.3 冻结") {
		t.Fatalf("项目报 = %s", body)
	}
	e.mustRun("", append([]string{"report", "se", "finalize", "--project", "A 项目", "--week", "2026-W44"}, w...)...)
	if res := e.run("", append([]string{"report", "se", "generate", "--project", "A 项目", "--week", "2026-W44"}, w...)...); res.code != 9 {
		t.Fatalf("定稿后重新生成退出码 = %d，期望 9", res.code)
	}
	e.mustRun("", append([]string{"report", "se", "rm", "--project", "A 项目", "--group", "存储组", "--week", "2026-W44"}, w...)...)

	// 月报同一套：--month。
	e.mustRun("## 本月总结\n- 月度进展\n", append([]string{"report", "se", "add", "--project", "A 项目", "--group", "存储组", "--month", "2026-10"}, w...)...)
	mst := e.mustRun("", append([]string{"report", "se", "status", "--project", "A 项目", "--month", "2026-10", "--json"}, w...)...).jsonOut(t)
	if mst["monthly"] != true || mst["counts"].(map[string]any)["received"].(float64) != 1 {
		t.Fatalf("月报收件 = %v", mst)
	}
}
