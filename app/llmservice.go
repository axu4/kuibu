package main

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/axu4/kuibu/internal/llm"
)

// LLMList 是「AI 模型」设置区的列表，密钥只给掩码。
type LLMList struct {
	Default string          `json:"default"`
	Models  []llm.ModelInfo `json:"models"`
}

// LLMModelInput 是新增 / 编辑模型的表单。APIKey 为空表示编辑时保留原密钥。
type LLMModelInput struct {
	Name        string `json:"name"`
	Provider    string `json:"provider"`
	BaseURL     string `json:"baseUrl"`
	Model       string `json:"model"`
	APIKey      string `json:"apiKey"`
	MakeDefault bool   `json:"makeDefault"`
}

// LLMService 暴露大模型配置给前端。逻辑都在 internal/llm，这里只做转发。
//
// 不提供任何「直接发送他人内容」的方法：外发由具体功能（归一、组报等）经
// llm.Outbound 预览 → 前端确认弹窗 → 确认后再调用。
type LLMService struct {
	env    func(string) string
	client *llm.Client
}

// newLLMService 构造使用默认依赖的服务。
func newLLMService() *LLMService {
	return &LLMService{env: os.Getenv}
}

// Presets 返回内置提供方模板。
func (s *LLMService) Presets() []llm.Preset {
	return llm.Presets()
}

// List 返回已配置的模型。
func (s *LLMService) List() (LLMList, error) {
	cfg, err := llm.Load(s.env)
	if err != nil {
		return LLMList{}, err
	}
	return LLMList{Default: cfg.Default, Models: cfg.Info()}, nil
}

// Save 新增或更新模型。
func (s *LLMService) Save(input LLMModelInput) (LLMList, error) {
	cfg, err := llm.Load(s.env)
	if err != nil {
		return LLMList{}, err
	}
	err = cfg.Upsert(llm.Model{
		Name:     input.Name,
		Provider: input.Provider,
		BaseURL:  input.BaseURL,
		Model:    input.Model,
		APIKey:   strings.TrimSpace(input.APIKey),
	}, input.MakeDefault)
	if err != nil {
		return LLMList{}, err
	}
	if err := llm.Save(s.env, cfg); err != nil {
		return LLMList{}, err
	}
	return LLMList{Default: cfg.Default, Models: cfg.Info()}, nil
}

// Remove 删除模型。
func (s *LLMService) Remove(name string) (LLMList, error) {
	cfg, err := llm.Load(s.env)
	if err != nil {
		return LLMList{}, err
	}
	if err := cfg.Remove(name); err != nil {
		return LLMList{}, err
	}
	if err := llm.Save(s.env, cfg); err != nil {
		return LLMList{}, err
	}
	return LLMList{Default: cfg.Default, Models: cfg.Info()}, nil
}

// SetDefault 设置默认模型。
func (s *LLMService) SetDefault(name string) (LLMList, error) {
	cfg, err := llm.Load(s.env)
	if err != nil {
		return LLMList{}, err
	}
	if err := cfg.SetDefault(name); err != nil {
		return LLMList{}, err
	}
	if err := llm.Save(s.env, cfg); err != nil {
		return LLMList{}, err
	}
	return LLMList{Default: cfg.Default, Models: cfg.Info()}, nil
}

// Test 用固定测试语（不含个人数据）检查连通，返回模型回复。
func (s *LLMService) Test(name string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := llm.Test(ctx, s.env, s.client, name)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Text), nil
}
