package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/axu4/kuibu/internal/core"
	"github.com/axu4/kuibu/internal/llm"
	"github.com/axu4/kuibu/internal/store"
)

// reportReview 实现 report review [set|suggest]：对照上周计划。
func reportReview(ctx context.Context, args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	if len(args) > 0 {
		switch args[0] {
		case "set":
			return reviewSet(args[1:], common, stdout, stderr, env)
		case "suggest":
			return reviewSuggest(ctx, args[1:], common, stdout, stderr, env)
		}
	}
	fs := newFlagSet("report review")
	addCommonFlags(fs, common)
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	ws, week, personID, err := draftTarget(pos, common, env, "用法: kuibu report review <week> <person> | review set ... | review suggest ...")
	if err != nil {
		return err
	}
	res, err := core.LoadReview(ws, week, personID)
	if err != nil {
		return err
	}
	return printReview(res, common, stdout)
}

// reviewSet 实现 report review set。
func reviewSet(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("report review set")
	addCommonFlags(fs, common)
	status := fs.String("status", "", "done|partial|delayed|canceled，空表示清除")
	var matches stringList
	fs.Var(&matches, "match", "对应的本周完成项 id，可重复")
	note := fs.String("note", "", "备注")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 3 {
		return usagef("用法: kuibu report review set <week> <person> <计划id> --status done|partial|delayed|canceled [--match id]... [--note 文本]")
	}
	ws, week, personID, err := draftTarget(pos[:2], common, env, "")
	if err != nil {
		return err
	}
	res, err := core.SetReviewItem(ws, week, personID, pos[2], *status, matches, *note)
	if err != nil {
		return err
	}
	upsertReportIndex(ws, week, store.DraftDirName, personID, stderr)
	return printReview(res, common, stdout)
}

// reviewSuggest 实现 report review suggest：让 AI 给出对应关系与状态建议。
// 外发组员内容：不带 --yes 时输出预览并以 NEEDS_CONFIRM(8) 退出。
func reviewSuggest(ctx context.Context, args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("report review suggest")
	addCommonFlags(fs, common)
	model := fs.String("model", "", "模型名称，默认用默认模型")
	yes := fs.Bool("yes", false, "已人工确认预览内容，真正发送")
	dryRun := fs.Bool("dry-run", false, "只输出将发送的内容")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	ws, week, personID, err := draftTarget(pos, common, env, "用法: kuibu report review suggest <week> <person> [--model m] [--dry-run|--yes]")
	if err != nil {
		return err
	}
	res, err := core.LoadReview(ws, week, personID)
	if err != nil {
		return err
	}
	if len(res.Plans) == 0 {
		return usagef("上周没有计划条目，无需对照")
	}
	req := core.ReviewSuggestRequest(res)
	out := &llm.Outbound{Env: env}
	if *dryRun || !*yes {
		preview, _, err := out.Prepare(*model, req)
		if err != nil {
			return err
		}
		if err := printPreview(preview, common, stdout); err != nil {
			return err
		}
		if *dryRun {
			return nil
		}
		return fmt.Errorf("%w：确认上面的内容无误后加 --yes 重新执行", llm.ErrNeedsConfirm)
	}
	_, resp, err := out.Send(ctx, *model, req, true)
	if err != nil {
		return err
	}
	res, applied, err := core.ApplySuggestion(ws, week, personID, resp.Text)
	if err != nil {
		return err
	}
	upsertReportIndex(ws, week, store.DraftDirName, personID, stderr)
	if !common.json {
		fmt.Fprintf(stdout, "AI 建议已写入 %d 条（人工标过的不覆盖），请逐条确认：\n", applied)
	}
	return printReview(res, common, stdout)
}

// reportCarry 实现 report carry：把未完成计划顺延到本周「下周计划」。
func reportCarry(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("report carry")
	addCommonFlags(fs, common)
	var items stringList
	fs.Var(&items, "items", "只顺延这些计划 id，可重复")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	ws, week, personID, err := draftTarget(pos, common, env, "用法: kuibu report carry <week> <person> [--items id]...")
	if err != nil {
		return err
	}
	now, err := nowFrom(env)
	if err != nil {
		return err
	}
	res, added, err := core.Carry(ws, week, personID, items, now)
	if err != nil {
		return err
	}
	upsertReportIndex(ws, week, store.DraftDirName, personID, stderr)
	if common.json {
		return writeJSON(stdout, map[string]any{"added": added, "review": res})
	}
	fmt.Fprintf(stdout, "已顺延 %d 条到 %s 下周计划\n", added, week)
	return nil
}

// printReview 输出对照结果。
func printReview(res *core.ReviewResult, common *commonFlags, stdout io.Writer) error {
	if common.json {
		return writeJSON(stdout, res)
	}
	fmt.Fprintf(stdout, "%s 对照 %s 计划  %s  对齐率 %s\n", res.Week, res.PrevWeek, orDash(res.Name), alignmentLabel(res.Alignment))
	c := res.Counts
	fmt.Fprintf(stdout, "完成 %d · 部分 %d · 延期 %d · 取消 %d · 未标 %d\n\n", c.Done, c.Partial, c.Delayed, c.Canceled, c.Unmarked)
	fmt.Fprintln(stdout, "上周计划:")
	for _, p := range res.Plans {
		extra := ""
		if len(p.Matches) > 0 {
			extra = " → " + strings.Join(p.Matches, ",")
		}
		if p.Source == "ai" {
			extra += "（AI 建议）"
		}
		if p.Carried {
			extra += "（已顺延）"
		}
		fmt.Fprintf(stdout, "  [%s] %-4s %s%s\n", p.ID, reviewStatusLabel(p.Status), p.Text, extra)
	}
	fmt.Fprintln(stdout, "本周完成:")
	for _, d := range res.Done {
		mark := ""
		if d.Unplanned {
			mark = "（计划外）"
		}
		fmt.Fprintf(stdout, "  [%s] %s%s\n", d.ID, d.Text, mark)
	}
	return nil
}

// reviewStatusLabel 返回对照状态中文。
func reviewStatusLabel(s string) string {
	switch s {
	case store.ReviewDone:
		return "完成"
	case store.ReviewPartial:
		return "部分"
	case store.ReviewDelayed:
		return "延期"
	case store.ReviewCanceled:
		return "取消"
	}
	return "未标"
}

// alignmentLabel 把对齐率显示成百分比。
func alignmentLabel(v *float64) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", *v*100)
}
