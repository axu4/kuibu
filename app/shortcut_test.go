package main

import "testing"

func TestNormalizeShortcut(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"默认值", "Ctrl+Alt+N", "Ctrl+Alt+N"},
		{"小写与多余空白", "  ctrl + option + n  ", "Ctrl+Alt+N"},
		{"修饰键顺序规范", "Shift+Cmd+K", "Shift+Cmd+K"},
		{"命令别名", "command+shift+space", "Shift+Cmd+Space"},
		{"单字符大写", "Ctrl+Shift+z", "Ctrl+Shift+Z"},
		{"去除重复修饰键", "Ctrl+Ctrl+Alt+N", "Ctrl+Alt+N"},
		{"功能键", "Ctrl+F5", "Ctrl+F5"},
		{"回车", "Cmd+Return", "Cmd+Enter"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeShortcut(tc.input)
			if err != nil {
				t.Fatalf("normalizeShortcut(%q) 返回错误: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("normalizeShortcut(%q) = %q，期望 %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeShortcutRejects(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"空字符串", ""},
		{"只有空白", "   "},
		{"无修饰键", "N"},
		{"纯修饰键", "Ctrl"},
		{"纯修饰键组合", "Ctrl+Alt"},
		{"未知修饰键", "Hyper+N"},
		{"缺少主键", "Ctrl+Alt+"},
		{"未知按键名", "Ctrl+Alt+NotAKey"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := normalizeShortcut(tc.input); err == nil {
				t.Errorf("normalizeShortcut(%q) = %q，期望返回错误", tc.input, got)
			}
		})
	}
}

func TestShortcutDisplay(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"默认值", "Ctrl+Alt+N", "⌃⌥N"},
		{"四个修饰键", "Cmd+Shift+Alt+Ctrl+K", "⌃⌥⇧⌘K"},
		{"空格", "Cmd+Shift+Space", "⇧⌘Space"},
		{"方向键", "Ctrl+Left", "⌃←"},
		{"非法输入原样返回", "N", "N"},
		{"空字符串", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shortcutDisplay(tc.input); got != tc.want {
				t.Errorf("shortcutDisplay(%q) = %q，期望 %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestTrayNewNoteLabel(t *testing.T) {
	if got, want := trayNewNoteLabel("Ctrl+Alt+N"), "新建随手记  ⌃⌥N"; got != want {
		t.Errorf("trayNewNoteLabel = %q，期望 %q", got, want)
	}
	if got, want := trayNewNoteLabel(""), "新建随手记"; got != want {
		t.Errorf("trayNewNoteLabel(\"\") = %q，期望 %q", got, want)
	}
}
