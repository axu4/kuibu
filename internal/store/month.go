package store

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// DefaultReportMonthDay 是默认月报收集日：每月 28 日（不因周末顺延）。
const DefaultReportMonthDay = 28

// monthPattern 匹配月份编号，形如 2026-10。
var monthPattern = regexp.MustCompile(`^(\d{4})-(0[1-9]|1[0-2])$`)

// ValidMonth 判断是否是 YYYY-MM 月份编号。
func ValidMonth(s string) bool {
	return monthPattern.MatchString(s)
}

// IsWeek 判断是否是 ISO 周编号。
func IsWeek(s string) bool {
	return isoWeekPattern.MatchString(s)
}

// ValidPeriod 校验周或月编号（归一稿、组报等既可按周也可按月）。
func ValidPeriod(p string) error {
	if ValidMonth(p) {
		return nil
	}
	_, _, err := WeekRange(p)
	return err
}

// validPeriodFor 组长月报要求月份编号，其余层级要求周编号。
func validPeriodFor(level, p string) error {
	if level == ReportLevelLeadMonth {
		if !ValidMonth(p) {
			return fmt.Errorf("%w: 月报需要月份编号（如 2026-10），得到 %q", ErrInvalid, p)
		}
		return nil
	}
	_, _, err := WeekRange(p)
	return err
}

// MonthOf 返回日期所在月份编号（TodoLocation 时区）。
func MonthOf(t time.Time) string {
	return t.In(TodoLocation()).Format("2006-01")
}

// MonthStart 返回月份第一天 00:00（TodoLocation 时区）。
func MonthStart(month string) (time.Time, error) {
	if !ValidMonth(month) {
		return time.Time{}, fmt.Errorf("%w: 无法解析月份 %q", ErrInvalid, month)
	}
	return time.ParseInLocation("2006-01", month, TodoLocation())
}

// ParseMonth 解析月份参数：this/last/next、YYYY-MM 或 YYYY-MM-DD 日期。
func ParseMonth(s string, now time.Time) (string, error) {
	s = strings.TrimSpace(s)
	cur := time.Date(now.In(TodoLocation()).Year(), now.In(TodoLocation()).Month(), 1, 0, 0, 0, 0, TodoLocation())
	switch strings.ToLower(s) {
	case "", "this":
		return MonthOf(cur), nil
	case "last":
		return MonthOf(cur.AddDate(0, -1, 0)), nil
	case "next":
		return MonthOf(cur.AddDate(0, 1, 0)), nil
	}
	if ValidMonth(s) {
		return s, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", s, TodoLocation()); err == nil {
		return MonthOf(t), nil
	}
	return "", fmt.Errorf("%w: 无法解析月份 %q", ErrInvalid, s)
}

// MonthCollectDay 返回该月收集日 00:00；day 超过当月天数时取月末。
func MonthCollectDay(month string, day int) (time.Time, error) {
	start, err := MonthStart(month)
	if err != nil {
		return time.Time{}, err
	}
	if day <= 0 {
		day = DefaultReportMonthDay
	}
	last := start.AddDate(0, 1, -1).Day()
	if day > last {
		day = last
	}
	return start.AddDate(0, 0, day-1), nil
}

// ReportMonthDay 返回工作台配置的月报收集日，缺省 28。
func (w *Workspace) ReportMonthDay() (int, error) {
	cfg, err := w.readConfig()
	if err != nil {
		return DefaultReportMonthDay, err
	}
	if cfg.ReportMonthDay < 1 || cfg.ReportMonthDay > 31 {
		return DefaultReportMonthDay, nil
	}
	return cfg.ReportMonthDay, nil
}

// SetReportMonthDay 修改月报收集日。
func (w *Workspace) SetReportMonthDay(day int) error {
	if day < 1 || day > 31 {
		return fmt.Errorf("%w: 月报收集日必须在 1-31 之间", ErrInvalid)
	}
	cfg, err := w.readConfig()
	if err != nil {
		return err
	}
	cfg.ReportMonthDay = day
	return w.writeConfig(cfg)
}

// MonthDeadline 返回月报迟交判定时刻：收集日当天 24:00。
func (w *Workspace) MonthDeadline(month string) (time.Time, error) {
	day, err := w.ReportMonthDay()
	if err != nil {
		return time.Time{}, err
	}
	d, err := MonthCollectDay(month, day)
	if err != nil {
		return time.Time{}, err
	}
	return d.AddDate(0, 0, 1), nil
}

// SubmitMonthReport 提交一份组长月报原文（覆盖时旧文件进 .history）。
func (w *Workspace) SubmitMonthReport(person, month, body, source string, now time.Time) (*Report, error) {
	if !ValidMonth(month) {
		return nil, fmt.Errorf("%w: 无法解析月份 %q", ErrInvalid, month)
	}
	p, err := w.ResolvePerson(person)
	if err != nil {
		return nil, err
	}
	start, err := MonthStart(month)
	if err != nil {
		return nil, err
	}
	group, products := p.MembershipAt(start)
	if group == "" {
		group, products = p.Group, p.ProductLines
	}
	if source == "" {
		source = ReportSourcePaste
	}
	if now.IsZero() {
		now = time.Now()
	}
	return w.WriteReport(NewReport{
		Person: p.ID, Name: p.Name, Week: month, Level: ReportLevelLeadMonth,
		Group: group, ProductLines: products, Source: source, SubmittedAt: now, Body: body,
	})
}

// DefaultMeetingWeekday 是默认周会日：周一。
const DefaultMeetingWeekday = 1

// MeetingWeekday 返回工作台配置的周会日（1-7），缺省周一。
func (w *Workspace) MeetingWeekday() (int, error) {
	cfg, err := w.readConfig()
	if err != nil {
		return DefaultMeetingWeekday, err
	}
	if cfg.MeetingWeekday < 1 || cfg.MeetingWeekday > 7 {
		return DefaultMeetingWeekday, nil
	}
	return cfg.MeetingWeekday, nil
}

// SetMeetingWeekday 修改周会日。
func (w *Workspace) SetMeetingWeekday(day int) error {
	if day < 1 || day > 7 {
		return fmt.Errorf("%w: 周会日必须在 1-7 之间", ErrInvalid)
	}
	cfg, err := w.readConfig()
	if err != nil {
		return err
	}
	cfg.MeetingWeekday = day
	return w.writeConfig(cfg)
}
