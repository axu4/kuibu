package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// 周报层级与来源取值。
const (
	// ReportLevelMember 表示组员交给组长。
	ReportLevelMember = "member"
	// ReportLevelLead 表示组长交给 SE。
	ReportLevelLead = "lead"
	// ReportLevelLeadMonth 表示组长月报（每月收集日交给 SE），目录在 reports/<YYYY-MM>/ 下。
	ReportLevelLeadMonth = "lead-month"

	// ReportSourcePaste 表示粘贴归档。
	ReportSourcePaste = "paste"
	// ReportSourceCLI 表示 CLI 提交。
	ReportSourceCLI = "cli"
	// ReportSourceGroup 表示由定稿组报一键带入（组长交给 SE）。
	ReportSourceGroup = "group-report"
)

// DefaultReportWeekday 是默认周报日：周四（1=周一…7=周日）。
const DefaultReportWeekday = 4

// HistoryDirName 是周报历史归档目录名，索引与枚举都跳过它。
const HistoryDirName = ".history"

// historyDirName 是 HistoryDirName 的内部别名。
const historyDirName = HistoryDirName

// isoWeekPattern 匹配 ISO 周编号，形如 2026-W44（W 不区分大小写）。
var isoWeekPattern = regexp.MustCompile(`^(\d{4})-[wW](\d{2})$`)

// WeekOf 返回日期所在的 ISO 周编号，形如 2026-W44。
//
// 日期语义统一使用 TodoLocation 时区，与待办保持一致。
func WeekOf(t time.Time) string {
	year, week := t.In(TodoLocation()).ISOWeek()
	return fmt.Sprintf("%04d-W%02d", year, week)
}

// WeekRange 返回周编号对应的周一与周日（00:00，TodoLocation 时区）。
//
// 周编号不合法（年份不匹配、周数超出该 ISO 年的周数）时返回 ErrInvalid。
func WeekRange(week string) (time.Time, time.Time, error) {
	m := isoWeekPattern.FindStringSubmatch(strings.TrimSpace(week))
	if m == nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: 无法解析周编号 %q", ErrInvalid, week)
	}
	var isoYear, isoWeek int
	_, _ = fmt.Sscanf(m[1], "%d", &isoYear)
	_, _ = fmt.Sscanf(m[2], "%d", &isoWeek)
	if isoWeek < 1 || isoWeek > 53 {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: 非法周编号 %q", ErrInvalid, week)
	}

	loc := TodoLocation()
	// ISO 年 1 月 4 日必在第 1 周，据此回推第 1 周的周一。
	jan4 := time.Date(isoYear, 1, 4, 0, 0, 0, 0, loc)
	isoWeekday := (int(jan4.Weekday()) + 6) % 7 // 周一=0 … 周日=6
	monday := jan4.AddDate(0, 0, -isoWeekday+(isoWeek-1)*7)
	if y, w := monday.ISOWeek(); y != isoYear || w != isoWeek {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: 非法周编号 %q", ErrInvalid, week)
	}
	return monday, monday.AddDate(0, 0, 6), nil
}

// ParseWeek 解析周参数：接受 this/last/next、YYYY-MM-DD 日期或 ISO 周编号，
// 返回规范化的 ISO 周编号（形如 2026-W44）。now 用于 this/last/next。
func ParseWeek(s string, now time.Time) (string, error) {
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "this":
		return WeekOf(now), nil
	case "last":
		return WeekOf(now.AddDate(0, 0, -7)), nil
	case "next":
		return WeekOf(now.AddDate(0, 0, 7)), nil
	}

	if t, err := time.ParseInLocation("2006-01-02", s, TodoLocation()); err == nil {
		return WeekOf(t), nil
	}
	if isoWeekPattern.MatchString(s) {
		if _, _, err := WeekRange(s); err != nil {
			return "", err
		}
		m := isoWeekPattern.FindStringSubmatch(s)
		return fmt.Sprintf("%s-W%s", m[1], strings.ToUpper(m[2])), nil
	}
	return "", fmt.Errorf("%w: 无法解析周 %q", ErrInvalid, s)
}

