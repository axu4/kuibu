package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/axu4/kuibu/internal/core"
	"github.com/axu4/kuibu/internal/store"
)

func TestSEServiceWeeklyFlow(t *testing.T) {
	f := newMonthFixture(t, "A 项目", "B 项目")
	if _, err := f.reports.CreatePerson(PersonInput{Name: "老陈", Group: "存储组", Role: store.RoleLead}); err != nil {
		t.Fatal(err)
	}
	triage := &TriageService{workspace: f.reports.workspace, emit: func(string, any) {}}
	if _, err := triage.SetProjectSE("A 项目", true, "", nil); err != nil {
		t.Fatal(err)
	}
	list, err := triage.SetProjectSE("B 项目", false, "老王", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !list[0].Mine || list[1].SE != "老王" {
		t.Fatalf("项目 SE = %+v", list)
	}
	mine, err := f.reports.SEProjects()
	if err != nil || len(mine) != 1 {
		t.Fatalf("我当 SE 的项目 = %+v, %v", mine, err)
	}
	pa, pb := f.projects[0].ID, f.projects[1].ID
	week := "2026-W44"
	f.weekReport(pa, week, "## 本周总结\n- A 进展\n", true)
	f.weekReport(pb, week, "## 本周总结\n- B 进展\n", true)

	g, err := f.reports.MarkProjectDelivered(week, "框架组", pb, true)
	if err != nil || g.Delivery == nil || g.Delivery.DeliveredAt == nil || g.Delivery.SE != "老王" {
		t.Fatalf("标记已交 = %+v, %v", g.Delivery, err)
	}
	rows, err := f.reports.ProjectGroups(week, "框架组")
	if err != nil || !rows[0].Delivery.Mine || rows[1].Delivery.DeliveredAt == nil {
		t.Fatalf("项目列表去向 = %+v, %v", rows, err)
	}

	inbox, err := f.reports.SEInbox(week, pa)
	if err != nil || inbox.Counts["received"] != 1 {
		t.Fatalf("收件 = %+v, %v", inbox, err)
	}
	if _, err := f.reports.SEGenerate(week, pa, false, false); !errors.Is(err, store.ErrNotFinal) {
		t.Fatalf("缺组生成 err = %v", err)
	}
	inbox, err = f.reports.SEInboxSubmit(week, pa, "存储组", "本周：\n- 存储进展\n")
	if err != nil || inbox.Counts["received"] != 2 {
		t.Fatalf("粘贴后收件 = %+v, %v", inbox, err)
	}
	src, err := f.reports.SEInboxSource(week, pa, "存储组")
	if err != nil || src.Source != "paste" {
		t.Fatalf("来源 = %+v, %v", src, err)
	}
	rep, err := f.reports.SEGenerate(week, pa, false, false)
	if err != nil || !strings.Contains(rep.Body, "### 存储组") || rep.ProjectName != "A 项目" {
		t.Fatalf("项目报 = %+v, %v", rep, err)
	}
	if _, err := f.reports.SEExportClipboard(week, pa); !errors.Is(err, store.ErrNotFinal) {
		t.Fatalf("未定稿复制 err = %v", err)
	}
	if rep, err = f.reports.SEFinalize(week, pa); err != nil || rep.Status != store.DraftStatusFinal {
		t.Fatalf("定稿 = %+v, %v", rep, err)
	}
	if content, err := f.reports.SEExportClipboard(week, pa); err != nil || !strings.Contains(content.Text, "存储进展") {
		t.Fatalf("复制 = %+v, %v", content, err)
	}
	if inbox, err = f.reports.SEInboxDelete(week, pa, "存储组"); err != nil || inbox.Counts[core.InboxMissing] != 1 {
		t.Fatalf("删除后 = %+v, %v", inbox, err)
	}
	if rep, err = f.reports.SEReport(week, pa); err != nil || !rep.SourceChanged {
		t.Fatalf("删除来源后应提示来源已更新: %+v, %v", rep, err)
	}
}
