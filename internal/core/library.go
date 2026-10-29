package core

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/axu4/kuibu/internal/store"
)

// 周报库条目来源。
const (
	// LibraryKindGroup 是本工作台整理的定稿组报。
	LibraryKindGroup = "group"
	// LibraryKindInbox 是其他 DE 交给我（SE）的项目报，粘贴归档即算交。
	LibraryKindInbox = "inbox"
	// LibraryKindSE 是我当 SE 汇总的定稿项目报。
	LibraryKindSE = "se"
)

// LibraryReport 是周报库里一份已定稿组报 / 已交项目报 / SE 定稿项目报。
type LibraryReport struct {
	ID string `json:"id"`
	// Kind 是来源：group / inbox / se。
	Kind        string `json:"kind"`
	Week        string `json:"week"`
	Group       string `json:"group"`
	Project     string `json:"project"`
	ProjectID   string `json:"projectId"`
	LeadID      string `json:"leadId"`
	LeadName    string `json:"leadName"`
	Count       int    `json:"count"`
	FinalizedAt string `json:"finalizedAt"`
	Path        string `json:"path"`
	Body        string `json:"body"`
}

// ListReportLibrary 扫描一周内所有已定稿组报（含按项目分册）、其他 DE 交来的项目报
// 和 SE 定稿的项目报。
//
// 只读；历史 lead / product 数据不参与。group / project 为空表示不过滤。
func ListReportLibrary(ws *store.Workspace, week, groupName, projectName string) ([]LibraryReport, error) {
	base := filepath.Join(ws.ReportsDir(), week, store.GroupDirName)
	projects, err := ws.ReportProjects()
	if err != nil {
		return nil, err
	}
	projectNames := map[string]string{}
	for _, p := range projects {
		projectNames[p.ID] = p.Name
	}
	out := make([]LibraryReport, 0)
	err = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			if d.Name() == ".history" || strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		rel, err := filepath.Rel(base, path)
		if err != nil {
			return nil
		}
		parts := strings.Split(rel, string(filepath.Separator))
		leadID := strings.TrimSuffix(parts[len(parts)-1], ".md")
		kind := store.GroupDirName
		projectID := ""
		if len(parts) == 2 {
			projectID = parts[0]
			kind = store.GroupReportKind(projectID)
		} else if len(parts) != 1 {
			return nil
		}
		draft, err := ws.ReadDraftKind(kind, week, leadID)
		if err != nil {
			return nil
		}
		if draft.Status != store.DraftStatusFinal {
			return nil
		}
		gname := strings.TrimSpace(draft.Group)
		if gname == "" {
			gname = "未命名组"
		}
		pname := projectNames[projectID]
		if groupName != "" && gname != groupName {
			return nil
		}
		if projectName != "" && pname != projectName && projectID != projectName {
			return nil
		}
		finalized := ""
		if !draft.FinalizedAt.IsZero() {
			finalized = draft.FinalizedAt.In(store.TodoLocation()).Format(time.RFC3339)
		}
		out = append(out, LibraryReport{
			ID:          week + "/" + kind + "/" + leadID,
			Kind:        LibraryKindGroup,
			Week:        week,
			Group:       gname,
			Project:     pname,
			ProjectID:   projectID,
			LeadID:      leadID,
			LeadName:    draft.Name,
			Count:       countReportItems(draft.Body),
			FinalizedAt: finalized,
			Path:        path,
			Body:        draft.Body,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("读取周报库失败: %w", err)
	}
	extra, err := libraryProjectReports(ws, week, projects)
	if err != nil {
		return nil, err
	}
	for _, r := range extra {
		if groupName != "" && r.Group != groupName {
			continue
		}
		if projectName != "" && r.Project != projectName && r.ProjectID != projectName {
			continue
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].FinalizedAt != out[j].FinalizedAt {
			return out[i].FinalizedAt > out[j].FinalizedAt
		}
		return out[i].Group < out[j].Group
	})
	return out, nil
}

// libraryProjectReports 收集其他 DE 交来的项目报（按组）与 SE 定稿的项目报。
func libraryProjectReports(ws *store.Workspace, week string, projects []store.ReportProject) ([]LibraryReport, error) {
	out := make([]LibraryReport, 0)
	for _, p := range projects {
		inbox, err := ws.ListProjectInbox(week, p.ID)
		if err != nil {
			return nil, fmt.Errorf("读取周报库失败: %w", err)
		}
		for _, r := range inbox {
			group := strings.TrimSpace(r.Group)
			if group == "" {
				group = r.Person
			}
			out = append(out, LibraryReport{
				ID:          week + "/" + store.ProjectInboxLevel(p.ID) + "/" + r.Person,
				Kind:        LibraryKindInbox,
				Week:        week,
				Group:       group,
				Project:     p.Name,
				ProjectID:   p.ID,
				LeadID:      r.Person,
				LeadName:    r.Name,
				Count:       countReportItems(r.Body),
				FinalizedAt: r.SubmittedAt.In(store.TodoLocation()).Format(time.RFC3339),
				Path:        ws.ProjectInboxPath(week, p.ID, r.Person),
				Body:        r.Body,
			})
		}
		d, err := ws.ReadProjectReport(week, p.ID)
		if err != nil || d.Status != store.DraftStatusFinal {
			continue
		}
		finalized := ""
		if !d.FinalizedAt.IsZero() {
			finalized = d.FinalizedAt.In(store.TodoLocation()).Format(time.RFC3339)
		}
		out = append(out, LibraryReport{
			ID:          week + "/" + store.ProjectReportDirName + "/" + p.ID,
			Kind:        LibraryKindSE,
			Week:        week,
			Group:       "SE 汇总",
			Project:     p.Name,
			ProjectID:   p.ID,
			Count:       countReportItems(d.Body),
			FinalizedAt: finalized,
			Path:        filepath.Join(ws.ReportsDir(), week, store.ProjectReportDirName, p.ID+".md"),
			Body:        d.Body,
		})
	}
	return out, nil
}

func countReportItems(body string) int {
	n := 0
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") || strings.HasPrefix(t, "+ ") {
			n++
			continue
		}
		if i := strings.Index(t, ". "); i > 0 && i <= 3 && strings.Trim(t[:i], "0123456789") == "" {
			n++
		}
	}
	return n
}
