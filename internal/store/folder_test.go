package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mustCreateFolder 建一个目录节点，失败即终止测试。
func mustCreateFolder(t *testing.T, w *Workspace, title, parent string) *Note {
	t.Helper()
	n, err := w.Create(NewNote{Title: title, Parent: parent, Type: FolderType})
	if err != nil {
		t.Fatalf("创建目录 %q 失败: %v", title, err)
	}
	return n
}

func TestCreateFolder(t *testing.T) {
	w := mustInit(t)

	f, err := w.Create(NewNote{Title: "工作", Type: FolderType})
	if err != nil {
		t.Fatalf("Create 目录失败: %v", err)
	}
	if f.Type != FolderType {
		t.Errorf("Type = %q，期望 %q", f.Type, FolderType)
	}
	if f.Body != "" {
		t.Errorf("目录 Body = %q，期望空", f.Body)
	}
	got, err := w.Get(f.ID)
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if !got.IsFolder() || got.Title != "工作" {
		t.Errorf("落盘目录 = %+v，期望 type=folder title=工作", got)
	}

	// 目录下可以再建目录和笔记。
	sub := mustCreateFolder(t, w, "子目录", f.ID)
	note, err := w.Create(NewNote{Title: "笔记", Parent: sub.ID, Body: "正文"})
	if err != nil {
		t.Fatalf("目录下建笔记失败: %v", err)
	}
	if note.Parent != sub.ID {
		t.Errorf("Parent = %q，期望 %q", note.Parent, sub.ID)
	}

	roots, err := w.Tree()
	if err != nil {
		t.Fatalf("Tree 失败: %v", err)
	}
	if len(roots) != 1 || !roots[0].Note.IsFolder() {
		t.Fatalf("根节点 = %+v，期望 1 个目录根", roots)
	}
	if len(roots[0].Children) != 1 || !roots[0].Children[0].Note.IsFolder() {
		t.Fatalf("目录子节点不是目录")
	}
	if len(roots[0].Children[0].Children) != 1 || roots[0].Children[0].Children[0].Note.ID != note.ID {
		t.Errorf("笔记未挂在子目录下")
	}
}

