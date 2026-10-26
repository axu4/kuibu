package store

import (
	"testing"
	"time"
)

// fixedShanghai 返回 Asia/Shanghai 时区的日期时间。
func fixedShanghai(y int, m time.Month, d, hh, mm, ss int) time.Time {
	return time.Date(y, m, d, hh, mm, ss, 0, TodoLocation())
}

func TestWeekOfCrossYear(t *testing.T) {
	cases := []struct {
		date string
		want string
	}{
		{"2026-12-27", "2026-W52"},
		{"2026-12-28", "2026-W53"},
		{"2026-12-31", "2026-W53"},
		{"2027-01-01", "2026-W53"},
		{"2027-01-03", "2026-W53"},
		{"2027-01-04", "2027-W01"},
		{"2026-11-03", "2026-W45"},
	}
	for _, tc := range cases {
		d, err := time.ParseInLocation("2006-01-02", tc.date, TodoLocation())
		if err != nil {
			t.Fatalf("解析日期失败: %v", err)
		}
		if got := WeekOf(d); got != tc.want {
			t.Errorf("WeekOf(%s) = %q，期望 %q", tc.date, got, tc.want)
		}
	}
}

func TestWeekRange(t *testing.T) {
	monday, sunday, err := WeekRange("2026-W53")
	if err != nil {
		t.Fatalf("WeekRange 失败: %v", err)
	}
	if monday.Format("2006-01-02") != "2026-12-28" {
		t.Errorf("周一 = %s，期望 2026-12-28", monday.Format("2006-01-02"))
	}
	if sunday.Format("2006-01-02") != "2027-01-03" {
		t.Errorf("周日 = %s，期望 2027-01-03", sunday.Format("2006-01-02"))
	}

	if _, _, err := WeekRange("2026-W54"); err == nil {
		t.Errorf("2026-W54 应报错")
	}
	if _, _, err := WeekRange("not-a-week"); err == nil {
		t.Errorf("非法周编号应报错")
	}
}

func TestParseWeek(t *testing.T) {
	now := fixedShanghai(2026, 11, 3, 10, 0, 0)
	if got, _ := ParseWeek("this", now); got != "2026-W45" {
		t.Errorf("this = %q，期望 2026-W45", got)
	}
	if got, _ := ParseWeek("last", now); got != "2026-W44" {
		t.Errorf("last = %q，期望 2026-W44", got)
	}
	if got, _ := ParseWeek("next", now); got != "2026-W46" {
		t.Errorf("next = %q，期望 2026-W46", got)
	}
	if got, _ := ParseWeek("2026-12-31", now); got != "2026-W53" {
		t.Errorf("日期 = %q，期望 2026-W53", got)
	}
	if got, _ := ParseWeek("2026-w45", now); got != "2026-W45" {
		t.Errorf("小写 w = %q，期望 2026-W45", got)
	}
	if _, err := ParseWeek("2026-13-01", now); err == nil {
		t.Errorf("非法日期应报错")
	}
}

func TestWeekDeadlineBoundary(t *testing.T) {
	deadline, err := WeekDeadline("2026-W44", 4)
	if err != nil {
		t.Fatalf("WeekDeadline 失败: %v", err)
	}
	if deadline.Format("2006-01-02 15:04:05") != "2026-10-30 00:00:00" {
		t.Errorf("截止 = %s，期望 2026-10-30 00:00:00", deadline.Format("2006-01-02 15:04:05"))
	}

	before := fixedShanghai(2026, 10, 29, 23, 59, 59)
	at := fixedShanghai(2026, 10, 30, 0, 0, 0)
	if before.Before(deadline) == false {
		t.Errorf("周报日 23:59:59 应早于截止")
	}
	if at.Before(deadline) {
		t.Errorf("次日 00:00 不应早于截止")
	}
	// 迟交判定：!submittedAt.Before(deadline)。
	if !before.Before(deadline) {
		t.Errorf("23:59:59 不应迟交")
	}
	if at.Before(deadline) {
		t.Errorf("00:00 应迟交")
	}
}
