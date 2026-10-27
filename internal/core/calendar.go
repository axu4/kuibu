package core

import (
	"fmt"
	"sort"
	"time"

	"github.com/axu4/kuibu/internal/store"
)

// CalendarWeek 是月视图里的一列：一周的区间与周报日。
type CalendarWeek struct {
	// Week 是 ISO 周编号，形如 2026-W44。
	Week string `json:"week"`
	// Start 是周一日期，End 是周日日期（YYYY-MM-DD）。
	Start string `json:"start"`
	End   string `json:"end"`
	// ReportDay 是该周的周报日日期（YYYY-MM-DD）。
	ReportDay string `json:"reportDay"`
}

// CalendarCell 是月视图里一个人一格。
type CalendarCell struct {
	// Week 是 ISO 周编号。
	Week string `json:"week"`
	// Status 是 submitted / late / missing；该人该周不属应交范围时为空。
	Status string `json:"status"`
	// Completed 是本周归一稿「本周完成」条数。
	Completed int `json:"completed"`
	// Delayed 是对照上周计划中标为延期的条数。
	Delayed int `json:"delayed"`
	// Alignment 是对齐率 0~1；上周没有定稿或没有计划时为 nil（前端显示「—」）。
	Alignment *float64 `json:"alignment"`
}

// CalendarRow 是月视图里的一行：一个人。
type CalendarRow struct {
	// Person 是组员 ID，Name 是姓名，Group 是所属组，Role 是 member / lead。
	Person string         `json:"person"`
	Name   string         `json:"name"`
	Group  string         `json:"group"`
	Role   string         `json:"role"`
	Cells  []CalendarCell `json:"cells"`
}

// CalendarMonth 是月视图的完整数据：行 = 组员，列 = 当月各周。
type CalendarMonth struct {
	// Month 是月份编号，形如 2026-10。
	Month string `json:"month"`
	// Group 是 --group 过滤的组名，空表示全部组。
	Group string `json:"group"`
	// Perspective 是 member（框架组长视角，默认）或 lead（SE 视角）。
	Perspective string         `json:"perspective"`
	Weeks       []CalendarWeek `json:"weeks"`
	Rows        []CalendarRow  `json:"rows"`
}

// CalendarDayItem 是日视图里一天上的一条：会议笔记或待办截止。
type CalendarDayItem struct {
	// ID 是笔记 ID 或待办 ID，点击后跳到对应笔记 / 待办。
	ID string `json:"id"`
	// Title 是标题。
	Title string `json:"title"`
	// Kind 是 meeting / todo。
	Kind string `json:"kind"`
}

// CalendarDay 是日视图里的一天。
type CalendarDay struct {
	// Date 是日期（YYYY-MM-DD）。
	Date string `json:"date"`
	// Weekday 是 1=周一 … 7=周日。
	Weekday int `json:"weekday"`
	// IsReportDay 表示这天是周报日（默认周四）。
	IsReportDay bool `json:"isReportDay"`
	// IsCurrentMonth 表示这天是否属于所查月份；首尾补齐的跨月天为 false。
	IsCurrentMonth bool              `json:"isCurrentMonth"`
	Meetings       []CalendarDayItem `json:"meetings"`
	Todos          []CalendarDayItem `json:"todos"`
}

// CalendarDayMonth 是日视图的完整数据：按整周补齐首尾的日历格。
type CalendarDayMonth struct {
	// Month 是月份编号，ReportWeekday 是周报日（1-7）。
	Month         string        `json:"month"`
	ReportWeekday int           `json:"reportWeekday"`
	Days          []CalendarDay `json:"days"`
}

