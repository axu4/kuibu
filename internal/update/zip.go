package update

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ExtractZip 解压 zipPath 到 destDir。
//
// 安全约束：
//   - 拒绝 zip 内的绝对路径与含 `..` 的路径（防 zip slip）；
//   - 保留文件的可执行权限位；
//   - 支持符号链接，但链接目标（相对链接所在目录解析）不得逃出 destDir。
func ExtractZip(zipPath, destDir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("打开 zip 失败: %w", err)
	}
	defer zr.Close()

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("创建解压目录失败: %w", err)
	}

	for _, f := range zr.File {
		target, err := safeZipTarget(destDir, f.Name)
		if err != nil {
			return err
		}

		mode := f.Mode()
		if mode.IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("创建目录 %s 失败: %w", target, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("创建目录 %s 失败: %w", filepath.Dir(target), err)
		}

		if mode&os.ModeSymlink != 0 {
			rc, err := f.Open()
			if err != nil {
				return fmt.Errorf("读取符号链接 %s 失败: %w", f.Name, err)
			}
			linkTarget, readErr := io.ReadAll(rc)
			closeErr := rc.Close()
			if readErr != nil {
				return fmt.Errorf("读取符号链接 %s 失败: %w", f.Name, readErr)
			}
			if closeErr != nil {
				return fmt.Errorf("关闭符号链接 %s 失败: %w", f.Name, closeErr)
			}
			link := strings.TrimSpace(string(linkTarget))
			if err := checkSymlinkTarget(filepath.Dir(target), link, destDir); err != nil {
				return fmt.Errorf("zip 内符号链接 %s 非法: %w", f.Name, err)
			}
			if err := os.Symlink(link, target); err != nil {
				return fmt.Errorf("创建符号链接 %s 失败: %w", target, err)
			}
			continue
		}

		perm := mode.Perm()
		if perm == 0 {
			perm = 0o644
		}
		if err := extractRegularFile(f, target, perm); err != nil {
			return err
		}
	}
	return nil
}

// safeZipTarget 计算 zip 条目在 destDir 内的安全落盘路径，防 zip slip。
func safeZipTarget(destDir, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("zip 内含空路径条目")
	}
	// 统一把反斜杠当作分隔符，避免 Windows 风格路径逃逸。
	name = strings.ReplaceAll(name, "\\", "/")
	if path.IsAbs(name) || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("zip 内含绝对路径: %q", name)
	}
	clean := path.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("zip 内路径逃逸出解压目录: %q", name)
	}

	target := filepath.Join(destDir, filepath.FromSlash(clean))
	rel, err := filepath.Rel(destDir, target)
	if err != nil {
		return "", fmt.Errorf("zip 内路径非法: %q", name)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("zip 内路径逃逸出解压目录: %q", name)
	}
	return target, nil
}

// checkSymlinkTarget 校验符号链接目标（相对链接所在目录）不逃出 destDir。
func checkSymlinkTarget(linkDir, linkTarget, destDir string) error {
	if linkTarget == "" {
		return fmt.Errorf("符号链接目标为空")
	}
	if path.IsAbs(linkTarget) || strings.HasPrefix(linkTarget, "/") {
		return fmt.Errorf("符号链接目标为绝对路径: %q", linkTarget)
	}
	resolved := path.Clean(path.Join(filepath.ToSlash(linkDir), linkTarget))
	rel, err := filepath.Rel(destDir, filepath.FromSlash(resolved))
	if err != nil {
		return fmt.Errorf("符号链接目标非法: %q", linkTarget)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("符号链接目标逃逸出解压目录: %q", linkTarget)
	}
	return nil
}

// extractRegularFile 解压普通文件并保留权限位（写后 Chmod，避免受 umask 影响）。
func extractRegularFile(f *zip.File, target string, perm os.FileMode) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("打开 zip 条目 %s 失败: %w", f.Name, err)
	}
	defer rc.Close()

	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("创建文件 %s 失败: %w", target, err)
	}
	if _, err := io.Copy(out, rc); err != nil {
		_ = out.Close()
		return fmt.Errorf("写入文件 %s 失败: %w", target, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("关闭文件 %s 失败: %w", target, err)
	}
	if err := os.Chmod(target, perm); err != nil {
		return fmt.Errorf("设置文件 %s 权限失败: %w", target, err)
	}
	return nil
}

// FindAppBundle 在 dir 中查找唯一的 *.app 目录。
//
// 跳过 macOS 解压常见的干扰条目：__MACOSX 目录以及以 ._ 开头的
// AppleDouble 条目，避免把元数据目录里的同名 .app 误算进去。
func FindAppBundle(dir string) (string, error) {
	var found []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if strings.HasPrefix(name, "._") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() && name == "__MACOSX" {
			return filepath.SkipDir
		}
		if d.IsDir() && strings.HasSuffix(name, ".app") {
			found = append(found, p)
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("遍历解压目录失败: %w", err)
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("解压结果中未找到 .app 目录")
	case 1:
		return found[0], nil
	default:
		return "", fmt.Errorf("解压结果中有多个 .app 目录: %s", strings.Join(found, "、"))
	}
}
