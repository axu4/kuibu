package core

import (
	"strings"
	"testing"
	"time"

	"github.com/axu4/kuibu/internal/store"
)

// fixedQuickAddNow 是固定测试时间：2026-10-26 10:00 Asia/Shanghai（周一）。
func fixedQuickAddNow() time.Time {
	return time.Date(2026, 10, 26, 10, 0, 0, 0, store.TodoLocation())
}

func TestParseQuickAddTable(t *testing.T) {
	now := fixedQuickAddNow()

	cases := []struct {
		name    string
		in      string
		title   string
		dueDate string
		dueTime string
		tags    []string
		matches []QuickMatch
	}{
		{
			name:    "今天",
			in:      "今天交方案",
			title:   "交方案",
			dueDate: "2026-10-26",
		},
		{
			name:    "明天",
			in:      "明天交方案",
			title:   "交方案",
			dueDate: "2026-10-27",
		},
		{
			name:    "后天",
			in:      "后天交方案",
			title:   "交方案",
			dueDate: "2026-10-28",
		},
		{
			name:    "大后天",
			in:      "大后天交方案",
			title:   "交方案",
			dueDate: "2026-10-29",
		},
		{
			name:    "周一等于今天",
			in:      "周一交方案",
			title:   "交方案",
			dueDate: "2026-10-26",
		},
		{
			name:    "周日取最近",
			in:      "周日交方案",
			title:   "交方案",
			dueDate: "2026-11-01",
		},
		{
			name:    "本周一今天",
			in:      "本周一交方案",
			title:   "交方案",
			dueDate: "2026-10-26",
		},
		{
			name:    "本周日",
			in:      "本周日交方案",
			title:   "交方案",
			dueDate: "2026-11-01",
		},
		{
			name:    "下周一",
			in:      "下周一交方案",
			title:   "交方案",
			dueDate: "2026-11-02",
		},
		{
			name:    "下周一15点",
			in:      "下周一 15:00",
			title:   "下周一 15:00", // 只剩日期与时间，标题回退为原文
			dueDate: "2026-11-02",
			dueTime: "15:00",
			matches: []QuickMatch{
				{Kind: "date", Text: "下周一", Value: "2026-11-02"},
				{Kind: "time", Text: "15:00", Value: "15:00"},
			},
		},
		{
			name:    "下周三下午3点",
			in:      "下周三下午3点",
			title:   "下周三下午3点",
			dueDate: "2026-11-04",
			dueTime: "15:00",
		},
		{
			name:    "M-D",
			in:      "10-30交方案",
			title:   "交方案",
			dueDate: "2026-10-30",
		},
		{
			name:    "M-D单独",
			in:      "10-30",
			title:   "10-30",
			dueDate: "2026-10-30",
		},
		{
			name:    "M/D",
			in:      "10/30交方案",
			title:   "交方案",
			dueDate: "2026-10-30",
		},
		{
			name:    "M月D日",
			in:      "10月30日交方案",
			title:   "交方案",
			dueDate: "2026-10-30",
		},
		{
			name:    "M月D号",
			in:      "10月30号交方案",
			title:   "交方案",
			dueDate: "2026-10-30",
		},
		{
			name:    "M-D早于今天取明年",
			in:      "1-5交方案",
			title:   "交方案",
			dueDate: "2027-01-05",
		},
		{
			name:  "非法月日不识别",
			in:    "2-30交方案",
			title: "2-30交方案",
		},
		{
			name:    "YYYY-MM-DD",
			in:      "2026-11-02交方案",
			title:   "交方案",
			dueDate: "2026-11-02",
		},
		{
			name:    "YYYY年M月D日",
			in:      "2026年11月2日交方案",
			title:   "交方案",
			dueDate: "2026-11-02",
		},
		{
			name:  "版本号不误判",
			in:    "v1.10-2",
			title: "v1.10-2",
		},
		{
			name:  "M.D不识别",
			in:    "1.2",
			title: "1.2",
		},
		{
			name:  "每周一次不识别",
			in:    "每周一次例会",
			title: "每周一次例会",
		},
		{
			name:    "只有时间取今天",
			in:      "15:00 开会",
			title:   "开会",
			dueDate: "2026-10-26",
			dueTime: "15:00",
		},
		{
			name:    "晚上8点",
			in:      "晚上8点",
			title:   "晚上8点",
			dueDate: "2026-10-26",
			dueTime: "20:00",
		},
		{
			name:    "中午12点",
			in:      "中午12点",
			title:   "中午12点",
			dueDate: "2026-10-26",
			dueTime: "12:00",
		},
		{
			name:    "全角冒号",
			in:      "9：30 开会",
			title:   "开会",
			dueDate: "2026-10-26",
			dueTime: "09:30",
		},
		{
			name:  "多标签",
			in:    "#工作 #框架组",
			title: "#工作 #框架组",
			tags:  []string{"工作", "框架组"},
		},
		{
			name:  "纯数字不算标签",
			in:    "#123 开会",
			title: "#123 开会",
		},
		{
			name:    "中文混排无空格标签不识别",
			in:      "周四交方案#评审",
			title:   "交方案#评审",
			dueDate: "2026-10-29",
		},
		{
			name:    "只有日期和标签标题回退",
			in:      "明天 #工作",
			title:   "明天 #工作",
			dueDate: "2026-10-27",
			tags:    []string{"工作"},
		},
		{
			name:    "多个日期只取第一个",
			in:      "多个日期 明天 后天",
			title:   "多个日期 后天",
			dueDate: "2026-10-27",
		},
		{
			name:    "时间不跟日期则不识别",
			in:      "15:00 周四",
			title:   "15:00",
			dueDate: "2026-10-29",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseQuickAdd(c.in, now)
			if got.Title != c.title {
				t.Errorf("Title = %q，期望 %q", got.Title, c.title)
			}
			if c.dueDate == "" {
				if got.Due != nil {
					t.Errorf("Due = %+v，期望 nil", got.Due)
				}
			} else {
				if got.Due == nil || got.Due.Date != c.dueDate || got.Due.Time != c.dueTime {
					t.Errorf("Due = %+v，期望 date=%q time=%q", got.Due, c.dueDate, c.dueTime)
				}
			}
			if len(c.tags) == 0 {
				if len(got.Tags) != 0 {
					t.Errorf("Tags = %v，期望空", got.Tags)
				}
			} else if strings.Join(got.Tags, ",") != strings.Join(c.tags, ",") {
				t.Errorf("Tags = %v，期望 %v", got.Tags, c.tags)
			}
			if c.matches != nil {
				if len(got.Matches) != len(c.matches) {
					t.Fatalf("Matches = %+v，期望 %+v", got.Matches, c.matches)
				}
				for i := range c.matches {
					if got.Matches[i] != c.matches[i] {
						t.Errorf("Matches[%d] = %+v，期望 %+v", i, got.Matches[i], c.matches[i])
					}
				}
			}
		})
	}
}

