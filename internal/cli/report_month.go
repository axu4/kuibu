package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/axu4/kuibu/internal/core"
	"github.com/axu4/kuibu/internal/llm"
	"github.com/axu4/kuibu/internal/store"
)

// reportMonth 实现 report month generate|show|edit|finalize|unfinalize：
// 组月报（--level group，当月各周定稿组报合并）与产品月报（--level product，
// 组长月报归一定稿 + 当月各周定稿产品报合并）。存储与生命周期同周报。
func reportMonth(ctx context.Context, args []string, common *commonFlags, stdin io.Reader, stdout, stderr io.Writer, env func(string) string) error {
	if len(args) == 0 {
		return usagef("report month 需要子命令: generate/show/edit/finalize/unfinalize")
	}
	sub, rest := args[0], args[1:]
	fs := newFlagSet("report month " + sub)
	addCommonFlags(fs, common)
	monthFlag := fs.String("month", "", "月份（如 2026-10），默认本月")
	level := fs.String("level", "group", "group（组月报）| product（产品月报）")
	group := fs.String("group", "", "组名（group），默认唯一有组长的组")
	allowMissing := fs.Bool("allow-missing", false, "缺周或有未定稿时仍然生成，并标注未纳入")
	model := fs.String("model", "", "模型名称，默认用默认模型")
	yes := fs.Bool("yes", false, "已人工确认预览内容，真正发送")
	dryRun := fs.Bool("dry-run", false, "只输出将发送的内容")
	force := fs.Bool("force", false, "覆盖已人工修改或已定稿的月报（旧版移入历史）")
	file := fs.String("file", "", "从文件读取新正文（edit，默认读 stdin）")
	pos, err := parseCommand(fs, rest)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("report month %s 不接受位置参数，用 --month / --level", sub)
	}
	if *level != "group" && *level != "product" {
		return usagef("非法 --level %q（group|product）", *level)
	}
	now, err := nowFrom(env)
	if err != nil {
		return err
	}
	month, err := store.ParseMonth(*monthFlag, now)
	if err != nil {
		return err
	}
	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	kind, id := store.ProductDirName, store.ProductReportID
	if *level == "group" {
		lead, _, err := core.MonthLead(ws, month, *group)
		if err != nil {
			return err
		}
		kind, id = store.GroupDirName, lead.ID
	}

	var d *store.Draft
	switch sub {
	case "generate":
		var plan *core.GroupPlan
		if *level == "group" {
			plan, err = core.PlanGroupMonth(ws, month, *group, *allowMissing)
		} else {
			plan, err = core.PlanProductMonth(ws, month, *allowMissing)
		}
		if err != nil {
			return err
		}
		if err := ws.CheckRegenerateKind(kind, month, id, *force); err != nil {
			return err
		}
		req := core.GroupRequest(plan)
		out := &llm.Outbound{Env: env}
		if *dryRun || !*yes {
			preview, _, err := out.Prepare(*model, req)
			if err != nil {
				return err
			}
			if !common.json && len(plan.Missing) > 0 {
				fmt.Fprintf(stdout, "未纳入（缺失或未定稿）：%s\n", strings.Join(plan.Missing, "、"))
			}
			if err := printPreview(preview, common, stdout); err != nil {
				return err
			}
			if *dryRun {
				return nil
			}
			return fmt.Errorf("%w：确认上面的内容无误后加 --yes 重新执行", llm.ErrNeedsConfirm)
		}
		preview, resp, err := out.Send(ctx, *model, req, true)
		if err != nil {
			return err
		}
		var warning string
		if *level == "group" {
			d, warning, err = core.SaveGroupReport(ws, plan, preview.ModelName, resp.Text, *force, now)
		} else {
			d, warning, err = core.SaveProductReport(ws, plan, preview.ModelName, resp.Text, *force, now)
		}
		if err != nil {
			return err
		}
		if warning != "" && !common.json {
			fmt.Fprintln(stdout, "! "+warning)
		}
	case "show":
		if d, err = ws.ReadDraftKind(kind, month, id); err != nil {
			return err
		}
		if common.json {
			return writeJSON(stdout, toGroupJSON(ws, d, true))
		}
		printGroupHeader(ws, d, stdout)
		fmt.Fprintln(stdout, d.Body)
		return nil
	case "edit":
		var data []byte
		if *file != "" {
			data, err = os.ReadFile(*file)
		} else {
			data, err = io.ReadAll(stdin)
		}
		if err != nil {
			return fmt.Errorf("读取正文失败: %w", err)
		}
		d, err = ws.EditDraftKind(kind, month, id, string(data), now)
	case "finalize":
		d, err = ws.FinalizeDraftKind(kind, month, id, now)
	case "unfinalize":
		d, err = ws.UnfinalizeDraftKind(kind, month, id)
	default:
		return usagef("未知 report month 子命令 %q", sub)
	}
	if err != nil {
		return err
	}
	upsertReportIndex(ws, month, kind, id, stderr)
	if common.json {
		return writeJSON(stdout, toGroupJSON(ws, d, false))
	}
	verb := map[string]string{"generate": "已生成草稿", "edit": "已保存修改", "finalize": "已定稿", "unfinalize": "已取消定稿"}[sub]
	noun := map[string]string{"group": "组月报", "product": "产品月报"}[*level]
	fmt.Fprintf(stdout, "%s %s：%s\n", noun, verb, month)
	return nil
}
