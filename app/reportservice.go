package main

import (
	"context"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/axu4/kuibu/internal/core"
	"github.com/axu4/kuibu/internal/index"
	"github.com/axu4/kuibu/internal/store"
)

// reportChangedEvent 是组员或周报文件变化时发给前端的事件名。
const reportChangedEvent = "report:changed"

// ReportChangeEvent 是 report:changed 事件的数据。
type ReportChangeEvent struct {
	// PeopleIDs 是发生变化的组员 ID 列表。
	PeopleIDs []string `json:"peopleIds"`
	// ReportIDs 是发生变化的周报 ID 列表（week/level/person）。
	ReportIDs []string `json:"reportIds"`
}

// ReportWeek 是一周的信息。
type ReportWeek struct {
	// Week 是 ISO 周编号，形如 2026-W44。
	Week string `json:"week"`
	// Start 是该周周一（YYYY-MM-DD）。
	Start string `json:"start"`
	// End 是该周周日（YYYY-MM-DD）。
	End string `json:"end"`
	// ReportDay 是周报日（1=周一 … 7=周日）。
	ReportDay int `json:"reportDay"`
	// IsCurrent 表示是否是当前周。
	IsCurrent bool `json:"isCurrent"`
}

// ReportCounts 是提交状态的汇总计数。
type ReportCounts struct {
	// Expected 是应交人数。
	Expected int `json:"expected"`
	// Submitted 是按时已交人数。
	Submitted int `json:"submitted"`
	// Late 是迟交人数。
	Late int `json:"late"`
	// Missing 是未交人数。
	Missing int `json:"missing"`
	// Extra 是有提交但不在应交名单里的人数。
	Extra int `json:"extra"`
	// Total 是名单与名单外的总数。
	Total int `json:"total"`
}

// ReportPerson 是提交状态里的一个人。
type ReportPerson struct {
	// PersonID 是组员 ID。
	PersonID string `json:"personId"`
	// Name 是姓名。
	Name string `json:"name"`
	// Group 是该周周一时所属组。
	Group string `json:"group"`
	// ProductLines 是该周周一时所属产品线。
	ProductLines []string `json:"productLines"`
	// Role 是角色：member / lead。
	Role string `json:"role"`
	// Active 表示是否在职。
	Active bool `json:"active"`
	// Status 是 submitted / late / missing。
	Status string `json:"status"`
	// FirstSubmittedAt 是首次提交时间（RFC3339），missing 时为空。
	FirstSubmittedAt string `json:"firstSubmittedAt"`
	// SubmittedAt 是最近一次提交时间（RFC3339），missing 时为空。
	SubmittedAt string `json:"submittedAt"`
	// Normalized 是归一稿状态 none / draft / final，组长层级为空。
	Normalized string `json:"normalized"`
	// SourceChanged 表示归一后原文又被重新提交。
	SourceChanged bool `json:"sourceChanged"`
	// FromGroup 表示组长周报由定稿组报带入，本身就是定稿。
	FromGroup bool `json:"fromGroup"`
	// Alignment 是对照上周计划的对齐率 0~1，没有可对照数据时为 null。
	Alignment *float64 `json:"alignment"`
}

// ReportGroupCounts 是一个组的提交进度。
type ReportGroupCounts struct {
	// Group 是组名，空组名由前端显示为「未分组」。
	Group string `json:"group"`
	// Expected 是该组应交人数。
	Expected int `json:"expected"`
	// Submitted 是该组按时已交人数。
	Submitted int `json:"submitted"`
	// Late 是该组迟交人数。
	Late int `json:"late"`
	// Missing 是该组未交人数。
	Missing int `json:"missing"`
}