func TestCreateFolderDefaultTitle(t *testing.T) {
	w := mustInit(t)
	f, err := w.Create(NewNote{Type: FolderType})
	if err != nil {
		t.Fatalf("Create 空标题目录失败: %v", err)
	}
	if f.Title != FolderTitle {
		t.Errorf("Title = %q，期望 %q", f.Title, FolderTitle)
	}
	got, err := w.Get(f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != FolderTitle {
		t.Errorf("落盘 Title = %q，期望 %q", got.Title, FolderTitle)
	}

	// 外部写的目录没有标题时读入也用默认目录名，而不是文件名。
	raw := "---\ntype: \"folder\"\nparent: \"\"\norder: 0\n---\n"
	if err := os.WriteFile(filepath.Join(w.NotesDir(), "raw-folder.md"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	raw2, err := w.Get("raw-folder")
	if err != nil {
		t.Fatal(err)
	}
	if raw2.Title != FolderTitle {
		t.Errorf("外部目录 Title = %q，期望 %q", raw2.Title, FolderTitle)
	}
}

func TestCreateFolderRejectsBody(t *testing.T) {
	w := mustInit(t)
	if _, err := w.Create(NewNote{Title: "带正文的目录", Type: FolderType, Body: "不该有正文"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("目录带正文错误 = %v，期望 ErrInvalid", err)
	}
	// 只有空白也视为空正文。
	if _, err := w.Create(NewNote{Title: "空目录", Type: FolderType, Body: "  \n\t"}); err != nil {
		t.Errorf("目录带空白正文应允许: %v", err)
	}
	metas, err := w.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 {
		t.Errorf("失败创建后笔记数 = %d，期望 1", len(metas))
	}
}

func TestCreateNoteUnderNoteRejected(t *testing.T) {
	w := mustInit(t)
	parent, err := w.Create(NewNote{Title: "普通笔记"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.Create(NewNote{Title: "子笔记", Parent: parent.ID})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("笔记下建子笔记错误 = %v，期望 ErrInvalid", err)
	}
	if !strings.Contains(err.Error(), "只有目录可以包含笔记") {
		t.Errorf("错误信息 %q 未说明「只有目录可以包含笔记」", err.Error())
	}
	metas, err := w.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 {
		t.Errorf("失败创建后笔记数 = %d，期望 1", len(metas))
	}

	// 父笔记不存在仍然是 ErrNotFound，不被新校验吞掉。
	if _, err := w.Create(NewNote{Title: "x", Parent: "nope"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("父不存在错误 = %v，期望 ErrNotFound", err)
	}
}

func TestUpdateFolderBody(t *testing.T) {
	w := mustInit(t)
	f := mustCreateFolder(t, w, "目录", "")

	// 改标题不受影响。
	renamed, err := w.Update(f.ID, func(n *Note) { n.Title = "新目录名" })
	if err != nil {
		t.Fatalf("改名失败: %v", err)
	}
	if renamed.Title != "新目录名" {
		t.Errorf("Title = %q", renamed.Title)
	}

	// 往目录写正文被拒绝。
	if _, err := w.UpdateBody(f.ID, "正文"); !errors.Is(err, ErrInvalid) {
		t.Errorf("目录写正文错误 = %v，期望 ErrInvalid", err)
	}
	got, err := w.Get(f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "" {
		t.Errorf("Body = %q，期望空", got.Body)
	}
}

func TestUpdateFolderWithChildrenCannotChangeType(t *testing.T) {
	w := mustInit(t)
	f := mustCreateFolder(t, w, "目录", "")
	if _, err := w.Create(NewNote{Title: "子笔记", Parent: f.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Update(f.ID, func(n *Note) { n.Type = "note" }); !errors.Is(err, ErrInvalid) {
		t.Errorf("有子节点的目录改类型错误 = %v，期望 ErrInvalid", err)
	}

	// 空目录可以改回普通笔记。
	empty := mustCreateFolder(t, w, "空目录", "")
	if _, err := w.Update(empty.ID, func(n *Note) { n.Type = "note" }); err != nil {
		t.Errorf("空目录改类型失败: %v", err)
	}
}

func TestMoveIntoNoteRejected(t *testing.T) {
	w := mustInit(t)
	note, err := w.Create(NewNote{Title: "笔记"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := w.Create(NewNote{Title: "另一条笔记"})
	if err != nil {
		t.Fatal(err)
	}

	// 拖进笔记里应被拒绝，位置不变。
	err = w.Move(other.ID, note.ID, 0)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("移入笔记错误 = %v，期望 ErrInvalid", err)
	}
	if !strings.Contains(err.Error(), "只有目录可以包含笔记") {
		t.Errorf("错误信息 %q 未说明「只有目录可以包含笔记」", err.Error())
	}
	got, err := w.Get(other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Parent != "" {
		t.Errorf("移动失败后 Parent = %q，期望空", got.Parent)
	}
}

func TestMoveBetweenFolders(t *testing.T) {
	w := mustInit(t)
	src := mustCreateFolder(t, w, "源目录", "")
	dst := mustCreateFolder(t, w, "目标目录", "")
	note, err := w.Create(NewNote{Title: "笔记", Parent: src.ID})
	if err != nil {
		t.Fatal(err)
	}

	if err := w.Move(note.ID, dst.ID, 0); err != nil {
		t.Fatalf("移动失败: %v", err)
	}
	if got := childIDs(t, w, src.ID); len(got) != 0 {
		t.Errorf("源目录子节点 = %v，期望空", got)
	}
	if got := childIDs(t, w, dst.ID); len(got) != 1 || got[0] != note.ID {
		t.Errorf("目标目录子节点 = %v，期望 [%s]", got, note.ID)
	}

	// 目录本身可以移到另一个目录下。
	if err := w.Move(src.ID, dst.ID, 0); err != nil {
		t.Fatalf("移动目录失败: %v", err)
	}
	// 移回根层级。
	if err := w.Move(src.ID, "", -1); err != nil {
		t.Fatalf("目录移回根层级失败: %v", err)
	}
}

// writeRawNote 直接写一个笔记文件，模拟老工作台里「有子节点但不是目录」的数据。
func writeRawNote(t *testing.T, w *Workspace, id, title, typ, parent, body string) {
	t.Helper()
	raw := "---\nid: \"" + id + "\"\ntitle: \"" + title + "\"\ntype: \"" + typ + "\"\n" +
		"created: \"2024-01-01T10:00:00+08:00\"\nupdated: \"2024-01-02T10:00:00+08:00\"\n" +
		"tags:\n  - 工作\nparent: \"" + parent + "\"\norder: 0\n---\n" + body
	if err := os.WriteFile(filepath.Join(w.NotesDir(), id+".md"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateFolders(t *testing.T) {
	w := mustInit(t)

	// 老数据：普通笔记带着两个子笔记，其中子笔记又带一个子笔记。
	writeRawNote(t, w, "old-folder", "老容器", "note", "", "# 老容器\n\n正文保留\n")
	writeRawNote(t, w, "child-a", "子 A", "note", "old-folder", "A 正文\n")
	writeRawNote(t, w, "child-b", "子 B", "note", "old-folder", "B 正文\n")
	writeRawNote(t, w, "grand", "孙", "note", "child-a", "孙正文\n")
	// 已经是目录的不动；叶子不动；inbox/meeting 类型的有子节点也要转。
	writeRawNote(t, w, "already", "已是目录", FolderType, "", "")
	writeRawNote(t, w, "leaf", "叶子笔记", "note", "", "叶子正文\n")
	writeRawNote(t, w, "inbox-old", "随手记", "inbox", "", "")
	writeRawNote(t, w, "inbox-item", "随手记条目", "note", "inbox-old", "随手记正文\n")
	writeRawNote(t, w, "meeting-folder", "周会", "note", "", "")
	writeRawNote(t, w, "meeting-1", "周会 2026-W44", "meeting", "meeting-folder", "## 议题\n")

	before, err := w.Tree()
	if err != nil {
		t.Fatalf("迁移前 Tree 失败: %v", err)
	}

	migrated, err := w.MigrateFolders()
	if err != nil {
		t.Fatalf("MigrateFolders 失败: %v", err)
	}
	// old-folder、child-a、inbox-old、meeting-folder 共 4 个。
	if migrated != 4 {
		t.Errorf("迁移数量 = %d，期望 4", migrated)
	}

	// 有子节点的节点全部变成目录，叶子与已是目录的类型保持不变。
	for _, id := range []string{"old-folder", "child-a", "inbox-old", "meeting-folder", "already"} {
		n, err := w.Get(id)
		if err != nil {
			t.Fatalf("Get %s 失败: %v", id, err)
		}
		if !n.IsFolder() {
			t.Errorf("%s 的 Type = %q，期望 %q", id, n.Type, FolderType)
		}
	}
	for _, id := range []string{"child-b", "grand", "leaf", "inbox-item"} {
		n, err := w.Get(id)
		if err != nil {
			t.Fatalf("Get %s 失败: %v", id, err)
		}
		if n.IsFolder() {
			t.Errorf("%s 是叶子，不应被转成目录", id)
		}
	}
	// meeting 笔记自身是叶子（没有子节点），保持原类型。
	if n, err := w.Get("meeting-1"); err != nil || n.Type != "meeting" {
		t.Errorf("周会笔记 Type = %v, %v，期望 meeting", n.Type, err)
	}

	// 标题、ID、顺序、正文、子节点都不变。
	old, err := w.Get("old-folder")
	if err != nil {
		t.Fatal(err)
	}
	if old.Title != "老容器" || old.Order != 0 {
		t.Errorf("迁移后 title/order = %q/%d，期望 老容器/0", old.Title, old.Order)
	}
	if !strings.Contains(old.Body, "正文保留") {
		t.Errorf("迁移后正文丢失: %q", old.Body)
	}
	// 迁移来的老目录带着正文时，改名等操作仍应可用。
	if _, err := w.Update("old-folder", func(n *Note) { n.Title = "改名后" }); err != nil {
		t.Errorf("迁移目录改名失败: %v", err)
	}

	after, err := w.Tree()
	if err != nil {
		t.Fatalf("迁移后 Tree 失败: %v", err)
	}
	if !sameTreeShape(before, after) {
		t.Errorf("迁移后树结构发生变化:\n before=%v\n after=%v", flattenTree(before), flattenTree(after))
	}

	// 幂等：再跑一次返回 0，文件不再变化。
	again, err := w.MigrateFolders()
	if err != nil {
		t.Fatalf("第二次 MigrateFolders 失败: %v", err)
	}
	if again != 0 {
		t.Errorf("第二次迁移数量 = %d，期望 0", again)
	}
	after2, err := w.Tree()
	if err != nil {
		t.Fatal(err)
	}
	if !sameTreeShape(after, after2) {
		t.Errorf("重复迁移改变了树结构")
	}
}

func TestMigrateFoldersEmptyWorkspace(t *testing.T) {
	w := mustInit(t)
	n, err := w.MigrateFolders()
	if err != nil {
		t.Fatalf("空工作台迁移失败: %v", err)
	}
	if n != 0 {
		t.Errorf("迁移数量 = %d，期望 0", n)
	}
}

// flattenTree 把树按「缩进 + ID」拍平，便于比较结构。
func flattenTree(roots []*TreeNode) []string {
	var out []string
	var walk func(nodes []*TreeNode, depth int)
	walk = func(nodes []*TreeNode, depth int) {
		for _, n := range nodes {
			out = append(out, strings.Repeat("  ", depth)+n.Note.ID)
			walk(n.Children, depth+1)
		}
	}
	walk(roots, 0)
	return out
}

// sameTreeShape 比较两棵树的层级形状（只看 ID 与嵌套关系，不看 type）。
func sameTreeShape(a, b []*TreeNode) bool {
	fa, fb := flattenTree(a), flattenTree(b)
	if len(fa) != len(fb) {
		return false
	}
	for i := range fa {
		if fa[i] != fb[i] {
			return false
		}
	}
	return true
}

func TestEnsureFolder(t *testing.T) {
	w := mustInit(t)

	// 已经是目录：原样返回，updated 不变。
	f := mustCreateFolder(t, w, "目录", "")
	got, err := w.EnsureFolder(f.ID)
	if err != nil {
		t.Fatalf("EnsureFolder 失败: %v", err)
	}
	if got.ID != f.ID || !got.IsFolder() || got.Title != "目录" {
		t.Errorf("EnsureFolder 改变了已有目录: %+v", got)
	}

	// 普通笔记：就地升级成目录，标题与正文不变。
	n, err := w.Create(NewNote{Title: "老容器", Body: "原有正文"})
	if err != nil {
		t.Fatal(err)
	}
	up, err := w.EnsureFolder(n.ID)
	if err != nil {
		t.Fatalf("升级笔记失败: %v", err)
	}
	if !up.IsFolder() || up.Title != "老容器" || up.Body != "原有正文" {
		t.Errorf("升级结果 = %+v，期望 folder/老容器/原有正文", up)
	}
	if _, err := w.Create(NewNote{Title: "子笔记", Parent: n.ID}); err != nil {
		t.Errorf("升级后应能建子笔记: %v", err)
	}

	// 不存在的 ID 返回 ErrNotFound。
	if _, err := w.EnsureFolder("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("EnsureFolder 不存在错误 = %v，期望 ErrNotFound", err)
	}
}
