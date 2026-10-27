package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/axu4/kuibu/internal/index"
	"github.com/axu4/kuibu/internal/store"
)

// noteJSON 是笔记的 JSON 输出结构。
type noteJSON struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	Type    string    `json:"type"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
	Tags    []string  `json:"tags"`
	Parent  string    `json:"parent"`
	Order   int       `json:"order"`
	Body    *string   `json:"body,omitempty"`
}

// newNoteJSON 把 store.Note 转为 JSON 结构；withBody 决定是否包含正文。
func newNoteJSON(n *store.Note, withBody bool) noteJSON {
	tags := n.Tags
	if tags == nil {
		tags = []string{}
	}
	out := noteJSON{
		ID:      n.ID,
		Title:   n.Title,
		Type:    n.Type,
		Created: n.Created,
		Updated: n.Updated,
		Tags:    tags,
		Parent:  n.Parent,
		Order:   n.Order,
	}
	if withBody {
		body := n.Body
		out.Body = &body
	}
	return out
}

// runNote 分发 note 子命令。
func runNote(ctx context.Context, args []string, common *commonFlags, stdin io.Reader, stdout, stderr io.Writer, env func(string) string) error {
	if len(args) == 0 {
		return usagef("note 需要子命令: new/list/show/edit/mv/rm/extract-todos/migrate-folders")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "new":
		return noteNew(rest, common, stdin, stdout, stderr, env)
	case "list":
		return noteList(rest, common, stdout, stderr, env)
	case "show":
		return noteShow(rest, common, stdout, stderr, env)
	case "edit":
		return noteEdit(rest, common, stdin, stdout, stderr, env)
	case "mv":
		return noteMove(rest, common, stdout, stderr, env)
	case "rm":
		return noteRemove(ctx, rest, common, stdout, stderr, env)
	case "extract-todos":
		return noteExtractTodos(rest, common, stdout, stderr, env)
	case "migrate-folders":
		return noteMigrateFolders(rest, common, stdout, stderr, env)
	default:
		return usagef("未知 note 子命令 %q", sub)
	}
}

// noteMigrateFolders 实现 note migrate-folders。
//
// 老工作台里父子关系只靠 parent 推导，任何笔记都能有子节点；引入目录模型后
// 这些节点必须是目录。命令调用 store.MigrateFolders 就地补上 type=folder，
// 可重复执行（没有需要迁移的节点时输出 0）。
func noteMigrateFolders(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("note migrate-folders")
	addCommonFlags(fs, common)
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("note migrate-folders 不接受位置参数: %s", strings.Join(pos, " "))
	}

	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	converted, err := ws.MigrateFolders()
	if err != nil {
		return err
	}

	if common.json {
		return writeJSON(stdout, map[string]int{"converted": converted})
	}
	fmt.Fprintf(stdout, "已把 %d 条笔记转为目录\n", converted)
	return nil
}

// stringList 是可重复出现的字符串参数。
type stringList []string

// String 实现 flag.Value。
func (s *stringList) String() string { return strings.Join(*s, ",") }

// Set 追加一个值。
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// bodySource 是正文来源参数集合。
type bodySource struct {
	body  string
	file  string
	stdin bool
	// set 表示是否显式给了正文来源。
	set bool
}

// register 注册正文来源参数。
func (b *bodySource) register(fs interface {
	StringVar(*string, string, string, string)
	BoolVar(*bool, string, bool, string)
}) {
	fs.StringVar(&b.body, "body", "", "正文文本")
	fs.StringVar(&b.file, "file", "", "从文件读取正文")
	fs.BoolVar(&b.stdin, "stdin", false, "从标准输入读取正文")
}

// markSet 记录本次解析是否出现了任一正文来源参数。
func (b *bodySource) markSet(fs visitedFlagSet) {
	if fs.visited("body") || fs.visited("file") || fs.visited("stdin") {
		b.set = true
	}
}

// visitedFlagSet 是判断参数是否出现的最小接口。
type visitedFlagSet interface {
	visited(name string) bool
}

// visitedSet 基于 flag.FlagSet 的已访问集合。
type visitedSet map[string]bool

// visited 判断参数是否出现。
func (v visitedSet) visited(name string) bool { return v[name] }

// read 读取正文内容。
func (b *bodySource) read(stdin io.Reader) (string, error) {
	switch {
	case b.file != "":
		data, err := os.ReadFile(b.file)
		if err != nil {
			return "", fmt.Errorf("读取正文文件失败: %w", err)
		}
		return string(data), nil
	case b.stdin:
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("读取标准输入失败: %w", err)
		}
		return string(data), nil
	default:
		return b.body, nil
	}
}

// noteNew 实现 note new。
func noteNew(args []string, common *commonFlags, stdin io.Reader, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("note new")
	addCommonFlags(fs, common)
	title := fs.String("title", "", "标题，建目录时可省略")
	parent := fs.String("parent", "", "父节点 ID，只能是目录")
	typ := fs.String("type", "note", "笔记类型（folder 表示目录）")
	// --folder 是 --type folder 的简写，-d 是它的短选项。
	folder := fs.Bool("folder", false, "新建目录，等价于 --type folder")
	fs.BoolVar(folder, "d", false, "新建目录（--folder 的简写）")
	var tags stringList
	fs.Var(&tags, "tag", "标签，可重复")
	var src bodySource
	src.register(fs)
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("note new 不接受位置参数: %s", strings.Join(pos, " "))
	}
	visited := visitedSet(visitedFlags(fs))
	src.markSet(visited)
	noteType, err := noteNewType(*typ, *folder, visited["type"])
	if err != nil {
		return err
	}
	isFolder := noteType == store.FolderType
	// 建目录不用正文，所以只有普通笔记才要求标题；目录标题为空时由 store 补默认目录名。
	if strings.TrimSpace(*title) == "" && !isFolder {
		return usagef("note new 需要 --title")
	}
	// 目录没有正文：建目录时忽略 --body/--file/--stdin（也不读 stdin，避免卡住）。
	body := ""
	if !isFolder {
		if body, err = src.read(stdin); err != nil {
			return err
		}
	}

	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	note, err := ws.Create(store.NewNote{
		Title:  *title,
		Body:   body,
		Parent: *parent,
		Tags:   tags,
		Type:   noteType,
	})
	if err != nil {
		return err
	}
	upsertIndex(ws, note.ID, stderr)

	if common.json {
		return writeJSON(stdout, newNoteJSON(note, true))
	}
	fmt.Fprintln(stdout, note.ID)
	return nil
}

// noteNewType 解析 note new 最终写入的笔记类型。
//
// --folder 与 --type 都能指定类型：--folder 等价于 --type folder，两者同时给且
// 都指向目录时也照常创建；--folder 与非 folder 的 --type 一起给属于参数冲突，
// 返回 USAGE。只给 --type 时按原样使用，空值退回默认类型 note。
func noteNewType(typ string, folder, typeSet bool) (string, error) {
	typ = strings.TrimSpace(typ)
	if folder {
		if !typeSet || typ == store.FolderType {
			return store.FolderType, nil
		}
		return "", usagef("--folder 与 --type %s 冲突：目录的类型是 %s", typ, store.FolderType)
	}
	if typ == "" {
		return "note", nil
	}
	return typ, nil
}

// noteList 实现 note list。
func noteList(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("note list")
	addCommonFlags(fs, common)
	parent := fs.String("parent", "", "只列出该父节点下的笔记")
	var tag string
	fs.StringVar(&tag, "tag", "", "只列出带该标签的笔记")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("note list 不接受位置参数: %s", strings.Join(pos, " "))
	}
	filterParent := visitedSet(visitedFlags(fs))["parent"]

	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	metas, err := ws.List()
	if err != nil {
		return err
	}

	notes := make([]*store.Note, 0, len(metas))
	for _, n := range metas {
		if filterParent && n.Parent != *parent {
			continue
		}
		if tag != "" && !hasTag(n.Tags, tag) {
			continue
		}
		notes = append(notes, n)
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].ID < notes[j].ID })

	if common.json {
		out := make([]noteJSON, len(notes))
		for i, n := range notes {
			out[i] = newNoteJSON(n, false)
		}
		return writeJSON(stdout, out)
	}
	if len(notes) == 0 {
		fmt.Fprintln(stdout, "（没有笔记）")
		return nil
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\t类型\t标题\t更新时间")
	for _, n := range notes {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", n.ID, noteTypeLabel(n), n.Title, formatLocal(n.Updated))
	}
	return tw.Flush()
}

// noteTypeLabel 返回人类可读的笔记类型：目录显示「目录」，其余原样显示 type。
func noteTypeLabel(n *store.Note) string {
	if n.IsFolder() {
		return "目录"
	}
	if n.Type == "" {
		return "note"
	}
	return n.Type
}

// noteShow 实现 note show。
func noteShow(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("note show")
	addCommonFlags(fs, common)
	metaOnly := fs.Bool("meta-only", false, "只输出元数据")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(pos, 1, "用法: kuibu note show <id> [--meta-only]"); err != nil {
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

	if common.json {
		return writeJSON(stdout, newNoteJSON(note, !*metaOnly))
	}
	fmt.Fprintf(stdout, "# %s  (%s)\n", note.Title, note.ID)
	if *metaOnly {
		fmt.Fprintf(stdout, "类型: %s\n标签: %s\n更新时间: %s\n",
			note.Type, strings.Join(note.Tags, ", "), formatLocal(note.Updated))
		return nil
	}
	fmt.Fprintf(stdout, "类型: %s  标签: %s  更新时间: %s\n\n",
		note.Type, strings.Join(note.Tags, ", "), formatLocal(note.Updated))
	fmt.Fprint(stdout, note.Body)
	if !strings.HasSuffix(note.Body, "\n") {
		fmt.Fprintln(stdout)
	}
	return nil
}

// noteEdit 实现 note edit。
func noteEdit(args []string, common *commonFlags, stdin io.Reader, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("note edit")
	addCommonFlags(fs, common)
	title := fs.String("title", "", "新标题")
	var tags stringList
	fs.Var(&tags, "tag", "替换标签，可重复")
	var src bodySource
	src.register(fs)
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(pos, 1, "用法: kuibu note edit <id> [--title t] [--tag t ...] [--body/--file/--stdin]"); err != nil {
		return err
	}
	visited := visitedSet(visitedFlags(fs))
	src.markSet(visited)
	if !visited["title"] && !visited["tag"] && !src.set {
		return usagef("note edit 至少需要一个修改项（--title/--tag/--body/--file/--stdin）")
	}
	body, err := src.read(stdin)
	if err != nil {
		return err
	}

	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	note, err := ws.Update(pos[0], func(n *store.Note) {
		if visited["title"] {
			n.Title = *title
		}
		if visited["tag"] {
			n.Tags = append([]string(nil), tags...)
		}
		if src.set {
			n.Body = body
		}
	})
	if err != nil {
		return err
	}
	upsertIndex(ws, note.ID, stderr)

	if common.json {
		return writeJSON(stdout, newNoteJSON(note, true))
	}
	fmt.Fprintf(stdout, "已更新 %s\n", note.ID)
	return nil
}

// noteMove 实现 note mv。
func noteMove(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("note mv")
	addCommonFlags(fs, common)
	parent := fs.String("parent", "", "新父笔记 ID")
	root := fs.Bool("root", false, "移到根层级")
	pos := fs.Int("pos", -1, "在目标层级中的位置，缺省为末尾")
	posArgs, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(posArgs, 1, "用法: kuibu note mv <id> [--parent <id>|--root] [--pos n]"); err != nil {
		return err
	}
	visited := visitedSet(visitedFlags(fs))
	if visited["parent"] && *root {
		return usagef("--parent 与 --root 不能同时使用")
	}

	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	id := posArgs[0]

	newParent := *parent
	if *root || !visited["parent"] {
		if *root {
			newParent = ""
		} else {
			// 未给 --parent/--root 时保持原父节点。
			note, err := ws.Get(id)
			if err != nil {
				return err
			}
			newParent = note.Parent
		}
	}
	if err := ws.Move(id, newParent, *pos); err != nil {
		return err
	}
	upsertIndex(ws, id, stderr)

	if common.json {
		note, err := ws.Get(id)
		if err != nil {
			return err
		}
		return writeJSON(stdout, newNoteJSON(note, false))
	}
	fmt.Fprintf(stdout, "已移动 %s\n", id)
	return nil
}

// noteRemove 实现 note rm。
func noteRemove(ctx context.Context, args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("note rm")
	addCommonFlags(fs, common)
	recursive := fs.Bool("recursive", false, "递归删除子笔记")
	fs.BoolVar(recursive, "r", false, "递归删除子笔记")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(pos, 1, "用法: kuibu note rm <id> [-r|--recursive]"); err != nil {
		return err
	}

	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	id := pos[0]
	if err := ws.Delete(id, *recursive); err != nil {
		return err
	}
	removeIndex(ctx, ws, id, stderr)

	if common.json {
		return writeJSON(stdout, map[string]any{"deleted": id, "recursive": *recursive})
	}
	fmt.Fprintf(stdout, "已删除 %s\n", id)
	return nil
}

// hasTag 判断标签列表是否包含指定标签。
func hasTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}

// formatLocal 把时间格式化为本地可读形式。
func formatLocal(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

// openIndex 打开工作台索引；失败时返回 nil 并在 stderr 给出警告。
func openIndex(ws *store.Workspace, stderr io.Writer) *index.Index {
	idx, err := index.Open(ws)
	if err != nil {
		fmt.Fprintf(stderr, "kuibu: 警告: 打开索引失败（不影响数据）: %v\n", err)
		return nil
	}
	return idx
}

// upsertIndex 增量更新单条笔记索引，失败只警告。
func upsertIndex(ws *store.Workspace, id string, stderr io.Writer) {
	idx := openIndex(ws, stderr)
	if idx == nil {
		return
	}
	defer idx.Close()
	path := filepath.Join(ws.NotesDir(), id+".md")
	if _, err := idx.UpsertFile(path); err != nil {
		fmt.Fprintf(stderr, "kuibu: 警告: 更新索引失败（不影响数据）: %v\n", err)
	}
}

// removeIndex 从索引删除笔记；递归删除时一并清理已删除的子笔记，失败只警告。
func removeIndex(ctx context.Context, ws *store.Workspace, id string, stderr io.Writer) {
	idx := openIndex(ws, stderr)
	if idx == nil {
		return
	}
	defer idx.Close()
	path := filepath.Join(ws.NotesDir(), id+".md")
	if _, err := idx.RemoveFile(path); err != nil {
		fmt.Fprintf(stderr, "kuibu: 警告: 更新索引失败（不影响数据）: %v\n", err)
	}
	// Sync 会清理已删除文件对应的索引项（递归删除的子笔记）。
	if _, err := idx.Sync(ctx); err != nil {
		fmt.Fprintf(stderr, "kuibu: 警告: 同步索引失败（不影响数据）: %v\n", err)
	}
}

// syncIndex 同步索引，失败只警告并返回 nil。
func syncIndex(ctx context.Context, ws *store.Workspace, stderr io.Writer) *index.Index {
	idx := openIndex(ws, stderr)
	if idx == nil {
		return nil
	}
	if _, err := idx.Sync(ctx); err != nil {
		fmt.Fprintf(stderr, "kuibu: 警告: 同步索引失败（不影响数据）: %v\n", err)
	}
	return idx
}
