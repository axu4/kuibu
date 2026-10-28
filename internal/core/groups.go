package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/axu4/kuibu/internal/store"
)

// GroupMember 是组里的一个人。
type GroupMemberInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

// GroupInfo 是组管理页需要的组信息。
type GroupInfo struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	LeadID   string            `json:"leadId"`
	LeadName string            `json:"leadName"`
	Members  []GroupMemberInfo `json:"members"`
}

// EnsureGroups 把名单里的组同步成组配置：已有组保留，缺的补上，空组长尽量推导。
//
// 首次使用时组员名单里可能只有 group 字符串，没有 groups 配置；这里做一次迁移，
// 之后组管理页就有稳定的 id。
func EnsureGroups(ws *store.Workspace, now time.Time) ([]store.ReportGroup, error) {
	groups, err := ws.ReportGroups()
	if err != nil {
		return nil, err
	}
	people, err := ws.ListPersons()
	if err != nil {
		return nil, err
	}
	byName := map[string]int{}
	for i, g := range groups {
		byName[g.Name] = i
	}
	changed := false
	for _, p := range people {
		if !p.Active {
			continue
		}
		name := currentGroup(p, now)
		if name == "" {
			continue
		}
		if _, ok := byName[name]; ok {
			continue
		}
		g := store.ReportGroup{ID: newGroupID(name), Name: name}
		groups = append(groups, g)
		byName[name] = len(groups) - 1
		changed = true
	}
	for i := range groups {
		if groups[i].Lead != "" {
			continue
		}
		if lead := inferGroupLead(people, groups[i].Name, now); lead != "" {
			groups[i].Lead = lead
			changed = true
		}
	}
	if changed {
		if err := ws.SetReportGroups(groups); err != nil {
			return nil, err
		}
	}
	return groups, nil
}

func newGroupID(name string) string {
	// 迁移出来的组用名字派生一个稳定 id，避免随机导致每次启动变化。
	sum := sha256.Sum256([]byte("group\n" + name))
	return "g" + hex.EncodeToString(sum[:])[:8]
}

// ListGroups 返回组及成员（按当前在职名单）。
func ListGroups(ws *store.Workspace, now time.Time) ([]GroupInfo, error) {
	groups, err := EnsureGroups(ws, now)
	if err != nil {
		return nil, err
	}
	people, err := ws.ListPersons()
	if err != nil {
		return nil, err
	}
	byID := map[string]*store.Person{}
	for _, p := range people {
		byID[p.ID] = p
	}
	out := make([]GroupInfo, 0, len(groups))
	for _, g := range groups {
		info := GroupInfo{ID: g.ID, Name: g.Name, LeadID: g.Lead}
		if lead, ok := byID[g.Lead]; ok {
			info.LeadName = lead.Name
		}
		for _, p := range people {
			if !p.Active {
				continue
			}
			if currentGroup(p, now) != g.Name {
				continue
			}
			info.Members = append(info.Members, GroupMemberInfo{ID: p.ID, Name: p.Name, Role: p.Role})
			if g.Lead == "" && p.Role == store.RoleLead && info.LeadID == "" {
				info.LeadID = p.ID
				info.LeadName = p.Name
			}
		}
		sort.Slice(info.Members, func(i, j int) bool { return info.Members[i].Name < info.Members[j].Name })
		out = append(out, info)
	}
	return out, nil
}

// AddGroup 新建组。
func AddGroup(ws *store.Workspace, name, leadID string, now time.Time) (GroupInfo, error) {
	if leadID != "" {
		if _, err := ws.GetPerson(leadID); err != nil {
			return GroupInfo{}, err
		}
	}
	_, g, err := ws.AddReportGroup(name, leadID)
	if err != nil {
		return GroupInfo{}, err
	}
	if leadID != "" {
		_ = assignLeadRole(ws, leadID, now)
	}
	return GroupInfo{ID: g.ID, Name: g.Name, LeadID: g.Lead}, nil
}

