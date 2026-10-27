package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
}

// ResolveLead 找到组长：group 为空时取唯一有组长的组。
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
	switch {
	case len(leads) == 1:
		g, _ := leads[0].MembershipAt(monday)
		if g == "" {
			g = leads[0].Group
		}
		return leads[0], g, nil
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
	sys.WriteString("你是研发小组长的组报整理助手。把下面各组员的定稿周报合并成组报条目。\n")
	sys.WriteString("只输出 JSON 数组，不要任何解释或代码围栏，每个元素：\n")
	sys.WriteString(`{"section": "栏目名", "productLine": "产品线", "category": "类别", "text": "事项", "sources": ["组员姓名"], "pending": false}` + "\n\n")
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
	sys.WriteString(`规则：
1. 同一事项在多位组员里出现时合并成一条，sources 列出所有相关组员。
2. 优先按事项的【领域】前缀判断产品线；判断不了时按组员所属产品线推断，并把 pending 设为 true。
3. 措辞简洁，保留进度、数字、链接、编号，不编造原文没有的内容。
4. text 不要再带【领域】前缀。`)

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
		fmt.Fprintf(&b, "> 未纳入（归一稿未定稿）：%s\n\n", strings.Join(p.Missing, "、"))
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
				pl := it.ProductLine
				if !knownLine[pl] {
					pl = OtherProductLine
				}
				if pl == line {
					inLine = append(inLine, it)
				}
			}
			if len(inLine) == 0 {
				continue
			}
			b.WriteString("\n### " + line + "\n")
			cats := append(append([]string{}, GroupCategories...), "未分类")
			for _, cat := range cats {
				var inCat []GroupItem
				for _, it := range inLine {
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
				b.WriteString("#### " + cat + "\n")
				for _, it := range inCat {
					b.WriteString(itemLine(it))
				}
			}
		}
	}
	return strings.TrimRight(b.String(), "\n"), true
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

// GroupSourceChanged 判断组报生成后，纳入组员的定稿内容是否又变了。
func GroupSourceChanged(ws *store.Workspace, d *store.Draft) bool {
	if d.SourceHash == "" || len(d.Sources) == 0 {
		return false
	}
	h := sha256.New()
	for _, id := range d.Sources {
		fd, err := ws.FinalDraft(d.Week, id)
		if err != nil {
			return true
		}
		h.Write([]byte(id + "\n" + fd.Body + "\n\x00"))
	}
	return hex.EncodeToString(h.Sum(nil)) != d.SourceHash
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
