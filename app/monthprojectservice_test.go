package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/axu4/kuibu/internal/core"
	"github.com/axu4/kuibu/internal/store"
)

// monthFixture 是一个「框架组 + 若干报送项目」的临时工作台，用于项目月报测试。
type monthFixture struct {
	t        *testing.T
	reports  *ReportService
	ws       *store.Workspace
	events   *reportEvents
	lead     PersonData
	projects []store.ReportProject
}

// newMonthFixture 初始化工作台、组长与报送项目。
func newMonthFixture(t *testing.T, projectNames ...string) *monthFixture {
	t.Helper()
	reports, workspace, events := newTestReports(t)
	lead, err := reports.CreatePerson(PersonInput{Name: "陈组长", Group: "框架组", Role: store.RoleLead})
	if err != nil {
		t.Fatalf("创建组长失败: %v", err)
	}
	ws := workspace.workspace()
	for _, name := range projectNames {
		if _, _, err := ws.AddReportProject(name); err != nil {
			t.Fatalf("添加项目 %q 失败: %v", name, err)
		}
	}
	projects, err := ws.ReportProjects()
	if err != nil {
		t.Fatalf("读取报送项目失败: %v", err)
	}
	return &monthFixture{t: t, reports: reports, ws: ws, events: events, lead: lead, projects: projects}
}

// weekReport 写一份按项目周组报，final 为 true 时同时定稿。
func (f *monthFixture) weekReport(projectID, week, body string, final bool) {
	f.t.Helper()
	if _, err := f.ws.SaveGeneratedDraft(store.GeneratedDraft{
		Kind: store.GroupReportKind(projectID), Group: "框架组",
		Person: f.lead.ID, Name: f.lead.Name, Week: week, Body: body, Now: fixedNow(),
	}, false); err != nil {
		f.t.Fatalf("写 %s %s 周组报失败: %v", week, projectID, err)
	}
	if !final {
		return
	}
	if _, err := f.ws.FinalizeDraftKind(store.GroupReportKind(projectID), week, f.lead.ID, fixedNow()); err != nil {
		f.t.Fatalf("定稿 %s %s 周组报失败: %v", week, projectID, err)
	}
}

// lastEvent 返回最近一次事件的名称与数据。
func (f *monthFixture) lastEvent() (string, any) {
	f.t.Helper()
	if len(f.events.names) == 0 {
		f.t.Fatal("没有发出任何事件")
	}
	return f.events.names[len(f.events.names)-1], f.events.data[len(f.events.data)-1]
}

// wantReportChanged 断言最近一次事件是 report:changed，且 reportIds 与期望一致。
func (f *monthFixture) wantReportChanged(wantIDs ...string) ReportChangeEvent {
	f.t.Helper()
	name, data := f.lastEvent()
	if name != reportChangedEvent {
		f.t.Fatalf("事件名 = %q，期望 %q", name, reportChangedEvent)
	}
	ev, ok := data.(ReportChangeEvent)
	if !ok {
		f.t.Fatalf("事件数据 = %T，期望 ReportChangeEvent", data)
	}
	if len(ev.ReportIDs) != len(wantIDs) {
		f.t.Fatalf("reportIds = %v，期望 %v", ev.ReportIDs, wantIDs)
	}
	for i := range wantIDs {
		if ev.ReportIDs[i] != wantIDs[i] {
			f.t.Fatalf("reportIds = %v，期望 %v", ev.ReportIDs, wantIDs)
		}
	}
	return ev
}

// weekRow 从来源视图里取一周。
func weekRow(t *testing.T, v core.MonthSourceView, week string) core.MonthWeekRow {
	t.Helper()
	for _, row := range v.Weeks {
		if row.Week == week {
			return row
		}
	}
	t.Fatalf("来源视图里没有 %s: %+v", week, v.Weeks)
	return core.MonthWeekRow{}
}

