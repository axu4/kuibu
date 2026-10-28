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

// 归一稿的三个固定小节。
const (
	SectionDone = "本周完成"
	SectionPlan = "下周计划"
	SectionRisk = "风险与求助"
)

// CarriedSuffix 是顺延条目末尾的标记。
const CarriedSuffix = "（顺延）"

// DraftItem 是归一稿里的一条列表项。
type DraftItem struct {
	// ID 是「节名 + 文本」的 sha256 前 8 位；文本改了 id 就变。
	ID   string `json:"id"`
	Text string `json:"text"`
}

// ItemID 计算条目 id。
func ItemID(section, text string) string {
	sum := sha256.Sum256([]byte(section + "\n" + strings.TrimSpace(text)))
	return hex.EncodeToString(sum[:])[:8]
}

// ParseDraftItems 按「## 节名」切分归一稿，返回每节的列表项（跳过「无」）。
// 同一节里文本重复的条目只保留一条，保证 id 唯一。
func ParseDraftItems(body string) map[string][]DraftItem {
	out := map[string][]DraftItem{}
	section := ""
	seen := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			section = sectionOf(trimmed)
			continue
		}
		text, ok := listItemText(trimmed)
		if !ok || section == "" || text == "无" || text == "" {
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

// sectionOf 把标题行映射成固定节名，其他标题返回空。
func sectionOf(heading string) string {
	title := strings.TrimSpace(strings.TrimLeft(heading, "#"))
	// 组长的说法是「本周总结」；数据层固定节名仍是「本周完成」，两者都认。
	if strings.HasPrefix(title, "本周总结") {
		return SectionDone
	}
	for _, s := range []string{SectionDone, SectionPlan, SectionRisk} {
		if strings.HasPrefix(title, s) {
			return s
		}
	}
	return ""
}

// listItemText 取「- / * / 1.」列表项的文本。
func listItemText(line string) (string, bool) {
	for _, p := range []string{"- ", "* ", "+ "} {
		if strings.HasPrefix(line, p) {
			return strings.TrimSpace(line[len(p):]), true
		}
	}
	if i := strings.Index(line, ". "); i > 0 && i <= 3 && strings.Trim(line[:i], "0123456789") == "" {
		return strings.TrimSpace(line[i+2:]), true
	}
	return "", false
}

// PrevWeek 返回上一个 ISO 周。
func PrevWeek(week string) (string, error) {
	monday, _, err := store.WeekRange(week)
	if err != nil {
		return "", err
	}
	return store.WeekOf(monday.AddDate(0, 0, -7)), nil
}

// PlanReview 是上周一条计划的对照结果。
type PlanReview struct {
	ID      string   `json:"id"`
	Text    string   `json:"text"`
	Status  string   `json:"status"`
	Matches []string `json:"matches"`
	Note    string   `json:"note"`
	Source  string   `json:"source"`
	Carried bool     `json:"carried"`
}

// DoneReview 是本周一条完成项。
type DoneReview struct {
	ID        string   `json:"id"`
	Text      string   `json:"text"`
	Unplanned bool     `json:"unplanned"`
	MatchedBy []string `json:"matchedBy"`
}

// ReviewCounts 是各状态计数。
type ReviewCounts struct {
	Done     int `json:"done"`
	Partial  int `json:"partial"`
	Delayed  int `json:"delayed"`
	Canceled int `json:"canceled"`
	Unmarked int `json:"unmarked"`
	Total    int `json:"total"`
}

// ReviewResult 是一个人的「对照上周计划」。
type ReviewResult struct {
	Week     string       `json:"week"`
	PrevWeek string       `json:"prevWeek"`
	Person   string       `json:"person"`
	Name     string       `json:"name"`
	Plans    []PlanReview `json:"plans"`
	Done     []DoneReview `json:"done"`
	Next     []DraftItem  `json:"next"`
	Counts   ReviewCounts `json:"counts"`
	// Alignment 是对齐率 0~1；计划全部取消或没有计划时为 nil。
	Alignment *float64 `json:"alignment"`
	// ThisFinal 表示本周归一稿已定稿（顺延前需取消定稿）。
	ThisFinal bool `json:"thisFinal"`
}

// AlignmentOf 计算对齐率 = (完成 + 0.5×部分) / (计划数 − 取消)；未标状态算 0。
func AlignmentOf(c ReviewCounts) *float64 {
	denom := c.Total - c.Canceled
	if denom <= 0 {
		return nil
	}
	v := (float64(c.Done) + 0.5*float64(c.Partial)) / float64(denom)
	return &v
}

// ComputeReview 由上周定稿正文与本周归一稿算出对照结果（纯函数，便于测试）。
func ComputeReview(prevBody string, cur *store.Draft) ReviewResult {
	prev := ParseDraftItems(prevBody)
	this := ParseDraftItems(cur.Body)

	var saved store.DraftReview
	if cur.Review != nil {
		saved = *cur.Review
	}
	byPlan := map[string]store.ReviewItem{}
	for _, it := range saved.Items {
		byPlan[it.Plan] = it
	}
	carried := map[string]bool{}
	for _, id := range saved.Carried {
		carried[id] = true
	}
	doneIDs := map[string]bool{}
	for _, d := range this[SectionDone] {
		doneIDs[d.ID] = true
	}

	res := ReviewResult{Week: cur.Week, Person: cur.Person, Name: cur.Name, ThisFinal: cur.Status == store.DraftStatusFinal}
	matchedBy := map[string][]string{}
	for _, p := range prev[SectionPlan] {
		pr := PlanReview{ID: p.ID, Text: p.Text, Matches: []string{}, Carried: carried[p.ID]}
		if it, ok := byPlan[p.ID]; ok {
			pr.Status, pr.Note, pr.Source = it.Status, it.Note, it.Source
			// 本周完成项文字改过后旧的对应关系失效，这里丢掉找不到的 id。
			for _, m := range it.Matches {
				if doneIDs[m] {
					pr.Matches = append(pr.Matches, m)
					matchedBy[m] = append(matchedBy[m], p.ID)
				}
			}
		}
		res.Counts.Total++
		switch pr.Status {
		case store.ReviewDone:
			res.Counts.Done++
		case store.ReviewPartial:
			res.Counts.Partial++
		case store.ReviewDelayed:
			res.Counts.Delayed++
		case store.ReviewCanceled:
			res.Counts.Canceled++
		default:
			res.Counts.Unmarked++
		}
		res.Plans = append(res.Plans, pr)
	}
	for _, d := range this[SectionDone] {
		by := matchedBy[d.ID]
		if by == nil {
			by = []string{}
		}
		res.Done = append(res.Done, DoneReview{ID: d.ID, Text: d.Text, Unplanned: len(by) == 0, MatchedBy: by})
	}
	res.Next = this[SectionPlan]
	if res.Plans == nil {
		res.Plans = []PlanReview{}
	}
	if res.Done == nil {
		res.Done = []DoneReview{}
	}
	if res.Next == nil {
		res.Next = []DraftItem{}
	}
	res.Alignment = AlignmentOf(res.Counts)
	return res
}

// loadReviewInputs 读取上周定稿与本周归一稿。上周没有定稿时返回 ErrNotFinal。
func loadReviewInputs(ws *store.Workspace, week, personID string) (*store.Draft, *store.Draft, string, error) {
	prevWeek, err := PrevWeek(week)
	if err != nil {
		return nil, nil, "", err
	}
	prev, err := ws.FinalDraft(prevWeek, personID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil, "", fmt.Errorf("%w：%s 没有归一稿，请先生成并定稿", store.ErrNotFinal, prevWeek)
	}
	if err != nil {
		return nil, nil, "", err
	}
	cur, err := ws.ReadDraft(week, personID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, "", fmt.Errorf("%w: %s 还没有归一稿，请先生成", store.ErrNotFound, week)
		}
		return nil, nil, "", err
	}
	return prev, cur, prevWeek, nil
}

