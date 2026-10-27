package core

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/axu4/kuibu/internal/store"
)

const prevBody = `## 本周完成
- 【A】旧事项
## 下周计划
- 【A】接口联调
- 【A】压测
- 【B】写文档
- 【B】重构缓存
## 风险与求助
- 无`

const curBody = `## 本周完成
- 【A】完成接口联调
- 【A】压测完成一半
- 【C】临时支援排障
## 下周计划
- 【A】上线
## 风险与求助
- 无`

func TestAlignmentFormula(t *testing.T) {
	cases := []struct {
		c    ReviewCounts
		want float64
		nil_ bool
	}{
		{ReviewCounts{Done: 2, Partial: 1, Delayed: 1, Total: 4}, 2.5 / 4, false},
		{ReviewCounts{Done: 1, Canceled: 1, Unmarked: 1, Total: 3}, 0.5, false},
		{ReviewCounts{Canceled: 2, Total: 2}, 0, true},
		{ReviewCounts{}, 0, true},
	}
	for _, c := range cases {
		got := AlignmentOf(c.c)
		if c.nil_ {
			if got != nil {
				t.Errorf("%+v 应为 nil, got %v", c.c, *got)
			}
			continue
		}
		if got == nil || *got != c.want {
			t.Errorf("%+v = %v, want %v", c.c, got, c.want)
		}
	}
}

func TestPrevWeekAcrossYear(t *testing.T) {
	for in, want := range map[string]string{"2026-W44": "2026-W43", "2026-W01": "2025-W52", "2021-W01": "2020-W53"} {
		got, err := PrevWeek(in)
		if err != nil || got != want {
			t.Errorf("PrevWeek(%s) = %s, %v; want %s", in, got, err, want)
		}
	}
}

func TestComputeReview(t *testing.T) {
	prev := ParseDraftItems(prevBody)[SectionPlan]
	done := ParseDraftItems(curBody)[SectionDone]
	cur := &store.Draft{Week: "2026-W44", Person: "p", Body: curBody, Review: &store.DraftReview{Items: []store.ReviewItem{
		{Plan: prev[0].ID, Status: store.ReviewDone, Matches: []string{done[0].ID}},
		{Plan: prev[1].ID, Status: store.ReviewPartial, Matches: []string{done[1].ID, "gone1234"}},
		{Plan: prev[2].ID, Status: store.ReviewCanceled},
	}}}
	res := ComputeReview(prevBody, cur)
	if len(res.Plans) != 4 || len(res.Done) != 3 || len(res.Next) != 1 {
		t.Fatalf("条目数 = %d/%d/%d", len(res.Plans), len(res.Done), len(res.Next))
	}
	if res.Counts != (ReviewCounts{Done: 1, Partial: 1, Canceled: 1, Unmarked: 1, Total: 4}) {
		t.Fatalf("counts = %+v", res.Counts)
	}
	// (1 + 0.5) / (4 - 1)
	if res.Alignment == nil || *res.Alignment != 0.5 {
		t.Fatalf("alignment = %v", res.Alignment)
	}
	if len(res.Plans[1].Matches) != 1 {
		t.Fatalf("失效的对应 id 应被丢掉: %v", res.Plans[1].Matches)
	}
	if res.Done[0].Unplanned || res.Done[1].Unplanned || !res.Done[2].Unplanned {
		t.Fatalf("计划外标记错误: %+v", res.Done)
	}
}

func TestAppendToSection(t *testing.T) {
	got := AppendToSection(curBody, SectionPlan, []string{"【A】压测（顺延）"})
	if !strings.Contains(got, "- 【A】上线\n- 【A】压测（顺延）\n## 风险与求助") {
		t.Fatalf("应追加在下周计划末尾:\n%s", got)
	}
	got = AppendToSection("## 本周完成\n- x\n## 下周计划\n- 无\n", SectionPlan, []string{"y"})
	if strings.Contains(got, "- 无") || !strings.HasSuffix(got, "## 下周计划\n- y") {
		t.Fatalf("应去掉「无」:\n%q", got)
	}
	got = AppendToSection("## 本周完成\n- x", SectionPlan, []string{"y"})
	if !strings.HasSuffix(got, "\n\n## 下周计划\n- y") {
		t.Fatalf("没有该节时应新建:\n%q", got)
	}
}

