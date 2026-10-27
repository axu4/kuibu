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

func TestReportGroupGenerateFinalizeSubmit(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()
	ws := func(a ...string) []string { return append(a, "--workspace", e.workspace) }

	e.addPersonJSON("勇旭", "--group", "框架组", "--role", "lead")
	e.addPersonJSON("张三", "--group", "框架组", "--product", "产品线A")
	e.addPersonJSON("李四", "--group", "框架组", "--product", "产品线B")
	e.addPersonJSON("王五", "--group", "框架组", "--product", "产品线A")

	groupJSON := `[
{"section":"本周完成","productLine":"产品线A","category":"功能开发","text":"完成接口联调","sources":["张三"]},
{"section":"本周完成","productLine":"产品线B","category":"客户支持","text":"处理客户工单","sources":["李四"],"pending":true},
{"section":"本周完成","productLine":"产品线X","category":"奇怪","text":"未知归类","sources":["李四"]},
{"section":"下周计划","productLine":"产品线A","category":"功能开发","text":"压测","sources":["张三"]},
{"section":"AI 使用 / AI Coding 实践经验","text":"用 AI 生成单测","sources":["张三","李四"]},
{"section":"本周亮点","text":"上线零故障","sources":["张三"]}]`
	var calls int32
	var lastBody atomic.Value
	lastBody.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		body, _ := io.ReadAll(r.Body)
		lastBody.Store(string(body))
		reply := "## 本周完成\n- 【产品线A】完成\n## 下周计划\n- 无"
		if strings.Contains(string(body), "组报整理助手") {
			reply = groupJSON
		}
		data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": reply}}}})
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	e.mustRun("sk-group-test-0000", "llm", "add", "fake", "--provider", "openai", "--base-url", srv.URL, "--model", "m")

	e.mustRun("【周报】张三 2026-10-29\n联调\n\n【周报】李四 2026-10-29\n工单\n\n【周报】王五 2026-10-29\n文档\n", ws("report", "add")...)
	e.mustRun("", ws("report", "normalize", "--week", "2026-W44", "--all", "--yes")...)
	e.mustRun("", ws("report", "finalize", "2026-W44", "张三")...)
	e.mustRun("", ws("report", "finalize", "2026-W44", "李四")...)

	// 王五未定稿：默认返回 7 并列出名单。
	res := e.run("", ws("report", "group", "generate", "--week", "2026-W44", "--json")...)
	if res.code != NotFinal || !strings.Contains(res.stderr, "王五") {
		t.Fatalf("有人未定稿应返回 7 并列出: %+v", res)
	}

	// 自定义模板：加一个「本周亮点」栏目。
	tpl := e.mustRun("", ws("template", "show", "group")...).stdout
	tpl += "\n## 本周亮点\n> 一两句话\n"
	e.mustRun(tpl, ws("template", "edit", "group")...)
	if _, err := os.Stat(filepath.Join(e.workspace, "templates", "group.md")); err != nil {
		t.Fatalf("模板应写到 templates/group.md: %v", err)
	}

	// 不带 --yes：预览、退出 8、零请求。
	before := atomic.LoadInt32(&calls)
	res = e.run("", ws("report", "group", "generate", "--week", "2026-W44", "--allow-missing")...)
	if res.code != NeedsConfirm || atomic.LoadInt32(&calls) != before || !strings.Contains(res.stdout, "未纳入（未定稿）：王五") {
		t.Fatalf("预览: code=%d out=%s", res.code, res.stdout)
	}

	e.mustRun("", ws("report", "group", "generate", "--week", "2026-W44", "--allow-missing", "--yes")...)
	if last := lastBody.Load().(string); !strings.Contains(last, "本周亮点") || strings.Contains(last, "文档") {
		t.Fatal("请求应包含自定义栏目，且不含未定稿组员的内容")
	}
	show := e.mustRun("", ws("report", "group", "show", "--week", "2026-W44", "--json")...).jsonOut(t)
	body, _ := show["body"].(string)
	for _, want := range []string{
		"> 未纳入（未定稿）：王五",
		"## 本周完成\n\n### 产品线A\n#### 功能开发\n- 完成接口联调（张三）",
		"### 产品线B\n#### 客户支持\n- 处理客户工单（李四）（待确认）",
		"### 其他\n#### 未分类\n- 未知归类（李四）",
		"## 风险与求助\n- 无",
		"## AI 使用 / AI Coding 实践经验\n- 用 AI 生成单测（张三、李四）",
		"## 本周亮点\n- 上线零故障（张三）",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("组报缺少 %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "#### 客户支持\n- 完成") {
		t.Fatal("空类别不应输出")
	}
	if missing, _ := show["missing"].([]any); len(missing) != 1 {
		t.Fatalf("missing = %v", show["missing"])
	}

	// 未定稿不能带入 SE。
	if res := e.run("", ws("report", "group", "submit", "--week", "2026-W44", "--json")...); res.code != NotFinal {
		t.Fatalf("未定稿 submit 应返回 7: %+v", res)
	}
	// 人工修改后再生成返回 9。
	e.mustRun(body+"\n- 补充一句", ws("report", "group", "edit", "--week", "2026-W44")...)
	if res := e.run("", ws("report", "group", "generate", "--week", "2026-W44", "--allow-missing", "--yes")...); res.code != HumanEditsExist {
		t.Fatalf("人工修改后再生成应返回 9: %+v", res)
	}

	e.mustRun("", ws("report", "group", "finalize", "--week", "2026-W44")...)
	sub := e.mustRun("", ws("report", "group", "submit", "--week", "2026-W44", "--json")...).jsonOut(t)
	if sub["source"] != "group-report" || sub["level"] != "lead" {
		t.Fatalf("submit = %v", sub)
	}
	st := e.mustRun("", ws("report", "status", "--week", "2026-W44", "--level", "lead", "--json")...).stdout
	if !strings.Contains(st, `"name":"勇旭"`) || !strings.Contains(st, `"status":"submitted"`) {
		t.Fatalf("SE tab 应显示组长已交: %s", st)
	}
	// 重复带入走覆盖 + .history。
	e.mustRun("", ws("report", "group", "submit", "--week", "2026-W44")...)
	hist, _ := os.ReadDir(filepath.Join(e.workspace, "reports", "2026-W44", ".history", "lead"))
	if len(hist) != 1 {
		t.Fatalf("重复带入应把旧版移入 .history, got %d", len(hist))
	}
}
