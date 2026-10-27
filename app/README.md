# app/ —— 跬步桌面外壳

Wails v3（v3.0.0 GA）+ React 18 + TypeScript + Vite 8。
里程碑 1：左侧大纲树 + 右侧 Markdown 编辑。里程碑 2：菜单栏托盘、关窗不退出、
全局快捷键（默认 ⌃⌥N）、随手记小窗（笔记 / 待办两种模式）、设置面板，以及
待办界面（分组列表 + 详情面板，轻量版飞书任务）。

## 运行与构建

```sh
# 开发（热重载）
cd app && wails3 dev

# 类型检查 / 前端构建
cd app/frontend && npm install && npm run typecheck && npm run build

# 重新生成 Go → TS 绑定（修改了 Go service 的方法或结构体后必须执行）
cd app && wails3 generate bindings -ts -i

# Go 测试（需要 frontend/dist 存在：先 npm run build 或 wails3 package）
cd app && go test ./...

# 打包 macOS .app
cd app && wails3 package        # 产物：app/bin/kuibu.app
```

Go 侧是独立 module `github.com/axu4/kuibu/app`，通过 `replace github.com/axu4/kuibu => ../`
复用根模块的 `internal/store`、`internal/index`、`internal/config`。根模块不依赖 Wails。

## 目录

Go 侧：

- `main.go`：Wails 应用入口，创建服务、主窗口与托盘；`ShouldQuit` 里置位 `quitting` 放行真正退出。
- `workspaceservice.go`：`WorkspaceService`（工作台定位、树的增删改查、正文保存、
  `/workspace-assets/` 静态资源中间件、外部变化事件），另有包内方法 `workspace()`
  与 `index()` 给 ShellService / TodoService 复用当前工作台与索引实例；
  `handleWatchEvent` 是文件变化的纯函数分发（笔记 → `workspace:changed`，
  待办 → `todo:changed`）。
- `todoservice.go`：`TodoService`（待办列表/详情/状态/截止/标签/正文/子任务/链接/
  附件/移动/删除，快速录入预览，原生文件选择与打开文件/链接）。定时器 `now` 与
  `openFile`/`openURL`/`pickFiles` 可注入，便于单测。
- `shellservice.go`：`ShellService`（快捷键状态/修改/恢复默认、随手记保存
  —— 笔记模式 `SaveQuickNote` 与待办模式 `SaveQuickTodo`、窗口控制）。
- `updateservice.go`：`UpdateService`（在线更新：状态、检查、应用更新、自动检查开关
  与 GitHub token；下载/校验/解压/替换复用 `internal/update`，进度通过事件推送）。
- `shell_ui.go`：`shellUI` 的桌面实现（托盘菜单、主窗口显隐、随手记小窗的按需创建与销毁）。
- `shortcut.go`：快捷键纯函数（`normalizeShortcut`、`shortcutDisplay`、`trayNewNoteLabel`）。
- `build/tray/tray-template.png`：菜单栏模板图标；`build/tray/gen/main.go` 是带
  `//go:build ignore` 的生成脚本。

前端：

- `frontend/src/main.tsx`：按 URL 选择渲染主窗口（`/`）还是随手记小窗（`/?view=quicknote`）。
- `frontend/src/App.tsx`：布局与状态编排（最左图标栏的笔记/待办视图切换、树、
  当前笔记、保存状态、外部修改提示、快捷键状态提示、设置面板入口）。
- `frontend/src/QuickNote.tsx`：随手记小窗（textarea、Tab 切换笔记/待办模式、
  ⌘Enter 保存、Esc 关闭、草稿与模式恢复）。
- `frontend/src/components/TodoView.tsx`：待办列表（分组、标签、快速录入与预览、
  搜索、缩进子任务、完成父任务确认）。
- `frontend/src/components/TodoDetail.tsx`：待办详情面板（标题、状态、截止、标签、
  Milkdown 正文、子任务、链接、附件、来源笔记）。