// ReportStatusData 是一次提交状态统计。
type ReportStatusData struct {
	// Week 是 ISO 周编号。
	Week string `json:"week"`
	// Level 是层级：member / lead。
	Level string `json:"level"`
	// Counts 是汇总计数。
	Counts ReportCounts `json:"counts"`
	// Entries 是应交名单及状态。
	Entries []ReportPerson `json:"entries"`
	// Extra 是有提交但不在应交名单里的人。
	Extra []ReportPerson `json:"extra"`
	// Groups 是按组汇总的应交进度，只统计应交名单，未分组（空组名）排在最后。
	Groups []ReportGroupCounts `json:"groups"`
}

// ReportData 是一份周报的原文与元数据。
type ReportData struct {
	// Person 是组员 ID。
	Person string `json:"person"`
	// Name 是提交时姓名快照。
	Name string `json:"name"`
	// Week 是 ISO 周编号。
	Week string `json:"week"`
	// Level 是层级：member / lead。
	Level string `json:"level"`
	// Group 是提交时所属组快照。
	Group string `json:"group"`
	// ProductLines 是提交时产品线快照。
	ProductLines []string `json:"productLines"`
	// Source 是来源：paste / cli。
	Source string `json:"source"`
	// FirstSubmittedAt 是首次提交时间（RFC3339）。
	FirstSubmittedAt string `json:"firstSubmittedAt"`
	// SubmittedAt 是最近一次提交时间（RFC3339）。
	SubmittedAt string `json:"submittedAt"`
	// HeaderDate 是原文头行里的日期，可为空。
	HeaderDate string `json:"headerDate"`
	// Body 是原文（只去首尾空行）。
	Body string `json:"body"`
}

// ReportPreview 是一份粘贴文本的识别预览结果。
type ReportPreview struct {
	// HasHeader 表示该段是否以【周报】头行开头。
	HasHeader bool `json:"hasHeader"`
	// Name 是头行里的姓名。
	Name string `json:"name"`
	// Date 是头行里的日期。
	Date string `json:"date"`
	// DateInvalid 表示头行日期无法解析。
	DateInvalid bool `json:"dateInvalid"`
	// Week 是识别出的 ISO 周。
	Week string `json:"week"`
	// Level 是层级。
	Level string `json:"level"`
	// PersonID 是匹配到的人员 ID，为空表示未匹配或需用户选择。
	PersonID string `json:"personId"`
	// PersonName 是匹配到的人员姓名。
	PersonName string `json:"personName"`
	// Overwrite 表示是否会覆盖已有提交。
	Overwrite bool `json:"overwrite"`
	// Late 表示现在提交是否会迟交。
	Late bool `json:"late"`
	// Duplicate 表示同一次粘贴里同一 person+week+level 的非最后一份。
	Duplicate bool `json:"duplicate"`
	// Reason 是无法识别的原因。
	Reason string `json:"reason"`
	// StartLine 与 EndLine 是该段在原文里的起止行号。
	StartLine int `json:"startLine"`
	EndLine   int `json:"endLine"`
	// Text 是该段原文。
	Text string `json:"text"`
}

// SubmitItem 是一次提交的输入。
type SubmitItem struct {
	// Text 是周报原文。
	Text string `json:"text"`
	// Person 是组员 ID 或姓名/别名。
	Person string `json:"person"`
	// Week 是 ISO 周编号。
	Week string `json:"week"`
	// Level 是层级：member / lead。
	Level string `json:"level"`
}

// PersonData 是一名组员的前端结构。
type PersonData struct {
	// ID 是组员标识。
	ID string `json:"id"`
	// Name 是姓名。
	Name string `json:"name"`
	// Aliases 是别名列表。
	Aliases []string `json:"aliases"`
	// Group 是当前所属组。
	Group string `json:"group"`
	// ProductLines 是当前产品线。
	ProductLines []string `json:"productLines"`
	// Role 是角色：member / lead。
	Role string `json:"role"`
	// Active 表示是否在职。
	Active bool `json:"active"`
	// Created 是创建时间（RFC3339）。
	Created string `json:"created"`
	// Updated 是最后更新时间（RFC3339）。
	Updated string `json:"updated"`
}

