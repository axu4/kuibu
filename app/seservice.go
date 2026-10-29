package main

// 我当 SE 的项目：收各组（DE）交来的项目报，合并成项目报。周报、月报共用，
// period 传周编号（2026-W44）或月份（2026-10）。读写一律经核心库。

import (
	"errors"

	"github.com/axu4/kuibu/internal/core"
	"github.com/axu4/kuibu/internal/store"
)

// SEProjects 返回我是 SE 的项目。
func (s *ReportService) SEProjects() ([]store.ReportProject, error) {
	ws, _, err := s.instances()
	if err != nil {
		return nil, err
	}
	return core.MineProjects(ws)
}

// SEInbox 返回某项目某期各组的交报情况。
func (s *ReportService) SEInbox(period, project string) (core.ProjectInbox, error) {
	ws, _, err := s.instances()
	if err != nil {
		return core.ProjectInbox{}, err
	}
	view, err := core.LoadProjectInbox(ws, period, project, s.clock())
	if err != nil {
		return core.ProjectInbox{}, err
	}
	return *view, nil
}

// SEInboxSource 读取某组交来的内容（粘贴原文或引用的定稿组报）。
func (s *ReportService) SEInboxSource(period, project, group string) (core.ProjectInboxSource, error) {
	ws, _, err := s.instances()
	if err != nil {
		return core.ProjectInboxSource{}, err
	}
	src, err := core.ReadProjectInboxSource(ws, period, project, group, s.clock())
	if err != nil {
		return core.ProjectInboxSource{}, err
	}
	return *src, nil
}

// SEInboxSubmit 把某组 DE 交来的项目报粘贴归档，返回刷新后的交报情况。
func (s *ReportService) SEInboxSubmit(period, project, group, body string) (core.ProjectInbox, error) {
	ws, _, err := s.instances()
	if err != nil {
		return core.ProjectInbox{}, err
	}
	r, err := core.SubmitProjectInbox(ws, period, project, group, body, s.clock())
	if err != nil {
		return core.ProjectInbox{}, err
	}
	s.emitReport(nil, []string{period + "/" + r.Level + "/" + r.Person})
	return s.SEInbox(period, project)
}

// SEInboxDelete 删除某组交来的项目报（移入历史）。
func (s *ReportService) SEInboxDelete(period, project, group string) (core.ProjectInbox, error) {
	ws, _, err := s.instances()
	if err != nil {
		return core.ProjectInbox{}, err
	}
	groupID, err := core.DeleteProjectInbox(ws, period, project, group, s.clock())
	if err != nil {
		return core.ProjectInbox{}, err
	}
	p, _ := projectByIDOrName(ws, project)
	s.emitReport(nil, []string{period + "/" + store.ProjectInboxLevel(p.ID) + "/" + groupID})
	return s.SEInbox(period, project)
}

// SEReport 返回某项目某期的 SE 项目报；还没生成时 Exists 为 false。
func (s *ReportService) SEReport(period, project string) (GroupData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return GroupData{}, err
	}
	p, err := projectByIDOrName(ws, project)
	if err != nil {
		return GroupData{}, err
	}
	d, err := ws.ReadProjectReport(period, p.ID)
	if errors.Is(err, store.ErrNotFound) {
		return GroupData{Week: period, Status: "none", Missing: []string{}, Project: p.ID, ProjectName: p.Name, LeadName: p.Name}, nil
	}
	if err != nil {
		return GroupData{}, err
	}
	return s.toSEData(ws, d, p), nil
}

// SEGenerate 合并各组交来的内容生成项目报草稿（不调 AI）。有组没交且 allowMissing 为
// false 时返回 NOT_FINAL；已人工修改或已定稿且 force 为 false 时返回 HUMAN_EDITS_EXIST。
func (s *ReportService) SEGenerate(period, project string, allowMissing, force bool) (GroupData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return GroupData{}, err
	}
	p, err := projectByIDOrName(ws, project)
	if err != nil {
		return GroupData{}, err
	}
	d, err := core.AssembleProjectReport(ws, period, p.ID, allowMissing, force, s.clock())
	if err != nil {
		return GroupData{}, err
	}
	s.emitReport(nil, []string{period + "/" + store.ProjectReportDirName + "/" + p.ID})
	return s.toSEData(ws, d, p), nil
}

