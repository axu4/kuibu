package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axu4/kuibu/internal/store"
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

func TestQuickNoteShortcutDefaults(t *testing.T) {
	vars := map[string]string{}
	newConfigDir(t, vars)

	got, err := QuickNoteShortcut(testEnv(vars))
	if err != nil {
		t.Fatalf("QuickNoteShortcut 返回错误: %v", err)
	}
	if want := DefaultQuickNoteShortcut; got != want {
		t.Errorf("QuickNoteShortcut = %q，期望 %q", got, want)
	}
}

func TestQuickNoteShortcutSaveAndRead(t *testing.T) {
	vars := map[string]string{}
	newConfigDir(t, vars)

	if err := SaveQuickNoteShortcut(testEnv(vars), "  Ctrl+Shift+K  "); err != nil {
		t.Fatalf("SaveQuickNoteShortcut 返回错误: %v", err)
	}
	got, err := QuickNoteShortcut(testEnv(vars))
	if err != nil {
		t.Fatalf("QuickNoteShortcut 返回错误: %v", err)
	}
	if want := "Ctrl+Shift+K"; got != want {
		t.Errorf("QuickNoteShortcut = %q，期望 %q", got, want)
	}
}

func TestSaveQuickNoteShortcutKeepsWorkspace(t *testing.T) {
	vars := map[string]string{}
	newConfigDir(t, vars)

	if err := SaveDefaultWorkspace(testEnv(vars), "/tmp/ws"); err != nil {
		t.Fatalf("写入默认工作台失败: %v", err)
	}
	if err := SaveQuickNoteShortcut(testEnv(vars), "Ctrl+Alt+Q"); err != nil {
		t.Fatalf("保存快捷键失败: %v", err)
	}

	cfg, err := Load(testEnv(vars))
	if err != nil {
		t.Fatalf("Load 返回错误: %v", err)
	}
	if want := "/tmp/ws"; cfg.Workspace != want {
		t.Errorf("Workspace = %q，期望 %q", cfg.Workspace, want)
	}
	if want := "Ctrl+Alt+Q"; cfg.QuickNoteShortcut != want {
		t.Errorf("QuickNoteShortcut = %q，期望 %q", cfg.QuickNoteShortcut, want)
	}
}

func TestSaveDefaultWorkspaceKeepsQuickNoteShortcut(t *testing.T) {
	vars := map[string]string{}
	newConfigDir(t, vars)

	if err := SaveQuickNoteShortcut(testEnv(vars), "Ctrl+Alt+Q"); err != nil {
		t.Fatalf("保存快捷键失败: %v", err)
	}
	if err := SaveDefaultWorkspace(testEnv(vars), "/tmp/ws2"); err != nil {
		t.Fatalf("写入默认工作台失败: %v", err)
	}

	cfg, err := Load(testEnv(vars))
	if err != nil {
		t.Fatalf("Load 返回错误: %v", err)
	}
	if want := "/tmp/ws2"; cfg.Workspace != want {
		t.Errorf("Workspace = %q，期望 %q", cfg.Workspace, want)
	}
	if want := "Ctrl+Alt+Q"; cfg.QuickNoteShortcut != want {
		t.Errorf("QuickNoteShortcut = %q，期望 %q", cfg.QuickNoteShortcut, want)
	}
}

func TestSaveQuickNoteShortcutRestoresDefault(t *testing.T) {
	vars := map[string]string{}
	dir := newConfigDir(t, vars)

	if err := SaveDefaultWorkspace(testEnv(vars), "/tmp/ws"); err != nil {
		t.Fatalf("写入默认工作台失败: %v", err)
	}
	if err := SaveQuickNoteShortcut(testEnv(vars), "Ctrl+Alt+Q"); err != nil {
		t.Fatalf("保存快捷键失败: %v", err)
	}
	// 空白表示恢复默认。
	if err := SaveQuickNoteShortcut(testEnv(vars), "   "); err != nil {
		t.Fatalf("恢复默认失败: %v", err)
	}

	got, err := QuickNoteShortcut(testEnv(vars))
	if err != nil {
		t.Fatalf("QuickNoteShortcut 返回错误: %v", err)
	}
	if want := DefaultQuickNoteShortcut; got != want {
		t.Errorf("QuickNoteShortcut = %q，期望默认值 %q", got, want)
	}

	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("读取配置文件失败: %v", err)
	}
	if strings.Contains(string(data), "quickNoteShortcut") {
		t.Errorf("恢复默认后不应写入 quickNoteShortcut 字段：%s", data)
	}
	if !strings.Contains(string(data), `"workspace":"/tmp/ws"`) {
		t.Errorf("恢复默认后 workspace 丢失：%s", data)
	}
}

