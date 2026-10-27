package main

import (
	"context"
	"errors"
	"time"

	"github.com/axu4/kuibu/internal/core"
	"github.com/axu4/kuibu/internal/llm"
	"github.com/axu4/kuibu/internal/store"
)

// GroupData 是组长本周的组报。Exists 为 false 表示还没生成。
type GroupData struct {
	Exists        bool     `json:"exists"`
	LeadID        string   `json:"leadId"`
	LeadName      string   `json:"leadName"`
	Group         string   `json:"group"`
	Week          string   `json:"week"`
	Status        string   `json:"status"`
	HumanEdited   bool     `json:"humanEdited"`
	SourceChanged bool     `json:"sourceChanged"`
	Missing       []string `json:"missing"`
	Model         string   `json:"model"`
	GeneratedAt   string   `json:"generatedAt"`
	FinalizedAt   string   `json:"finalizedAt"`
	Body          string   `json:"body"`
	// Submitted 表示 SE tab 里组长本人名下已有由组报带入的周报。
	Submitted bool `json:"submitted"`
	// Warning 是本次生成的提示（模型输出不符合结构时）。
	Warning string `json:"warning"`
}

// GroupPlanData 是组报外发预览。
type GroupPlanData struct {
	Preview llm.Preview `json:"preview"`
	Missing []string    `json:"missing"`
}

// TemplateData 是一份报告模板。
type TemplateData struct {
	Name      string `json:"name"`
	Body      string `json:"body"`
	IsDefault bool   `json:"isDefault"`
}

// GroupReport 返回组长本周的组报（group 为空时取唯一有组长的组）。
func (s *ReportService) GroupReport(week, group string) (GroupData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return GroupData{}, err
	}
	lead, groupName, err := core.ResolveLead(ws, week, group)
	if err != nil {
		return GroupData{}, err
	}
	d, err := ws.ReadGroupReport(week, lead.ID)
	if errors.Is(err, store.ErrNotFound) {
		return GroupData{LeadID: lead.ID, LeadName: lead.Name, Group: groupName, Week: week, Status: "none", Missing: []string{}}, nil
	}
	if err != nil {
		return GroupData{}, err
	}
	return toGroupData(ws, d, lead), nil
}

// PrepareGroup 生成组报外发预览，不发请求。有组员未定稿且 allowMissing 为 false 时返回 NOT_FINAL。
func (s *ReportService) PrepareGroup(week, group string, allowMissing, force bool) (GroupPlanData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return GroupPlanData{}, err
	}
	plan, err := core.PlanGroupReport(ws, week, group, "group", allowMissing)
	if err != nil {
		return GroupPlanData{}, err
	}
	if err := ws.CheckRegenerateKind(store.GroupDirName, week, plan.LeadID, force); err != nil {
		return GroupPlanData{}, err
	}
	out := &llm.Outbound{Env: s.llmEnv()}
	preview, _, err := out.Prepare("", core.GroupRequest(plan))
	if err != nil {
		return GroupPlanData{}, err
	}
	return GroupPlanData{Preview: preview, Missing: nonNilStrings(plan.Missing)}, nil
}

