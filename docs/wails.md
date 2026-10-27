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
- 菜单栏托盘图标（模板图标深浅色自适应）、菜单各项、托盘左键开菜单
- 关主窗口不退出，Dock 图标与托盘「打开跬步」能把窗口找回来
- ⌃⌥N 在别的应用里也能弹随手记，⌘Enter 保存、Esc 关闭、草稿恢复
- 设置面板录制新快捷键、恢复默认、失败提示
- 树的展开/收起、右键菜单、行内重命名
- 编辑并保存笔记（内容落盘正确，`updated` 刷新）
- 外部改文件后界面自动刷新（`workspace:changed` 事件仍生效）
- 图片正常显示（`/workspace-assets/` 中间件与 AssetServer 行为）
- **里程碑 2 起追加**：托盘、全局快捷键、关窗不退出

## Go 版本

官方建议 **Go 1.26.10+**，以获得 `crypto/x509` 的安全修复。

- 本机当前是 **Go 1.26.6**，建议择机升级本机 Go；`go.mod` 仍写 `go 1.26.0`（只约束语言/模块最低版本）。
- CI 用 `actions/setup-go` + `go-version-file: go.mod`，会自动取 1.26 的最新补丁，无需手动改 workflow。

## 里程碑 2 A 线：桌面外壳（托盘 / 关窗不退出 / 全局快捷键 / 随手记小窗）

实现的文件都在 `app/`：

| 文件 | 职责 |
| --- | --- |
| `app/shell_ui.go` | 托盘、主窗口显隐、随手记小窗的创建与销毁（`shellUI`） |
| `app/shellservice.go` | `ShellService`：快捷键状态/修改/恢复默认、随手记保存、窗口控制 |
| `app/shortcut.go` | 快捷键纯函数：校验规范化、macOS 符号化、托盘菜单标签 |
| `app/build/tray/tray-template.png` | 菜单栏模板图标（44×44，纯黑 + 透明） |
| `app/build/tray/tray-template.svg` | 菜单栏图标源文件；`render.sh` 渲染成上面的 PNG |
| `app/build/tray/candidates/` | 图标候选（SVG + PNG + `preview.png` 预览），换图标时用 |
| `app/frontend/src/lib/shortcut.ts` | 前端同规则的加速键 ↔ 符号 ↔ KeyboardEvent 转换 |
| `app/frontend/src/QuickNote.tsx` | 随手记小窗（`/?view=quicknote`） |
| `app/frontend/src/components/Settings.tsx` | 设置面板（快捷键录制） |

### 读源码确认的 Wails 行为（v3.0.0）

1. **`App.Quit` 会被 `WindowClosing` 钩子拦住。**
   `App.Quit()` → `InvokeSync(impl.destroy)` → ObjC `destroyApp()` = `[NSApp terminate:nil]`
   → `applicationShouldTerminate:` → `shouldQuit()`（即 `Options.ShouldQuit`）→ `cleanup()`
   → `cleanup()` 里对每个窗口调用 `window.Close()`，而 `Close()` 正是 `InvokeSync(w.emit(WindowClosing))`。
   所以在 `WindowClosing` 钩子里 `Hide()+Cancel()` 会让退出流程卡在窗口关闭上。
   对策：用 `quitting atomic.Bool`，在 `Options.ShouldQuit` 与 `App.OnShutdown` 里置位，
   钩子看到就放行。顺便也覆盖了系统注销/关机触发的 terminate。
2. **`GlobalShortcut.Register` 的时机。**
   `GlobalShortcutManager.Register` 在 `m.started == false` 时只把快捷键放进 `pending` 并返回 nil；
   真正的系统注册发生在 `App.Run()` 内部的 `startup()`：先 `a.running = true`，紧接着
   `a.GlobalShortcut.flushPending()`（`flushPending` 里才把 `started` 置位），失败通过
   `app.handleError` 报出，**不会**返回给当初的调用方。所以 `ShellService` 在
   `ServiceStartup`（此时 `startup()` 还没跑完）里只读取配置，注册动作放到
   `events.Common.ApplicationStarted`（macOS 上由 `ApplicationDidFinishLaunching` 映射）回调里，
   这样 `Register` 走的是「已 started」分支，能拿到真实错误（例如被别的 App 占用）。
