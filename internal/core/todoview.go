package core

import (
	"fmt"
	"time"

	"github.com/axu4/kuibu/internal/store"
)

// 待办视图，与 App 侧栏分组一致。
const (
	TodoViewAll   = "all"
	TodoViewToday = "today"
	TodoViewWeek  = "week"
	TodoViewDone  = "done"
)

// ValidTodoView 判断视图名是否合法。
func ValidTodoView(view string) bool {
	switch view {
	case TodoViewAll, TodoViewToday, TodoViewWeek, TodoViewDone:
		return true
	}
	return false
}

// TodoViewStatuses 返回视图包含的状态：done 视图是已完成 / 已取消，其余是未完成。
func TodoViewStatuses(view string) []string {
	if view == TodoViewDone {
		return []string{store.StatusDone, store.StatusCanceled}
	}
	return []string{store.StatusTodo, store.StatusDoing}
}

// InTodoView 判断截止日期是否落在视图里（状态由 TodoViewStatuses 另行过滤）：
// today = 有截止且不晚于今天（含逾期），week = 落在本周，all / done 不看日期。
// due 是索引里的截止字符串（YYYY-MM-DD 或 YYYY-MM-DD HH:MM）。
func InTodoView(view, due string, now time.Time) (bool, error) {
	date := ""
	if len(due) >= 10 {
		date = due[:10]
	}
	switch view {
	case TodoViewToday:
		today := now.In(store.TodoLocation()).Format("2006-01-02")
		return date != "" && date <= today, nil
	case TodoViewWeek:
		from, to, err := DueRange("week", now)
		if err != nil {
			return false, err
		}
		return date != "" && date >= from && date <= to, nil
	case TodoViewAll, TodoViewDone, "":
		return true, nil
	}
	return false, fmt.Errorf("%w: 非法视图 %q（today|week|all|done）", store.ErrInvalid, view)
}

// TodoOverdue 判断是否逾期：未完成且截止时刻早于 now。
func TodoOverdue(status string, dueAt *time.Time, now time.Time) bool {
	return dueAt != nil && (status == store.StatusTodo || status == store.StatusDoing) && dueAt.Before(now)
}
