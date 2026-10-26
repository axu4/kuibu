# 在线更新

跬步面向 GitHub Releases 自更新：启动时（可配置是否自动检查）拉取最新 Release，下载 `kuibu-macos-arm64.zip` 与 `SHA256SUMS`，校验 SHA-256，解压后整包替换正在运行的 `跬步.app`，重启后生效。

## 原理

1. `internal/version.Version` 是唯一的版本来源（默认 `0.0.0-dev`，发布时 `-ldflags -X` 注入）。
2. `internal/update` 用 GitHub REST API 拿最新 Release：
   - `Client.Latest` 请求 `GET /repos/{repo}/releases/latest`。
   - `Client.Download` 用资产 API URL 下载，带 `Accept: application/octet-stream` 与 token；GitHub 302 到 S3 时，Go 会自动在跨域重定向里丢掉 `Authorization`（这正是我们想要的，不会把 token 发给 S3）。
3. 校验：解析 `SHA256SUMS`（`shasum -a 256` 格式），对 zip 做 SHA-256 校验。
4. 解压：防 zip slip、保留可执行权限位、支持 `.app` 内 framework 的相对符号链接（目标不得逃出解压目录）。
5. 替换：先把新包放到当前 App 同目录的临时名，再 `当前 App → 备份名 → 新包 → 当前 App`，任一步失败都回滚；成功后删除备份。
6. 版本比较：`latest > current` 才提示更新；`current` 是 `0.0.0-dev` 或无法解析时视为开发版，不提示。

## Token 来源顺序

私有仓库必须提供有读权限的 GitHub token。查找顺序：

1. 环境变量 `GITHUB_TOKEN`（其次 `GH_TOKEN`）；
2. 设置里填写的 token（`SaveGitHubToken` 保存到 config.json 同目录的 `github-token`，权限 0600）；
3. `gh auth token`（macOS GUI App 的 PATH 很短，额外尝试 `/opt/homebrew/bin/gh`、`/usr/local/bin/gh`）。

token 只用于认证，任何地方都不会打印或记录 token 本身。

## App 内使用

设置 → 软件更新：

- 显示当前版本与是否为开发版；开发版（`0.0.0-dev` 或可执行文件不在 `.app` 包内）不支持自更新，按钮禁用并显示原因。
- 「检查更新」拉取 GitHub 最新 Release；有新版本时显示发布时间与更新说明（Markdown 按纯文本保留换行展示），可点「更新并重启」。
- 「启动时自动检查更新」开关写入 config.json，不覆盖其它字段；启动 5 秒后后台检查一次，发现新版本只在主窗口顶部提示条显示「发现新版本 vX」，不打扰用户。
- GitHub Token 一栏显示当前来源（环境变量 / gh 登录 / 设置中填写 / 未找到），可手填或清除；token 保存在 config.json 同目录的 `github-token`，权限 600。

### 更新进度阶段

`Apply` 期间 Go 通过 `update:progress` 事件推送阶段，设置面板显示阶段文字：

| stage | 含义 |
| --- | --- |
| `download` | 下载 `SHA256SUMS` 与 `kuibu-macos-arm64.zip` |
| `verify` | SHA-256 校验 |
| `extract` | 解压更新包 |
| `replace` | 替换当前 `.app` |
| `restart` | 启动新 App 并退出当前进程 |

### 失败处理

- 检查失败（无 token、网络错误、404/401/403 速率限制）只在设置面板显示中文错误，不弹窗、不动当前 App。
- 更新失败：`Prepare` 阶段（下载/校验/解压）失败不会调用 `ReplaceApp`；`ReplaceApp` 阶段失败会自动回滚；任一步失败都会删除临时目录并保留当前 App，可在设置面板点「检查更新」重试。
- 替换成功后启动新版本失败时，当前 App 已是新版本，下次手动启动即可生效。

## 限制

- **无 Apple 开发者证书无法公证**：ad-hoc 签名的 App 首次从浏览器下载会被 Gatekeeper 拦，需右键点开或 `xattr -dr com.apple.quarantine /Applications/跬步.app`。App 自己下载的更新包不携带隔离属性，一般不会被拦。
- **私有仓库需要 token**，且 token 必须有该仓库读权限；无 token/无权限时 GitHub 会返回 404，错误提示会提醒检查权限。
- **API 速率限制**：未认证请求额度很低，建议配 token；403 且 `X-RateLimit-Remaining: 0` 表示额度用尽，稍后再试。
- **仅 arm64**：资产是 `kuibu-macos-arm64.zip`，只支持 Apple Silicon。
- **整包替换需重启**：替换的是正在运行的 `.app`，本次进程仍是旧版本，下次启动才生效。
- **App 要装在用户可写位置**：当前 App 所在目录不可写（如装在只读位置）时，更新会报错并提示装到 `/Applications` 或用户可写位置。
- **开发模式不支持自更新**：可执行文件不在 `.app` 包内（`go run` / `wails3 dev`）时不会更新。