// WeekReportDay 返回该周第 weekday 天（1=周一…7=周日）的 00:00。
//
// weekday 为 0 时视为 DefaultReportWeekday。week 或 weekday 非法时返回错误。
func WeekReportDay(week string, weekday int) (time.Time, error) {
	if weekday == 0 {
		weekday = DefaultReportWeekday
	}
	if weekday < 1 || weekday > 7 {
		return time.Time{}, fmt.Errorf("%w: 周报日必须在 1-7 之间", ErrInvalid)
	}
	monday, _, err := WeekRange(week)
	if err != nil {
		return time.Time{}, err
	}
	return monday.AddDate(0, 0, weekday-1), nil
}

// WeekDeadline 返回周报日当天 24:00（即次日 00:00）。迟交判定使用它：
// submittedAt 晚于该时刻才算迟交。
func WeekDeadline(week string, weekday int) (time.Time, error) {
	day, err := WeekReportDay(week, weekday)
	if err != nil {
		return time.Time{}, err
	}
	return day.AddDate(0, 0, 1), nil
}

// GroupRecord 是组员的一条转组记录。
type GroupRecord struct {
	// Group 是所属组名。
	Group string `json:"group"`
	// ProductLines 是当时所属产品线。
	ProductLines []string `json:"productLines"`
	// From 是生效日期 YYYY-MM-DD，第一条是加入时。
	From string `json:"from"`
}

// Report 是一份周报原文归档。
type Report struct {
	// Person 是提交人的组员 ID（同时也是文件名）。
	Person string
	// Name 是提交时的姓名快照。
	Name string
	// Week 是 ISO 周编号。
	Week string
	// Level 是层级：member / lead。
	Level string
	// Group 是提交人所属组（按该周周一时快照）。
	Group string
	// ProductLines 是提交人所属产品线（按该周周一时快照）。
	ProductLines []string
	// Source 是来源：paste / cli。
	Source string
	// FirstSubmittedAt 是该周该层级的首次提交时间。
	FirstSubmittedAt time.Time
	// SubmittedAt 是最近一次提交时间。
	SubmittedAt time.Time
	// HeaderDate 是原文头行里的日期，可为空。
	HeaderDate string
	// Body 是原文正文（只去首尾空行）。
	Body string
	// Extra 保存 front matter 中未识别的字段。
	Extra map[string]any

	// extraNodes 保存未识别字段的原始 YAML 节点，用于无损写回。
	extraNodes map[string]*yaml.Node
}

// NewReport 是写入周报时传入的字段。
type NewReport struct {
	Person       string
	Name         string
	Week         string
	Level        string
	Group        string
	ProductLines []string
	Source       string
	SubmittedAt  time.Time
	HeaderDate   string
	Body         string
}

// knownReportFrontMatterKeys 是周报认识的标准 front matter 字段。
var knownReportFrontMatterKeys = map[string]bool{
	"person":           true,
	"name":             true,
	"week":             true,
	"level":            true,
	"group":            true,
	"productLines":     true,
	"source":           true,
	"firstSubmittedAt": true,
	"submittedAt":      true,
	"headerDate":       true,
}

// ValidReportLevel 判断周报层级是否合法。
func ValidReportLevel(level string) bool {
	return level == ReportLevelMember || level == ReportLevelLead || level == ReportLevelLeadMonth
}

// ValidReportSource 判断周报来源是否合法。
func ValidReportSource(source string) bool {
	return source == ReportSourcePaste || source == ReportSourceCLI || source == ReportSourceGroup
}

// reportPath 返回周报文件的绝对路径。
func (w *Workspace) reportPath(week, level, personID string) string {
	return filepath.Join(w.ReportsDir(), week, level, personID+noteExt)
}

