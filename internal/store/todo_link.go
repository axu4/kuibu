package store

import (
	"fmt"
	"strings"
)

// 待办优先级取值。空字符串表示未设置。
const (
	// PriorityHigh 表示高优先级。
	PriorityHigh = "high"
	// PriorityMedium 表示中优先级。
	PriorityMedium = "medium"
	// PriorityLow 表示低优先级。
	PriorityLow = "low"
)

// ValidTodoPriority 判断待办优先级是否合法；空字符串（未设置）也合法。
func ValidTodoPriority(p string) bool {
	switch p {
	case "", PriorityHigh, PriorityMedium, PriorityLow:
		return true
	}
	return false
}

// TodoPriorityRank 返回优先级排序值：high=0、medium=1、low=2、未设置=3。
func TodoPriorityRank(p string) int {
	switch p {
	case PriorityHigh:
		return 0
	case PriorityMedium:
		return 1
	case PriorityLow:
		return 2
	}
	return 3
}

// DefaultLinkSchemes 是点击后直接交给系统打开的默认协议白名单。
var DefaultLinkSchemes = []string{"http", "https", "mailto", "message"}

// forbiddenLinkSchemes 是一律拒绝保存和打开的协议。
var forbiddenLinkSchemes = map[string]bool{"javascript": true, "data": true, "vbscript": true}

// confirmLinkSchemes 是即使在白名单里也要点击前确认的协议。
var confirmLinkSchemes = map[string]bool{"file": true}

// 打开链接前的判定结果。
const (
	// LinkOpen 表示可以直接打开。
	LinkOpen = "open"
	// LinkConfirm 表示需要用户二次确认后再打开。
	LinkConfirm = "confirm"
	// LinkDeny 表示一律拒绝。
	LinkDeny = "deny"
)

// LinkScheme 返回链接的协议名（小写）；不是「scheme:」开头时返回空字符串。
//
// 按 RFC 3986：首字符为字母，其后为字母、数字、+、-、.。只看协议名，不要求
// 有 host，因此 message://%3C…%3E、mailto:a@b、obsidian://open?… 都能识别。
func LinkScheme(s string) string {
	i := strings.IndexByte(s, ':')
	if i <= 0 {
		return ""
	}
	if !validSchemeName(s[:i]) {
		return ""
	}
	return strings.ToLower(s[:i])
}

// validSchemeName 判断 name 是否符合 RFC 3986 的协议名语法。
func validSchemeName(name string) bool {
	if name == "" {
		return false
	}
	for i, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case i > 0 && (c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.'):
		default:
			return false
		}
	}
	return true
}

// ValidateTodoURL 校验待办链接：必须以合法协议名加冒号开头、冒号后非空、
// 不含空白与控制字符；javascript:/data: 等危险协议一律拒绝。CLI 与 App 共用。
func ValidateTodoURL(s string) error {
	scheme := LinkScheme(s)
	if scheme == "" {
		return fmt.Errorf("%w: 非法链接 URL %q（需要以协议名开头，如 https: / mailto: / message:）", ErrInvalid, s)
	}
	if len(s) == len(scheme)+1 {
		return fmt.Errorf("%w: 非法链接 URL %q（协议名后没有内容）", ErrInvalid, s)
	}
	for _, c := range s {
		if c <= ' ' || c == 0x7f {
			return fmt.Errorf("%w: 非法链接 URL %q（含空白或控制字符）", ErrInvalid, s)
		}
	}
	if forbiddenLinkSchemes[scheme] {
		return fmt.Errorf("%w: 不允许 %s: 链接", ErrInvalid, scheme)
	}
	return nil
}

// NormalizeLinkSchemes 规整协议白名单：去空白、转小写、去掉结尾的「:」「://」、
// 去重保序。协议名不合法或是被禁止的协议时返回 ErrInvalid。
func NormalizeLinkSchemes(schemes []string) ([]string, error) {
	out := make([]string, 0, len(schemes))
	seen := map[string]bool{}
	for _, s := range schemes {
		s = strings.ToLower(strings.TrimSpace(s))
		s = strings.TrimSuffix(strings.TrimSuffix(s, "//"), ":")
		if s == "" {
			continue
		}
		if !validSchemeName(s) {
			return nil, fmt.Errorf("%w: 非法协议名 %q", ErrInvalid, s)
		}
		if forbiddenLinkSchemes[s] {
			return nil, fmt.Errorf("%w: 不允许把 %s 加入白名单", ErrInvalid, s)
		}
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out, nil
}

// CheckLinkOpen 判定点击链接时怎么处理：非法或危险协议拒绝；file: 与白名单
// 外的协议需要确认；其余直接打开。allowed 为 nil 时使用 DefaultLinkSchemes。
func CheckLinkOpen(url string, allowed []string) (string, error) {
	if err := ValidateTodoURL(url); err != nil {
		return LinkDeny, err
	}
	if allowed == nil {
		allowed = DefaultLinkSchemes
	}
	scheme := LinkScheme(url)
	if confirmLinkSchemes[scheme] {
		return LinkConfirm, nil
	}
	for _, a := range allowed {
		if strings.EqualFold(a, scheme) {
			return LinkOpen, nil
		}
	}
	return LinkConfirm, nil
}
