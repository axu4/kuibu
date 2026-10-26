package update

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// ParseChecksums 解析 `shasum -a 256` 输出的校验和文件。
//
// 格式为每行 `<64位十六进制哈希>  <文件名>`，文件名前可能带 `*`（二进制模式标记）。
// 返回文件名到小写十六进制哈希的映射。
func ParseChecksums(r io.Reader) (map[string]string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("读取校验和文件失败: %w", err)
	}

	sums := make(map[string]string)
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		hexPart := ""
		name := ""
		if len(line) >= 64 {
			hexPart = line[:64]
			rest := strings.TrimLeft(line[64:], " ")
			if strings.HasPrefix(rest, "*") {
				rest = rest[1:]
			}
			name = strings.TrimSpace(rest)
		} else {
			// 容忍不规范的空白，退回按字段切分。
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				hexPart = fields[0]
				name = strings.TrimLeft(strings.Join(fields[1:], " "), "*")
			}
		}

		if len(hexPart) != 64 || !isHex(hexPart) {
			return nil, fmt.Errorf("校验和文件第 %d 行哈希不合法: %q", i+1, line)
		}
		if name == "" {
			return nil, fmt.Errorf("校验和文件第 %d 行缺少文件名: %q", i+1, line)
		}
		sums[name] = strings.ToLower(hexPart)
	}
	return sums, nil
}

// isHex 判断 s 是否全部为十六进制字符。
func isHex(s string) bool {
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}

// VerifyFile 校验文件的 SHA-256 是否等于 wantHex（大小写不敏感）。
func VerifyFile(path, wantHex string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("打开文件失败: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("读取文件失败: %w", err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, wantHex) {
		return fmt.Errorf("SHA-256 校验和不匹配：期望 %s，实际 %s", strings.ToLower(wantHex), got)
	}
	return nil
}
