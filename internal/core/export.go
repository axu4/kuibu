package core

import (
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/axu4/kuibu/internal/store"
)

// 导出格式。
const (
	FormatMarkdown = "md"
	FormatHTML     = "feishu-html"
	FormatText     = "text"
)

// ExportDoc 是一份可导出的定稿报告。
type ExportDoc struct {
	// Title 用于提示，如「框架组 组报 2026-W44」。
	Title string `json:"title"`
	// FileName 是默认导出文件名（.md）。
	FileName string `json:"fileName"`
	// Body 是定稿正文（Markdown，不含 front matter）。
	Body string `json:"body"`
}

// ExportFinal 读取定稿报告；kind 为 group（组报）或 product（产品报）。未定稿返回 ErrNotFinal。
func ExportFinal(ws *store.Workspace, kind, week, group string) (*ExportDoc, error) {
	return ExportFinalProject(ws, kind, week, group, "")
}

// ExportFinalProject 同 ExportFinal，kind=group-project 时按项目导出。
func ExportFinalProject(ws *store.Workspace, kind, week, group, projectID string) (*ExportDoc, error) {
	switch kind {
	case "group":
		lead, groupName, err := ResolveLead(ws, week, group)
		if err != nil {
			return nil, err
		}
		d, err := ws.FinalDraftKind(store.GroupDirName, week, lead.ID)
		if err != nil {
			return nil, err
		}
		if d.Group != "" {
			groupName = d.Group
		}
		return &ExportDoc{
			Title:    fmt.Sprintf("%s 组报 %s", groupName, week),
			FileName: fmt.Sprintf("%s-组报-%s.md", safeFileName(groupName), week),
			Body:     d.Body,
		}, nil
	case "group-project":
		lead, groupName, err := ResolveLead(ws, week, group)
		if err != nil {
			return nil, err
		}
		project, ok := findProject(ws, projectID)
		if !ok {
			return nil, fmt.Errorf("%w: 找不到报送项目 %q", store.ErrNotFound, projectID)
		}
		d, err := ws.FinalDraftKind(store.GroupReportKind(project.ID), week, lead.ID)
		if err != nil {
			return nil, err
		}
		if d.Group != "" {
			groupName = d.Group
		}
		return &ExportDoc{
			Title:    fmt.Sprintf("%s 组报 %s（%s）", groupName, week, project.Name),
			FileName: fmt.Sprintf("%s-组报-%s-%s.md", safeFileName(groupName), week, safeFileName(project.Name)),
			Body:     d.Body,
		}, nil
	case "product":
		d, err := ws.FinalDraftKind(store.ProductDirName, week, store.ProductReportID)
		if err != nil {
			return nil, err
		}
		return &ExportDoc{
			Title:    "产品报 " + week,
			FileName: fmt.Sprintf("产品报-%s.md", week),
			Body:     d.Body,
		}, nil
	case "month-group":
		lead, groupName, err := MonthLead(ws, week, group)
		if err != nil {
			return nil, err
		}
		d, err := ws.FinalDraftKind(store.GroupDirName, week, lead.ID)
		if err != nil {
			return nil, err
		}
		return &ExportDoc{
			Title:    fmt.Sprintf("%s 组月报 %s", groupName, week),
			FileName: fmt.Sprintf("%s-组月报-%s.md", safeFileName(groupName), week),
			Body:     d.Body,
		}, nil
	case "month-product":
		d, err := ws.FinalDraftKind(store.ProductDirName, week, store.ProductReportID)
		if err != nil {
			return nil, err
		}
		return &ExportDoc{Title: "产品月报 " + week, FileName: fmt.Sprintf("产品月报-%s.md", week), Body: d.Body}, nil
	}
	return nil, fmt.Errorf("%w: 未知报告类型 %q（group|product|month-group|month-product）", store.ErrInvalid, kind)
}

// Render 按格式输出正文。
func (d *ExportDoc) Render(format string) (string, error) {
	switch format {
	case "", FormatMarkdown:
		return strings.TrimRight(d.Body, "\n") + "\n", nil
	case FormatHTML:
		return MarkdownToHTML(d.Body), nil
	case FormatText:
		return MarkdownToText(d.Body), nil
	}
	return "", fmt.Errorf("%w: 未知格式 %q（md|feishu-html|text）", store.ErrInvalid, format)
}

// safeFileName 去掉文件名里不安全的字符。
func safeFileName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "未分组"
	}
	return strings.NewReplacer("/", "-", "\\", "-", ":", "-", "*", "", "?", "", "\"", "", "<", "", ">", "", "|", "").Replace(s)
}

// mdLine 是一行 Markdown 的分类结果。
type mdLine struct {
	kind   string // heading / item / quote / text / blank
	level  int    // 标题级别或列表缩进层级
	text   string
	marker string // 有序列表时为 "ol"
}

var (
	reHeading = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	reBullet  = regexp.MustCompile(`^(\s*)[-*+]\s+(.*)$`)
	reOrdered = regexp.MustCompile(`^(\s*)\d+[.)]\s+(.*)$`)
	reQuote   = regexp.MustCompile(`^\s*>\s?(.*)$`)
)

