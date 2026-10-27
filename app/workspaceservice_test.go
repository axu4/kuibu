package main

import (
	"errors"
	"testing"

	"github.com/axu4/kuibu/internal/store"
)

// newTestWorkspace 构造一个挂在临时工作台上的 WorkspaceService。
func newTestWorkspace(t *testing.T) *WorkspaceService {
	t.Helper()
	ws := newWorkspaceService()
	openTempWorkspace(t, ws)
	return ws
}

// findTreeItem 在树里按 ID 找节点。
func findTreeItem(items []TreeItem, id string) *TreeItem {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
		if found := findTreeItem(items[i].Children, id); found != nil {
			return found
		}
	}
	return nil
}

// 目录与笔记在树里靠 Type 区分，空目录也要带 type=folder。
func TestTreeItemsCarryType(t *testing.T) {
	s := newTestWorkspace(t)

	folder, err := s.CreateFolder("", "")
	if err != nil {
		t.Fatalf("CreateFolder 返回错误: %v", err)
	}
	if folder.Type != store.FolderType {
		t.Errorf("CreateFolder Type = %q，期望 %q", folder.Type, store.FolderType)
	}
	if folder.Title != store.FolderTitle {
		t.Errorf("空标题的目录 Title = %q，期望 %q", folder.Title, store.FolderTitle)
	}

	note, err := s.Create("", folder.ID)
	if err != nil {
		t.Fatalf("在目录下新建笔记失败: %v", err)
	}
	if note.Type != "note" {
		t.Errorf("普通笔记 Type = %q，期望 note", note.Type)
	}
	if note.Parent != folder.ID {
		t.Errorf("笔记 Parent = %q，期望 %q", note.Parent, folder.ID)
	}

	items, err := s.Tree()
	if err != nil {
		t.Fatalf("Tree 返回错误: %v", err)
	}
	root := findTreeItem(items, folder.ID)
	if root == nil {
		t.Fatalf("树里找不到目录 %s", folder.ID)
	}
	if root.Type != store.FolderType {
		t.Errorf("树里目录 Type = %q，期望 %q", root.Type, store.FolderType)
	}
	if len(root.Children) != 1 || root.Children[0].ID != note.ID {
		t.Fatalf("目录子节点 = %+v，期望只有 %s", root.Children, note.ID)
	}
	if root.Children[0].Type != "note" {
		t.Errorf("子笔记 Type = %q，期望 note", root.Children[0].Type)
	}
}

// 只有目录能包含子节点：往笔记下建笔记 / 建子目录都返回 ErrInvalid。
func TestCreateRejectsNoteAsParent(t *testing.T) {
	s := newTestWorkspace(t)

	note, err := s.Create("叶子笔记", "")
	if err != nil {
		t.Fatalf("Create 返回错误: %v", err)
	}
	if _, err := s.Create("", note.ID); !errors.Is(err, store.ErrInvalid) {
		t.Errorf("往笔记下建笔记错误 = %v，期望 ErrInvalid", err)
	}
	if _, err := s.CreateFolder("", note.ID); !errors.Is(err, store.ErrInvalid) {
		t.Errorf("往笔记下建目录错误 = %v，期望 ErrInvalid", err)
	}
}

// 目录没有正文，SaveBody 直接拒绝并给出中文提示。
func TestSaveBodyRejectsFolder(t *testing.T) {
	s := newTestWorkspace(t)

	folder, err := s.CreateFolder("资料", "")
	if err != nil {
		t.Fatalf("CreateFolder 返回错误: %v", err)
	}
	err = s.SaveBody(folder.ID, "不该写进来的正文")
	if !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("目录保存正文错误 = %v，期望 ErrInvalid", err)
	}
	if err == nil || err.Error() == "" {
		t.Error("目录保存正文应带中文错误信息")
	}
}

// 老工作台里「有子节点但不是目录」的笔记，打开时被迁移成目录。
func TestOpenMigratesLegacyParents(t *testing.T) {
	// 直接用 store 造老数据：普通笔记当父节点。
	dir := t.TempDir()
	w, err := store.Init(dir)
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	parent, err := w.Create(store.NewNote{Title: "老父节点"})
	if err != nil {
		t.Fatalf("Create 父笔记失败: %v", err)
	}
	// 绕过目录校验直接写子节点的 parent 字段，模拟老数据。
	if _, err := w.Update(parent.ID, func(n *store.Note) { n.Title = "老父节点" }); err != nil {
		t.Fatalf("Update 失败: %v", err)
	}
	child, err := w.Create(store.NewNote{Title: "子笔记"})
	if err != nil {
		t.Fatalf("Create 子笔记失败: %v", err)
	}
	child.Parent = parent.ID
	if _, err := w.Update(child.ID, func(n *store.Note) { n.Parent = parent.ID }); err != nil {
		t.Fatalf("改子笔记 parent 失败: %v", err)
	}

	s := newWorkspaceService()
	if err := s.open(dir); err != nil {
		t.Fatalf("打开工作台失败: %v", err)
	}
	t.Cleanup(s.close)

	got, err := s.Get(parent.ID)
	if err != nil {
		t.Fatalf("Get 父节点失败: %v", err)
	}
	if got.Type != store.FolderType {
		t.Errorf("打开后父节点 Type = %q，期望 %q", got.Type, store.FolderType)
	}
	childData, err := s.Get(child.ID)
	if err != nil {
		t.Fatalf("Get 子节点失败: %v", err)
	}
	if childData.Parent != parent.ID {
		t.Errorf("子节点 Parent = %q，期望 %q", childData.Parent, parent.ID)
	}
}