// LoadReview 返回对照结果。
func LoadReview(ws *store.Workspace, week, personID string) (*ReviewResult, error) {
	prev, cur, prevWeek, err := loadReviewInputs(ws, week, personID)
	if err != nil {
		return nil, err
	}
	res := ComputeReview(prev.Body, cur)
	res.PrevWeek = prevWeek
	if res.Name == "" {
		res.Name = prev.Name
	}
	return &res, nil
}

// SetReviewItem 人工设置一条计划的状态与对应项（source=human）。status 为空表示清除。
func SetReviewItem(ws *store.Workspace, week, personID, planID, status string, matches []string, note string) (*ReviewResult, error) {
	if status != "" && !store.ValidReviewStatus(status) {
		return nil, fmt.Errorf("%w: 非法状态 %q（done|partial|delayed|canceled）", store.ErrInvalid, status)
	}
	res, err := LoadReview(ws, week, personID)
	if err != nil {
		return nil, err
	}
	if !hasPlan(res, planID) {
		return nil, fmt.Errorf("%w: 上周计划里没有 id %s", store.ErrNotFound, planID)
	}
	done := map[string]bool{}
	for _, d := range res.Done {
		done[d.ID] = true
	}
	for _, m := range matches {
		if !done[m] {
			return nil, fmt.Errorf("%w: 本周完成里没有 id %s", store.ErrNotFound, m)
		}
	}
	cur, err := ws.ReadDraft(week, personID)
	if err != nil {
		return nil, err
	}
	review := reviewOf(cur)
	review.Items = upsertItem(review.Items, store.ReviewItem{
		Plan: planID, Status: status, Matches: uniq(matches), Note: strings.TrimSpace(note), Source: "human",
	})
	if _, err := ws.SaveDraftReview(week, personID, review); err != nil {
		return nil, err
	}
	return LoadReview(ws, week, personID)
}

