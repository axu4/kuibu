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
        ├── member-normalized/    # 组员周报的 AI 归一稿（草稿 / 定稿）
        │   └── <personId>.md
        ├── group/                # 组报（合并组员定稿，草稿 / 定稿）
        │   ├── <组长 personId>.md
        │   └── <项目 id>/        # 配置报送项目后，每个项目一份组报
        │       └── <组长 personId>.md
        ├── triage/               # 三步流程的逐条筛选记录（只有 front matter）
        │   └── <组长 personId>.md
        ├── lead-normalized/      # 组长周报原文的归一稿（结构同组报）
        │   └── <组长 personId>.md
        ├── product/              # SE 产品 / 项目周报（每周一份）
        │   └── product.md
        └── .history/             # 覆盖 / 删除前的历史，不参与索引
            ├── member/
            │   └── <personId>-<旧 submittedAt 紧凑格式>.md
            └── member-normalized/
                └── <personId>-<旧 editedAt 或 generatedAt>.md
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

## 归一稿（reports/<周>/member-normalized/<personId>.md）

组员写法五花八门，组长没法直接合并。归一稿用 AI 把每份原文整理成统一结构，
**AI 产物只算草稿**，人工审阅、修改、定稿后才能被下一级引用和导出。

生命周期：原文（只读）→ AI 草稿 → 人工修改 → 定稿。

```markdown
---
person: "20261026-100000-9b3a"
name: "张三"
week: "2026-W44"
status: "draft"            # draft | final
sourceHash: "9f86d0…"      # 生成时原文正文的 sha256
model: "DeepSeek"          # 生成用的模型名称
generatedAt: "2026-10-29T10:00:00+08:00"
editedAt: ""               # 最近一次人工修改，未改过为空
finalizedAt: ""            # 定稿时间，未定稿为空
humanEdited: false
---
## 本周完成
- 【产品线A】完成接口联调
## 下周计划
- 【产品线A】压测
## 风险与求助
- 无
```

规则：

- **结构**：三节标题固定；每条以【领域】开头，领域自由，提示词给出该组员的产品线作为候选；同一事项去重合并。提示词模板在 `internal/core/prompts/normalize.md`。
- **解析失败不丢内容**：模型输出缺少「本周完成 / 下周计划」标题时，整段作为草稿正文保存并提示人工整理；外层代码围栏会被去掉。
- **外发确认**：组员原文发给外部大模型前必须预览确认（见 [llm.md](llm.md)）。CLI 不带 `--yes` 退出码 8 且不发请求。
- **不覆盖人工修改**：归一稿已人工修改（`humanEdited: true`）或已定稿时，再次 AI 生成返回 `9 HUMAN_EDITS_EXIST`；加 `--force` 才覆盖，覆盖前旧版移入 `.history/member-normalized/`。未改过的 AI 草稿可直接重新生成。
- **定稿后只读**：要修改先「取消定稿」（`report unfinalize`），否则报 `FINALIZED`。
- **原文已更新**：原文重新提交后 `sourceHash` 与当前原文不一致，`report status`、`draft show` 与 App 标出「原文已更新」；已定稿的归一稿保持不动，由组长决定是否重新生成。
- **下一级只认定稿**：核心库 `Workspace.FinalDraft` 对未定稿的归一稿返回 `ErrNotFinal`，CLI 退出码 `7 NOT_FINAL`。
- **索引**：归一稿与原文共用 `reports` 表和全文索引，`level` 记为 `member-normalized`，ID 为 `<周>/member-normalized/<personId>`；`report list` 不指定 `--level` 时不混入归一稿。

## 对照上周计划

把**上周定稿**归一稿的「下周计划」与**本周**归一稿的「本周完成」逐条对照。计算全部在
`internal/core/review.go`，CLI 与 App 共用。

- **数据**：上周没有定稿的归一稿 → `7 NOT_FINAL`；本周归一稿草稿或定稿均可。
- **条目 id**：「节名 + 条目文本」sha256 的前 8 位，不往正文里写隐藏注释。上周计划已定稿，id 稳定；本周完成项改了文字后，旧的对应关系会失效并显示为未对应，需要重新连线。`- 无` 不算条目。
- **状态**：完成 `done` / 部分 `partial` / 延期 `delayed` / 取消 `canceled`；没对应到任何计划的本周完成项标「计划外」。
- **对齐率** = (完成 + 0.5 × 部分) / (计划数 − 取消)；未标状态的计划算 0；分母为 0 时不显示。
- **存储**：结果写在**本周归一稿 front matter 的 `review` 字段**，只改 front matter，不改正文、`status`、`humanEdited`，所以不影响定稿：

  ```yaml
  review:
    items:
      - plan: "a1b2c3d4"       # 上周计划 id
        status: "partial"
        matches: ["e5f60718"]  # 对应的本周完成 id
        note: "压测一半"
        source: "human"        # ai = AI 建议、未经人工确认
    carried: ["a1b2c3d4"]      # 已顺延的计划
  ```

