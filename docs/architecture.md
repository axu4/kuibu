# 架构

## 总览

```
             ┌──────────────┐        ┌──────────────┐
             │  app/ (Wails)│        │ cmd/kuibu    │
             │  React + TS  │        │  CLI 入口    │
             └──────┬───────┘        └──────┬───────┘
                    │                       │
                    └──────────┬────────────┘
                               ▼
                        internal/cli       命令、--json、退出码
                               │
              ┌────────────────┼────────────────┐
              ▼                ▼                ▼
        internal/store   internal/index   internal/config
        Markdown 读写     SQLite 索引      用户配置/工作台定位
              │                │
              ▼                ▼
        notes/*.md       .kuibu/index.db
```

- **根 module `github.com/axu4/kuibu`**：核心库（`internal/...`）+ CLI（`cmd/kuibu`）。不依赖 Wails，可在 Linux 构建和测试。
- **`internal/store`**：工作台初始化/打开、笔记 Markdown + YAML front matter 读写、树操作、资源归档、原子写。
- **`internal/index`**：SQLite 索引（`modernc.org/sqlite`，纯 Go，无 cgo）、FTS5 检索、fsnotify 文件监听。
- **`internal/config`**：用户配置（默认工作台）与工作台定位，CLI 与 App 共用同一套优先级。
- **`internal/core`**：领域模型与业务逻辑的预留位置（当前为空包）。
- **`internal/cli`**：子命令实现、`--json` 输出、退出码语义；`cmd/kuibu/main.go` 只负责把 `os.Args` 转给 `cli.Run` 并 `os.Exit`。
- **`app/`**：独立 Go module `github.com/axu4/kuibu/app`，Wails v3 桌面外壳，通过 `replace github.com/axu4/kuibu => ../` 复用根模块的 `internal/*`。前端 React/TypeScript 放 `app/frontend`。

CLI 是唯一正式读写接口；AI 一律经 CLI 读写数据，不直接改文件或数据库。App 只是同一套核心库的另一个调用方。

## 数据流

**写入（App 或 CLI → 文件 → 索引）**

1. 调用方（App 的 `WorkspaceService` 或 CLI 命令）调 `internal/store`。
2. `store` 只改 Markdown 文件：同目录临时文件 + `rename` 原子替换（`writeFileAtomic`），front matter 与正文一起写。
3. 文件变化被 `index.Watch` 的 fsnotify 捕获，200ms 防抖合并后 `UpsertFile` / `RemoveFile` 增量更新索引。
4. App 侧 `Watch` 的回调把变化 ID 通过 Wails 事件 `workspace:changed`（`{ ids: string[] }`）发给前端，前端刷新树与当前笔记。自己保存也会触发该事件，前端比较正文与本地是否一致来忽略。

**外部编辑（编辑器、Finder、其他工具改文件）**

1. `notes/` 目录出现 Create/Write/Remove/Rename 事件。
2. 防抖后按路径增量同步索引；文件已删除则删除索引项。
3. 索引变化 → `workspace:changed` → App 刷新界面。索引本身从不回写 Markdown。

索引只是缓存：删掉 `.kuibu/index.db` 后，下次 `index rebuild` 或 `Sync` 从 Markdown 全量重建，数据不丢。

## 工作台目录结构

```
<工作台>/
├── notes/                        # 笔记，平铺存放，一条笔记一个文件
│   ├── 20240101-120000-abcd.md
│   └── ...
├── todos/                        # 待办，平铺存放，一条待办一个文件
│   ├── 20261026-100000-19ae.md
│   └── ...
├── people/                       # 组员，平铺存放，一个组员一个文件
│   └── 20261026-100000-9b3a.md
├── reports/                      # 周报，按周/层级归档（.history 为覆盖/删除历史）
│   └── 2026-W44/
│       ├── member/person.md
│       └── .history/member/person-20261029-100000.md
├── assets/                       # 图片等资源，扁平存放
│   └── 20240101-1a2b3c4d-截图.png
└── .kuibu/                       # 元数据（不参与笔记树）
    ├── config.json               # {"version":1,"reportWeekday":4}
    └── index.db                  # SQLite 索引（可删可重建）
```

