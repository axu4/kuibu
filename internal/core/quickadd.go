// Package core 是跬步的领域模型与业务逻辑所在，供 App 与 CLI 共用。
package core

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/axu4/kuibu/internal/store"
)

// QuickAdd 是一次快速录入解析的结果。
type QuickAdd struct {
	// Input 是原始输入文本。
	Input string `json:"input"`
	// Title 是去掉识别出的日期/时间与 #标签 后的标题，空白已压缩并首尾去空白。
	Title string `json:"title"`
	// Due 是解析出的截止日期，无日期时 JSON 序列化为 null。
	Due *store.Due `json:"due"`
	// Tags 是解析出的标签，去重保持顺序，空时为 []。
	Tags []string `json:"tags"`
	// Matches 是识别明细，空时为 []。
	Matches []QuickMatch `json:"matches"`
}

// QuickMatch 是一次识别到的表达式。
type QuickMatch struct {
	// Kind 取值 "date"、"time" 或 "tag"。
	Kind string `json:"kind"`
	// Text 是原始文本里匹配到的片段。
	Text string `json:"text"`
	// Value 是规范化后的值：日期为 YYYY-MM-DD，时间为 HH:MM，标签为标签内容。
	Value string `json:"value"`
}

// weekdayNum 把中文星期字符映射为 1（周一）到 7（周日）。
var weekdayNum = map[rune]int{
	'一': 1,
	'二': 2,
	'三': 3,
	'四': 4,
	'五': 5,
	'六': 6,
	'日': 7,
	'天': 7,
}

// relativeDayWords 是相对日期词与偏移天数。
var relativeDayWords = []struct {
	word string
	days int
}{
	{"大后天", 3},
	{"后天", 2},
	{"明天", 1},
	{"明日", 1},
	{"今天", 0},
	{"今日", 0},
}

// weekdayPrefixes 是按「前缀 + 星期几」识别的表达式。base 表示周的偏移：
// 0 为本周，1 为下周；kind 为 "this" 时本周、为 "next" 时下周。
var weekdayPrefixes = []struct {
	word   string
	offset int
}{
	{"下星期", 1},
	{"下礼拜", 1},
	{"下周", 1},
	{"本周", 0},
	{"这周", 0},
}

// standaloneWeekdayPrefixes 是「周X/星期X/礼拜X」的前缀，从今天起取最近一天。
var standaloneWeekdayPrefixes = []string{"星期", "礼拜", "周"}

