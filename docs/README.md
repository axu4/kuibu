# docs/

跬步的设计与开发文档。

| 文档 | 内容 |
| --- | --- |
| [architecture.md](architecture.md) | 整体架构与模块职责、数据流（App/CLI → store → index；外部编辑 → fsnotify → index → App 事件）、工作台目录结构、front matter 字段表、树存储方案与理由、索引设计、资源存储 |
| [development.md](development.md) | 目录说明、开发环境（Go 1.26.x / Node 22 / wails3 v3.0.0）、常用命令、绑定生成、测试约定、分支与 PR 工作流 |
| [ci.md](ci.md) | CI 三个 job（go-linux / go-macos / app-macos）的职责与触发条件、artifact 与签名说明、本地复现 |
| [wails.md](wails.md) | Wails v3.0.0 GA 与 `@wailsio/runtime` 3.0.0、升级注意事项与回归清单、Go 版本建议 |

其他文档：

- [../README.md](../README.md)：项目说明与 CLI 用法
- [../app/README.md](../app/README.md)：桌面外壳的目录、数据流、编辑器选型
- [../AGENTS.md](../AGENTS.md)：开发规范（硬性约束）
