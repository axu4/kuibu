# 待办（todo）

待办是跬步的第二类 Markdown 数据，平铺存放在工作台的 `todos/` 目录，支持飞书任务式的详情、附件、链接与子任务，录入步骤尽量少。

## CLI 用法

所有 `todo` 子命令都支持 `--json` 与 `--workspace <dir>`，参数与位置参数可混排；成功退出码 0，找不到待办退出 1（JSON 错误码 `NOT_FOUND`），找不到工作台退出 10（`NOT_OPEN`）。

| 命令 | 说明 |
| --- | --- |
| `todo add "<文本>" [--due <值>] [--tag t]... [--parent <id>] [--raw] [--dry-run] [--body/--file/--stdin]` | 快速录入待办；文本可由多个位置参数拼接 |
| `todo list [--status todo,doing\|all\|...] [--due today\|tomorrow\|week\|overdue] [--tag t]... [--parent <id>\|--root] [--search <词>] [--tree]` | 列出待办（走索引，先增量同步） |
| `todo show <id>` | 查看待办详情（含正文、子任务、链接、附件） |
| `todo done <id>` / `todo reopen <id>` / `todo start <id>` / `todo cancel <id>` | 变更状态 |
| `todo edit <id> [--title t] [--due <值>\|--no-due] [--tag t ...] [--status s] [--body/--file/--stdin]` | 修改待办，至少给一个修改项 |
| `todo sub <id> "<文本>"` | 新增子待办，等价于 `todo add "<文本>" --parent <id>` |
| `todo link <id> <url> [--title t]` / `todo unlink <id> <url>` | 管理链接 |
| `todo attach <id> <file>` / `todo detach <id> <相对路径>` | 管理附件，attach 输出附件相对路径 |
| `todo mv <id> [--parent <id>\|--root] [--position n]` | 移动待办 |
| `todo rm <id> [-r\|--recursive]` | 删除待办；有子任务且无 `-r` 时返回 `HAS_CHILDREN` |

`todo add` 默认对文本做快速录入解析；`--raw` 跳过解析把整段当标题；`--due` 优先于文本解析出的日期，其值既接受 `store.ParseDue` 的格式，也接受快速录入日期表达（如 `明天`、`周四 15:00`）；`--due ""` 等价于不设截止。

### 示例

以下示例运行在固定时间 `KUIBU_NOW=2026-10-26T10:00:00+08:00`（周一）下。