// ParseQuickAdd 解析快速录入文本。
//
// now 会先转换到 store.TodoLocation()（Asia/Shanghai）。规则：
//   - 只识别第一处日期表达（最左匹配，同位置取最长）；后续日期原样留在标题。
//     日期支持相对日、周X、M-D、M月D日、月底/下月底 等。
//   - 时间可选，必须紧跟日期（中间允许空格）；没有日期时取第一处时间表达，日期为今天。
//     小时与分钟都支持阿拉伯数字与中文数字（三点、十点半、九点十五、下午3点半 等）。
//   - 标签 #xxx / ＃xxx，必须位于开头或空白之后，内容不能为空且不能是纯数字。
//
// 解析后若标题为空，则回退为原始输入去首尾空白。
func ParseQuickAdd(text string, now time.Time) QuickAdd {
	now = now.In(store.TodoLocation())
	rs := []rune(text)

	out := QuickAdd{
		Input:   text,
		Tags:    []string{},
		Matches: []QuickMatch{},
	}

	type rawMatch struct {
		pos   int
		match QuickMatch
	}
	var raws []rawMatch

	// 1. 日期：取最左匹配，同位置取最长。
	dateStart, dateEnd, dateVal, hasDate := findDate(rs, now)
	if hasDate {
		raws = append(raws, rawMatch{dateStart, QuickMatch{Kind: "date", Text: string(rs[dateStart:dateEnd]), Value: dateVal}})
	}

	// 2. 时间：紧跟日期（允许空格），或没有日期时取第一处时间表达。
	timeStart, timeEnd, timeVal, hasTime := findTime(rs, dateEnd, hasDate)
	if hasTime {
		raws = append(raws, rawMatch{timeStart, QuickMatch{Kind: "time", Text: string(rs[timeStart:timeEnd]), Value: timeVal}})
	}

	// 3. 标签。
	tagMatches := findTags(rs)
	for _, tg := range tagMatches {
		raws = append(raws, rawMatch{tg.start, QuickMatch{Kind: "tag", Text: string(rs[tg.start:tg.end]), Value: tg.value}})
		out.Tags = append(out.Tags, tg.value)
	}
	out.Tags = dedupStrings(out.Tags)

	// 4. 标题：去掉识别出的日期/时间/标签片段，压缩空白。
	removed := make([][2]int, 0, len(raws))
	for _, r := range raws {
		var end int
		switch r.match.Kind {
		case "date":
			end = dateEnd
		case "time":
			end = timeEnd
		case "tag":
			for _, tg := range tagMatches {
				if tg.start == r.pos {
					end = tg.end
					break
				}
			}
		}
		removed = append(removed, [2]int{r.pos, end})
	}
	title := strings.Join(strings.Fields(removeSpans(rs, removed)), " ")
	if title == "" {
		title = strings.TrimSpace(text)
	}
	out.Title = title

	// 5. 截止日期。
	if hasDate {
		out.Due = &store.Due{Date: dateVal}
		if hasTime {
			out.Due.Time = timeVal
		}
	} else if hasTime {
		out.Due = &store.Due{Date: now.Format("2006-01-02"), Time: timeVal}
	}

	// 6. 匹配明细按在原文中的位置排序。
	sort.SliceStable(raws, func(i, j int) bool { return raws[i].pos < raws[j].pos })
	out.Matches = make([]QuickMatch, 0, len(raws))
	for _, r := range raws {
		out.Matches = append(out.Matches, r.match)
	}
	return out
}

// DueRange 返回常用截止日期范围：today / week / tomorrow。
//
// today 返回今天；week 返回本周一到本周日；tomorrow 返回明天；其他 kind 返回错误。
// 日期均为 YYYY-MM-DD，按 store.TodoLocation() 计算。
func DueRange(kind string, now time.Time) (from, to string, err error) {
	now = now.In(store.TodoLocation())
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch kind {
	case "today":
		return today.Format("2006-01-02"), today.Format("2006-01-02"), nil
	case "tomorrow":
		d := today.AddDate(0, 0, 1)
		return d.Format("2006-01-02"), d.Format("2006-01-02"), nil
	case "week":
		offset := weekday1to7(now) - 1
		monday := today.AddDate(0, 0, -offset)
		sunday := monday.AddDate(0, 0, 6)
		return monday.Format("2006-01-02"), sunday.Format("2006-01-02"), nil
	default:
		return "", "", fmt.Errorf("未知的日期范围 %q", kind)
	}
}

// findDate 在 rune 切片里找第一处日期表达，返回 (start, end, value, ok)。
func findDate(rs []rune, now time.Time) (int, int, string, bool) {
	for i := 0; i < len(rs); i++ {
		if end, value, ok := matchDateAt(rs, i, now); ok {
			return i, end, value, true
		}
	}
	return 0, 0, "", false
}

