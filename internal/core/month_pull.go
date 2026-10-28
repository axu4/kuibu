package core

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/axu4/kuibu/internal/store"
)

// 月报条目所属的原始节。
const (
	monthSectionSummary = "summary"
	monthSectionPlan    = "plan"
)

var (
	monthItemBulletPattern  = regexp.MustCompile(`^[-*+]\s+(.*)$`)
	monthItemPunctPattern   = regexp.MustCompile(`[\s，。、；：？！“”‘’（）()【】《》…—\-,.;:!?]+`)
	monthItemHeadingPattern = regexp.MustCompile(`^##\s+(.*)$`)
)

// MonthWeekProject 是本期某一周某个项目的周组报状态。
type MonthWeekProject struct {
	Project     string `json:"project"`
	ProjectName string `json:"projectName"`
	// Status 是 final / draft / missing。
	Status string `json:"status"`
	// Included 表示这份定稿周组报是否被纳入本期。
	Included bool `json:"included"`
	// Items 是周组报本周总结 + 下周计划的条目数，未定稿时为 0。
	Items int `json:"items"`
	// FinalizedAt 是定稿时间，未定稿为空。
	FinalizedAt string `json:"finalizedAt,omitempty"`
}

// MonthWeekRow 是本期周范围里的一周。
type MonthWeekRow struct {
	Week string `json:"week"`
	// ReportDay 是该周周报日的日期（YYYY-MM-DD）。
	ReportDay string `json:"reportDay"`
	// Default 表示按周报日归属本月的默认周。
	Default bool `json:"default"`
	// Scope 表示当前是否在本期周范围里。
	Scope bool `json:"scope"`
	// Side 是 prev / next / custom；默认周为空。
	Side string `json:"side,omitempty"`
	// Label 是相邻周的展示名（上月最后一周 / 下月第一周）。
	Label   string             `json:"label,omitempty"`
	Reports []MonthWeekProject `json:"reports"`
}

// MonthSourceView 是月报第 ① 步的来源视图。
type MonthSourceView struct {
	Month        string   `json:"month"`
	Group        string   `json:"group"`
	LeadID       string   `json:"leadId"`
	LeadName     string   `json:"leadName"`
	DefaultWeeks []string `json:"defaultWeeks"`
	// CandidateWeeks 是本期可以勾选的周：上月最后一周 + 默认周 + 下月第一周。
	CandidateWeeks []string              `json:"candidateWeeks"`
	ScopeWeeks     []string              `json:"scopeWeeks"`
	Weeks          []MonthWeekRow        `json:"weeks"`
	Projects       []store.ReportProject `json:"projects"`
	// Missing 是本期范围内未定稿 / 未提交的来源，形如「云平台项目 W44（未定稿）」。
	Missing []string `json:"missing"`
	// FinalSlots / IncludedSlots / Slots 是来源份数统计（按 项目 × 周）。
	FinalSlots    int `json:"finalSlots"`
	IncludedSlots int `json:"includedSlots"`
	Slots         int `json:"slots"`
	// CandidateTotal 是当前各项目候选条目数之和。
	CandidateTotal int `json:"candidateTotal"`
}

// MonthItem 是月报里的一条候选事项。
type MonthItem struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	// Section 是原始节：summary / plan。
	Section string `json:"section"`
	// Pool 是当前归口：summary / plan / archive / excluded。
	Pool      string   `json:"pool"`
	Weeks     []string `json:"weeks"`
	FirstWeek string   `json:"firstWeek"`
	LastWeek  string   `json:"lastWeek"`
	// Duplicated 是跨周重复的次数（weeks 的长度）。
	Duplicated int `json:"duplicated"`
}

// MonthCounts 是一个项目的候选统计。
type MonthCounts struct {
	Summary  int `json:"summary"`
	Plan     int `json:"plan"`
	Archive  int `json:"archive"`
	Excluded int `json:"excluded"`
	// Total 是 summary + plan，即会进入月报的候选数。
	Total int `json:"total"`
}

// MonthProjectCandidates 是一个项目的本月候选。
type MonthCandidatesData struct {
	Project     string      `json:"project"`
	ProjectName string      `json:"projectName"`
	Summary     []MonthItem `json:"summary"`
	Plan        []MonthItem `json:"plan"`
	Archive     []MonthItem `json:"archive"`
	Excluded    []MonthItem `json:"excluded"`
	// Missing 是本期范围内这个项目未定稿 / 未提交的周，形如「W44（未定稿）」。
	Missing []string `json:"missing"`
	// PlanFrom 是下月计划的来源周，没有来源时为空。
	PlanFrom string `json:"planFrom,omitempty"`
	// SourceWeeks 是本期实际纳入的周。
	SourceWeeks []string    `json:"sourceWeeks"`
	Counts      MonthCounts `json:"counts"`
}