// GenerateGroup 发送并保存组报草稿。只能由外发确认弹窗的「确认发送」调用。
func (s *ReportService) GenerateGroup(week, group string, allowMissing, force bool) (GroupData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return GroupData{}, err
	}
	plan, err := core.PlanGroupReport(ws, week, group, "group", allowMissing)
	if err != nil {
		return GroupData{}, err
	}
	if err := ws.CheckRegenerateKind(store.GroupDirName, week, plan.LeadID, force); err != nil {
		return GroupData{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	out := &llm.Outbound{Env: s.llmEnv()}
	preview, resp, err := out.Send(ctx, "", core.GroupRequest(plan), true)
	if err != nil {
		return GroupData{}, err
	}
	d, warning, err := core.SaveGroupReport(ws, plan, preview.ModelName, resp.Text, force, s.clock())
	if err != nil {
		return GroupData{}, err
	}
	s.emitReport(nil, []string{week + "/" + store.GroupDirName + "/" + plan.LeadID})
	lead, _, _ := core.ResolveLead(ws, week, group)
	data := toGroupData(ws, d, lead)
	data.Warning = warning
	return data, nil
}

// SaveGroup 保存组报人工修改。
func (s *ReportService) SaveGroup(week, group, body string) (GroupData, error) {
	return s.changeGroup(week, group, func(ws *store.Workspace, leadID string) (*store.Draft, error) {
		return ws.EditDraftKind(store.GroupDirName, week, leadID, body, s.clock())
	})
}

// FinalizeGroup 组报定稿。
func (s *ReportService) FinalizeGroup(week, group string) (GroupData, error) {
	return s.changeGroup(week, group, func(ws *store.Workspace, leadID string) (*store.Draft, error) {
		return ws.FinalizeDraftKind(store.GroupDirName, week, leadID, s.clock())
	})
}

// UnfinalizeGroup 组报取消定稿。
func (s *ReportService) UnfinalizeGroup(week, group string) (GroupData, error) {
	return s.changeGroup(week, group, func(ws *store.Workspace, leadID string) (*store.Draft, error) {
		return ws.UnfinalizeDraftKind(store.GroupDirName, week, leadID)
	})
}

// SubmitGroup 把定稿组报带入 SE tab（组长本人 lead 层级）。未定稿返回 NOT_FINAL。
func (s *ReportService) SubmitGroup(week, group string) (ReportData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return ReportData{}, err
	}
	lead, _, err := core.ResolveLead(ws, week, group)
	if err != nil {
		return ReportData{}, err
	}
	r, err := core.SubmitGroupReport(ws, week, lead.ID, s.clock())
	if err != nil {
		return ReportData{}, err
	}
	s.emitReport(nil, []string{week + "/" + store.ReportLevelLead + "/" + lead.ID})
	return toReportData(r), nil
}

// Template 读取报告模板（group 不存在时返回内置默认）。
func (s *ReportService) Template(name string) (TemplateData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return TemplateData{}, err
	}
	body, isDefault, err := ws.ReadTemplate(name)
	if err != nil {
		return TemplateData{}, err
	}
	return TemplateData{Name: name, Body: body, IsDefault: isDefault}, nil
}

// SaveTemplate 保存报告模板。
func (s *ReportService) SaveTemplate(name, body string) (TemplateData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return TemplateData{}, err
	}
	if len(core.ParseTemplate(body)) == 0 {
		return TemplateData{}, errors.New("模板至少要有一个「## 栏目」")
	}
	if err := ws.WriteTemplate(name, body); err != nil {
		return TemplateData{}, err
	}
	return TemplateData{Name: name, Body: body}, nil
}

// changeGroup 解析组长后执行写操作并通知前端。
func (s *ReportService) changeGroup(week, group string, fn func(*store.Workspace, string) (*store.Draft, error)) (GroupData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return GroupData{}, err
	}
	lead, _, err := core.ResolveLead(ws, week, group)
	if err != nil {
		return GroupData{}, err
	}
	d, err := fn(ws, lead.ID)
	if err != nil {
		return GroupData{}, err
	}
	s.emitReport(nil, []string{week + "/" + store.GroupDirName + "/" + lead.ID})
	return toGroupData(ws, d, lead), nil
}

// toGroupData 把组报转为前端结构。
func toGroupData(ws *store.Workspace, d *store.Draft, lead *store.Person) GroupData {
	out := GroupData{
		Exists:        true,
		LeadID:        d.Person,
		LeadName:      d.Name,
		Group:         d.Group,
		Week:          d.Week,
		Status:        d.Status,
		HumanEdited:   d.HumanEdited,
		SourceChanged: core.GroupSourceChanged(ws, d),
		Missing:       nonNilStrings(d.Missing),
		Model:         d.Model,
		Body:          d.Body,
	}
	if lead != nil {
		out.LeadName = lead.Name
	}
	if !d.GeneratedAt.IsZero() {
		out.GeneratedAt = formatTime(d.GeneratedAt)
	}
	if !d.FinalizedAt.IsZero() {
		out.FinalizedAt = formatTime(d.FinalizedAt)
	}
	if d.Kind == store.ProductDirName {
		out.LeadName = "产品报"
	} else if r, err := ws.ReadReport(d.Week, store.ReportLevelLead, d.Person); err == nil && r.Source == store.ReportSourceGroup {
		out.Submitted = true
	}
	return out
}

