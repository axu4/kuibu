package main

import (
	"github.com/axu4/kuibu/internal/core"
)

// CalendarMonth 返回团队日历月视图数据，与 CLI `kuibu calendar month --json` 一致。
// perspective 为 "" 时默认 member（框架组长视角），"lead" 为 SE 视角。只读。
func (s *ReportService) CalendarMonth(month, group, perspective string) (core.CalendarMonth, error) {
	ws, _, err := s.instances()
	if err != nil {
		return core.CalendarMonth{}, err
	}
	cal, err := core.MonthCalendar(ws, month, group, perspective, s.clock())
	if err != nil {
		return core.CalendarMonth{}, err
	}
	return *cal, nil
}

// CalendarDay 返回团队日历日视图数据，与 CLI `kuibu calendar day --json` 一致。只读。
func (s *ReportService) CalendarDay(month string) (core.CalendarDayMonth, error) {
	ws, _, err := s.instances()
	if err != nil {
		return core.CalendarDayMonth{}, err
	}
	cal, err := core.DayCalendar(ws, month, s.clock())
	if err != nil {
		return core.CalendarDayMonth{}, err
	}
	return *cal, nil
}
