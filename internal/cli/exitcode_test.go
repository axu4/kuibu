package cli

import "testing"

func TestExitCodeValues(t *testing.T) {
	cases := []struct {
		name string
		code int
		want int
	}{
		{"OK", OK, 0},
		{"ERROR", Error, 1},
		{"NOT_FINAL", NotFinal, 7},
		{"NEEDS_CONFIRM", NeedsConfirm, 8},
		{"HUMAN_EDITS_EXIST", HumanEditsExist, 9},
		{"NOT_OPEN", NotOpen, 10},
	}
	for _, c := range cases {
		if c.code != c.want {
			t.Errorf("%s = %d，期望 %d", c.name, c.code, c.want)
		}
	}
}

func TestExitCodeNames(t *testing.T) {
	cases := []struct {
		code int
		want string
	}{
		{OK, "OK"},
		{Error, "ERROR"},
		{NotFinal, "NOT_FINAL"},
		{NeedsConfirm, "NEEDS_CONFIRM"},
		{HumanEditsExist, "HUMAN_EDITS_EXIST"},
		{NotOpen, "NOT_OPEN"},
	}
	if len(ExitCodeNames) != len(cases) {
		t.Fatalf("ExitCodeNames 条目数 = %d，期望 %d", len(ExitCodeNames), len(cases))
	}
	for _, c := range cases {
		got, ok := ExitCodeNames[c.code]
		if !ok {
			t.Errorf("ExitCodeNames 缺少退出码 %d", c.code)
			continue
		}
		if got != c.want {
			t.Errorf("ExitCodeNames[%d] = %q，期望 %q", c.code, got, c.want)
		}
	}
}
