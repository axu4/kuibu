package main

import (
	"github.com/axu4/kuibu/internal/core"
	"github.com/axu4/kuibu/internal/store"
)

// groupChangedEvent 是组配置变化时发给前端的事件名。
const groupChangedEvent = "group:changed"

// GroupMemberData 是组里的一位成员。
type GroupMemberData struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

// GroupInfoData 是组管理页的一项。
type GroupInfoData struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	LeadID   string            `json:"leadId"`
	LeadName string            `json:"leadName"`
	Members  []GroupMemberData `json:"members"`
}

// Groups 返回所有组及成员（首次会从组员名单推导组）。
func (s *ReportService) GroupList() ([]GroupInfoData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return nil, err
	}
	list, err := core.ListGroups(ws, s.clock())
	if err != nil {
		return nil, err
	}
	out := make([]GroupInfoData, 0, len(list))
	for _, g := range list {
		item := GroupInfoData{ID: g.ID, Name: g.Name, LeadID: g.LeadID, LeadName: g.LeadName, Members: make([]GroupMemberData, 0, len(g.Members))}
		for _, m := range g.Members {
			item.Members = append(item.Members, GroupMemberData{ID: m.ID, Name: m.Name, Role: m.Role})
		}
		out = append(out, item)
	}
	return out, nil
}

// AddGroup 新建组。
func (s *ReportService) AddGroup(name, leadID string) ([]GroupInfoData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return nil, err
	}
	if _, err := core.AddGroup(ws, name, leadID, s.clock()); err != nil {
		return nil, err
	}
	s.emitGroup()
	return s.GroupList()
}

// UpdateGroup 修改组名、组长与成员。
func (s *ReportService) UpdateGroup(id, name, leadID string, memberIDs []string) ([]GroupInfoData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return nil, err
	}
	if _, err := core.UpdateGroup(ws, id, name, leadID, memberIDs, s.clock()); err != nil {
		return nil, err
	}
	s.emitGroup()
	return s.GroupList()
}

// DeleteGroup 删除组；有在职成员时拒绝。
func (s *ReportService) DeleteGroup(id string) ([]GroupInfoData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return nil, err
	}
	if _, err := core.DeleteGroup(ws, id, s.clock()); err != nil {
		return nil, err
	}
	s.emitGroup()
	return s.GroupList()
}

func (s *ReportService) emitGroup() {
	if s.emit != nil {
		s.emit(groupChangedEvent, map[string]string{})
		return
	}
	if s.workspace != nil {
		s.workspace.emit(groupChangedEvent, map[string]string{})
	}
}

// ReportLibrary 返回某周所有已定稿组报（跨组），可按组/项目过滤。
func (s *ReportService) ReportLibrary(week, group, project string) ([]core.LibraryReport, error) {
	ws, _, err := s.instances()
	if err != nil {
		return nil, err
	}
	if week == "" {
		week = store.WeekOf(s.clock())
	}
	return core.ListReportLibrary(ws, week, group, project)
}
