// Package config 负责用户配置（默认工作台）与工作台定位，供 CLI 与 App 共用。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/axu4/kuibu/internal/store"
)

// UserConfig 是用户配置文件 $KUIBU_CONFIG_DIR/config.json 的内容。
type UserConfig struct {
	// Workspace 是默认工作台目录的绝对路径。
	Workspace string `json:"workspace"`
	// QuickNoteShortcut 是随手记全局快捷键（Wails v3 加速键语法），空表示用默认值。
	QuickNoteShortcut string `json:"quickNoteShortcut,omitempty"`
	// AutoCheckUpdate 是否自动检查在线更新；nil 视为 true。
	AutoCheckUpdate *bool `json:"autoCheckUpdate,omitempty"`
}

// AutoCheckUpdateEnabled 返回是否自动检查更新；未配置（nil）时默认为 true。
func (c UserConfig) AutoCheckUpdateEnabled() bool {
	return c.AutoCheckUpdate == nil || *c.AutoCheckUpdate
}

// DefaultQuickNoteShortcut 是随手记全局快捷键的默认值。
// Wails v3 加速键语法，macOS 上即 ⌃⌥N。
const DefaultQuickNoteShortcut = "Ctrl+Alt+N"

// Path 返回用户配置文件路径。
//
// 优先使用 KUIBU_CONFIG_DIR，未设置时用 os.UserConfigDir()/kuibu。
func Path(env func(string) string) (string, error) {
	if dir := env("KUIBU_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "config.json"), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("定位用户配置目录失败: %w", err)
	}
	return filepath.Join(base, "kuibu", "config.json"), nil
}

// Load 读取用户配置；文件不存在时返回空配置。
func Load(env func(string) string) (UserConfig, error) {
	var cfg UserConfig
	path, err := Path(env)
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("读取用户配置失败: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("解析用户配置失败: %w", err)
	}
	return cfg, nil
}

// Save 原子地写入用户配置（同目录临时文件 + rename），避免并发写坏。
func Save(env func(string) string, cfg UserConfig) error {
	path, err := Path(env)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建用户配置目录失败: %w", err)
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("序列化用户配置失败: %w", err)
	}
	data = append(data, '\n')
	if err := writeFileAtomic(path, data); err != nil {
		return fmt.Errorf("写入用户配置失败: %w", err)
	}
	return nil
}

// writeFileAtomic 以「同目录临时文件 + rename」的方式原子写入文件。
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()

	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}

	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("写入临时文件失败: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("同步临时文件失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("关闭临时文件失败: %w", err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("设置文件权限失败: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("替换目标文件失败: %w", err)
	}
	return nil
}

// SaveDefaultWorkspace 把 dir 写为默认工作台。
func SaveDefaultWorkspace(env func(string) string, dir string) error {
	cfg, err := Load(env)
	if err != nil {
		return err
	}
	cfg.Workspace = dir
	return Save(env, cfg)
}

// QuickNoteShortcut 返回随手记全局快捷键，未配置时返回默认值。
func QuickNoteShortcut(env func(string) string) (string, error) {
	cfg, err := Load(env)
	if err != nil {
		return "", err
	}
	if cfg.QuickNoteShortcut == "" {
		return DefaultQuickNoteShortcut, nil
	}
	return cfg.QuickNoteShortcut, nil
}

// SaveQuickNoteShortcut 读-改-写随手记全局快捷键，保留工作台等其他字段。
// accel 去空白后为空表示恢复默认（写空字符串即省略该字段）。
func SaveQuickNoteShortcut(env func(string) string, accel string) error {
	cfg, err := Load(env)
	if err != nil {
		return err
	}
	cfg.QuickNoteShortcut = strings.TrimSpace(accel)
	return Save(env, cfg)
}

// DefaultWorkspace 返回用户配置里的默认工作台目录，未配置时返回空字符串。
func DefaultWorkspace(env func(string) string) (string, error) {
	cfg, err := Load(env)
	if err != nil {
		return "", err
	}
	return cfg.Workspace, nil
}

// Resolve 按优先级定位工作台：
// override（--workspace）> KUIBU_WORKSPACE > 用户配置默认工作台 > 从当前目录向上查找。
func Resolve(env func(string) string, override string) (*store.Workspace, error) {
	if override != "" {
		return store.Open(override)
	}
	if dir := env("KUIBU_WORKSPACE"); dir != "" {
		return store.Open(dir)
	}
	defaultDir, err := DefaultWorkspace(env)
	if err != nil {
		return nil, err
	}
	if defaultDir != "" {
		return store.Open(defaultDir)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("获取当前目录失败: %w", err)
	}
	return store.Find(cwd)
}

// TokenPath 返回 GitHub token 文件路径（与 config.json 同目录）。
//
// token 不放 config.json，而是单独文件 github-token，权限 0600。
func TokenPath(env func(string) string) (string, error) {
	path, err := Path(env)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), "github-token"), nil
}

// LoadGitHubToken 读取 GitHub token；文件不存在时返回空字符串（不算错误）。
func LoadGitHubToken(env func(string) string) (string, error) {
	path, err := TokenPath(env)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("读取 GitHub token 失败: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// SaveGitHubToken 写入 GitHub token（权限 0600）；空字符串表示删除文件。
func SaveGitHubToken(env func(string) string, token string) error {
	path, err := TokenPath(env)
	if err != nil {
		return err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("删除 GitHub token 文件失败: %w", err)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建用户配置目录失败: %w", err)
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return fmt.Errorf("写入 GitHub token 失败: %w", err)
	}
	// 文件已存在时 os.WriteFile 不会改权限，显式 Chmod 保证 0600。
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("设置 GitHub token 权限失败: %w", err)
	}
	return nil
}

// Locate 返回 App 启动时应打开的工作台目录：
// KUIBU_WORKSPACE > 用户配置默认工作台；都没有时返回空字符串（显示欢迎页）。
func Locate(env func(string) string) (string, error) {
	if dir := env("KUIBU_WORKSPACE"); dir != "" {
		return dir, nil
	}
	return DefaultWorkspace(env)
}