// RenameGroup 修改组名，并同步当前组员及其当前归属记录。
func RenameGroup(ws *store.Workspace, idOrName, newName string, now time.Time) (GroupInfo, error) {
	groups, err := EnsureGroups(ws, now)
	if err != nil {
		return GroupInfo{}, err
	}
	idx := -1
	for i := range groups {
		if groups[i].ID == idOrName || groups[i].Name == idOrName {
			idx = i
			break
		}
	}
	if idx < 0 {
		return GroupInfo{}, fmt.Errorf("%w: 找不到组 %q", store.ErrNotFound, idOrName)
	}
	oldName := groups[idx].Name
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return GroupInfo{}, fmt.Errorf("%w: 组名不能为空", store.ErrInvalid)
	}
	if newName == oldName {
		return GroupInfo{ID: groups[idx].ID, Name: oldName, LeadID: groups[idx].Lead}, nil
	}
	for i, g := range groups {
		if i != idx && g.Name == newName {
			return GroupInfo{}, fmt.Errorf("%w: 组「%s」已存在", store.ErrConflict, newName)
		}
	}
	groups[idx].Name = newName
	if err := ws.SetReportGroups(groups); err != nil {
		return GroupInfo{}, err
	}
	// 当前组员改名；历史记录同步改名，避免 MembershipAt 仍返回旧组。
	people, err := ws.ListPersons()
	if err != nil {
		return GroupInfo{}, err
	}
	for _, p := range people {
		if currentGroup(p, now) != oldName {
			continue
		}
		_, err := ws.UpdatePerson(p.ID, now, func(person *store.Person) error {
			person.Group = newName
			// 只改「当前生效」的那条归属记录，历史周仍保留旧组名。
			latest := -1
			for i := range person.GroupHistory {
				if latest < 0 || person.GroupHistory[i].From > person.GroupHistory[latest].From {
					latest = i
				}
			}
			if latest >= 0 && person.GroupHistory[latest].Group == oldName {
				person.GroupHistory[latest].Group = newName
			}
			return nil
		})
		if err != nil {
			return GroupInfo{}, err
		}
	}
	return GroupInfo{ID: groups[idx].ID, Name: newName, LeadID: groups[idx].Lead}, nil
}

// DeleteGroup 删除组；有在职成员时拒绝，避免静默丢归属。
func DeleteGroup(ws *store.Workspace, idOrName string, now time.Time) (store.ReportGroup, error) {
	groups, err := EnsureGroups(ws, now)
	if err != nil {
		return store.ReportGroup{}, err
	}
	idx := -1
	for i := range groups {
		if groups[i].ID == idOrName || groups[i].Name == idOrName {
			idx = i
			break
		}
	}
	if idx < 0 {
		return store.ReportGroup{}, fmt.Errorf("%w: 找不到组 %q", store.ErrNotFound, idOrName)
	}
	people, err := ws.ListPersons()
	if err != nil {
		return store.ReportGroup{}, err
	}
	memberCount := 0
	for _, p := range people {
		if p.Active && currentGroup(p, now) == groups[idx].Name {
			memberCount++
		}
	}
	if memberCount > 0 {
		return store.ReportGroup{}, fmt.Errorf("%w: 组「%s」还有 %d 位在职成员，先把他们转到别的组", store.ErrInvalid, groups[idx].Name, memberCount)
	}
	removed := groups[idx]
	groups = append(groups[:idx], groups[idx+1:]...)
	if err := ws.SetReportGroups(groups); err != nil {
		return store.ReportGroup{}, err
	}
	return removed, nil
}

