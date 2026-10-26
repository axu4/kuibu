// kuibu 是跬步的命令行入口，也是读写工作台数据的唯一正式接口。
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/axu4/kuibu/internal/cli"
)

// version 是当前版本号。
const version = "0.0.1-dev"

// usage 是简短的中文用法说明。
const usage = `用法:
  kuibu version          打印版本号
  kuibu version --json   以 JSON 打印版本信息
`

// versionInfo 是 --json 输出的版本信息结构。
type versionInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Go      string `json:"go"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run 执行命令行并返回退出码。args 不含程序名，便于测试。
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return cli.Error
	}

	switch args[0] {
	case "version":
		return runVersion(args[1:], stdout, stderr)
	default:
		fmt.Fprint(stderr, usage)
		return cli.Error
	}
}

func runVersion(args []string, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 0:
		fmt.Fprintf(stdout, "kuibu %s\n", version)
		return cli.OK
	case len(args) == 1 && args[0] == "--json":
		info := versionInfo{
			Name:    "kuibu",
			Version: version,
			Go:      runtime.Version(),
			OS:      runtime.GOOS,
			Arch:    runtime.GOARCH,
		}
		b, err := json.Marshal(info)
		if err != nil {
			fmt.Fprintf(stderr, "kuibu: 生成版本信息失败: %v\n", err)
			return cli.Error
		}
		fmt.Fprintln(stdout, string(b))
		return cli.OK
	default:
		fmt.Fprint(stderr, usage)
		return cli.Error
	}
}
