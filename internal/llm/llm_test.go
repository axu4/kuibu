package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testKey = "sk-test-0123456789abcdef"

func testEnv(t *testing.T) func(string) string {
	dir := t.TempDir()
	return func(key string) string {
		if key == "KUIBU_CONFIG_DIR" {
			return dir
		}
		return ""
	}
}

func noSleep(context.Context, time.Duration) error { return nil }

func TestMaskKey(t *testing.T) {
	cases := map[string]string{
		"":                   "",
		"abcdef":             "…ef",
		"sk-1234567890abcd":  "sk-…abcd",
		"noprefixkey123wxyz": "…wxyz",
	}
	for in, want := range cases {
		if got := MaskKey(in); got != want {
			t.Errorf("MaskKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConfigSaveLoadPermissions(t *testing.T) {
	env := testEnv(t)
	var cfg Config
	if err := cfg.Upsert(Model{Name: "ds", Provider: ProviderOpenAI, BaseURL: "https://api.deepseek.com/", Model: "deepseek-chat", APIKey: testKey}, false); err != nil {
		t.Fatal(err)
	}
	if cfg.Default != "ds" {
		t.Fatalf("第一个模型应自动设为默认，got %q", cfg.Default)
	}
	if err := Save(env, cfg); err != nil {
		t.Fatal(err)
	}
	path, _ := Path(env)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("llm.json 权限 = %o, want 600", perm)
	}
	got, err := Load(env)
	if err != nil {
		t.Fatal(err)
	}
	if got.Models[0].BaseURL != "https://api.deepseek.com" {
		t.Fatalf("BaseURL 末尾斜杠应去掉: %q", got.Models[0].BaseURL)
	}
	infoJSON, _ := json.Marshal(got.Info())
	if strings.Contains(string(infoJSON), testKey) {
		t.Fatal("Info 不应包含明文密钥")
	}
}

func TestUpsertKeepsKeyAndRemoveDefault(t *testing.T) {
	var cfg Config
	must(t, cfg.Upsert(Model{Name: "a", Provider: ProviderOpenAI, BaseURL: "http://x", Model: "m", APIKey: "k1"}, false))
	must(t, cfg.Upsert(Model{Name: "b", Provider: ProviderClaude, BaseURL: "http://y", Model: "c", APIKey: "k2"}, true))
	if cfg.Default != "b" {
		t.Fatalf("default = %q", cfg.Default)
	}
	// 编辑时不填密钥表示保留原密钥。
	must(t, cfg.Upsert(Model{Name: "a", Provider: ProviderOpenAI, BaseURL: "http://x2", Model: "m2"}, false))
	m, _ := cfg.Find("a")
	if m.APIKey != "k1" || m.Model != "m2" {
		t.Fatalf("编辑后 = %+v", m)
	}
	// 新增时密钥必填。
	if err := cfg.Upsert(Model{Name: "c", Provider: ProviderOpenAI, BaseURL: "http://z", Model: "m"}, false); !errors.Is(err, ErrInvalid) {
		t.Fatalf("新增缺密钥应报 ErrInvalid, got %v", err)
	}
	must(t, cfg.Remove("b"))
	if cfg.Default != "a" {
		t.Fatalf("删除默认后应回退到 a, got %q", cfg.Default)
	}
	if err := cfg.Remove("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestCompleteOpenAI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer "+testKey {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		var body struct {
			Model    string    `json:"model"`
			Messages []Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Model != "deepseek-chat" || len(body.Messages) != 2 || body.Messages[0].Role != "system" {
			t.Errorf("body = %+v", body)
		}
		_, _ = w.Write([]byte(`{"model":"deepseek-chat","choices":[{"message":{"content":"好的"}}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`))
	}))
	defer srv.Close()

	var usage []UsageRecord
	c := &Client{OnUsage: func(r UsageRecord) { usage = append(usage, r) }}
	resp, err := c.Complete(context.Background(),
		Model{Name: "ds", Provider: ProviderOpenAI, BaseURL: srv.URL, Model: "deepseek-chat", APIKey: testKey},
		Request{System: "sys", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "好的" || resp.InputTokens != 10 || resp.OutputTokens != 2 {
		t.Fatalf("resp = %+v", resp)
	}
	if len(usage) != 1 || !usage[0].OK || usage[0].InputTokens != 10 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestCompleteClaude(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != testKey || r.Header.Get("anthropic-version") == "" {
			t.Errorf("headers = %v", r.Header)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["system"] != "sys" {
			t.Errorf("system = %v", body["system"])
		}
		_, _ = w.Write([]byte(`{"model":"claude-sonnet-5-5","content":[{"type":"text","text":"好"},{"type":"text","text":"的"}],"usage":{"input_tokens":7,"output_tokens":3}}`))
	}))
	defer srv.Close()

	resp, err := (&Client{}).Complete(context.Background(),
		Model{Name: "c", Provider: ProviderClaude, BaseURL: srv.URL, Model: "claude-sonnet-5-5", APIKey: testKey},
		Request{System: "sys", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "好的" || resp.OutputTokens != 3 {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestRetryOn429ThenSuccess(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	var waits []time.Duration
	c := &Client{Sleep: func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }}
	resp, err := c.Complete(context.Background(),
		Model{Name: "x", Provider: ProviderOpenAI, BaseURL: srv.URL, Model: "m", APIKey: testKey},
		Request{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "ok" || calls != 3 {
		t.Fatalf("resp=%+v calls=%d", resp, calls)
	}
	if len(waits) != 2 || waits[0] != time.Second {
		t.Fatalf("应按 Retry-After 等待: %v", waits)
	}
}

func TestNoRetryOn401AndChineseMessage(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusUnauthorized)
		// 有的网关会把请求头回显到错误里，确认会被打码。
		_, _ = w.Write([]byte(`{"error":{"message":"invalid key ` + testKey + `"}}`))
	}))
	defer srv.Close()

	_, err := (&Client{Sleep: noSleep}).Complete(context.Background(),
		Model{Name: "x", Provider: ProviderOpenAI, BaseURL: srv.URL, Model: "m", APIKey: testKey},
		Request{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("应报错")
	}
	if calls != 1 {
		t.Fatalf("401 不应重试, calls=%d", calls)
	}
	if !strings.Contains(err.Error(), "密钥无效") {
		t.Fatalf("错误应中文化: %v", err)
	}
	if strings.Contains(err.Error(), testKey) {
		t.Fatalf("错误信息里不应有明文密钥: %v", err)
	}
}

func TestTimeoutRetriesThenFails(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	c := &Client{HTTP: &http.Client{Timeout: 20 * time.Millisecond}, Sleep: noSleep}
	_, err := c.Complete(context.Background(),
		Model{Name: "x", Provider: ProviderClaude, BaseURL: srv.URL, Model: "m", APIKey: testKey},
		Request{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err == nil || !strings.Contains(err.Error(), "超时") {
		t.Fatalf("应报超时, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("超时应重试 2 次, calls=%d", calls)
	}
}

func TestOutboundNeedsConfirmSendsNothing(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	env := testEnv(t)
	var cfg Config
	must(t, cfg.Upsert(Model{Name: "ds", Provider: ProviderOpenAI, BaseURL: srv.URL, Model: "m", APIKey: testKey}, true))
	must(t, Save(env, cfg))

	o := &Outbound{Env: env}
	req := Request{System: "归一周报", Messages: []Message{{Role: "user", Content: "张三本周完成了 A"}}}
	preview, _, err := o.Send(context.Background(), "", req, false)
	if !errors.Is(err, ErrNeedsConfirm) {
		t.Fatalf("未确认应返回 ErrNeedsConfirm, got %v", err)
	}
	if calls != 0 {
		t.Fatalf("未确认不应发请求, calls=%d", calls)
	}
	if !strings.Contains(preview.Text, "张三本周完成了 A") || !strings.Contains(preview.Text, "归一周报") || preview.ModelName != "ds" {
		t.Fatalf("预览应包含完整内容: %+v", preview)
	}

	_, resp, err := o.Send(context.Background(), "", req, true)
	if err != nil || resp.Text != "ok" || calls != 1 {
		t.Fatalf("确认后应发送: resp=%+v err=%v calls=%d", resp, err, calls)
	}
	// 用量日志只记元数据。
	logPath, _ := UsagePath(env)
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "张三") || strings.Contains(string(data), testKey) {
		t.Fatalf("用量日志不应含内容或密钥: %s", data)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
