package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/axu4/kuibu/internal/core"
	"github.com/axu4/kuibu/internal/store"
)

// triageChangedEvent 是筛选记录或报送项目变化时发给前端的事件名。
const triageChangedEvent = "triage:changed"

// TriageProjectData 是一个报送项目。
type TriageProjectData struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// TriageItemData 是筛选台里的一条事项。
type TriageItemData struct {
	ID           string `json:"id"`
	Person       string `json:"person"`
	Name         string `json:"name"`
	Section      string `json:"section"`
	SectionTitle string `json:"sectionTitle"`
	Domain       string `json:"domain"`
	Text         string `json:"text"`
	Decision     string `json:"decision"`
	Project      string `json:"project"`
	ProjectName  string `json:"projectName"`
	Order        int    `json:"order"`
	DecidedAt    string `json:"decidedAt"`
}

// TriageSectionData 是分节进度（本周总结 / 下周计划 / 风险与求助）。
type TriageSectionData struct {
	Section   string `json:"section"`
	Title     string `json:"title"`
	Done      int    `json:"done"`
	Total     int    `json:"total"`
	Undecided int    `json:"undecided"`
}

// TriageCountsData 是筛选进度统计。
type TriageCountsData struct {
	Total     int            `json:"total"`
	Done      int            `json:"done"`
	Accepted  int            `json:"accepted"`
	Rejected  int            `json:"rejected"`
	Skipped   int            `json:"skipped"`
	Undecided int            `json:"undecided"`
	ByProject map[string]int `json:"byProject"`
}

// TriageData 是第 1 步筛选台 / 第 2 步预览的全部数据。
type TriageData struct {
	Week       string              `json:"week"`
	Group      string              `json:"group"`
	Lead       string              `json:"lead"`
	SourceHash string              `json:"sourceHash"`
	Projects   []TriageProjectData `json:"projects"`
	Items      []TriageItemData    `json:"items"`
	Sections   []TriageSectionData `json:"sections"`
	Missing    []string            `json:"missing"`
	Counts     TriageCountsData    `json:"counts"`
	AllJudged  bool                `json:"allJudged"`
}

// TriageService 负责报送项目配置与逐条筛选。组报编辑仍走 ReportService。
type TriageService struct {
	workspace *WorkspaceService
	now       func() time.Time
	emit      func(name string, data any)
}

func newTriageService(workspace *WorkspaceService) *TriageService {
	return &TriageService{workspace: workspace, now: time.Now}
}

// ServiceName 返回服务名，供 Wails 日志与绑定使用。
func (s *TriageService) ServiceName() string { return "TriageService" }

func (s *TriageService) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *TriageService) ws() (*store.Workspace, error) {
	if s.workspace == nil {
		return nil, errNotOpen()
	}
	ws := s.workspace.workspace()
	if ws == nil {
		return nil, errNotOpen()
	}
	return ws, nil
}

func (s *TriageService) emitTriage(week string) {
	if s.emit != nil {
		s.emit(triageChangedEvent, map[string]string{"week": week})
		return
	}
	if s.workspace != nil {
		s.workspace.emit(triageChangedEvent, map[string]string{"week": week})
	}
}

// Projects 列出报送项目。
func (s *TriageService) Projects() ([]TriageProjectData, error) {
	ws, err := s.ws()
	if err != nil {
		return nil, err
	}
	projects, err := ws.ReportProjects()
	if err != nil {
		return nil, err
	}
	return toProjectData(projects), nil
}

// AddProject 添加报送项目。
func (s *TriageService) AddProject(name string) ([]TriageProjectData, error) {
	ws, err := s.ws()
	if err != nil {
		return nil, err
	}
	projects, _, err := ws.AddReportProject(name)
	if err != nil {
		return nil, err
	}
	s.emitTriage("")
	return toProjectData(projects), nil
}

