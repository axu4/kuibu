package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/axu4/kuibu/internal/config"
	"github.com/axu4/kuibu/internal/index"
	"github.com/axu4/kuibu/internal/store"
)

// runInit 实现 init 命令。
func runInit(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("init")
	addCommonFlags(fs, common)
	makeDefault := fs.Bool("default", false, "写入用户配置作为默认工作台")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(pos, 1, "用法: kuibu init <dir> [--default]"); err != nil {
		return err
	}

	ws, err := store.Init(pos[0])
	if err != nil {
		return err
	}

	idx, idxErr := index.Open(ws)
	if idxErr != nil {
		fmt.Fprintf(stderr, "kuibu: 警告: 打开索引失败（不影响数据）: %v\n", idxErr)
	} else {
		defer idx.Close()
		if _, err := idx.Rebuild(context.Background()); err != nil {
			fmt.Fprintf(stderr, "kuibu: 警告: 建立索引失败（不影响数据）: %v\n", err)
		}
	}

	if *makeDefault {
		if err := config.SaveDefaultWorkspace(env, ws.Dir()); err != nil {
			return err
		}
	}

	if common.json {
		return writeJSON(stdout, map[string]any{
			"workspace": ws.Dir(),
			"default":   *makeDefault,
		})
	}
	fmt.Fprintf(stdout, "已初始化工作台: %s\n", ws.Dir())
	if *makeDefault {
		fmt.Fprintln(stdout, "已设为默认工作台")
	}
	return nil
}

// treeJSON 是 tree --json 的嵌套结构。
type treeJSON struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Type 是节点类型，目录为 "folder"，其余为普通笔记类型。
	Type     string     `json:"type"`
	Orphan   bool       `json:"orphan"`
	Cycle    bool       `json:"cycle"`
	Children []treeJSON `json:"children"`
}

// toTreeJSON 递归转换树节点。
func toTreeJSON(n *store.TreeNode) treeJSON {
	typ := n.Note.Type
	if typ == "" {
		typ = "note"
	}
	out := treeJSON{
		ID:       n.Note.ID,
		Title:    n.Note.Title,
		Type:     typ,
		Orphan:   n.Orphan,
		Cycle:    n.Cycle,
		Children: make([]treeJSON, 0, len(n.Children)),
	}
	for _, c := range n.Children {
		out.Children = append(out.Children, toTreeJSON(c))
	}
	return out
}

// runTree 实现 tree 命令。
func runTree(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("tree")
	addCommonFlags(fs, common)
	root := fs.String("root", "", "只输出该笔记及其子树")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("tree 不接受位置参数: %s", strings.Join(pos, " "))
	}

	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	roots, err := ws.Tree()
	if err != nil {
		return err
	}

	if *root != "" {
		found, err := findSubtree(roots, *root)
		if err != nil {
			return err
		}
		roots = []*store.TreeNode{found}
	}

	if common.json {
		out := make([]treeJSON, 0, len(roots))
		for _, r := range roots {
			out = append(out, toTreeJSON(r))
		}
		return writeJSON(stdout, out)
	}
	if len(roots) == 0 {
		fmt.Fprintln(stdout, "（没有笔记）")
		return nil
	}
	var b strings.Builder
	for _, r := range roots {
		printTree(&b, r, 0)
	}
	fmt.Fprint(stdout, b.String())
	return nil
}

// findSubtree 在树中查找指定 ID 的节点。
func findSubtree(nodes []*store.TreeNode, id string) (*store.TreeNode, error) {
	for _, n := range nodes {
		if n.Note.ID == id {
			return n, nil
		}
		if found, err := findSubtree(n.Children, id); err == nil {
			return found, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", store.ErrNotFound, id)
}

// printTree 以缩进大纲输出一个节点及其子树。
func printTree(b *strings.Builder, n *store.TreeNode, depth int) {
	b.WriteString(strings.Repeat("  ", depth))
	b.WriteString("- ")
	b.WriteString(n.Note.Title)
	b.WriteString("  (")
	b.WriteString(n.Note.ID)
	b.WriteString(")")
	if n.Note.IsFolder() {
		b.WriteString(" [目录]")
	}
	if n.Orphan {
		b.WriteString(" [孤儿]")
	}
	if n.Cycle {
		b.WriteString(" [环]")
	}
	b.WriteString("\n")
	for _, c := range n.Children {
		printTree(b, c, depth+1)
	}
}

// runSearch 实现 search 命令。
func runSearch(ctx context.Context, args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("search")
	addCommonFlags(fs, common)
	limit := fs.Int("limit", 20, "最多返回条数")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("用法: kuibu search <query> [--limit n]")
	}

	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	idx := syncIndex(ctx, ws, stderr)
	if idx == nil {
		return fmt.Errorf("索引不可用，无法检索")
	}
	defer idx.Close()

	hits, err := idx.Search(pos[0], *limit)
	if err != nil {
		return err
	}

	if common.json {
		return writeJSON(stdout, hits)
	}
	if len(hits) == 0 {
		fmt.Fprintln(stdout, "（没有匹配结果）")
		return nil
	}
	for _, h := range hits {
		kind := ""
		if h.Type == "report" {
			kind = "[周报] "
		}
		fmt.Fprintf(stdout, "%s%s  %s\n    %s\n", kind, h.ID, h.Title, strings.TrimSpace(h.Snippet))
	}
	return nil
}

// runAsset 分发 asset 子命令。
func runAsset(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	if len(args) == 0 {
		return usagef("asset 需要子命令: add")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "add":
		fs := newFlagSet("asset add")
		addCommonFlags(fs, common)
		pos, err := parseCommand(fs, rest)
		if err != nil {
			return err
		}
		if err := requireArgs(pos, 1, "用法: kuibu asset add <file>"); err != nil {
			return err
		}
		ws, err := resolveWorkspace(common, env)
		if err != nil {
			return err
		}
		rel, err := ws.AddAsset(pos[0])
		if err != nil {
			return err
		}
		if common.json {
			return writeJSON(stdout, map[string]string{"path": rel})
		}
		fmt.Fprintln(stdout, rel)
		return nil
	default:
		return usagef("未知 asset 子命令 %q", sub)
	}
}

// runIndex 分发 index 子命令。
func runIndex(ctx context.Context, args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	if len(args) == 0 {
		return usagef("index 需要子命令: rebuild")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "rebuild":
		fs := newFlagSet("index rebuild")
		addCommonFlags(fs, common)
		pos, err := parseCommand(fs, rest)
		if err != nil {
			return err
		}
		if len(pos) > 0 {
			return usagef("index rebuild 不接受位置参数: %s", strings.Join(pos, " "))
		}
		ws, err := resolveWorkspace(common, env)
		if err != nil {
			return err
		}
		idx, err := index.Open(ws)
		if err != nil {
			return err
		}
		defer idx.Close()
		stats, err := idx.Rebuild(ctx)
		if err != nil {
			return err
		}
		if common.json {
			return writeJSON(stdout, stats)
		}
		fmt.Fprintf(stdout, "索引已重建: 新增 %d，更新 %d，删除 %d，共 %d 条\n",
			stats.Added, stats.Updated, stats.Removed, stats.Total)
		return nil
	default:
		return usagef("未知 index 子命令 %q", sub)
	}
}
