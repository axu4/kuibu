package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axu4/kuibu/internal/store"
)

func newReportWorkspace(t *testing.T) *store.Workspace {
	t.Helper()
	ws, err := store.Init(filepath.Join(t.TempDir(), "ws"))
	if err != nil {
		t.Fatalf("初始化工作台失败: %v", err)
	}
	return ws
}

func reportNow() time.Time {
	return time.Date(2026, 10, 26, 10, 0, 0, 0, store.TodoLocation())
}

func TestParseBatchVariants(t *testing.T) {
	text := "【周报】张三 2026-11-01\n总结一\n\n【周报】李四 2026/11/1\n总结二\n"
	items := ParseBatch(text)
	if len(items) != 2 {
		t.Fatalf("份数 = %d，期望 2", len(items))
	}
	if items[0].Name != "张三" || items[0].Date != "2026-11-01" || items[0].StartLine != 1 || items[0].EndLine != 3 {
		t.Errorf("第 1 份 = %+v", items[0])
	}
	if !items[0].HasHeader || !strings.Contains(items[0].Text, "【周报】张三") {
		t.Errorf("第 1 份应保留头行: %+v", items[0])
	}
	if items[1].Name != "李四" || items[1].Date != "2026-11-01" {
		t.Errorf("第 2 份 = %+v", items[1])
	}

	// 变体：半角括号、全角空格、YYYY.MM.DD、YYYY-M-D。
	variants := []string{
		"[周报] 王五 2026.11.1\n内容",
		"【周报】\u3000赵六\u30002026-11-1\n内容",
	}
	for _, v := range variants {
		items := ParseBatch(v)
		if len(items) != 1 || items[0].Date != "2026-11-01" {
			t.Errorf("变体 %q 解析 = %+v", v, items)
		}
	}

	// 无头行：整段一份。
	items = ParseBatch("\n\n没有头行\n\n")
	if len(items) != 1 || items[0].HasHeader || items[0].Text != "没有头行" {
		t.Errorf("无头行解析 = %+v", items)
	}

	// 姓名与日期之间没有空白也要识别。
	items = ParseBatch("【周报】老张2026/10/28\n内容")
	if len(items) != 1 || items[0].Name != "老张" || items[0].Date != "2026-10-28" || !items[0].HasHeader {
		t.Errorf("无空白头行解析 = %+v", items[0])
	}

	// 日期非法。
	items = ParseBatch("【周报】张三 2026-02-30\n内容")
	if len(items) != 1 || !items[0].DateInvalid || items[0].Date != "" {
		t.Errorf("非法日期解析 = %+v", items[0])
	}
}

func TestPreviewReportsMatching(t *testing.T) {
	ws := newReportWorkspace(t)
	now := reportNow()

	p, err := ws.CreatePerson(store.NewPerson{
		Name: "张三", Aliases: []string{"小张"}, Group: "框架组", Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 姓名匹配。
	preview, err := PreviewReports(ws, PreviewInput{Text: "【周报】张三 2026-10-26\n内容", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview) != 1 || preview[0].PersonID != p.ID || preview[0].Week != "2026-W44" || preview[0].Reason != "" {
		t.Fatalf("姓名匹配预览 = %+v", preview)
	}

	// 别名匹配。
	preview, err = PreviewReports(ws, PreviewInput{Text: "【周报】小张 2026-10-26\n内容", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview) != 1 || preview[0].PersonID != p.ID || preview[0].Reason != "" {
		t.Fatalf("别名匹配预览 = %+v", preview)
	}

	// 姓名不匹配。
	preview, err = PreviewReports(ws, PreviewInput{Text: "【周报】王五 2026-10-26\n内容", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if preview[0].Reason != "姓名不匹配" {
		t.Errorf("不匹配原因 = %q，期望 姓名不匹配", preview[0].Reason)
	}

	// 歧义：直接写一个同名的在职人员文件（绕过唯一性校验）。
	if err := os.WriteFile(filepath.Join(ws.PeopleDir(), "dupe.md"), []byte("---\nname: \"张三\"\nactive: true\nrole: \"member\"\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	preview, err = PreviewReports(ws, PreviewInput{Text: "【周报】张三 2026-10-26\n内容", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if preview[0].Reason != "姓名有歧义" {
		t.Errorf("歧义原因 = %q，期望 姓名有歧义", preview[0].Reason)
	}
}

func TestPreviewReportsDuplicate(t *testing.T) {
	ws := newReportWorkspace(t)
	now := reportNow()

	if _, err := ws.CreatePerson(store.NewPerson{Name: "张三", Group: "框架组", Now: now}); err != nil {
		t.Fatal(err)
	}

	preview, err := PreviewReports(ws, PreviewInput{
		Text: "【周报】张三 2026-10-29\n第一份\n【周报】张三 2026/10/29\n第二份\n",
		Now:  now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview) != 2 {
		t.Fatalf("份数 = %d，期望 2", len(preview))
	}
	if !preview[0].Duplicate || preview[0].Reason == "" {
		t.Errorf("第 1 份应标记为重复: %+v", preview[0])
	}
	if preview[1].Duplicate {
		t.Errorf("第 2 份不应标记为重复: %+v", preview[1])
	}
}

func TestPreviewReportsStripsHeaderWhenPersonLocked(t *testing.T) {
	ws := newReportWorkspace(t)
	now := reportNow()

	p, err := ws.CreatePerson(store.NewPerson{Name: "张三", Group: "框架组", Now: now})
	if err != nil {
		t.Fatal(err)
	}

	// 锁定人员时，头行只是元信息，不并入正文。
	preview, err := PreviewReports(ws, PreviewInput{
		Text:   "【周报】张三 2026-10-26\n本周完成 A\n下周计划 B\n",
		Person: p.ID,
		Now:    now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview) != 1 {
		t.Fatalf("预览份数 = %d", len(preview))
	}
	if want := "本周完成 A\n下周计划 B"; preview[0].Text != want {
		t.Errorf("锁定人员后正文 = %q，期望 %q", preview[0].Text, want)
	}
	if !preview[0].HasHeader {
		t.Error("HasHeader 应保留，用来告诉用户识别到了头行")
	}

	// 不锁定人员时正文原样保留（头行是切分多份的依据）。
	preview, err = PreviewReports(ws, PreviewInput{
		Text: "【周报】张三 2026-10-26\n本周完成 A\n",
		Now:  now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := "【周报】张三 2026-10-26\n本周完成 A"; preview[0].Text != want {
		t.Errorf("未锁定时正文 = %q，期望 %q", preview[0].Text, want)
	}

	// 只有头行、没有正文时保留原样，不写入空内容。
	preview, err = PreviewReports(ws, PreviewInput{
		Text:   "【周报】张三 2026-10-26",
		Person: p.ID,
		Now:    now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if preview[0].Text != "【周报】张三 2026-10-26" {
		t.Errorf("只有头行时正文 = %q，应保留原样", preview[0].Text)
	}

	// 头行前有空行也能剥掉。
	preview, err = PreviewReports(ws, PreviewInput{
		Text:   "\n\n【周报】张三 2026/10/26\n本周完成 A",
		Person: p.ID,
		Now:    now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := "本周完成 A"; preview[0].Text != want {
		t.Errorf("头行前有空行时正文 = %q，期望 %q", preview[0].Text, want)
	}
}