// RenameProject 给报送项目改名；id 不变，历史筛选快照不改。
func (s *TriageService) RenameProject(idOrName, newName string) ([]TriageProjectData, error) {
	ws, err := s.ws()
	if err != nil {
		return nil, err
	}
	projects, _, err := ws.RenameReportProject(idOrName, newName)
	if err != nil {
		return nil, err
	}
	s.emitTriage("")
	return toProjectData(projects), nil
}

// RemoveProject 删除报送项目；旧筛选记录在下次读取时把相关条目标为待定。
func (s *TriageService) RemoveProject(idOrName string) ([]TriageProjectData, error) {
	ws, err := s.ws()
	if err != nil {
		return nil, err
	}
	projects, _, err := ws.RemoveReportProject(idOrName)
	if err != nil {
		return nil, err
	}
	s.emitTriage("")
	return toProjectData(projects), nil
}

// Triage 读取/重建本周筛选记录。来源或项目变化时自动重建；新条目为未判。
func (s *TriageService) Triage(week, group string) (TriageData, error) {
	ws, err := s.ws()
	if err != nil {
		return TriageData{}, err
	}
	t, _, err := core.LoadTriage(ws, week, group)
	if err != nil {
		return TriageData{}, err
	}
	return toTriageData(t), nil
}

// TriageDecide 写一条决策。decision 取 accepted / rejected / skipped；accepted 时 project 必填。
func (s *TriageService) TriageDecide(week, group, itemID, decision, projectID string) (TriageData, error) {
	ws, err := s.ws()
	if err != nil {
		return TriageData{}, err
	}
	t, _, err := core.LoadTriage(ws, week, group)
	if err != nil {
		return TriageData{}, err
	}
	if projectID != "" {
		p, ok := store.FindReportProject(triageProjects(t.Projects), projectID)
		if !ok {
			return TriageData{}, fmt.Errorf("%w: 找不到报送项目 %q", store.ErrNotFound, projectID)
		}
		projectID = p.ID
	}
	if err := core.SetTriageDecision(t, itemID, decision, projectID, s.clock()); err != nil {
		return TriageData{}, err
	}
	if err := ws.WriteTriage(t); err != nil {
		return TriageData{}, err
	}
	s.emitTriage(week)
	return toTriageData(t), nil
}

// TriageReset 清空本周筛选决策（保留条目）。
func (s *TriageService) TriageReset(week, group string) (TriageData, error) {
	ws, err := s.ws()
	if err != nil {
		return TriageData{}, err
	}
	t, _, err := core.LoadTriage(ws, week, group)
	if err != nil {
		return TriageData{}, err
	}
	for i := range t.Items {
		t.Items[i].Decision = ""
		t.Items[i].Project = ""
		t.Items[i].DecidedAt = ""
	}
	t.UpdatedAt = s.clock().Format(time.RFC3339)
	if err := ws.WriteTriage(t); err != nil {
		return TriageData{}, err
	}
	s.emitTriage(week)
	return toTriageData(t), nil
}

func toProjectData(projects []store.ReportProject) []TriageProjectData {
	out := make([]TriageProjectData, 0, len(projects))
	for _, p := range projects {
		out = append(out, TriageProjectData{ID: p.ID, Name: p.Name})
	}
	return out
}

func triageProjects(list []store.TriageProject) []store.ReportProject {
	out := make([]store.ReportProject, 0, len(list))
	for _, p := range list {
		out = append(out, store.ReportProject{ID: p.ID, Name: p.Name})
	}
	return out
}

