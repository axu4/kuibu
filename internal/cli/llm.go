package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/axu4/kuibu/internal/llm"
)

// runLLM 分发 llm 子命令。
func runLLM(ctx context.Context, args []string, common *commonFlags, stdin io.Reader, stdout, stderr io.Writer, env func(string) string) error {
	if len(args) == 0 {
		return usagef("llm 需要子命令: add/list/default/rm/test/send")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "add":
		return llmAdd(rest, common, stdin, stdout, env)
	case "list":
		return llmList(rest, common, stdout, env)
	case "default":
		return llmDefault(rest, common, stdout, env)
	case "rm":
		return llmRemove(rest, common, stdout, env)
	case "test":
		return llmTest(ctx, rest, common, stdout, env)
	case "send":
		return llmSend(ctx, rest, common, stdin, stdout, env)
	default:
		return usagef("未知 llm 子命令 %q", sub)
	}
}

// llmAdd 实现 llm add：新增或更新模型，密钥从标准输入读取。
func llmAdd(args []string, common *commonFlags, stdin io.Reader, stdout io.Writer, env func(string) string) error {
	fs := newFlagSet("llm add")
	addCommonFlags(fs, common)
	preset := fs.String("preset", "", "提供方模板: deepseek|claude")
	provider := fs.String("provider", "", "提供方: openai|claude")
	baseURL := fs.String("base-url", "", "接口地址")
	model := fs.String("model", "", "模型 ID")
	makeDefault := fs.Bool("default", false, "设为默认模型")
	keepKey := fs.Bool("keep-key", false, "更新已有模型时保留原密钥，不读标准输入")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(pos, 1, "用法: kuibu llm add <名称> [--preset deepseek|claude] [--provider openai|claude] [--base-url URL] [--model ID] [--default] < 密钥"); err != nil {
		return err
	}
	m := llm.Model{Name: pos[0], Provider: *provider, BaseURL: *baseURL, Model: *model}
	if *preset != "" {
		p, ok := llm.FindPreset(*preset)
		if !ok {
			return usagef("未知模板 %q（deepseek|claude）", *preset)
		}
		if m.Provider == "" {
			m.Provider = p.Provider
		}
		if m.BaseURL == "" {
			m.BaseURL = p.BaseURL
		}
		if m.Model == "" {
			m.Model = p.Model
		}
	}
	if !*keepKey {
		data, err := io.ReadAll(io.LimitReader(stdin, 64<<10))
		if err != nil {
			return fmt.Errorf("读取密钥失败: %w", err)
		}
		m.APIKey = strings.TrimSpace(string(data))
		if m.APIKey == "" {
			return usagef("密钥从标准输入读取，例如: pbpaste | kuibu llm add %s ...（更新时可用 --keep-key 保留原密钥）", pos[0])
		}
	}

	cfg, err := llm.Load(env)
	if err != nil {
		return err
	}
	if err := cfg.Upsert(m, *makeDefault); err != nil {
		return err
	}
	if err := llm.Save(env, cfg); err != nil {
		return err
	}
	return printModel(cfg, pos[0], common, stdout, "已保存")
}

// llmList 实现 llm list。
func llmList(args []string, common *commonFlags, stdout io.Writer, env func(string) string) error {
	fs := newFlagSet("llm list")
	addCommonFlags(fs, common)
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("用法: kuibu llm list")
	}
	cfg, err := llm.Load(env)
	if err != nil {
		return err
	}
	infos := cfg.Info()
	if common.json {
		return writeJSON(stdout, map[string]any{"default": cfg.Default, "models": infos})
	}
	if len(infos) == 0 {
		fmt.Fprintln(stdout, "还没有配置大模型。示例: pbpaste | kuibu llm add deepseek --preset deepseek")
		return nil
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "\t名称\t提供方\t模型\t接口地址\t密钥")
	for _, m := range infos {
		mark := ""
		if m.Default {
			mark = "*"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", mark, m.Name, m.Provider, m.Model, m.BaseURL, m.KeyMasked)
	}
	return tw.Flush()
}

// llmDefault 实现 llm default。
func llmDefault(args []string, common *commonFlags, stdout io.Writer, env func(string) string) error {
	fs := newFlagSet("llm default")
	addCommonFlags(fs, common)
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(pos, 1, "用法: kuibu llm default <名称>"); err != nil {
		return err
	}
	cfg, err := llm.Load(env)
	if err != nil {
		return err
	}
	if err := cfg.SetDefault(pos[0]); err != nil {
		return err
	}
	if err := llm.Save(env, cfg); err != nil {
		return err
	}
	return printModel(cfg, pos[0], common, stdout, "已设为默认")
}

