package index

import (
	"context"
	"testing"
	"time"

	"github.com/axu4/kuibu/internal/store"
)

func TestIndexProjectGroupReports(t *testing.T) {
	ws := newWorkspace(t)
	now := time.Date(2026, 10, 29, 10, 0, 0, 0, store.TodoLocation())
	lead, err := ws.CreatePerson(store.NewPerson{Name: "组长", Group: "框架组", Role: store.RoleLead, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	projects, p1, err := ws.AddReportProject("云平台项目")
	if err != nil || len(projects) != 1 {
		t.Fatal(err)
	}
	if _, err := ws.SaveGeneratedDraft(store.GeneratedDraft{
		Kind: store.GroupReportKind(p1.ID), Person: lead.ID, Name: lead.Name,
		Week: "2026-W44", Group: "框架组", Body: "## 本周总结\n- 完成退款接口联调\n",
		SourceHash: "hash1", Now: now,
	}, false); err != nil {
		t.Fatal(err)
	}

	path := ws.GroupReportProjectPath("2026-W44", p1.ID, lead.ID)
	if r, err := readReportOrDraft(path); err != nil {
		t.Fatalf("readReportOrDraft: %v", err)
	} else if r.Level != store.GroupReportKind(p1.ID) {
		t.Fatalf("读取层级 = %q", r.Level)
	}
	idx := openIndex(t, ws)
	if _, err := idx.Rebuild(context.Background()); err != nil {
		t.Fatalf("重建索引失败: %v", err)
	}
	metas, err := idx.ListReports(ReportFilter{Week: "2026-W44", Level: store.GroupReportKind(p1.ID)})
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 {
		t.Fatalf("项目组报索引 = %+v", metas)
	}
	wantLevel := store.GroupReportKind(p1.ID)
	if metas[0].Level != wantLevel || metas[0].Person != lead.ID {
		t.Fatalf("索引项 = %+v，期望 level=%s", metas[0], wantLevel)
	}
	if metas[0].ID != "2026-W44/"+wantLevel+"/"+lead.ID {
		t.Fatalf("RID = %q", metas[0].ID)
	}
	hits, err := idx.SearchReports("退款接口", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != metas[0].ID {
		t.Fatalf("检索 = %+v", hits)
	}
}
