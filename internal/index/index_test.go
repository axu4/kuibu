package index

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/axu4/kuibu/internal/store"
)

// newWorkspace 初始化一个临时工作台。
func newWorkspace(t *testing.T) *store.Workspace {
	t.Helper()
	ws, err := store.Init(filepath.Join(t.TempDir(), "ws"))
	if err != nil {
		t.Fatalf("初始化工作台失败: %v", err)
	}
	return ws
}

// openIndex 打开工作台索引，测试结束时自动关闭。
func openIndex(t *testing.T, ws *store.Workspace) *Index {
	t.Helper()
	idx, err := Open(ws)
	if err != nil {
		t.Fatalf("打开索引失败: %v", err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	return idx
}

// writeNoteFile 直接在工作台 notes/ 下写一个笔记文件，模拟外部编辑器。
func writeNoteFile(t *testing.T, ws *store.Workspace, id, content string) string {
	t.Helper()
	path := filepath.Join(ws.NotesDir(), id+".md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写入笔记文件失败: %v", err)
	}
	return path
}

// frontMatterNote 生成带 front matter 的笔记内容。
func frontMatterNote(id, title, parent string, order int, body string) string {
	return "---\nid: \"" + id + "\"\ntitle: \"" + title + "\"\ntype: \"note\"\n" +
		"created: \"2024-01-01T10:00:00+08:00\"\nupdated: \"2024-01-02T10:00:00+08:00\"\n" +
		"tags:\n  - 工作\nparent: \"" + parent + "\"\norder: " + itoa(order) + "\n---\n" + body
}

// itoa 把整数转成字符串。
func itoa(n int) string { return strconv.Itoa(n) }

// openRaw 直接打开索引数据库，供测试修改 PRAGMA。
func openRaw(path string) (*sql.DB, error) {
	return sql.Open("sqlite", dsn(path))
}

// searchIDs 返回搜索结果中的 ID 列表。
func searchIDs(t *testing.T, idx *Index, query string, limit int) []string {
	t.Helper()
	hits, err := idx.Search(query, limit)
	if err != nil {
		t.Fatalf("检索 %q 失败: %v", query, err)
	}
	ids := make([]string, len(hits))
	for i, h := range hits {
		ids[i] = h.ID
	}
	return ids
}

// contains 判断切片是否包含目标字符串。
func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestRebuildAndQueries(t *testing.T) {
	ws := newWorkspace(t)
	idx := openIndex(t, ws)

	writeNoteFile(t, ws, "root", frontMatterNote("root", "根笔记", "", 0, "跬步笔记全文检索，支持中文子串。\n"))
	writeNoteFile(t, ws, "child-b", frontMatterNote("child-b", "子笔记 B", "root", 1, "普通内容\n"))
	writeNoteFile(t, ws, "child-a", frontMatterNote("child-a", "子笔记 A", "root", 0, "另一个内容\n"))
	writeNoteFile(t, ws, "plain", "# 无 front matter\n\n只有正文\n")

	stats, err := idx.Rebuild(context.Background())
	if err != nil {
		t.Fatalf("Rebuild 失败: %v", err)
	}
	if stats.Added != 4 || stats.Total != 4 || stats.Updated != 0 || stats.Removed != 0 {
		t.Fatalf("Stats = %+v，期望 added=4 total=4", stats)
	}

	metas, err := idx.List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(metas) != 4 {
		t.Fatalf("List 数量 = %d，期望 4", len(metas))
	}

	// List 顺序：先按 parent（"" 在最前），再 order，再 id。
	rootChildren, err := idx.Children("root")
	if err != nil {
		t.Fatalf("Children 失败: %v", err)
	}
	if len(rootChildren) != 2 {
		t.Fatalf("root 子节点数 = %d，期望 2", len(rootChildren))
	}
	if rootChildren[0].ID != "child-a" || rootChildren[1].ID != "child-b" {
		t.Errorf("子节点顺序 = %s,%s，期望 child-a,child-b", rootChildren[0].ID, rootChildren[1].ID)
	}
	if rootChildren[0].Order != 0 || rootChildren[1].Order != 1 {
		t.Errorf("子节点 order = %d,%d", rootChildren[0].Order, rootChildren[1].Order)
	}

	root, err := idx.Get("root")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if root.Title != "根笔记" || root.Type != "note" {
		t.Errorf("Get 结果 = %+v", root)
	}
	if len(root.Tags) != 1 || root.Tags[0] != "工作" {
		t.Errorf("Tags = %v，期望 [工作]", root.Tags)
	}
	if root.Created.IsZero() || root.Updated.IsZero() {
		t.Errorf("时间字段为空: created=%v updated=%v", root.Created, root.Updated)
	}
	if root.Path != filepath.Join(ws.NotesDir(), "root.md") {
		t.Errorf("Path = %q", root.Path)
	}
	if root.Size == 0 {
		t.Errorf("Size 应大于 0")
	}

	// 中文子串检索：三字及以上走 trigram，两字走 LIKE 回退。
	for _, q := range []string{"全文检索", "全文检", "跬步"} {
		ids := searchIDs(t, idx, q, 10)
		if !contains(ids, "root") {
			t.Errorf("检索 %q 未命中 root，结果 = %v", q, ids)
		}
	}

	hits, err := idx.Search("全文检索", 10)
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("Search 结果为空")
	}
	if !strings.Contains(hits[0].Snippet, "全文检索") {
		t.Errorf("Snippet = %q，期望包含命中词", hits[0].Snippet)
	}
	if !strings.Contains(hits[0].Snippet, "[") || !strings.Contains(hits[0].Snippet, "]") {
		t.Errorf("Snippet = %q，期望带高亮标记", hits[0].Snippet)
	}

	// 标题命中也要能搜到。
	ids := searchIDs(t, idx, "根笔记", 10)
	if !contains(ids, "root") {
		t.Errorf("标题检索未命中 root，结果 = %v", ids)
	}

	// 不存在的词返回空结果。
	if ids := searchIDs(t, idx, "不存在的关键词", 10); len(ids) != 0 {
		t.Errorf("不存在的关键词命中 %v", ids)
	}

	// limit 生效。
	if ids := searchIDs(t, idx, "内容", 1); len(ids) != 1 {
		t.Errorf("limit=1 返回 %d 条", len(ids))
	}

	// 特殊字符不会让 FTS 查询报错。
	for _, q := range []string{`a"b`, "foo-bar", "a:b", "*"} {
		if _, err := idx.Search(q, 5); err != nil {
			t.Errorf("检索 %q 失败: %v", q, err)
		}
	}
}

