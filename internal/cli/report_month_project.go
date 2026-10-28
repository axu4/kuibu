package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/axu4/kuibu/internal/core"
	"github.com/axu4/kuibu/internal/store"
)

// monthScopeJSON 是 scope / pull 的 JSON 输出：落盘的拉取记录 + 更新后的来源视图。
type monthScopeJSON struct {
	Pull    *store.MonthPull      `json:"pull"`
	Sources *core.MonthSourceView `json:"sources"`
}

// monthProjectRow 是 report month list 的一行。
type monthProjectRow struct {
	Project       string           `json:"project"`
	ProjectName   string           `json:"projectName"`
	Status        string           `json:"status"`
	Path          string           `json:"path"`
	FinalizedAt   *time.Time       `json:"finalizedAt,omitempty"`
	HumanEdited   bool             `json:"humanEdited"`
	SourceChanged bool             `json:"sourceChanged"`
	Counts        core.MonthCounts `json:"counts"`
	Missing       []string         `json:"missing"`
	SourceWeeks   []string         `json:"sourceWeeks"`
	PlanFrom      string           `json:"planFrom,omitempty"`
	Weeks         []string         `json:"weeks"`
}

// reportMonthProjects 分发「配置了报送项目」后的组月报新流程（确定性、不调 AI）。
func reportMonthProjects(ws *store.Workspace, o reportMonthOpts) error {
	switch o.sub {
	case "sources":
		return monthSources(ws, o)
	case "scope":
		return monthScope(ws, o)
	case "pull":
		return monthPull(ws, o)
	case "candidates":
		return monthCandidates(ws, o)
	case "move":
		return monthMove(ws, o)
	case "list":
		return monthList(ws, o)
	case "generate":
		return monthGenerate(ws, o)
	default: // show / edit / finalize / unfinalize
		return monthDraft(ws, o)
	}
}

// monthSources 输出月报第 ① 步来源视图：本期各周 × 项目的定稿状态、候选统计、缺周。
func monthSources(ws *store.Workspace, o reportMonthOpts) error {
	data, err := core.MonthSourceData(ws, o.month, o.group)
	if err != nil {
		return err
	}
	if o.common.json {
		return writeJSON(o.stdout, data)
	}
	printMonthSources(data, o.stdout)
	return nil
}

// monthScope 修改本期周范围：--weeks 指定整周，--reset 恢复按周报日归属的默认周。
func monthScope(ws *store.Workspace, o reportMonthOpts) error {
	if o.reset && strings.TrimSpace(o.weeksArg) != "" {
		return usagef("scope 的 --weeks 与 --reset 只能选一个")
	}
	var weeks []string
	if o.reset {
		data, err := core.MonthSourceData(ws, o.month, o.group)
		if err != nil {
			return err
		}
		weeks = data.DefaultWeeks
	} else {
		arg := strings.TrimSpace(o.weeksArg)
		if arg == "" {
			return usagef("用法: kuibu report month scope --weeks 2026-W39,2026-W40,... | --reset [--month M] [--group G]")
		}
		for _, token := range strings.Split(arg, ",") {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			week, err := store.ParseWeek(token, o.now)
			if err != nil {
				return err
			}
			weeks = append(weeks, week)
		}
		if len(weeks) == 0 {
			return usagef("scope 的 --weeks 至少要有一个周")
		}
	}
	pull, data, err := core.SetMonthScope(ws, o.month, o.group, weeks)
	if err != nil {
		return err
	}
	if o.common.json {
		return writeJSON(o.stdout, monthScopeJSON{Pull: pull, Sources: data})
	}
	fmt.Fprintf(o.stdout, "本期周范围已更新：%s（%d 周）\n", monthWeeksLabel(data.ScopeWeeks), len(data.ScopeWeeks))
	printMonthScopeSummary(data, o.stdout)
	return nil
}