// UpdateGroup 修改组名、组长和成员。
func UpdateGroup(ws *store.Workspace, idOrName, name, leadID string, memberIDs []string, now time.Time) (GroupInfo, error) {
	groups, err := EnsureGroups(ws, now)
	if err != nil {
		return GroupInfo{}, err
	}
	idx := -1
	for i := range groups {
		if groups[i].ID == idOrName || groups[i].Name == idOrName {
			idx = i
			break
		}
	}
	if idx < 0 {
		return GroupInfo{}, fmt.Errorf("%w: 找不到组 %q", store.ErrNotFound, idOrName)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return GroupInfo{}, fmt.Errorf("%w: 组名不能为空", store.ErrInvalid)
	}
	oldName := groups[idx].Name
	if name != oldName {
		if _, err := RenameGroup(ws, groups[idx].ID, name, now); err != nil {
			return GroupInfo{}, err
		}
		groups, err = ws.ReportGroups()
		if err != nil {
			return GroupInfo{}, err
		}
		for i := range groups {
			if groups[i].ID == idOrName || groups[i].Name == name {
				idx = i
				break
			}
		}
	}
	if leadID != "" {
		if _, err := ws.GetPerson(leadID); err != nil {
			return GroupInfo{}, err
		}
	}
	groups[idx].Lead = leadID
	if err := ws.SetReportGroups(groups); err != nil {
		return GroupInfo{}, err
	}
	if leadID != "" {
		_ = assignLeadRole(ws, leadID, now)
	}
	want := map[string]bool{}
	for _, id := range memberIDs {
		want[id] = true
	}
	people, err := ws.ListPersons()
	if err != nil {
		return GroupInfo{}, err
	}
	for _, p := range people {
		if !p.Active {
			continue
		}
		inGroup := currentGroup(p, now) == name
		if want[p.ID] && !inGroup {
			if _, err := setGroupCurrent(ws, p.ID, name, now); err != nil {
				return GroupInfo{}, err
			}
		} else if !want[p.ID] && inGroup {
			if _, err := setGroupCurrent(ws, p.ID, "", now); err != nil {
				return GroupInfo{}, err
			}
		}
		delete(want, p.ID)
	}
	return GroupInfo{ID: groups[idx].ID, Name: name, LeadID: groups[idx].Lead}, nil
}

func setGroupCurrent(ws *store.Workspace, personID, group string, now time.Time) (*store.Person, error) {
	return ws.UpdatePerson(personID, now, func(p *store.Person) error {
		p.Group = group
		day := now.Format("2006-01-02")
		// 同一天调整组时覆盖当天那条记录；MembershipAt 遇到相同 From 取先出现的一条，
		// 直接 append 会保留旧组。
		for i := len(p.GroupHistory) - 1; i >= 0; i-- {
			if p.GroupHistory[i].From == day {
				p.GroupHistory[i].Group = group
				p.GroupHistory[i].ProductLines = append([]string(nil), p.ProductLines...)
				return nil
			}
		}
		p.GroupHistory = append(p.GroupHistory, store.GroupRecord{
			Group:        group,
			ProductLines: append([]string(nil), p.ProductLines...),
			From:         day,
		})
		return nil
	})
}

func assignLeadRole(ws *store.Workspace, personID string, now time.Time) error {
	_, err := ws.UpdatePerson(personID, now, func(p *store.Person) error {
		p.Role = store.RoleLead
		return nil
	})
	return err
}

func currentGroup(p *store.Person, now time.Time) string {
	g, _ := p.MembershipAt(now)
	if g == "" {
		g = p.Group
	}
	return g
}

func inferGroupLead(people []*store.Person, group string, now time.Time) string {
	for _, p := range people {
		if !p.Active || p.Role != store.RoleLead {
			continue
		}
		g, _ := p.MembershipAt(now)
		if g == "" {
			g = p.Group
		}
		if g == group {
			return p.ID
		}
	}
	// 组长自己的组为空、名单里只有一个组长时，视为该组组长。
	var leads []*store.Person
	for _, p := range people {
		if p.Active && p.Role == store.RoleLead {
			leads = append(leads, p)
		}
	}
	if len(leads) == 1 {
		g, _ := leads[0].MembershipAt(now)
		if g == "" && leads[0].Group == "" {
			return leads[0].ID
		}
	}
	return ""
}

// GroupForPerson 返回某人当前组配置（用于校验）。
func GroupForPerson(ws *store.Workspace, personID string, now time.Time) (store.ReportGroup, bool, error) {
	groups, err := EnsureGroups(ws, now)
	if err != nil {
		return store.ReportGroup{}, false, err
	}
	p, err := ws.GetPerson(personID)
	if err != nil {
		return store.ReportGroup{}, false, err
	}
	name := currentGroup(p, now)
	for _, g := range groups {
		if g.Name == name {
			return g, true, nil
		}
	}
	return store.ReportGroup{}, false, nil
}