// classify 把正文切成行并分类。列表缩进每 2 个空格（或 1 个 tab）算一层。
func classify(body string) []mdLine {
	var out []mdLine
	for _, raw := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		line := strings.ReplaceAll(raw, "\t", "  ")
		switch {
		case strings.TrimSpace(line) == "":
			out = append(out, mdLine{kind: "blank"})
		case reHeading.MatchString(strings.TrimSpace(line)):
			m := reHeading.FindStringSubmatch(strings.TrimSpace(line))
			out = append(out, mdLine{kind: "heading", level: len(m[1]), text: m[2]})
		case reBullet.MatchString(line):
			m := reBullet.FindStringSubmatch(line)
			out = append(out, mdLine{kind: "item", level: len(m[1]) / 2, text: m[2]})
		case reOrdered.MatchString(line):
			m := reOrdered.FindStringSubmatch(line)
			out = append(out, mdLine{kind: "item", level: len(m[1]) / 2, text: m[2], marker: "ol"})
		case reQuote.MatchString(line):
			out = append(out, mdLine{kind: "quote", text: reQuote.FindStringSubmatch(line)[1]})
		default:
			out = append(out, mdLine{kind: "text", text: strings.TrimSpace(line)})
		}
	}
	return out
}

var (
	reCode   = regexp.MustCompile("`([^`]+)`")
	reLink   = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
	reBold   = regexp.MustCompile(`\*\*([^*]+)\*\*|__([^_]+)__`)
	reItalic = regexp.MustCompile(`(^|[^*])\*([^*\s][^*]*)\*`)
)

// inlineHTML 转换行内语法：代码、链接、加粗、斜体。先转义 HTML。
func inlineHTML(s string) string {
	s = html.EscapeString(s)
	var codes []string
	s = reCode.ReplaceAllStringFunc(s, func(m string) string {
		codes = append(codes, "<code>"+reCode.FindStringSubmatch(m)[1]+"</code>")
		return fmt.Sprintf("\x00%d\x00", len(codes)-1)
	})
	s = reLink.ReplaceAllString(s, `<a href="$2">$1</a>`)
	s = reBold.ReplaceAllString(s, "<strong>$1$2</strong>")
	s = reItalic.ReplaceAllString(s, "$1<em>$2</em>")
	for i, c := range codes {
		s = strings.Replace(s, fmt.Sprintf("\x00%d\x00", i), c, 1)
	}
	return s
}

// MarkdownToHTML 把报告 Markdown 转成 HTML，覆盖标题、（嵌套）列表、引用、段落与行内语法。
// 只服务于本项目生成的报告，不追求完整 CommonMark。
func MarkdownToHTML(body string) string {
	lines := classify(body)
	var b strings.Builder
	var stack []string // 打开的列表标签
	closeLists := func(to int) {
		for len(stack) > to {
			b.WriteString("</li></" + stack[len(stack)-1] + ">")
			stack = stack[:len(stack)-1]
		}
	}
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if l.kind != "item" {
			closeLists(0)
		}
		switch l.kind {
		case "heading":
			fmt.Fprintf(&b, "<h%d>%s</h%d>", l.level, inlineHTML(l.text), l.level)
		case "quote":
			var parts []string
			for ; i < len(lines) && lines[i].kind == "quote"; i++ {
				parts = append(parts, inlineHTML(lines[i].text))
			}
			i--
			b.WriteString("<blockquote>" + strings.Join(parts, "<br>") + "</blockquote>")
		case "text":
			var parts []string
			for ; i < len(lines) && lines[i].kind == "text"; i++ {
				parts = append(parts, inlineHTML(lines[i].text))
			}
			i--
			b.WriteString("<p>" + strings.Join(parts, "<br>") + "</p>")
		case "item":
			tag := "ul"
			if l.marker == "ol" {
				tag = "ol"
			}
			depth := l.level + 1
			if depth > len(stack)+1 {
				depth = len(stack) + 1
			}
			switch {
			case depth > len(stack):
				b.WriteString("<" + tag + ">")
				stack = append(stack, tag)
			case depth < len(stack):
				closeLists(depth)
				b.WriteString("</li>")
			default:
				b.WriteString("</li>")
			}
			b.WriteString("<li>" + inlineHTML(l.text))
		}
	}
	closeLists(0)
	return b.String()
}

var reStripInline = strings.NewReplacer("**", "", "__", "", "`", "")

// textInline 去掉行内 Markdown 符号，链接写成「文字（地址）」。
func textInline(s string) string {
	s = reLink.ReplaceAllString(s, "$1（$2）")
	s = reStripInline.Replace(s)
	return reItalic.ReplaceAllString(s, "$1$2")
}

// MarkdownToText 转成纯文本：标题只留文字，列表用「•」与两格缩进，引用去掉「>」。
func MarkdownToText(body string) string {
	var b strings.Builder
	prevBlank, prevHeading := true, false
	for _, l := range classify(body) {
		switch l.kind {
		case "blank":
			if !prevBlank {
				b.WriteString("\n")
			}
			prevBlank = true
			continue
		case "heading":
			// 连续标题之间不空行，内容之后的标题前空一行。
			if !prevBlank && !prevHeading {
				b.WriteString("\n")
			}
			b.WriteString(textInline(l.text) + "\n")
			prevBlank, prevHeading = false, true
			continue
		case "item":
			marker := "• "
			if l.marker == "ol" {
				marker = "· "
			}
			b.WriteString(strings.Repeat("  ", l.level) + marker + textInline(l.text) + "\n")
		default:
			b.WriteString(textInline(l.text) + "\n")
		}
		prevBlank, prevHeading = false, false
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func findProject(ws *store.Workspace, idOrName string) (store.ReportProject, bool) {
	projects, err := ws.ReportProjects()
	if err != nil {
		return store.ReportProject{}, false
	}
	return store.FindReportProject(projects, idOrName)
}
