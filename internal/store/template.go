package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// TemplatesDirName 是报告模板目录（工作台根目录下，普通 Markdown，任何编辑器都能改）。
const TemplatesDirName = "templates"

// DefaultGroupTemplate 是内置的组报模板。templates/group.md 不存在时使用。
//
// 「## 栏目名」一行一个栏目；紧跟的「> 」行是给 AI 的说明；说明里含
// [产品线×类别] 的栏目按 产品线 → 类别 二维结构输出，其余栏目平铺。
const DefaultGroupTemplate = `# 组报模板

## 本周完成
> [产品线×类别] 合并组员本周完成的事项，同一事项去重，保留进度与关键数字。

## 下周计划
> [产品线×类别] 合并组员下周计划。

## 风险与求助
> 需要 SE 或其他组关注、协调的风险和求助，没有就写「无」。

## AI 使用 / AI Coding 实践经验
> 只提炼对其他组有借鉴意义的 AI 使用和 AI Coding 经验，没有就写「无」。
`

// DefaultProductTemplate 是内置的 SE 产品 / 项目周报模板。templates/product.md 不存在时使用。
// 带 [产品线×类别] 的栏目放进各产品线一章，其余栏目在最后单独成章。
const DefaultProductTemplate = `# 产品报模板

## 本周完成
> [产品线×类别] 合并各组本周完成的事项，同一事项去重，保留进度、关键数字和来源组。

## 下周计划
> [产品线×类别] 合并各组下周计划。

## 风险与求助
> 需要产品线负责人或跨组协调的风险和求助，没有就写「无」。

## AI 使用 / AI Coding 实践经验
> 各组值得推广的 AI 使用经验，没有就写「无」。
`

var templateNamePattern = regexp.MustCompile(`^[\p{Han}\w-]{1,40}$`)

// TemplatesDir 返回模板目录。
func (w *Workspace) TemplatesDir() string {
	return filepath.Join(w.Dir(), TemplatesDirName)
}

// ValidTemplateName 判断模板名是否合法（中文、字母、数字、下划线、短横线）。
func ValidTemplateName(name string) bool {
	return templateNamePattern.MatchString(name)
}

// ReadTemplate 读取模板；group 模板不存在时返回内置默认，isDefault 为 true。
func (w *Workspace) ReadTemplate(name string) (body string, isDefault bool, err error) {
	if !ValidTemplateName(name) {
		return "", false, fmt.Errorf("%w: 非法模板名 %q", ErrInvalid, name)
	}
	data, err := os.ReadFile(filepath.Join(w.TemplatesDir(), name+noteExt))
	if errors.Is(err, os.ErrNotExist) {
		switch name {
		case "group":
			return DefaultGroupTemplate, true, nil
		case "product":
			return DefaultProductTemplate, true, nil
		}
		return "", false, newReportError(ErrNotFound, "模板不存在: %s", name)
	}
	if err != nil {
		return "", false, fmt.Errorf("读取模板失败: %w", err)
	}
	return string(data), false, nil
}

// WriteTemplate 写入模板。
func (w *Workspace) WriteTemplate(name, body string) error {
	if !ValidTemplateName(name) {
		return fmt.Errorf("%w: 非法模板名 %q", ErrInvalid, name)
	}
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("%w: 模板不能为空", ErrInvalid)
	}
	if err := os.MkdirAll(w.TemplatesDir(), 0o755); err != nil {
		return fmt.Errorf("创建模板目录失败: %w", err)
	}
	return writeFileAtomic(filepath.Join(w.TemplatesDir(), name+noteExt), []byte(ensureTrailingNewline(body)))
}

// ListTemplates 返回模板名；内置的 group 总在列表里。
func (w *Workspace) ListTemplates() ([]string, error) {
	names := map[string]bool{"group": true, "product": true}
	entries, err := os.ReadDir(w.TemplatesDir())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("读取模板目录失败: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !strings.HasSuffix(e.Name(), noteExt) {
			continue
		}
		names[strings.TrimSuffix(e.Name(), noteExt)] = true
	}
	out := make([]string, 0, len(names))
	for n := range names {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}
