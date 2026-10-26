package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axu4/kuibu/internal/config"
	"github.com/axu4/kuibu/internal/version"
)

// fakeUpdateUI 记录 UpdateService 对桌面能力的调用。
type fakeUpdateUI struct {
	events  []string
	data    []any
	quit    bool
	started []string
}

func (u *fakeUpdateUI) progressStages() []string {
	var stages []string
	for _, d := range u.data {
		if p, ok := d.(UpdateProgress); ok {
			stages = append(stages, p.Stage)
		}
	}
	return stages
}

// newTestUpdate 构造一个使用隔离配置目录与假桌面能力的 UpdateService。
func newTestUpdate(t *testing.T) (*UpdateService, *fakeUpdateUI) {
	t.Helper()
	configDir := t.TempDir()
	s := newUpdateService()
	s.env = func(key string) string {
		if key == "KUIBU_CONFIG_DIR" {
			return configDir
		}
		return ""
	}
	s.getenv = func(string) string { return "" }
	s.lookPath = func(string) (string, error) { return "", errors.New("no gh") }
	s.run = func(string, ...string) ([]byte, error) { return nil, errors.New("no gh") }
	s.now = func() time.Time {
		return time.Date(2026, 3, 4, 9, 30, 0, 0, time.Local)
	}

	ui := &fakeUpdateUI{}
	s.emit = func(name string, data any) {
		ui.events = append(ui.events, name)
		ui.data = append(ui.data, data)
	}
	s.quit = func() { ui.quit = true }
	s.start = func(appPath string) error {
		ui.started = append(ui.started, appPath)
		return nil
	}
	return s, ui
}

// withVersion 临时覆盖版本号并在测试结束后还原。
func withVersion(t *testing.T, v string) {
	t.Helper()
	old := version.Version
	version.Version = v
	t.Cleanup(func() { version.Version = old })
}

// tempAppBundle 创建并返回一个假的 .app 目录，exe 指向包内可执行文件。
func tempAppBundle(t *testing.T) (appDir, exe string) {
	t.Helper()
	appDir = filepath.Join(t.TempDir(), "跬步.app")
	contents := filepath.Join(appDir, "Contents", "MacOS")
	if err := os.MkdirAll(contents, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contents, "kuibu"), []byte("old-bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	return appDir, filepath.Join(contents, "kuibu")
}

func TestInfoDevVersion(t *testing.T) {
	withVersion(t, "0.0.0-dev")
	s, _ := newTestUpdate(t)
	s.exe = func() (string, error) { return "/tmp/not-in-app", nil }

	info := s.Info()
	if info.Current != "0.0.0-dev" {
		t.Errorf("Current = %q", info.Current)
	}
	if !info.IsDev || info.CanSelfUpdate {
		t.Errorf("开发版应 IsDev=true 且 CanSelfUpdate=false: %+v", info)
	}
	if info.DevReason == "" || info.HasToken || info.TokenSource != "未找到" || info.SavedToken {
		t.Errorf("开发版状态不符: %+v", info)
	}
	if !info.AutoCheck {
		t.Errorf("未配置时 AutoCheck 应默认 true")
	}
}

func TestInfoReleaseAndSavedToken(t *testing.T) {
	withVersion(t, "0.2.0")
	s, _ := newTestUpdate(t)
	_, exe := tempAppBundle(t)
	s.exe = func() (string, error) { return exe, nil }

	if err := config.SaveGitHubToken(s.env, "saved-token"); err != nil {
		t.Fatal(err)
	}

	info := s.Info()
	if info.IsDev || !info.CanSelfUpdate {
		t.Errorf("打包版本应可自更新: %+v", info)
	}
	if info.DevReason != "" {
		t.Errorf("DevReason = %q", info.DevReason)
	}
	if !info.HasToken || info.TokenSource != "设置中填写" || !info.SavedToken {
		t.Errorf("token 状态不符: %+v", info)
	}
}

func TestInfoGHLoginSource(t *testing.T) {
	withVersion(t, "0.2.0")
	s, _ := newTestUpdate(t)
	_, exe := tempAppBundle(t)
	s.exe = func() (string, error) { return exe, nil }
	s.lookPath = func(string) (string, error) { return "/opt/homebrew/bin/gh", nil }
	s.run = func(string, ...string) ([]byte, error) { return []byte("gh-token"), nil }

	info := s.Info()
	if !info.HasToken || info.TokenSource != "gh 登录" {
		t.Errorf("token 来源 = %q, HasToken=%v，期望 gh 登录", info.TokenSource, info.HasToken)
	}
	if info.SavedToken {
		t.Errorf("gh 登录不应算作已保存手填 token")
	}
}

func TestCheckSuccess(t *testing.T) {
	withVersion(t, "0.1.0")
	s, _ := newTestUpdate(t)
	s.getenv = func(string) string { return "env-token" }

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer env-token" {
			t.Errorf("Authorization = %q", got)
		}
		fmt.Fprint(w, `{
			"tag_name": "v0.2.0",
			"name": "v0.2.0",
			"body": "更新内容",
			"html_url": "https://github.com/axu4/kuibu/releases/tag/v0.2.0",
			"published_at": "2025-01-02T03:04:05Z",
			"draft": false,
			"prerelease": false,
			"assets": []
		}`)
	}))
	defer srv.Close()

	s.repo = "axu4/kuibu"
	s.baseURL = srv.URL
	result, err := s.Check()
	if err != nil {
		t.Fatalf("Check 返回错误: %v", err)
	}
	if result.Current != "0.1.0" || result.Latest != "0.2.0" || !result.HasUpdate {
		t.Errorf("CheckResult = %+v", result)
	}
	if result.Notes != "更新内容" || result.HTMLURL == "" || result.CheckedAt != "2026-03-04T09:30:00+08:00" {
		t.Errorf("CheckResult 字段不符: %+v", result)
	}
}

