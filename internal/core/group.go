package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/axu4/kuibu/internal/llm"
	"github.com/axu4/kuibu/internal/store"
)

// GroupCategories 是组报固定的四个类别。
var GroupCategories = []string{"功能开发", "客户支持", "功能优化", "项目管理"}

// OtherProductLine 是归不进任何产品线时的兜底。
const OtherProductLine = "其他"

// structuredMarker 出现在栏目说明里表示按 产品线 × 类别 输出。
const structuredMarker = "[产品线×类别]"

// TemplateSection 是模板里的一个栏目。
type TemplateSection struct {
	Title       string `json:"title"`
	Instruction string `json:"instruction"`
	Structured  bool   `json:"structured"`
}

// ParseTemplate 解析模板：「## 栏目」一行一个栏目，紧跟的「> 」行是说明。
func ParseTemplate(body string) []TemplateSection {
	var out []TemplateSection
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "## "):
			out = append(out, TemplateSection{Title: strings.TrimSpace(t[3:])})
		case strings.HasPrefix(t, ">") && len(out) > 0:
			sec := &out[len(out)-1]
			text := strings.TrimSpace(strings.TrimPrefix(t, ">"))
			if strings.Contains(text, structuredMarker) {
				sec.Structured = true
				text = strings.TrimSpace(strings.ReplaceAll(text, structuredMarker, ""))
			}
			if text != "" {
				if sec.Instruction != "" {
					sec.Instruction += " "
				}
				sec.Instruction += text
			}
		}
	}
	return out
}

// GroupMember 是组报的一个组员及其定稿归一稿。
type GroupMember struct {
	Person       string
	Name         string
	ProductLines []string
	Draft        *store.Draft
}

// GroupPlan 是生成组报所需的全部输入。
type GroupPlan struct {
	Week         string
	Group        string
	LeadID       string
	LeadName     string
	Template     string
	Sections     []TemplateSection
	ProductLines []string
	Included     []GroupMember
	// Missing 是没有定稿归一稿的组员姓名。
	Missing []string
	// ProductFirst 为 true 时按 产品线 → 栏目 → 类别 渲染（产品报），否则 栏目 → 产品线 → 类别（组报）。
	ProductFirst bool
	// SourceNoun 是来源的称呼：组员（组报、组长归一）或 组（产品报）。
	SourceNoun string
}

// sourceNoun 返回来源称呼，默认「组员」。
func (p *GroupPlan) sourceNoun() string {
	if p.SourceNoun == "" {
		return "组员"
	}
	return p.SourceNoun
}

// GroupOwnerKey 返回某组组报 / 筛选记录 / 月报拉取记录的存储键。
//
// 这些文件按组长 personId 存放；同一个人带多个组时（比如自己的组 + 收各 DE 周报的组），
// 按组配置顺序第一个组沿用 personId（兼容已有数据），其余组用「personId.组 id」，避免互相覆盖。
func GroupOwnerKey(ws *store.Workspace, leadID, group string) string {
	if group == "" {
		return leadID
	}
	groups, err := ws.ReportGroups()
	if err != nil {
		return leadID
	}
	first := ""
	for _, g := range groups {
		if g.Lead != leadID {
			continue
		}
		if first == "" {
			first = g.Name
		}
		if g.Name == group {
			if first == group {
				return leadID
			}
			return leadID + "." + g.ID
		}
	}
	return leadID
}

// LeadIDFromKey 从存储键还原组长 personId。
func LeadIDFromKey(key string) string {
	if i := strings.Index(key, "."); i > 0 {
		return key[:i]
	}
	return key
}

// leadForKey 按存储键找组长：返回的副本 ID 是存储键，姓名等取自名单。
func leadForKey(ws *store.Workspace, key string) (*store.Person, error) {
	p, err := ws.ResolvePerson(LeadIDFromKey(key))
	if err != nil {
		return nil, err
	}
	cp := *p
	cp.ID = key
	return &cp, nil
}

