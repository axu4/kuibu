package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReportWeekdayConfig(t *testing.T) {
	w := mustInit(t)

	n, err := w.ReportWeekday()
	if err != nil {
		t.Fatalf("ReportWeekday 失败: %v", err)
	}
	if n != DefaultReportWeekday {
		t.Errorf("默认周报日 = %d，期望 %d", n, DefaultReportWeekday)
	}

	if err := w.SetReportWeekday(3); err != nil {
		t.Fatalf("SetReportWeekday 失败: %v", err)
	}
	reopened, err := Open(w.Dir())
	if err != nil {
		t.Fatalf("重新打开工作台失败: %v", err)
	}
	if n, _ := reopened.ReportWeekday(); n != 3 {
		t.Errorf("重新打开后周报日 = %d，期望 3", n)
	}

	if err := w.SetReportWeekday(0); err == nil {
		t.Errorf("SetReportWeekday(0) 应报错")
	}
	if err := w.SetReportWeekday(8); err == nil {
		t.Errorf("SetReportWeekday(8) 应报错")
	}

	// 老工作台配置没有 reportWeekday 字段时按周四。
	old := mustInit(t)
	data, err := os.ReadFile(filepath.Join(old.MetaDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"version":1`) {
		t.Fatalf("config.json 缺少 version: %s", data)
	}
	if n, _ := old.ReportWeekday(); n != 4 {
		t.Errorf("缺省字段周报日 = %d，期望 4", n)
	}
}

func TestWriteReportOverwriteAndHistory(t *testing.T) {
	w := mustInit(t)
	person := mustPerson(t, w, "张三", "框架组", RoleMember)

	first := fixedShanghai(2026, 10, 29, 9, 0, 0)
	r1, err := w.WriteReport(NewReport{
		Person: person.ID, Name: person.Name, Week: "2026-W44", Level: ReportLevelMember,
		Group: person.Group, ProductLines: person.ProductLines, Source: ReportSourceCLI,
		SubmittedAt: first, HeaderDate: "2026-10-29", Body: "第一版\n",
	})
	if err != nil {
		t.Fatalf("第一次写入失败: %v", err)
	}
	if r1.Body != "第一版" {
		t.Errorf("正文首尾空行未去掉: %q", r1.Body)
	}

	second := fixedShanghai(2026, 10, 29, 10, 0, 0)
	if _, err := w.WriteReport(NewReport{
		Person: person.ID, Name: person.Name, Week: "2026-W44", Level: ReportLevelMember,
		Group: person.Group, ProductLines: person.ProductLines, Source: ReportSourceCLI,
		SubmittedAt: second, HeaderDate: "2026-10-29", Body: "第二版\n",
	}); err != nil {
		t.Fatalf("第二次写入失败: %v", err)
	}

	got, err := w.ReadReport("2026-W44", ReportLevelMember, person.ID)
	if err != nil {
		t.Fatalf("读取周报失败: %v", err)
	}
	if got.Body != "第二版" {
		t.Errorf("覆盖后正文 = %q，期望 第二版", got.Body)
	}

	historyDir := filepath.Join(w.ReportsDir(), "2026-W44", historyDirName, ReportLevelMember)
	entries, err := os.ReadDir(historyDir)
	if err != nil {
		t.Fatalf("读取历史目录失败: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("历史文件数 = %d，期望 1", len(entries))
	}
	if !strings.HasPrefix(entries[0].Name(), person.ID+"-") {
		t.Errorf("历史文件名 = %q，应以 %s- 开头", entries[0].Name(), person.ID)
	}
	data, err := os.ReadFile(filepath.Join(historyDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "第一版") {
		t.Errorf("历史文件应保留旧版本: %s", data)
	}
	if !strings.HasSuffix(string(data), "\n") || strings.HasSuffix(string(data), "\n\n") {
		t.Errorf("历史文件末尾应恰好一个换行: %q", string(data))
	}
}

func TestMovePersonMembershipAt(t *testing.T) {
	w := mustInit(t)
	now := fixedShanghai(2026, 10, 1, 10, 0, 0)
	p, err := w.CreatePerson(NewPerson{Name: "张三", Group: "A组", ProductLines: []string{"P1"}, Role: RoleMember, Now: now})
	if err != nil {
		t.Fatalf("创建组员失败: %v", err)
	}

	moveAt := fixedShanghai(2026, 11, 3, 10, 0, 0)
	moved, err := w.MovePerson(p.ID, "B组", nil, "2026-11-01", moveAt)
	if err != nil {
		t.Fatalf("转组失败: %v", err)
	}

	g, products := moved.MembershipAt(fixedShanghai(2026, 10, 26, 0, 0, 0))
	if g != "A组" || len(products) != 1 || products[0] != "P1" {
		t.Errorf("转组前归属 = %s %v，期望 A组 [P1]", g, products)
	}
	g, products = moved.MembershipAt(fixedShanghai(2026, 11, 2, 0, 0, 0))
	if g != "B组" || len(products) != 1 || products[0] != "P1" {
		t.Errorf("转组后归属 = %s %v，期望 B组 [P1]（产品线保持）", g, products)
	}

	// 重新读取后 groupHistory 应包含两条记录。
	got, err := w.GetPerson(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.GroupHistory) != 2 {
		t.Fatalf("groupHistory 条数 = %d，期望 2", len(got.GroupHistory))
	}
}

func TestSubmitReportSnapshotAfterMove(t *testing.T) {
	w := mustInit(t)
	now := fixedShanghai(2026, 10, 1, 10, 0, 0)
	p, err := w.CreatePerson(NewPerson{Name: "张三", Group: "A组", ProductLines: []string{"P1"}, Role: RoleMember, Now: now})
	if err != nil {
		t.Fatalf("创建组员失败: %v", err)
	}

	// 先提交 W44（周一是 2026-10-26，转组生效前）→ 归 A组。
	if _, err := w.SubmitReport(SubmitReportInput{
		Person: p.ID, Week: "2026-W44", Level: ReportLevelMember,
		HeaderDate: "2026-10-29", Source: ReportSourceCLI, Body: "W44 总结", Now: fixedShanghai(2026, 10, 29, 10, 0, 0),
	}); err != nil {
		t.Fatalf("提交 W44 失败: %v", err)
	}

	if _, err := w.MovePerson(p.ID, "B组", nil, "2026-11-01", fixedShanghai(2026, 11, 3, 10, 0, 0)); err != nil {
		t.Fatalf("转组失败: %v", err)
	}

	// 再提交 W45（周一是 2026-11-02，转组生效后）→ 归 B组。
	if _, err := w.SubmitReport(SubmitReportInput{
		Person: p.ID, Week: "2026-W45", Level: ReportLevelMember,
		HeaderDate: "2026-11-05", Source: ReportSourceCLI, Body: "W45 总结", Now: fixedShanghai(2026, 11, 5, 10, 0, 0),
	}); err != nil {
		t.Fatalf("提交 W45 失败: %v", err)
	}

	r44, err := w.ReadReport("2026-W44", ReportLevelMember, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r44.Group != "A组" {
		t.Errorf("W44 归属 = %q，期望 A组", r44.Group)
	}
	r45, err := w.ReadReport("2026-W45", ReportLevelMember, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r45.Group != "B组" {
		t.Errorf("W45 归属 = %q，期望 B组", r45.Group)
	}
}

func TestReportStatusCounts(t *testing.T) {
	w := mustInit(t)
	m1 := mustPerson(t, w, "张三", "G组", RoleMember)
	m2 := mustPerson(t, w, "李四", "G组", RoleMember)
	m3 := mustPerson(t, w, "王五", "G组", RoleMember)
	lead := mustPerson(t, w, "组长", "G组", RoleLead)

	write := func(p *Person, at time.Time) {
		t.Helper()
		if _, err := w.WriteReport(NewReport{
			Person: p.ID, Name: p.Name, Week: "2026-W44", Level: ReportLevelMember,
			Group: "G组", Source: ReportSourceCLI, SubmittedAt: at, HeaderDate: "2026-10-29", Body: "正文",
		}); err != nil {
			t.Fatalf("写入周报失败: %v", err)
		}
	}
	write(m1, fixedShanghai(2026, 10, 29, 23, 59, 0))  // 按时
	write(m2, fixedShanghai(2026, 10, 30, 0, 0, 0))    // 迟交
	write(lead, fixedShanghai(2026, 10, 29, 10, 0, 0)) // 组长交 member 层级 → 名单外

	st, err := w.ReportStatus("2026-W44", ReportLevelMember, "", "", fixedShanghai(2026, 10, 30, 1, 0, 0))
	if err != nil {
		t.Fatalf("ReportStatus 失败: %v", err)
	}
	if st.Counts["expected"] != 3 || st.Counts["submitted"] != 1 || st.Counts["late"] != 1 || st.Counts["missing"] != 1 || st.Counts["extra"] != 1 {
		t.Errorf("计数 = %v，期望 expected=3 submitted=1 late=1 missing=1 extra=1", st.Counts)
	}

	statusByPerson := map[string]string{}
	for _, e := range st.Entries {
		statusByPerson[e.Person] = e.Status
	}
	if statusByPerson[m1.ID] != "submitted" || statusByPerson[m2.ID] != "late" || statusByPerson[m3.ID] != "missing" {
		t.Errorf("状态 = %v", statusByPerson)
	}
	if len(st.Extra) != 1 || st.Extra[0].Person != lead.ID {
		t.Errorf("名单外 = %+v", st.Extra)
	}
}

func TestWriteReportFirstSubmittedAt(t *testing.T) {
	w := mustInit(t)
	person := mustPerson(t, w, "张三", "框架组", RoleMember)

	first := fixedShanghai(2026, 10, 29, 23, 59, 0)
	if _, err := w.WriteReport(NewReport{
		Person: person.ID, Name: person.Name, Week: "2026-W44", Level: ReportLevelMember,
		Group: person.Group, ProductLines: person.ProductLines, Source: ReportSourceCLI,
		SubmittedAt: first, HeaderDate: "2026-10-29", Body: "第一版",
	}); err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}

	second := fixedShanghai(2026, 10, 30, 10, 0, 0)
	if _, err := w.WriteReport(NewReport{
		Person: person.ID, Name: person.Name, Week: "2026-W44", Level: ReportLevelMember,
		Group: person.Group, ProductLines: person.ProductLines, Source: ReportSourceCLI,
		SubmittedAt: second, HeaderDate: "2026-10-29", Body: "第二版",
	}); err != nil {
		t.Fatalf("覆盖写入失败: %v", err)
	}

	r, err := w.ReadReport("2026-W44", ReportLevelMember, person.ID)
	if err != nil {
		t.Fatalf("读取周报失败: %v", err)
	}
	if !r.FirstSubmittedAt.Equal(first) {
		t.Errorf("firstSubmittedAt = %v，期望 %v", r.FirstSubmittedAt, first)
	}
	if !r.SubmittedAt.Equal(second) {
		t.Errorf("submittedAt = %v，期望 %v", r.SubmittedAt, second)
	}

	st, err := w.ReportStatus("2026-W44", ReportLevelMember, "", "", fixedShanghai(2026, 10, 30, 11, 0, 0))
	if err != nil {
		t.Fatalf("ReportStatus 失败: %v", err)
	}
	if len(st.Entries) != 1 {
		t.Fatalf("entries = %d，期望 1", len(st.Entries))
	}
	e := st.Entries[0]
	if e.Status != "submitted" {
		t.Errorf("status = %q，期望 submitted", e.Status)
	}
	if e.FirstSubmittedAt == nil || !e.FirstSubmittedAt.Equal(first) {
		t.Errorf("firstSubmittedAt = %v，期望 %v", e.FirstSubmittedAt, first)
	}
	if e.SubmittedAt == nil || !e.SubmittedAt.Equal(second) {
		t.Errorf("submittedAt = %v，期望 %v", e.SubmittedAt, second)
	}
}

func TestCompactTimeUsesLocalTimezone(t *testing.T) {
	// 2026-10-29 23:00 Asia/Shanghai == 2026-10-29 15:00 UTC。
	shanghai := fixedShanghai(2026, 10, 29, 23, 0, 0)
	if got := compactTime(shanghai); got != "20261029-230000" {
		t.Errorf("compactTime = %q，期望 20261029-230000", got)
	}
}

func mustPerson(t *testing.T, w *Workspace, name, group, role string) *Person {
	t.Helper()
	p, err := w.CreatePerson(NewPerson{
		Name: name, Group: group, Role: role,
		Now: fixedShanghai(2026, 10, 1, 10, 0, 0),
	})
	if err != nil {
		t.Fatalf("创建组员失败: %v", err)
	}
	return p
}
