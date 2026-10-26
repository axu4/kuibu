package update

import "strings"

// FindToken 按优先级查找 GitHub token，返回 (token, 来源中文描述)。
//
// 顺序：
//  1. 环境变量 GITHUB_TOKEN、GH_TOKEN（getenv 注入，便于测试）；
//  2. cfgToken（设置中填写的 token）；
//  3. `gh auth token`。macOS GUI App 的 PATH 很短，除了 lookPath 找到的 gh，
//     再额外尝试 /opt/homebrew/bin/gh 与 /usr/local/bin/gh。
//
// 任何地方都不会打印或记录 token 本身。
func FindToken(getenv func(string) string, cfgToken string, lookPath func(string) (string, error), run func(name string, args ...string) ([]byte, error)) (token, source string) {
	if getenv != nil {
		if t := strings.TrimSpace(getenv("GITHUB_TOKEN")); t != "" {
			return t, "环境变量 GITHUB_TOKEN"
		}
		if t := strings.TrimSpace(getenv("GH_TOKEN")); t != "" {
			return t, "环境变量 GH_TOKEN"
		}
	}
	if t := strings.TrimSpace(cfgToken); t != "" {
		return t, "设置中填写"
	}

	candidates := make([]string, 0, 3)
	if lookPath != nil {
		if p, err := lookPath("gh"); err == nil && strings.TrimSpace(p) != "" {
			candidates = append(candidates, p)
		}
	}
	candidates = append(candidates, "/opt/homebrew/bin/gh", "/usr/local/bin/gh")

	seen := make(map[string]bool)
	for _, gh := range candidates {
		if gh == "" || seen[gh] {
			continue
		}
		seen[gh] = true
		if run == nil {
			continue
		}
		out, err := run(gh, "auth", "token")
		if err != nil {
			continue
		}
		if t := strings.TrimSpace(string(out)); t != "" {
			return t, "gh 登录"
		}
	}
	return "", ""
}
