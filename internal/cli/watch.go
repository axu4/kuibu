package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/axu4/kuibu/internal/index"
)

// runWatch 实现 watch 命令：监听工作台变化，每个事件输出一行。
//
// 收到 SIGINT/SIGTERM（Run 传入的 ctx 取消）或 stdin 关闭后正常退出。
func runWatch(ctx context.Context, args []string, common *commonFlags, stdin io.Reader, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("watch")
	addCommonFlags(fs, common)
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("watch 不接受位置参数: %s", strings.Join(pos, " "))
	}

	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	idx, err := index.Open(ws)
	if err != nil {
		return err
	}
	defer idx.Close()

	// stdin 关闭时取消监听，方便管道/脚本使用。
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if stdin != nil {
		go func() {
			_, _ = io.Copy(io.Discard, stdin)
			cancel()
		}()
	}

	onChange := func(e index.Event) {
		if common.json {
			_ = writeJSON(stdout, e)
			return
		}
		parts := make([]string, 0, len(e.Changes))
		for _, c := range e.Changes {
			parts = append(parts, fmt.Sprintf("%s %s", c.Kind, c.ID))
		}
		fmt.Fprintln(stdout, strings.Join(parts, ", "))
	}
	return index.Watch(watchCtx, ws, idx, onChange)
}