func TestSyncStats(t *testing.T) {
	ws := newWorkspace(t)
	idx := openIndex(t, ws)

	writeNoteFile(t, ws, "n1", frontMatterNote("n1", "笔记一", "", 0, "初始正文\n"))
	if _, err := idx.Rebuild(context.Background()); err != nil {
		t.Fatalf("Rebuild 失败: %v", err)
	}

	// 没有变化时 Sync 统计全为 0。
	stats, err := idx.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync 失败: %v", err)
	}
	if stats.Added != 0 || stats.Updated != 0 || stats.Removed != 0 || stats.Total != 1 {
		t.Fatalf("无变化 Sync = %+v", stats)
	}

	// 外部新增文件。
	writeNoteFile(t, ws, "n2", frontMatterNote("n2", "笔记二", "", 1, "新增正文\n"))
	stats, err = idx.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync 失败: %v", err)
	}
	if stats.Added != 1 || stats.Updated != 0 || stats.Removed != 0 || stats.Total != 2 {
		t.Fatalf("新增后 Sync = %+v，期望 added=1 total=2", stats)
	}

	// 外部修改文件（内容长度变化，确保 size 也变）。
	writeNoteFile(t, ws, "n1", frontMatterNote("n1", "笔记一（改）", "", 0, "修改后的更长正文内容\n"))
	stats, err = idx.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync 失败: %v", err)
	}
	if stats.Added != 0 || stats.Updated != 1 || stats.Removed != 0 || stats.Total != 2 {
		t.Fatalf("修改后 Sync = %+v，期望 updated=1", stats)
	}
	meta, err := idx.Get("n1")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if meta.Title != "笔记一（改）" {
		t.Errorf("修改后 Title = %q", meta.Title)
	}
	if ids := searchIDs(t, idx, "修改后的更长正文", 10); !contains(ids, "n1") {
		t.Errorf("修改后全文索引未更新，结果 = %v", ids)
	}

	// 外部删除文件。
	if err := os.Remove(filepath.Join(ws.NotesDir(), "n2.md")); err != nil {
		t.Fatal(err)
	}
	stats, err = idx.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync 失败: %v", err)
	}
	if stats.Added != 0 || stats.Updated != 0 || stats.Removed != 1 || stats.Total != 1 {
		t.Fatalf("删除后 Sync = %+v，期望 removed=1 total=1", stats)
	}
	if _, err := idx.Get("n2"); !errors.Is(err, ErrNotFound) {
		t.Errorf("删除后 Get 错误 = %v，期望索引项不存在", err)
	}
	if ids := searchIDs(t, idx, "新增正文", 10); len(ids) != 0 {
		t.Errorf("删除后仍能搜到，结果 = %v", ids)
	}

	// 隐藏文件与非 md 文件不参与索引。
	if err := os.WriteFile(filepath.Join(ws.NotesDir(), ".hidden.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.NotesDir(), "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	stats, err = idx.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync 失败: %v", err)
	}
	if stats.Added != 0 || stats.Total != 1 {
		t.Fatalf("隐藏文件/非 md 文件被索引了: %+v", stats)
	}
}

