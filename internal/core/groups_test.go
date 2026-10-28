package core

import (
	"errors"
	"testing"

	"github.com/axu4/kuibu/internal/store"
)

func TestGroupCRUDAndLibrary(t *testing.T) {
	ws := newReportWorkspace(t)
	now := reportNow()
	lead, err := ws.CreatePerson(store.NewPerson{Name: "组长", Role: store.RoleLead, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	zhang, err := ws.CreatePerson(store.NewPerson{Name: "张三", Group: "框架组", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	li, err := ws.CreatePerson(store.NewPerson{Name: "李四", Group: "数据组", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	groups, err := EnsureGroups(ws, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("组 = %+v", groups)
	}
	list, err := ListGroups(ws, now)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListGroups = %+v %v", list, err)
	}
	var frame GroupInfo
	for _, g := range list {
		if g.Name == "框架组" {
			frame = g
		}
	}
	if frame.ID == "" || len(frame.Members) != 1 || frame.Members[0].ID != zhang.ID {
		t.Fatalf("框架组 = %+v", frame)
	}
	if frame.LeadID != lead.ID {
		t.Fatalf("空组组长应推导为该组组长: %+v", frame)
	}

	renamed, err := RenameGroup(ws, frame.ID, "平台组", now)
	if err != nil || renamed.Name != "平台组" {
		t.Fatalf("改名 = %+v %v", renamed, err)
	}
	p, _ := ws.GetPerson(zhang.ID)
	if currentGroup(p, now) != "平台组" {
		t.Fatalf("组员当前组 = %q", currentGroup(p, now))
	}

	created, err := AddGroup(ws, "新组", lead.ID, now)
	if err != nil || created.ID == "" {
		t.Fatalf("新建组 = %+v %v", created, err)
	}
	updated, err := UpdateGroup(ws, created.ID, "新组", lead.ID, []string{li.ID}, now)
	if err != nil {
		t.Fatalf("更新成员失败: %v", err)
	}
	if updated.Name != "新组" {
		t.Fatalf("更新组 = %+v", updated)
	}
	li2, _ := ws.GetPerson(li.ID)
	if currentGroup(li2, now) != "新组" {
		t.Fatalf("李四当前组 = %q", currentGroup(li2, now))
	}

	if _, err := DeleteGroup(ws, created.ID, now); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("有成员的组删除应报错，实际 %v", err)
	}
	if _, err := UpdateGroup(ws, created.ID, "新组", lead.ID, nil, now); err != nil {
		t.Fatal(err)
	}
	if _, err := DeleteGroup(ws, created.ID, now); err != nil {
		t.Fatalf("空组删除失败: %v", err)
	}

	// 周报库：一个组成稿后能跨组列出，只列已定稿。
	if _, _, err := ws.AddReportProject("云平台项目"); err != nil {
		t.Fatal(err)
	}
	projects, _ := ws.ReportProjects()
	body := "## 本周总结\n- 完成事项\n"
	if _, err := ws.SaveGeneratedDraft(store.GeneratedDraft{
		Kind: store.GroupReportKind(projects[0].ID), Person: lead.ID, Name: lead.Name,
		Week: "2026-W44", Group: "平台组", Body: body, Now: now,
	}, true); err != nil {
		t.Fatal(err)
	}
	d, err := ws.ReadDraftKind(store.GroupReportKind(projects[0].ID), "2026-W44", lead.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.FinalizeDraftKind(d.Kind, "2026-W44", lead.ID, now); err != nil {
		t.Fatal(err)
	}
	lib, err := ListReportLibrary(ws, "2026-W44", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(lib) != 1 || lib[0].Group != "平台组" || lib[0].Project != "云平台项目" || lib[0].Count != 1 {
		t.Fatalf("周报库 = %+v", lib)
	}
	if lib[0].LeadName != lead.Name || lib[0].FinalizedAt == "" {
		t.Fatalf("周报库元数据 = %+v", lib[0])
	}
}
