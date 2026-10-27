package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// 待办状态取值。缺失或非法值读入时统一按 StatusTodo 处理。
const (
	// StatusTodo 表示待办未开始。
	StatusTodo = "todo"
	// StatusDoing 表示待办进行中。
	StatusDoing = "doing"
	// StatusDone 表示待办已完成。
	StatusDone = "done"
	// StatusCanceled 表示待办已取消。
	StatusCanceled = "canceled"
)

// ValidTodoStatus 判断待办状态是否合法。
func ValidTodoStatus(s string) bool {
	switch s {
	case StatusTodo, StatusDoing, StatusDone, StatusCanceled:
		return true
	}
	return false
}

// TodoLocation 返回待办日期语义使用的时区 Asia/Shanghai。
//
// 系统缺少时区数据时回退为固定偏移 CST（UTC+8）。
func TodoLocation() *time.Location {
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	return time.FixedZone("CST", 8*3600)
}

// Due 是待办截止日期（可选，日期必填，时间可空）。
type Due struct {
	// Date 是截止日期，格式 YYYY-MM-DD。
	Date string
	// Time 是截止时间，格式 HH:MM，可为空。
	Time string
}

// String 返回 Due 在 front matter 中的写法：只有日期写 "2026-10-30"，
// 有时间写 "2026-10-30 15:00"。nil 或空日期返回空字符串。
func (d *Due) String() string {
	if d == nil || d.Date == "" {
		return ""
	}
	if d.Time == "" {
		return d.Date
	}
	return d.Date + " " + d.Time
}

// MarshalJSON 把 Due 序列化为 JSON 字符串：只有日期写 "2026-10-30"，
// 有时间写 "2026-10-30 15:00"；nil 或空日期输出 null。
func (d *Due) MarshalJSON() ([]byte, error) {
	if d == nil || d.Date == "" {
		return []byte("null"), nil
	}
	return json.Marshal(d.String())
}

// UnmarshalJSON 从 JSON 字符串反序列化 Due，兼容 null 与空字符串。
func (d *Due) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	if s == "null" {
		*d = Due{}
		return nil
	}
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	parsed, err := ParseDue(str)
	if err != nil {
		return err
	}
	if parsed == nil {
		*d = Due{}
		return nil
	}
	*d = *parsed
	return nil
}

// ParseDue 解析截止日期字符串。
//
// 接受 YYYY-MM-DD、YYYY-MM-DD HH:MM、YYYY-MM-DDTHH:MM 与 RFC3339；RFC3339
// 会先转换到 Asia/Shanghai 再取日期与时间。空字符串返回 (nil, nil)。
func ParseDue(s string) (*Due, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}

	if t, err := time.Parse("2006-01-02", s); err == nil {
		return &Due{Date: t.Format("2006-01-02")}, nil
	}
	if t, err := time.Parse("2006-01-02 15:04", s); err == nil {
		return &Due{Date: t.Format("2006-01-02"), Time: t.Format("15:04")}, nil
	}
	if t, err := time.Parse("2006-01-02T15:04", s); err == nil {
		return &Due{Date: t.Format("2006-01-02"), Time: t.Format("15:04")}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		local := t.In(TodoLocation())
		return &Due{Date: local.Format("2006-01-02"), Time: local.Format("15:04")}, nil
	}
	return nil, fmt.Errorf("%w: 无法解析截止日期 %q", ErrInvalid, s)
}

// At 返回截止时刻。没有时间时取当天 23:59:59 作为截止时刻；loc 为 nil 时
// 使用 TodoLocation。日期或时间非法时返回零值。
func (d *Due) At(loc *time.Location) time.Time {
	if d == nil || d.Date == "" {
		return time.Time{}
	}
	if loc == nil {
		loc = TodoLocation()
	}
	date, err := time.ParseInLocation("2006-01-02", d.Date, loc)
	if err != nil {
		return time.Time{}
	}
	hour, minute, second := 23, 59, 59
	if d.Time != "" {
		t, err := time.Parse("15:04", d.Time)
		if err != nil {
			return time.Time{}
		}
		hour, minute, second = t.Hour(), t.Minute(), 0
	}
	return time.Date(date.Year(), date.Month(), date.Day(), hour, minute, second, 0, loc)
}