// matchDateAt 尝试匹配从 i 开始的日期表达，返回 (end, value, ok)。同位置取最长。
func matchDateAt(rs []rune, i int, now time.Time) (int, string, bool) {
	bestEnd := -1
	bestValue := ""
	set := func(end int, value string) {
		if end > bestEnd {
			bestEnd = end
			bestValue = value
		}
	}

	// 今天/明天/后天/大后天。
	for _, w := range relativeDayWords {
		if hasRunePrefix(rs[i:], w.word) {
			date := now.AddDate(0, 0, w.days).Format("2006-01-02")
			set(i+len([]rune(w.word)), date)
		}
	}

	// 月底/下月底/下个月底：本月/下月的最后一天。
	if hasRunePrefix(rs[i:], "下个月底") {
		set(i+4, lastDayOfMonth(now, 1))
	}
	if hasRunePrefix(rs[i:], "下月底") {
		set(i+3, lastDayOfMonth(now, 1))
	}
	if hasRunePrefix(rs[i:], "月底") {
		set(i+2, lastDayOfMonth(now, 0))
	}

	// 周X/星期X/礼拜X：前一个字是「每」时不识别。
	if i == 0 || rs[i-1] != '每' {
		for _, p := range weekdayPrefixes {
			if hasRunePrefix(rs[i:], p.word) {
				if v, ok := weekdayAfter(rs, i+len([]rune(p.word))); ok {
					date := weekdayInWeek(now, v, p.offset).Format("2006-01-02")
					set(i+len([]rune(p.word))+1, date)
				}
			}
		}
		for _, p := range standaloneWeekdayPrefixes {
			if hasRunePrefix(rs[i:], p) {
				if v, ok := weekdayAfter(rs, i+len([]rune(p))); ok {
					date := nextWeekday(now, v).Format("2006-01-02")
					set(i+len([]rune(p))+1, date)
				}
			}
		}
	}

	// YYYY-MM-DD、YYYY/MM/DD。
	if y, j, ok := digitsAt(rs, i, 4); ok && j-i == 4 && j < len(rs) && (rs[j] == '-' || rs[j] == '/') {
		sep := rs[j]
		if m, k, ok2 := digitsAt(rs, j+1, 2); ok2 && k < len(rs) && rs[k] == sep {
			if d, l, ok3 := digitsAt(rs, k+1, 2); ok3 && numericBoundary(rs, i, l) && validDate(y, m, d) {
				set(l, time.Date(y, time.Month(m), d, 0, 0, 0, 0, now.Location()).Format("2006-01-02"))
			}
		}
	}

	// YYYY年M月D日。
	if y, j, ok := digitsAt(rs, i, 4); ok && j-i == 4 && j < len(rs) && rs[j] == '年' {
		if m, k, ok2 := digitsAt(rs, j+1, 2); ok2 && k < len(rs) && rs[k] == '月' {
			if d, l, ok3 := digitsAt(rs, k+1, 2); ok3 && l < len(rs) && rs[l] == '日' && digitBoundary(rs, i, l+1) && validDate(y, m, d) {
				set(l+1, time.Date(y, time.Month(m), d, 0, 0, 0, 0, now.Location()).Format("2006-01-02"))
			}
		}
	}

	// M-D、M/D。
	if m, j, ok := digitsAt(rs, i, 2); ok && j < len(rs) && (rs[j] == '-' || rs[j] == '/') {
		if d, k, ok2 := digitsAt(rs, j+1, 2); ok2 && numericBoundary(rs, i, k) {
			if date, ok3 := resolveMonthDay(now, m, d); ok3 {
				set(k, date)
			}
		}
	}

	// M月D日、M月D号。
	if m, j, ok := digitsAt(rs, i, 2); ok && j < len(rs) && rs[j] == '月' {
		if d, k, ok2 := digitsAt(rs, j+1, 2); ok2 && k < len(rs) && (rs[k] == '日' || rs[k] == '号') && digitBoundary(rs, i, k+1) {
			if date, ok3 := resolveMonthDay(now, m, d); ok3 {
				set(k+1, date)
			}
		}
	}

	if bestEnd < 0 {
		return 0, "", false
	}
	return bestEnd, bestValue, true
}

// findTime 找时间表达。有日期时要求紧跟日期（允许空格），否则取第一处。
func findTime(rs []rune, dateEnd int, hasDate bool) (int, int, string, bool) {
	if hasDate {
		j := dateEnd
		for j < len(rs) && unicode.IsSpace(rs[j]) {
			j++
		}
		if end, h, m, ok := matchTimeAt(rs, j); ok {
			return j, end, fmt.Sprintf("%02d:%02d", h, m), true
		}
		return 0, 0, "", false
	}
	for i := 0; i < len(rs); i++ {
		if end, h, m, ok := matchTimeAt(rs, i); ok {
			return i, end, fmt.Sprintf("%02d:%02d", h, m), true
		}
	}
	return 0, 0, "", false
}

