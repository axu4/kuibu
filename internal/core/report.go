package core

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/axu4/kuibu/internal/store"
)

// reportHeaderRe 匹配【周报】/ [周报] 头行：
//
//	【周报】姓名 YYYY-MM-DD（也容忍全角空格、YYYY/MM/DD、YYYY.MM.DD、YYYY-M-D；
//	姓名与日期之间可以没有空白）。
var reportHeaderRe = regexp.MustCompile(`^[ \t\x{3000}]*(?:【周报】|\[周报\])[ \t\x{3000}]*(.+?)[ \t\x{3000}]*(\d{4}[-/.]\d{1,2}[-/.]\d{1,2})[ \t\x{3000}]*$`)

// BatchItem 是一次粘贴里按【周报】头行切分出的一段原文。
type BatchItem struct {
	// HasHeader 表示该段是否以【周报】头行开头。
	HasHeader bool `json:"hasHeader"`
	// Name 是头行里的姓名，无头行时为空。
	Name string `json:"name"`
	// Date 是头行里的日期（归一化为 YYYY-MM-DD），无头行或无日期时为空。
	Date string `json:"date,omitempty"`
	// DateInvalid 表示头行里出现日期但无法解析（如 2026-02-30）。
	DateInvalid bool `json:"dateInvalid,omitempty"`
	// Text 是该段原文（含头行，只去掉了首尾空行）。
	Text string `json:"text"`
	// StartLine 与 EndLine 是该段在原文里的起止行号（1 起，闭区间）。
	StartLine int `json:"startLine"`
	EndLine   int `json:"endLine"`
}

// ParseBatch 把一段粘贴文本按【周报】头行切分成多份。
//
// 头行保留在每份原文里；两个头行之间的内容归前一份。没有任何头行时整段
// 当成一份（HasHeader=false，需要调用方指定 --person）。头行前的空行会被
// 丢弃，非空内容也会被丢弃（不属于任何一份）。
func ParseBatch(text string) []BatchItem {
	lines := strings.Split(text, "\n")

	type header struct {
		idx     int
		name    string
		date    string
		invalid bool
	}
	var headers []header
	for i, line := range lines {
		name, date, invalid, ok := parseReportHeader(line)
		if ok {
			headers = append(headers, header{idx: i, name: name, date: date, invalid: invalid})
		}
	}

	if len(headers) == 0 {
		return []BatchItem{{
			HasHeader: false,
			Text:      trimBlankLines(lines),
			StartLine: 1,
			EndLine:   len(lines),
		}}
	}

	items := make([]BatchItem, 0, len(headers))
	for i, h := range headers {
		end := len(lines)
		if i+1 < len(headers) {
			end = headers[i+1].idx
		}
		items = append(items, BatchItem{
			HasHeader:   true,
			Name:        h.name,
			Date:        h.date,
			DateInvalid: h.invalid,
			Text:        trimBlankLines(lines[h.idx:end]),
			StartLine:   h.idx + 1,
			EndLine:     end,
		})
	}
	return items
}

// parseReportHeader 解析一行【周报】头，返回姓名、日期（YYYY-MM-DD）、日期是否非法。
func parseReportHeader(line string) (name, date string, dateInvalid, ok bool) {
	m := reportHeaderRe.FindStringSubmatch(strings.TrimRight(line, "\r"))
	if m == nil {
		return "", "", false, false
	}
	name = strings.TrimSpace(m[1])
	norm, err := normalizeHeaderDate(m[2])
	if err != nil {
		return name, "", true, true
	}
	return name, norm, false, true
}

// normalizeHeaderDate 把 YYYY/MM/DD、YYYY.MM.DD、YYYY-M-D 归一化为 YYYY-MM-DD。
func normalizeHeaderDate(raw string) (string, error) {
	s := strings.NewReplacer("/", "-", ".", "-").Replace(raw)
	t, err := time.Parse("2006-1-2", s)
	if err != nil {
		return "", err
	}
	return t.Format("2006-01-02"), nil
}

