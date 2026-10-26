package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/axu4/kuibu/internal/config"
	"github.com/axu4/kuibu/internal/update"
	"github.com/axu4/kuibu/internal/version"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// 事件名。Go → 前端。
const (
	// updateProgressEvent 在 Apply 期间持续推送阶段进度。
	updateProgressEvent = "update:progress"
	// updateAvailableEvent 在启动时自动检查发现新版本时推送。
	updateAvailableEvent = "update:available"
	// updateRepo 是在线更新检查的 GitHub 仓库。
	updateRepo = "axu4/kuibu"
)

// UpdateInfo 是「软件更新」设置区的状态。
type UpdateInfo struct {
	// Current 是当前版本号（不带 v 前缀）。
	Current string `json:"current"`
	// IsDev 表示是否为开发版（版本为 0.0.0-dev 或不在 .app 包内）。
	IsDev bool `json:"isDev"`
	// CanSelfUpdate 表示能否自更新（仅非开发版为 true）。
	CanSelfUpdate bool `json:"canSelfUpdate"`
	// DevReason 是开发版不能自更新的中文原因，非开发版为空。
	DevReason string `json:"devReason"`
	// AutoCheck 表示「启动时自动检查更新」是否开启。
	AutoCheck bool `json:"autoCheck"`
	// HasToken 表示是否找到可用的 GitHub token。
	HasToken bool `json:"hasToken"`
	// TokenSource 是 token 的中文来源，如「gh 登录」「设置中填写」「未找到」。
	TokenSource string `json:"tokenSource"`
	// SavedToken 表示是否已在设置里保存手填 token。
	SavedToken bool `json:"savedToken"`
}

// CheckResult 是一次在线检查的结果。
type CheckResult struct {
	// Current 是当前版本号（不带 v 前缀）。
	Current string `json:"current"`
	// Latest 是最新版本号（已去掉 v 前缀）。
	Latest string `json:"latest"`
	// HasUpdate 表示是否有可更新的新版本。
	HasUpdate bool `json:"hasUpdate"`
	// Notes 是 Release 说明（Markdown）。
	Notes string `json:"notes"`
	// Name 是 Release 名称。
	Name string `json:"name"`
	// HTMLURL 是 Release 页面地址。
	HTMLURL string `json:"htmlUrl"`
	// PublishedAt 是发布时间（RFC3339）。
	PublishedAt string `json:"publishedAt"`
	// CheckedAt 是本次检查时间（RFC3339）。
	CheckedAt string `json:"checkedAt"`
}

// UpdateProgress 是 update:progress 事件的数据。
type UpdateProgress struct {
	// Stage 是阶段：download / verify / extract / replace / restart。
	Stage string `json:"stage"`
	// Message 是给用户看的中文进度说明。
	Message string `json:"message"`
}

// UpdateService 暴露在线更新能力给前端：查看状态、检查更新、应用更新与 token 配置。
//
// 下载、校验、解压与替换都复用 internal/update，本层只做依赖注入、
// 并发保护与事件转发；token 与更新包内容绝不记录日志。
type UpdateService struct {
	app *application.App

	// 以下依赖可在测试里替换，避免启动 GUI 或访问真实网络。
	env      func(string) string
	getenv   func(string) string
	lookPath func(string) (string, error)
	run      func(name string, args ...string) ([]byte, error)
	exe      func() (string, error)
	http     *http.Client
	baseURL  string
	repo     string
	quit     func()
	start    func(appPath string) error
	now      func() time.Time
	emit     func(name string, data any)

	// startOnce 保证 ServiceStartup 只挂一次自动检查。
	startOnce sync.Once
	// opMu 保证同一时间只有一个 Check/Apply 在跑。
	opMu sync.Mutex
	// lastRelease 缓存最近一次 Check 拿到的 Release，Apply 可复用。
	lastRelease *update.Release
}

// newUpdateService 构造一个使用默认依赖的更新服务。
func newUpdateService() *UpdateService {
	s := &UpdateService{
		env:      os.Getenv,
		getenv:   os.Getenv,
		lookPath: exec.LookPath,
		run:      runGH,
		exe:      os.Executable,
		// 不设整体 Timeout：下载更新包在慢网络上可能超过几十秒。
		// 查询用 20 秒 context，下载用 15 分钟 context；连接与响应头另有超时。
		http: &http.Client{Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		}},
		repo: updateRepo,
		now:  time.Now,
	}
	s.start = defaultStartApp
	return s
}

// runGH 用 5 秒超时执行 `gh auth token`，避免 GUI App 里 gh 卡住。
func runGH(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Output()
}

