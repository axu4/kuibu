package config

import (
	"os"
	"path/filepath"
	"testing"
)

// testEnv 返回一个只认识 vars 的 env 函数，避免读取进程真实环境变量。
func testEnv(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

// newConfigDir 返回隔离的配置目录，并把它注入 env，保证不读写真实用户配置。
func newConfigDir(t *testing.T, vars map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	vars["KUIBU_CONFIG_DIR"] = dir
	return dir
}

func TestLocatePrefersWorkspaceEnv(t *testing.T) {
	vars := map[string]string{"KUIBU_WORKSPACE": "/tmp/from-env"}
	dir := newConfigDir(t, vars)
	// 用户配置里也写一个默认工作台，验证环境变量优先。
	if err := SaveDefaultWorkspace(testEnv(vars), "/tmp/from-config"); err != nil {
		t.Fatalf("写入默认工作台失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err != nil {
		t.Fatalf("配置文件未写入 KUIBU_CONFIG_DIR: %v", err)
	}

	got, err := Locate(testEnv(vars))
	if err != nil {
		t.Fatalf("Locate 返回错误: %v", err)
	}
	if want := "/tmp/from-env"; got != want {
		t.Errorf("Locate = %q，期望 %q", got, want)
	}
}

func TestLocateFallsBackToUserConfig(t *testing.T) {
	vars := map[string]string{}
	newConfigDir(t, vars)
	if err := SaveDefaultWorkspace(testEnv(vars), "/tmp/from-config"); err != nil {
		t.Fatalf("写入默认工作台失败: %v", err)
	}

	got, err := Locate(testEnv(vars))
	if err != nil {
		t.Fatalf("Locate 返回错误: %v", err)
	}
	if want := "/tmp/from-config"; got != want {
		t.Errorf("Locate = %q，期望 %q", got, want)
	}
}

func TestLocateReturnsEmptyWhenUnconfigured(t *testing.T) {
	vars := map[string]string{}
	newConfigDir(t, vars)

	got, err := Locate(testEnv(vars))
	if err != nil {
		t.Fatalf("Locate 返回错误: %v", err)
	}
	if got != "" {
		t.Errorf("Locate = %q，期望空字符串", got)
	}
}

func TestSaveDefaultWorkspaceCreatesConfigDir(t *testing.T) {
	vars := map[string]string{}
	// 指向尚不存在的多级子目录，验证 Save 会创建父目录。
	dir := filepath.Join(t.TempDir(), "nested", "config")
	vars["KUIBU_CONFIG_DIR"] = dir

	if err := SaveDefaultWorkspace(testEnv(vars), "/tmp/ws"); err != nil {
		t.Fatalf("SaveDefaultWorkspace 返回错误: %v", err)
	}

	cfg, err := Load(testEnv(vars))
	if err != nil {
		t.Fatalf("Load 返回错误: %v", err)
	}
	if want := "/tmp/ws"; cfg.Workspace != want {
		t.Errorf("Workspace = %q，期望 %q", cfg.Workspace, want)
	}

	// 再写一次不同路径，验证覆盖而不是追加。
	if err := SaveDefaultWorkspace(testEnv(vars), "/tmp/ws2"); err != nil {
		t.Fatalf("第二次 SaveDefaultWorkspace 返回错误: %v", err)
	}
	got, err := DefaultWorkspace(testEnv(vars))
	if err != nil {
		t.Fatalf("DefaultWorkspace 返回错误: %v", err)
	}
	if want := "/tmp/ws2"; got != want {
		t.Errorf("DefaultWorkspace = %q，期望 %q", got, want)
	}
}
