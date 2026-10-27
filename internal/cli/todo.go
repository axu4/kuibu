package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/axu4/kuibu/internal/core"
	"github.com/axu4/kuibu/internal/index"
	"github.com/axu4/kuibu/internal/store"
)

// todoJSON 是待办的 JSON 输出结构。Body 只在 show/edit 时填充，Children 只在
// tree/show 时填充（非 nil 指针保证空列表也输出 [] 而不是被 omitempty 省略）。
type todoJSON struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	Status      string           `json:"status"`
	Due         *store.Due       `json:"due"`
	DueAt       *time.Time       `json:"dueAt"`
	DoneAt      *time.Time       `json:"doneAt"`
	Overdue     bool             `json:"overdue"`
	Created     time.Time        `json:"created"`
	Updated     time.Time        `json:"updated"`
	Tags        []string         `json:"tags"`
	Parent      string           `json:"parent"`
	Order       int              `json:"order"`
	Links       []store.TodoLink `json:"links"`
	Attachments []string         `json:"attachments"`
	Source      string           `json:"source"`
	Path        string           `json:"path"`
	Body        *string          `json:"body,omitempty"`
	Children    *[]todoJSON      `json:"children,omitempty"`
}

// runTodo 分发 todo 子命令。
func runTodo(ctx context.Context, args []string, common *commonFlags, stdin io.Reader, stdout, stderr io.Writer, env func(string) string) error {
	if len(args) == 0 {
		return usagef("todo 需要子命令: add/list/show/done/reopen/start/cancel/edit/sub/link/unlink/attach/detach/mv/rm")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "add":
		return todoAdd(rest, common, stdin, stdout, stderr, env)
	case "list":
		return todoList(ctx, rest, common, stdout, stderr, env)
	case "show":
		return todoShow(rest, common, stdout, stderr, env)
	case "done":
		return todoSetStatus(rest, common, stdout, stderr, env, store.StatusDone, "已完成")
	case "reopen":
		return todoSetStatus(rest, common, stdout, stderr, env, store.StatusTodo, "已重新打开")
	case "start":
		return todoSetStatus(rest, common, stdout, stderr, env, store.StatusDoing, "已开始")
	case "cancel":
		return todoSetStatus(rest, common, stdout, stderr, env, store.StatusCanceled, "已取消")
	case "edit":
		return todoEdit(rest, common, stdin, stdout, stderr, env)
	case "sub":
		return todoSub(rest, common, stdin, stdout, stderr, env)
	case "link":
		return todoLink(rest, common, stdout, stderr, env)
	case "unlink":
		return todoUnlink(rest, common, stdout, stderr, env)
	case "attach":
		return todoAttach(rest, common, stdout, stderr, env)
	case "detach":
		return todoDetach(rest, common, stdout, stderr, env)
	case "mv":
		return todoMove(rest, common, stdout, stderr, env)
	case "rm":
		return todoRemove(ctx, rest, common, stdout, stderr, env)
	default:
		return usagef("未知 todo 子命令 %q", sub)
	}
}

// nowFrom 返回命令使用的「现在」：KUIBU_NOW（RFC3339）存在时用它，否则 time.Now()。
func nowFrom(env func(string) string) (time.Time, error) {
	if v := env("KUIBU_NOW"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return time.Time{}, usagef("KUIBU_NOW 不是合法 RFC3339: %v", err)
		}
		return t, nil
	}
	return time.Now(), nil
}

// parseDueArg 解析 --due 的值：先按 store.ParseDue，再按快速录入表达式。
func parseDueArg(value string, now time.Time) (*store.Due, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	if d, err := store.ParseDue(value); err == nil {
		return d, nil
	}
	if q := core.ParseQuickAdd(value, now); q.Due != nil {
		return q.Due, nil
	}
	return nil, usagef("无法识别截止时间 %q", value)
}

