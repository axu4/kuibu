package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/axu4/kuibu/internal/core"
	"github.com/axu4/kuibu/internal/store"
)

// reportProjects 实现 report projects：列出报送项目。
func reportProjects(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("report projects")
	addCommonFlags(fs, common)
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("report projects 不接受位置参数")
	}
	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	projects, err := ws.ReportProjects()
	if err != nil {
		return err
	}
	if common.json {
		return writeJSON(stdout, projects)
	}
	if len(projects) == 0 {
		fmt.Fprintln(stdout, "还没有配置报送项目（report project add <名称>）")
		return nil
	}
	for i, p := range projects {
		fmt.Fprintf(stdout, "%d. %s  (%s)  %s\n", i+1, p.Name, p.ID, projectSELabel(p))
	}
	return nil
}

// projectSELabel 返回项目 SE 的说明。
func projectSELabel(p store.ReportProject) string {
	if p.Mine {
		return "我是 SE"
	}
	if p.SE != "" {
		return "交给 " + p.SE
	}
	return "SE 未设置"
}

// reportProject 实现 report project add|rename|rm|se。
func reportProject(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	if len(args) == 0 {
		return usagef("report project 需要子命令: add|rename|rm|se")
	}
	sub, rest := args[0], args[1:]
	fs := newFlagSet("report project " + sub)
	addCommonFlags(fs, common)
	yes := fs.Bool("yes", false, "确认删除项目（连带影响筛选记录）")
	mine := fs.Bool("mine", false, "se: 我是这个项目的 SE")
	seName := fs.String("se", "", "se: 项目 SE 的称呼（不是我时，组报交给谁）")
	groupsFlag := fs.String("groups", "", "se: 我当 SE 时应交项目报的组（逗号分隔，id 或名称；默认全部组）")
	pos, err := parseCommand(fs, rest)
	if err != nil {
		return err
	}
	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	switch sub {
	case "add":
		if err := requireArgs(pos, 1, "用法: kuibu report project add <名称>"); err != nil {
			return err
		}
		projects, project, err := ws.AddReportProject(pos[0])
		if err != nil {
			return err
		}
		if common.json {
			return writeJSON(stdout, map[string]any{"projects": projects, "project": project})
		}
		fmt.Fprintf(stdout, "已添加报送项目「%s」（%s）\n", project.Name, project.ID)
		return nil
	case "rename":
		if err := requireArgs(pos, 2, "用法: kuibu report project rename <id|名称> <新名称>"); err != nil {
			return err
		}
		projects, project, err := ws.RenameReportProject(pos[0], pos[1])
		if err != nil {
			return err
		}
		if common.json {
			return writeJSON(stdout, map[string]any{"projects": projects, "project": project})
		}
		fmt.Fprintf(stdout, "已改名为「%s」（%s）\n", project.Name, project.ID)
		return nil
	case "rm":
		if err := requireArgs(pos, 1, "用法: kuibu report project rm <id|名称> [--yes]"); err != nil {
			return err
		}
		project, ok := store.FindReportProject(mustProjects(ws), pos[0])
		if !ok {
			return fmt.Errorf("%w: 找不到报送项目 %q", store.ErrNotFound, pos[0])
		}
		if !*yes {
			return fmt.Errorf("%w：删除「%s」后，已分到它的筛选条目会退回待定；确认请加 --yes", store.ErrNeedsConfirm, project.Name)
		}
		projects, removed, err := ws.RemoveReportProject(pos[0])
		if err != nil {
			return err
		}
		if common.json {
			return writeJSON(stdout, map[string]any{"projects": projects, "removed": removed})
		}
		fmt.Fprintf(stdout, "已删除「%s」；旧筛选记录会在下次打开该周筛选时把相关条目退回待定\n", removed.Name)
		return nil
	case "se":
		if err := requireArgs(pos, 1, "用法: kuibu report project se <id|名称> (--mine [--groups 组1,组2] | --se <称呼>)"); err != nil {
			return err
		}
		if *mine == (*seName != "") {
			return usagef("report project se 需要 --mine 或 --se <称呼>（二选一）")
		}
		var groupIDs []string
		if *mine && strings.TrimSpace(*groupsFlag) != "" {
			groups, err := core.EnsureGroups(ws, time.Now())
			if err != nil {
				return err
			}
			for _, name := range strings.Split(*groupsFlag, ",") {
				name = strings.TrimSpace(name)
				if name == "" {
					continue
				}
				g, ok := store.ReportGroupByID(groups, name)
				if !ok {
					return fmt.Errorf("%w: 找不到组 %q", store.ErrNotFound, name)
				}
				groupIDs = append(groupIDs, g.ID)
			}
		}
		projects, project, err := ws.SetReportProjectSE(pos[0], *mine, *seName, groupIDs)
		if err != nil {
			return err
		}
		if common.json {
			return writeJSON(stdout, map[string]any{"projects": projects, "project": project})
		}
		fmt.Fprintf(stdout, "「%s」：%s\n", project.Name, projectSELabel(project))
		return nil
	default:
		return usagef("未知 report project 子命令 %q", sub)
	}
}

func mustProjects(ws *store.Workspace) []store.ReportProject {
	projects, err := ws.ReportProjects()
	if err != nil {
		return nil
	}
	return projects
}

func projectByIDOrName(ws *store.Workspace, idOrName string) (store.ReportProject, error) {
	projects, err := ws.ReportProjects()
	if err != nil {
		return store.ReportProject{}, err
	}
	p, ok := store.FindReportProject(projects, idOrName)
	if !ok {
		return store.ReportProject{}, fmt.Errorf("%w: 找不到报送项目 %q", store.ErrNotFound, idOrName)
	}
	return p, nil
}

func projectSummary(projects []store.ReportProject) string {
	names := make([]string, 0, len(projects))
	for _, p := range projects {
		names = append(names, p.Name)
	}
	return strings.Join(names, "、")
}
