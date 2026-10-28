package core

// SE 按项目汇总：我是某项目的 SE 时，各组（DE）把本组在这个项目上的周报 / 月报
// 交给我，我合并成一份项目报。周报、月报同一套逻辑，周期编号区分。
//
// 来源有两种：
//   - 本工作台里整理的组（比如我自己的组）：直接引用该组这个项目的定稿组报，不用再贴；
//   - 其他 DE：把交来的原文粘贴归档到 reports/<周期>/project/<项目>/<组>.md。
//
// 合并不调 AI：按「总结 / 计划 / 其他事项」分节，节内按组列出，各组自己写的小标题
// 原样保留（类别是手填的，每期都可能变，不强行归到固定类别）。

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/axu4/kuibu/internal/store"
)

// 项目报收件状态。
const (
	// InboxLinked 表示直接引用本工作台该组的定稿组报。
	InboxLinked = "linked"
	// InboxDraft 表示本工作台该组的组报还没定稿。
	InboxDraft = "draft"
	// InboxSubmitted 表示其他 DE 已按时交来（粘贴归档）。
	InboxSubmitted = "submitted"
	// InboxLate 表示交来时已过截止时间。
	InboxLate = "late"
	// InboxMissing 表示还没交。
	InboxMissing = "missing"
)

// 项目报的三节。
const (
	projectSecSummary = "summary"
	projectSecPlan    = "plan"
	projectSecOther   = "other"
)

var projectSections = []string{projectSecSummary, projectSecPlan, projectSecOther}

// ProjectSectionTitle 返回项目报的节名：周报「本周总结 / 下周计划」，月报「本月总结 / 下月计划」。
func ProjectSectionTitle(period, section string) string {
	monthly := store.ValidMonth(period)
	switch section {
	case projectSecSummary:
		if monthly {
			return "本月总结"
		}
		return "本周总结"
	case projectSecPlan:
		if monthly {
			return "下月计划"
		}
		return "下周计划"
	}
	return "其他事项"
}

// ProjectInboxEntry 是某组在这个项目上的交报情况。
type ProjectInboxEntry struct {
	GroupID string `json:"groupId"`
	Group   string `json:"group"`
	// Lead 是该组 DE 姓名。
	Lead   string `json:"lead"`
	LeadID string `json:"leadId,omitempty"`
	// Status 见 Inbox* 常量。
	Status string `json:"status"`
	// SubmittedAt 是粘贴归档或组报定稿的时间。
	SubmittedAt *time.Time `json:"submittedAt,omitempty"`
	// Items 是可合并的条目数（各节列表项合计）。
	Items int `json:"items"`
}

// ProjectInbox 是 SE 视角某项目某期的收件情况。
type ProjectInbox struct {
	Period  string              `json:"period"`
	Monthly bool                `json:"monthly"`
	Project store.ReportProject `json:"project"`
	// Deadline 是迟交判定时刻（周报日 / 月报收集日当天 24:00）。
	Deadline time.Time           `json:"deadline"`
	Entries  []ProjectInboxEntry `json:"entries"`
	// Extra 是交过、但不在应交名单里的组（名单改过之后）。
	Extra  []ProjectInboxEntry `json:"extra"`
	Counts map[string]int      `json:"counts"`
}

// MineProjects 返回我是 SE 的项目。
func MineProjects(ws *store.Workspace) ([]store.ReportProject, error) {
	projects, err := ws.ReportProjects()
	if err != nil {
		return nil, err
	}
	out := make([]store.ReportProject, 0)
	for _, p := range projects {
		if p.Mine {
			out = append(out, p)
		}
	}
	return out, nil
}

