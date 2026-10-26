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
- **界面**：Wails v3（beta，GA 后升级）+ React/TypeScript，放在 `app/`。
- 不用 git 备份笔记。

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
│   ├── store/          # Markdown 文件与 front matter 读写
│   ├── index/          # SQLite 索引，可删除重建
│   └── cli/            # 命令实现、--json 输出与退出码
├── app/                # Wails v3 桌面外壳（里程碑 1）
└── docs/               # 设计文档
```

## 构建

```bash
go build ./...
go test ./...
go vet ./...
go run ./cmd/kuibu version --json
```

## 里程碑

1. **骨架**：核心库、CLI、存储、索引、Wails 外壳（树 + 内容区）。
2. **随手记与待办**：全局快捷键（默认 ⌃⌥N，可自定义）。
3. **周报收集**：粘贴归档、提交状态。
4. **逐级汇总**：组报 / 产品项目报、月报、导出。
