package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// MonthPullDirName 是月报拉取记录的目录名：reports/<YYYY-MM>/pull/<组长 personId>.md。
//
// 文件只有 front matter，记录本期包含哪些周、纳入哪些周组报、以及条目归口调整；
// 条目文本本身不落盘，每次从定稿周组报正文解析，避免两份事实来源。
const MonthPullDirName = "pull"

// 月报条目池。
const (
	// MonthPoolSummary 是本月总结池。
	MonthPoolSummary = "summary"
	// MonthPoolPlan 是下月计划池。
	MonthPoolPlan = "plan"
	// MonthPoolArchive 是更早的计划，默认不纳入。
	MonthPoolArchive = "archive"
	// MonthPoolExcluded 是本项目不纳入的条目。
	MonthPoolExcluded = "excluded"
)

// ValidMonthPool 判断月报条目池是否合法。
func ValidMonthPool(pool string) bool {
	switch pool {
	case MonthPoolSummary, MonthPoolPlan, MonthPoolArchive, MonthPoolExcluded:
		return true
	}
	return false
}

// MonthPullSource 是本期纳入的一份按项目周组报。
type MonthPullSource struct {
	Week    string `yaml:"week" json:"week"`
	Project string `yaml:"project" json:"project"`
}

// MonthPullOverride 是一条偏离默认归口的条目。
//
// Item 是「项目 + 节名 + 规范化文本」的稳定 id；Week 只作追溯，条目可能跨周出现。
type MonthPullOverride struct {
	Project string `yaml:"project" json:"project"`
	Week    string `yaml:"week,omitempty" json:"week,omitempty"`
	Section string `yaml:"section" json:"section"`
	Item    string `yaml:"item" json:"item"`
	Pool    string `yaml:"pool" json:"pool"`
}

// MonthPull 是一个月、一个组的月报拉取记录。
type MonthPull struct {
	Month     string              `yaml:"month" json:"month"`
	Group     string              `yaml:"group" json:"group"`
	Lead      string              `yaml:"lead" json:"lead"`
	UpdatedAt string              `yaml:"updatedAt" json:"updatedAt"`
	Weeks     []string            `yaml:"weeks" json:"weeks"`
	Sources   []MonthPullSource   `yaml:"sources" json:"sources"`
	Overrides []MonthPullOverride `yaml:"overrides,omitempty" json:"overrides"`
}

// MonthPullPath 返回拉取记录路径。
func (w *Workspace) MonthPullPath(month, leadID string) string {
	return filepath.Join(w.ReportsDir(), month, MonthPullDirName, leadID+noteExt)
}

// ReadMonthPull 读取拉取记录；不存在时返回 ErrNotFound。
func (w *Workspace) ReadMonthPull(month, leadID string) (*MonthPull, error) {
	path := w.MonthPullPath(month, leadID)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, newReportError(ErrNotFound, "月报拉取记录不存在: %s %s", month, leadID)
		}
		return nil, fmt.Errorf("读取月报拉取记录失败: %w", err)
	}
	front, body, ok := splitFrontMatter(data)
	if !ok {
		return nil, fmt.Errorf("%w: 月报拉取记录缺少 front matter", ErrInvalid)
	}
	if strings.TrimSpace(string(body)) != "" {
		return nil, fmt.Errorf("%w: 月报拉取记录正文应为空", ErrInvalid)
	}
	var p MonthPull
	if err := yaml.Unmarshal(front, &p); err != nil {
		return nil, fmt.Errorf("解析月报拉取记录失败: %w", err)
	}
	if p.Month == "" {
		p.Month = month
	}
	if p.Lead == "" {
		p.Lead = leadID
	}
	initMonthPullDefaults(&p)
	if err := validateMonthPull(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

// WriteMonthPull 原子写入拉取记录（只有 front matter）。
func (w *Workspace) WriteMonthPull(p *MonthPull) error {
	if p == nil {
		return fmt.Errorf("%w: 月报拉取记录为空", ErrInvalid)
	}
	if err := validateMonthPull(p); err != nil {
		return err
	}
	front, err := yaml.Marshal(p)
	if err != nil {
		return fmt.Errorf("序列化月报拉取记录失败: %w", err)
	}
	data := append([]byte("---\n"), front...)
	data = append(data, []byte("---\n")...)
	path := w.MonthPullPath(p.Month, p.Lead)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建月报拉取目录失败: %w", err)
	}
	if err := writeFileAtomic(path, data); err != nil {
		return fmt.Errorf("写入月报拉取记录失败: %w", err)
	}
	return nil
}