// PersonInput 是新增 / 修改组员时传入的字段。
type PersonInput struct {
	// Name 是姓名。
	Name string `json:"name"`
	// Aliases 是别名列表。
	Aliases []string `json:"aliases"`
	// Group 是所属组。
	Group string `json:"group"`
	// ProductLines 是产品线。
	ProductLines []string `json:"productLines"`
	// Role 是角色：member / lead，空视为 member。
	Role string `json:"role"`
}

// ReportService 是暴露给前端的周报收集服务。
//
// 所有读写都走 internal/store 与 internal/core，本层只做参数校验、聚合与
// 事件转发。索引沿用 WorkspaceService 的实例，写前先增量同步保持新鲜。
type ReportService struct {
	// workspace 用于复用当前打开的工作台与索引实例。
	workspace *WorkspaceService

	// syncMu 保护 lastSync，把「读前同步」做成短时间内的 single-flight。
	syncMu   sync.Mutex
	lastSync time.Time

	// now 可在测试里替换，默认系统时间。
	now func() time.Time
	// emit 可在测试里替换，默认走 WorkspaceService.emit。
	emit func(name string, data any)
	// env 读取大模型配置（KUIBU_CONFIG_DIR），测试可替换，默认 os.Getenv。
	env func(string) string
}

// newReportService 创建周报服务，复用 WorkspaceService 的工作台与索引。
func newReportService(workspace *WorkspaceService) *ReportService {
	return &ReportService{
		workspace: workspace,
		now:       time.Now,
	}
}

// ServiceName 返回服务名，供 Wails 日志与绑定使用。
func (s *ReportService) ServiceName() string {
	return "ReportService"
}

// clock 返回当前时间。
func (s *ReportService) clock() time.Time {
	now := time.Now
	if s.now != nil {
		now = s.now
	}
	return now()
}

// syncInterval 是同一次界面刷新里跳过重复同步的时间窗口。
const reportSyncInterval = 50 * time.Millisecond

// instances 返回当前工作台与索引，并先做一次增量同步。
func (s *ReportService) instances() (*store.Workspace, *index.Index, error) {
	if s.workspace == nil {
		return nil, nil, errNotOpen()
	}
	ws := s.workspace.workspace()
	idx := s.workspace.index()
	if ws == nil || idx == nil {
		return nil, nil, errNotOpen()
	}
	s.syncIndex(idx, false)
	return ws, idx, nil
}

// syncIndex 增量同步索引；force 为 false 时，刚同步过则跳过。
func (s *ReportService) syncIndex(idx *index.Index, force bool) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	if !force && time.Since(s.lastSync) < reportSyncInterval {
		return
	}
	s.lastSync = time.Now()
	if _, err := idx.Sync(context.Background()); err != nil {
		log.Printf("kuibu: 同步索引失败（继续使用旧索引）: %v", err)
	}
}

// emitReport 发送 report:changed 事件；切片保证非 nil，避免前端拿到 null。
func (s *ReportService) emitReport(peopleIDs, reportIDs []string) {
	if peopleIDs == nil {
		peopleIDs = []string{}
	}
	if reportIDs == nil {
		reportIDs = []string{}
	}
	event := ReportChangeEvent{PeopleIDs: peopleIDs, ReportIDs: reportIDs}
	if s.emit != nil {
		s.emit(reportChangedEvent, event)
		return
	}
	if s.workspace != nil {
		s.workspace.emit(reportChangedEvent, event)
	}
}

// Week 返回指定周的信息。ref 为 ""/this/last/next/周编号/日期。
func (s *ReportService) Week(ref string) (ReportWeek, error) {
	ws, _, err := s.instances()
	if err != nil {
		return ReportWeek{}, err
	}
	if strings.TrimSpace(ref) == "" {
		ref = "this"
	}
	week, err := store.ParseWeek(ref, s.clock())
	if err != nil {
		return ReportWeek{}, err
	}
	return s.weekInfo(ws, week), nil
}

