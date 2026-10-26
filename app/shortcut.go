package main

import (
	"fmt"
	"strings"
)

// 本文件是随手记快捷键的纯函数部分：校验、规范化、macOS 符号化。
//
// Wails v3 的加速键语法用 "+" 连接修饰键与主键，例如 "Ctrl+Alt+N"。
// 这里不复用 Wails 内部的 parseAccelerator（未导出），而是按同一套
// 规则做更严格的校验：至少一个修饰键、拒绝纯修饰键、只接受可打印
// 单字符或已知命名键。

// modifierSpec 描述一个修饰键的规范名与可接受的别名。
type modifierSpec struct {
	// canonical 是规范化后写回配置的名字。
	canonical string
	// display 是 macOS 上的符号。
	display string
	// aliases 是 Wails 语法里可接受的写法（小写）。
	aliases []string
}

// modifiers 的顺序就是规范化与展示的顺序：⌃⌥⇧⌘。
var modifiers = []modifierSpec{
	{canonical: "Ctrl", display: "⌃", aliases: []string{"ctrl", "control"}},
	{canonical: "Alt", display: "⌥", aliases: []string{"alt", "option", "opt"}},
	{canonical: "Shift", display: "⇧", aliases: []string{"shift"}},
	{canonical: "Cmd", display: "⌘", aliases: []string{"cmd", "command", "super", "win"}},
	{canonical: "CmdOrCtrl", display: "⌘", aliases: []string{"cmdorctrl"}},
}

// namedKey 描述一个命名键：写回配置的规范名（Wails 可以解析）与展示名。
type namedKey struct {
	canonical string
	display   string
}

// namedKeys 是 Wails 接受的命名键（小写）。canonical 必须能被 Wails 的
// parseAccelerator 解析（它对每个组件做小写化），display 只用于界面展示。
var namedKeys = map[string]namedKey{
	"backspace":      {"Backspace", "Backspace"},
	"tab":            {"Tab", "Tab"},
	"return":         {"Enter", "Enter"},
	"enter":          {"Enter", "Enter"},
	"escape":         {"Escape", "Esc"},
	"left":           {"Left", "←"},
	"right":          {"Right", "→"},
	"up":             {"Up", "↑"},
	"down":           {"Down", "↓"},
	"space":          {"Space", "Space"},
	"delete":         {"Delete", "Delete"},
	"forward delete": {"Forward delete", "Delete"},
	"home":           {"Home", "Home"},
	"end":            {"End", "End"},
	"page up":        {"Page up", "PageUp"},
	"page down":      {"Page down", "PageDown"},
	"numlock":        {"NumLock", "NumLock"},
	"plus":           {"Plus", "+"},
}

// lookupModifier 返回别名对应的修饰键。
func lookupModifier(name string) (modifierSpec, bool) {
	lower := strings.ToLower(strings.TrimSpace(name))
	for _, m := range modifiers {
		for _, alias := range m.aliases {
			if lower == alias {
				return m, true
			}
		}
	}
	return modifierSpec{}, false
}

// lookupNamedKey 返回命名键（含 F1~F35）。
func lookupNamedKey(key string) (namedKey, bool) {
	lower := strings.ToLower(strings.TrimSpace(key))
	if k, ok := namedKeys[lower]; ok {
		return k, true
	}
	if len(lower) >= 2 && lower[0] == 'f' {
		if n, err := parseSmallInt(lower[1:]); err == nil && n >= 1 && n <= 35 {
			name := "F" + lower[1:]
			return namedKey{canonical: name, display: name}, true
		}
	}
	return namedKey{}, false
}