// monthWeekWindows 返回某月按周报日归属的默认周，以及可选周（上月最后一周 + 默认周 + 下月第一周）。
func monthWeekWindows(ws *store.Workspace, month string) ([]string, []string, error) {
	weeks, err := monthWeeks(ws, month)
	if err != nil {
		return nil, nil, err
	}
	if len(weeks) == 0 {
		return nil, nil, fmt.Errorf("%w: %s 没有任何周", store.ErrInvalid, month)
	}
	firstMonday, _, err := store.WeekRange(weeks[0])
	if err != nil {
		return nil, nil, err
	}
	lastMonday, _, err := store.WeekRange(weeks[len(weeks)-1])
	if err != nil {
		return nil, nil, err
	}
	prev := store.WeekOf(firstMonday.AddDate(0, 0, -7))
	next := store.WeekOf(lastMonday.AddDate(0, 0, 7))
	candidates := make([]string, 0, len(weeks)+2)
	candidates = append(candidates, prev)
	candidates = append(candidates, weeks...)
	candidates = append(candidates, next)
	return weeks, candidates, nil
}

// monthContext 解析月份、组长、默认周和可选周。
func monthContext(ws *store.Workspace, month, group string) (*store.Person, string, []string, []string, error) {
	defaultWeeks, candidates, err := monthWeekWindows(ws, month)
	if err != nil {
		return nil, "", nil, nil, err
	}
	lead, groupName, err := MonthLead(ws, month, group)
	if err != nil {
		return nil, "", nil, nil, err
	}
	return lead, groupName, defaultWeeks, candidates, nil
}

// MonthCandidateWeeks 返回某月可选的周范围（上月最后一周 + 默认周 + 下月第一周）。
func MonthCandidateWeeks(ws *store.Workspace, month string) ([]string, error) {
	_, candidates, err := monthWeekWindows(ws, month)
	return candidates, err
}

