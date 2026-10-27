package core

import (
	"testing"
	"time"

	"github.com/axu4/kuibu/internal/store"
)

func calendarCell(t *testing.T, cal *CalendarMonth, person, week string) CalendarCell {
	t.Helper()
	for _, r := range cal.Rows {
		if r.Person != person {
			continue
		}
		for _, c := range r.Cells {
			if c.Week == week {
				return c
			}
		}
	}
	t.Fatalf("找不到 %s 在 %s 的格子", person, week)
	return CalendarCell{}
}

// 样例：2026-10 月，组员张三在 W43 交了周报，W44 未交；
// W43 的归一稿有 2 条完成，对照 W42 的计划标了 1 条延期，
// 对齐率 = (0 完成 + 0.5×0 部分) / (1 计划 − 0 取消) = 0（分母含延期）。
func TestMonthCalendarSample(t *testing.T) {
	ws := newReportWorkspace(t)
	now := reportNow()

	p, err := ws.CreatePerson(store.NewPerson{Name: "张三", Group: "框架组", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	// W42 定稿：下周计划 1 条。
	prevBody := "## 本周完成\n- 旧事项\n## 下周计划\n- 计划 A\n## 风险与求助\n- 无"
	if _, err := ws.SaveGeneratedDraft(store.GeneratedDraft{Person: p.ID, Name: "张三", Week: "2026-W42", Body: prevBody, Now: now}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.FinalizeDraft("2026-W42", p.ID, now); err != nil {
		t.Fatal(err)
	}
	// W43 归一稿：完成 2 条，并把 W42 的计划标为延期。
	curBody := "## 本周完成\n- 完成一\n- 完成二\n## 下周计划\n- 计划 B\n## 风险与求助\n- 无"
	if _, err := ws.SaveGeneratedDraft(store.GeneratedDraft{Person: p.ID, Name: "张三", Week: "2026-W43", Body: curBody, Now: now}, true); err != nil {
		t.Fatal(err)
	}
	plans := ParseDraftItems(prevBody)[SectionPlan]
	if _, err := ws.SaveDraftReview("2026-W43", p.ID, store.DraftReview{Items: []store.ReviewItem{
		{Plan: plans[0].ID, Status: store.ReviewDelayed, Source: "human"},
	}}); err != nil {
		t.Fatal(err)
	}
	// W43 交了周报（周报日周四 10-22 之前提交，不迟交）。
	if _, err := ws.WriteReport(store.NewReport{
		Person: p.ID, Name: "张三", Week: "2026-W43", Level: store.ReportLevelMember,
		Group: "框架组", Source: store.ReportSourceCLI,
		SubmittedAt: time.Date(2026, 10, 21, 18, 0, 0, 0, store.TodoLocation()), Body: "周报正文",
	}); err != nil {
		t.Fatal(err)
	}

	cal, err := MonthCalendar(ws, "2026-10", "框架组", "", now)
	if err != nil {
		t.Fatal(err)
	}
	// 2026-10 按周四归属：W40–W44，其中 W40 的周一在 9 月，属跨月周。
	if len(cal.Weeks) != 5 || cal.Weeks[0].Week != "2026-W40" || cal.Weeks[0].Start != "2026-09-28" {
		t.Fatalf("weeks = %+v，期望 W40 起、周一起 2026-09-28", cal.Weeks)
	}
	if len(cal.Rows) != 1 {
		t.Fatalf("rows = %d，期望 1", len(cal.Rows))
	}
	cell := calendarCell(t, cal, p.ID, "2026-W43")
	if cell.Status != "submitted" || cell.Completed != 2 || cell.Delayed != 1 {
		t.Fatalf("W43 格子 = %+v，期望 submitted/完成 2/延期 1", cell)
	}
	if cell.Alignment == nil || *cell.Alignment != 0 {
		t.Fatalf("W43 对齐率 = %v，期望 0", cell.Alignment)
	}
	// W44 没有周报与归一稿：未交、完成 0、对齐率 nil（显示「—」）。
	cell = calendarCell(t, cal, p.ID, "2026-W44")
	if cell.Status != "missing" || cell.Completed != 0 || cell.Alignment != nil {
		t.Fatalf("W44 格子 = %+v，期望 missing/完成 0/对齐率 nil", cell)
	}
	// 视角参数非法时报错，不返回半成品。
	if _, err := MonthCalendar(ws, "2026-10", "", "bogus", now); err == nil {
		t.Errorf("非法视角应报错")
	}
}

func TestDayCalendarSample(t *testing.T) {
	ws := newReportWorkspace(t)
	now := reportNow()

	// 周会笔记：OpenMeeting 在周会日（默认周一）建一条，日期为该周周一。
	n, _, err := OpenMeeting(ws, "2026-W44")
	if err != nil {
		t.Fatal(err)
	}
	// 待办截止：10-29（周四，周报日）一条，11-01（补齐格，周日）一条。
	if _, err := ws.CreateTodo(store.NewTodo{Title: "周四截止", Due: &store.Due{Date: "2026-10-29"}, Now: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.CreateTodo(store.NewTodo{Title: "下月截止", Due: &store.Due{Date: "2026-11-01"}, Now: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.CreateTodo(store.NewTodo{Title: "已取消不出现", Due: &store.Due{Date: "2026-10-29"}, Now: now}); err != nil {
		t.Fatal(err)
	}
	// 上一条待办改为取消，应从日视图消失（取回再改状态的写法走 SetTodoStatus）。
	todos, err := ws.ListTodos()
	if err != nil {
		t.Fatal(err)
	}
	for _, td := range todos {
		if td.Title == "已取消不出现" {
			if _, err := ws.SetTodoStatus(td.ID, store.StatusCanceled, now); err != nil {
				t.Fatal(err)
			}
		}
	}

	day, err := DayCalendar(ws, "2026-10", now)
	if err != nil {
		t.Fatal(err)
	}
	if day.ReportWeekday != 4 {
		t.Fatalf("周报日 = %d，期望 4（周四）", day.ReportWeekday)
	}
	byDate := map[string]CalendarDay{}
	for _, d := range day.Days {
		byDate[d.Date] = d
	}
	// 10-26 周一：周会笔记在这一天，且不是周报日。
	oct26 := byDate["2026-10-26"]
	if oct26.IsReportDay {
		t.Errorf("2026-10-26 不应是周报日")
	}
	found := false
	for _, mt := range oct26.Meetings {
		if mt.ID == n.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("2026-10-26 会议 = %+v，期望包含周会 %s", oct26.Meetings, n.ID)
	}
	// 10-29 周四：是周报日，且只有未取消的待办。
	oct29 := byDate["2026-10-29"]
	if !oct29.IsReportDay || len(oct29.Todos) != 1 || oct29.Todos[0].Title != "周四截止" {
		t.Fatalf("2026-10-29 = %+v，期望周报日且 1 条待办", oct29)
	}
	// 补齐格：11-01 属下月但在 10 月最后一周内，IsCurrentMonth 为 false 仍列出待办。
	nov01 := byDate["2026-11-01"]
	if nov01.IsCurrentMonth || len(nov01.Todos) != 1 {
		t.Fatalf("2026-11-01 = %+v，期望非本月且 1 条待办", nov01)
	}
	// 日历格按整周补齐：首天应为 2026-09-28（周一），末天应为 2026-11-01（周日）。
	if day.Days[0].Date != "2026-09-28" || day.Days[len(day.Days)-1].Date != "2026-11-01" {
		t.Fatalf("首末天 = %s / %s，期望 2026-09-28 / 2026-11-01",
			day.Days[0].Date, day.Days[len(day.Days)-1].Date)
	}
}