// mineProject 找到项目并要求我是它的 SE。
func mineProject(ws *store.Workspace, idOrName string) (store.ReportProject, error) {
	project, ok := findProjectByIDOrName(ws, idOrName)
	if !ok {
		return store.ReportProject{}, fmt.Errorf("%w: 找不到报送项目 %q", store.ErrNotFound, idOrName)
	}
	if !project.Mine {
		return store.ReportProject{}, fmt.Errorf("%w: 我不是「%s」的 SE；先在项目设置里设为「我是 SE」", store.ErrInvalid, project.Name)
	}
	return project, nil
}

// periodDeadline 返回迟交判定时刻。
func periodDeadline(ws *store.Workspace, period string) (time.Time, error) {
	if store.ValidMonth(period) {
		return ws.MonthDeadline(period)
	}
	weekday, err := ws.ReportWeekday()
	if err != nil {
		return time.Time{}, err
	}
	return store.WeekDeadline(period, weekday)
}

// projectGroups 返回项目的应交组；项目没指定时为全部组。
func projectGroups(ws *store.Workspace, project store.ReportProject, now time.Time) ([]GroupInfo, error) {
	groups, err := ListGroups(ws, now)
	if err != nil {
		return nil, err
	}
	if len(project.Groups) == 0 {
		return groups, nil
	}
	byID := map[string]GroupInfo{}
	for _, g := range groups {
		byID[g.ID] = g
	}
	out := make([]GroupInfo, 0, len(project.Groups))
	for _, id := range project.Groups {
		if g, ok := byID[id]; ok {
			out = append(out, g)
		}
	}
	return out, nil
}

// linkedGroupReport 读取本工作台该组这个项目的组报（不要求定稿）；不存在返回 nil。
func linkedGroupReport(ws *store.Workspace, period, projectID string, g GroupInfo) *store.Draft {
	if g.LeadID == "" {
		return nil
	}
	d, err := ws.ReadGroupReportProject(period, projectID, g.LeadID)
	if err != nil {
		return nil
	}
	if d.Group != "" && d.Group != g.Name {
		return nil
	}
	return d
}

// LoadProjectInbox 列出 SE 某项目某期各组的交报情况。
func LoadProjectInbox(ws *store.Workspace, period, projectIDOrName string, now time.Time) (*ProjectInbox, error) {
	if err := store.ValidPeriod(period); err != nil {
		return nil, err
	}
	project, err := mineProject(ws, projectIDOrName)
	if err != nil {
		return nil, err
	}
	deadline, err := periodDeadline(ws, period)
	if err != nil {
		return nil, err
	}
	groups, err := projectGroups(ws, project, now)
	if err != nil {
		return nil, err
	}
	pasted, err := ws.ListProjectInbox(period, project.ID)
	if err != nil {
		return nil, err
	}
	byGroup := map[string]*store.Report{}
	for _, r := range pasted {
		byGroup[r.Person] = r
	}
	view := &ProjectInbox{
		Period: period, Monthly: store.ValidMonth(period), Project: project, Deadline: deadline,
		Entries: []ProjectInboxEntry{}, Extra: []ProjectInboxEntry{}, Counts: map[string]int{},
	}
	seen := map[string]bool{}
	for _, g := range groups {
		seen[g.ID] = true
		e := ProjectInboxEntry{GroupID: g.ID, Group: g.Name, Lead: g.LeadName, LeadID: g.LeadID, Status: InboxMissing}
		if r := byGroup[g.ID]; r != nil {
			fillPastedEntry(&e, r, deadline)
		} else if d := linkedGroupReport(ws, period, project.ID, g); d != nil {
			if d.Status == store.DraftStatusFinal {
				e.Status = InboxLinked
				t := d.FinalizedAt
				e.SubmittedAt = &t
				e.Items = projectItemCount(d.Body)
			} else {
				e.Status = InboxDraft
			}
		}
		view.Entries = append(view.Entries, e)
		view.Counts[e.Status]++
	}
	for _, r := range pasted {
		if seen[r.Person] {
			continue
		}
		e := ProjectInboxEntry{GroupID: r.Person, Group: r.Group, Lead: r.Name}
		if e.Group == "" {
			e.Group = r.Person
		}
		fillPastedEntry(&e, r, deadline)
		view.Extra = append(view.Extra, e)
	}
	view.Counts["expected"] = len(view.Entries)
	view.Counts["received"] = view.Counts[InboxLinked] + view.Counts[InboxSubmitted] + view.Counts[InboxLate]
	return view, nil
}

