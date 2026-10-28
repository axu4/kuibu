package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/axu4/kuibu/internal/core"
	"github.com/axu4/kuibu/internal/store"
)

// 项目月报：从本期各周已定稿的按项目组报拉出候选，每个项目各出一份月报。
//
// 数据在 internal/core/month_pull.go 与 internal/store/month_pull.go 里：
// 拉取记录（reports/<月>/pull/<组长>.md）决定本期周范围与纳入的来源，
// 项目月报草稿沿用按项目组报的路径（reports/<月>/group/<项目 id>/<组长>.md）。
// 本层只做月份 / 项目解析、事件转发与结构转换；没有配置 reportProjects 的
// 工作台继续走旧的单份月报方法（MonthReport / GenerateMonth / ...）。

// ProjectMonthList 是月报首页左侧项目列表的一行。
type ProjectMonthList struct {
	Project     string `json:"project"`
	ProjectName string `json:"projectName"`
	Status      string `json:"status"`
	Exists      bool   `json:"exists"`
	HumanEdited bool   `json:"humanEdited"`
	// Candidates 是会进入月报的候选条目数（总结 + 计划）。
	Candidates int `json:"candidates"`
	// Missing 是本期范围内这个项目未定稿 / 未提交的周。
	Missing []string `json:"missing"`
	// PlanFrom 是下月计划的来源周，没有来源时为空。
	PlanFrom      string `json:"planFrom"`
	FinalizedAt   string `json:"finalizedAt"`
	EditedAt      string `json:"editedAt"`
	SourceChanged bool   `json:"sourceChanged"`
}

// monthRef 把 ""/this/last/next/YYYY-MM/日期 解析成月份编号。
func (s *ReportService) monthRef(month string) (string, error) {
	return store.ParseMonth(month, s.clock())
}

// MonthSources 返回月报第 ① 步的来源视图：本期周范围、各周 × 项目的定稿状态
// 与纳入情况、缺周与候选统计。
func (s *ReportService) MonthSources(month, group string) (core.MonthSourceView, error) {
	ws, _, err := s.instances()
	if err != nil {
		return core.MonthSourceView{}, err
	}
	m, err := s.monthRef(month)
	if err != nil {
		return core.MonthSourceView{}, err
	}
	data, err := core.MonthSourceData(ws, m, group)
	if err != nil {
		return core.MonthSourceView{}, err
	}
	return *data, nil
}

// SetMonthScope 修改本期周范围（上月最后一周 / 本月周 / 下月第一周），
// 返回更新后的来源视图。新增周会自动纳入其中已定稿的周组报。
func (s *ReportService) SetMonthScope(month, group string, weeks []string) (core.MonthSourceView, error) {
	ws, _, err := s.instances()
	if err != nil {
		return core.MonthSourceView{}, err
	}
	m, err := s.monthRef(month)
	if err != nil {
		return core.MonthSourceView{}, err
	}
	_, data, err := core.SetMonthScope(ws, m, group, weeks)
	if err != nil {
		return core.MonthSourceView{}, err
	}
	s.emitReport(nil, nil)
	return *data, nil
}

// SetMonthSource 纳入 / 去选一份定稿周组报，返回更新后的来源视图。
// included 为 true 时该周组报必须已定稿，否则返回 NOT_FINAL。
func (s *ReportService) SetMonthSource(month, group, week, project string, included bool) (core.MonthSourceView, error) {
	ws, _, err := s.instances()
	if err != nil {
		return core.MonthSourceView{}, err
	}
	m, err := s.monthRef(month)
	if err != nil {
		return core.MonthSourceView{}, err
	}
	if _, err := core.SetMonthSource(ws, m, group, week, project, included); err != nil {
		return core.MonthSourceView{}, err
	}
	data, err := core.MonthSourceData(ws, m, group)
	if err != nil {
		return core.MonthSourceView{}, err
	}
	s.emitReport(nil, nil)
	return *data, nil
}

// MonthProjectCandidates 返回一个项目本期的月报候选（总结 / 计划 / 更早计划 / 不纳入）。
func (s *ReportService) MonthProjectCandidates(month, group, project string) (core.MonthCandidatesData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return core.MonthCandidatesData{}, err
	}
	m, err := s.monthRef(month)
	if err != nil {
		return core.MonthCandidatesData{}, err
	}
	c, err := core.MonthProjectCandidates(ws, m, group, project)
	if err != nil {
		return core.MonthCandidatesData{}, err
	}
	return *c, nil
}

