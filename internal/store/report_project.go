package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// MaxReportProjects 是工作台最多允许配置的报送项目数。
const MaxReportProjects = 4

// reportProjectIDPattern 限定项目 id 只能用于路径的一段，防止手改配置越出目录。
var reportProjectIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ReportProject 是一个报送项目（组报按项目分别成稿）。
//
// 项目只是报送口径，不改变组员名单里的产品线；id 稳定、名称可改。
type ReportProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ReportProjects 返回工作台配置的报送项目（按配置顺序）。
func (w *Workspace) ReportProjects() ([]ReportProject, error) {
	cfg, err := w.readConfig()
	if err != nil {
		return nil, err
	}
	return append([]ReportProject(nil), cfg.ReportProjects...), nil
}

// SetReportProjects 覆盖报送项目配置，保持其他配置字段不变。
func (w *Workspace) SetReportProjects(projects []ReportProject) error {
	normalized, err := normalizeReportProjects(projects)
	if err != nil {
		return err
	}
	cfg, err := w.readConfig()
	if err != nil {
		return err
	}
	cfg.ReportProjects = normalized
	return w.writeConfig(cfg)
}

// AddReportProject 追加一个项目并返回新列表。重名返回 ErrConflict。
func (w *Workspace) AddReportProject(name string) ([]ReportProject, ReportProject, error) {
	projects, err := w.ReportProjects()
	if err != nil {
		return nil, ReportProject{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ReportProject{}, fmt.Errorf("%w: 项目名不能为空", ErrInvalid)
	}
	if len(projects) >= MaxReportProjects {
		return nil, ReportProject{}, fmt.Errorf("%w: 最多 %d 个报送项目", ErrInvalid, MaxReportProjects)
	}
	for _, p := range projects {
		if p.Name == name {
			return nil, ReportProject{}, fmt.Errorf("%w: 项目「%s」已存在", ErrConflict, name)
		}
	}
	project := ReportProject{ID: newReportProjectID(), Name: name}
	projects = append(projects, project)
	if err := w.SetReportProjects(projects); err != nil {
		return nil, ReportProject{}, err
	}
	return projects, project, nil
}

// RenameReportProject 按 id 或名称改名并返回新列表。名称与其他项目冲突时返回 ErrConflict。
func (w *Workspace) RenameReportProject(idOrName, newName string) ([]ReportProject, ReportProject, error) {
	projects, err := w.ReportProjects()
	if err != nil {
		return nil, ReportProject{}, err
	}
	idx := findReportProjectIndex(projects, idOrName)
	if idx < 0 {
		return nil, ReportProject{}, fmt.Errorf("%w: 找不到报送项目 %q", ErrNotFound, idOrName)
	}
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return nil, ReportProject{}, fmt.Errorf("%w: 项目名不能为空", ErrInvalid)
	}
	for i, p := range projects {
		if i != idx && p.Name == newName {
			return nil, ReportProject{}, fmt.Errorf("%w: 项目「%s」已存在", ErrConflict, newName)
		}
	}
	projects[idx].Name = newName
	if err := w.SetReportProjects(projects); err != nil {
		return nil, ReportProject{}, err
	}
	return projects, projects[idx], nil
}

// RemoveReportProject 按 id 或名称删除项目并返回新列表与被删除的项目。
func (w *Workspace) RemoveReportProject(idOrName string) ([]ReportProject, ReportProject, error) {
	projects, err := w.ReportProjects()
	if err != nil {
		return nil, ReportProject{}, err
	}
	idx := findReportProjectIndex(projects, idOrName)
	if idx < 0 {
		return nil, ReportProject{}, fmt.Errorf("%w: 找不到报送项目 %q", ErrNotFound, idOrName)
	}
	removed := projects[idx]
	projects = append(projects[:idx], projects[idx+1:]...)
	if err := w.SetReportProjects(projects); err != nil {
		return nil, ReportProject{}, err
	}
	return projects, removed, nil
}

// FindReportProject 按 id 或名称查找项目。
func FindReportProject(projects []ReportProject, idOrName string) (ReportProject, bool) {
	idx := findReportProjectIndex(projects, idOrName)
	if idx < 0 {
		return ReportProject{}, false
	}
	return projects[idx], true
}

// ReportProjectName 返回项目名称；找不到时返回 id 本身（历史快照兜底）。
func ReportProjectName(projects []ReportProject, id string) string {
	if p, ok := FindReportProject(projects, id); ok {
		return p.Name
	}
	return id
}

func findReportProjectIndex(projects []ReportProject, idOrName string) int {
	idOrName = strings.TrimSpace(idOrName)
	for i, p := range projects {
		if p.ID == idOrName || p.Name == idOrName {
			return i
		}
	}
	return -1
}

func normalizeReportProjects(projects []ReportProject) ([]ReportProject, error) {
	if len(projects) > MaxReportProjects {
		return nil, fmt.Errorf("%w: 最多 %d 个报送项目", ErrInvalid, MaxReportProjects)
	}
	out := make([]ReportProject, 0, len(projects))
	ids := map[string]bool{}
	names := map[string]bool{}
	for _, p := range projects {
		p.ID = strings.TrimSpace(p.ID)
		p.Name = strings.TrimSpace(p.Name)
		if p.ID == "" || p.Name == "" {
			return nil, fmt.Errorf("%w: 报送项目的 id 与名称都不能为空", ErrInvalid)
		}
		if !reportProjectIDPattern.MatchString(p.ID) {
			return nil, fmt.Errorf("%w: 报送项目 id 只能包含字母、数字、下划线和连字符: %q", ErrInvalid, p.ID)
		}
		if ids[p.ID] {
			return nil, fmt.Errorf("%w: 报送项目 id 重复: %s", ErrInvalid, p.ID)
		}
		if names[p.Name] {
			return nil, fmt.Errorf("%w: 报送项目名称重复: %s", ErrConflict, p.Name)
		}
		ids[p.ID] = true
		names[p.Name] = true
		out = append(out, p)
	}
	return out, nil
}

func newReportProjectID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "p" + fmt.Sprintf("%d", len(b))
	}
	return "p" + hex.EncodeToString(b[:])
}

// SortReportProjects 按名称排序（CLI 展示用；写配置仍保持原顺序）。
func SortReportProjects(projects []ReportProject) []ReportProject {
	out := append([]ReportProject(nil), projects...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
