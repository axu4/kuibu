package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/axu4/kuibu/internal/core"
	"github.com/axu4/kuibu/internal/store"
)

// reportGroups 实现 report groups list|add|rename|rm|set。
func reportGroups(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	sub := "list"
	rest := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, rest = args[0], args[1:]
	}
	fs := newFlagSet("report groups " + sub)
	addCommonFlags(fs, common)
	name := fs.String("name", "", "组名")
	lead := fs.String("lead", "", "组长 personId 或姓名")
	members := fs.String("members", "", "成员 personId 或姓名，逗号分隔")
	yes := fs.Bool("yes", false, "确认删除（有成员会拒绝）")
	pos, err := parseCommand(fs, rest)
	if err != nil {
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
	list := func() error {
		groups, err := core.ListGroups(ws, now)
		if err != nil {
			return err
		}
		if common.json {
			return writeJSON(stdout, groups)
		}
		for _, g := range groups {
			names := make([]string, 0, len(g.Members))
			for _, m := range g.Members {
				names = append(names, m.Name)
			}
			fmt.Fprintf(stdout, "%-10s 组长 %-6s 成员 %d：%s\n", g.Name, orDash(g.LeadName), len(g.Members), strings.Join(names, "、"))
		}
		return nil
	}
	resolvePerson := func(idOrName string) (string, error) {
		if strings.TrimSpace(idOrName) == "" {
			return "", nil
		}
		p, err := ws.ResolvePerson(idOrName)
		if err != nil {
			return "", err
		}
		return p.ID, nil
	}
	switch sub {
	case "list":
		if len(pos) > 0 {
			return usagef("report groups list 不接受位置参数")
		}
		return list()
	case "add":
		if err := requireArgs(pos, 1, "用法: kuibu report groups add <名称> [--lead 人]"); err != nil {
			return err
		}
		leadID, err := resolvePerson(*lead)
		if err != nil {
			return err
		}
		if _, err := core.AddGroup(ws, pos[0], leadID, now); err != nil {
			return err
		}
		return list()
	case "rename":
		if err := requireArgs(pos, 2, "用法: kuibu report groups rename <id|名称> <新名称>"); err != nil {
			return err
		}
		if _, err := core.RenameGroup(ws, pos[0], pos[1], now); err != nil {
			return err
		}
		return list()
	case "rm":
		if err := requireArgs(pos, 1, "用法: kuibu report groups rm <id|名称> [--yes]"); err != nil {
			return err
		}
		if !*yes {
			return fmt.Errorf("%w：删除组前请确认；没有在职成员的组才能删除，确认请加 --yes", store.ErrNeedsConfirm)
		}
		if _, err := core.DeleteGroup(ws, pos[0], now); err != nil {
			return err
		}
		return list()
	case "set":
		if err := requireArgs(pos, 1, "用法: kuibu report groups set <id|名称> [--name 新名] [--lead 人] [--members a,b]"); err != nil {
			return err
		}
		groups, err := core.ListGroups(ws, now)
		if err != nil {
			return err
		}
		var current *core.GroupInfo
		for i := range groups {
			if groups[i].ID == pos[0] || groups[i].Name == pos[0] {
				current = &groups[i]
				break
			}
		}
		if current == nil {
			return fmt.Errorf("%w: 找不到组 %q", store.ErrNotFound, pos[0])
		}
		newName := current.Name
		if *name != "" {
			newName = *name
		}
		leadID := current.LeadID
		if *lead != "" {
			leadID, err = resolvePerson(*lead)
			if err != nil {
				return err
			}
		}
		memberIDs := make([]string, 0, len(current.Members))
		for _, m := range current.Members {
			memberIDs = append(memberIDs, m.ID)
		}
		if *members != "" {
			memberIDs = nil
			for _, raw := range strings.Split(*members, ",") {
				id, err := resolvePerson(strings.TrimSpace(raw))
				if err != nil {
					return err
				}
				if id != "" {
					memberIDs = append(memberIDs, id)
				}
			}
		}
		if _, err := core.UpdateGroup(ws, current.ID, newName, leadID, memberIDs, now); err != nil {
			return err
		}
		return list()
	default:
		return usagef("未知 report groups 子命令 %q（list/add/rename/rm/set）", sub)
	}
}