// SetMonthItemPool 调整一条候选的归口（summary / plan / archive / excluded），
// 返回更新后的候选。
func (s *ReportService) SetMonthItemPool(month, group, project, section, itemID, pool string) (core.MonthCandidatesData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return core.MonthCandidatesData{}, err
	}
	m, err := s.monthRef(month)
	if err != nil {
		return core.MonthCandidatesData{}, err
	}
	c, err := core.SetMonthItemPool(ws, m, group, project, section, itemID, pool)
	if err != nil {
		return core.MonthCandidatesData{}, err
	}
	s.emitReport(nil, nil)
	return *c, nil
}

// MonthProjectReport 读取某个项目的月报；还没生成时 Exists 为 false、Status 为 none。
func (s *ReportService) MonthProjectReport(month, group, project string) (GroupData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return GroupData{}, err
	}
	m, err := s.monthRef(month)
	if err != nil {
		return GroupData{}, err
	}
	p, err := projectByIDOrName(ws, project)
	if err != nil {
		return GroupData{}, err
	}
	lead, groupName, err := core.MonthLead(ws, m, group)
	if err != nil {
		return GroupData{}, err
	}
	d, err := ws.ReadDraftKind(store.GroupReportKind(p.ID), m, lead.ID)
	if errors.Is(err, store.ErrNotFound) {
		return GroupData{LeadID: lead.ID, LeadName: lead.Name, Group: groupName, Week: m, Status: "none",
			Missing: []string{}, Project: p.ID, ProjectName: p.Name}, nil
	}
	if err != nil {
		return GroupData{}, err
	}
	return toProjectMonthData(ws, d, p), nil
}

// GenerateMonthProject 按当前拉取记录为某个项目生成 / 重新生成月报草稿。
// 本期有未定稿 / 未提交的来源且 allowMissing 为 false 时返回 NOT_FINAL；
// 已有草稿覆盖人工修改时按 force 返回 HUMAN_EDITS_EXIST / NEEDS_CONFIRM。
func (s *ReportService) GenerateMonthProject(month, group, project string, allowMissing, force bool) (GroupData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return GroupData{}, err
	}
	m, err := s.monthRef(month)
	if err != nil {
		return GroupData{}, err
	}
	p, err := projectByIDOrName(ws, project)
	if err != nil {
		return GroupData{}, err
	}
	d, err := core.AssembleMonthProject(ws, m, group, p.ID, allowMissing, force, s.clock())
	if err != nil {
		return GroupData{}, err
	}
	s.emitReport(nil, []string{m + "/" + store.GroupReportKind(p.ID) + "/" + d.Person})
	return toProjectMonthData(ws, d, p), nil
}

// SaveMonthProject 保存某个项目月报的人工修改。
func (s *ReportService) SaveMonthProject(month, group, project, body string) (GroupData, error) {
	return s.changeMonthProject(month, group, project,
		func(ws *store.Workspace, projectID, m, leadID string) (*store.Draft, error) {
			return ws.EditDraftKind(store.GroupReportKind(projectID), m, leadID, body, s.clock())
		})
}

// FinalizeMonthProject 定稿某个项目的月报。
func (s *ReportService) FinalizeMonthProject(month, group, project string) (GroupData, error) {
	return s.changeMonthProject(month, group, project,
		func(ws *store.Workspace, projectID, m, leadID string) (*store.Draft, error) {
			return ws.FinalizeDraftKind(store.GroupReportKind(projectID), m, leadID, s.clock())
		})
}

// UnfinalizeMonthProject 取消定稿某个项目的月报。
func (s *ReportService) UnfinalizeMonthProject(month, group, project string) (GroupData, error) {
	return s.changeMonthProject(month, group, project,
		func(ws *store.Workspace, projectID, m, leadID string) (*store.Draft, error) {
			return ws.UnfinalizeDraftKind(store.GroupReportKind(projectID), m, leadID)
		})
}

// changeMonthProject 解析月份、项目与组长后执行写操作，并通知前端刷新。
func (s *ReportService) changeMonthProject(month, group, project string,
	change func(ws *store.Workspace, projectID, month, leadID string) (*store.Draft, error)) (GroupData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return GroupData{}, err
	}
	m, err := s.monthRef(month)
	if err != nil {
		return GroupData{}, err
	}
	p, err := projectByIDOrName(ws, project)
	if err != nil {
		return GroupData{}, err
	}
	lead, _, err := core.MonthLead(ws, m, group)
	if err != nil {
		return GroupData{}, err
	}
	d, err := change(ws, p.ID, m, lead.ID)
	if err != nil {
		return GroupData{}, err
	}
	s.emitReport(nil, []string{m + "/" + store.GroupReportKind(p.ID) + "/" + lead.ID})
	return toProjectMonthData(ws, d, p), nil
}