- **顺延**：把「部分 / 延期」的计划追加到本周「下周计划」，末尾加「（顺延）」，去掉该节的「- 无」；已顺延或文本已存在的不重复加，可重复执行。顺延会改正文，所以本周归一稿已定稿时要先取消定稿。
- **AI 建议**：把上周计划和本周完成（带 id）发给模型，要求返回 JSON `[{plan, status, matches}]`，写入时 `source: ai`；人工标过的条目不覆盖。外发先预览确认。

## 组报（reports/<周>/group/<组长 personId>.md）

组长把本组组员**定稿**的归一稿合并成组报，定稿后一键带入 SE tab。核心逻辑在
`internal/core/group.go`。

- **组长与组员**：组长 = 名单里角色为组长的人（`--group` 不填时取唯一有组长的组）；组员 = 该周周一时在这个组、角色为组员的在职人员。没有组长时提示先把自己加进名单。
- **只用定稿**：有组员没定稿 / 没交时默认返回 `7 NOT_FINAL` 并列出名单；`--allow-missing`（App「仍然生成」）才生成，并在组报开头标「未纳入：xxx」，front matter 记 `missing`。
- **结构由代码保证**：AI 只把条目归类、提炼措辞，返回 JSON `[{section, productLine, category, text, sources, pending}]`；代码渲染为 `## 栏目` → `### 产品线` → `#### 类别` → `- 事项（来源组员）`，空类别不输出，判不准的加「（待确认）」。类别固定：功能开发 / 客户支持 / 功能优化 / 项目管理；产品线取组员名单的 productLines，归不进去的放「其他」，类别不认识的放「未分类」。JSON 解析失败时整段保存为草稿并提示。
- **front matter**：同归一稿，另有 `group`、`sources`（纳入的组员 ID）、`missing`。`sourceHash` 是纳入组员定稿正文的合并哈希，组员定稿内容再变时标「组员定稿已更新」。
- **生命周期**同归一稿：人工修改 / 已定稿再生成返回 `9`，`--force` 覆盖且旧版进 `.history/group/`；定稿后只读。
- **带入 SE**：`report group submit` 把定稿组报以组长本人身份写入该周 `lead` 层级，`source: group-report`，与手动粘贴效果一致；重复带入走现有覆盖 + `.history` 规则。未定稿返回 `7`。
- **索引**：与归一稿一样复用 `reports` 表（`level = group`）。

### 模板（templates/<名称>.md）

模板是工作台根目录 `templates/` 下的普通 Markdown，CLI、App 和任何编辑器都能改；组报默认用 `group`，文件不存在时用内置默认模板。

```markdown
## 本周完成
> [产品线×类别] 合并组员本周完成的事项，同一事项去重，保留进度与关键数字。

## 下周计划
> [产品线×类别] 合并组员下周计划。

## 风险与求助
> 需要 SE 或其他组关注、协调的风险和求助，没有就写「无」。

## AI 使用 / AI Coding 实践经验
> 只提炼对其他组有借鉴意义的 AI 使用和 AI Coding 经验，没有就写「无」。
```

- `## 栏目名` 一行一个栏目，可增删、改名、排序；紧跟的 `> ` 行是给 AI 的说明。
- 说明里含 `[产品线×类别]` 的栏目按二维结构输出，其余栏目平铺。

## 产品 / 项目周报（SE，reports/<周>/product/product.md）

SE tab 把各组长**定稿**的周报按产品线汇总。与框架组长 tab 完全分开计算。核心逻辑在
`internal/core/product.go`，存储、生命周期、合并请求与渲染都和组报共用。

