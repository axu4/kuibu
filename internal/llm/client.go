package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Message 是一条对话消息。Role 为 user 或 assistant。
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Request 是一次调用的输入。
type Request struct {
	// System 是系统提示，可为空。
	System string `json:"system,omitempty"`
	// Messages 是对话消息，至少一条。
	Messages []Message `json:"messages"`
	// MaxTokens 是输出上限，0 用默认值。
	MaxTokens int `json:"maxTokens,omitempty"`
}

// Response 是一次调用的结果。
type Response struct {
	Text         string `json:"text"`
	Model        string `json:"model"`
	InputTokens  int    `json:"inputTokens"`
	OutputTokens int    `json:"outputTokens"`
}

// DefaultMaxTokens 是未指定时的输出上限。
const DefaultMaxTokens = 4096

// DefaultTimeout 是单次请求超时。
const DefaultTimeout = 120 * time.Second

// Client 发起调用。零值可用；测试可替换 HTTP、Sleep、Retries。
type Client struct {
	// HTTP 为空时使用带 DefaultTimeout 的客户端。
	HTTP *http.Client
	// Retries 是最多重试次数（不含首次），负数表示不重试，0 用默认 2 次。
	Retries int
	// Sleep 为空时用 time.Sleep 风格的可取消等待。
	Sleep func(ctx context.Context, d time.Duration) error
	// OnUsage 每次调用结束后回调（成功或失败），用于记录用量日志。
	OnUsage func(UsageRecord)
}

// APIError 是提供方返回的非 2xx 错误，Message 已翻成中文。
type APIError struct {
	Status  int
	Message string
	// Detail 是提供方返回的原始错误说明（已去掉密钥），便于排查。
	Detail string
}

// Error 实现 error。
func (e *APIError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("%s（HTTP %d：%s）", e.Message, e.Status, e.Detail)
	}
	return fmt.Sprintf("%s（HTTP %d）", e.Message, e.Status)
}

// Complete 调用模型并返回完整文本。
func (c *Client) Complete(ctx context.Context, m Model, req Request) (Response, error) {
	if len(req.Messages) == 0 {
		return Response{}, fmt.Errorf("%w：没有要发送的内容", ErrInvalid)
	}
	if strings.TrimSpace(m.APIKey) == "" {
		return Response{}, fmt.Errorf("%w：模型 %s 没有密钥", ErrInvalid, m.Name)
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = DefaultMaxTokens
	}
	retries := c.Retries
	if retries == 0 {
		retries = 2
	}
	if retries < 0 {
		retries = 0
	}

	var (
		resp Response
		err  error
	)
	for attempt := 0; ; attempt++ {
		var wait time.Duration
		resp, wait, err = c.once(ctx, m, req)
		if err == nil || attempt >= retries || !retryable(err) || ctx.Err() != nil {
			break
		}
		if wait <= 0 {
			wait = time.Duration(1<<attempt) * time.Second
		}
		if serr := c.sleep(ctx, wait); serr != nil {
			break
		}
	}
	if c.OnUsage != nil {
		c.OnUsage(UsageRecord{
			Time:         time.Now(),
			Model:        m.Name,
			ModelID:      m.Model,
			InputTokens:  resp.InputTokens,
			OutputTokens: resp.OutputTokens,
			OK:           err == nil,
		})
	}
	if err != nil {
		return Response{}, scrubKey(err, m.APIKey)
	}
	return resp, nil
}

// once 发一次请求；返回值 wait 是服务端建议的重试等待（Retry-After）。
func (c *Client) once(ctx context.Context, m Model, req Request) (Response, time.Duration, error) {
	var (
		url  string
		body any
	)
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	switch m.Provider {
	case ProviderOpenAI:
		url = m.BaseURL + "/chat/completions"
		body = openAIBody(m, req)
		header.Set("Authorization", "Bearer "+m.APIKey)
	case ProviderClaude:
		url = m.BaseURL + "/v1/messages"
		body = claudeBody(m, req)
		header.Set("x-api-key", m.APIKey)
		header.Set("anthropic-version", "2023-06-01")
	default:
		return Response{}, 0, fmt.Errorf("%w：未知提供方 %q", ErrInvalid, m.Provider)
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Response{}, 0, fmt.Errorf("序列化请求失败: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return Response{}, 0, fmt.Errorf("%w：接口地址不合法: %v", ErrInvalid, err)
	}
	httpReq.Header = header

	httpResp, err := c.httpClient().Do(httpReq)
	if err != nil {
		return Response{}, 0, networkError(err)
	}
	defer httpResp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(httpResp.Body, 8<<20))
	if err != nil {
		return Response{}, 0, networkError(err)
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode > 299 {
		return Response{}, retryAfter(httpResp.Header.Get("Retry-After")), apiError(httpResp.StatusCode, data)
	}
	var resp Response
	switch m.Provider {
	case ProviderOpenAI:
		resp, err = parseOpenAI(data)
	default:
		resp, err = parseClaude(data)
	}
	if err != nil {
		return Response{}, 0, err
	}
	if resp.Model == "" {
		resp.Model = m.Model
	}
	return resp, 0, nil
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: DefaultTimeout}
}

