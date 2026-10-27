# CI

工作流文件：`.github/workflows/ci.yml`。

## 触发条件

- `push` 到 `main`
- 任意 `pull_request`

同一 ref 只保留最新一次运行：`concurrency` 分组为 `${{ github.workflow }}-${{ github.ref }}`，`cancel-in-progress: true`。

权限只有 `contents: read`。

CI 跑完后会触发 `Auto merge` 工作流：带 `automerge` 标签的 PR 在 CI 成功后自动合并，带 `release:*` 标签的再自动发版，详见 [release.md](release.md#自动合并与自动发版按-pr-标签)。

## 三个 job

三个 job 彼此不依赖，并行执行。

### `go-linux`（Go (Linux)，ubuntu-latest）

根模块的跨平台守门人——核心库与 CLI 必须在 Linux 上可构建、可测试。

1. `actions/setup-go@v5`，`go-version-file: go.mod`（即 go 1.26.0，自动取该 minor 的最新补丁），开 module 缓存。
2. **gofmt 检查**：`gofmt -l .` 有输出就失败。注意这是按目录扫描，会扫到 `app/` 下手写的 Go 文件（`app/main.go`、`app/workspaceservice.go`、`app/build/{ios,android}/*.go`），这些也必须是 gofmt 干净的；`app/frontend/bindings` 只有 `.ts/.js`，node_modules 在这个 job 里不安装，无需排除。
3. `go vet ./...`
4. `go build ./...`
5. `go test -race ./...`（需要 cgo，因此不设 `CGO_ENABLED=0`；构建不依赖 cgo 由纯 Go 的 `modernc.org/sqlite` 保证）

### `go-macos`（Go (macOS)，macos-latest）

根模块在 macOS 上的原生验证，与 go-linux 的区别是不跑 gofmt（已由 Linux job 覆盖）。

`go vet ./...` → `go build ./...` → `go test -race ./...`。

### `app-macos`（App (macOS)，macos-latest）

`app/` 独立 module 的构建、绑定一致性与打包。

1. setup-go 用 `go-version-file: app/go.mod`，`cache-dependency-path` 同时包含 `go.sum` 与 `app/go.sum`（两个独立 module 都参与缓存 key）。
2. setup-node 用 **Node 22**，缓存 `app/frontend/package-lock.json`。
3. 安装 Wails CLI：`go install github.com/wailsapp/wails/v3/cmd/wails3@${WAILS_VERSION}`，`WAILS_VERSION` 在 workflow 顶层 `env` 里，当前 `v3.0.0`，必须与 `app/go.mod` 一致；随后把 `$(go env GOPATH)/bin` 加进 `GITHUB_PATH`。
4. `app/frontend`：`npm ci` + `npm run typecheck`。
5. **校验绑定是最新的**：跑 `wails3 generate bindings -ts -i`，然后检查 `frontend/bindings` 的已跟踪文件改动（`git diff --exit-code`）和新增未跟踪文件（`git ls-files --others`）；任一非空就失败，并提示本地执行 `cd app && wails3 generate bindings -ts -i` 后提交。必须带 `-ts -i`，与 `app/README.md`、`app/build/Taskfile.yml` 保持一致，不带会生成 `.js` 而不是仓库里的 `.ts`。
6. `wails3 package` 打包（顺带把前端构建到 `frontend/dist`，满足 `main.go` 的 `go:embed`）。
7. `go vet ./...`（在 `app/` 下）。
8. 校验产物存在 `app/bin/kuibu.app/Contents/MacOS/kuibu`，用 `ditto -c -k --keepParent` 打成 `kuibu-macos-arm64.zip`。
9. 上传 artifact。

## Artifact

| 项 | 值 |
| --- | --- |
| 名称 | `kuibu-macos-<sha>`（`github.sha`） |
| 内容 | `kuibu-macos-arm64.zip`，内含 `kuibu.app` |
| 保留 | 14 天 |

**签名说明**：产物是 **ad-hoc 签名、未经公证**（notarized）的。首次打开会被 Gatekeeper 拦下，需要：

- 右键点 `kuibu.app` → 「打开」→ 再确认一次；或
- 终端执行 `xattr -dr com.apple.quarantine /path/to/kuibu.app` 后再打开。

这个产物用于真机回归（里程碑 1：启动、树、编辑保存、外部修改刷新、图片显示；里程碑 2 起加托盘、全局快捷键、关窗不退出），不是正式发布渠道。

## 本地复现

根模块：

```bash
gofmt -l .                # 必须无输出
go vet ./...
go build ./...
go test -race ./...
```

App：

```bash
export WAILS_VERSION=v3.0.0
go install github.com/wailsapp/wails/v3/cmd/wails3@${WAILS_VERSION}
export PATH="$(go env GOPATH)/bin:$PATH"

cd app/frontend
npm ci
npm run typecheck

cd ..
wails3 generate bindings -ts -i
git diff --exit-code -- frontend/bindings
git ls-files --others --exclude-standard -- frontend/bindings   # 必须为空

wails3 package
go vet ./...
test -f bin/kuibu.app/Contents/MacOS/kuibu
ditto -c -k --keepParent bin/kuibu.app /tmp/kuibu-macos-arm64.zip
```
