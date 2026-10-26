package store

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func mustInit(t *testing.T) *Workspace {
	t.Helper()
	w, err := Init(filepath.Join(t.TempDir(), "ws"))
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	return w
}

func TestInitCreatesLayout(t *testing.T) {
	w := mustInit(t)

	for _, dir := range []string{w.Dir(), w.NotesDir(), w.AssetsDir(), w.MetaDir()} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("目录 %s 不存在: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s 不是目录", dir)
		}
	}

	data, err := os.ReadFile(filepath.Join(w.MetaDir(), "config.json"))
	if err != nil {
		t.Fatalf("读取 config.json 失败: %v", err)
	}
	if !strings.Contains(string(data), `"version":1`) {
		t.Errorf("config.json = %q，期望包含 version:1", data)
	}
}

func TestInitAlreadyInit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ws")
	if _, err := Init(dir); err != nil {
		t.Fatalf("首次 Init 失败: %v", err)
	}
	if _, err := Init(dir); !errors.Is(err, ErrAlreadyInit) {
		t.Fatalf("重复 Init 错误 = %v，期望 ErrAlreadyInit", err)
	}
}

func TestInitInvalidNonEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(dir); !errors.Is(err, ErrInvalid) {
		t.Fatalf("非空目录 Init 错误 = %v，期望 ErrInvalid", err)
	}
}

func TestInitExistingEmptyDir(t *testing.T) {
	dir := t.TempDir()
	w, err := Init(dir)
	if err != nil {
		t.Fatalf("空目录 Init 失败: %v", err)
	}
	if w.Dir() != dir {
		t.Errorf("Dir() = %q，期望 %q", w.Dir(), dir)
	}
}

func TestInitFileIsInvalid(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(file); !errors.Is(err, ErrInvalid) {
		t.Fatalf("文件 Init 错误 = %v，期望 ErrInvalid", err)
	}
}

