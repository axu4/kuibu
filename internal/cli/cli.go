package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/axu4/kuibu/internal/store"
)

// Version 是 kuibu 的版本号。
const Version = "0.0.1-dev"

// usageText 是中文用法总览。
const usageText = `用法:
  kuibu <命令> [参数...] [--json] [--workspace <dir>]

命令:
  init <dir> [--default]                 初始化工作台
  note new --title <t> [--parent <id>]   新建笔记
  note list [--parent <id>] [--tag t]    列出笔记
  note show <id> [--meta-only]           查看笔记
  note edit <id> [--title t] ...         修改笔记
  note mv <id> [--parent <id>|--root]    移动笔记
  note rm <id> [-r|--recursive]          删除笔记
  tree [--root <id>]                     打印笔记树
  search <query> [--limit n]             全文检索
  asset add <file>                       归档资源并输出相对路径
  index rebuild                          重建索引
  watch                                  监听工作台变化
  version [--json]                       打印版本号
  help                                   打印本帮助

工作台定位优先级:
  --workspace <dir> > 环境变量 KUIBU_WORKSPACE > 用户配置默认工作台 > 从当前目录向上查找

退出码:
  0  成功
  1  参数错误、笔记不存在、成环、仍有子节点等
  7  NOT_FINAL（引用/导出了未定稿内容）
  8  NEEDS_CONFIRM（需人工确认）
  9  HUMAN_EDITS_EXIST（存在人工修改，拒绝覆盖）
  10 NOT_OPEN（未打开工作台）
`

// commonFlags 是每个子命令都支持的全局参数。
type commonFlags struct {
	// workspace 是 --workspace 指定的工作台目录。
	workspace string
	// json 表示以 JSON 输出。
	json bool
}

// codedError 是带 JSON 错误码的错误。
type codedError struct {
	code string
	err  error
}

// Error 实现 error 接口。
func (e *codedError) Error() string { return e.err.Error() }

// Unwrap 支持 errors.Is/As。
func (e *codedError) Unwrap() error { return e.err }

// usagef 构造一个 USAGE 错误。
func usagef(format string, args ...any) error {
	return &codedError{code: "USAGE", err: fmt.Errorf(format, args...)}
}

// coded 构造一个指定错误码的错误。
func coded(code string, err error) error {
	return &codedError{code: code, err: err}
}

// Run 执行命令行并返回退出码。
//
// env 用于读取环境变量，传 nil 时使用 os.Getenv。
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer, env func(string) string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return RunContext(ctx, args, stdin, stdout, stderr, env)
}

// RunContext 与 Run 相同，但由调用方控制取消（watch 命令使用）。
func RunContext(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, env func(string) string) int {
	if env == nil {
		env = os.Getenv
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}

	var common commonFlags
	rest, err := parseLeadingGlobals(args, &common)
	if err != nil {
		return report(err, common.json, stderr)
	}
	if len(rest) == 0 {
		if common.json {
			return report(usagef("缺少命令"), true, stderr)
		}
		fmt.Fprint(stderr, usageText)
		return Error
	}

	cmd, cmdArgs := rest[0], rest[1:]
	switch cmd {
	case "version":
		err = runVersion(cmdArgs, &common, stdout)
	case "help":
		fmt.Fprint(stdout, usageText)
		return OK
	case "init":
		err = runInit(cmdArgs, &common, stdout, stderr, env)
	case "note":
		err = runNote(ctx, cmdArgs, &common, stdin, stdout, stderr, env)
	case "tree":
		err = runTree(cmdArgs, &common, stdout, stderr, env)
	case "search":
		err = runSearch(ctx, cmdArgs, &common, stdout, stderr, env)
	case "asset":
		err = runAsset(cmdArgs, &common, stdout, stderr, env)
	case "index":
		err = runIndex(ctx, cmdArgs, &common, stdout, stderr, env)
	case "watch":
		err = runWatch(ctx, cmdArgs, &common, stdin, stdout, stderr, env)
	default:
		jsonMode := common.json || hasJSONFlag(cmdArgs)
		if jsonMode {
			return report(usagef("未知命令 %q", cmd), true, stderr)
		}
		fmt.Fprint(stderr, usageText)
		return Error
	}
	if err != nil {
		return report(err, common.json, stderr)
	}
	return OK
}

// hasJSONFlag 判断参数里是否出现了 --json。
//
// 未知命令时无法用 flag 解析，只能做一次简单的存在性检查。
func hasJSONFlag(args []string) bool {
	for _, a := range args {
		if a == "--json" || a == "-json" || a == "--json=true" {
			return true
		}
	}
	return false
}

