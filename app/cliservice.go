package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/axu4/kuibu/internal/clilink"
	"github.com/axu4/kuibu/internal/update"
)

// errElevateCanceled 表示用户在系统密码框里点了取消。
var errElevateCanceled = errors.New("已取消")

// CLIStatus 是命令行工具的安装状态，暴露给前端设置页。
type CLIStatus struct {
	// Available 表示当前 App 自带 CLI、可以安装。
	Available bool `json:"available"`
	// Reason 是不可安装时的中文原因。
	Reason string `json:"reason"`
	// Target 是 .app 包内 CLI 的路径。
	Target string `json:"target"`
	// LinkPath 是软链位置，例如 /usr/local/bin/kuibu。
	LinkPath string `json:"linkPath"`
	// State 是 missing / installed / other。
	State string `json:"state"`
	// Current 是占用链接位置的软链所指向的路径（State 为 other 时有意义）。
	Current string `json:"current"`
}

// CLIService 负责把 .app 自带的 kuibu 命令行链接到 PATH。
type CLIService struct {
	exe  func() (string, error)
	link string
	// elevate 以管理员权限执行 shell 命令；非 macOS 上返回不支持。
	elevate func(cmd string) error
}

// newCLIService 构造使用默认依赖的服务。
func newCLIService() *CLIService {
	return &CLIService{
		exe:     os.Executable,
		link:    clilink.DefaultLinkPath,
		elevate: elevateShell,
	}
}

// ServiceName 返回服务名，供 Wails 日志与绑定使用。
func (s *CLIService) ServiceName() string {
	return "CLIService"
}

// Status 返回当前安装状态。
func (s *CLIService) Status() CLIStatus {
	st := CLIStatus{LinkPath: s.link}
	target, reason := s.target()
	if reason != "" {
		st.Reason = reason
		return st
	}
	st.Available = true
	st.Target = target
	link, err := clilink.Check(target, s.link)
	if err != nil {
		st.Available = false
		st.Reason = err.Error()
		return st
	}
	st.State = string(link.State)
	st.Current = link.Current
	return st
}

// Install 建立（或覆盖）软链；目录没有写权限时弹系统密码框提权。
func (s *CLIService) Install() (CLIStatus, error) {
	target, reason := s.target()
	if reason != "" {
		return s.Status(), errors.New(reason)
	}
	err := clilink.Install(target, s.link)
	if errors.Is(err, fs.ErrPermission) {
		err = s.elevate(clilink.ShellCommand(target, s.link))
		if errors.Is(err, errElevateCanceled) {
			return s.Status(), errors.New("已取消安装")
		}
	}
	if err != nil {
		return s.Status(), fmt.Errorf("安装命令行工具失败：%v", err)
	}
	return s.Status(), nil
}

// target 返回包内 CLI 路径；不可安装时返回中文原因。
func (s *CLIService) target() (string, string) {
	exe, err := s.exe()
	if err != nil {
		return "", fmt.Sprintf("无法获取可执行文件路径：%v", err)
	}
	bundle, err := update.CurrentAppBundle(exe)
	if err != nil {
		return "", "开发模式不在 .app 包内，请用 go install ./cmd/kuibu 安装命令行"
	}
	// 未拖进「应用程序」直接从 dmg / 下载目录打开时，系统会把 App 搬到随机临时路径，
	// 软链指过去下次就失效了。
	if strings.Contains(bundle, "/AppTranslocation/") || strings.HasPrefix(bundle, "/Volumes/") {
		return "", "请先把跬步.app 拖到「应用程序」文件夹并从那里打开，再安装命令行工具"
	}
	target := clilink.BundledCLI(bundle)
	if _, err := os.Stat(target); err != nil {
		return "", "当前 App 包内没有自带命令行程序（旧版本打包），更新到新版后再试"
	}
	return target, ""
}