// matchTimeAt 尝试匹配从 i 开始的时间表达，返回 (end, hour, minute, ok)。
func matchTimeAt(rs []rune, i int) (int, int, int, bool) {
	j := i
	prefix := ""
	for _, p := range []string{"上午", "早上", "中午", "下午", "晚上", "傍晚", "凌晨"} {
		if hasRunePrefix(rs[j:], p) {
			prefix = p
			j += len([]rune(p))
			break
		}
	}
	for j < len(rs) && unicode.IsSpace(rs[j]) {
		j++
	}

	// 保守边界：数字串中间（如「二十五点」里的「十五点」）与「第3点」「第二点」不当作时间。
	if j > 0 && (isNumeralRune(rs[j-1]) || rs[j-1] == '第') {
		return 0, 0, 0, false
	}

	h, m, end, ok := matchTimeToken(rs, j)
	if !ok {
		return 0, 0, 0, false
	}
	switch prefix {
	case "下午", "晚上", "傍晚":
		if h < 12 {
			h += 12
		}
	case "中午":
		if h < 11 {
			h += 12
		}
	}
	if h > 23 || m > 59 {
		return 0, 0, 0, false
	}
	return end, h, m, true
}

// matchTimeToken 匹配不含前缀的时间片段。
func matchTimeToken(rs []rune, i int) (hour, minute, end int, ok bool) {
	// HH:MM 或 HH：MM（冒号形式仅支持阿拉伯数字）。
	if h, j, ok := digitsAt(rs, i, 2); ok && j < len(rs) && (rs[j] == ':' || rs[j] == '：') {
		if m, k, ok2 := digitsAt(rs, j+1, 2); ok2 && k-j-1 == 2 {
			return h, m, k, true
		}
	}

	// N点M分、N点半、N点一刻、N点三刻、N点M、N点。
	h, j, ok := hourNumberAt(rs, i)
	if !ok || j >= len(rs) || rs[j] != '点' {
		return 0, 0, 0, false
	}
	// 「一点点」等：点后紧跟点不当作时间。
	if j+1 < len(rs) && rs[j+1] == '点' {
		return 0, 0, 0, false
	}
	m, k, ok2, invalid := minuteAfterDian(rs, j+1)
	if invalid {
		return 0, 0, 0, false
	}
	if ok2 {
		return h, m, k, true
	}
	return h, 0, j + 1, true
}

// minuteAfterDian 解析「点」之后的部分，返回 (minute, end, ok, invalid)。
// invalid 表示出现了形如「二刻」这种不支持但明显属于时间的写法，整体不识别。
func minuteAfterDian(rs []rune, i int) (minute, end int, ok, invalid bool) {
	if i >= len(rs) {
		return 0, i, false, false
	}
	if rs[i] == '半' {
		return 30, i + 1, true, false
	}
	v, j, okNum := minuteNumberAt(rs, i)
	if !okNum {
		return 0, i, false, false
	}
	// 数字串后续仍是数字（如「九点123分」）时不认为时间。
	if j < len(rs) && isNumeralRune(rs[j]) {
		return 0, i, false, true
	}
	if j < len(rs) && rs[j] == '刻' {
		switch v {
		case 1:
			return 15, j + 1, true, false
		case 3:
			return 45, j + 1, true, false
		default:
			return 0, i, false, true
		}
	}
	if j < len(rs) && rs[j] == '分' {
		return v, j + 1, true, false
	}
	return v, j, true, false
}

// hourNumberAt 读取小时数字，支持阿拉伯数字与中文数字。
func hourNumberAt(rs []rune, i int) (int, int, bool) {
	if v, j, ok := digitsAt(rs, i, 2); ok {
		return v, j, true
	}
	return chineseNumberAt(rs, i)
}

