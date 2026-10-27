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

// GetDraft 返回某人某周的归一稿（level 为 member / lead）；还没生成时 Exists 为 false。
func (s *ReportService) GetDraft(week, person, level string) (DraftData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return DraftData{}, err
	}
	level = levelOrMember(level)
	personID, err := ws.ResolveReportPerson(week, level, person)
	if err != nil {
		return DraftData{}, err
	}
	d, err := ws.ReadDraftKind(store.NormalizedKind(level), week, personID)
	if errors.Is(err, store.ErrNotFound) {
		return DraftData{Person: personID, Week: week, Status: "none"}, nil
	}
	if err != nil {
		return DraftData{}, err
	}
	return toDraftData(ws, d, level), nil
}

// levelOrMember 空层级视为组员。
func levelOrMember(level string) string {
	if level == "" {
		return store.ReportLevelMember
	}
	return level
}

// PrepareNormalize 生成归一的外发预览，不发任何请求。persons 为空表示本周已交的全部人员。
func (s *ReportService) PrepareNormalize(week, level string, persons []string, model string, force bool) (NormalizePlan, error) {
	ws, _, err := s.instances()
	if err != nil {
		return NormalizePlan{}, err
	}
	batch, skipped, err := core.PlanNormalizeLevel(ws, week, levelOrMember(level), persons, force)
	if err != nil {
		return NormalizePlan{}, err
	}
	if skipped == nil {
		skipped = []core.NormalizeSkip{}
	}
	plan := NormalizePlan{Persons: batch.Persons, Skipped: skipped}
	if len(batch.Persons) == 0 {
		return plan, nil
	}
	out := &llm.Outbound{Env: s.llmEnv()}
	preview, _, err := out.Prepare(model, llm.Request{})
	if err != nil {
		return NormalizePlan{}, err
	}
	preview.Text = batch.Text
	preview.Chars = len([]rune(preview.Text))
	plan.Preview = preview
	return plan, nil
}

// Normalize 发送并保存草稿。只能由外发确认弹窗的「确认发送」调用：persons 必须是
// PrepareNormalize 预览过的那批人，内容与预览一致。
func (s *ReportService) Normalize(week, level string, persons []string, model string, force bool) ([]NormalizeResultData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return nil, err
	}
	if len(persons) == 0 {
		return []NormalizeResultData{}, nil
	}
	level = levelOrMember(level)
	batch, _, err := core.PlanNormalizeLevel(ws, week, level, persons, force)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	out := &llm.Outbound{Env: s.llmEnv()}
	results, err := batch.Run(ctx, ws, out, model, force, true, s.clock())
	if err != nil {
		return nil, err
	}
	data := make([]NormalizeResultData, 0, len(results))
	ids := make([]string, 0, len(results))
	for _, r := range results {
		data = append(data, NormalizeResultData{Person: r.Person, Name: r.Name, Warning: r.Warning, Error: r.Error})
		ids = append(ids, week+"/"+store.NormalizedKind(level)+"/"+r.Person)
	}
	s.emitReport(nil, ids)
	return data, nil
}

// SaveDraft 保存人工修改。已定稿时报错，需先取消定稿。
func (s *ReportService) SaveDraft(week, person, level, body string) (DraftData, error) {
	return s.changeDraft(week, person, level, func(ws *store.Workspace, kind, id string) (*store.Draft, error) {
		return ws.EditDraftKind(kind, week, id, body, s.clock())
	})
}

// FinalizeDraft 定稿。
func (s *ReportService) FinalizeDraft(week, person, level string) (DraftData, error) {
	return s.changeDraft(week, person, level, func(ws *store.Workspace, kind, id string) (*store.Draft, error) {
		return ws.FinalizeDraftKind(kind, week, id, s.clock())
	})
}

// UnfinalizeDraft 取消定稿。
func (s *ReportService) UnfinalizeDraft(week, person, level string) (DraftData, error) {
	return s.changeDraft(week, person, level, func(ws *store.Workspace, kind, id string) (*store.Draft, error) {
		return ws.UnfinalizeDraftKind(kind, week, id)
	})
}

// changeDraft 解析人员后执行写操作并通知前端。
func (s *ReportService) changeDraft(week, person, level string, fn func(*store.Workspace, string, string) (*store.Draft, error)) (DraftData, error) {
	ws, _, err := s.instances()
	if err != nil {
		return DraftData{}, err
	}
	level = levelOrMember(level)
	kind := store.NormalizedKind(level)
	personID, err := ws.ResolveReportPerson(week, level, person)
	if err != nil {
		// 原文已删除时仍允许按 ID 操作归一稿。
		personID = strings.TrimSpace(person)
	}
	d, err := fn(ws, kind, personID)
	if err != nil {
		return DraftData{}, err
	}
	s.emitReport(nil, []string{week + "/" + kind + "/" + personID})
	return toDraftData(ws, d, level), nil
}

