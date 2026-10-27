# 周会笔记与从笔记抽待办

## 周会笔记

每周一条周会笔记，就是普通 Markdown 笔记，front matter 多三个字段：

```yaml
type: meeting
week: 2026-W44
date: 2026-10-26   # 周会日
```

- 放在笔记树根下的「周会」节点里（没有就自动建），标题「周会 2026-W44（10-26）」。
- 周会日来自工作台配置 `meetingWeekday`（`.kuibu/config.json`，1=周一…7=周日，缺省周一）。
- 按 `type + week` 查找，已存在直接打开，**同一周不会重复建**。
- 新建时的模板：`## 议题`、`## 决定`、`## 待办` + 一个空的 `- [ ] `。暂不自动带入上周延期计划。

## 从笔记抽待办

- 任意笔记里每个**未勾选**的 `- [ ] …` 行（含嵌套）都能抽成一条待办；`- [x]` 与空行跳过。
- 标题走快速录入规则（`core.ParseQuickAdd`）识别日期、时间、`#标签`；`source` 记笔记 ID，待办详情能跳回笔记。
- 从周会笔记抽出的待办额外带 `#周会`。
- **不重复**：笔记 front matter 的 `extractedTodos` 记录「行文本哈希 → 待办 ID」，同一行再执行会跳过，可以反复执行；不往正文里写任何标记（编辑器可能丢掉 HTML 注释）。改了行文字会被当成新行。
- **先预览再写入**：CLI `--dry-run`，App 弹窗列出候选并可勾选，确认后才创建。
- AI 从自由文本里找行动项暂未实现。

## CLI

```bash
kuibu meeting open [--week W] [--json]   # 本周周会笔记（没有就建），输出笔记 ID
kuibu meeting list [--json]
kuibu note extract-todos <id> --dry-run  # 预览，列出 key
kuibu note extract-todos <id> [--only key]...
```

## App

- 笔记侧栏「全部笔记」旁的日历按钮：打开本周周会（没有就建）。
- 笔记「…」菜单 →「从此笔记抽待办」：预览、勾选、创建。
