package main

import (
	"context"
	"errors"
	"time"

	"github.com/axu4/kuibu/internal/core"
	"github.com/axu4/kuibu/internal/llm"
	"github.com/axu4/kuibu/internal/store"
)

// ReportMonth 是一个月的信息。
type ReportMonth struct {
	// Month 是月份编号，形如 2026-10。
	Month string `json:"month"`
	// CollectDay 是组长月报收集日（1-31）。
	CollectDay int `json:"collectDay"`
	// Weeks 是按周报日归属本月的周。
	Weeks []string `json:"weeks"`
	// IsCurrent 表示是否是当前月。
	IsCurrent bool `json:"isCurrent"`
}

// Month 返回月份信息。ref 为 ""/this/last/next/YYYY-MM/日期。
func (s *ReportService) Month(ref string) (ReportMonth, error) {
	ws, _, err := s.instances()
	if err != nil {
		return ReportMonth{}, err
	}
	m, err := store.ParseMonth(ref, s.clock())
	if err != nil {
		return ReportMonth{}, err
	}
	return s.monthInfo(ws, m)
}

// ShiftMonth 返回相对 month 偏移 delta 个月的月份信息。
func (s *ReportService) ShiftMonth(month string, delta int) (ReportMonth, error) {
	ws, _, err := s.instances()
	if err != nil {
		return ReportMonth{}, err
	}
	start, err := store.MonthStart(month)
	if err != nil {
		return ReportMonth{}, err
	}
	return s.monthInfo(ws, store.MonthOf(start.AddDate(0, delta, 0)))
}

func (s *ReportService) monthInfo(ws *store.Workspace, month string) (ReportMonth, error) {
	day, err := ws.ReportMonthDay()
	if err != nil {
		return ReportMonth{}, err
	}
	weekday, err := ws.ReportWeekday()
	if err != nil {
		return ReportMonth{}, err
	}
	weeks, err := core.WeeksOfMonth(month, weekday)
	if err != nil {
		return ReportMonth{}, err
	}
	return ReportMonth{Month: month, CollectDay: day, Weeks: weeks, IsCurrent: month == store.MonthOf(s.clock())}, nil
}

// SetReportMonthDay 修改月报收集日。
func (s *ReportService) SetReportMonthDay(day int) (int, error) {
	ws, _, err := s.instances()
	if err != nil {
		return 0, err
	}
	if err := ws.SetReportMonthDay(day); err != nil {
		return 0, err
	}
	s.emitReport(nil, nil)
	return ws.ReportMonthDay()
}

// monthTarget 返回月报的目录与文件 ID：group → 组长本人，product → product。
func monthTarget(ws *store.Workspace, month, level string) (string, string, *store.Person, error) {
	if level == "product" {
		return store.ProductDirName, store.ProductReportID, nil, nil
	}
	lead, _, err := core.MonthLead(ws, month, "")
	if err != nil {
		return "", "", nil, err
	}
	return store.GroupDirName, lead.ID, lead, nil
}

// monthPlan 生成组月报 / 产品月报计划。
func monthPlan(ws *store.Workspace, month, level string, allowMissing bool) (*core.GroupPlan, error) {
	if level == "product" {
		return core.PlanProductMonth(ws, month, allowMissing)
	}
	return core.PlanGroupMonth(ws, month, "", allowMissing)
}

// MonthReport 返回组月报（level=group）或产品月报（level=product）；还没生成时 Exists 为 false。
func (s *ReportService) MonthReport(month, level string) (GroupData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return GroupData{}, err
	}
	kind, id, lead, err := monthTarget(ws, month, level)
	if err != nil {
		return GroupData{}, err
	}
	d, err := ws.ReadDraftKind(kind, month, id)
	if errors.Is(err, store.ErrNotFound) {
		out := GroupData{Week: month, Status: "none", Missing: []string{}, LeadName: "产品月报"}
		if lead != nil {
			out.LeadID, out.LeadName = lead.ID, lead.Name
		}
		return out, nil
	}
	if err != nil {
		return GroupData{}, err
	}
	return toGroupData(ws, d, lead), nil
}

