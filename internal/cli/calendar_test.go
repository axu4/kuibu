package cli

import (
	"strings"
	"testing"
)

// 空工作台的日历命令：month/day --json 应返回与核心库一致的结构，
// 2026-10 按周四归属有 5 周，日视图按整周补齐为 35 格（9-28 至 11-01）。
func TestCLICalendarEmptyWorkspace(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()

	month := e.mustRun("", "calendar", "month", "--workspace", e.workspace, "--month", "2026-10", "--json").jsonOut(t)
	if month["month"] != "2026-10" {
		t.Fatalf("month = %v，期望 2026-10", month["month"])
	}
	weeks, _ := month["weeks"].([]any)
	if len(weeks) != 5 {
		t.Fatalf("weeks 条数 = %d，期望 5", len(weeks))
	}
	if rows, _ := month["rows"].([]any); len(rows) != 0 {
		t.Fatalf("rows 条数 = %d，期望 0（空工作台）", len(rows))
	}

	day := e.mustRun("", "calendar", "day", "--workspace", e.workspace, "--month", "2026-10", "--json").jsonOut(t)
	if day["reportWeekday"] != float64(4) {
		t.Fatalf("reportWeekday = %v，期望 4", day["reportWeekday"])
	}
	days, _ := day["days"].([]any)
	if len(days) != 35 {
		t.Fatalf("days 条数 = %d，期望 35", len(days))
	}
	first, _ := days[0].(map[string]any)
	last, _ := days[len(days)-1].(map[string]any)
	if first["date"] != "2026-09-28" || last["date"] != "2026-11-01" {
		t.Fatalf("首末天 = %v / %v，期望 2026-09-28 / 2026-11-01", first["date"], last["date"])
	}

	// 人类模式输出包含月份与周报日文案；非法子命令与非法月份走参数错误。
	human := e.mustRun("", "calendar", "month", "--workspace", e.workspace, "--month", "2026-10")
	if !strings.Contains(human.stdout, "2026-10") {
		t.Fatalf("人类模式输出缺少月份:\\n%s", human.stdout)
	}
	if res := e.run("", "calendar", "bogus", "--workspace", e.workspace); res.code != 1 {
		t.Fatalf("非法子命令退出码 = %d，期望 1", res.code)
	}
	if res := e.run("", "calendar", "month", "--workspace", e.workspace, "--month", "2026-13"); res.code != 1 {
		t.Fatalf("非法月份退出码 = %d，期望 1", res.code)
	}
}
