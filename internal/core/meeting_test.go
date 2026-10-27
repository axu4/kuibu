package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/axu4/kuibu/internal/store"
)

// writeLegacyNote 直接在工作台 notes/ 下写一个笔记文件，模拟老工作台的数据。
func writeLegacyNote(t *testing.T, ws *store.Workspace, id, title, typ, parent, extra, body string) {
	t.Helper()
	raw := "---\nid: \"" + id + "\"\ntitle: \"" + title + "\"\ntype: \"" + typ + "\"\n" +
		"created: \"2026-10-01T10:00:00+08:00\"\nupdated: \"2026-10-02T10:00:00+08:00\"\n" +
		"tags: []\nparent: \"" + parent + "\"\norder: 0\n" + extra + "---\n" + body
	if err := os.WriteFile(filepath.Join(ws.NotesDir(), id+".md"), []byte(raw), 0o644); err != nil {
		t.Fatalf("写入老笔记 %s 失败: %v", id, err)
	}
}

// TestOpenMeetingFolderIsFolder 确认「周会」容器是目录节点（只有目录能包含子节点），
// 周会笔记挂在它下面。
func TestOpenMeetingFolderIsFolder(t *testing.T) {
	ws := newReportWorkspace(t)

	meeting, created, err := OpenMeeting(ws, "2026-W44")
	if err != nil {
		t.Fatalf("OpenMeeting 失败: %v", err)
	}
	if !created {
		t.Fatalf("首次 OpenMeeting created = false，期望 true")
	}
	if meeting.Parent == "" {
		t.Fatalf("周会笔记没有父节点")
	}
	folder, err := ws.Get(meeting.Parent)
	if err != nil {
		t.Fatalf("读取周会容器失败: %v", err)
	}
	if !folder.IsFolder() {
		t.Errorf("周会容器 Type = %q，期望 %q", folder.Type, store.FolderType)
	}
	if folder.Title != MeetingFolderTitle {
		t.Errorf("周会容器标题 = %q，期望 %q", folder.Title, MeetingFolderTitle)
	}

	// 同一周再次打开返回同一条笔记，不重复建容器。
	again, createdAgain, err := OpenMeeting(ws, "2026-W44")
	if err != nil {
		t.Fatalf("再次 OpenMeeting 失败: %v", err)
	}
	if createdAgain || again.ID != meeting.ID {
		t.Errorf("再次打开得到 %q/%v，期望复用 %q", again.ID, createdAgain, meeting.ID)
	}
}

// TestOpenMeetingUpgradesLegacyFolder 覆盖老工作台：容器是普通笔记（type=note）
// 但已有周会子节点，OpenMeeting 会就地把它升级成目录，再往里加周会。
func TestOpenMeetingUpgradesLegacyFolder(t *testing.T) {
	ws := newReportWorkspace(t)

	writeLegacyNote(t, ws, "zhouhui", MeetingFolderTitle, "note", "", "",
		"## 议题\n\n## 决定\n\n## 待办\n- [ ] \n")
	writeLegacyNote(t, ws, "m44", "周会 2026-W44（10-26）", MeetingType, "zhouhui",
		"week: \"2026-W44\"\ndate: \"2026-10-26\"\n", "## 议题\n")

	// 老容器一开始不是目录。
	if folder, err := ws.Get("zhouhui"); err != nil || folder.IsFolder() {
		t.Fatalf("老容器 Type = %v, %v，期望 note", folder.Type, err)
	}

	// 老周会仍能按周打开。
	same, created, err := OpenMeeting(ws, "2026-W44")
	if err != nil {
		t.Fatalf("打开老周会失败: %v", err)
	}
	if created || same.ID != "m44" {
		t.Errorf("打开老周会得到 %q/%v，期望复用 m44", same.ID, created)
	}

	// 同一容器下开新一周：容器被升级成目录，新周会挂进去。
	next, created, err := OpenMeeting(ws, "2026-W45")
	if err != nil {
		t.Fatalf("新建周会失败: %v", err)
	}
	if !created || next.Parent != "zhouhui" {
		t.Fatalf("新周会 = %q/%v，期望新建并挂在 zhouhui 下", next.ID, created)
	}
	folder, err := ws.Get("zhouhui")
	if err != nil {
		t.Fatalf("读取容器失败: %v", err)
	}
	if !folder.IsFolder() {
		t.Errorf("老容器未升级成目录，Type = %q", folder.Type)
	}
	if folder.Title != MeetingFolderTitle {
		t.Errorf("容器标题 = %q，期望 %q", folder.Title, MeetingFolderTitle)
	}
	if _, err := ws.Create(store.NewNote{Title: "子笔记", Parent: "zhouhui"}); err != nil {
		t.Errorf("升级后应能建子笔记: %v", err)
	}
}
