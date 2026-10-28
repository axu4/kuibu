package cli

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axu4/kuibu/internal/store"
)

// monthTestNow 是月报新流程测试的固定「现在」。
func monthTestNow() time.Time {
	return time.Date(2026, 10, 26, 18, 0, 0, 0, time.Local)
}

// monthTestProjectID 取出 report project add --json 里的项目 id。
func monthTestProjectID(t *testing.T, out map[string]any) string {
	t.Helper()
	project, _ := out["project"].(map[string]any)
	id, _ := project["id"].(string)
	if id == "" {
		t.Fatalf("项目 id 为空: %v", out)
	}
	return id
}

// monthTestPull 取出 scope / pull 输出里的拉取记录。
func monthTestPull(t *testing.T, out map[string]any) map[string]any {
	t.Helper()
	pull, ok := out["pull"].(map[string]any)
	if !ok {
		t.Fatalf("输出缺少 pull: %v", out)
	}
	return pull
}

// monthTestWeeks 把 JSON 数组转成 []string。
func monthTestStrings(t *testing.T, v any) []string {
	t.Helper()
	raw, ok := v.([]any)
	if !ok {
		t.Fatalf("不是 JSON 数组: %v", v)
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		s, _ := item.(string)
		out = append(out, s)
	}
	return out
}

// monthTestSourceIncluded 判断拉取记录里是否纳入了某份周组报。
func monthTestSourceIncluded(t *testing.T, pull map[string]any, week, projectID string) bool {
	t.Helper()
	raw, _ := pull["sources"].([]any)
	for _, item := range raw {
		src, _ := item.(map[string]any)
		if src["week"] == week && src["project"] == projectID {
			return true
		}
	}
	return false
}

// monthTestItemID 在候选的某个池里按文本找条目 id。
func monthTestItemID(t *testing.T, candidates map[string]any, pool, text string) string {
	t.Helper()
	raw, _ := candidates[pool].([]any)
	for _, item := range raw {
		it, _ := item.(map[string]any)
		if it["text"] == text {
			id, _ := it["id"].(string)
			return id
		}
	}
	t.Fatalf("候选 %s 里找不到 %q: %v", pool, text, candidates[pool])
	return ""
}