// LoadMonthPull 读取月报拉取记录；没有时按默认周范围和所有定稿周组报初始化（不落盘）。
func LoadMonthPull(ws *store.Workspace, month, group string) (*store.MonthPull, error) {
	lead, groupName, defaultWeeks, _, err := monthContext(ws, month, group)
	if err != nil {
		return nil, err
	}
	pull, err := ws.ReadMonthPull(month, lead.ID)
	if err == nil {
		if pull.Group == "" {
			pull.Group = groupName
		}
		if pull.Lead == "" {
			pull.Lead = lead.ID
		}
		return pull, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	projects, err := ws.ReportProjects()
	if err != nil {
		return nil, err
	}
	pull = &store.MonthPull{
		Month:     month,
		Group:     groupName,
		Lead:      lead.ID,
		UpdatedAt: time.Now().Format(time.RFC3339),
		Weeks:     append([]string(nil), defaultWeeks...),
		Sources:   []store.MonthPullSource{},
		Overrides: []store.MonthPullOverride{},
	}
	for _, week := range defaultWeeks {
		for _, project := range projects {
			if monthReportStatus(ws, week, project.ID, lead.ID) == store.DraftStatusFinal {
				pull.Sources = append(pull.Sources, store.MonthPullSource{Week: week, Project: project.ID})
			}
		}
	}
	return pull, nil
}

// MonthSourceData 构建月报第 ① 步视图。
func MonthSourceData(ws *store.Workspace, month, group string) (*MonthSourceView, error) {
	lead, groupName, defaultWeeks, candidates, err := monthContext(ws, month, group)
	if err != nil {
		return nil, err
	}
	pull, err := LoadMonthPull(ws, month, group)
	if err != nil {
		return nil, err
	}
	projects, err := ws.ReportProjects()
	if err != nil {
		return nil, err
	}
	if len(projects) == 0 {
		return nil, fmt.Errorf("%w: 还没有配置报送项目，请先在周报设置里添加", store.ErrInvalid)
	}
	scope := monthWeekSet(pull.Weeks)
	defaultSet := monthWeekSet(defaultWeeks)
	weekday, err := ws.ReportWeekday()
	if err != nil {
		return nil, err
	}
	rows := make([]MonthWeekRow, 0, len(candidates))
	seen := map[string]bool{}
	appendWeek := func(week, side, label string) error {
		if seen[week] {
			return nil
		}
		seen[week] = true
		day, err := store.WeekReportDay(week, weekday)
		if err != nil {
			return err
		}
		row := MonthWeekRow{
			Week:      week,
			ReportDay: day.Format("2006-01-02"),
			Default:   defaultSet[week],
			Scope:     scope[week],
			Side:      side,
			Label:     label,
			Reports:   make([]MonthWeekProject, 0, len(projects)),
		}
		for _, project := range projects {
			r := MonthWeekProject{Project: project.ID, ProjectName: project.Name}
			d, status, err := monthReportDraft(ws, week, project.ID, lead.ID)
			if err != nil {
				return err
			}
			r.Status = status
			r.Included = monthSourceIncluded(pull, week, project.ID)
			if d != nil {
				r.Items = monthReportItemCount(d.Body)
				if !d.FinalizedAt.IsZero() {
					r.FinalizedAt = d.FinalizedAt.Format(time.RFC3339)
				}
			}
			row.Reports = append(row.Reports, r)
		}
		rows = append(rows, row)
		return nil
	}
	for _, week := range candidates {
		side, label := "", ""
		switch {
		case week == candidates[0] && week != defaultWeeks[0]:
			side, label = "prev", "上月最后一周"
		case week == candidates[len(candidates)-1] && week != defaultWeeks[len(defaultWeeks)-1]:
			side, label = "next", "下月第一周"
		}
		if err := appendWeek(week, side, label); err != nil {
			return nil, err
		}
	}
	// 兼容手工写入的任意周：不在候选列表里的范围周也展示出来。
	extra := make([]string, 0)
	for _, week := range pull.Weeks {
		if !seen[week] {
			extra = append(extra, week)
		}
	}
	sort.Strings(extra)
	for _, week := range extra {
		if err := appendWeek(week, "custom", "自定义周"); err != nil {
			return nil, err
		}
	}

	out := &MonthSourceView{
		Month:          month,
		Group:          groupName,
		LeadID:         lead.ID,
		LeadName:       lead.Name,
		DefaultWeeks:   defaultWeeks,
		CandidateWeeks: candidates,
		ScopeWeeks:     append([]string(nil), pull.Weeks...),
		Weeks:          rows,
		Projects:       projects,
		Missing:        []string{},
	}
	sort.Strings(out.ScopeWeeks)
	for _, row := range rows {
		if !row.Scope {
			continue
		}
		out.Slots += len(projects)
		out.FinalSlots += monthFinalSlots(row)
		for _, report := range row.Reports {
			if report.Included {
				out.IncludedSlots++
			}
			if report.Status != store.DraftStatusFinal {
				out.Missing = append(out.Missing, report.ProjectName+" "+shortWeek(row.Week)+"（"+monthStatusWord(report.Status)+"）")
			}
		}
	}
	sort.Strings(out.Missing)
	for _, project := range projects {
		c, err := monthProjectCandidates(ws, pull, project)
		if err != nil {
			return nil, err
		}
		out.CandidateTotal += c.Counts.Total
	}
	return out, nil
}

// SetMonthScope 修改本期周范围；新增周自动纳入其中的定稿周组报，移除周同时移除来源和归口。
func SetMonthScope(ws *store.Workspace, month, group string, weeks []string) (*store.MonthPull, *MonthSourceView, error) {
	lead, groupName, _, candidates, err := monthContext(ws, month, group)
	if err != nil {
		return nil, nil, err
	}
	allowed := monthWeekSet(candidates)
	next := make([]string, 0, len(weeks))
	seen := map[string]bool{}
	for _, week := range weeks {
		week = strings.TrimSpace(week)
		if week == "" {
			continue
		}
		if !allowed[week] {
			return nil, nil, fmt.Errorf("%w: %s 不在本期可选周范围里（上月最后一周 / 本月周 / 下月第一周）", store.ErrInvalid, week)
		}
		if seen[week] {
			continue
		}
		seen[week] = true
		next = append(next, week)
	}
	sort.Slice(next, func(i, j int) bool { return monthWeekIndex(candidates, next[i]) < monthWeekIndex(candidates, next[j]) })
	pull, err := LoadMonthPull(ws, month, group)
	if err != nil {
		return nil, nil, err
	}
	old := monthWeekSet(pull.Weeks)
	projects, err := ws.ReportProjects()
	if err != nil {
		return nil, nil, err
	}
	kept := make([]store.MonthPullSource, 0, len(pull.Sources))
	for _, src := range pull.Sources {
		if seen[src.Week] {
			kept = append(kept, src)
		}
	}
	for _, week := range next {
		if old[week] {
			continue
		}
		for _, project := range projects {
			if monthReportStatus(ws, week, project.ID, lead.ID) == store.DraftStatusFinal &&
				!monthSourceIncluded(pull, week, project.ID) {
				kept = append(kept, store.MonthPullSource{Week: week, Project: project.ID})
			}
		}
	}
	overrides := make([]store.MonthPullOverride, 0, len(pull.Overrides))
	for _, o := range pull.Overrides {
		if o.Week != "" && !seen[o.Week] {
			continue
		}
		overrides = append(overrides, o)
	}
	pull.Month = month
	pull.Group = groupName
	pull.Lead = lead.ID
	pull.Weeks = next
	pull.Sources = kept
	pull.Overrides = overrides
	pull.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := ws.WriteMonthPull(pull); err != nil {
		return nil, nil, err
	}
	data, err := MonthSourceData(ws, month, group)
	if err != nil {
		return nil, nil, err
	}
	return pull, data, nil
}

// SetMonthSource 纳入 / 去选一份定稿周组报。
func SetMonthSource(ws *store.Workspace, month, group, week, projectID string, included bool) (*store.MonthPull, error) {
	lead, _, _, candidates, err := monthContext(ws, month, group)
	if err != nil {
		return nil, err
	}
	pull, err := LoadMonthPull(ws, month, group)
	if err != nil {
		return nil, err
	}
	if !monthWeekSet(pull.Weeks)[week] {
		return nil, fmt.Errorf("%w: %s 不在本期周范围里", store.ErrInvalid, week)
	}
	if !monthWeekSet(candidates)[week] {
		return nil, fmt.Errorf("%w: %s 不是本期可选周", store.ErrInvalid, week)
	}
	project, ok := findProjectByIDOrName(ws, projectID)
	if !ok {
		return nil, fmt.Errorf("%w: 找不到报送项目 %q", store.ErrNotFound, projectID)
	}
	if included && monthReportStatus(ws, week, project.ID, lead.ID) != store.DraftStatusFinal {
		return nil, newReportErrorf(store.ErrNotFinal, "%s 的 %s 周组报尚未定稿，不能纳入月报", project.Name, week)
	}
	if included {
		if !monthSourceIncluded(pull, week, project.ID) {
			pull.Sources = append(pull.Sources, store.MonthPullSource{Week: week, Project: project.ID})
		}
	} else {
		kept := make([]store.MonthPullSource, 0, len(pull.Sources))
		for _, src := range pull.Sources {
			if src.Week == week && src.Project == project.ID {
				continue
			}
			kept = append(kept, src)
		}
		pull.Sources = kept
	}
	pull.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := ws.WriteMonthPull(pull); err != nil {
		return nil, err
	}
	return pull, nil
}

// MonthProjectCandidates 返回一个项目的本月候选（按当前拉取记录）。
func MonthProjectCandidates(ws *store.Workspace, month, group, projectID string) (*MonthCandidatesData, error) {
	project, ok := findProjectByIDOrName(ws, projectID)
	if !ok {
		return nil, fmt.Errorf("%w: 找不到报送项目 %q", store.ErrNotFound, projectID)
	}
	pull, err := LoadMonthPull(ws, month, group)
	if err != nil {
		return nil, err
	}
	return monthProjectCandidates(ws, pull, project)
}

func monthProjectCandidates(ws *store.Workspace, pull *store.MonthPull, project store.ReportProject) (*MonthCandidatesData, error) {
	lead, err := ws.ResolvePerson(pull.Lead)
	if err != nil {
		return nil, err
	}
	scope := append([]string(nil), pull.Weeks...)
	sort.Strings(scope)
	out := &MonthCandidatesData{
		Project:     project.ID,
		ProjectName: project.Name,
		Summary:     []MonthItem{},
		Plan:        []MonthItem{},
		Archive:     []MonthItem{},
		Excluded:    []MonthItem{},
		Missing:     []string{},
		SourceWeeks: []string{},
	}
	var sources []monthParsedSource
	for _, src := range pull.Sources {
		if src.Project != project.ID {
			continue
		}
		d, status, err := monthReportDraft(ws, src.Week, project.ID, lead.ID)
		if err != nil {
			return nil, err
		}
		if d == nil || status != store.DraftStatusFinal {
			continue
		}
		summary, plan := parseMonthReportBody(d.Body)
		sources = append(sources, monthParsedSource{Week: src.Week, Summary: summary, Plan: plan})
		out.SourceWeeks = append(out.SourceWeeks, src.Week)
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Week < sources[j].Week })
	sort.Strings(out.SourceWeeks)

	var summaryRaw, planRaw, archiveRaw []monthRawItem
	for i, src := range sources {
		order := 0
		for _, text := range src.Summary {
			summaryRaw = append(summaryRaw, monthRawItem{Text: text, Week: src.Week, Section: monthSectionSummary, Order: order})
			order++
		}
		if i == len(sources)-1 {
			for _, text := range src.Plan {
				planRaw = append(planRaw, monthRawItem{Text: text, Week: src.Week, Section: monthSectionPlan, Order: order})
				order++
			}
		} else {
			for _, text := range src.Plan {
				archiveRaw = append(archiveRaw, monthRawItem{Text: text, Week: src.Week, Section: monthSectionPlan, Order: order})
				order++
			}
		}
	}
	summaryGroups := makeMonthGroups(project.ID, monthSectionSummary, summaryRaw)
	planGroups := makeMonthGroups(project.ID, monthSectionPlan, planRaw)
	archiveGroups := makeMonthGroups(project.ID, monthSectionPlan, archiveRaw)
	planIDs := map[string]bool{}
	for _, g := range planGroups {
		planIDs[g.ID] = true
	}
	archiveGroups = filterMonthGroups(archiveGroups, func(g monthGroup) bool { return !planIDs[g.ID] })

	if len(sources) > 0 {
		out.PlanFrom = sources[len(sources)-1].Week
	}
	overrides := monthOverrideMap(pull, project.ID)
	summaryGroups = applyMonthPool(monthSectionSummary, store.MonthPoolSummary, summaryGroups, overrides)
	planGroups = applyMonthPool(monthSectionPlan, store.MonthPoolPlan, planGroups, overrides)
	archiveGroups = applyMonthPool(monthSectionPlan, store.MonthPoolArchive, archiveGroups, overrides)
	all := make([]monthGroup, 0, len(summaryGroups)+len(planGroups)+len(archiveGroups))
	all = append(all, summaryGroups...)
	all = append(all, planGroups...)
	all = append(all, archiveGroups...)
	all = sortMonthGroups(all)
	for _, g := range all {
		item := MonthItem{
			ID:         g.ID,
			Text:       g.Text,
			Section:    g.Section,
			Pool:       g.Pool,
			Weeks:      g.Weeks,
			FirstWeek:  g.FirstWeek,
			LastWeek:   g.LastWeek,
			Duplicated: len(g.Weeks),
		}
		switch g.Pool {
		case store.MonthPoolSummary:
			out.Summary = append(out.Summary, item)
		case store.MonthPoolPlan:
			out.Plan = append(out.Plan, item)
		case store.MonthPoolArchive:
			out.Archive = append(out.Archive, item)
		case store.MonthPoolExcluded:
			out.Excluded = append(out.Excluded, item)
		}
	}
	out.Counts = MonthCounts{
		Summary:  len(out.Summary),
		Plan:     len(out.Plan),
		Archive:  len(out.Archive),
		Excluded: len(out.Excluded),
	}
	out.Counts.Total = out.Counts.Summary + out.Counts.Plan
	for _, week := range scope {
		_, status, err := monthReportDraft(ws, week, project.ID, lead.ID)
		if err != nil {
			return nil, err
		}
		if status != store.DraftStatusFinal {
			out.Missing = append(out.Missing, shortWeek(week)+"（"+monthStatusWord(status)+"）")
		}
	}
	sort.Strings(out.Missing)
	return out, nil
}