- **组长原文先归一**：`report normalize --level lead` 把组长原文归一到 `reports/<周>/lead-normalized/`，结构同组报（`## 栏目` → `### 产品线` → `#### 类别`，模板用 `group`），生命周期、7 / 9 规则同组员归一稿；`report draft show|edit`、`report finalize|unfinalize` 加 `--level lead`。
- **组报带入的直接用**：组长经组报带入（`source: group-report`）的周报本身就是定稿，不再归一；`report status --level lead` 显示「定稿（组报）」，批量归一时跳过。
- **只用定稿**：有组长未定稿默认返回 `7 NOT_FINAL` 并列出（显示为「组名（组长）」）；`--allow-missing` 才生成并在开头标「未纳入」。
- **结构**：一份报告按产品线分章：`## 产品线` → `### 栏目` → `#### 类别` → `- 事项（来源组）`；模板里不带 `[产品线×类别]` 的栏目（如 风险与求助）放在最后单独成章。产品线取名单里所有在职人员的 productLines，没有条目的产品线不输出。
- **模板**：`templates/product.md`，不存在时用内置默认（同组报四栏，措辞面向 SE）。
- **front matter**：同组报，`sources` 为纳入的组长 ID，`missing` 为未纳入的组；组长定稿内容变化后标「来源已更新」。
- `report product show --product <产品线>` 只输出那一章。

## 月报（reports/<YYYY-MM>/）

月报结构同周报，只引用定稿。核心逻辑在 `internal/core/month.go`。

- **月与周**：一周按**周报日**（默认周四）所在月份归属，跨年同理（如 2026-W01 的周四是 2026-01-01，归 2026-01）。`WeeksOfMonth` / `MonthOfWeek` 有跨月、跨年单测。
- **收集日**：组长月报每月 `reportMonthDay` 日收（工作台配置，缺省 28；`report config --month-day N`），超过当月天数按月末，**不因周末顺延**；晚于收集日 24:00 算迟交。
- **目录**（与周目录并列，周列表只认 `YYYY-Www` 目录，不会混入）：

  ```
  reports/2026-10/
  ├── lead-month/<组长 id>.md             # 组长月报原文（粘贴归档，覆盖进 .history）
  ├── lead-month-normalized/<组长 id>.md  # 组长月报归一稿（结构同组报）
  ├── group/<组长 id>.md                  # 组月报
  └── product/product.md                  # 产品月报
  ```

- **组月报**（框架组长）：合并当月各周**定稿组报**，结构同组报；缺周 / 未定稿默认返回 `7` 并列出，`--allow-missing` 才生成并标注。来源是各周，周组报定稿内容变化后标「来源已更新」。
- **产品月报**（SE）：合并各组长月报（归一定稿）+ 当月各周定稿产品报，按产品线分章；缺的同样列出。
- 生命周期、`9` / `7`、外发预览确认同周报；索引复用 `reports` 表（week 列存 `2026-10`），`report list` 默认不混入。

## 导出定稿

组报、产品报定稿后可以导出（核心逻辑 `internal/core/export.go`，CLI 与 App 共用）。

- **只认定稿**：未定稿导出 / 复制一律返回 `7 NOT_FINAL`；App 里「导出 Markdown」「复制为飞书格式」在未定稿时置灰。导出内容是定稿正文，不含 front matter。
- **格式**：`md`（原文）、`feishu-html`（HTML）、`text`（纯文本：标题只留文字，列表用「•」与两格缩进，链接写成「文字（地址）」）。
- **Markdown → HTML** 由自写的小转换器完成（不引入第三方库），覆盖标题、嵌套 / 有序列表、引用、段落、加粗、斜体、行内代码、链接，并转义 HTML。只服务于本项目生成的报告，不追求完整 CommonMark。
- **文件名**：`<组名>-组报-<周>.md`、`产品报-<周>.md`、`<组名>-组月报-<月>.md`、`产品月报-<月>.md`。
- **复制为飞书格式**：前端用 `navigator.clipboard.write` 同时写 `text/html` 与 `text/plain`，飞书粘贴取 HTML；WKWebView 不支持时退回纯文本并提示。只进剪贴板，不发送到任何地方。

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

### 按组汇总

App 的 `ReportService.Status` 在整体计数之外，另外给出按组的进度
`Groups []ReportGroupCounts`（`json:"groups"`）：

| 字段 | 说明 |
| --- | --- |
| `group` | 组名；空组名表示未分组 |
| `expected` | 该组应交人数 |
| `submitted` | 该组按时已交人数 |
| `late` | 该组迟交人数 |
| `missing` | 该组未交人数 |

