package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReportMonthFlow(t *testing.T) {
	e := newTestEnv(t)
	e.initWorkspace()
	e.setNow()
	ws := func(a ...string) []string { return append(a, "--workspace", e.workspace) }

	e.addPersonJSON("勇旭", "--group", "框架组", "--role", "lead", "--product", "产品线A")
	e.addPersonJSON("赵六", "--group", "平台组", "--role", "lead", "--product", "产品线B")
	e.addPersonJSON("张三", "--group", "框架组", "--product", "产品线A")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body := string(b)
		reply := "## 本周完成\n- 【产品线A】完成联调\n## 下周计划\n- 无"
		switch {
		case strings.Contains(body, "各周的周报"):
			reply = `[{"section":"本周完成","productLine":"产品线A","category":"功能开发","text":"本月完成联调","sources":["2026-W44"]}]`
		case strings.Contains(body, "各来源的周报"):
			reply = `[{"section":"本周完成","productLine":"产品线B","category":"功能开发","text":"本月网关上线","sources":["平台组月报"]}]`
		case strings.Contains(body, "各组员的周报"):
			reply = `[{"section":"本周完成","productLine":"产品线B","category":"功能开发","text":"网关上线","sources":["赵六"]}]`
		}
		data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": reply}}}})
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	e.mustRun("sk-month-test-0000", "llm", "add", "fake", "--provider", "openai", "--base-url", srv.URL, "--model", "m")

	// 收集日配置。
	cfg := e.mustRun("", ws("report", "config", "--month-day", "28", "--json")...).jsonOut(t)
	if cfg["monthDay"] != float64(28) {
		t.Fatalf("config = %v", cfg)
	}

	// W44 组报定稿。
	e.mustRun("【周报】张三 2026-10-29\n联调\n", ws("report", "add")...)
	e.mustRun("", ws("report", "normalize", "--week", "2026-W44", "--all", "--yes")...)
	e.mustRun("", ws("report", "finalize", "2026-W44", "张三")...)
	e.mustRun("", ws("report", "group", "generate", "--week", "2026-W44", "--group", "框架组", "--yes")...)
	e.mustRun("", ws("report", "group", "finalize", "--week", "2026-W44", "--group", "框架组")...)

	// 组月报：W40~W43 没有组报，默认返回 7 并列出。
	r := e.run("", ws("report", "month", "generate", "--month", "2026-10", "--group", "框架组", "--json")...)
	if r.code != NotFinal || !strings.Contains(r.stderr, "2026-W40") || strings.Contains(r.stderr, "2026-W44") {
		t.Fatalf("缺周应返回 7: %+v", r)
	}
	r = e.run("", ws("report", "month", "generate", "--month", "2026-10", "--group", "框架组", "--allow-missing")...)
	if r.code != NeedsConfirm {
		t.Fatalf("不带 --yes 应只预览: %+v", r)
	}
	e.mustRun("", ws("report", "month", "generate", "--month", "2026-10", "--group", "框架组", "--allow-missing", "--yes")...)
	gm := e.mustRun("", ws("report", "month", "show", "--month", "2026-10", "--group", "框架组", "--json")...).jsonOut(t)
	if body, _ := gm["body"].(string); !strings.Contains(body, "- 本月完成联调（2026-W44）") || !strings.Contains(body, "未纳入") {
		t.Fatalf("组月报:\n%s", body)
	}
	e.mustRun("", ws("report", "month", "finalize", "--month", "2026-10", "--group", "框架组")...)
	md := e.mustRun("", ws("report", "export", "month-group", "--month", "2026-10", "--group", "框架组", "--json")...).jsonOut(t)
	if md["fileName"] != "框架组-组月报-2026-10.md" {
		t.Fatalf("月报导出文件名 = %v", md["fileName"])
	}

	// 组长月报收集与状态：赵六 10-27 交（收集日 28 日之前，按时），勇旭未交。
	e.mustRun("【周报】赵六 2026-10-27\n本月网关上线\n", ws("report", "add", "--month", "2026-10")...)
	st := e.mustRun("", ws("report", "status", "--month", "2026-10", "--json")...).jsonOut(t)
	counts, _ := st["counts"].(map[string]any)
	if st["level"] != "lead-month" || counts["submitted"] != float64(1) || counts["missing"] != float64(1) {
		t.Fatalf("组长月报状态 = %v", st)
	}
	// 周列表不应混入月目录。
	if list := e.mustRun("", ws("report", "list", "--week", "2026-W44", "--json")...).stdout; strings.Contains(list, "lead-month") {
		t.Fatalf("周列表混入了月报: %s", list)
	}

	// 产品月报：组长月报归一定稿；当月各周没有产品报 → 默认 7。
	e.mustRun("", ws("report", "normalize", "--month", "2026-10", "--all", "--yes")...)
	e.mustRun("", ws("report", "finalize", "2026-10", "赵六", "--level", "lead-month")...)
	r = e.run("", ws("report", "month", "generate", "--month", "2026-10", "--level", "product", "--json")...)
	if r.code != NotFinal || !strings.Contains(r.stderr, "产品报") {
		t.Fatalf("缺周产品报应返回 7: %+v", r)
	}
	e.mustRun("", ws("report", "month", "generate", "--month", "2026-10", "--level", "product", "--allow-missing", "--yes")...)
	pm := e.mustRun("", ws("report", "month", "show", "--month", "2026-10", "--level", "product", "--json")...).jsonOut(t)
	if body, _ := pm["body"].(string); !strings.Contains(body, "## 产品线B\n\n### 本周完成\n#### 功能开发\n- 本月网关上线（平台组月报）") ||
		!strings.Contains(body, "框架组月报（勇旭）") {
		t.Fatalf("产品月报:\n%s", body)
	}
}
