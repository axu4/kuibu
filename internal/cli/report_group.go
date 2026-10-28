package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/axu4/kuibu/internal/core"
	"github.com/axu4/kuibu/internal/llm"
	"github.com/axu4/kuibu/internal/store"
)

// groupJSON 是组报的 JSON 输出。
type groupJSON struct {
	draftJSON
	Group         string   `json:"group"`
	Project       string   `json:"project,omitempty"`
	ProjectName   string   `json:"projectName,omitempty"`
	Sources       []string `json:"sources"`
	Missing       []string `json:"missing"`
	SourceChanged bool     `json:"sourceChanged"`
}

// reportGroup 分发 report group 子命令。
func reportGroup(ctx context.Context, args []string, common *commonFlags, stdin io.Reader, stdout, stderr io.Writer, env func(string) string) error {
	if len(args) == 0 {
		return usagef("report group 需要子命令: generate/assemble/list/show/edit/finalize/unfinalize/submit")
	}
	sub, rest := args[0], args[1:]
	fs := newFlagSet("report group " + sub)
	addCommonFlags(fs, common)
	weekFlag := fs.String("week", "", "周编号，默认本周")
	group := fs.String("group", "", "组名，默认唯一有组长的组")
	projectFlag := fs.String("project", "", "报送项目 id 或名称；给出时操作该项目的独立组报")
	var (
		template        *string
		allowMissing    *bool
		allowUnreviewed *bool
		model           *string
		yes, dryRun     *bool
		force           *bool
		file            *string
	)
	switch sub {
	case "generate":
		template = fs.String("template", "group", "模板名")
		allowMissing = fs.Bool("allow-missing", false, "有组员未定稿时仍然生成，并在组报里标注未纳入")
		model = fs.String("model", "", "模型名称，默认用默认模型")
		yes = fs.Bool("yes", false, "已人工确认预览内容，真正发送")
		dryRun = fs.Bool("dry-run", false, "只输出将发送的内容")
		force = fs.Bool("force", false, "覆盖已人工修改或已定稿的组报（旧版移入历史）")
	case "edit":
		file = fs.String("file", "", "从文件读取新正文（默认读 stdin）")
	case "assemble":
		allowUnreviewed = fs.Bool("allow-unreviewed", false, "有未判条目时仍然生成")
		force = fs.Bool("force", false, "覆盖已人工修改或已定稿的组报（旧版移入历史）")
	case "list":
	case "show", "finalize", "unfinalize", "submit":
	default:
		return usagef("未知 report group 子命令 %q", sub)
	}
	pos, err := parseCommand(fs, rest)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("report group %s 不接受位置参数，用 --week / --group", sub)
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

	if sub == "generate" {
		return groupGenerate(ctx, ws, week, *group, *template, *model, *allowMissing, *yes, *dryRun, *force, common, stdout, stderr, env, now)
	}
	if sub == "assemble" {
		return groupAssemble(ws, week, *group, *projectFlag, *force, *allowUnreviewed, common, stdout, stderr, now)
	}
	if sub == "list" {
		return groupList(ws, week, *group, common, stdout)
	}

	lead, _, err := core.ResolveLead(ws, week, *group)
	if err != nil {
		return err
	}
	kind := store.GroupDirName
	if *projectFlag != "" {
		p, err := projectByIDOrName(ws, *projectFlag)
		if err != nil {
			return err
		}
		kind = store.GroupReportKind(p.ID)
	}
	switch sub {
	case "show":
		var d *store.Draft
		var err error
		if kind == store.GroupDirName {
			d, err = ws.ReadGroupReport(week, lead.ID)
		} else {
			d, err = ws.ReadDraftKind(kind, week, lead.ID)
		}
		if err != nil {
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
		d, err := ws.EditDraftKind(kind, week, lead.ID, string(data), now)
		if err != nil {
			return err
		}
		return groupDone(ws, d, "已保存修改", common, stdout, stderr)
	case "finalize":
		d, err := ws.FinalizeDraftKind(kind, week, lead.ID, now)
		if err != nil {
			return err
		}
		return groupDone(ws, d, "已定稿", common, stdout, stderr)
	case "unfinalize":
		d, err := ws.UnfinalizeDraftKind(kind, week, lead.ID)
		if err != nil {
			return err
		}
		return groupDone(ws, d, "已取消定稿", common, stdout, stderr)
	default: // submit
		if kind != store.GroupDirName {
			return usagef("按项目带入 SE 暂未实现；请先导出该项目组报")
		}
		r, err := core.SubmitGroupReport(ws, week, lead.ID, now)
		if err != nil {
			return err
		}
		upsertReportIndex(ws, week, store.ReportLevelLead, lead.ID, stderr)
		if common.json {
			return writeJSON(stdout, reportJSONFromStore(ws, r, false))
		}
		fmt.Fprintf(stdout, "已把 %s 组报带入 SE（%s 名下）\n", week, r.Name)
		return nil
	}
}

// groupGenerate 实现 report group generate。
func groupGenerate(ctx context.Context, ws *store.Workspace, week, group, template, model string, allowMissing, yes, dryRun, force bool,
	common *commonFlags, stdout, stderr io.Writer, env func(string) string, now time.Time) error {
	plan, err := core.PlanGroupReport(ws, week, group, template, allowMissing)
	if err != nil {
		return err
	}
	if err := ws.CheckRegenerateKind(store.GroupDirName, week, plan.LeadID, force); err != nil {
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
	d, warning, err := core.SaveGroupReport(ws, plan, preview.ModelName, resp.Text, force, now)
	if err != nil {
		return err
	}
	if warning != "" && !common.json {
		fmt.Fprintln(stdout, "! "+warning)
	}
	return groupDone(ws, d, "已生成组报草稿", common, stdout, stderr)
}

// groupDone 更新索引并输出结果。
func groupDone(ws *store.Workspace, d *store.Draft, verb string, common *commonFlags, stdout, stderr io.Writer) error {
	upsertReportIndex(ws, d.Week, draftKindOf(d), d.Person, stderr)
	if common.json {
		return writeJSON(stdout, toGroupJSON(ws, d, false))
	}
	fmt.Fprintf(stdout, "%s：%s %s 组\n", verb, d.Week, orDash(d.Group))
	return nil
}

// toGroupJSON 把组报转成 JSON。
func toGroupJSON(ws *store.Workspace, d *store.Draft, withBody bool) groupJSON {
	dj := draftJSONFrom(ws, d, nil, withBody)
	dj.Path = filepath.Join(ws.ReportsDir(), d.Week, draftKindOf(d), d.Person+".md")
	out := groupJSON{
		draftJSON:     dj,
		Group:         d.Group,
		Sources:       nonNilStrings(d.Sources),
		Missing:       nonNilStrings(d.Missing),
		SourceChanged: groupSourceChanged(ws, d),
	}
	if projectID := store.ProjectIDFromGroupKind(d.Kind); projectID != "" {
		out.Project = projectID
		if projects, err := ws.ReportProjects(); err == nil {
			out.ProjectName = store.ReportProjectName(projects, projectID)
		}
	}
	return out
}

// printGroupHeader 输出组报头部信息。
func printGroupHeader(ws *store.Workspace, d *store.Draft, stdout io.Writer) {
	label := map[string]string{store.DraftStatusDraft: "草稿", store.DraftStatusFinal: "已定稿"}[d.Status]
	if groupSourceChanged(ws, d) {
		label += "·候选已更新"
	}
	fmt.Fprintf(stdout, "%s  %s 组  %s  模型: %s\n", d.Week, orDash(d.Group), label, orDash(d.Model))
	if len(d.Missing) > 0 {
		fmt.Fprintf(stdout, "未纳入: %s\n", strings.Join(d.Missing, "、"))
	}
	fmt.Fprintln(stdout)
}

func nonNilStrings(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

// runTemplate 实现 template list|show|edit。
func runTemplate(args []string, common *commonFlags, stdin io.Reader, stdout, stderr io.Writer, env func(string) string) error {
	if len(args) == 0 {
		return usagef("template 需要子命令: list/show/edit")
	}
	sub, rest := args[0], args[1:]
	fs := newFlagSet("template " + sub)
	addCommonFlags(fs, common)
	file := fs.String("file", "", "从文件读取模板（edit，默认读 stdin）")
	pos, err := parseCommand(fs, rest)
	if err != nil {
		return err
	}
	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	switch sub {
	case "list":
		names, err := ws.ListTemplates()
		if err != nil {
			return err
		}
		if common.json {
			return writeJSON(stdout, names)
		}
		for _, n := range names {
			fmt.Fprintln(stdout, n)
		}
		return nil
	case "show":
		if err := requireArgs(pos, 1, "用法: kuibu template show <名称>"); err != nil {
			return err
		}
		body, isDefault, err := ws.ReadTemplate(pos[0])
		if err != nil {
			return err
		}
		if common.json {
			return writeJSON(stdout, map[string]any{
				"name": pos[0], "default": isDefault, "body": body, "sections": core.ParseTemplate(body),
			})
		}
		fmt.Fprint(stdout, body)
		return nil
	case "edit":
		if err := requireArgs(pos, 1, "用法: kuibu template edit <名称> [--file f] < 模板"); err != nil {
			return err
		}
		var data []byte
		if *file != "" {
			data, err = os.ReadFile(*file)
		} else {
			data, err = io.ReadAll(stdin)
		}
		if err != nil {
			return fmt.Errorf("读取模板失败: %w", err)
		}
		if len(core.ParseTemplate(string(data))) == 0 {
			return usagef("模板至少要有一个「## 栏目」")
		}
		if err := ws.WriteTemplate(pos[0], string(data)); err != nil {
			return err
		}
		if common.json {
			return writeJSON(stdout, map[string]any{"name": pos[0], "sections": core.ParseTemplate(string(data))})
		}
		fmt.Fprintf(stdout, "已保存模板 %s\n", pos[0])
		return nil
	default:
		return usagef("未知 template 子命令 %q", sub)
	}
}

// groupAssemble 由筛选结果按项目生成组报草稿；不给 --project 时逐项目生成。
func groupAssemble(ws *store.Workspace, week, group, projectArg string, force, allowUnreviewed bool,
	common *commonFlags, stdout, stderr io.Writer, now time.Time) error {
	projects, err := ws.ReportProjects()
	if err != nil {
		return err
	}
	if len(projects) == 0 {
		return usagef("还没有配置报送项目（report project add <名称>）")
	}
	targets := projects
	if projectArg != "" {
		p, ok := store.FindReportProject(projects, projectArg)
		if !ok {
			return fmt.Errorf("%w: 找不到报送项目 %q", store.ErrNotFound, projectArg)
		}
		targets = []store.ReportProject{p}
	}
	out := make([]groupJSON, 0, len(targets))
	for _, p := range targets {
		d, err := core.AssembleProjectGroup(ws, week, group, p.ID, force, allowUnreviewed, now)
		if err != nil {
			return err
		}
		upsertReportIndex(ws, d.Week, draftKindOf(d), d.Person, stderr)
		out = append(out, toGroupJSON(ws, d, false))
		if !common.json {
			fmt.Fprintf(stdout, "已生成「%s」组报草稿：%s %s\n", p.Name, d.Week, d.Status)
		}
	}
	if common.json {
		if len(out) == 1 {
			return writeJSON(stdout, out[0])
		}
		return writeJSON(stdout, out)
	}
	return nil
}

// groupList 列出各项目组报草稿及状态。
func groupList(ws *store.Workspace, week, group string, common *commonFlags, stdout io.Writer) error {
	lead, _, err := core.ResolveLead(ws, week, group)
	if err != nil {
		return err
	}
	projects, err := ws.ReportProjects()
	if err != nil {
		return err
	}
	type row struct {
		Project string     `json:"project"`
		Name    string     `json:"name"`
		Draft   *groupJSON `json:"draft,omitempty"`
	}
	rows := make([]row, 0, len(projects))
	for _, p := range projects {
		d, err := ws.ReadDraftKind(store.GroupReportKind(p.ID), week, lead.ID)
		r := row{Project: p.ID, Name: p.Name}
		if err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				return err
			}
		} else {
			gj := toGroupJSON(ws, d, false)
			r.Draft = &gj
		}
		rows = append(rows, r)
	}
	if common.json {
		return writeJSON(stdout, rows)
	}
	for _, r := range rows {
		status := "未生成"
		if r.Draft != nil {
			status = map[string]string{store.DraftStatusDraft: "草稿", store.DraftStatusFinal: "已定稿"}[r.Draft.Status]
		}
		fmt.Fprintf(stdout, "%-12s %-8s %s\n", r.Name, status, week)
	}
	return nil
}

func groupSourceChanged(ws *store.Workspace, d *store.Draft) bool {
	if store.ProjectIDFromGroupKind(d.Kind) != "" {
		return core.ProjectGroupSourceChanged(ws, d)
	}
	return core.GroupSourceChanged(ws, d)
}