func (c *Client) sleep(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// netError 是网络层错误（连接失败、超时），可重试。
type netError struct{ err error }

func (e *netError) Error() string { return e.err.Error() }
func (e *netError) Unwrap() error { return e.err }

// networkError 把网络错误翻成中文。
func networkError(err error) error {
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return &netError{fmt.Errorf("请求超时，请检查网络或稍后再试: %w", err)}
	}
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("请求已取消: %w", err)
	}
	return &netError{fmt.Errorf("连接模型接口失败，请检查接口地址和网络: %w", err)}
}

// retryable 只对网络错误、429、5xx 重试。
func retryable(err error) bool {
	var ne *netError
	if errors.As(err, &ne) {
		return true
	}
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Status == http.StatusTooManyRequests || ae.Status >= 500
	}
	return false
}

// retryAfter 解析 Retry-After 秒数，最多等 30 秒。
func retryAfter(v string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return 0
	}
	if n > 30 {
		n = 30
	}
	return time.Duration(n) * time.Second
}

// apiError 从响应体里取错误说明并给出中文提示。
func apiError(status int, data []byte) error {
	var parsed struct {
		Error json.RawMessage `json:"error"`
	}
	detail := ""
	if json.Unmarshal(data, &parsed) == nil && len(parsed.Error) > 0 {
		var obj struct {
			Message string `json:"message"`
		}
		var str string
		if json.Unmarshal(parsed.Error, &obj) == nil && obj.Message != "" {
			detail = obj.Message
		} else if json.Unmarshal(parsed.Error, &str) == nil {
			detail = str
		}
	}
	if detail == "" {
		detail = strings.TrimSpace(string(data))
	}
	if r := []rune(detail); len(r) > 200 {
		detail = string(r[:200]) + "…"
	}
	var msg string
	switch {
	case status == http.StatusUnauthorized:
		msg = "密钥无效或已过期"
	case status == http.StatusForbidden:
		msg = "没有调用该模型的权限"
	case status == http.StatusNotFound:
		msg = "接口地址或模型 ID 不存在"
	case status == http.StatusBadRequest:
		msg = "请求被模型接口拒绝"
	case status == http.StatusPaymentRequired:
		msg = "账户余额不足"
	case status == http.StatusTooManyRequests:
		msg = "请求过于频繁或额度用尽，请稍后再试"
	case status >= 500:
		msg = "模型服务暂时不可用，请稍后再试"
	default:
		msg = "模型接口返回错误"
	}
	return &APIError{Status: status, Message: msg, Detail: detail}
}

// scrubKey 防止密钥出现在错误信息里（例如网关把请求头回显到错误中）。
func scrubKey(err error, key string) error {
	key = strings.TrimSpace(key)
	if key == "" || !strings.Contains(err.Error(), key) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), key, MaskKey(key)))
}

// openAIBody 构造 OpenAI 兼容 /chat/completions 请求体。
func openAIBody(m Model, req Request) any {
	msgs := make([]Message, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, Message{Role: "system", Content: req.System})
	}
	msgs = append(msgs, req.Messages...)
	return map[string]any{
		"model":      m.Model,
		"messages":   msgs,
		"max_tokens": req.MaxTokens,
		"stream":     false,
	}
}

// parseOpenAI 解析 OpenAI 兼容响应。
func parseOpenAI(data []byte) (Response, error) {
	var r struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return Response{}, fmt.Errorf("解析模型返回失败: %w", err)
	}
	if len(r.Choices) == 0 {
		return Response{}, errors.New("模型没有返回内容")
	}
	return Response{
		Text:         r.Choices[0].Message.Content,
		Model:        r.Model,
		InputTokens:  r.Usage.PromptTokens,
		OutputTokens: r.Usage.CompletionTokens,
	}, nil
}

// claudeBody 构造 Anthropic /v1/messages 请求体。
func claudeBody(m Model, req Request) any {
	body := map[string]any{
		"model":      m.Model,
		"messages":   req.Messages,
		"max_tokens": req.MaxTokens,
	}
	if req.System != "" {
		body["system"] = req.System
	}
	return body
}

// parseClaude 解析 Anthropic 响应，拼接全部 text 块。
func parseClaude(data []byte) (Response, error) {
	var r struct {
		Model   string `json:"model"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return Response{}, fmt.Errorf("解析模型返回失败: %w", err)
	}
	var b strings.Builder
	for _, block := range r.Content {
		if block.Type == "text" {
			b.WriteString(block.Text)
		}
	}
	if b.Len() == 0 {
		return Response{}, errors.New("模型没有返回内容")
	}
	return Response{
		Text:         b.String(),
		Model:        r.Model,
		InputTokens:  r.Usage.InputTokens,
		OutputTokens: r.Usage.OutputTokens,
	}, nil
}
