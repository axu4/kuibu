package store

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

const testMessageURL = "message://%3C20261029.abc123@mail.example.com%3E"

func TestValidateTodoURL(t *testing.T) {
	ok := []string{
		"https://example.com/a?b=c",
		"http://localhost:8080",
		"mailto:a@b.c",
		testMessageURL,
		"obsidian://open?vault=x&file=y",
		"x-apple-reminderable://ABC",
		"file:///Users/me/a.pdf",
	}
	for _, u := range ok {
		if err := ValidateTodoURL(u); err != nil {
			t.Errorf("ValidateTodoURL(%q) = %v，期望通过", u, err)
		}
	}
	bad := []string{
		"", "not-a-url", "://x", "1http://x", "https:", "https://a b",
		"javascript:alert(1)", "JavaScript:alert(1)", "data:text/html,hi",
	}
	for _, u := range bad {
		if err := ValidateTodoURL(u); !errors.Is(err, ErrInvalid) {
			t.Errorf("ValidateTodoURL(%q) = %v，期望 ErrInvalid", u, err)
		}
	}
}

func TestCheckLinkOpen(t *testing.T) {
	cases := []struct {
		url     string
		allowed []string
		want    string
	}{
		{"https://a.com", nil, LinkOpen},
		{"HTTPS://a.com", nil, LinkOpen},
		{testMessageURL, nil, LinkOpen},
		{"mailto:a@b.c", nil, LinkOpen},
		{"obsidian://open", nil, LinkConfirm},
		{"obsidian://open", []string{"obsidian"}, LinkOpen},
		{"https://a.com", []string{"obsidian"}, LinkConfirm},
		{"file:///tmp/a", nil, LinkConfirm},
		{"file:///tmp/a", []string{"file"}, LinkConfirm},
		{"javascript:alert(1)", []string{"javascript"}, LinkDeny},
		{"data:text/plain,x", nil, LinkDeny},
	}
	for _, c := range cases {
		got, _ := CheckLinkOpen(c.url, c.allowed)
		if got != c.want {
			t.Errorf("CheckLinkOpen(%q, %v) = %s，期望 %s", c.url, c.allowed, got, c.want)
		}
	}
}

func TestNormalizeLinkSchemes(t *testing.T) {
	got, err := NormalizeLinkSchemes([]string{" HTTPS ", "message://", "obsidian:", "https", ""})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"https", "message", "obsidian"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeLinkSchemes = %v，期望 %v", got, want)
	}
	for _, bad := range []string{"javascript", "data", "a b", "1x"} {
		if _, err := NormalizeLinkSchemes([]string{bad}); !errors.Is(err, ErrInvalid) {
			t.Errorf("NormalizeLinkSchemes(%q) = %v，期望 ErrInvalid", bad, err)
		}
	}
}

func TestTodoMessageLinkRoundTrip(t *testing.T) {
	w := mustInit(t)
	todo, err := w.CreateTodo(NewTodo{Title: "回邮件"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.AddTodoLink(todo.ID, TodoLink{URL: testMessageURL, Title: "原邮件"}, time.Time{}); err != nil {
		t.Fatalf("AddTodoLink(message://) 失败: %v", err)
	}
	got, err := w.GetTodo(todo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Links) != 1 || got.Links[0].URL != testMessageURL {
		t.Fatalf("链接 = %+v，期望 message:// 原样保存", got.Links)
	}
}

func TestCreateTodoPrioritySourceExternalID(t *testing.T) {
	w := mustInit(t)
	todo, err := w.CreateTodo(NewTodo{
		Title:      "回复客户",
		Status:     StatusDoing,
		Priority:   PriorityHigh,
		Source:     "mail",
		ExternalID: "<abc@example.com>",
		Links:      []TodoLink{{URL: testMessageURL}, {URL: testMessageURL, Title: "原邮件"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := w.GetTodo(todo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusDoing || got.Priority != PriorityHigh || got.Source != "mail" || got.ExternalID != "<abc@example.com>" {
		t.Fatalf("字段未保存: %+v", got)
	}
	if len(got.Links) != 1 || got.Links[0].Title != "原邮件" {
		t.Fatalf("链接应去重并保留标题: %+v", got.Links)
	}

	found, err := w.FindTodoByExternalID("<abc@example.com>")
	if err != nil || found == nil || found.ID != todo.ID {
		t.Fatalf("FindTodoByExternalID = %v, %v", found, err)
	}
	if none, err := w.FindTodoByExternalID("<none>"); err != nil || none != nil {
		t.Fatalf("不存在的 external_id 应返回 nil: %v, %v", none, err)
	}

	if _, err := w.CreateTodo(NewTodo{Title: "重复", ExternalID: "<abc@example.com>"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("重复 external_id = %v，期望 ErrDuplicate", err)
	}
	other, err := w.CreateTodo(NewTodo{Title: "另一条"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.UpdateTodo(other.ID, time.Time{}, func(t *Todo) error {
		t.ExternalID = "<abc@example.com>"
		return nil
	}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("改成重复 external_id = %v，期望 ErrDuplicate", err)
	}

	if _, err := w.CreateTodo(NewTodo{Title: "x", Priority: "urgent"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("非法优先级 = %v，期望 ErrInvalid", err)
	}
	if _, err := w.CreateTodo(NewTodo{Title: "x", Links: []TodoLink{{URL: "javascript:alert(1)"}}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("javascript: 链接 = %v，期望 ErrInvalid", err)
	}
}