// ReportWeekday 返回工作台配置的周报日（1-7），缺省或 0 视为周四。
func (w *Workspace) ReportWeekday() (int, error) {
	cfg, err := w.readConfig()
	if err != nil {
		return DefaultReportWeekday, err
	}
	if cfg.ReportWeekday < 1 || cfg.ReportWeekday > 7 {
		return DefaultReportWeekday, nil
	}
	return cfg.ReportWeekday, nil
}

// SetReportWeekday 修改工作台配置的周报日，保持其他配置字段不变。
func (w *Workspace) SetReportWeekday(weekday int) error {
	if weekday < 1 || weekday > 7 {
		return fmt.Errorf("%w: 周报日必须在 1-7 之间", ErrInvalid)
	}
	cfg, err := w.readConfig()
	if err != nil {
		return err
	}
	cfg.ReportWeekday = weekday
	return w.writeConfig(cfg)
}

// readConfig 读取 .kuibu/config.json；文件不存在时返回空配置。
func (w *Workspace) readConfig() (config, error) {
	var cfg config
	data, err := os.ReadFile(filepath.Join(w.MetaDir(), configFileName))
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("读取工作台配置失败: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("解析工作台配置失败: %w", err)
	}
	return cfg, nil
}

// writeConfig 原子地写回 .kuibu/config.json。
func (w *Workspace) writeConfig(cfg config) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("序列化工作台配置失败: %w", err)
	}
	data = append(data, '\n')
	if err := writeFileAtomic(filepath.Join(w.MetaDir(), configFileName), data); err != nil {
		return fmt.Errorf("写入工作台配置失败: %w", err)
	}
	return nil
}

// WriteReport 写入一份周报。同一人同一周同一层级再次提交时，旧文件先移入
// reports/<周>/.history/<层级>/ 保留，再覆盖写新文件。正文只去掉首尾空行。
func (w *Workspace) WriteReport(in NewReport) (*Report, error) {
	if strings.TrimSpace(in.Person) == "" {
		return nil, fmt.Errorf("%w: 周报人员 ID 不能为空", ErrInvalid)
	}
	if !ValidReportLevel(in.Level) {
		return nil, fmt.Errorf("%w: 非法周报层级 %q", ErrInvalid, in.Level)
	}
	if err := validPeriodFor(in.Level, in.Week); err != nil {
		return nil, err
	}
	source := in.Source
	if source == "" {
		source = ReportSourcePaste
	}
	if !ValidReportSource(source) {
		return nil, fmt.Errorf("%w: 非法周报来源 %q", ErrInvalid, source)
	}
	now := in.SubmittedAt
	if now.IsZero() {
		now = time.Now()
	}
	now = now.Truncate(time.Second)

	// 覆盖前先把旧文件归档，保留提交历史；旧文件的 firstSubmittedAt 要保留。
	old, _, err := w.archiveReport(in.Week, in.Level, in.Person)
	if err != nil {
		return nil, err
	}
	firstSubmittedAt := now
	if old != nil {
		firstSubmittedAt = old.FirstSubmittedAt
		if firstSubmittedAt.IsZero() {
			firstSubmittedAt = old.SubmittedAt
		}
	}

	report := &Report{
		Person:           in.Person,
		Name:             in.Name,
		Week:             in.Week,
		Level:            in.Level,
		Group:            in.Group,
		ProductLines:     append([]string(nil), in.ProductLines...),
		Source:           source,
		FirstSubmittedAt: firstSubmittedAt,
		SubmittedAt:      now,
		HeaderDate:       in.HeaderDate,
		Body:             trimBlankLines(in.Body),
		Extra:            map[string]any{},
	}
	if report.ProductLines == nil {
		report.ProductLines = []string{}
	}
	if err := w.writeReport(report); err != nil {
		return nil, err
	}
	return report, nil
}

// ReadReport 按周、层级与人员 ID 读取周报（含原文）。
func (w *Workspace) ReadReport(week, level, personID string) (*Report, error) {
	return readReportFile(w.reportPath(week, level, personID))
}

// ReadReportFile 读取任意路径的周报文件，供索引使用。
func ReadReportFile(path string) (*Report, error) {
	return readReportFile(path)
}

