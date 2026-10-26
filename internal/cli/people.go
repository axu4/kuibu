package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/axu4/kuibu/internal/store"
)

// personJSON 是组员的 JSON 输出结构。Body 只在 show 时填充。
type personJSON struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Aliases      []string            `json:"aliases"`
	Group        string              `json:"group"`
	ProductLines []string            `json:"productLines"`
	Role         string              `json:"role"`
	Active       bool                `json:"active"`
	GroupHistory []store.GroupRecord `json:"groupHistory"`
	Created      time.Time           `json:"created"`
	Updated      time.Time           `json:"updated"`
	Path         string              `json:"path"`
	Body         *string             `json:"body,omitempty"`
}

// personJSONFromStore 把 store.Person 转为 JSON 结构。
func personJSONFromStore(ws *store.Workspace, p *store.Person, withBody bool) personJSON {
	aliases := p.Aliases
	if aliases == nil {
		aliases = []string{}
	}
	products := p.ProductLines
	if products == nil {
		products = []string{}
	}
	history := p.GroupHistory
	if history == nil {
		history = []store.GroupRecord{}
	}
	out := personJSON{
		ID:           p.ID,
		Name:         p.Name,
		Aliases:      aliases,
		Group:        p.Group,
		ProductLines: products,
		Role:         p.Role,
		Active:       p.Active,
		GroupHistory: history,
		Created:      p.Created,
		Updated:      p.Updated,
		Path:         filepath.Join(ws.PeopleDir(), p.ID+".md"),
	}
	if withBody {
		body := p.Body
		out.Body = &body
	}
	return out
}

// runPerson 分发 person 子命令。
func runPerson(ctx context.Context, args []string, common *commonFlags, stdin io.Reader, stdout, stderr io.Writer, env func(string) string) error {
	if len(args) == 0 {
		return usagef("person 需要子命令: add/list/show/edit/move")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "add":
		return personAdd(rest, common, stdout, stderr, env)
	case "list":
		return personList(rest, common, stdout, stderr, env)
	case "show":
		return personShow(rest, common, stdout, stderr, env)
	case "edit":
		return personEdit(rest, common, stdout, stderr, env)
	case "move":
		return personMove(rest, common, stdout, stderr, env)
	default:
		return usagef("未知 person 子命令 %q", sub)
	}
}

// personAdd 实现 person add。
func personAdd(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("person add")
	addCommonFlags(fs, common)
	var aliases, products stringList
	fs.Var(&aliases, "alias", "别名，可重复")
	group := fs.String("group", "", "所属组")
	fs.Var(&products, "product", "产品线，可重复")
	role := fs.String("role", store.RoleMember, "角色: member|lead")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(pos, 1, "用法: kuibu person add <姓名> [--alias a]... [--group g] [--product p]... [--role member|lead]"); err != nil {
		return err
	}
	if !store.ValidPersonRole(*role) {
		return usagef("非法角色 %q（member|lead）", *role)
	}

	now, err := nowFrom(env)
	if err != nil {
		return err
	}
	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	person, err := ws.CreatePerson(store.NewPerson{
		Name:         pos[0],
		Aliases:      aliases,
		Group:        *group,
		ProductLines: products,
		Role:         *role,
		Now:          now,
	})
	if err != nil {
		return err
	}
	upsertPersonIndex(ws, person.ID, stderr)

	if common.json {
		return writeJSON(stdout, personJSONFromStore(ws, person, false))
	}
	fmt.Fprintf(stdout, "已添加组员 %s「%s」\n", person.ID, person.Name)
	return nil
}

// personList 实现 person list。
func personList(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("person list")
	addCommonFlags(fs, common)
	group := fs.String("group", "", "按组过滤")
	role := fs.String("role", "", "按角色过滤")
	all := fs.Bool("all", false, "含离职")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("person list 不接受位置参数: %s", strings.Join(pos, " "))
	}
	if *role != "" && !store.ValidPersonRole(*role) {
		return usagef("非法角色 %q（member|lead）", *role)
	}

	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	people, err := ws.ListPersons()
	if err != nil {
		return err
	}
	filtered := make([]*store.Person, 0, len(people))
	for _, p := range people {
		if !*all && !p.Active {
			continue
		}
		if *group != "" && p.Group != *group {
			continue
		}
		if *role != "" && p.Role != *role {
			continue
		}
		filtered = append(filtered, p)
	}

	if common.json {
		out := make([]personJSON, 0, len(filtered))
		for _, p := range filtered {
			out = append(out, personJSONFromStore(ws, p, false))
		}
		return writeJSON(stdout, out)
	}
	if len(filtered) == 0 {
		fmt.Fprintln(stdout, "（没有组员）")
		return nil
	}
	for _, p := range filtered {
		active := "在职"
		if !p.Active {
			active = "离职"
		}
		fmt.Fprintf(stdout, "%s  %s  %s  %s  %s\n", p.ID, p.Name, p.Role, p.Group, active)
	}
	return nil
}