// toDraftData 把归一稿转为前端结构。
func toDraftData(ws *store.Workspace, d *store.Draft, level string) DraftData {
	report, _ := ws.ReadReport(d.Week, level, d.Person)
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

// CarryResult 是一键顺延的结果。
type CarryResult struct {
	Added  int               `json:"added"`
	Review core.ReviewResult `json:"review"`
}

// Review 返回「对照上周计划」。上周没有定稿时返回 NOT_FINAL 错误。
func (s *ReportService) Review(week, person string) (core.ReviewResult, error) {
	ws, _, err := s.instances()
	if err != nil {
		return core.ReviewResult{}, err
	}
	res, err := core.LoadReview(ws, week, s.draftPersonID(ws, week, person))
	if err != nil {
		return core.ReviewResult{}, err
	}
	return *res, nil
}

// SetReview 人工设置一条上周计划的状态与对应项；status 为空表示清除。
func (s *ReportService) SetReview(week, person, planID, status string, matches []string, note string) (core.ReviewResult, error) {
	ws, _, err := s.instances()
	if err != nil {
		return core.ReviewResult{}, err
	}
	id := s.draftPersonID(ws, week, person)
	res, err := core.SetReviewItem(ws, week, id, planID, status, matches, note)
	if err != nil {
		return core.ReviewResult{}, err
	}
	s.emitReport(nil, []string{week + "/" + store.DraftDirName + "/" + id})
	return *res, nil
}

// PrepareReviewSuggest 生成 AI 对照建议的外发预览，不发请求。
func (s *ReportService) PrepareReviewSuggest(week, person, model string) (llm.Preview, error) {
	ws, _, err := s.instances()
	if err != nil {
		return llm.Preview{}, err
	}
	res, err := core.LoadReview(ws, week, s.draftPersonID(ws, week, person))
	if err != nil {
		return llm.Preview{}, err
	}
	out := &llm.Outbound{Env: s.llmEnv()}
	preview, _, err := out.Prepare(model, core.ReviewSuggestRequest(res))
	return preview, err
}

// ReviewSuggest 发送并写入 AI 建议。只能由外发确认弹窗的「确认发送」调用。
func (s *ReportService) ReviewSuggest(week, person, model string) (core.ReviewResult, error) {
	ws, _, err := s.instances()
	if err != nil {
		return core.ReviewResult{}, err
	}
	id := s.draftPersonID(ws, week, person)
	res, err := core.LoadReview(ws, week, id)
	if err != nil {
		return core.ReviewResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	out := &llm.Outbound{Env: s.llmEnv()}
	_, resp, err := out.Send(ctx, model, core.ReviewSuggestRequest(res), true)
	if err != nil {
		return core.ReviewResult{}, err
	}
	res, _, err = core.ApplySuggestion(ws, week, id, resp.Text)
	if err != nil {
		return core.ReviewResult{}, err
	}
	s.emitReport(nil, []string{week + "/" + store.DraftDirName + "/" + id})
	return *res, nil
}

// Carry 把「部分 / 延期」的计划顺延到本周下周计划；ids 为空表示全部。
func (s *ReportService) Carry(week, person string, ids []string) (CarryResult, error) {
	ws, _, err := s.instances()
	if err != nil {
		return CarryResult{}, err
	}
	id := s.draftPersonID(ws, week, person)
	res, added, err := core.Carry(ws, week, id, ids, s.clock())
	if err != nil {
		return CarryResult{}, err
	}
	s.emitReport(nil, []string{week + "/" + store.DraftDirName + "/" + id})
	return CarryResult{Added: added, Review: *res}, nil
}

// draftPersonID 把姓名 / 别名解析成组员 ID，解析不到时原样返回。
func (s *ReportService) draftPersonID(ws *store.Workspace, week, person string) string {
	if id, err := ws.ResolveReportPerson(week, store.ReportLevelMember, person); err == nil {
		return id
	}
	return strings.TrimSpace(person)
}

// alignmentFor 返回某人本周对齐率；没有可对照的数据时为 nil。
func alignmentFor(ws *store.Workspace, week, personID string) *float64 {
	res, err := core.LoadReview(ws, week, personID)
	if err != nil {
		return nil
	}
	return res.Alignment
}
