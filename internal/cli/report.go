package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/axu4/kuibu/internal/core"
	"github.com/axu4/kuibu/internal/index"
	"github.com/axu4/kuibu/internal/store"
)

// reportJSON 是周报的 JSON 输出结构。Body 只在 show 时填充。
type reportJSON struct {
	Person       string    `json:"person"`
	Name         string    `json:"name"`
	Week         string    `json:"week"`
	Level        string    `json:"level"`
	Group        string    `json:"group"`
	ProductLines []string  `json:"productLines"`
	Source       string    `json:"source"`
	SubmittedAt  time.Time `json:"submittedAt"`
	HeaderDate   string    `json:"headerDate,omitempty"`
	Path         string    `json:"path"`
	Body         *string   `json:"body,omitempty"`
}

// runReport 分发 report 子命令。
func runReport(ctx context.Context, args []string, common *commonFlags, stdin io.Reader, stdout, stderr io.Writer, env func(string) string) error {
	if len(args) == 0 {
		return usagef("report 需要子命令: add/list/show/status/rm/config/normalize/draft/finalize/unfinalize/review/carry/projects/project/triage/group/product/export/month")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "add":
		return reportAdd(rest, common, stdin, stdout, stderr, env)
	case "list":
		return reportList(ctx, rest, common, stdout, stderr, env)
	case "show":
		return reportShow(rest, common, stdout, stderr, env)
	case "status":
		return reportStatus(rest, common, stdout, stderr, env)
	case "rm":
		return reportRemove(ctx, rest, common, stdout, stderr, env)
	case "config":
		return reportConfig(rest, common, stdout, stderr, env)
	case "normalize":
		return reportNormalize(ctx, rest, common, stdout, stderr, env)
	case "draft":
		return reportDraft(rest, common, stdin, stdout, stderr, env)
	case "finalize":
		return reportFinalize(rest, true, common, stdout, stderr, env)
	case "unfinalize":
		return reportFinalize(rest, false, common, stdout, stderr, env)
	case "review":
		return reportReview(ctx, rest, common, stdout, stderr, env)
	case "carry":
		return reportCarry(rest, common, stdout, stderr, env)
	case "projects":
		return reportProjects(rest, common, stdout, stderr, env)
	case "project":
		return reportProject(rest, common, stdout, stderr, env)
	case "triage":
		return reportTriage(rest, common, stdout, stderr, env)
	case "group":
		return reportGroup(ctx, rest, common, stdin, stdout, stderr, env)
	case "product":
		return reportProduct(ctx, rest, common, stdin, stdout, stderr, env)
	case "export":
		return reportExport(rest, common, stdout, stderr, env)
	case "month":
		return reportMonth(ctx, rest, common, stdin, stdout, stderr, env)
	default:
		return usagef("未知 report 子命令 %q", sub)
	}
}

// reportJSONFromStore 把 store.Report 转为 JSON 结构。
func reportJSONFromStore(ws *store.Workspace, r *store.Report, withBody bool) reportJSON {
	products := r.ProductLines
	if products == nil {
		products = []string{}
	}
	out := reportJSON{
		Person:       r.Person,
		Name:         r.Name,
		Week:         r.Week,
		Level:        r.Level,
		Group:        r.Group,
		ProductLines: products,
		Source:       r.Source,
		SubmittedAt:  r.SubmittedAt,
		HeaderDate:   r.HeaderDate,
		Path:         filepath.Join(ws.ReportsDir(), r.Week, r.Level, r.Person+".md"),
	}
	if withBody {
		body := r.Body
		out.Body = &body
	}
	return out
}