// monthPull 纳入 / 去选一份定稿周组报。
func monthPull(ws *store.Workspace, o reportMonthOpts) error {
	if o.on && o.off {
		return usagef("pull 的 --on 与 --off 只能选一个")
	}
	if strings.TrimSpace(o.weekArg) == "" || strings.TrimSpace(o.projectArg) == "" {
		return usagef("用法: kuibu report month pull --week W --project P [--off|--on] [--month M] [--group G]")
	}
	week, err := store.ParseWeek(o.weekArg, o.now)
	if err != nil {
		return err
	}
	project, err := projectByIDOrName(ws, o.projectArg)
	if err != nil {
		return err
	}
	pull, err := core.SetMonthSource(ws, o.month, o.group, week, project.ID, !o.off)
	if err != nil {
		return err
	}
	data, err := core.MonthSourceData(ws, o.month, o.group)
	if err != nil {
		return err
	}
	if o.common.json {
		return writeJSON(o.stdout, monthScopeJSON{Pull: pull, Sources: data})
	}
	verb := "已纳入"
	if o.off {
		verb = "已去选"
	}
	fmt.Fprintf(o.stdout, "%s %s 的 %s 周组报\n", verb, project.Name, week)
	printMonthScopeSummary(data, o.stdout)
	return nil
}

// monthCandidates 输出一个项目的本月候选：总结 / 计划 / 更早计划 / 不纳入。
func monthCandidates(ws *store.Workspace, o reportMonthOpts) error {
	if strings.TrimSpace(o.projectArg) == "" {
		return usagef("用法: kuibu report month candidates --project P [--month M] [--group G]")
	}
	project, err := projectByIDOrName(ws, o.projectArg)
	if err != nil {
		return err
	}
	c, err := core.MonthProjectCandidates(ws, o.month, o.group, project.ID)
	if err != nil {
		return err
	}
	if o.common.json {
		return writeJSON(o.stdout, c)
	}
	printMonthCandidates(c, o.stdout)
	return nil
}

// monthMove 调整一条候选的归口。
func monthMove(ws *store.Workspace, o reportMonthOpts) error {
	if strings.TrimSpace(o.projectArg) == "" {
		return usagef("move 需要 --project <项目 id|名称>")
	}
	section := strings.TrimSpace(o.section)
	if section != "summary" && section != "plan" {
		return usagef("move 需要 --section summary|plan（条目原始所在节）")
	}
	if !store.ValidMonthPool(o.to) {
		return usagef("非法 --to %q（summary|plan|archive|excluded）", o.to)
	}
	project, err := projectByIDOrName(ws, o.projectArg)
	if err != nil {
		return err
	}
	c, err := core.SetMonthItemPool(ws, o.month, o.group, project.ID, section, o.pos[0], o.to)
	if err != nil {
		return err
	}
	if o.common.json {
		return writeJSON(o.stdout, c)
	}
	fmt.Fprintf(o.stdout, "已把 %s 改到「%s」（%s）：总结 %d · 计划 %d · 更早计划 %d · 不纳入 %d\n",
		o.pos[0], monthPoolLabel(o.to), project.Name,
		c.Counts.Summary, c.Counts.Plan, c.Counts.Archive, c.Counts.Excluded)
	return nil
}

