// Package config 负责用户配置（默认工作台）与工作台定位，供 CLI 与 App 共用。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/axu4/kuibu/internal/store"
)

// UserConfig 是用户配置文件 $KUIBU_CONFIG_DIR/config.json 的内容。
type UserConfig struct {
	// Workspace 是默认工作台目录的绝对路径。
	Workspace string `json:"workspace"`
}

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

// Save 写入用户配置。
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
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("写入用户配置失败: %w", err)
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

// Locate 返回 App 启动时应打开的工作台目录：
// KUIBU_WORKSPACE > 用户配置默认工作台；都没有时返回空字符串（显示欢迎页）。
func Locate(env func(string) string) (string, error) {
	if dir := env("KUIBU_WORKSPACE"); dir != "" {
		return dir, nil
	}
	return DefaultWorkspace(env)
}
