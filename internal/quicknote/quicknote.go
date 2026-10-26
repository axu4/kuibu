// Package quicknote 实现随手记收件箱：把零散想法快速追加到工作台里的
// 「随手记」笔记下，最新条目排在最前面。
//
// 本包只通过 internal/store 的公开 API 读写笔记，不自己拼路径操作文件。
package quicknote

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/axu4/kuibu/internal/store"
)

// InboxType 是随手记收件箱笔记的类型。
const InboxType = "inbox"

// InboxTitle 是随手记收件箱的默认标题。
const InboxTitle = "随手记"

// ErrEmpty 表示要保存的随手记内容为空。
var ErrEmpty = errors.New("随手记内容为空")

// maxTitleRunes 是标题的最大字符数（按 rune 计），超出部分截断。
const maxTitleRunes = 40

// EnsureInbox 返回工作台根节点中的随手记收件箱，不存在时自动创建。
//
// 根节点中存在多条 Type 为 InboxType 的笔记时，取 Order 最小、再按 ID
// 排序的第一个。只认 Type，不认标题，避免误用用户手建的同名笔记。
func EnsureInbox(ws *store.Workspace) (*store.Note, error) {
	if ws == nil {
		return nil, fmt.Errorf("%w: 工作台为空", store.ErrInvalid)
	}
	notes, err := ws.List()
	if err != nil {
		return nil, err
	}

	var candidates []*store.Note
	for _, n := range notes {
		if n.Parent == "" && n.Type == InboxType {
			candidates = append(candidates, n)
		}
	}
	if len(candidates) > 0 {
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].Order != candidates[j].Order {
				return candidates[i].Order < candidates[j].Order
			}
			return candidates[i].ID < candidates[j].ID
		})
		return candidates[0], nil
	}

	return ws.Create(store.NewNote{
		Title: InboxTitle,
		Type:  InboxType,
		Body:  "",
	})
}

// Save 把 text 作为一条新随手记写入收件箱，返回新建的笔记。
//
// text 去掉首尾空白后为空时返回 ErrEmpty；正文保留去掉首尾空白后的原文
// （内部多行 Markdown 原样保留）。新条目排在收件箱子节点的最前面。
func Save(ws *store.Workspace, text string, now time.Time) (*store.Note, error) {
	body := strings.TrimSpace(text)
	if body == "" {
		return nil, ErrEmpty
	}

	inbox, err := EnsureInbox(ws)
	if err != nil {
		return nil, err
	}

	note, err := ws.Create(store.NewNote{
		Title:  Title(text, now),
		Body:   body,
		Parent: inbox.ID,
	})
	if err != nil {
		return nil, err
	}

	// store.Create 只能追加到同级末尾，这里再移到最前面，实现「最新在上」。
	if err := ws.Move(note.ID, inbox.ID, 0); err != nil {
		return nil, fmt.Errorf("移动随手记到收件箱最前面失败: %w", err)
	}
	return ws.Get(note.ID)
}

// Title 从随手记正文推导标题。
//
// 取第一个非空行，去掉 Markdown 标题前缀（# ~ ###### 加空格）、列表/引用
// 前缀（- * + > 以及任务列表的 "- [ ] "/"- [x] "）与首尾空白；超过 40 个
// 字符（按 rune）截断并加「…」。结果为空时返回带时间的默认标题。
func Title(text string, now time.Time) string {
	var line string
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(strings.TrimRight(l, "\r"))
		if l != "" {
			line = l
			break
		}
	}

	line = stripPrefixes(line)
	if line == "" {
		return InboxTitle + " " + now.Format("2006-01-02 15:04")
	}
	if utf8.RuneCountInString(line) > maxTitleRunes {
		runes := []rune(line)
		return string(runes[:maxTitleRunes]) + "…"
	}
	return line
}

// stripPrefixes 反复去掉行首的 Markdown 标题、列表、引用与任务列表前缀，
// 直到没有可去掉的前缀为止（例如 "> - [ ] 任务" 会得到 "任务"）。
func stripPrefixes(line string) string {
	for {
		before := line
		line = strings.TrimSpace(line)
		line = trimHeadingPrefix(line)
		line = trimTaskPrefix(line)
		line = trimListOrQuotePrefix(line)
		if line == before {
			return line
		}
	}
}

// trimHeadingPrefix 去掉 "# " ~ "###### " 形式的 Markdown 标题前缀。
func trimHeadingPrefix(line string) string {
	n := 0
	for n < len(line) && line[n] == '#' && n < 6 {
		n++
	}
	if n == 0 || n >= len(line) {
		return line
	}
	if line[n] != ' ' && line[n] != '\t' {
		return line
	}
	return strings.TrimLeft(line[n:], " \t")
}

// trimTaskPrefix 去掉 "- [ ] "、"- [x] " 形式的任务列表前缀。
func trimTaskPrefix(line string) string {
	if len(line) < 5 {
		return line
	}
	if line[0] != '-' || line[1] != ' ' || line[2] != '[' || line[4] != ']' {
		return line
	}
	if line[3] != ' ' && line[3] != 'x' && line[3] != 'X' {
		return line
	}
	if len(line) > 5 && line[5] != ' ' && line[5] != '\t' {
		return line
	}
	return strings.TrimLeft(line[5:], " \t")
}

// trimListOrQuotePrefix 去掉 "- "、"* "、"+ "、">" 形式的列表或引用前缀。
func trimListOrQuotePrefix(line string) string {
	if line == "" {
		return line
	}
	switch line[0] {
	case '-', '*', '+':
		if len(line) > 1 && (line[1] == ' ' || line[1] == '\t') {
			return strings.TrimLeft(line[1:], " \t")
		}
	case '>':
		return strings.TrimLeft(line[1:], " \t")
	}
	return line
}
