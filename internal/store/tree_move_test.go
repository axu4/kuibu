package store

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var noteIDPattern = regexp.MustCompile(`^\d{8}-\d{6}-[0-9a-f]{4}$`)

// childIDs 返回 parent 下子笔记的 ID 列表，按 order 排序。
func childIDs(t *testing.T, w *Workspace, parent string) []string {
	t.Helper()
	metas, err := w.List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	children := childrenOf(metas, parent)
	ids := make([]string, len(children))
	for i, c := range children {
		ids[i] = c.ID
		if c.Order != i {
			t.Errorf("笔记 %s 的 order = %d，期望 %d", c.ID, c.Order, i)
		}
	}
	return ids
}

func TestMoveReorderSameParent(t *testing.T) {
	w := mustInit(t)
	var ids []string
	for i := 0; i < 3; i++ {
		n, err := w.Create(NewNote{Title: "n"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, n.ID)
	}

	// 把最后一个移到最前面。
	if err := w.Move(ids[2], "", 0); err != nil {
		t.Fatalf("Move 失败: %v", err)
	}
	got := childIDs(t, w, "")
	want := []string{ids[2], ids[0], ids[1]}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("顺序 = %v，期望 %v", got, want)
		}
	}

	// 移到末尾（超界按末尾处理）。
	if err := w.Move(ids[2], "", 99); err != nil {
		t.Fatalf("Move 到末尾失败: %v", err)
	}
	got = childIDs(t, w, "")
	want = []string{ids[0], ids[1], ids[2]}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("顺序 = %v，期望 %v", got, want)
		}
	}

	// position = -1 也表示末尾。
	if err := w.Move(ids[0], "", -1); err != nil {
		t.Fatalf("Move -1 失败: %v", err)
	}
	got = childIDs(t, w, "")
	want = []string{ids[1], ids[2], ids[0]}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("顺序 = %v，期望 %v", got, want)
		}
	}
}

