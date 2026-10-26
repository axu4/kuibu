# app/ —— 跬步桌面外壳

Wails v3（v3.0.0 GA）+ React 18 + TypeScript + Vite 8。
里程碑 1：左侧大纲树 + 右侧 Markdown 编辑。里程碑 2 A 线：菜单栏托盘、关窗不退出、
全局快捷键（默认 ⌃⌥N）、随手记小窗、设置面板。

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
  给 ShellService 复用当前工作台实例。
- `shellservice.go`：`ShellService`（快捷键状态/修改/恢复默认、随手记保存、窗口控制）。
- `shell_ui.go`：`shellUI` 的桌面实现（托盘菜单、主窗口显隐、随手记小窗的按需创建与销毁）。
- `shortcut.go`：快捷键纯函数（`normalizeShortcut`、`shortcutDisplay`、`trayNewNoteLabel`）。
- `build/tray/tray-template.png`：菜单栏模板图标；`build/tray/gen/main.go` 是带
  `//go:build ignore` 的生成脚本。

前端：

- `frontend/src/main.tsx`：按 URL 选择渲染主窗口（`/`）还是随手记小窗（`/?view=quicknote`）。
- `frontend/src/App.tsx`：布局与状态编排（树、当前笔记、保存状态、外部修改提示、
  快捷键状态提示、设置面板入口）。
- `frontend/src/QuickNote.tsx`：随手记小窗（textarea、⌘Enter 保存、Esc 关闭、草稿恢复）。
- `frontend/src/components/Settings.tsx`：设置面板（快捷键录制/保存/恢复默认/错误提示）。
- `frontend/src/components/Tree.tsx`：可展开的大纲树、右键菜单、行内重命名。
- `frontend/src/components/Editor.tsx`：Milkdown（Crepe）编辑器封装。
- `frontend/src/lib/markdown.ts`：`../assets/xxx` ↔ `/workspace-assets/xxx` 的路径改写。
- `frontend/src/lib/shortcut.ts`：Wails 加速键 ↔ macOS 符号 ↔ `KeyboardEvent` 的纯函数。

## 窗口

| 窗口 | 创建方式 | 关闭行为 |
| --- | --- | --- |
| 主窗口「跬步」（1200×780） | 启动时创建，`Name: "main"` | `WindowClosing` 钩子里 `Hide()+Cancel()`，应用继续常驻；`quitting` 为真时放行 |
| 随手记小窗（560×300，居中浮动、跨 Space、可盖全屏） | 每次按快捷键 / 托盘「新建随手记」时按需创建，`Name: "quicknote"`，`URL: "/?view=quicknote"` | 真正 `Close()` 销毁（不隐藏常驻），从根本上避开 Wails Dock reopen 会 Show 所有隐藏窗口的默认行为 |

## 服务与事件

| Service | 主要方法 |
| --- | --- |
| `WorkspaceService` | `Current`、`ChooseWorkspace`、`Tree`、`Get`、`Create`、`SaveBody`、`Rename`、`Move`、`Delete` |
| `ShellService` | `GetShortcutStatus`、`SetQuickNoteShortcut`、`ResetQuickNoteShortcut`、`SaveQuickNote`、`CloseQuickNote`、`OpenMainWindow` |

Go → 前端事件：`workspace:changed`（`{ ids }`）、`quicknote:saved`（`{ id, inboxId, title }`）、
`shell:open-settings`、`shell:shortcut-changed`（`ShortcutStatus`）、`quicknote:focus`。

快捷键配置字段 `quickNoteShortcut` 存在 `~/Library/Application Support/kuibu/config.json`，
可用 `KUIBU_CONFIG_DIR` 覆盖目录；注册时机、Wails API 细节见 `docs/wails.md`。

## 数据流

- 读写一律经 `internal/store`（Markdown + YAML front matter 是唯一事实来源），
  索引与文件监听用 `internal/index`。App 不自己拼文件路径读写笔记。
- 启动时按 `KUIBU_WORKSPACE` > 用户配置默认工作台（`KUIBU_CONFIG_DIR`/config.json）定位工作台，
  打开后 `index.Sync` 并启动 `index.Watch`；外部变化通过 Wails 事件 `workspace:changed`
  （`{ ids: string[] }`）通知前端。自己保存也会触发事件，前端比较正文与本地是否一致来忽略。
- 工作台图片：AssetServer 中间件把 `/workspace-assets/<file>` 映射到 `<工作台>/assets/<file>`，
  只接受不含路径分隔符的普通文件名，防止路径穿越。

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

里程碑 2 A 线已完成：托盘、关窗不退出、全局快捷键、随手记小窗、设置面板（待办 B/C 线进行中）。