// MonthCalendar 返回月视图数据。perspective 为 "" 时默认 member；
// group 为 "" 时不过滤组。只读：不写任何工作台文件。
func MonthCalendar(ws *store.Workspace, month, group, perspective string, now time.Time) (*CalendarMonth, error) {
	if perspective == "" {
		perspective = store.RoleMember
	}
	level := store.ReportLevelMember
	if perspective == store.RoleLead {
		level = store.ReportLevelLead
	} else if perspective != store.RoleMember {
		return nil, fmt.Errorf("%w: 非法视角 %q（member|lead）", store.ErrInvalid, perspective)
	}
	m, err := store.ParseMonth(month, now)
	if err != nil {
		return nil, err
	}
	weekday, err := ws.ReportWeekday()
	if err != nil {
		return nil, err
	}
	weeks, err := WeeksOfMonth(m, weekday)
	if err != nil {
		return nil, err
	}
	people, err := ws.ListPersons()
	if err != nil {
		return nil, err
	}
	byID := map[string]*store.Person{}
	for _, p := range people {
		byID[p.ID] = p
	}

	out := &CalendarMonth{Month: m, Group: group, Perspective: perspective, Weeks: []CalendarWeek{}, Rows: []CalendarRow{}}
	rows := map[string]*CalendarRow{}
	// weekEntries[i] 是第 i 周的提交状态（应交 + 名单外）。
	weekEntries := make([]map[string]store.ReportStatusEntry, len(weeks))
	for i, w := range weeks {
		monday, sunday, err := store.WeekRange(w)
		if err != nil {
			return nil, err
		}
		day, err := store.WeekReportDay(w, weekday)
		if err != nil {
			return nil, err
		}
		out.Weeks = append(out.Weeks, CalendarWeek{
			Week: w, Start: monday.Format("2006-01-02"), End: sunday.Format("2006-01-02"),
			ReportDay: day.Format("2006-01-02"),
		})
		st, err := ws.ReportStatus(w, level, group, "", now)
		if err != nil {
			return nil, err
		}
		entries := map[string]store.ReportStatusEntry{}
		for _, e := range append(append([]store.ReportStatusEntry{}, st.Entries...), st.Extra...) {
			entries[e.Person] = e
			if _, ok := rows[e.Person]; !ok {
				rows[e.Person] = &CalendarRow{Person: e.Person, Name: e.Name, Group: e.Group, Role: e.Role, Cells: []CalendarCell{}}
			}
			if rows[e.Person].Name == "" {
				rows[e.Person].Name = e.Name
			}
			if rows[e.Person].Group == "" {
				rows[e.Person].Group = e.Group
			}
		}
		weekEntries[i] = entries
	}

	for wi, w := range weeks {
		monday, _, err := store.WeekRange(w)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			cell := CalendarCell{Week: w}
			if e, ok := weekEntries[wi][row.Person]; ok {
				cell.Status = e.Status
				cell.Completed, cell.Delayed, cell.Alignment = calendarCellCounts(ws, level, w, row.Person)
			} else if p, ok := byID[row.Person]; ok && p.Active {
				// 该周不在应交名单里：还没入组 / 已转出组时留空格子，不算未交。
				g, _ := p.MembershipAt(monday)
				if g == "" || (group != "" && g != group) {
					cell.Status = ""
				} else {
					cell.Status = "missing"
					cell.Completed, cell.Delayed, cell.Alignment = calendarCellCounts(ws, level, w, row.Person)
				}
			}
			row.Cells = append(row.Cells, cell)
		}
	}
	for _, row := range rows {
		if row.Cells == nil {
			row.Cells = []CalendarCell{}
		}
		out.Rows = append(out.Rows, *row)
	}
	sort.Slice(out.Rows, func(i, j int) bool {
		if out.Rows[i].Name != out.Rows[j].Name {
			return out.Rows[i].Name < out.Rows[j].Name
		}
		return out.Rows[i].Person < out.Rows[j].Person
	})
	return out, nil
}

