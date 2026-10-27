package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Preview 是外发前给人看的完整内容：发给谁、发什么。
type Preview struct {
	// ModelName 是配置里的模型名称。
	ModelName string `json:"modelName"`
	// Provider 与 ModelID、BaseURL 说明内容会发到哪里。
	Provider string `json:"provider"`
	ModelID  string `json:"modelId"`
	BaseURL  string `json:"baseUrl"`
	// Text 是将发送的全部文本（系统提示 + 各条消息），与实际请求内容一致。
	Text string `json:"text"`
	// Chars 是 Text 的字数，便于判断是否过长。
	Chars int `json:"chars"`
}

// Outbound 是外发闸门：所有把他人内容发给外部大模型的调用都必须经过它。
//
// 只能手动触发：Send 必须带 confirmed=true，否则返回 ErrNeedsConfirm 且不发请求。
// 本类型没有任何跳过确认的开关，也不应被定时任务调用。
type Outbound struct {
	Env    func(string) string
	Client *Client
}

// Prepare 按模型名（空为默认模型）生成预览。
func (o *Outbound) Prepare(modelName string, req Request) (Preview, Model, error) {
	cfg, err := Load(o.Env)
	if err != nil {
		return Preview{}, Model{}, err
	}
	m, err := cfg.Find(modelName)
	if err != nil {
		return Preview{}, Model{}, err
	}
	text := RenderRequest(req)
	return Preview{
		ModelName: m.Name,
		Provider:  m.Provider,
		ModelID:   m.Model,
		BaseURL:   m.BaseURL,
		Text:      text,
		Chars:     len([]rune(text)),
	}, m, nil
}

// Send 发送请求。confirmed 必须来自人工确认（CLI --yes、App「确认发送」按钮）。
// 未确认时返回预览与 ErrNeedsConfirm，不发任何请求。
func (o *Outbound) Send(ctx context.Context, modelName string, req Request, confirmed bool) (Preview, Response, error) {
	preview, m, err := o.Prepare(modelName, req)
	if err != nil {
		return Preview{}, Response{}, err
	}
	if !confirmed {
		return preview, Response{}, ErrNeedsConfirm
	}
	client := o.Client
	if client == nil {
		client = &Client{}
	}
	if client.OnUsage == nil {
		withLog := *client
		withLog.OnUsage = func(r UsageRecord) { _ = AppendUsage(o.Env, r) }
		client = &withLog
	}
	resp, err := client.Complete(ctx, m, req)
	return preview, resp, err
}

// RenderRequest 把请求渲染成给人看的文本，内容与实际发送的一致。
func RenderRequest(req Request) string {
	var b strings.Builder
	if req.System != "" {
		b.WriteString("【系统提示】\n")
		b.WriteString(req.System)
		b.WriteString("\n\n")
	}
	for i, msg := range req.Messages {
		if i > 0 {
			b.WriteString("\n\n")
		}
		label := "发送内容"
		if msg.Role == "assistant" {
			label = "模型回复"
		}
		b.WriteString("【" + label + "】\n")
		b.WriteString(msg.Content)
	}
	return b.String()
}

// TestPrompt 是连通测试发送的固定内容，不含任何个人数据，因此不需要外发确认。
const TestPrompt = "这是跬步的连通测试，请只回复「好的」两个字。"

// Test 用固定测试语检查模型能否调通。
func Test(ctx context.Context, env func(string) string, client *Client, modelName string) (Response, error) {
	cfg, err := Load(env)
	if err != nil {
		return Response{}, err
	}
	m, err := cfg.Find(modelName)
	if err != nil {
		return Response{}, err
	}
	if client == nil {
		client = &Client{}
	}
	if client.OnUsage == nil {
		withLog := *client
		withLog.OnUsage = func(r UsageRecord) { _ = AppendUsage(env, r) }
		client = &withLog
	}
	return client.Complete(ctx, m, Request{
		Messages:  []Message{{Role: "user", Content: TestPrompt}},
		MaxTokens: 32,
	})
}

// UsageRecord 是一行用量日志，不含任何请求或返回内容。
type UsageRecord struct {
	Time         time.Time `json:"time"`
	Model        string    `json:"model"`
	ModelID      string    `json:"modelId"`
	InputTokens  int       `json:"inputTokens"`
	OutputTokens int       `json:"outputTokens"`
	OK           bool      `json:"ok"`
}

// UsagePath 返回用量日志路径（与 llm.json 同目录）。
func UsagePath(env func(string) string) (string, error) {
	path, err := Path(env)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), "llm-usage.log"), nil
}

// AppendUsage 追加一行用量日志。
func AppendUsage(env func(string) string, r UsageRecord) error {
	path, err := UsagePath(env)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("写入用量日志失败: %w", err)
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}
