package index

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/axu4/kuibu/internal/store"
)

// frontMatterTyped 生成带指定 type 的笔记内容，用于目录节点。
func frontMatterTyped(id, title, typ, parent string, order int, body string) string {
	return "---\nid: \"" + id + "\"\ntitle: \"" + title + "\"\ntype: \"" + typ + "\"\n" +
		"created: \"2024-01-01T10:00:00+08:00\"\nupdated: \"2024-01-02T10:00:00+08:00\"\n" +
		"tags: []\nparent: \"" + parent + "\"\norder: " + itoa(order) + "\n---\n" + body
}

// TestIndexFolderType 确认目录（type=folder）在索引里当普通笔记处理：
// 类型原样保存、父子查询正常、检索不报错。
func TestIndexFolderType(t *testing.T) {
	ws := newWorkspace(t)
	idx := openIndex(t, ws)

	writeNoteFile(t, ws, "dir", frontMatterTyped("dir", "工作", store.FolderType, "", 0, ""))
	writeNoteFile(t, ws, "sub", frontMatterTyped("sub", "子目录", store.FolderType, "dir", 0, ""))
	writeNoteFile(t, ws, "note", frontMatterNote("note", "笔记", "sub", 0, "目录里的笔记正文\n"))

	stats, err := idx.Rebuild(context.Background())
	if err != nil {
		t.Fatalf("Rebuild 失败: %v", err)
	}
	if stats.Added != 3 || stats.Total != 3 {
		t.Fatalf("Stats = %+v，期望 added=3 total=3", stats)
	}

	dir, err := idx.Get("dir")
	if err != nil {
		t.Fatalf("Get 目录失败: %v", err)
	}
	if dir.Type != store.FolderType {
		t.Errorf("Type = %q，期望 %q", dir.Type, store.FolderType)
	}
	if dir.Title != "工作" {
		t.Errorf("Title = %q，期望 工作", dir.Title)
	}
	if dir.Path != filepath.Join(ws.NotesDir(), "dir.md") {
		t.Errorf("Path = %q", dir.Path)
	}

	// 目录的子节点：先是子目录，再是笔记，按 order/id 排序。
	kids, err := idx.Children("dir")
	if err != nil {
		t.Fatalf("Children 失败: %v", err)
	}
	if len(kids) != 1 || kids[0].ID != "sub" || kids[0].Type != store.FolderType {
		t.Fatalf("dir 子节点 = %+v，期望 [sub:folder]", kids)
	}

	// 空目录也要能被检索到（按标题），不会因 type 不是 note 出错。
	ids := searchIDs(t, idx, "工作", 10)
	if !contains(ids, "dir") {
		t.Errorf("检索「工作」未命中目录，结果 = %v", ids)
	}

	// 目录本身没有正文，检索正文不应命中它。
	if ids := searchIDs(t, idx, "目录里的笔记正文", 10); len(ids) != 1 || !contains(ids, "note") {
		t.Errorf("正文检索结果 = %v，期望只命中 note", ids)
	}
}

// TestIndexMigratedFolder 确认迁移后的目录能被索引重建正确读取。
func TestIndexMigratedFolder(t *testing.T) {
	ws := newWorkspace(t)
	idx := openIndex(t, ws)

	// 老数据：普通笔记带子节点。
	writeNoteFile(t, ws, "old", frontMatterNote("old", "老容器", "", 0, "老正文\n"))
	writeNoteFile(t, ws, "old-child", frontMatterNote("old-child", "子", "old", 0, "子正文\n"))

	if _, err := idx.Rebuild(context.Background()); err != nil {
		t.Fatalf("Rebuild 失败: %v", err)
	}
	if got, err := idx.Get("old"); err != nil || got.Type != "note" {
		t.Fatalf("迁移前 Type = %v, %v，期望 note", got.Type, err)
	}

	migrated, err := ws.MigrateFolders()
	if err != nil || migrated != 1 {
		t.Fatalf("MigrateFolders = %d, %v，期望 1", migrated, err)
	}

	// 索引是缓存：重新同步后类型跟着文件更新。
	if _, err := idx.Sync(context.Background()); err != nil {
		t.Fatalf("Sync 失败: %v", err)
	}
	got, err := idx.Get("old")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got.Type != store.FolderType {
		t.Errorf("迁移后索引 Type = %q，期望 %q", got.Type, store.FolderType)
	}
	if got.Title != "老容器" {
		t.Errorf("迁移后 Title = %q，期望 老容器", got.Title)
	}
	if kids, err := idx.Children("old"); err != nil || len(kids) != 1 {
		t.Errorf("迁移后 Children = %v, %v，期望 1 个子节点", kids, err)
	}
}
