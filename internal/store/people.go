package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// 组员角色取值。
const (
	// RoleMember 表示组员。
	RoleMember = "member"
	// RoleLead 表示组长。
	RoleLead = "lead"
)

// ValidPersonRole 判断组员角色是否合法。
func ValidPersonRole(role string) bool {
	return role == RoleMember || role == RoleLead
}

// Person 是一名组员，包含 front matter 元数据与可选备注正文。
type Person struct {
	// ID 是组员标识，同时也是文件名（不含 .md）。
	ID string
	// Name 是姓名。
	Name string
	// Aliases 是别名列表，用于粘贴识别。
	Aliases []string
	// Group 是当前所属组。
	Group string
	// ProductLines 是当前产品线，可多个。
	ProductLines []string
	// Role 是角色：member 组员 / lead 组长。
	Role string
	// Active 表示是否在职，默认 true。
	Active bool
	// GroupHistory 是转组记录列表，第一条是加入时。
	GroupHistory []GroupRecord
	// Created 是创建时间。
	Created time.Time
	// Updated 是最后更新时间。
	Updated time.Time
	// Body 是可选备注。
	Body string
	// Extra 保存 front matter 中未识别的字段。
	Extra map[string]any

	// extraNodes 保存未识别字段的原始 YAML 节点，用于无损写回。
	extraNodes map[string]*yaml.Node
}

// NewPerson 是创建组员时传入的字段。
type NewPerson struct {
	Name         string
	Aliases      []string
	Group        string
	ProductLines []string
	Role         string
	// Active 为 nil 时默认 true。
	Active       *bool
	GroupHistory []GroupRecord
	Body         string
	Now          time.Time
}

// knownPersonFrontMatterKeys 是组员认识的标准 front matter 字段。
var knownPersonFrontMatterKeys = map[string]bool{
	"id":           true,
	"name":         true,
	"aliases":      true,
	"group":        true,
	"productLines": true,
	"role":         true,
	"active":       true,
	"groupHistory": true,
	"created":      true,
	"updated":      true,
}

// personPath 返回组员文件的绝对路径。
func (w *Workspace) personPath(id string) string {
	return filepath.Join(w.PeopleDir(), id+noteExt)
}

// CreatePerson 新建一名组员。姓名/别名在在职人员中必须唯一（大小写不敏感、
// 去首尾空格），冲突时返回 ErrConflict。
func (w *Workspace) CreatePerson(in NewPerson) (*Person, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: 姓名不能为空", ErrInvalid)
	}
	role := in.Role
	if role == "" {
		role = RoleMember
	}
	if !ValidPersonRole(role) {
		return nil, fmt.Errorf("%w: 非法组员角色 %q", ErrInvalid, role)
	}

	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	now = now.Truncate(time.Second)

	active := true
	if in.Active != nil {
		active = *in.Active
	}

	if err := w.checkPersonUniqueness("", name, in.Aliases); err != nil {
		return nil, err
	}

	person := &Person{
		Name:         name,
		Aliases:      dedupTrim(in.Aliases),
		Group:        strings.TrimSpace(in.Group),
		ProductLines: dedupTrim(in.ProductLines),
		Role:         role,
		Active:       active,
		Created:      now,
		Updated:      now,
		Body:         in.Body,
		Extra:        map[string]any{},
	}
	if person.Aliases == nil {
		person.Aliases = []string{}
	}
	if person.ProductLines == nil {
		person.ProductLines = []string{}
	}

	history := append([]GroupRecord(nil), in.GroupHistory...)
	if len(history) == 0 {
		// 第一条转组记录就是加入时。
		history = append(history, GroupRecord{
			Group:        person.Group,
			ProductLines: append([]string(nil), person.ProductLines...),
			From:         now.Format("2006-01-02"),
		})
	}
	sortGroupHistory(history)
	person.GroupHistory = history

	for i := 0; i < 100; i++ {
		id, err := newID(now)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(w.personPath(id)); errors.Is(err, os.ErrNotExist) {
			person.ID = id
			break
		}
	}
	if person.ID == "" {
		return nil, fmt.Errorf("%w: 无法生成唯一组员 ID", ErrInvalid)
	}

	if err := w.writePerson(person); err != nil {
		return nil, err
	}
	return person, nil
}

