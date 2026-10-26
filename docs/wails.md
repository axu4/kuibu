# Wails

## 当前版本

| 组件 | 版本 |
| --- | --- |
| Wails（Go 依赖） | `github.com/wailsapp/wails/v3` **v3.0.0** |
| Wails CLI | `wails3` **v3.0.0**（CI 里的 `WAILS_VERSION`） |
| 前端运行时 | `@wailsio/runtime` **3.0.0** |
| 前端 | React 18 + TypeScript + Vite 8 + Milkdown（Crepe）7.22.3 |

v3.0.0 是 **2026-10-26 发布的 GA**。项目此前规划用 beta，正好赶上 GA，因此直接落在 GA 版本上。

GA 之后 v3 按周发小版本（v3.1.0、v3.2.0…），小版本可能带 API 调整，升级要按下面的清单走。

## 升级注意事项

**版本必须三处一致**：

1. `app/go.mod` 里的 `github.com/wailsapp/wails/v3`
2. `app/frontend/package.json` 里的 `@wailsio/runtime`
3. Wails CLI（本地 `wails3` 与 CI 的 `env.WAILS_VERSION`）

三者不一致时绑定生成、运行时 RPC、打包可能互相不兼容。CI 的 `WAILS_VERSION` 也要同步改，否则 `app-macos` 会用旧 CLI 生成绑定，和仓库里新生成的绑定对不上而失败。

升级步骤：

```bash
# 1. 升级 CLI
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.x.y

# 2. 升级 Go 依赖
cd app && go get github.com/wailsapp/wails/v3@v3.x.y && go mod tidy

# 3. 升级前端运行时
cd app/frontend && npm install @wailsio/runtime@3.x.y

# 4. 改 CI 的 WAILS_VERSION（.github/workflows/ci.yml 顶层 env）

# 5. 重新生成绑定并提交
cd app && wails3 generate bindings -ts -i

# 6. 本地验证
cd app/frontend && npm run typecheck
cd app && wails3 package && go vet ./...
```

升级后必须回归（真机 macOS）：

- 启动应用，正常显示窗口与工作台树
- 树的展开/收起、右键菜单、行内重命名
- 编辑并保存笔记（内容落盘正确，`updated` 刷新）
- 外部改文件后界面自动刷新（`workspace:changed` 事件仍生效）
- 图片正常显示（`/workspace-assets/` 中间件与 AssetServer 行为）
- **里程碑 2 起追加**：托盘、全局快捷键、关窗不退出

## Go 版本

官方建议 **Go 1.26.10+**，以获得 `crypto/x509` 的安全修复。

- 本机当前是 **Go 1.26.6**，建议择机升级本机 Go；`go.mod` 仍写 `go 1.26.0`（只约束语言/模块最低版本）。
- CI 用 `actions/setup-go` + `go-version-file: go.mod`，会自动取 1.26 的最新补丁，无需手动改 workflow。
