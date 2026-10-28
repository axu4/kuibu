package core

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/axu4/kuibu/internal/store"
)

// TriageItemDomainPattern 匹配归一稿条目的「【领域】」前缀。
var triageItemDomainPattern = regexp.MustCompile(`^【([^】]+)】\s*(.*)$`)

// triageParserVersion 用于让解析规则变化后重新生成筛选记录；改解析逻辑时递增。
const triageParserVersion = "2"

// TriageSections 是筛选的顺序：先本周总结（现有归一稿的「本周完成」），再下周计划，最后风险。
var TriageSections = []string{SectionDone, SectionPlan, SectionRisk}

// TriageSectionTitle 返回筛选/组报正文里展示的节名。
//
// 数据层沿用归一稿的固定节名「本周完成」，展示给组长用「本周总结」。
func TriageSectionTitle(section string) string {
	switch section {
	case SectionDone:
		return "本周总结"
	case SectionRisk:
		return "其他事项"
	}
	return section
}

// ReportSectionInfo 是录入阶段识别出的一节（用于前端预览/校正）。
type ReportSectionInfo struct {
	Section string   `json:"section"`
	Title   string   `json:"title"`
	Items   []string `json:"items"`
}

// ParseReportSections 识别组员原文里的本周总结 / 下周计划 / 其他事项。
//
// 只做确定性拆条，不调 AI；前端在录入时展示并允许人工改分节。
func ParseReportSections(body string) []ReportSectionInfo {
	bySection := parseTriageBody(body)
	out := make([]ReportSectionInfo, 0, len(TriageSections))
	for _, section := range TriageSections {
		items := make([]string, 0, len(bySection[section]))
		for _, item := range bySection[section] {
			if text := strings.TrimSpace(item.Text); text != "" {
				items = append(items, text)
			}
		}
		out = append(out, ReportSectionInfo{Section: section, Title: TriageSectionTitle(section), Items: items})
	}
	return out
}