// personShow 实现 person show。
func personShow(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("person show")
	addCommonFlags(fs, common)
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(pos, 1, "用法: kuibu person show <id|姓名>"); err != nil {
		return err
	}

	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	p, err := ws.ResolvePerson(pos[0])
	if err != nil {
		return err
	}

	if common.json {
		return writeJSON(stdout, personJSONFromStore(ws, p, true))
	}
	active := "在职"
	if !p.Active {
		active = "离职"
	}
	fmt.Fprintf(stdout, "%s  (%s)\n", p.Name, p.ID)
	fmt.Fprintf(stdout, "角色: %s\n", roleLabel(p.Role))
	fmt.Fprintf(stdout, "状态: %s\n", active)
	fmt.Fprintf(stdout, "组: %s\n", orDash(p.Group))
	fmt.Fprintf(stdout, "产品线: %s\n", orDash(strings.Join(p.ProductLines, ", ")))
	fmt.Fprintf(stdout, "别名: %s\n", orDash(strings.Join(p.Aliases, ", ")))
	if len(p.GroupHistory) > 0 {
		fmt.Fprintln(stdout, "转组记录:")
		for _, r := range p.GroupHistory {
			fmt.Fprintf(stdout, "  - %s %s %s\n", r.From, r.Group, strings.Join(r.ProductLines, ", "))
		}
	}
	fmt.Fprintf(stdout, "创建: %s\n", formatLocal(p.Created))
	fmt.Fprintf(stdout, "更新: %s\n", formatLocal(p.Updated))
	if p.Body != "" {
		fmt.Fprintln(stdout, "备注:")
		fmt.Fprint(stdout, p.Body)
		if !strings.HasSuffix(p.Body, "\n") {
			fmt.Fprintln(stdout)
		}
	}
	return nil
}

// personEdit 实现 person edit。
func personEdit(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("person edit")
	addCommonFlags(fs, common)
	name := fs.String("name", "", "新姓名")
	var aliases, products stringList
	fs.Var(&aliases, "alias", "替换别名，可重复")
	noAlias := fs.Bool("no-alias", false, "清空别名")
	fs.Var(&products, "product", "替换产品线，可重复")
	role := fs.String("role", "", "新角色: member|lead")
	active := fs.String("active", "", "在职: true|false")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(pos, 1, "用法: kuibu person edit <id|姓名> [--name n] [--alias a ...|--no-alias] [--product p ...] [--role r] [--active true|false]"); err != nil {
		return err
	}
	visited := visitedSet(visitedFlags(fs))
	if !visited["name"] && !visited["alias"] && !visited["no-alias"] && !visited["product"] && !visited["role"] && !visited["active"] {
		return usagef("person edit 至少需要一个修改项")
	}
	if visited["alias"] && *noAlias {
		return usagef("--alias 与 --no-alias 不能同时使用")
	}
	if visited["role"] && !store.ValidPersonRole(*role) {
		return usagef("非法角色 %q（member|lead）", *role)
	}
	var activeVal *bool
	if visited["active"] {
		switch strings.TrimSpace(*active) {
		case "true":
			v := true
			activeVal = &v
		case "false":
			v := false
			activeVal = &v
		default:
			return usagef("--active 只接受 true 或 false，得到 %q", *active)
		}
	}

	now, err := nowFrom(env)
	if err != nil {
		return err
	}
	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	target, err := ws.ResolvePerson(pos[0])
	if err != nil {
		return err
	}
	p, err := ws.UpdatePerson(target.ID, now, func(p *store.Person) error {
		if visited["name"] {
			p.Name = *name
		}
		if visited["alias"] {
			p.Aliases = append([]string(nil), aliases...)
		}
		if *noAlias {
			p.Aliases = []string{}
		}
		if visited["product"] {
			p.ProductLines = append([]string(nil), products...)
		}
		if visited["role"] {
			p.Role = *role
		}
		if activeVal != nil {
			p.Active = *activeVal
		}
		return nil
	})
	if err != nil {
		return err
	}
	upsertPersonIndex(ws, p.ID, stderr)

	if common.json {
		return writeJSON(stdout, personJSONFromStore(ws, p, true))
	}
	fmt.Fprintf(stdout, "已更新组员 %s\n", p.ID)
	return nil
}

// personMove 实现 person move。
func personMove(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("person move")
	addCommonFlags(fs, common)
	group := fs.String("group", "", "新组")
	var products stringList
	fs.Var(&products, "product", "新产品线，可重复")
	from := fs.String("from", "", "生效日期 YYYY-MM-DD，默认今天")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(pos, 1, "用法: kuibu person move <id|姓名> --group g [--product p]... [--from YYYY-MM-DD]"); err != nil {
		return err
	}
	visited := visitedSet(visitedFlags(fs))
	if !visited["group"] || strings.TrimSpace(*group) == "" {
		return usagef("person move 需要 --group")
	}

	now, err := nowFrom(env)
	if err != nil {
		return err
	}
	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	target, err := ws.ResolvePerson(pos[0])
	if err != nil {
		return err
	}
	p, err := ws.MovePerson(target.ID, *group, products, *from, now)
	if err != nil {
		return err
	}
	upsertPersonIndex(ws, p.ID, stderr)

	if common.json {
		return writeJSON(stdout, personJSONFromStore(ws, p, true))
	}
	fmt.Fprintf(stdout, "已转组 %s「%s」→ %s（%s 起生效）\n", p.ID, p.Name, *group, moveFromLabel(*from, now))
	return nil
}

// roleLabel 返回角色中文名。
func roleLabel(role string) string {
	if role == store.RoleLead {
		return "lead（组长）"
	}
	return "member（组员）"
}

// orDash 空字符串输出 "-"。
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// upsertPersonIndex 增量更新单条组员索引，失败只警告。
func upsertPersonIndex(ws *store.Workspace, id string, stderr io.Writer) {
	idx := openIndex(ws, stderr)
	if idx == nil {
		return
	}
	defer idx.Close()
	path := filepath.Join(ws.PeopleDir(), id+".md")
	if _, err := idx.UpsertFile(path); err != nil {
		fmt.Fprintf(stderr, "kuibu: 警告: 更新索引失败（不影响数据）: %v\n", err)
	}
}

// moveFromLabel 返回转组生效日期的展示文本，未指定时为今天。
func moveFromLabel(from string, now time.Time) string {
	if strings.TrimSpace(from) != "" {
		return strings.TrimSpace(from)
	}
	return now.In(store.TodoLocation()).Format("2006-01-02")
}
