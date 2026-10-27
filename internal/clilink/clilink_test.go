package clilink

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func writeTarget(t *testing.T, dir string) string {
	t.Helper()
	target := BundledCLI(filepath.Join(dir, "跬步.app"))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return target
}

func TestInstallAndCheck(t *testing.T) {
	dir := t.TempDir()
	target := writeTarget(t, dir)
	link := filepath.Join(dir, "bin", "kuibu")

	st, err := Check(target, link)
	if err != nil || st.State != StateMissing {
		t.Fatalf("安装前应为 missing，得到 %+v, %v", st, err)
	}
	if err := Install(target, link); err != nil {
		t.Fatalf("Install: %v", err)
	}
	st, err = Check(target, link)
	if err != nil || st.State != StateInstalled || st.Current != target {
		t.Fatalf("安装后应为 installed，得到 %+v, %v", st, err)
	}
	// 重复安装幂等。
	if err := Install(target, link); err != nil {
		t.Fatalf("重复 Install: %v", err)
	}
}

func TestCheckOtherAndOverwrite(t *testing.T) {
	dir := t.TempDir()
	target := writeTarget(t, dir)
	link := filepath.Join(dir, "kuibu")

	// 手动放过的独立二进制。
	if err := os.WriteFile(link, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if st, _ := Check(target, link); st.State != StateOther || st.Current != "" {
		t.Fatalf("普通文件应为 other，得到 %+v", st)
	}

	// 指向别处的软链。
	_ = os.Remove(link)
	if err := os.Symlink("/elsewhere/kuibu", link); err != nil {
		t.Fatal(err)
	}
	if st, _ := Check(target, link); st.State != StateOther || st.Current != "/elsewhere/kuibu" {
		t.Fatalf("外部软链应为 other，得到 %+v", st)
	}

	if err := Install(target, link); err != nil {
		t.Fatalf("覆盖 Install: %v", err)
	}
	if st, _ := Check(target, link); st.State != StateInstalled {
		t.Fatalf("覆盖后应为 installed，得到 %+v", st)
	}
}

func TestInstallErrors(t *testing.T) {
	dir := t.TempDir()
	if err := Install(filepath.Join(dir, "nope"), filepath.Join(dir, "kuibu")); err == nil {
		t.Fatal("目标不存在应报错")
	}
	target := writeTarget(t, dir)
	link := filepath.Join(dir, "adir")
	if err := os.Mkdir(link, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Install(target, link); err == nil {
		t.Fatal("链接位置是目录应报错")
	}
}

func TestShellCommand(t *testing.T) {
	dir := t.TempDir()
	target := writeTarget(t, dir)
	link := filepath.Join(dir, "it's bin", "kuibu")

	out, err := exec.Command("/bin/sh", "-c", ShellCommand(target, link)).CombinedOutput()
	if err != nil {
		t.Fatalf("执行 %q 失败: %v %s", ShellCommand(target, link), err, out)
	}
	if st, _ := Check(target, link); st.State != StateInstalled {
		t.Fatalf("shell 安装后应为 installed，得到 %+v", st)
	}
}
