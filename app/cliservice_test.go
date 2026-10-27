package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axu4/kuibu/internal/clilink"
)

// newTestCLIService 在临时目录里搭一个带自带 CLI 的假 .app。
func newTestCLIService(t *testing.T, withCLI bool) (*CLIService, string) {
	t.Helper()
	dir := t.TempDir()
	bundle := filepath.Join(dir, "Applications", "跬步.app")
	exe := filepath.Join(bundle, "Contents", "MacOS", "kuibu")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if withCLI {
		target := clilink.BundledCLI(bundle)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("cli"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s := &CLIService{
		exe:  func() (string, error) { return exe, nil },
		link: filepath.Join(dir, "usr", "local", "bin", "kuibu"),
		elevate: func(string) error {
			t.Fatal("可写目录不应提权")
			return nil
		},
	}
	return s, dir
}

func TestCLIServiceInstall(t *testing.T) {
	s, _ := newTestCLIService(t, true)
	if st := s.Status(); !st.Available || st.State != "missing" {
		t.Fatalf("安装前状态不对: %+v", st)
	}
	st, err := s.Install()
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if st.State != "installed" {
		t.Fatalf("安装后状态不对: %+v", st)
	}
}

func TestCLIServiceNoBundledCLI(t *testing.T) {
	s, _ := newTestCLIService(t, false)
	st := s.Status()
	if st.Available || !strings.Contains(st.Reason, "没有自带") {
		t.Fatalf("缺少自带 CLI 应不可安装: %+v", st)
	}
	if _, err := s.Install(); err == nil {
		t.Fatal("缺少自带 CLI 时 Install 应报错")
	}
}

func TestCLIServiceDevMode(t *testing.T) {
	s := &CLIService{
		exe:  func() (string, error) { return "/tmp/kuibu-dev", nil },
		link: filepath.Join(t.TempDir(), "kuibu"),
	}
	if st := s.Status(); st.Available || !strings.Contains(st.Reason, "go install") {
		t.Fatalf("开发模式应提示 go install: %+v", st)
	}
}

func TestCLIServiceElevatesOnPermissionDenied(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 不受目录权限限制")
	}
	s, dir := newTestCLIService(t, true)
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	s.link = filepath.Join(locked, "kuibu")

	var got string
	s.elevate = func(cmd string) error {
		got = cmd
		// 模拟提权成功：解锁目录后执行同一条命令。
		_ = os.Chmod(locked, 0o755)
		return exec.Command("/bin/sh", "-c", cmd).Run()
	}
	st, err := s.Install()
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if got == "" || st.State != "installed" {
		t.Fatalf("应提权安装成功，cmd=%q status=%+v", got, st)
	}

	_ = os.Remove(s.link)
	s.elevate = func(string) error { return errElevateCanceled }
	_ = os.Chmod(locked, 0o555)
	if _, err := s.Install(); err == nil || !strings.Contains(err.Error(), "取消") {
		t.Fatalf("取消提权应返回已取消，得到 %v", err)
	}
	if errors.Is(err, errElevateCanceled) {
		t.Fatal("返回给前端的应是中文提示而不是内部哨兵错误")
	}
}