- 工作台的判定标准：目录下存在 `.kuibu/config.json`。
- `init` 要求目标目录不存在或为空；已是工作台返回 `ALREADY_INIT`，非空且不是工作台返回 `INVALID`。
- 枚举笔记时跳过隐藏文件（`.xxx.md`、编辑器临时文件）和非 `.md` 文件。

## front matter 字段

每条笔记是一个 Markdown 文件，文件名为 `<id>.md`，元数据放文件头部的 YAML front matter：

```markdown
---
id: "20240101-120000-abcd"
title: "今日记录"
type: "note"
created: "2024-01-01T12:00:00+08:00"
updated: "2024-01-01T12:30:00+08:00"
tags:
  - 工作
parent: ""
order: 0
---
正文……
```

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | string | 笔记标识，等于文件名去掉 `.md`。写回时总以文件名为准，front matter 里的 `id` 读入时被忽略 |
| `title` | string | 标题；缺失时取正文第一个 `# ` 标题，再退化到文件名（ID） |
| `type` | string | 笔记类型，默认 `note`；`meeting` 为周会笔记（另有 `week`、`date`，见 [meeting.md](meeting.md)）；`todo`、`weekly` 等预留 |
| `created` | RFC3339 string | 创建时间，保留原时区；缺失时取文件 mtime |
| `updated` | RFC3339 string | 最后更新时间，由 `store` 在每次写回时刷新 |
| `tags` | string[] | 标签列表，空时写 `[]` 而非 `null` |
| `parent` | string | 父笔记 ID，空字符串表示根节点 |
| `order` | int | 同级排序序号，从 0 开始 |
| 其他任意字段 | 任意 YAML | 未知字段进 `Note.Extra`，**无损保留**：值未被修改时复用原始 YAML 节点写回（保留 flow 风格等排版），被修改时按新值重新编码，键按字母序追加在标准字段之后 |

设计要点：

- **未知字段无损保留**，外部编辑器手加的字段、后续待办/周报要加的扩展字段（如 `status`、`due`、`week`）都不会被覆盖丢失，不需要先改 schema。
- **容错**：front matter 解析失败或不是映射时，整个文件按正文处理，不报错；没有 front matter 的外部 Markdown 也能读入。
- 标准字段按固定顺序写入，`created`/`updated` 一律双引号字符串，避免 YAML 把时间解析成非字符串类型。

组员（`people/<id>.md`）与周报（`reports/<周>/<层级>/<personId>.md`）的 front matter
字段表见 [report.md](report.md)。

## 待办

待办与笔记一样是工作台里的 Markdown 文件，平铺放在 `todos/` 目录，一条待办一个文件，文件名为 `<id>.md`。待办之间也通过 `parent` + `order` 形成父子层级（子任务），不单独建表。

### 文件格式

完整示例：

```markdown
---
id: "20261026-100000-19ae"
title: "交周报"
type: "todo"
status: "doing"
due: "2026-10-30 15:00"
created: "2026-10-26T10:00:00+08:00"
updated: "2026-10-26T11:00:00+08:00"
done_at: ""
tags:
  - 工作
  - 周报
parent: ""
order: 0
links:
  - url: "https://example.com"
    title: "示例"
attachments:
  - ../assets/20261026-abcdef12-截图.png
source: ""
---
详情正文……
```