// ReportBodyHash 返回周报原文的哈希，用于判断录入结构化是否还对应当前原文。
func ReportBodyHash(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// TriageItemID 计算一条筛选事项的稳定 id：sha256(人员 + 节名 + 文本) 前 8 位。
func TriageItemID(person, section, text string) string {
	sum := sha256.Sum256([]byte(person + "\n" + section + "\n" + strings.TrimSpace(text)))
	return hex.EncodeToString(sum[:])[:8]
}

// SplitTriageDomain 拆出条目的【领域】前缀；没有前缀时 domain 为空、text 原样返回。
func SplitTriageDomain(text string) (domain, body string) {
	text = strings.TrimSpace(text)
	if m := triageItemDomainPattern.FindStringSubmatch(text); m != nil {
		return strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
	}
	return "", text
}

// BuildTriage 从本周组员的归一稿（草稿或定稿）构建筛选记录，并保留已有决策。
//
// 与 PlanGroupReport 不同，这里不要求组员归一稿先定稿：你们组员本来就一条一条写，
// 筛选就是第一次人工过目；组报定稿是最终闸门。
func BuildTriage(ws *store.Workspace, week, group string) (*store.Triage, error) {
	lead, groupName, err := ResolveLead(ws, week, group)
	if err != nil {
		return nil, err
	}
	if groupName == "" {
		// 组长没填自己的组：从本周在职组员里推导唯一组名。
		groupName, err = inferLeaderGroup(ws, week)
		if err != nil {
			return nil, err
		}
	}
	projects, err := ws.ReportProjects()
	if err != nil {
		return nil, err
	}
	if len(projects) == 0 {
		return nil, fmt.Errorf("%w: 还没有配置报送项目，请先在周报设置里添加", store.ErrInvalid)
	}
	people, err := ws.ListPersons()
	if err != nil {
		return nil, err
	}
	monday, _, err := store.WeekRange(week)
	if err != nil {
		return nil, err
	}
	existing, exErr := ws.ReadTriage(week, lead.ID)
	if exErr != nil && !errors.Is(exErr, store.ErrNotFound) {
		return nil, exErr
	}
	old := map[string]store.TriageItem{}
	if existing != nil {
		for _, it := range existing.Items {
			old[it.ID] = it
		}
	}

	type memberSource struct {
		person *store.Person
		body   string
		stored *store.ReportSections
	}
	members := make([]memberSource, 0)
	missing := make([]string, 0)
	for _, p := range people {
		if !p.Active || p.Role != store.RoleMember {
			continue
		}
		g, _ := p.MembershipAt(monday)
		if g == "" {
			g = p.Group
		}
		if g != groupName {
			continue
		}
		// 优先读组员原文：三步流程不再依赖 AI 归一稿。原文拆不出条目时，
		// 再退回归一稿（草稿或定稿），兼容已经归一过的数据。
		body := ""
		rawBody := ""
		var stored *store.ReportSections
		if raw, err := ws.ReadReport(week, store.ReportLevelMember, p.ID); err == nil {
			rawBody = raw.Body
			// 录入时确认过的结构化条目优先；仍用原文哈希绑定，原文改了就不再用旧结构。
			if sec, err := ws.ReadReportSections(week, p.ID); err == nil && sec.SourceHash == ReportBodyHash(rawBody) {
				stored = sec
				body = rawBody
			} else if triageBodyLooksItemized(rawBody) {
				body = rawBody
			}
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		if body == "" {
			if d, err := ws.ReadDraft(week, p.ID); err == nil {
				body = d.Body
			} else if !errors.Is(err, store.ErrNotFound) {
				return nil, err
			}
		}
		if body == "" {
			// 没有归一稿、原文又不是典型小节/列表：仍按一行一条兜底。
			body = rawBody
		}
		bySection := parseTriageBody(body)
		if stored != nil && len(stored.Sections) > 0 {
			bySection = map[string][]DraftItem{}
			for _, sec := range stored.Sections {
				if !isTriageSection(sec.Section) {
					continue
				}
				for _, text := range sec.Items {
					bySection[sec.Section] = append(bySection[sec.Section], DraftItem{ID: ItemID(sec.Section, text), Text: text})
				}
			}
		}
		if len(bySection) == 0 {
			missing = append(missing, p.Name)
			continue
		}
		members = append(members, memberSource{person: p, body: body, stored: stored})
	}
	sort.Slice(members, func(i, j int) bool {
		if members[i].person.Name != members[j].person.Name {
			return members[i].person.Name < members[j].person.Name
		}
		return members[i].person.ID < members[j].person.ID
	})
	sort.Strings(missing)

	// 先按人员、节、原文顺序生成条目，再从旧记录里搬决策。
	projectIDs := map[string]bool{}
	for _, p := range projects {
		projectIDs[p.ID] = true
	}
	t := &store.Triage{
		Week:      week,
		Group:     groupName,
		Lead:      lead.ID,
		UpdatedAt: time.Now().Format(time.RFC3339),
		Projects:  make([]store.TriageProject, 0, len(projects)),
		Items:     make([]store.TriageItem, 0),
		Missing:   missing,
	}
	for _, p := range projects {
		t.Projects = append(t.Projects, store.TriageProject{ID: p.ID, Name: p.Name})
	}
	hash := sha256.New()
	hash.Write([]byte("triage-parser:" + triageParserVersion + "\x00"))
	order := 0
	for _, m := range members {
		hash.Write([]byte(m.person.ID + "\n" + m.body + "\n\x00"))
		bySection := parseTriageBody(m.body)
		if m.stored != nil {
			hash.Write([]byte("stored:" + m.stored.UpdatedAt + "\x00"))
			bySection = map[string][]DraftItem{}
			for _, sec := range m.stored.Sections {
				if !isTriageSection(sec.Section) {
					continue
				}
				for _, text := range sec.Items {
					bySection[sec.Section] = append(bySection[sec.Section], DraftItem{ID: ItemID(sec.Section, text), Text: text})
				}
			}
		}
		for _, section := range TriageSections {
			for _, item := range bySection[section] {
				domain, body := SplitTriageDomain(item.Text)
				if body == "" {
					continue
				}
				order++
				it := store.TriageItem{
					ID:      TriageItemID(m.person.ID, section, body),
					Person:  m.person.ID,
					Name:    m.person.Name,
					Section: section,
					Domain:  domain,
					Text:    body,
					Order:   order,
				}
				if prev, ok := old[it.ID]; ok && prev.Decision != "" {
					it.Decision = prev.Decision
					it.Project = prev.Project
					it.DecidedAt = prev.DecidedAt
					// 项目被删掉后，原归口失效：退回待定，避免静默改到别的项目。
					if it.Decision == store.TriageAccepted && !projectIDs[it.Project] {
						it.Decision = store.TriageSkipped
						it.Project = ""
					}
					if it.Decision != store.TriageAccepted {
						it.Project = ""
					}
				}
				t.Items = append(t.Items, it)
			}
		}
	}
	t.SourceHash = hex.EncodeToString(hash.Sum(nil))
	return t, nil
}

// LoadTriage 构建筛选记录；来源变化或还没有记录时写回，返回是否发生了重建。
func LoadTriage(ws *store.Workspace, week, group string) (*store.Triage, bool, error) {
	t, err := BuildTriage(ws, week, group)
	if err != nil {
		return nil, false, err
	}
	old, oldErr := ws.ReadTriage(week, t.Lead)
	changed := oldErr != nil || old.SourceHash != t.SourceHash || old.Group != t.Group || !sameTriageProjects(old.Projects, t.Projects)
	if changed {
		if err := ws.WriteTriage(t); err != nil {
			return nil, false, err
		}
	}
	return t, changed, nil
}

func sameTriageProjects(a, b []store.TriageProject) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func triageProjectList(t *store.Triage) []store.ReportProject {
	out := make([]store.ReportProject, 0, len(t.Projects))
	for _, p := range t.Projects {
		out = append(out, store.ReportProject{ID: p.ID, Name: p.Name})
	}
	return out
}

// SetTriageDecision 修改一条事项的决策；decision 为空时清除决策。
func SetTriageDecision(t *store.Triage, itemID, decision, projectID string, now time.Time) error {
	if t == nil {
		return fmt.Errorf("%w: 筛选记录为空", store.ErrInvalid)
	}
	idx := -1
	for i := range t.Items {
		if t.Items[i].ID == itemID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("%w: 筛选条目不存在: %s", store.ErrNotFound, itemID)
	}
	switch decision {
	case "":
		t.Items[idx].Decision = ""
		t.Items[idx].Project = ""
		t.Items[idx].DecidedAt = ""
	case store.TriageRejected, store.TriageSkipped:
		t.Items[idx].Decision = decision
		t.Items[idx].Project = ""
		t.Items[idx].DecidedAt = now.Format(time.RFC3339)
	case store.TriageAccepted:
		if _, ok := store.FindReportProject(triageProjectList(t), projectID); !ok {
			return fmt.Errorf("%w: 找不到报送项目 %q", store.ErrNotFound, projectID)
		}
		t.Items[idx].Decision = decision
		t.Items[idx].Project = projectID
		t.Items[idx].DecidedAt = now.Format(time.RFC3339)
	default:
		return fmt.Errorf("%w: 非法筛选决策 %q", store.ErrInvalid, decision)
	}
	t.UpdatedAt = now.Format(time.RFC3339)
	return nil
}

// TriageCounts 是筛选进度统计。
type TriageCounts struct {
	Total     int            `json:"total"`
	Done      int            `json:"done"`
	Accepted  int            `json:"accepted"`
	Rejected  int            `json:"rejected"`
	Skipped   int            `json:"skipped"`
	Undecided int            `json:"undecided"`
	ByProject map[string]int `json:"byProject"`
}

// CountTriage 统计筛选进度。
func CountTriage(t *store.Triage) TriageCounts {
	c := TriageCounts{ByProject: map[string]int{}}
	if t == nil {
		return c
	}
	for _, it := range t.Items {
		c.Total++
		switch it.Decision {
		case "":
			c.Undecided++
		case store.TriageAccepted:
			c.Done++
			c.Accepted++
			c.ByProject[it.Project]++
		case store.TriageRejected:
			c.Done++
			c.Rejected++
		case store.TriageSkipped:
			c.Done++
			c.Skipped++
		}
	}
	return c
}

// AssembleProjectGroup 按筛选结果生成某个项目的组报草稿。
//
// 只吃该项目 accepted 的条目，按 本周总结 / 下周计划 / 风险与求助 分节；正文不带姓名。
// 未判条目默认拒绝生成（ErrNeedsConfirm），allowUnreviewed 才继续。
func AssembleProjectGroup(ws *store.Workspace, week, group, projectIDOrName string, force, allowUnreviewed bool, now time.Time) (*store.Draft, error) {
	lead, groupName, err := ResolveLead(ws, week, group)
	if err != nil {
		return nil, err
	}
	t, err := ws.ReadTriage(week, lead.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("%w: 还没有筛选记录，请先在第 1 步逐条筛选", store.ErrNotFound)
		}
		return nil, err
	}
	if groupName == "" {
		groupName = t.Group
	}
	if t.Group != "" && t.Group != groupName {
		return nil, fmt.Errorf("%w: 筛选记录属于组「%s」，不是「%s」", store.ErrInvalid, t.Group, groupName)
	}
	// 先按当前配置找（改名后仍能用新名字/稳定 id），再退回筛选记录里的当周快照。
	project, ok := store.FindReportProject(wsProjects(ws), projectIDOrName)
	if !ok {
		project, ok = store.FindReportProject(triageProjectList(t), projectIDOrName)
	}
	if !ok {
		return nil, fmt.Errorf("%w: 找不到报送项目 %q", store.ErrNotFound, projectIDOrName)
	}
	c := CountTriage(t)
	if c.Undecided > 0 && !allowUnreviewed {
		return nil, fmt.Errorf("%w: 还有 %d 条未判，确认先不纳入请加 --allow-unreviewed", store.ErrNeedsConfirm, c.Undecided)
	}
	items := make([]store.TriageItem, 0)
	sources := map[string]bool{}
	for _, it := range t.Items {
		if it.Decision != store.TriageAccepted || it.Project != project.ID {
			continue
		}
		items = append(items, it)
		sources[it.Person] = true
	}
	body := renderProjectGroup(items)
	h := sha256.New()
	for _, it := range items {
		h.Write([]byte(it.ID + "\x00" + it.Text + "\x00"))
	}
	srcIDs := make([]string, 0, len(sources))
	for id := range sources {
		srcIDs = append(srcIDs, id)
	}
	sort.Strings(srcIDs)
	d, err := ws.SaveGeneratedDraft(store.GeneratedDraft{
		Kind:       store.GroupReportKind(project.ID),
		Group:      groupName,
		Sources:    srcIDs,
		Missing:    t.Missing,
		Person:     lead.ID,
		Name:       lead.Name,
		Week:       week,
		SourceHash: hex.EncodeToString(h.Sum(nil)),
		Model:      "",
		Body:       body,
		Now:        now,
	}, force)
	if err != nil {
		return nil, err
	}
	return d, nil
}

func renderProjectGroup(items []store.TriageItem) string {
	var b strings.Builder
	for _, section := range TriageSections {
		list := make([]store.TriageItem, 0)
		for _, it := range items {
			if it.Section == section {
				list = append(list, it)
			}
		}
		if len(list) == 0 {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("## " + TriageSectionTitle(section) + "\n")
		for _, it := range list {
			b.WriteString("- " + it.Text + "\n")
		}
	}
	if b.Len() == 0 {
		return "本周没有归入该项目的条目。\n"
	}
	return b.String()
}

// ReadTriage 读取本周筛选记录（存在时），供 App/CLI 展示。
func ReadTriage(ws *store.Workspace, week, group string) (*store.Triage, error) {
	lead, _, err := ResolveLead(ws, week, group)
	if err != nil {
		return nil, err
	}
	return ws.ReadTriage(week, lead.ID)
}

// ProjectGroupSourceChanged 判断按项目生成的组报是否已经落后于当前筛选结果。
func ProjectGroupSourceChanged(ws *store.Workspace, d *store.Draft) bool {
	if d == nil || d.SourceHash == "" {
		return false
	}
	projectID := store.ProjectIDFromGroupKind(d.Kind)
	if projectID == "" {
		return GroupSourceChanged(ws, d)
	}
	t, err := ws.ReadTriage(d.Week, d.Person)
	if err != nil {
		return true
	}
	h := sha256.New()
	for _, it := range t.Items {
		if it.Decision != store.TriageAccepted || it.Project != projectID {
			continue
		}
		h.Write([]byte(it.ID + "\x00" + it.Text + "\x00"))
	}
	return hex.EncodeToString(h.Sum(nil)) != d.SourceHash
}

func wsProjects(ws *store.Workspace) []store.ReportProject {
	projects, err := ws.ReportProjects()
	if err != nil {
		return nil
	}
	return projects
}

func isTriageSection(section string) bool {
	for _, s := range TriageSections {
		if s == section {
			return true
		}
	}
	return false
}

// parseTriageBody 从组员原文/归一稿里拆条目。
//
// 优先用 ParseDraftItems（识别「## 本周完成/本周总结/下周计划/风险与求助」）；
// 原文没有 Markdown 标题时退化为按行解析，识别常见节名，未识别到标题的列表项
// 归入「本周完成」，保证已经一条一条写的原文也能直接筛选。
func parseTriageBody(body string) map[string][]DraftItem {
	out := ParseDraftItems(body)
	if len(out) > 0 {
		return out
	}
	out = map[string][]DraftItem{}
	lines := strings.Split(body, "\n")
	// 组员的原文常见「本周：」「下周：」这种没有 Markdown 标题的写法。
	// 先判断是否存在可识别的小节，存在时跳过第一个小节之前的内容（姓名/项目行）。
	hasSection := false
	for _, line := range lines {
		if rawSectionLabel(line) != "" {
			hasSection = true
			break
		}
	}
	section := SectionDone
	if hasSection {
		section = ""
	}
	seen := map[string]bool{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "AI经验") || strings.HasPrefix(trimmed, "AI 经验") {
			section = ""
			continue
		}
		if label := rawSectionLabel(trimmed); label != "" {
			section = label
			continue
		}
		if section == "" {
			continue
		}
		text, ok := rawListItemText(trimmed)
		if !ok {
			// 没有列表前缀时按一行一条；跳过明显的标题/项目行。
			if strings.HasPrefix(trimmed, "【周报】") || strings.HasPrefix(trimmed, "#") || strings.HasSuffix(trimmed, "：") {
				continue
			}
			text = trimmed
		}
		text = strings.TrimSpace(text)
		if text == "" || text == "无" {
			continue
		}
		id := ItemID(section, text)
		if seen[id] {
			continue
		}
		seen[id] = true
		out[section] = append(out[section], DraftItem{ID: id, Text: text})
	}
	return out
}

// rawSectionLabel 识别组员原文里常见的小节写法；返回空表示不是小节行。
//
// 除了「本周总结/下周计划」，也认「本周：」「下周：」「本周工作」「风险」「求助」
// 和「AI经验」；AI经验 不属于三步流程，返回的 section 为空以跳过其内容。
func rawSectionLabel(line string) string {
	t := strings.TrimSpace(line)
	t = strings.TrimSpace(strings.Trim(t, "：:"))
	switch {
	case t == "本周" || t == "本周总结" || t == "本周完成" || t == "本周工作" || t == "本周完成事项":
		return SectionDone
	case t == "下周" || t == "下周计划" || t == "下周工作" || t == "下周计划事项" || t == "下步计划":
		return SectionPlan
	case t == "风险" || t == "风险与求助" || t == "求助" || t == "其他" || t == "其他事项" || t == "其他工作" || t == "领导交办":
		return SectionRisk
	case strings.HasPrefix(t, "AI经验") || strings.HasPrefix(t, "AI 经验"):
		return ""
	}
	return ""
}

// rawListItemText 识别「- / * / 1. / 1、/ 1）」等列表项。
func rawListItemText(line string) (string, bool) {
	for _, p := range []string{"- ", "* ", "+ "} {
		if strings.HasPrefix(line, p) {
			return strings.TrimSpace(line[len(p):]), true
		}
	}
	// 中文序号：1、 1） 1) 1． 1.
	for _, sep := range []string{"、", "）", ")", "．", "."} {
		if i := strings.Index(line, sep); i > 0 && i <= 3 {
			digits := strings.TrimSpace(line[:i])
			if digits != "" && strings.Trim(digits, "0123456789") == "" {
				return strings.TrimSpace(line[i+len(sep):]), true
			}
		}
	}
	return "", false
}

// triageBodyLooksItemized 判断原文是否有可用的结构：Markdown 小节、常见节名或列表项。
func triageBodyLooksItemized(body string) bool {
	if len(ParseDraftItems(body)) > 0 {
		return true
	}
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "+ ") {
			return true
		}
		if strings.Contains(trimmed, "本周总结") || strings.Contains(trimmed, "本周完成") ||
			strings.Contains(trimmed, "下周计划") || strings.Contains(trimmed, "风险") {
			return true
		}
	}
	return false
}

// inferLeaderGroup 从本周在职组员里推导唯一组名；组长自己的 group 为空时使用。
func inferLeaderGroup(ws *store.Workspace, week string) (string, error) {
	people, err := ws.ListPersons()
	if err != nil {
		return "", err
	}
	monday, _, err := store.WeekRange(week)
	if err != nil {
		return "", err
	}
	groups := map[string]bool{}
	for _, p := range people {
		if !p.Active || p.Role != store.RoleMember {
			continue
		}
		g, _ := p.MembershipAt(monday)
		if g == "" {
			g = p.Group
		}
		if g != "" {
			groups[g] = true
		}
	}
	switch len(groups) {
	case 0:
		return "", nil
	case 1:
		for g := range groups {
			return g, nil
		}
	}
	return "", fmt.Errorf("%w: 组长没有填组，且名单里有多个组；请用 --group 指定要汇总的组", store.ErrInvalid)
}