// SetMonthItemPool 调整一条候选的归口。
func SetMonthItemPool(ws *store.Workspace, month, group, projectID, section, itemID, pool string) (*MonthCandidatesData, error) {
	if !store.ValidMonthPool(pool) {
		return nil, fmt.Errorf("%w: 非法月报归口 %q", store.ErrInvalid, pool)
	}
	if section != monthSectionSummary && section != monthSectionPlan {
		return nil, fmt.Errorf("%w: 非法月报节名 %q", store.ErrInvalid, section)
	}
	project, ok := findProjectByIDOrName(ws, projectID)
	if !ok {
		return nil, fmt.Errorf("%w: 找不到报送项目 %q", store.ErrNotFound, projectID)
	}
	pull, err := LoadMonthPull(ws, month, group)
	if err != nil {
		return nil, err
	}
	c, err := monthProjectCandidates(ws, pull, project)
	if err != nil {
		return nil, err
	}
	item, ok := findMonthItem(c, section, itemID)
	if !ok {
		return nil, fmt.Errorf("%w: 月报候选条目不存在: %s", store.ErrNotFound, itemID)
	}
	defaultPool := monthSectionSummary
	if section == monthSectionPlan {
		defaultPool = store.MonthPoolPlan
	}
	overrides := make([]store.MonthPullOverride, 0, len(pull.Overrides)+1)
	for _, o := range pull.Overrides {
		if o.Project == project.ID && o.Section == section && o.Item == itemID {
			continue
		}
		overrides = append(overrides, o)
	}
	if pool != defaultPool {
		overrides = append(overrides, store.MonthPullOverride{
			Project: project.ID,
			Week:    item.LastWeek,
			Section: section,
			Item:    itemID,
			Pool:    pool,
		})
	}
	pull.Overrides = overrides
	pull.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := ws.WriteMonthPull(pull); err != nil {
		return nil, err
	}
	return monthProjectCandidates(ws, pull, project)
}

