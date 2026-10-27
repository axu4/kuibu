// Package llm 是大模型配置与调用通道，供 CLI 与 App 共用。
//
// 支持两类提供方：OpenAI 兼容（DeepSeek、通义、内网网关等）与 Claude（Anthropic
// Messages API），只用标准库 net/http 实现。
//
// 硬性规则：
//   - 密钥只存在用户配置目录下的 llm.json（权限 0600），不进工作台；对外的结构
//     （CLI --json、App 绑定、错误信息）只给掩码。
//   - 把他人内容发给外部大模型必须经过 Outbound：先预览，确认后才发送，没有开关
//     可以跳过，也不提供定时入口。
package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/axu4/kuibu/internal/config"
)

// 提供方。
const (
	ProviderOpenAI = "openai"
	ProviderClaude = "claude"
)

// 错误。
var (
	// ErrNotFound 表示指定名称的模型不存在。
	ErrNotFound = errors.New("模型不存在")
	// ErrNoModel 表示还没有配置任何模型或没有默认模型。
	ErrNoModel = errors.New("还没有配置大模型，请先添加（kuibu llm add 或 设置 → AI 模型）")
	// ErrInvalid 表示配置参数不合法。
	ErrInvalid = errors.New("模型配置不合法")
	// ErrNeedsConfirm 表示外发内容尚未经人工确认。
	ErrNeedsConfirm = errors.New("外发内容需要先预览并确认")
)

// Model 是一个模型配置。APIKey 是明文，只在本包内用于发请求。
type Model struct {
	// Name 是用户起的名称，唯一。
	Name string `json:"name"`
	// Provider 是 openai 或 claude。
	Provider string `json:"provider"`
	// BaseURL 是接口地址。OpenAI 兼容拼 /chat/completions，Claude 拼 /v1/messages。
	BaseURL string `json:"baseUrl"`
	// Model 是提供方的模型 ID。
	Model string `json:"model"`
	// APIKey 是密钥明文。
	APIKey string `json:"apiKey"`
}

// Config 是 llm.json 的内容。
type Config struct {
	// Default 是默认模型名称。
	Default string `json:"default"`
	// Models 是全部模型配置。
	Models []Model `json:"models"`
}

// ModelInfo 是对外展示的模型信息，密钥只给掩码。
type ModelInfo struct {
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	BaseURL   string `json:"baseUrl"`
	Model     string `json:"model"`
	KeyMasked string `json:"keyMasked"`
	HasKey    bool   `json:"hasKey"`
	Default   bool   `json:"default"`
}

// Preset 是提供方模板。
type Preset struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Provider string `json:"provider"`
	BaseURL  string `json:"baseUrl"`
	Model    string `json:"model"`
}

// Presets 返回内置模板：DeepSeek（OpenAI 兼容）与 Claude，其余手填。
func Presets() []Preset {
	return []Preset{
		{ID: "deepseek", Label: "DeepSeek", Provider: ProviderOpenAI, BaseURL: "https://api.deepseek.com", Model: "deepseek-chat"},
		{ID: "claude", Label: "Claude", Provider: ProviderClaude, BaseURL: "https://api.anthropic.com", Model: "claude-sonnet-5-5"},
	}
}

// FindPreset 按 ID 查模板。
func FindPreset(id string) (Preset, bool) {
	for _, p := range Presets() {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

// Path 返回 llm.json 路径（与 config.json 同目录）。
func Path(env func(string) string) (string, error) {
	path, err := config.Path(env)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), "llm.json"), nil
}

// Load 读取模型配置；文件不存在时返回空配置。
func Load(env func(string) string) (Config, error) {
	var cfg Config
	path, err := Path(env)
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("读取模型配置失败: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("解析模型配置失败: %w", err)
	}
	return cfg, nil
}

// Save 原子写入模型配置，权限 0600。
func Save(env func(string) string, cfg Config) error {
	path, err := Path(env)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建用户配置目录失败: %w", err)
	}
	if cfg.Models == nil {
		cfg.Models = []Model{}
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化模型配置失败: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".llm.json.tmp-*")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	// CreateTemp 已是 0600；写入、落盘后再 rename，避免中途崩溃写坏或短暂放宽权限。
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("写入模型配置失败: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("写入模型配置失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("写入模型配置失败: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("设置模型配置权限失败: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("写入模型配置失败: %w", err)
	}
	return nil
}

