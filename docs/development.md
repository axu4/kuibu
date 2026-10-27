# 开发

## 目录说明

```
.
├── AGENTS.md                 # 开发规范（硬性约束，先读）
├── README.md                 # 项目说明与 CLI 用法
├── go.mod                    # 根 module github.com/axu4/kuibu（go 1.26.0）
├── cmd/kuibu/                # CLI 入口 main.go 与端到端测试 main_test.go
├── internal/
│   ├── core/                 # 领域模型与业务逻辑（预留）
│   ├── store/                # 工作台、Markdown + front matter、树、资源、原子写
│   ├── index/                # SQLite 索引、FTS5 检索、fsnotify 监听
│   ├── config/               # 用户配置与工作台定位
│   └── cli/                  # 子命令、--json 输出、退出码
├── app/                      # 独立 module github.com/axu4/kuibu/app（Wails v3 外壳）
│   ├── main.go               # 应用入口、窗口、AssetServer
│   ├── workspaceservice.go   # 暴露给前端的唯一 service
│   ├── frontend/             # React + TypeScript + Vite + Milkdown
│   │   ├── src/              # App.tsx / components / lib
│   │   └── bindings/         # Go → TS 绑定（生成物，提交进仓库）
│   └── build/                # wails3 平台构建配置与 Taskfile
├── docs/                     # 设计文档
└── .github/workflows/ci.yml  # CI
```

## 开发环境

| 依赖 | 版本 |
| --- | --- |
| Go | 1.26.x（`go.mod` 为 `go 1.26.0`） |
| Node | 22（CI 用 22，前端 Vite 8） |
| Wails CLI | v3.0.0 |

安装 wails3：

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0
# 装到 $(go env GOPATH)/bin，通常是 ~/go/bin，确保在 PATH 里
export PATH="$HOME/go/bin:$PATH"
```

Wails CLI 版本必须与 `app/go.mod` 里的 `github.com/wailsapp/wails/v3`、`app/frontend/package.json` 里的 `@wailsio/runtime` 保持一致，详见 [wails.md](wails.md)。

日常开发在 Linux 上：核心库和 CLI 必须能在 Linux 构建和测试。Mac 相关能力（托盘、快捷键、打包）用 build tag 隔离，只在 Mac 实测。

## 常用命令

根模块（在仓库根目录）：

```bash
go build ./...
go vet ./...
go test ./...
gofmt -l .            # CI 会检查，必须为空
```

CLI 本地运行：

```bash
go run ./cmd/kuibu version --json
go run ./cmd/kuibu init /tmp/ws --default
go run ./cmd/kuibu note new --title 测试 --workspace /tmp/ws --json
# 或先构建再跑
go build -o /tmp/kuibu ./cmd/kuibu && /tmp/kuibu tree --workspace /tmp/ws
```

App（在 `app/` 下）：

```bash
cd app/frontend && npm ci     # 首次或依赖变动后安装前端依赖
cd app && wails3 dev          # 开发模式，热重载

cd app && wails3 generate bindings -ts -i   # 重新生成 Go → TS 绑定
cd app && wails3 package                    # 打包，产物 app/bin/kuibu.app
```

注意：

- **`app/` 是独立 module**，根模块的 `./...` 不包含它；两边要分别 `go vet`/`go build`。
- **绑定是提交进仓库的生成物**（`app/frontend/bindings/`）。改了 `WorkspaceService` 的方法签名或结构体后必须跑 `wails3 generate bindings -ts -i` 并提交；CI 会重新生成并 `git diff` 校验，不一致直接失败。
- `app/main.go` 用 `//go:embed all:frontend/dist` 打包前端，所以 `app/` 下 `go build`/`go vet` 前必须先有 `frontend/dist`：跑过 `wails3 package`，或 `cd app/frontend && npm ci && npm run build`。
- Linux 上构建/测试 `app/` 要加 `-tags gtk3`（需 pkg-config、libgtk-3-dev、libwebkit2gtk-4.1-dev），例如 `cd app && go build -tags gtk3 ./... && go vet -tags gtk3 ./...`。
- 本项目只做 macOS（以后 Windows），Wails 模板生成的 `build/ios`、`build/android` 已删除；它们是没有 `main` 的 `package main`，会让 `app/` 下 `go build ./...` 链接失败。

## 测试约定

- 一律用 `t.TempDir()` 造工作台与配置文件目录，不碰真实用户目录。
- 涉及用户配置的测试用 `KUIBU_CONFIG_DIR` 指向临时目录隔离，例如：

```go
env := func(key string) string {
    if key == "KUIBU_CONFIG_DIR" {
        return t.TempDir()
    }
    return ""
}
```

- `internal/cli/cli_test.go` 里的 `testEnv` 就是这套约定：临时工作台 + 隔离的配置目录 + 可注入的 env 函数（`cli.Run` 接收 `getenv func(string) string`）。
- `cmd/kuibu/main_test.go` 会 `go build` 出真实二进制并跑端到端命令，断言 stdout/stderr/退出码，环境里显式设置 `KUIBU_CONFIG_DIR`。
- 文件监听相关的测试用可注入的 `ready` channel 等待 watcher 就绪，避免 sleep 竞态。
- 每次改动必须跑通 `go build ./... && go vet ./... && go test ./...`。

## 工作流

1. 从 `main` 拉分支（`feat/...`、`fix/...`、`docs/...`）。
2. 本地跑通 gofmt / vet / build / test（改 App 时另跑 typecheck 与绑定生成）。
3. 推分支开 PR。
4. **CI 全绿后 squash merge** 进 `main`，保持主干线性历史；提交信息用中文、一句话说清改了什么。
5. 绝不提交密钥、token、个人工作台数据（`.gitignore` 已排除 `app/bin`、`frontend/dist`、`node_modules`、`*.db` 等）。

CI 细节见 [ci.md](ci.md)。
