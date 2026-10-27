package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

const llmTestKey = "sk-cli-test-9876543210zyxw"

// fakeOpenAI 返回一个计数的 OpenAI 兼容假服务器。
func fakeOpenAI(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = w.Write([]byte(`{"model":"fake","choices":[{"message":{"content":"好的"}}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestLLMAddListDefaultRemove(t *testing.T) {
	e := newTestEnv(t)
	srv, _ := fakeOpenAI(t)

	res := e.mustRun(llmTestKey+"\n", "llm", "add", "ds", "--preset", "deepseek", "--base-url", srv.URL, "--json")
	out := res.jsonOut(t)
	if out["default"] != true || out["model"] != "deepseek-chat" || out["keyMasked"] != "sk-…zyxw" {
		t.Fatalf("add 输出 = %v", out)
	}
	e.mustRun("other-key-123456", "llm", "add", "cl", "--preset", "claude")

	res = e.mustRun("", "llm", "list", "--json")
	if strings.Contains(res.stdout, llmTestKey) {
		t.Fatalf("list --json 不应含明文密钥: %s", res.stdout)
	}
	if !strings.Contains(res.stdout, `"default":"ds"`) {
		t.Fatalf("默认应为 ds: %s", res.stdout)
	}

	e.mustRun("", "llm", "default", "cl")
	res = e.mustRun("", "llm", "list")
	if !strings.Contains(res.stdout, "*  cl") {
		t.Fatalf("文本列表应标记默认: %s", res.stdout)
	}

	// 更新时保留原密钥。
	e.mustRun("", "llm", "add", "ds", "--provider", "openai", "--base-url", srv.URL, "--model", "m2", "--keep-key")

	e.mustRun("", "llm", "rm", "cl")
	res = e.mustRun("", "llm", "list", "--json")
	if !strings.Contains(res.stdout, `"default":"ds"`) || !strings.Contains(res.stdout, `"model":"m2"`) {
		t.Fatalf("删除默认后应回退到 ds: %s", res.stdout)
	}

	// 密钥文件 0600，且不在工作台里。
	info, err := os.Stat(filepath.Join(e.configDir, "llm.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("llm.json 权限 = %o", info.Mode().Perm())
	}

	// 缺密钥报用法错误。
	if res := e.run("", "llm", "add", "x", "--preset", "deepseek"); res.code != Error {
		t.Fatalf("缺密钥应失败, code=%d", res.code)
	}
	if res := e.run("", "llm", "rm", "nope", "--json"); res.code != Error || !strings.Contains(res.stderr, "NOT_FOUND") {
		t.Fatalf("删除不存在的模型: %+v", res)
	}
}

func TestLLMSendNeedsConfirm(t *testing.T) {
	e := newTestEnv(t)
	srv, calls := fakeOpenAI(t)
	e.mustRun(llmTestKey, "llm", "add", "ds", "--provider", "openai", "--base-url", srv.URL, "--model", "m")

	body := "张三本周完成了接口联调"
	res := e.run(body, "llm", "send", "--system", "归一", "--json")
	if res.code != NeedsConfirm {
		t.Fatalf("不带 --yes 退出码 = %d, want 8\nstderr=%s", res.code, res.stderr)
	}
	if atomic.LoadInt32(calls) != 0 {
		t.Fatalf("不带 --yes 不应发请求, calls=%d", *calls)
	}
	if !strings.Contains(res.stdout, body) || !strings.Contains(res.stdout, "归一") {
		t.Fatalf("应输出完整预览: %s", res.stdout)
	}
	if !strings.Contains(res.stderr, "NEEDS_CONFIRM") {
		t.Fatalf("stderr 应含 NEEDS_CONFIRM: %s", res.stderr)
	}

	res = e.mustRun(body, "llm", "send", "--dry-run")
	if atomic.LoadInt32(calls) != 0 || !strings.Contains(res.stdout, body) {
		t.Fatalf("--dry-run 只预览: calls=%d out=%s", *calls, res.stdout)
	}

	res = e.mustRun(body, "llm", "send", "--yes", "--json")
	if atomic.LoadInt32(calls) != 1 || res.jsonOut(t)["text"] != "好的" {
		t.Fatalf("--yes 应发送: calls=%d out=%s", *calls, res.stdout)
	}
	if strings.Contains(res.stdout+res.stderr, llmTestKey) {
		t.Fatal("输出不应含明文密钥")
	}
}

func TestLLMTest(t *testing.T) {
	e := newTestEnv(t)
	srv, calls := fakeOpenAI(t)
	e.mustRun(llmTestKey, "llm", "add", "ds", "--provider", "openai", "--base-url", srv.URL, "--model", "m")
	res := e.mustRun("", "llm", "test", "--json")
	if res.jsonOut(t)["ok"] != true || atomic.LoadInt32(calls) != 1 {
		t.Fatalf("test 输出 = %s", res.stdout)
	}
	if res := e.run("", "llm", "test", "nope"); res.code != Error {
		t.Fatalf("未知模型应失败, code=%d", res.code)
	}
}

func TestLLMNoModel(t *testing.T) {
	e := newTestEnv(t)
	res := e.run("hi", "llm", "send", "--json")
	if res.code != Error || !strings.Contains(res.stderr, "NO_MODEL") {
		t.Fatalf("未配置模型: %+v", res)
	}
}
