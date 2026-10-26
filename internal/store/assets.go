package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// assetHashPrefixLen 是资源文件名中 SHA-256 前缀的长度。
const assetHashPrefixLen = 8

// AddAsset 把 srcPath 指向的文件复制到 assets/，返回相对 notes/ 下笔记文件的
// 相对路径（如 ../assets/20240101-abcdef12-pic.png）。
//
// 文件名形如 <yyyymmdd>-<sha256 前 8 位>-<清洗后的原文件名>。内容相同（SHA-256
// 一致）的资源会去重复用已有文件。
func (w *Workspace) AddAsset(srcPath string) (string, error) {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return "", fmt.Errorf("读取资源文件失败: %w", err)
	}
	return w.AddAssetBytes(filepath.Base(srcPath), data)
}

// AddAssetBytes 把内存中的资源数据写入 assets/，返回相对 notes/ 下笔记文件的
// 相对路径。name 用于生成可读文件名，会被清洗成文件系统安全的字符。
func (w *Workspace) AddAssetBytes(name string, data []byte) (string, error) {
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	prefix := hash[:assetHashPrefixLen]

	// 同内容去重：查找已有同前缀资源，并用完整哈希确认。
	entries, err := os.ReadDir(w.AssetsDir())
	if err != nil {
		return "", fmt.Errorf("读取资源目录失败: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.Contains(e.Name(), "-"+prefix+"-") {
			continue
		}
		existing := filepath.Join(w.AssetsDir(), e.Name())
		if existingHash, err := fileSHA256(existing); err == nil && existingHash == hash {
			return assetRelPath(e.Name()), nil
		}
	}

	fileName := time.Now().Format("20060102") + "-" + prefix + "-" + sanitizeAssetName(name)
	dest := filepath.Join(w.AssetsDir(), fileName)
	if err := writeFileAtomic(dest, data); err != nil {
		return "", fmt.Errorf("写入资源文件失败: %w", err)
	}
	return assetRelPath(fileName), nil
}

// assetRelPath 返回资源相对于 notes/ 目录下笔记文件的路径。
func assetRelPath(fileName string) string {
	return "../" + assetsDirName + "/" + fileName
}

// fileSHA256 计算文件的 SHA-256 十六进制字符串。
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// sanitizeAssetName 清洗资源文件名：去掉路径分隔符与不安全字符，保留中文等
// 可打印字符，其余替换为下划线。清洗后为空时返回 "asset"。
func sanitizeAssetName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == '.' || r == '-' || r == '_':
			b.WriteRune(r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.Trim(b.String(), ".")
	if out == "" || out == "_" {
		return "asset"
	}
	return out
}
