package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/axu4/kuibu/internal/llm"
	"github.com/axu4/kuibu/internal/store"
)

func TestGroupReportService(t *testing.T) {
	reports, _, _ := newTestReports(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"[{\"section\":\"本周完成\",\"productLine\":\"Kuibu\",\"category\":\"功能开发\",\"text\":\"完成核心库\",\"sources\":[\"张三\"]}]"}}]}`))
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

	if _, err := reports.CreatePerson(PersonInput{Name: "勇旭", Group: "框架组", Role: store.RoleLead}); err != nil {
		t.Fatal(err)
	}
	zhang, err := reports.CreatePerson(PersonInput{Name: "张三", Group: "框架组", ProductLines: []string{"Kuibu"}, Role: store.RoleMember})
	if err != nil {
		t.Fatal(err)
	}
	ws := reports.workspace.workspace()
	if _, err := ws.SaveGeneratedDraft(store.GeneratedDraft{Person: zhang.ID, Name: "张三", Week: "2026-W44", Body: "## 本周完成\n- 【Kuibu】完成核心库"}, true); err != nil {
		t.Fatal(err)
	}

	g, err := reports.GroupReport("2026-W44", "")
	if err != nil || g.Exists || g.LeadName != "勇旭" {
		t.Fatalf("未生成时 = %+v, %v", g, err)
	}
	if _, err := reports.PrepareGroup("2026-W44", "", false, false); !errors.Is(err, store.ErrNotFinal) {
		t.Fatalf("组员未定稿应 NotFinal, got %v", err)
	}
	if _, err := reports.FinalizeDraft("2026-W44", zhang.ID, ""); err != nil {
		t.Fatal(err)
	}
	p, err := reports.PrepareGroup("2026-W44", "", false, false)
	if err != nil || !strings.Contains(p.Preview.Text, "完成核心库") {
		t.Fatalf("PrepareGroup = %+v, %v", p, err)
	}
	g, err = reports.GenerateGroup("2026-W44", "", false, false)
	if err != nil || !strings.Contains(g.Body, "### Kuibu\n#### 功能开发\n- 完成核心库（张三）") {
		t.Fatalf("GenerateGroup = %+v, %v", g, err)
	}
	if _, err := reports.SubmitGroup("2026-W44", ""); !errors.Is(err, store.ErrNotFinal) {
		t.Fatalf("未定稿带入应 NotFinal, got %v", err)
	}
	if _, err := reports.FinalizeGroup("2026-W44", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := reports.SubmitGroup("2026-W44", ""); err != nil {
		t.Fatal(err)
	}
	g, _ = reports.GroupReport("2026-W44", "")
	if !g.Submitted {
		t.Fatal("带入后 Submitted 应为 true")
	}
	st, err := reports.Status("2026-W44", store.ReportLevelLead, "", "")
	if err != nil || st.Counts.Submitted != 1 {
		t.Fatalf("SE tab 应显示已交: %+v, %v", st.Counts, err)
	}

	tpl, err := reports.Template("group")
	if err != nil || !tpl.IsDefault {
		t.Fatalf("Template = %+v, %v", tpl, err)
	}
	if _, err := reports.SaveTemplate("group", "没有栏目"); err == nil {
		t.Fatal("没有栏目的模板应拒绝")
	}
}

func TestProductReportService(t *testing.T) {
	reports, _, _ := newTestReports(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"[{\"section\":\"本周完成\",\"productLine\":\"Kuibu\",\"category\":\"功能开发\",\"text\":\"完成网关\",\"sources\":[\"平台组\"]}]"}}]}`))
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
	zhao, err := reports.CreatePerson(PersonInput{Name: "赵六", Group: "平台组", ProductLines: []string{"Kuibu"}, Role: store.RoleLead})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reports.Submit([]SubmitItem{{Person: zhao.ID, Week: "2026-W44", Level: store.ReportLevelLead, Text: "网关完成"}}); err != nil {
		t.Fatal(err)
	}

	if _, err := reports.PrepareProduct("2026-W44", false, false); !errors.Is(err, store.ErrNotFinal) {
		t.Fatalf("组长未定稿应 NotFinal, got %v", err)
	}
	plan, err := reports.PrepareNormalize("2026-W44", store.ReportLevelLead, nil, "", false)
	if err != nil || len(plan.Persons) != 1 {
		t.Fatalf("组长归一预览 = %+v, %v", plan, err)
	}
	if _, err := reports.Normalize("2026-W44", store.ReportLevelLead, plan.Persons, "", false); err != nil {
		t.Fatal(err)
	}
	d, err := reports.GetDraft("2026-W44", zhao.ID, store.ReportLevelLead)
	if err != nil || !d.Exists || !strings.Contains(d.Body, "#### 功能开发") {
		t.Fatalf("组长归一稿 = %+v, %v", d, err)
	}
	if _, err := reports.FinalizeDraft("2026-W44", zhao.ID, store.ReportLevelLead); err != nil {
		t.Fatal(err)
	}
	st, _ := reports.Status("2026-W44", store.ReportLevelLead, "", "")
	if st.Entries[0].Normalized != "final" {
		t.Fatalf("SE 名单应显示已定稿: %+v", st.Entries[0])
	}

	g, err := reports.GenerateProduct("2026-W44", false, false)
	if err != nil || !strings.Contains(g.Body, "## Kuibu\n\n### 本周完成\n#### 功能开发\n- 完成网关（平台组）") {
		t.Fatalf("GenerateProduct = %+v, %v", g, err)
	}
	if _, err := reports.FinalizeProduct("2026-W44"); err != nil {
		t.Fatal(err)
	}
	if g, _ = reports.ProductReport("2026-W44"); g.Status != "final" || g.LeadName != "产品报" {
		t.Fatalf("ProductReport = %+v", g)
	}
}