- **只统计应交名单**（`Entries`）：名单外（`Extra`）的人不进任何一组，
  因此各组 `expected` 之和等于整体 `expected`。
- 每组满足 `expected = submitted + late + missing`。
- 排序：组名升序，**未分组（空组名）排最后**；空名单时是不含元素的数组，
  不是 `null`。
- 注：应交名单本身只含「该周周一时所属组非空」的在职人员，所以没填组名的人
  不会出现在按组汇总里。
- App 界面在提交状态上方按组展示「组名 + 已交/应交 + 进度条 + 迟交/未交」，
  空组名显示为「未分组」。

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
| `report normalize [--week W] [--level member\|lead] (--person p... \| --all) [--model m] [--dry-run \| --yes] [--force]` | AI 归一组员周报为草稿；不带 `--yes` 只输出完整预览并退出 8；`--all` 跳过已修改 / 已定稿的人并最终退出 9 |
| `report draft show <week> <person>` | 查看归一稿（`--json` 含 `sourceChanged`） |
| `report draft edit <week> <person> [--file f]` | 保存人工修改（默认读 stdin），标记 `humanEdited` |
| `report finalize <week> <person>` / `report unfinalize <week> <person>` | 定稿 / 取消定稿 |
| `report review <week> <person>` | 对照上周计划：状态、对应关系、计划外、对齐率 |
| `report review set <week> <person> <计划id> --status done\|partial\|delayed\|canceled [--match id]... [--note 文本]` | 人工标一条计划（`--status ""` 清除） |
| `report review suggest <week> <person> [--yes \| --dry-run]` | AI 建议对应关系与状态（不带 `--yes` 只预览，退出 8） |
| `report carry <week> <person> [--items id]...` | 把「部分 / 延期」计划顺延到本周下周计划，可重复执行 |
| `report group generate [--week W] [--group g] [--template t] [--allow-missing] [--yes \| --dry-run] [--force]` | AI 合并组员定稿为组报草稿；有人未定稿返回 7；不带 `--yes` 只预览，退出 8 |
| `report group show\|edit\|finalize\|unfinalize [--week W] [--group g]` | 查看 / 修改（stdin 或 `--file`）/ 定稿 / 取消定稿组报 |
| `report group submit [--week W] [--group g]` | 定稿组报带入 SE tab（组长本人 lead 层级）；未定稿返回 7 |
| `report product generate [--week W] [--template t] [--allow-missing] [--yes \| --dry-run] [--force]` | SE 按产品线汇总组长定稿；有组长未定稿返回 7；不带 `--yes` 只预览，退出 8 |
| `report product show [--product p]\|edit\|finalize\|unfinalize [--week W]` | 查看（可只看一章）/ 修改 / 定稿 / 取消定稿产品报 |
| `report export group\|product\|month-group\|month-product [--week W \| --month M] [--group g] [--format md\|feishu-html\|text] [--out file]` | 导出定稿，不给 `--out` 输出到 stdout；未定稿返回 7 |
| `report month generate\|show\|edit\|finalize\|unfinalize [--month M] [--level group\|product] [--allow-missing] [--yes \| --dry-run] [--force]` | 组月报（group）/ 产品月报（product） |
| `report add --month M` / `report status --month M` | 组长月报归档 / 提交状态 |
| `report normalize --month M --all` | 归一组长月报（finalize / draft 用 `<月份> <人> --level lead-month`） |
| `report config [--weekday N] [--month-day N]` | 周报日 / 月报收集日 |
| `template list\|show\|edit <名称>` | 报告模板（`templates/<名称>.md`，edit 读 stdin） |

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

## App 界面

App 侧栏第三个视图「周报」，服务是 `app/reportservice.go`（`ReportService`），
前端在 `app/frontend/src/components/ReportView.tsx`、`ReportPaste.tsx`、
`PeopleManager.tsx`。

- `ReportService.Status(week, level, group, productLine)` 返回 `ReportStatusData`，
  其中 `Groups` 是按组汇总的应交进度（见上文「按组汇总」），前端用
  `lib/report.ts` 的 `groupLabel` / `groupProgressPercent` 渲染。
- `ReportService.Groups()` 返回名单里出现过的组名（去重排序），供筛选下拉用；
  它与 `Status().Groups` 不同，后者带计数。

- 顶部周切换器：`‹ 2026-W44 · 10/26–11/01 ›`，非本周显示「回到本周」；旁边
  小字提示周报日（如「周四截止」）。
