package main

import (
	"strings"
	"testing"

	"github.com/axu4/kuibu/internal/store"
)

func TestTriageServiceFlow(t *testing.T) {
	reports, workspace, _ := newTestReports(t)
	if _, err := reports.CreatePerson(PersonInput{Name: "组长", Group: "框架组", Role: store.RoleLead}); err != nil {
		t.Fatal(err)
	}
	zhang, err := reports.CreatePerson(PersonInput{Name: "张三", Group: "框架组", Role: store.RoleMember})
	if err != nil {
		t.Fatal(err)
	}
	li, err := reports.CreatePerson(PersonInput{Name: "李四", Group: "框架组", Role: store.RoleMember})
	if err != nil {
		t.Fatal(err)
	}
	wsImpl := workspace.workspace()
	if _, _, err := wsImpl.AddReportProject("云平台项目"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := wsImpl.AddReportProject("数据中台项目"); err != nil {
		t.Fatal(err)
	}
	for _, m := range []struct {
		id, name, body string
	}{
		{zhang.ID, zhang.Name, "## 本周完成\n- 【支付】退款接口联调\n## 下周计划\n- 【支付】补齐用例\n## 风险与求助\n- 无\n"},
		{li.ID, li.Name, "## 本周完成\n- 【数据】报表导出 Excel\n## 风险与求助\n- 测试环境不足\n"},
	} {
		if _, err := wsImpl.SaveGeneratedDraft(store.GeneratedDraft{
			Person: m.id, Name: m.name, Week: "2026-W44", Body: m.body,
		}, false); err != nil {
			t.Fatal(err)
		}
	}

	triageSvc := newTriageService(workspace)
	triageSvc.now = fixedNow
	var events []string
	triageSvc.emit = func(name string, data any) { events = append(events, name) }

	data, err := triageSvc.Triage("2026-W44", "框架组")
	if err != nil {
		t.Fatal(err)
	}
	if data.Counts.Total != 4 || data.Counts.Undecided != 4 || len(data.Projects) != 2 {
		t.Fatalf("triage = %+v", data)
	}
	if len(data.Sections) != 3 || data.Sections[0].Title != "本周总结" || data.Sections[0].Total != 2 {
		t.Fatalf("sections = %+v", data.Sections)
	}
	byText := map[string]TriageItemData{}
	for _, it := range data.Items {
		byText[it.Text] = it
	}
	decide := func(text, decision, project string) TriageData {
		t.Helper()
		out, err := triageSvc.TriageDecide("2026-W44", "框架组", byText[text].ID, decision, project)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	decide("退款接口联调", store.TriageAccepted, data.Projects[0].ID)
	decide("补齐用例", store.TriageAccepted, data.Projects[0].ID)
	decide("报表导出 Excel", store.TriageAccepted, data.Projects[1].ID)
	data = decide("测试环境不足", store.TriageAccepted, data.Projects[0].ID)
	if !data.AllJudged || data.Counts.Accepted != 4 {
		t.Fatalf("决策后 = %+v", data.Counts)
	}
	if len(events) == 0 || events[0] != triageChangedEvent {
		t.Fatalf("应发 triage:changed，实际 %v", events)
	}

	groups, err := reports.ProjectGroups("2026-W44", "框架组")
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 || groups[0].Exists || groups[0].Candidates != 3 {
		t.Fatalf("项目列表 = %+v", groups)
	}

	p1 := data.Projects[0]
	g, err := reports.AssembleProjectGroup("2026-W44", "框架组", p1.ID, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !g.Exists || g.Project != p1.ID || g.ProjectName != p1.Name || g.Status != "draft" {
		t.Fatalf("组装结果 = %+v", g)
	}
	for _, want := range []string{"## 本周总结", "## 下周计划", "## 风险与求助", "退款接口联调"} {
		if !strings.Contains(g.Body, want) {
			t.Errorf("正文缺少 %q:\n%s", want, g.Body)
		}
	}
	for _, no := range []string{"张三", "李四", "报表导出"} {
		if strings.Contains(g.Body, no) {
			t.Errorf("正文不应出现 %q:\n%s", no, g.Body)
		}
	}
	if _, err := reports.SaveProjectGroup("2026-W44", "框架组", p1.ID, g.Body+"- 人工补充\n"); err != nil {
		t.Fatal(err)
	}
	groups, _ = reports.ProjectGroups("2026-W44", "框架组")
	if !groups[0].HumanEdited || groups[0].Status != "draft" {
		t.Fatalf("保存后列表 = %+v", groups[0])
	}
	// 人工修改后再生成应返回 HUMAN_EDITS_EXIST。
	if _, err := reports.AssembleProjectGroup("2026-W44", "框架组", p1.ID, false, false); err == nil {
		t.Fatal("覆盖人工修改应报错")
	}
	if _, err := reports.FinalizeProjectGroup("2026-W44", "框架组", p1.ID); err != nil {
		t.Fatal(err)
	}
	groups, _ = reports.ProjectGroups("2026-W44", "框架组")
	if groups[0].Status != "final" || groups[0].FinalizedAt == "" {
		t.Fatalf("定稿后列表 = %+v", groups[0])
	}
	if _, err := reports.UnfinalizeProjectGroup("2026-W44", "框架组", p1.ID); err != nil {
		t.Fatal(err)
	}
}