字段表：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | string | 待办标识，等于文件名去掉 `.md`；写回时以文件名为准 |
| `title` | string | 标题，去空白后不能为空 |
| `type` | string | 固定 `todo` |
| `status` | string | `todo` / `doing` / `done` / `canceled`，非法值读入时按 `todo` 处理 |
| `due` | string | 截止，格式 `YYYY-MM-DD` 或 `YYYY-MM-DD HH:MM`，空字符串表示无截止 |
| `created` | RFC3339 string | 创建时间 |
| `updated` | RFC3339 string | 最后更新时间，store 每次写回时刷新 |
| `done_at` | RFC3339 string | 完成时间，仅 `done` 时有值，其余状态为空字符串 |
| `tags` | string[] | 标签列表，空时写 `[]` |
| `parent` | string | 父待办 ID，空字符串表示顶层 |
| `order` | int | 同级排序序号，从 0 开始 |
| `links` | object[] | 链接列表，每项 `{url, title}`，`title` 可空 |
| `attachments` | string[] | 附件相对路径，形如 `../assets/<文件名>` |
| `source` | string | 来源笔记 ID，可空 |
| 其他任意字段 | 任意 YAML | 未知字段进 `Todo.Extra`，无损保留（规则同笔记） |

### 状态与 done_at

- 四种状态：`todo`（未开始）、`doing`（进行中）、`done`（已完成）、`canceled`（已取消）。
- `done_at` 只在 `done` 时有值：首次从非 `done` 转为 `done` 时设置为当前时间；再次设为 `done` 不覆盖已有完成时间；转成非 `done` 时清空。
- `store.UpdateTodo` 统一维护 `updated` 与 `done_at`，业务代码不要直接写这两个字段。

### due 格式与时区

- `due` 的日期部分必填，时间部分可空；无时间时截止时刻取当天 `23:59:59`。
- 日期语义一律使用 `store.TodoLocation()`（Asia/Shanghai），系统缺时区数据时回退固定偏移 CST（UTC+8）。
- `store.ParseDue` 接受 `YYYY-MM-DD`、`YYYY-MM-DD HH:MM`、`YYYY-MM-DDTHH:MM` 与 RFC3339；RFC3339 先转上海时区再取日期与时间。
- 索引里 `due_at` 是解析后的截止时刻（RFC3339），`--due overdue` 用它比较。

### 子任务 = parent

- 子任务就是把 `parent` 指向另一条待办的 ID，与笔记树共用同一套 `parent` + `order` 方案。
- 移动到自身或后代返回 `CYCLE`；删除有子任务的待办需要 `-r/--recursive`，否则返回 `HAS_CHILDREN`。
- 待办错误使用专用文案：`待办不存在: <id>`、`父待办不存在: <id>`、`待办仍有子任务: <id>（如需连同子任务删除请加 -r）`、`移动会导致待办父子关系成环: <id>`。它们由 `store` 内的私有包装错误返回，`Unwrap` 仍指向 `ErrNotFound/ErrHasChildren/ErrCycle` 等哨兵，因此 `errors.Is` 与 CLI 错误码分类（`NOT_FOUND/HAS_CHILDREN/CYCLE`）不变。
- `todo list --tree` 的根节点按 `index.ListTodos` 的默认排序（未完成在前、逾期按截止升序等），子任务在父下按 `order,id` 排序。

### 链接与附件

- 链接必须含 scheme（http/https/mailto 等），同 URL 只更新标题不重复添加。
- 附件经 `store.AddAsset` 去重复制进 `assets/`，列表存相对待办文件的 `../assets/<文件名>`；移除附件只改列表，不删资源文件。

### source

`source` 保存来源笔记 ID，用于「从笔记创建待办」等场景；普通手工录入为空字符串。

### 索引表与过滤

待办索引表 `todos` 在 `notes` 之外单独维护，含 `status/due_date/due_time/due_at/done_at/parent/ord/tags/links/attachments/source` 等列，另有 `todos_fts`（FTS5 trigram）。`index.ListTodos(TodoFilter)` 支持：

- `Statuses`：状态白名单；
- `Tags`：全部包含；
- `Parent`：父待办过滤（`""` 表示顶层）；
- `DueFrom/DueTo`：截止日期闭区间；
- `OverdueAt`：未完成且截止时刻早于该时刻；
- `Query`：标题与正文检索（≥3 字走 FTS，否则 LIKE 回退）。

