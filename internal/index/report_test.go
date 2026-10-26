package index

import (
	"context"
	"testing"
	"time"

	"github.com/axu4/kuibu/internal/store"
)

func TestIndexPeopleAndReports(t *testing.T) {
	ws := newWorkspace(t)
	now := time.Date(2026, 10, 29, 10, 0, 0, 0, store.TodoLocation())
	person, err := ws.CreatePerson(store.NewPerson{
		Name: "张三", Aliases: []string{"三儿"}, Group: "框架组",
		ProductLines: []string{"产品线A"}, Role: store.RoleMember, Now: now,
	})
	if err != nil {
		t.Fatalf("创建组员失败: %v", err)
	}
	if _, err := ws.WriteReport(store.NewReport{
		Person: person.ID, Name: person.Name, Week: "2026-W44", Level: store.ReportLevelMember,
		Group: "框架组", ProductLines: []string{"产品线A"}, Source: store.ReportSourceCLI,
		SubmittedAt: now, HeaderDate: "2026-10-29", Body: "本周完成核心库周报模块",
	}); err != nil {
		t.Fatalf("写入周报失败: %v", err)
	}

	idx := openIndex(t, ws)
	if _, err := idx.Rebuild(context.Background()); err != nil {
		t.Fatalf("重建索引失败: %v", err)
	}

	people, err := idx.ListPeople()
	if err != nil {
		t.Fatalf("ListPeople 失败: %v", err)
	}
	if len(people) != 1 || people[0].Name != "张三" {
		t.Errorf("组员索引 = %+v", people)
	}

	metas, err := idx.ListReports(ReportFilter{Week: "2026-W44"})
	if err != nil {
		t.Fatalf("ListReports 失败: %v", err)
	}
	if len(metas) != 1 || metas[0].Person != person.ID || metas[0].Group != "框架组" {
		t.Errorf("周报索引 = %+v", metas)
	}

	hits, err := idx.SearchReports("周报模块", 10)
	if err != nil {
		t.Fatalf("SearchReports 失败: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != metas[0].ID {
		t.Errorf("周报检索结果 = %+v", hits)
	}

	// 覆盖后重建：只索引当前版本，.history 不参与。
	if _, err := ws.WriteReport(store.NewReport{
		Person: person.ID, Name: person.Name, Week: "2026-W44", Level: store.ReportLevelMember,
		Group: "框架组", ProductLines: []string{"产品线A"}, Source: store.ReportSourceCLI,
		SubmittedAt: now.Add(time.Hour), HeaderDate: "2026-10-29", Body: "第二版内容",
	}); err != nil {
		t.Fatalf("覆盖写入失败: %v", err)
	}
	if _, err := idx.Rebuild(context.Background()); err != nil {
		t.Fatalf("覆盖后重建失败: %v", err)
	}
	metas, err = idx.ListReports(ReportFilter{Week: "2026-W44"})
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 {
		t.Fatalf("覆盖后周报数 = %d，期望 1（.history 不参与索引）", len(metas))
	}
	if hits, _ := idx.SearchReports("第一版", 10); len(hits) != 0 {
		t.Errorf("历史版本不应被检索到")
	}
}

func TestIndexSyncRemovesDeletedReport(t *testing.T) {
	ws := newWorkspace(t)
	now := time.Date(2026, 10, 29, 10, 0, 0, 0, store.TodoLocation())
	person, err := ws.CreatePerson(store.NewPerson{Name: "张三", Group: "框架组", Role: store.RoleMember, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.WriteReport(store.NewReport{
		Person: person.ID, Name: person.Name, Week: "2026-W44", Level: store.ReportLevelMember,
		Group: "框架组", Source: store.ReportSourceCLI, SubmittedAt: now, Body: "正文",
	}); err != nil {
		t.Fatal(err)
	}

	idx := openIndex(t, ws)
	if _, err := idx.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if metas, _ := idx.ListReports(ReportFilter{}); len(metas) != 1 {
		t.Fatalf("同步后周报数 = %d", len(metas))
	}

	if err := ws.DeleteReport("2026-W44", store.ReportLevelMember, person.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if metas, _ := idx.ListReports(ReportFilter{}); len(metas) != 0 {
		t.Fatalf("删除后周报数 = %d，期望 0", len(metas))
	}
}
