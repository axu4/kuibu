package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Asset 是 GitHub Release 里的一个附件，对应 REST JSON 字段。
type Asset struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	URL                string `json:"url"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Release 是 GitHub REST API 的 Release 对象（只取需要的字段）。
type Release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	Assets      []Asset   `json:"assets"`
}

// Client 是面向 GitHub Releases 的更新客户端。
type Client struct {
	// Repo 是仓库全名，如 "axu4/kuibu"。
	Repo string
	// Token 是可选 GitHub token（私有仓库必填）。
	Token string
	// HTTP 是自定义 http.Client，nil 时使用 http.DefaultClient。
	HTTP *http.Client
	// BaseURL 默认是 https://api.github.com，测试用 httptest 覆盖。
	BaseURL string
}

// baseURL 返回 API 基础地址。
func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return "https://api.github.com"
}

// httpClient 返回实际使用的 http.Client。
func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// setCommonHeaders 设置所有请求都带上的头。token 非空时带 Bearer 认证。
func (c *Client) setCommonHeaders(req *http.Request) {
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "kuibu-updater")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
}

// Latest 获取仓库的最新（非 draft、非 prerelease）Release。
func (c *Client) Latest(ctx context.Context) (*Release, error) {
	url := c.baseURL() + "/repos/" + c.Repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}
	c.setCommonHeaders(req)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 GitHub Releases 失败: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var rel Release
		if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
			return nil, fmt.Errorf("解析 GitHub 响应失败: %w", err)
		}
		return &rel, nil
	case http.StatusNotFound:
		// 私有仓库无 token 或 token 无读权限时，GitHub 也返回 404 而非 403。
		return nil, fmt.Errorf("未找到最新 Release（HTTP 404）；私有仓库无 token 或 token 无权限时也会返回 404，请确认 GitHub token 有该仓库读权限")
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("GitHub token 无效或已过期（HTTP 401）")
	case http.StatusForbidden:
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return nil, fmt.Errorf("GitHub API 速率限制已用尽（HTTP 403，X-RateLimit-Remaining=0），请稍后再试")
		}
		return nil, fmt.Errorf("GitHub API 拒绝访问（HTTP 403），请检查 token 权限与速率限制")
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("GitHub API 返回状态 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
}

// Download 把 asset 下载到 dst。
//
// 私有仓库走 asset 的 API URL（/repos/{repo}/releases/assets/{id}）并带
// Accept: application/octet-stream 与 token；GitHub 会 302 到 S3。跨域重定向时
// Go 会自动丢掉 Authorization 头，这正是我们需要的，不要手动再加到 S3 请求上。
func (c *Client) Download(ctx context.Context, asset Asset, dst io.Writer) error {
	url := asset.URL
	if url == "" {
		url = asset.BrowserDownloadURL
	}
	if url == "" {
		return fmt.Errorf("资产 %q 没有下载地址", asset.Name)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("构造下载请求失败: %w", err)
	}
	c.setCommonHeaders(req)
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("下载 %s 失败: %w", asset.Name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("下载 %s 失败（HTTP %d）: %s", asset.Name, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if _, err := io.Copy(dst, resp.Body); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", asset.Name, err)
	}
	return nil
}

// FindAsset 在 Release 里按文件名找资产。
func FindAsset(rel *Release, name string) (Asset, bool) {
	if rel == nil {
		return Asset{}, false
	}
	for _, a := range rel.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}