func TestReportMonthProjectFlow(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()
	ws := func(a ...string) []string { return append(a, "--workspace", e.workspace) }

	lead := e.addPersonJSON("陈组长", "--group", "框架组", "--role", "lead")
	leadID, _ := lead["id"].(string)
	e.addPersonJSON("张三", "--group", "框架组")
	p1 := monthTestProjectID(t, e.mustRun("", ws("report", "project", "add", "云平台项目", "--json")...).jsonOut(t))
	p2 := monthTestProjectID(t, e.mustRun("", ws("report", "project", "add", "数据中台项目", "--json")...).jsonOut(t))

	// 直接写入各周按项目组报：W40–W43 定稿，W44 未定稿 / 未提交；W39、W45 是相邻周。
	st, err := store.Open(e.workspace)
	if err != nil {
		t.Fatal(err)
	}
	now := monthTestNow()
	write := func(week, projectID, body string, final bool) {
		t.Helper()
		if _, err := st.SaveGeneratedDraft(store.GeneratedDraft{
			Kind: store.GroupReportKind(projectID), Group: "框架组", Person: leadID, Name: "陈组长",
			Week: week, Body: body, Now: now,
		}, false); err != nil {
			t.Fatalf("写 %s 周组报失败: %v", week, err)
		}
		if final {
			if _, err := st.FinalizeDraftKind(store.GroupReportKind(projectID), week, leadID, now); err != nil {
				t.Fatalf("定稿 %s 周组报失败: %v", week, err)
			}
		}
	}
	write("2026-W39", p1, "## 本周总结\n- 上月收尾\n## 下周计划\n- 上月计划\n", true)
	write("2026-W40", p1, "## 本周总结\n- 完成退款接口联调\n- 排查线上超时\n## 下周计划\n- 补齐退款异常分支\n", true)
	write("2026-W41", p1, "## 本周总结\n- 完成退款接口联调\n- 对账服务改造完成\n## 下周计划\n- 完成对账服务回归\n", true)
	write("2026-W42", p1, "## 本周总结\n- 对账服务回归通过\n## 下周计划\n- 对账服务灰度 50%\n", true)
	write("2026-W43", p1, "## 本周总结\n- 对账服务灰度 50% 无异常\n## 下周计划\n- 对账服务全量发布\n", true)
	write("2026-W44", p1, "## 本周总结\n- 还没定稿\n## 下周计划\n- 观察指标\n", false)
	write("2026-W45", p1, "## 本周总结\n- 下月开局\n## 下周计划\n- 下月计划\n", true)
	write("2026-W40", p2, "## 本周总结\n- 报表导出支持 Excel\n## 下周计划\n- 梳理报表权限模型\n", true)
	write("2026-W41", p2, "## 本周总结\n- 报表权限模型评审通过\n## 下周计划\n- 报表权限模型开发\n", true)
	write("2026-W42", p2, "## 本周总结\n- 报表权限模型联调通过\n## 下周计划\n- 报表权限模型灰度\n", true)
	write("2026-W43", p2, "## 本周总结\n- 报表权限模型灰度无异常\n## 下周计划\n- 报表权限模型全量发布\n", true)

	// sources：本期默认 W40–W44，候选周含相邻 W39 / W45，缺周要显式列出。
	src := e.mustRun("", ws("report", "month", "sources", "--month", "2026-10", "--group", "框架组", "--json")...).jsonOut(t)
	if src["month"] != "2026-10" || src["group"] != "框架组" || src["leadName"] != "陈组长" {
		t.Fatalf("sources 头部 = %v", src)
	}
	if got := len(monthTestStrings(t, src["weeks"])); got != 7 {
		t.Fatalf("周视图 = %d 行，期望 7", got)
	}
	if got := len(monthTestStrings(t, src["scopeWeeks"])); got != 5 {
		t.Fatalf("默认本期 = %v", src["scopeWeeks"])
	}
	missing := strings.Join(monthTestStrings(t, src["missing"]), "|")
	if !strings.Contains(missing, "云平台项目 W44（未定稿）") || !strings.Contains(missing, "数据中台项目 W44（未提交）") {
		t.Fatalf("缺周 = %v", src["missing"])
	}
	if total, _ := src["candidateTotal"].(float64); total <= 0 {
		t.Fatalf("候选统计 = %v", src["candidateTotal"])
	}
	if text := e.mustRun("", ws("report", "month", "sources", "--month", "2026-10", "--group", "框架组")...).stdout; !strings.Contains(text, "月报来源 2026-10") {
		t.Fatalf("sources 文本输出 = %q", text)
	}

	// scope：手工纳入上月最后一周 W39 和下月第一周 W45，去掉本月 W44。
	scoped := e.mustRun("", ws("report", "month", "scope", "--month", "2026-10", "--group", "框架组",
		"--weeks", "2026-W39,2026-W40,2026-W41,2026-W42,2026-W43,2026-W45", "--json")...).jsonOut(t)
	pull := monthTestPull(t, scoped)
	if weeks := monthTestStrings(t, pull["weeks"]); len(weeks) != 6 || weeks[0] != "2026-W39" || weeks[5] != "2026-W45" {
		t.Fatalf("自定义本期 = %v", pull["weeks"])
	}
	if !monthTestSourceIncluded(t, pull, "2026-W39", p1) || !monthTestSourceIncluded(t, pull, "2026-W45", p1) {
		t.Fatalf("相邻周定稿组报应自动纳入: %v", pull["sources"])
	}
	scopedSources, _ := scoped["sources"].(map[string]any)
	if missing := strings.Join(monthTestStrings(t, scopedSources["missing"]), "|"); strings.Contains(missing, "W44") {
		t.Fatalf("排除 W44 后不应再算缺周: %v", missing)
	}

	// candidates：下月计划取自最后一期（W45），更早计划默认进 archive。
	cand := e.mustRun("", ws("report", "month", "candidates", "--project", "云平台项目", "--month", "2026-10", "--group", "框架组", "--json")...).jsonOut(t)
	if cand["planFrom"] != "2026-W45" {
		t.Fatalf("planFrom = %v", cand["planFrom"])
	}
	counts, _ := cand["counts"].(map[string]any)
	if counts["summary"] != float64(7) || counts["plan"] != float64(1) || counts["archive"] != float64(5) || counts["total"] != float64(8) {
		t.Fatalf("自定义范围候选 = %v", counts)
	}
	if text := e.mustRun("", ws("report", "month", "candidates", "--project", "云平台项目", "--month", "2026-10", "--group", "框架组")...).stdout; !strings.Contains(text, "本月总结") || !strings.Contains(text, "下月计划取自 2026-W45") {
		t.Fatalf("candidates 文本输出 = %q", text)
	}

	// pull：去选 / 纳入一份定稿周组报。
	off := e.mustRun("", ws("report", "month", "pull", "--week", "2026-W43", "--project", "云平台项目", "--off", "--json")...).jsonOut(t)
	if monthTestSourceIncluded(t, monthTestPull(t, off), "2026-W43", p1) {
		t.Fatalf("去选后仍纳入 W43: %v", off)
	}
	on := e.mustRun("", ws("report", "month", "pull", "--week", "2026-W43", "--project", "云平台项目", "--on", "--json")...).jsonOut(t)
	if !monthTestSourceIncluded(t, monthTestPull(t, on), "2026-W43", p1) {
		t.Fatalf("重新纳入后缺少 W43: %v", on)
	}

	// scope --reset 恢复默认 W40–W44，W44 重新成为缺周。
	reset := e.mustRun("", ws("report", "month", "scope", "--month", "2026-10", "--group", "框架组", "--reset", "--json")...).jsonOut(t)
	if weeks := monthTestStrings(t, monthTestPull(t, reset)["weeks"]); len(weeks) != 5 || weeks[0] != "2026-W40" || weeks[4] != "2026-W44" {
		t.Fatalf("重置后本期 = %v", weeks)
	}
	resetSources, _ := reset["sources"].(map[string]any)
	if missing := strings.Join(monthTestStrings(t, resetSources["missing"]), "|"); !strings.Contains(missing, "云平台项目 W44（未定稿）") {
		t.Fatalf("重置后缺周 = %v", missing)
	}
	// 未定稿的周组报不能纳入（退出 7）。
	if r := e.run("", ws("report", "month", "pull", "--week", "2026-W44", "--project", "云平台项目", "--on")...); r.code != NotFinal {
		t.Fatalf("纳入未定稿周组报应返回 7: %+v", r)
	}

	// 默认范围下的候选：总结 5 · 计划 1（取自 W43）· 更早计划 3。
	cand = e.mustRun("", ws("report", "month", "candidates", "--project", "云平台项目", "--month", "2026-10", "--group", "框架组", "--json")...).jsonOut(t)
	counts, _ = cand["counts"].(map[string]any)
	if cand["planFrom"] != "2026-W43" || counts["summary"] != float64(5) || counts["plan"] != float64(1) ||
		counts["archive"] != float64(3) || counts["total"] != float64(6) {
		t.Fatalf("默认范围候选 = %v / planFrom=%v", counts, cand["planFrom"])
	}

	// move：把跨周合并的总结临时移到计划，再改回默认池（清掉 override）。
	mergedID := monthTestItemID(t, cand, "summary", "完成退款接口联调")
	moved := e.mustRun("", ws("report", "month", "move", mergedID, "--project", "云平台项目",
		"--section", "summary", "--to", "plan", "--month", "2026-10", "--group", "框架组", "--json")...).jsonOut(t)
	movedCounts, _ := moved["counts"].(map[string]any)
	if movedCounts["summary"] != float64(4) || movedCounts["plan"] != float64(2) {
		t.Fatalf("改归口后 = %v", movedCounts)
	}
	if text := e.mustRun("", ws("report", "month", "move", mergedID, "--project", "云平台项目",
		"--section", "summary", "--to", "summary", "--month", "2026-10", "--group", "框架组")...).stdout; !strings.Contains(text, "已把") || !strings.Contains(text, "本月总结") {
		t.Fatalf("move 文本输出 = %q", text)
	}
	if r := e.run("", ws("report", "month", "move", mergedID, "--project", "云平台项目",
		"--section", "summary", "--to", "bogus", "--json")...); r.code != Error || r.jsonErrorCode(t) != "USAGE" {
		t.Fatalf("非法归口应返回 USAGE/1: %+v", r)
	}

	// generate：缺周默认返回 7，加 --allow-missing 才生成。
	if r := e.run("", ws("report", "month", "generate", "--project", "云平台项目", "--month", "2026-10", "--group", "框架组", "--json")...); r.code != NotFinal {
		t.Fatalf("缺周生成应返回 7: %+v", r)
	}
	gen := e.mustRun("", ws("report", "month", "generate", "--project", "云平台项目", "--month", "2026-10", "--group", "框架组", "--allow-missing", "--json")...).jsonOut(t)
	if gen["status"] != "draft" || gen["project"] != p1 || gen["projectName"] != "云平台项目" {
		t.Fatalf("生成结果 = %v", gen)
	}
	wantPath := filepath.Join(e.workspace, "reports", "2026-10", "group", p1, leadID+".md")
	if gen["path"] != wantPath {
		t.Fatalf("月报路径 = %v，期望 %s", gen["path"], wantPath)
	}
	show := e.mustRun("", ws("report", "month", "show", "--project", "云平台项目", "--month", "2026-10", "--group", "框架组", "--json")...).jsonOut(t)
	body, _ := show["body"].(string)
	for _, want := range []string{"## 本月总结", "## 下月计划", "- 完成退款接口联调", "- 对账服务全量发布", "未纳入：W44（未定稿）"} {
		if !strings.Contains(body, want) {
			t.Fatalf("月报正文缺少 %q:\n%s", want, body)
		}
	}
	if show["sourceChanged"] != false {
		t.Fatalf("刚生成的月报不应来源已更新: %v", show)
	}
	if text := e.mustRun("", ws("report", "month", "show", "--project", "云平台项目", "--month", "2026-10", "--group", "框架组")...).stdout; !strings.Contains(text, "云平台项目") || !strings.Contains(text, "草稿") {
		t.Fatalf("show 文本输出 = %q", text)
	}

	// list：两个项目各自状态、候选数与缺周。
	rows := e.mustRun("", ws("report", "month", "list", "--month", "2026-10", "--group", "框架组", "--json")...).jsonArray(t)
	if len(rows) != 2 {
		t.Fatalf("list = %v", rows)
	}
	byProject := map[string]map[string]any{}
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		byProject[row["project"].(string)] = row
	}
	row1, row2 := byProject[p1], byProject[p2]
	if row1 == nil || row2 == nil {
		t.Fatalf("list 缺少项目: %v", rows)
	}
	if row1["status"] != "draft" || row2["status"] != "none" {
		t.Fatalf("list 状态 = %v / %v", row1["status"], row2["status"])
	}
	row1Counts, _ := row1["counts"].(map[string]any)
	if row1Counts["total"] != float64(6) {
		t.Fatalf("list p1 counts = %v", row1Counts)
	}
	if missing := strings.Join(monthTestStrings(t, row2["missing"]), "|"); !strings.Contains(missing, "W44（未提交）") {
		t.Fatalf("list p2 缺周 = %v", row2["missing"])
	}
	if text := e.mustRun("", ws("report", "month", "list", "--month", "2026-10", "--group", "框架组")...).stdout; !strings.Contains(text, "云平台项目") || !strings.Contains(text, "数据中台项目") {
		t.Fatalf("list 文本输出 = %q", text)
	}

	// generate --all：逐个项目生成。
	all := e.mustRun("", ws("report", "month", "generate", "--all", "--month", "2026-10", "--group", "框架组", "--allow-missing", "--json")...).jsonArray(t)
	if len(all) != 2 {
		t.Fatalf("--all 生成 = %v", all)
	}

	// edit：人工修改后生成要被拒绝（9），--force 才覆盖。
	edited := "## 本月总结\n\n- 人工补充的一条总结\n\n## 下月计划\n\n- 待补充下月计划。\n"
	e.mustRun(edited, ws("report", "month", "edit", "--project", "云平台项目", "--month", "2026-10", "--group", "框架组")...)
	editedShow := e.mustRun("", ws("report", "month", "show", "--project", "云平台项目", "--month", "2026-10", "--group", "框架组", "--json")...).jsonOut(t)
	if b, _ := editedShow["body"].(string); !strings.Contains(b, "人工补充的一条总结") {
		t.Fatalf("保存后的正文 = %q", b)
	}
	if r := e.run("", ws("report", "month", "generate", "--project", "云平台项目", "--month", "2026-10", "--group", "框架组", "--allow-missing", "--json")...); r.code != HumanEditsExist {
		t.Fatalf("人工修改后生成应返回 9: %+v", r)
	}

	// finalize + export：只认定稿，文件名带项目名。
	fin := e.mustRun("", ws("report", "month", "finalize", "--project", "云平台项目", "--month", "2026-10", "--group", "框架组", "--json")...).jsonOut(t)
	if fin["status"] != "final" {
		t.Fatalf("定稿结果 = %v", fin)
	}
	doc := e.mustRun("", ws("report", "export", "month-group", "--project", "云平台项目", "--month", "2026-10", "--group", "框架组", "--json")...).jsonOut(t)
	if doc["fileName"] != "框架组-组月报-2026-10-云平台项目.md" {
		t.Fatalf("导出文件名 = %v", doc["fileName"])
	}
	if content, _ := doc["content"].(string); !strings.Contains(content, "人工补充的一条总结") {
		t.Fatalf("导出内容 = %q", content)
	}

	// unfinalize 后 --force 重新生成，人工修改被覆盖。
	e.mustRun("", ws("report", "month", "unfinalize", "--project", "云平台项目", "--month", "2026-10", "--group", "框架组")...)
	if text := e.mustRun("", ws("report", "month", "generate", "--project", "云平台项目", "--month", "2026-10", "--group", "框架组", "--allow-missing", "--force")...).stdout; !strings.Contains(text, "已生成「云平台项目」项目月报草稿") {
		t.Fatalf("generate 文本输出 = %q", text)
	}
	regen := e.mustRun("", ws("report", "month", "show", "--project", "云平台项目", "--month", "2026-10", "--group", "框架组", "--json")...).jsonOut(t)
	if b, _ := regen["body"].(string); strings.Contains(b, "人工补充的一条总结") || !strings.Contains(b, "## 本月总结") {
		t.Fatalf("--force 重新生成后正文 = %q", b)
	}

	// 改归口后草稿标记「来源已更新」，list 里能看到。
	e.mustRun("", ws("report", "month", "move", mergedID, "--project", "云平台项目",
		"--section", "summary", "--to", "plan", "--month", "2026-10", "--group", "框架组")...)
	rows = e.mustRun("", ws("report", "month", "list", "--month", "2026-10", "--group", "框架组", "--json")...).jsonArray(t)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["project"] == p1 && row["sourceChanged"] != true {
			t.Fatalf("改归口后应标记来源已更新: %v", row)
		}
	}

	// 配置了项目时，不给 --project 应提示用法错误。
	if r := e.run("", ws("report", "month", "show", "--month", "2026-10", "--group", "框架组", "--json")...); r.code != Error || r.jsonErrorCode(t) != "USAGE" {
		t.Fatalf("缺 --project 应返回 USAGE/1: %+v", r)
	}
	if r := e.run("", ws("report", "month", "generate", "--month", "2026-10", "--group", "框架组", "--json")...); r.code != Error || r.jsonErrorCode(t) != "USAGE" {
		t.Fatalf("generate 缺 --project/--all 应返回 USAGE/1: %+v", r)
	}
}