// weekReportOf 从一行里取某个项目的周组报状态。
func weekReportOf(t *testing.T, row core.MonthWeekRow, projectID string) core.MonthWeekProject {
	t.Helper()
	for _, r := range row.Reports {
		if r.Project == projectID {
			return r
		}
	}
	t.Fatalf("%s 行里没有项目 %s: %+v", row.Week, projectID, row.Reports)
	return core.MonthWeekProject{}
}

// findItem 在候选列表里按文本找条目。
func findItem(items []core.MonthItem, text string) (core.MonthItem, bool) {
	for _, it := range items {
		if it.Text == text {
			return it, true
		}
	}
	return core.MonthItem{}, false
}

func TestMonthProjectSourcesAndCandidates(t *testing.T) {
	f := newMonthFixture(t, "云平台项目", "数据中台项目")
	p1, p2 := f.projects[0], f.projects[1]

	f.weekReport(p1.ID, "2026-W39", "## 本周总结\n- 上月收尾\n## 下周计划\n- 上月计划\n", true)
	f.weekReport(p1.ID, "2026-W40", "## 本周总结\n- 完成退款接口联调\n## 下周计划\n- 补齐异常分支\n", true)
	f.weekReport(p1.ID, "2026-W41", "## 本周总结\n- 完成退款接口联调\n- 对账改造完成\n## 下周计划\n- 完成对账回归\n", true)
	f.weekReport(p1.ID, "2026-W42", "## 本周总结\n- 对账回归通过\n## 下周计划\n- 灰度扩到 50%\n", true)
	f.weekReport(p1.ID, "2026-W43", "## 本周总结\n- 灰度扩到 50%\n## 下周计划\n- 全量发布\n", true)
	f.weekReport(p1.ID, "2026-W44", "## 本周总结\n- 还没定稿的内容\n", false)
	f.weekReport(p1.ID, "2026-W45", "## 本周总结\n- 下月开局\n## 下周计划\n- 下月计划\n", true)
	f.weekReport(p2.ID, "2026-W40", "## 本周总结\n- 报表导出支持 Excel\n## 下周计划\n- 梳理权限模型\n", true)
	f.weekReport(p2.ID, "2026-W41", "## 本周总结\n- 权限模型评审通过\n## 下周计划\n- 权限模型开发\n", true)
	f.weekReport(p2.ID, "2026-W42", "## 本周总结\n- 权限模型联调通过\n## 下周计划\n- 权限模型灰度\n", true)
	f.weekReport(p2.ID, "2026-W44", "## 本周总结\n- 权限模型全量\n## 下周计划\n- 观察指标\n", true)

	// 来源视图：默认本期 W40–W44，相邻两周 W39 / W45 可勾选但不在本期。
	view, err := f.reports.MonthSources("", "框架组")
	if err != nil {
		t.Fatalf("MonthSources 失败: %v", err)
	}
	if view.Month != "2026-10" || view.Group != "框架组" || view.LeadID != f.lead.ID || view.LeadName != f.lead.Name {
		t.Fatalf("来源视图头部 = %+v", view)
	}
	if len(view.DefaultWeeks) != 5 || len(view.ScopeWeeks) != 5 || len(view.CandidateWeeks) != 7 || len(view.Weeks) != 7 {
		t.Fatalf("周范围 = default %v / scope %v / candidates %v / rows %d",
			view.DefaultWeeks, view.ScopeWeeks, view.CandidateWeeks, len(view.Weeks))
	}
	if len(view.Projects) != 2 || view.Slots != 10 || view.FinalSlots != 8 {
		t.Fatalf("项目/slots = %+v / %d/%d，期望 2 个项目、10/8", view.Projects, view.Slots, view.FinalSlots)
	}
	missing := strings.Join(view.Missing, "|")
	if !strings.Contains(missing, "云平台项目 W44（未定稿）") || !strings.Contains(missing, "数据中台项目 W43（未提交）") {
		t.Fatalf("缺周 = %v", view.Missing)
	}
	if view.CandidateTotal == 0 {
		t.Fatal("候选总数不应为 0")
	}
	w39 := weekRow(t, view, "2026-W39")
	if w39.Side != "prev" || w39.Label != "上月最后一周" || w39.Default || w39.Scope || w39.ReportDay != "2026-09-24" {
		t.Fatalf("W39 行 = %+v", w39)
	}
	if r := weekReportOf(t, w39, p1.ID); r.Status != store.DraftStatusFinal || r.Included || r.Items != 2 {
		t.Fatalf("W39 云平台 = %+v", r)
	}
	w44 := weekRow(t, view, "2026-W44")
	if !w44.Default || !w44.Scope || w44.Side != "" {
		t.Fatalf("W44 行 = %+v", w44)
	}
	if r := weekReportOf(t, w44, p1.ID); r.Status != store.DraftStatusDraft || r.Included || r.Items != 1 {
		t.Fatalf("W44 云平台 = %+v", r)
	}
	if r := weekReportOf(t, w44, p2.ID); r.Status != store.DraftStatusFinal || !r.Included || r.Items != 2 {
		t.Fatalf("W44 数据中台 = %+v", r)
	}
	if w45 := weekRow(t, view, "2026-W45"); w45.Side != "next" || w45.Label != "下月第一周" || w45.Scope {
		t.Fatalf("W45 行 = %+v", w45)
	}

	// 项目候选：总结跨周相同条目合并，计划取自最后一份纳入的周组报。
	c1, err := f.reports.MonthProjectCandidates("2026-10", "框架组", p1.ID)
	if err != nil {
		t.Fatalf("MonthProjectCandidates 失败: %v", err)
	}
	if c1.Project != p1.ID || c1.ProjectName != p1.Name || c1.PlanFrom != "2026-W43" || len(c1.SourceWeeks) != 4 {
		t.Fatalf("云平台头部 = %+v", c1)
	}
	if c1.Counts.Summary != 4 || c1.Counts.Plan != 1 || c1.Counts.Archive != 3 || c1.Counts.Total != 5 {
		t.Fatalf("云平台统计 = %+v", c1.Counts)
	}
	merged, ok := findItem(c1.Summary, "完成退款接口联调")
	if !ok || merged.Duplicated != 2 || len(merged.Weeks) != 2 || merged.FirstWeek != "2026-W40" || merged.LastWeek != "2026-W41" {
		t.Fatalf("跨周合并 = %+v（存在=%v）", merged, ok)
	}
	if !strings.Contains(strings.Join(c1.Missing, "|"), "W44（未定稿）") {
		t.Fatalf("云平台缺周 = %v", c1.Missing)
	}
	c2, err := f.reports.MonthProjectCandidates("2026-10", "框架组", p2.ID)
	if err != nil {
		t.Fatalf("MonthProjectCandidates 数据中台失败: %v", err)
	}
	if c2.PlanFrom != "2026-W44" || c2.Counts.Summary != 4 || c2.Counts.Plan != 1 || c2.Counts.Total != 5 {
		t.Fatalf("数据中台候选 = %+v / %+v", c2, c2.Counts)
	}
	if !strings.Contains(strings.Join(c2.Missing, "|"), "W43（未提交）") {
		t.Fatalf("数据中台缺周 = %v", c2.Missing)
	}

	// 改本期范围：去掉 W44，纳入 W39 / W45；新增周自动纳入其中的定稿组报。
	view, err = f.reports.SetMonthScope("2026-10", "框架组",
		[]string{"2026-W39", "2026-W40", "2026-W41", "2026-W42", "2026-W43", "2026-W45"})
	if err != nil {
		t.Fatalf("SetMonthScope 失败: %v", err)
	}
	f.wantReportChanged()
	if len(view.ScopeWeeks) != 6 || strings.Contains(strings.Join(view.ScopeWeeks, ","), "2026-W44") {
		t.Fatalf("改范围后 scope = %v", view.ScopeWeeks)
	}
	if w44 := weekRow(t, view, "2026-W44"); w44.Scope || !w44.Default {
		t.Fatalf("W44 应默认但不在本期: %+v", w44)
	}
	if w39 := weekRow(t, view, "2026-W39"); !w39.Scope || weekReportOf(t, w39, p1.ID).Included != true {
		t.Fatalf("W39 应纳入云平台定稿: %+v", w39)
	}
	if w45 := weekRow(t, view, "2026-W45"); !w45.Scope || !weekReportOf(t, w45, p1.ID).Included {
		t.Fatalf("W45 应纳入云平台定稿: %+v", w45)
	}
	c1, err = f.reports.MonthProjectCandidates("2026-10", "框架组", p1.ID)
	if err != nil {
		t.Fatalf("改范围后候选失败: %v", err)
	}
	if c1.PlanFrom != "2026-W45" || len(c1.SourceWeeks) != 6 || c1.Counts.Total != 7 {
		t.Fatalf("改范围后云平台候选 = planFrom %q / sources %v / counts %+v", c1.PlanFrom, c1.SourceWeeks, c1.Counts)
	}
	if _, ok := findItem(c1.Summary, "上月收尾"); !ok {
		t.Fatalf("纳入 W39 后应多出上月条目: %+v", c1.Summary)
	}
	c2, err = f.reports.MonthProjectCandidates("2026-10", "框架组", p2.ID)
	if err != nil {
		t.Fatalf("改范围后数据中台候选失败: %v", err)
	}
	if c2.PlanFrom != "2026-W42" || len(c2.SourceWeeks) != 3 || c2.Counts.Total != 4 {
		t.Fatalf("改范围后数据中台候选 = planFrom %q / sources %v / counts %+v", c2.PlanFrom, c2.SourceWeeks, c2.Counts)
	}

	// 去选 / 重新纳入一份来源；未定稿或不在本期的来源不能纳入。
	if _, err := f.reports.SetMonthSource("2026-10", "框架组", "2026-W45", p1.ID, false); err != nil {
		t.Fatalf("SetMonthSource 去选失败: %v", err)
	}
	f.wantReportChanged()
	if c1, _ = f.reports.MonthProjectCandidates("2026-10", "框架组", p1.ID); c1.PlanFrom != "2026-W43" {
		t.Fatalf("去选 W45 后 planFrom = %q，期望 W43", c1.PlanFrom)
	}
	if _, err := f.reports.SetMonthSource("2026-10", "框架组", "2026-W45", p1.ID, true); err != nil {
		t.Fatalf("SetMonthSource 重新纳入失败: %v", err)
	}
	if c1, _ = f.reports.MonthProjectCandidates("2026-10", "框架组", p1.ID); c1.PlanFrom != "2026-W45" {
		t.Fatalf("重新纳入 W45 后 planFrom = %q，期望 W45", c1.PlanFrom)
	}
	if _, err := f.reports.SetMonthSource("2026-10", "框架组", "2026-W43", p2.ID, true); !errors.Is(err, store.ErrNotFinal) {
		t.Fatalf("纳入未定稿来源应 NOT_FINAL，得到 %v", err)
	}
	if _, err := f.reports.SetMonthSource("2026-10", "框架组", "2026-W44", p1.ID, true); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("纳入不在本期的来源应 INVALID，得到 %v", err)
	}
	if _, err := f.reports.SetMonthSource("2026-10", "框架组", "2026-W46", p1.ID, true); err == nil {
		t.Fatal("纳入范围外的周应报错")
	}
	if _, err := f.reports.SetMonthScope("2026-10", "框架组", []string{"2026-W30"}); err == nil {
		t.Fatal("范围外的周应报错")
	}

	// 改归口：总结挪到计划、挪到不纳入，再改回默认。
	nBase := c1.Counts.Summary
	item, ok := findItem(c1.Summary, "对账改造完成")
	if !ok {
		t.Fatalf("找不到待移动条目: %+v", c1.Summary)
	}
	c1, err = f.reports.SetMonthItemPool("2026-10", "框架组", p1.ID, item.Section, item.ID, store.MonthPoolPlan)
	if err != nil {
		t.Fatalf("挪到计划失败: %v", err)
	}
	f.wantReportChanged()
	if _, ok := findItem(c1.Plan, "对账改造完成"); !ok || c1.Counts.Summary != nBase-1 || c1.Counts.Plan != 2 {
		t.Fatalf("挪到计划后 = %+v / %+v", c1.Plan, c1.Counts)
	}
	c1, err = f.reports.SetMonthItemPool("2026-10", "框架组", p1.ID, item.Section, item.ID, store.MonthPoolExcluded)
	if err != nil {
		t.Fatalf("挪到不纳入失败: %v", err)
	}
	if _, ok := findItem(c1.Excluded, "对账改造完成"); !ok || c1.Counts.Excluded != 1 {
		t.Fatalf("挪到不纳入后 = %+v / %+v", c1.Excluded, c1.Counts)
	}
	c1, err = f.reports.SetMonthItemPool("2026-10", "框架组", p1.ID, item.Section, item.ID, store.MonthPoolSummary)
	if err != nil {
		t.Fatalf("改回总结失败: %v", err)
	}
	if _, ok := findItem(c1.Summary, "对账改造完成"); !ok || c1.Counts.Summary != nBase || c1.Counts.Excluded != 0 {
		t.Fatalf("改回总结后 = %+v / %+v", c1.Summary, c1.Counts)
	}
	if _, err := f.reports.SetMonthItemPool("2026-10", "框架组", p1.ID, item.Section, item.ID, "unknown"); err == nil {
		t.Fatal("非法归口应报错")
	}
	if _, err := f.reports.SetMonthItemPool("2026-10", "框架组", p1.ID, "unknown", item.ID, store.MonthPoolPlan); err == nil {
		t.Fatal("非法节名应报错")
	}
}

