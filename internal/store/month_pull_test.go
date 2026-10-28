package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMonthPullRoundTrip(t *testing.T) {
	w := mustInit(t)
	p := &MonthPull{
		Month:     "2026-10",
		Group:     "框架组",
		Lead:      "lead-1",
		UpdatedAt: "2026-11-01T20:00:00+08:00",
		Weeks:     []string{"2026-W39", "2026-W40", "2026-W41", "2026-W45"},
		Sources: []MonthPullSource{
			{Week: "2026-W39", Project: "p1"},
			{Week: "2026-W40", Project: "p1"},
		},
		Overrides: []MonthPullOverride{
			{Project: "p1", Week: "2026-W40", Section: "summary", Item: "abcd1234", Pool: MonthPoolPlan},
		},
	}
	if err := w.WriteMonthPull(p); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	path := w.MonthPullPath("2026-10", "lead-1")
	if !strings.HasSuffix(path, filepath.Join("2026-10", "pull", "lead-1.md")) {
		t.Fatalf("路径 = %s", path)
	}
	got, err := w.ReadMonthPull("2026-10", "lead-1")
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if got.Month != "2026-10" || got.Group != "框架组" || len(got.Weeks) != 4 ||
		len(got.Sources) != 2 || len(got.Overrides) != 1 || got.Overrides[0].Pool != MonthPoolPlan {
		t.Fatalf("读回 = %+v", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "---\n") || strings.Contains(string(data), "\n# ") {
		t.Fatalf("文件应为纯 front matter:\n%s", data)
	}
}

func TestMonthPullValidation(t *testing.T) {
	w := mustInit(t)
	base := func() *MonthPull {
		return &MonthPull{
			Month: "2026-10", Group: "框架组", Lead: "lead-1",
			Weeks:   []string{"2026-W40"},
			Sources: []MonthPullSource{{Week: "2026-W40", Project: "p1"}},
		}
	}
	cases := []struct {
		name string
		edit func(*MonthPull)
	}{
		{"bad month", func(p *MonthPull) { p.Month = "2026-13" }},
		{"bad week", func(p *MonthPull) { p.Weeks = []string{"2026-W99"} }},
		{"source outside scope", func(p *MonthPull) { p.Sources = []MonthPullSource{{Week: "2026-W41", Project: "p1"}} }},
		{"missing project", func(p *MonthPull) { p.Sources = []MonthPullSource{{Week: "2026-W40"}} }},
		{"bad pool", func(p *MonthPull) {
			p.Overrides = []MonthPullOverride{{Project: "p1", Section: "summary", Item: "abcd1234", Pool: "bogus"}}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := base()
			c.edit(p)
			if err := w.WriteMonthPull(p); err == nil {
				t.Fatalf("期望校验失败: %+v", p)
			}
		})
	}
}

func TestMonthPullRejectsBody(t *testing.T) {
	w := mustInit(t)
	path := w.MonthPullPath("2026-10", "lead-1")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nmonth: 2026-10\ngroup: 框架组\nlead: lead-1\nweeks: [2026-W40]\nsources: []\n---\n正文不该有\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ReadMonthPull("2026-10", "lead-1"); err == nil || !strings.Contains(err.Error(), "正文应为空") {
		t.Fatalf("错误 = %v", err)
	}
	if _, err := w.ReadMonthPull("2026-10", "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在错误 = %v", err)
	}
}
