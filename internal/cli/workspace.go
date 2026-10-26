package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/axu4/kuibu/internal/store"
)

// userConfig 是用户配置文件 $KUIBU_CONFIG_DIR/config.json 的内容。
type userConfig struct {
	// Workspace 是默认工作台目录的绝对路径。
	Workspace string `json:"workspace"`
}

// configPath 返回用户配置文件路径。
//
// 优先使用 KUIBU_CONFIG_DIR，未设置时用 os.UserConfigDir()/kuibu。
func configPath(env func(string) string) (string, error) {
	if dir := env("KUIBU_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "config.json"), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("定位用户配置目录失败: %w", err)
	}
	return filepath.Join(base, "kuibu", "config.json"), nil
}

// loadUserConfig 读取用户配置；文件不存在时返回空配置。
func loadUserConfig(env func(string) string) (userConfig, error) {
	var cfg userConfig
	path, err := configPath(env)
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

// saveUserConfig 写入用户配置。
func saveUserConfig(env func(string) string, cfg userConfig) error {
	path, err := configPath(env)
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
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("写入用户配置失败: %w", err)
	}
	return nil
}

// resolveWorkspace 按优先级定位工作台：
// --workspace > KUIBU_WORKSPACE > 用户配置默认工作台 > 从当前目录向上查找。
func resolveWorkspace(common *commonFlags, env func(string) string) (*store.Workspace, error) {
	if common.workspace != "" {
		return store.Open(common.workspace)
	}
	if dir := env("KUIBU_WORKSPACE"); dir != "" {
		return store.Open(dir)
	}
	cfg, err := loadUserConfig(env)
	if err != nil {
		return nil, err
	}
	if cfg.Workspace != "" {
		return store.Open(cfg.Workspace)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("获取当前目录失败: %w", err)
	}
	return store.Find(cwd)
}