func TestUpsertAndRemoveFile(t *testing.T) {
	ws := newWorkspace(t)
	idx := openIndex(t, ws)

	path := writeNoteFile(t, ws, "n1", frontMatterNote("n1", "标题", "", 0, "正文\n"))

	change, err := idx.UpsertFile(path)
	if err != nil {
		t.Fatalf("UpsertFile 失败: %v", err)
	}
	if change.ID != "n1" || change.Kind != ChangeAdded {
		t.Errorf("Change = %+v，期望 added n1", change)
	}

	change, err = idx.UpsertFile(path)
	if err != nil {
		t.Fatalf("UpsertFile 失败: %v", err)
	}
	if change.Kind != ChangeUpdated {
		t.Errorf("重复 Upsert 的 Change = %+v，期望 updated", change)
	}

	change, err = idx.RemoveFile(path)
	if err != nil {
		t.Fatalf("RemoveFile 失败: %v", err)
	}
	if change.ID != "n1" || change.Kind != ChangeRemoved {
		t.Errorf("RemoveFile Change = %+v", change)
	}

	change, err = idx.RemoveFile(path)
	if err != nil {
		t.Fatalf("重复 RemoveFile 失败: %v", err)
	}
	if change.Kind != ChangeUnchanged {
		t.Errorf("重复 RemoveFile Change = %+v，期望 unchanged", change)
	}

	// 文件已删除时 UpsertFile 等价于删除。
	missing := filepath.Join(ws.NotesDir(), "missing.md")
	change, err = idx.UpsertFile(missing)
	if err != nil {
		t.Fatalf("文件不存在时 UpsertFile 失败: %v", err)
	}
	if change.Kind != ChangeUnchanged {
		t.Errorf("文件不存在时 Change = %+v，期望 unchanged", change)
	}
}

