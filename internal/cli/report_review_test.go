package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/axu4/kuibu/internal/core"
)

func TestReportReviewAndCarry(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()
	e.addPersonJSON("张三", "--group", "框架组")
	ws := func(a ...string) []string { return append(a, "--workspace", e.workspace) }

	prev := "## 本周完成\n- 【A】旧事项\n## 下周计划\n- 【A】接口联调\n- 【A】压测\n## 风险与求助\n- 无"
	cur := "## 本周完成\n- 【A】完成接口联调\n- 【C】临时排障\n## 下周计划\n- 【A】上线"
	planA, planB := core.ItemID(core.SectionPlan, "【A】接口联调"), core.ItemID(core.SectionPlan, "【A】压测")
	doneA := core.ItemID(core.SectionDone, "【A】完成接口联调")

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		body, _ := io.ReadAll(r.Body)
		reply := cur
		switch {
		case strings.Contains(string(body), "上周计划："):
			reply = `[{"plan":"` + planA + `","status":"done","matches":["` + doneA + `"]},{"plan":"` + planB + `","status":"delayed","matches":[]}]`
		case strings.Contains(string(body), "2026-W43"):
			reply = prev
		}
		data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": reply}}}})
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	e.mustRun("sk-review-test-0000", "llm", "add", "fake", "--provider", "openai", "--base-url", srv.URL, "--model", "m")

	e.mustRun("【周报】张三 2026-10-22\n联调与压测\n", ws("report", "add")...)
	e.mustRun("【周报】张三 2026-10-29\n联调完成\n", ws("report", "add")...)
	e.mustRun("", ws("report", "normalize", "--week", "2026-W43", "--person", "张三", "--yes")...)
	e.mustRun("", ws("report", "normalize", "--week", "2026-W44", "--person", "张三", "--yes")...)

	// 上周未定稿 → 7。
	res := e.run("", ws("report", "review", "2026-W44", "张三", "--json")...)
	if res.code != NotFinal || !strings.Contains(res.stderr, "NOT_FINAL") {
		t.Fatalf("上周未定稿应返回 7: %+v", res)
	}
	e.mustRun("", ws("report", "finalize", "2026-W43", "张三")...)

	// AI 建议：不带 --yes 只预览、退出 8。
	before := atomic.LoadInt32(&calls)
	res = e.run("", ws("report", "review", "suggest", "2026-W44", "张三")...)
	if res.code != NeedsConfirm || atomic.LoadInt32(&calls) != before || !strings.Contains(res.stdout, planA) {
		t.Fatalf("suggest 预览: code=%d out=%s", res.code, res.stdout)
	}
	e.mustRun("", ws("report", "review", "suggest", "2026-W44", "张三", "--yes")...)

	out := e.mustRun("", ws("report", "review", "2026-W44", "张三", "--json")...).jsonOut(t)
	if out["alignment"] != 0.5 {
		t.Fatalf("对齐率应为 0.5: %v", out["alignment"])
	}

	// 人工把 planB 改为部分完成：(1 + 0.5) / 2 = 75%。
	e.mustRun("", ws("report", "review", "set", "2026-W44", "张三", planB, "--status", "partial", "--note", "压测一半")...)
	text := e.mustRun("", ws("report", "review", "2026-W44", "张三")...).stdout
	if !strings.Contains(text, "对齐率 75%") || !strings.Contains(text, "（计划外）") {
		t.Fatalf("文本输出:\n%s", text)
	}

	// 顺延幂等。
	carry := e.mustRun("", ws("report", "carry", "2026-W44", "张三", "--json")...).jsonOut(t)
	if carry["added"] != float64(1) {
		t.Fatalf("顺延 = %v", carry["added"])
	}
	carry = e.mustRun("", ws("report", "carry", "2026-W44", "张三", "--json")...).jsonOut(t)
	if carry["added"] != float64(0) {
		t.Fatalf("重复顺延应为 0: %v", carry["added"])
	}
	draft := e.mustRun("", ws("report", "draft", "show", "2026-W44", "张三", "--json")...).jsonOut(t)
	if body, _ := draft["body"].(string); strings.Count(body, "【A】压测（顺延）") != 1 {
		t.Fatalf("顺延后正文:\n%s", body)
	}

	if res := e.run("", ws("report", "review", "set", "2026-W44", "张三", planA, "--status", "bad")...); res.code != Error {
		t.Fatalf("非法状态应失败: %+v", res)
	}
}
