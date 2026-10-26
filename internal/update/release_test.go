package update

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestLatestSuccess(t *testing.T) {
	want := `{
		"tag_name": "v0.2.0",
		"name": "v0.2.0",
		"body": "更新内容",
		"html_url": "https://github.com/axu4/kuibu/releases/tag/v0.2.0",
		"published_at": "2025-01-02T03:04:05Z",
		"draft": false,
		"prerelease": false,
		"assets": [
			{"name": "kuibu-macos-arm64.zip", "size": 123, "url": "https://api.github.com/repos/axu4/kuibu/releases/assets/1", "browser_download_url": "https://github.com/axu4/kuibu/releases/download/v0.2.0/kuibu-macos-arm64.zip"}
		]
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/axu4/kuibu/releases/latest" {
			t.Errorf("请求路径 = %q", r.URL.Path)
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept = %q", got)
		}
		if got := r.Header.Get("X-GitHub-Api-Version"); got != "2022-11-28" {
			t.Errorf("X-GitHub-Api-Version = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		if r.Header.Get("User-Agent") == "" {
			t.Errorf("缺少 User-Agent")
		}
		fmt.Fprint(w, want)
	}))
	defer srv.Close()

	c := &Client{Repo: "axu4/kuibu", Token: "test-token", BaseURL: srv.URL}
	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest 返回错误: %v", err)
	}
	if rel.TagName != "v0.2.0" {
		t.Errorf("TagName = %q", rel.TagName)
	}
	if len(rel.Assets) != 1 || rel.Assets[0].Name != "kuibu-macos-arm64.zip" {
		t.Errorf("Assets = %+v", rel.Assets)
	}
}

func TestLatestNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := &Client{Repo: "axu4/kuibu", BaseURL: srv.URL}
	_, err := c.Latest(context.Background())
	if err == nil {
		t.Fatal("Latest 应返回错误")
	}
	if !strings.Contains(err.Error(), "请确认 GitHub token 有该仓库读权限") {
		t.Errorf("404 错误缺少提示: %v", err)
	}
}

func TestLatestUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := (&Client{Repo: "axu4/kuibu", BaseURL: srv.URL}).Latest(context.Background())
	if err == nil || !strings.Contains(err.Error(), "token 无效") {
		t.Errorf("401 错误 = %v", err)
	}
}

func TestLatestForbiddenRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := (&Client{Repo: "axu4/kuibu", BaseURL: srv.URL}).Latest(context.Background())
	if err == nil || !strings.Contains(err.Error(), "速率限制") {
		t.Errorf("403 速率限制错误 = %v", err)
	}
}

func TestDownloadFollowsRedirectAndDropsAuthOnCrossDomain(t *testing.T) {
	var targetSawAuth bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			targetSawAuth = true
		}
		fmt.Fprint(w, "hello-update")
	}))
	defer target.Close()

	// 重定向到 localhost（而非 127.0.0.1）以模拟跨域：
	// 两个 httptest 都监听 127.0.0.1，但域名不同时 Go 才会丢掉 Authorization。
	targetURL, _ := url.Parse(target.URL)
	crossDomainURL := "http://localhost:" + targetURL.Port()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("首请求 Authorization = %q，期望带 token", got)
		}
		if got := r.Header.Get("Accept"); got != "application/octet-stream" {
			t.Errorf("首请求 Accept = %q", got)
		}
		http.Redirect(w, r, crossDomainURL, http.StatusFound)
	}))
	defer redirector.Close()

	c := &Client{Repo: "axu4/kuibu", Token: "secret"}
	var sb strings.Builder
	asset := Asset{Name: "kuibu-macos-arm64.zip", URL: redirector.URL + "/repos/axu4/kuibu/releases/assets/1"}
	if err := c.Download(context.Background(), asset, &sb); err != nil {
		t.Fatalf("Download 返回错误: %v", err)
	}
	if sb.String() != "hello-update" {
		t.Errorf("下载内容 = %q", sb.String())
	}
	if targetSawAuth {
		t.Errorf("跨域重定向后不应携带 Authorization")
	}
}

func TestDownloadHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer srv.Close()

	var sb strings.Builder
	asset := Asset{Name: "x.zip", URL: srv.URL}
	if err := (&Client{}).Download(context.Background(), asset, &sb); err == nil {
		t.Fatal("Download 应返回错误")
	}
}

func TestFindAsset(t *testing.T) {
	rel := &Release{Assets: []Asset{
		{Name: "kuibu-macos-arm64.zip"},
		{Name: "SHA256SUMS"},
	}}
	if a, ok := FindAsset(rel, "SHA256SUMS"); !ok || a.Name != "SHA256SUMS" {
		t.Errorf("FindAsset 未找到 SHA256SUMS: %+v, %v", a, ok)
	}
	if _, ok := FindAsset(rel, "nope"); ok {
		t.Errorf("FindAsset 不应找到 nope")
	}
	if _, ok := FindAsset(nil, "x"); ok {
		t.Errorf("FindAsset(nil) 不应找到任何资产")
	}
}
