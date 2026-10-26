package main

import (
	"strings"
	"testing"
	"time"

	"github.com/axu4/kuibu/internal/index"
	"github.com/axu4/kuibu/internal/store"
)

// reportEvents 记录 ReportService 发出去的事件。
type reportEvents struct {
	names []string
	data  []any
}

func (e *reportEvents) reset() {
	e.names = nil
	e.data = nil
}

// newTestReports 构造一个挂在临时工作台上的 ReportService，并注入固定时钟与
// 事件记录器。
func newTestReports(t *testing.T) (*ReportService, *WorkspaceService, *reportEvents) {
	t.Helper()
	ws := newWorkspaceService()
	openTempWorkspace(t, ws)
	reports := newReportService(ws)
	reports.now = fixedNow
	events := &reportEvents{}
	reports.emit = func(name string, data any) {
		events.names = append(events.names, name)
		events.data = append(events.data, data)
	}
	return reports, ws, events
}

func TestReportWeekAndShift(t *testing.T) {
	reports, _, _ := newTestReports(t)

	week, err := reports.Week("this")
	if err != nil {
		t.Fatalf("Week 返回错误: %v", err)
	}
	if week.Week != "2026-W44" {
		t.Errorf("Week = %q，期望 2026-W44", week.Week)
	}
	if week.Start != "2026-10-26" || week.End != "2026-11-01" {
		t.Errorf("周范围 = %s / %s，期望 2026-10-26 / 2026-11-01", week.Start, week.End)
	}
	if !week.IsCurrent {
		t.Error("this 周应标记为当前周")
	}
	if week.ReportDay != store.DefaultReportWeekday {
		t.Errorf("ReportDay = %d，期望默认周四 %d", week.ReportDay, store.DefaultReportWeekday)
	}

	// 空 ref 等同 this。
	if w, err := reports.Week(""); err != nil || w.Week != "2026-W44" {
		t.Errorf("Week(\"\") = %+v, %v", w, err)
	}

	last, err := reports.ShiftWeek("2026-W44", -1)
	if err != nil {
		t.Fatalf("ShiftWeek 返回错误: %v", err)
	}
	if last.Week != "2026-W43" {
		t.Errorf("上一周 = %q，期望 2026-W43", last.Week)
	}
	next, err := reports.ShiftWeek("2026-W44", 1)
	if err != nil {
		t.Fatalf("ShiftWeek 返回错误: %v", err)
	}
	if next.Week != "2026-W45" {
		t.Errorf("下一周 = %q，期望 2026-W45", next.Week)
	}
}

