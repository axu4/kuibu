# 跬步（kuibu）

「不积跬步，无以至千里」—— 跬步是一款自用的 Mac 桌面 App，主打轻量、秒开的 Markdown 笔记与团队周报管理。

## 核心功能

1. **笔记（第一核心）**：Markdown，Typora 式实时渲染；粘贴图片存入笔记所在目录的 `assets/`，正文用相对路径引用。
2. **周报收集与逐级汇总（第二核心）**：
   - 「框架组长」与「SE」是两个独立 tab，分开计算。
   - 框架组长：收集组员周报（总结+计划），与上周计划对比，产出组报。
   - SE：收集各组组报，按产品线 × 类别产出产品项目报。
   - 月报同结构，28 日收集。
   - AI 产物只算草稿，必须人工审阅、修改、定稿；导出和下一级引用只认定稿版本。
3. **待办**：飞书任务式，支持详情、附件、链接、子任务，录入步骤最少。
4. **组织树**：大纲式自由树。
5. **团队日历**：按月/周/日查看组员情况。

## 架构

- **Go 核心库**（`internal/...`）供 App 与 CLI 共用，业务逻辑只写在核心库里。
- **CLI `kuibu` 是唯一正式读写接口**，AI 一律经 CLI 读写数据，不直接改文件或数据库。
  - 所有命令支持 `--json` 输出。
  - 明确退出码：`0` 成功；`7 NOT_FINAL`、`8 NEEDS_CONFIRM`、`9 HUMAN_EDITS_EXIST`、`10 NOT_OPEN`；其他错误用 `1`。
- **数据**：工作台文件夹里的 Markdown 文件是唯一事实来源，元数据放 YAML front matter。
- **索引**：SQLite（modernc.org/sqlite，纯 Go，不用 cgo）只作索引，可随时删除重建；外部改文件时用 `fsnotify` 检测并增量重建。
- **界面**：Wails v3.0.0 GA + React/TypeScript，放在 `app/`（独立 module）。
- 不用 git 备份笔记。

## 里程碑 1 已完成

- **核心库**：工作台初始化/定位、Markdown + YAML front matter 读写（未知字段无损保留）、原子写。
- **树**：`parent` + `order` 平铺存储，移动不改文件名，孤儿/环安全降级为根节点。
- **索引**：纯 Go SQLite（`modernc.org/sqlite`），FTS5 trigram 检索 + 短查询 LIKE 回退，`user_version` 版本管理，可随时删除重建。
- **监听**：fsnotify 增量同步，200ms 防抖，事件推给 App 刷新界面。
- **资源**：sha256 去重归档到 `assets/`，正文用 `../assets/` 相对路径。
- **CLI**：`init` / `note` / `tree` / `search` / `asset` / `index` / `watch` / `version`，全部支持 `--json` 与约定退出码。
- **App**：Wails v3 外壳，左侧大纲树 + 右侧 Milkdown（Crepe）编辑器，外部修改自动刷新，`wails3 package` 可打出 macOS `.app`。
- **CI**：Linux/macOS 上 gofmt + vet + build + race 测试，macOS 上校验绑定并打包上传 artifact。

## 里程碑 2 已完成：A 线桌面外壳、B 线待办核心与 CLI、C 线待办界面与随手记接入

**A 线 · 桌面外壳**

- **菜单栏托盘**：自绘 macOS 模板图标（`app/build/tray/tray-template.png`，44×44），菜单含「新建随手记」（带当前快捷键）、「打开跬步」、「设置…」、「退出跬步」。
- **关窗不退出**：`ApplicationShouldTerminateAfterLastWindowClosed=false`，关主窗口只隐藏、应用常驻菜单栏；Dock 图标保留，点 Dock / 托盘「打开跬步」重新显示。⌘Q 与托盘「退出跬步」才真正退出。
- **全局快捷键**：默认 ⌃⌥N（可自定义），应用启动完成后注册以拿到真实错误；注册失败在设置面板与主窗口顶部提示，托盘菜单项始终可用。
- **随手记小窗**：560×300 居中浮窗、可跨 Space 与全屏应用、按需创建关闭即销毁；⌘Enter 保存到工作台「随手记」收件箱，Esc 关闭并把草稿存 localStorage。
- **设置面板**：录制式改快捷键、恢复默认、就地显示注册状态与错误。

**B 线 · 待办核心与 CLI**