- 两个分段 tab：「框架组长」（`level=member`，收组员周报）与「SE」
  （`level=lead`，收各组长周报），记住上次选择。
- 统计行：应交 / 已交 / 迟交 / 未交；主操作「粘贴周报」，更多菜单里放「复制
  催交话术」「管理名单」「设置周报日」。
- 人员列表：框架组长 tab 按组分小节；SE tab 按产品线分组（一个组长多条产品线
  会在每个产品线下都出现，没有产品线归「未分产品线」），名单外提交放末尾
  「名单外」小节。每行显示姓名、状态（已交/迟交/未交的克制色点 + 文字）与
  提交时间（如「周四 18:20」）。
- 点开一行是右侧原文详情面板（只读、保留换行）；已交的显示原文，可「重新粘贴」
  （预设此人）或「删除」（确认后移入 `.history`）；未交的显示空状态与「粘贴」
  按钮（预设此人）。
- 详情面板是**浮层**（`position: absolute` 叠在主内容右侧），打开时主列表宽度不变，
  不会被挤压变形。拖左边缘可调宽度（360 ~ 960px，最多占窗口 85%），双击恢复
  默认 420px；宽度记在 localStorage 的 `kuibu.report.detailWidth`。
- 粘贴流程：大文本框 → 「识别」（`ReportService.Preview`，只读）→ 预览列表
  →「确认写入 N 份」（`ReportService.Submit`，source 记为 paste）。
- **粘贴分两种入口**：
  - **锁定某人**（收周报时的主路径）：未交那一行右侧直接有「粘贴」，点开就是
    「粘贴 张三 的周报」，不再出现人员下拉——从哪一行进来就归谁；也可先点中
    某人再按 **⌘V**，剪贴板内容直接带进弹窗。锁定后开头那行
    「【周报】姓名 日期」由 `PreviewReports` 剥掉，只归档正文（正文为空时保留原样）。
  - **不限人员**（「粘贴周报」按钮）：可一次贴多份，按头行自动认人；
    预览里每一份都有人员下拉，可改、可跳过，标记覆盖/迟交/重复与无法识别原因。
- **「识别」后是否还要点一次「确认写入」**：只有存在需要人判断的分支时才停。
  锁定某人 + 只交一份 + 不覆盖 + 不迟交 + 无重复 + 周一致时，识别完直接归档，
  弹窗关闭，底部提示「已归档「张三」的周报 · 2026-W44」并给 8 秒 **撤销**
  （撤销走 `ReportService.Delete`，旧文件照常进 `.history`）。
  其余情况仍给预览，并在预览下方写明原因（如「请确认：会覆盖 张三 已有的周报」），
  按钮随之变成「覆盖写入」。
- 复制催交话术只写剪贴板（`navigator.clipboard`），绝不发消息；未交为 0 时菜单
  项显示「都交齐了」。
- 组员名单弹窗：在职平铺、离职折叠，可新增（姓名、别名、组、产品线、角色）、
  编辑、转组（新组 + 生效日期）、设为离职/恢复。
- 前端监听 `report:changed` 事件自动刷新（App 自己写入或 CLI 外部改文件都会
  触发），刷新不打断正在打开的粘贴弹窗。

## 三步流程与按项目组报（2026-10 新增）

配置了 `reportProjects` 的工作台，可以在组报面板走「筛选 → 项目分拣预览 → 分别整理各项目
周报」三步：

- 筛选记录：`reports/<周>/triage/<组长>.md`，只有 front matter；每条记 `decision`
  （accepted / rejected / skipped）与 `project`。
- 按项目组报：`reports/<周>/group/<项目 id>/<组长>.md`，格式与普通组报相同，按项目独立
  保存 / 定稿 / 导出；正文按「本周总结 / 下周计划 / 风险与求助」分节，不带姓名。
- 索引里按项目组报的 `level = group/<项目 id>`，`report list` 不指定 level 时默认仍只列
  member / lead。
- 筛选**优先读组员周报原文**，按标题/一行一条拆条目；原文拆不出时才退回已有归一稿。不要求先逐人定稿，也不需要配置 AI；组报定稿仍是导出/带入 SE 的闸门。
- 项目配置在 `.kuibu/config.json` 的 `reportProjects`，最多 4 个；设计说明见
  [design/report-triage.md](design/report-triage.md)。