// AssembleMonthProject 按当前拉取记录为某个项目生成确定性的月报草稿。
func AssembleMonthProject(ws *store.Workspace, month, group, projectID string, allowMissing, force bool, now time.Time) (*store.Draft, error) {
	project, ok := findProjectByIDOrName(ws, projectID)
	if !ok {
		return nil, fmt.Errorf("%w: 找不到报送项目 %q", store.ErrNotFound, projectID)
	}
	pull, err := LoadMonthPull(ws, month, group)
	if err != nil {
		return nil, err
	}
	if len(pull.Weeks) == 0 {
		return nil, fmt.Errorf("%w: 本期周范围为空，请先勾选至少一周", store.ErrInvalid)
	}
	c, err := monthProjectCandidates(ws, pull, project)
	if err != nil {
		return nil, err
	}
	if len(c.Missing) > 0 && !allowMissing {
		return nil, newReportErrorf(store.ErrNotFinal, "%s 月报有未纳入的来源：%s（确认先不纳入请加 --allow-missing）",
			project.Name, strings.Join(c.Missing, "、"))
	}
	// 把默认拉取记录落盘，后续「来源已更新」才有基线可比较。
	if err := ws.WriteMonthPull(pull); err != nil {
		return nil, err
	}
	_, _, defaultWeeks, _, err := monthContext(ws, month, group)
	if err != nil {
		return nil, err
	}
	body := renderMonthProjectBody(pull, c, defaultWeeks)
	sources := make([]string, 0)
	for _, src := range pull.Sources {
		if src.Project == project.ID {
			sources = append(sources, src.Week+"/"+src.Project)
		}
	}
	sort.Strings(sources)
	lead, err := ws.ResolvePerson(pull.Lead)
	if err != nil {
		return nil, err
	}
	h, err := monthProjectSourceHash(ws, pull, project)
	if err != nil {
		return nil, err
	}
	if now.IsZero() {
		now = time.Now()
	}
	return ws.SaveGeneratedDraft(store.GeneratedDraft{
		Kind:       store.GroupReportKind(project.ID),
		Group:      pull.Group,
		Sources:    sources,
		Missing:    c.Missing,
		Person:     lead.ID,
		Name:       lead.Name,
		Week:       month,
		SourceHash: h,
		Body:       body,
		Now:        now,
	}, force)
}