// ShiftWeek 把给定周前后平移 delta 周（负数向前）。
func (s *ReportService) ShiftWeek(week string, delta int) (ReportWeek, error) {
	ws, _, err := s.instances()
	if err != nil {
		return ReportWeek{}, err
	}
	monday, _, err := store.WeekRange(week)
	if err != nil {
		return ReportWeek{}, err
	}
	target := monday.AddDate(0, 0, delta*7)
	return s.weekInfo(ws, store.WeekOf(target)), nil
}

// weekInfo 组装周信息。
func (s *ReportService) weekInfo(ws *store.Workspace, week string) ReportWeek {
	monday, sunday, err := store.WeekRange(week)
	if err != nil {
		monday, sunday = time.Time{}, time.Time{}
	}
	weekday, err := ws.ReportWeekday()
	if err != nil || weekday < 1 || weekday > 7 {
		weekday = store.DefaultReportWeekday
	}
	info := ReportWeek{
		Week:      week,
		ReportDay: weekday,
		IsCurrent: store.WeekOf(s.clock()) == week,
	}
	if !monday.IsZero() {
		info.Start = monday.Format("2006-01-02")
		info.End = sunday.Format("2006-01-02")
	}
	return info
}

// Status 返回某周某层级的提交状态与计数。
func (s *ReportService) Status(week, level, group, productLine string) (ReportStatusData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return ReportStatusData{}, err
	}
	if level == "" {
		level = store.ReportLevelMember
	}
	if strings.TrimSpace(week) == "" {
		week = store.WeekOf(s.clock())
	}
	st, err := ws.ReportStatus(week, level, group, productLine, s.clock())
	if err != nil {
		return ReportStatusData{}, err
	}
	out := ReportStatusData{
		Week:  st.Week,
		Level: st.Level,
		Counts: ReportCounts{
			Expected:  st.Counts["expected"],
			Submitted: st.Counts["submitted"],
			Late:      st.Counts["late"],
			Missing:   st.Counts["missing"],
			Extra:     st.Counts["extra"],
			Total:     st.Counts["total"],
		},
		Entries: make([]ReportPerson, 0, len(st.Entries)),
		Extra:   make([]ReportPerson, 0, len(st.Extra)),
	}
	for _, e := range st.Entries {
		out.Entries = append(out.Entries, s.withAlignment(ws, week, level, toReportPerson(e)))
	}
	for _, e := range st.Extra {
		out.Extra = append(out.Extra, s.withAlignment(ws, week, level, toReportPerson(e)))
	}
	out.Groups = groupCounts(out.Entries)
	return out, nil
}

// groupCounts 按组汇总应交名单的提交进度。
//
// 只统计应交名单（entries），名单外（extra）不计入任何一个组；未分组（空组名）
// 排在最后，其余按组名升序，保证同一份名单每次展示的顺序一致。
func groupCounts(entries []ReportPerson) []ReportGroupCounts {
	byGroup := make(map[string]*ReportGroupCounts)
	for _, e := range entries {
		key := strings.TrimSpace(e.Group)
		item, ok := byGroup[key]
		if !ok {
			item = &ReportGroupCounts{Group: key}
			byGroup[key] = item
		}
		item.Expected++
		switch e.Status {
		case "submitted":
			item.Submitted++
		case "late":
			item.Late++
		default:
			item.Missing++
		}
	}
	out := make([]ReportGroupCounts, 0, len(byGroup))
	for _, item := range byGroup {
		out = append(out, *item)
	}
	sort.Slice(out, func(i, j int) bool {
		// 未分组（空组名）排最后。
		if (out[i].Group == "") != (out[j].Group == "") {
			return out[j].Group == ""
		}
		return out[i].Group < out[j].Group
	})
	return out
}