// ResolveReportPerson 把 show/rm 的人员参数解析成周报文件名里的组员 ID。
//
// 优先按 ID 或姓名/别名解析组员；解析不到时把参数当 ID 直接按文件存在性回退，
// 这样即使组员已从 people/ 删除，仍能查看/删除其历史周报。
func (w *Workspace) ResolveReportPerson(week, level, query string) (string, error) {
	query = strings.TrimSpace(query)
	if p, err := w.ResolvePerson(query); err == nil {
		return p.ID, nil
	} else if !errors.Is(err, ErrNotFound) {
		return "", err
	}
	if exists, err := w.ReportExists(week, level, query); err == nil && exists {
		return query, nil
	}
	return "", newReportError(ErrNotFound, "周报不存在: %s %s %s", week, level, query)
}

// ReportExists 判断某周某层级下某人的周报是否已归档。
func (w *Workspace) ReportExists(week, level, personID string) (bool, error) {
	_, err := os.Stat(w.reportPath(week, level, personID))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("检查周报是否存在失败: %w", err)
}

// SubmitReportInput 是提交周报时的输入。
type SubmitReportInput struct {
	// Person 是组员 ID 或姓名/别名。
	Person string
	// Week 是周编号（this/last/next/日期也可）。
	Week string
	// Level 是层级 member/lead，空视为 member。
	Level string
	// HeaderDate 是原文头里的日期，可空。
	HeaderDate string
	// Source 是来源 paste/cli，空视为 paste。
	Source string
	// Body 是原文。
	Body string
	// Now 是提交时间，零值使用 time.Now()。
	Now time.Time
}

// SubmitReport 提交一份周报：解析人员与周，按该周周一时快照所属组/产品线，
// 姓名快照取提交时姓名，再写入归档（覆盖时旧文件先入 .history）。
func (w *Workspace) SubmitReport(in SubmitReportInput) (*Report, error) {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	level := in.Level
	if level == "" {
		level = ReportLevelMember
	}
	week, err := ParseWeek(in.Week, now)
	if err != nil {
		return nil, err
	}
	person, err := w.ResolvePerson(in.Person)
	if err != nil {
		return nil, err
	}
	monday, _, err := WeekRange(week)
	if err != nil {
		return nil, err
	}
	group, products := person.MembershipAt(monday)
	if group == "" {
		group, products = person.Group, person.ProductLines
	}
	source := in.Source
	if source == "" {
		source = ReportSourcePaste
	}

	return w.WriteReport(NewReport{
		Person:       person.ID,
		Name:         person.Name,
		Week:         week,
		Level:        level,
		Group:        group,
		ProductLines: products,
		Source:       source,
		SubmittedAt:  now,
		HeaderDate:   in.HeaderDate,
		Body:         in.Body,
	})
}

// DeleteReport 删除周报：先移入 .history 再移除当前文件，保留提交历史。
func (w *Workspace) DeleteReport(week, level, personID string) error {
	_, moved, err := w.archiveReport(week, level, personID)
	if err != nil {
		return err
	}
	if !moved {
		return newReportError(ErrNotFound, "周报不存在: %s %s %s", week, level, personID)
	}
	return nil
}

// ListReports 返回某周某层级下的全部周报（含原文），按人员 ID 排序。
func (w *Workspace) ListReports(week, level string) ([]*Report, error) {
	dir := filepath.Join(w.ReportsDir(), week, level)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []*Report{}, nil
		}
		return nil, fmt.Errorf("读取周报目录失败: %w", err)
	}
	var reports []*Report
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !strings.HasSuffix(e.Name(), noteExt) {
			continue
		}
		r, err := readReportFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		reports = append(reports, r)
	}
	sort.Slice(reports, func(i, j int) bool { return reports[i].Person < reports[j].Person })
	return reports, nil
}

