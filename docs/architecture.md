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
├── assets/                       # 图片等资源，扁平存放
│   └── 20240101-1a2b3c4d-截图.png
└── .kuibu/                       # 元数据（不参与笔记树）
    ├── config.json               # {"version":1}
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
| `type` | string | 笔记类型，默认 `note`；为 `todo`、`weekly` 等预留 |
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

选择理由：

1. **文件即事实来源**：单文件就能自解释位置，用编辑器或脚本搬动/改写单条笔记时不会与另一份树文件冲突。
2. **无索引也能读**：删掉 `index.db` 后仅靠 front matter 就能完整重建树。
3. **移动代价低**：只改一个字段 + 少量同级重编号，不移动/重命名文件，避免链接、外部引用、同步工具失效。
4. **天然可扩展**：树结构对 `todo`、周报等所有类型统一适用，无需按类型分表。

## 索引设计

- **SQLite，`modernc.org/sqlite`（纯 Go）**：不用 cgo，Linux/macOS 都能直接交叉构建，CI 不需要 C 工具链。
- 位置：`<工作台>/.kuibu/index.db`；连接参数 `journal_mode(WAL)`、`busy_timeout(10000)`、`synchronous(NORMAL)`，允许 App 与 CLI 同时访问；进程内 `SetMaxOpenConns(1)` 加互斥锁串行化写操作。
- 表：
  - `notes(id PK, title, type, parent, ord, tags JSON, created, updated, path, mtime, size, extra JSON)`，索引 `(parent, ord, id)` 与 `path`。
  - `notes_fts`：FTS5 虚表 `(id UNINDEXED, title, body)`，`tokenize='trigram'`。
- **检索**：查询 ≥ 3 个字符时用 FTS5 trigram（trigram 支持中文子串），按 `bm25` + `updated` 倒序，命中词用 `[]` 包裹成片段；**少于 3 个字符时 trigram 无法匹配，回退为 `title`/`body` 的 LIKE 检索**（通配符转义）。FTS 查询被当作整体短语加引号，避免 `- : *` 被解释成查询语法；FTS 报错时也退回 LIKE。
- **增量同步**：`Sync` 按 `path + mtime + size` 判断变化，新增/改动重读，消失的删除；`UpsertFile`/`RemoveFile` 供 watcher 用，`UpsertFile` 即使 mtime/size 未变也重读（防同秒小改动漏掉）。文件改名时按 `id` 兜底匹配并清理旧行。
- **版本**：结构版本写在 `PRAGMA user_version`（当前 `1`）。版本不匹配时直接 `DROP` 旧表并按最新结构重建——索引是缓存，丢弃没有代价。
- **可删可重建**：`index rebuild` 清空后从全部 Markdown 重建；watcher 收到 `fsnotify.ErrEventOverflow` 时做一次全量 `Sync` 兜底。

## 资源存储

- `store.AddAsset` / `AddAssetBytes` 把文件复制进 `<工作台>/assets/`，文件名 `<yyyymmdd>-<sha256 前 8 位>-<清洗后的原文件名>`。
- **sha256 去重**：先算内容完整哈希，在 `assets/` 里找文件名含 `-<前 8 位>-` 的候选，再用完整哈希确认，命中则直接复用已有文件，不重复落盘。
- 原文件名清洗为文件系统安全字符（保留中文、字母、数字与 `.-_`，其余变 `_`，为空时用 `asset`），并去掉路径分隔符。
- 返回的是**相对 `notes/` 下笔记文件**的路径：`../assets/<文件名>`，直接写进 Markdown 正文，笔记搬到别处也仍然成立。
- **App 显示**：`WorkspaceService` 的 AssetServer 中间件把 `/workspace-assets/<file>` 映射到 `<工作台>/assets/<file>`，只接受不含路径分隔符、不含 `..` 的普通文件名（防路径穿越）。前端 `markdown.ts` 负责 `../assets/x` ↔ `/workspace-assets/x` 的双向改写：编辑器里显示 URL，保存前改回相对路径，保证文件内容始终是 Markdown 原生相对引用。