// withAlignment 给有归一稿的组员补上对齐率。
func (s *ReportService) withAlignment(ws *store.Workspace, week, level string, p ReportPerson) ReportPerson {
	if level == store.ReportLevelMember && p.Normalized != "" && p.Normalized != "none" {
		p.Alignment = alignmentFor(ws, week, p.PersonID)
	}
	return p
}

// Get 返回一份周报的原文与元数据。person 可为 ID 或姓名/别名。
func (s *ReportService) Get(week, person, level string) (ReportData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return ReportData{}, err
	}
	if level == "" {
		level = store.ReportLevelMember
	}
	if strings.TrimSpace(week) == "" {
		week = store.WeekOf(s.clock())
	}
	personID, err := ws.ResolveReportPerson(week, level, person)
	if err != nil {
		return ReportData{}, err
	}
	report, err := ws.ReadReport(week, level, personID)
	if err != nil {
		return ReportData{}, err
	}
	return toReportData(report), nil
}

// Preview 解析粘贴文本并按在职人员匹配，返回每份的预览结果（只读不写）。
func (s *ReportService) Preview(text, level, week, person string) ([]ReportPreview, error) {
	ws, _, err := s.instances()
	if err != nil {
		return nil, err
	}
	// 组长月报：按组长层级识别人员，再按月份归档。
	month := ""
	if level == store.ReportLevelLeadMonth {
		month, level, week = week, store.ReportLevelLead, ""
	}
	items, err := core.PreviewReports(ws, core.PreviewInput{
		Text:   text,
		Level:  level,
		Week:   week,
		Person: person,
		Now:    s.clock(),
	})
	if err != nil {
		return nil, err
	}
	out := make([]ReportPreview, 0, len(items))
	for _, it := range items {
		p := toReportPreview(it)
		if month != "" {
			p.Week, p.Level, p.Late = month, store.ReportLevelLeadMonth, false
			p.Overwrite = false
			if p.PersonID != "" {
				p.Overwrite, _ = ws.ReportExists(month, store.ReportLevelLeadMonth, p.PersonID)
			}
			if deadline, err := ws.MonthDeadline(month); err == nil {
				p.Late = !s.clock().Before(deadline)
			}
		}
		out = append(out, p)
	}
	return out, nil
}

// Submit 写入一批周报原文，source 记为 paste，返回每份的归档结果。
func (s *ReportService) Submit(items []SubmitItem) ([]ReportData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return nil, err
	}
	now := s.clock()
	results := make([]ReportData, 0, len(items))
	for _, it := range items {
		if it.Level == store.ReportLevelLeadMonth {
			report, err := ws.SubmitMonthReport(it.Person, it.Week, it.Text, store.ReportSourcePaste, now)
			if err != nil {
				return nil, err
			}
			results = append(results, toReportData(report))
			continue
		}
		report, err := ws.SubmitReport(store.SubmitReportInput{
			Person: it.Person,
			Week:   it.Week,
			Level:  it.Level,
			Source: store.ReportSourcePaste,
			Body:   it.Text,
			Now:    now,
		})
		if err != nil {
			return nil, err
		}
		results = append(results, toReportData(report))
	}
	s.emitReport(nil, nil)
	return results, nil
}

// Delete 删除一份周报（移入 .history）。person 可为 ID 或姓名/别名。
func (s *ReportService) Delete(week, person, level string) error {
	ws, _, err := s.instances()
	if err != nil {
		return err
	}
	if level == "" {
		level = store.ReportLevelMember
	}
	if strings.TrimSpace(week) == "" {
		week = store.WeekOf(s.clock())
	}
	personID, err := ws.ResolveReportPerson(week, level, person)
	if err != nil {
		return err
	}
	if err := ws.DeleteReport(week, level, personID); err != nil {
		return err
	}
	s.emitReport(nil, []string{week + "/" + level + "/" + personID})
	return nil
}