// parseSmallInt 解析 1~3 位十进制数，避免引入 strconv 只为解析 F 键。
func parseSmallInt(s string) (int, error) {
	if len(s) == 0 || len(s) > 3 {
		return 0, fmt.Errorf("不是数字")
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("不是数字")
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

// normalizeShortcut 校验并规范化加速键。
//
// 返回的字符串形如 "Ctrl+Alt+N"，修饰键按 Ctrl、Alt、Shift、Cmd 排序，
// 主键单字符统一大写，命名键统一展示名（如 Space、F5）。
func normalizeShortcut(accel string) (string, error) {
	trimmed := strings.TrimSpace(accel)
	if trimmed == "" {
		return "", fmt.Errorf("快捷键不能为空")
	}

	parts := strings.Split(trimmed, "+")
	if len(parts) < 2 {
		return "", fmt.Errorf("至少需要一个修饰键（Ctrl、Alt、Shift、Cmd）")
	}

	seen := make(map[string]bool)
	var ordered []string
	for _, part := range parts[:len(parts)-1] {
		m, ok := lookupModifier(part)
		if !ok {
			return "", fmt.Errorf("无法识别的修饰键 %q", strings.TrimSpace(part))
		}
		if !seen[m.canonical] {
			seen[m.canonical] = true
			ordered = append(ordered, m.canonical)
		}
	}
	if len(ordered) == 0 {
		return "", fmt.Errorf("至少需要一个修饰键（Ctrl、Alt、Shift、Cmd）")
	}

	rawKey := strings.TrimSpace(parts[len(parts)-1])
	if rawKey == "" {
		return "", fmt.Errorf("缺少主键")
	}
	if _, isModifier := lookupModifier(rawKey); isModifier {
		return "", fmt.Errorf("纯修饰键不能作为快捷键")
	}

	key := ""
	if named, ok := lookupNamedKey(rawKey); ok {
		// 写回配置的是 Wails 可解析的规范名（如 Space、Left、F5），
		// 而不是 "←" 这类展示符号，保证下次还能被解析。
		key = named.canonical
	} else {
		runes := []rune(rawKey)
		if len(runes) != 1 || runes[0] < 0x20 || runes[0] == 0x7f {
			return "", fmt.Errorf("无法识别的按键 %q", rawKey)
		}
		key = strings.ToUpper(rawKey)
	}

	// 按固定顺序排列修饰键，保证同一个快捷键只有一种写法。
	var out []string
	for _, m := range modifiers {
		if seen[m.canonical] {
			out = append(out, m.canonical)
		}
	}
	out = append(out, key)
	return strings.Join(out, "+"), nil
}

// shortcutDisplay 把加速键转成 macOS 符号形式，例如 "⌃⌥N"。
//
// 非法输入原样返回，方便界面把用户的错误输入显示出来。
func shortcutDisplay(accel string) string {
	trimmed := strings.TrimSpace(accel)
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(trimmed, "+")
	if len(parts) < 2 {
		return trimmed
	}

	seen := make(map[string]bool)
	for _, part := range parts[:len(parts)-1] {
		m, ok := lookupModifier(part)
		if !ok {
			return trimmed
		}
		seen[m.canonical] = true
	}

	rawKey := strings.TrimSpace(parts[len(parts)-1])
	key := rawKey
	if named, ok := lookupNamedKey(rawKey); ok {
		key = named.display
	} else if runes := []rune(rawKey); len(runes) == 1 {
		key = strings.ToUpper(rawKey)
	}

	var b strings.Builder
	for _, m := range modifiers {
		if seen[m.canonical] {
			b.WriteString(m.display)
		}
	}
	b.WriteString(key)
	return b.String()
}

// trayNewNoteLabel 是托盘「新建随手记」菜单项的标签。
//
// Wails 的 MenuItem.SetAccelerator 在 macOS 上会把快捷键注册成菜单项的
// key equivalent（只在菜单打开时生效，且会显示成原生快捷键）。这里选择
// 直接把符号写进标签：行为可控、刷新简单，也不会和全局快捷键抢按键。
func trayNewNoteLabel(accel string) string {
	if display := shortcutDisplay(accel); display != "" {
		return "新建随手记  " + display
	}
	return "新建随手记"
}
