package store

import (
	"errors"
	"testing"
)

func TestReportProjectsConfig(t *testing.T) {
	w := mustInit(t)

	projects, err := w.ReportProjects()
	if err != nil || len(projects) != 0 {
		t.Fatalf("默认项目 = %+v, err=%v", projects, err)
	}

	projects, p1, err := w.AddReportProject("云平台项目")
	if err != nil || len(projects) != 1 || p1.Name != "云平台项目" || p1.ID == "" {
		t.Fatalf("添加项目失败: %+v %+v %v", projects, p1, err)
	}
	if _, _, err := w.AddReportProject("云平台项目"); !errors.Is(err, ErrConflict) {
		t.Fatalf("重名应返回 ErrConflict，实际 %v", err)
	}
	projects, p2, err := w.AddReportProject("数据中台项目")
	if err != nil || len(projects) != 2 {
		t.Fatalf("添加第二个项目失败: %+v %v", projects, err)
	}

	// 重新打开后仍在。
	reopened, err := Open(w.Dir())
	if err != nil {
		t.Fatal(err)
	}
	projects, err = reopened.ReportProjects()
	if err != nil || len(projects) != 2 || projects[1].ID != p2.ID {
		t.Fatalf("重新打开后项目 = %+v, err=%v", projects, err)
	}

	projects, renamed, err := reopened.RenameReportProject(p1.ID, "云平台")
	if err != nil || renamed.Name != "云平台" || projects[0].ID != p1.ID {
		t.Fatalf("改名失败: %+v %+v %v", projects, renamed, err)
	}
	if _, _, err := reopened.RenameReportProject(p2.ID, "云平台"); !errors.Is(err, ErrConflict) {
		t.Fatalf("改成重名应返回 ErrConflict，实际 %v", err)
	}

	projects, removed, err := reopened.RemoveReportProject("数据中台项目")
	if err != nil || removed.ID != p2.ID || len(projects) != 1 {
		t.Fatalf("删除失败: %+v %+v %v", projects, removed, err)
	}
	if _, _, err := reopened.RemoveReportProject("不存在"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除不存在项目应返回 ErrNotFound，实际 %v", err)
	}

	// 最多 4 个。
	for i := 0; i < 3; i++ {
		if _, _, err := reopened.AddReportProject("项目" + string(rune('A'+i))); err != nil {
			t.Fatalf("添加第 %d 个失败: %v", i+2, err)
		}
	}
	if _, _, err := reopened.AddReportProject("第五个"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("超过 4 个应报错，实际 %v", err)
	}
}

func TestTriageFileRoundTrip(t *testing.T) {
	w := mustInit(t)
	projects, p1, err := w.AddReportProject("云平台项目")
	if err != nil || len(projects) != 1 {
		t.Fatal(err)
	}
	tr := &Triage{
		Week: "2026-W44", Group: "框架组", Lead: "20261026-100000-9b3a",
		SourceHash: "abc", Projects: []TriageProject{{ID: p1.ID, Name: p1.Name}},
		Items: []TriageItem{
			{ID: "a1b2c3d4", Person: "p", Name: "张三", Section: "本周完成", Domain: "支付", Text: "完成退款", Decision: TriageAccepted, Project: p1.ID, Order: 1},
			{ID: "e5f6a7b8", Person: "p", Name: "张三", Section: "下周计划", Text: "补用例", Order: 2},
		},
		Missing: []string{"王五"},
	}
	if err := w.WriteTriage(tr); err != nil {
		t.Fatal(err)
	}
	got, err := w.ReadTriage("2026-W44", tr.Lead)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 2 || got.Items[0].Decision != TriageAccepted || got.Items[1].Decision != "" {
		t.Fatalf("读回 = %+v", got)
	}
	if len(got.Missing) != 1 || got.Missing[0] != "王五" {
		t.Fatalf("missing = %+v", got.Missing)
	}
	if got.Items[0].Project != p1.ID {
		t.Fatalf("项目 = %q", got.Items[0].Project)
	}

	// 非法决策、未知项目要拒绝。
	bad := *got
	bad.Items = append([]TriageItem(nil), got.Items...)
	bad.Items[0].Decision = "unknown"
	if err := w.WriteTriage(&bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("非法决策应报错，实际 %v", err)
	}
	bad.Items[0].Decision = TriageAccepted
	bad.Items[0].Project = "missing"
	if err := w.WriteTriage(&bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("未知项目应报错，实际 %v", err)
	}

	// 正文不为空时读取应报错。
	path := w.TriagePath("2026-W44", tr.Lead)
	if err := writeFileAtomic(path, []byte("---\nweek: 2026-W44\n---\n正文\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ReadTriage("2026-W44", tr.Lead); err == nil {
		t.Fatal("正文非空应报错")
	}
}
