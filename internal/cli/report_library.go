package cli

import (
	"fmt"
	"io"

	"github.com/axu4/kuibu/internal/core"
)

// reportLibrary 实现 report library：列出某周所有已定稿组报（跨组）。
func reportLibrary(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("report library")
	addCommonFlags(fs, common)
	weekFlag := fs.String("week", "", "周编号，默认本周")
	group := fs.String("group", "", "只看某个组")
	project := fs.String("project", "", "只看某个项目")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("report library 不接受位置参数")
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
	list, err := core.ListReportLibrary(ws, week, *group, *project)
	if err != nil {
		return err
	}
	if common.json {
		return writeJSON(stdout, list)
	}
	if len(list) == 0 {
		fmt.Fprintf(stdout, "%s 还没有已定稿组报\n", week)
		return nil
	}
	for _, r := range list {
		fmt.Fprintf(stdout, "%-10s %-14s %2d 条  定稿 %s\n", r.Group, orDash(r.Project), r.Count, r.FinalizedAt)
	}
	return nil
}