// minuteNumberAt 读取分钟数字，支持阿拉伯数字与中文数字。
func minuteNumberAt(rs []rune, i int) (int, int, bool) {
	return hourNumberAt(rs, i)
}

// cnDigitValue 把中文数字字符映射为 0-9 的值。
func cnDigitValue(c rune) (int, bool) {
	switch c {
	case '零', '〇':
		return 0, true
	case '一':
		return 1, true
	case '二', '两':
		return 2, true
	case '三':
		return 3, true
	case '四':
		return 4, true
	case '五':
		return 5, true
	case '六':
		return 6, true
	case '七':
		return 7, true
	case '八':
		return 8, true
	case '九':
		return 9, true
	}
	return 0, false
}

// chineseNumberAt 解析中文数字，支持 零/〇、一..九、两、十、十X、X十、X十Y。
// 采用最长匹配：如「二十五」整体解析为 25，不会退化成「二十」+「五」。
func chineseNumberAt(rs []rune, i int) (int, int, bool) {
	if i >= len(rs) {
		return 0, i, false
	}
	c := rs[i]
	if c == '零' || c == '〇' {
		// 零X 表示 0X，用于分钟（如「九点零五」）。
		if i+1 < len(rs) {
			if v, ok := cnDigitValue(rs[i+1]); ok && v > 0 {
				return v, i + 2, true
			}
		}
		return 0, i + 1, true
	}
	if c == '十' {
		if i+1 < len(rs) {
			if d, ok := cnDigitValue(rs[i+1]); ok && d > 0 {
				return 10 + d, i + 2, true
			}
		}
		return 10, i + 1, true
	}
	d, ok := cnDigitValue(c)
	if !ok || d == 0 {
		return 0, i, false
	}
	if i+1 < len(rs) && rs[i+1] == '十' {
		v := d * 10
		if i+2 < len(rs) {
			if d2, ok := cnDigitValue(rs[i+2]); ok && d2 > 0 {
				return v + d2, i + 3, true
			}
		}
		return v, i + 2, true
	}
	return d, i + 1, true
}

// isNumeralRune 判断是否为数字字符（阿拉伯或中文数字），用于避免从数字串中间开始匹配。
func isNumeralRune(c rune) bool {
	if isDigit(c) {
		return true
	}
	if c == '十' {
		return true
	}
	_, ok := cnDigitValue(c)
	return ok
}

// tagMatch 是一处标签匹配。
type tagMatch struct {
	start int
	end   int
	value string
}

// findTags 找出全部合法标签。# 必须位于开头或空白之后，内容不能为空且不能是纯数字。
func findTags(rs []rune) []tagMatch {
	var out []tagMatch
	for i := 0; i < len(rs); i++ {
		if rs[i] != '#' && rs[i] != '＃' {
			continue
		}
		if i > 0 && !unicode.IsSpace(rs[i-1]) {
			continue
		}
		j := i + 1
		for j < len(rs) && !unicode.IsSpace(rs[j]) && rs[j] != '#' && rs[j] != '＃' {
			j++
		}
		content := string(rs[i+1 : j])
		if content == "" || allDigits(content) {
			continue
		}
		out = append(out, tagMatch{start: i, end: j, value: content})
	}
	return out
}

// lastDayOfMonth 返回相对 now 所在月份偏移 offset 个月的最后一天（YYYY-MM-DD）。
// offset 为 0 表示本月，1 表示下个月；12 月会自动跨年。
func lastDayOfMonth(now time.Time, offset int) string {
	t := time.Date(now.Year(), now.Month()+time.Month(offset)+1, 0, 0, 0, 0, 0, now.Location())
	return t.Format("2006-01-02")
}