// PreviewInput 是「粘贴→预览→确认写入」的输入。
type PreviewInput struct {
	// Text 是粘贴原文。
	Text string
	// Level 是层级 member/lead，空视为 member。
	Level string
	// Person 是外部指定的人员（ID 或姓名/别名），只在单份文本时允许。
	Person string
	// Week 是外部指定的周，优先于头行日期识别。
	Week string
	// Now 是「现在」，用于 this/last/next、周报日与迟交判断。
	Now time.Time
}

// PreviewItem 是一份周报的识别与匹配预览结果。
type PreviewItem struct {
	// HasHeader 表示该段是否以【周报】头行开头。
	HasHeader bool `json:"hasHeader"`
	// Name 是头行里的姓名。
	Name string `json:"name"`
	// Date 是头行里的日期。
	Date string `json:"date,omitempty"`
	// DateInvalid 表示头行日期无法解析。
	DateInvalid bool `json:"dateInvalid,omitempty"`
	// Week 是识别出的 ISO 周。
	Week string `json:"week,omitempty"`
	// Level 是层级。
	Level string `json:"level"`
	// PersonID 是匹配到的人员 ID。
	PersonID string `json:"person,omitempty"`
	// PersonName 是匹配到的人员姓名。
	PersonName string `json:"personName,omitempty"`
	// Overwrite 表示是否会覆盖已有提交。
	Overwrite bool `json:"overwrite"`
	// Late 表示现在提交是否会迟交。
	Late bool `json:"late"`
	// Duplicate 表示同一次粘贴里同一 person+week+level 出现多份时的非最后一份。
	Duplicate bool `json:"duplicate,omitempty"`
	// Reason 是无法识别的原因（无头行/姓名不匹配/姓名有歧义/日期不合法）。
	Reason string `json:"reason,omitempty"`
	// StartLine 与 EndLine 是该段在原文里的起止行号。
	StartLine int `json:"startLine"`
	EndLine   int `json:"endLine"`
	// Text 是该段原文。
	Text string `json:"text"`
}

