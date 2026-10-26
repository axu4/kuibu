package store

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// noteExt 是笔记文件扩展名。
const noteExt = ".md"

// 默认笔记类型。
const defaultNoteType = "note"

// Note 是一条笔记，包含 front matter 元数据与正文。
//
// 除 Extra 之外的字段都是跬步认识的标准字段；Extra 保存 front matter 中
// 未识别的字段，写入时原样写回，供后续待办、周报等扩展字段以及外部编辑器
// 手加的字段使用。
type Note struct {
	// ID 是笔记标识，同时也是文件名（不含 .md）。
	ID string
	// Title 是笔记标题。
	Title string
	// Type 是笔记类型，默认 "note"，为 todo/weekly 等预留。
	Type string
	// Created 是创建时间，带时区。
	Created time.Time
	// Updated 是最后更新时间，带时区。
	Updated time.Time
	// Tags 是标签列表。
	Tags []string
	// Parent 是父笔记 ID，空字符串表示根节点。
	Parent string
	// Order 是同级排序序号。
	Order int
	// Body 是 front matter 之后的正文，原样保存。
	Body string
	// Extra 保存 front matter 中未识别的字段。
	Extra map[string]any

	// extraNodes 保存未识别字段的原始 YAML 节点，用于无损写回。
	extraNodes map[string]*yaml.Node
}

// NewNote 是创建笔记时传入的字段。
type NewNote struct {
	// Title 是标题。
	Title string
	// Body 是正文。
	Body string
	// Parent 是父笔记 ID，空表示根节点。
	Parent string
	// Tags 是标签列表。
	Tags []string
	// Type 是笔记类型，空则使用默认类型 "note"。
	Type string
}

// knownFrontMatterKeys 是跬步认识的标准 front matter 字段，其余字段进 Extra。
var knownFrontMatterKeys = map[string]bool{
	"id":      true,
	"title":   true,
	"type":    true,
	"created": true,
	"updated": true,
	"tags":    true,
	"parent":  true,
	"order":   true,
}

// newID 生成形如 yyyymmdd-hhmmss-xxxx 的笔记 ID（本地时间 + 4 位小写随机 hex）。
func newID(t time.Time) (string, error) {
	var buf [2]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("生成笔记 ID 随机数失败: %w", err)
	}
	return t.Format("20060102-150405") + "-" + hex.EncodeToString(buf[:]), nil
}

// IDFromPath 从笔记文件路径推导笔记 ID（文件名去掉 .md）。
func IDFromPath(path string) string {
	return strings.TrimSuffix(filepath.Base(path), noteExt)
}

// notePath 返回笔记文件的绝对路径。
func (w *Workspace) notePath(id string) string {
	return filepath.Join(w.NotesDir(), id+noteExt)
}

// Create 新建一条笔记。order 自动放在同级末尾；父笔记不存在时返回 ErrNotFound。
func (w *Workspace) Create(in NewNote) (*Note, error) {
	if in.Parent != "" {
		if _, err := w.Get(in.Parent); err != nil {
			return nil, err
		}
	}

	metas, err := w.listMetas()
	if err != nil {
		return nil, err
	}
	order := 0
	for _, m := range metas {
		if m.Parent == in.Parent && m.Order >= order {
			order = m.Order + 1
		}
	}

	// 时间截断到秒，保证写回 RFC3339 后往返一致。
	now := time.Now().Truncate(time.Second)
	typ := in.Type
	if typ == "" {
		typ = defaultNoteType
	}
	note := &Note{
		Title:   in.Title,
		Type:    typ,
		Created: now,
		Updated: now,
		Tags:    append([]string(nil), in.Tags...),
		Parent:  in.Parent,
		Order:   order,
		Body:    in.Body,
	}

	// 生成不与现有文件冲突的 ID。
	for i := 0; i < 100; i++ {
		id, err := newID(now)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(w.notePath(id)); errors.Is(err, os.ErrNotExist) {
			note.ID = id
			break
		}
	}
	if note.ID == "" {
		return nil, fmt.Errorf("%w: 无法生成唯一笔记 ID", ErrInvalid)
	}

	if err := w.writeNote(note); err != nil {
		return nil, err
	}
	return note, nil
}