// MonthProjectSourceChanged 判断项目月报是否落后于当前拉取记录 / 周组报。
func MonthProjectSourceChanged(ws *store.Workspace, d *store.Draft) bool {
	if d == nil || d.SourceHash == "" {
		return false
	}
	projectID := store.ProjectIDFromGroupKind(d.Kind)
	if projectID == "" || !store.ValidMonth(d.Week) {
		return false
	}
	pull, err := ws.ReadMonthPull(d.Week, d.Person)
	if err != nil {
		return true
	}
	project, ok := findProjectByIDOrName(ws, projectID)
	if !ok {
		return true
	}
	h, err := monthProjectSourceHash(ws, pull, project)
	if err != nil {
		return true
	}
	return h != d.SourceHash
}

/* ============================== 内部工具 ============================== */

type monthParsedSource struct {
	Week    string
	Summary []string
	Plan    []string
}

type monthRawItem struct {
	Text    string
	Week    string
	Section string
	Order   int
}

type monthGroup struct {
	ID        string
	Text      string
	Section   string
	Pool      string
	Weeks     []string
	FirstWeek string
	LastWeek  string
	Order     int
}

func parseMonthReportBody(body string) (summary, plan []string) {
	section := ""
	for _, raw := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if m := monthItemHeadingPattern.FindStringSubmatch(line); m != nil {
			title := strings.TrimSpace(m[1])
			switch {
			case strings.Contains(title, "总结") || strings.Contains(title, "完成"):
				section = monthSectionSummary
			case strings.Contains(title, "计划"):
				section = monthSectionPlan
			default:
				section = ""
			}
			continue
		}
		if section == "" {
			continue
		}
		m := monthItemBulletPattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		text := strings.TrimSpace(m[1])
		if text == "" || text == "无" || strings.HasPrefix(text, "本周没有归入") || strings.HasPrefix(text, "本月没有") {
			continue
		}
		if section == monthSectionSummary {
			summary = append(summary, text)
		} else {
			plan = append(plan, text)
		}
	}
	return summary, plan
}

func monthReportItemCount(body string) int {
	summary, plan := parseMonthReportBody(body)
	return len(summary) + len(plan)
}

func makeMonthGroups(projectID, section string, items []monthRawItem) []monthGroup {
	byKey := map[string]*monthGroup{}
	order := 0
	for _, it := range items {
		key := normalizeMonthText(it.Text)
		if key == "" {
			continue
		}
		g, ok := byKey[key]
		if !ok {
			order++
			g = &monthGroup{
				ID:        MonthItemID(projectID, section, it.Text),
				Text:      it.Text,
				Section:   section,
				Pool:      poolForMonthSection(section),
				FirstWeek: it.Week,
				LastWeek:  it.Week,
				Order:     order,
			}
			byKey[key] = g
		} else {
			g.Text = it.Text
			g.LastWeek = it.Week
		}
		if !containsMonthString(g.Weeks, it.Week) {
			g.Weeks = append(g.Weeks, it.Week)
		}
	}
	out := make([]monthGroup, 0, len(byKey))
	for _, g := range byKey {
		sort.Slice(g.Weeks, func(i, j int) bool { return monthWeekLess(g.Weeks[i], g.Weeks[j]) })
		out = append(out, *g)
	}
	return sortMonthGroups(out)
}