### watcher 的 TodoIDs 事件

`index.Watch` 同时监听 `notes/`、`todos/`、`people/` 与 `reports/`（`.history/` 忽略）。事件 `Event` 中 `Changes/IDs` 只含笔记变化，`TodoChanges/TodoIDs` 只含待办变化，`PeopleChanges/PeopleIDs` 只含组员变化，`ReportChanges/ReportIDs` 只含周报变化，互不混入，App 可据此分别刷新。

## 组员

组员平铺放在 `people/` 目录，一个组员一个文件，文件名为 `<id>.md`。front matter：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `name` | string | 姓名 |
| `aliases` | string[] | 别名列表，用于粘贴识别 |
| `group` | string | 当前所属组 |
| `productLines` | string[] | 产品线，可多个 |
| `role` | string | `member` 组员 / `lead` 组长 |
| `active` | bool | 在职，缺省 true |
| `groupHistory` | object[] | 转组记录 `{group, productLines, from: YYYY-MM-DD}`，第一条是加入时 |
| `created` / `updated` | RFC3339 string | 创建/更新时间 |
| 其他任意字段 | 任意 YAML | 进 `Person.Extra`，无损保留 |

姓名与别名在在职人员中唯一（大小写不敏感、去首尾空格），冲突返回 `CONFLICT`。

## 周报

周报原文归档在 `reports/<周>/<层级>/<personId>.md`，层级 `member`（组员交给组长）与 `lead`（组长交给 SE）。front matter：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `person` | string | 提交人组员 ID |
| `name` | string | 提交时姓名快照 |
| `week` | string | ISO 周编号，如 `2026-W44` |
| `level` | string | `member` / `lead` |
| `group` | string | 按该周周一时所属组的快照 |
| `productLines` | string[] | 按该周周一时产品线快照 |
| `source` | string | `paste` / `cli` |
| `submittedAt` | RFC3339 string | 提交时间 |
| `headerDate` | string | 原文头里的日期，可空 |

正文为原文原样保存（只去首尾空行）。同一人同周同层级再次提交时，旧文件先移入 `reports/<周>/.history/<层级>/<personId>-<旧 submittedAt 紧凑格式>.md`。周报日存 `.kuibu/config.json` 的 `reportWeekday`（1-7，缺省 4），迟交 = `submittedAt` 晚于周报日当天 24:00。

## 树存储方案

树不存在单独的文件里，而是完全由每条笔记 front matter 的 `parent` + `order` 推导：

- 文件平铺在 `notes/` 下，目录层级不承载树结构。
- `parent` 为空 → 根节点；同级按 `order` 升序、`order` 相同再按 `id` 排序。
- **移动不改文件名**：`Move` 只改被移动笔记的 `parent`，并对受影响的两个同级列表重新编号 `order`（0,1,2…），只重写 `order` 实际变化的文件。
- 同级重排只在同一列表内重编号；跨层级移动先落盘 `parent`，再分别重编号旧列表和新列表。
- 移动到自身或自己的后代下返回 `CYCLE`，由 `isDescendant` 沿 parent 链检查。
- **孤儿**：`parent` 指向不存在的笔记时，节点作为根节点返回并标记 `orphan`（CLI `tree` 会带标记）。
- **环**：外部手改可能造出父子环。`Tree` 用「沿 parent 链向上走 + 记录路径位置」找出环上节点，把它们降为根节点并标记 `cycle`，不会死循环。
- 删除有子节点的笔记需要 `-r/--recursive`，否则返回 `HAS_CHILDREN`；递归删除按 BFS 收集全部后代。
- **目录 + 叶子笔记**：只有 `type=folder` 的目录能包含子节点，目录不能有正文。`Create` / `Move` 在父节点非空时校验它是目录，否则返回 `INVALID`（父节点不存在返回 `NOT_FOUND`）；创建目录时带正文、或给目录写入正文同样返回 `INVALID`。随手记收件箱、周会节点也是目录（老收件箱 `type=inbox` 由 `quicknote.EnsureInbox` 就地升级）。
- **迁移**：`Workspace.MigrateFolders()` 把「有子节点但不是目录」的笔记就地改成 `type=folder`（ID、标题、顺序、子节点、正文都不变），幂等。CLI 侧对应 `kuibu note migrate-folders`，JSON 输出 `{"converted":N}`；App 打开工作台时也会自动跑一次。