func TestAutoCheckUpdateEnabled(t *testing.T) {
	trueVal := true
	falseVal := false

	cases := []struct {
		name string
		cfg  UserConfig
		want bool
	}{
		{"未配置默认开启", UserConfig{}, true},
		{"显式开启", UserConfig{AutoCheckUpdate: &trueVal}, true},
		{"显式关闭", UserConfig{AutoCheckUpdate: &falseVal}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.AutoCheckUpdateEnabled(); got != tc.want {
				t.Errorf("AutoCheckUpdateEnabled() = %v，期望 %v", got, tc.want)
			}
		})
	}
}

func TestGitHubTokenSaveLoadAndDelete(t *testing.T) {
	vars := map[string]string{}
	dir := newConfigDir(t, vars)

	// 未保存时读取返回空字符串。
	got, err := LoadGitHubToken(testEnv(vars))
	if err != nil {
		t.Fatalf("LoadGitHubToken 返回错误: %v", err)
	}
	if got != "" {
		t.Errorf("未保存时 token = %q，期望空字符串", got)
	}

	// 保存后能读回，且文件权限为 0600。
	if err := SaveGitHubToken(testEnv(vars), "  ghp_secret_123  "); err != nil {
		t.Fatalf("SaveGitHubToken 返回错误: %v", err)
	}
	tokenPath := filepath.Join(dir, "github-token")
	info, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatalf("token 文件未写入: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("token 文件权限 = %o，期望 600", perm)
	}

	got, err = LoadGitHubToken(testEnv(vars))
	if err != nil {
		t.Fatalf("LoadGitHubToken 返回错误: %v", err)
	}
	if want := "ghp_secret_123"; got != want {
		t.Errorf("token = %q，期望 %q", got, want)
	}

	// 空字符串表示删除文件。
	if err := SaveGitHubToken(testEnv(vars), "   "); err != nil {
		t.Fatalf("删除 token 返回错误: %v", err)
	}
	if _, err := os.Stat(tokenPath); !os.IsNotExist(err) {
		t.Errorf("删除后 token 文件仍存在: %v", err)
	}
	got, err = LoadGitHubToken(testEnv(vars))
	if err != nil {
		t.Fatalf("删除后 LoadGitHubToken 返回错误: %v", err)
	}
	if got != "" {
		t.Errorf("删除后 token = %q，期望空字符串", got)
	}
}

