package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/axu4/kuibu/internal/llm"
	"github.com/axu4/kuibu/internal/store"
)

// allProductLines 返回名单里所有在职人员的产品线（去重排序）。
func allProductLines(ws *store.Workspace) ([]string, error) {
	people, err := ws.ListPersons()
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, p := range people {
		if !p.Active {
			continue
		}
		for _, l := range p.ProductLines {
			if l = strings.TrimSpace(l); l != "" {
				set[l] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for l := range set {
		out = append(out, l)
	}
	sort.Strings(out)
	return out, nil
}

// LeadNormalizeJob 是一份待归一的组长周报。结构同组报（产品线 × 类别），由代码渲染。
type LeadNormalizeJob struct {
	Person string
	Name   string
	Report *store.Report
	Plan   *GroupPlan
}

// PlanLeadNormalize 列出要归一的组长周报。persons 为空表示本周已交的全部组长；
// 经组报带入（source=group-report）的本身就是定稿，跳过。
func PlanLeadNormalize(ws *store.Workspace, week string, persons []string, force bool) ([]LeadNormalizeJob, []NormalizeSkip, error) {
	var reports []*store.Report
	if len(persons) == 0 {
		list, err := ws.ListReports(week, store.ReportLevelLead)
		if err != nil {
			return nil, nil, err
		}
		reports = list
	} else {
		for _, p := range persons {
			id, err := ws.ResolveReportPerson(week, store.ReportLevelLead, p)
			if err != nil {
				return nil, nil, err
			}
			r, err := ws.ReadReport(week, store.ReportLevelLead, id)
			if err != nil {
				return nil, nil, err
			}
			reports = append(reports, r)
		}
	}
	tpl, _, err := ws.ReadTemplate("group")
	if err != nil {
		return nil, nil, err
	}
	sections := ParseTemplate(tpl)
	lines, err := allProductLines(ws)
	if err != nil {
		return nil, nil, err
	}
	var (
		jobs    []LeadNormalizeJob
		skipped []NormalizeSkip
	)
	for _, r := range reports {
		if r.Week == "" {
			r.Week = week
		}
		name := r.Name
		if name == "" {
			name = r.Person
		}
		if r.Source == store.ReportSourceGroup {
			if len(persons) == 1 {
				return nil, nil, fmt.Errorf("%w: %s 的周报由定稿组报带入，本身就是定稿，无需归一", store.ErrInvalid, name)
			}
			skipped = append(skipped, NormalizeSkip{Person: r.Person, Name: name, Reason: "由定稿组报带入，无需归一"})
			continue
		}
		if err := ws.CheckRegenerateKind(store.LeadDraftDirName, week, r.Person, force); err != nil {
			if len(persons) == 1 {
				return nil, nil, err
			}
			skipped = append(skipped, NormalizeSkip{Person: r.Person, Name: name, Reason: err.Error()})
			continue
		}
		own := r.ProductLines
		if len(own) == 0 {
			own = lines
		}
		plan := &GroupPlan{
			Week: week, Group: r.Group, LeadID: r.Person, LeadName: name,
			Template: "group", Sections: sections, ProductLines: own, SourceNoun: "组员",
			Included: []GroupMember{{Person: r.Person, Name: name, ProductLines: own, Draft: &store.Draft{Body: r.Body}}},
		}
		jobs = append(jobs, LeadNormalizeJob{Person: r.Person, Name: name, Report: r, Plan: plan})
	}
	return jobs, skipped, nil
}

// SaveLeadNormalized 渲染模型输出并保存为组长归一稿草稿。
func SaveLeadNormalized(ws *store.Workspace, job LeadNormalizeJob, model, text string, force bool, now time.Time) (*store.Draft, string, error) {
	body, ok := RenderGroupReport(job.Plan, text)
	warning := ""
	if !ok {
		warning = "模型输出不是预期的 JSON，已整段保存为草稿，请人工整理"
	}
	d, err := ws.SaveGeneratedDraft(store.GeneratedDraft{
		Kind:       store.LeadDraftDirName,
		Person:     job.Person,
		Name:       job.Name,
		Week:       job.Report.Week,
		SourceHash: store.SourceHash(job.Report.Body),
		Model:      model,
		Body:       body,
		Now:        now,
	}, force)
	return d, warning, err
}

// leadFinalBody 返回组长本周的定稿输入：组长归一稿定稿，或经组报带入的周报。
func leadFinalBody(ws *store.Workspace, week, leadID string) (string, error) {
	if r, err := ws.ReadReport(week, store.ReportLevelLead, leadID); err == nil && r.Source == store.ReportSourceGroup {
		return r.Body, nil
	}
	d, err := ws.FinalDraftKind(store.LeadDraftDirName, week, leadID)
	if err != nil {
		return "", err
	}
	return d.Body, nil
}

// PlanProduct 收集本周各组长的定稿，生成产品报计划（产品线 → 栏目 → 类别）。
// 有组长未定稿时，allowMissing 为 false 返回 ErrNotFinal 并列出名单。
func PlanProduct(ws *store.Workspace, week, template string, allowMissing bool) (*GroupPlan, error) {
	if template == "" {
		template = "product"
	}
	tpl, _, err := ws.ReadTemplate(template)
	if err != nil {
		return nil, err
	}
	sections := ParseTemplate(tpl)
	if len(sections) == 0 {
		return nil, fmt.Errorf("%w: 模板 %s 没有任何「## 栏目」", store.ErrInvalid, template)
	}
	lines, err := allProductLines(ws)
	if err != nil {
		return nil, err
	}
	people, err := ws.ListPersons()
	if err != nil {
		return nil, err
	}
	monday, _, err := store.WeekRange(week)
	if err != nil {
		return nil, err
	}
	plan := &GroupPlan{
		Week: week, LeadID: store.ProductReportID, LeadName: "产品报", Template: template,
		Sections: sections, ProductLines: lines, ProductFirst: true, SourceNoun: "组",
	}
	for _, p := range people {
		if !p.Active || p.Role != store.RoleLead {
			continue
		}
		group, products := p.MembershipAt(monday)
		if group == "" {
			group, products = p.Group, p.ProductLines
		}
		label := group
		if label == "" {
			label = p.Name
		}
		body, err := leadFinalBody(ws, week, p.ID)
		if err != nil {
			if errors.Is(err, store.ErrNotFinal) || errors.Is(err, store.ErrNotFound) {
				plan.Missing = append(plan.Missing, label+"（"+p.Name+"）")
				continue
			}
			return nil, err
		}
		plan.Included = append(plan.Included, GroupMember{Person: p.ID, Name: label, ProductLines: products, Draft: &store.Draft{Body: body}})
	}
	sort.Strings(plan.Missing)
	sort.Slice(plan.Included, func(i, j int) bool { return plan.Included[i].Name < plan.Included[j].Name })
	if len(plan.Included) == 0 {
		return nil, fmt.Errorf("%w: %s 还没有任何组长的定稿周报", store.ErrNotFinal, week)
	}
	if len(plan.Missing) > 0 && !allowMissing {
		return nil, fmt.Errorf("%w: 以下组长的周报未定稿：%s（确认先不纳入请加 --allow-missing / 点「仍然生成」）",
			store.ErrNotFinal, strings.Join(plan.Missing, "、"))
	}
	return plan, nil
}

// SaveProductReport 渲染模型输出并保存为产品报草稿。
func SaveProductReport(ws *store.Workspace, p *GroupPlan, model, text string, force bool, now time.Time) (*store.Draft, string, error) {
	body, ok := RenderGroupReport(p, text)
	warning := ""
	if !ok {
		warning = "模型输出不是预期的 JSON，已整段保存为草稿，请人工整理"
	}
	d, err := ws.SaveGeneratedDraft(store.GeneratedDraft{
		Kind:       store.ProductDirName,
		Sources:    p.SourceIDs(),
		Missing:    p.Missing,
		Person:     store.ProductReportID,
		Name:       "产品报",
		Week:       p.Week,
		SourceHash: p.SourceHash(),
		Model:      model,
		Body:       body,
		Now:        now,
	}, force)
	return d, warning, err
}

// ProductChapter 从产品报里取出「## 产品线」那一章；找不到时返回 ErrNotFound。
func ProductChapter(body, line string) (string, error) {
	lines := strings.Split(body, "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "## "+line {
			start = i
			break
		}
	}
	if start < 0 {
		return "", fmt.Errorf("%w: 产品报里没有「%s」这一章", store.ErrNotFound, line)
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	return strings.TrimRight(strings.Join(lines[start:end], "\n"), "\n"), nil
}

// sourcesHash 按来源顺序重算合并哈希；任何来源取不到定稿都视为已变化。
func sourcesHash(ids []string, body func(id string) (string, error)) (string, bool) {
	h := sha256.New()
	for _, id := range ids {
		b, err := body(id)
		if err != nil {
			return "", false
		}
		h.Write([]byte(id + "\n" + b + "\n\x00"))
	}
	return hex.EncodeToString(h.Sum(nil)), true
}

// LeadCombinedPreviewText 把多份组长归一请求拼成一段给人确认的文本。
func LeadCombinedPreviewText(jobs []LeadNormalizeJob) string {
	var b strings.Builder
	for i, job := range jobs {
		if i > 0 {
			b.WriteString("\n\n========================================\n\n")
		}
		b.WriteString("〔" + job.Name + "〕\n")
		b.WriteString(llm.RenderRequest(GroupRequest(job.Plan)))
	}
	return b.String()
}

// RunLeadNormalize 逐个发送并保存组长归一稿。confirmed 必须来自人工确认。
func RunLeadNormalize(ctx context.Context, ws *store.Workspace, out *llm.Outbound, model string, jobs []LeadNormalizeJob, force, confirmed bool, now time.Time) ([]NormalizeResult, error) {
	if !confirmed {
		return nil, llm.ErrNeedsConfirm
	}
	results := make([]NormalizeResult, 0, len(jobs))
	for _, job := range jobs {
		res := NormalizeResult{Person: job.Person, Name: job.Name}
		preview, resp, err := out.Send(ctx, model, GroupRequest(job.Plan), true)
		if err != nil {
			if ctx.Err() != nil {
				return results, err
			}
			res.Error = err.Error()
			results = append(results, res)
			continue
		}
		d, warning, err := SaveLeadNormalized(ws, job, preview.ModelName, resp.Text, force, now)
		if err != nil {
			res.Error = err.Error()
		}
		res.Draft, res.Warning = d, warning
		results = append(results, res)
	}
	return results, nil
}

// NormalizeBatch 是一批待归一的原文（组员或组长），CLI 与 App 共用。
type NormalizeBatch struct {
	Level   string
	Persons []string
	// Text 是给人确认的完整外发内容，与实际逐人发送的请求一致。
	Text   string
	member []NormalizeJob
	lead   []LeadNormalizeJob
}

// PlanNormalizeLevel 按层级列出要归一的原文：member 用组员归一结构，lead 用组报结构。
func PlanNormalizeLevel(ws *store.Workspace, week, level string, persons []string, force bool) (*NormalizeBatch, []NormalizeSkip, error) {
	b := &NormalizeBatch{Level: level, Persons: []string{}}
	if level == store.ReportLevelLead {
		jobs, skipped, err := PlanLeadNormalize(ws, week, persons, force)
		if err != nil {
			return nil, nil, err
		}
		b.lead = jobs
		for _, j := range jobs {
			b.Persons = append(b.Persons, j.Person)
		}
		b.Text = LeadCombinedPreviewText(jobs)
		return b, skipped, nil
	}
	jobs, skipped, err := PlanNormalize(ws, week, persons, force)
	if err != nil {
		return nil, nil, err
	}
	b.member = jobs
	for _, j := range jobs {
		b.Persons = append(b.Persons, j.Person)
	}
	b.Text = CombinedPreviewText(jobs)
	return b, skipped, nil
}

// Run 逐个发送并保存草稿；confirmed 必须来自人工确认。
func (b *NormalizeBatch) Run(ctx context.Context, ws *store.Workspace, out *llm.Outbound, model string, force, confirmed bool, now time.Time) ([]NormalizeResult, error) {
	if b.Level == store.ReportLevelLead {
		return RunLeadNormalize(ctx, ws, out, model, b.lead, force, confirmed, now)
	}
	return RunNormalize(ctx, ws, out, model, b.member, force, confirmed, now)
}