// monthList 列出各项目月报状态、候选数与缺周。
func monthList(ws *store.Workspace, o reportMonthOpts) error {
	pull, err := core.LoadMonthPull(ws, o.month, o.group)
	if err != nil {
		return err
	}
	lead, err := ws.ResolvePerson(pull.Lead)
	if err != nil {
		return err
	}
	projects, err := ws.ReportProjects()
	if err != nil {
		return err
	}
	rows := make([]monthProjectRow, 0, len(projects))
	for _, p := range projects {
		c, err := core.MonthProjectCandidates(ws, o.month, o.group, p.ID)
		if err != nil {
			return err
		}
		row := monthProjectRow{
			Project:     p.ID,
			ProjectName: p.Name,
			Status:      "none",
			Path:        filepath.Join(ws.ReportsDir(), o.month, store.GroupReportKind(p.ID), lead.ID+".md"),
			Counts:      c.Counts,
			Missing:     nonNilStrings(c.Missing),
			SourceWeeks: nonNilStrings(c.SourceWeeks),
			PlanFrom:    c.PlanFrom,
			Weeks:       nonNilStrings(pull.Weeks),
		}
		d, err := ws.ReadDraftKind(store.GroupReportKind(p.ID), o.month, lead.ID)
		switch {
		case err == nil:
			row.Status = d.Status
			row.HumanEdited = d.HumanEdited
			row.FinalizedAt = timePtr(d.FinalizedAt)
			row.SourceChanged = core.MonthProjectSourceChanged(ws, d)
		case !errors.Is(err, store.ErrNotFound):
			return err
		}
		rows = append(rows, row)
	}
	if o.common.json {
		return writeJSON(o.stdout, rows)
	}
	fmt.Fprintf(o.stdout, "月报 %s  %s（%s）  本期 %s（%d 周）\n",
		o.month, orDash(pull.Group), orDash(lead.Name), monthWeeksLabel(pull.Weeks), len(pull.Weeks))
	tw := tabwriter.NewWriter(o.stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "项目\t状态\t总结\t计划\t更早计划\t缺周\t来源")
	for _, r := range rows {
		status := monthDraftStatusLabel(r.Status)
		if r.SourceChanged {
			status += "·来源已更新"
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%s\t%d 周\n", r.ProjectName, status,
			r.Counts.Summary, r.Counts.Plan, r.Counts.Archive,
			orDash(strings.Join(r.Missing, "、")), len(r.SourceWeeks))
	}
	return tw.Flush()
}

// monthGenerate 按当前拉取记录为项目生成确定性月报草稿；不调 LLM。
func monthGenerate(ws *store.Workspace, o reportMonthOpts) error {
	projects, err := ws.ReportProjects()
	if err != nil {
		return err
	}
	var targets []store.ReportProject
	switch {
	case o.allProjects && strings.TrimSpace(o.projectArg) != "":
		return usagef("--project 与 --all 只能选一个")
	case o.allProjects:
		targets = projects
	case strings.TrimSpace(o.projectArg) != "":
		p, err := projectByIDOrName(ws, o.projectArg)
		if err != nil {
			return err
		}
		targets = []store.ReportProject{p}
	default:
		return usagef("按项目生成月报需要 --project <项目 id|名称>，或 --all 逐个项目生成")
	}
	out := make([]groupJSON, 0, len(targets))
	for _, p := range targets {
		d, err := core.AssembleMonthProject(ws, o.month, o.group, p.ID, o.allowMissing, o.force, o.now)
		if err != nil {
			return err
		}
		upsertReportIndex(ws, d.Week, draftKindOf(d), d.Person, o.stderr)
		out = append(out, toMonthProjectJSON(ws, d, false))
		if !o.common.json {
			fmt.Fprintf(o.stdout, "已生成「%s」项目月报草稿：%s %s\n", p.Name, d.Week, monthDraftStatusLabel(d.Status))
		}
	}
	if o.common.json {
		if len(out) == 1 {
			return writeJSON(o.stdout, out[0])
		}
		return writeJSON(o.stdout, out)
	}
	return nil
}