func TestReportPreviewSubmitStatusAndGet(t *testing.T) {
	reports, ws, _ := newTestReports(t)

	zhang, err := reports.CreatePerson(PersonInput{
		Name:         "张三",
		Aliases:      []string{"小张"},
		Group:        "框架组",
		ProductLines: []string{"Kuibu"},
		Role:         store.RoleMember,
	})
	if err != nil {
		t.Fatalf("CreatePerson 张三失败: %v", err)
	}
	li, err := reports.CreatePerson(PersonInput{Name: "李四", Group: "框架组", Role: store.RoleMember})
	if err != nil {
		t.Fatalf("CreatePerson 李四失败: %v", err)
	}

	preview, err := reports.Preview(
		"【周报】张三 2026-10-29\n完成核心库\n\n【周报】李四 2026/10/29\n完成 CLI\n",
		store.ReportLevelMember, "", "")
	if err != nil {
		t.Fatalf("Preview 失败: %v", err)
	}
	if len(preview) != 2 {
		t.Fatalf("预览份数 = %d，期望 2", len(preview))
	}
	if preview[0].PersonID != zhang.ID || preview[0].Week != "2026-W44" {
		t.Errorf("第一份 = %+v", preview[0])
	}
	if preview[1].PersonID != li.ID || preview[1].Week != "2026-W44" {
		t.Errorf("第二份 = %+v", preview[1])
	}

	items := make([]SubmitItem, 0, len(preview))
	for _, p := range preview {
		items = append(items, SubmitItem{
			Text:   p.Text,
			Person: p.PersonID,
			Week:   p.Week,
			Level:  p.Level,
		})
	}
	results, err := reports.Submit(items)
	if err != nil {
		t.Fatalf("Submit 失败: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("提交结果数 = %d，期望 2", len(results))
	}
	if results[0].Source != store.ReportSourcePaste {
		t.Errorf("来源 = %q，期望 paste", results[0].Source)
	}

	st, err := reports.Status("2026-W44", store.ReportLevelMember, "", "")
	if err != nil {
		t.Fatalf("Status 失败: %v", err)
	}
	if st.Counts.Expected != 2 || st.Counts.Submitted != 2 || st.Counts.Late != 0 || st.Counts.Missing != 0 {
		t.Errorf("计数 = %+v", st.Counts)
	}
	for _, e := range st.Entries {
		if e.Status != "submitted" {
			t.Errorf("状态 = %q，期望 submitted", e.Status)
		}
		if e.FirstSubmittedAt == "" || e.SubmittedAt == "" {
			t.Errorf("提交时间缺失: %+v", e)
		}
	}

	got, err := reports.Get("2026-W44", zhang.ID, store.ReportLevelMember)
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got.Name != "张三" || !strings.Contains(got.Body, "完成核心库") {
		t.Errorf("Get = %+v", got)
	}

	// 落盘检查：文件直接可读且来源正确。
	if _, err := ws.workspace().ReadReport("2026-W44", store.ReportLevelMember, zhang.ID); err != nil {
		t.Errorf("落盘周报不可读: %v", err)
	}
}

func TestReportLate(t *testing.T) {
	reports, _, _ := newTestReports(t)

	p, err := reports.CreatePerson(PersonInput{Name: "王五", Group: "框架组", Role: store.RoleMember})
	if err != nil {
		t.Fatalf("CreatePerson 失败: %v", err)
	}

	// 周报日（周四）之后才提交，应记为迟交。
	reports.now = func() time.Time {
		return time.Date(2026, 10, 31, 10, 0, 0, 0, store.TodoLocation())
	}
	if _, err := reports.Submit([]SubmitItem{{
		Text:   "【周报】王五 2026-10-30\n周末补交",
		Person: p.ID,
		Week:   "2026-W44",
		Level:  store.ReportLevelMember,
	}}); err != nil {
		t.Fatalf("Submit 失败: %v", err)
	}

	st, err := reports.Status("2026-W44", store.ReportLevelMember, "", "")
	if err != nil {
		t.Fatalf("Status 失败: %v", err)
	}
	if st.Counts.Late != 1 || st.Counts.Submitted != 0 || st.Counts.Missing != 0 {
		t.Errorf("计数 = %+v，期望 late=1", st.Counts)
	}
	if len(st.Entries) != 1 || st.Entries[0].Status != "late" {
		t.Errorf("条目 = %+v", st.Entries)
	}
}

func TestReportOverwriteKeepsFirstSubmittedAt(t *testing.T) {
	reports, _, _ := newTestReports(t)

	p, err := reports.CreatePerson(PersonInput{Name: "张三", Group: "框架组", Role: store.RoleMember})
	if err != nil {
		t.Fatalf("CreatePerson 失败: %v", err)
	}
	first := []SubmitItem{{
		Text:   "【周报】张三 2026-10-29\n第一版",
		Person: p.ID,
		Week:   "2026-W44",
		Level:  store.ReportLevelMember,
	}}
	if _, err := reports.Submit(first); err != nil {
		t.Fatalf("首次 Submit 失败: %v", err)
	}

	// 周报日之后覆盖提交：覆盖沿用旧 firstSubmittedAt，不应变成迟交。
	reports.now = func() time.Time {
		return time.Date(2026, 10, 31, 10, 0, 0, 0, store.TodoLocation())
	}
	preview, err := reports.Preview("【周报】张三 2026-10-30\n第二版", store.ReportLevelMember, "", "")
	if err != nil {
		t.Fatalf("Preview 失败: %v", err)
	}
	if len(preview) != 1 || !preview[0].Overwrite {
		t.Fatalf("预览应标记覆盖: %+v", preview)
	}
	if preview[0].Late {
		t.Errorf("按时首交的人覆盖提交不应标记迟交: %+v", preview[0])
	}

	if _, err := reports.Submit([]SubmitItem{{
		Text:   preview[0].Text,
		Person: preview[0].PersonID,
		Week:   preview[0].Week,
		Level:  store.ReportLevelMember,
	}}); err != nil {
		t.Fatalf("覆盖 Submit 失败: %v", err)
	}

	got, err := reports.Get("2026-W44", p.ID, store.ReportLevelMember)
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if !strings.Contains(got.Body, "第二版") {
		t.Errorf("覆盖后正文 = %q", got.Body)
	}

	st, err := reports.Status("2026-W44", store.ReportLevelMember, "", "")
	if err != nil {
		t.Fatalf("Status 失败: %v", err)
	}
	if st.Counts.Late != 0 || st.Counts.Submitted != 1 {
		t.Errorf("覆盖后计数 = %+v", st.Counts)
	}
	if st.Entries[0].FirstSubmittedAt != got.FirstSubmittedAt {
		t.Errorf("firstSubmittedAt 应保持首次提交: %+v", st.Entries[0])
	}
}

func TestReportDelete(t *testing.T) {
	reports, _, _ := newTestReports(t)

	p, _ := reports.CreatePerson(PersonInput{Name: "张三", Group: "框架组", Role: store.RoleMember})
	if _, err := reports.Submit([]SubmitItem{{
		Text:   "【周报】张三 2026-10-29\n内容",
		Person: p.ID,
		Week:   "2026-W44",
		Level:  store.ReportLevelMember,
	}}); err != nil {
		t.Fatalf("Submit 失败: %v", err)
	}

	if err := reports.Delete("2026-W44", p.ID, store.ReportLevelMember); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}
	if _, err := reports.Get("2026-W44", p.ID, store.ReportLevelMember); err == nil {
		t.Error("删除后应读不到周报")
	}

	st, err := reports.Status("2026-W44", store.ReportLevelMember, "", "")
	if err != nil {
		t.Fatalf("Status 失败: %v", err)
	}
	if st.Counts.Missing != 1 || st.Counts.Submitted != 0 {
		t.Errorf("删除后计数 = %+v", st.Counts)
	}
}