```bash
# 快速录入：日期/时间/标签被解析，标题只保留「交周报」
kuibu todo add "明天 15:00 交周报" "#工作" --json
# {"parsed":{"input":"明天 15:00 交周报 #工作","title":"交周报","due":"2026-10-27 15:00","tags":["工作"],"matches":[{"kind":"date","text":"明天","value":"2026-10-27"},{"kind":"time","text":"15:00","value":"15:00"},{"kind":"tag","text":"#工作","value":"工作"}]},"todo":{"id":"20261026-100000-19ae","title":"交周报","status":"todo","due":"2026-10-27 15:00","dueAt":"2026-10-27T15:00:00+08:00","doneAt":null,"overdue":false,"created":"2026-10-26T10:00:00+08:00","updated":"2026-10-26T10:00:00+08:00","tags":["工作"],"parent":"","order":0,"links":[],"attachments":[],"source":"","path":"/tmp/kuibu-ws/todos/20261026-100000-19ae.md"}}

# 人类可读输出
kuibu todo add "周四 交方案 #评审"
# 已创建待办 20261026-100000-f1d2「交方案」 截止 10-29 周四 #评审

# 默认只列未完成
kuibu todo list
# [ ] 20261026-100000-19ae  交周报  10-27 周二 15:00  #工作
# [ ] 20261026-100000-f1d2  交方案  10-29 周四  #评审

kuibu todo list --json
# [{"id":"20261026-100000-19ae","title":"交周报","status":"todo","due":"2026-10-27 15:00","dueAt":"2026-10-27T15:00:00+08:00","doneAt":null,"overdue":false,"created":"2026-10-26T10:00:00+08:00","updated":"2026-10-26T10:00:00+08:00","tags":["工作"],"parent":"","order":0,"links":[],"attachments":[],"source":"","path":"/tmp/kuibu-ws/todos/20261026-100000-19ae.md"},{"id":"20261026-100000-f1d2","title":"交方案","status":"todo","due":"2026-10-29","dueAt":"2026-10-29T23:59:59+08:00","doneAt":null,"overdue":false,"created":"2026-10-26T10:00:00+08:00","updated":"2026-10-26T10:00:00+08:00","tags":["评审"],"parent":"","order":1,"links":[],"attachments":[],"source":"","path":"/tmp/kuibu-ws/todos/20261026-100000-f1d2.md"}]

# 完成与查看
kuibu todo done 20261026-100000-19ae
# 已完成 20261026-100000-19ae
kuibu todo list --status all
# [ ] 20261026-100000-f1d2  交方案  10-29 周四  #评审
# [x] 20261026-100000-19ae  交周报  10-27 周二 15:00  #工作

kuibu todo show 20261026-100000-19ae
# 交周报  (20261026-100000-19ae)
# 状态: todo（未开始）
# 截止: 10-27 周二 15:00
# 创建: 2026-10-26 10:00:00
# 更新: 2026-10-26 10:00:00
# 完成: -
# 标签: 工作
# 父任务: 顶层
# 来源: -

# 子任务与树：只有子任务命中标签时，父任务作为（上级）显示
kuibu todo sub 20261026-100000-4f3e "子任务 #工作"
kuibu todo list --tag 工作 --tree
# [ ] 20261026-100000-4f3e  父任务  子任务 0/1 （上级）
#   [ ] 20261026-100000-7a6c  子任务  #工作

kuibu todo list --tag 工作 --tree --json
# [{"id":"20261026-100000-4f3e","title":"父任务","status":"todo","due":null,"dueAt":null,"doneAt":null,"overdue":false,"created":"2026-10-26T10:00:00+08:00","updated":"2026-10-26T10:00:00+08:00","tags":[],"parent":"","order":2,"links":[],"attachments":[],"source":"","path":"/tmp/kuibu-ws/todos/20261026-100000-4f3e.md","children":[{"id":"20261026-100000-7a6c","title":"子任务","status":"todo","due":null,"dueAt":null,"doneAt":null,"overdue":false,"created":"2026-10-26T10:00:00+08:00","updated":"2026-10-26T10:00:00+08:00","tags":["工作"],"parent":"20261026-100000-4f3e","order":0,"links":[],"attachments":[],"source":"","path":"/tmp/kuibu-ws/todos/20261026-100000-7a6c.md","children":[]}]}

# 树顺序：根节点按 todo list 默认排序（与 index.ListTodos 一致），子任务按 order,id
kuibu todo add "未来" --due 2026-11-01
# 已创建待办 20261026-100000-635e「未来」 截止 11-01 周日
kuibu todo add "逾期" --due 2026-10-25
# 已创建待办 20261026-100000-0826「逾期」 截止 10-25 周日
kuibu todo add "父任务"
# 已创建待办 20261026-100000-9b37「父任务」
kuibu todo add "子1" --parent 20261026-100000-9b37
# 已创建待办 20261026-100000-a266「子1」
kuibu todo add "子2" --parent 20261026-100000-9b37
# 已创建待办 20261026-100000-7b72「子2」
kuibu todo mv 20261026-100000-7b72 --position 0
# 已移动 20261026-100000-7b72

kuibu todo list
# [ ] 20261026-100000-0826  逾期  10-25 周日 已逾期
# [ ] 20261026-100000-635e  未来  11-01 周日
# [ ] 20261026-100000-9b37  父任务  子任务 0/2
# [ ] 20261026-100000-7b72  子2
# [ ] 20261026-100000-a266  子1

kuibu todo list --tree
# [ ] 20261026-100000-0826  逾期  10-25 周日 已逾期
# [ ] 20261026-100000-635e  未来  11-01 周日
# [ ] 20261026-100000-9b37  父任务  子任务 0/2
#   [ ] 20261026-100000-7b72  子2
#   [ ] 20261026-100000-a266  子1

# 错误信息：待办使用专用文案；--json 的 message 与人类输出一致
kuibu todo show nope
# kuibu: 待办不存在: nope
kuibu todo show nope --json
# {"error":{"code":"NOT_FOUND","message":"待办不存在: nope"}}
kuibu todo add "x" --parent nope
# kuibu: 父待办不存在: nope
kuibu todo rm 20261026-100000-9b37
# kuibu: 待办仍有子任务: 20261026-100000-9b37（如需连同子任务删除请加 -r）
kuibu todo mv 20261026-100000-9b37 --parent 20261026-100000-7b72
# kuibu: 移动会导致待办父子关系成环: 20261026-100000-9b37
```

## 快速录入规则

`core.ParseQuickAdd(text, now)` 只识别第一处日期表达（最左匹配，同位置取最长），后续日期原样留在标题；时间可紧跟日期（中间允许空格），没有日期时取第一处时间表达且日期为今天；标签 `#xxx` / `＃xxx` 必须位于开头或空白之后，内容不能为空且不能是纯数字。解析后标题为空时回退为原始输入去首尾空白。

以下规则基于 `now = 2026-10-26 10:00 Asia/Shanghai`（周一）：