// ListLevelReports 返回某层级下所有周的周报，供预览与状态查询使用。
func (w *Workspace) ListLevelReports(level string) ([]*Report, error) {
	weeks, err := w.ListReportWeeks()
	if err != nil {
		return nil, err
	}
	var reports []*Report
	for _, week := range weeks {
		list, err := w.ListReports(week, level)
		if err != nil {
			return nil, err
		}
		reports = append(reports, list...)
	}
	return reports, nil
}

// ListReportWeeks 返回 reports/ 下已有的周编号目录列表（跳过 .history 与隐藏）。
func (w *Workspace) ListReportWeeks() ([]string, error) {
	entries, err := os.ReadDir(w.ReportsDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("读取周报目录失败: %w", err)
	}
	var weeks []string
	for _, e := range entries {
		// 只认 ISO 周目录，月目录（YYYY-MM）不混进周列表。
		if !e.IsDir() || !isoWeekPattern.MatchString(e.Name()) {
			continue
		}
		weeks = append(weeks, e.Name())
	}
	sort.Strings(weeks)
	return weeks, nil
}

// ReportStatusEntry 是提交状态中的一个人。
type ReportStatusEntry struct {
	// Person 是组员 ID。
	Person string `json:"person"`
	// Name 是组员姓名。
	Name string `json:"name"`
	// Group 是该周周一时所属组。
	Group string `json:"group"`
	// ProductLines 是该周周一时所属产品线。
	ProductLines []string `json:"productLines"`
	// Role 是角色（member/lead）。
	Role string `json:"role"`
	// Active 表示是否在职。
	Active bool `json:"active"`
	// Status 是 submitted / late / missing。
	Status string `json:"status"`
	// FirstSubmittedAt 是首次提交时间，missing 时为空。
	FirstSubmittedAt *time.Time `json:"firstSubmittedAt,omitempty"`
	// SubmittedAt 是最近一次提交时间，missing 时为空。
	SubmittedAt *time.Time `json:"submittedAt,omitempty"`
	// Normalized 是归一稿状态，只在组员层级（member）填写。
	Normalized *DraftState `json:"normalized,omitempty"`
}

// ReportStatus 是一次提交状态统计。
type ReportStatus struct {
	Week    string              `json:"week"`
	Level   string              `json:"level"`
	Entries []ReportStatusEntry `json:"entries"`
	// Extra 是有提交但不在应交名单里的人（含已离职但交过的人）。
	Extra  []ReportStatusEntry `json:"extra"`
	Counts map[string]int      `json:"counts"`
}

