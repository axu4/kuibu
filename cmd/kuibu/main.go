// kuibu 是跬步的命令行入口，也是读写工作台数据的唯一正式接口。
package main

import (
	"io"
	"os"

	"github.com/axu4/kuibu/internal/cli"
)

// version 是当前版本号，与 cli.Version 保持一致。
const version = cli.Version

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run 执行命令行并返回退出码。args 不含程序名，便于测试。
func run(args []string, stdout, stderr io.Writer) int {
	return cli.Run(args, os.Stdin, stdout, stderr, os.Getenv)
}