// nonNilStrings 保证切片序列化为 [] 而不是 null。
func nonNilStrings(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

// ProductReport 返回本周 SE 产品报；还没生成时 Exists 为 false。
func (s *ReportService) ProductReport(week string) (GroupData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return GroupData{}, err
	}
	d, err := ws.ReadDraftKind(store.ProductDirName, week, store.ProductReportID)
	if errors.Is(err, store.ErrNotFound) {
		return GroupData{Week: week, Status: "none", LeadName: "产品报", Missing: []string{}}, nil
	}
	if err != nil {
		return GroupData{}, err
	}
	return toGroupData(ws, d, nil), nil
}

// PrepareProduct 生成产品报外发预览，不发请求。有组长未定稿且 allowMissing 为 false 时返回 NOT_FINAL。
func (s *ReportService) PrepareProduct(week string, allowMissing, force bool) (GroupPlanData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return GroupPlanData{}, err
	}
	plan, err := core.PlanProduct(ws, week, "product", allowMissing)
	if err != nil {
		return GroupPlanData{}, err
	}
	if err := ws.CheckRegenerateKind(store.ProductDirName, week, store.ProductReportID, force); err != nil {
		return GroupPlanData{}, err
	}
	out := &llm.Outbound{Env: s.llmEnv()}
	preview, _, err := out.Prepare("", core.GroupRequest(plan))
	if err != nil {
		return GroupPlanData{}, err
	}
	return GroupPlanData{Preview: preview, Missing: nonNilStrings(plan.Missing)}, nil
}

// GenerateProduct 发送并保存产品报草稿。只能由外发确认弹窗的「确认发送」调用。
func (s *ReportService) GenerateProduct(week string, allowMissing, force bool) (GroupData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return GroupData{}, err
	}
	plan, err := core.PlanProduct(ws, week, "product", allowMissing)
	if err != nil {
		return GroupData{}, err
	}
	if err := ws.CheckRegenerateKind(store.ProductDirName, week, store.ProductReportID, force); err != nil {
		return GroupData{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	out := &llm.Outbound{Env: s.llmEnv()}
	preview, resp, err := out.Send(ctx, "", core.GroupRequest(plan), true)
	if err != nil {
		return GroupData{}, err
	}
	d, warning, err := core.SaveProductReport(ws, plan, preview.ModelName, resp.Text, force, s.clock())
	if err != nil {
		return GroupData{}, err
	}
	s.emitReport(nil, []string{week + "/" + store.ProductDirName + "/" + store.ProductReportID})
	data := toGroupData(ws, d, nil)
	data.Warning = warning
	return data, nil
}

// SaveProduct 保存产品报人工修改。
func (s *ReportService) SaveProduct(week, body string) (GroupData, error) {
	return s.changeProduct(week, func(ws *store.Workspace) (*store.Draft, error) {
		return ws.EditDraftKind(store.ProductDirName, week, store.ProductReportID, body, s.clock())
	})
}

// FinalizeProduct 产品报定稿。
func (s *ReportService) FinalizeProduct(week string) (GroupData, error) {
	return s.changeProduct(week, func(ws *store.Workspace) (*store.Draft, error) {
		return ws.FinalizeDraftKind(store.ProductDirName, week, store.ProductReportID, s.clock())
	})
}

// UnfinalizeProduct 产品报取消定稿。
func (s *ReportService) UnfinalizeProduct(week string) (GroupData, error) {
	return s.changeProduct(week, func(ws *store.Workspace) (*store.Draft, error) {
		return ws.UnfinalizeDraftKind(store.ProductDirName, week, store.ProductReportID)
	})
}

func (s *ReportService) changeProduct(week string, fn func(*store.Workspace) (*store.Draft, error)) (GroupData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return GroupData{}, err
	}
	d, err := fn(ws)
	if err != nil {
		return GroupData{}, err
	}
	s.emitReport(nil, []string{week + "/" + store.ProductDirName + "/" + store.ProductReportID})
	return toGroupData(ws, d, nil), nil
}
