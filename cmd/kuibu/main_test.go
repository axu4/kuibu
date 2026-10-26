package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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

func TestBinaryInitAndNote(t *testing.T) {
	if testing.Short() {
		t.Skip("短模式跳过真实进程测试")
	}

	bin := filepath.Join(t.TempDir(), "kuibu")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("编译 kuibu 失败: %v\n%s", err, out)
	}

	configDir := t.TempDir()
	wsDir := filepath.Join(t.TempDir(), "ws")
	runBinary := func(args ...string) (string, string, int) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Env = append(os.Environ(), "KUIBU_CONFIG_DIR="+configDir)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		code := 0
		if err != nil {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("执行 %v 失败: %v", args, err)
			}
			code = exitErr.ExitCode()
		}
		return stdout.String(), stderr.String(), code
	}

	stdout, stderr, code := runBinary("init", wsDir, "--json")
	if code != 0 {
		t.Fatalf("init 退出码 = %d，期望 0；stderr=%s", code, stderr)
	}
	var initOut struct {
		Workspace string `json:"workspace"`
	}
	if err := json.Unmarshal([]byte(stdout), &initOut); err != nil {
		t.Fatalf("init 输出不是 JSON: %v；输出=%q", err, stdout)
	}

	stdout, stderr, code = runBinary("note", "new", "--workspace", wsDir, "--title", "真实进程笔记", "--json")
	if code != 0 {
		t.Fatalf("note new 退出码 = %d，期望 0；stderr=%s", code, stderr)
	}
	var note struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(stdout), &note); err != nil {
		t.Fatalf("note new 输出不是 JSON: %v；输出=%q", err, stdout)
	}
	if note.ID == "" || note.Title != "真实进程笔记" {
		t.Errorf("note new 输出 = %+v", note)
	}
	if _, err := os.Stat(filepath.Join(wsDir, "notes", note.ID+".md")); err != nil {
		t.Errorf("真实进程未写入笔记文件: %v", err)
	}

	// 工作台不存在时返回 10。
	_, stderr, code = runBinary("note", "list", "--workspace", filepath.Join(t.TempDir(), "nope"), "--json")
	if code != 10 {
		t.Errorf("未打开工作台退出码 = %d，期望 10；stderr=%s", code, stderr)
	}
}

func TestBinaryTodoAddAndList(t *testing.T) {
	if testing.Short() {
		t.Skip("短模式跳过真实进程测试")
	}

	bin := filepath.Join(t.TempDir(), "kuibu")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("编译 kuibu 失败: %v\n%s", err, out)
	}

	configDir := t.TempDir()
	wsDir := filepath.Join(t.TempDir(), "ws")
	runBinary := func(args ...string) (string, string, int) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Env = append(os.Environ(), "KUIBU_CONFIG_DIR="+configDir, "KUIBU_NOW=2026-10-26T10:00:00+08:00")
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		code := 0
		if err != nil {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("执行 %v 失败: %v", args, err)
			}
			code = exitErr.ExitCode()
		}
		return stdout.String(), stderr.String(), code
	}

	stdout, stderr, code := runBinary("init", wsDir, "--json")
	if code != 0 {
		t.Fatalf("init 退出码 = %d，期望 0；stderr=%s", code, stderr)
	}
	_ = stdout

	stdout, stderr, code = runBinary("todo", "add", "--workspace", wsDir, "明天 15:00 交周报", "#工作", "--json")
	if code != 0 {
		t.Fatalf("todo add 退出码 = %d，期望 0；stderr=%s", code, stderr)
	}
	var addOut struct {
		Todo struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Due   string `json:"due"`
		} `json:"todo"`
		Parsed struct {
			Due string `json:"due"`
		} `json:"parsed"`
	}
	if err := json.Unmarshal([]byte(stdout), &addOut); err != nil {
		t.Fatalf("todo add 输出不是 JSON: %v；输出=%q", err, stdout)
	}
	if addOut.Todo.ID == "" || addOut.Todo.Title != "交周报" || addOut.Todo.Due != "2026-10-27 15:00" {
		t.Errorf("todo add 输出 = %+v", addOut)
	}
	if addOut.Parsed.Due != "2026-10-27 15:00" {
		t.Errorf("parsed due = %q", addOut.Parsed.Due)
	}

	stdout, stderr, code = runBinary("todo", "list", "--workspace", wsDir, "--json")
	if code != 0 {
		t.Fatalf("todo list 退出码 = %d，期望 0；stderr=%s", code, stderr)
	}
	var listOut []map[string]any
	if err := json.Unmarshal([]byte(stdout), &listOut); err != nil {
		t.Fatalf("todo list 输出不是 JSON: %v；输出=%q", err, stdout)
	}
	if len(listOut) != 1 || listOut[0]["id"] != addOut.Todo.ID {
		t.Errorf("todo list 输出 = %v", listOut)
	}

	// 再建一条并完成，验证 --status done 只返回 done。
	stdout, stderr, code = runBinary("todo", "add", "--workspace", wsDir, "第二条", "--json")
	if code != 0 {
		t.Fatalf("todo add 第二条退出码 = %d，期望 0；stderr=%s", code, stderr)
	}
	var secondAdd struct {
		Todo struct {
			ID string `json:"id"`
		} `json:"todo"`
	}
	if err := json.Unmarshal([]byte(stdout), &secondAdd); err != nil {
		t.Fatalf("todo add 第二条输出不是 JSON: %v；输出=%q", err, stdout)
	}
	if secondAdd.Todo.ID == "" {
		t.Fatalf("第二条 todo id 为空")
	}

	stdout, stderr, code = runBinary("todo", "done", secondAdd.Todo.ID, "--workspace", wsDir)
	if code != 0 {
		t.Fatalf("todo done 退出码 = %d，期望 0；stderr=%s", code, stderr)
	}

	stdout, stderr, code = runBinary("todo", "list", "--workspace", wsDir, "--status", "done", "--json")
	if code != 0 {
		t.Fatalf("todo list --status done 退出码 = %d，期望 0；stderr=%s", code, stderr)
	}
	var doneOut []map[string]any
	if err := json.Unmarshal([]byte(stdout), &doneOut); err != nil {
		t.Fatalf("todo list --status done 输出不是 JSON: %v；输出=%q", err, stdout)
	}
	if len(doneOut) != 1 || doneOut[0]["id"] != secondAdd.Todo.ID {
		t.Errorf("todo list --status done 输出 = %v，期望只含 %s", doneOut, secondAdd.Todo.ID)
	}

	// 不存在的待办返回待办专用文案，不带绝对路径。
	_, stderr, code = runBinary("todo", "show", "nope", "--workspace", wsDir)
	if code != 1 {
		t.Errorf("todo show nope 退出码 = %d，期望 1；stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "待办不存在: nope") || strings.Contains(stderr, "todos") {
		t.Errorf("todo show nope stderr = %q", stderr)
	}
}