// resolveMonthDay 把 M-D 解析为今年（早于今天则明年）的日期。
func resolveMonthDay(now time.Time, m, d int) (string, bool) {
	y := now.Year()
	if !validDate(y, m, d) {
		return "", false
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	date := time.Date(y, time.Month(m), d, 0, 0, 0, 0, now.Location())
	if date.Before(today) {
		y++
		if !validDate(y, m, d) {
			return "", false
		}
		date = time.Date(y, time.Month(m), d, 0, 0, 0, 0, now.Location())
	}
	return date.Format("2006-01-02"), true
}

// weekday1to7 返回周一=1 到周日=7 的星期编号。
func weekday1to7(t time.Time) int {
	if t.Weekday() == time.Sunday {
		return 7
	}
	return int(t.Weekday())
}

// nextWeekday 返回从今天起（含今天）最近的指定星期几。
func nextWeekday(now time.Time, weekday int) time.Time {
	diff := (weekday - weekday1to7(now) + 7) % 7
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, diff)
}

// weekdayInWeek 返回第 offset 周（0=本周，1=下周）的指定星期几。
func weekdayInWeek(now time.Time, weekday, offset int) time.Time {
	monday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).
		AddDate(0, 0, -(weekday1to7(now)-1)+offset*7)
	return monday.AddDate(0, 0, weekday-1)
}

// weekdayAfter 读取 rs[pos] 是否为星期字符，返回其编号。
func weekdayAfter(rs []rune, pos int) (int, bool) {
	if pos >= len(rs) {
		return 0, false
	}
	v, ok := weekdayNum[rs[pos]]
	return v, ok
}

// digitsAt 从 i 开始读取最多 maxLen 位十进制数字，返回 (值, 结束位置, ok)。
func digitsAt(rs []rune, i, maxLen int) (int, int, bool) {
	if i >= len(rs) || !isDigit(rs[i]) {
		return 0, i, false
	}
	j := i
	val := 0
	for j < len(rs) && j-i < maxLen && isDigit(rs[j]) {
		val = val*10 + int(rs[j]-'0')
		j++
	}
	return val, j, true
}

// isDigit 判断是否为 ASCII 数字。
func isDigit(c rune) bool { return c >= '0' && c <= '9' }

// validDate 判断年月日是否构成合法日期。
func validDate(y, m, d int) bool {
	if m < 1 || m > 12 || d < 1 || d > 31 {
		return false
	}
	t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	return t.Year() == y && int(t.Month()) == m && t.Day() == d
}

// numericBoundary 要求数字型日期前后不紧挨数字或 -/.，避免版本号等误判。
func numericBoundary(rs []rune, start, end int) bool {
	if start > 0 && isNumOrSep(rs[start-1]) {
		return false
	}
	if end < len(rs) && isNumOrSep(rs[end]) {
		return false
	}
	return true
}

// digitBoundary 要求日期前后不紧挨数字。
func digitBoundary(rs []rune, start, end int) bool {
	if start > 0 && isDigit(rs[start-1]) {
		return false
	}
	if end < len(rs) && isDigit(rs[end]) {
		return false
	}
	return true
}

// isNumOrSep 判断是否为数字或常见分隔符。
func isNumOrSep(c rune) bool {
	return isDigit(c) || c == '-' || c == '/' || c == '.'
}

// allDigits 判断字符串是否全部由 ASCII 数字组成（空串返回 false）。
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !isDigit(r) {
			return false
		}
	}
	return true
}

// hasRunePrefix 判断 rune 切片是否以 s 开头。
func hasRunePrefix(rs []rune, s string) bool {
	sr := []rune(s)
	if len(rs) < len(sr) {
		return false
	}
	for i := range sr {
		if rs[i] != sr[i] {
			return false
		}
	}
	return true
}

// removeSpans 去掉 spans 覆盖的 rune 区间，返回剩余文本。
func removeSpans(rs []rune, spans [][2]int) string {
	if len(spans) == 0 {
		return string(rs)
	}
	removed := make([]bool, len(rs))
	for _, s := range spans {
		for k := s[0]; k < s[1] && k < len(rs); k++ {
			removed[k] = true
		}
	}
	var b strings.Builder
	for i, r := range rs {
		if !removed[i] {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// dedupStrings 去重保持顺序，过滤空串。
func dedupStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