func TestReviewWorkflowAndCarry(t *testing.T) {
	ws, err := store.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 29, 10, 0, 0, 0, store.TodoLocation())
	save := func(week, body string) {
		t.Helper()
		if _, err := ws.SaveGeneratedDraft(store.GeneratedDraft{Person: "p", Name: "张三", Week: week, Body: body, Now: now}, true); err != nil {
			t.Fatal(err)
		}
	}
	save("2026-W43", prevBody)
	save("2026-W44", curBody)

	// 上周未定稿 → NOT_FINAL。
	if _, err := LoadReview(ws, "2026-W44", "p"); !errors.Is(err, store.ErrNotFinal) {
		t.Fatalf("上周未定稿应返回 ErrNotFinal, got %v", err)
	}
	if _, err := ws.FinalizeDraft("2026-W43", "p", now); err != nil {
		t.Fatal(err)
	}
	// 上周没有归一稿也算未定稿。
	if _, err := LoadReview(ws, "2026-W44", "q"); !errors.Is(err, store.ErrNotFinal) {
		t.Fatalf("上周没有归一稿应 NotFinal, got %v", err)
	}

	res, err := LoadReview(ws, "2026-W44", "p")
	if err != nil {
		t.Fatal(err)
	}
	plans, done := res.Plans, res.Done
	if _, err := SetReviewItem(ws, "2026-W44", "p", plans[0].ID, store.ReviewDone, []string{done[0].ID}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := SetReviewItem(ws, "2026-W44", "p", plans[1].ID, store.ReviewPartial, nil, "做了一半"); err != nil {
		t.Fatal(err)
	}
	if _, err := SetReviewItem(ws, "2026-W44", "p", plans[3].ID, store.ReviewDelayed, nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := SetReviewItem(ws, "2026-W44", "p", "nope", store.ReviewDone, nil, ""); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("未知计划 id 应报错, got %v", err)
	}

	// AI 建议不覆盖人工标过的条目，只填 plans[2]。
	sug := "```json\n[{\"plan\":\"" + plans[0].ID + "\",\"status\":\"delayed\",\"matches\":[]}," +
		"{\"plan\":\"" + plans[2].ID + "\",\"status\":\"canceled\",\"matches\":[\"bogus\"]}]\n```"
	res, applied, err := ApplySuggestion(ws, "2026-W44", "p", sug)
	if err != nil || applied != 1 {
		t.Fatalf("ApplySuggestion applied=%d err=%v", applied, err)
	}
	if res.Plans[0].Status != store.ReviewDone || res.Plans[2].Status != store.ReviewCanceled || res.Plans[2].Source != "ai" {
		t.Fatalf("建议应用结果 = %+v", res.Plans)
	}

	// 顺延：部分 + 延期两条，可重复执行。
	res, added, err := Carry(ws, "2026-W44", "p", nil, now)
	if err != nil || added != 2 {
		t.Fatalf("Carry added=%d err=%v", added, err)
	}
	if _, added, err = Carry(ws, "2026-W44", "p", nil, now); err != nil || added != 0 {
		t.Fatalf("重复顺延应为 0, added=%d err=%v", added, err)
	}
	cur, _ := ws.ReadDraft("2026-W44", "p")
	if strings.Count(cur.Body, "（顺延）") != 2 || !cur.HumanEdited {
		t.Fatalf("顺延后正文:\n%s", cur.Body)
	}
	// 顺延改的是正文，对照结果仍在：完成 1、部分 1、取消 1、延期 1 → (1+0.5)/3。
	res, err = LoadReview(ws, "2026-W44", "p")
	if err != nil || res.Alignment == nil || *res.Alignment != 0.5 || !res.Plans[1].Carried {
		t.Fatalf("顺延后对照 = %+v, %v", res, err)
	}

	// 本周定稿后顺延需先取消定稿。
	if _, err := ws.FinalizeDraft("2026-W44", "p", now); err != nil {
		t.Fatal(err)
	}
	if _, err := SetReviewItem(ws, "2026-W44", "p", plans[1].ID, store.ReviewDelayed, nil, ""); err != nil {
		t.Fatalf("定稿后仍可修改对照结果: %v", err)
	}
	d, _ := ws.ReadDraft("2026-W44", "p")
	if d.Status != store.DraftStatusFinal {
		t.Fatal("修改对照结果不应改变定稿状态")
	}
}