// groupOwner 返回 ID 换成该组存储键的组长副本，并把旧版混在 personId 下的筛选记录挪到所属组。
func groupOwner(ws *store.Workspace, week string, lead *store.Person, group string) *store.Person {
	migrateTriage(ws, week, lead.ID)
	key := GroupOwnerKey(ws, lead.ID, group)
	if key == lead.ID {
		return lead
	}
	cp := *lead
	cp.ID = key
	return &cp
}

// migrateTriage：旧版同一组长带多个组时，筛选记录都写在 personId 下，后打开的组会覆盖。
// 记录里存着组名，属于非首组时挪到该组自己的存储键（目标已存在则不动）。
func migrateTriage(ws *store.Workspace, week, leadID string) {
	t, err := ws.ReadTriage(week, leadID)
	if err != nil || t.Group == "" {
		return
	}
	key := GroupOwnerKey(ws, leadID, t.Group)
	if key == leadID {
		return
	}
	if _, err := ws.ReadTriage(week, key); err == nil {
		return
	}
	t.Lead = key
	if err := ws.WriteTriage(t); err != nil {
		return
	}
	_ = os.Remove(ws.TriagePath(week, leadID))
}

// ResolveLead 找到组长：group 为空时取唯一有组长的组。
//
// 返回的组长 ID 是该组的存储键（见 GroupOwnerKey），用于读写组报、筛选记录等文件。
func ResolveLead(ws *store.Workspace, week, group string) (*store.Person, string, error) {
	people, err := ws.ListPersons()
	if err != nil {
		return nil, "", err
	}
	monday, _, err := store.WeekRange(week)
	if err != nil {
		return nil, "", err
	}
	var leads []*store.Person
	// 组长的 group 可能留空（只带组员、不给自己填组）；指定组时先精确匹配，
	// 没有精确匹配再把空组组长视为该组，避免「组里有组员却找不到组长」。
	for _, p := range people {
		if !p.Active || p.Role != store.RoleLead {
			continue
		}
		g, _ := p.MembershipAt(monday)
		if g == "" {
			g = p.Group
		}
		if group == "" || g == group {
			leads = append(leads, p)
		}
	}
	if len(leads) == 0 && group != "" {
		for _, p := range people {
			if !p.Active || p.Role != store.RoleLead {
				continue
			}
			g, _ := p.MembershipAt(monday)
			if g == "" && p.Group == "" {
				leads = append(leads, p)
			}
		}
	}
	switch {
	case len(leads) == 1:
		g, _ := leads[0].MembershipAt(monday)
		if g == "" {
			g = leads[0].Group
		}
		if group != "" && g == "" {
			g = group
		}
		return groupOwner(ws, week, leads[0], g), g, nil
	case len(leads) == 0 && group != "":
		return nil, "", fmt.Errorf("%w: 组「%s」没有组长，请在名单里把自己加为组长", store.ErrNotFound, group)
	case len(leads) == 0:
		return nil, "", fmt.Errorf("%w: 名单里没有组长，请在名单里把自己加为组长", store.ErrNotFound)
	default:
		return nil, "", fmt.Errorf("%w: 有多个组的组长，请用 --group 指定", store.ErrInvalid)
	}
}