func TestGitHubTokenNotInConfigJSON(t *testing.T) {
	vars := map[string]string{}
	dir := newConfigDir(t, vars)

	if err := SaveGitHubToken(testEnv(vars), "ghp_secret_456"); err != nil {
		t.Fatalf("SaveGitHubToken 返回错误: %v", err)
	}

	// 保存默认工作台会写 config.json，token 不应混进去。
	if err := SaveDefaultWorkspace(testEnv(vars), "/tmp/ws"); err != nil {
		t.Fatalf("写入默认工作台失败: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("读取 config.json 失败: %v", err)
	}
	if strings.Contains(string(data), "ghp_secret_456") {
		t.Errorf("config.json 不应包含 token：%s", data)
	}
}

func TestLoadOldConfigWithoutQuickNoteShortcut(t *testing.T) {
	vars := map[string]string{}
	dir := newConfigDir(t, vars)

	old := []byte("{\n  \"workspace\": \"/tmp/legacy\"\n}\n")
	if err := os.WriteFile(filepath.Join(dir, "config.json"), old, 0o644); err != nil {
		t.Fatalf("写入老配置失败: %v", err)
	}

	cfg, err := Load(testEnv(vars))
	if err != nil {
		t.Fatalf("Load 返回错误: %v", err)
	}
	if want := "/tmp/legacy"; cfg.Workspace != want {
		t.Errorf("Workspace = %q，期望 %q", cfg.Workspace, want)
	}
	if cfg.QuickNoteShortcut != "" {
		t.Errorf("QuickNoteShortcut = %q，期望空字符串", cfg.QuickNoteShortcut)
	}

	got, err := QuickNoteShortcut(testEnv(vars))
	if err != nil {
		t.Fatalf("QuickNoteShortcut 返回错误: %v", err)
	}
	if want := DefaultQuickNoteShortcut; got != want {
		t.Errorf("QuickNoteShortcut = %q，期望默认值 %q", got, want)
	}
}

func TestEnsureWorkspaceCreatesFallback(t *testing.T) {
	vars := map[string]string{}
	newConfigDir(t, vars)
	fallback := filepath.Join(t.TempDir(), "Documents", "跬步")

	dir, created, err := EnsureWorkspace(testEnv(vars), fallback)
	if err != nil {
		t.Fatalf("EnsureWorkspace 返回错误: %v", err)
	}
	if !created || dir != fallback {
		t.Fatalf("EnsureWorkspace = (%q, %v)，期望 (%q, true)", dir, created, fallback)
	}
	if _, err := os.Stat(filepath.Join(fallback, ".kuibu", "config.json")); err != nil {
		t.Fatalf("默认工作台未初始化: %v", err)
	}
	// 写入了用户配置：下次启动直接用它。
	got, err := Locate(testEnv(vars))
	if err != nil || got != fallback {
		t.Fatalf("Locate = (%q, %v)，期望 %q", got, err, fallback)
	}

	// 再次调用走已配置分支，不再新建。
	dir, created, err = EnsureWorkspace(testEnv(vars), fallback)
	if err != nil || created || dir != fallback {
		t.Fatalf("第二次 EnsureWorkspace = (%q, %v, %v)", dir, created, err)
	}
}

func TestEnsureWorkspaceReusesExistingFallback(t *testing.T) {
	vars := map[string]string{}
	newConfigDir(t, vars)
	fallback := filepath.Join(t.TempDir(), "跬步")
	if _, err := store.Init(fallback); err != nil {
		t.Fatalf("初始化工作台失败: %v", err)
	}

	dir, created, err := EnsureWorkspace(testEnv(vars), fallback)
	if err != nil || created || dir != fallback {
		t.Fatalf("EnsureWorkspace = (%q, %v, %v)，期望沿用已有工作台", dir, created, err)
	}
}

func TestEnsureWorkspaceKeepsConfigured(t *testing.T) {
	vars := map[string]string{}
	newConfigDir(t, vars)
	if err := SaveDefaultWorkspace(testEnv(vars), "/tmp/configured"); err != nil {
		t.Fatalf("写入默认工作台失败: %v", err)
	}
	fallback := filepath.Join(t.TempDir(), "跬步")

	dir, created, err := EnsureWorkspace(testEnv(vars), fallback)
	if err != nil || created || dir != "/tmp/configured" {
		t.Fatalf("EnsureWorkspace = (%q, %v, %v)，期望用已配置目录", dir, created, err)
	}
	if _, err := os.Stat(fallback); !os.IsNotExist(err) {
		t.Fatalf("已配置时不应创建默认目录: %v", err)
	}
}

func TestEnsureWorkspaceRefusesNonEmptyFallback(t *testing.T) {
	vars := map[string]string{}
	newConfigDir(t, vars)
	fallback := t.TempDir()
	if err := os.WriteFile(filepath.Join(fallback, "别人的文件.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := EnsureWorkspace(testEnv(vars), fallback); err == nil {
		t.Fatal("非空且不是工作台的目录应返回错误")
	}
	if got, _ := Locate(testEnv(vars)); got != "" {
		t.Fatalf("失败时不应写入配置，Locate = %q", got)
	}
}

func TestLinkSchemes(t *testing.T) {
	dir := t.TempDir()
	env := func(k string) string {
		if k == "KUIBU_CONFIG_DIR" {
			return dir
		}
		return ""
	}
	got, err := LinkSchemes(env)
	if err != nil || strings.Join(got, ",") != "http,https,mailto,message" {
		t.Fatalf("默认白名单 = %v, %v", got, err)
	}
	got, err = SaveLinkSchemes(env, []string{"HTTPS", "obsidian://"})
	if err != nil || strings.Join(got, ",") != "https,obsidian" {
		t.Fatalf("保存白名单 = %v, %v", got, err)
	}
	if got, _ := SaveLinkSchemes(env, []string{}); len(got) != 0 {
		t.Fatalf("清空白名单后应为空，得到 %v", got)
	}
	if got, _ := SaveLinkSchemes(env, nil); len(got) != 4 {
		t.Fatalf("nil 应恢复默认，得到 %v", got)
	}
	if _, err := SaveLinkSchemes(env, []string{"javascript"}); err == nil {
		t.Fatal("javascript 不应能加入白名单")
	}
}
