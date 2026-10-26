package update

import (
	"fmt"
	"strconv"
	"strings"
)

// Version 是一个解析后的语义化版本（仅支持 MAJOR.MINOR.PATCH 与 -prerelease）。
type Version struct {
	Major      int
	Minor      int
	Patch      int
	Prerelease string
}

// ParseVersion 解析版本号，容忍前缀 v/V，支持 MAJOR.MINOR.PATCH 与 -prerelease。
func ParseVersion(s string) (Version, error) {
	var v Version
	s = strings.TrimSpace(s)
	if len(s) > 0 && (s[0] == 'v' || s[0] == 'V') {
		s = s[1:]
	}

	base := s
	hasPrerelease := false
	if i := strings.IndexByte(s, '-'); i >= 0 {
		base = s[:i]
		v.Prerelease = s[i+1:]
		hasPrerelease = true
	}
	if hasPrerelease && strings.TrimSpace(v.Prerelease) == "" {
		return v, fmt.Errorf("版本号 %q 格式不合法：prerelease 为空", s)
	}

	parts := strings.Split(base, ".")
	if len(parts) != 3 {
		return v, fmt.Errorf("版本号 %q 格式不合法，应为 MAJOR.MINOR.PATCH", s)
	}
	var err error
	if v.Major, err = parseVersionNumber(parts[0]); err != nil {
		return v, fmt.Errorf("版本号 %q 不合法: %w", s, err)
	}
	if v.Minor, err = parseVersionNumber(parts[1]); err != nil {
		return v, fmt.Errorf("版本号 %q 不合法: %w", s, err)
	}
	if v.Patch, err = parseVersionNumber(parts[2]); err != nil {
		return v, fmt.Errorf("版本号 %q 不合法: %w", s, err)
	}
	return v, nil
}

// parseVersionNumber 解析非负整数版本字段，拒绝空串、负数与前后空白。
func parseVersionNumber(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("版本字段为空")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("版本字段 %q 不是数字", s)
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("版本字段 %q 不是合法整数", s)
	}
	return n, nil
}

// Compare 比较两个版本号，返回 -1（a<b）、0（相等）、1（a>b）。
func (v Version) Compare(o Version) int {
	if v.Major != o.Major {
		return cmpInt(v.Major, o.Major)
	}
	if v.Minor != o.Minor {
		return cmpInt(v.Minor, o.Minor)
	}
	if v.Patch != o.Patch {
		return cmpInt(v.Patch, o.Patch)
	}
	return comparePrerelease(v.Prerelease, o.Prerelease)
}

// Compare 解析并比较两个版本号字符串。
func Compare(a, b string) (int, error) {
	va, err := ParseVersion(a)
	if err != nil {
		return 0, err
	}
	vb, err := ParseVersion(b)
	if err != nil {
		return 0, err
	}
	return va.Compare(vb), nil
}

// IsNewer 判断 latest 是否比 current 新。
//
// current 为 "0.0.0-dev" 或无法解析时视为开发版，返回 false（开发版不提示更新）；
// latest 无法解析时同样返回 false（宁可错过更新也不报错）。
func IsNewer(latest, current string) bool {
	if strings.TrimSpace(current) == "0.0.0-dev" {
		return false
	}
	l, err := ParseVersion(latest)
	if err != nil {
		return false
	}
	c, err := ParseVersion(current)
	if err != nil {
		return false
	}
	return l.Compare(c) > 0
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// comparePrerelease 比较 prerelease 标识。
//
// 规则遵循语义化版本：正式版（无 prerelease）大于任何 prerelease；
// 数字标识按数值比较且小于非数字标识；非数字标识按字典序比较。
func comparePrerelease(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return 1
	}
	if b == "" {
		return -1
	}
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	n := len(as)
	if len(bs) < n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		ai, aerr := strconv.Atoi(as[i])
		bi, berr := strconv.Atoi(bs[i])
		switch {
		case aerr == nil && berr == nil:
			if ai != bi {
				return cmpInt(ai, bi)
			}
		case aerr == nil:
			return -1 // 数字标识小于非数字标识
		case berr == nil:
			return 1
		default:
			if as[i] != bs[i] {
				return strings.Compare(as[i], bs[i])
			}
		}
	}
	return cmpInt(len(as), len(bs))
}
