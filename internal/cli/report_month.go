package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/axu4/kuibu/internal/core"
	"github.com/axu4/kuibu/internal/llm"
	"github.com/axu4/kuibu/internal/store"
)

// reportMonth 实现 report month。
//
// 配置了报送项目（report project add）后，组月报走按项目、确定性、不调 AI 的新流程：
// sources/scope/pull/candidates/move/list + 按项目 generate/show/edit/finalize/unfinalize/export。
// 没配置报送项目时，generate/show/edit/finalize/unfinalize --level group 仍走旧的 LLM 组月报；
// --level product（SE 产品月报）始终走旧路径，不在本次范围内。
func reportMonth(ctx context.Context, args []string, common *commonFlags, stdin io.Reader, stdout, stderr io.Writer, env func(string) string) error {
	if len(args) == 0 {
		return usagef("report month 需要子命令: sources/scope/pull/candidates/move/generate/list/show/edit/finalize/unfinalize")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "sources", "scope", "pull", "candidates", "move", "list", "generate", "show", "edit", "finalize", "unfinalize":
	default:
		return usagef("未知 report month 子命令 %q", sub)
	}

	fs := newFlagSet("report month " + sub)
	addCommonFlags(fs, common)
	monthFlag := fs.String("month", "", "月份（如 2026-10），默认本月")
	level := fs.String("level", "group", "group（组月报）| product（产品月报）")
	group := fs.String("group", "", "组名（group），默认唯一有组长的组")
	projectFlag := fs.String("project", "", "报送项目 id 或名称")
	allProjects := fs.Bool("all", false, "逐个项目处理（generate）")
	weeksFlag := fs.String("weeks", "", "本期周范围，逗号分隔（scope），如 2026-W39,2026-W40")
	reset := fs.Bool("reset", false, "恢复默认本期周范围（scope）")
	weekFlag := fs.String("week", "", "周编号（pull）")
	onFlag := fs.Bool("on", false, "纳入这份周组报（pull，默认行为）")
	offFlag := fs.Bool("off", false, "去选这份周组报（pull）")
	sectionFlag := fs.String("section", "", "原始节：summary|plan（move）")
	toFlag := fs.String("to", "", "归口：summary|plan|archive|excluded（move）")
	allowMissing := fs.Bool("allow-missing", false, "缺周或有未定稿时仍然生成，并标注未纳入")
	model := fs.String("model", "", "模型名称，默认用默认模型（旧 LLM 流程）")
	yes := fs.Bool("yes", false, "已人工确认预览内容，真正发送（旧 LLM 流程）")
	dryRun := fs.Bool("dry-run", false, "只输出将发送的内容（旧 LLM 流程）")
	force := fs.Bool("force", false, "覆盖已人工修改或已定稿的月报（旧版移入历史）")
	file := fs.String("file", "", "从文件读取新正文（edit，默认读 stdin）")
	pos, err := parseCommand(fs, rest)
	if err != nil {
		return err
	}
	if sub == "move" {
		if err := requireArgs(pos, 1, "用法: kuibu report month move <itemID> --project P --section summary|plan --to summary|plan|archive|excluded [--month M] [--group G]"); err != nil {
			return err
		}
	} else if len(pos) > 0 {
		return usagef("report month %s 不接受位置参数，用 --month / --group / --project", sub)
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

	// 产品月报（SE）沿用旧路径。
	if *level == "product" {
		switch sub {
		case "generate", "show", "edit", "finalize", "unfinalize":
		default:
			return usagef("report month %s 只支持 --level group（组月报）", sub)
		}
		return reportMonthLegacy(ctx, ws, sub, *level, month, *group, *allowMissing, *yes, *dryRun, *force,
			*model, *file, common, stdin, stdout, stderr, env, now)
	}

	projects, err := ws.ReportProjects()
	if err != nil {
		return err
	}
	if len(projects) == 0 {
		// 没配置报送项目：只有旧的单份组月报 / 产品月报可选。
		switch sub {
		case "sources", "scope", "pull", "candidates", "move", "list":
			return usagef("还没有配置报送项目，report month %s 需要先 report project add <名称>", sub)
		}
		return reportMonthLegacy(ctx, ws, sub, *level, month, *group, *allowMissing, *yes, *dryRun, *force,
			*model, *file, common, stdin, stdout, stderr, env, now)
	}

	// 配置了报送项目：组月报走按项目的新流程。
	return reportMonthProjects(ws, reportMonthOpts{
		sub:          sub,
		month:        month,
		group:        *group,
		projectArg:   *projectFlag,
		allProjects:  *allProjects,
		weeksArg:     *weeksFlag,
		reset:        *reset,
		weekArg:      *weekFlag,
		on:           *onFlag,
		off:          *offFlag,
		section:      *sectionFlag,
		to:           *toFlag,
		allowMissing: *allowMissing,
		force:        *force,
		file:         *file,
		pos:          pos,
		now:          now,
		common:       common,
		stdin:        stdin,
		stdout:       stdout,
		stderr:       stderr,
	})
}

// reportMonthOpts 是 report month 新流程（按项目）的参数。
type reportMonthOpts struct {
	sub          string
	month        string
	group        string
	projectArg   string
	allProjects  bool
	weeksArg     string
	reset        bool
	weekArg      string
	on           bool
	off          bool
	section      string
	to           string
	allowMissing bool
	force        bool
	file         string
	pos          []string
	now          time.Time
	common       *commonFlags
	stdin        io.Reader
	stdout       io.Writer
	stderr       io.Writer
}

// reportMonthLegacy 是没配置报送项目（或 --level product）时的旧行为：
// 组月报合并当月各周单份组报、产品月报合并组长月报与各周产品报，都由 LLM 生成。
func reportMonthLegacy(ctx context.Context, ws *store.Workspace, sub, level, month, group string,
	allowMissing, yes, dryRun, force bool, model, file string,
	common *commonFlags, stdin io.Reader, stdout, stderr io.Writer, env func(string) string, now time.Time) error {
	kind, id := store.ProductDirName, store.ProductReportID
	if level == "group" {
		lead, _, err := core.MonthLead(ws, month, group)
		if err != nil {
			return err
		}
		kind, id = store.GroupDirName, lead.ID
	}

	var d *store.Draft
	var err error
	switch sub {
	case "generate":
		var plan *core.GroupPlan
		if level == "group" {
			plan, err = core.PlanGroupMonth(ws, month, group, allowMissing)
		} else {
			plan, err = core.PlanProductMonth(ws, month, allowMissing)
		}
		if err != nil {
			return err
		}
		if err := ws.CheckRegenerateKind(kind, month, id, force); err != nil {
			return err
		}
		req := core.GroupRequest(plan)
		out := &llm.Outbound{Env: env}
		if dryRun || !yes {
			preview, _, err := out.Prepare(model, req)
			if err != nil {
				return err
			}
			if !common.json && len(plan.Missing) > 0 {
				fmt.Fprintf(stdout, "未纳入（缺失或未定稿）：%s\n", strings.Join(plan.Missing, "、"))
			}
			if err := printPreview(preview, common, stdout); err != nil {
				return err
			}
			if dryRun {
				return nil
			}
			return fmt.Errorf("%w：确认上面的内容无误后加 --yes 重新执行", llm.ErrNeedsConfirm)
		}
		preview, resp, err := out.Send(ctx, model, req, true)
		if err != nil {
			return err
		}
		var warning string
		if level == "group" {
			d, warning, err = core.SaveGroupReport(ws, plan, preview.ModelName, resp.Text, force, now)
		} else {
			d, warning, err = core.SaveProductReport(ws, plan, preview.ModelName, resp.Text, force, now)
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
		if file != "" {
			data, err = os.ReadFile(file)
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
	noun := map[string]string{"group": "组月报", "product": "产品月报"}[level]
	fmt.Fprintf(stdout, "%s %s：%s\n", noun, verb, month)
	return nil
}