// PrepareMonth 生成月报外发预览，不发请求。
func (s *ReportService) PrepareMonth(month, level string, allowMissing, force bool) (GroupPlanData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return GroupPlanData{}, err
	}
	plan, err := monthPlan(ws, month, level, allowMissing)
	if err != nil {
		return GroupPlanData{}, err
	}
	kind, id, _, err := monthTarget(ws, month, level)
	if err != nil {
		return GroupPlanData{}, err
	}
	if err := ws.CheckRegenerateKind(kind, month, id, force); err != nil {
		return GroupPlanData{}, err
	}
	out := &llm.Outbound{Env: s.llmEnv()}
	preview, _, err := out.Prepare("", core.GroupRequest(plan))
	if err != nil {
		return GroupPlanData{}, err
	}
	return GroupPlanData{Preview: preview, Missing: nonNilStrings(plan.Missing)}, nil
}

// GenerateMonth 发送并保存月报草稿。只能由外发确认弹窗的「确认发送」调用。
func (s *ReportService) GenerateMonth(month, level string, allowMissing, force bool) (GroupData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return GroupData{}, err
	}
	plan, err := monthPlan(ws, month, level, allowMissing)
	if err != nil {
		return GroupData{}, err
	}
	kind, id, lead, err := monthTarget(ws, month, level)
	if err != nil {
		return GroupData{}, err
	}
	if err := ws.CheckRegenerateKind(kind, month, id, force); err != nil {
		return GroupData{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	out := &llm.Outbound{Env: s.llmEnv()}
	preview, resp, err := out.Send(ctx, "", core.GroupRequest(plan), true)
	if err != nil {
		return GroupData{}, err
	}
	var (
		d       *store.Draft
		warning string
	)
	if level == "product" {
		d, warning, err = core.SaveProductReport(ws, plan, preview.ModelName, resp.Text, force, s.clock())
	} else {
		d, warning, err = core.SaveGroupReport(ws, plan, preview.ModelName, resp.Text, force, s.clock())
	}
	if err != nil {
		return GroupData{}, err
	}
	s.emitReport(nil, []string{month + "/" + kind + "/" + id})
	data := toGroupData(ws, d, lead)
	data.Warning = warning
	return data, nil
}

// SaveMonth 保存月报人工修改。
func (s *ReportService) SaveMonth(month, level, body string) (GroupData, error) {
	return s.changeMonth(month, level, func(ws *store.Workspace, kind, id string) (*store.Draft, error) {
		return ws.EditDraftKind(kind, month, id, body, s.clock())
	})
}

// FinalizeMonth 月报定稿。
func (s *ReportService) FinalizeMonth(month, level string) (GroupData, error) {
	return s.changeMonth(month, level, func(ws *store.Workspace, kind, id string) (*store.Draft, error) {
		return ws.FinalizeDraftKind(kind, month, id, s.clock())
	})
}

// UnfinalizeMonth 月报取消定稿。
func (s *ReportService) UnfinalizeMonth(month, level string) (GroupData, error) {
	return s.changeMonth(month, level, func(ws *store.Workspace, kind, id string) (*store.Draft, error) {
		return ws.UnfinalizeDraftKind(kind, month, id)
	})
}

func (s *ReportService) changeMonth(month, level string, fn func(*store.Workspace, string, string) (*store.Draft, error)) (GroupData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return GroupData{}, err
	}
	kind, id, lead, err := monthTarget(ws, month, level)
	if err != nil {
		return GroupData{}, err
	}
	d, err := fn(ws, kind, id)
	if err != nil {
		return GroupData{}, err
	}
	s.emitReport(nil, []string{month + "/" + kind + "/" + id})
	return toGroupData(ws, d, lead), nil
}