// PlanGroupReport 收集组员定稿归一稿。有组员没定稿时，allowMissing 为 false 返回
// ErrNotFinal 并列出名单。
func PlanGroupReport(ws *store.Workspace, week, group, template string, allowMissing bool) (*GroupPlan, error) {
	lead, groupName, err := ResolveLead(ws, week, group)
	if err != nil {
		return nil, err
	}
	if groupName == "" {
		groupName, err = inferLeaderGroup(ws, week)
		if err != nil {
			return nil, err
		}
	}
	if template == "" {
		template = "group"
	}
	tplBody, _, err := ws.ReadTemplate(template)
	if err != nil {
		return nil, err
	}
	sections := ParseTemplate(tplBody)
	if len(sections) == 0 {
		return nil, fmt.Errorf("%w: 模板 %s 没有任何「## 栏目」", store.ErrInvalid, template)
	}
	people, err := ws.ListPersons()
	if err != nil {
		return nil, err
	}
	monday, _, err := store.WeekRange(week)
	if err != nil {
		return nil, err
	}
	plan := &GroupPlan{Week: week, Group: groupName, LeadID: lead.ID, LeadName: lead.Name, Template: template, Sections: sections}
	lines := map[string]bool{}
	for _, p := range people {
		if !p.Active || p.Role != store.RoleMember {
			continue
		}
		g, products := p.MembershipAt(monday)
		if g != groupName {
			continue
		}
		for _, l := range products {
			lines[l] = true
		}
		d, err := ws.FinalDraft(week, p.ID)
		if err != nil {
			if errors.Is(err, store.ErrNotFinal) || errors.Is(err, store.ErrNotFound) {
				plan.Missing = append(plan.Missing, p.Name)
				continue
			}
			return nil, err
		}
		plan.Included = append(plan.Included, GroupMember{Person: p.ID, Name: p.Name, ProductLines: products, Draft: d})
	}
	for l := range lines {
		plan.ProductLines = append(plan.ProductLines, l)
	}
	sort.Strings(plan.ProductLines)
	sort.Strings(plan.Missing)
	sort.Slice(plan.Included, func(i, j int) bool { return plan.Included[i].Name < plan.Included[j].Name })
	if len(plan.Included) == 0 {
		return nil, fmt.Errorf("%w: %s 组还没有任何定稿的归一稿", store.ErrNotFinal, groupName)
	}
	if len(plan.Missing) > 0 && !allowMissing {
		return nil, fmt.Errorf("%w: 以下组员的归一稿未定稿：%s（确认先不纳入请加 --allow-missing / 点「仍然生成」）",
			store.ErrNotFinal, strings.Join(plan.Missing, "、"))
	}
	return plan, nil
}