| 输入 | 解析结果 | 说明 |
| --- | --- | --- |
| `今天交方案` | 截止 2026-10-26，标题「交方案」 | `今天`/`今日` |
| `明天交方案` | 截止 2026-10-27，标题「交方案」 | `明天`/`明日` |
| `后天交方案` | 截止 2026-10-28，标题「交方案」 | `后天` |
| `大后天交方案` | 截止 2026-10-29，标题「交方案」 | `大后天` |
| `周一交方案` | 截止 2026-10-26，标题「交方案」 | `周X`，周一 = 今天 |
| `周日交方案` | 截止 2026-11-01，标题「交方案」 | `周X` 从今天起取最近一天 |
| `本周一交方案` | 截止 2026-10-26，标题「交方案」 | `本周X`/`这周X`，周一为一周开始 |
| `下周一 15:00` | 截止 2026-11-02 15:00 | `下周X`，时间紧跟日期 |
| `下周三下午3点` | 截止 2026-11-04 15:00 | `下午` 且 N<12 时 +12 |
| `10-30 交方案` | 截止 2026-10-30，标题「交方案」 | `M-D`，今年，早于今天取明年 |
| `10/30` | 截止 2026-10-30 | `M/D` |
| `10月30日` | 截止 2026-10-30 | `M月D日`；`M月D号` 同 |
| `1-5` | 截止 2027-01-05 | 早于今天（1-5 已过）取明年 |
| `2-30` | 不识别，标题「2-30」 | 月日非法不识别 |
| `2026-11-02` | 截止 2026-11-02 | `YYYY-MM-DD` |
| `2026年11月2日` | 截止 2026-11-02 | `YYYY年M月D日` |
| `v1.10-2` | 不识别，标题「v1.10-2」 | 前后紧挨数字/`.` 不误判 |
| `1.2` | 不识别，标题「1.2」 | `M.D` 不识别，避免版本号冲突 |
| `每周一次例会` | 不识别，标题「每周一次例会」 | `周X` 前一个字是 `每` 时不识别 |
| `15:00 开会` | 截止今天 15:00，标题「开会」 | 只有时间没有日期 → 日期取今天 |
| `晚上8点` | 截止今天 20:00 | `晚上` 且 N<12 时 +12 |
| `中午12点` | 截止今天 12:00 | `中午` 且 N<11 时才 +12 |
| `#工作 #框架组` | 标签 `工作`、`框架组` | 多个标签，去重保持顺序 |
| `#123 开会` | 无标签，标题「#123 开会」 | 纯数字不算标签 |
| `周四交方案#评审` | 截止 2026-10-29，无标签 | `#` 前无空白则不当标签 |
| `明天 #工作` | 截止 2026-10-27，标签 `工作` | 只剩日期与标签，标题回退为原文 |
| `多个日期 明天 后天` | 截止 2026-10-27，标题「多个日期 后天」 | 多个日期只取第一个 |

## KUIBU_NOW

CLI 取「现在」的函数优先读取环境变量 `KUIBU_NOW`（RFC3339，例如 `2026-10-26T10:00:00+08:00`），未设置时才用 `time.Now()`。所有待办命令的创建/更新/完成时间、日期解析与到期过滤都使用它。它只用于测试与脚本复现，日常使用不要设置。

```bash
KUIBU_NOW=2026-10-26T10:00:00+08:00 kuibu todo list --due overdue --json
```

## 给 App（Mac 待办界面）的接口说明

App 与 CLI 共用同一套 Go 核心库，推荐直接调用以下 API：

**存储（`internal/store`）**

- `ws.CreateTodo(store.NewTodo{...})`：新建待办；`NewTodo` 含 `Title/Body/Parent/Tags/Due/Status/Links/Source/Now`。
- `ws.GetTodo(id)` / `ws.ListTodos()`：读取单条 / 全部待办（含正文）。
- `ws.UpdateTodo(id, now, func(*Todo) error)`：通用修改，自动维护 `updated` 与 `done_at`。
- `ws.SetTodoStatus(id, status, now)`：`todo`/`doing`/`done`/`canceled` 状态切换。
- `ws.AddTodoLink(id, link, now)` / `ws.RemoveTodoLink(id, url, now)`：管理链接（同 URL 去重）。
- `ws.AttachTodoFile(id, srcPath, now)` / `ws.DetachTodoFile(id, rel, now)`：管理附件（内容哈希去重）。
- `ws.MoveTodo(id, newParent, position)` / `ws.DeleteTodo(id, recursive)`：移动与删除。
- `store.ParseDue(s)` / `store.TodoLocation()`：截止字符串解析与时区。

**快速录入（`internal/core`）**

- `core.ParseQuickAdd(text, now)`：返回 `QuickAdd{Input, Title, Due, Tags, Matches}`，App 的「最少录入」输入框直接用。
- `core.DueRange(kind, now)`：`today`/`week`/`tomorrow` 的日期范围。

**索引（`internal/index`）**

- `idx.ListTodos(index.TodoFilter{...})`：过滤列表；`TodoFilter` 支持 `Statuses/Tags/Parent/DueFrom/DueTo/OverdueAt/Query/Limit`。
- `idx.GetTodo(id)` / `idx.TodoChildren(parent)`：单条与子任务元数据。
- `idx.Sync(ctx)`：外部改文件后的增量同步（App 打开工作台与每次读写前可调用）。

**监听（`internal/index`）**

- `index.Watch(ctx, ws, idx, onChange)`：`onChange` 收到 `Event`；笔记变化在 `Changes/IDs`，待办变化在 `TodoChanges/TodoIDs`，两者互不混入。App 可据此分别刷新笔记树与待办列表。
