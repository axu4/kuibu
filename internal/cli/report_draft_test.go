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

// fakeNormalizer 是返回固定归一结果的 OpenAI 兼容假模型。
func fakeNormalizer(t *testing.T, reply string) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + jsonString(reply) + `}}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func jsonString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s) + `"`
}

func TestReportNormalizeLifecycle(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()
	e.addPersonJSON("张三", "--group", "框架组", "--product", "产品线A")
	e.addPersonJSON("王五", "--group", "框架组")
	e.mustRun("【周报】张三 2026-10-29\n完成接口联调\n下周压测\n\n【周报】王五 2026-10-29\n修复 3 个 bug\n",
		"report", "add", "--workspace", e.workspace)

	reply := "```markdown\n## 本周完成\n- 【产品线A】完成接口联调\n## 下周计划\n- 【产品线A】压测\n## 风险与求助\n- 无\n```"
	srv, calls := fakeNormalizer(t, reply)
	e.mustRun("sk-normalize-test-0000", "llm", "add", "fake", "--provider", "openai", "--base-url", srv.URL, "--model", "m")

	ws := []string{"--workspace", e.workspace}
	args := func(a ...string) []string { return append(a, ws...) }

	// 不带 --yes：预览、退出 8、零请求；预览包含原文与提示词。
	res := e.run("", args("report", "normalize", "--week", "2026-W44", "--person", "张三")...)
	if res.code != NeedsConfirm {
		t.Fatalf("不带 --yes 退出码 = %d\n%s", res.code, res.stderr)
	}
	if atomic.LoadInt32(calls) != 0 {
		t.Fatal("不带 --yes 不应发请求")
	}
	if !strings.Contains(res.stdout, "完成接口联调") || !strings.Contains(res.stdout, "【产品线A】") {
		t.Fatalf("预览应包含原文与领域候选: %s", res.stdout)
	}

	// --yes 生成草稿：代码围栏被去掉，front matter 正确。
	e.mustRun("", args("report", "normalize", "--week", "2026-W44", "--person", "张三", "--yes")...)
	if atomic.LoadInt32(calls) != 1 {
		t.Fatalf("calls = %d", atomic.LoadInt32(calls))
	}
	show := e.mustRun("", args("report", "draft", "show", "2026-W44", "张三", "--json")...).jsonOut(t)
	body, _ := show["body"].(string)
	if show["status"] != "draft" || show["model"] != "fake" || show["humanEdited"] != false ||
		strings.Contains(body, "```") || !strings.HasPrefix(body, "## 本周完成") {
		t.Fatalf("草稿 = %v", show)
	}

	// AI 草稿未被人工改过，可直接重新生成。
	e.mustRun("", args("report", "normalize", "--week", "2026-W44", "--person", "张三", "--yes")...)

	// 人工修改后再生成返回 9。
	edited := body + "\n- 【产品线A】补充：联调文档已更新"
	e.mustRun(edited, args("report", "draft", "edit", "2026-W44", "张三")...)
	res = e.run("", args("report", "normalize", "--week", "2026-W44", "--person", "张三", "--yes", "--json")...)
	if res.code != HumanEditsExist || !strings.Contains(res.stderr, "HUMAN_EDITS_EXIST") {
		t.Fatalf("人工修改后再生成应返回 9: %+v", res)
	}

	// 定稿后不能直接修改，取消定稿后可以。
	e.mustRun("", args("report", "finalize", "2026-W44", "张三")...)
	res = e.run("改", args("report", "draft", "edit", "2026-W44", "张三", "--json")...)
	if res.code != Error || !strings.Contains(res.stderr, "FINALIZED") {
		t.Fatalf("定稿后修改应失败: %+v", res)
	}
	status := e.mustRun("", args("report", "status", "--week", "2026-W44")...)
	if !strings.Contains(status.stdout, "已定稿") || !strings.Contains(status.stdout, "未归一") {
		t.Fatalf("status 应显示归一状态: %s", status.stdout)
	}

	// --force 覆盖，旧版进 .history。
	e.mustRun("", args("report", "normalize", "--week", "2026-W44", "--person", "张三", "--yes", "--force")...)
	hist, _ := os.ReadDir(filepath.Join(e.workspace, "reports", "2026-W44", ".history", "member-normalized"))
	if len(hist) == 0 {
		t.Fatal("--force 覆盖前应把旧版移入 .history")
	}
	show = e.mustRun("", args("report", "draft", "show", "2026-W44", "张三", "--json")...).jsonOut(t)
	if show["status"] != "draft" || show["humanEdited"] != false {
		t.Fatalf("覆盖后应是新草稿: %v", show)
	}

	// 原文重新提交后标「原文已更新」。
	e.mustRun("【周报】张三 2026-10-30\n完成接口联调和压测\n", args("report", "add")...)
	show = e.mustRun("", args("report", "draft", "show", "2026-W44", "张三", "--json")...).jsonOut(t)
	if show["sourceChanged"] != true {
		t.Fatalf("原文更新后 sourceChanged 应为 true: %v", show)
	}
	status = e.mustRun("", args("report", "status", "--week", "2026-W44", "--json")...)
	if !strings.Contains(status.stdout, `"sourceChanged":true`) {
		t.Fatalf("status --json 应标出原文已更新: %s", status.stdout)
	}

	// --all：张三已有草稿（未人工修改）可覆盖，王五新生成；report list 默认不混入归一稿。
	before := atomic.LoadInt32(calls)
	e.mustRun("", args("report", "normalize", "--week", "2026-W44", "--all", "--yes")...)
	if atomic.LoadInt32(calls)-before != 2 {
		t.Fatalf("--all 应发 2 次请求, got %d", atomic.LoadInt32(calls)-before)
	}
	list := e.mustRun("", args("report", "list", "--week", "2026-W44", "--json")...).jsonArray(t)
	if len(list) != 2 {
		t.Fatalf("report list 默认只列原文, got %d", len(list))
	}
}

func TestReportNormalizeParseFallbackKeepsText(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()
	e.addPersonJSON("张三", "--group", "框架组")
	e.mustRun("【周报】张三 2026-10-29\n完成接口联调\n", "report", "add", "--workspace", e.workspace)

	srv, _ := fakeNormalizer(t, "本周：完成接口联调。")
	e.mustRun("sk-normalize-test-0000", "llm", "add", "fake", "--provider", "openai", "--base-url", srv.URL, "--model", "m")
	res := e.mustRun("", "report", "normalize", "--week", "2026-W44", "--person", "张三", "--yes", "--workspace", e.workspace)
	if !strings.Contains(res.stdout, "整段保存为草稿") {
		t.Fatalf("应提示结构不符: %s", res.stdout)
	}
	show := e.mustRun("", "report", "draft", "show", "2026-W44", "张三", "--json", "--workspace", e.workspace).jsonOut(t)
	if show["body"] != "本周：完成接口联调。" {
		t.Fatalf("不应丢内容: %v", show["body"])
	}
}
