// Package version 提供 kuibu 的版本号。
//
// 开发构建默认是 "0.0.0-dev"；正式发布时通过链接器注入真实版本号（不带 v 前缀）：
//
//	go build -ldflags "-X github.com/axu4/kuibu/internal/version.Version=0.2.0" ./cmd/kuibu
package version

// Version 是 kuibu 的当前版本号（不带 v 前缀）。
//
// 它是 var 而不是 const，这样 -ldflags "-X ..." 才能覆盖它；cli 与 App
// 都在运行时读取这个包级变量，因此注入后的值会被正确输出。
var Version = "0.0.0-dev"