func TestOpenAndFind(t *testing.T) {
	w := mustInit(t)

	if _, err := Open(w.Dir()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	sub := filepath.Join(w.NotesDir(), "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	found, err := Find(sub)
	if err != nil {
		t.Fatalf("Find 失败: %v", err)
	}
	if found.Dir() != w.Dir() {
		t.Errorf("Find 得到 %q，期望 %q", found.Dir(), w.Dir())
	}

	if _, err := Open(t.TempDir()); !errors.Is(err, ErrNotWorkspace) {
		t.Errorf("Open 非工作台错误 = %v，期望 ErrNotWorkspace", err)
	}
	if _, err := Find(t.TempDir()); !errors.Is(err, ErrNotWorkspace) {
		t.Errorf("Find 未找到错误 = %v，期望 ErrNotWorkspace", err)
	}
}

func TestCreateGetRoundTrip(t *testing.T) {
	w := mustInit(t)

	body := "第一段\n\n---\n\n正文里的分隔线不该被当作 front matter\n\n# 不是标题开头\n"
	note, err := w.Create(NewNote{
		Title: "中文标题",
		Body:  body,
		Tags:  []string{"工作", "周报"},
		Type:  "note",
	})
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if !noteIDPattern.MatchString(note.ID) {
		t.Errorf("ID = %q，不符合 yyyymmdd-hhmmss-xxxx", note.ID)
	}

	got, err := w.Get(note.ID)
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got.Title != "中文标题" {
		t.Errorf("Title = %q", got.Title)
	}
	if got.Body != body {
		t.Errorf("Body 往返不一致:\n got=%q\nwant=%q", got.Body, body)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "工作" || got.Tags[1] != "周报" {
		t.Errorf("Tags = %v", got.Tags)
	}
	if got.Type != "note" {
		t.Errorf("Type = %q，期望 note", got.Type)
	}
	if got.Created.IsZero() || got.Updated.IsZero() {
		t.Errorf("时间字段为空: created=%v updated=%v", got.Created, got.Updated)
	}
	if got.Created.Location() == nil || got.Created.Format("Z07:00") == "" {
		t.Errorf("Created 缺少时区: %v", got.Created)
	}
}

func TestCreateDefaultTypeAndOrder(t *testing.T) {
	w := mustInit(t)

	var ids []string
	for i := 0; i < 3; i++ {
		n, err := w.Create(NewNote{Title: "n"})
		if err != nil {
			t.Fatalf("Create 失败: %v", err)
		}
		if n.Type != "note" {
			t.Errorf("默认 Type = %q，期望 note", n.Type)
		}
		if n.Order != i {
			t.Errorf("第 %d 个笔记 Order = %d，期望 %d", i, n.Order, i)
		}
		ids = append(ids, n.ID)
	}

	metas, err := w.List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(metas) != 3 {
		t.Fatalf("List 数量 = %d，期望 3", len(metas))
	}
	if !sort.SliceIsSorted(metas, func(i, j int) bool { return metas[i].ID < metas[j].ID }) {
		t.Errorf("List 未按 ID 排序")
	}
}

func TestCreateParentNotFound(t *testing.T) {
	w := mustInit(t)
	if _, err := w.Create(NewNote{Title: "x", Parent: "nope"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("父不存在错误 = %v，期望 ErrNotFound", err)
	}
}

func TestUpdateBodyAndMeta(t *testing.T) {
	w := mustInit(t)
	n, err := w.Create(NewNote{Title: "t", Body: "b"})
	if err != nil {
		t.Fatal(err)
	}
	before := n.Updated

	updated, err := w.UpdateBody(n.ID, "新正文")
	if err != nil {
		t.Fatalf("UpdateBody 失败: %v", err)
	}
	if updated.Body != "新正文" {
		t.Errorf("Body = %q", updated.Body)
	}

	got, err := w.Get(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "新正文" {
		t.Errorf("落盘 Body = %q", got.Body)
	}
	if got.Updated.Before(before) {
		t.Errorf("updated 未刷新: before=%v after=%v", before, got.Updated)
	}

	if _, err := w.Update(n.ID, func(n *Note) {
		n.Title = "改标题"
		n.Tags = []string{"x"}
	}); err != nil {
		t.Fatalf("Update 失败: %v", err)
	}
	got, err = w.Get(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "改标题" || len(got.Tags) != 1 || got.Tags[0] != "x" {
		t.Errorf("Update 未生效: title=%q tags=%v", got.Title, got.Tags)
	}
}

func TestUpdateNotFound(t *testing.T) {
	w := mustInit(t)
	if _, err := w.UpdateBody("nope", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("错误 = %v，期望 ErrNotFound", err)
	}
	if _, err := w.Get("../escape"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("非法 ID 错误 = %v，期望 ErrNotFound", err)
	}
}

func TestUnknownFrontMatterRoundTrip(t *testing.T) {
	w := mustInit(t)

	raw := `---
id: 20240101-000000-abcd
title: "原始标题"
type: "note"
created: "2024-01-01T10:00:00+08:00"
updated: "2024-01-02T11:00:00+08:00"
tags:
  - 旧标签
parent: ""
order: 0
custom_str: "保留我"
custom_list:
  - a
  - b
custom_map:
  k1: v1
  k2: 2
---
正文内容
`
	path := filepath.Join(w.NotesDir(), "20240101-000000-abcd.md")
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := w.Get("20240101-000000-abcd")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if n.Extra["custom_str"] != "保留我" {
		t.Errorf("Extra[custom_str] = %v", n.Extra["custom_str"])
	}

	if _, err := w.UpdateBody(n.ID, "新正文"); err != nil {
		t.Fatalf("UpdateBody 失败: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"custom_str", "保留我", "custom_list", "custom_map", "k2"} {
		if !strings.Contains(text, want) {
			t.Errorf("写回内容缺少 %q:\n%s", want, text)
		}
	}

	got, err := w.Get(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Extra["custom_str"] != "保留我" {
		t.Errorf("重新读取 Extra[custom_str] = %v", got.Extra["custom_str"])
	}
	if got.Body != "新正文" {
		t.Errorf("Body = %q", got.Body)
	}
}

func TestUpdateExistingExtraField(t *testing.T) {
	w := mustInit(t)

	raw := `---
title: "t"
status: "pending"
priority: 1
---
正文
`
	path := filepath.Join(w.NotesDir(), "n1.md")
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := w.Update("n1", func(n *Note) {
		n.Extra["status"] = "done"
		n.Extra["priority"] = 3
	}); err != nil {
		t.Fatalf("Update 失败: %v", err)
	}

	got, err := w.Get("n1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Extra["status"] != "done" {
		t.Errorf("Extra[status] = %v，期望 done", got.Extra["status"])
	}
	if got.Extra["priority"] != 3 {
		t.Errorf("Extra[priority] = %v，期望 3", got.Extra["priority"])
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `status: "done"`) && !strings.Contains(string(data), "status: done") {
		t.Errorf("落盘未写入新 status:\n%s", data)
	}
}

func TestDeleteExtraField(t *testing.T) {
	w := mustInit(t)

	raw := `---
title: "t"
status: "pending"
keep: "yes"
---
正文
`
	path := filepath.Join(w.NotesDir(), "n2.md")
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := w.Update("n2", func(n *Note) {
		delete(n.Extra, "status")
	}); err != nil {
		t.Fatalf("Update 失败: %v", err)
	}

	got, err := w.Get("n2")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Extra["status"]; ok {
		t.Errorf("Extra[status] 应已删除，实际 = %v", got.Extra["status"])
	}
	if got.Extra["keep"] != "yes" {
		t.Errorf("Extra[keep] = %v，期望 yes", got.Extra["keep"])
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "status:") {
		t.Errorf("落盘仍包含已删除字段 status:\n%s", data)
	}
	if !strings.Contains(string(data), "keep:") {
		t.Errorf("落盘丢失未删除字段 keep:\n%s", data)
	}
}

func TestAddExtraField(t *testing.T) {
	w := mustInit(t)

	n, err := w.Create(NewNote{Title: "t", Body: "正文"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := w.Update(n.ID, func(n *Note) {
		n.Extra["status"] = "done"
		n.Extra["points"] = 5
		n.Extra["labels"] = []string{"a", "b"}
	}); err != nil {
		t.Fatalf("Update 失败: %v", err)
	}

	got, err := w.Get(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Extra["status"] != "done" {
		t.Errorf("Extra[status] = %v，期望 done", got.Extra["status"])
	}
	if got.Extra["points"] != 5 {
		t.Errorf("Extra[points] = %v，期望 5", got.Extra["points"])
	}
	labels, ok := got.Extra["labels"].([]any)
	if !ok || len(labels) != 2 || labels[0] != "a" || labels[1] != "b" {
		t.Errorf("Extra[labels] = %v，期望 [a b]", got.Extra["labels"])
	}
}

func TestUnchangedExtraKeepsOriginalFormat(t *testing.T) {
	w := mustInit(t)

	// flow 风格列表和单引号标量在重新编码后会变成 block 风格/双引号，
	// 未修改时必须原样写回。
	raw := `---
title: "t"
labels: [a, b]
mode: 'fast'
---
正文
`
	path := filepath.Join(w.NotesDir(), "n3.md")
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := w.UpdateBody("n3", "新正文"); err != nil {
		t.Fatalf("UpdateBody 失败: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "labels: [a, b]") {
		t.Errorf("未修改的 flow 列表未保持原格式:\n%s", text)
	}
	if !strings.Contains(text, "mode: 'fast'") {
		t.Errorf("未修改的单引号标量未保持原格式:\n%s", text)
	}
}

func TestReadFileWithoutFrontMatter(t *testing.T) {
	w := mustInit(t)

	path := filepath.Join(w.NotesDir(), "external.md")
	content := "# 外部标题\n\n正文\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile 失败: %v", err)
	}
	if n.ID != "external" {
		t.Errorf("ID = %q，期望 external", n.ID)
	}
	if n.Title != "外部标题" {
		t.Errorf("Title = %q，期望 外部标题", n.Title)
	}
	if n.Body != content {
		t.Errorf("Body = %q，期望原文", n.Body)
	}
	if n.Type != "note" {
		t.Errorf("Type = %q，期望 note", n.Type)
	}
	if n.Created.IsZero() || n.Updated.IsZero() {
		t.Errorf("时间字段为空")
	}
	if IDFromPath(path) != "external" {
		t.Errorf("IDFromPath = %q", IDFromPath(path))
	}

	// 没有 "# " 标题时用文件名。
	path2 := filepath.Join(w.NotesDir(), "plain.md")
	if err := os.WriteFile(path2, []byte("只有正文\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	n2, err := ReadFile(path2)
	if err != nil {
		t.Fatal(err)
	}
	if n2.Title != "plain" {
		t.Errorf("Title = %q，期望 plain", n2.Title)
	}
}

func TestFrontMatterIDMismatchUsesFileName(t *testing.T) {
	w := mustInit(t)
	raw := "---\nid: other-id\ntitle: t\n---\nbody\n"
	path := filepath.Join(w.NotesDir(), "real-id.md")
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := w.Get("real-id")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if n.ID != "real-id" {
		t.Errorf("ID = %q，期望 real-id", n.ID)
	}

	if _, err := w.UpdateBody(n.ID, "x"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `id: "real-id"`) {
		t.Errorf("写回后 id 未统一为文件名:\n%s", data)
	}
}

func TestReadFileNotFound(t *testing.T) {
	if _, err := ReadFile(filepath.Join(t.TempDir(), "nope.md")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("错误 = %v，期望 ErrNotFound", err)
	}
}

func TestAtomicWriteLeavesNoTempFiles(t *testing.T) {
	w := mustInit(t)
	n, err := w.Create(NewNote{Title: "t", Body: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.UpdateBody(n.ID, "c"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.AddAssetBytes("a.png", []byte("data")); err != nil {
		t.Fatal(err)
	}

	for _, dir := range []string{w.NotesDir(), w.AssetsDir(), w.MetaDir()} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if strings.Contains(e.Name(), ".tmp-") {
				t.Errorf("目录 %s 残留临时文件 %s", dir, e.Name())
			}
		}
	}
}