选择理由：

1. **文件即事实来源**：单文件就能自解释位置，用编辑器或脚本搬动/改写单条笔记时不会与另一份树文件冲突。
2. **无索引也能读**：删掉 `index.db` 后仅靠 front matter 就能完整重建树。
3. **移动代价低**：只改一个字段 + 少量同级重编号，不移动/重命名文件，避免链接、外部引用、同步工具失效。
4. **天然可扩展**：树结构对 `todo`、周报等所有类型统一适用，无需按类型分表。

### CLI 里的目录（`internal/cli`）

- `kuibu note new --folder [--title t] [--parent id]`（`-d` 为简写）等价于 `--type folder`；建目录时忽略 `--body/--file/--stdin`（也不读 stdin），标题可省略、空标题由 store 补成「未命名目录」。`--folder` 与非 `folder` 的 `--type` 同时给出属于参数冲突，按 `USAGE` 退出。
- `note list` 人类模式多一列「类型」，目录显示「目录」，其余显示原始 `type`；`note list --json` 本就带 `type` 字段，目录为 `"folder"`。
- `tree` 人类模式给目录行加 ` [目录]`（在 `(id)` 之后、`[孤儿]`/`[环]` 之前），`tree --json` 每个节点带 `type`。
- `note mv --parent` 只能指向目录：指向普通笔记返回 `INVALID`，指向自己的后代返回 `CYCLE`。
- 以上错误都沿用既有退出码 `1`，不新增退出码。

### App 里移动笔记

- 拖拽（原生 HTML5，无依赖）：拖到行的上 1/4 = 放到它前面、下 1/4 = 放到它后面、中间 = 成为子节点；拖到树下方空白 = 移到根层级末尾。拖到自己或后代上不接受（与 `ErrCycle` 一致）。
- 键盘（选中笔记、焦点不在编辑器里）：⌥⌘↑/↓ 同级上下移，⌥⌘← 升级到父节点后面，⌥⌘→ 成为上一个兄弟的末尾子节点。
- 位置计算在 `app/frontend/src/lib/tree.ts`，结果交给 `WorkspaceService.Move(id, parent, pos)` → `store.Move`，只改 `parent / order`。

## 索引设计

- **SQLite，`modernc.org/sqlite`（纯 Go）**：不用 cgo，Linux/macOS 都能直接交叉构建，CI 不需要 C 工具链。
- 位置：`<工作台>/.kuibu/index.db`；连接参数 `journal_mode(WAL)`、`busy_timeout(10000)`、`synchronous(NORMAL)`，允许 App 与 CLI 同时访问；进程内 `SetMaxOpenConns(1)` 加互斥锁串行化写操作。
- 表：
  - `notes(id PK, title, type, parent, ord, tags JSON, created, updated, path, mtime, size, extra JSON)`，索引 `(parent, ord, id)` 与 `path`。
  - `notes_fts`：FTS5 虚表 `(id UNINDEXED, title, body)`，`tokenize='trigram'`。
  - `todos(id PK, title, status, due_date, due_time, due_at, done_at, parent, ord, tags JSON, links JSON, attachments JSON, source, created, updated, path, mtime, size, extra JSON)`，索引 `(parent, ord, id)`、`(status, due_date)` 与 `path`。
  - `todos_fts`：FTS5 虚表 `(id UNINDEXED, title, body)`，`tokenize='trigram'`。
  - `people(id PK, name, aliases JSON, grp, product_lines JSON, role, active, group_history JSON, created, updated, path, mtime, size, extra JSON)`，索引 `path`。
  - `people_fts`：FTS5 虚表 `(id UNINDEXED, name, body)`，`tokenize='trigram'`。
  - `reports(id PK = week/level/person, person, name, week, level, grp, product_lines JSON, source, submitted_at, header_date, path, mtime, size, extra JSON)`，索引 `(week, level)` 与 `path`。
  - `reports_fts`：FTS5 虚表 `(id UNINDEXED, name, body)`，`tokenize='trigram'`。