// monthDraft 按项目 show / edit / finalize / unfinalize 项目月报。
func monthDraft(ws *store.Workspace, o reportMonthOpts) error {
	if o.allProjects {
		return usagef("report month %s 不支持 --all，用 --project 指定项目（列表用 report month list）", o.sub)
	}
	if strings.TrimSpace(o.projectArg) == "" {
		return usagef("配置了报送项目后，report month %s 需要 --project <项目 id|名称>", o.sub)
	}
	project, err := projectByIDOrName(ws, o.projectArg)
	if err != nil {
		return err
	}
	lead, _, err := core.MonthLead(ws, o.month, o.group)
	if err != nil {
		return err
	}
	kind, id := store.GroupReportKind(project.ID), lead.ID
	var d *store.Draft
	switch o.sub {
	case "show":
		d, err = ws.ReadDraftKind(kind, o.month, id)
		if err != nil {
			return err
		}
		if o.common.json {
			return writeJSON(o.stdout, toMonthProjectJSON(ws, d, true))
		}
		printMonthProjectHeader(ws, d, o.stdout)
		fmt.Fprintln(o.stdout, d.Body)
		return nil
	case "edit":
		var data []byte
		if o.file != "" {
			data, err = os.ReadFile(o.file)
		} else {
			data, err = io.ReadAll(o.stdin)
		}
		if err != nil {
			return fmt.Errorf("读取正文失败: %w", err)
		}
		d, err = ws.EditDraftKind(kind, o.month, id, string(data), o.now)
	case "finalize":
		d, err = ws.FinalizeDraftKind(kind, o.month, id, o.now)
	case "unfinalize":
		d, err = ws.UnfinalizeDraftKind(kind, o.month, id)
	}
	if err != nil {
		return err
	}
	upsertReportIndex(ws, d.Week, draftKindOf(d), d.Person, o.stderr)
	if o.common.json {
		return writeJSON(o.stdout, toMonthProjectJSON(ws, d, false))
	}
	verb := map[string]string{"edit": "已保存修改", "finalize": "已定稿", "unfinalize": "已取消定稿"}[o.sub]
	fmt.Fprintf(o.stdout, "「%s」项目月报%s：%s %s\n", project.Name, verb, d.Week, monthDraftStatusLabel(d.Status))
	return nil
}

/* ============================== 输出与标签 ============================== */

// toMonthProjectJSON 在组报 JSON 基础上，用月报来源判断覆盖「来源已更新」。
func toMonthProjectJSON(ws *store.Workspace, d *store.Draft, withBody bool) groupJSON {
	out := toGroupJSON(ws, d, withBody)
	out.SourceChanged = core.MonthProjectSourceChanged(ws, d)
	return out
}

// printMonthSources 输出第 ① 步来源视图（文本）。
func printMonthSources(data *core.MonthSourceView, stdout io.Writer) {
	fmt.Fprintf(stdout, "月报来源 %s  %s（%s）  默认 %d 周  本期 %d 周  定稿 %d/%d 份  候选 %d 条\n",
		data.Month, orDash(data.Group), orDash(data.LeadName),
		len(data.DefaultWeeks), len(data.ScopeWeeks), data.FinalSlots, data.Slots, data.CandidateTotal)
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "周\t周报日\t本期\t项目")
	for _, row := range data.Weeks {
		scope := "否"
		if row.Scope {
			scope = "是"
		}
		week := row.Week
		if row.Label != "" {
			week += "（" + row.Label + "）"
		}
		parts := make([]string, 0, len(row.Reports))
		for _, r := range row.Reports {
			part := r.ProjectName + "：" + monthWeekStatusLabel(r.Status)
			if r.Status == store.DraftStatusFinal {
				part += fmt.Sprintf(" %d 条", r.Items)
				if r.Included {
					part += "·已纳入"
				} else {
					part += "·未纳入"
				}
			} else {
				part += "·不能纳入"
			}
			parts = append(parts, part)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", week, row.ReportDay, scope, strings.Join(parts, "；"))
	}
	if err := tw.Flush(); err != nil {
		return
	}
	printMonthScopeSummary(data, stdout)
}

// printMonthScopeSummary 输出本期统计与缺周。
func printMonthScopeSummary(data *core.MonthSourceView, stdout io.Writer) {
	if len(data.Missing) > 0 {
		fmt.Fprintf(stdout, "缺周：%s\n", strings.Join(data.Missing, "、"))
		return
	}
	fmt.Fprintln(stdout, "本期没有缺周")
}