// ServiceName 返回服务名，供 Wails 日志与绑定使用。
func (s *UpdateService) ServiceName() string {
	return "UpdateService"
}

// ServiceStartup 在应用启动完成后延迟 5 秒后台检查一次更新。
//
// 检查结果只在有新版本时通过 update:available 事件告知前端；
// 失败只记录简短原因，不打扰用户。
func (s *UpdateService) ServiceStartup(_ context.Context, _ application.ServiceOptions) error {
	s.startOnce.Do(func() {
		go func() {
			time.Sleep(5 * time.Second)
			s.autoCheck()
		}()
	})
	return nil
}

// autoCheck 执行启动时的后台自动检查。
func (s *UpdateService) autoCheck() {
	cfg, err := config.Load(s.env)
	if err != nil {
		log.Printf("kuibu: 读取自动更新配置失败: %v", err)
		return
	}
	if !cfg.AutoCheckUpdateEnabled() {
		return
	}
	if dev, _ := s.devStatus(); dev {
		return
	}
	result, err := s.Check()
	if err != nil {
		log.Printf("kuibu: 自动检查更新失败: %v", err)
		return
	}
	if result.HasUpdate {
		s.emitEvent(updateAvailableEvent, result)
	}
}

// Info 返回当前版本、自更新能力、自动检查与 token 状态。
//
// 绝不返回 token 本身，只返回是否存在与来源。
func (s *UpdateService) Info() UpdateInfo {
	cfg, err := config.Load(s.env)
	autoCheck := true
	if err != nil {
		log.Printf("kuibu: 读取自动更新配置失败: %v", err)
	} else {
		autoCheck = cfg.AutoCheckUpdateEnabled()
	}

	token, source := s.findToken()
	if token == "" {
		source = "未找到"
	}

	savedToken := false
	if saved, err := config.LoadGitHubToken(s.env); err == nil && strings.TrimSpace(saved) != "" {
		savedToken = true
	}

	isDev, reason := s.devStatus()
	return UpdateInfo{
		Current:       version.Version,
		IsDev:         isDev,
		CanSelfUpdate: !isDev,
		DevReason:     reason,
		AutoCheck:     autoCheck,
		HasToken:      token != "",
		TokenSource:   source,
		SavedToken:    savedToken,
	}
}

// devStatus 判断当前是否为不可自更新的开发版，并给出中文原因。
func (s *UpdateService) devStatus() (bool, string) {
	if strings.TrimSpace(version.Version) == "0.0.0-dev" {
		return true, "当前是开发版（0.0.0-dev），不支持自更新"
	}
	exe, err := s.exe()
	if err != nil {
		return true, fmt.Sprintf("无法获取可执行文件路径：%v", err)
	}
	if _, err := update.CurrentAppBundle(exe); err != nil {
		return true, "当前可执行文件不在 .app 包内（开发模式不支持自更新）"
	}
	return false, ""
}

// findToken 按 update.FindToken 的优先级查找 token，只返回 token 与中文来源。
func (s *UpdateService) findToken() (string, string) {
	env := s.env
	if env == nil {
		env = os.Getenv
	}
	getenv := s.getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	lookPath := s.lookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	run := s.run
	if run == nil {
		run = runGH
	}

	cfgToken := ""
	if t, err := config.LoadGitHubToken(env); err == nil {
		cfgToken = t
	} else {
		log.Printf("kuibu: 读取 GitHub token 失败: %v", err)
	}
	return update.FindToken(getenv, cfgToken, lookPath, run)
}

// Check 检查 GitHub 最新 Release，返回结果与中文错误。
func (s *UpdateService) Check() (CheckResult, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.check()
}

// check 是 Check 的锁内实现，便于 Apply 复用缓存 Release 时保持一致。
func (s *UpdateService) check() (CheckResult, error) {
	token, _ := s.findToken()
	if token == "" {
		return CheckResult{}, errors.New("未找到 GitHub token：请在终端执行 gh auth login，或在设置里填写一个有 axu4/kuibu 读权限的 token")
	}

	client := s.client(token)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rel, err := client.Latest(ctx)
	if err != nil {
		return CheckResult{}, err
	}
	s.lastRelease = rel
	return resultFromRelease(rel, s.now()), nil
}

// SetAutoCheck 写回「启动时自动检查更新」开关，保留 config.json 的其它字段。
func (s *UpdateService) SetAutoCheck(enabled bool) error {
	cfg, err := config.Load(s.env)
	if err != nil {
		return err
	}
	cfg.AutoCheckUpdate = &enabled
	return config.Save(s.env, cfg)
}