// dedupTags 去重保持顺序并过滤空串。
func dedupTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, t := range tags {
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// createTodo 是 add 与 sub 的公共实现。
func createTodo(common *commonFlags, stdin io.Reader, stdout, stderr io.Writer, env func(string) string,
	text, dueFlag string, dueVisited bool, flagTags []string, parentID string, raw, dryRun bool, src bodySource, srcVisited bool) error {
	if strings.TrimSpace(text) == "" {
		return usagef("需要待办标题文本")
	}
	now, err := nowFrom(env)
	if err != nil {
		return err
	}

	quick := core.QuickAdd{Input: text, Tags: []string{}, Matches: []core.QuickMatch{}}
	if raw {
		quick.Title = strings.TrimSpace(text)
	} else {
		quick = core.ParseQuickAdd(text, now)
	}

	due := quick.Due
	if dueVisited {
		if dueFlag == "" {
			due = nil
		} else {
			d, err := parseDueArg(dueFlag, now)
			if err != nil {
				return err
			}
			due = d
		}
	}

	tags := dedupTags(append(append([]string{}, quick.Tags...), flagTags...))
	final := quick
	final.Due = due
	final.Tags = tags

	body := ""
	if srcVisited {
		body, err = src.read(stdin)
		if err != nil {
			return err
		}
	}

	if dryRun {
		if common.json {
			return writeJSON(stdout, map[string]any{"todo": nil, "parsed": final})
		}
		fmt.Fprintf(stdout, "解析结果: 标题「%s」", final.Title)
		if final.Due != nil {
			fmt.Fprintf(stdout, " 截止 %s", formatDueHuman(final.Due.String()))
		}
		if len(final.Tags) > 0 {
			fmt.Fprintf(stdout, " 标签 %s", strings.Join(final.Tags, " "))
		}
		fmt.Fprintln(stdout)
		return nil
	}

	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	todo, err := ws.CreateTodo(store.NewTodo{
		Title:  final.Title,
		Body:   body,
		Parent: parentID,
		Tags:   final.Tags,
		Due:    final.Due,
		Now:    now,
	})
	if err != nil {
		return err
	}
	upsertTodoIndex(ws, todo.ID, stderr)

	if common.json {
		return writeJSON(stdout, map[string]any{"todo": todoJSONFromStore(ws, todo, now, false, nil), "parsed": final})
	}
	fmt.Fprintf(stdout, "已创建待办 %s「%s」", todo.ID, todo.Title)
	if todo.Due != nil {
		fmt.Fprintf(stdout, " 截止 %s", formatDueHuman(todo.Due.String()))
	}
	if len(todo.Tags) > 0 {
		fmt.Fprintf(stdout, " #%s", strings.Join(todo.Tags, " #"))
	}
	fmt.Fprintln(stdout)
	return nil
}

// todoAdd 实现 todo add。
func todoAdd(args []string, common *commonFlags, stdin io.Reader, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("todo add")
	addCommonFlags(fs, common)
	var tags stringList
	fs.Var(&tags, "tag", "标签，可重复")
	dueFlag := fs.String("due", "", "截止日期，优先于文本解析")
	parent := fs.String("parent", "", "父待办 ID")
	raw := fs.Bool("raw", false, "不解析文本")
	dryRun := fs.Bool("dry-run", false, "只输出解析结果，不创建")
	var src bodySource
	src.register(fs)
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	visited := visitedSet(visitedFlags(fs))
	return createTodo(common, stdin, stdout, stderr, env, strings.Join(pos, " "), *dueFlag,
		visited["due"], tags, *parent, *raw, *dryRun, src, visited["body"] || visited["file"] || visited["stdin"])
}

// todoSub 实现 todo sub <id> "<文本>"，等价于 todo add --parent <id>。
func todoSub(args []string, common *commonFlags, stdin io.Reader, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("todo sub")
	addCommonFlags(fs, common)
	var tags stringList
	fs.Var(&tags, "tag", "标签，可重复")
	dueFlag := fs.String("due", "", "截止日期，优先于文本解析")
	raw := fs.Bool("raw", false, "不解析文本")
	dryRun := fs.Bool("dry-run", false, "只输出解析结果，不创建")
	var src bodySource
	src.register(fs)
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 2 {
		return usagef("用法: kuibu todo sub <id> \"<文本>\" [--due <值>] [--tag t] [--raw]")
	}
	visited := visitedSet(visitedFlags(fs))
	return createTodo(common, stdin, stdout, stderr, env, strings.Join(pos[1:], " "), *dueFlag,
		visited["due"], tags, pos[0], *raw, *dryRun, src, visited["body"] || visited["file"] || visited["stdin"])
}

// todoList 实现 todo list。
func todoList(ctx context.Context, args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("todo list")
	addCommonFlags(fs, common)
	var statusFlags stringList
	fs.Var(&statusFlags, "status", "状态过滤，逗号分隔或重复")
	dueFlag := fs.String("due", "", "截止过滤: today|tomorrow|week|overdue")
	var tags stringList
	fs.Var(&tags, "tag", "标签，可重复")
	parent := fs.String("parent", "", "父待办 ID")
	root := fs.Bool("root", false, "只看顶层")
	search := fs.String("search", "", "标题与正文检索")
	tree := fs.Bool("tree", false, "按父子缩进显示")
	view := fs.String("view", "", "视图: today（含逾期）|week|all|done，与 App 侧栏一致")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("todo list 不接受位置参数: %s", strings.Join(pos, " "))
	}
	visited := visitedSet(visitedFlags(fs))
	if *view != "" && !core.ValidTodoView(*view) {
		return usagef("非法 --view 值 %q（today|week|all|done）", *view)
	}
	if *view != "" && (visited["due"] || visited["status"]) {
		return usagef("--view 不能与 --due / --status 同时使用")
	}

	now, err := nowFrom(env)
	if err != nil {
		return err
	}

	var statuses []string
	if visited["status"] {
		var collected []string
		all := false
		for _, v := range statusFlags {
			for _, part := range strings.Split(v, ",") {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				if part == "all" {
					all = true
					continue
				}
				if !store.ValidTodoStatus(part) {
					return usagef("非法待办状态 %q", part)
				}
				collected = append(collected, part)
			}
		}
		if !all {
			statuses = collected
		}
	} else if *view != "" {
		statuses = core.TodoViewStatuses(*view)
	} else {
		statuses = []string{store.StatusTodo, store.StatusDoing}
	}

	filter := index.TodoFilter{Statuses: statuses, Tags: tags, Query: *search}
	if visited["due"] && *dueFlag != "" {
		switch *dueFlag {
		case "today", "tomorrow", "week":
			from, to, err := core.DueRange(*dueFlag, now)
			if err != nil {
				return usagef("%v", err)
			}
			filter.DueFrom, filter.DueTo = from, to
		case "overdue":
			filter.OverdueAt = now
		default:
			return usagef("非法 --due 值 %q（today|tomorrow|week|overdue）", *dueFlag)
		}
	}
	if visited["parent"] && *root {
		return usagef("--parent 与 --root 不能同时使用")
	}
	if *root {
		top := ""
		filter.Parent = &top
	} else if visited["parent"] {
		filter.Parent = parent
	}

	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	idx := syncIndex(ctx, ws, stderr)
	if idx == nil {
		return fmt.Errorf("索引不可用，无法列出待办")
	}
	defer idx.Close()

	matched, err := idx.ListTodos(filter)
	if err != nil {
		return err
	}
	if *view != "" {
		kept := matched[:0]
		for _, m := range matched {
			ok, err := core.InTodoView(*view, m.Due, now)
			if err != nil {
				return err
			}
			if ok {
				kept = append(kept, m)
			}
		}
		matched = kept
	}

	if *tree {
		all, err := idx.ListTodos(index.TodoFilter{})
		if err != nil {
			return err
		}
		childrenStats := todoChildrenStats(all)
		nodes := buildTodoTree(all, matched)
		if common.json {
			out := make([]todoJSON, 0, len(nodes))
			for _, n := range nodes {
				out = append(out, todoTreeJSON(n, now))
			}
			return writeJSON(stdout, out)
		}
		if len(nodes) == 0 {
			fmt.Fprintln(stdout, "没有待办")
			return nil
		}
		var b strings.Builder
		printTodoTree(&b, nodes, 0, now, childrenStats)
		fmt.Fprint(stdout, b.String())
		return nil
	}

	all, err := idx.ListTodos(index.TodoFilter{})
	if err != nil {
		return err
	}
	children := todoChildrenStats(all)

	if common.json {
		out := make([]todoJSON, 0, len(matched))
		for _, m := range matched {
			out = append(out, todoJSONFromMeta(m, now, nil))
		}
		return writeJSON(stdout, out)
	}
	if len(matched) == 0 {
		fmt.Fprintln(stdout, "没有待办")
		return nil
	}
	for _, m := range matched {
		fmt.Fprintln(stdout, formatTodoLine(m, now, children[m.ID]))
	}
	return nil
}

