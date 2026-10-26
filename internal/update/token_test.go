package update

import (
	"errors"
	"testing"
)

func TestFindTokenEnv(t *testing.T) {
	getenv := func(k string) string {
		if k == "GITHUB_TOKEN" {
			return "env-token"
		}
		return ""
	}
	tok, src := FindToken(getenv, "cfg-token", nil, nil)
	if tok != "env-token" {
		t.Errorf("token = %q，期望 env-token", tok)
	}
	if src != "环境变量 GITHUB_TOKEN" {
		t.Errorf("来源 = %q", src)
	}
}

func TestFindTokenGHBeforeConfig(t *testing.T) {
	getenv := func(k string) string {
		if k == "GH_TOKEN" {
			return "gh-env-token"
		}
		return ""
	}
	tok, src := FindToken(getenv, "cfg-token", nil, nil)
	if tok != "gh-env-token" {
		t.Errorf("token = %q，期望 gh-env-token", tok)
	}
	if src != "环境变量 GH_TOKEN" {
		t.Errorf("来源 = %q", src)
	}
}

func TestFindTokenConfig(t *testing.T) {
	tok, src := FindToken(func(string) string { return "" }, "  cfg-token  ", nil, nil)
	if tok != "cfg-token" {
		t.Errorf("token = %q，期望 cfg-token", tok)
	}
	if src != "设置中填写" {
		t.Errorf("来源 = %q", src)
	}
}

func TestFindTokenGHCli(t *testing.T) {
	lookPath := func(name string) (string, error) {
		if name == "gh" {
			return "/opt/homebrew/bin/gh", nil
		}
		return "", errors.New("not found")
	}
	run := func(name string, args ...string) ([]byte, error) {
		if name == "/opt/homebrew/bin/gh" && len(args) == 2 && args[0] == "auth" && args[1] == "token" {
			return []byte("gh-token\n"), nil
		}
		return nil, errors.New("not logged in")
	}
	tok, src := FindToken(nil, "", lookPath, run)
	if tok != "gh-token" {
		t.Errorf("token = %q，期望 gh-token", tok)
	}
	if src != "gh 登录" {
		t.Errorf("来源 = %q", src)
	}
}

func TestFindTokenGHAbsoluteFallback(t *testing.T) {
	lookPath := func(name string) (string, error) {
		return "", errors.New("not found")
	}
	run := func(name string, args ...string) ([]byte, error) {
		if name == "/usr/local/bin/gh" {
			return []byte("gh-abs-token\n"), nil
		}
		return nil, errors.New("not logged in")
	}
	tok, src := FindToken(nil, "", lookPath, run)
	if tok != "gh-abs-token" {
		t.Errorf("token = %q，期望 gh-abs-token", tok)
	}
	if src != "gh 登录" {
		t.Errorf("来源 = %q", src)
	}
}

func TestFindTokenNone(t *testing.T) {
	tok, src := FindToken(
		func(string) string { return "" },
		"",
		func(string) (string, error) { return "", errors.New("not found") },
		func(string, ...string) ([]byte, error) { return nil, errors.New("not logged in") },
	)
	if tok != "" || src != "" {
		t.Errorf("未找到时 token/source = %q/%q，期望空", tok, src)
	}
}