func TestCheckNoToken(t *testing.T) {
	s, _ := newTestUpdate(t)
	_, err := s.Check()
	if err == nil || !strings.Contains(err.Error(), "未找到 GitHub token") {
		t.Fatalf("无 token 错误 = %v", err)
	}
}

func TestSetAutoCheckPreservesOtherFields(t *testing.T) {
	s, _ := newTestUpdate(t)
	if err := config.SaveDefaultWorkspace(s.env, "/tmp/ws"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAutoCheck(false); err != nil {
		t.Fatalf("SetAutoCheck 返回错误: %v", err)
	}
	cfg, err := config.Load(s.env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AutoCheckUpdateEnabled() {
		t.Errorf("AutoCheckUpdate 应为 false")
	}
	if cfg.Workspace != "/tmp/ws" {
		t.Errorf("Workspace = %q，期望保留 /tmp/ws", cfg.Workspace)
	}
}

func TestSetTokenClear(t *testing.T) {
	s, _ := newTestUpdate(t)
	if err := s.SetToken("abc"); err != nil {
		t.Fatalf("SetToken 返回错误: %v", err)
	}
	if got, _ := config.LoadGitHubToken(s.env); got != "abc" {
		t.Errorf("保存的 token = %q", got)
	}
	if err := s.SetToken("  "); err != nil {
		t.Fatalf("清除 token 返回错误: %v", err)
	}
	if got, _ := config.LoadGitHubToken(s.env); got != "" {
		t.Errorf("清除后 token = %q", got)
	}
}

func TestApplySuccessReplacesAppAndRestarts(t *testing.T) {
	withVersion(t, "0.1.0")
	s, ui := newTestUpdate(t)
	s.getenv = func(string) string { return "env-token" }

	appDir, exe := tempAppBundle(t)
	s.exe = func() (string, error) { return exe, nil }
	oldInfo := filepath.Join(appDir, "Contents", "Info.plist")
	if err := os.WriteFile(oldInfo, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	zipBytes, sums := newTestReleaseZip(t, "new-info")
	srv := releaseServer(t, zipBytes, sums)
	defer srv.Close()
	s.repo = "axu4/kuibu"
	s.baseURL = srv.URL

	if err := s.Apply(); err != nil {
		t.Fatalf("Apply 返回错误: %v", err)
	}

	if !ui.quit {
		t.Errorf("Apply 成功后应调用 quit")
	}
	if len(ui.started) != 1 || ui.started[0] != appDir {
		t.Errorf("started = %v，期望 [%s]", ui.started, appDir)
	}
	data, err := os.ReadFile(oldInfo)
	if err != nil || string(data) != "new-info" {
		t.Errorf("Info.plist = %q, %v，期望 new-info", data, err)
	}

	stages := ui.progressStages()
	for _, want := range []string{"download", "verify", "extract", "replace", "restart"} {
		if !containsString(stages, want) {
			t.Errorf("progress 缺少阶段 %q，实际 %v", want, stages)
		}
	}
}

func TestApplyPrepareErrorLeavesCurrentAppUntouched(t *testing.T) {
	withVersion(t, "0.1.0")
	s, ui := newTestUpdate(t)
	s.getenv = func(string) string { return "env-token" }

	appDir, exe := tempAppBundle(t)
	s.exe = func() (string, error) { return exe, nil }
	oldInfo := filepath.Join(appDir, "Contents", "Info.plist")
	if err := os.WriteFile(oldInfo, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 最新 Release 引用的资产 404，Prepare 在下载阶段就会失败。
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/axu4/kuibu/releases/latest" {
			fmt.Fprint(w, releaseJSON(
				srv.URL+"/assets/SHA256SUMS",
				srv.URL+"/assets/kuibu-macos-arm64.zip",
			))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	s.repo = "axu4/kuibu"
	s.baseURL = srv.URL

	err := s.Apply()
	if err == nil {
		t.Fatal("Apply 应返回错误")
	}
	if ui.quit || len(ui.started) != 0 {
		t.Errorf("失败时不应退出或启动新版本: quit=%v started=%v", ui.quit, ui.started)
	}
	data, readErr := os.ReadFile(oldInfo)
	if readErr != nil || string(data) != "old" {
		t.Errorf("失败后当前 App 不应被改动: %q, %v", data, readErr)
	}
}

// newTestReleaseZip 构造一个包含新 .app 的 zip 与对应 SHA256SUMS 文本。
func newTestReleaseZip(t *testing.T, infoContent string) (zipBytes []byte, sums string) {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entries := []struct {
		name    string
		content string
		perm    os.FileMode
	}{
		{"跬步.app/", "", 0o755},
		{"跬步.app/Contents/", "", 0o755},
		{"跬步.app/Contents/Info.plist", infoContent, 0o644},
		{"跬步.app/Contents/MacOS/", "", 0o755},
		{"跬步.app/Contents/MacOS/kuibu", "new-bin", 0o755},
	}
	for _, e := range entries {
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.name == "" {
			continue
		}
		if e.content == "" && strings.HasSuffix(e.name, "/") {
			hdr.SetMode(os.ModeDir | e.perm)
			if _, err := zw.CreateHeader(hdr); err != nil {
				t.Fatal(err)
			}
			continue
		}
		hdr.SetMode(e.perm)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(e.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zipBytes = buf.Bytes()
	sum := sha256.Sum256(zipBytes)
	return zipBytes, fmt.Sprintf("%x  kuibu-macos-arm64.zip\n", sum)
}

// releaseJSON 生成 Latest 接口返回的 Release JSON。
func releaseJSON(sumsURL, zipURL string) string {
	return fmt.Sprintf(`{
		"tag_name": "v0.2.0",
		"name": "v0.2.0",
		"body": "更新内容",
		"html_url": "https://github.com/axu4/kuibu/releases/tag/v0.2.0",
		"published_at": "2025-01-02T03:04:05Z",
		"draft": false,
		"prerelease": false,
		"assets": [
			{"name": "SHA256SUMS", "size": 64, "url": %q, "browser_download_url": ""},
			{"name": "kuibu-macos-arm64.zip", "size": 512, "url": %q, "browser_download_url": ""}
		]
	}`, sumsURL, zipURL)
}

// releaseServer 启动一个同时提供 Latest 与资产下载的 httptest server。
func releaseServer(t *testing.T, zipBytes []byte, sums string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/axu4/kuibu/releases/latest":
			fmt.Fprint(w, releaseJSON(
				"http://"+r.Host+"/assets/SHA256SUMS",
				"http://"+r.Host+"/assets/kuibu-macos-arm64.zip",
			))
		case "/assets/SHA256SUMS":
			fmt.Fprint(w, sums)
		case "/assets/kuibu-macos-arm64.zip":
			_, _ = w.Write(zipBytes)
		default:
			http.NotFound(w, r)
		}
	}))
}

// 小型断言辅助，避免测试文件引入额外依赖。
func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