// Carry 把「部分 / 延期」的上周计划顺延到本周「下周计划」，末尾加「（顺延）」。
// ids 为空表示全部可顺延的计划。已顺延或文本已存在的不重复加，可重复执行。
func Carry(ws *store.Workspace, week, personID string, ids []string, now time.Time) (*ReviewResult, int, error) {
	res, err := LoadReview(ws, week, personID)
	if err != nil {
		return nil, 0, err
	}
	want := map[string]bool{}
	for _, id := range ids {
		if !hasPlan(res, id) {
			return nil, 0, fmt.Errorf("%w: 上周计划里没有 id %s", store.ErrNotFound, id)
		}
		want[id] = true
	}
	existing := map[string]bool{}
	for _, n := range res.Next {
		existing[n.Text] = true
	}
	var (
		lines   []string
		carried []string
	)
	for _, p := range res.Plans {
		if len(want) > 0 && !want[p.ID] {
			continue
		}
		if p.Status != store.ReviewPartial && p.Status != store.ReviewDelayed {
			if len(want) > 0 {
				return nil, 0, fmt.Errorf("%w: 只有「部分完成 / 延期」的计划能顺延：%s", store.ErrInvalid, p.Text)
			}
			continue
		}
		if p.Carried {
			continue
		}
		carried = append(carried, p.ID)
		text := p.Text
		if !strings.HasSuffix(text, CarriedSuffix) {
			text += CarriedSuffix
		}
		if existing[text] || existing[p.Text] {
			continue
		}
		existing[text] = true
		lines = append(lines, text)
	}
	if len(carried) == 0 {
		return res, 0, nil
	}
	cur, err := ws.ReadDraft(week, personID)
	if err != nil {
		return nil, 0, err
	}
	if len(lines) > 0 {
		if cur, err = ws.EditDraft(week, personID, AppendToSection(cur.Body, SectionPlan, lines), now); err != nil {
			return nil, 0, err
		}
	}
	review := reviewOf(cur)
	review.Carried = uniq(append(review.Carried, carried...))
	if _, err := ws.SaveDraftReview(week, personID, review); err != nil {
		return nil, 0, err
	}
	out, err := LoadReview(ws, week, personID)
	return out, len(lines), err
}

// AppendToSection 在「## section」小节末尾追加列表项，去掉该节的「- 无」；
// 没有该节时在末尾新建。
func AppendToSection(body, section string, items []string) string {
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	start := -1
	for i, l := range lines {
		if t := strings.TrimSpace(l); strings.HasPrefix(t, "#") && sectionOf(t) == section {
			start = i
			break
		}
	}
	add := make([]string, 0, len(items))
	for _, it := range items {
		add = append(add, "- "+it)
	}
	if start < 0 {
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		lines = append(lines, "## "+section)
		return strings.Join(append(lines, add...), "\n")
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "#") {
			end = i
			break
		}
	}
	var sec []string
	for _, l := range lines[start+1 : end] {
		if t, ok := listItemText(strings.TrimSpace(l)); ok && t == "无" {
			continue
		}
		sec = append(sec, l)
	}
	// 小节末尾的空行留在新增条目之后。
	trail := 0
	for trail < len(sec) && strings.TrimSpace(sec[len(sec)-1-trail]) == "" {
		trail++
	}
	merged := append([]string{}, sec[:len(sec)-trail]...)
	merged = append(merged, add...)
	merged = append(merged, sec[len(sec)-trail:]...)
	out := append([]string{}, lines[:start+1]...)
	out = append(out, merged...)
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n")
}