// reportAdd 实现 report add。
func reportAdd(args []string, common *commonFlags, stdin io.Reader, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("report add")
	addCommonFlags(fs, common)
	person := fs.String("person", "", "人员 ID 或姓名/别名（仅单份文本时允许）")
	week := fs.String("week", "", "周编号（this/last/next/YYYY-MM-DD/2026-W44），优先于头行日期")
	level := fs.String("level", store.ReportLevelMember, "层级: member|lead")
	dryRun := fs.Bool("dry-run", false, "只输出预览，不写入")
	file := fs.String("file", "", "从文件读取原文")
	stdinFlag := fs.Bool("stdin", false, "从标准输入读取原文（默认）")
	month := fs.String("month", "", "组长月报的月份（如 2026-10）；给出时按组长月报归档")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("report add 不接受位置参数: %s", strings.Join(pos, " "))
	}
	if *month != "" {
		// 组长月报：识别人员沿用组长层级的规则，按月份归档。
		*level = store.ReportLevelLead
	}
	if *file != "" && *stdinFlag {
		return usagef("--file 与 --stdin 不能同时使用")
	}
	if !store.ValidReportLevel(*level) {
		return usagef("非法周报层级 %q（member|lead）", *level)
	}

	text := ""
	if *file != "" {
		data, err := os.ReadFile(*file)
		if err != nil {
			return fmt.Errorf("读取周报文件失败: %w", err)
		}
		text = string(data)
	} else {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return fmt.Errorf("读取标准输入失败: %w", err)
		}
		text = string(data)
	}

	if *person != "" && len(core.ParseBatch(text)) > 1 {
		return usagef("仅单份文本时可指定 --person")
	}

	now, err := nowFrom(env)
	if err != nil {
		return err
	}
	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}

	preview, err := core.PreviewReports(ws, core.PreviewInput{
		Text:   text,
		Level:  *level,
		Person: *person,
		Week:   *week,
		Now:    now,
	})
	if err != nil {
		return err
	}

	if *dryRun {
		if common.json {
			return writeJSON(stdout, preview)
		}
		if len(preview) == 0 {
			fmt.Fprintln(stdout, "没有可识别的周报")
			return nil
		}
		for i, p := range preview {
			if p.Duplicate {
				fmt.Fprintf(stdout, "第 %d 份: %s  %s  %s  重复: 是（%s）\n",
					i+1, p.PersonName, p.Week, levelLabel(p.Level), p.Reason)
				continue
			}
			if p.Reason != "" {
				fmt.Fprintf(stdout, "第 %d 份: %s（无法识别: %s）\n", i+1, orDash(p.Name), p.Reason)
				continue
			}
			overwrite := "否"
			if p.Overwrite {
				overwrite = "是"
			}
			late := "否"
			if p.Late {
				late = "是"
			}
			fmt.Fprintf(stdout, "第 %d 份: %s  %s  %s  覆盖: %s  迟交: %s\n",
				i+1, p.PersonName, p.Week, levelLabel(p.Level), overwrite, late)
		}
		return nil
	}

	// 写入前先整体校验，识别失败不落盘任何一份（重复份不报错，只写最后一份）。
	for i, p := range preview {
		if p.Reason != "" && !p.Duplicate {
			code := "PARSE_FAILED"
			switch p.Reason {
			case "姓名有歧义":
				code = "AMBIGUOUS_PERSON"
			case "姓名不匹配":
				code = "PERSON_NOT_FOUND"
			}
			name := p.Name
			if name == "" {
				name = p.PersonName
			}
			return coded(code, fmt.Errorf("无法识别第 %d 份周报（姓名 %s）: %s", i+1, orDash(name), p.Reason))
		}
	}

	monthStr := ""
	if *month != "" {
		if monthStr, err = store.ParseMonth(*month, now); err != nil {
			return err
		}
	}
	reports := make([]*store.Report, 0, len(preview))
	for _, p := range preview {
		if p.Duplicate {
			continue
		}
		if monthStr != "" {
			r, err := ws.SubmitMonthReport(p.PersonID, monthStr, p.Text, store.ReportSourceCLI, now)
			if err != nil {
				return err
			}
			upsertReportIndex(ws, r.Week, r.Level, r.Person, stderr)
			reports = append(reports, r)
			continue
		}
		headerDate := ""
		if p.HasHeader && !p.DateInvalid && p.Date != "" {
			headerDate = p.Date
		}
		r, err := ws.SubmitReport(store.SubmitReportInput{
			Person:     p.PersonID,
			Week:       p.Week,
			Level:      p.Level,
			HeaderDate: headerDate,
			Source:     store.ReportSourceCLI,
			Body:       p.Text,
			Now:        now,
		})
		if err != nil {
			return err
		}
		upsertReportIndex(ws, r.Week, r.Level, r.Person, stderr)
		reports = append(reports, r)
	}

	if common.json {
		out := make([]reportJSON, 0, len(reports))
		for _, r := range reports {
			out = append(out, reportJSONFromStore(ws, r, false))
		}
		return writeJSON(stdout, out)
	}
	fmt.Fprintf(stdout, "已归档 %d 份周报\n", len(reports))
	for _, r := range reports {
		fmt.Fprintf(stdout, "  %s  %s  %s  %s\n", r.Week, r.Level, r.Name, r.Person)
	}
	return nil
}