// People 返回组员列表；includeInactive 为 false 时只返回在职人员。
func (s *ReportService) People(includeInactive bool) ([]PersonData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return nil, err
	}
	people, err := ws.ListPersons()
	if err != nil {
		return nil, err
	}
	out := make([]PersonData, 0, len(people))
	for _, p := range people {
		if !includeInactive && !p.Active {
			continue
		}
		out = append(out, toPersonData(p))
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// CreatePerson 新增一名组员。
func (s *ReportService) CreatePerson(in PersonInput) (PersonData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return PersonData{}, err
	}
	role := strings.TrimSpace(in.Role)
	if role == "" {
		role = store.RoleMember
	}
	person, err := ws.CreatePerson(store.NewPerson{
		Name:         in.Name,
		Aliases:      cleanStrings(in.Aliases),
		Group:        strings.TrimSpace(in.Group),
		ProductLines: cleanStrings(in.ProductLines),
		Role:         role,
		Now:          s.clock(),
	})
	if err != nil {
		return PersonData{}, err
	}
	s.emitReport([]string{person.ID}, nil)
	return toPersonData(person), nil
}

// UpdatePerson 修改组员姓名、别名、角色、组与产品线。组或产品线变化时按今天
// 追加一条转组记录，保证历史快照一致。
func (s *ReportService) UpdatePerson(id string, in PersonInput) (PersonData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return PersonData{}, err
	}
	now := s.clock()
	role := strings.TrimSpace(in.Role)
	if role == "" {
		role = store.RoleMember
	}
	group := strings.TrimSpace(in.Group)
	products := cleanStrings(in.ProductLines)
	person, err := ws.UpdatePerson(id, now, func(p *store.Person) error {
		if p.Group != group || !sameStrings(p.ProductLines, products) {
			p.GroupHistory = append(p.GroupHistory, store.GroupRecord{
				Group:        group,
				ProductLines: append([]string(nil), products...),
				From:         now.Format("2006-01-02"),
			})
		}
		p.Name = strings.TrimSpace(in.Name)
		p.Aliases = cleanStrings(in.Aliases)
		p.Group = group
		p.ProductLines = products
		p.Role = role
		return nil
	})
	if err != nil {
		return PersonData{}, err
	}
	s.emitReport([]string{person.ID}, nil)
	return toPersonData(person), nil
}

// MovePerson 转组：给出新组（可选新产品线）与生效日期，默认今天。
func (s *ReportService) MovePerson(id, group string, productLines []string, from string) (PersonData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return PersonData{}, err
	}
	person, err := ws.MovePerson(id, group, productLines, from, s.clock())
	if err != nil {
		return PersonData{}, err
	}
	s.emitReport([]string{person.ID}, nil)
	return toPersonData(person), nil
}

// SetPersonActive 设为离职或恢复在职。
func (s *ReportService) SetPersonActive(id string, active bool) (PersonData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return PersonData{}, err
	}
	person, err := ws.UpdatePerson(id, s.clock(), func(p *store.Person) error {
		p.Active = active
		return nil
	})
	if err != nil {
		return PersonData{}, err
	}
	s.emitReport([]string{person.ID}, nil)
	return toPersonData(person), nil
}

// Groups 返回名单里出现过的组名（去重排序，供下拉）。
func (s *ReportService) Groups() ([]string, error) {
	ws, _, err := s.instances()
	if err != nil {
		return nil, err
	}
	people, err := ws.ListPersons()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var groups []string
	for _, p := range people {
		g := strings.TrimSpace(p.Group)
		if g != "" && !seen[g] {
			seen[g] = true
			groups = append(groups, g)
		}
	}
	sort.Strings(groups)
	return groups, nil
}

// ProductLines 返回名单里出现过的产品线（去重排序，供下拉）。
func (s *ReportService) ProductLines() ([]string, error) {
	ws, _, err := s.instances()
	if err != nil {
		return nil, err
	}
	people, err := ws.ListPersons()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var lines []string
	for _, p := range people {
		for _, line := range p.ProductLines {
			line = strings.TrimSpace(line)
			if line != "" && !seen[line] {
				seen[line] = true
				lines = append(lines, line)
			}
		}
	}
	sort.Strings(lines)
	return lines, nil
}