func fillPastedEntry(e *ProjectInboxEntry, r *store.Report, deadline time.Time) {
	e.Status = InboxSubmitted
	first := r.FirstSubmittedAt
	if first.IsZero() {
		first = r.SubmittedAt
	}
	if !deadline.IsZero() && first.After(deadline) {
		e.Status = InboxLate
	}
	t := r.SubmittedAt
	e.SubmittedAt = &t
	e.Items = projectItemCount(r.Body)
}

// ProjectInboxSource 是 SE 查看某组交来的原文。
type ProjectInboxSource struct {
	ProjectInboxEntry
	// Source 是 linked（引用定稿组报）或 paste（粘贴归档）。
	Source string `json:"source"`
	Body   string `json:"body"`
}

// ReadProjectInboxSource 读取某组交来的内容：粘贴的原文优先，否则引用定稿组报。
func ReadProjectInboxSource(ws *store.Workspace, period, projectIDOrName, groupID string, now time.Time) (*ProjectInboxSource, error) {
	view, err := LoadProjectInbox(ws, period, projectIDOrName, now)
	if err != nil {
		return nil, err
	}
	entry, ok := findInboxEntry(view, groupID)
	if !ok {
		return nil, fmt.Errorf("%w: 组 %q 不在「%s」的应交名单里", store.ErrNotFound, groupID, view.Project.Name)
	}
	out := &ProjectInboxSource{ProjectInboxEntry: entry}
	switch entry.Status {
	case InboxSubmitted, InboxLate:
		r, err := ws.ReadProjectInbox(period, view.Project.ID, entry.GroupID)
		if err != nil {
			return nil, err
		}
		out.Source, out.Body = "paste", r.Body
	case InboxLinked:
		d, err := ws.ReadGroupReportProject(period, view.Project.ID, entry.LeadID)
		if err != nil {
			return nil, err
		}
		out.Source, out.Body = InboxLinked, d.Body
	}
	return out, nil
}

func findInboxEntry(view *ProjectInbox, groupID string) (ProjectInboxEntry, bool) {
	for _, list := range [][]ProjectInboxEntry{view.Entries, view.Extra} {
		for _, e := range list {
			if e.GroupID == groupID || e.Group == groupID {
				return e, true
			}
		}
	}
	return ProjectInboxEntry{}, false
}

