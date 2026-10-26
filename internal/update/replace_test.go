package update

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeTestApp 创建一个小型 .app 目录树并写入标记文件。
func makeTestApp(t *testing.T, root, name, marker string) string {
	t.Helper()
	app := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(marker), 0o644); err != nil {
		t.Fatal(err)
	}
	return app
}

func TestReplaceAppSuccess(t *testing.T) {
	dir := t.TempDir()
	current := makeTestApp(t, dir, "跬步.app", "old")
	newApp := makeTestApp(t, filepath.Join(dir, "staging"), "跬步.app", "new")

	if err := ReplaceApp(current, newApp); err != nil {
		t.Fatalf("ReplaceApp 返回错误: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(current, "Contents", "Info.plist"))
	if err != nil || string(data) != "new" {
		t.Errorf("替换后内容 = %q, %v，期望 new", data, err)
	}
	// 新包目录应被移走。
	if _, err := os.Stat(newApp); !os.IsNotExist(err) {
		t.Errorf("新包目录仍存在: %v", err)
	}
	// 不应残留临时目录。
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".old-") || strings.Contains(e.Name(), ".new-") {
			t.Errorf("残留临时目录: %s", e.Name())
		}
	}
}

func TestReplaceAppUnwritableDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 下无法用权限位模拟不可写目录")
	}
	dir := t.TempDir()
	current := makeTestApp(t, dir, "跬步.app", "old")
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)

	err := ReplaceApp(current, filepath.Join(t.TempDir(), "new.app"))
	if err == nil || !strings.Contains(err.Error(), "目标目录不可写") {
		t.Errorf("不可写目录错误 = %v", err)
	}
}

func TestReplaceAppRollbackOnRenameFailure(t *testing.T) {
	dir := t.TempDir()
	current := makeTestApp(t, dir, "跬步.app", "old")
	newApp := makeTestApp(t, filepath.Join(dir, "staging"), "跬步.app", "new")

	calls := 0
	orig := renameFunc
	renameFunc = func(oldpath, newpath string) error {
		calls++
		if calls == 2 {
			return errors.New("injected rename failure")
		}
		return os.Rename(oldpath, newpath)
	}
	defer func() { renameFunc = orig }()

	err := ReplaceApp(current, newApp)
	if err == nil || !strings.Contains(err.Error(), "已回滚") {
		t.Fatalf("第三步失败应回滚: %v", err)
	}

	// 回滚后当前 App 仍是旧内容。
	data, readErr := os.ReadFile(filepath.Join(current, "Contents", "Info.plist"))
	if readErr != nil || string(data) != "old" {
		t.Errorf("回滚后内容 = %q, %v，期望 old", data, readErr)
	}
	// 不残留临时目录。
	entries, listErr := os.ReadDir(dir)
	if listErr != nil {
		t.Fatal(listErr)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".old-") || strings.Contains(e.Name(), ".new-") {
			t.Errorf("残留临时目录: %s", e.Name())
		}
	}
}

func TestCurrentAppBundle(t *testing.T) {
	dir := t.TempDir()
	app := makeTestApp(t, dir, "跬步.app", "x")
	exe := filepath.Join(app, "Contents", "MacOS", "kuibu")

	got, err := CurrentAppBundle(exe)
	if err != nil || got != app {
		t.Errorf("CurrentAppBundle(%q) = %q, %v，期望 %q", exe, got, err, app)
	}

	if _, err := CurrentAppBundle(filepath.Join(dir, "bin", "kuibu")); err == nil {
		t.Error("不在 .app 内应返回错误")
	}
}