// GetPerson 按 ID 读取一名组员（含备注正文）。
func (w *Workspace) GetPerson(id string) (*Person, error) {
	if id == "" || strings.ContainsAny(id, `/\`) {
		return nil, newPersonError(ErrNotFound, "人员不存在: %q", id)
	}
	return readPersonFile(w.personPath(id))
}

// ReadPersonFile 读取任意路径的组员文件，供索引使用。
func ReadPersonFile(path string) (*Person, error) {
	return readPersonFile(path)
}

// ListPersons 返回全部组员（含备注正文），按 ID 排序。
func (w *Workspace) ListPersons() ([]*Person, error) {
	paths, err := w.ListWorkspacePeople()
	if err != nil {
		return nil, err
	}
	people := make([]*Person, 0, len(paths))
	for _, p := range paths {
		person, err := readPersonFile(p)
		if err != nil {
			return nil, err
		}
		people = append(people, person)
	}
	sort.Slice(people, func(i, j int) bool { return people[i].ID < people[j].ID })
	return people, nil
}

// ResolvePerson 按 ID 或姓名/别名解析组员。
//
// 优先按 ID 精确匹配；否则在在职人员中按姓名/别名匹配（大小写不敏感、去首尾
// 空格）。匹配到多个时返回 ErrAmbiguousPerson。
func (w *Workspace) ResolvePerson(query string) (*Person, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, newPersonError(ErrNotFound, "人员不存在: %q", query)
	}
	if p, err := w.GetPerson(query); err == nil {
		return p, nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	people, err := w.ListPersons()
	if err != nil {
		return nil, err
	}
	target := normalizeName(query)
	var matches []*Person
	for _, p := range people {
		if normalizeName(p.Name) == target {
			matches = append(matches, p)
			continue
		}
		for _, a := range p.Aliases {
			if normalizeName(a) == target {
				matches = append(matches, p)
				break
			}
		}
	}
	switch len(matches) {
	case 0:
		return nil, newPersonError(ErrNotFound, "人员不存在: %s", query)
	case 1:
		return matches[0], nil
	default:
		return nil, newPersonError(ErrAmbiguousPerson, "姓名匹配不唯一: %s", query)
	}
}

// UpdatePerson 用 fn 修改组员，自动刷新 updated 并校验姓名唯一性。
func (w *Workspace) UpdatePerson(id string, now time.Time, fn func(*Person) error) (*Person, error) {
	person, err := w.GetPerson(id)
	if err != nil {
		return nil, err
	}
	if fn != nil {
		if err := fn(person); err != nil {
			return nil, err
		}
	}
	person.ID = id
	person.Name = strings.TrimSpace(person.Name)
	if person.Name == "" {
		return nil, fmt.Errorf("%w: 姓名不能为空", ErrInvalid)
	}
	if !ValidPersonRole(person.Role) {
		return nil, fmt.Errorf("%w: 非法组员角色 %q", ErrInvalid, person.Role)
	}
	if err := w.checkPersonUniqueness(id, person.Name, person.Aliases); err != nil {
		return nil, err
	}

	if now.IsZero() {
		now = time.Now()
	}
	person.Updated = now.Truncate(time.Second)
	person.Aliases = dedupTrim(person.Aliases)
	person.ProductLines = dedupTrim(person.ProductLines)
	if person.Aliases == nil {
		person.Aliases = []string{}
	}
	if person.ProductLines == nil {
		person.ProductLines = []string{}
	}
	sortGroupHistory(person.GroupHistory)

	if err := w.writePerson(person); err != nil {
		return nil, err
	}
	return person, nil
}

// MovePerson 转组：给出新组（可选新产品线）与生效日期（默认今天），追加一条
// groupHistory，并更新当前组与产品线。productLines 为空时保持当前产品线。
func (w *Workspace) MovePerson(id, group string, productLines []string, from string, now time.Time) (*Person, error) {
	if strings.TrimSpace(group) == "" {
		return nil, fmt.Errorf("%w: 转组目标组不能为空", ErrInvalid)
	}
	if from == "" {
		if now.IsZero() {
			now = time.Now()
		}
		from = now.Format("2006-01-02")
	}
	if _, err := time.ParseInLocation("2006-01-02", from, TodoLocation()); err != nil {
		return nil, fmt.Errorf("%w: 转组生效日期非法 %q", ErrInvalid, from)
	}

	person, err := w.ResolvePerson(id)
	if err != nil {
		return nil, err
	}
	return w.UpdatePerson(person.ID, now, func(p *Person) error {
		if len(productLines) > 0 {
			p.ProductLines = dedupTrim(productLines)
		}
		p.GroupHistory = append(p.GroupHistory, GroupRecord{
			Group:        strings.TrimSpace(group),
			ProductLines: append([]string(nil), p.ProductLines...),
			From:         from,
		})
		// 当前组/产品线按今天生效的记录重算，未来生效的转组不会提前改变当前归属。
		today := now
		if today.IsZero() {
			today = time.Now()
		}
		p.Group, p.ProductLines = p.MembershipAt(today)
		return nil
	})
}

// MembershipAt 返回某人在指定日期所属的组与产品线（按 groupHistory 最近一条
// from <= date 的记录；没有则回退当前字段）。date 使用 TodoLocation 时区。
func (p *Person) MembershipAt(date time.Time) (string, []string) {
	if len(p.GroupHistory) == 0 {
		return p.Group, p.ProductLines
	}
	day := date.In(TodoLocation()).Format("2006-01-02")
	var best GroupRecord
	found := false
	for _, r := range p.GroupHistory {
		if r.From > day {
			continue
		}
		if !found || r.From > best.From {
			best = r
			found = true
		}
	}
	if !found {
		return p.Group, p.ProductLines
	}
	return best.Group, best.ProductLines
}

// checkPersonUniqueness 校验姓名与别名在在职人员中唯一（排除 selfID）。
func (w *Workspace) checkPersonUniqueness(selfID, name string, aliases []string) error {
	people, err := w.ListPersons()
	if err != nil {
		return err
	}
	tokens := make([]string, 0, 1+len(aliases))
	if normalizeName(name) != "" {
		tokens = append(tokens, normalizeName(name))
	}
	for _, a := range aliases {
		if n := normalizeName(a); n != "" {
			tokens = append(tokens, n)
		}
	}
	for _, p := range people {
		if p.ID == selfID || !p.Active {
			continue
		}
		owned := []string{normalizeName(p.Name)}
		for _, a := range p.Aliases {
			owned = append(owned, normalizeName(a))
		}
		for _, t := range tokens {
			for _, o := range owned {
				if t == o {
					return newPersonError(ErrConflict, "姓名或别名冲突: %s（已由 %s 使用）", t, p.Name)
				}
			}
		}
	}
	return nil
}

// readPersonFile 读取并解析组员文件。
func readPersonFile(path string) (*Person, error) {
	id := IDFromPath(path)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, newPersonError(ErrNotFound, "人员不存在: %s", id)
		}
		return nil, fmt.Errorf("读取组员失败: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("读取组员信息失败: %w", err)
	}

	person := &Person{
		ID:      id,
		Role:    RoleMember,
		Active:  true,
		Created: info.ModTime(),
		Updated: info.ModTime(),
		Body:    string(data),
		Extra:   map[string]any{},
	}

	front, body, ok := splitFrontMatter(data)
	if ok {
		if parsed := decodePersonFrontMatter(front); parsed != nil {
			person.Name = parsed.Name
			person.Aliases = parsed.Aliases
			person.Group = parsed.Group
			person.ProductLines = parsed.ProductLines
			person.Role = parsed.Role
			person.Active = parsed.Active
			person.GroupHistory = parsed.GroupHistory
			person.Extra = parsed.Extra
			person.extraNodes = parsed.extraNodes
			if !parsed.Created.IsZero() {
				person.Created = parsed.Created
			}
			if !parsed.Updated.IsZero() {
				person.Updated = parsed.Updated
			}
			person.Body = string(body)
		}
	}

	if person.Role == "" {
		person.Role = RoleMember
	}
	if person.Name == "" {
		person.Name = id
	}
	if person.Aliases == nil {
		person.Aliases = []string{}
	}
	if person.ProductLines == nil {
		person.ProductLines = []string{}
	}
	if person.Extra == nil {
		person.Extra = map[string]any{}
	}
	sortGroupHistory(person.GroupHistory)
	return person, nil
}

// decodePersonFrontMatter 解析组员 front matter。解析失败或不是映射时返回 nil。
func decodePersonFrontMatter(front []byte) *Person {
	root, empty, ok := frontMatterMapping(front)
	if !ok {
		if empty {
			return &Person{Active: true}
		}
		return nil
	}

	person := &Person{Active: true, Extra: map[string]any{}, extraNodes: map[string]*yaml.Node{}}
	for i := 0; i+1 < len(root.Content); i += 2 {
		keyNode := root.Content[i]
		valNode := root.Content[i+1]
		switch keyNode.Value {
		case "name":
			_ = valNode.Decode(&person.Name)
		case "aliases":
			_ = valNode.Decode(&person.Aliases)
		case "group":
			_ = valNode.Decode(&person.Group)
		case "productLines":
			_ = valNode.Decode(&person.ProductLines)
		case "role":
			_ = valNode.Decode(&person.Role)
		case "active":
			_ = valNode.Decode(&person.Active)
		case "groupHistory":
			person.GroupHistory = decodeGroupHistory(valNode)
		case "created":
			person.Created = decodeTime(valNode)
		case "updated":
			person.Updated = decodeTime(valNode)
		case "id":
			// ID 以文件名为准，忽略 front matter 里的值。
		default:
			var v any
			if err := valNode.Decode(&v); err == nil {
				person.Extra[keyNode.Value] = v
			}
			person.extraNodes[keyNode.Value] = valNode
		}
	}
	return person
}

// decodeGroupHistory 解析 groupHistory 序列。
func decodeGroupHistory(node *yaml.Node) []GroupRecord {
	var records []GroupRecord
	if node == nil || node.Kind != yaml.SequenceNode {
		return records
	}
	for _, item := range node.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		var r GroupRecord
		for i := 0; i+1 < len(item.Content); i += 2 {
			k := item.Content[i].Value
			v := item.Content[i+1]
			switch k {
			case "group":
				_ = v.Decode(&r.Group)
			case "productLines":
				_ = v.Decode(&r.ProductLines)
			case "from":
				_ = v.Decode(&r.From)
			}
		}
		records = append(records, r)
	}
	return records
}

// writePerson 原子地写回组员文件。
func (w *Workspace) writePerson(person *Person) error {
	data, err := marshalPerson(person)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(w.personPath(person.ID), data); err != nil {
		return fmt.Errorf("写入组员 %s 失败: %w", person.ID, err)
	}
	return nil
}

// marshalPerson 把组员序列化为 front matter + 正文。
func marshalPerson(person *Person) ([]byte, error) {
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}

	aliases := person.Aliases
	if aliases == nil {
		aliases = []string{}
	}
	products := person.ProductLines
	if products == nil {
		products = []string{}
	}
	history := person.GroupHistory
	if history == nil {
		history = []GroupRecord{}
	}

	appendFrontMatterKey(root, "id", stringNode(person.ID))
	appendFrontMatterKey(root, "name", stringNode(person.Name))
	if err := appendFrontMatterValue(root, "aliases", aliases); err != nil {
		return nil, fmt.Errorf("序列化 aliases 失败: %w", err)
	}
	appendFrontMatterKey(root, "group", stringNode(person.Group))
	if err := appendFrontMatterValue(root, "productLines", products); err != nil {
		return nil, fmt.Errorf("序列化 productLines 失败: %w", err)
	}
	appendFrontMatterKey(root, "role", stringNode(person.Role))
	if err := appendFrontMatterValue(root, "active", person.Active); err != nil {
		return nil, fmt.Errorf("序列化 active 失败: %w", err)
	}
	appendFrontMatterKey(root, "groupHistory", encodeGroupHistory(history))
	appendFrontMatterKey(root, "created", stringNode(formatTime(person.Created)))
	appendFrontMatterKey(root, "updated", stringNode(formatTime(person.Updated)))

	if err := appendExtraFrontMatter(root, person.Extra, person.extraNodes, knownPersonFrontMatterKeys); err != nil {
		return nil, err
	}
	return marshalFrontMatter(root, person.Body)
}

// encodeGroupHistory 把转组记录编码为 YAML 序列节点。
func encodeGroupHistory(records []GroupRecord) *yaml.Node {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, r := range records {
		m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		m.Content = append(m.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "group"},
			stringNode(r.Group),
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "productLines"},
		)
		products := r.ProductLines
		if products == nil {
			products = []string{}
		}
		productNode, err := encodeNode(products)
		if err != nil {
			productNode = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		}
		m.Content = append(m.Content, productNode)
		m.Content = append(m.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "from"},
			stringNode(r.From),
		)
		seq.Content = append(seq.Content, m)
	}
	return seq
}

// sortGroupHistory 按生效日期升序排序转组记录，保持第一条为最早（加入时）。
func sortGroupHistory(records []GroupRecord) {
	sort.SliceStable(records, func(i, j int) bool { return records[i].From < records[j].From })
}

// dedupTrim 去重（大小写不敏感）并去首尾空格，保持顺序、过滤空串。
func dedupTrim(list []string) []string {
	out := make([]string, 0, len(list))
	seen := map[string]bool{}
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		k := strings.ToLower(s)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	return out
}

// normalizeName 归一化姓名/别名：去首尾空格并转小写。
func normalizeName(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