// PreviewReports 解析粘贴文本并按在职人员匹配，返回每份的预览结果。
//
// 该函数只读工作台（人员、已有提交、周报日配置），不写任何文件。
func PreviewReports(ws *store.Workspace, in PreviewInput) ([]PreviewItem, error) {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	level := in.Level
	if level == "" {
		level = store.ReportLevelMember
	}
	if !store.ValidReportLevel(level) {
		return nil, fmt.Errorf("非法周报层级 %q", level)
	}

	var weekOverride string
	if in.Week != "" {
		w, err := store.ParseWeek(in.Week, now)
		if err != nil {
			return nil, err
		}
		weekOverride = w
	}

	items := ParseBatch(in.Text)
	if in.Person != "" && len(items) > 1 {
		return nil, fmt.Errorf("仅单份文本时可指定 --person")
	}

	people, err := ws.ListPersons()
	if err != nil {
		return nil, err
	}
	weekday, err := ws.ReportWeekday()
	if err != nil {
		return nil, err
	}

	out := make([]PreviewItem, 0, len(items))
	for _, it := range items {
		pi := PreviewItem{
			HasHeader:   it.HasHeader,
			Name:        it.Name,
			Date:        it.Date,
			DateInvalid: it.DateInvalid,
			Level:       level,
			StartLine:   it.StartLine,
			EndLine:     it.EndLine,
			Text:        it.Text,
		}

		// 周：外部指定优先于头行日期。
		switch {
		case weekOverride != "":
			pi.Week = weekOverride
		case it.HasHeader && !it.DateInvalid && it.Date != "":
			if w, err := store.ParseWeek(it.Date, now); err == nil {
				pi.Week = w
			} else {
				pi.Reason = "日期不合法"
			}
		default:
			if it.HasHeader {
				pi.Reason = "日期不合法"
			} else {
				pi.Reason = "无头行"
			}
		}

		// 人员：外部指定优先于头行姓名匹配。
		if in.Person != "" {
			p, err := ws.ResolvePerson(in.Person)
			if err != nil {
				pi.Reason = previewPersonReason(err)
			} else {
				pi.PersonID = p.ID
				pi.PersonName = p.Name
			}
		} else if it.HasHeader {
			p, reason := matchActivePerson(people, it.Name)
			if reason != "" {
				pi.Reason = reason
			} else {
				pi.PersonID = p.ID
				pi.PersonName = p.Name
			}
		} else if pi.Reason == "" {
			pi.Reason = "无头行"
		}

		// 信息齐全时给出覆盖与迟交提示。
		if pi.Reason == "" && pi.PersonID != "" && pi.Week != "" {
			if ok, err := ws.ReportExists(pi.Week, level, pi.PersonID); err == nil {
				pi.Overwrite = ok
			}
			if deadline, err := store.WeekDeadline(pi.Week, weekday); err == nil {
				// 迟交按该周该层级的首次提交时间判断：覆盖时沿用旧文件的
				// firstSubmittedAt，否则用当前时间。
				lateAt := now
				if pi.Overwrite {
					if r, err := ws.ReadReport(pi.Week, level, pi.PersonID); err == nil && !r.FirstSubmittedAt.IsZero() {
						lateAt = r.FirstSubmittedAt
					}
				}
				pi.Late = !lateAt.Before(deadline)
			}
		}
		out = append(out, pi)
	}

	// 同一次粘贴里同一 person+week+level 出现多份时，只保留最后一份，
	// 前面的标记为重复。
	markDuplicateReports(out)
	return out, nil
}

// markDuplicateReports 把同一 person+week+level 的非最后一份标记为重复。
func markDuplicateReports(items []PreviewItem) {
	type dupKey struct{ person, week, level string }
	last := map[dupKey]int{}
	for i := range items {
		p := &items[i]
		if p.Reason == "" && p.PersonID != "" && p.Week != "" {
			last[dupKey{p.PersonID, p.Week, p.Level}] = i
		}
	}
	for i := range items {
		p := &items[i]
		if p.Reason != "" || p.PersonID == "" || p.Week == "" {
			continue
		}
		if last[dupKey{p.PersonID, p.Week, p.Level}] != i {
			p.Duplicate = true
			p.Reason = "重复提交，仅保留最后一份"
		}
	}
}

// matchActivePerson 在在职人员中按姓名/别名匹配（大小写不敏感、去首尾空格）。
func matchActivePerson(people []*store.Person, name string) (*store.Person, string) {
	target := strings.ToLower(strings.TrimSpace(name))
	var matches []*store.Person
	for _, p := range people {
		if !p.Active {
			continue
		}
		if strings.ToLower(strings.TrimSpace(p.Name)) == target {
			matches = append(matches, p)
			continue
		}
		for _, a := range p.Aliases {
			if strings.ToLower(strings.TrimSpace(a)) == target {
				matches = append(matches, p)
				break
			}
		}
	}
	switch len(matches) {
	case 0:
		return nil, "姓名不匹配"
	case 1:
		return matches[0], ""
	default:
		return nil, "姓名有歧义"
	}
}

// previewPersonReason 把 ResolvePerson 的错误翻译成预览原因。
func previewPersonReason(err error) string {
	switch {
	case errors.Is(err, store.ErrAmbiguousPerson):
		return "姓名有歧义"
	case errors.Is(err, store.ErrNotFound):
		return "姓名不匹配"
	default:
		return err.Error()
	}
}

// trimBlankLines 只去掉文本首尾的空行（含仅空白字符的行），中间原样保留。
func trimBlankLines(lines []string) string {
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