// ReportStatus 计算某周某层级的提交状态。
//
// 应交名单 = 在职且 role 与层级对应（member→role=member，lead→role=lead）的
// 组员；group/productLine 过滤按该周周一时该人的所属组/产品线判断。已交的人
// 若 firstSubmittedAt 晚于周报日 24:00 记为 late。有提交但不在应交名单里的
// 单独放 Extra（已离职但交过的人也在此列出）。
func (w *Workspace) ReportStatus(week, level, group, productLine string, now time.Time) (*ReportStatus, error) {
	if !ValidReportLevel(level) {
		return nil, fmt.Errorf("%w: 非法周报层级 %q", ErrInvalid, level)
	}
	// monday 是判断所属组 / 产品线的参照日；月报用当月 1 日。
	var monday, deadline time.Time
	if level == ReportLevelLeadMonth {
		start, err := MonthStart(week)
		if err != nil {
			return nil, err
		}
		monday = start
		if deadline, err = w.MonthDeadline(week); err != nil {
			return nil, err
		}
	} else {
		var err error
		if monday, _, err = WeekRange(week); err != nil {
			return nil, err
		}
		weekday, err := w.ReportWeekday()
		if err != nil {
			return nil, err
		}
		if deadline, err = WeekDeadline(week, weekday); err != nil {
			return nil, err
		}
	}

	people, err := w.ListPersons()
	if err != nil {
		return nil, err
	}
	reports, err := w.ListReports(week, level)
	if err != nil {
		return nil, err
	}
	submitted := map[string]*Report{}
	for _, r := range reports {
		submitted[r.Person] = r
	}
	// 附带归一稿状态（组员 → member-normalized，组长 → lead-normalized）。
	list, err := w.ListDraftsKind(NormalizedKind(level), week)
	if err != nil {
		return nil, err
	}
	drafts := map[string]*Draft{}
	for _, d := range list {
		drafts[d.Person] = d
	}
	draftState := func(person string) *DraftState {
		// 组长经组报带入的周报本身就是定稿，不需要再归一。
		if r := submitted[person]; r != nil && r.Source == ReportSourceGroup && drafts[person] == nil {
			return &DraftState{Status: DraftStatusFinal, FromGroup: true}
		}
		st := DraftStateFor(drafts[person], submitted[person])
		return &st
	}

	wantRole := RoleMember
	if level == ReportLevelLead || level == ReportLevelLeadMonth {
		wantRole = RoleLead
	}

	st := &ReportStatus{Week: week, Level: level, Counts: map[string]int{}}
	due := map[string]bool{}
	byID := map[string]*Person{}
	for _, p := range people {
		byID[p.ID] = p
		if !p.Active || p.Role != wantRole {
			continue
		}
		groupAt, productsAt := p.MembershipAt(monday)
		if groupAt == "" {
			continue
		}
		if group != "" && groupAt != group {
			continue
		}
		if productLine != "" && !containsStr(productsAt, productLine) {
			continue
		}
		due[p.ID] = true

		entry := ReportStatusEntry{
			Person:       p.ID,
			Name:         p.Name,
			Group:        groupAt,
			ProductLines: append([]string(nil), productsAt...),
			Role:         p.Role,
			Active:       true,
			Status:       "missing",
			Normalized:   draftState(p.ID),
		}
		if r, ok := submitted[p.ID]; ok {
			first := r.FirstSubmittedAt
			if first.IsZero() {
				first = r.SubmittedAt
			}
			entry.Status = "submitted"
			if !first.Before(deadline) {
				entry.Status = "late"
			}
			t := r.SubmittedAt
			entry.SubmittedAt = &t
			f := first
			entry.FirstSubmittedAt = &f
		}
		st.Counts[entry.Status]++
		st.Entries = append(st.Entries, entry)
	}
	sort.Slice(st.Entries, func(i, j int) bool { return st.Entries[i].Person < st.Entries[j].Person })

	// 有提交但不在应交名单里的人（含已离职的人）。
	for _, r := range reports {
		if due[r.Person] {
			continue
		}
		entry := ReportStatusEntry{
			Person:       r.Person,
			Name:         r.Name,
			Group:        r.Group,
			ProductLines: append([]string(nil), r.ProductLines...),
			Status:       "submitted",
			Normalized:   draftState(r.Person),
		}
		if p, ok := byID[r.Person]; ok {
			entry.Role = p.Role
			entry.Active = p.Active
			if entry.Name == "" {
				entry.Name = p.Name
			}
		}
		first := r.FirstSubmittedAt
		if first.IsZero() {
			first = r.SubmittedAt
		}
		if !first.Before(deadline) {
			entry.Status = "late"
		}
		t := r.SubmittedAt
		entry.SubmittedAt = &t
		f := first
		entry.FirstSubmittedAt = &f
		st.Extra = append(st.Extra, entry)
	}
	sort.Slice(st.Extra, func(i, j int) bool { return st.Extra[i].Person < st.Extra[j].Person })

	st.Counts["expected"] = len(st.Entries)
	st.Counts["extra"] = len(st.Extra)
	st.Counts["total"] = len(st.Entries) + len(st.Extra)
	return st, nil
}

