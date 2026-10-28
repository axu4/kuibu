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

// LibraryReport 是周报库里一份已定稿组报。
type LibraryReport struct {
	ID          string `json:"id"`
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

// ListReportLibrary 扫描一周内所有已定稿组报（含按项目分册）。
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
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].FinalizedAt != out[j].FinalizedAt {
			return out[i].FinalizedAt > out[j].FinalizedAt
		}
		return out[i].Group < out[j].Group
	})
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