// calendarCellCounts 返回某人某周的完成数、延期数与对齐率。
// 没有归一稿时三者均为零 / nil；上周没有定稿时对齐率为 nil，不报错，
// 调用方把对齐列显示为「—」。
func calendarCellCounts(ws *store.Workspace, level, week, personID string) (completed, delayed int, alignment *float64) {
	kind := store.NormalizedKind(level)
	cur, err := ws.ReadDraftKind(kind, week, personID)
	if err != nil {
		return 0, 0, nil
	}
	completed = len(ParseDraftItems(cur.Body)[SectionDone])
	prevWeek, err := PrevWeek(week)
	if err != nil {
		return completed, 0, nil
	}
	prev, err := ws.FinalDraftKind(kind, prevWeek, personID)
	if err != nil {
		return completed, 0, nil
	}
	res := ComputeReview(prev.Body, cur)
	return completed, res.Counts.Delayed, res.Alignment
}

// DayCalendar 返回日视图数据：整月按整周补齐首尾，
// 每天列出会议笔记与待办截止；周报日按工作台周报日标记。只读。
func DayCalendar(ws *store.Workspace, month string, now time.Time) (*CalendarDayMonth, error) {
	m, err := store.ParseMonth(month, now)
	if err != nil {
		return nil, err
	}
	weekday, err := ws.ReportWeekday()
	if err != nil {
		return nil, err
	}
	start, err := store.MonthStart(m)
	if err != nil {
		return nil, err
	}
	end := start.AddDate(0, 1, 0)

	meetingsByDate := map[string][]CalendarDayItem{}
	meetings, err := ListMeetings(ws)
	if err != nil {
		return nil, err
	}
	meetingWeekday, err := ws.MeetingWeekday()
	if err != nil {
		return nil, err
	}
	for _, mt := range meetings {
		date := mt.Date
		if date == "" && mt.Week != "" {
			if day, err := store.WeekReportDay(mt.Week, meetingWeekday); err == nil {
				date = day.Format("2006-01-02")
			}
		}
		if date == "" {
			continue
		}
		meetingsByDate[date] = append(meetingsByDate[date], CalendarDayItem{ID: mt.ID, Title: mt.Title, Kind: "meeting"})
	}

	todosByDate := map[string][]CalendarDayItem{}
	todos, err := ws.ListTodos()
	if err != nil {
		return nil, err
	}
	for _, td := range todos {
		if td.Status == store.StatusCanceled || td.Due == nil || td.Due.Date == "" {
			continue
		}
		todosByDate[td.Due.Date] = append(todosByDate[td.Due.Date], CalendarDayItem{ID: td.ID, Title: td.Title, Kind: "todo"})
	}
	for _, list := range meetingsByDate {
		sort.Slice(list, func(i, j int) bool { return list[i].Title < list[j].Title })
	}
	for _, list := range todosByDate {
		sort.Slice(list, func(i, j int) bool { return list[i].Title < list[j].Title })
	}

	// 从包含 1 日的那周周一，到包含月末的那周周日。
	first := start.AddDate(0, 0, -((int(start.Weekday()) + 6) % 7))
	lastDay := end.AddDate(0, 0, -1)
	last := lastDay.AddDate(0, 0, (7-int(lastDay.Weekday()))%7)

	out := &CalendarDayMonth{Month: m, ReportWeekday: weekday, Days: []CalendarDay{}}
	for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
		date := d.Format("2006-01-02")
		wd := (int(d.Weekday()) + 6) % 7 // 周一=1 … 周日=7
		meetings := meetingsByDate[date]
		if meetings == nil {
			meetings = []CalendarDayItem{}
		}
		dayTodos := todosByDate[date]
		if dayTodos == nil {
			dayTodos = []CalendarDayItem{}
		}
		out.Days = append(out.Days, CalendarDay{
			Date: date, Weekday: wd + 1, IsReportDay: wd+1 == weekday,
			IsCurrentMonth: store.MonthOf(d) == m, Meetings: meetings, Todos: dayTodos,
		})
	}
	return out, nil
}