func TestParseQuickAddEmptyAndRaw(t *testing.T) {
	now := fixedQuickAddNow()

	got := ParseQuickAdd("", now)
	if got.Title != "" || got.Due != nil || len(got.Tags) != 0 || len(got.Matches) != 0 {
		t.Errorf("空输入解析结果 = %+v", got)
	}

	// 无法识别任何日期/时间/标签时，标题保持原文（去首尾空白）。
	got = ParseQuickAdd("  写周报  ", now)
	if got.Title != "写周报" || got.Due != nil {
		t.Errorf("纯文本解析结果 = %+v", got)
	}
}

func TestDueRange(t *testing.T) {
	now := fixedQuickAddNow()

	cases := []struct {
		kind    string
		from    string
		to      string
		wantErr bool
	}{
		{kind: "today", from: "2026-10-26", to: "2026-10-26"},
		{kind: "tomorrow", from: "2026-10-27", to: "2026-10-27"},
		{kind: "week", from: "2026-10-26", to: "2026-11-01"},
		{kind: "bad", wantErr: true},
	}
	for _, c := range cases {
		from, to, err := DueRange(c.kind, now)
		if c.wantErr {
			if err == nil {
				t.Errorf("DueRange(%q) 期望报错", c.kind)
			}
			continue
		}
		if err != nil {
			t.Errorf("DueRange(%q) 错误: %v", c.kind, err)
			continue
		}
		if from != c.from || to != c.to {
			t.Errorf("DueRange(%q) = %q..%q，期望 %q..%q", c.kind, from, to, c.from, c.to)
		}
	}
}