// reportList 实现 report list。
func reportList(ctx context.Context, args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("report list")
	addCommonFlags(fs, common)
	weekFlag := fs.String("week", "", "周编号，默认本周")
	level := fs.String("level", "", "层级: member|lead，默认全部")
	group := fs.String("group", "", "只列出该组")
	search := fs.String("search", "", "按姓名与正文检索")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("report list 不接受位置参数: %s", strings.Join(pos, " "))
	}
	visited := visitedSet(visitedFlags(fs))

	now, err := nowFrom(env)
	if err != nil {
		return err
	}
	week := ""
	if visited["week"] {
		if strings.TrimSpace(*weekFlag) != "" {
			w, err := store.ParseWeek(*weekFlag, now)
			if err != nil {
				return err
			}
			week = w
		}
	} else {
		week = store.WeekOf(now)
	}

	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	idx := syncIndex(ctx, ws, stderr)
	if idx == nil {
		return fmt.Errorf("索引不可用，无法列出周报")
	}
	defer idx.Close()

	metas, err := idx.ListReports(index.ReportFilter{
		Week:  week,
		Level: *level,
		Group: *group,
		Query: *search,
	})
	if err != nil {
		return err
	}

	if common.json {
		return writeJSON(stdout, metas)
	}
	if len(metas) == 0 {
		fmt.Fprintln(stdout, "没有周报")
		return nil
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "周\t层级\t人员\t组\t提交时间")
	for _, m := range metas {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", m.Week, m.Level, m.Name, m.Group, formatLocal(m.SubmittedAt))
	}
	return tw.Flush()
}

// reportShow 实现 report show。
func reportShow(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("report show")
	addCommonFlags(fs, common)
	level := fs.String("level", store.ReportLevelMember, "层级: member|lead")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(pos, 2, "用法: kuibu report show <week> <person> [--level L]"); err != nil {
		return err
	}

	now, err := nowFrom(env)
	if err != nil {
		return err
	}
	week, err := store.ParseWeek(pos[0], now)
	if err != nil {
		return err
	}
	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	personID, err := ws.ResolveReportPerson(week, *level, pos[1])
	if err != nil {
		return err
	}
	r, err := ws.ReadReport(week, *level, personID)
	if err != nil {
		return err
	}

	if common.json {
		return writeJSON(stdout, reportJSONFromStore(ws, r, true))
	}
	fmt.Fprintf(stdout, "%s  %s  %s  %s\n", r.Week, r.Level, r.Name, r.Person)
	fmt.Fprintf(stdout, "组: %s  产品线: %s\n", orDash(r.Group), orDash(strings.Join(r.ProductLines, ", ")))
	fmt.Fprintf(stdout, "来源: %s  提交: %s", sourceLabel(r.Source), formatLocal(r.SubmittedAt))
	if r.HeaderDate != "" {
		fmt.Fprintf(stdout, "  头行日期: %s", r.HeaderDate)
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "原文:")
	fmt.Fprint(stdout, r.Body)
	if !strings.HasSuffix(r.Body, "\n") {
		fmt.Fprintln(stdout)
	}
	return nil
}

// reportStatus 实现 report status。
func reportStatus(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("report status")
	addCommonFlags(fs, common)
	weekFlag := fs.String("week", "", "周编号，默认本周")
	level := fs.String("level", store.ReportLevelMember, "层级: member|lead")
	group := fs.String("group", "", "按组过滤")
	product := fs.String("product", "", "按产品线过滤")
	monthFlag := fs.String("month", "", "组长月报的月份（如 2026-10），给出时统计组长月报")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("report status 不接受位置参数: %s", strings.Join(pos, " "))
	}
	visited := visitedSet(visitedFlags(fs))

	now, err := nowFrom(env)
	if err != nil {
		return err
	}
	week := store.WeekOf(now)
	if visited["week"] && strings.TrimSpace(*weekFlag) != "" {
		w, err := store.ParseWeek(*weekFlag, now)
		if err != nil {
			return err
		}
		week = w
	}
	if visited["month"] {
		m, err := store.ParseMonth(*monthFlag, now)
		if err != nil {
			return err
		}
		week, *level = m, store.ReportLevelLeadMonth
	}

	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	st, err := ws.ReportStatus(week, *level, *group, *product, now)
	if err != nil {
		return err
	}

	if common.json {
		return writeJSON(stdout, st)
	}

	fmt.Fprintf(stdout, "周报状态 %s (%s)\n", st.Week, levelLabel(st.Level))
	submittedTotal := st.Counts["submitted"] + st.Counts["late"]
	fmt.Fprintf(stdout, "应交 %d 人，已交 %d（其中迟交 %d），未交 %d，名单外 %d\n",
		st.Counts["expected"], submittedTotal, st.Counts["late"], st.Counts["missing"], st.Counts["extra"])

	if len(st.Entries) == 0 && len(st.Extra) == 0 {
		fmt.Fprintln(stdout, "没有周报提交状态")
		return nil
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	normalized := func(e store.ReportStatusEntry) string {
		if e.Normalized == nil {
			return ""
		}
		return "\t" + draftStateLabel(*e.Normalized)
	}
	header := "人员\t组\t状态\t归一"
	fmt.Fprintln(tw, header)
	for _, e := range st.Entries {
		fmt.Fprintf(tw, "%s\t%s\t%s%s\n", e.Name, orDash(e.Group), reportStatusLabel(e.Status), normalized(e))
	}
	for _, e := range st.Extra {
		fmt.Fprintf(tw, "%s\t%s\t%s（名单外）%s\n", e.Name, orDash(e.Group), reportStatusLabel(e.Status), normalized(e))
	}
	return tw.Flush()
}