// parseLeadingGlobals 解析子命令之前的全局参数。
//
// 使用标准 flag 的“遇到第一个非参数即停止”语义，因此
// `kuibu --workspace X note list` 可用；子命令之后的参数由各命令自行解析。
func parseLeadingGlobals(args []string, common *commonFlags) ([]string, error) {
	if len(args) == 0 || !strings.HasPrefix(args[0], "-") {
		return args, nil
	}
	fs := flag.NewFlagSet("kuibu", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addCommonFlags(fs, common)
	if err := fs.Parse(args); err != nil {
		return nil, usagef("%v", err)
	}
	return fs.Args(), nil
}

// addCommonFlags 给子命令注册全局参数。
func addCommonFlags(fs *flag.FlagSet, common *commonFlags) {
	fs.StringVar(&common.workspace, "workspace", common.workspace, "工作台目录")
	fs.BoolVar(&common.json, "json", common.json, "以 JSON 输出")
}

// newFlagSet 创建不向 stderr 打印错误的 FlagSet。
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

// parseCommand 解析子命令参数，允许参数与位置参数混排。
func parseCommand(fs *flag.FlagSet, args []string) ([]string, error) {
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return nil, usagef("%v", err)
	}
	return pos, nil
}

// parseInterspersed 解析参数，允许 flag 与位置参数任意顺序混排。
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if len(arg) < 2 || arg[0] != '-' || arg == "-" {
			positional = append(positional, arg)
			continue
		}

		name := strings.TrimLeft(arg, "-")
		value := ""
		hasValue := false
		if idx := strings.IndexByte(name, '='); idx >= 0 {
			value = name[idx+1:]
			name = name[:idx]
			hasValue = true
		}
		if name == "" {
			return nil, fmt.Errorf("非法参数 %q", arg)
		}
		f := fs.Lookup(name)
		if f == nil {
			return nil, fmt.Errorf("未知参数 %q", arg)
		}

		if !hasValue {
			if isBoolFlag(f) {
				value = "true"
			} else {
				if i+1 >= len(args) {
					return nil, fmt.Errorf("参数 --%s 需要一个值", name)
				}
				i++
				value = args[i]
			}
		}
		if err := fs.Set(name, value); err != nil {
			return nil, fmt.Errorf("参数 --%s 的值不合法: %v", name, err)
		}
	}
	return positional, nil
}

// isBoolFlag 判断 flag 是否是布尔开关。
func isBoolFlag(f *flag.Flag) bool {
	type boolFlag interface{ IsBoolFlag() bool }
	bf, ok := f.Value.(boolFlag)
	return ok && bf.IsBoolFlag()
}

// visitedFlags 返回本次实际出现过的 flag 名称集合。
func visitedFlags(fs *flag.FlagSet) map[string]bool {
	seen := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { seen[f.Name] = true })
	return seen
}

// report 输出错误并返回退出码。
func report(err error, jsonMode bool, stderr io.Writer) int {
	code, exit := classify(err)
	if jsonMode {
		_ = writeJSON(stderr, map[string]any{
			"error": map[string]string{
				"code":    code,
				"message": err.Error(),
			},
		})
	} else {
		fmt.Fprintf(stderr, "kuibu: %s\n", err.Error())
	}
	return exit
}

// classify 把错误映射为 JSON 错误码与退出码。
func classify(err error) (string, int) {
	var ce *codedError
	if errors.As(err, &ce) {
		return ce.code, Error
	}
	switch {
	case errors.Is(err, store.ErrNotWorkspace):
		return "NOT_OPEN", NotOpen
	case errors.Is(err, store.ErrAlreadyInit):
		return "ALREADY_INIT", Error
	case errors.Is(err, store.ErrNotFound):
		return "NOT_FOUND", Error
	case errors.Is(err, store.ErrCycle):
		return "CYCLE", Error
	case errors.Is(err, store.ErrHasChildren):
		return "HAS_CHILDREN", Error
	case errors.Is(err, store.ErrInvalid):
		return "INVALID", Error
	default:
		return "INTERNAL", Error
	}
}

// writeJSON 以单个 JSON 值写入 w，保留中文原样。
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// versionInfo 是 version --json 的输出结构。
type versionInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Go      string `json:"go"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}

// runVersion 实现 version 命令。
func runVersion(args []string, common *commonFlags, stdout io.Writer) error {
	fs := newFlagSet("version")
	addCommonFlags(fs, common)
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("version 不接受位置参数: %s", strings.Join(pos, " "))
	}
	if common.json {
		return writeJSON(stdout, versionInfo{
			Name:    "kuibu",
			Version: Version,
			Go:      runtime.Version(),
			OS:      runtime.GOOS,
			Arch:    runtime.GOARCH,
		})
	}
	fmt.Fprintf(stdout, "kuibu %s\n", Version)
	return nil
}

// requireArgs 校验位置参数个数。
func requireArgs(pos []string, want int, usage string) error {
	if len(pos) != want {
		return usagef("%s", usage)
	}
	return nil
}