// ReportWeekday 返回工作台配置的周报日（1-7）。
func (s *ReportService) ReportWeekday() (int, error) {
	ws, _, err := s.instances()
	if err != nil {
		return 0, err
	}
	return ws.ReportWeekday()
}

// SetReportWeekday 修改工作台配置的周报日。
func (s *ReportService) SetReportWeekday(weekday int) (int, error) {
	ws, _, err := s.instances()
	if err != nil {
		return 0, err
	}
	if err := ws.SetReportWeekday(weekday); err != nil {
		return 0, err
	}
	s.emitReport(nil, nil)
	return weekday, nil
}

// toReportPerson 把核心库的状态条目转为前端结构。
func toReportPerson(e store.ReportStatusEntry) ReportPerson {
	p := ReportPerson{
		PersonID:     e.Person,
		Name:         e.Name,
		Group:        e.Group,
		ProductLines: append([]string(nil), e.ProductLines...),
		Role:         e.Role,
		Active:       e.Active,
		Status:       e.Status,
	}
	if p.ProductLines == nil {
		p.ProductLines = []string{}
	}
	if e.FirstSubmittedAt != nil {
		p.FirstSubmittedAt = formatTime(*e.FirstSubmittedAt)
	}
	if e.SubmittedAt != nil {
		p.SubmittedAt = formatTime(*e.SubmittedAt)
	}
	if e.Normalized != nil {
		p.Normalized = e.Normalized.Status
		p.SourceChanged = e.Normalized.SourceChanged
		p.FromGroup = e.Normalized.FromGroup
	}
	return p
}

// toReportData 把核心库的周报转为前端结构。
func toReportData(r *store.Report) ReportData {
	d := ReportData{
		Person:           r.Person,
		Name:             r.Name,
		Week:             r.Week,
		Level:            r.Level,
		Group:            r.Group,
		Source:           r.Source,
		HeaderDate:       r.HeaderDate,
		Body:             r.Body,
		FirstSubmittedAt: formatTime(r.FirstSubmittedAt),
		SubmittedAt:      formatTime(r.SubmittedAt),
	}
	d.ProductLines = append([]string(nil), r.ProductLines...)
	if d.ProductLines == nil {
		d.ProductLines = []string{}
	}
	return d
}

// toReportPreview 把核心库的预览项转为前端结构。
func toReportPreview(it core.PreviewItem) ReportPreview {
	return ReportPreview{
		HasHeader:   it.HasHeader,
		Name:        it.Name,
		Date:        it.Date,
		DateInvalid: it.DateInvalid,
		Week:        it.Week,
		Level:       it.Level,
		PersonID:    it.PersonID,
		PersonName:  it.PersonName,
		Overwrite:   it.Overwrite,
		Late:        it.Late,
		Duplicate:   it.Duplicate,
		Reason:      it.Reason,
		StartLine:   it.StartLine,
		EndLine:     it.EndLine,
		Text:        it.Text,
	}
}

// toPersonData 把核心库的组员转为前端结构。
func toPersonData(p *store.Person) PersonData {
	d := PersonData{
		ID:      p.ID,
		Name:    p.Name,
		Group:   p.Group,
		Role:    p.Role,
		Active:  p.Active,
		Created: formatTime(p.Created),
		Updated: formatTime(p.Updated),
	}
	d.Aliases = append([]string(nil), p.Aliases...)
	d.ProductLines = append([]string(nil), p.ProductLines...)
	if d.Aliases == nil {
		d.Aliases = []string{}
	}
	if d.ProductLines == nil {
		d.ProductLines = []string{}
	}
	return d
}

// cleanStrings 去空白、去重（大小写不敏感）并过滤空串。
func cleanStrings(list []string) []string {
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

// sameStrings 判断两个字符串切片内容一致。
func sameStrings(a, b []string) bool {
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