3. **Dock reopen 的默认行为会误弹隐藏窗口。**
   `events_common_darwin.go` 的 `setupCommonEvents()` 自己注册了
   `ApplicationShouldHandleReopen` 监听：**只要没有可见窗口，就 `Show()` 所有不可见窗口**。
   如果随手记小窗隐藏常驻，点 Dock 图标会把它一起弹出来。对策：小窗**每次按需创建、
   关闭时 `Close()` 真正销毁**（`webview_window.go` 里 `WindowClosing` 的默认监听会
   `markAsDestroyed` + `impl.close` + `WindowManager.Remove`），从根上不留下「隐藏窗口」。
   主窗口仍按需求隐藏/显示，另外注册一个 reopen 监听把它 `Show()+Focus()`。
4. **托盘左键点击打开菜单。** `SystemTray` 的 `applySmartDefaults()` 只在显式设置了
   `OnClick` 或 `AttachWindow` 时才改变默认行为；两者都不设置时，macOS 的
   `systrayPreClickCallback` 对左键返回 1，用原生 tracking 打开菜单（不激活 App），正是 macOS 惯例。
5. **菜单加速键只是展示。** `MenuItem.SetAccelerator` 在 macOS 上设的是
   `keyEquivalent + keyEquivalentModifierMask`，菜单打开时会真的抢按键；本项目的「新建随手记」
   已有全局快捷键，菜单项只需要展示，因此把符号直接写进标签（`新建随手记  ⌃⌥N`），
   用 `SetLabel` 更新，避免两套快捷键打架。`SetLabel` 是同步生效的（`dispatch_sync`）。
6. **随手记小窗用普通 NSPanel。** `MacWindowClassPanel` + `FloatingPanel: true`
   + `WindowLevel: MacWindowLevelFloating` 得到浮动面板层级；
   `CollectionBehavior: CanJoinAllSpaces | FullScreenAuxiliary` 让它跨 Space 并盖住全屏应用。
   **不要**用 `PanelPreferences.NonActivating`：那样 `windowFocus` 走 `orderFrontRegardless`
   不激活应用，WKWebView 可能拿不到键盘输入。这里保持可激活（`ActivationPolicyRegular`），
   弹出时 `Show()+Focus()` 让 `windowFocus` 里 `activateIgnoringOtherApps` 生效。

### 快捷键配置

- 字段：`quickNoteShortcut`（Wails 加速键语法，如 `"Ctrl+Alt+N"`），空/缺失表示默认 ⌃⌥N。
- 位置：`~/Library/Application Support/kuibu/config.json`（即 `os.UserConfigDir()/kuibu/config.json`），
  可用环境变量 `KUIBU_CONFIG_DIR` 覆盖目录（测试与多工作台隔离都用它）。
- 修改顺序是「先注册新的，成功后再注销旧的并落盘」：新快捷键被占用时旧快捷键继续生效，
  返回中文错误给前端就地显示。
- 注册失败只影响快捷键：托盘「新建随手记」与设置面板始终可用。

### 事件名（Go → 前端）

| 事件 | 数据 | 触发时机 |
| --- | --- | --- |
| `quicknote:saved` | `{ id, inboxId, title }` | 随手记保存成功（主窗口刷新树 + 展开收件箱） |
| `shell:open-settings` | 无 | 托盘「设置…」 |
| `shell:shortcut-changed` | `ShortcutStatus` | 快捷键注册/修改/失败后 |
| `quicknote:focus` | 无 | 已打开的小窗再次按快捷键（重新聚焦输入框） |
| `workspace:changed` | `{ ids }` | 原有事件，未改动 |

### 图标生成

```sh
cd app && sh build/tray/render.sh                                   # tray-template.svg → tray-template.png
cd app && sh build/tray/render.sh build/tray/candidates/b-two-steps.svg   # 换成某个候选
```

图标源是 SVG（`build/tray/tray-template.svg`，44×44 视图、纯黑 + 透明，模板图标由系统
自动适配深浅色菜单栏），`render.sh` 用本机 Google Chrome 的 headless 模式渲染成 44×44 PNG
（菜单栏显示为 22pt）。PNG 生成物提交进仓库，构建不依赖 Chrome。
当前用候选 A「一枚脚印」；候选见 `build/tray/candidates/preview.png`（浅色 / 深色菜单栏对比）。
