package quicknote

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axu4/kuibu/internal/store"
)

// newWorkspace 创建隔离的临时工作台。
func newWorkspace(t *testing.T) *store.Workspace {
	t.Helper()
	w, err := store.Init(filepath.Join(t.TempDir(), "ws"))
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	return w
}

func TestEnsureInboxCreatesOnce(t *testing.T) {
	w := newWorkspace(t)

	first, err := EnsureInbox(w)
	if err != nil {
		t.Fatalf("首次 EnsureInbox 失败: %v", err)
	}
	if first.Title != InboxTitle || first.Type != InboxType || first.Parent != "" || first.Body != "" {
		t.Fatalf("收件箱字段不对: %+v", first)
	}

	second, err := EnsureInbox(w)
	if err != nil {
		t.Fatalf("第二次 EnsureInbox 失败: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("两次 EnsureInbox ID 不同：%q vs %q", first.ID, second.ID)
	}

	notes, err := w.List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	count := 0
	for _, n := range notes {
		if n.Type == InboxType {
			count++
		}
	}
	if count != 1 {
		t.Errorf("inbox 笔记数量 = %d，期望 1", count)
	}
}

func TestEnsureInboxIgnoresSameTitleDifferentType(t *testing.T) {
	w := newWorkspace(t)

	user, err := w.Create(store.NewNote{Title: InboxTitle})
	if err != nil {
		t.Fatalf("创建同名普通笔记失败: %v", err)
	}
	if user.Type == InboxType {
		t.Fatalf("普通笔记类型不应是 %q", InboxType)
	}

	inbox, err := EnsureInbox(w)
	if err != nil {
		t.Fatalf("EnsureInbox 失败: %v", err)
	}
	if inbox.ID == user.ID {
		t.Fatalf("EnsureInbox 误用了同名的普通笔记 %q", user.ID)
	}
	if inbox.Type != InboxType {
		t.Errorf("Type = %q，期望 %q", inbox.Type, InboxType)
	}
}

func TestSaveCreatesChildWithMultilineBody(t *testing.T) {
	w := newWorkspace(t)
	now := time.Date(2026, 3, 4, 9, 30, 0, 0, time.Local)

	note, err := Save(w, "  第一行标题\n\n第二段 **粗体**\n- 列表  ", now)
	if err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
	inbox, err := EnsureInbox(w)
	if err != nil {
		t.Fatalf("EnsureInbox 失败: %v", err)
	}
	if note.Parent != inbox.ID {
		t.Errorf("Parent = %q，期望收件箱 %q", note.Parent, inbox.ID)
	}
	wantBody := "第一行标题\n\n第二段 **粗体**\n- 列表"
	if note.Body != wantBody {
		t.Errorf("Body = %q，期望 %q", note.Body, wantBody)
	}
	if note.Title != "第一行标题" {
		t.Errorf("Title = %q，期望 %q", note.Title, "第一行标题")
	}

	read, err := w.Get(note.ID)
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if read.Body != wantBody {
		t.Errorf("落盘 Body = %q，期望 %q", read.Body, wantBody)
	}
	if read.Parent != inbox.ID {
		t.Errorf("落盘 Parent = %q，期望 %q", read.Parent, inbox.ID)
	}
}

func TestSaveNewestFirst(t *testing.T) {
	w := newWorkspace(t)
	now := time.Date(2026, 3, 4, 9, 30, 0, 0, time.Local)

	first, err := Save(w, "第一条", now)
	if err != nil {
		t.Fatalf("第一次 Save 失败: %v", err)
	}
	second, err := Save(w, "第二条", now)
	if err != nil {
		t.Fatalf("第二次 Save 失败: %v", err)
	}

	inbox, err := EnsureInbox(w)
	if err != nil {
		t.Fatalf("EnsureInbox 失败: %v", err)
	}
	notes, err := w.List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	var children []*store.Note
	for _, n := range notes {
		if n.Parent == inbox.ID {
			children = append(children, n)
		}
	}
	if len(children) != 2 {
		t.Fatalf("子笔记数量 = %d，期望 2", len(children))
	}
	var head *store.Note
	for _, n := range children {
		if head == nil || n.Order < head.Order {
			head = n
		}
	}
	if head.ID != second.ID {
		t.Errorf("最前面的是 %q，期望最新一条 %q（第一条 %q）", head.ID, second.ID, first.ID)
	}
	if second.Order != 0 {
		t.Errorf("最新条目 Order = %d，期望 0", second.Order)
	}
}

func TestSaveEmptyReturnsErrEmpty(t *testing.T) {
	w := newWorkspace(t)

	for _, text := range []string{"", "   ", "\n\t\r\n"} {
		if _, err := Save(w, text, time.Now()); !errors.Is(err, ErrEmpty) {
			t.Errorf("Save(%q) 错误 = %v，期望 ErrEmpty", text, err)
		}
	}
}

func TestTitle(t *testing.T) {
	now := time.Date(2026, 3, 4, 9, 5, 0, 0, time.Local)
	cases := []struct {
		name string
		text string
		want string
	}{
		{"普通首行", "  买牛奶  \n第二行", "买牛奶"},
		{"跳过空行", "\n\n  第一条非空\n", "第一条非空"},
		{"一级标题", "# 周会记录", "周会记录"},
		{"六级标题", "###### 深层标题", "深层标题"},
		{"不是标题的连写井号", "#没有空格", "#没有空格"},
		{"无序列表", "- 列表项", "列表项"},
		{"星号列表", "* 星号项", "星号项"},
		{"加号列表", "+ 加号项", "加号项"},
		{"引用", "> 被引用的话", "被引用的话"},
		{"未完成任务", "- [ ] 待办事项", "待办事项"},
		{"已完成任务", "- [x] 已完成", "已完成"},
		{"大写已完成任务", "- [X] 已完成", "已完成"},
		{"组合前缀", "> - [ ] # 组合前缀", "组合前缀"},
		{"长标题截断", strings.Repeat("字", 45), strings.Repeat("字", 40) + "…"},
		{"恰好 40 字不截断", strings.Repeat("字", 40), strings.Repeat("字", 40)},
		{"空内容回退", "  \n\t\n", "随手记 2026-03-04 09:05"},
		{"只有前缀回退", "- [ ] ", "随手记 2026-03-04 09:05"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Title(tc.text, now); got != tc.want {
				t.Errorf("Title(%q) = %q，期望 %q", tc.text, got, tc.want)
			}
		})
	}
}
