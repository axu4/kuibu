package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// MaxReportGroups 是组数量上限，主要防止误操作写入异常配置。
const MaxReportGroups = 50

// ReportGroup 是周报组织单位：名称、组长；成员由组员的 group 决定。
//
// 组的组长就是 DE；项目 SE 记在报送项目上（见 ReportProject），不另设「SE 组」。
type ReportGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Lead 是组长 personId；可以为空，表示还没指定组长。
	Lead string `json:"lead,omitempty"`
}

// ReportGroups 返回配置里的组（按配置顺序）。
func (w *Workspace) ReportGroups() ([]ReportGroup, error) {
	cfg, err := w.readConfig()
	if err != nil {
		return nil, err
	}
	return append([]ReportGroup(nil), cfg.ReportGroups...), nil
}

// SetReportGroups 覆盖组配置，保持其他配置字段不变。
func (w *Workspace) SetReportGroups(groups []ReportGroup) error {
	normalized, err := normalizeReportGroups(groups)
	if err != nil {
		return err
	}
	cfg, err := w.readConfig()
	if err != nil {
		return err
	}
	cfg.ReportGroups = normalized
	return w.writeConfig(cfg)
}

// AddReportGroup 追加一个组（不自动加成员）。
func (w *Workspace) AddReportGroup(name, lead string) ([]ReportGroup, ReportGroup, error) {
	groups, err := w.ReportGroups()
	if err != nil {
		return nil, ReportGroup{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ReportGroup{}, fmt.Errorf("%w: 组名不能为空", ErrInvalid)
	}
	if len(groups) >= MaxReportGroups {
		return nil, ReportGroup{}, fmt.Errorf("%w: 最多 %d 个组", ErrInvalid, MaxReportGroups)
	}
	for _, g := range groups {
		if g.Name == name {
			return nil, ReportGroup{}, fmt.Errorf("%w: 组「%s」已存在", ErrConflict, name)
		}
	}
	group := ReportGroup{ID: newReportGroupID(), Name: name, Lead: strings.TrimSpace(lead)}
	groups = append(groups, group)
	if err := w.SetReportGroups(groups); err != nil {
		return nil, ReportGroup{}, err
	}
	return groups, group, nil
}

// ReportGroupByID 按 id 或名称查组。
func ReportGroupByID(groups []ReportGroup, idOrName string) (ReportGroup, bool) {
	idOrName = strings.TrimSpace(idOrName)
	for _, g := range groups {
		if g.ID == idOrName || g.Name == idOrName {
			return g, true
		}
	}
	return ReportGroup{}, false
}

// ReportGroupName 返回组名；找不到时返回 id 本身。
func ReportGroupName(groups []ReportGroup, id string) string {
	if g, ok := ReportGroupByID(groups, id); ok {
		return g.Name
	}
	return id
}

// ReportGroupLeadName 返回组长姓名；找不到时返回空。
func ReportGroupLeadName(groups []ReportGroup, id string) string {
	g, ok := ReportGroupByID(groups, id)
	if !ok {
		return ""
	}
	return g.Lead
}

func normalizeReportGroups(groups []ReportGroup) ([]ReportGroup, error) {
	if len(groups) > MaxReportGroups {
		return nil, fmt.Errorf("%w: 最多 %d 个组", ErrInvalid, MaxReportGroups)
	}
	out := make([]ReportGroup, 0, len(groups))
	ids := map[string]bool{}
	names := map[string]bool{}
	for _, g := range groups {
		g.ID = strings.TrimSpace(g.ID)
		g.Name = strings.TrimSpace(g.Name)
		g.Lead = strings.TrimSpace(g.Lead)
		if g.ID == "" || g.Name == "" {
			return nil, fmt.Errorf("%w: 组的 id 与名称都不能为空", ErrInvalid)
		}
		if !reportProjectIDPattern.MatchString(g.ID) {
			return nil, fmt.Errorf("%w: 组 id 只能包含字母、数字、下划线和连字符: %q", ErrInvalid, g.ID)
		}
		if ids[g.ID] {
			return nil, fmt.Errorf("%w: 组 id 重复: %s", ErrInvalid, g.ID)
		}
		if names[g.Name] {
			return nil, fmt.Errorf("%w: 组名重复: %s", ErrConflict, g.Name)
		}
		ids[g.ID] = true
		names[g.Name] = true
		out = append(out, g)
	}
	return out, nil
}

func newReportGroupID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "g0"
	}
	return "g" + hex.EncodeToString(b[:])
}

// SortReportGroups 按名称排序，供展示使用。
func SortReportGroups(groups []ReportGroup) []ReportGroup {
	out := append([]ReportGroup(nil), groups...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
