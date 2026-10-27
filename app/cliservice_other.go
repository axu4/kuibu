//go:build !darwin

package main

import "errors"

// elevateShell 只在 macOS 上实现。
func elevateShell(string) error {
	return errors.New("当前系统不支持自动提权，请手动以管理员身份创建软链")
}