func sortMonthGroups(groups []monthGroup) []monthGroup {
	out := append([]monthGroup(nil), groups...)
	sort.SliceStable(out, func(i, j int) bool {
		yi, wi := monthWeekParts(out[i].FirstWeek)
		yj, wj := monthWeekParts(out[j].FirstWeek)
		if yi != yj {
			return yi < yj
		}
		if wi != wj {
			return wi < wj
		}
		return out[i].Order < out[j].Order
	})
	return out
}

func filterMonthGroups(groups []monthGroup, keep func(monthGroup) bool) []monthGroup {
	out := make([]monthGroup, 0, len(groups))
	for _, g := range groups {
		if keep(g) {
			out = append(out, g)
		}
	}
	return out
}

func applyMonthPool(section, defaultPool string, groups []monthGroup, overrides map[string]string) []monthGroup {
	out := make([]monthGroup, 0, len(groups))
	for _, g := range groups {
		g.Pool = defaultPool
		if pool, ok := overrides[g.ID]; ok {
			g.Pool = pool
		}
		out = append(out, g)
	}
	return out
}

func monthOverrideMap(pull *store.MonthPull, projectID string) map[string]string {
	out := map[string]string{}
	if pull == nil {
		return out
	}
	for _, o := range pull.Overrides {
		if o.Project == projectID {
			out[o.Item] = o.Pool
		}
	}
	return out
}

func findMonthItem(c *MonthCandidatesData, section, itemID string) (MonthItem, bool) {
	if c == nil {
		return MonthItem{}, false
	}
	lists := [][]MonthItem{c.Summary, c.Plan, c.Archive, c.Excluded}
	for _, list := range lists {
		for _, it := range list {
			if it.ID == itemID && it.Section == section {
				return it, true
			}
		}
	}
	return MonthItem{}, false
}

func renderMonthProjectBody(pull *store.MonthPull, c *MonthCandidatesData, defaultWeeks []string) string {
	notes := make([]string, 0)
	if !sameStringSet(pull.Weeks, defaultWeeks) {
		notes = append(notes, "本期范围："+monthWeeksLabel(pull.Weeks)+"（自定义）")
	}
	if len(c.Missing) > 0 {
		notes = append(notes, "未纳入："+strings.Join(c.Missing, "、"))
	}
	if c.PlanFrom != "" {
		last := lastMonthWeek(pull.Weeks)
		if last != "" && c.PlanFrom != last {
			notes = append(notes, "下月计划取自 "+shortWeek(c.PlanFrom))
		}
	}
	var b strings.Builder
	if len(notes) > 0 {
		b.WriteString("> " + strings.Join(notes, " · ") + "\n\n")
	}
	b.WriteString("## 本月总结\n\n")
	if len(c.Summary) == 0 {
		b.WriteString("- 本月没有可纳入的总结条目。\n")
	} else {
		for _, it := range c.Summary {
			b.WriteString("- " + it.Text + "\n")
		}
	}
	b.WriteString("\n## 下月计划\n\n")
	if len(c.Plan) == 0 {
		b.WriteString("- 待补充下月计划。\n")
	} else {
		for _, it := range c.Plan {
			b.WriteString("- " + it.Text + "\n")
		}
	}
	return b.String()
}

func monthProjectSourceHash(ws *store.Workspace, pull *store.MonthPull, project store.ReportProject) (string, error) {
	c, err := monthProjectCandidates(ws, pull, project)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write([]byte(pull.Month + "\x00" + project.ID + "\x00"))
	weeks := append([]string(nil), pull.Weeks...)
	sort.Strings(weeks)
	for _, week := range weeks {
		h.Write([]byte("w:" + week + "\x00"))
	}
	sources := make([]string, 0)
	for _, src := range pull.Sources {
		if src.Project == project.ID {
			sources = append(sources, src.Week)
		}
	}
	sort.Strings(sources)
	for _, week := range sources {
		h.Write([]byte("s:" + week + "\x00"))
	}
	items := make([]MonthItem, 0, len(c.Summary)+len(c.Plan))
	items = append(items, c.Summary...)
	items = append(items, c.Plan...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].Section != items[j].Section {
			return items[i].Section < items[j].Section
		}
		return items[i].ID < items[j].ID
	})
	for _, it := range items {
		h.Write([]byte("i:" + it.Section + ":" + it.ID + ":" + it.Pool + ":" + it.Text + "\x00"))
	}
	for _, missing := range c.Missing {
		h.Write([]byte("m:" + missing + "\x00"))
	}
	h.Write([]byte("p:" + c.PlanFrom + "\x00"))
	return hex.EncodeToString(h.Sum(nil)), nil
}

