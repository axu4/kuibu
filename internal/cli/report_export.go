package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/axu4/kuibu/internal/core"
	"github.com/axu4/kuibu/internal/store"
)

// reportExport 实现 report export group|product：只导出定稿，未定稿返回 NOT_FINAL(7)。
func reportExport(args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("report export")
	addCommonFlags(fs, common)
	weekFlag := fs.String("week", "", "周编号，默认本周")
	monthFlag := fs.String("month", "", "月份（month-group / month-product），默认本月")
	group := fs.String("group", "", "组名（group），默认唯一有组长的组")
	project := fs.String("project", "", "报送项目 id 或名称（group 按项目导出）")
	format := fs.String("format", core.FormatMarkdown, "格式: md|feishu-html|text")
	outFile := fs.String("out", "", "写入文件，默认输出到 stdout")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(pos, 1, "用法: kuibu report export group|product|month-group|month-product [--week W|--month M] [--group g] [--project p] [--format md|feishu-html|text] [--out file]"); err != nil {
		return err
	}
	now, err := nowFrom(env)
	if err != nil {
		return err
	}
	var week string
	if strings.HasPrefix(pos[0], "month-") {
		week, err = store.ParseMonth(*monthFlag, now)
	} else {
		week, err = weekOrThis(*weekFlag, now)
	}
	if err != nil {
		return err
	}
	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	var doc *core.ExportDoc
	switch {
	case pos[0] == "group" && *project != "":
		doc, err = core.ExportFinalProject(ws, "group-project", week, *group, *project)
	case pos[0] == "month-group" && *project != "":
		doc, err = core.ExportFinalProject(ws, "month-group-project", week, *group, *project)
	case *project != "":
		return usagef("--project 只用于 group / month-group（按项目导出），%s 不接受", pos[0])
	default:
		doc, err = core.ExportFinal(ws, pos[0], week, *group)
	}
	if err != nil {
		return err
	}
	text, err := doc.Render(*format)
	if err != nil {
		return err
	}
	if *outFile == "" {
		if common.json {
			return writeJSON(stdout, map[string]any{"title": doc.Title, "fileName": doc.FileName, "format": *format, "content": text})
		}
		_, err := io.WriteString(stdout, text)
		return err
	}
	if err := os.WriteFile(*outFile, []byte(text), 0o644); err != nil {
		return fmt.Errorf("写入导出文件失败: %w", err)
	}
	if common.json {
		return writeJSON(stdout, map[string]any{"title": doc.Title, "fileName": doc.FileName, "format": *format, "out": *outFile})
	}
	fmt.Fprintf(stdout, "已导出 %s → %s\n", doc.Title, *outFile)
	return nil
}