func toTriageData(t *store.Triage) TriageData {
	out := TriageData{
		Week:       t.Week,
		Group:      t.Group,
		Lead:       t.Lead,
		SourceHash: t.SourceHash,
		Projects:   make([]TriageProjectData, 0, len(t.Projects)),
		Items:      make([]TriageItemData, 0, len(t.Items)),
		Missing:    nonNilStrings(t.Missing),
	}
	projectNames := map[string]string{}
	for _, p := range t.Projects {
		out.Projects = append(out.Projects, TriageProjectData{ID: p.ID, Name: p.Name})
		projectNames[p.ID] = p.Name
	}
	byProject := map[string]int{}
	sectionDone := map[string]int{}
	sectionTotal := map[string]int{}
	for _, it := range t.Items {
		sectionTotal[it.Section]++
		if it.Decision != "" {
			sectionDone[it.Section]++
		}
		if it.Decision == store.TriageAccepted {
			byProject[it.Project]++
		}
		out.Items = append(out.Items, TriageItemData{
			ID:           it.ID,
			Person:       it.Person,
			Name:         it.Name,
			Section:      it.Section,
			SectionTitle: core.TriageSectionTitle(it.Section),
			Domain:       it.Domain,
			Text:         it.Text,
			Decision:     it.Decision,
			Project:      it.Project,
			ProjectName:  projectNames[it.Project],
			Order:        it.Order,
			DecidedAt:    it.DecidedAt,
		})
	}
	for _, section := range core.TriageSections {
		total := sectionTotal[section]
		done := sectionDone[section]
		out.Sections = append(out.Sections, TriageSectionData{
			Section: section, Title: core.TriageSectionTitle(section), Done: done, Total: total, Undecided: total - done,
		})
	}
	c := core.CountTriage(t)
	out.Counts = TriageCountsData{
		Total: c.Total, Done: c.Done, Accepted: c.Accepted, Rejected: c.Rejected,
		Skipped: c.Skipped, Undecided: c.Undecided, ByProject: byProject,
	}
	out.AllJudged = c.Undecided == 0
	return out
}

// ProjectGroupList 是第 3 步左侧项目列表的一行。
type ProjectGroupList struct {
	Project       string `json:"project"`
	ProjectName   string `json:"projectName"`
	Exists        bool   `json:"exists"`
	Status        string `json:"status"`
	HumanEdited   bool   `json:"humanEdited"`
	SourceChanged bool   `json:"sourceChanged"`
	Candidates    int    `json:"candidates"`
	FinalizedAt   string `json:"finalizedAt"`
	EditedAt      string `json:"editedAt"`
}

// ProjectGroups 列出各项目的组报状态。
func (s *ReportService) ProjectGroups(week, group string) ([]ProjectGroupList, error) {
	ws, _, err := s.instances()
	if err != nil {
		return nil, err
	}
	lead, _, err := core.ResolveLead(ws, week, group)
	if err != nil {
		return nil, err
	}
	projects, err := ws.ReportProjects()
	if err != nil {
		return nil, err
	}
	t, _ := ws.ReadTriage(week, lead.ID)
	accepted := map[string]int{}
	if t != nil {
		for _, it := range t.Items {
			if it.Decision == store.TriageAccepted {
				accepted[it.Project]++
			}
		}
	}
	out := make([]ProjectGroupList, 0, len(projects))
	for _, p := range projects {
		row := ProjectGroupList{Project: p.ID, ProjectName: p.Name, Status: "none", Candidates: accepted[p.ID]}
		d, err := ws.ReadDraftKind(store.GroupReportKind(p.ID), week, lead.ID)
		if err == nil {
			row.Exists = true
			row.Status = d.Status
			row.HumanEdited = d.HumanEdited
			row.SourceChanged = core.ProjectGroupSourceChanged(ws, d)
			if !d.FinalizedAt.IsZero() {
				row.FinalizedAt = formatTime(d.FinalizedAt)
			}
			if !d.EditedAt.IsZero() {
				row.EditedAt = formatTime(d.EditedAt)
			}
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

// ProjectGroup 读取某个项目的组报。
func (s *ReportService) ProjectGroup(week, group, projectID string) (GroupData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return GroupData{}, err
	}
	lead, _, err := core.ResolveLead(ws, week, group)
	if err != nil {
		return GroupData{}, err
	}
	project, err := projectByIDOrName(ws, projectID)
	if err != nil {
		return GroupData{}, err
	}
	d, err := ws.ReadDraftKind(store.GroupReportKind(project.ID), week, lead.ID)
	if errors.Is(err, store.ErrNotFound) {
		return GroupData{LeadID: lead.ID, LeadName: lead.Name, Group: group, Week: week, Status: "none",
			Missing: []string{}, Project: project.ID, ProjectName: project.Name}, nil
	}
	if err != nil {
		return GroupData{}, err
	}
	return toProjectGroupData(ws, d, lead, project), nil
}

// AssembleProjectGroup 按筛选结果生成/重新生成某个项目的组报草稿。
func (s *ReportService) AssembleProjectGroup(week, group, projectID string, force, allowUnreviewed bool) (GroupData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return GroupData{}, err
	}
	project, err := projectByIDOrName(ws, projectID)
	if err != nil {
		return GroupData{}, err
	}
	d, err := core.AssembleProjectGroup(ws, week, group, project.ID, force, allowUnreviewed, s.clock())
	if err != nil {
		return GroupData{}, err
	}
	lead, _, _ := core.ResolveLead(ws, week, group)
	s.emitReport(nil, []string{week + "/" + store.GroupReportKind(project.ID) + "/" + d.Person})
	return toProjectGroupData(ws, d, lead, project), nil
}

// SaveProjectGroup 保存某个项目的组报修改。
func (s *ReportService) SaveProjectGroup(week, group, projectID, body string) (GroupData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return GroupData{}, err
	}
	project, err := projectByIDOrName(ws, projectID)
	if err != nil {
		return GroupData{}, err
	}
	lead, _, err := core.ResolveLead(ws, week, group)
	if err != nil {
		return GroupData{}, err
	}
	d, err := ws.EditDraftKind(store.GroupReportKind(project.ID), week, lead.ID, body, s.clock())
	if err != nil {
		return GroupData{}, err
	}
	s.emitReport(nil, []string{week + "/" + store.GroupReportKind(project.ID) + "/" + lead.ID})
	return toProjectGroupData(ws, d, lead, project), nil
}

