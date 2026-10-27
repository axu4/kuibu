# 团队日历

团队日历是只读汇总视图：按月/周/日查看组员情况，不在日历里直接改周报内容。
数据来源全部是已有内容——周报提交状态、归一稿与对照结果、会议笔记、待办截止——
汇总计算放在核心库，CLI 与 App 共用同一套数据结构。

## 两种视图

### 月视图（人 × 周）

- 行 = 组员，列 = 当月各周。
- 列的归属：按周报日（默认周四，见 `report config --weekday`）所在月份归属；
  一周跨月时，整周归到周报日所在的月，格子里同时给出周一/周日区间，
  所以 10 月的表里会出现周一起于 9 月的周。
- 格子内容：提交状态（已交 / 迟交 / 未交）、本周归一稿「本周完成」条数、
  对照上周计划中标为延期的条数、对齐率。
- 对齐率 = (完成 + 0.5 × 部分) / (计划数 − 取消)，复用 `internal/core/review.go`；
  上周没有定稿或没有计划时显示「—」，不阻塞查看。
- 空格子：该人该周不属应交范围（如尚未入组、已转出组）时格子留空，不算未交。
- 视角：默认框架组长视角（组员 × 周）；SE 视角（组长 × 周）作为切换项。
  CLI 用 `--perspective member|lead`，App 用月视图顶部的框架组长 / SE 分段切换；
  `--group` 可再按组过滤，不传则看全部组。

### 日视图（月历格）

- 标准月历：整月按整周补齐首尾格，跨月补齐格标为非本月，但仍列出当日内容。
- 每天显示：会议笔记（周会笔记）、待办截止；已取消的待办不列出。
- 每周的周报日（默认周四）有标记；周报日可在 `report config` 里改。
- 暂不做组员日报，暂不标节假日/调休。

## 点击跳转

- 月视图点格子：跳到该人该周的周报（框架组长 tab；SE 视角下跳 SE tab）。
- 日视图点会议：跳到对应笔记；点待办：跳到对应待办详情。
- 日历本身不写数据；写入（CLI 提交周报、改待办等）后 App 会自动刷新。

## CLI

```sh
kuibu calendar month [--month 2026-10] [--group g] [--perspective member|lead] [--json]
kuibu calendar day [--month 2026-10] [--json]
```

`--month` 缺省为本月，也接受 `this/last/next` 与日期。`--json` 输出结构与 App 一致：

```json
{
  "month": "2026-10",
  "group": "",
  "perspective": "member",
  "weeks": [{ "week": "2026-W40", "start": "2026-09-28", "end": "2026-10-04", "reportDay": "2026-10-01" }],
  "rows": [{
    "person": "zhangsan", "name": "张三", "group": "框架组", "role": "member",
    "cells": [{ "week": "2026-W40", "status": "submitted", "completed": 2, "delayed": 1, "alignment": 0.75 }]
  }]
}
```

- `status` 为 `submitted`（已交）/ `late`（迟交）/ `missing`（未交）；
  不属应交范围的格子 `status` 为空字符串。
- `alignment` 缺失时为 `null`，App 显示「—」。

```json
{
  "month": "2026-10",
  "reportWeekday": 4,
  "days": [{
    "date": "2026-10-29", "weekday": 4, "isReportDay": true, "isCurrentMonth": true,
    "meetings": [{ "id": "abc", "title": "周会 2026-W44（10-29）", "kind": "meeting" }],
    "todos": [{ "id": "def", "title": "上线", "kind": "todo" }]
  }]
}
```

## App

主导航新增「日历」视图（⌘4），顶部用分段切换月视图 / 日视图；
月视图内再切框架组长 / SE 视角与组过滤。Go 侧由
`ReportService.CalendarMonth / CalendarDay` 暴露给前端，计算仍在
`internal/core/calendar.go`，前端不重算。

## 实现位置

- 汇总：`internal/core/calendar.go`（`MonthCalendar`、`DayCalendar`，只读）
- CLI：`internal/cli/calendar.go`，注册在 `internal/cli/cli.go`
- App 服务：`app/calendarservice.go`；前端：`app/frontend/src/components/CalendarView.tsx`
- 测试：`internal/core/calendar_test.go`（样例工作台：跨月周、已交/未交格子、
  完成与延期计数、对齐率缺失、日视图周报日与补齐格、已取消待办不出现）、
  `internal/cli/calendar_test.go`（空工作台的 `--json` 结构）
