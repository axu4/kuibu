package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestReportProductFlow(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()
	ws := func(a ...string) []string { return append(a, "--workspace", e.workspace) }

	// 勇旭（框架组组长）经组报带入；另两位组长交原文。
	e.addPersonJSON("勇旭", "--group", "框架组", "--role", "lead", "--product", "产品线A")
	e.addPersonJSON("赵六", "--group", "平台组", "--role", "lead", "--product", "产品线A", "--product", "产品线B")
	e.addPersonJSON("钱七", "--group", "测试组", "--role", "lead", "--product", "产品线C")
	e.addPersonJSON("张三", "--group", "框架组", "--product", "产品线A")

	leadJSON := `[{"section":"本周完成","productLine":"产品线B","category":"功能开发","text":"平台组完成网关","sources":["赵六"]}]`
	productJSON := `[
{"section":"本周完成","productLine":"产品线A","category":"功能开发","text":"完成接口联调","sources":["框架组"]},
{"section":"本周完成","productLine":"产品线B","category":"功能开发","text":"完成网关","sources":["平台组"]},
{"section":"下周计划","productLine":"产品线A","category":"项目管理","text":"版本发布","sources":["框架组","平台组"]},
{"section":"风险与求助","text":"测试环境不足","sources":["平台组"]}]`
	groupJSON := `[{"section":"本周完成","productLine":"产品线A","category":"功能开发","text":"完成接口联调","sources":["张三"]}]`
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		b, _ := io.ReadAll(r.Body)
		body := string(b)
		reply := "## 本周完成\n- 【产品线A】完成接口联调\n## 下周计划\n- 无"
		switch {
		case strings.Contains(body, "各组的周报"):
			reply = productJSON
		case strings.Contains(body, "平台组网关"):
			reply = leadJSON
		case strings.Contains(body, "各组员的周报"):
			reply = groupJSON
		}
		data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": reply}}}})
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	e.mustRun("sk-product-test-0000", "llm", "add", "fake", "--provider", "openai", "--base-url", srv.URL, "--model", "m")

	// 框架组：组员归一定稿 → 组报定稿 → 带入 SE。
	e.mustRun("【周报】张三 2026-10-29\n联调\n", ws("report", "add")...)
	e.mustRun("", ws("report", "normalize", "--week", "2026-W44", "--all", "--yes")...)
	e.mustRun("", ws("report", "finalize", "2026-W44", "张三")...)
	e.mustRun("", ws("report", "group", "generate", "--week", "2026-W44", "--group", "框架组", "--yes")...)
	e.mustRun("", ws("report", "group", "finalize", "--week", "2026-W44", "--group", "框架组")...)
	e.mustRun("", ws("report", "group", "submit", "--week", "2026-W44", "--group", "框架组")...)

	// 另两位组长交原文。
	e.mustRun("【周报】赵六 2026-10-29\n平台组网关完成\n\n【周报】钱七 2026-10-29\n测试用例补齐\n", ws("report", "add", "--level", "lead")...)

	// 组长批量归一：勇旭（组报带入）跳过且不算错误。
	res := e.mustRun("", ws("report", "normalize", "--week", "2026-W44", "--level", "lead", "--all", "--yes")...)
	if !strings.Contains(res.stdout, "跳过 勇旭：由定稿组报带入") {
		t.Fatalf("组报带入的组长应跳过: %s", res.stdout)
	}
	show := e.mustRun("", ws("report", "draft", "show", "2026-W44", "赵六", "--level", "lead", "--json")...).jsonOut(t)
	if body, _ := show["body"].(string); !strings.Contains(body, "### 产品线B\n#### 功能开发\n- 平台组完成网关（赵六）") {
		t.Fatalf("组长归一稿应为组报结构:\n%s", body)
	}
	st := e.mustRun("", ws("report", "status", "--week", "2026-W44", "--level", "lead")...).stdout
	if !strings.Contains(st, "定稿（组报）") || !strings.Contains(st, "草稿") {
		t.Fatalf("SE 名单应显示归一状态:\n%s", st)
	}

	// 赵六定稿、钱七未定稿：默认返回 7 并列出钱七。
	e.mustRun("", ws("report", "finalize", "2026-W44", "赵六", "--level", "lead")...)
	r := e.run("", ws("report", "product", "generate", "--week", "2026-W44", "--json")...)
	if r.code != NotFinal || !strings.Contains(r.stderr, "钱七") {
		t.Fatalf("有组长未定稿应返回 7: %+v", r)
	}

	before := atomic.LoadInt32(&calls)
	r = e.run("", ws("report", "product", "generate", "--week", "2026-W44", "--allow-missing")...)
	if r.code != NeedsConfirm || atomic.LoadInt32(&calls) != before {
		t.Fatalf("不带 --yes 应只预览: %+v", r)
	}
	e.mustRun("", ws("report", "product", "generate", "--week", "2026-W44", "--allow-missing", "--yes")...)
	out := e.mustRun("", ws("report", "product", "show", "--week", "2026-W44", "--json")...).jsonOut(t)
	body, _ := out["body"].(string)
	for _, want := range []string{
		"> 未纳入（未定稿）：测试组（钱七）",
		"## 产品线A\n\n### 本周完成\n#### 功能开发\n- 完成接口联调（框架组）\n\n### 下周计划\n#### 项目管理\n- 版本发布（框架组、平台组）",
		"## 产品线B\n\n### 本周完成\n#### 功能开发\n- 完成网关（平台组）",
		"## 风险与求助\n- 测试环境不足（平台组）",
		"## AI 使用 / AI Coding 实践经验\n- 无",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("产品报缺少 %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "## 产品线C") {
		t.Fatal("没有条目的产品线不应输出")
	}
	if src, _ := out["sources"].([]any); len(src) != 2 {
		t.Fatalf("sources = %v", out["sources"])
	}

	ch := e.mustRun("", ws("report", "product", "show", "--week", "2026-W44", "--product", "产品线B")...).stdout
	if strings.Contains(ch, "产品线A") || !strings.Contains(ch, "完成网关") {
		t.Fatalf("--product 只输出一章:\n%s", ch)
	}
	e.mustRun("", ws("report", "product", "finalize", "--week", "2026-W44")...)
	if r := e.run("改", ws("report", "product", "edit", "--week", "2026-W44")...); r.code != Error {
		t.Fatalf("定稿后修改应失败: %+v", r)
	}
	// 导出：只认定稿，内容与定稿正文一致（不含 front matter）。
	md := e.mustRun("", ws("report", "export", "product", "--week", "2026-W44")...).stdout
	if strings.Contains(md, "status:") || !strings.HasPrefix(md, "> 未纳入") {
		t.Fatalf("导出 Markdown 应只含正文:\n%s", md)
	}
	out2 := filepath.Join(t.TempDir(), "x.html")
	res2 := e.mustRun("", ws("report", "export", "product", "--week", "2026-W44", "--format", "feishu-html", "--out", out2, "--json")...).jsonOut(t)
	if res2["fileName"] != "产品报-2026-W44.md" {
		t.Fatalf("默认文件名 = %v", res2["fileName"])
	}
	if data, _ := os.ReadFile(out2); !strings.Contains(string(data), "<h2>产品线A</h2>") {
		t.Fatalf("HTML 导出:\n%s", data)
	}
	if r := e.run("", ws("report", "export", "group", "--week", "2026-W43", "--group", "框架组")...); r.code != Error && r.code != NotFinal {
		t.Fatalf("没有组报时导出应失败: %+v", r)
	}

	// 组长定稿内容变了 → 来源已更新。
	e.mustRun("", ws("report", "unfinalize", "2026-W44", "赵六", "--level", "lead")...)
	out = e.mustRun("", ws("report", "product", "show", "--week", "2026-W44", "--json")...).jsonOut(t)
	if out["sourceChanged"] != true {
		t.Fatal("组长取消定稿后应标来源已更新")
	}
	// 产品报取消定稿后导出返回 7。
	e.mustRun("", ws("report", "product", "unfinalize", "--week", "2026-W44")...)
	if r := e.run("", ws("report", "export", "product", "--week", "2026-W44", "--json")...); r.code != NotFinal {
		t.Fatalf("未定稿导出应返回 7: %+v", r)
	}
}
