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

// reportProduct 实现 report product generate|show|edit|finalize|unfinalize：
// SE 按产品线汇总各组长定稿周报。存储、生命周期与组报共用一套。
func reportProduct(ctx context.Context, args []string, common *commonFlags, stdin io.Reader, stdout, stderr io.Writer, env func(string) string) error {
	if len(args) == 0 {
		return usagef("report product 需要子命令: generate/show/edit/finalize/unfinalize")
	}
	sub, rest := args[0], args[1:]
	fs := newFlagSet("report product " + sub)
	addCommonFlags(fs, common)
	weekFlag := fs.String("week", "", "周编号，默认本周")
	var (
		template, model, product, file *string
		allowMissing, yes, dryRun      *bool
		force                          *bool
	)
	switch sub {
	case "generate":
		template = fs.String("template", "product", "模板名")
		allowMissing = fs.Bool("allow-missing", false, "有组长未定稿时仍然生成，并标注未纳入")
		model = fs.String("model", "", "模型名称，默认用默认模型")
		yes = fs.Bool("yes", false, "已人工确认预览内容，真正发送")
		dryRun = fs.Bool("dry-run", false, "只输出将发送的内容")
		force = fs.Bool("force", false, "覆盖已人工修改或已定稿的产品报（旧版移入历史）")
	case "show":
		product = fs.String("product", "", "只输出某条产品线那一章")
	case "edit":
		file = fs.String("file", "", "从文件读取新正文（默认读 stdin）")
	case "finalize", "unfinalize":
	default:
		return usagef("未知 report product 子命令 %q", sub)
	}
	pos, err := parseCommand(fs, rest)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("report product %s 不接受位置参数，用 --week", sub)
	}
	now, err := nowFrom(env)
	if err != nil {
		return err
	}
	week, err := weekOrThis(*weekFlag, now)
	if err != nil {
		return err
	}
	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	const id = store.ProductReportID
	switch sub {
	case "generate":
		return productGenerate(ctx, ws, week, *template, *model, *allowMissing, *yes, *dryRun, *force, common, stdout, stderr, env, now)
	case "show":
		d, err := ws.ReadDraftKind(store.ProductDirName, week, id)
		if err != nil {
			return err
		}
		if *product != "" {
			ch, err := core.ProductChapter(d.Body, *product)
			if err != nil {
				return err
			}
			d.Body = ch
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
		d, err := ws.EditDraftKind(store.ProductDirName, week, id, string(data), now)
		if err != nil {
			return err
		}
		return productDone(ws, d, "已保存修改", common, stdout, stderr)
	case "finalize":
		d, err := ws.FinalizeDraftKind(store.ProductDirName, week, id, now)
		if err != nil {
			return err
		}
		return productDone(ws, d, "已定稿", common, stdout, stderr)
	default: // unfinalize
		d, err := ws.UnfinalizeDraftKind(store.ProductDirName, week, id)
		if err != nil {
			return err
		}
		return productDone(ws, d, "已取消定稿", common, stdout, stderr)
	}
}

// productGenerate 实现 report product generate。
func productGenerate(ctx context.Context, ws *store.Workspace, week, template, model string, allowMissing, yes, dryRun, force bool,
	common *commonFlags, stdout, stderr io.Writer, env func(string) string, now time.Time) error {
	plan, err := core.PlanProduct(ws, week, template, allowMissing)
	if err != nil {
		return err
	}
	if err := ws.CheckRegenerateKind(store.ProductDirName, week, store.ProductReportID, force); err != nil {
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
			fmt.Fprintf(stdout, "未纳入（未定稿）：%s\n", strings.Join(plan.Missing, "、"))
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
	d, warning, err := core.SaveProductReport(ws, plan, preview.ModelName, resp.Text, force, now)
	if err != nil {
		return err
	}
	if warning != "" && !common.json {
		fmt.Fprintln(stdout, "! "+warning)
	}
	return productDone(ws, d, "已生成产品报草稿", common, stdout, stderr)
}

// productDone 更新索引并输出结果。
func productDone(ws *store.Workspace, d *store.Draft, verb string, common *commonFlags, stdout, stderr io.Writer) error {
	upsertReportIndex(ws, d.Week, store.ProductDirName, d.Person, stderr)
	if common.json {
		return writeJSON(stdout, toGroupJSON(ws, d, false))
	}
	fmt.Fprintf(stdout, "%s：%s\n", verb, d.Week)
	return nil
}