// printMonthCandidates 输出一个项目的候选（文本）。
func printMonthCandidates(c *core.MonthCandidatesData, stdout io.Writer) {
	fmt.Fprintf(stdout, "%s 月报候选（本期来源 %d 周：%s）\n",
		c.ProjectName, len(c.SourceWeeks), orDash(strings.Join(c.SourceWeeks, "、")))
	fmt.Fprintf(stdout, "总结 %d · 计划 %d · 更早计划 %d · 不纳入 %d\n",
		c.Counts.Summary, c.Counts.Plan, c.Counts.Archive, c.Counts.Excluded)
	if c.PlanFrom != "" {
		fmt.Fprintf(stdout, "下月计划取自 %s\n", c.PlanFrom)
	}
	if len(c.Missing) > 0 {
		fmt.Fprintf(stdout, "缺周：%s\n", strings.Join(c.Missing, "、"))
	}
	for _, pool := range []struct {
		title string
		items []core.MonthItem
	}{
		{"本月总结", c.Summary},
		{"下月计划", c.Plan},
		{"更早计划", c.Archive},
		{"不纳入", c.Excluded},
	} {
		if len(pool.items) == 0 {
			continue
		}
		fmt.Fprintf(stdout, "%s（%d）\n", pool.title, len(pool.items))
		for _, it := range pool.items {
			weeks := ""
			if len(it.Weeks) > 0 {
				weeks = "（" + strings.Join(it.Weeks, "、") + "）"
			}
			fmt.Fprintf(stdout, "  %s  %s%s\n", it.ID, it.Text, weeks)
		}
	}
}

// printMonthProjectHeader 输出项目月报头部（文本）。
func printMonthProjectHeader(ws *store.Workspace, d *store.Draft, stdout io.Writer) {
	projects, _ := ws.ReportProjects()
	name := store.ReportProjectName(projects, store.ProjectIDFromGroupKind(d.Kind))
	status := monthDraftStatusLabel(d.Status)
	if core.MonthProjectSourceChanged(ws, d) {
		status += "·来源已更新"
	}
	fmt.Fprintf(stdout, "%s  %s 组月报  %s  %s\n", d.Week, orDash(d.Group), orDash(name), status)
	if len(d.Sources) > 0 {
		fmt.Fprintf(stdout, "来源：%d 份周组报\n", len(d.Sources))
	}
	if len(d.Missing) > 0 {
		fmt.Fprintf(stdout, "未纳入：%s\n", strings.Join(d.Missing, "、"))
	}
	fmt.Fprintln(stdout)
}

// monthDraftStatusLabel 返回月报草稿状态的中文。
func monthDraftStatusLabel(status string) string {
	switch status {
	case store.DraftStatusDraft:
		return "草稿"
	case store.DraftStatusFinal:
		return "已定稿"
	}
	return "未生成"
}

// monthWeekStatusLabel 返回周组报状态的中文。
func monthWeekStatusLabel(status string) string {
	switch status {
	case store.DraftStatusFinal:
		return "已定稿"
	case store.DraftStatusDraft:
		return "未定稿"
	}
	return "未提交"
}

// monthPoolLabel 返回月报条目归口的中文。
func monthPoolLabel(pool string) string {
	switch pool {
	case store.MonthPoolSummary:
		return "本月总结"
	case store.MonthPoolPlan:
		return "下月计划"
	case store.MonthPoolArchive:
		return "更早计划"
	case store.MonthPoolExcluded:
		return "不纳入"
	}
	return pool
}

// monthWeeksLabel 把周列表写成「W40、W41」这样的短标签。
func monthWeeksLabel(weeks []string) string {
	parts := make([]string, 0, len(weeks))
	for _, week := range weeks {
		parts = append(parts, shortWeekLabel(week))
	}
	return strings.Join(parts, "、")
}

// shortWeekLabel 把 2026-W40 缩成 W40。
func shortWeekLabel(week string) string {
	if i := strings.LastIndex(week, "-W"); i >= 0 {
		return week[i+1:]
	}
	return week
}