// archiveReport 把当前周报文件移入 .history/<层级>/，返回旧报告与是否真的移动。
//
// 归档文件名为 <personId>-<旧 submittedAt 紧凑格式>.md，同名冲突时追加序号。
func (w *Workspace) archiveReport(week, level, personID string) (*Report, bool, error) {
	path := w.reportPath(week, level, personID)
	old, err := readReportFile(path)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}

	// 归档的原文文件末尾保证有且只有一个换行。
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false, fmt.Errorf("读取待归档周报失败: %w", err)
	}
	normalized := ensureTrailingNewlineBytes(raw)
	if !bytes.Equal(normalized, raw) {
		if err := writeFileAtomic(path, normalized); err != nil {
			return nil, false, fmt.Errorf("规范化周报文件末尾失败: %w", err)
		}
	}

	destDir := filepath.Join(w.ReportsDir(), week, historyDirName, level)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, false, fmt.Errorf("创建周报历史目录失败: %w", err)
	}
	base := personID + "-" + compactTime(old.SubmittedAt)
	dest := filepath.Join(destDir, base+noteExt)
	for i := 1; ; i++ {
		if _, err := os.Stat(dest); errors.Is(err, os.ErrNotExist) {
			break
		}
		dest = filepath.Join(destDir, fmt.Sprintf("%s-%d%s", base, i, noteExt))
	}
	if err := os.Rename(path, dest); err != nil {
		return nil, false, fmt.Errorf("归档旧周报失败: %w", err)
	}
	return old, true, nil
}

// compactTime 把时间格式化为文件名安全的紧凑形式（TodoLocation 时区）。
func compactTime(t time.Time) string {
	return t.In(TodoLocation()).Format("20060102-150405")
}

// ensureTrailingNewline 保证正文以恰好一个换行结尾；空正文保持为空。
func ensureTrailingNewline(s string) string {
	if s == "" {
		return ""
	}
	return strings.TrimRight(s, "\r\n") + "\n"
}

// trimTrailingNewline 去掉正文末尾恰好一个换行（兼容 CRLF）。
func trimTrailingNewline(s string) string {
	s = strings.TrimSuffix(s, "\n")
	return strings.TrimSuffix(s, "\r")
}

// ensureTrailingNewlineBytes 保证文件字节以恰好一个换行结尾。
func ensureTrailingNewlineBytes(data []byte) []byte {
	trimmed := bytes.TrimRight(data, "\r\n")
	if len(trimmed) == 0 {
		return data
	}
	return append(trimmed, '\n')
}

// readReportFile 读取并解析周报文件。
func readReportFile(path string) (*Report, error) {
	id := IDFromPath(path)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, newReportError(ErrNotFound, "周报不存在: %s", id)
		}
		return nil, fmt.Errorf("读取周报失败: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("读取周报信息失败: %w", err)
	}

	report := &Report{
		Person:      id,
		Level:       ReportLevelMember,
		Source:      ReportSourcePaste,
		SubmittedAt: info.ModTime(),
		Body:        string(data),
		Extra:       map[string]any{},
	}

	front, body, ok := splitFrontMatter(data)
	if ok {
		if parsed := decodeReportFrontMatter(front); parsed != nil {
			report.Person = parsed.Person
			report.Name = parsed.Name
			report.Week = parsed.Week
			report.Level = parsed.Level
			report.Group = parsed.Group
			report.ProductLines = parsed.ProductLines
			report.Source = parsed.Source
			report.HeaderDate = parsed.HeaderDate
			report.Extra = parsed.Extra
			report.extraNodes = parsed.extraNodes
			if !parsed.SubmittedAt.IsZero() {
				report.SubmittedAt = parsed.SubmittedAt
			}
			if !parsed.FirstSubmittedAt.IsZero() {
				report.FirstSubmittedAt = parsed.FirstSubmittedAt
			}
			report.Body = trimTrailingNewline(string(body))
		}
	}

	if report.FirstSubmittedAt.IsZero() {
		report.FirstSubmittedAt = report.SubmittedAt
	}

	if report.Person == "" {
		report.Person = id
	}
	if report.Level == "" {
		report.Level = ReportLevelMember
	}
	if report.Source == "" {
		report.Source = ReportSourcePaste
	}
	if report.ProductLines == nil {
		report.ProductLines = []string{}
	}
	if report.Extra == nil {
		report.Extra = map[string]any{}
	}
	return report, nil
}

