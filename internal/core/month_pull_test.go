package core

import (
	"errors"
	"strings"
	"testing"

	"github.com/axu4/kuibu/internal/store"
)

func TestMonthCandidateWeeks(t *testing.T) {
	ws := newReportWorkspace(t)
	weeks, err := MonthCandidateWeeks(ws, "2026-10")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"2026-W39", "2026-W40", "2026-W41", "2026-W42", "2026-W43", "2026-W44", "2026-W45"}
	if len(weeks) != len(want) {
		t.Fatalf("weeks = %v", weeks)
	}
	for i := range want {
		if weeks[i] != want[i] {
			t.Fatalf("weeks = %v，期望 %v", weeks, want)
		}
	}
}

func TestMonthExportProject(t *testing.T) {
	ws := newReportWorkspace(t)
	now := reportNow()
	lead, err := ws.CreatePerson(store.NewPerson{Name: "组长", Group: "框架组", Role: store.RoleLead, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.AddReportProject("云平台项目"); err != nil {
		t.Fatal(err)
	}
	projects, _ := ws.ReportProjects()
	p1 := projects[0]
	if _, err := ws.SaveGeneratedDraft(store.GeneratedDraft{
		Kind: store.GroupReportKind(p1.ID), Group: "框架组", Person: lead.ID, Name: lead.Name,
		Week: "2026-W40", Body: "## 本周总结\n- A\n## 下周计划\n- B\n", Now: now,
	}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.FinalizeDraftKind(store.GroupReportKind(p1.ID), "2026-W40", lead.ID, now); err != nil {
		t.Fatal(err)
	}
	d, err := AssembleMonthProject(ws, "2026-10", "框架组", p1.ID, true, false, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.FinalizeDraftKind(store.GroupReportKind(p1.ID), "2026-10", lead.ID, now); err != nil {
		t.Fatal(err)
	}
	doc, err := ExportFinalProject(ws, "month-group-project", "2026-10", "框架组", p1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if doc.FileName != "框架组-组月报-2026-10-云平台项目.md" || !strings.Contains(doc.Body, "## 本月总结") || d.Status != store.DraftStatusDraft {
		t.Fatalf("export = %+v", doc)
	}
}

func TestMonthSourceDataAndCandidates(t *testing.T) {
	ws := newReportWorkspace(t)
	now := reportNow()
	lead, err := ws.CreatePerson(store.NewPerson{Name: "组长", Group: "框架组", Role: store.RoleLead, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.AddReportProject("云平台项目"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.AddReportProject("数据中台项目"); err != nil {
		t.Fatal(err)
	}
	projects, err := ws.ReportProjects()
	if err != nil {
		t.Fatal(err)
	}
	p1, p2 := projects[0], projects[1]

	write := func(week, projectID, body string, final bool) {
		t.Helper()
		if _, err := ws.SaveGeneratedDraft(store.GeneratedDraft{
			Kind: store.GroupReportKind(projectID), Group: "框架组", Person: lead.ID, Name: lead.Name,
			Week: week, Body: body, Now: now,
		}, false); err != nil {
			t.Fatalf("写 %s %s 周组报失败: %v", week, projectID, err)
		}
		if final {
			if _, err := ws.FinalizeDraftKind(store.GroupReportKind(projectID), week, lead.ID, now); err != nil {
				t.Fatalf("定稿 %s %s 周组报失败: %v", week, projectID, err)
			}
		}
	}

	write("2026-W40", p1.ID, "## 本周总结\n- 完成退款接口联调\n- 排查线上超时\n## 下周计划\n- 补齐退款异常分支\n", true)
	write("2026-W41", p1.ID, "## 本周总结\n- 完成退款接口联调\n- 对账服务改造完成\n## 下周计划\n- 完成对账服务回归\n", true)
	write("2026-W42", p1.ID, "## 本周总结\n- 对账服务回归通过\n## 下周计划\n- 对账服务灰度扩到 50%\n", true)
	write("2026-W43", p1.ID, "## 本周总结\n- 对账服务灰度扩到 50%\n## 下周计划\n- 对账服务全量发布\n", true)
	write("2026-W44", p1.ID, "## 本周总结\n- 对账服务全量发布完成\n## 下周计划\n- 观察指标\n", false)

	write("2026-W40", p2.ID, "## 本周总结\n- 报表导出支持 Excel\n## 下周计划\n- 梳理报表权限模型\n", true)
	write("2026-W41", p2.ID, "## 本周总结\n- 报表权限模型评审通过\n## 下周计划\n- 报表权限模型开发\n", true)
	write("2026-W43", p2.ID, "## 本周总结\n- 报表权限模型联调通过\n## 下周计划\n- 报表权限模型灰度\n", true)
	write("2026-W44", p2.ID, "## 本周总结\n- 报表权限模型灰度无异常\n## 下周计划\n- 报表权限模型全量发布\n", true)

	data, err := MonthSourceData(ws, "2026-10", "框架组")
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Weeks) != 7 || len(data.DefaultWeeks) != 5 || len(data.ScopeWeeks) != 5 {
		t.Fatalf("周视图 = %+v", data)
	}
	if data.Slots != 10 || data.FinalSlots != 8 {
		t.Fatalf("统计 slots/final = %d/%d，期望 10/8", data.Slots, data.FinalSlots)
	}
	if len(data.Missing) != 2 || !strings.Contains(strings.Join(data.Missing, "|"), "云平台项目 W44（未定稿）") ||
		!strings.Contains(strings.Join(data.Missing, "|"), "数据中台项目 W42（未提交）") {
		t.Fatalf("missing = %+v", data.Missing)
	}
	// 默认候选：p1 总结 4 条（退款重复合并）、计划 1 条；p2 总结 3 条、计划 1 条。
	p1c, err := MonthProjectCandidates(ws, "2026-10", "框架组", p1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p1c.Counts.Summary != 5 || p1c.Counts.Plan != 1 || p1c.Counts.Archive != 3 {
		t.Fatalf("p1 counts = %+v", p1c.Counts)
	}
	if len(p1c.Summary) == 0 || len(p1c.Summary[0].Weeks) != 2 {
		t.Fatalf("退款联调应合并两周: %+v", p1c.Summary)
	}
	if p1c.PlanFrom != "2026-W43" {
		t.Fatalf("planFrom = %q，期望 W43", p1c.PlanFrom)
	}
	p2c, err := MonthProjectCandidates(ws, "2026-10", "框架组", p2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p2c.Counts.Total != 5 {
		t.Fatalf("p2 counts = %+v，期望 5", p2c.Counts)
	}
	if !strings.Contains(strings.Join(p2c.Missing, "|"), "W42（未提交）") {
		t.Fatalf("p2 missing = %+v", p2c.Missing)
	}
}

func TestMonthSetScopeSourceAndPools(t *testing.T) {
	ws := newReportWorkspace(t)
	now := reportNow()
	lead, err := ws.CreatePerson(store.NewPerson{Name: "组长", Group: "框架组", Role: store.RoleLead, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.AddReportProject("云平台项目"); err != nil {
		t.Fatal(err)
	}
	projects, _ := ws.ReportProjects()
	p1 := projects[0]
	write := func(week, body string) {
		if _, err := ws.SaveGeneratedDraft(store.GeneratedDraft{
			Kind: store.GroupReportKind(p1.ID), Group: "框架组", Person: lead.ID, Name: lead.Name,
			Week: week, Body: body, Now: now,
		}, false); err != nil {
			t.Fatal(err)
		}
		if _, err := ws.FinalizeDraftKind(store.GroupReportKind(p1.ID), week, lead.ID, now); err != nil {
			t.Fatal(err)
		}
	}
	write("2026-W40", "## 本周总结\n- A\n- B\n## 下周计划\n- C\n")
	write("2026-W41", "## 本周总结\n- B\n- D\n## 下周计划\n- E\n")
	write("2026-W42", "## 本周总结\n- F\n## 下周计划\n- G\n")
	// 相邻周也准备定稿组报，验证纳入 / 排除。
	write("2026-W39", "## 本周总结\n- 上月收尾\n## 下周计划\n- 上月计划\n")
	write("2026-W45", "## 本周总结\n- 下月开局\n## 下周计划\n- 下月计划\n")

	data, err := MonthSourceData(ws, "2026-10", "框架组")
	if err != nil {
		t.Fatal(err)
	}
	if len(data.ScopeWeeks) != 5 {
		t.Fatalf("默认范围 = %v", data.ScopeWeeks)
	}

	// 排除 W44、W42：缺周只剩 W42 被排除后的空范围？这里 W42 有组报，范围改成 W40/W41/W43。
	pull, data, err := SetMonthScope(ws, "2026-10", "框架组", []string{"2026-W40", "2026-W41", "2026-W43"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pull.Weeks) != 3 || data.Slots != 3 {
		t.Fatalf("改范围 = %+v / slots=%d", pull.Weeks, data.Slots)
	}
	// 加回 W39 和 W45：自动纳入它们的定稿组报。
	pull, data, err = SetMonthScope(ws, "2026-10", "框架组", []string{"2026-W39", "2026-W40", "2026-W41", "2026-W43", "2026-W45"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pull.Sources) != 4 {
		t.Fatalf("相邻周来源 = %+v，期望 4", pull.Sources)
	}
	c, err := MonthProjectCandidates(ws, "2026-10", "框架组", p1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c.PlanFrom != "2026-W45" {
		t.Fatalf("加下月第一周后 planFrom = %q", c.PlanFrom)
	}

	// 去选一份来源：W43 不纳入后，计划回退到 W41。
	if _, err := SetMonthSource(ws, "2026-10", "框架组", "2026-W43", p1.ID, false); err != nil {
		t.Fatal(err)
	}
	c, err = MonthProjectCandidates(ws, "2026-10", "框架组", p1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c.PlanFrom != "2026-W45" {
		t.Fatalf("W45 仍在，planFrom = %q", c.PlanFrom)
	}
	// 把 W45 也去掉，计划回退到 W41。
	if _, err := SetMonthSource(ws, "2026-10", "框架组", "2026-W45", p1.ID, false); err != nil {
		t.Fatal(err)
	}
	c, _ = MonthProjectCandidates(ws, "2026-10", "框架组", p1.ID)
	if c.PlanFrom != "2026-W41" {
		t.Fatalf("去掉 W43/W45 后 planFrom = %q，期望 W41", c.PlanFrom)
	}

	// 改归口：把总结里的 B 移到计划。
	var b MonthItem
	for _, it := range c.Summary {
		if it.Text == "B" {
			b = it
		}
	}
	if b.ID == "" {
		t.Fatalf("找不到 B: %+v", c.Summary)
	}
	c, err = SetMonthItemPool(ws, "2026-10", "框架组", p1.ID, b.Section, b.ID, store.MonthPoolPlan)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range c.Plan {
		if it.ID == b.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("B 没进计划: %+v", c.Plan)
	}
	// 改回默认会删除 override。
	c, err = SetMonthItemPool(ws, "2026-10", "框架组", p1.ID, b.Section, b.ID, store.MonthPoolSummary)
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, it := range c.Plan {
		if it.ID == b.ID {
			found = true
		}
	}
	if found {
		t.Fatalf("B 应回到总结: %+v", c.Plan)
	}
}

func TestAssembleMonthProjectAndSourceChanged(t *testing.T) {
	ws := newReportWorkspace(t)
	now := reportNow()
	lead, err := ws.CreatePerson(store.NewPerson{Name: "组长", Group: "框架组", Role: store.RoleLead, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.AddReportProject("云平台项目"); err != nil {
		t.Fatal(err)
	}
	projects, _ := ws.ReportProjects()
	p1 := projects[0]
	write := func(week, body string, final bool) {
		if _, err := ws.SaveGeneratedDraft(store.GeneratedDraft{
			Kind: store.GroupReportKind(p1.ID), Group: "框架组", Person: lead.ID, Name: lead.Name,
			Week: week, Body: body, Now: now,
		}, false); err != nil {
			t.Fatal(err)
		}
		if final {
			if _, err := ws.FinalizeDraftKind(store.GroupReportKind(p1.ID), week, lead.ID, now); err != nil {
				t.Fatal(err)
			}
		}
	}
	write("2026-W40", "## 本周总结\n- A\n## 下周计划\n- C\n", true)
	write("2026-W41", "## 本周总结\n- D\n## 下周计划\n- E\n", true)
	write("2026-W42", "## 本周总结\n- F\n## 下周计划\n- G\n", true)
	write("2026-W43", "## 本周总结\n- H\n## 下周计划\n- I\n", true)
	write("2026-W44", "## 本周总结\n- 还没定稿\n", false)

	// 缺 W44 未定稿，默认不允许生成。
	if _, err := AssembleMonthProject(ws, "2026-10", "框架组", p1.ID, false, false, now); !errors.Is(err, store.ErrNotFinal) {
		t.Fatalf("缺周应 ErrNotFinal，得到 %v", err)
	}
	d, err := AssembleMonthProject(ws, "2026-10", "框架组", p1.ID, true, false, now)
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != store.GroupReportKind(p1.ID) || d.Week != "2026-10" || d.Person != lead.ID {
		t.Fatalf("draft = %+v", d)
	}
	for _, want := range []string{"## 本月总结", "## 下月计划", "- A", "- I", "未纳入：W44（未定稿）", "下月计划取自 W43"} {
		if !strings.Contains(d.Body, want) {
			t.Fatalf("月报正文缺少 %q:\n%s", want, d.Body)
		}
	}
	if strings.Contains(d.Body, "组长") || strings.Contains(d.Body, "2026-W40") {
		t.Fatalf("正文不应带姓名/周次:\n%s", d.Body)
	}
	if MonthProjectSourceChanged(ws, d) {
		t.Fatal("刚生成的月报不应标记来源已更新")
	}
	// 改一条来源归口后应标记来源已更新。
	c, err := MonthProjectCandidates(ws, "2026-10", "框架组", p1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Summary) == 0 {
		t.Fatal("没有总结候选")
	}
	it := c.Summary[0]
	if _, err := SetMonthItemPool(ws, "2026-10", "框架组", p1.ID, it.Section, it.ID, store.MonthPoolPlan); err != nil {
		t.Fatal(err)
	}
	if !MonthProjectSourceChanged(ws, d) {
		t.Fatal("归口改了应标记来源已更新")
	}

	// 自定义范围：排除 W44 后允许生成，并带「本期范围」。
	if _, _, err := SetMonthScope(ws, "2026-10", "框架组", []string{"2026-W40", "2026-W41", "2026-W42", "2026-W43"}); err != nil {
		t.Fatal(err)
	}
	d2, err := AssembleMonthProject(ws, "2026-10", "框架组", p1.ID, false, true, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d2.Body, "本期范围：W40–W43（自定义）") {
		t.Fatalf("自定义范围正文:\n%s", d2.Body)
	}
	if strings.Contains(d2.Body, "未纳入：W44") {
		t.Fatalf("排除 W44 后不应再标注 W44:\n%s", d2.Body)
	}
}

func TestParseMonthReportBody(t *testing.T) {
	body := "## 本周完成\n### 产品线A\n#### 功能开发\n- 接口联调\n- 无\n## 下周计划\n### 产品线A\n- 补齐用例\n## 风险与求助\n- 环境不足\n"
	summary, plan := parseMonthReportBody(body)
	if len(summary) != 1 || summary[0] != "接口联调" {
		t.Fatalf("summary = %v", summary)
	}
	if len(plan) != 1 || plan[0] != "补齐用例" {
		t.Fatalf("plan = %v", plan)
	}
}
