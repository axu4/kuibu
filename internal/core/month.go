package core

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/axu4/kuibu/internal/store"
)

// WeeksOfMonth 返回属于某月的 ISO 周：周报日（weekday，1=周一…7=周日）落在该月的周。
// 一周跨月时按周报日所在月份归属，跨年同理。
func WeeksOfMonth(month string, weekday int) ([]string, error) {
	start, err := store.MonthStart(month)
	if err != nil {
		return nil, err
	}
	end := start.AddDate(0, 1, 0)
	var weeks []string
	seen := map[string]bool{}
	for d := start.AddDate(0, 0, -7); d.Before(end.AddDate(0, 0, 7)); d = d.AddDate(0, 0, 1) {
		w := store.WeekOf(d)
		if seen[w] {
			continue
		}
		seen[w] = true
		day, err := store.WeekReportDay(w, weekday)
		if err != nil {
			return nil, err
		}
		if store.MonthOf(day) == month {
			weeks = append(weeks, w)
		}
	}
	sort.Strings(weeks)
	return weeks, nil
}

// MonthOfWeek 返回某周按周报日归属的月份。
func MonthOfWeek(week string, weekday int) (string, error) {
	day, err := store.WeekReportDay(week, weekday)
	if err != nil {
		return "", err
	}
	return store.MonthOf(day), nil
}

// monthWeeks 用工作台配置的周报日列出当月的周。
func monthWeeks(ws *store.Workspace, month string) ([]string, error) {
	weekday, err := ws.ReportWeekday()
	if err != nil {
		return nil, err
	}
	return WeeksOfMonth(month, weekday)
}

// MonthLead 按当月最后一周的名单找组长。
func MonthLead(ws *store.Workspace, month, group string) (*store.Person, string, error) {
	weeks, err := monthWeeks(ws, month)
	if err != nil {
		return nil, "", err
	}
	if len(weeks) == 0 {
		return nil, "", fmt.Errorf("%w: %s 没有任何周", store.ErrInvalid, month)
	}
	return ResolveLead(ws, weeks[len(weeks)-1], group)
}

// productWeekSource 是产品月报里「某周产品报」来源的 ID 前缀。
const productWeekSource = "week:"

// PlanGroupMonth 用当月各周的定稿组报生成组月报计划（结构同组报）。
// 缺周或未定稿时 allowMissing 为 false 返回 ErrNotFinal 并列出。
func PlanGroupMonth(ws *store.Workspace, month, group string, allowMissing bool) (*GroupPlan, error) {
	weeks, err := monthWeeks(ws, month)
	if err != nil {
		return nil, err
	}
	lead, groupName, err := MonthLead(ws, month, group)
	if err != nil {
		return nil, err
	}
	tpl, _, err := ws.ReadTemplate("group")
	if err != nil {
		return nil, err
	}
	lines, err := allProductLines(ws)
	if err != nil {
		return nil, err
	}
	plan := &GroupPlan{
		Week: month, Group: groupName, LeadID: lead.ID, LeadName: lead.Name, Template: "group",
		Sections: ParseTemplate(tpl), ProductLines: lines, SourceNoun: "周",
	}
	for _, w := range weeks {
		d, err := ws.FinalDraftKind(store.GroupDirName, w, lead.ID)
		if err != nil {
			if errors.Is(err, store.ErrNotFinal) || errors.Is(err, store.ErrNotFound) {
				plan.Missing = append(plan.Missing, w)
				continue
			}
			return nil, err
		}
		plan.Included = append(plan.Included, GroupMember{Person: w, Name: w, Draft: d})
	}
	return finishMonthPlan(plan, allowMissing, "周组报")
}

// PlanProductMonth 用组长月报（归一定稿）与当月各周定稿产品报生成产品月报计划。
func PlanProductMonth(ws *store.Workspace, month string, allowMissing bool) (*GroupPlan, error) {
	weeks, err := monthWeeks(ws, month)
	if err != nil {
		return nil, err
	}
	tpl, _, err := ws.ReadTemplate("product")
	if err != nil {
		return nil, err
	}
	lines, err := allProductLines(ws)
	if err != nil {
		return nil, err
	}
	people, err := ws.ListPersons()
	if err != nil {
		return nil, err
	}
	start, err := store.MonthStart(month)
	if err != nil {
		return nil, err
	}
	plan := &GroupPlan{
		Week: month, LeadID: store.ProductReportID, LeadName: "产品月报", Template: "product",
		Sections: ParseTemplate(tpl), ProductLines: lines, ProductFirst: true, SourceNoun: "来源",
	}
	for _, p := range people {
		if !p.Active || p.Role != store.RoleLead {
			continue
		}
		group, _ := p.MembershipAt(start)
		if group == "" {
			group = p.Group
		}
		label := group + "月报"
		d, err := ws.FinalDraftKind(store.LeadMonthDraftDirName, month, p.ID)
		if err != nil {
			if errors.Is(err, store.ErrNotFinal) || errors.Is(err, store.ErrNotFound) {
				plan.Missing = append(plan.Missing, label+"（"+p.Name+"）")
				continue
			}
			return nil, err
		}
		plan.Included = append(plan.Included, GroupMember{Person: p.ID, Name: label, ProductLines: p.ProductLines, Draft: d})
	}
	for _, w := range weeks {
		d, err := ws.FinalDraftKind(store.ProductDirName, w, store.ProductReportID)
		if err != nil {
			if errors.Is(err, store.ErrNotFinal) || errors.Is(err, store.ErrNotFound) {
				plan.Missing = append(plan.Missing, w+" 产品报")
				continue
			}
			return nil, err
		}
		plan.Included = append(plan.Included, GroupMember{Person: productWeekSource + w, Name: w + " 产品报", Draft: d})
	}
	return finishMonthPlan(plan, allowMissing, "来源")
}

// finishMonthPlan 统一处理缺失与空输入。
func finishMonthPlan(plan *GroupPlan, allowMissing bool, noun string) (*GroupPlan, error) {
	sort.Strings(plan.Missing)
	if len(plan.Included) == 0 {
		return nil, fmt.Errorf("%w: %s 还没有任何定稿的%s", store.ErrNotFinal, plan.Week, noun)
	}
	if len(plan.Missing) > 0 && !allowMissing {
		return nil, fmt.Errorf("%w: 以下%s缺失或未定稿：%s（确认先不纳入请加 --allow-missing / 点「仍然生成」）",
			store.ErrNotFinal, noun, strings.Join(plan.Missing, "、"))
	}
	return plan, nil
}

// monthSourceBody 返回月报来源的当前定稿正文，用于判断来源是否变化。
func monthSourceBody(ws *store.Workspace, d *store.Draft, id string) (string, error) {
	var (
		fd  *store.Draft
		err error
	)
	switch {
	case d.Kind == store.GroupDirName:
		fd, err = ws.FinalDraftKind(store.GroupDirName, id, d.Person)
	case strings.HasPrefix(id, productWeekSource):
		fd, err = ws.FinalDraftKind(store.ProductDirName, strings.TrimPrefix(id, productWeekSource), store.ProductReportID)
	default:
		fd, err = ws.FinalDraftKind(store.LeadMonthDraftDirName, d.Week, id)
	}
	if err != nil {
		return "", err
	}
	return fd.Body, nil
}
