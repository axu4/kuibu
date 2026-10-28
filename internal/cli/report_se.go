package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/axu4/kuibu/internal/core"
	"github.com/axu4/kuibu/internal/store"
)

// seReportJSON 是 SE 项目报的 JSON 输出。
type seReportJSON struct {
	draftJSON
	Project       string   `json:"project"`
	ProjectName   string   `json:"projectName"`
	Sources       []string `json:"sources"`
	Missing       []string `json:"missing"`
	SourceChanged bool     `json:"sourceChanged"`
}

// reportSE 实现 report se：我是 SE 的项目，收各组项目报并合并成项目报。
func reportSE(args []string, common *commonFlags, stdin io.Reader, stdout, stderr io.Writer, env func(string) string) error {
	if len(args) == 0 {
		return usagef("report se 需要子命令: projects/status/add/show/rm/generate/report/edit/finalize/unfinalize")
	}
	sub, rest := args[0], args[1:]
	fs := newFlagSet("report se " + sub)
	addCommonFlags(fs, common)
	weekFlag := fs.String("week", "", "周编号，默认本周")
	monthFlag := fs.String("month", "", "月份编号；给出时按月报处理（与 --week 二选一）")
	projectFlag := fs.String("project", "", "报送项目 id 或名称（只能是我当 SE 的项目）")
	groupFlag := fs.String("group", "", "交报的组（id 或名称）")
	file := fs.String("file", "", "从文件读取正文（默认读 stdin）")
	allowMissing := fs.Bool("allow-missing", false, "有组没交时仍然生成，并在项目报里标注未纳入")
	force := fs.Bool("force", false, "覆盖已人工修改或已定稿的项目报（旧版移入历史）")
	switch sub {
	case "projects", "status", "add", "show", "rm", "generate", "report", "edit", "finalize", "unfinalize":
	default:
		return usagef("未知 report se 子命令 %q", sub)
	}
	pos, err := parseCommand(fs, rest)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("report se %s 不接受位置参数，用 --project / --group", sub)
	}
	now, err := nowFrom(env)
	if err != nil {
		return err
	}
	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	if sub == "projects" {
		list, err := core.MineProjects(ws)
		if err != nil {
			return err
		}
		if common.json {
			return writeJSON(stdout, list)
		}
		if len(list) == 0 {
			fmt.Fprintln(stdout, "还没有我当 SE 的项目（report project se <项目> --mine）")
			return nil
		}
		for _, p := range list {
			fmt.Fprintf(stdout, "%s  (%s)\n", p.Name, p.ID)
		}
		return nil
	}
	period, err := sePeriod(*weekFlag, *monthFlag, now)
	if err != nil {
		return err
	}
	if *projectFlag == "" {
		return usagef("report se %s 需要 --project", sub)
	}
	project, err := projectByIDOrName(ws, *projectFlag)
	if err != nil {
		return err
	}
	needGroup := sub == "add" || sub == "show" || sub == "rm"
	if needGroup && *groupFlag == "" {
		return usagef("report se %s 需要 --group", sub)
	}

	switch sub {
	case "status":
		view, err := core.LoadProjectInbox(ws, period, project.ID, now)
		if err != nil {
			return err
		}
		if common.json {
			return writeJSON(stdout, view)
		}
		printProjectInbox(view, stdout)
		return nil
	case "add":
		body, err := readBody(*file, stdin)
		if err != nil {
			return err
		}
		r, err := core.SubmitProjectInbox(ws, period, project.ID, *groupFlag, body, now)
		if err != nil {
			return err
		}
		upsertReportIndex(ws, period, r.Level, r.Person, stderr)
		if common.json {
			return writeJSON(stdout, map[string]any{"period": period, "project": project.ID, "group": r.Group, "lead": r.Name, "submittedAt": r.SubmittedAt})
		}
		fmt.Fprintf(stdout, "已归档 %s 交来的「%s」项目报 · %s\n", r.Group, project.Name, period)
		return nil
	case "show":
		src, err := core.ReadProjectInboxSource(ws, period, project.ID, *groupFlag, now)
		if err != nil {
			return err
		}
		if common.json {
			return writeJSON(stdout, src)
		}
		fmt.Fprintf(stdout, "%s · %s  %s\n\n", src.Group, inboxStatusLabel(src.Status), period)
		fmt.Fprintln(stdout, src.Body)
		return nil
	case "rm":
		groupID, err := core.DeleteProjectInbox(ws, period, project.ID, *groupFlag, now)
		if err != nil {
			return err
		}
		removeReportIndex(context.Background(), ws, period, store.ProjectInboxLevel(project.ID), groupID, stderr)
		if common.json {
			return writeJSON(stdout, map[string]any{"removed": true})
		}
		fmt.Fprintln(stdout, "已移入历史")
		return nil
	case "generate":
		d, err := core.AssembleProjectReport(ws, period, project.ID, *allowMissing, *force, now)
		if err != nil {
			return err
		}
		return seDone(ws, d, "已生成项目报草稿", common, stdout, stderr, now)
	case "report":
		d, err := ws.ReadProjectReport(period, project.ID)
		if err != nil {
			return err
		}
		if common.json {
			return writeJSON(stdout, toSEJSON(ws, d, true, now))
		}
		label := map[string]string{store.DraftStatusDraft: "草稿", store.DraftStatusFinal: "已定稿"}[d.Status]
		if core.ProjectReportSourceChanged(ws, d, now) {
			label += "·来源已更新"
		}
		fmt.Fprintf(stdout, "%s  %s  %s\n\n", period, project.Name, label)
		fmt.Fprintln(stdout, d.Body)
		return nil
	case "edit":
		body, err := readBody(*file, stdin)
		if err != nil {
			return err
		}
		d, err := ws.EditDraftKind(store.ProjectReportDirName, period, project.ID, body, now)
		if err != nil {
			return err
		}
		return seDone(ws, d, "已保存修改", common, stdout, stderr, now)
	case "finalize":
		d, err := ws.FinalizeDraftKind(store.ProjectReportDirName, period, project.ID, now)
		if err != nil {
			return err
		}
		return seDone(ws, d, "已定稿", common, stdout, stderr, now)
	default: // unfinalize
		d, err := ws.UnfinalizeDraftKind(store.ProjectReportDirName, period, project.ID)
		if err != nil {
			return err
		}
		return seDone(ws, d, "已取消定稿", common, stdout, stderr, now)
	}
}

