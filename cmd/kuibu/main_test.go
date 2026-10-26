package main

import (
	"bytes"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

func TestRunVersionJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"version", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0；stderr=%q", code, stderr.String())
	}

	out := strings.TrimSpace(stdout.String())
	var info struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Go      string `json:"go"`
		OS      string `json:"os"`
		Arch    string `json:"arch"`
	}
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		t.Fatalf("输出不是合法 JSON: %v；输出=%q", err, out)
	}

	if info.Name != "kuibu" {
		t.Errorf("name = %q，期望 %q", info.Name, "kuibu")
	}
	if info.Version != version {
		t.Errorf("version = %q，期望 %q", info.Version, version)
	}
	if info.Go != runtime.Version() {
		t.Errorf("go = %q，期望 %q", info.Go, runtime.Version())
	}
	if info.OS != runtime.GOOS {
		t.Errorf("os = %q，期望 %q", info.OS, runtime.GOOS)
	}
	if info.Arch != runtime.GOARCH {
		t.Errorf("arch = %q，期望 %q", info.Arch, runtime.GOARCH)
	}
}

func TestRunVersionPlain(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0；stderr=%q", code, stderr.String())
	}

	want := "kuibu " + version + "\n"
	if got := stdout.String(); got != want {
		t.Errorf("输出 = %q，期望 %q", got, want)
	}
}

func TestRunNoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("退出码 = %d，期望 1", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout 应为空，实际 = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "用法") {
		t.Errorf("stderr 应包含用法说明，实际 = %q", stderr.String())
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"nope"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("退出码 = %d，期望 1", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout 应为空，实际 = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "用法") {
		t.Errorf("stderr 应包含用法说明，实际 = %q", stderr.String())
	}
}

func TestRunVersionUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"version", "--unknown"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("退出码 = %d，期望 1", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout 应为空，实际 = %q", stdout.String())
	}
}