// ReviewSuggestRequest 构造 AI 建议请求：上周计划与本周完成都带 id。
func ReviewSuggestRequest(res *ReviewResult) llm.Request {
	var b strings.Builder
	b.WriteString("上周计划：\n")
	for _, p := range res.Plans {
		fmt.Fprintf(&b, "- [%s] %s\n", p.ID, p.Text)
	}
	b.WriteString("\n本周完成：\n")
	for _, d := range res.Done {
		fmt.Fprintf(&b, "- [%s] %s\n", d.ID, d.Text)
	}
	return llm.Request{
		System: strings.TrimSpace(`你是研发小组长的周报核对助手。逐条判断上周计划在本周的完成情况。
只输出 JSON 数组，不要任何解释或代码围栏，每个元素：
{"plan": "上周计划 id", "status": "done|partial|delayed|canceled", "matches": ["对应的本周完成 id"]}
规则：done=已完成；partial=做了一部分；delayed=没做或推迟；canceled=原文明确说不做了。
matches 只能用给出的 id，可以为空数组。每条上周计划都要输出。`),
		Messages: []llm.Message{{Role: "user", Content: b.String()}},
	}
}

// ApplySuggestion 解析模型返回的 JSON 并写入（source=ai），不覆盖人工标过的条目。
// 返回写入的条数。
func ApplySuggestion(ws *store.Workspace, week, personID, text string) (*ReviewResult, int, error) {
	var sug []struct {
		Plan    string   `json:"plan"`
		Status  string   `json:"status"`
		Matches []string `json:"matches"`
	}
	raw := strings.TrimSpace(text)
	if i, j := strings.IndexByte(raw, '['), strings.LastIndexByte(raw, ']'); i >= 0 && j > i {
		raw = raw[i : j+1]
	}
	if err := json.Unmarshal([]byte(raw), &sug); err != nil {
		return nil, 0, fmt.Errorf("模型返回的不是预期的 JSON，请重试或人工标注: %w", err)
	}
	res, err := LoadReview(ws, week, personID)
	if err != nil {
		return nil, 0, err
	}
	done := map[string]bool{}
	for _, d := range res.Done {
		done[d.ID] = true
	}
	cur, err := ws.ReadDraft(week, personID)
	if err != nil {
		return nil, 0, err
	}
	review := reviewOf(cur)
	human := map[string]bool{}
	for _, it := range review.Items {
		if it.Source == "human" {
			human[it.Plan] = true
		}
	}
	applied := 0
	for _, s := range sug {
		if !hasPlan(res, s.Plan) || human[s.Plan] || !store.ValidReviewStatus(s.Status) {
			continue
		}
		var matches []string
		for _, m := range s.Matches {
			if done[m] {
				matches = append(matches, m)
			}
		}
		review.Items = upsertItem(review.Items, store.ReviewItem{Plan: s.Plan, Status: s.Status, Matches: uniq(matches), Source: "ai"})
		applied++
	}
	if applied > 0 {
		if _, err := ws.SaveDraftReview(week, personID, review); err != nil {
			return nil, 0, err
		}
	}
	out, err := LoadReview(ws, week, personID)
	return out, applied, err
}

func reviewOf(d *store.Draft) store.DraftReview {
	if d.Review == nil {
		return store.DraftReview{}
	}
	return *d.Review
}

// upsertItem 按 plan 替换或追加；status 为空表示删除该条。
func upsertItem(items []store.ReviewItem, it store.ReviewItem) []store.ReviewItem {
	out := items[:0:0]
	for _, x := range items {
		if x.Plan != it.Plan {
			out = append(out, x)
		}
	}
	if it.Status != "" {
		out = append(out, it)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Plan < out[j].Plan })
	return out
}

func hasPlan(res *ReviewResult, id string) bool {
	for _, p := range res.Plans {
		if p.ID == id {
			return true
		}
	}
	return false
}

func uniq(list []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range list {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