func poolForMonthSection(section string) string {
	if section == monthSectionPlan {
		return store.MonthPoolPlan
	}
	return store.MonthPoolSummary
}

// MonthItemID 计算月报候选的稳定 id：sha256(项目 + 节 + 规范化文本) 前 8 位。
func MonthItemID(projectID, section, text string) string {
	norm := normalizeMonthText(text)
	sum := sha256.Sum256([]byte(projectID + "\n" + section + "\n" + norm))
	return hex.EncodeToString(sum[:])[:8]
}

func normalizeMonthText(text string) string {
	return monthItemPunctPattern.ReplaceAllString(strings.TrimSpace(text), "")
}

func monthReportStatus(ws *store.Workspace, week, projectID, leadID string) string {
	_, status, err := monthReportDraft(ws, week, projectID, leadID)
	if err != nil {
		return store.DraftStatusDraft
	}
	return status
}

func monthReportDraft(ws *store.Workspace, week, projectID, leadID string) (*store.Draft, string, error) {
	d, err := ws.ReadDraftKind(store.GroupReportKind(projectID), week, leadID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, "missing", nil
		}
		return nil, "", err
	}
	if d.Status == store.DraftStatusFinal {
		return d, store.DraftStatusFinal, nil
	}
	return d, store.DraftStatusDraft, nil
}

func monthSourceIncluded(pull *store.MonthPull, week, projectID string) bool {
	if pull == nil {
		return false
	}
	for _, src := range pull.Sources {
		if src.Week == week && src.Project == projectID {
			return true
		}
	}
	return false
}

func monthFinalSlots(row MonthWeekRow) int {
	n := 0
	for _, report := range row.Reports {
		if report.Status == store.DraftStatusFinal {
			n++
		}
	}
	return n
}

func monthStatusWord(status string) string {
	switch status {
	case store.DraftStatusFinal:
		return "已定稿"
	case store.DraftStatusDraft:
		return "未定稿"
	}
	return "未提交"
}

func monthWeekSet(weeks []string) map[string]bool {
	out := make(map[string]bool, len(weeks))
	for _, week := range weeks {
		out[week] = true
	}
	return out
}

func monthWeekIndex(weeks []string, week string) int {
	for i, w := range weeks {
		if w == week {
			return i
		}
	}
	return len(weeks)
}

func containsMonthString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as := append([]string(nil), a...)
	bs := append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

func lastMonthWeek(weeks []string) string {
	if len(weeks) == 0 {
		return ""
	}
	sorted := append([]string(nil), weeks...)
	sort.Slice(sorted, func(i, j int) bool { return monthWeekLess(sorted[i], sorted[j]) })
	return sorted[len(sorted)-1]
}

func monthWeekLess(a, b string) bool {
	ya, wa := monthWeekParts(a)
	yb, wb := monthWeekParts(b)
	if ya != yb {
		return ya < yb
	}
	return wa < wb
}

func monthWeekParts(week string) (int, int) {
	i := strings.LastIndex(week, "-W")
	if i < 0 {
		return 0, 0
	}
	year, _ := strconv.Atoi(week[:i])
	num, _ := strconv.Atoi(week[i+2:])
	return year, num
}

func monthWeekNumber(week string) int {
	_, n := monthWeekParts(week)
	return n
}

func shortWeek(week string) string {
	if i := strings.LastIndex(week, "-W"); i >= 0 {
		return week[i+1:]
	}
	return week
}

func monthWeeksLabel(weeks []string) string {
	if len(weeks) == 0 {
		return "—"
	}
	sorted := append([]string(nil), weeks...)
	sort.Slice(sorted, func(i, j int) bool { return monthWeekLess(sorted[i], sorted[j]) })
	type span struct{ start, end int }
	nums := make([]int, 0, len(sorted))
	for _, week := range sorted {
		nums = append(nums, monthWeekNumber(week))
	}
	spans := make([]span, 0)
	start, prev := nums[0], nums[0]
	for _, n := range nums[1:] {
		if n == prev+1 {
			prev = n
			continue
		}
		spans = append(spans, span{start, prev})
		start, prev = n, n
	}
	spans = append(spans, span{start, prev})
	parts := make([]string, 0, len(spans))
	for _, s := range spans {
		if s.start == s.end {
			parts = append(parts, "W"+strconv.Itoa(s.start))
		} else {
			parts = append(parts, "W"+strconv.Itoa(s.start)+"–W"+strconv.Itoa(s.end))
		}
	}
	return strings.Join(parts, "、")
}

func findProjectByIDOrName(ws *store.Workspace, idOrName string) (store.ReportProject, bool) {
	projects, err := ws.ReportProjects()
	if err != nil {
		return store.ReportProject{}, false
	}
	return store.FindReportProject(projects, idOrName)
}

func newReportErrorf(kind error, format string, args ...any) error {
	return fmt.Errorf("%w: %s", kind, fmt.Sprintf(format, args...))
}
