package core

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/axu4/kuibu/internal/store"
)

func TestBuildTriageSectionsAndAssembleProjects(t *testing.T) {
	ws := newReportWorkspace(t)
	now := reportNow()
	lead, err := ws.CreatePerson(store.NewPerson{Name: "组长", Group: "框架组", Role: store.RoleLead, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	zhang, err := ws.CreatePerson(store.NewPerson{Name: "张三", Group: "框架组", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	li, err := ws.CreatePerson(store.NewPerson{Name: "李四", Group: "框架组", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.CreatePerson(store.NewPerson{Name: "王五", Group: "框架组", Now: now}); err != nil {
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

	writeDraft := func(person *store.Person, body string) {
		t.Helper()
		if _, err := ws.SaveGeneratedDraft(store.GeneratedDraft{
			Person: person.ID, Name: person.Name, Week: "2026-W44",
			Model: "test", Body: body, Now: now,
		}, false); err != nil {
			t.Fatal(err)
		}
	}
	zhangBody := "## 本周完成\n- 【支付】退款接口联调\n- 【支付】排查超时\n## 下周计划\n- 【支付】补齐用例\n## 风险与求助\n- 无\n"
	writeDraft(zhang, zhangBody)
	writeDraft(li, "## 本周完成\n- 【数据】报表导出 Excel\n## 下周计划\n- 【数据】梳理权限\n## 风险与求助\n- 测试环境不足\n")

	triage, err := BuildTriage(ws, "2026-W44", "框架组")
	if err != nil {
		t.Fatal(err)
	}
	if triage.Lead != lead.ID || triage.Group != "框架组" {
		t.Fatalf("triage lead/group = %q/%q", triage.Lead, triage.Group)
	}
	if len(triage.Projects) != 2 || triage.Projects[0].ID != p1.ID {
		t.Fatalf("项目快照 = %+v", triage.Projects)
	}
	if len(triage.Items) != 6 {
		t.Fatalf("条目数 = %d，期望 6: %+v", len(triage.Items), triage.Items)
	}
	if len(triage.Missing) != 1 || triage.Missing[0] != "王五" {
		t.Fatalf("missing = %+v", triage.Missing)
	}
	want := []struct {
		section string
		domain  string
	}{
		{SectionDone, "支付"}, {SectionDone, "支付"}, {SectionPlan, "支付"},
		{SectionDone, "数据"}, {SectionPlan, "数据"}, {SectionRisk, ""},
	}
	for i, it := range triage.Items {
		if it.Section != want[i].section || it.Domain != want[i].domain {
			t.Errorf("第 %d 条 = %+v，期望节 %s 领域 %s", i, it, want[i].section, want[i].domain)
		}
		if strings.Contains(it.Text, "【") {
			t.Errorf("正文不应保留领域前缀: %q", it.Text)
		}
	}
	if TriageSectionTitle(SectionDone) != "本周总结" {
		t.Errorf("节名展示 = %q", TriageSectionTitle(SectionDone))
	}

	// 决策：p1 拿张三完成 1、下周计划、李四风险；p2 拿张三完成 2、李四完成和计划。
	byText := map[string]store.TriageItem{}
	for _, it := range triage.Items {
		byText[it.Text] = it
	}
	set := func(text, decision, project string) {
		t.Helper()
		if err := SetTriageDecision(triage, byText[text].ID, decision, project, now); err != nil {
			t.Fatal(err)
		}
	}
	set("退款接口联调", store.TriageAccepted, p1.ID)
	set("补齐用例", store.TriageAccepted, p1.ID)
	set("测试环境不足", store.TriageAccepted, p1.ID)
	set("排查超时", store.TriageAccepted, p2.ID)
	set("报表导出 Excel", store.TriageAccepted, p2.ID)
	set("梳理权限", store.TriageSkipped, "")
	if err := ws.WriteTriage(triage); err != nil {
		t.Fatal(err)
	}
	back, err := ws.ReadTriage("2026-W44", lead.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Items) != 6 || back.Items[0].Decision != store.TriageAccepted {
		t.Fatalf("读回筛选记录失败: %+v", back)
	}
	c := CountTriage(back)
	if c.Total != 6 || c.Accepted != 5 || c.Skipped != 1 || c.Undecided != 0 || c.ByProject[p1.ID] != 3 {
		t.Fatalf("统计 = %+v", c)
	}

	d1, err := AssembleProjectGroup(ws, "2026-W44", "", p1.ID, false, false, now)
	if err != nil {
		t.Fatal(err)
	}
	if d1.Kind != store.GroupReportKind(p1.ID) || d1.Person != lead.ID {
		t.Fatalf("组报草稿 = %+v", d1)
	}
	for _, wantPart := range []string{"## 本周总结", "## 下周计划", "## 其他事项", "退款接口联调", "补齐用例", "测试环境不足"} {
		if !strings.Contains(d1.Body, wantPart) {
			t.Errorf("p1 组报缺少 %q:\n%s", wantPart, d1.Body)
		}
	}
	for _, noPart := range []string{"排查超时", "报表导出", "张三", "李四", "（"} {
		if strings.Contains(d1.Body, noPart) {
			t.Errorf("p1 组报不应出现 %q:\n%s", noPart, d1.Body)
		}
	}
	if _, err := ws.ReadGroupReportProject("2026-W44", p1.ID, lead.ID); err != nil {
		t.Fatalf("读取 p1 组报失败: %v", err)
	}
	d2, err := AssembleProjectGroup(ws, "2026-W44", "", p2.ID, false, false, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d2.Body, "排查超时") || strings.Contains(d2.Body, "退款接口联调") {
		t.Errorf("p2 组报内容不对:\n%s", d2.Body)
	}

	// 人工修改后重新生成必须 --force。
	if _, err := ws.EditDraftKind(store.GroupReportKind(p1.ID), "2026-W44", lead.ID, d1.Body+"- 人工补充\n", now); err != nil {
		t.Fatal(err)
	}
	if _, err := AssembleProjectGroup(ws, "2026-W44", "", p1.ID, false, false, now); !errors.Is(err, store.ErrHumanEdits) {
		t.Fatalf("覆盖人工修改应返回 ErrHumanEdits，实际 %v", err)
	}
	if _, err := AssembleProjectGroup(ws, "2026-W44", "", p1.ID, true, false, now); err != nil {
		t.Fatalf("--force 重新生成失败: %v", err)
	}

	// 来源新增一条：旧决策保留，新条目未判。
	newBody := zhangBody + "- 【支付】新增灰度验证\n"
	if _, err := ws.SaveGeneratedDraft(store.GeneratedDraft{
		Person: zhang.ID, Name: zhang.Name, Week: "2026-W44", Model: "test", Body: newBody, Now: now,
	}, true); err != nil {
		t.Fatal(err)
	}
	triage2, err := BuildTriage(ws, "2026-W44", "框架组")
	if err != nil {
		t.Fatal(err)
	}
	foundNew, kept := false, 0
	for _, it := range triage2.Items {
		if it.Text == "新增灰度验证" {
			foundNew = true
			if it.Decision != "" {
				t.Errorf("新条目不应自动带决策: %+v", it)
			}
		}
		if it.Decision != "" {
			kept++
		}
	}
	if !foundNew || kept != 6 {
		t.Fatalf("来源更新后 new=%v kept=%d: %+v", foundNew, kept, triage2.Items)
	}
	if triage2.SourceHash == triage.SourceHash {
		t.Error("来源变化后 sourceHash 应变化")
	}
}

func TestBuildTriageFromRawReportsWithoutAI(t *testing.T) {
	ws := newReportWorkspace(t)
	now := reportNow()
	if _, err := ws.CreatePerson(store.NewPerson{Name: "组长", Group: "框架组", Role: store.RoleLead, Now: now}); err != nil {
		t.Fatal(err)
	}
	zhang, err := ws.CreatePerson(store.NewPerson{Name: "张三", Group: "框架组", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.AddReportProject("云平台项目"); err != nil {
		t.Fatal(err)
	}
	// 原文直接按条写，没有归一稿，也没有 AI 配置。
	if _, err := ws.WriteReport(store.NewReport{
		Person: zhang.ID, Name: zhang.Name, Week: "2026-W44", Level: store.ReportLevelMember,
		Group: "框架组", Source: store.ReportSourcePaste, SubmittedAt: now,
		Body: "【周报】张三 2026-10-29\n本周总结\n- 退款接口联调\n- 排查超时\n下周计划\n- 补齐用例\n风险与求助\n- 测试环境不足\n",
	}); err != nil {
		t.Fatal(err)
	}
	triage, err := BuildTriage(ws, "2026-W44", "框架组")
	if err != nil {
		t.Fatal(err)
	}
	if len(triage.Items) != 4 {
		t.Fatalf("原文条目 = %d: %+v", len(triage.Items), triage.Items)
	}
	want := []struct{ section, text string }{
		{SectionDone, "退款接口联调"},
		{SectionDone, "排查超时"},
		{SectionPlan, "补齐用例"},
		{SectionRisk, "测试环境不足"},
	}
	for i, it := range triage.Items {
		if it.Section != want[i].section || it.Text != want[i].text {
			t.Errorf("第 %d 条 = %+v，期望 %s/%s", i, it, want[i].section, want[i].text)
		}
	}
	// 原文优先于已有归一稿：归一稿里没有的条目也应出现。
	if _, err := ws.SaveGeneratedDraft(store.GeneratedDraft{
		Person: zhang.ID, Name: zhang.Name, Week: "2026-W44", Body: "## 本周完成\n- 只有归一稿\n",
	}, true); err != nil {
		t.Fatal(err)
	}
	triage, err = BuildTriage(ws, "2026-W44", "框架组")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range triage.Items {
		if it.Text == "只有归一稿" {
			found = true
		}
	}
	if found {
		t.Fatal("有原文时应优先原文，不应读归一稿")
	}
}

func TestBuildTriageInfersGroupWhenLeadHasNoGroup(t *testing.T) {
	ws := newReportWorkspace(t)
	now := reportNow()
	if _, err := ws.CreatePerson(store.NewPerson{Name: "组长", Role: store.RoleLead, Now: now}); err != nil {
		t.Fatal(err)
	}
	zhang, err := ws.CreatePerson(store.NewPerson{Name: "张三", Group: "框架组", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.AddReportProject("云平台项目"); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.WriteReport(store.NewReport{
		Person: zhang.ID, Name: zhang.Name, Week: "2026-W44", Level: store.ReportLevelMember,
		Group: "框架组", Source: store.ReportSourcePaste, SubmittedAt: now,
		Body: "本周完成：\n1. 完成退款接口联调\n下周计划：\n1. 补齐用例\n",
	}); err != nil {
		t.Fatal(err)
	}
	triage, err := BuildTriage(ws, "2026-W44", "")
	if err != nil {
		t.Fatal(err)
	}
	if triage.Group != "框架组" {
		t.Fatalf("组名 = %q，期望由组员推导出 框架组", triage.Group)
	}
	if len(triage.Items) != 2 {
		t.Fatalf("条目 = %d: %+v", len(triage.Items), triage.Items)
	}
	// 显式传组也应能找到空组的组长。
	lead, groupName, err := ResolveLead(ws, "2026-W44", "框架组")
	if err != nil || lead == nil || groupName != "框架组" {
		t.Fatalf("ResolveLead = %+v/%q/%v", lead, groupName, err)
	}
}

func TestParseRawReportWeekPlanSections(t *testing.T) {
	body := "InManage\n本周：\n1、KH5KVM openBmc 单机自研推进\n2、修复登录页闪回\n下周：\n1、修改主分支加密解密方案\n2、配置操作系统安装功能开发\nAI经验 ：H5KVM 协议分析\n"
	items := parseTriageBody(body)
	if len(items[SectionDone]) != 2 || len(items[SectionPlan]) != 2 {
		t.Fatalf("分节 = %+v", items)
	}
	if items[SectionDone][0].Text != "KH5KVM openBmc 单机自研推进" {
		t.Fatalf("中文序号未去掉: %+v", items[SectionDone][0])
	}
	if items[SectionPlan][0].Text != "修改主分支加密解密方案" {
		t.Fatalf("计划条目 = %+v", items[SectionPlan][0])
	}
	if TriageSectionTitle(SectionRisk) != "其他事项" {
		t.Fatalf("风险节显示名 = %q", TriageSectionTitle(SectionRisk))
	}
}

func TestBuildTriageUsesEntrySections(t *testing.T) {
	ws := newReportWorkspace(t)
	now := reportNow()
	if _, err := ws.CreatePerson(store.NewPerson{Name: "组长", Role: store.RoleLead, Now: now}); err != nil {
		t.Fatal(err)
	}
	zhang, err := ws.CreatePerson(store.NewPerson{Name: "张三", Group: "框架组", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.AddReportProject("云平台项目"); err != nil {
		t.Fatal(err)
	}
	body := "本周：\n1、完成 A\n下周：\n1、计划 B\n"
	report, err := ws.WriteReport(store.NewReport{
		Person: zhang.ID, Name: zhang.Name, Week: "2026-W44", Level: store.ReportLevelMember,
		Group: "框架组", Source: store.ReportSourcePaste, SubmittedAt: now, Body: body,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 录入时人工把「完成 A」改到「其他事项」。
	if err := ws.WriteReportSections(store.ReportSections{
		Person: zhang.ID, Week: "2026-W44", SourceHash: ReportBodyHash(report.Body), UpdatedAt: now.Format(time.RFC3339),
		Sections: []store.ReportSection{
			{Section: SectionDone, Items: []string{"完成 A"}},
			{Section: SectionPlan, Items: []string{"计划 B"}},
			{Section: SectionRisk, Items: []string{"领导交办 C"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	triage, err := BuildTriage(ws, "2026-W44", "框架组")
	if err != nil {
		t.Fatal(err)
	}
	byText := map[string]string{}
	for _, it := range triage.Items {
		byText[it.Text] = it.Section
	}
	if byText["领导交办 C"] != SectionRisk || byText["计划 B"] != SectionPlan || byText["完成 A"] != SectionDone {
		t.Fatalf("录入结构未生效: %+v", triage.Items)
	}
	// 原文变了，旧结构应失效并退回原文解析。
	if _, err := ws.WriteReport(store.NewReport{
		Person: zhang.ID, Name: zhang.Name, Week: "2026-W44", Level: store.ReportLevelMember,
		Group: "框架组", Source: store.ReportSourcePaste, SubmittedAt: now, Body: body + "本周：\n1、新增 D\n",
	}); err != nil {
		t.Fatal(err)
	}
	triage, err = BuildTriage(ws, "2026-W44", "框架组")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range triage.Items {
		if it.Text == "新增 D" {
			found = true
		}
	}
	if !found {
		t.Fatalf("原文更新后应退回解析: %+v", triage.Items)
	}
}

func TestRebuildTriageClearsDecisions(t *testing.T) {
	ws := newReportWorkspace(t)
	now := reportNow()
	if _, err := ws.CreatePerson(store.NewPerson{Name: "组长", Role: store.RoleLead, Now: now}); err != nil {
		t.Fatal(err)
	}
	zhang, err := ws.CreatePerson(store.NewPerson{Name: "张三", Group: "框架组", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.AddReportProject("云平台项目"); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.WriteReport(store.NewReport{
		Person: zhang.ID, Name: zhang.Name, Week: "2026-W44", Level: store.ReportLevelMember,
		Group: "框架组", Source: store.ReportSourcePaste, SubmittedAt: now,
		Body: "本周：\n1、完成 A\n下周：\n1、计划 B\n",
	}); err != nil {
		t.Fatal(err)
	}
	triage, err := BuildTriage(ws, "2026-W44", "框架组")
	if err != nil {
		t.Fatal(err)
	}
	projects, _ := ws.ReportProjects()
	if err := SetTriageDecision(triage, triage.Items[0].ID, store.TriageAccepted, projects[0].ID, now); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteTriage(triage); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := RebuildTriage(ws, "2026-W44", "框架组")
	if err != nil {
		t.Fatal(err)
	}
	if len(rebuilt.Items) != 2 {
		t.Fatalf("条目 = %d", len(rebuilt.Items))
	}
	for _, it := range rebuilt.Items {
		if it.Decision != "" || it.Project != "" {
			t.Fatalf("重建后决策应清空: %+v", it)
		}
	}
}
