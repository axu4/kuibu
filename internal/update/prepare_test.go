package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepare(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "in.zip")
	writeTestZip(t, zipPath, [][]string{
		{"dir", "跬步.app/"},
		{"dir", "跬步.app/Contents/"},
		{"file", "跬步.app/Contents/Info.plist", "version-0.2.0", "644"},
	})
	zipBytes, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(zipBytes)
	sumHex := hex.EncodeToString(sum[:])

	mux := http.NewServeMux()
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  kuibu-macos-arm64.zip\n", sumHex)
	})
	mux.HandleFunc("/zip", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zipBytes)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	rel := &Release{Assets: []Asset{
		{Name: "SHA256SUMS", URL: srv.URL + "/sums"},
		{Name: "kuibu-macos-arm64.zip", URL: srv.URL + "/zip"},
	}}
	c := &Client{BaseURL: srv.URL}
	workDir := filepath.Join(dir, "work")

	got, err := Prepare(context.Background(), c, rel, workDir)
	if err != nil {
		t.Fatalf("Prepare 返回错误: %v", err)
	}
	if filepath.Base(got) != "跬步.app" {
		t.Errorf("返回的新 App 路径 = %q", got)
	}
	data, err := os.ReadFile(filepath.Join(got, "Contents", "Info.plist"))
	if err != nil || string(data) != "version-0.2.0" {
		t.Errorf("解压内容 = %q, %v", data, err)
	}
}

func TestPrepareChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	zipBytes := []byte("not really a zip")

	mux := http.NewServeMux()
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  kuibu-macos-arm64.zip\n", strings.Repeat("0", 64))
	})
	mux.HandleFunc("/zip", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zipBytes)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	rel := &Release{Assets: []Asset{
		{Name: "SHA256SUMS", URL: srv.URL + "/sums"},
		{Name: "kuibu-macos-arm64.zip", URL: srv.URL + "/zip"},
	}}
	c := &Client{BaseURL: srv.URL}

	_, err := Prepare(context.Background(), c, rel, filepath.Join(dir, "work"))
	if err == nil || !strings.Contains(err.Error(), "校验失败") {
		t.Errorf("校验和不匹配应返回错误: %v", err)
	}
}