func TestReportPeopleCRUDAndMove(t *testing.T) {
	reports, _, _ := newTestReports(t)

	p, err := reports.CreatePerson(PersonInput{
		Name:         "张三",
		Aliases:      []string{"小张", " 小张 "},
		Group:        "框架组",
		ProductLines: []string{"Kuibu"},
		Role:         store.RoleMember,
	})
	if err != nil {
		t.Fatalf("CreatePerson 失败: %v", err)
	}
	if p.Name != "张三" || len(p.Aliases) != 1 || p.Aliases[0] != "小张" {
		t.Errorf("创建结果 = %+v", p)
	}
	if len(p.ProductLines) != 1 || p.ProductLines[0] != "Kuibu" {
		t.Errorf("产品线 = %+v", p.ProductLines)
	}

	// 姓名唯一性（大小写不敏感）。
	if _, err := reports.CreatePerson(PersonInput{Name: " 张三 ", Group: "框架组", Role: store.RoleMember}); err == nil {
		t.Error("重名应返回错误")
	}

	updated, err := reports.UpdatePerson(p.ID, PersonInput{
		Name:         "张三丰",
		Aliases:      []string{"小张"},
		Group:        "框架组",
		ProductLines: []string{"Kuibu"},
		Role:         store.RoleMember,
	})
	if err != nil {
		t.Fatalf("UpdatePerson 失败: %v", err)
	}
	if updated.Name != "张三丰" {
		t.Errorf("修改后姓名 = %q", updated.Name)
	}

	// 今天生效的转组：当前组立即变化。先把时钟拨到创建之后，避免与加入
	// 记录同一天（同一天时 MembershipAt 取第一条记录）。
	reports.now = func() time.Time {
		return time.Date(2026, 10, 28, 10, 0, 0, 0, store.TodoLocation())
	}
	moved, err := reports.MovePerson(p.ID, "产品组", []string{"Wechat"}, "2026-10-28")
	if err != nil {
		t.Fatalf("MovePerson 失败: %v", err)
	}
	if moved.Group != "产品组" || len(moved.ProductLines) != 1 || moved.ProductLines[0] != "Wechat" {
		t.Errorf("转组结果 = %+v", moved)
	}

	// 未来生效的转组不改变当前组。
	future, err := reports.MovePerson(p.ID, "海外组", nil, "2026-11-02")
	if err != nil {
		t.Fatalf("MovePerson 失败: %v", err)
	}
	if future.Group != "产品组" {
		t.Errorf("未来转组不应改变当前组: %+v", future)
	}

	inactive, err := reports.SetPersonActive(p.ID, false)
	if err != nil {
		t.Fatalf("SetPersonActive 失败: %v", err)
	}
	if inactive.Active {
		t.Error("应设为离职")
	}

	activeOnly, err := reports.People(false)
	if err != nil {
		t.Fatalf("People(false) 失败: %v", err)
	}
	for _, item := range activeOnly {
		if item.ID == p.ID {
			t.Error("离职人员不应出现在在职列表")
		}
	}
	all, err := reports.People(true)
	if err != nil {
		t.Fatalf("People(true) 失败: %v", err)
	}
	found := false
	for _, item := range all {
		if item.ID == p.ID {
			found = true
			if item.Active {
				t.Error("全量列表里离职人员应标记 Active=false")
			}
		}
	}
	if !found {
		t.Error("全量列表应包含离职人员")
	}

	groups, err := reports.Groups()
	if err != nil {
		t.Fatalf("Groups 失败: %v", err)
	}
	if len(groups) == 0 {
		t.Error("应汇总出组名")
	}
	lines, err := reports.ProductLines()
	if err != nil {
		t.Fatalf("ProductLines 失败: %v", err)
	}
	if len(lines) == 0 {
		t.Error("应汇总出产品线")
	}
}