- `frontend/src/components/ReportView.tsx`：周报收集视图（周切换、框架组长/SE 分段、
  状态列表与分组、右侧原文详情、粘贴/删除入口）。
- `frontend/src/components/ReportPaste.tsx`：粘贴周报弹窗（文本框 → 识别 → 预览改人 →
  确认写入，一次可归档多人）。
- `frontend/src/components/PeopleManager.tsx`：组员名单弹窗（在职/离职分组，新增、编辑、
  转组、设为离职/恢复）。
- `frontend/src/lib/report.ts`：周报视图纯函数（周展示、周报日、状态文案、层级偏好）。
- `frontend/src/components/Settings.tsx`：设置面板（快捷键录制/保存/恢复默认/错误提示，
  软件更新与 GitHub token）。
- `frontend/src/components/Tree.tsx`：可展开的大纲树、右键菜单、行内重命名。
- `frontend/src/components/Editor.tsx`：Milkdown（Crepe）编辑器封装。
- `frontend/src/lib/markdown.ts`：`../assets/xxx` ↔ `/workspace-assets/xxx` 的路径改写。
- `frontend/src/lib/shortcut.ts`：Wails 加速键 ↔ macOS 符号 ↔ `KeyboardEvent` 的纯函数。
- `frontend/src/lib/todo.ts`：待办视图的纯函数（截止日期展示、状态文案、链接显示、
  主视图与随手记模式的本地存储、绑定错误整理）。

## 窗口

| 窗口 | 创建方式 | 关闭行为 |
| --- | --- | --- |
| 主窗口「跬步」（1200×780） | 启动时创建，`Name: "main"`，`EnableFileDrop: true` | `WindowClosing` 钩子里 `Hide()+Cancel()`，应用继续常驻；`quitting` 为真时放行 |
| 随手记小窗（560×300，居中浮动、跨 Space、可盖全屏） | 每次按快捷键 / 托盘「新建随手记」时按需创建，`Name: "quicknote"`，`URL: "/?view=quicknote"` | 真正 `Close()` 销毁（不隐藏常驻），从根本上避开 Wails Dock reopen 会 Show 所有隐藏窗口的默认行为 |

## 服务与事件

| Service | 主要方法 |
| --- | --- |
| `WorkspaceService` | `Current`、`ChooseWorkspace`、`Tree`、`Get`、`Create`、`SaveBody`、`Rename`、`Move`、`Delete` |
| `ShellService` | `GetShortcutStatus`、`SetQuickNoteShortcut`、`ResetQuickNoteShortcut`、`SaveQuickNote`、`SaveQuickTodo`、`CloseQuickNote`、`OpenMainWindow` |
| `UpdateService` | `Info`、`Check`、`SetAutoCheck`、`SetToken`、`Apply` |
| `TodoService` | `Preview`、`Create`、`List`、`Tags`、`Get`、`UpdateTitle`、`UpdateDue`、`UpdateTags`、`UpdateBody`、`SetStatus`、`OpenChildrenCount`、`AddLink`、`RemoveLink`、`OpenLink`、`ChooseAndAttach`、`AttachPaths`、`Detach`、`OpenAttachment`、`Move`、`Delete`、`HandleDroppedFiles` |
| `ReportService` | `Week`、`ShiftWeek`、`Status`、`Get`、`Preview`、`Submit`、`Delete`、`People`、`CreatePerson`、`UpdatePerson`、`MovePerson`、`SetPersonActive`、`Groups`、`ProductLines`、`ReportWeekday`、`SetReportWeekday` |

Go → 前端事件：`workspace:changed`（`{ ids }`，笔记）、`todo:changed`（`{ ids }`，待办）、
`report:changed`（`{ peopleIds, reportIds }`，组员/周报）、
`quicknote:saved`（`{ id, inboxId, title }`）、`shell:open-settings`、
`shell:shortcut-changed`（`ShortcutStatus`）、`quicknote:focus`、
`update:available`（`CheckResult`，启动自动检查发现新版本）、
`update:progress`（`{ stage, message }`，应用更新时的阶段进度）。