// reportRemove 实现 report rm。
func reportRemove(ctx context.Context, args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("report rm")
	addCommonFlags(fs, common)
	level := fs.String("level", store.ReportLevelMember, "层级: member|lead")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(pos, 2, "用法: kuibu report rm <week> <person> [--level L]"); err != nil {
		return err
	}

	now, err := nowFrom(env)
	if err != nil {
		return err
	}
	week, err := store.ParseWeek(pos[0], now)
	if err != nil {
		return err
	}
	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	personID, err := ws.ResolveReportPerson(week, *level, pos[1])
	if err != nil {
		return err
	}
	if err := ws.DeleteReport(week, *level, personID); err != nil {
		return err
	}
	removeReportIndex(ctx, ws, week, *level, personID, stderr)

	if common.json {
		return writeJSON(stdout, map[string]any{"deleted": personID, "week": week, "level": *level})
	}
	fmt.Fprintf(stdout, "已删除（移入历史）%s %s %s\n", week, *level, personID)
	return nil
}

// reportConfig 实现 report config。
func reportConfig(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("report config")
	addCommonFlags(fs, common)
	weekday := fs.Int("weekday", 0, "周报日 1-7（1=周一…7=周日）")
	monthDay := fs.Int("month-day", 0, "月报收集日 1-31（超过当月天数按月末，不因周末顺延）")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("report config 不接受位置参数: %s", strings.Join(pos, " "))
	}
	visited := visitedSet(visitedFlags(fs))

	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	if visited["weekday"] {
		if *weekday < 1 || *weekday > 7 {
			return usagef("周报日必须在 1-7 之间")
		}
		if err := ws.SetReportWeekday(*weekday); err != nil {
			return err
		}
	}
	if visited["month-day"] {
		if err := ws.SetReportMonthDay(*monthDay); err != nil {
			return err
		}
	}
	wd, err := ws.ReportWeekday()
	if err != nil {
		return err
	}
	md, err := ws.ReportMonthDay()
	if err != nil {
		return err
	}

	if common.json {
		return writeJSON(stdout, map[string]int{"weekday": wd, "monthDay": md})
	}
	fmt.Fprintf(stdout, "周报日: %s（%d）\n月报收集日: 每月 %d 日\n", weekdayLabel(wd), wd, md)
	return nil
}

// levelLabel 返回层级中文名。
func levelLabel(level string) string {
	switch level {
	case store.ReportLevelLead:
		return "lead（组长）"
	case store.ReportLevelLeadMonth:
		return "lead-month（组长月报）"
	}
	return "member（组员）"
}

// sourceLabel 返回来源中文名。
func sourceLabel(source string) string {
	if source == store.ReportSourceCLI {
		return "cli"
	}
	return "paste"
}

// reportStatusLabel 返回状态中文名。
func reportStatusLabel(status string) string {
	switch status {
	case "submitted":
		return "已交"
	case "late":
		return "迟交"
	default:
		return "未交"
	}
}

// weekdayLabel 返回周报日中文名。
func weekdayLabel(n int) string {
	labels := [...]string{"", "周一", "周二", "周三", "周四", "周五", "周六", "周日"}
	if n < 1 || n > 7 {
		return "周四"
	}
	return labels[n]
}

// upsertReportIndex 增量更新单份周报索引，失败只警告。
func upsertReportIndex(ws *store.Workspace, week, level, person string, stderr io.Writer) {
	idx := openIndex(ws, stderr)
	if idx == nil {
		return
	}
	defer idx.Close()
	path := filepath.Join(ws.ReportsDir(), week, level, person+".md")
	if _, err := idx.UpsertFile(path); err != nil {
		fmt.Fprintf(stderr, "kuibu: 警告: 更新索引失败（不影响数据）: %v\n", err)
	}
}

// removeReportIndex 从索引删除周报；删除后同步一次以清理历史文件，失败只警告。
func removeReportIndex(ctx context.Context, ws *store.Workspace, week, level, person string, stderr io.Writer) {
	idx := openIndex(ws, stderr)
	if idx == nil {
		return
	}
	defer idx.Close()
	path := filepath.Join(ws.ReportsDir(), week, level, person+".md")
	if _, err := idx.RemoveFile(path); err != nil {
		fmt.Fprintf(stderr, "kuibu: 警告: 更新索引失败（不影响数据）: %v\n", err)
	}
	if _, err := idx.Sync(ctx); err != nil {
		fmt.Fprintf(stderr, "kuibu: 警告: 同步索引失败（不影响数据）: %v\n", err)
	}
}