// MaskKey 把密钥显示成「sk-…abcd」，短密钥只显示「…」加末两位。
func MaskKey(key string) string {
	key = strings.TrimSpace(key)
	switch {
	case key == "":
		return ""
	case len(key) <= 8:
		return "…" + key[len(key)-2:]
	}
	prefix := ""
	if i := strings.IndexByte(key, '-'); i > 0 && i <= 6 {
		prefix = key[:i+1]
	}
	return prefix + "…" + key[len(key)-4:]
}

// Info 返回对外展示的模型信息。
func (c Config) Info() []ModelInfo {
	out := make([]ModelInfo, 0, len(c.Models))
	for _, m := range c.Models {
		out = append(out, ModelInfo{
			Name:      m.Name,
			Provider:  m.Provider,
			BaseURL:   m.BaseURL,
			Model:     m.Model,
			KeyMasked: MaskKey(m.APIKey),
			HasKey:    strings.TrimSpace(m.APIKey) != "",
			Default:   m.Name == c.Default,
		})
	}
	return out
}

// Find 按名称查模型；name 为空时返回默认模型。
func (c Config) Find(name string) (Model, error) {
	if name == "" {
		if c.Default == "" {
			if len(c.Models) == 0 {
				return Model{}, ErrNoModel
			}
			return Model{}, fmt.Errorf("%w：没有设置默认模型", ErrNoModel)
		}
		name = c.Default
	}
	for _, m := range c.Models {
		if m.Name == name {
			return m, nil
		}
	}
	return Model{}, fmt.Errorf("%w：%s", ErrNotFound, name)
}

// Normalize 校验并规范化一个模型配置（去空白、去 BaseURL 末尾斜杠）。
func Normalize(m Model) (Model, error) {
	m.Name = strings.TrimSpace(m.Name)
	m.Provider = strings.TrimSpace(m.Provider)
	m.BaseURL = strings.TrimRight(strings.TrimSpace(m.BaseURL), "/")
	m.Model = strings.TrimSpace(m.Model)
	m.APIKey = strings.TrimSpace(m.APIKey)
	switch {
	case m.Name == "":
		return m, fmt.Errorf("%w：名称不能为空", ErrInvalid)
	case m.Provider != ProviderOpenAI && m.Provider != ProviderClaude:
		return m, fmt.Errorf("%w：提供方只能是 openai 或 claude", ErrInvalid)
	case m.BaseURL == "":
		return m, fmt.Errorf("%w：接口地址不能为空", ErrInvalid)
	case !strings.HasPrefix(m.BaseURL, "http://") && !strings.HasPrefix(m.BaseURL, "https://"):
		return m, fmt.Errorf("%w：接口地址要以 http:// 或 https:// 开头", ErrInvalid)
	case m.Model == "":
		return m, fmt.Errorf("%w：模型 ID 不能为空", ErrInvalid)
	}
	return m, nil
}

// Upsert 新增或更新模型。更新时 APIKey 为空表示保留原密钥。
// 第一个模型自动设为默认；makeDefault 为 true 时设为默认。
func (c *Config) Upsert(m Model, makeDefault bool) error {
	m, err := Normalize(m)
	if err != nil {
		return err
	}
	replaced := false
	for i := range c.Models {
		if c.Models[i].Name == m.Name {
			if m.APIKey == "" {
				m.APIKey = c.Models[i].APIKey
			}
			c.Models[i] = m
			replaced = true
			break
		}
	}
	if !replaced {
		if m.APIKey == "" {
			return fmt.Errorf("%w：密钥不能为空", ErrInvalid)
		}
		c.Models = append(c.Models, m)
	}
	if makeDefault || c.Default == "" {
		c.Default = m.Name
	}
	return nil
}

// Remove 删除模型；删的是默认模型时，默认改为剩下的第一个。
func (c *Config) Remove(name string) error {
	for i, m := range c.Models {
		if m.Name != name {
			continue
		}
		c.Models = append(c.Models[:i], c.Models[i+1:]...)
		if c.Default == name {
			c.Default = ""
			if len(c.Models) > 0 {
				c.Default = c.Models[0].Name
			}
		}
		return nil
	}
	return fmt.Errorf("%w：%s", ErrNotFound, name)
}

// SetDefault 设置默认模型。
func (c *Config) SetDefault(name string) error {
	if _, err := c.Find(name); err != nil {
		return err
	}
	c.Default = name
	return nil
}