func TestMoveAcrossParents(t *testing.T) {
	w := mustInit(t)
	root1, err := w.Create(NewNote{Title: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	root2, err := w.Create(NewNote{Title: "r2"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := w.Create(NewNote{Title: "a", Parent: root1.ID})
	if err != nil {
		t.Fatal(err)
	}
	b, err := w.Create(NewNote{Title: "b", Parent: root1.ID})
	if err != nil {
		t.Fatal(err)
	}
	c, err := w.Create(NewNote{Title: "c", Parent: root2.ID})
	if err != nil {
		t.Fatal(err)
	}

	// 把 b 移到 root2 的第 0 位。
	if err := w.Move(b.ID, root2.ID, 0); err != nil {
		t.Fatalf("Move 失败: %v", err)
	}

	if got := childIDs(t, w, root1.ID); len(got) != 1 || got[0] != a.ID {
		t.Errorf("root1 子节点 = %v，期望 [%s]", got, a.ID)
	}
	if got := childIDs(t, w, root2.ID); len(got) != 2 || got[0] != b.ID || got[1] != c.ID {
		t.Errorf("root2 子节点 = %v，期望 [%s %s]", got, b.ID, c.ID)
	}

	// 正文不受移动影响。
	n, err := w.Get(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n.Parent != root2.ID {
		t.Errorf("Parent = %q，期望 %q", n.Parent, root2.ID)
	}

	// 移回根层级末尾。
	if err := w.Move(b.ID, "", -1); err != nil {
		t.Fatalf("移回根层级失败: %v", err)
	}
	if got := childIDs(t, w, ""); len(got) != 3 || got[2] != b.ID {
		t.Errorf("根层级 = %v，期望 b 在末尾", got)
	}
}

func TestMoveCycleRejected(t *testing.T) {
	w := mustInit(t)
	root, err := w.Create(NewNote{Title: "root"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := w.Create(NewNote{Title: "child", Parent: root.ID})
	if err != nil {
		t.Fatal(err)
	}
	grand, err := w.Create(NewNote{Title: "grand", Parent: child.ID})
	if err != nil {
		t.Fatal(err)
	}

	if err := w.Move(root.ID, root.ID, 0); !errors.Is(err, ErrCycle) {
		t.Errorf("移到自己错误 = %v，期望 ErrCycle", err)
	}
	if err := w.Move(root.ID, child.ID, 0); !errors.Is(err, ErrCycle) {
		t.Errorf("移到子节点错误 = %v，期望 ErrCycle", err)
	}
	if err := w.Move(root.ID, grand.ID, 0); !errors.Is(err, ErrCycle) {
		t.Errorf("移到孙节点错误 = %v，期望 ErrCycle", err)
	}

	// 移动失败后结构不变。
	got, err := w.Get(root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Parent != "" {
		t.Errorf("root 的 Parent = %q，期望空", got.Parent)
	}
}

func TestMoveNotFound(t *testing.T) {
	w := mustInit(t)
	n, err := w.Create(NewNote{Title: "n"})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Move("nope", "", 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("移动不存在的笔记错误 = %v，期望 ErrNotFound", err)
	}
	if err := w.Move(n.ID, "nope", 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("移到不存在的父错误 = %v，期望 ErrNotFound", err)
	}
}

func TestMoveOnlyRewritesChangedOrders(t *testing.T) {
	w := mustInit(t)
	var ids []string
	for i := 0; i < 4; i++ {
		n, err := w.Create(NewNote{Title: "n"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, n.ID)
	}

	// 记录未变化文件的内容。
	unchanged := filepath.Join(w.NotesDir(), ids[0]+".md")
	before, err := os.ReadFile(unchanged)
	if err != nil {
		t.Fatal(err)
	}

	// 把最后一个移到最前，ids[0] 会变成 order 1。
	if err := w.Move(ids[3], "", 0); err != nil {
		t.Fatalf("Move 失败: %v", err)
	}

	// ids[0] 的 order 变了，内容应变化；而移动前的 ids[3] 文件必然变化。
	after, err := os.ReadFile(unchanged)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) == string(after) {
		t.Errorf("order 变化后文件未重写")
	}
	if !strings.Contains(string(after), "order: 1") {
		t.Errorf("order 未更新:\n%s", after)
	}
}

func TestDelete(t *testing.T) {
	w := mustInit(t)
	root, err := w.Create(NewNote{Title: "root"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := w.Create(NewNote{Title: "child", Parent: root.ID})
	if err != nil {
		t.Fatal(err)
	}
	grand, err := w.Create(NewNote{Title: "grand", Parent: child.ID})
	if err != nil {
		t.Fatal(err)
	}

	if err := w.Delete(root.ID, false); !errors.Is(err, ErrHasChildren) {
		t.Fatalf("非递归删除有子节点错误 = %v，期望 ErrHasChildren", err)
	}
	if _, err := w.Get(root.ID); err != nil {
		t.Fatalf("删除失败后 root 应仍存在: %v", err)
	}

	if err := w.Delete(root.ID, true); err != nil {
		t.Fatalf("递归删除失败: %v", err)
	}
	for _, id := range []string{root.ID, child.ID, grand.ID} {
		if _, err := w.Get(id); !errors.Is(err, ErrNotFound) {
			t.Errorf("笔记 %s 仍存在，错误 = %v", id, err)
		}
	}

	if err := w.Delete("nope", false); !errors.Is(err, ErrNotFound) {
		t.Errorf("删除不存在笔记错误 = %v，期望 ErrNotFound", err)
	}
}

func TestDeleteLeaf(t *testing.T) {
	w := mustInit(t)
	n, err := w.Create(NewNote{Title: "leaf"})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Delete(n.ID, false); err != nil {
		t.Fatalf("删除叶子失败: %v", err)
	}
	metas, err := w.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 0 {
		t.Errorf("删除后仍有 %d 条笔记", len(metas))
	}
}

func TestTree(t *testing.T) {
	w := mustInit(t)
	root1, err := w.Create(NewNote{Title: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	root2, err := w.Create(NewNote{Title: "r2"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := w.Create(NewNote{Title: "a", Parent: root1.ID})
	if err != nil {
		t.Fatal(err)
	}
	b, err := w.Create(NewNote{Title: "b", Parent: root1.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.Create(NewNote{Title: "a1", Parent: a.ID})
	if err != nil {
		t.Fatal(err)
	}

	// 调整顺序：b 在 a 之前。
	if err := w.Move(b.ID, root1.ID, 0); err != nil {
		t.Fatal(err)
	}

	roots, err := w.Tree()
	if err != nil {
		t.Fatalf("Tree 失败: %v", err)
	}
	if len(roots) != 2 {
		t.Fatalf("根节点数 = %d，期望 2", len(roots))
	}
	if roots[0].Note.ID != root1.ID || roots[1].Note.ID != root2.ID {
		t.Errorf("根节点顺序不对: %s, %s", roots[0].Note.ID, roots[1].Note.ID)
	}
	if len(roots[0].Children) != 2 {
		t.Fatalf("root1 子节点数 = %d，期望 2", len(roots[0].Children))
	}
	if roots[0].Children[0].Note.ID != b.ID || roots[0].Children[1].Note.ID != a.ID {
		t.Errorf("子节点顺序不对")
	}
	if len(roots[0].Children[1].Children) != 1 {
		t.Errorf("a 的子节点数 = %d，期望 1", len(roots[0].Children[1].Children))
	}
	if roots[0].Orphan || roots[0].Cycle {
		t.Errorf("正常节点不应标记 Orphan/Cycle")
	}
}

func TestTreeOrphanAndCycle(t *testing.T) {
	w := mustInit(t)

	// 手工写三个文件：孤儿、双节点环、指向环的节点。
	write := func(id, parent, title string) {
		t.Helper()
		raw := "---\nid: \"" + id + "\"\ntitle: \"" + title + "\"\ntype: \"note\"\n" +
			"created: \"2024-01-01T00:00:00+08:00\"\nupdated: \"2024-01-01T00:00:00+08:00\"\n" +
			"tags: []\nparent: \"" + parent + "\"\norder: 0\n---\n\n"
		if err := os.WriteFile(filepath.Join(w.NotesDir(), id+".md"), []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("orphan", "missing-parent", "孤儿")
	write("cycle-a", "cycle-b", "环 A")
	write("cycle-b", "cycle-a", "环 B")
	write("normal", "", "正常根")

	roots, err := w.Tree()
	if err != nil {
		t.Fatalf("Tree 失败: %v", err)
	}
	if len(roots) != 4 {
		t.Fatalf("根节点数 = %d，期望 4", len(roots))
	}

	byID := map[string]*TreeNode{}
	for _, r := range roots {
		byID[r.Note.ID] = r
		if len(r.Children) != 0 {
			t.Errorf("根节点 %s 不应有子节点", r.Note.ID)
		}
	}

	if n, ok := byID["orphan"]; !ok {
		t.Errorf("孤儿未作为根节点返回")
	} else if !n.Orphan {
		t.Errorf("孤儿未标记 Orphan")
	}

	for _, id := range []string{"cycle-a", "cycle-b"} {
		n, ok := byID[id]
		if !ok {
			t.Errorf("环上节点 %s 未作为根节点返回", id)
			continue
		}
		if !n.Cycle {
			t.Errorf("环上节点 %s 未标记 Cycle", id)
		}
	}

	if n, ok := byID["normal"]; !ok {
		t.Errorf("正常根节点缺失")
	} else if n.Orphan || n.Cycle {
		t.Errorf("正常根节点不应标记 Orphan/Cycle")
	}
}

func TestTreeSelfCycle(t *testing.T) {
	w := mustInit(t)
	raw := "---\nid: \"self\"\ntitle: \"自环\"\nparent: \"self\"\norder: 0\n---\n"
	if err := os.WriteFile(filepath.Join(w.NotesDir(), "self.md"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	roots, err := w.Tree()
	if err != nil {
		t.Fatalf("Tree 失败: %v", err)
	}
	if len(roots) != 1 {
		t.Fatalf("根节点数 = %d，期望 1", len(roots))
	}
	if !roots[0].Cycle {
		t.Errorf("自环节点未标记 Cycle")
	}
}