// TodoLink 是待办附带的一个链接。
type TodoLink struct {
	// URL 是链接地址，含 scheme（http/https/mailto 等）。
	URL string `json:"url"`
	// Title 是链接标题，可为空。
	Title string `json:"title"`
}

// Todo 是一条待办，包含 front matter 元数据与详情正文。
type Todo struct {
	// ID 是待办标识，同时也是文件名（不含 .md）。
	ID string
	// Title 是待办标题。
	Title string
	// Status 是待办状态，取值见 StatusTodo 等常量。
	Status string
	// Due 是截止日期，可空。
	Due *Due
	// Created 是创建时间，带时区。
	Created time.Time
	// Updated 是最后更新时间，带时区。
	Updated time.Time
	// DoneAt 是完成时间，done 时设置，其他状态为 nil。
	DoneAt *time.Time
	// Tags 是标签列表。
	Tags []string
	// Parent 是父待办 ID，空字符串表示顶层。
	Parent string
	// Order 是同级排序序号。
	Order int
	// Body 是详情 Markdown，原样保存。
	Body string
	// Links 是待办附带的链接列表。
	Links []TodoLink
	// Attachments 是相对待办文件的资源路径列表，形如 ../assets/<文件名>。
	Attachments []string
	// Source 是来源笔记 ID，可为空。
	Source string
	// Extra 保存 front matter 中未识别的字段。
	Extra map[string]any

	// extraNodes 保存未识别字段的原始 YAML 节点，用于无损写回。
	extraNodes map[string]*yaml.Node
}

// NewTodo 是创建待办时传入的字段。
type NewTodo struct {
	// Title 是标题，去空白后不能为空。
	Title string
	// Body 是详情正文。
	Body string
	// Parent 是父待办 ID，空表示顶层。
	Parent string
	// Tags 是标签列表。
	Tags []string
	// Due 是截止日期，可空。
	Due *Due
	// Status 是初始状态，空则使用 StatusTodo。
	Status string
	// Links 是链接列表。
	Links []TodoLink
	// Source 是来源笔记 ID，可空。
	Source string
	// Now 是创建时间，零值则使用 time.Now()。
	Now time.Time
}

// knownTodoFrontMatterKeys 是待办认识的标准 front matter 字段。
var knownTodoFrontMatterKeys = map[string]bool{
	"id":          true,
	"title":       true,
	"type":        true,
	"status":      true,
	"due":         true,
	"created":     true,
	"updated":     true,
	"done_at":     true,
	"tags":        true,
	"parent":      true,
	"order":       true,
	"links":       true,
	"attachments": true,
	"source":      true,
}

// todoPath 返回待办文件的绝对路径。
func (w *Workspace) todoPath(id string) string {
	return filepath.Join(w.TodosDir(), id+noteExt)
}

// CreateTodo 新建一条待办。order 自动放在同级末尾；父待办不存在时返回
// ErrNotFound；标题去空白后为空或状态非法时返回 ErrInvalid。
func (w *Workspace) CreateTodo(in NewTodo) (*Todo, error) {
	if strings.TrimSpace(in.Title) == "" {
		return nil, fmt.Errorf("%w: 待办标题不能为空", ErrInvalid)
	}
	if in.Parent != "" {
		if _, err := w.GetTodo(in.Parent); err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, newTodoError(ErrNotFound, "父待办不存在: %s", in.Parent)
			}
			return nil, err
		}
	}

	metas, err := w.listTodoMetas()
	if err != nil {
		return nil, err
	}
	order := 0
	for _, m := range metas {
		if m.Parent == in.Parent && m.Order >= order {
			order = m.Order + 1
		}
	}

	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	now = now.Truncate(time.Second)

	status := in.Status
	if status == "" {
		status = StatusTodo
	}
	if !ValidTodoStatus(status) {
		return nil, fmt.Errorf("%w: 非法待办状态 %q", ErrInvalid, status)
	}

	todo := &Todo{
		Title:       in.Title,
		Status:      status,
		Due:         in.Due,
		Created:     now,
		Updated:     now,
		Tags:        append([]string(nil), in.Tags...),
		Parent:      in.Parent,
		Order:       order,
		Body:        in.Body,
		Links:       append([]TodoLink(nil), in.Links...),
		Attachments: []string{},
		Source:      in.Source,
	}
	if status == StatusDone {
		t := now
		todo.DoneAt = &t
	}

	// 生成不与现有文件冲突的 ID（规则与笔记一致）。
	for i := 0; i < 100; i++ {
		id, err := newID(now)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(w.todoPath(id)); errors.Is(err, os.ErrNotExist) {
			todo.ID = id
			break
		}
	}
	if todo.ID == "" {
		return nil, fmt.Errorf("%w: 无法生成唯一待办 ID", ErrInvalid)
	}

	if err := w.writeTodo(todo); err != nil {
		return nil, err
	}
	return todo, nil
}