// SetToken 保存或清除手填 GitHub token（空字符串表示清除）。
//
// 不记录日志，token 只写入权限 0600 的文件。
func (s *UpdateService) SetToken(token string) error {
	return config.SaveGitHubToken(s.env, token)
}

// Apply 下载、校验、解压并替换当前 App，然后重启。
//
// 任一步失败都会返回明确中文错误；Prepare 失败不会调用 ReplaceApp，
// ReplaceApp 内部会回滚。成功后启动新 App 并退出当前进程。
func (s *UpdateService) Apply() error {
	s.opMu.Lock()
	defer s.opMu.Unlock()

	exe, err := s.exe()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败：%w", err)
	}
	currentApp, err := update.CurrentAppBundle(exe)
	if err != nil {
		return err
	}

	token, _ := s.findToken()
	if token == "" {
		return errors.New("未找到 GitHub token：请在终端执行 gh auth login，或在设置里填写一个有 axu4/kuibu 读权限的 token")
	}

	rel := s.lastRelease
	if rel == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		rel, err = s.client(token).Latest(ctx)
		cancel()
		if err != nil {
			return err
		}
		s.lastRelease = rel
	}
	if rel == nil {
		return errors.New("没有可用的更新信息，请先检查更新")
	}
	if !update.IsNewer(rel.TagName, version.Version) {
		return errors.New("当前已是最新版本，无需更新")
	}

	tmpDir, err := os.MkdirTemp("", "kuibu-update-*")
	if err != nil {
		return fmt.Errorf("创建临时目录失败：%w", err)
	}
	defer os.RemoveAll(tmpDir)

	dlCtx, dlCancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer dlCancel()
	newApp, err := update.PrepareWithProgress(
		dlCtx,
		s.client(token),
		rel,
		tmpDir,
		func(stage, message string) {
			s.emitProgress(stage, message)
		},
	)
	if err != nil {
		return fmt.Errorf("下载更新包失败：%w", err)
	}

	s.emitProgress("replace", "正在替换当前应用…")
	if err := update.ReplaceApp(currentApp, newApp); err != nil {
		return fmt.Errorf("替换当前应用失败：%w", err)
	}

	s.emitProgress("restart", "正在重启应用…")
	if err := s.startApp(currentApp); err != nil {
		return fmt.Errorf("更新已完成，但启动新版本失败：%w", err)
	}
	if s.quit != nil {
		s.quit()
	}
	return nil
}

// client 用当前 token 与注入的 http/baseURL 构造更新客户端。
func (s *UpdateService) client(token string) *update.Client {
	return &update.Client{
		Repo:    s.repo,
		Token:   token,
		HTTP:    s.http,
		BaseURL: s.baseURL,
	}
}

// startApp 启动新版本 App；默认实现用 open -n，并让子进程脱离父进程。
func (s *UpdateService) startApp(appPath string) error {
	if s.start != nil {
		return s.start(appPath)
	}
	return defaultStartApp(appPath)
}

// emitProgress 推送 update:progress 事件。
func (s *UpdateService) emitProgress(stage, message string) {
	s.emitEvent(updateProgressEvent, UpdateProgress{Stage: stage, Message: message})
}

// emitEvent 向前端发事件；app 未注入且测试未注入 emit 时安全忽略。
func (s *UpdateService) emitEvent(name string, data any) {
	if s.emit != nil {
		s.emit(name, data)
		return
	}
	if s.app == nil {
		return
	}
	if data == nil {
		s.app.Event.Emit(name)
		return
	}
	s.app.Event.Emit(name, data)
}

// resultFromRelease 把 GitHub Release 转成前端结构，latest 去掉 v 前缀。
func resultFromRelease(rel *update.Release, now time.Time) CheckResult {
	latest := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v"), "V")
	return CheckResult{
		Current:     version.Version,
		Latest:      latest,
		HasUpdate:   update.IsNewer(rel.TagName, version.Version),
		Notes:       rel.Body,
		Name:        rel.Name,
		HTMLURL:     rel.HTMLURL,
		PublishedAt: rel.PublishedAt.Format(time.RFC3339),
		CheckedAt:   now.Format(time.RFC3339),
	}
}

// defaultStartApp 延迟 1 秒后用 `open -n` 启动新 App。
//
// SysProcAttr.Setpgid 让子进程脱离当前进程组，父进程退出后仍能存活。
// 这是 unix-only 实现（本项目只支持 macOS，测试在 Linux 上编译运行）。
func defaultStartApp(appPath string) error {
	cmd := exec.Command("/bin/sh", "-c", `sleep 1; /usr/bin/open -n "$0"`, appPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd.Start()
}