// sePeriod 解析 --week / --month，默认本周。
func sePeriod(weekFlag, monthFlag string, now time.Time) (string, error) {
	if monthFlag != "" {
		if weekFlag != "" {
			return "", usagef("--week 与 --month 只能给一个")
		}
		return store.ParseMonth(monthFlag, now)
	}
	return weekOrThis(weekFlag, now)
}

func readBody(file string, stdin io.Reader) (string, error) {
	var (
		data []byte
		err  error
	)
	if file != "" {
		data, err = os.ReadFile(file)
	} else {
		data, err = io.ReadAll(stdin)
	}
	if err != nil {
		return "", fmt.Errorf("读取正文失败: %w", err)
	}
	return string(data), nil
}

func seDone(ws *store.Workspace, d *store.Draft, verb string, common *commonFlags, stdout, stderr io.Writer, now time.Time) error {
	upsertReportIndex(ws, d.Week, store.ProjectReportDirName, d.Person, stderr)
	if common.json {
		return writeJSON(stdout, toSEJSON(ws, d, false, now))
	}
	fmt.Fprintf(stdout, "%s：%s %s\n", verb, d.Week, d.Name)
	if len(d.Missing) > 0 {
		fmt.Fprintf(stdout, "未纳入: %s\n", strings.Join(d.Missing, "、"))
	}
	return nil
}

func toSEJSON(ws *store.Workspace, d *store.Draft, withBody bool, now time.Time) seReportJSON {
	dj := draftJSONFrom(ws, d, nil, withBody)
	dj.Path = filepath.Join(ws.ReportsDir(), d.Week, store.ProjectReportDirName, d.Person+".md")
	return seReportJSON{
		draftJSON:     dj,
		Project:       d.Person,
		ProjectName:   d.Name,
		Sources:       nonNilStrings(d.Sources),
		Missing:       nonNilStrings(d.Missing),
		SourceChanged: core.ProjectReportSourceChanged(ws, d, now),
	}
}

func inboxStatusLabel(status string) string {
	switch status {
	case core.InboxLinked:
		return "引用组报定稿"
	case core.InboxDraft:
		return "组报未定稿"
	case core.InboxSubmitted:
		return "已交"
	case core.InboxLate:
		return "迟交"
	}
	return "未交"
}

func printProjectInbox(view *core.ProjectInbox, stdout io.Writer) {
	fmt.Fprintf(stdout, "%s · %s  应交 %d  已收 %d\n", view.Project.Name, view.Period, view.Counts["expected"], view.Counts["received"])
	for _, list := range [][]core.ProjectInboxEntry{view.Entries, view.Extra} {
		for _, e := range list {
			when := ""
			if e.SubmittedAt != nil {
				when = e.SubmittedAt.In(store.TodoLocation()).Format("01-02 15:04")
			}
			fmt.Fprintf(stdout, "  %-10s %-8s %-10s %s\n", e.Group, orDash(e.Lead), inboxStatusLabel(e.Status), when)
		}
	}
}
