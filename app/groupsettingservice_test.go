package main

import (
	"testing"

	"github.com/axu4/kuibu/internal/store"
)

func TestGroupAndLibraryService(t *testing.T) {
	reports, workspace, _ := newTestReports(t)
	lead, err := reports.CreatePerson(PersonInput{Name: "组长", Role: store.RoleLead})
	if err != nil {
		t.Fatal(err)
	}
	zhang, err := reports.CreatePerson(PersonInput{Name: "张三", Group: "框架组", Role: store.RoleMember})
	if err != nil {
		t.Fatal(err)
	}
	ws := workspace.workspace()

	groups, err := reports.GroupList()
	if err != nil || len(groups) != 1 || groups[0].Name != "框架组" {
		t.Fatalf("GroupList = %+v %v", groups, err)
	}
	if groups[0].LeadID != lead.ID || len(groups[0].Members) != 1 || groups[0].Members[0].ID != zhang.ID {
		t.Fatalf("组信息 = %+v", groups[0])
	}

	created, err := reports.AddGroup("新组", lead.ID)
	if err != nil {
		t.Fatal(err)
	}
	var newID string
	for _, g := range created {
		if g.Name == "新组" {
			newID = g.ID
		}
	}
	if newID == "" {
		t.Fatalf("新建组失败: %+v", created)
	}
	updated, err := reports.UpdateGroup(newID, "新组", lead.ID, []string{zhang.ID})
	if err != nil {
		t.Fatal(err)
	}
	var moved bool
	for _, g := range updated {
		if g.ID == newID {
			for _, m := range g.Members {
				if m.ID == zhang.ID {
					moved = true
				}
			}
		}
	}
	if !moved {
		t.Fatalf("成员没有移动到新组: %+v", updated)
	}
	if _, err := reports.DeleteGroup(newID); err == nil {
		t.Fatal("有成员的组删除应失败")
	}
	// 清空成员后删除。
	if _, err := reports.UpdateGroup(newID, "新组", lead.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := reports.DeleteGroup(newID); err != nil {
		t.Fatal(err)
	}

	// 周报库只列已定稿组报。
	if _, _, err := ws.AddReportProject("云平台项目"); err != nil {
		t.Fatal(err)
	}
	projects, _ := ws.ReportProjects()
	if _, err := ws.SaveGeneratedDraft(store.GeneratedDraft{
		Kind: store.GroupReportKind(projects[0].ID), Person: lead.ID, Name: lead.Name,
		Week: "2026-W44", Group: "框架组", Body: "## 本周总结\n- 完成退款\n",
	}, false); err != nil {
		t.Fatal(err)
	}
	if lib, err := reports.ReportLibrary("2026-W44", "", ""); err != nil || len(lib) != 0 {
		t.Fatalf("未定稿不应进库: %+v %v", lib, err)
	}
	if _, err := ws.FinalizeDraftKind(store.GroupReportKind(projects[0].ID), "2026-W44", lead.ID, reports.clock()); err != nil {
		t.Fatal(err)
	}
	lib, err := reports.ReportLibrary("2026-W44", "", "")
	if err != nil || len(lib) != 1 || lib[0].Group != "框架组" || lib[0].Project != "云平台项目" {
		t.Fatalf("周报库 = %+v %v", lib, err)
	}
}
