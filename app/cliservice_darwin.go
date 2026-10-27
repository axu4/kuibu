//go:build darwin

package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// elevateShell 用 osascript 弹出系统管理员密码框执行 cmd。
func elevateShell(cmd string) error {
	script := fmt.Sprintf("do shell script %s with administrator privileges", appleScriptString(cmd))
	out, err := exec.Command("/usr/bin/osascript", "-e", script).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		// -128 是 AppleScript 的 userCanceledErr。
		if strings.Contains(msg, "(-128)") {
			return errElevateCanceled
		}
		if msg == "" {
			return err
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

// appleScriptString 把 s 转成 AppleScript 字符串字面量。
func appleScriptString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
