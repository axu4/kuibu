// Package clilink 管理「把 .app 里自带的 kuibu 命令行链接到 PATH」这件事。
//
// .app 里带一份 CLI（Contents/Helpers/kuibu），用户在设置里点「安装命令行工具」后，
// 在 /usr/local/bin/kuibu 建一个指向它的软链。App 自更新整体替换 .app 后，
// 软链路径不变，CLI 自动跟着升级。
package clilink

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// DefaultLinkPath 是软链的默认位置；macOS 的 path_helper 默认把 /usr/local/bin 放进 PATH。
const DefaultLinkPath = "/usr/local/bin/kuibu"

// BundledRelPath 是 CLI 在 .app 包内的相对路径。
const BundledRelPath = "Contents/Helpers/kuibu"

// State 是软链位置的状态。
type State string

const (
	// StateMissing 表示链接位置什么都没有。
	StateMissing State = "missing"
	// StateInstalled 表示链接已指向本 App 自带的 CLI。
	StateInstalled State = "installed"
	// StateOther 表示链接位置被别的文件或指向别处的软链占用。
	StateOther State = "other"
)

// Status 描述链接位置的当前情况。
type Status struct {
	// State 是状态。
	State State
	// Current 是占用者的说明：软链时为其指向，普通文件时为空。
	Current string
}

// BundledCLI 返回 .app 包内 CLI 的路径。
func BundledCLI(appBundle string) string {
	return filepath.Join(appBundle, filepath.FromSlash(BundledRelPath))
}

// Check 检查 link 是否已指向 target。
func Check(target, link string) (Status, error) {
	info, err := os.Lstat(link)
	if errors.Is(err, fs.ErrNotExist) {
		return Status{State: StateMissing}, nil
	}
	if err != nil {
		return Status{}, fmt.Errorf("检查 %s 失败: %w", link, err)
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		return Status{State: StateOther}, nil
	}
	dest, err := os.Readlink(link)
	if err != nil {
		return Status{}, fmt.Errorf("读取软链 %s 失败: %w", link, err)
	}
	if sameFile(dest, target) {
		return Status{State: StateInstalled, Current: dest}, nil
	}
	// 相对软链或中间路径带软链时，按解析后的真实路径再比一次。
	if real, err := filepath.EvalSymlinks(link); err == nil {
		if realTarget, err := filepath.EvalSymlinks(target); err == nil && real == realTarget {
			return Status{State: StateInstalled, Current: dest}, nil
		}
	}
	return Status{State: StateOther, Current: dest}, nil
}

// Install 在 link 建立指向 target 的软链，已有的文件或软链会被替换。
//
// 先在同目录建临时软链再改名，保证任何时刻 link 要么是旧的要么是新的。
// 目录不可写时返回的错误满足 errors.Is(err, fs.ErrPermission)，调用方可据此提权重试。
func Install(target, link string) error {
	if _, err := os.Stat(target); err != nil {
		return fmt.Errorf("找不到自带的命令行程序 %s: %w", target, err)
	}
	if info, err := os.Lstat(link); err == nil && info.IsDir() {
		return fmt.Errorf("%s 是目录，无法安装", link)
	}
	dir := filepath.Dir(link)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建目录 %s 失败: %w", dir, err)
	}
	tmp := filepath.Join(dir, "."+filepath.Base(link)+".tmp")
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return fmt.Errorf("创建软链失败: %w", err)
	}
	if err := os.Rename(tmp, link); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("替换 %s 失败: %w", link, err)
	}
	return nil
}

// ShellCommand 返回与 Install 等价的 shell 命令，供需要管理员权限时交给系统提权执行。
func ShellCommand(target, link string) string {
	return fmt.Sprintf("mkdir -p %s && ln -sfn %s %s",
		shellQuote(filepath.Dir(link)), shellQuote(target), shellQuote(link))
}

// shellQuote 用单引号包住 s，内部单引号转义成 '\''。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func sameFile(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}
