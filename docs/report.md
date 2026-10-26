# 周报收集（report）

周报收集是跬步的第三类 Markdown 数据，核心诉求是「粘贴 → 预览 → 确认归档」，
并在周报日（默认周四）之后按人给出已交 / 迟交 / 未交状态。

## 目录结构

```
<工作台>/
├── people/                       # 组员，平铺存放，一个组员一个文件
│   └── 20261026-100000-9b3a.md
└── reports/                      # 周报原文归档
    └── 2026-W44/                 # ISO 周编号
        ├── member/               # 组员交给组长
        │   └── <personId>.md
        ├── lead/                 # 组长交给 SE
        │   └── <personId>.md
        └── .history/             # 覆盖 / 删除前的历史，不参与索引
            └── member/
                └── <personId>-<旧 submittedAt 紧凑格式>.md
```

老工作台可能没有 `people/` 与 `reports/` 目录，`store.Init` 会创建 `people/`，
`reports/` 在首次写入时懒创建；枚举时缺目录视为空列表。

## 组员（people/<id>.md）

front matter：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | string | 组员标识，等于文件名去掉 `.md`；写回时以文件名为准 |
| `name` | string | 姓名，去空白后不能为空 |
| `aliases` | string[] | 别名列表，用于粘贴识别，空时写 `[]` |
| `group` | string | 当前所属组 |
| `productLines` | string[] | 产品线，可多个，空时写 `[]` |
| `role` | string | `member` 组员 / `lead` 组长，非法值读入按 `member` |
| `active` | bool | 在职，默认 `true` |
| `groupHistory` | object[] | 转组记录 `{group, productLines, from: YYYY-MM-DD}`，第一条是加入时 |
| `created` | RFC3339 string | 创建时间 |
| `updated` | RFC3339 string | 最后更新时间 |
| 其他任意字段 | 任意 YAML | 进 `Person.Extra`，无损保留 |

姓名与别名在**在职人员**中必须唯一：大小写不敏感、去首尾空格，冲突返回
`CONFLICT`。离职人员的姓名/别名不再参与唯一性校验，也不再参与粘贴识别。

示例：

```markdown
---
id: "20261026-100000-9b3a"
name: "张三"
aliases:
    - 小张
group: "框架组"
productLines:
    - Kuibu
role: "member"
active: true
groupHistory:
    - group: "框架组"
      productLines:
        - Kuibu
      from: "2026-10-26"
created: "2026-10-26T10:00:00+08:00"
updated: "2026-10-26T10:00:00+08:00"
---
可选备注正文
```

## 周报（reports/<周>/<层级>/<personId>.md）

front matter：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `person` | string | 提交人的组员 ID |
| `name` | string | 提交时的姓名快照 |
| `week` | string | ISO 周编号，形如 `2026-W44` |
| `level` | string | `member` / `lead` |
| `group` | string | 按该周周一时所属组的快照 |
| `productLines` | string[] | 按该周周一时产品线快照 |
| `source` | string | `paste` / `cli` |
| `submittedAt` | RFC3339 string | 最近一次提交时间 |
| `firstSubmittedAt` | RFC3339 string | 首次提交时间（覆盖时保留原值） |
| `headerDate` | string | 原文头行里的日期，可空 |
| 其他任意字段 | 任意 YAML | 进 `Report.Extra`，无损保留 |

正文是原文原样保存，只去掉首尾空行。组 / 产品线按**该周周一**时该人的
`groupHistory` 快照，因此转组后的历史周报仍归原组。

同一人同一周同一层级再次提交：覆盖当前文件，旧文件先移动到
`reports/<周>/.history/<层级>/<personId>-<旧 submittedAt 紧凑格式>.md` 保留。
覆盖时 `firstSubmittedAt` 保留原值（老文件没有该字段时用旧 `submittedAt`），
`submittedAt` 更新为最近一次提交时间。`report rm` 同样先移入 `.history`。
归档的原文文件末尾保证有且只有一个换行。

示例：

```markdown
---
person: "20261026-100000-9b3a"
name: "张三"
week: "2026-W44"
level: "member"
group: "框架组"
productLines:
    - Kuibu
source: "cli"
submittedAt: "2026-10-29T10:00:00+08:00"
firstSubmittedAt: "2026-10-29T10:00:00+08:00"
headerDate: "2026-10-29"
---
【周报】张三 2026-10-29

本周完成了核心库周报模块。
```

