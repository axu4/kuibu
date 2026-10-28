package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/axu4/kuibu/internal/core"
	"github.com/axu4/kuibu/internal/store"
)

// triageJSON 是 report triage 的 JSON 输出。
type triageJSON struct {
	Week       string                `json:"week"`
	Group      string                `json:"group"`
	Lead       string                `json:"lead"`
	SourceHash string                `json:"sourceHash"`
	Projects   []store.TriageProject `json:"projects"`
	Items      []store.TriageItem    `json:"items"`
	Missing    []string              `json:"missing"`
	Counts     core.TriageCounts     `json:"counts"`
}

// reportTriage 实现 report triage list/set/reset。
func reportTriage(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	sub := "list"
	rest := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, rest = args[0], args[1:]
	}
	fs := newFlagSet("report triage " + sub)
	addCommonFlags(fs, common)
	weekFlag := fs.String("week", "", "周编号，默认本周")
	group := fs.String("group", "", "组名，默认唯一有组长的组")
	project := fs.String("project", "", "项目 id 或名称（set --accept 时必填）")
	section := fs.String("section", "", "只输出某个节（本周完成/下周计划/风险与求助）")
	accept := fs.Bool("accept", false, "采纳并归入 --project")
	reject := fs.Bool("reject", false, "移除")
	skip := fs.Bool("skip", false, "待定")
	clear := fs.Bool("clear", false, "清除决策")
	yes := fs.Bool("yes", false, "确认清空筛选（reset）")
	pos, err := parseCommand(fs, rest)
	if err != nil {
		return err
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
	t, _, err := core.LoadTriage(ws, week, *group)
	if err != nil {
		return err
	}
	switch sub {
	case "list":
		if *section != "" && !containsString(core.TriageSections, *section) {
			return usagef("未知栏目 %q（本周完成/下周计划/风险与求助）", *section)
		}
		items := t.Items
		if *section != "" {
			items = nil
			for _, it := range t.Items {
				if it.Section == *section {
					items = append(items, it)
				}
			}
		}
		out := triageJSON{Week: t.Week, Group: t.Group, Lead: t.Lead, SourceHash: t.SourceHash,
			Projects: t.Projects, Items: items, Missing: t.Missing, Counts: core.CountTriage(t)}
		if common.json {
			return writeJSON(stdout, out)
		}
		printTriage(out, stdout)
		return nil
	case "set":
		if err := requireArgs(pos, 1, "用法: kuibu report triage set <itemID> --accept --project p1 | --reject | --skip | --clear"); err != nil {
			return err
		}
		projectID := ""
		if *accept {
			if *reject || *skip || *clear {
				return usagef("--accept 与 --reject/--skip/--clear 只能选一个")
			}
			if *project == "" {
				return usagef("--accept 需要同时给 --project")
			}
			p, err := projectByIDOrName(ws, *project)
			if err != nil {
				return err
			}
			projectID = p.ID
		}
		decision := ""
		switch {
		case *accept:
			decision = store.TriageAccepted
		case *reject:
			decision = store.TriageRejected
		case *skip:
			decision = store.TriageSkipped
		case *clear:
			decision = ""
		default:
			return usagef("需要 --accept/--reject/--skip/--clear 之一")
		}
		decisionAt := time.Now()
		if err := core.SetTriageDecision(t, pos[0], decision, projectID, decisionAt); err != nil {
			return err
		}
		if err := ws.WriteTriage(t); err != nil {
			return err
		}
		return triageDone(ws, t, common, stdout)
	case "move":
		if err := requireArgs(pos, 1, "用法: kuibu report triage move <itemID> --project p1"); err != nil {
			return err
		}
		p, err := projectByIDOrName(ws, *project)
		if err != nil {
			return err
		}
		if err := core.SetTriageDecision(t, pos[0], store.TriageAccepted, p.ID, time.Now()); err != nil {
			return err
		}
		if err := ws.WriteTriage(t); err != nil {
			return err
		}
		return triageDone(ws, t, common, stdout)
	case "reset":
		if !*yes {
			return fmt.Errorf("%w：清空本周所有筛选决策？确认请加 --yes", store.ErrNeedsConfirm)
		}
		for i := range t.Items {
			t.Items[i].Decision = ""
			t.Items[i].Project = ""
			t.Items[i].DecidedAt = ""
		}
		t.UpdatedAt = time.Now().Format(time.RFC3339)
		if err := ws.WriteTriage(t); err != nil {
			return err
		}
		if common.json {
			return writeJSON(stdout, triageJSON{Week: t.Week, Group: t.Group, Lead: t.Lead, Projects: t.Projects, Items: t.Items, Missing: t.Missing, Counts: core.CountTriage(t)})
		}
		fmt.Fprintln(stdout, "已清空本周筛选决策")
		return nil
	default:
		return usagef("未知 report triage 子命令 %q（list/set/move/reset）", sub)
	}
}

func triageDone(ws *store.Workspace, t *store.Triage, common *commonFlags, stdout io.Writer) error {
	if common.json {
		return writeJSON(stdout, triageJSON{Week: t.Week, Group: t.Group, Lead: t.Lead, SourceHash: t.SourceHash,
			Projects: t.Projects, Items: t.Items, Missing: t.Missing, Counts: core.CountTriage(t)})
	}
	c := core.CountTriage(t)
	fmt.Fprintf(stdout, "%s 已判 %d/%d（采纳 %d · 移除 %d · 待定 %d · 未判 %d）\n",
		t.Week, c.Done, c.Total, c.Accepted, c.Rejected, c.Skipped, c.Undecided)
	return nil
}

func printTriage(t triageJSON, stdout io.Writer) {
	c := t.Counts
	fmt.Fprintf(stdout, "%s  %s  已判 %d/%d（采纳 %d · 移除 %d · 待定 %d · 未判 %d）\n",
		t.Week, orDash(t.Group), c.Done, c.Total, c.Accepted, c.Rejected, c.Skipped, c.Undecided)
	if len(t.Missing) > 0 {
		fmt.Fprintf(stdout, "没有可筛选条目：%s\n", strings.Join(t.Missing, "、"))
	}
	if len(t.Items) == 0 {
		fmt.Fprintln(stdout, "（没有条目）")
		return
	}
	for _, it := range t.Items {
		status := "未判"
		switch it.Decision {
		case store.TriageAccepted:
			status = "采纳 → " + store.ReportProjectName(projectsOf(t.Projects), it.Project)
		case store.TriageRejected:
			status = "移除"
		case store.TriageSkipped:
			status = "待定"
		}
		fmt.Fprintf(stdout, "  %s  %s  [%s] %s  %s\n", it.ID, status, core.TriageSectionTitle(it.Section), orDash(it.Name), it.Text)
	}
}

func projectsOf(list []store.TriageProject) []store.ReportProject {
	out := make([]store.ReportProject, 0, len(list))
	for _, p := range list {
		out = append(out, store.ReportProject{ID: p.ID, Name: p.Name})
	}
	return out
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
