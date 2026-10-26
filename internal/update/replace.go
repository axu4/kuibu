package update

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// 以下变量可在测试中替换，用于模拟文件系统操作失败。
var (
	renameFunc    = os.Rename
	removeAllFunc = os.RemoveAll
	writableCheck = ensureWritable
)

// ReplaceApp 用 newApp 替换正在运行的 currentApp。
//
// 两者都是 .app 目录。步骤：
//  1. 把 newApp 移/复制到 currentApp 同目录的临时名（保证同一文件系统）；
//  2. 把 currentApp 改名为备份名；
//  3. 把临时新包改名为 currentApp。
//
// 任一步失败都会回滚（把备份改回 currentApp）并返回明确错误；成功后删除备份。
func ReplaceApp(currentApp, newApp string) error {
	if currentApp == "" || newApp == "" {
		return fmt.Errorf("应用路径不能为空")
	}
	info, err := os.Stat(currentApp)
	if err != nil {
		return fmt.Errorf("当前应用不存在: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("当前应用不是目录: %s", currentApp)
	}

	parent := filepath.Dir(currentApp)
	if err := writableCheck(parent); err != nil {
		return err
	}

	tmpNew := filepath.Join(parent, ".跬步.app.new-"+randomSuffix())
	backup := filepath.Join(parent, ".跬步.app.old-"+randomSuffix())

	if err := moveOrCopy(newApp, tmpNew); err != nil {
		return fmt.Errorf("准备新版本失败: %w", err)
	}
	movedNew := true
	cleanup := func() {
		if movedNew {
			_ = removeAllFunc(tmpNew)
		}
	}

	if err := renameFunc(currentApp, backup); err != nil {
		cleanup()
		return fmt.Errorf("备份当前应用失败: %w", err)
	}
	if err := renameFunc(tmpNew, currentApp); err != nil {
		// 回滚：把备份改回当前应用。
		_ = renameFunc(backup, currentApp)
		cleanup()
		return fmt.Errorf("启用新版本失败，已回滚: %w", err)
	}

	// 替换成功，删除备份；失败只影响遗留旧目录，不影响已完成的替换。
	_ = removeAllFunc(backup)
	return nil
}

// moveOrCopy 先尝试同文件系统 rename；跨文件系统失败时递归复制后再删除源。
func moveOrCopy(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyTree(src, dst); err != nil {
		return fmt.Errorf("复制 %s 到 %s 失败: %w", src, dst, err)
	}
	if err := removeAllFunc(src); err != nil {
		return fmt.Errorf("删除源目录 %s 失败: %w", src, err)
	}
	return nil
}

// copyTree 递归复制目录树，保留权限位与符号链接。
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		switch {
		case info.IsDir():
			return os.MkdirAll(target, info.Mode().Perm())
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			return os.Symlink(link, target)
		default:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			return copyFile(p, target, info.Mode().Perm())
		}
	})
}

// copyFile 复制单个普通文件并保留权限位。
func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, perm)
}

// ensureWritable 检查目录是否可写（创建并删除一个探针文件）。
func ensureWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".kuibu-write-test-*")
	if err != nil {
		return fmt.Errorf("目标目录不可写: %w；请把 App 安装到 /Applications 或用户可写位置", err)
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return nil
}

// randomSuffix 返回随机十六进制后缀，用于临时目录名。
func randomSuffix() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand 几乎不会失败；极端情况下用时间戳兜底。
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b[:])
}

// CurrentAppBundle 从可执行文件路径（.../X.app/Contents/MacOS/kuibu）推导 .app 根目录。
//
// 不在 .app 包内时返回错误（开发模式不支持自更新）。
func CurrentAppBundle(exe string) (string, error) {
	if exe == "" {
		return "", fmt.Errorf("无法获取可执行文件路径")
	}
	dir := filepath.Dir(exe)
	for {
		if strings.HasSuffix(dir, ".app") {
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("当前可执行文件不在 .app 包内（开发模式不支持自更新）: %s", exe)
}