## ISO 周与周报日

- 周编号 `YYYY-WNN`，周一到周日；跨年按 `time.ISOWeek` 规则。例如
  `2026-12-31` 与 `2027-01-01` 都属于 `2026-W53`。
- `store.ParseWeek` 接受 `2026-W44`、`this` / `last` / `next`、`YYYY-MM-DD`。
- 周报日 = 该周第 N 天（1=周一 … 7=周日），存 `.kuibu/config.json` 的
  `reportWeekday`，缺省或 0 视为 4（周四）。CLI：`report config [--weekday 1-7]`。
- 迟交 = `firstSubmittedAt` 晚于该周周报日当天 24:00（本地时区 Asia/Shanghai）。
  即周报日 23:59 首交不迟交，次日 00:00 首交才迟交；覆盖提交不会改变首次
  提交时间，因此不会把按时首交的人改成迟交。

## 识别规则（粘贴预览）

`core.ParseBatch(text)` 把一段粘贴按【周报】头行切分成多份：

- 头行格式：`【周报】` 后跟姓名，空白后跟日期；容忍 `[周报]`、全角空格、
  日期写 `YYYY/MM/DD`、`YYYY.MM.DD`、`YYYY-M-D`；姓名与日期之间也可以没有
  空白（如 `【周报】老张2026/10/28`，姓名取到日期数字开始之前）。头行前可
  有空行。
- 每份正文从该头行开始（头行保留）到下一个头行之前；没有任何头行时整段
  当成一份（`HasHeader=false`，需要调用方指定 `--person`）。
- 头行日期非法（如 `2026-02-30`）时 `DateInvalid=true`。

`core.PreviewReports(ws, in)` 只读工作台，按**在职人员**的姓名 / 别名匹配，
给出每份的：人员、ISO 周、是否会覆盖已有提交、现在提交是否迟交、无法识别
的原因（`无头行` / `姓名不匹配` / `姓名有歧义` / `日期不合法`）。预览与写入
分离，App 的「粘贴 → 预览 → 确认写入」直接用它。

同一次粘贴里同一个人（按匹配到的 person + 周 + 层级）出现多份时，预览会把
非最后一份标记为重复（`duplicate: true` 并给出原因），写入时只写最后一份。

外部覆盖：

- `--person`（ID 或姓名 / 别名）优先于头行姓名识别，只在单份文本时允许；
- `--week` 优先于头行日期识别。

## 提交状态

`store.ReportStatus(week, level, group, productLine, now)`：

- 应交名单 = 在职、`role` 与层级对应（`member` 层级 → `role=member`，
  `lead` 层级 → `role=lead`）、按该周周一时所属组 / 产品线过滤。
- 每人状态：`submitted` 已交 / `late` 迟交 / `missing` 未交；迟交按
  `firstSubmittedAt` 判断，status JSON 同时给出 `firstSubmittedAt` 与
  `submittedAt`。
- 有提交但不在应交名单里的（含已离职但交过的人）单独放 `Extra`。
- 汇总计数：`expected` / `submitted` / `late` / `missing` / `extra` / `total`。

## CLI 用法

所有 `person` / `report` 子命令都支持 `--json` 与 `--workspace <dir>`，参数
与位置参数可混排。成功退出码 0；找不到工作台 10（`NOT_OPEN`）；找不到对象 1
（`NOT_FOUND`）；识别失败 1（`PARSE_FAILED` / `PERSON_NOT_FOUND` /
`AMBIGUOUS_PERSON`）；姓名冲突 1（`CONFLICT`）。

| 命令 | 说明 |
| --- | --- |
| `person add <姓名> [--alias a]... [--group g] [--product p]... [--role member\|lead]` | 新增组员 |
| `person list [--group g] [--role r] [--all 含离职]` | 列出组员 |
| `person show <id\|姓名>` | 查看组员 |
| `person edit <id\|姓名> [--name n] [--alias a ...\|--no-alias] [--product p ...] [--role r] [--active true\|false]` | 修改组员 |
| `person move <id\|姓名> --group g [--product p]... [--from YYYY-MM-DD]` | 转组，默认今天生效 |
| `report add [--person p] [--week W] [--level member\|lead] [--dry-run] [--file f \| --stdin]` | 归档周报原文（默认 stdin），支持一次粘贴多份 |
| `report list [--week W] [--level L] [--group g] [--search 词]` | 列出周报（默认本周，走索引） |
| `report show <week> <person> [--level L]` | 查看周报原文与元数据 |
| `report status [--week W] [--level L] [--group g] [--product p]` | 提交状态与汇总计数 |
| `report rm <week> <person> [--level L]` | 删除周报（移入 `.history`） |
| `report config [--weekday 1-7]` | 查看 / 修改周报日 |