func TestRebuildIsRepeatable(t *testing.T) {
	ws := newWorkspace(t)
	idx := openIndex(t, ws)

	writeNoteFile(t, ws, "a", frontMatterNote("a", "A", "", 0, "正文 A\n"))
	writeNoteFile(t, ws, "b", frontMatterNote("b", "B", "a", 0, "正文 B\n"))

	first, err := idx.Rebuild(context.Background())
	if err != nil {
		t.Fatalf("首次 Rebuild 失败: %v", err)
	}
	if first.Total != 2 {
		t.Fatalf("首次 Rebuild = %+v", first)
	}

	second, err := idx.Rebuild(context.Background())
	if err != nil {
		t.Fatalf("再次 Rebuild 失败: %v", err)
	}
	if second.Total != 2 || second.Added != 2 {
		t.Fatalf("再次 Rebuild = %+v，期望 added=2 total=2", second)
	}

	// 重建后索引文件被删除，重新打开并重建，结果应一致。
	before, err := idx.List()
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Fatalf("关闭索引失败: %v", err)
	}
	if err := os.Remove(idx.Path()); err != nil {
		t.Fatalf("删除索引文件失败: %v", err)
	}

	reopened, err := Open(ws)
	if err != nil {
		t.Fatalf("重新打开索引失败: %v", err)
	}
	defer reopened.Close()

	stats, err := reopened.Rebuild(context.Background())
	if err != nil {
		t.Fatalf("重建失败: %v", err)
	}
	if stats.Total != 2 {
		t.Fatalf("重建 Stats = %+v", stats)
	}
	after, err := reopened.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Fatalf("重建前后数量不同: %d vs %d", len(before), len(after))
	}
	for i := range before {
		if !metaEqual(before[i], after[i]) {
			t.Errorf("重建前后第 %d 项不同:\nbefore=%+v\nafter=%+v", i, before[i], after[i])
		}
	}
	if ids := searchIDs(t, reopened, "正文", 10); len(ids) != 2 {
		t.Errorf("重建后检索结果 = %v，期望 2 条", ids)
	}
}

// metaEqual 比较元数据的关键字段（MTime 精度在各平台可能略有差异）。
func metaEqual(a, b Meta) bool {
	a.MTime, b.MTime = time.Time{}, time.Time{}
	if len(a.Extra) == 0 {
		a.Extra = nil
	}
	if len(b.Extra) == 0 {
		b.Extra = nil
	}
	return a.ID == b.ID && a.Title == b.Title && a.Parent == b.Parent &&
		a.Order == b.Order && a.Path == b.Path && a.Size == b.Size &&
		a.Created.Equal(b.Created) && a.Updated.Equal(b.Updated)
}

func TestSchemaVersionMismatchRebuilds(t *testing.T) {
	ws := newWorkspace(t)
	idx := openIndex(t, ws)

	writeNoteFile(t, ws, "n1", frontMatterNote("n1", "标题", "", 0, "正文\n"))
	if _, err := idx.Rebuild(context.Background()); err != nil {
		t.Fatalf("Rebuild 失败: %v", err)
	}
	if err := idx.Close(); err != nil {
		t.Fatalf("关闭索引失败: %v", err)
	}

	// 手工把 user_version 改成旧版本，模拟结构升级。
	db, err := openRaw(idx.Path())
	if err != nil {
		t.Fatalf("直接打开数据库失败: %v", err)
	}
	if _, err := db.Exec("PRAGMA user_version = 0"); err != nil {
		t.Fatalf("写入旧版本失败: %v", err)
	}
	_ = db.Close()

	reopened, err := Open(ws)
	if err != nil {
		t.Fatalf("重新打开索引失败: %v", err)
	}
	defer reopened.Close()

	var version int
	if err := reopened.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("读取版本失败: %v", err)
	}
	if version != schemaVersion {
		t.Fatalf("user_version = %d，期望 %d", version, schemaVersion)
	}
	total, err := reopened.Count()
	if err != nil {
		t.Fatalf("Count 失败: %v", err)
	}
	if total != 0 {
		t.Errorf("版本不匹配后索引应清空，实际 %d 条", total)
	}

	// 重建后数据恢复。
	stats, err := reopened.Rebuild(context.Background())
	if err != nil {
		t.Fatalf("重建失败: %v", err)
	}
	if stats.Total != 1 {
		t.Errorf("重建 Stats = %+v", stats)
	}
}