// MonthProjectList 列出本期各项目的月报状态与候选统计，按配置顺序。
func (s *ReportService) MonthProjectList(month, group string) ([]ProjectMonthList, error) {
	ws, _, err := s.instances()
	if err != nil {
		return nil, err
	}
	m, err := s.monthRef(month)
	if err != nil {
		return nil, err
	}
	projects, err := ws.ReportProjects()
	if err != nil {
		return nil, err
	}
	if len(projects) == 0 {
		return []ProjectMonthList{}, nil
	}
	lead, _, err := core.MonthLead(ws, m, group)
	if err != nil {
		return nil, err
	}
	out := make([]ProjectMonthList, 0, len(projects))
	for _, p := range projects {
		c, err := core.MonthProjectCandidates(ws, m, group, p.ID)
		if err != nil {
			return nil, err
		}
		row := ProjectMonthList{
			Project:     p.ID,
			ProjectName: p.Name,
			Status:      "none",
			Candidates:  c.Counts.Total,
			Missing:     nonNilStrings(c.Missing),
			PlanFrom:    c.PlanFrom,
		}
		d, err := ws.ReadDraftKind(store.GroupReportKind(p.ID), m, lead.ID)
		switch {
		case err == nil:
			row.Exists = true
			row.Status = d.Status
			row.HumanEdited = d.HumanEdited
			row.SourceChanged = core.MonthProjectSourceChanged(ws, d)
			if !d.FinalizedAt.IsZero() {
				row.FinalizedAt = formatTime(d.FinalizedAt)
			}
			if !d.EditedAt.IsZero() {
				row.EditedAt = formatTime(d.EditedAt)
			}
		case !errors.Is(err, store.ErrNotFound):
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

// toProjectMonthData 把项目月报草稿转成前端结构。
func toProjectMonthData(ws *store.Workspace, d *store.Draft, project store.ReportProject) GroupData {
	out := GroupData{
		Exists:        true,
		LeadID:        d.Person,
		LeadName:      d.Name,
		Group:         d.Group,
		Week:          d.Week,
		Status:        d.Status,
		HumanEdited:   d.HumanEdited,
		SourceChanged: core.MonthProjectSourceChanged(ws, d),
		Missing:       nonNilStrings(d.Missing),
		Model:         d.Model,
		Body:          d.Body,
		Project:       project.ID,
		ProjectName:   project.Name,
	}
	if !d.GeneratedAt.IsZero() {
		out.GeneratedAt = formatTime(d.GeneratedAt)
	}
	if !d.FinalizedAt.IsZero() {
		out.FinalizedAt = formatTime(d.FinalizedAt)
	}
	return out
}

// saveExportDoc 弹出保存对话框，把定稿报告写成 Markdown；用户取消时返回空路径。
func (s *ReportService) saveExportDoc(doc *core.ExportDoc) (string, error) {
	text, err := doc.Render(core.FormatMarkdown)
	if err != nil {
		return "", err
	}
	if s.workspace == nil || s.workspace.app == nil {
		return "", fmt.Errorf("保存对话框不可用")
	}
	path, err := s.workspace.app.Dialog.SaveFile().
		SetFilename(doc.FileName).
		CanCreateDirectories(true).
		SetMessage("导出「" + doc.Title + "」").
		SetButtonText("导出").
		PromptForSingleSelection()
	if err != nil {
		return "", fmt.Errorf("打开保存对话框失败: %w", err)
	}
	if path == "" {
		return "", nil
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "", fmt.Errorf("写入导出文件失败: %w", err)
	}
	return path, nil
}

// ExportMonthProject 弹出保存对话框，导出某个项目的定稿月报。
// 未定稿返回 NOT_FINAL；用户取消时返回空路径且不报错。
func (s *ReportService) ExportMonthProject(month, group, project string) (string, error) {
	ws, _, err := s.instances()
	if err != nil {
		return "", err
	}
	m, err := s.monthRef(month)
	if err != nil {
		return "", err
	}
	doc, err := core.ExportFinalProject(ws, "month-group-project", m, group, project)
	if err != nil {
		return "", err
	}
	return s.saveExportDoc(doc)
}

// ExportMonthProjectClipboard 返回某个项目定稿月报的飞书 HTML 与纯文本。
// 只返回内容，不发送到任何地方；未定稿返回 NOT_FINAL。
func (s *ReportService) ExportMonthProjectClipboard(month, group, project string) (ExportContent, error) {
	ws, _, err := s.instances()
	if err != nil {
		return ExportContent{}, err
	}
	m, err := s.monthRef(month)
	if err != nil {
		return ExportContent{}, err
	}
	doc, err := core.ExportFinalProject(ws, "month-group-project", m, group, project)
	if err != nil {
		return ExportContent{}, err
	}
	return ExportContent{Title: doc.Title, HTML: core.MarkdownToHTML(doc.Body), Text: core.MarkdownToText(doc.Body)}, nil
}
