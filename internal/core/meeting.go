package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/axu4/kuibu/internal/store"
)

// MeetingType 是周会笔记的 type。
const MeetingType = "meeting"

// MeetingFolderTitle 是笔记树里存放周会的节点标题。
const MeetingFolderTitle = "周会"

// MeetingTag 是从周会笔记抽出的待办默认带的标签。
const MeetingTag = "周会"

// extractedKey 是笔记 front matter 里记录「行 → 待办」映射的字段。
const extractedKey = "extractedTodos"

// meetingTemplate 是新周会笔记的正文。
const meetingTemplate = "## 议题\n\n## 决定\n\n## 待办\n- [ ] \n"

// MeetingInfo 是一条周会笔记的摘要。
type MeetingInfo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Week  string `json:"week"`
	Date  string `json:"date"`
}

// OpenMeeting 打开某周的周会笔记；不存在时在「周会」节点下新建（节点也按需新建）。
// 同一周多次调用返回同一条笔记。
func OpenMeeting(ws *store.Workspace, week string) (*store.Note, bool, error) {
	if _, _, err := store.WeekRange(week); err != nil {
		return nil, false, err
	}
	notes, err := ws.List()
	if err != nil {
		return nil, false, err
	}
	folder := ""
	for _, n := range notes {
		if n.Type == MeetingType && extraString(n, "week") == week {
			note, err := ws.Get(n.ID)
			return note, false, err
		}
		if folder == "" && n.Parent == "" && n.Type != MeetingType && strings.TrimSpace(n.Title) == MeetingFolderTitle {
			folder = n.ID
		}
	}
	if folder == "" {
		f, err := ws.Create(store.NewNote{Title: MeetingFolderTitle, Type: store.FolderType})
		if err != nil {
			return nil, false, err
		}
		folder = f.ID
	} else {
		// 只有目录能包含子节点：老工作台的「周会」节点可能是普通笔记，就地转成目录。
		if _, err := ws.EnsureFolder(folder); err != nil {
			return nil, false, err
		}
	}
	weekday, err := ws.MeetingWeekday()
	if err != nil {
		return nil, false, err
	}
	day, err := store.WeekReportDay(week, weekday)
	if err != nil {
		return nil, false, err
	}
	n, err := ws.Create(store.NewNote{
		Title:  fmt.Sprintf("周会 %s（%s）", week, day.Format("01-02")),
		Type:   MeetingType,
		Parent: folder,
		Body:   meetingTemplate,
	})
	if err != nil {
		return nil, false, err
	}
	n, err = ws.Update(n.ID, func(note *store.Note) {
		if note.Extra == nil {
			note.Extra = map[string]any{}
		}
		note.Extra["week"] = week
		note.Extra["date"] = day.Format("2006-01-02")
	})
	return n, true, err
}

// ListMeetings 返回全部周会笔记，按周倒序。
func ListMeetings(ws *store.Workspace) ([]MeetingInfo, error) {
	notes, err := ws.List()
	if err != nil {
		return nil, err
	}
	out := []MeetingInfo{}
	for _, n := range notes {
		if n.Type != MeetingType {
			continue
		}
		out = append(out, MeetingInfo{ID: n.ID, Title: n.Title, Week: extraString(n, "week"), Date: extraString(n, "date")})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Week > out[j].Week })
	return out, nil
}

// TodoCandidate 是从笔记里识别出的一条待办。
type TodoCandidate struct {
	// Key 是行文本的哈希，用于去重与勾选。
	Key   string     `json:"key"`
	Line  string     `json:"line"`
	Title string     `json:"title"`
	Due   *store.Due `json:"due"`
	Tags  []string   `json:"tags"`
	// TodoID 非空表示这一行已经抽过（对应的待办 ID）。
	TodoID string `json:"todoId,omitempty"`
}

var reCheckbox = regexp.MustCompile(`^\s*[-*+]\s+\[ \]\s+(.*\S)\s*$`)

// lineKey 是待办行的去重键。
func lineKey(text string) string {
	sum := sha256.Sum256([]byte(strings.Join(strings.Fields(text), " ")))
	return hex.EncodeToString(sum[:])[:12]
}

// TodoCandidates 识别笔记里未勾选的「- [ ] …」行；已抽过的带 TodoID。
func TodoCandidates(note *store.Note, now time.Time) []TodoCandidate {
	done := extractedMap(note)
	out := []TodoCandidate{}
	seen := map[string]bool{}
	for _, line := range strings.Split(note.Body, "\n") {
		m := reCheckbox.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := lineKey(m[1])
		if seen[key] {
			continue
		}
		seen[key] = true
		qa := ParseQuickAdd(m[1], now)
		tags := append([]string{}, qa.Tags...)
		if note.Type == MeetingType && !containsString(tags, MeetingTag) {
			tags = append(tags, MeetingTag)
		}
		title := qa.Title
		if title == "" {
			title = strings.TrimSpace(m[1])
		}
		out = append(out, TodoCandidate{Key: key, Line: strings.TrimSpace(m[1]), Title: title, Due: qa.Due, Tags: tags, TodoID: done[key]})
	}
	return out
}

// ExtractTodos 把笔记里的待办行建成待办（source = 笔记 ID）。keys 为空表示全部未抽过的行；
// 已抽过的行跳过，可重复执行。返回新建的待办。
func ExtractTodos(ws *store.Workspace, noteID string, keys []string, now time.Time) ([]*store.Todo, error) {
	note, err := ws.Get(noteID)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, k := range keys {
		want[k] = true
	}
	done := extractedMap(note)
	var created []*store.Todo
	for _, c := range TodoCandidates(note, now) {
		if c.TodoID != "" || (len(want) > 0 && !want[c.Key]) {
			continue
		}
		t, err := ws.CreateTodo(store.NewTodo{Title: c.Title, Tags: c.Tags, Due: c.Due, Source: noteID, Now: now})
		if err != nil {
			return created, err
		}
		done[c.Key] = t.ID
		created = append(created, t)
	}
	if len(created) == 0 {
		return created, nil
	}
	_, err = ws.Update(noteID, func(n *store.Note) {
		if n.Extra == nil {
			n.Extra = map[string]any{}
		}
		m := map[string]any{}
		for k, v := range done {
			m[k] = v
		}
		n.Extra[extractedKey] = m
	})
	return created, err
}

// extractedMap 读取笔记 front matter 里已抽过的行。
func extractedMap(n *store.Note) map[string]string {
	out := map[string]string{}
	raw, ok := n.Extra[extractedKey].(map[string]any)
	if !ok {
		return out
	}
	for k, v := range raw {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

func extraString(n *store.Note, key string) string {
	if v, ok := n.Extra[key].(string); ok {
		return v
	}
	return ""
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
