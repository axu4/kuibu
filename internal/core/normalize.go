package core

import (
	"context"
	_ "embed"
	"strings"
	"time"

	"github.com/axu4/kuibu/internal/llm"
	"github.com/axu4/kuibu/internal/store"
)

//go:embed prompts/normalize.md
var normalizePrompt string

// NormalizeInput 是归一一份组员周报所需的信息。
type NormalizeInput struct {
	Name    string
	Week    string
	Domains []string
	Body    string
}

// NormalizeRequest 构造发给大模型的归一请求：系统提示是固定模板，正文是周报原文。
func NormalizeRequest(in NormalizeInput) llm.Request {
	domains := "（无）"
	if len(in.Domains) > 0 {
		domains = "【" + strings.Join(in.Domains, "】【") + "】"
	}
	system := strings.NewReplacer(
		"{{DOMAINS}}", domains,
		"{{NAME}}", in.Name,
		"{{WEEK}}", in.Week,
	).Replace(strings.TrimSpace(normalizePrompt))
	return llm.Request{
		System:   system,
		Messages: []llm.Message{{Role: "user", Content: in.Body}},
	}
}

// NormalizeInputFor 由周报原文构造归一输入，领域候选取周报快照里的产品线。
func NormalizeInputFor(r *store.Report) NormalizeInput {
	name := r.Name
	if name == "" {
		name = r.Person
	}
	return NormalizeInput{Name: name, Week: r.Week, Domains: r.ProductLines, Body: r.Body}
}

// ParseNormalized 整理模型输出：去掉代码围栏与首尾空白。缺少「本周完成」或
// 「下周计划」标题时 ok 为 false，正文仍原样返回，绝不丢内容。
func ParseNormalized(text string) (body string, ok bool) {
	body = strings.TrimSpace(text)
	if strings.HasPrefix(body, "```") {
		if i := strings.IndexByte(body, '\n'); i >= 0 {
			body = body[i+1:]
		}
		body = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(body), "```"))
	}
	ok = hasHeading(body, "本周完成") && hasHeading(body, "下周计划")
	return body, ok
}

// hasHeading 判断是否有以 title 开头的 Markdown 标题行。
func hasHeading(body, title string) bool {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") && strings.Contains(strings.TrimLeft(line, "# "), title) {
			return true
		}
	}
	return false
}

// NormalizeJob 是一份待归一的组员周报。
type NormalizeJob struct {
	Person  string
	Name    string
	Report  *store.Report
	Request llm.Request
}

// NormalizeSkip 是因存在人工修改或已定稿而跳过的人。
type NormalizeSkip struct {
	Person string `json:"person"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// PlanNormalize 列出某周要归一的组员周报。persons 为空表示本周已交的全部组员；
// 归一稿已人工修改或已定稿的人，不带 force 时放进 skipped（单人时直接返回错误）。
func PlanNormalize(ws *store.Workspace, week string, persons []string, force bool) ([]NormalizeJob, []NormalizeSkip, error) {
	var reports []*store.Report
	if len(persons) == 0 {
		list, err := ws.ListReports(week, store.ReportLevelMember)
		if err != nil {
			return nil, nil, err
		}
		reports = list
	} else {
		for _, p := range persons {
			id, err := ws.ResolveReportPerson(week, store.ReportLevelMember, p)
			if err != nil {
				return nil, nil, err
			}
			r, err := ws.ReadReport(week, store.ReportLevelMember, id)
			if err != nil {
				return nil, nil, err
			}
			reports = append(reports, r)
		}
	}
	var (
		jobs    []NormalizeJob
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
		if err := ws.CheckRegenerate(week, r.Person, force); err != nil {
			if len(persons) == 1 {
				return nil, nil, err
			}
			skipped = append(skipped, NormalizeSkip{Person: r.Person, Name: name, Reason: err.Error()})
			continue
		}
		jobs = append(jobs, NormalizeJob{Person: r.Person, Name: name, Report: r, Request: NormalizeRequest(NormalizeInputFor(r))})
	}
	return jobs, skipped, nil
}

// CombinedPreviewText 把多份请求拼成一段给人确认的文本，批量归一时一次确认。
func CombinedPreviewText(jobs []NormalizeJob) string {
	var b strings.Builder
	for i, job := range jobs {
		if i > 0 {
			b.WriteString("\n\n========================================\n\n")
		}
		b.WriteString("〔" + job.Name + "〕\n")
		b.WriteString(llm.RenderRequest(job.Request))
	}
	return b.String()
}

// NormalizeResult 是一个人的归一结果。
type NormalizeResult struct {
	Person string       `json:"person"`
	Name   string       `json:"name"`
	Draft  *store.Draft `json:"-"`
	// Warning 非空表示模型输出不符合结构，已整段保存为草稿正文。
	Warning string `json:"warning,omitempty"`
	// Error 非空表示这个人生成失败。
	Error string `json:"error,omitempty"`
}

// RunNormalize 逐个发送并保存草稿。confirmed 必须来自人工确认；未确认时返回
// llm.ErrNeedsConfirm 且不发任何请求。单人失败不影响其他人，失败写在结果里。
func RunNormalize(ctx context.Context, ws *store.Workspace, out *llm.Outbound, model string, jobs []NormalizeJob, force, confirmed bool, now time.Time) ([]NormalizeResult, error) {
	if !confirmed {
		return nil, llm.ErrNeedsConfirm
	}
	results := make([]NormalizeResult, 0, len(jobs))
	for _, job := range jobs {
		res := NormalizeResult{Person: job.Person, Name: job.Name}
		preview, resp, err := out.Send(ctx, model, job.Request, true)
		if err != nil {
			if ctx.Err() != nil {
				return results, err
			}
			res.Error = err.Error()
			results = append(results, res)
			continue
		}
		body, ok := ParseNormalized(resp.Text)
		if !ok {
			res.Warning = "模型输出缺少「本周完成 / 下周计划」标题，已整段保存为草稿，请人工整理"
		}
		draft, err := ws.SaveGeneratedDraft(store.GeneratedDraft{
			Person:     job.Person,
			Name:       job.Name,
			Week:       job.Report.Week,
			SourceHash: store.SourceHash(job.Report.Body),
			Model:      preview.ModelName,
			Body:       body,
			Now:        now,
		}, force)
		if err != nil {
			res.Error = err.Error()
		}
		res.Draft = draft
		results = append(results, res)
	}
	return results, nil
}