### 示例

以下示例运行在固定时间 `KUIBU_NOW=2026-10-26T10:00:00+08:00`（周一，
2026-W44）下。

```bash
# 组员
kuibu person add 张三 --alias 小张 --group 框架组 --product Kuibu --json
# {"id":"20261026-100000-9b3a","name":"张三","aliases":["小张"],"group":"框架组","productLines":["Kuibu"],"role":"member","active":true,"groupHistory":[{"group":"框架组","productLines":["Kuibu"],"from":"2026-10-26"}],...}

kuibu person add 李四 --group 框架组 --role lead
kuibu person list --json
kuibu person show 小张          # 按别名查看
kuibu person move 张三 --group 产品组 --from 2026-11-02

# 一次粘贴多份（按【周报】头行切分）
printf '【周报】张三 2026-10-29\n完成核心库\n\n【周报】李四 2026/10/29\n完成 CLI\n' \
  | kuibu report add --json
# [{"person":"20261026-100000-9b3a","name":"张三","week":"2026-W44","level":"member","group":"框架组","source":"cli",...}, {...}]

# 预览不写入
printf '【周报】张三 2026-10-29\n完成核心库\n' | kuibu report add --dry-run --json

# 无头行时用 --person 指定（仅单份）
printf '本周完成了周报模块' | kuibu report add --person 张三 --week 2026-W44

# 状态与配置
kuibu report config --json          # {"weekday":4}
kuibu report config --weekday 5     # 周报日改成周五
kuibu report status --week 2026-W44 --json
# {"week":"2026-W44","level":"member","counts":{"expected":1,"submitted":1,"late":0,"missing":0,"extra":0,"total":1},"entries":[...],"extra":[]}

kuibu report list --week 2026-W44 --search 核心库 --json
kuibu report show 2026-W44 张三
kuibu report rm 2026-W44 张三
```

## 给 App 的 Go API 推荐

App 与 CLI 共用同一套核心库，推荐直接调用：

**周 / 解析（`internal/store`）**

- `store.WeekOf(t)` / `store.ParseWeek(s, now)`：日期 → 周编号 / 解析周表达。
- `store.WeekRange(week)`：周编号 → 周一、周日。
- `store.WeekDeadline(week, weekday)`：周报日截止时刻（次日 00:00）。

**组员（`internal/store`）**

- `ws.CreatePerson(store.NewPerson{...})`：新建组员。
- `ws.GetPerson(id)` / `ws.ListPersons()` / `ws.ResolvePerson(idOrName)`。
- `ws.UpdatePerson(id, now, func(*Person) error)`：通用修改。
- `ws.MovePerson(idOrName, group, productLines, from, now)`：转组。
- `p.MembershipAt(date)`：某人在某日期所属的组 / 产品线。

**周报（`internal/store`）**

- `ws.SubmitReport(store.SubmitReportInput{...})`：提交（自动解析人员、周，
  并按周一时快照组 / 产品线，覆盖时旧文件入 `.history`）。
- `ws.ReadReport(week, level, personID)` / `ws.ListReports(week, level)`。
- `ws.DeleteReport(week, level, personID)`：删除（入 `.history`）。
- `ws.ReportStatus(week, level, group, productLine, now)`：提交状态。
- `ws.ReportWeekday()` / `ws.SetReportWeekday(n)`：周报日配置。

**识别与预览（`internal/core`）**

- `core.ParseBatch(text)`：纯函数，按【周报】头行切分。
- `core.PreviewReports(ws, core.PreviewInput{...})`：粘贴解析 + 在职人员匹配
  + 覆盖 / 迟交提示，只读不写；App 的「粘贴 → 预览 → 确认写入」直接用它，
  确认后逐份调 `ws.SubmitReport`。

**索引与监听（`internal/index`）**

- `idx.ListReports(index.ReportFilter{...})` / `idx.SearchReports(query, limit)`。
- `idx.ListPeople()`。
- `index.Watch(ctx, ws, idx, onChange)`：`Event.PeopleIDs` / `Event.ReportIDs`
  分别携带组员与周报变化，App 可据此刷新。