func TestWatchDetectsChanges(t *testing.T) {
	ws := newWorkspace(t)
	idx := openIndex(t, ws)

	writeNoteFile(t, ws, "existing", frontMatterNote("existing", "已有", "", 0, "已有正文\n"))
	if _, err := idx.Rebuild(context.Background()); err != nil {
		t.Fatalf("Rebuild 失败: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan Event, 16)
	done := make(chan error, 1)
	ready := make(chan struct{})
	go func() {
		done <- watch(ctx, ws, idx, func(e Event) {
			select {
			case events <- e:
			default:
			}
		}, ready)
	}()
	<-ready

	// 等待监听器就绪：写入一个探针文件并等到事件出现。
	waitEvent := func(t *testing.T, wantID string) Event {
		t.Helper()
		timeout := time.After(5 * time.Second)
		for {
			select {
			case e := <-events:
				if contains(e.IDs, wantID) {
					return e
				}
			case <-timeout:
				t.Fatalf("等待 %s 的事件超时", wantID)
			}
		}
	}

	// 新增文件。
	writeNoteFile(t, ws, "added", frontMatterNote("added", "新增", "", 1, "监听新增正文\n"))
	waitEvent(t, "added")
	if ids := searchIDs(t, idx, "监听新增正文", 10); !contains(ids, "added") {
		t.Errorf("新增文件未被索引，检索结果 = %v", ids)
	}

	// 修改文件（模拟 store 的原子写：临时文件 + rename）。
	tmpPath := filepath.Join(ws.NotesDir(), ".existing.md.tmp-test")
	if err := os.WriteFile(tmpPath, []byte(frontMatterNote("existing", "已有（改）", "", 0, "监听修改正文\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmpPath, filepath.Join(ws.NotesDir(), "existing.md")); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, "existing")
	meta, err := idx.Get("existing")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if meta.Title != "已有（改）" {
		t.Errorf("修改后 Title = %q", meta.Title)
	}
	if ids := searchIDs(t, idx, "监听修改正文", 10); !contains(ids, "existing") {
		t.Errorf("修改后全文索引未更新，检索结果 = %v", ids)
	}

	// 删除文件。
	if err := os.Remove(filepath.Join(ws.NotesDir(), "added.md")); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, "added")
	if _, err := idx.Get("added"); !errors.Is(err, ErrNotFound) {
		t.Errorf("删除后 Get 错误 = %v，期望索引项不存在", err)
	}

	// 取消后 Watch 正常返回。
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Watch 返回错误: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("取消后 Watch 未返回")
	}
}

func TestWatchIgnoresHiddenAndNonMarkdown(t *testing.T) {
	ws := newWorkspace(t)
	idx := openIndex(t, ws)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan Event, 16)
	done := make(chan error, 1)
	ready := make(chan struct{})
	go func() {
		done <- watch(ctx, ws, idx, func(e Event) { events <- e }, ready)
	}()
	<-ready

	// 隐藏文件与 txt 文件不应触发回调。
	if err := os.WriteFile(filepath.Join(ws.NotesDir(), ".tmp.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.NotesDir(), "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case e := <-events:
		t.Fatalf("不应收到事件，实际 = %+v", e)
	case <-time.After(500 * time.Millisecond):
	}

	// 正常的 md 文件仍能触发。
	writeNoteFile(t, ws, "real", frontMatterNote("real", "真实", "", 0, "正文\n"))
	select {
	case e := <-events:
		if !contains(e.IDs, "real") {
			t.Errorf("事件 IDs = %v，期望包含 real", e.IDs)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("等待真实文件事件超时")
	}

	cancel()
	<-done
}

func TestOpenInvalidWorkspace(t *testing.T) {
	if _, err := Open(nil); !errors.Is(err, store.ErrInvalid) {
		t.Errorf("nil 工作台错误 = %v，期望 ErrInvalid", err)
	}
}