// GetTodo 按 ID 读取一条待办（含正文）。
func (w *Workspace) GetTodo(id string) (*Todo, error) {
	if id == "" || strings.ContainsAny(id, `/\`) {
		return nil, newTodoError(ErrNotFound, "非法待办 ID %q", id)
	}
	return readTodoFile(w.todoPath(id))
}

// ListTodos 返回全部待办（含正文），按 ID 排序。
func (w *Workspace) ListTodos() ([]*Todo, error) {
	paths, err := w.ListWorkspaceTodos()
	if err != nil {
		return nil, err
	}
	todos := make([]*Todo, 0, len(paths))
	for _, p := range paths {
		t, err := readTodoFile(p)
		if err != nil {
			return nil, err
		}
		todos = append(todos, t)
	}
	sort.Slice(todos, func(i, j int) bool { return todos[i].ID < todos[j].ID })
	return todos, nil
}

// ReadTodoFile 读取任意路径的 Markdown 待办文件，供索引使用。
func ReadTodoFile(path string) (*Todo, error) {
	return readTodoFile(path)
}

// UpdateTodo 用 fn 修改待办，自动刷新 updated 并维护 DoneAt。
//
// now 为零值时使用 time.Now()。若 fn 把状态改成 done 且原来不是 done，则把
// DoneAt 设为 now；改成非 done 时清空 DoneAt。状态非法或标题为空返回 ErrInvalid。
func (w *Workspace) UpdateTodo(id string, now time.Time, fn func(*Todo) error) (*Todo, error) {
	todo, err := w.GetTodo(id)
	if err != nil {
		return nil, err
	}
	oldStatus := todo.Status
	if fn != nil {
		if err := fn(todo); err != nil {
			return nil, err
		}
	}
	todo.ID = id

	if now.IsZero() {
		now = time.Now()
	}
	now = now.Truncate(time.Second)
	todo.Updated = now

	if !ValidTodoStatus(todo.Status) {
		return nil, fmt.Errorf("%w: 非法待办状态 %q", ErrInvalid, todo.Status)
	}
	if strings.TrimSpace(todo.Title) == "" {
		return nil, fmt.Errorf("%w: 待办标题不能为空", ErrInvalid)
	}

	if todo.Status == StatusDone && oldStatus != StatusDone {
		t := now
		todo.DoneAt = &t
	}
	if todo.Status != StatusDone {
		todo.DoneAt = nil
	}

	if err := w.writeTodo(todo); err != nil {
		return nil, err
	}
	return todo, nil
}

// SetTodoStatus 更新待办状态。
func (w *Workspace) SetTodoStatus(id, status string, now time.Time) (*Todo, error) {
	return w.UpdateTodo(id, now, func(t *Todo) error {
		t.Status = status
		return nil
	})
}

// MoveTodo 把待办移到 newParent 下的第 position 个位置，语义与笔记 Move 相同。
func (w *Workspace) MoveTodo(id, newParent string, position int) error {
	metas, err := w.listTodoMetas()
	if err != nil {
		return err
	}
	byID := make(map[string]*Todo, len(metas))
	for _, m := range metas {
		byID[m.ID] = m
	}

	moving, ok := byID[id]
	if !ok {
		return newTodoError(ErrNotFound, "待办不存在: %s", id)
	}
	if newParent != "" {
		if _, ok := byID[newParent]; !ok {
			return newTodoError(ErrNotFound, "父待办不存在: %s", newParent)
		}
	}
	if newParent == id || w.todoIsDescendant(byID, newParent, id) {
		return newTodoError(ErrCycle, "移动会导致待办父子关系成环: %s", id)
	}

	oldParent := moving.Parent

	// 同层级重排：只处理一个列表。
	if oldParent == newParent {
		siblings := todoChildrenOf(metas, newParent)
		reordered := make([]*Todo, 0, len(siblings))
		for _, s := range siblings {
			if s.ID == id {
				continue
			}
			reordered = append(reordered, s)
		}
		reordered = insertTodoAt(reordered, moving, position)
		return w.todoRenumber(reordered)
	}

	// 跨层级：旧列表去掉该节点后重新编号，新列表插入后重新编号。
	oldList := make([]*Todo, 0, len(metas))
	for _, s := range todoChildrenOf(metas, oldParent) {
		if s.ID == id {
			continue
		}
		oldList = append(oldList, s)
	}
	newList := todoChildrenOf(metas, newParent)
	newList = insertTodoAt(newList, moving, position)

	// 先落盘 parent 变更，renumber 会重新读取完整待办以保留正文。
	full, err := w.GetTodo(id)
	if err != nil {
		return err
	}
	full.Parent = newParent
	full.Updated = time.Now().Truncate(time.Second)
	if err := w.writeTodo(full); err != nil {
		return err
	}

	if err := w.todoRenumber(oldList); err != nil {
		return err
	}
	return w.todoRenumber(newList)
}

// DeleteTodo 删除一条待办。存在子待办且 recursive 为 false 时返回
// ErrHasChildren；recursive 为 true 时连同全部后代一起删除。附件文件不删除。
func (w *Workspace) DeleteTodo(id string, recursive bool) error {
	metas, err := w.listTodoMetas()
	if err != nil {
		return err
	}
	byID := make(map[string]*Todo, len(metas))
	for _, m := range metas {
		byID[m.ID] = m
	}
	if _, ok := byID[id]; !ok {
		return newTodoError(ErrNotFound, "待办不存在: %s", id)
	}

	children := todoChildrenOf(metas, id)
	if len(children) > 0 && !recursive {
		return newTodoError(ErrHasChildren, "待办仍有子任务: %s（如需连同子任务删除请加 -r）", id)
	}

	targets := []string{id}
	if recursive {
		queue := []string{id}
		seen := map[string]bool{id: true}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for _, c := range todoChildrenOf(metas, cur) {
				if seen[c.ID] {
					continue
				}
				seen[c.ID] = true
				targets = append(targets, c.ID)
				queue = append(queue, c.ID)
			}
		}
	}

	for _, tid := range targets {
		if err := os.Remove(w.todoPath(tid)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("删除待办 %s 失败: %w", tid, err)
		}
	}
	return nil
}

// AddTodoLink 给待办添加链接。URL 必须含 scheme；同 URL 已存在时只更新标题。
func (w *Workspace) AddTodoLink(id string, link TodoLink, now time.Time) (*Todo, error) {
	if !validTodoURL(link.URL) {
		return nil, fmt.Errorf("%w: 非法链接 URL %q", ErrInvalid, link.URL)
	}
	return w.UpdateTodo(id, now, func(t *Todo) error {
		for i := range t.Links {
			if t.Links[i].URL == link.URL {
				t.Links[i].Title = link.Title
				return nil
			}
		}
		t.Links = append(t.Links, link)
		return nil
	})
}

// RemoveTodoLink 按 URL 移除待办链接。
func (w *Workspace) RemoveTodoLink(id, url string, now time.Time) (*Todo, error) {
	return w.UpdateTodo(id, now, func(t *Todo) error {
		out := t.Links[:0]
		for _, l := range t.Links {
			if l.URL != url {
				out = append(out, l)
			}
		}
		t.Links = out
		return nil
	})
}

// AttachTodoFile 把 srcPath 复制进 assets/（去重），并把相对路径加入
// Attachments。返回复制后的相对路径。
func (w *Workspace) AttachTodoFile(id, srcPath string, now time.Time) (*Todo, string, error) {
	rel, err := w.AddAsset(srcPath)
	if err != nil {
		return nil, "", err
	}
	todo, err := w.UpdateTodo(id, now, func(t *Todo) error {
		for _, a := range t.Attachments {
			if a == rel {
				return nil
			}
		}
		t.Attachments = append(t.Attachments, rel)
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return todo, rel, nil
}

// DetachTodoFile 从待办附件列表中移除相对路径，不删除资源文件。
func (w *Workspace) DetachTodoFile(id, rel string, now time.Time) (*Todo, error) {
	return w.UpdateTodo(id, now, func(t *Todo) error {
		out := t.Attachments[:0]
		for _, a := range t.Attachments {
			if a != rel {
				out = append(out, a)
			}
		}
		t.Attachments = out
		return nil
	})
}

// TodoChildren 返回 parent 下的直接子待办，按 order 再按 id 排序。
func (w *Workspace) TodoChildren(id string) ([]*Todo, error) {
	metas, err := w.listTodoMetas()
	if err != nil {
		return nil, err
	}
	return todoChildrenOf(metas, id), nil
}

// readTodoFile 读取并解析待办文件。
func readTodoFile(path string) (*Todo, error) {
	id := IDFromPath(path)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, newTodoError(ErrNotFound, "待办不存在: %s", id)
		}
		return nil, fmt.Errorf("读取待办失败: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("读取待办信息失败: %w", err)
	}

	todo := &Todo{
		ID:      id,
		Status:  StatusTodo,
		Created: info.ModTime(),
		Updated: info.ModTime(),
		Body:    string(data),
		Extra:   map[string]any{},
	}

	front, body, ok := splitFrontMatter(data)
	if ok {
		if parsed := decodeTodoFrontMatter(front); parsed != nil {
			todo.Title = parsed.Title
			todo.Status = parsed.Status
			todo.Due = parsed.Due
			todo.Tags = parsed.Tags
			todo.Parent = parsed.Parent
			todo.Order = parsed.Order
			todo.Links = parsed.Links
			todo.Attachments = parsed.Attachments
			todo.Source = parsed.Source
			todo.DoneAt = parsed.DoneAt
			todo.Extra = parsed.Extra
			todo.extraNodes = parsed.extraNodes
			if !parsed.Created.IsZero() {
				todo.Created = parsed.Created
			}
			if !parsed.Updated.IsZero() {
				todo.Updated = parsed.Updated
			}
			todo.Body = string(body)
		}
	}

	// 状态缺失或非法时按 todo 处理，但不会因此静默改写文件。
	if !ValidTodoStatus(todo.Status) {
		todo.Status = StatusTodo
	}
	if todo.Title == "" {
		todo.Title = deriveTitle(todo.Body, id)
	}
	if todo.Extra == nil {
		todo.Extra = map[string]any{}
	}
	if todo.Tags == nil {
		todo.Tags = []string{}
	}
	if todo.Links == nil {
		todo.Links = []TodoLink{}
	}
	if todo.Attachments == nil {
		todo.Attachments = []string{}
	}
	return todo, nil
}

// decodeTodoFrontMatter 解析待办 front matter。解析失败或不是映射时返回 nil。
func decodeTodoFrontMatter(front []byte) *Todo {
	root, empty, ok := frontMatterMapping(front)
	if !ok {
		if empty {
			return &Todo{}
		}
		return nil
	}

	todo := &Todo{Extra: map[string]any{}, extraNodes: map[string]*yaml.Node{}}
	for i := 0; i+1 < len(root.Content); i += 2 {
		keyNode := root.Content[i]
		valNode := root.Content[i+1]
		key := keyNode.Value
		switch key {
		case "title":
			_ = valNode.Decode(&todo.Title)
		case "status":
			_ = valNode.Decode(&todo.Status)
		case "due":
			var s string
			if valNode.Decode(&s) == nil {
				if d, err := ParseDue(s); err == nil {
					todo.Due = d
				}
			}
		case "created":
			todo.Created = decodeTime(valNode)
		case "updated":
			todo.Updated = decodeTime(valNode)
		case "done_at":
			if t := decodeTime(valNode); !t.IsZero() {
				todo.DoneAt = &t
			}
		case "tags":
			_ = valNode.Decode(&todo.Tags)
		case "parent":
			_ = valNode.Decode(&todo.Parent)
		case "order":
			_ = valNode.Decode(&todo.Order)
		case "links":
			todo.Links = decodeTodoLinks(valNode)
		case "attachments":
			_ = valNode.Decode(&todo.Attachments)
		case "source":
			_ = valNode.Decode(&todo.Source)
		case "id":
			// ID 以文件名为准，忽略 front matter 里的值。
		case "type":
			// 待办类型固定为 todo，忽略 front matter 里的值。
		default:
			var v any
			if err := valNode.Decode(&v); err == nil {
				todo.Extra[key] = v
			}
			todo.extraNodes[key] = valNode
		}
	}
	return todo
}

// decodeTodoLinks 解析 links 字段，兼容对象列表与纯字符串列表。
func decodeTodoLinks(node *yaml.Node) []TodoLink {
	var links []TodoLink
	if node.Kind != yaml.SequenceNode {
		return links
	}
	for _, item := range node.Content {
		// 纯字符串项只有 URL。
		if item.Kind == yaml.ScalarNode {
			var s string
			if item.Decode(&s) == nil && s != "" {
				links = append(links, TodoLink{URL: s})
			}
			continue
		}
		if item.Kind != yaml.MappingNode {
			continue
		}
		var l TodoLink
		for i := 0; i+1 < len(item.Content); i += 2 {
			k := item.Content[i].Value
			v := item.Content[i+1]
			switch k {
			case "url":
				_ = v.Decode(&l.URL)
			case "title":
				_ = v.Decode(&l.Title)
			}
		}
		if l.URL != "" {
			links = append(links, l)
		}
	}
	return links
}

// writeTodo 原子地写回待办文件。
func (w *Workspace) writeTodo(todo *Todo) error {
	data, err := marshalTodo(todo)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(w.todoPath(todo.ID), data); err != nil {
		return fmt.Errorf("写入待办 %s 失败: %w", todo.ID, err)
	}
	return nil
}

// marshalTodo 把待办序列化为 front matter + 正文。
//
// front matter 字段顺序固定为 id, title, type（固定 "todo"）, status, due,
// created, updated, done_at, tags, parent, order, links, attachments, source，
// 之后按 key 字母序写未知字段。due/done_at 无值时写空字符串以保持字段稳定。
func marshalTodo(todo *Todo) ([]byte, error) {
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}

	tags := todo.Tags
	if tags == nil {
		tags = []string{}
	}
	attachments := todo.Attachments
	if attachments == nil {
		attachments = []string{}
	}

	appendFrontMatterKey(root, "id", stringNode(todo.ID))
	appendFrontMatterKey(root, "title", stringNode(todo.Title))
	appendFrontMatterKey(root, "type", stringNode("todo"))
	appendFrontMatterKey(root, "status", stringNode(todo.Status))
	appendFrontMatterKey(root, "due", stringNode(dueString(todo.Due)))
	appendFrontMatterKey(root, "created", stringNode(formatTime(todo.Created)))
	appendFrontMatterKey(root, "updated", stringNode(formatTime(todo.Updated)))
	appendFrontMatterKey(root, "done_at", stringNode(formatDoneAt(todo.DoneAt)))
	if err := appendFrontMatterValue(root, "tags", tags); err != nil {
		return nil, fmt.Errorf("序列化 tags 失败: %w", err)
	}
	appendFrontMatterKey(root, "parent", stringNode(todo.Parent))
	if err := appendFrontMatterValue(root, "order", todo.Order); err != nil {
		return nil, fmt.Errorf("序列化 order 失败: %w", err)
	}
	appendFrontMatterKey(root, "links", encodeTodoLinks(todo.Links))
	if err := appendFrontMatterValue(root, "attachments", attachments); err != nil {
		return nil, fmt.Errorf("序列化 attachments 失败: %w", err)
	}
	appendFrontMatterKey(root, "source", stringNode(todo.Source))

	if err := appendExtraFrontMatter(root, todo.Extra, todo.extraNodes, knownTodoFrontMatterKeys); err != nil {
		return nil, err
	}
	return marshalFrontMatter(root, todo.Body)
}

// dueString 返回 Due 的 front matter 字符串，nil 时返回空字符串。
func dueString(d *Due) string {
	if d == nil {
		return ""
	}
	return d.String()
}

// formatDoneAt 把完成时间格式化为 RFC3339，空值输出空字符串。
func formatDoneAt(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// encodeTodoLinks 把链接列表编码为 YAML 序列节点；title 为空时省略。
func encodeTodoLinks(links []TodoLink) *yaml.Node {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, l := range links {
		m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		m.Content = append(m.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "url"},
			stringNode(l.URL),
		)
		if l.Title != "" {
			m.Content = append(m.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "title"},
				stringNode(l.Title),
			)
		}
		seq.Content = append(seq.Content, m)
	}
	return seq
}

// listTodoMetas 读取全部待办元数据（不含正文）。
func (w *Workspace) listTodoMetas() ([]*Todo, error) {
	paths, err := w.ListWorkspaceTodos()
	if err != nil {
		return nil, err
	}
	metas := make([]*Todo, 0, len(paths))
	for _, p := range paths {
		todo, err := readTodoFile(p)
		if err != nil {
			return nil, err
		}
		todo.Body = ""
		metas = append(metas, todo)
	}
	return metas, nil
}

// todoChildrenOf 返回 parent 下的子待办，按 order 再按 id 排序。
func todoChildrenOf(metas []*Todo, parent string) []*Todo {
	var children []*Todo
	for _, m := range metas {
		if m.Parent == parent {
			children = append(children, m)
		}
	}
	sort.Slice(children, func(i, j int) bool {
		if children[i].Order != children[j].Order {
			return children[i].Order < children[j].Order
		}
		return children[i].ID < children[j].ID
	})
	return children
}

// insertTodoAt 把待办插入 list 的第 position 个位置；position 越界时追加到末尾。
func insertTodoAt(list []*Todo, todo *Todo, position int) []*Todo {
	if position < 0 || position >= len(list) {
		return append(list, todo)
	}
	list = append(list, nil)
	copy(list[position+1:], list[position:])
	list[position] = todo
	return list
}

// todoIsDescendant 判断 candidate 是否是 ancestor 的后代（沿 parent 链向上查找）。
func (w *Workspace) todoIsDescendant(byID map[string]*Todo, candidate, ancestor string) bool {
	seen := map[string]bool{}
	cur := candidate
	for cur != "" {
		if cur == ancestor {
			return true
		}
		if seen[cur] {
			return false
		}
		seen[cur] = true
		n, ok := byID[cur]
		if !ok {
			return false
		}
		cur = n.Parent
	}
	return false
}

// todoRenumber 把 list 里的待办 order 重新编为 0,1,2…，只重写 order 变化的文件。
func (w *Workspace) todoRenumber(list []*Todo) error {
	for i, n := range list {
		if n.Order == i {
			continue
		}
		full, err := w.GetTodo(n.ID)
		if err != nil {
			return err
		}
		full.Order = i
		if err := w.writeTodo(full); err != nil {
			return err
		}
	}
	return nil
}

// validTodoURL 判断链接 URL 是否合法（url.Parse 成功且 scheme 非空）。
func validTodoURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return u.Scheme != ""
}

// SetTodoStatusTree 把待办及其全部后代设为 status（已是该状态的后代不改写），
// 返回更新后的本条与所有被改动的 ID（含本条）。
func (w *Workspace) SetTodoStatusTree(id, status string, now time.Time) (*Todo, []string, error) {
	t, err := w.SetTodoStatus(id, status, now)
	if err != nil {
		return nil, nil, err
	}
	changed := []string{t.ID}
	var walk func(parent string) error
	walk = func(parent string) error {
		children, err := w.TodoChildren(parent)
		if err != nil {
			return err
		}
		for _, c := range children {
			if c.Status != status {
				if _, err := w.SetTodoStatus(c.ID, status, now); err != nil {
					return err
				}
				changed = append(changed, c.ID)
			}
			if err := walk(c.ID); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(id); err != nil {
		return t, changed, err
	}
	return t, changed, nil
}

// OpenDescendantCount 返回未完成（todo / doing）的后代数量。
func (w *Workspace) OpenDescendantCount(id string) (int, error) {
	children, err := w.TodoChildren(id)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, c := range children {
		if c.Status != StatusDone && c.Status != StatusCanceled {
			total++
		}
		n, err := w.OpenDescendantCount(c.ID)
		if err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}

// TodoAttachmentPath 把待办附件的相对路径解析成 assets/ 下的绝对路径。只接受该待办
// 附件列表里的路径，且必须落在工作台 assets/ 目录内，拒绝任何路径穿越。
func (w *Workspace) TodoAttachmentPath(id, rel string) (string, error) {
	todo, err := w.GetTodo(id)
	if err != nil {
		return "", err
	}
	allowed := false
	for _, a := range todo.Attachments {
		if a == rel {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("%w: 附件不在该待办列表中: %s", ErrInvalid, rel)
	}
	name := path.Base(strings.ReplaceAll(rel, `\`, "/"))
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("%w: 非法附件路径 %q", ErrInvalid, rel)
	}
	assetsDir := filepath.Clean(w.AssetsDir())
	full := filepath.Join(assetsDir, name)
	if !strings.HasPrefix(full, assetsDir+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: 附件路径越界 %q", ErrInvalid, rel)
	}
	return full, nil
}
