package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// reportSectionsDirName 是录入时识别结果的旁挂目录，放在周目录下的隐藏目录，
// 不参与周报索引，也不改变组员原文。这样原文仍然可追溯，分拣用校正后的结构化条目。
const reportSectionsDirName = ".member-items"

// ReportSection 是录入时识别并允许人工校正的一节。
type ReportSection struct {
	// Section 是固定节名：本周完成 / 下周计划 / 风险与求助。
	Section string `json:"section"`
	// Items 是这一节的条目文本，顺序保留。
	Items []string `json:"items"`
}

// ReportSections 是一份组员周报在录入阶段确认过的结构化条目。
type ReportSections struct {
	Person     string          `json:"person"`
	Week       string          `json:"week"`
	SourceHash string          `json:"sourceHash"`
	UpdatedAt  string          `json:"updatedAt"`
	Sections   []ReportSection `json:"sections"`
}

// ReportSectionsPath 返回某份录入结构化的旁挂文件路径。
func (w *Workspace) ReportSectionsPath(week, personID string) string {
	return filepath.Join(w.ReportsDir(), week, reportSectionsDirName, personID+".json")
}

// ReadReportSections 读取录入结构化；不存在时返回 ErrNotFound。
func (w *Workspace) ReadReportSections(week, personID string) (*ReportSections, error) {
	path := w.ReportSectionsPath(week, personID)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, newReportError(ErrNotFound, "录入识别结果不存在: %s %s", week, personID)
		}
		return nil, fmt.Errorf("读取录入识别结果失败: %w", err)
	}
	var out ReportSections
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("解析录入识别结果失败: %w", err)
	}
	if out.Sections == nil {
		out.Sections = []ReportSection{}
	}
	return &out, nil
}

// WriteReportSections 原子写入录入结构化；空条目会被清掉。
func (w *Workspace) WriteReportSections(in ReportSections) error {
	if strings.TrimSpace(in.Person) == "" || strings.TrimSpace(in.Week) == "" {
		return fmt.Errorf("%w: 录入识别结果缺少人员或周", ErrInvalid)
	}
	if err := ValidPeriod(in.Week); err != nil {
		return err
	}
	sections := make([]ReportSection, 0, len(in.Sections))
	for _, sec := range in.Sections {
		if strings.TrimSpace(sec.Section) == "" {
			return fmt.Errorf("%w: 录入识别结果缺少节名", ErrInvalid)
		}
		items := make([]string, 0, len(sec.Items))
		for _, item := range sec.Items {
			if text := strings.TrimSpace(item); text != "" {
				items = append(items, text)
			}
		}
		if len(items) > 0 {
			sections = append(sections, ReportSection{Section: strings.TrimSpace(sec.Section), Items: items})
		}
	}
	in.Sections = sections
	if in.UpdatedAt == "" {
		in.UpdatedAt = time.Now().Format(time.RFC3339)
	}
	data, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化录入识别结果失败: %w", err)
	}
	data = append(data, '\n')
	path := w.ReportSectionsPath(in.Week, in.Person)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建录入识别目录失败: %w", err)
	}
	if err := writeFileAtomic(path, data); err != nil {
		return fmt.Errorf("写入录入识别结果失败: %w", err)
	}
	return nil
}

// DeleteReportSections 删除录入结构化；不存在时返回 nil。
func (w *Workspace) DeleteReportSections(week, personID string) error {
	err := os.Remove(w.ReportSectionsPath(week, personID))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