// decodeReportFrontMatter 解析周报 front matter。解析失败或不是映射时返回 nil。
func decodeReportFrontMatter(front []byte) *Report {
	root, empty, ok := frontMatterMapping(front)
	if !ok {
		if empty {
			return &Report{}
		}
		return nil
	}

	report := &Report{Extra: map[string]any{}, extraNodes: map[string]*yaml.Node{}}
	for i := 0; i+1 < len(root.Content); i += 2 {
		keyNode := root.Content[i]
		valNode := root.Content[i+1]
		switch keyNode.Value {
		case "person":
			_ = valNode.Decode(&report.Person)
		case "name":
			_ = valNode.Decode(&report.Name)
		case "week":
			_ = valNode.Decode(&report.Week)
		case "level":
			_ = valNode.Decode(&report.Level)
		case "group":
			_ = valNode.Decode(&report.Group)
		case "productLines":
			_ = valNode.Decode(&report.ProductLines)
		case "source":
			_ = valNode.Decode(&report.Source)
		case "submittedAt":
			report.SubmittedAt = decodeTime(valNode)
		case "firstSubmittedAt":
			report.FirstSubmittedAt = decodeTime(valNode)
		case "headerDate":
			_ = valNode.Decode(&report.HeaderDate)
		default:
			var v any
			if err := valNode.Decode(&v); err == nil {
				report.Extra[keyNode.Value] = v
			}
			report.extraNodes[keyNode.Value] = valNode
		}
	}
	return report
}

// writeReport 原子地写回周报文件。
func (w *Workspace) writeReport(report *Report) error {
	data, err := marshalReport(report)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(w.reportPath(report.Week, report.Level, report.Person), data); err != nil {
		return fmt.Errorf("写入周报 %s 失败: %w", report.Person, err)
	}
	return nil
}

// marshalReport 把周报序列化为 front matter + 正文。
func marshalReport(report *Report) ([]byte, error) {
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}

	products := report.ProductLines
	if products == nil {
		products = []string{}
	}

	appendFrontMatterKey(root, "person", stringNode(report.Person))
	appendFrontMatterKey(root, "name", stringNode(report.Name))
	appendFrontMatterKey(root, "week", stringNode(report.Week))
	appendFrontMatterKey(root, "level", stringNode(report.Level))
	appendFrontMatterKey(root, "group", stringNode(report.Group))
	if err := appendFrontMatterValue(root, "productLines", products); err != nil {
		return nil, fmt.Errorf("序列化 productLines 失败: %w", err)
	}
	appendFrontMatterKey(root, "source", stringNode(report.Source))
	appendFrontMatterKey(root, "firstSubmittedAt", stringNode(formatTime(report.FirstSubmittedAt)))
	appendFrontMatterKey(root, "submittedAt", stringNode(formatTime(report.SubmittedAt)))
	appendFrontMatterKey(root, "headerDate", stringNode(report.HeaderDate))

	if err := appendExtraFrontMatter(root, report.Extra, report.extraNodes, knownReportFrontMatterKeys); err != nil {
		return nil, err
	}
	return marshalFrontMatter(root, ensureTrailingNewline(report.Body))
}

// trimBlankLines 只去掉正文首尾的空行（含仅空白字符的行），中间原样保留。
func trimBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	end := len(lines)
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	if start == end {
		return ""
	}
	return strings.Join(lines[start:end], "\n")
}

// containsStr 判断切片是否包含指定字符串。
func containsStr(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// reportError 是周报操作专用的私有错误类型：Error() 返回周报文案，Unwrap()
// 返回哨兵，保证 errors.Is(err, ErrNotFound) 仍然成立。
type reportError struct {
	msg string
	err error
}

// Error 实现 error 接口。
func (e *reportError) Error() string { return e.msg }

// Unwrap 返回底层哨兵错误。
func (e *reportError) Unwrap() error { return e.err }

// newReportError 构造带自定义文案、但保持哨兵可识别的周报错误。
func newReportError(sentinel error, format string, args ...any) error {
	return &reportError{msg: fmt.Sprintf(format, args...), err: sentinel}
}
