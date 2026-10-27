package core

import (
	"reflect"
	"testing"
)

func TestWeeksOfMonth(t *testing.T) {
	cases := []struct {
		month   string
		weekday int
		want    []string
	}{
		// 2026-10：周四分别是 1、8、15、22、29 日。
		{"2026-10", 4, []string{"2026-W40", "2026-W41", "2026-W42", "2026-W43", "2026-W44"}},
		// 周报日改周一：2026-10-26 是周一（W44），W40 的周一是 9-28 归 9 月。
		{"2026-10", 1, []string{"2026-W41", "2026-W42", "2026-W43", "2026-W44"}},
		// 跨年：2026-W01 的周四是 2026-01-01，归 2026-01；2025-W52 周四 12-25 归 2025-12。
		{"2026-01", 4, []string{"2026-W01", "2026-W02", "2026-W03", "2026-W04", "2026-W05"}},
		{"2025-12", 4, []string{"2025-W49", "2025-W50", "2025-W51", "2025-W52"}},
	}
	for _, c := range cases {
		got, err := WeeksOfMonth(c.month, c.weekday)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("WeeksOfMonth(%s, %d) = %v, %v; want %v", c.month, c.weekday, got, err, c.want)
		}
	}
	for week, want := range map[string]string{"2026-W01": "2026-01", "2025-W52": "2025-12", "2026-W40": "2026-10", "2020-W53": "2020-12"} {
		if got, err := MonthOfWeek(week, 4); err != nil || got != want {
			t.Errorf("MonthOfWeek(%s) = %s, %v; want %s", week, got, err, want)
		}
	}
}