func TestReportWeekdayConfig(t *testing.T) {
	reports, _, _ := newTestReports(t)

	if n, err := reports.ReportWeekday(); err != nil || n != store.DefaultReportWeekday {
		t.Errorf("默认周报日 = %d, %v", n, err)
	}
	if n, err := reports.SetReportWeekday(5); err != nil || n != 5 {
		t.Errorf("SetReportWeekday = %d, %v", n, err)
	}
	if n, _ := reports.ReportWeekday(); n != 5 {
		t.Errorf("修改后周报日 = %d，期望 5", n)
	}
	if _, err := reports.SetReportWeekday(8); err == nil {
		t.Error("非法周报日应返回错误")
	}
}

func TestReportServiceEmitsEvents(t *testing.T) {
	reports, _, events := newTestReports(t)

	p, err := reports.CreatePerson(PersonInput{Name: "张三", Group: "框架组", Role: store.RoleMember})
	if err != nil {
		t.Fatalf("CreatePerson 失败: %v", err)
	}
	if len(events.names) != 1 || events.names[0] != reportChangedEvent {
		t.Fatalf("新增组员事件 = %v", events.names)
	}

	events.reset()
	if _, err := reports.Submit([]SubmitItem{{
		Text:   "【周报】张三 2026-10-29\n内容",
		Person: p.ID,
		Week:   "2026-W44",
		Level:  store.ReportLevelMember,
	}}); err != nil {
		t.Fatalf("Submit 失败: %v", err)
	}
	if len(events.names) != 1 || events.names[0] != reportChangedEvent {
		t.Errorf("提交事件 = %v", events.names)
	}

	events.reset()
	if err := reports.Delete("2026-W44", p.ID, store.ReportLevelMember); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}
	if len(events.names) != 1 || events.names[0] != reportChangedEvent {
		t.Errorf("删除事件 = %v", events.names)
	}
	if data, ok := events.data[0].(ReportChangeEvent); !ok || len(data.ReportIDs) != 1 {
		t.Errorf("删除事件数据 = %+v", events.data[0])
	}
}

func TestHandleWatchEventEmitsReportChanged(t *testing.T) {
	var got []struct {
		name string
		data any
	}
	emit := func(name string, data any) {
		got = append(got, struct {
			name string
			data any
		}{name, data})
	}

	handleWatchEvent(index.Event{PeopleIDs: []string{"p1"}, ReportIDs: []string{"2026-W44/member/p1"}}, emit)
	if len(got) != 1 || got[0].name != reportChangedEvent {
		t.Fatalf("组员/周报变化事件 = %+v", got)
	}
	data, ok := got[0].data.(ReportChangeEvent)
	if !ok || len(data.PeopleIDs) != 1 || len(data.ReportIDs) != 1 {
		t.Errorf("事件数据 = %+v", got[0].data)
	}

	// 只有组员变化也发 report:changed，且 ReportIDs 为 [] 而不是 nil。
	got = nil
	handleWatchEvent(index.Event{PeopleIDs: []string{"p2"}}, emit)
	if len(got) != 1 {
		t.Fatalf("组员变化事件 = %+v", got)
	}
	if data, ok := got[0].data.(ReportChangeEvent); !ok || data.ReportIDs == nil || len(data.ReportIDs) != 0 {
		t.Errorf("组员事件数据 = %+v", got[0].data)
	}

	// 笔记与待办变化不触发 report:changed。
	got = nil
	handleWatchEvent(index.Event{IDs: []string{"n1"}, TodoIDs: []string{"t1"}}, emit)
	for _, e := range got {
		if e.name == reportChangedEvent {
			t.Error("笔记/待办变化不应触发 report:changed")
		}
	}
}