// FinalizeProjectGroup 定稿某个项目的组报。
func (s *ReportService) FinalizeProjectGroup(week, group, projectID string) (GroupData, error) {
	return s.changeProjectGroup(week, group, projectID, func(ws *store.Workspace, kind, leadID string) (*store.Draft, error) {
		return ws.FinalizeDraftKind(kind, week, leadID, s.clock())
	})
}

// UnfinalizeProjectGroup 取消定稿某个项目的组报。
func (s *ReportService) UnfinalizeProjectGroup(week, group, projectID string) (GroupData, error) {
	return s.changeProjectGroup(week, group, projectID, func(ws *store.Workspace, kind, leadID string) (*store.Draft, error) {
		return ws.UnfinalizeDraftKind(kind, week, leadID)
	})
}

func (s *ReportService) changeProjectGroup(week, group, projectID string,
	change func(ws *store.Workspace, kind, leadID string) (*store.Draft, error)) (GroupData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return GroupData{}, err
	}
	project, err := projectByIDOrName(ws, projectID)
	if err != nil {
		return GroupData{}, err
	}
	lead, _, err := core.ResolveLead(ws, week, group)
	if err != nil {
		return GroupData{}, err
	}
	d, err := change(ws, store.GroupReportKind(project.ID), lead.ID)
	if err != nil {
		return GroupData{}, err
	}
	s.emitReport(nil, []string{week + "/" + store.GroupReportKind(project.ID) + "/" + lead.ID})
	return toProjectGroupData(ws, d, lead, project), nil
}

func toProjectGroupData(ws *store.Workspace, d *store.Draft, lead *store.Person, project store.ReportProject) GroupData {
	out := toGroupData(ws, d, lead)
	out.Project = project.ID
	out.ProjectName = project.Name
	out.SourceChanged = core.ProjectGroupSourceChanged(ws, d)
	out.Submitted = false
	return out
}

func projectByIDOrName(ws *store.Workspace, idOrName string) (store.ReportProject, error) {
	projects, err := ws.ReportProjects()
	if err != nil {
		return store.ReportProject{}, err
	}
	p, ok := store.FindReportProject(projects, idOrName)
	if !ok {
		return store.ReportProject{}, fmt.Errorf("%w: 找不到报送项目 %q", store.ErrNotFound, idOrName)
	}
	return p, nil
}
