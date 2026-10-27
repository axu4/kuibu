package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/axu4/kuibu/internal/core"
)

// runMeeting 实现 meeting open|list：每周一条周会笔记。
func runMeeting(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	if len(args) == 0 {
		return usagef("meeting 需要子命令: open/list")
	}
	sub, rest := args[0], args[1:]
	fs := newFlagSet("meeting " + sub)
	addCommonFlags(fs, common)
	weekFlag := fs.String("week", "", "周编号，默认本周（open）")
	pos, err := parseCommand(fs, rest)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("meeting %s 不接受位置参数", sub)
	}
	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	switch sub {
	case "open":
		now, err := nowFrom(env)
		if err != nil {
			return err
		}
		week, err := weekOrThis(*weekFlag, now)
		if err != nil {
			return err
		}
		n, created, err := core.OpenMeeting(ws, week)
		if err != nil {
			return err
		}
		if created {
			upsertIndex(ws, n.Parent, stderr)
			upsertIndex(ws, n.ID, stderr)
		}
		if common.json {
			return writeJSON(stdout, map[string]any{"id": n.ID, "title": n.Title, "week": week, "created": created})
		}
		fmt.Fprintln(stdout, n.ID)
		return nil
	case "list":
		list, err := core.ListMeetings(ws)
		if err != nil {
			return err
		}
		if common.json {
			return writeJSON(stdout, list)
		}
		for _, m := range list {
			fmt.Fprintf(stdout, "%s  %s  %s\n", m.Week, m.ID, m.Title)
		}
		return nil
	}
	return usagef("未知 meeting 子命令 %q", sub)
}

// noteExtractTodos 实现 note extract-todos：把笔记里未勾选的「- [ ] …」行建成待办。
// --dry-run 只预览；已抽过的行不会重复创建。
func noteExtractTodos(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("note extract-todos")
	addCommonFlags(fs, common)
	dryRun := fs.Bool("dry-run", false, "只预览，不创建")
	var only stringList
	fs.Var(&only, "only", "只创建这些键（预览里的 key），可重复")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(pos, 1, "用法: kuibu note extract-todos <id> [--dry-run] [--only key]..."); err != nil {
		return err
	}
	now, err := nowFrom(env)
	if err != nil {
		return err
	}
	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	note, err := ws.Get(pos[0])
	if err != nil {
		return err
	}
	if *dryRun {
		cands := core.TodoCandidates(note, now)
		if common.json {
			return writeJSON(stdout, cands)
		}
		if len(cands) == 0 {
			fmt.Fprintln(stdout, "笔记里没有「- [ ] …」待办行")
			return nil
		}
		for _, c := range cands {
			mark := "新建"
			if c.TodoID != "" {
				mark = "已抽过"
			}
			fmt.Fprintf(stdout, "[%s] %s  %s", c.Key, mark, c.Title)
			if c.Due != nil {
				fmt.Fprintf(stdout, "  截止 %s", c.Due.Date)
			}
			if len(c.Tags) > 0 {
				fmt.Fprintf(stdout, "  #%s", strings.Join(c.Tags, " #"))
			}
			fmt.Fprintln(stdout)
		}
		return nil
	}
	created, err := core.ExtractTodos(ws, note.ID, only, now)
	for _, t := range created {
		upsertTodoIndex(ws, t.ID, stderr)
	}
	if len(created) > 0 {
		upsertIndex(ws, note.ID, stderr)
	}
	if err != nil {
		return err
	}
	if common.json {
		out := make([]todoJSON, 0, len(created))
		for _, t := range created {
			out = append(out, todoJSONFromStore(ws, t, now, false, nil))
		}
		return writeJSON(stdout, out)
	}
	fmt.Fprintf(stdout, "已创建 %d 条待办\n", len(created))
	return nil
}