// llmRemove 实现 llm rm。
func llmRemove(args []string, common *commonFlags, stdout io.Writer, env func(string) string) error {
	fs := newFlagSet("llm rm")
	addCommonFlags(fs, common)
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(pos, 1, "用法: kuibu llm rm <名称>"); err != nil {
		return err
	}
	cfg, err := llm.Load(env)
	if err != nil {
		return err
	}
	if err := cfg.Remove(pos[0]); err != nil {
		return err
	}
	if err := llm.Save(env, cfg); err != nil {
		return err
	}
	if common.json {
		return writeJSON(stdout, map[string]any{"removed": pos[0], "default": cfg.Default})
	}
	fmt.Fprintf(stdout, "已删除 %s\n", pos[0])
	return nil
}

// llmTest 实现 llm test：发固定测试语，不含个人数据。
func llmTest(ctx context.Context, args []string, common *commonFlags, stdout io.Writer, env func(string) string) error {
	fs := newFlagSet("llm test")
	addCommonFlags(fs, common)
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return usagef("用法: kuibu llm test [<名称>]")
	}
	name := ""
	if len(pos) == 1 {
		name = pos[0]
	}
	resp, err := llm.Test(ctx, env, nil, name)
	if err != nil {
		return err
	}
	if common.json {
		return writeJSON(stdout, map[string]any{"ok": true, "reply": resp.Text, "model": resp.Model})
	}
	fmt.Fprintf(stdout, "连通正常（%s）：%s\n", resp.Model, strings.TrimSpace(resp.Text))
	return nil
}

// llmSend 实现 llm send：通用外发入口。正文从标准输入读取。
//
// 不带 --yes 时只输出完整预览并以 NEEDS_CONFIRM(8) 退出，不发请求；
// --dry-run 只输出预览并以 0 退出。
func llmSend(ctx context.Context, args []string, common *commonFlags, stdin io.Reader, stdout io.Writer, env func(string) string) error {
	fs := newFlagSet("llm send")
	addCommonFlags(fs, common)
	model := fs.String("model", "", "模型名称，默认用默认模型")
	system := fs.String("system", "", "系统提示")
	maxTokens := fs.Int("max-tokens", 0, "输出上限")
	yes := fs.Bool("yes", false, "已人工确认预览内容，真正发送")
	dryRun := fs.Bool("dry-run", false, "只输出将发送的内容")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("用法: kuibu llm send [--model 名称] [--system 文本] [--yes|--dry-run] < 正文")
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("读取正文失败: %w", err)
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return usagef("正文从标准输入读取，不能为空")
	}
	req := llm.Request{
		System:    *system,
		Messages:  []llm.Message{{Role: "user", Content: text}},
		MaxTokens: *maxTokens,
	}
	out := &llm.Outbound{Env: env}
	if *dryRun {
		preview, _, err := out.Prepare(*model, req)
		if err != nil {
			return err
		}
		return printPreview(preview, common, stdout)
	}
	preview, resp, err := out.Send(ctx, *model, req, *yes)
	if err != nil {
		if errors.Is(err, llm.ErrNeedsConfirm) {
			if perr := printPreview(preview, common, stdout); perr != nil {
				return perr
			}
			return fmt.Errorf("%w：确认上面的内容无误后加 --yes 重新执行", llm.ErrNeedsConfirm)
		}
		return err
	}
	if common.json {
		return writeJSON(stdout, map[string]any{
			"model":        preview.ModelName,
			"text":         resp.Text,
			"inputTokens":  resp.InputTokens,
			"outputTokens": resp.OutputTokens,
		})
	}
	fmt.Fprintln(stdout, resp.Text)
	return nil
}

// printPreview 输出外发预览。
func printPreview(p llm.Preview, common *commonFlags, stdout io.Writer) error {
	if common.json {
		return writeJSON(stdout, map[string]any{"preview": p})
	}
	fmt.Fprintf(stdout, "将发送到：%s（%s · %s · %s），共 %d 字\n", p.ModelName, p.Provider, p.ModelID, p.BaseURL, p.Chars)
	fmt.Fprintln(stdout, "----------------------------------------")
	fmt.Fprintln(stdout, p.Text)
	fmt.Fprintln(stdout, "----------------------------------------")
	return nil
}

// printModel 输出单个模型（密钥打码）。
func printModel(cfg llm.Config, name string, common *commonFlags, stdout io.Writer, verb string) error {
	for _, m := range cfg.Info() {
		if m.Name != name {
			continue
		}
		if common.json {
			return writeJSON(stdout, m)
		}
		def := ""
		if m.Default {
			def = "（默认）"
		}
		fmt.Fprintf(stdout, "%s %s%s：%s · %s · %s · 密钥 %s\n", verb, m.Name, def, m.Provider, m.Model, m.BaseURL, m.KeyMasked)
		return nil
	}
	return nil
}