文件拖放：主窗口开了 `EnableFileDrop`，前端在待办详情面板上写
`data-todo-id`/`data-file-drop-target`；Wails 把 `DropTargetDetails.Attributes`
原样带回 Go，`main.go` 的 `WindowFilesDropped` 监听器取 `data-todo-id` 交给
`TodoService.HandleDroppedFiles` 归档到附件。

快捷键配置字段 `quickNoteShortcut` 存在 `~/Library/Application Support/kuibu/config.json`，
可用 `KUIBU_CONFIG_DIR` 覆盖目录；注册时机、Wails API 细节见 `docs/wails.md`。

## 数据流

- 读写一律经 `internal/store`（Markdown + YAML front matter 是唯一事实来源），
  索引与文件监听用 `internal/index`。App 不自己拼文件路径读写笔记。
- 启动时按 `KUIBU_WORKSPACE` > 用户配置默认工作台（`KUIBU_CONFIG_DIR`/config.json）定位工作台，
  都没有时自动初始化 `~/Documents/跬步`（`config.EnsureWorkspace`），
  打开后 `index.Sync` 并启动 `index.Watch`；外部变化通过 Wails 事件 `workspace:changed`
  （`{ ids: string[] }`）通知前端。自己保存也会触发事件，前端比较正文与本地是否一致来忽略。
- 工作台图片：AssetServer 中间件把 `/workspace-assets/<file>` 映射到 `<工作台>/assets/<file>`，
  只接受不含路径分隔符的普通文件名，防止路径穿越。
- 待办：读之前先 `idx.Sync`（50ms 内合并），写入后强制同步一次再读回，避免
  「刚创建就打开」拿不到索引数据。打开附件只允许该待办附件列表里、且落在工作台
  `assets/` 内的文件，其余一律拒绝。
- 周报：读写走 `internal/store`（组员名单、原文归档）与 `internal/core`（粘贴识别预览），
  状态由 `store.ReportStatus` 计算；写入或 CLI 外部改动（watcher 检测到 people/ 与
  reports/ 变化）都会发 `report:changed`，前端据此整体刷新周报视图。
- 自己写入同样会触发 `todo:changed`/`workspace:changed`（监听没有「谁写的」概念），
  前端只在本地没有未保存修改时才采用外部值，避免打断正在编辑的标题与正文。

## 编辑器选型：Milkdown（Crepe）而不是 TipTap / CodeMirror

- **Markdown 原生往返**：Milkdown 基于 ProseMirror + remark，Markdown 就是数据源（`defaultValue`
  进、`getMarkdown`/`markdownUpdated` 出），不需要额外的 Markdown 扩展。TipTap 的
  Markdown 支持要靠第三方扩展，HTML↔Markdown 往返会丢格式（例如嵌套列表、表格对齐、
  HTML 片段），对本项目「文件即事实来源、AI 与 CLI 都直接读写 Markdown」是硬伤。
- **Typora 式所见即所得**：Crepe 已内置标题/列表/表格/代码块/图片块/斜杠菜单等，接近
  Typora 的编辑体验，后续可以在这个基础上演进；TipTap 需要自己攒扩展，CodeMirror 则只是
  源码编辑器，做不出实时渲染。
- **插件化与可控性**：Milkdown 用 remark 插件体系，后续要加周报模板、待办勾选、front matter
  之外的扩展字段都能落在同一条 Markdown 管线上。CodeMirror 保留在「源码」模式里用普通
  textarea 即可（够用且零依赖），真需要时再换 CodeMirror 也不影响数据格式。
- 注意：Milkdown 序列化可能改写 Markdown 排版（列表符号、空行等），因此只在用户真正编辑后
  才保存；仅打开笔记不会写回文件。

## 里程碑 1 未做的事

拖拽排序（`Move` 方法已暴露）、粘贴图片归档。

里程碑 2 已完成：A 线（托盘、关窗不退出、全局快捷键、随手记小窗、设置面板）、
B 线（待办核心库与 CLI）、C 线（待办界面与随手记接入待办）。