- **检索**：查询 ≥ 3 个字符时用 FTS5 trigram（trigram 支持中文子串），按 `bm25` + `updated` 倒序，命中词用 `[]` 包裹成片段；**少于 3 个字符时 trigram 无法匹配，回退为 `title`/`body` 的 LIKE 检索**（通配符转义）。FTS 查询被当作整体短语加引号，避免 `- : *` 被解释成查询语法；FTS 报错时也退回 LIKE。
- **增量同步**：`Sync` 按 `path + mtime + size` 判断变化，新增/改动重读，消失的删除；`UpsertFile`/`RemoveFile` 供 watcher 用，`UpsertFile` 即使 mtime/size 未变也重读（防同秒小改动漏掉）。文件改名时按 `id` 兜底匹配并清理旧行。
- **版本**：结构版本写在 `PRAGMA user_version`（当前 `3`）。版本不匹配时直接 `DROP` 旧表并按最新结构重建——索引是缓存，丢弃没有代价。
- **可删可重建**：`index rebuild` 清空后从全部 Markdown 重建；watcher 收到 `fsnotify.ErrEventOverflow` 时做一次全量 `Sync` 兜底。

## 资源存储

- `store.AddAsset` / `AddAssetBytes` 把文件复制进 `<工作台>/assets/`，文件名 `<yyyymmdd>-<sha256 前 8 位>-<清洗后的原文件名>`。
- **sha256 去重**：先算内容完整哈希，在 `assets/` 里找文件名含 `-<前 8 位>-` 的候选，再用完整哈希确认，命中则直接复用已有文件，不重复落盘。
- 原文件名清洗为文件系统安全字符（保留中文、字母、数字与 `.-_`，其余变 `_`，为空时用 `asset`），并去掉路径分隔符。
- 返回的是**相对 `notes/` 下笔记文件**的路径：`../assets/<文件名>`，直接写进 Markdown 正文，笔记搬到别处也仍然成立。
- **App 显示**：`WorkspaceService` 的 AssetServer 中间件把 `/workspace-assets/<file>` 映射到 `<工作台>/assets/<file>`，只接受不含路径分隔符、不含 `..` 的普通文件名（防路径穿越）。前端 `markdown.ts` 负责 `../assets/x` ↔ `/workspace-assets/x` 的双向改写：编辑器里显示 URL，保存前改回相对路径，保证文件内容始终是 Markdown 原生相对引用。
- **编辑器粘贴 / 拖入**：
  - ⌘V 粘贴截图、拖入图片、图片块「上传」：Crepe 的 upload 插件调用 ImageBlock 的 `onUpload`（`Editor.tsx` 里配置），前端把文件读成 base64 交给 `WorkspaceService.SaveAsset` → `AddAssetBytes`，返回的 `../assets/x` 改写成显示 URL 插入。笔记与待办详情共用这个编辑器，两处都生效。
  - 从 Finder 拖文件：主窗口开了 Wails 原生文件拖放，会拦下 DOM 的 drop。笔记编辑器区域带 `data-file-drop-target` + `data-note-drop`，`main.go` 的 `WindowFilesDropped` 按属性分流：笔记 → `WorkspaceService.HandleNoteDroppedFiles` → `AddAsset`，再发 `note:assets-dropped`，编辑器在光标处插入 `![名](...)`（图片）或 `[名](...)`（其他文件）；待办详情（`data-todo-id`）仍是加附件。
  - 不压缩，原样保存。
