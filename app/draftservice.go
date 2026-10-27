package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/axu4/kuibu/internal/core"
	"github.com/axu4/kuibu/internal/llm"
	"github.com/axu4/kuibu/internal/store"
)

// DraftData 是一份组员周报归一稿。Exists 为 false 表示还没有生成。
type DraftData struct {
	Exists        bool   `json:"exists"`
	Person        string `json:"person"`
	Name          string `json:"name"`
	Week          string `json:"week"`
	Status        string `json:"status"`
	SourceChanged bool   `json:"sourceChanged"`
	HumanEdited   bool   `json:"humanEdited"`
	Model         string `json:"model"`
	GeneratedAt   string `json:"generatedAt"`
	EditedAt      string `json:"editedAt"`
	FinalizedAt   string `json:"finalizedAt"`
	Body          string `json:"body"`
}

// NormalizePlan 是批量归一的外发预览：前端用 SendPreview 弹窗展示，用户确认后
// 再调 Normalize。
type NormalizePlan struct {
	Preview llm.Preview          `json:"preview"`
	Persons []string             `json:"persons"`
	Skipped []core.NormalizeSkip `json:"skipped"`
}

// NormalizeResultData 是一个人的归一结果。
type NormalizeResultData struct {
	Person  string `json:"person"`
	Name    string `json:"name"`
	Warning string `json:"warning"`
	Error   string `json:"error"`
}

// llmEnv 返回读取大模型配置用的环境变量函数。
func (s *ReportService) llmEnv() func(string) string {
	if s.env != nil {
		return s.env
	}
	return os.Getenv
}

// GetDraft 返回某人某周的归一稿；还没生成时 Exists 为 false。
func (s *ReportService) GetDraft(week, person string) (DraftData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return DraftData{}, err
	}
	personID, err := ws.ResolveReportPerson(week, store.ReportLevelMember, person)
	if err != nil {
		return DraftData{}, err
	}
	d, err := ws.ReadDraft(week, personID)
	if errors.Is(err, store.ErrNotFound) {
		return DraftData{Person: personID, Week: week, Status: "none"}, nil
	}
	if err != nil {
		return DraftData{}, err
	}
	return toDraftData(ws, d), nil
}

// PrepareNormalize 生成归一的外发预览，不发任何请求。persons 为空表示本周已交的全部组员。
func (s *ReportService) PrepareNormalize(week string, persons []string, model string, force bool) (NormalizePlan, error) {
	ws, _, err := s.instances()
	if err != nil {
		return NormalizePlan{}, err
	}
	jobs, skipped, err := core.PlanNormalize(ws, week, persons, force)
	if err != nil {
		return NormalizePlan{}, err
	}
	if skipped == nil {
		skipped = []core.NormalizeSkip{}
	}
	plan := NormalizePlan{Persons: []string{}, Skipped: skipped}
	if len(jobs) == 0 {
		return plan, nil
	}
	out := &llm.Outbound{Env: s.llmEnv()}
	preview, _, err := out.Prepare(model, llm.Request{})
	if err != nil {
		return NormalizePlan{}, err
	}
	preview.Text = core.CombinedPreviewText(jobs)
	preview.Chars = len([]rune(preview.Text))
	plan.Preview = preview
	for _, j := range jobs {
		plan.Persons = append(plan.Persons, j.Person)
	}
	return plan, nil
}

// Normalize 发送并保存草稿。只能由外发确认弹窗的「确认发送」调用：persons 必须是
// PrepareNormalize 预览过的那批人，内容与预览一致。
func (s *ReportService) Normalize(week string, persons []string, model string, force bool) ([]NormalizeResultData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return nil, err
	}
	if len(persons) == 0 {
		return []NormalizeResultData{}, nil
	}
	jobs, _, err := core.PlanNormalize(ws, week, persons, force)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	out := &llm.Outbound{Env: s.llmEnv()}
	results, err := core.RunNormalize(ctx, ws, out, model, jobs, force, true, s.clock())
	if err != nil {
		return nil, err
	}
	data := make([]NormalizeResultData, 0, len(results))
	ids := make([]string, 0, len(results))
	for _, r := range results {
		data = append(data, NormalizeResultData{Person: r.Person, Name: r.Name, Warning: r.Warning, Error: r.Error})
		ids = append(ids, week+"/"+store.DraftDirName+"/"+r.Person)
	}
	s.emitReport(nil, ids)
	return data, nil
}

// SaveDraft 保存人工修改。已定稿时报错，需先取消定稿。
func (s *ReportService) SaveDraft(week, person, body string) (DraftData, error) {
	return s.changeDraft(week, person, func(ws *store.Workspace, id string) (*store.Draft, error) {
		return ws.EditDraft(week, id, body, s.clock())
	})
}

// FinalizeDraft 定稿。
func (s *ReportService) FinalizeDraft(week, person string) (DraftData, error) {
	return s.changeDraft(week, person, func(ws *store.Workspace, id string) (*store.Draft, error) {
		return ws.FinalizeDraft(week, id, s.clock())
	})
}

// UnfinalizeDraft 取消定稿。
func (s *ReportService) UnfinalizeDraft(week, person string) (DraftData, error) {
	return s.changeDraft(week, person, func(ws *store.Workspace, id string) (*store.Draft, error) {
		return ws.UnfinalizeDraft(week, id)
	})
}

// changeDraft 解析人员后执行写操作并通知前端。
func (s *ReportService) changeDraft(week, person string, fn func(*store.Workspace, string) (*store.Draft, error)) (DraftData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return DraftData{}, err
	}
	personID, err := ws.ResolveReportPerson(week, store.ReportLevelMember, person)
	if err != nil {
		// 原文已删除时仍允许按 ID 操作归一稿。
		personID = strings.TrimSpace(person)
	}
	d, err := fn(ws, personID)
	if err != nil {
		return DraftData{}, err
	}
	s.emitReport(nil, []string{week + "/" + store.DraftDirName + "/" + personID})
	return toDraftData(ws, d), nil
}

// toDraftData 把归一稿转为前端结构。
func toDraftData(ws *store.Workspace, d *store.Draft) DraftData {
	report, _ := ws.ReadReport(d.Week, store.ReportLevelMember, d.Person)
	st := store.DraftStateFor(d, report)
	out := DraftData{
		Exists:        true,
		Person:        d.Person,
		Name:          d.Name,
		Week:          d.Week,
		Status:        d.Status,
		SourceChanged: st.SourceChanged,
		HumanEdited:   d.HumanEdited,
		Model:         d.Model,
		Body:          d.Body,
	}
	if !d.GeneratedAt.IsZero() {
		out.GeneratedAt = formatTime(d.GeneratedAt)
	}
	if !d.EditedAt.IsZero() {
		out.EditedAt = formatTime(d.EditedAt)
	}
	if !d.FinalizedAt.IsZero() {
		out.FinalizedAt = formatTime(d.FinalizedAt)
	}
	return out
}
