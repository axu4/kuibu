package update

import (
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// writeTestZip 用 entries 构造一个 zip 文件。entry 可以是：
//
//	{"dir", "path/"}                目录
//	{"file", "path", "content", perm} 普通文件
//	{"symlink", "path", "target"}    符号链接
func writeTestZip(t *testing.T, path string, entries [][]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for _, e := range entries {
		switch e[0] {
		case "dir":
			_, err := zw.Create(e[1])
			if err != nil {
				t.Fatal(err)
			}
		case "file":
			hdr := &zip.FileHeader{Name: e[1], Method: zip.Deflate}
			perm := os.FileMode(0o644)
			if len(e) > 3 {
				perm = parsePerm(t, e[3])
			}
			hdr.SetMode(perm)
			w, err := zw.CreateHeader(hdr)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write([]byte(e[2])); err != nil {
				t.Fatal(err)
			}
		case "symlink":
			hdr := &zip.FileHeader{Name: e[1]}
			hdr.SetMode(os.ModeSymlink | 0o777)
			w, err := zw.CreateHeader(hdr)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write([]byte(e[2])); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

// parsePerm 解析三位八进制权限字符串（如 "755"）。
func parsePerm(t *testing.T, s string) os.FileMode {
	t.Helper()
	if len(s) != 3 {
		t.Fatalf("权限字符串应为三位八进制: %q", s)
	}
	var m os.FileMode
	for i, r := range s {
		if r < '0' || r > '7' {
			t.Fatalf("权限字符串非法: %q", s)
		}
		m |= os.FileMode(r-'0') << uint(6-3*i)
	}
	return m
}

func TestExtractZipNormal(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "in.zip")
	writeTestZip(t, zipPath, [][]string{
		{"dir", "跬步.app/"},
		{"dir", "跬步.app/Contents/"},
		{"file", "跬步.app/Contents/Info.plist", "<plist/>", "644"},
		{"file", "跬步.app/Contents/MacOS/kuibu", "binary", "755"},
	})

	dest := filepath.Join(dir, "out")
	if err := ExtractZip(zipPath, dest); err != nil {
		t.Fatalf("ExtractZip 返回错误: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dest, "跬步.app", "Contents", "Info.plist"))
	if err != nil || string(data) != "<plist/>" {
		t.Errorf("Info.plist = %q, %v", data, err)
	}

	bin := filepath.Join(dest, "跬步.app", "Contents", "MacOS", "kuibu")
	info, err := os.Stat(bin)
	if err != nil {
		t.Fatalf("kuibu 二进制未解压: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		t.Errorf("可执行权限位未保留: %o", info.Mode().Perm())
	}
}

func TestExtractZipRejectsZipSlip(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "in.zip")
	writeTestZip(t, zipPath, [][]string{
		{"file", "../evil.txt", "bad", "644"},
	})

	dest := filepath.Join(dir, "out")
	if err := ExtractZip(zipPath, dest); err == nil {
		t.Fatal("zip slip 应被拒绝")
	}
	if _, err := os.Stat(filepath.Join(dir, "evil.txt")); !os.IsNotExist(err) {
		t.Errorf("zip slip 写出了目录外文件: %v", err)
	}
}

func TestExtractZipRejectsSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "in.zip")
	writeTestZip(t, zipPath, [][]string{
		{"dir", "跬步.app/"},
		{"symlink", "跬步.app/escape", "../../outside"},
	})

	dest := filepath.Join(dir, "out")
	if err := ExtractZip(zipPath, dest); err == nil {
		t.Fatal("符号链接逃逸应被拒绝")
	}
	if _, err := os.Lstat(filepath.Join(dest, "跬步.app", "escape")); !os.IsNotExist(err) {
		t.Errorf("逃逸符号链接不应被创建: %v", err)
	}
}

func TestExtractZipAllowsSymlinkInside(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "in.zip")
	writeTestZip(t, zipPath, [][]string{
		{"dir", "跬步.app/Contents/Frameworks/F.framework/Versions/A/"},
		{"file", "跬步.app/Contents/Frameworks/F.framework/Versions/A/F", "lib", "755"},
		{"symlink", "跬步.app/Contents/Frameworks/F.framework/Versions/Current", "A"},
	})

	dest := filepath.Join(dir, "out")
	if err := ExtractZip(zipPath, dest); err != nil {
		t.Fatalf("ExtractZip 返回错误: %v", err)
	}
	link := filepath.Join(dest, "跬步.app", "Contents", "Frameworks", "F.framework", "Versions", "Current")
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("符号链接未创建: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("应为符号链接: %v", fi.Mode())
	}
	target, err := os.Readlink(link)
	if err != nil || target != "A" {
		t.Errorf("链接目标 = %q, %v", target, err)
	}
}

func TestFindAppBundle(t *testing.T) {
	t.Run("唯一", func(t *testing.T) {
		dir := t.TempDir()
		app := filepath.Join(dir, "跬步.app")
		if err := os.MkdirAll(app, 0o755); err != nil {
			t.Fatal(err)
		}
		got, err := FindAppBundle(dir)
		if err != nil || got != app {
			t.Errorf("FindAppBundle = %q, %v，期望 %q", got, err, app)
		}
	})
	t.Run("缺失", func(t *testing.T) {
		if _, err := FindAppBundle(t.TempDir()); err == nil {
			t.Error("没有 .app 时应返回错误")
		}
	})
	t.Run("多个", func(t *testing.T) {
		dir := t.TempDir()
		for _, name := range []string{"A.app", "B.app"} {
			if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := FindAppBundle(dir); err == nil {
			t.Error("多个 .app 时应返回错误")
		}
	})
}

// 确认权限解析辅助函数（覆盖三位八进制解析）。
func TestParsePermHelper(t *testing.T) {
	if got := parsePerm(t, "755"); got != 0o755 {
		t.Errorf("parsePerm(755) = %o", got)
	}
	if got := parsePerm(t, "644"); got != 0o644 {
		t.Errorf("parsePerm(644) = %o", got)
	}
}
