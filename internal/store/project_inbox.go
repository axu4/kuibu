package store

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// ProjectInboxDirName 是 SE 收到的各组项目报目录：
// reports/<周或月>/project/<项目 id>/<组 id>.md。周报、月报共用，周期目录区分。
//
// 层级写作 project/<项目 id>，沿用周报原文的覆盖 + .history 规则。
const ProjectInboxDirName = "project"

// ProjectReportDirName 是 SE 汇总的项目报目录：reports/<周或月>/se/<项目 id>.md。
// 格式与组报相同，草稿 → 人工修改 → 定稿。
const ProjectReportDirName = "se"

// ProjectInboxLevel 返回某项目收件箱的层级：project/<项目 id>。
func ProjectInboxLevel(projectID string) string {
	return ProjectInboxDirName + "/" + strings.TrimSpace(projectID)
}

// ProjectIDFromInboxLevel 返回 project/<项目 id> 里的项目 id；不是收件箱层级返回空。
func ProjectIDFromInboxLevel(level string) string {
	if !strings.HasPrefix(level, ProjectInboxDirName+"/") {
		return ""
	}
	id := strings.TrimPrefix(level, ProjectInboxDirName+"/")
	if !reportProjectIDPattern.MatchString(id) {
		return ""
	}
	return id
}

// ProjectInboxInput 是 SE 收到一份项目报时写入的字段。
type ProjectInboxInput struct {
	// Period 是周编号（2026-W44）或月份（2026-10）。
	Period  string
	Project string
	// GroupID 是交报的组 id（同时是文件名）。
	GroupID string
	// Group 是组名快照。
	Group string
	// Lead 是该组 DE 姓名快照。
	Lead   string
	Source string
	Body   string
	Now    time.Time
}

// WriteProjectInbox 归档一份组交来的项目报；同组再次提交时旧文件进 .history。
func (w *Workspace) WriteProjectInbox(in ProjectInboxInput) (*Report, error) {
	if strings.TrimSpace(in.GroupID) == "" {
		return nil, fmt.Errorf("%w: 组 id 不能为空", ErrInvalid)
	}
	if !reportProjectIDPattern.MatchString(strings.TrimSpace(in.Project)) {
		return nil, fmt.Errorf("%w: 非法项目 id %q", ErrInvalid, in.Project)
	}
	if !reportProjectIDPattern.MatchString(strings.TrimSpace(in.GroupID)) {
		return nil, fmt.Errorf("%w: 非法组 id %q", ErrInvalid, in.GroupID)
	}
	if strings.TrimSpace(in.Body) == "" {
		return nil, fmt.Errorf("%w: 项目报正文不能为空", ErrInvalid)
	}
	return w.WriteReport(NewReport{
		Person:      strings.TrimSpace(in.GroupID),
		Name:        in.Lead,
		Week:        in.Period,
		Level:       ProjectInboxLevel(in.Project),
		Group:       in.Group,
		Source:      in.Source,
		SubmittedAt: in.Now,
		Body:        in.Body,
	})
}

// ReadProjectInbox 读取某组交来的项目报；不存在返回 ErrNotFound。
func (w *Workspace) ReadProjectInbox(period, projectID, groupID string) (*Report, error) {
	return w.ReadReport(period, ProjectInboxLevel(projectID), groupID)
}

// ListProjectInbox 列出某期某项目收到的全部项目报（按组 id 排序）。
func (w *Workspace) ListProjectInbox(period, projectID string) ([]*Report, error) {
	return w.ListReports(period, ProjectInboxLevel(projectID))
}

// DeleteProjectInbox 把某组交来的项目报移入历史。
func (w *Workspace) DeleteProjectInbox(period, projectID, groupID string) error {
	return w.DeleteReport(period, ProjectInboxLevel(projectID), groupID)
}

// ProjectInboxPath 返回某组项目报的文件路径（供测试与提示）。
func (w *Workspace) ProjectInboxPath(period, projectID, groupID string) string {
	return filepath.Join(w.ReportsDir(), period, ProjectInboxDirName, projectID, groupID+noteExt)
}

// ReadProjectReport 读取 SE 汇总的项目报（周报或月报）。
func (w *Workspace) ReadProjectReport(period, projectID string) (*Draft, error) {
	return w.ReadDraftKind(ProjectReportDirName, period, projectID)
}
