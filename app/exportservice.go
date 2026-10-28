package main

import (
	"fmt"
	"os"

	"github.com/axu4/kuibu/internal/core"
)

// ExportContent 是「复制为飞书格式」要写进剪贴板的内容。
type ExportContent struct {
	Title string `json:"title"`
	HTML  string `json:"html"`
	Text  string `json:"text"`
}

// ExportReport 弹出保存对话框，把定稿报告导出为 Markdown。kind 为 group / product。
// 未定稿返回 NOT_FINAL；用户取消时返回空路径且不报错。
func (s *ReportService) ExportReport(kind, week string) (string, error) {
	ws, _, err := s.instances()
	if err != nil {
		return "", err
	}
	doc, err := core.ExportFinal(ws, kind, week, "")
	if err != nil {
		return "", err
	}
	text, err := doc.Render(core.FormatMarkdown)
	if err != nil {
		return "", err
	}
	if s.workspace == nil || s.workspace.app == nil {
		return "", fmt.Errorf("保存对话框不可用")
	}
	path, err := s.workspace.app.Dialog.SaveFile().
		SetFilename(doc.FileName).
		CanCreateDirectories(true).
		SetMessage("导出「" + doc.Title + "」").
		SetButtonText("导出").
		PromptForSingleSelection()
	if err != nil {
		return "", fmt.Errorf("打开保存对话框失败: %w", err)
	}
	if path == "" {
		return "", nil
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "", fmt.Errorf("写入导出文件失败: %w", err)
	}
	return path, nil
}

// ExportClipboard 返回定稿报告的 HTML 与纯文本，前端同时写入剪贴板（飞书取 HTML）。
// 只返回内容，不发送到任何地方。未定稿返回 NOT_FINAL。
func (s *ReportService) ExportClipboard(kind, week string) (ExportContent, error) {
	ws, _, err := s.instances()
	if err != nil {
		return ExportContent{}, err
	}
	doc, err := core.ExportFinal(ws, kind, week, "")
	if err != nil {
		return ExportContent{}, err
	}
	return ExportContent{Title: doc.Title, HTML: core.MarkdownToHTML(doc.Body), Text: core.MarkdownToText(doc.Body)}, nil
}

// ExportProjectGroup 弹出保存对话框，导出某个项目的定稿组报。
func (s *ReportService) ExportProjectGroup(week, group, projectID string) (string, error) {
	ws, _, err := s.instances()
	if err != nil {
		return "", err
	}
	doc, err := core.ExportFinalProject(ws, "group-project", week, group, projectID)
	if err != nil {
		return "", err
	}
	text, err := doc.Render(core.FormatMarkdown)
	if err != nil {
		return "", err
	}
	if s.workspace == nil || s.workspace.app == nil {
		return "", fmt.Errorf("保存对话框不可用")
	}
	path, err := s.workspace.app.Dialog.SaveFile().
		SetFilename(doc.FileName).
		CanCreateDirectories(true).
		SetMessage("导出「" + doc.Title + "」").
		SetButtonText("导出").
		PromptForSingleSelection()
	if err != nil {
		return "", fmt.Errorf("打开保存对话框失败: %w", err)
	}
	if path == "" {
		return "", nil
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "", fmt.Errorf("写入导出文件失败: %w", err)
	}
	return path, nil
}

// ExportProjectGroupClipboard 返回某个项目定稿组报的飞书 HTML 与纯文本。
func (s *ReportService) ExportProjectGroupClipboard(week, group, projectID string) (ExportContent, error) {
	ws, _, err := s.instances()
	if err != nil {
		return ExportContent{}, err
	}
	doc, err := core.ExportFinalProject(ws, "group-project", week, group, projectID)
	if err != nil {
		return ExportContent{}, err
	}
	return ExportContent{Title: doc.Title, HTML: core.MarkdownToHTML(doc.Body), Text: core.MarkdownToText(doc.Body)}, nil
}