- 待办平铺存放在工作台 `todos/`，Markdown + YAML front matter（标题、状态、截止、标签、父任务、order、链接、附件、来源笔记），未知字段无损保留。
- 快速录入 `core.ParseQuickAdd`：日期、时间（含中文数字与时段）、`#标签`，规则与示例见 [docs/todo.md](docs/todo.md)。
- `kuibu todo add/list/show/done/reopen/start/cancel/edit/sub/link/unlink/attach/detach/mv/rm` 全套命令，支持 `--json` 与树形显示。
- 索引侧 `todos` 表与 FTS 检索，监听 `todos/` 目录并用 `TodoIDs` 与笔记变化分开上报。

**C 线 · 待办界面与随手记接入**

- **待办视图**：最左图标栏切换「笔记 / 待办」并记住上次视图。左侧分组「今天（含逾期）/ 本周 / 全部未完成 / 已完成」+ 标签筛选，中间快速录入实时预览与搜索，右侧详情面板（标题、状态、截止、标签、Milkdown 正文、子任务、链接、附件、来源笔记）。
- **完成父任务确认**：有未完成子任务时弹应用内模态，「一起完成 / 只完成父任务 / 取消」。
- **随手记接入待办**：小窗 Tab 切换笔记/待办模式（输入法组合中不切换），待办模式第一行快速录入、其余行为正文，⌘Enter 保存为待办。
- **外部变化**：待办变化发 `todo:changed` 事件刷新列表与详情，正在编辑的标题/正文不被覆盖。

## 目录结构

```
.
├── AGENTS.md           # 开发规范（硬性约束）
├── README.md           # 项目说明
├── go.mod
├── cmd/
│   └── kuibu/          # CLI 入口与测试
├── internal/
│   ├── core/           # 领域模型与业务逻辑
│   ├── store/          # 工作台、Markdown 与 front matter 读写、树、资源
│   ├── index/          # SQLite 索引，可删除重建
│   ├── config/         # 用户配置与工作台定位、GitHub token
│   ├── update/         # 面向 GitHub Releases 的在线更新（下载/校验/解压/替换）
│   ├── version/        # 版本号（发布时 -ldflags 注入）
│   └── cli/            # 命令实现、--json 输出与退出码
├── app/                # Wails v3 桌面外壳（独立 module）
└── docs/               # 设计文档
```

## 下载安装

### App（macOS，Apple Silicon）

