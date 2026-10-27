package main

import (
	"strings"
	"testing"

	"github.com/axu4/kuibu/internal/store"
)

func TestMonthService(t *testing.T) {
	reports, _, _ := newTestReports(t)
	m, err := reports.Month("this")
	if err != nil || m.Month != "2026-10" || m.CollectDay != 28 || !m.IsCurrent || len(m.Weeks) != 5 {
		t.Fatalf("Month = %+v, %v", m, err)
	}
	if m, err = reports.ShiftMonth("2026-01", -1); err != nil || m.Month != "2025-12" {
		t.Fatalf("ShiftMonth = %+v, %v", m, err)
	}
	if day, err := reports.SetReportMonthDay(25); err != nil || day != 25 {
		t.Fatalf("SetReportMonthDay = %d, %v", day, err)
	}

	zhao, err := reports.CreatePerson(PersonInput{Name: "赵六", Group: "平台组", Role: store.RoleLead})
	if err != nil {
		t.Fatal(err)
	}
	prev, err := reports.Preview("【周报】赵六 2026-10-20\n本月网关上线", store.ReportLevelLeadMonth, "2026-10", "")
	if err != nil || len(prev) != 1 || prev[0].Week != "2026-10" || prev[0].Level != store.ReportLevelLeadMonth || prev[0].PersonID != zhao.ID {
		t.Fatalf("Preview = %+v, %v", prev, err)
	}
	if _, err := reports.Submit([]SubmitItem{{Person: zhao.ID, Week: "2026-10", Level: store.ReportLevelLeadMonth, Text: prev[0].Text}}); err != nil {
		t.Fatal(err)
	}
	st, err := reports.Status("2026-10", store.ReportLevelLeadMonth, "", "")
	// 固定时钟 2026-10-26 晚于收集日 25 日，算迟交。
	if err != nil || st.Counts.Late != 1 {
		t.Fatalf("组长月报状态 = %+v, %v", st.Counts, err)
	}
	r, err := reports.Get("2026-10", zhao.ID, store.ReportLevelLeadMonth)
	if err != nil || !strings.Contains(r.Body, "网关") {
		t.Fatalf("Get = %+v, %v", r, err)
	}
	g, err := reports.MonthReport("2026-10", "product")
	if err != nil || g.Exists {
		t.Fatalf("MonthReport = %+v, %v", g, err)
	}
	if _, err := reports.PrepareMonth("2026-10", "product", false, false); err == nil {
		t.Fatal("没有任何定稿时应报错")
	}
}
