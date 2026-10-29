package core

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/axu4/kuibu/internal/store"
)

// seFixture 搭一个「我是框架组 DE，兼 A 项目 SE；B 项目交给别人」的工作台。
type seFixture struct {
	ws       *store.Workspace
	now      time.Time
	me       *store.Person
	projectA store.ReportProject
	projectB store.ReportProject
	storage  GroupInfo
	frame    GroupInfo
	network  GroupInfo
}

func newSEFixture(t *testing.T) *seFixture {
	t.Helper()
	ws := newReportWorkspace(t)
	now := reportNow()
	me, err := ws.CreatePerson(store.NewPerson{Name: "勇旭", Group: "框架组", Role: store.RoleLead, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.CreatePerson(store.NewPerson{Name: "张三", Group: "框架组", Now: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.CreatePerson(store.NewPerson{Name: "老陈", Group: "存储组", Role: store.RoleLead, Now: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.CreatePerson(store.NewPerson{Name: "老李", Group: "网络组", Role: store.RoleLead, Now: now}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.AddReportProject("A 项目"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.AddReportProject("B 项目"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.SetReportProjectSE("A 项目", true, "", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.SetReportProjectSE("B 项目", false, "老王", nil); err != nil {
		t.Fatal(err)
	}
	projects, err := ws.ReportProjects()
	if err != nil {
		t.Fatal(err)
	}
	groups, err := ListGroups(ws, now)
	if err != nil {
		t.Fatal(err)
	}
	f := &seFixture{ws: ws, now: now, me: me, projectA: projects[0], projectB: projects[1]}
	for _, g := range groups {
		switch g.Name {
		case "框架组":
			f.frame = g
		case "存储组":
			f.storage = g
		case "网络组":
			f.network = g
		}
	}
	// 组配置的顺序取决于首次推导时的人员顺序，测试里显式指定应交组和顺序。
	projects, _, err = ws.SetReportProjectSE("A 项目", true, "", []string{f.frame.ID, f.storage.ID, f.network.ID})
	if err != nil {
		t.Fatal(err)
	}
	f.projectA = projects[0]
	return f
}

// groupReport 写一份框架组某项目的组报，final 时定稿。
func (f *seFixture) groupReport(t *testing.T, period string, project store.ReportProject, body string, final bool) {
	t.Helper()
	if _, err := f.ws.SaveGeneratedDraft(store.GeneratedDraft{
		Kind: store.GroupReportKind(project.ID), Group: "框架组", Person: f.me.ID, Name: f.me.Name,
		Week: period, Body: body, Now: f.now,
	}, true); err != nil {
		t.Fatal(err)
	}
	if final {
		if _, err := f.ws.FinalizeDraftKind(store.GroupReportKind(project.ID), period, f.me.ID, f.now); err != nil {
			t.Fatal(err)
		}
	}
}

func inboxStatus(view *ProjectInbox, group string) string {
	for _, e := range view.Entries {
		if e.Group == group {
			return e.Status
		}
	}
	return ""
}

func TestProjectInboxLinksOwnGroupAndCollectsPaste(t *testing.T) {
	f := newSEFixture(t)
	week := "2026-W44"

	view, err := LoadProjectInbox(f.ws, week, "A 项目", f.now)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Entries) != 3 || view.Counts["expected"] != 3 || view.Counts["received"] != 0 {
		t.Fatalf("初始收件 = %+v", view)
	}

	// 框架组组报还是草稿：不算交。
	f.groupReport(t, week, f.projectA, "## 本周总结\n- 双镜像升级提测\n## 下周计划\n- 问题收敛\n", false)
	view, _ = LoadProjectInbox(f.ws, week, "A 项目", f.now)
	if got := inboxStatus(view, "框架组"); got != InboxDraft {
		t.Fatalf("框架组未定稿状态 = %q", got)
	}

	f.groupReport(t, week, f.projectA, "## 本周总结\n- 双镜像升级提测\n## 下周计划\n- 问题收敛\n", true)
	body := "【周报】存储组\n本周：\n### 客户支持\n1、某局点掉盘问题闭环\n- 存储固件 3.2 发布\n下周：\n- 3.3 需求冻结\n其他：\n- 测试环境不足\n"
	if _, err := SubmitProjectInbox(f.ws, week, "A 项目", "存储组", body, f.now); err != nil {
		t.Fatal(err)
	}
	view, _ = LoadProjectInbox(f.ws, week, "A 项目", f.now)
	if inboxStatus(view, "框架组") != InboxLinked || inboxStatus(view, "存储组") != InboxSubmitted || inboxStatus(view, "网络组") != InboxMissing {
		t.Fatalf("收件状态 = %+v", view.Entries)
	}
	if view.Counts["received"] != 2 {
		t.Fatalf("received = %d", view.Counts["received"])
	}

	// 缺网络组：默认拒绝，allowMissing 才生成。
	if _, err := AssembleProjectReport(f.ws, week, "A 项目", false, false, f.now); !errors.Is(err, store.ErrNotFinal) {
		t.Fatalf("缺组时 err = %v", err)
	}
	d, err := AssembleProjectReport(f.ws, week, "A 项目", true, false, f.now)
	if err != nil {
		t.Fatal(err)
	}
	want := "> 未纳入：网络组（未交）\n\n" +
		"## 本周总结\n\n### 框架组\n\n- 双镜像升级提测\n\n### 存储组\n\n#### 客户支持\n1、某局点掉盘问题闭环\n- 存储固件 3.2 发布\n\n" +
		"## 下周计划\n\n### 框架组\n\n- 问题收敛\n\n### 存储组\n\n- 3.3 需求冻结\n\n" +
		"## 其他事项\n\n### 存储组\n\n- 测试环境不足\n"
	if d.Body != strings.TrimRight(want, "\n") && d.Body != want {
		t.Fatalf("项目报正文 =\n%s\n期望\n%s", d.Body, want)
	}
	if d.Kind != store.ProjectReportDirName || d.Person != f.projectA.ID || d.Name != "A 项目" {
		t.Fatalf("项目报元数据 = %+v", d)
	}
	if ProjectReportSourceChanged(f.ws, d, f.now) {
		t.Fatal("刚生成不应提示来源已更新")
	}
	if _, err := SubmitProjectInbox(f.ws, week, "A 项目", "存储组", body+"- 补一条\n", f.now); err != nil {
		t.Fatal(err)
	}
	d, _ = f.ws.ReadProjectReport(week, f.projectA.ID)
	if !ProjectReportSourceChanged(f.ws, d, f.now) {
		t.Fatal("重新粘贴后应提示来源已更新")
	}

	src, err := ReadProjectInboxSource(f.ws, week, "A 项目", "框架组", f.now)
	if err != nil || src.Source != InboxLinked || !strings.Contains(src.Body, "双镜像") {
		t.Fatalf("框架组来源 = %+v, %v", src, err)
	}
	if _, err := DeleteProjectInbox(f.ws, week, "A 项目", "存储组", f.now); err != nil {
		t.Fatal(err)
	}
	view, _ = LoadProjectInbox(f.ws, week, "A 项目", f.now)
	if inboxStatus(view, "存储组") != InboxMissing {
		t.Fatalf("删除后存储组 = %q", inboxStatus(view, "存储组"))
	}
}

func TestProjectInboxRequiresMineAndRespectsGroups(t *testing.T) {
	f := newSEFixture(t)
	if _, err := LoadProjectInbox(f.ws, "2026-W44", "B 项目", f.now); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("不是我的项目 err = %v", err)
	}
	if _, _, err := f.ws.SetReportProjectSE("A 项目", true, "", []string{f.frame.ID, f.storage.ID}); err != nil {
		t.Fatal(err)
	}
	view, err := LoadProjectInbox(f.ws, "2026-W44", "A 项目", f.now)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Entries) != 2 || view.Entries[0].Group != "框架组" || view.Entries[1].Group != "存储组" {
		t.Fatalf("按项目指定组 = %+v", view.Entries)
	}
}

func TestProjectInboxMonthlyAndLate(t *testing.T) {
	f := newSEFixture(t)
	month := "2026-10"
	late := time.Date(2026, 11, 2, 10, 0, 0, 0, store.TodoLocation())
	if _, err := SubmitProjectInbox(f.ws, month, "A 项目", "存储组", "## 本月总结\n- 3.2 发布\n## 下月计划\n- 3.3\n", late); err != nil {
		t.Fatal(err)
	}
	f.groupReport(t, month, f.projectA, "> 本期范围：W40–W44\n\n## 本月总结\n\n- 双镜像升级\n\n## 下月计划\n\n- 随 2.9 发布\n", true)
	view, err := LoadProjectInbox(f.ws, month, "A 项目", f.now)
	if err != nil {
		t.Fatal(err)
	}
	if !view.Monthly || inboxStatus(view, "存储组") != InboxLate || inboxStatus(view, "框架组") != InboxLinked {
		t.Fatalf("月报收件 = %+v", view.Entries)
	}
	d, err := AssembleProjectReport(f.ws, month, "A 项目", true, false, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.Body, "## 本月总结") || !strings.Contains(d.Body, "## 下月计划") || strings.Contains(d.Body, "本期范围") {
		t.Fatalf("月度项目报 =\n%s", d.Body)
	}
}

func TestMarkProjectGroupDelivered(t *testing.T) {
	f := newSEFixture(t)
	week := "2026-W44"
	f.groupReport(t, week, f.projectB, "## 本周总结\n- B 样机点亮\n", false)
	if _, err := MarkProjectGroupDelivered(f.ws, week, "框架组", "B 项目", true, f.now); !errors.Is(err, store.ErrNotFinal) {
		t.Fatalf("草稿标记已交 err = %v", err)
	}
	f.groupReport(t, week, f.projectB, "## 本周总结\n- B 样机点亮\n", true)
	d, err := MarkProjectGroupDelivered(f.ws, week, "框架组", "B 项目", true, f.now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	got := DeliveryOf(f.projectB, d)
	if got.Mine || got.SE != "老王" || got.DeliveredAt == nil || got.Stale {
		t.Fatalf("去向 = %+v", got)
	}
	d, _ = f.ws.ReadGroupReportProject(week, f.projectB.ID, f.me.ID)
	if d.DeliveredAt.IsZero() {
		t.Fatal("已交时间应落盘")
	}
	if _, err := MarkProjectGroupDelivered(f.ws, week, "框架组", "A 项目", true, f.now); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("我是 SE 的项目标记已交 err = %v", err)
	}
	// 撤回定稿后提示要重新交。
	d, err = f.ws.UnfinalizeDraftKind(store.GroupReportKind(f.projectB.ID), week, f.me.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !DeliveryOf(f.projectB, d).Stale {
		t.Fatal("撤回定稿后应提示重新交")
	}
	d, err = MarkProjectGroupDelivered(f.ws, week, "框架组", "B 项目", false, f.now)
	if err != nil || !d.DeliveredAt.IsZero() {
		t.Fatalf("撤销已交 = %+v, %v", d, err)
	}
}

func TestSplitProjectSectionsWholeBodyIsSummary(t *testing.T) {
	got := splitProjectSections("- 只写了几条\n- 没有小节\n")
	if len(got[projectSecSummary]) != 2 || len(got) != 1 {
		t.Fatalf("无小节 = %+v", got)
	}
	got = splitProjectSections("**本周总结**\n- a\n**下周计划：**\n- b\n")
	if got[projectSecSummary][0] != "- a" || got[projectSecPlan][0] != "- b" {
		t.Fatalf("加粗小节 = %+v", got)
	}
}

func TestReportLibraryIncludesInboxAndSE(t *testing.T) {
	f := newSEFixture(t)
	week := "2026-W44"
	f.groupReport(t, week, f.projectA, "## 本周总结\n- A 进展\n", true)
	if _, err := SubmitProjectInbox(f.ws, week, "A 项目", "存储组", "本周：\n- 存储进展\n", f.now); err != nil {
		t.Fatal(err)
	}
	if _, err := AssembleProjectReport(f.ws, week, "A 项目", true, false, f.now); err != nil {
		t.Fatal(err)
	}
	kinds := func() map[string]LibraryReport {
		list, err := ListReportLibrary(f.ws, week, "", "")
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]LibraryReport{}
		for _, r := range list {
			out[r.Kind+"/"+r.Group] = r
		}
		return out
	}
	got := kinds()
	if _, ok := got[LibraryKindGroup+"/框架组"]; !ok {
		t.Fatalf("缺本组定稿组报: %+v", got)
	}
	inbox, ok := got[LibraryKindInbox+"/存储组"]
	if !ok || inbox.Project != "A 项目" || inbox.LeadName != "老陈" || inbox.Count != 1 {
		t.Fatalf("其他 DE 交来的 = %+v", inbox)
	}
	if _, ok := got[LibraryKindSE+"/SE 汇总"]; ok {
		t.Fatal("项目报未定稿不应进周报库")
	}
	if _, err := f.ws.FinalizeDraftKind(store.ProjectReportDirName, week, f.projectA.ID, f.now); err != nil {
		t.Fatal(err)
	}
	if se, ok := kinds()[LibraryKindSE+"/SE 汇总"]; !ok || se.Project != "A 项目" {
		t.Fatalf("SE 定稿项目报 = %+v", se)
	}
	list, err := ListReportLibrary(f.ws, week, "存储组", "")
	if err != nil || len(list) != 1 || list[0].Kind != LibraryKindInbox {
		t.Fatalf("按组过滤 = %+v, %v", list, err)
	}
}

// 同一个人带两个组（自己的组 + 收各 DE 周报的组）：组报、筛选记录按组分开存，
// 旧版混在 personId 下的筛选记录挪到所属组，SE 两个组都能引用。
func TestOneLeadTwoGroupsKeepSeparateFiles(t *testing.T) {
	ws := newReportWorkspace(t)
	now := reportNow()
	week := "2026-W44"
	me, err := ws.CreatePerson(store.NewPerson{Name: "勇旭", Role: store.RoleLead, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.CreatePerson(store.NewPerson{Name: "张三", Group: "框架组", Now: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.CreatePerson(store.NewPerson{Name: "老陈", Group: "DE组", Now: now}); err != nil {
		t.Fatal(err)
	}
	if err := ws.SetReportGroups([]store.ReportGroup{
		{ID: "gframe", Name: "框架组", Lead: me.ID},
		{ID: "gde", Name: "DE组", Lead: me.ID},
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.AddReportProject("A 项目"); err != nil {
		t.Fatal(err)
	}
	projects, _, err := ws.SetReportProjectSE("A 项目", true, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	pa := projects[0]

	// 旧版：DE组 的筛选记录写在了 personId 下。
	if err := ws.WriteTriage(&store.Triage{Week: week, Group: "DE组", Lead: me.ID}); err != nil {
		t.Fatal(err)
	}
	frame, _, err := ResolveLead(ws, week, "框架组")
	if err != nil || frame.ID != me.ID || frame.Name != "勇旭" {
		t.Fatalf("框架组组长 = %+v, %v", frame, err)
	}
	de, _, err := ResolveLead(ws, week, "DE组")
	if err != nil || de.ID != me.ID+".gde" {
		t.Fatalf("DE组 存储键 = %+v, %v", de, err)
	}
	if tr, err := ws.ReadTriage(week, de.ID); err != nil || tr.Group != "DE组" {
		t.Fatalf("DE组 筛选记录应挪到自己的键: %+v, %v", tr, err)
	}
	if _, err := ws.ReadTriage(week, me.ID); err == nil {
		t.Fatal("personId 下不应再留 DE组 的筛选记录")
	}

	for _, g := range []struct {
		owner *store.Person
		group string
	}{{frame, "框架组"}, {de, "DE组"}} {
		if _, err := ws.SaveGeneratedDraft(store.GeneratedDraft{
			Kind: store.GroupReportKind(pa.ID), Group: g.group, Person: g.owner.ID, Name: g.owner.Name,
			Week: week, Body: "## 本周总结\n- " + g.group + " 进展\n", Now: now,
		}, false); err != nil {
			t.Fatal(err)
		}
		if _, err := ws.FinalizeDraftKind(store.GroupReportKind(pa.ID), week, g.owner.ID, now); err != nil {
			t.Fatal(err)
		}
	}
	view, err := LoadProjectInbox(ws, week, pa.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if inboxStatus(view, "框架组") != InboxLinked || inboxStatus(view, "DE组") != InboxLinked {
		t.Fatalf("两个组都应引用定稿: %+v", view.Entries)
	}
	d, err := AssembleProjectReport(ws, week, pa.ID, false, false, now)
	if err != nil || !strings.Contains(d.Body, "- 框架组 进展") || !strings.Contains(d.Body, "- DE组 进展") {
		t.Fatalf("项目报 = %v\n%s", err, d.Body)
	}
	lib, err := ListReportLibrary(ws, week, "DE组", "")
	if err != nil || len(lib) != 1 || lib[0].Kind != LibraryKindGroup {
		t.Fatalf("周报库 DE组 = %+v, %v", lib, err)
	}
}