// todoShow 实现 todo show。
func todoShow(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("todo show")
	addCommonFlags(fs, common)
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(pos, 1, "用法: kuibu todo show <id>"); err != nil {
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
	t, err := ws.GetTodo(pos[0])
	if err != nil {
		return err
	}
	children, err := ws.TodoChildren(t.ID)
	if err != nil {
		return err
	}
	childJSON := make([]todoJSON, 0, len(children))
	for _, c := range children {
		childJSON = append(childJSON, todoJSONFromStore(ws, c, now, false, nil))
	}

	if common.json {
		return writeJSON(stdout, todoJSONFromStore(ws, t, now, true, &childJSON))
	}

	fmt.Fprintf(stdout, "%s  (%s)\n", t.Title, t.ID)
	fmt.Fprintf(stdout, "状态: %s\n", statusLabel(t.Status))
	if t.Due != nil {
		fmt.Fprintf(stdout, "截止: %s\n", formatDueHuman(t.Due.String()))
	} else {
		fmt.Fprintln(stdout, "截止: -")
	}
	fmt.Fprintf(stdout, "创建: %s\n", formatLocal(t.Created))
	fmt.Fprintf(stdout, "更新: %s\n", formatLocal(t.Updated))
	if t.DoneAt != nil && !t.DoneAt.IsZero() {
		fmt.Fprintf(stdout, "完成: %s\n", formatLocal(*t.DoneAt))
	} else {
		fmt.Fprintln(stdout, "完成: -")
	}
	if len(t.Tags) > 0 {
		fmt.Fprintf(stdout, "标签: %s\n", strings.Join(t.Tags, ", "))
	} else {
		fmt.Fprintln(stdout, "标签: -")
	}
	if t.Parent != "" {
		fmt.Fprintf(stdout, "父任务: %s\n", t.Parent)
	} else {
		fmt.Fprintln(stdout, "父任务: 顶层")
	}
	if t.Source != "" {
		fmt.Fprintf(stdout, "来源: %s\n", t.Source)
	} else {
		fmt.Fprintln(stdout, "来源: -")
	}
	if len(t.Links) > 0 {
		fmt.Fprintln(stdout, "链接:")
		for _, l := range t.Links {
			if l.Title != "" {
				fmt.Fprintf(stdout, "  - %s  %s\n", l.URL, l.Title)
			} else {
				fmt.Fprintf(stdout, "  - %s\n", l.URL)
			}
		}
	}
	if len(t.Attachments) > 0 {
		fmt.Fprintln(stdout, "附件:")
		for _, a := range t.Attachments {
			fmt.Fprintf(stdout, "  - %s\n", a)
		}
	}
	if len(children) > 0 {
		fmt.Fprintln(stdout, "子任务:")
		for _, c := range children {
			fmt.Fprintf(stdout, "  - %s %s  %s\n", statusMark(c.Status), c.ID, c.Title)
		}
	}
	if t.Body != "" {
		fmt.Fprintln(stdout, "详情:")
		fmt.Fprint(stdout, t.Body)
		if !strings.HasSuffix(t.Body, "\n") {
			fmt.Fprintln(stdout)
		}
	}
	return nil
}

// todoSetStatus 实现 done/reopen/start/cancel。
func todoSetStatus(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string, status, verb string) error {
	fs := newFlagSet("todo " + status)
	addCommonFlags(fs, common)
	// 完成 / 取消父任务时，未完成的子任务怎么办需要明确选择。
	closing := status == store.StatusDone || status == store.StatusCanceled
	var withChildren, onlyParent *bool
	if closing {
		withChildren = fs.Bool("with-children", false, "连同全部未完成的子任务一起"+verb)
		onlyParent = fs.Bool("only-parent", false, "只处理本条，子任务保持不变")
	}
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(pos, 1, "用法: kuibu todo "+status+" <id>"); err != nil {
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
	var (
		t       *store.Todo
		changed []string
	)
	if closing && *withChildren && *onlyParent {
		return usagef("--with-children 与 --only-parent 不能同时使用")
	}
	if closing && !*withChildren && !*onlyParent {
		n, err := ws.OpenDescendantCount(pos[0])
		if err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("%w：还有 %d 个未完成的子任务，加 --with-children 一起处理，或 --only-parent 只处理本条", store.ErrNeedsConfirm, n)
		}
	}
	if closing && *withChildren {
		t, changed, err = ws.SetTodoStatusTree(pos[0], status, now)
	} else {
		t, err = ws.SetTodoStatus(pos[0], status, now)
		if t != nil {
			changed = []string{t.ID}
		}
	}
	if err != nil {
		return err
	}
	for _, id := range changed {
		upsertTodoIndex(ws, id, stderr)
	}

	if common.json {
		return writeJSON(stdout, todoJSONFromStore(ws, t, now, false, nil))
	}
	fmt.Fprintf(stdout, "%s %s\n", verb, t.ID)
	return nil
}

// todoEdit 实现 todo edit。
func todoEdit(args []string, common *commonFlags, stdin io.Reader, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("todo edit")
	addCommonFlags(fs, common)
	title := fs.String("title", "", "新标题")
	dueFlag := fs.String("due", "", "新截止日期")
	noDue := fs.Bool("no-due", false, "清除截止日期")
	var tags stringList
	fs.Var(&tags, "tag", "替换标签，可重复")
	statusFlag := fs.String("status", "", "新状态: todo|doing|done|canceled")
	var src bodySource
	src.register(fs)
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(pos, 1, "用法: kuibu todo edit <id> [--title t] [--due <值>|--no-due] [--tag t ...] [--status s] [--body/--file/--stdin]"); err != nil {
		return err
	}
	visited := visitedSet(visitedFlags(fs))
	srcVisited := visited["body"] || visited["file"] || visited["stdin"]
	if !visited["title"] && !visited["due"] && !visited["no-due"] && !visited["tag"] && !visited["status"] && !srcVisited {
		return usagef("todo edit 至少需要一个修改项（--title/--due/--no-due/--tag/--status/--body/--file/--stdin）")
	}
	if visited["due"] && *noDue {
		return usagef("--due 与 --no-due 不能同时使用")
	}
	if visited["status"] && !store.ValidTodoStatus(*statusFlag) {
		return usagef("非法待办状态 %q", *statusFlag)
	}

	now, err := nowFrom(env)
	if err != nil {
		return err
	}
	var newDue *store.Due
	if visited["due"] {
		if strings.TrimSpace(*dueFlag) == "" {
			return usagef("--due 需要一个非空值，清除截止用 --no-due")
		}
		d, err := parseDueArg(*dueFlag, now)
		if err != nil {
			return err
		}
		newDue = d
	}
	body, err := src.read(stdin)
	if err != nil {
		return err
	}

	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	t, err := ws.UpdateTodo(pos[0], now, func(t *store.Todo) error {
		if visited["title"] {
			t.Title = *title
		}
		if visited["due"] || *noDue {
			t.Due = newDue
		}
		if visited["tag"] {
			t.Tags = dedupTags(append([]string(nil), tags...))
		}
		if visited["status"] {
			t.Status = *statusFlag
		}
		if srcVisited {
			t.Body = body
		}
		return nil
	})
	if err != nil {
		return err
	}
	upsertTodoIndex(ws, t.ID, stderr)

	if common.json {
		return writeJSON(stdout, todoJSONFromStore(ws, t, now, true, nil))
	}
	fmt.Fprintf(stdout, "已更新 %s\n", t.ID)
	return nil
}

// todoLink 实现 todo link。
func todoLink(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("todo link")
	addCommonFlags(fs, common)
	title := fs.String("title", "", "链接标题")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(pos, 2, "用法: kuibu todo link <id> <url> [--title t]"); err != nil {
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
	t, err := ws.AddTodoLink(pos[0], store.TodoLink{URL: pos[1], Title: *title}, now)
	if err != nil {
		return err
	}
	upsertTodoIndex(ws, t.ID, stderr)

	if common.json {
		return writeJSON(stdout, todoJSONFromStore(ws, t, now, false, nil))
	}
	fmt.Fprintf(stdout, "已添加链接 %s\n", pos[1])
	return nil
}

// todoUnlink 实现 todo unlink。
func todoUnlink(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("todo unlink")
	addCommonFlags(fs, common)
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(pos, 2, "用法: kuibu todo unlink <id> <url>"); err != nil {
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
	t, err := ws.RemoveTodoLink(pos[0], pos[1], now)
	if err != nil {
		return err
	}
	upsertTodoIndex(ws, t.ID, stderr)

	if common.json {
		return writeJSON(stdout, todoJSONFromStore(ws, t, now, false, nil))
	}
	fmt.Fprintf(stdout, "已移除链接 %s\n", pos[1])
	return nil
}

// todoAttach 实现 todo attach。
func todoAttach(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("todo attach")
	addCommonFlags(fs, common)
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(pos, 2, "用法: kuibu todo attach <id> <file>"); err != nil {
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
	t, rel, err := ws.AttachTodoFile(pos[0], pos[1], now)
	if err != nil {
		return err
	}
	upsertTodoIndex(ws, t.ID, stderr)

	if common.json {
		return writeJSON(stdout, map[string]string{"path": rel})
	}
	fmt.Fprintln(stdout, rel)
	return nil
}

// todoDetach 实现 todo detach。
func todoDetach(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("todo detach")
	addCommonFlags(fs, common)
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(pos, 2, "用法: kuibu todo detach <id> <相对路径>"); err != nil {
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
	t, err := ws.DetachTodoFile(pos[0], pos[1], now)
	if err != nil {
		return err
	}
	upsertTodoIndex(ws, t.ID, stderr)

	if common.json {
		return writeJSON(stdout, map[string]string{"path": pos[1]})
	}
	fmt.Fprintf(stdout, "已移除附件 %s\n", pos[1])
	return nil
}

// todoMove 实现 todo mv。
func todoMove(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("todo mv")
	addCommonFlags(fs, common)
	parent := fs.String("parent", "", "新父待办 ID")
	root := fs.Bool("root", false, "移到顶层")
	position := fs.Int("position", -1, "在目标层级中的位置，缺省为末尾")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(pos, 1, "用法: kuibu todo mv <id> [--parent <id>|--root] [--position n]"); err != nil {
		return err
	}
	visited := visitedSet(visitedFlags(fs))
	if visited["parent"] && *root {
		return usagef("--parent 与 --root 不能同时使用")
	}
	now, err := nowFrom(env)
	if err != nil {
		return err
	}

	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	id := pos[0]

	newParent := *parent
	if !visited["parent"] && !*root {
		t, err := ws.GetTodo(id)
		if err != nil {
			return err
		}
		newParent = t.Parent
	} else if *root {
		newParent = ""
	}

	if err := ws.MoveTodo(id, newParent, *position); err != nil {
		return err
	}
	upsertTodoIndex(ws, id, stderr)

	if common.json {
		t, err := ws.GetTodo(id)
		if err != nil {
			return err
		}
		return writeJSON(stdout, todoJSONFromStore(ws, t, now, false, nil))
	}
	fmt.Fprintf(stdout, "已移动 %s\n", id)
	return nil
}

// todoRemove 实现 todo rm。
func todoRemove(ctx context.Context, args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("todo rm")
	addCommonFlags(fs, common)
	recursive := fs.Bool("recursive", false, "递归删除子待办")
	fs.BoolVar(recursive, "r", false, "递归删除子待办")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(pos, 1, "用法: kuibu todo rm <id> [-r|--recursive]"); err != nil {
		return err
	}
	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	if err := ws.DeleteTodo(pos[0], *recursive); err != nil {
		return err
	}
	removeTodoIndex(ctx, ws, pos[0], stderr)

	if common.json {
		return writeJSON(stdout, map[string]any{"deleted": pos[0], "recursive": *recursive})
	}
	fmt.Fprintf(stdout, "已删除 %s\n", pos[0])
	return nil
}

// todoTreeNode 是待办树节点。
type todoTreeNode struct {
	meta     index.TodoMeta
	children []*todoTreeNode
	matched  bool
	ancestor bool
}

// buildTodoTree 把全部待办与命中待办构造成树；命中项的祖先即使未命中也显示。
func buildTodoTree(all, matched []index.TodoMeta) []*todoTreeNode {
	byID := map[string]index.TodoMeta{}
	for _, m := range all {
		byID[m.ID] = m
	}
	matchedSet := map[string]bool{}
	for _, m := range matched {
		matchedSet[m.ID] = true
	}

	include := map[string]bool{}
	var addAncestors func(id string)
	addAncestors = func(id string) {
		for id != "" && !include[id] {
			parent, ok := byID[id]
			if !ok {
				// 孤儿：parent 指向不存在的待办，不把不存在的父 ID 纳入树。
				return
			}
			include[id] = true
			id = parent.Parent
		}
	}
	for id := range matchedSet {
		addAncestors(id)
	}

	nodes := map[string]*todoTreeNode{}
	for id := range include {
		nodes[id] = &todoTreeNode{meta: byID[id], matched: matchedSet[id]}
	}
	var roots []*todoTreeNode
	for _, n := range nodes {
		if p := n.meta.Parent; p != "" && nodes[p] != nil {
			nodes[p].children = append(nodes[p].children, n)
		}
	}

	// 根节点按 index.ListTodos 的默认顺序（all 已排序），子任务在父下按 order、id 排序。
	for _, m := range all {
		if n := nodes[m.ID]; n != nil {
			if p := n.meta.Parent; p == "" || nodes[p] == nil {
				roots = append(roots, n)
			}
		}
	}
	for _, n := range nodes {
		sortTodoChildren(n.children)
	}
	for _, r := range roots {
		markTodoAncestor(r)
	}
	return roots
}

// sortTodoChildren 按 order、id 排序子任务。
func sortTodoChildren(nodes []*todoTreeNode) {
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].meta.Order != nodes[j].meta.Order {
			return nodes[i].meta.Order < nodes[j].meta.Order
		}
		return nodes[i].meta.ID < nodes[j].meta.ID
	})
}

// markTodoAncestor 标记未命中但有命中后代的节点，并返回子树是否含命中项。
func markTodoAncestor(n *todoTreeNode) bool {
	has := n.matched
	for _, c := range n.children {
		if markTodoAncestor(c) {
			has = true
		}
	}
	n.ancestor = !n.matched && has
	return has
}

// todoTreeJSON 递归转换树节点。
func todoTreeJSON(n *todoTreeNode, now time.Time) todoJSON {
	kids := make([]todoJSON, 0, len(n.children))
	for _, c := range n.children {
		kids = append(kids, todoTreeJSON(c, now))
	}
	return todoJSONFromMeta(n.meta, now, &kids)
}

// printTodoTree 缩进打印待办树。
func printTodoTree(b *strings.Builder, nodes []*todoTreeNode, depth int, now time.Time, childrenStats map[string]struct{ total, done int }) {
	indent := strings.Repeat("  ", depth)
	for _, n := range nodes {
		line := indent + formatTodoLine(n.meta, now, childrenStats[n.meta.ID])
		if n.ancestor {
			line += " （上级）"
		}
		b.WriteString(line + "\n")
		printTodoTree(b, n.children, depth+1, now, childrenStats)
	}
}

// todoChildrenStats 统计每个父待办下的子任务总数与已完成数。
func todoChildrenStats(all []index.TodoMeta) map[string]struct{ total, done int } {
	out := map[string]struct{ total, done int }{}
	for _, m := range all {
		if m.Parent == "" {
			continue
		}
		st := out[m.Parent]
		st.total++
		if m.Status == store.StatusDone {
			st.done++
		}
		out[m.Parent] = st
	}
	return out
}

// statusMark 返回状态符。
func statusMark(s string) string {
	switch s {
	case store.StatusDoing:
		return "[~]"
	case store.StatusDone:
		return "[x]"
	case store.StatusCanceled:
		return "[-]"
	default:
		return "[ ]"
	}
}

// statusLabel 返回状态中文名。
func statusLabel(s string) string {
	switch s {
	case store.StatusDoing:
		return "doing（进行中）"
	case store.StatusDone:
		return "done（已完成）"
	case store.StatusCanceled:
		return "canceled（已取消）"
	default:
		return "todo（未开始）"
	}
}

// formatTodoLine 格式化一行待办（人类输出）。
func formatTodoLine(m index.TodoMeta, now time.Time, children struct{ total, done int }) string {
	line := statusMark(m.Status) + " " + m.ID + "  " + m.Title
	if m.Due != "" {
		line += "  " + formatDueHuman(m.Due)
		if (m.Status == store.StatusTodo || m.Status == store.StatusDoing) && m.DueAt != nil && !m.DueAt.IsZero() && m.DueAt.Before(now) {
			line += " 已逾期"
		}
	}
	if len(m.Tags) > 0 {
		line += "  #" + strings.Join(m.Tags, " #")
	}
	if children.total > 0 {
		line += fmt.Sprintf("  子任务 %d/%d", children.done, children.total)
	}
	return line
}

// formatDueHuman 把截止日期格式化为「MM-DD 周X [HH:MM]」。
func formatDueHuman(due string) string {
	d, err := store.ParseDue(due)
	if err != nil || d == nil {
		return due
	}
	t, err := time.ParseInLocation("2006-01-02", d.Date, store.TodoLocation())
	if err != nil {
		return due
	}
	weeks := [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}
	out := d.Date[5:7] + "-" + d.Date[8:10] + " " + weeks[t.Weekday()]
	if d.Time != "" {
		out += " " + d.Time
	}
	return out
}

// todoJSONFromStore 把 store.Todo 转为 JSON 结构。
func todoJSONFromStore(ws *store.Workspace, t *store.Todo, now time.Time, withBody bool, children *[]todoJSON) todoJSON {
	tags := t.Tags
	if tags == nil {
		tags = []string{}
	}
	links := t.Links
	if links == nil {
		links = []store.TodoLink{}
	}
	atts := t.Attachments
	if atts == nil {
		atts = []string{}
	}

	out := todoJSON{
		ID:          t.ID,
		Title:       t.Title,
		Status:      t.Status,
		Due:         t.Due,
		Created:     t.Created,
		Updated:     t.Updated,
		Tags:        tags,
		Parent:      t.Parent,
		Order:       t.Order,
		Links:       links,
		Attachments: atts,
		Source:      t.Source,
		Path:        filepath.Join(ws.TodosDir(), t.ID+".md"),
		Children:    children,
	}
	if t.Due != nil {
		if at := t.Due.At(store.TodoLocation()); !at.IsZero() {
			out.DueAt = &at
		}
	}
	if t.DoneAt != nil && !t.DoneAt.IsZero() {
		out.DoneAt = t.DoneAt
	}
	out.Overdue = (t.Status == store.StatusTodo || t.Status == store.StatusDoing) && out.DueAt != nil && out.DueAt.Before(now)
	if withBody {
		body := t.Body
		out.Body = &body
	}
	return out
}

// todoJSONFromMeta 把索引元数据转为 JSON 结构。
func todoJSONFromMeta(m index.TodoMeta, now time.Time, children *[]todoJSON) todoJSON {
	tags := m.Tags
	if tags == nil {
		tags = []string{}
	}
	links := m.Links
	if links == nil {
		links = []store.TodoLink{}
	}
	atts := m.Attachments
	if atts == nil {
		atts = []string{}
	}

	var due *store.Due
	if m.Due != "" {
		if d, err := store.ParseDue(m.Due); err == nil {
			due = d
		}
	}

	out := todoJSON{
		ID:          m.ID,
		Title:       m.Title,
		Status:      m.Status,
		Due:         due,
		DueAt:       m.DueAt,
		DoneAt:      m.DoneAt,
		Created:     m.Created,
		Updated:     m.Updated,
		Tags:        tags,
		Parent:      m.Parent,
		Order:       m.Order,
		Links:       links,
		Attachments: atts,
		Source:      m.Source,
		Path:        m.Path,
		Children:    children,
	}
	out.Overdue = (m.Status == store.StatusTodo || m.Status == store.StatusDoing) && m.DueAt != nil && m.DueAt.Before(now)
	return out
}

// upsertTodoIndex 增量更新单条待办索引，失败只警告。
func upsertTodoIndex(ws *store.Workspace, id string, stderr io.Writer) {
	idx := openIndex(ws, stderr)
	if idx == nil {
		return
	}
	defer idx.Close()
	path := filepath.Join(ws.TodosDir(), id+".md")
	if _, err := idx.UpsertFile(path); err != nil {
		fmt.Fprintf(stderr, "kuibu: 警告: 更新索引失败（不影响数据）: %v\n", err)
	}
}

// removeTodoIndex 从索引删除待办；递归删除时清理子待办，失败只警告。
func removeTodoIndex(ctx context.Context, ws *store.Workspace, id string, stderr io.Writer) {
	idx := openIndex(ws, stderr)
	if idx == nil {
		return
	}
	defer idx.Close()
	path := filepath.Join(ws.TodosDir(), id+".md")
	if _, err := idx.RemoveFile(path); err != nil {
		fmt.Fprintf(stderr, "kuibu: 警告: 更新索引失败（不影响数据）: %v\n", err)
	}
	if _, err := idx.Sync(ctx); err != nil {
		fmt.Fprintf(stderr, "kuibu: 警告: 同步索引失败（不影响数据）: %v\n", err)
	}
}
