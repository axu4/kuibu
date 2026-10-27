package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/axu4/kuibu/internal/llm"
	"github.com/axu4/kuibu/internal/store"
)

func TestReportDraftFlow(t *testing.T) {
	reports, _, events := newTestReports(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"## 本周完成\n- 【Kuibu】完成核心库\n## 下周计划\n- 无"}}]}`))
	}))
	defer srv.Close()
	cfgDir := t.TempDir()
	reports.env = func(k string) string {
		if k == "KUIBU_CONFIG_DIR" {
			return cfgDir
		}
		return ""
	}
	var cfg llm.Config
	if err := cfg.Upsert(llm.Model{Name: "fake", Provider: llm.ProviderOpenAI, BaseURL: srv.URL, Model: "m", APIKey: "k"}, true); err != nil {
		t.Fatal(err)
	}
	if err := llm.Save(reports.env, cfg); err != nil {
		t.Fatal(err)
	}

	zhang, err := reports.CreatePerson(PersonInput{Name: "张三", Group: "框架组", ProductLines: []string{"Kuibu"}, Role: store.RoleMember})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reports.Submit([]SubmitItem{{Person: zhang.ID, Week: "2026-W44", Level: store.ReportLevelMember, Text: "完成核心库"}}); err != nil {
		t.Fatal(err)
	}

	d, err := reports.GetDraft("2026-W44", zhang.ID)
	if err != nil || d.Exists {
		t.Fatalf("未生成时 = %+v, %v", d, err)
	}

	// 预览不发请求。
	plan, err := reports.PrepareNormalize("2026-W44", nil, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&calls) != 0 || len(plan.Persons) != 1 || !strings.Contains(plan.Preview.Text, "完成核心库") {
		t.Fatalf("plan = %+v calls=%d", plan, calls)
	}

	events.reset()
	res, err := reports.Normalize("2026-W44", plan.Persons, "", false)
	if err != nil || len(res) != 1 || res[0].Error != "" {
		t.Fatalf("Normalize = %+v, %v", res, err)
	}
	if len(events.names) == 0 {
		t.Fatal("生成后应发 report:changed")
	}

	if _, err := reports.SaveDraft("2026-W44", zhang.ID, "## 本周完成\n- 【Kuibu】完成核心库（已改）"); err != nil {
		t.Fatal(err)
	}
	d, err = reports.FinalizeDraft("2026-W44", zhang.ID)
	if err != nil || d.Status != "final" || !d.HumanEdited {
		t.Fatalf("定稿 = %+v, %v", d, err)
	}
	if _, err := reports.SaveDraft("2026-W44", zhang.ID, "再改"); err == nil {
		t.Fatal("定稿后直接修改应失败")
	}

	st, err := reports.Status("2026-W44", store.ReportLevelMember, "", "")
	if err != nil || st.Entries[0].Normalized != "final" {
		t.Fatalf("状态应显示已定稿: %+v, %v", st.Entries, err)
	}

	// 定稿后批量预览会跳过他。
	plan, err = reports.PrepareNormalize("2026-W44", nil, "", false)
	if err != nil || len(plan.Persons) != 0 || len(plan.Skipped) != 1 {
		t.Fatalf("应跳过已定稿: %+v, %v", plan, err)
	}
}
