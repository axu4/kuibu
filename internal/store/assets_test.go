package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddAssetBytesAndDedup(t *testing.T) {
	w := mustInit(t)

	data := []byte("fake png data")
	rel1, err := w.AddAssetBytes("图片 1.png", data)
	if err != nil {
		t.Fatalf("AddAssetBytes 失败: %v", err)
	}
	if !strings.HasPrefix(rel1, "../assets/") {
		t.Errorf("相对路径 = %q，期望以 ../assets/ 开头", rel1)
	}
	if !strings.HasSuffix(rel1, ".png") {
		t.Errorf("相对路径 = %q，期望保留扩展名", rel1)
	}

	// 从 notes/ 下的笔记文件解析该相对路径应能命中真实文件。
	resolved := filepath.Join(w.NotesDir(), rel1)
	if _, err := os.Stat(resolved); err != nil {
		t.Fatalf("相对路径解析后文件不存在: %v", err)
	}

	// 相同内容去重。
	rel2, err := w.AddAssetBytes("另一个名字.png", data)
	if err != nil {
		t.Fatalf("AddAssetBytes 失败: %v", err)
	}
	if rel1 != rel2 {
		t.Errorf("相同内容返回不同路径: %q vs %q", rel1, rel2)
	}

	entries, err := os.ReadDir(w.AssetsDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("assets 目录文件数 = %d，期望 1（去重）", len(entries))
	}

	// 不同内容生成不同文件。
	rel3, err := w.AddAssetBytes("图片 1.png", []byte("other"))
	if err != nil {
		t.Fatal(err)
	}
	if rel3 == rel1 {
		t.Errorf("不同内容复用了同一路径")
	}
}

func TestAddAssetFromFile(t *testing.T) {
	w := mustInit(t)
	src := filepath.Join(t.TempDir(), "a b+c.png")
	if err := os.WriteFile(src, []byte("bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	rel, err := w.AddAsset(src)
	if err != nil {
		t.Fatalf("AddAsset 失败: %v", err)
	}
	if !strings.HasPrefix(rel, "../assets/") {
		t.Errorf("相对路径 = %q", rel)
	}
	// 文件名清洗后不含空格与加号。
	name := filepath.Base(rel)
	if strings.ContainsAny(name, " +") {
		t.Errorf("资源文件名未清洗: %q", name)
	}

	if _, err := w.AddAsset(filepath.Join(t.TempDir(), "nope.png")); err == nil {
		t.Errorf("不存在的源文件应报错")
	}
}

func TestSanitizeAssetName(t *testing.T) {
	cases := map[string]string{
		"a b.png":      "a_b.png",
		"中文图片.jpg":     "中文图片.jpg",
		"../evil.sh":   "evil.sh",
		"":             "asset",
		"...":          "asset",
		"a/b\\c.png":   "c.png",
		"tab\there.md": "tab_here.md",
	}
	for in, want := range cases {
		if got := sanitizeAssetName(in); got != want {
			t.Errorf("sanitizeAssetName(%q) = %q，期望 %q", in, got, want)
		}
	}
}