1. 到 [Releases](https://github.com/axu4/kuibu/releases) 下载最新 `kuibu-macos-arm64.dmg`。
2. 打开 dmg，把 `跬步.app` 拖到 `Applications`。
3. 首次打开若被 Gatekeeper 拦（ad-hoc 签名、未公证），右键点 App 选「打开」；或执行：

   ```bash
   xattr -dr com.apple.quarantine /Applications/跬步.app
   ```

启动后 App 会按需检查在线更新（详见 [docs/update.md](docs/update.md)），发布流程见 [docs/release.md](docs/release.md)。

### CLI

1. 下载 `kuibu-darwin-arm64`。
2. 加执行权限并放到 PATH 里：

   ```bash
   chmod +x kuibu-darwin-arm64
   sudo mv kuibu-darwin-arm64 /usr/local/bin/kuibu
   ```

3. 首次运行同样可能被 Gatekeeper 拦，执行 `xattr -dr com.apple.quarantine /usr/local/bin/kuibu` 后重试。

## 快速开始

### CLI

```bash
# 构建 / 检查
go build ./... && go vet ./... && go test ./...

# 初始化工作台并设为默认
go run ./cmd/kuibu init ~/Notes/kuibu --default

# 新建、查看、检索
echo "今天完成了 CLI 骨架。" | go run ./cmd/kuibu note new --title "今日记录" --tag 工作 --stdin
go run ./cmd/kuibu tree
go run ./cmd/kuibu search "CLI 骨架" --json
```

### App

需要 Go 1.26.x、Node 22、Wails CLI v3.0.0：

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0   # 装到 ~/go/bin
export PATH="$HOME/go/bin:$PATH"

cd app/frontend && npm ci
cd app && wails3 dev            # 开发模式
cd app && wails3 package        # 打包，产物 app/bin/kuibu.app
```

App 启动时按 `KUIBU_WORKSPACE` > 用户配置默认工作台（`KUIBU_CONFIG_DIR`/config.json）定位工作台；都没有时（首次安装）自动在 `~/Documents/跬步` 建一个工作台并写入用户配置，之后可在侧栏顶部或设置里更换。该目录已存在且非空、又不是工作台时不动它，退回欢迎页让用户选择。

## 反馈与协作

- 需求和 bug 都记录在 GitHub Issues。
- 可以直接提 issue（有 Bug / 需求模板）；也可以在聊天里把问题或想法丢给助手，由助手整理成 issue。
- 标签含义：`bug`、`需求`、`M3 周报收集`、`M4 逐级汇总`；`已评估` = 方案已整理待确认，`已确认` = 可开工。
- 修复 PR 在描述里写 `Fixes #N` 关联对应 issue，合并后自动关闭。

## 文档

| 文档 | 内容 |
| --- | --- |
| [docs/architecture.md](docs/architecture.md) | 整体架构、数据流、工作台目录、front matter、树存储、索引、资源 |
| [docs/todo.md](docs/todo.md) | 待办 CLI 用法与示例、快速录入规则、KUIBU_NOW、App 接口说明、App 待办界面 |
| [docs/report.md](docs/report.md) | 周报收集：组员与周报目录/字段、识别规则、状态规则、CLI 用法、App 接口说明 |
| [docs/development.md](docs/development.md) | 目录说明、开发环境、常用命令、测试约定、分支与 PR 流程 |
| [docs/ci.md](docs/ci.md) | CI 三个 job、触发条件、artifact 与本地复现 |
| [docs/update.md](docs/update.md) | 在线更新原理、token 来源顺序与限制 |
| [docs/release.md](docs/release.md) | 发布步骤、资产命名约定、失败重跑与版本号规则 |
| [docs/wails.md](docs/wails.md) | Wails 版本、升级步骤与回归清单、Go 版本建议 |
| [app/README.md](app/README.md) | 桌面外壳的目录、数据流与编辑器选型 |
| [AGENTS.md](AGENTS.md) | 开发规范（硬性约束） |

## CLI 用法

`kuibu` 是读写工作台的唯一正式接口，所有子命令都支持 `--json`（flag 可放在位置参数前后）。

### 工作台定位

优先级从高到低：

1. `--workspace <dir>`
2. 环境变量 `KUIBU_WORKSPACE`
3. 用户配置中的默认工作台：`$KUIBU_CONFIG_DIR/config.json`（未设 `KUIBU_CONFIG_DIR` 时为 `os.UserConfigDir()/kuibu/config.json`），内容 `{"workspace":"/abs/path"}`
4. 从当前目录向上查找 `.kuibu/config.json`

### 退出码

| 退出码 | 含义 |
| --- | --- |
| 0 | 成功 |
| 1 | 参数错误、笔记不存在、成环、仍有子节点等（JSON `code` 区分 `USAGE` / `NOT_FOUND` / `CYCLE` / `HAS_CHILDREN` / `ALREADY_INIT` / `INVALID` / `INTERNAL`） |
| 7 | `NOT_FINAL`：引用/导出了未定稿内容 |
| 8 | `NEEDS_CONFIRM`：需人工确认 |
| 9 | `HUMAN_EDITS_EXIST`：存在人工修改，拒绝覆盖 |
| 10 | `NOT_OPEN`：未打开工作台（JSON `code` 为 `NOT_OPEN`） |

`--json` 成功时 stdout 输出单个 JSON 值；出错时 stderr 输出 `{"error":{"code":"...","message":"..."}}`。

### 命令

| 命令 | 说明 |
| --- | --- |
| `init <dir> [--default]` | 初始化工作台并建立索引；`--default` 写入用户配置作为默认工作台 |
| `note new --title <t> [--parent <id>] [--tag a ...] [--type note] [--body <text>\|--file <path>\|--stdin]` | 新建笔记，打印 ID（JSON 输出完整元数据） |
| `note list [--parent <id>] [--tag t]` | 表格式列出 id、标题、更新时间 |
| `note show <id> [--meta-only]` | 输出标题行与正文；JSON 输出元数据 + body |
| `note edit <id> [--title t] [--tag t ...] [--body <text>\|--file <path>\|--stdin]` | 修改笔记，至少给一个修改项 |
| `note mv <id> [--parent <id>\|--root] [--pos n]` | 改父节点与位置，`--pos` 缺省为末尾 |
| `note rm <id> [-r\|--recursive]` | 删除笔记，有子节点时必须递归 |
| `todo add <文本> [--due <值>]` | 快速录入待办，日期/时间/标签自动解析 |
| `todo list [--status s] [--due d] [--tree]` | 列出待办，支持状态/到期/标签/检索过滤与树形显示 |
| `todo show/done/reopen/start/cancel/edit/sub/link/unlink/attach/detach/mv/rm` | 待办详情、状态、子任务、链接、附件、移动与删除 |
| `person add <姓名> [--alias a ...] [--group g] [--product p ...] [--role member\|lead]` | 新增组员；姓名/别名在在职人员中唯一 |
| `person list [--group g] [--role r] [--all]` / `person show <id\|姓名>` | 列出/查看组员 |
| `person edit <id\|姓名> [--name n] [--alias a ...\|--no-alias] [--product p ...] [--role r] [--active true\|false]` | 修改组员 |
| `person move <id\|姓名> --group g [--product p ...] [--from YYYY-MM-DD]` | 转组 |
| `report add [--person p] [--week W] [--level member\|lead] [--dry-run] [--file f \| --stdin]` | 归档周报，支持一次粘贴多人 |
| `report list [--week W] [--level L] [--group g] [--search 词]` | 列出周报，`--week` 默认本周 |
| `report show <week> <person> [--level L]` / `report rm <week> <person> [--level L]` | 查看/删除周报 |
| `report status [--week W] [--level L] [--group g] [--product p]` | 提交状态统计 |
| `report config [--weekday N]` | 查看/修改周报日（1-7，缺省 4） |
| `tree [--root <id>]` | 缩进大纲打印笔记树，孤儿/环节点带标记 |
| `search <query> [--limit n]` | 全文检索，输出 id、标题、片段 |
| `asset add <file>` | 归档资源并打印可直接写入 Markdown 的相对路径 |
| `index rebuild` | 重建索引并打印统计 |
| `watch` | 监听工作台变化，每个事件输出一行，SIGINT/SIGTERM 或 stdin 关闭时退出 |
| `version [--json]` | 打印版本号 |
| `help` | 打印用法总览 |

### 示例

```bash
# 初始化并设为默认工作台
kuibu init ~/Notes/kuibu --default

# 新建笔记（正文来自标准输入）
echo "今天完成了 CLI 骨架。" | kuibu note new --title "今日记录" --tag 工作 --stdin

# 列出根节点下的笔记，JSON 输出
kuibu note list --parent "" --json
# [{"id":"20240101-120000-abcd","title":"今日记录","type":"note","created":"...","updated":"...","tags":["工作"],"parent":"","order":0}]

# 查看、编辑、移动、删除
kuibu note show 20240101-120000-abcd
kuibu note edit 20240101-120000-abcd --body "补充内容"
kuibu note mv 20240101-120000-abcd --root --pos 0
kuibu note rm 20240101-120000-abcd -r

# 树与检索
kuibu tree
# - 今日记录  (20240101-120000-abcd)
kuibu tree --json
# [{"id":"20240101-120000-abcd","title":"今日记录","orphan":false,"cycle":false,"children":[]}]
kuibu search "CLI 骨架" --limit 5
# 20240101-120000-abcd  今日记录
#     今天完成了 [CLI 骨架]。
kuibu search "CLI 骨架" --json
# [{"id":"20240101-120000-abcd","title":"今日记录","snippet":"今天完成了 [CLI 骨架]。"}]

# 资源归档：输出可直接粘进 Markdown 的相对路径
kuibu asset add ~/Desktop/截图.png
# ../assets/20240101-1234567890-截图.png

# 周报收集：粘贴归档与提交状态
kuibu person add 张三 --group 框架组 --product A
printf '【周报】张三 2026-10-29\n本周总结' | kuibu report add
kuibu report status --week 2026-W44 --level member

# 索引与监听
kuibu index rebuild --json
# {"added":1,"updated":0,"removed":0,"total":1}
kuibu watch --json
# {"changes":[{"id":"20240101-120000-abcd","kind":"updated"}],"ids":["20240101-120000-abcd"]}
```

## 里程碑

1. **骨架**（已完成）：核心库、CLI、存储、索引、Wails 外壳（树 + 内容区）。
2. **随手记与待办**（已完成）：A 线桌面外壳（托盘、关窗不退出、全局快捷键 ⌃⌥N、随手记小窗、设置面板）、B 线待办核心库与 CLI、C 线待办界面与随手记接入待办。
3. **周报收集**：粘贴归档、提交状态。
4. **逐级汇总**：组报 / 产品项目报、月报、导出。
