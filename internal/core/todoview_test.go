package core

import (
	"testing"
	"time"

	"github.com/axu4/kuibu/internal/store"
)

func TestTodoViewHelpers(t *testing.T) {
	now := time.Date(2026, 10, 26, 10, 0, 0, 0, store.TodoLocation()) // 周一
	cases := []struct {
		view, due string
		want      bool
	}{
		{TodoViewToday, "2026-10-20", true},
		{TodoViewToday, "2026-10-26 18:00", true},
		{TodoViewToday, "2026-10-27", false},
		{TodoViewToday, "", false},
		{TodoViewWeek, "2026-11-01", true},
		{TodoViewWeek, "2026-11-02", false},
		{TodoViewWeek, "2026-10-25", false},
		{TodoViewAll, "", true},
		{TodoViewDone, "", true},
	}
	for _, c := range cases {
		got, err := InTodoView(c.view, c.due, now)
		if err != nil || got != c.want {
			t.Errorf("InTodoView(%s, %q) = %v, %v; want %v", c.view, c.due, got, err, c.want)
		}
	}
	if _, err := InTodoView("month", "", now); err == nil {
		t.Error("非法视图应报错")
	}
	past := now.Add(-time.Hour)
	if !TodoOverdue(store.StatusTodo, &past, now) || TodoOverdue(store.StatusDone, &past, now) || TodoOverdue(store.StatusTodo, nil, now) {
		t.Error("TodoOverdue 判断错误")
	}
	if got := TodoViewStatuses(TodoViewDone); len(got) != 2 || got[0] != store.StatusDone {
		t.Errorf("done 视图状态 = %v", got)
	}
}
