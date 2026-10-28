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

// TriageDirName 是每周筛选记录的目录名：reports/<周>/triage/<组长 personId>.md。
//
// 文件只有 front matter，记录每条事项的去留和项目归口；正文留空，避免与来源正文
// 形成两份事实来源。条目文本会快照保存，来源变化用 sourceHash 与条目 id 对照。
const TriageDirName = "triage"

// 筛选决策。
const (
	// TriageAccepted 表示采纳并归入某个项目。
	TriageAccepted = "accepted"
	// TriageRejected 表示移除，不进任何项目的组报。
	TriageRejected = "rejected"
	// TriageSkipped 表示待定，暂不带入。
	TriageSkipped = "skipped"
)

// TriageProject 是筛选记录里的项目快照（名称改了也保留当周叫法）。
type TriageProject struct {
	ID   string `yaml:"id" json:"id"`
	Name string `yaml:"name" json:"name"`
}

// TriageItem 是一条待筛选事项及其决策。
type TriageItem struct {
	// ID 是「人员 + 节名 + 文本」的 sha256 前 8 位，文本改了 id 就变。
	ID      string `yaml:"id" json:"id"`
	Person  string `yaml:"person" json:"person"`
	Name    string `yaml:"name" json:"name"`
	Section string `yaml:"section" json:"section"`
	Domain  string `yaml:"domain" json:"domain"`
	Text    string `yaml:"text" json:"text"`
	// Decision 取 accepted / rejected / skipped；空表示未判。
	Decision string `yaml:"decision,omitempty" json:"decision"`
	// Project 仅在 accepted 时存在。
	Project string `yaml:"project,omitempty" json:"project"`
	// Order 是来源顺序（组员、节、条目），用于稳定的展示顺序。
	Order int `yaml:"order" json:"order"`
	// DecidedAt 是最近一次决策时间，未判为空。
	DecidedAt string `yaml:"decidedAt,omitempty" json:"decidedAt"`
}

// Triage 是一周的筛选记录。
type Triage struct {
	Week       string          `yaml:"week" json:"week"`
	Group      string          `yaml:"group" json:"group"`
	Lead       string          `yaml:"lead" json:"lead"`
	UpdatedAt  string          `yaml:"updatedAt" json:"updatedAt"`
	SourceHash string          `yaml:"sourceHash,omitempty" json:"sourceHash"`
	Projects   []TriageProject `yaml:"projects" json:"projects"`
	Items      []TriageItem    `yaml:"items" json:"items"`
	// Missing 是没有可筛选条目（没交/没按条写）的组员姓名。
	Missing []string `yaml:"missing,omitempty" json:"missing"`
}

// TriagePath 返回筛选记录路径。
func (w *Workspace) TriagePath(week, leadID string) string {
	return filepath.Join(w.ReportsDir(), week, TriageDirName, leadID+noteExt)
}

// ReadTriage 读取筛选记录；不存在时返回 ErrNotFound。
func (w *Workspace) ReadTriage(week, leadID string) (*Triage, error) {
	path := w.TriagePath(week, leadID)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, newReportError(ErrNotFound, "筛选记录不存在: %s %s", week, leadID)
		}
		return nil, fmt.Errorf("读取筛选记录失败: %w", err)
	}
	front, body, ok := splitFrontMatter(data)
	if !ok {
		return nil, fmt.Errorf("%w: 筛选记录缺少 front matter", ErrInvalid)
	}
	if strings.TrimSpace(string(body)) != "" {
		return nil, fmt.Errorf("%w: 筛选记录正文应为空", ErrInvalid)
	}
	var t Triage
	if err := yaml.Unmarshal(front, &t); err != nil {
		return nil, fmt.Errorf("解析筛选记录失败: %w", err)
	}
	if t.Week == "" {
		t.Week = week
	}
	if t.Lead == "" {
		t.Lead = leadID
	}
	initTriageDefaults(&t)
	return &t, nil
}

// WriteTriage 原子写入筛选记录（只有 front matter）。
func (w *Workspace) WriteTriage(t *Triage) error {
	if t == nil {
		return fmt.Errorf("%w: 筛选记录为空", ErrInvalid)
	}
	if strings.TrimSpace(t.Lead) == "" {
		return fmt.Errorf("%w: 筛选记录缺少组长", ErrInvalid)
	}
	if err := ValidPeriod(t.Week); err != nil {
		return err
	}
	initTriageDefaults(t)
	if err := validateTriage(t); err != nil {
		return err
	}
	front, err := yaml.Marshal(t)
	if err != nil {
		return fmt.Errorf("序列化筛选记录失败: %w", err)
	}
	data := append([]byte("---\n"), front...)
	data = append(data, []byte("---\n")...)
	path := w.TriagePath(t.Week, t.Lead)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建筛选目录失败: %w", err)
	}
	if err := writeFileAtomic(path, data); err != nil {
		return fmt.Errorf("写入筛选记录失败: %w", err)
	}
	return nil
}

// DeleteTriage 删除筛选记录；不存在时返回 nil。
func (w *Workspace) DeleteTriage(week, leadID string) error {
	err := os.Remove(w.TriagePath(week, leadID))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("删除筛选记录失败: %w", err)
	}
	return nil
}

// UpdateTriage 读取、修改并写回筛选记录；updater 返回错误时不写盘。
func (w *Workspace) UpdateTriage(week, leadID string, updater func(*Triage) error) (*Triage, error) {
	t, err := w.ReadTriage(week, leadID)
	if err != nil {
		return nil, err
	}
	if err := updater(t); err != nil {
		return nil, err
	}
	t.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := w.WriteTriage(t); err != nil {
		return nil, err
	}
	return t, nil
}

func initTriageDefaults(t *Triage) {
	if t.Projects == nil {
		t.Projects = []TriageProject{}
	}
	if t.Items == nil {
		t.Items = []TriageItem{}
	}
	if t.Missing == nil {
		t.Missing = []string{}
	}
	if t.UpdatedAt == "" && len(t.Items) > 0 {
		t.UpdatedAt = time.Now().Format(time.RFC3339)
	}
}

func validateTriage(t *Triage) error {
	ids := map[string]bool{}
	projects := map[string]bool{}
	for _, p := range t.Projects {
		if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Name) == "" {
			return fmt.Errorf("%w: 筛选记录里的项目 id 与名称不能为空", ErrInvalid)
		}
		projects[p.ID] = true
	}
	for _, item := range t.Items {
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Text) == "" {
			return fmt.Errorf("%w: 筛选条目缺少 id 或文本", ErrInvalid)
		}
		if ids[item.ID] {
			return fmt.Errorf("%w: 筛选条目 id 重复: %s", ErrInvalid, item.ID)
		}
		ids[item.ID] = true
		switch item.Decision {
		case "", TriageAccepted, TriageRejected, TriageSkipped:
		default:
			return fmt.Errorf("%w: 非法筛选决策 %q", ErrInvalid, item.Decision)
		}
		if item.Decision == TriageAccepted && !projects[item.Project] {
			return fmt.Errorf("%w: 条目 %s 归入了不存在的项目 %q", ErrInvalid, item.ID, item.Project)
		}
	}
	return nil
}