// SubmitProjectInbox 把某组 DE 交来的项目报粘贴归档。group 可以是组 id 或组名。
func SubmitProjectInbox(ws *store.Workspace, period, projectIDOrName, group, body string, now time.Time) (*store.Report, error) {
	if err := store.ValidPeriod(period); err != nil {
		return nil, err
	}
	project, err := mineProject(ws, projectIDOrName)
	if err != nil {
		return nil, err
	}
	groups, err := ListGroups(ws, now)
	if err != nil {
		return nil, err
	}
	var target *GroupInfo
	for i := range groups {
		if groups[i].ID == group || groups[i].Name == group {
			target = &groups[i]
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("%w: 找不到组 %q（先在组管理里加上这个组和它的 DE）", store.ErrNotFound, group)
	}
	return ws.WriteProjectInbox(store.ProjectInboxInput{
		Period: period, Project: project.ID, GroupID: target.ID, Group: target.Name, Lead: target.LeadName,
		Source: store.ReportSourcePaste, Body: body, Now: now,
	})
}

// DeleteProjectInbox 删除某组交来的项目报（移入历史），返回组 id。
func DeleteProjectInbox(ws *store.Workspace, period, projectIDOrName, group string, now time.Time) (string, error) {
	view, err := LoadProjectInbox(ws, period, projectIDOrName, now)
	if err != nil {
		return "", err
	}
	entry, ok := findInboxEntry(view, group)
	if !ok {
		return "", fmt.Errorf("%w: 找不到组 %q 交来的项目报", store.ErrNotFound, group)
	}
	return entry.GroupID, ws.DeleteProjectInbox(period, view.Project.ID, entry.GroupID)
}

// projectSource 是参与合并的一组内容。
type projectSource struct {
	GroupID string
	Group   string
	Body    string
}

// projectSources 收集可合并的来源与未纳入的组。
func projectSources(ws *store.Workspace, view *ProjectInbox) ([]projectSource, []string, error) {
	sources := make([]projectSource, 0)
	missing := make([]string, 0)
	for _, e := range view.Entries {
		switch e.Status {
		case InboxSubmitted, InboxLate, InboxLinked:
			src, err := readEntryBody(ws, view, e)
			if err != nil {
				return nil, nil, err
			}
			sources = append(sources, src)
		case InboxDraft:
			missing = append(missing, e.Group+"（组报未定稿）")
		default:
			missing = append(missing, e.Group+"（未交）")
		}
	}
	return sources, missing, nil
}

func readEntryBody(ws *store.Workspace, view *ProjectInbox, e ProjectInboxEntry) (projectSource, error) {
	src := projectSource{GroupID: e.GroupID, Group: e.Group}
	if e.Status == InboxLinked {
		d, err := ws.ReadGroupReportProject(view.Period, view.Project.ID, e.LeadID)
		if err != nil {
			return src, err
		}
		src.Body = d.Body
		return src, nil
	}
	r, err := ws.ReadProjectInbox(view.Period, view.Project.ID, e.GroupID)
	if err != nil {
		return src, err
	}
	src.Body = r.Body
	return src, nil
}

func projectSourceHash(sources []projectSource) string {
	h := sha256.New()
	for _, s := range sources {
		h.Write([]byte(s.GroupID + "\x00" + s.Body + "\x00"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// AssembleProjectReport 合并各组交来的内容，生成 SE 项目报草稿（不调 AI）。
//
// 有组没交或组报未定稿时，allowMissing 为 false 返回 ErrNotFinal 并列出名单。
// 已人工修改或已定稿的项目报，不带 force 返回 ErrHumanEdits。
func AssembleProjectReport(ws *store.Workspace, period, projectIDOrName string, allowMissing, force bool, now time.Time) (*store.Draft, error) {
	view, err := LoadProjectInbox(ws, period, projectIDOrName, now)
	if err != nil {
		return nil, err
	}
	sources, missing, err := projectSources(ws, view)
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("%w: 「%s」%s 还没有任何组交来项目报", store.ErrNotFinal, view.Project.Name, period)
	}
	if len(missing) > 0 && !allowMissing {
		return nil, fmt.Errorf("%w: 「%s」还有组没交齐：%s（确认先不纳入请加 --allow-missing / 点「仍然生成」）",
			store.ErrNotFinal, view.Project.Name, strings.Join(missing, "、"))
	}
	ids := make([]string, 0, len(sources))
	for _, s := range sources {
		ids = append(ids, s.GroupID)
	}
	if now.IsZero() {
		now = time.Now()
	}
	return ws.SaveGeneratedDraft(store.GeneratedDraft{
		Kind:       store.ProjectReportDirName,
		Sources:    ids,
		Missing:    missing,
		Person:     view.Project.ID,
		Name:       view.Project.Name,
		Week:       period,
		SourceHash: projectSourceHash(sources),
		Body:       RenderProjectReport(period, sources, missing),
		Now:        now,
	}, force)
}

// ProjectReportSourceChanged 判断项目报是否落后于当前各组交来的内容。
func ProjectReportSourceChanged(ws *store.Workspace, d *store.Draft, now time.Time) bool {
	if d == nil || d.SourceHash == "" {
		return false
	}
	view, err := LoadProjectInbox(ws, d.Week, d.Person, now)
	if err != nil {
		return true
	}
	sources, _, err := projectSources(ws, view)
	if err != nil {
		return true
	}
	return projectSourceHash(sources) != d.SourceHash
}

// RenderProjectReport 渲染项目报正文：节 → 组 → 该组原样的内容。
func RenderProjectReport(period string, sources []projectSource, missing []string) string {
	split := make([]map[string][]string, len(sources))
	for i, s := range sources {
		split[i] = splitProjectSections(s.Body)
	}
	var b strings.Builder
	if len(missing) > 0 {
		b.WriteString("> 未纳入：" + strings.Join(missing, "、") + "\n\n")
	}
	for _, sec := range projectSections {
		var part strings.Builder
		for i, s := range sources {
			lines := split[i][sec]
			if len(lines) == 0 {
				continue
			}
			part.WriteString("\n### " + s.Group + "\n\n")
			for _, l := range lines {
				part.WriteString(l + "\n")
			}
		}
		if part.Len() == 0 {
			if sec == projectSecOther {
				continue
			}
			part.WriteString("\n- 无\n")
		}
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n\n") {
			b.WriteString("\n")
		}
		b.WriteString("## " + ProjectSectionTitle(period, sec) + "\n")
		b.WriteString(part.String())
	}
	return b.String()
}

var (
	projectHeadingPattern = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	projectBoldPattern    = regexp.MustCompile(`^\*\*(.+?)\*\*[：:]?$`)
)

// projectSectionOf 判断一行标题属于哪一节；不是节标题返回空。
//
// 认 Markdown 标题、加粗行和「本周：」这类裸标签；标题文字要短，避免把正文误判成节。
func projectSectionOf(title string) string {
	t := strings.TrimSpace(strings.Trim(strings.TrimSpace(title), "：:"))
	if t == "" || len([]rune(t)) > 12 {
		return ""
	}
	if label := rawSectionLabel(t); label != "" {
		switch label {
		case SectionDone:
			return projectSecSummary
		case SectionPlan:
			return projectSecPlan
		}
		return projectSecOther
	}
	switch {
	case strings.Contains(t, "计划") || strings.HasPrefix(t, "下周") || strings.HasPrefix(t, "下月") || strings.HasPrefix(t, "下步"):
		return projectSecPlan
	case strings.Contains(t, "总结") || strings.Contains(t, "完成") || t == "本月" || t == "本周" || t == "本月工作" || t == "本周工作":
		return projectSecSummary
	case strings.Contains(t, "其他") || strings.Contains(t, "风险") || strings.Contains(t, "求助") || strings.Contains(t, "交办"):
		return projectSecOther
	}
	return ""
}

// splitProjectSections 把一份组报 / DE 交来的原文切成三节，节内的行原样保留；
// 原文里的小标题统一降到四级，放在「### 组名」之下。找不到任何节标题时整份算总结。
func splitProjectSections(body string) map[string][]string {
	out := map[string][]string{}
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	hasSection := false
	for _, raw := range lines {
		if _, sec := projectLineSection(strings.TrimSpace(raw)); sec != "" {
			hasSection = true
			break
		}
	}
	section := projectSecSummary
	if hasSection {
		section = ""
	}
	for _, raw := range lines {
		line := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimSpace(line)
		if isHeading, sec := projectLineSection(trimmed); sec != "" {
			section = sec
			continue
		} else if isHeading && section != "" {
			if m := projectHeadingPattern.FindStringSubmatch(trimmed); m != nil {
				line = "#### " + strings.TrimSpace(m[2])
			}
		}
		if section == "" || strings.HasPrefix(trimmed, ">") {
			continue
		}
		if trimmed == "" {
			if n := len(out[section]); n > 0 && out[section][n-1] != "" {
				out[section] = append(out[section], "")
			}
			continue
		}
		if t, ok := rawListItemText(trimmed); ok && (t == "无" || strings.HasPrefix(t, "本周没有归入") || strings.HasPrefix(t, "本月没有")) {
			continue
		}
		out[section] = append(out[section], line)
	}
	for sec, list := range out {
		for len(list) > 0 && list[len(list)-1] == "" {
			list = list[:len(list)-1]
		}
		if len(list) == 0 {
			delete(out, sec)
		} else {
			out[sec] = list
		}
	}
	return out
}

// projectLineSection 返回一行是否是标题行，以及它对应的节（非节标题为空）。
func projectLineSection(line string) (bool, string) {
	if m := projectHeadingPattern.FindStringSubmatch(line); m != nil {
		return true, projectSectionOf(m[2])
	}
	if m := projectBoldPattern.FindStringSubmatch(line); m != nil {
		return true, projectSectionOf(m[1])
	}
	if strings.HasSuffix(line, "：") || strings.HasSuffix(line, ":") || rawSectionLabel(line) != "" {
		return false, projectSectionOf(line)
	}
	return false, ""
}

// projectItemCount 统计可合并的列表项数。
func projectItemCount(body string) int {
	n := 0
	for _, lines := range splitProjectSections(body) {
		for _, l := range lines {
			if _, ok := rawListItemText(strings.TrimSpace(l)); ok {
				n++
			}
		}
	}
	return n
}

// ProjectGroupDelivery 是 DE 视角某个项目组报的去向。
type ProjectGroupDelivery struct {
	// Mine 表示我是这个项目的 SE：定稿后自动出现在 SE 汇总里，不用交。
	Mine bool `json:"mine"`
	// SE 是对方 SE 的称呼（Mine 为 false 时）。
	SE string `json:"se"`
	// DeliveredAt 是标记已交的时间，未交为空。
	DeliveredAt *time.Time `json:"deliveredAt,omitempty"`
	// Stale 表示标记已交之后又重新定稿过，需要重新交。
	Stale bool `json:"stale"`
}

// DeliveryOf 返回某份按项目组报的去向。
func DeliveryOf(project store.ReportProject, d *store.Draft) ProjectGroupDelivery {
	out := ProjectGroupDelivery{Mine: project.Mine, SE: project.SE}
	if d != nil && !d.DeliveredAt.IsZero() {
		t := d.DeliveredAt
		out.DeliveredAt = &t
		out.Stale = d.FinalizedAt.After(d.DeliveredAt) || d.Status != store.DraftStatusFinal
	}
	return out
}

// MarkProjectGroupDelivered 记录（或撤销）按项目组报已交给对方 SE。period 是周或月。
// 我自己是 SE 的项目不需要交，返回 ErrInvalid。
func MarkProjectGroupDelivered(ws *store.Workspace, period, group, projectIDOrName string, delivered bool, now time.Time) (*store.Draft, error) {
	project, ok := findProjectByIDOrName(ws, projectIDOrName)
	if !ok {
		return nil, fmt.Errorf("%w: 找不到报送项目 %q", store.ErrNotFound, projectIDOrName)
	}
	if project.Mine && delivered {
		return nil, fmt.Errorf("%w: 我就是「%s」的 SE，定稿后会自动进 SE 汇总，不用标记已交", store.ErrInvalid, project.Name)
	}
	lead, err := periodLead(ws, period, group)
	if err != nil {
		return nil, err
	}
	return ws.MarkDraftDelivered(store.GroupReportKind(project.ID), period, lead.ID, delivered, now)
}

// periodLead 按周或月解析组长。
func periodLead(ws *store.Workspace, period, group string) (*store.Person, error) {
	if store.ValidMonth(period) {
		lead, _, err := MonthLead(ws, period, group)
		return lead, err
	}
	lead, _, err := ResolveLead(ws, period, group)
	return lead, err
}