// Get 按 ID 读取一条笔记（含正文）。
func (w *Workspace) Get(id string) (*Note, error) {
	if id == "" || strings.ContainsAny(id, `/\`) {
		return nil, fmt.Errorf("%w: 非法笔记 ID %q", ErrNotFound, id)
	}
	return w.readNote(w.notePath(id))
}

// List 返回全部笔记的元数据（不含正文），按 ID 排序。
func (w *Workspace) List() ([]*Note, error) {
	metas, err := w.listMetas()
	if err != nil {
		return nil, err
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].ID < metas[j].ID })
	return metas, nil
}

// ListMeta 与 List 等价，返回全部笔记元数据（不含正文）。
func (w *Workspace) ListMeta() ([]*Note, error) {
	return w.List()
}

// UpdateBody 替换笔记正文，并刷新 updated。
func (w *Workspace) UpdateBody(id, body string) (*Note, error) {
	return w.Update(id, func(n *Note) { n.Body = body })
}

// Update 用 fn 修改笔记元数据或正文，自动刷新 updated 后写回。
// 笔记 ID 不允许通过 fn 修改。
func (w *Workspace) Update(id string, fn func(*Note)) (*Note, error) {
	note, err := w.Get(id)
	if err != nil {
		return nil, err
	}
	if fn != nil {
		fn(note)
	}
	note.ID = id
	note.Updated = time.Now().Truncate(time.Second)
	if note.Type == "" {
		note.Type = defaultNoteType
	}
	if err := w.writeNote(note); err != nil {
		return nil, err
	}
	return note, nil
}

// Move 把笔记移到 newParent 下的第 position 个位置。
//
// newParent 为空表示根层级；position 为负或超出子列表长度时追加到末尾。
// 移动后对受影响的同级列表重新编号 order（0,1,2…），只重写 order 变化的
// 文件。移到自己或自己的后代下面时返回 ErrCycle。
func (w *Workspace) Move(id, newParent string, position int) error {
	metas, err := w.listMetas()
	if err != nil {
		return err
	}
	byID := make(map[string]*Note, len(metas))
	for _, m := range metas {
		byID[m.ID] = m
	}

	moving, ok := byID[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if newParent != "" {
		if _, ok := byID[newParent]; !ok {
			return fmt.Errorf("%w: 父笔记 %s", ErrNotFound, newParent)
		}
	}
	if newParent == id || w.isDescendant(byID, newParent, id) {
		return fmt.Errorf("%w: 不能移动到 %q 下", ErrCycle, newParent)
	}

	oldParent := moving.Parent

	// 同层级重排：只处理一个列表。
	if oldParent == newParent {
		siblings := childrenOf(metas, newParent)
		reordered := make([]*Note, 0, len(siblings))
		for _, s := range siblings {
			if s.ID == id {
				continue
			}
			reordered = append(reordered, s)
		}
		reordered = insertAt(reordered, moving, position)
		return w.renumber(reordered)
	}

	// 跨层级：旧列表去掉该节点后重新编号，新列表插入后重新编号。
	oldList := make([]*Note, 0, len(metas))
	for _, s := range childrenOf(metas, oldParent) {
		if s.ID == id {
			continue
		}
		oldList = append(oldList, s)
	}
	newList := childrenOf(metas, newParent)
	newList = insertAt(newList, moving, position)

	// 先落盘 parent 变更，renumber 会重新读取完整笔记以保留正文。
	full, err := w.Get(id)
	if err != nil {
		return err
	}
	full.Parent = newParent
	full.Updated = time.Now().Truncate(time.Second)
	if err := w.writeNote(full); err != nil {
		return err
	}

	if err := w.renumber(oldList); err != nil {
		return err
	}
	return w.renumber(newList)
}

// Delete 删除一条笔记。存在子笔记且 recursive 为 false 时返回 ErrHasChildren；
// recursive 为 true 时连同全部后代一起删除。
func (w *Workspace) Delete(id string, recursive bool) error {
	metas, err := w.listMetas()
	if err != nil {
		return err
	}
	byID := make(map[string]*Note, len(metas))
	for _, m := range metas {
		byID[m.ID] = m
	}
	if _, ok := byID[id]; !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}

	children := childrenOf(metas, id)
	if len(children) > 0 && !recursive {
		return fmt.Errorf("%w: %s", ErrHasChildren, id)
	}

	targets := []string{id}
	if recursive {
		queue := []string{id}
		seen := map[string]bool{id: true}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for _, c := range childrenOf(metas, cur) {
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
		if err := os.Remove(w.notePath(tid)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("删除笔记 %s 失败: %w", tid, err)
		}
	}
	return nil
}

// ReadFile 读取任意路径的 Markdown 笔记文件。
//
// 没有 front matter 的文件同样可以读取：ID 取文件名去掉 .md，标题取第一个
// "# " 标题或文件名，创建/更新时间取文件修改时间。front matter 里的 id 与
// 文件名不一致时以文件名为准。
func ReadFile(path string) (*Note, error) {
	return readNoteFile(path)
}

// readNote 读取工作台内指定路径的笔记。
func (w *Workspace) readNote(path string) (*Note, error) {
	note, err := readNoteFile(path)
	if err != nil {
		return nil, err
	}
	return note, nil
}

// readNoteFile 读取并解析笔记文件。
func readNoteFile(path string) (*Note, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, path)
		}
		return nil, fmt.Errorf("读取笔记失败: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("读取笔记信息失败: %w", err)
	}

	id := IDFromPath(path)
	note := &Note{
		ID:      id,
		Type:    defaultNoteType,
		Created: info.ModTime(),
		Updated: info.ModTime(),
		Body:    string(data),
		Extra:   map[string]any{},
	}

	front, body, ok := splitFrontMatter(data)
	if ok {
		if parsed := decodeFrontMatter(front); parsed != nil {
			note.Title = parsed.Title
			note.Type = parsed.Type
			note.Tags = parsed.Tags
			note.Parent = parsed.Parent
			note.Order = parsed.Order
			note.Extra = parsed.Extra
			note.extraNodes = parsed.extraNodes
			if !parsed.Created.IsZero() {
				note.Created = parsed.Created
			}
			if !parsed.Updated.IsZero() {
				note.Updated = parsed.Updated
			}
			note.Body = string(body)
		}
	}

	if note.Type == "" {
		note.Type = defaultNoteType
	}
	if note.Title == "" {
		note.Title = deriveTitle(note.Body, id)
	}
	if note.Extra == nil {
		note.Extra = map[string]any{}
	}
	return note, nil
}

// deriveTitle 从正文第一个 "# " 标题推导标题，找不到时用文件名（ID）。
func deriveTitle(body, fallback string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "# ") {
			if t := strings.TrimSpace(line[2:]); t != "" {
				return t
			}
		}
	}
	return fallback
}

// splitFrontMatter 把文件内容拆成 front matter 与正文。
//
// 只有当文件第一行恰好是 "---" 且能找到结束行（"---" 或 "..."）时才认为
// 存在 front matter；正文里出现的 "---" 不会被误判。
func splitFrontMatter(data []byte) (front, body []byte, ok bool) {
	nl := bytes.IndexByte(data, '\n')
	if nl < 0 {
		return nil, data, false
	}
	if strings.TrimRight(string(data[:nl]), "\r") != "---" {
		return nil, data, false
	}

	rest := data[nl+1:]
	offset := 0
	for {
		idx := bytes.IndexByte(rest[offset:], '\n')
		var line string
		var next int
		if idx < 0 {
			line = string(rest[offset:])
			next = len(rest)
		} else {
			line = string(rest[offset : offset+idx])
			next = offset + idx + 1
		}
		trimmed := strings.TrimRight(line, "\r")
		if trimmed == "---" || trimmed == "..." {
			return rest[:offset], rest[next:], true
		}
		if idx < 0 {
			return nil, data, false
		}
		offset = next
	}
}

// decodeFrontMatter 解析 front matter。解析失败或不是映射时返回 nil，
// 调用方据此把整个文件当正文处理（兼容外部编辑器写坏的文件）。
func decodeFrontMatter(front []byte) *Note {
	var doc yaml.Node
	if err := yaml.Unmarshal(front, &doc); err != nil {
		return nil
	}
	if len(doc.Content) == 0 {
		return &Note{}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil
	}

	note := &Note{Extra: map[string]any{}, extraNodes: map[string]*yaml.Node{}}
	for i := 0; i+1 < len(root.Content); i += 2 {
		keyNode := root.Content[i]
		valNode := root.Content[i+1]
		key := keyNode.Value
		switch key {
		case "title":
			_ = valNode.Decode(&note.Title)
		case "type":
			_ = valNode.Decode(&note.Type)
		case "created":
			note.Created = decodeTime(valNode)
		case "updated":
			note.Updated = decodeTime(valNode)
		case "tags":
			_ = valNode.Decode(&note.Tags)
		case "parent":
			_ = valNode.Decode(&note.Parent)
		case "order":
			_ = valNode.Decode(&note.Order)
		case "id":
			// ID 以文件名为准，忽略 front matter 里的值。
		default:
			var v any
			if err := valNode.Decode(&v); err == nil {
				note.Extra[key] = v
			}
			note.extraNodes[key] = valNode
		}
	}
	return note
}

// decodeTime 解析 RFC3339 时间，失败时返回零值。
func decodeTime(node *yaml.Node) time.Time {
	var s string
	if err := node.Decode(&s); err != nil {
		return time.Time{}
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// writeNote 原子地写回笔记文件。
func (w *Workspace) writeNote(note *Note) error {
	data, err := marshalNote(note)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(w.notePath(note.ID), data); err != nil {
		return fmt.Errorf("写入笔记 %s 失败: %w", note.ID, err)
	}
	return nil
}

// marshalNote 把笔记序列化为 front matter + 正文。
func marshalNote(note *Note) ([]byte, error) {
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}

	appendKey := func(key string, val *yaml.Node) {
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
			val,
		)
	}
	appendValue := func(key string, v any) error {
		node, err := encodeNode(v)
		if err != nil {
			return err
		}
		appendKey(key, node)
		return nil
	}

	typ := note.Type
	if typ == "" {
		typ = defaultNoteType
	}
	tags := note.Tags
	if tags == nil {
		tags = []string{}
	}

	// 标准字段按固定顺序写入。
	appendKey("id", stringNode(note.ID))
	appendKey("title", stringNode(note.Title))
	appendKey("type", stringNode(typ))
	appendKey("created", stringNode(formatTime(note.Created)))
	appendKey("updated", stringNode(formatTime(note.Updated)))
	if err := appendValue("tags", tags); err != nil {
		return nil, fmt.Errorf("序列化 tags 失败: %w", err)
	}
	appendKey("parent", stringNode(note.Parent))
	if err := appendValue("order", note.Order); err != nil {
		return nil, fmt.Errorf("序列化 order 失败: %w", err)
	}

	// 未知字段按 key 排序后原样写回。
	extraKeys := make([]string, 0, len(note.Extra))
	for k := range note.Extra {
		if knownFrontMatterKeys[k] {
			continue
		}
		extraKeys = append(extraKeys, k)
	}
	sort.Strings(extraKeys)
	for _, k := range extraKeys {
		// 只有当前值与原始节点解码出的值完全一致时才复用原始节点，
		// 保持外部编辑器写入的格式（flow 风格、注释外的排版等）；
		// 值被调用方修改过时按新值重新编码，避免修改丢失。
		if node, ok := note.extraNodes[k]; ok && extraValueUnchanged(node, note.Extra[k]) {
			appendKey(k, cloneNode(node))
			continue
		}
		if err := appendValue(k, note.Extra[k]); err != nil {
			return nil, fmt.Errorf("序列化扩展字段 %s 失败: %w", k, err)
		}
	}

	front, err := yaml.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("序列化 front matter 失败: %w", err)
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(front)
	buf.WriteString("---\n")
	buf.WriteString(note.Body)
	return buf.Bytes(), nil
}

// extraValueUnchanged 判断 Extra 中的当前值是否与原始 YAML 节点解码出的值一致。
// 解码失败时视为已修改，走重新编码路径。
func extraValueUnchanged(node *yaml.Node, current any) bool {
	var orig any
	if err := node.Decode(&orig); err != nil {
		return false
	}
	return reflect.DeepEqual(orig, current)
}

// stringNode 构造一个双引号风格的 YAML 字符串标量，避免时间等被解析成非字符串类型。
func stringNode(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s, Style: yaml.DoubleQuotedStyle}
}

// encodeNode 把任意值编码为 YAML 节点。
func encodeNode(v any) (*yaml.Node, error) {
	node := &yaml.Node{}
	if err := node.Encode(v); err != nil {
		return nil, err
	}
	return node, nil
}

// cloneNode 深拷贝 YAML 节点，避免写回时污染缓存。
func cloneNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	c := *n
	c.Content = make([]*yaml.Node, len(n.Content))
	for i, child := range n.Content {
		c.Content[i] = cloneNode(child)
	}
	return &c
}

// formatTime 按 RFC3339 输出时间，保留原时区。
func formatTime(t time.Time) string {
	if t.IsZero() {
		return time.Time{}.Format(time.RFC3339)
	}
	return t.Format(time.RFC3339)
}

// listMetas 读取全部笔记元数据（不含正文）。
func (w *Workspace) listMetas() ([]*Note, error) {
	paths, err := w.ListWorkspaceNotes()
	if err != nil {
		return nil, err
	}
	metas := make([]*Note, 0, len(paths))
	for _, p := range paths {
		note, err := readNoteFile(p)
		if err != nil {
			return nil, err
		}
		note.Body = ""
		metas = append(metas, note)
	}
	return metas, nil
}

// childrenOf 返回 parent 下的子笔记，按 order 再按 id 排序。
func childrenOf(metas []*Note, parent string) []*Note {
	var children []*Note
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

// insertAt 把 note 插入 list 的第 position 个位置；position 越界时追加到末尾。
func insertAt(list []*Note, note *Note, position int) []*Note {
	if position < 0 || position >= len(list) {
		return append(list, note)
	}
	list = append(list, nil)
	copy(list[position+1:], list[position:])
	list[position] = note
	return list
}

// isDescendant 判断 candidate 是否是 ancestor 的后代（沿 parent 链向上查找）。
func (w *Workspace) isDescendant(byID map[string]*Note, candidate, ancestor string) bool {
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

// renumber 把 list 里的笔记 order 重新编为 0,1,2…，只重写 order 变化的文件。
// list 里可能只有元数据，写回前会重新读取完整笔记以保留正文。
func (w *Workspace) renumber(list []*Note) error {
	for i, n := range list {
		if n.Order == i {
			continue
		}
		full, err := w.Get(n.ID)
		if err != nil {
			return err
		}
		full.Order = i
		if err := w.writeNote(full); err != nil {
			return err
		}
	}
	return nil
}