// SourceHash 是纳入组员定稿正文的合并哈希，任何人定稿内容变化都会改变它。
func (p *GroupPlan) SourceHash() string {
	h := sha256.New()
	for _, m := range p.Included {
		h.Write([]byte(m.Person + "\n" + m.Draft.Body + "\n\x00"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// SourceIDs 返回纳入组员的 ID。
func (p *GroupPlan) SourceIDs() []string {
	out := make([]string, 0, len(p.Included))
	for _, m := range p.Included {
		out = append(out, m.Person)
	}
	return out
}

// GroupRequest 构造组报请求：AI 只负责归类与提炼措辞，返回 JSON，结构由代码渲染。
func GroupRequest(p *GroupPlan) llm.Request {
	var sys strings.Builder
	noun := p.sourceNoun()
	fmt.Fprintf(&sys, "你是研发周报的组报整理助手。把下面各%s的周报合并成报告条目。\n", noun)
	sys.WriteString("只输出 JSON 数组，不要任何解释或代码围栏，每个元素：\n")
	fmt.Fprintf(&sys, `{"section": "栏目名", "productLine": "产品线", "category": "类别", "text": "事项", "sources": ["%s名称"], "pending": false}`+"\n\n", noun)
	sys.WriteString("栏目（section 只能取这些，原样照抄）：\n")
	for _, s := range p.Sections {
		kind := "平铺，productLine 和 category 留空"
		if s.Structured {
			kind = "按产品线 × 类别归类"
		}
		fmt.Fprintf(&sys, "- %s（%s）", s.Title, kind)
		if s.Instruction != "" {
			sys.WriteString("：" + s.Instruction)
		}
		sys.WriteString("\n")
	}
	lines := append(append([]string{}, p.ProductLines...), OtherProductLine)
	fmt.Fprintf(&sys, "\n产品线只能取：%s。\n", strings.Join(lines, "、"))
	fmt.Fprintf(&sys, "类别只能取：%s。\n", strings.Join(GroupCategories, "、"))
	fmt.Fprintf(&sys, `规则：
1. 同一事项在多个%[1]s里出现时合并成一条，sources 列出所有相关%[1]s（用〔〕里的名称）。
2. 优先按事项的【领域】前缀或所在产品线小节判断产品线；判断不了时按%[1]s所属产品线推断，并把 pending 设为 true。
3. 措辞简洁，保留进度、数字、链接、编号，不编造原文没有的内容。
4. text 不要再带【领域】前缀。`, noun)

	var user strings.Builder
	for i, m := range p.Included {
		if i > 0 {
			user.WriteString("\n\n")
		}
		fmt.Fprintf(&user, "〔%s〕所属产品线：%s\n%s", m.Name, strings.Join(m.ProductLines, "、"), m.Draft.Body)
	}
	return llm.Request{System: sys.String(), Messages: []llm.Message{{Role: "user", Content: user.String()}}}
}

// GroupItem 是模型返回的一条组报条目。
type GroupItem struct {
	Section     string   `json:"section"`
	ProductLine string   `json:"productLine"`
	Category    string   `json:"category"`
	Text        string   `json:"text"`
	Sources     []string `json:"sources"`
	Pending     bool     `json:"pending"`
}

// RenderGroupReport 解析模型 JSON 并渲染组报 Markdown。结构由代码保证：
// ## 栏目 → ### 产品线 → #### 类别 → - 事项（来源），空类别不输出。
// JSON 解析失败时 ok 为 false，返回原文，不丢内容。
func RenderGroupReport(p *GroupPlan, text string) (body string, ok bool) {
	raw := strings.TrimSpace(text)
	if i, j := strings.IndexByte(raw, '['), strings.LastIndexByte(raw, ']'); i >= 0 && j > i {
		raw = raw[i : j+1]
	}
	var items []GroupItem
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return strings.TrimSpace(text), false
	}

	lines := append(append([]string{}, p.ProductLines...), OtherProductLine)
	knownLine := map[string]bool{}
	for _, l := range lines {
		knownLine[l] = true
	}
	knownCat := map[string]bool{}
	for _, c := range GroupCategories {
		knownCat[c] = true
	}
	bySection := map[string][]GroupItem{}
	for _, it := range items {
		it.Text = strings.TrimSpace(it.Text)
		if it.Text == "" {
			continue
		}
		bySection[it.Section] = append(bySection[it.Section], it)
	}

	var b strings.Builder
	if len(p.Missing) > 0 {
		fmt.Fprintf(&b, "> 未纳入（未定稿）：%s\n\n", strings.Join(p.Missing, "、"))
	}
	lineOf := func(it GroupItem) string {
		if knownLine[it.ProductLine] {
			return it.ProductLine
		}
		return OtherProductLine
	}
	writeCats := func(list []GroupItem, heading string) {
		cats := append(append([]string{}, GroupCategories...), "未分类")
		for _, cat := range cats {
			var inCat []GroupItem
			for _, it := range list {
				c := it.Category
				if !knownCat[c] {
					c = "未分类"
				}
				if c == cat {
					inCat = append(inCat, it)
				}
			}
			if len(inCat) == 0 {
				continue
			}
			b.WriteString(heading + " " + cat + "\n")
			for _, it := range inCat {
				b.WriteString(itemLine(it))
			}
		}
	}
	if p.ProductFirst {
		renderProductFirst(&b, p, lines, bySection, lineOf, writeCats)
		return strings.TrimRight(b.String(), "\n"), true
	}
	for i, sec := range p.Sections {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("## " + sec.Title + "\n")
		list := bySection[sec.Title]
		if len(list) == 0 {
			b.WriteString("- 无\n")
			continue
		}
		if !sec.Structured {
			for _, it := range list {
				b.WriteString(itemLine(it))
			}
			continue
		}
		for _, line := range lines {
			var inLine []GroupItem
			for _, it := range list {
				if lineOf(it) == line {
					inLine = append(inLine, it)
				}
			}
			if len(inLine) == 0 {
				continue
			}
			b.WriteString("\n### " + line + "\n")
			writeCats(inLine, "####")
		}
	}
	return strings.TrimRight(b.String(), "\n"), true
}

// renderProductFirst 产品报布局：## 产品线 → ### 栏目 → #### 类别；不分产品线的栏目在最后单独成章。
func renderProductFirst(b *strings.Builder, p *GroupPlan, lines []string, bySection map[string][]GroupItem,
	lineOf func(GroupItem) string, writeCats func([]GroupItem, string)) {
	first := true
	for _, line := range lines {
		var secs []TemplateSection
		for _, sec := range p.Sections {
			if !sec.Structured {
				continue
			}
			for _, it := range bySection[sec.Title] {
				if lineOf(it) == line {
					secs = append(secs, sec)
					break
				}
			}
		}
		if len(secs) == 0 {
			continue
		}
		if !first {
			b.WriteString("\n")
		}
		first = false
		b.WriteString("## " + line + "\n")
		for _, sec := range secs {
			var inLine []GroupItem
			for _, it := range bySection[sec.Title] {
				if lineOf(it) == line {
					inLine = append(inLine, it)
				}
			}
			b.WriteString("\n### " + sec.Title + "\n")
			writeCats(inLine, "####")
		}
	}
	for _, sec := range p.Sections {
		if sec.Structured {
			continue
		}
		if !first {
			b.WriteString("\n")
		}
		first = false
		b.WriteString("## " + sec.Title + "\n")
		list := bySection[sec.Title]
		if len(list) == 0 {
			b.WriteString("- 无\n")
			continue
		}
		for _, it := range list {
			b.WriteString(itemLine(it))
		}
	}
}

// itemLine 渲染一条：- 事项（来源）（待确认）。
func itemLine(it GroupItem) string {
	line := "- " + it.Text
	if src := uniq(it.Sources); len(src) > 0 {
		line += "（" + strings.Join(src, "、") + "）"
	}
	if it.Pending {
		line += "（待确认）"
	}
	return line + "\n"
}

// GroupSourceChanged 判断组报 / 产品报生成后，纳入来源的定稿内容是否又变了。
func GroupSourceChanged(ws *store.Workspace, d *store.Draft) bool {
	if d.SourceHash == "" || len(d.Sources) == 0 {
		return false
	}
	body := func(id string) (string, error) {
		fd, err := ws.FinalDraft(d.Week, id)
		if err != nil {
			return "", err
		}
		return fd.Body, nil
	}
	switch {
	case store.ValidMonth(d.Week):
		body = func(id string) (string, error) { return monthSourceBody(ws, d, id) }
	case d.Kind == store.ProductDirName:
		body = func(id string) (string, error) { return leadFinalBody(ws, d.Week, id) }
	}
	h, ok := sourcesHash(d.Sources, body)
	return !ok || h != d.SourceHash
}

// SaveGroupReport 渲染模型输出并保存为组报草稿。warning 非空表示结构解析失败、整段保存。
func SaveGroupReport(ws *store.Workspace, p *GroupPlan, model, text string, force bool, now time.Time) (*store.Draft, string, error) {
	body, ok := RenderGroupReport(p, text)
	warning := ""
	if !ok {
		warning = "模型输出不是预期的 JSON，已整段保存为草稿，请人工整理"
	}
	d, err := ws.SaveGeneratedDraft(store.GeneratedDraft{
		Kind:       store.GroupDirName,
		Group:      p.Group,
		Sources:    p.SourceIDs(),
		Missing:    p.Missing,
		Person:     p.LeadID,
		Name:       p.LeadName,
		Week:       p.Week,
		SourceHash: p.SourceHash(),
		Model:      model,
		Body:       body,
		Now:        now,
	}, force)
	return d, warning, err
}

// SubmitGroupReport 把定稿组报以组长本人身份写入该周 lead 层级（带入 SE tab），
// source 记 group-report，复用覆盖 + .history 规则。未定稿返回 ErrNotFinal。
func SubmitGroupReport(ws *store.Workspace, week, leadID string, now time.Time) (*store.Report, error) {
	d, err := ws.ReadGroupReport(week, leadID)
	if err != nil {
		return nil, err
	}
	if d.Status != store.DraftStatusFinal {
		return nil, fmt.Errorf("%w: %s 的组报尚未定稿，定稿后才能带入 SE", store.ErrNotFinal, week)
	}
	return ws.SubmitReport(store.SubmitReportInput{
		Person: leadID,
		Week:   week,
		Level:  store.ReportLevelLead,
		Source: store.ReportSourceGroup,
		Body:   d.Body,
		Now:    now,
	})
}
