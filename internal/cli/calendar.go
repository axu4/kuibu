package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/axu4/kuibu/internal/core"
)

// runCalendar 实现 calendar month|day：日历只读汇总视图，
// 数据结构与 App 日历视图共用 internal/core 的 CalendarMonth / CalendarDayMonth。
func runCalendar(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	if len(args) == 0 {
		return usagef("calendar 需要子命令: month/day")
	}
	sub, rest := args[0], args[1:]
	fs := newFlagSet("calendar " + sub)
	addCommonFlags(fs, common)
	monthFlag := fs.String("month", "", "月份（如 2026-10），默认本月")
	group := fs.String("group", "", "组名（month），默认全部组")
	perspective := fs.String("perspective", "", "视角（month）: member（框架组长，默认）| lead（SE）")
	pos, err := parseCommand(fs, rest)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("calendar %s 不接受位置参数，用 --month / --group / --perspective", sub)
	}
	now, err := nowFrom(env)
	if err != nil {
		return err
	}
	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	switch sub {
	case "month":
		cal, err := core.MonthCalendar(ws, *monthFlag, *group, *perspective, now)
		if err != nil {
			return err
		}
		if common.json {
			return writeJSON(stdout, cal)
		}
		return printCalendarMonth(cal, stdout)
	case "day":
		cal, err := core.DayCalendar(ws, *monthFlag, now)
		if err != nil {
			return err
		}
		if common.json {
			return writeJSON(stdout, cal)
		}
		return printCalendarDay(cal, stdout)
	}
	return usagef("未知 calendar 子命令 %q", sub)
}

func printCalendarMonth(cal *core.CalendarMonth, stdout io.Writer) error {
	fmt.Fprintf(stdout, "团队日历 %s（%s）\n", cal.Month, map[string]string{"member": "框架组长", "lead": "SE"}[cal.Perspective])
	fmt.Fprintf(stdout, "%-10s", "组员")
	for _, w := range cal.Weeks {
		fmt.Fprintf(stdout, "  %s(%s–%s)", w.Week, w.Start[5:], w.End[5:])
	}
	fmt.Fprintln(stdout)
	for _, r := range cal.Rows {
		fmt.Fprintf(stdout, "%-10s", r.Name)
		for _, c := range r.Cells {
			fmt.Fprintf(stdout, "  %s", calendarCellLabel(c))
		}
		fmt.Fprintln(stdout)
	}
	return nil
}

func calendarCellLabel(c core.CalendarCell) string {
	if c.Status == "" {
		return "—"
	}
	align := "—"
	if c.Alignment != nil {
		align = fmt.Sprintf("%.0f%%", *c.Alignment*100)
	}
	return fmt.Sprintf("%s %d完 %d延 %s", calendarStatusLabel(c.Status), c.Completed, c.Delayed, align)
}

func calendarStatusLabel(status string) string {
	switch status {
	case "submitted":
		return "已交"
	case "late":
		return "迟交"
	default:
		return "未交"
	}
}

func printCalendarDay(cal *core.CalendarDayMonth, stdout io.Writer) error {
	fmt.Fprintf(stdout, "日历 %s（周报日：%s）\n", cal.Month, weekdayLabel(cal.ReportWeekday))
	for _, d := range cal.Days {
		if !d.IsCurrentMonth {
			continue
		}
		var parts []string
		for _, m := range d.Meetings {
			parts = append(parts, "会议："+m.Title)
		}
		for _, td := range d.Todos {
			parts = append(parts, "待办截止："+td.Title)
		}
		mark := ""
		if d.IsReportDay {
			mark = "（周报日）"
		}
		fmt.Fprintf(stdout, "%s%s  %s\n", d.Date, mark, strings.Join(parts, "；"))
	}
	return nil
}
