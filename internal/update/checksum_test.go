package update

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseChecksums(t *testing.T) {
	h1 := strings.Repeat("a", 64)
	h2 := strings.Repeat("b", 64)
	in := h1 + "  kuibu-macos-arm64.zip\n" + h2 + " *kuibu-darwin-arm64\n\n"
	sums, err := ParseChecksums(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseChecksums 返回错误: %v", err)
	}
	if got := sums["kuibu-macos-arm64.zip"]; got != h1 {
		t.Errorf("zip 哈希 = %q，期望 %q", got, h1)
	}
	if got := sums["kuibu-darwin-arm64"]; got != h2 {
		t.Errorf("cli 哈希 = %q，期望 %q", got, h2)
	}
}

func TestParseChecksumsInvalid(t *testing.T) {
	bad := []string{
		"not-hex  file.zip\n",
		"abc\n",
		strings.Repeat("z", 64) + "  file.zip\n",
	}
	for _, in := range bad {
		if _, err := ParseChecksums(strings.NewReader(in)); err == nil {
			t.Errorf("ParseChecksums(%q) 应返回错误", in)
		}
	}
}

func TestVerifyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.bin")
	data := []byte("kuibu-update")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	want := hex.EncodeToString(sum[:])

	if err := VerifyFile(path, want); err != nil {
		t.Fatalf("VerifyFile 正确哈希返回错误: %v", err)
	}
	if err := VerifyFile(path, strings.ToUpper(want)); err != nil {
		t.Fatalf("VerifyFile 大小写不敏感校验返回错误: %v", err)
	}
	if err := VerifyFile(path, strings.Repeat("0", 64)); err == nil {
		t.Fatal("VerifyFile 错误哈希应返回错误")
	}
	if err := VerifyFile(filepath.Join(dir, "missing"), want); err == nil {
		t.Fatal("VerifyFile 文件不存在应返回错误")
	}
}