// DeleteMonthPull 删除拉取记录；不存在时返回 nil。
func (w *Workspace) DeleteMonthPull(month, leadID string) error {
	err := os.Remove(w.MonthPullPath(month, leadID))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("删除月报拉取记录失败: %w", err)
	}
	return nil
}

// UpdateMonthPull 读取、修改并写回拉取记录；updater 返回错误时不写盘。
func (w *Workspace) UpdateMonthPull(month, leadID string, updater func(*MonthPull) error) (*MonthPull, error) {
	p, err := w.ReadMonthPull(month, leadID)
	if err != nil {
		return nil, err
	}
	if err := updater(p); err != nil {
		return nil, err
	}
	p.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := w.WriteMonthPull(p); err != nil {
		return nil, err
	}
	return p, nil
}

func initMonthPullDefaults(p *MonthPull) {
	if p.Weeks == nil {
		p.Weeks = []string{}
	}
	if p.Sources == nil {
		p.Sources = []MonthPullSource{}
	}
	if p.Overrides == nil {
		p.Overrides = []MonthPullOverride{}
	}
	if p.UpdatedAt == "" && (len(p.Weeks) > 0 || len(p.Sources) > 0) {
		p.UpdatedAt = time.Now().Format(time.RFC3339)
	}
}

func validateMonthPull(p *MonthPull) error {
	if !ValidMonth(p.Month) {
		return fmt.Errorf("%w: 月报月份必须形如 2026-10，得到 %q", ErrInvalid, p.Month)
	}
	if strings.TrimSpace(p.Lead) == "" {
		return fmt.Errorf("%w: 月报拉取记录缺少组长", ErrInvalid)
	}
	weeks := map[string]bool{}
	for _, week := range p.Weeks {
		if !IsWeek(week) {
			return fmt.Errorf("%w: 月报周范围里有非法周 %q", ErrInvalid, week)
		}
		if weeks[week] {
			return fmt.Errorf("%w: 月报周范围重复: %s", ErrInvalid, week)
		}
		weeks[week] = true
	}
	sources := map[string]bool{}
	for _, src := range p.Sources {
		if !weeks[src.Week] {
			return fmt.Errorf("%w: 纳入的周组报 %s 不在本期周范围里", ErrInvalid, src.Week)
		}
		if strings.TrimSpace(src.Project) == "" {
			return fmt.Errorf("%w: 纳入的周组报缺少项目", ErrInvalid)
		}
		key := src.Week + "/" + src.Project
		if sources[key] {
			return fmt.Errorf("%w: 纳入的周组报重复: %s", ErrInvalid, key)
		}
		sources[key] = true
	}
	overrides := map[string]bool{}
	for _, o := range p.Overrides {
		if strings.TrimSpace(o.Project) == "" || strings.TrimSpace(o.Section) == "" || strings.TrimSpace(o.Item) == "" {
			return fmt.Errorf("%w: 月报归口调整缺少项目、节名或条目 id", ErrInvalid)
		}
		if o.Week != "" && !IsWeek(o.Week) {
			return fmt.Errorf("%w: 月报归口调整的周非法: %q", ErrInvalid, o.Week)
		}
		if !ValidMonthPool(o.Pool) {
			return fmt.Errorf("%w: 月报归口池非法: %q", ErrInvalid, o.Pool)
		}
		key := o.Project + "/" + o.Section + "/" + o.Item
		if overrides[key] {
			return fmt.Errorf("%w: 月报归口调整重复: %s", ErrInvalid, key)
		}
		overrides[key] = true
	}
	return nil
}