func TestMonthProjectGenerateSaveFinalizeExport(t *testing.T) {
	f := newMonthFixture(t, "云平台项目")
	p1 := f.projects[0]

	f.weekReport(p1.ID, "2026-W40", "## 本周总结\n- A\n## 下周计划\n- B\n", true)
	f.weekReport(p1.ID, "2026-W41", "## 本周总结\n- C\n## 下周计划\n- D\n", true)
	f.weekReport(p1.ID, "2026-W42", "## 本周总结\n- E\n## 下周计划\n- F\n", true)
	f.weekReport(p1.ID, "2026-W43", "## 本周总结\n- G\n## 下周计划\n- H\n", true)
	f.weekReport(p1.ID, "2026-W44", "## 本周总结\n- 未定稿内容\n", false)

	// 还没生成草稿：列表与详情都应给出 none，候选统计仍可用。
	list, err := f.reports.MonthProjectList("2026-10", "框架组")
	if err != nil {
		t.Fatalf("MonthProjectList 失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("列表 = %+v", list)
	}
	row := list[0]
	if row.Project != p1.ID || row.ProjectName != p1.Name || row.Exists || row.Status != "none" || row.SourceChanged || row.HumanEdited {
		t.Fatalf("未生成时列表行 = %+v", row)
	}
	if row.Candidates != 5 || row.PlanFrom != "2026-W43" || !strings.Contains(strings.Join(row.Missing, "|"), "W44（未定稿）") {
		t.Fatalf("未生成时列表统计 = %+v", row)
	}
	g, err := f.reports.MonthProjectReport("2026-10", "框架组", p1.ID)
	if err != nil {
		t.Fatalf("MonthProjectReport 失败: %v", err)
	}
	if g.Exists || g.Status != "none" || g.LeadID != f.lead.ID || g.Week != "2026-10" || g.Project != p1.ID || g.ProjectName != p1.Name || g.Missing == nil {
		t.Fatalf("未生成时详情 = %+v", g)
	}
	if _, err := f.reports.MonthProjectReport("2026-10", "框架组", "不存在"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("未知项目应 NOT_FOUND，得到 %v", err)
	}
	if _, err := f.reports.ExportMonthProject("2026-10", "框架组", p1.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("未生成时导出应 NOT_FOUND，得到 %v", err)
	}
	if _, err := f.reports.ExportMonthProjectClipboard("2026-10", "框架组", p1.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("未生成时复制应 NOT_FOUND，得到 %v", err)
	}

	// 缺周未确认时拒绝生成；确认后生成草稿。
	if _, err := f.reports.GenerateMonthProject("2026-10", "框架组", p1.ID, false, false); !errors.Is(err, store.ErrNotFinal) {
		t.Fatalf("缺周生成应 NOT_FINAL，得到 %v", err)
	}
	f.events.reset()
	g, err = f.reports.GenerateMonthProject("2026-10", "框架组", p1.ID, true, false)
	if err != nil {
		t.Fatalf("GenerateMonthProject 失败: %v", err)
	}
	if !g.Exists || g.Status != store.DraftStatusDraft || g.Project != p1.ID || g.ProjectName != p1.Name || g.Week != "2026-10" {
		t.Fatalf("生成结果 = %+v", g)
	}
	if !g.Exists || g.SourceChanged || g.HumanEdited {
		t.Fatalf("刚生成不应标记来源变化 / 人工修改: %+v", g)
	}
	for _, want := range []string{"## 本月总结", "- A", "- G", "## 下月计划", "- H", "未纳入：W44（未定稿）"} {
		if !strings.Contains(g.Body, want) {
			t.Errorf("正文缺少 %q:\n%s", want, g.Body)
		}
	}
	if strings.Contains(g.Body, "陈组长") {
		t.Errorf("月报正文不应带姓名:\n%s", g.Body)
	}
	f.wantReportChanged("2026-10/" + store.GroupReportKind(p1.ID) + "/" + f.lead.ID)

	// 已有草稿但未定稿时，导出 / 复制都返回 NOT_FINAL。
	if _, err := f.reports.ExportMonthProject("2026-10", "框架组", p1.ID); !errors.Is(err, store.ErrNotFinal) {
		t.Fatalf("未定稿导出应 NOT_FINAL，得到 %v", err)
	}
	if _, err := f.reports.ExportMonthProjectClipboard("2026-10", "框架组", p1.ID); !errors.Is(err, store.ErrNotFinal) {
		t.Fatalf("未定稿复制应 NOT_FINAL，得到 %v", err)
	}

	// 保存人工修改后，再次生成需要 force。
	saved, err := f.reports.SaveMonthProject("2026-10", "框架组", p1.ID, g.Body+"- 人工补充\n")
	if err != nil {
		t.Fatalf("SaveMonthProject 失败: %v", err)
	}
	f.wantReportChanged("2026-10/" + store.GroupReportKind(p1.ID) + "/" + f.lead.ID)
	if !saved.HumanEdited || !strings.Contains(saved.Body, "人工补充") || saved.Status != store.DraftStatusDraft {
		t.Fatalf("保存结果 = %+v", saved)
	}
	if _, err := f.reports.GenerateMonthProject("2026-10", "框架组", p1.ID, true, false); !errors.Is(err, store.ErrHumanEdits) {
		t.Fatalf("覆盖人工修改应 HUMAN_EDITS_EXIST，得到 %v", err)
	}
	list, err = f.reports.MonthProjectList("2026-10", "框架组")
	if err != nil {
		t.Fatalf("生成后 MonthProjectList 失败: %v", err)
	}
	if !list[0].Exists || list[0].Status != store.DraftStatusDraft || !list[0].HumanEdited || list[0].EditedAt == "" {
		t.Fatalf("生成后列表行 = %+v", list[0])
	}
	if list[0].Candidates != 5 || !strings.Contains(strings.Join(list[0].Missing, "|"), "W44（未定稿）") {
		t.Fatalf("生成后列表统计 = %+v", list[0])
	}

	// 定稿：正文只读，再次保存报错。
	draft, err := f.reports.MonthProjectReport("2026-10", "框架组", p1.ID)
	if err != nil {
		t.Fatalf("生成后 MonthProjectReport 失败: %v", err)
	}
	if !draft.Exists || draft.SourceChanged || draft.GeneratedAt == "" {
		t.Fatalf("生成后详情 = %+v", draft)
	}
	final, err := f.reports.FinalizeMonthProject("2026-10", "框架组", p1.ID)
	if err != nil {
		t.Fatalf("FinalizeMonthProject 失败: %v", err)
	}
	f.wantReportChanged("2026-10/" + store.GroupReportKind(p1.ID) + "/" + f.lead.ID)
	if final.Status != store.DraftStatusFinal || final.FinalizedAt == "" {
		t.Fatalf("定稿结果 = %+v", final)
	}
	if _, err := f.reports.SaveMonthProject("2026-10", "框架组", p1.ID, "改定稿"); !errors.Is(err, store.ErrFinalized) {
		t.Fatalf("改已定稿应 FINALIZED，得到 %v", err)
	}
	list, _ = f.reports.MonthProjectList("2026-10", "框架组")
	if list[0].Status != store.DraftStatusFinal || list[0].FinalizedAt == "" {
		t.Fatalf("定稿后列表行 = %+v", list[0])
	}

	// 复制飞书只返回内容；导出文件需要保存对话框，测试环境没有 App。
	content, err := f.reports.ExportMonthProjectClipboard("2026-10", "框架组", p1.ID)
	if err != nil {
		t.Fatalf("ExportMonthProjectClipboard 失败: %v", err)
	}
	if !strings.Contains(content.Title, "框架组 组月报 2026-10") || !strings.Contains(content.Title, p1.Name) {
		t.Fatalf("导出标题 = %q", content.Title)
	}
	if !strings.Contains(content.HTML, "<h2>本月总结</h2>") || !strings.Contains(content.Text, "本月总结") || !strings.Contains(content.Text, "人工补充") {
		t.Fatalf("导出内容 = %+v", content)
	}
	path, err := f.reports.ExportMonthProject("2026-10", "框架组", p1.ID)
	if path != "" || err == nil || !strings.Contains(err.Error(), "保存对话框不可用") {
		t.Fatalf("无 App 时导出 = %q, %v", path, err)
	}

	// 取消定稿后来源变化会标记「来源已更新」，恢复来源后标记消失。
	unfinal, err := f.reports.UnfinalizeMonthProject("2026-10", "框架组", p1.ID)
	if err != nil {
		t.Fatalf("UnfinalizeMonthProject 失败: %v", err)
	}
	f.wantReportChanged("2026-10/" + store.GroupReportKind(p1.ID) + "/" + f.lead.ID)
	if unfinal.Status != store.DraftStatusDraft || unfinal.FinalizedAt != "" {
		t.Fatalf("取消定稿结果 = %+v", unfinal)
	}
	if _, err := f.reports.SetMonthSource("2026-10", "框架组", "2026-W43", p1.ID, false); err != nil {
		t.Fatalf("去选 W43 失败: %v", err)
	}
	changed, err := f.reports.MonthProjectReport("2026-10", "框架组", p1.ID)
	if err != nil {
		t.Fatalf("来源变化后读取失败: %v", err)
	}
	if !changed.SourceChanged {
		t.Fatalf("去选来源后应标记来源已更新: %+v", changed)
	}
	if _, err := f.reports.SetMonthSource("2026-10", "框架组", "2026-W43", p1.ID, true); err != nil {
		t.Fatalf("恢复 W43 失败: %v", err)
	}
	restored, err := f.reports.MonthProjectReport("2026-10", "框架组", p1.ID)
	if err != nil {
		t.Fatalf("恢复来源后读取失败: %v", err)
	}
	if restored.SourceChanged {
		t.Fatalf("恢复来源后不应再标记来源已更新: %+v", restored)
	}
}

func TestMonthProjectWithoutProjects(t *testing.T) {
	reports, _, _ := newTestReports(t)
	if _, err := reports.CreatePerson(PersonInput{Name: "陈组长", Group: "框架组", Role: store.RoleLead}); err != nil {
		t.Fatalf("创建组长失败: %v", err)
	}
	list, err := reports.MonthProjectList("2026-10", "框架组")
	if err != nil || len(list) != 0 {
		t.Fatalf("没有项目时列表 = %+v, %v", list, err)
	}
	if _, err := reports.MonthSources("2026-10", "框架组"); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("没有项目时 MonthSources 应 INVALID，得到 %v", err)
	}
}
