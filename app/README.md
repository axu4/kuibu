# app/ —— 跬步桌面外壳

Wails v3（v3.0.0 GA）+ React 18 + TypeScript + Vite 8。里程碑 1：左侧大纲树 + 右侧 Markdown 编辑。

## 运行与构建

```sh
# 开发（热重载）
cd app && wails3 dev

# 类型检查 / 前端构建
cd app/frontend && npm install && npm run typecheck && npm run build

# 重新生成 Go → TS 绑定（修改了 WorkspaceService 的方法或结构体后必须执行）
cd app && wails3 generate bindings -ts -i

# 打包 macOS .app
cd app && wails3 package        # 产物：app/bin/kuibu.app
```

Go 侧是独立 module `github.com/axu4/kuibu/app`，通过 `replace github.com/axu4/kuibu => ../`
复用根模块的 `internal/store`、`internal/index`、`internal/config`。根模块不依赖 Wails。

## 目录

- `main.go`：Wails 应用入口，创建服务与主窗口（1200×780，标题「跬步」）。
- `workspaceservice.go`：暴露给前端的唯一 service（工作台定位、树的增删改查、正文保存、
  `/workspace-assets/` 静态资源中间件、外部变化事件）。
- `frontend/src/App.tsx`：布局与状态编排（树、当前笔记、保存状态、外部修改提示）。
- `frontend/src/components/Tree.tsx`：可展开的大纲树、右键菜单、行内重命名。
- `frontend/src/components/Editor.tsx`：Milkdown（Crepe）编辑器封装。
- `frontend/src/lib/markdown.ts`：`../assets/xxx` ↔ `/workspace-assets/xxx` 的路径改写。

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

拖拽排序（`Move` 方法已暴露）、粘贴图片归档、托盘 / 全局快捷键 / 关窗不退出（里程碑 2）。
