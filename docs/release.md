# 发布流程

跬步用 GitHub Release 分发 macOS App 与 CLI，发布由 `.github/workflows/release.yml` 自动完成。本文件说明怎么发版本、workflow 做了什么、资产命名约定，以及失败怎么重跑。

## 版本号规则

- 版本号不带 `v` 前缀，格式 `MAJOR.MINOR.PATCH`，可带 `-prerelease`（如 `0.3.0-rc1`）。
- tag 带 `v` 前缀：`v0.2.0`、`v0.3.0-rc1`。
- 预发布：tag 里含 `-`（如 `v0.3.0-rc1`）时，workflow 会自动给 Release 加 `--prerelease` 标记。
- 开发构建的版本号是 `0.0.0-dev`；`internal/version.Version` 默认就是这个值，发布时用 `-ldflags -X` 注入真实版本号。

## 发布步骤

```bash
# 1. 确认本地通过检查
gofmt -l . && go vet ./... && go build ./... && go test ./...

# 2. 打 tag（版本号不带 v，tag 带 v）
git tag -a v0.2.0 -m "跬步 0.2.0"

# 3. 推 tag（只推 tag，不会推工作台数据/未提交内容）
git push origin v0.2.0
```

推完 tag 后 GitHub Actions 会自动跑 `Release` workflow。也可以不推 tag，直接在 GitHub 的 Actions → Release → Run workflow 里填 tag 手动触发（见下文「失败如何重跑」）。

## workflow 做了什么

1. **计算版本**：从 tag 去掉 `v` 前缀得到 `VERSION`，并校验 `MAJOR.MINOR.PATCH[-prerelease]` 格式。
2. **构建 App**：`wails3 package` 打包 `.app`。版本号通过环境变量 `KUIBU_LDFLAGS` 注入——`app/build/darwin/Taskfile.yml` 的 `EXTRA_LDFLAGS` 变量（`sh:` 动态变量）读取它并拼进生产构建的 `-ldflags`，最终等价于 `-ldflags "-w -s -X github.com/axu4/kuibu/internal/version.Version=$VERSION"`。
3. **改 Info.plist**：用 `PlistBuddy` 把 `CFBundleShortVersionString` / `CFBundleVersion` 改成 `$VERSION`，再 `codesign --force --deep -s -` 重签并 `codesign --verify --deep --strict` 校验。
4. **打包**：
   - 把 `.app` 改名为 `跬步.app`（CFBundleName 已经是「跬步」）。
   - `ditto -c -k --keepParent` 打成 `kuibu-macos-arm64.zip`。
   - `hdiutil create` 打成 `kuibu-macos-arm64.dmg`，dmg 里放 `跬步.app` 和指向 `/Applications` 的软链。
5. **构建 CLI**：`CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build`，同样注入版本号，`codesign -s -` ad-hoc 签名，产出 `kuibu-darwin-arm64`。
6. **校验**：`shasum -a 256` 生成 `SHA256SUMS`；冒烟检查 `./kuibu-darwin-arm64 version` 输出包含 `$VERSION`、Info.plist 版本等于 `$VERSION`。
7. **发布**：`gh release create` 创建 Release（标题「跬步 $TAG」，`--generate-notes` 自动生成变更说明）；如果 Release 已存在则 `gh release upload --clobber` 覆盖资产。

## 资产命名约定

资产文件名一律 ASCII（中文目录名在 zip/dmg 里没问题，但文件名保持 ASCII 更稳妥）：

| 文件 | 内容 |
| --- | --- |
| `kuibu-macos-arm64.zip` | macOS App 更新包，内含 `跬步.app` |
| `kuibu-macos-arm64.dmg` | 安装镜像，拖到 `/Applications` 即可 |
| `kuibu-darwin-arm64` | 无 GUI 的 CLI 二进制 |
| `SHA256SUMS` | 上面三个文件的 SHA-256 校验和 |

在线更新（`internal/update`）依赖的约定资产是 `kuibu-macos-arm64.zip` 与 `SHA256SUMS`，不要改名。

## 如何验证

```bash
# 看 Release 与资产
gh release view v0.2.0

# 下载全部资产
gh release download v0.2.0 --pattern 'kuibu-*' --pattern 'SHA256SUMS'

# 校验下载完整性（注意 shasum 会把 SHA256SUMS 本身也列进去，忽略那一行）
shasum -a 256 -c SHA256SUMS 2>/dev/null | grep -v SHA256SUMS

# 本地验证 App 版本
unzip -q kuibu-macos-arm64.zip -d /tmp/kuibu-check
defaults read /tmp/kuibu-check/跬步.app/Contents/Info CFBundleShortVersionString

# 本地验证 CLI 版本（darwin arm64 二进制，在 Apple Silicon Mac 上运行）
chmod +x kuibu-darwin-arm64 && ./kuibu-darwin-arm64 version
```

## 失败如何重跑

发布失败（比如打包、签名、上传某一步挂了）时：

1. 打开仓库 Actions → Release workflow → **Run workflow**。
2. 在 `tag` 输入框填要重跑的 tag（如 `v0.2.0`），Run。
3. workflow 会 `checkout` 该 tag、重新构建并上传资产。因为 Release 已存在，最后会走 `gh release upload --clobber` 覆盖资产，不会重复创建。

重跑时 `permissions: contents: write` 已经给 `GITHUB_TOKEN`，不需要额外配置 secret；`gh` CLI 使用 `${{ github.token }}`。

## 注意

- 没有 Apple Developer ID 证书时只能用 ad-hoc 签名，**无法公证**。用户首次从浏览器下载会被 Gatekeeper 拦（见 [update.md](update.md) 的绕过方式）。
- 只发布 arm64（macos-latest 是 Apple Silicon runner）；Intel 机器暂不支持。
- 不要手动改资产文件名，否则在线更新找不到 `kuibu-macos-arm64.zip` / `SHA256SUMS`。