// SESave 保存项目报人工修改。
func (s *ReportService) SESave(period, project, body string) (GroupData, error) {
	return s.changeSE(period, project, func(ws *store.Workspace, id string) (*store.Draft, error) {
		return ws.EditDraftKind(store.ProjectReportDirName, period, id, body, s.clock())
	})
}

// SEFinalize 项目报定稿。
func (s *ReportService) SEFinalize(period, project string) (GroupData, error) {
	return s.changeSE(period, project, func(ws *store.Workspace, id string) (*store.Draft, error) {
		return ws.FinalizeDraftKind(store.ProjectReportDirName, period, id, s.clock())
	})
}

// SEUnfinalize 项目报取消定稿。
func (s *ReportService) SEUnfinalize(period, project string) (GroupData, error) {
	return s.changeSE(period, project, func(ws *store.Workspace, id string) (*store.Draft, error) {
		return ws.UnfinalizeDraftKind(store.ProjectReportDirName, period, id)
	})
}

// SEExportClipboard 返回定稿项目报的飞书 HTML 与纯文本；未定稿返回 NOT_FINAL。
func (s *ReportService) SEExportClipboard(period, project string) (ExportContent, error) {
	ws, _, err := s.instances()
	if err != nil {
		return ExportContent{}, err
	}
	kind := "se"
	if store.ValidMonth(period) {
		kind = "month-se"
	}
	doc, err := core.ExportFinalProject(ws, kind, period, "", project)
	if err != nil {
		return ExportContent{}, err
	}
	return ExportContent{Title: doc.Title, HTML: core.MarkdownToHTML(doc.Body), Text: core.MarkdownToText(doc.Body)}, nil
}

// SEExport 弹出保存对话框，导出定稿项目报。
func (s *ReportService) SEExport(period, project string) (string, error) {
	ws, _, err := s.instances()
	if err != nil {
		return "", err
	}
	kind := "se"
	if store.ValidMonth(period) {
		kind = "month-se"
	}
	doc, err := core.ExportFinalProject(ws, kind, period, "", project)
	if err != nil {
		return "", err
	}
	return s.saveExportDoc(doc)
}

// MarkProjectDelivered 记录（或撤销）按项目组报 / 月报已交给对方 SE。
func (s *ReportService) MarkProjectDelivered(period, group, project string, delivered bool) (GroupData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return GroupData{}, err
	}
	p, err := projectByIDOrName(ws, project)
	if err != nil {
		return GroupData{}, err
	}
	d, err := core.MarkProjectGroupDelivered(ws, period, group, p.ID, delivered, s.clock())
	if err != nil {
		return GroupData{}, err
	}
	s.emitReport(nil, []string{period + "/" + store.GroupReportKind(p.ID) + "/" + d.Person})
	if store.ValidMonth(period) {
		return toProjectMonthData(ws, d, p), nil
	}
	return toProjectGroupData(ws, d, nil, p), nil
}

func (s *ReportService) changeSE(period, project string, fn func(*store.Workspace, string) (*store.Draft, error)) (GroupData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return GroupData{}, err
	}
	p, err := projectByIDOrName(ws, project)
	if err != nil {
		return GroupData{}, err
	}
	d, err := fn(ws, p.ID)
	if err != nil {
		return GroupData{}, err
	}
	s.emitReport(nil, []string{period + "/" + store.ProjectReportDirName + "/" + p.ID})
	return s.toSEData(ws, d, p), nil
}

func (s *ReportService) toSEData(ws *store.Workspace, d *store.Draft, p store.ReportProject) GroupData {
	out := GroupData{
		Exists:        true,
		LeadID:        d.Person,
		LeadName:      p.Name,
		Week:          d.Week,
		Status:        d.Status,
		HumanEdited:   d.HumanEdited,
		SourceChanged: core.ProjectReportSourceChanged(ws, d, s.clock()),
		Missing:       nonNilStrings(d.Missing),
		Body:          d.Body,
		Project:       p.ID,
		ProjectName:   p.Name,
	}
	if !d.GeneratedAt.IsZero() {
		out.GeneratedAt = formatTime(d.GeneratedAt)
	}
	if !d.FinalizedAt.IsZero() {
		out.FinalizedAt = formatTime(d.FinalizedAt)
	}
	return out
}
