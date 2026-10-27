package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/axu4/kuibu/internal/core"
	"github.com/axu4/kuibu/internal/llm"
	"github.com/axu4/kuibu/internal/store"
)

// draftJSON 是归一稿的 JSON 输出结构。
type draftJSON struct {
	Person        string     `json:"person"`
	Name          string     `json:"name"`
	Week          string     `json:"week"`
	Status        string     `json:"status"`
	SourceHash    string     `json:"sourceHash"`
	SourceChanged bool       `json:"sourceChanged"`
	Model         string     `json:"model"`
	GeneratedAt   *time.Time `json:"generatedAt,omitempty"`
	EditedAt      *time.Time `json:"editedAt,omitempty"`
	FinalizedAt   *time.Time `json:"finalizedAt,omitempty"`
	HumanEdited   bool       `json:"humanEdited"`
	Path          string     `json:"path"`
	Body          *string    `json:"body,omitempty"`
}

// draftJSONFrom 把归一稿转成 JSON 结构；report 为当前原文，用于判断原文是否已更新。
func draftJSONFrom(ws *store.Workspace, d *store.Draft, report *store.Report, withBody bool) draftJSON {
	out := draftJSON{
		Person:        d.Person,
		Name:          d.Name,
		Week:          d.Week,
		Status:        d.Status,
		SourceHash:    d.SourceHash,
		SourceChanged: store.DraftStateFor(d, report).SourceChanged,
		Model:         d.Model,
		GeneratedAt:   timePtr(d.GeneratedAt),
		EditedAt:      timePtr(d.EditedAt),
		FinalizedAt:   timePtr(d.FinalizedAt),
		HumanEdited:   d.HumanEdited,
		Path:          filepath.Join(ws.ReportsDir(), d.Week, draftKindOf(d), d.Person+".md"),
	}
	if withBody {
		body := d.Body
		out.Body = &body
	}
	return out
}

// draftKindOf 返回草稿所在目录，缺省为组员归一稿。
func draftKindOf(d *store.Draft) string {
	if d.Kind == "" {
		return store.DraftDirName
	}
	return d.Kind
}

// timePtr 零值返回 nil。
func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// reportNormalize 实现 report normalize：用 AI 把组员周报原文归一成草稿。
//
// 外发组员内容必须人工确认：不带 --yes 时输出完整预览并以 NEEDS_CONFIRM(8)
// 退出，不发请求；--dry-run 只输出预览。
func reportNormalize(ctx context.Context, args []string, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	fs := newFlagSet("report normalize")
	addCommonFlags(fs, common)
	weekFlag := fs.String("week", "", "周编号，默认本周")
	var persons stringList
	fs.Var(&persons, "person", "组员 ID 或姓名，可重复")
	all := fs.Bool("all", false, "本周已交的全部组员")
	model := fs.String("model", "", "模型名称，默认用默认模型")
	yes := fs.Bool("yes", false, "已人工确认预览内容，真正发送")
	dryRun := fs.Bool("dry-run", false, "只输出将发送的内容")
	force := fs.Bool("force", false, "覆盖已人工修改或已定稿的归一稿（旧版移入历史）")
	level := fs.String("level", store.ReportLevelMember, "层级: member（组员）| lead（组长）")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 || (len(persons) == 0) == !*all {
		return usagef("用法: kuibu report normalize [--week W] [--level member|lead] (--person p... | --all) [--model m] [--dry-run|--yes] [--force]")
	}
	if !store.ValidReportLevel(*level) {
		return usagef("非法层级 %q（member|lead）", *level)
	}

	now, err := nowFrom(env)
	if err != nil {
		return err
	}
	week, err := weekOrThis(*weekFlag, now)
	if err != nil {
		return err
	}
	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return err
	}
	batch, skipped, err := core.PlanNormalizeLevel(ws, week, *level, persons, *force)
	if err != nil {
		return err
	}
	if len(batch.Persons) == 0 {
		if len(skipped) > 0 {
			printSkipped(skipped, common, stdout)
			if hasHumanEdits(skipped) {
				return skippedError(skipped)
			}
			return nil
		}
		return usagef("%s 没有可归一的%s周报", week, levelLabel(*level))
	}

	out := &llm.Outbound{Env: env}
	text := batch.Text
	preview, _, err := out.Prepare(*model, llm.Request{Messages: []llm.Message{{Role: "user", Content: text}}})
	if err != nil {
		return err
	}
	// 合并预览的正文就是逐人请求的拼接，与实际发送的内容一致。
	preview.Text = text
	preview.Chars = len([]rune(text))
	if *dryRun || !*yes {
		if err := printPreview(preview, common, stdout); err != nil {
			return err
		}
		if *dryRun {
			return nil
		}
		return fmt.Errorf("%w：共 %d 人，确认上面的内容无误后加 --yes 重新执行", llm.ErrNeedsConfirm, len(batch.Persons))
	}

	results, err := batch.Run(ctx, ws, out, *model, *force, true, now)
	if err != nil {
		return err
	}
	failed := 0
	for _, r := range results {
		if r.Error != "" {
			failed++
		}
		if r.Draft != nil {
			upsertReportIndex(ws, week, store.NormalizedKind(*level), r.Person, stderr)
		}
	}
	if common.json {
		if err := writeJSON(stdout, map[string]any{"week": week, "results": results, "skipped": skippedOrEmpty(skipped)}); err != nil {
			return err
		}
	} else {
		for _, r := range results {
			switch {
			case r.Error != "":
				fmt.Fprintf(stdout, "✗ %s：%s\n", r.Name, r.Error)
			case r.Warning != "":
				fmt.Fprintf(stdout, "! %s：已生成草稿，%s\n", r.Name, r.Warning)
			default:
				fmt.Fprintf(stdout, "✓ %s：已生成草稿\n", r.Name)
			}
		}
		printSkipped(skipped, common, stdout)
	}
	if hasHumanEdits(skipped) {
		return skippedError(skipped)
	}
	if failed > 0 {
		return fmt.Errorf("%d 人生成失败", failed)
	}
	return nil
}

// printSkipped 输出被跳过的人（文本模式）。
func printSkipped(skipped []core.NormalizeSkip, common *commonFlags, stdout io.Writer) {
	if common.json {
		return
	}
	for _, s := range skipped {
		fmt.Fprintf(stdout, "- 跳过 %s：%s\n", s.Name, s.Reason)
	}
}

// hasHumanEdits 判断跳过的人里是否有因人工修改 / 已定稿而跳过的（组报带入的不算）。
func hasHumanEdits(skipped []core.NormalizeSkip) bool {
	for _, s := range skipped {
		if !strings.Contains(s.Reason, "组报带入") {
			return true
		}
	}
	return false
}

// skippedError 有人因人工修改被跳过时返回 HUMAN_EDITS_EXIST。
func skippedError(skipped []core.NormalizeSkip) error {
	return fmt.Errorf("%w：%d 人的归一稿已人工修改或已定稿，未覆盖（确认覆盖加 --force）", store.ErrHumanEdits, len(skipped))
}

func skippedOrEmpty(s []core.NormalizeSkip) []core.NormalizeSkip {
	if s == nil {
		return []core.NormalizeSkip{}
	}
	return s
}

// reportDraft 实现 report draft show|edit。
func reportDraft(args []string, common *commonFlags, stdin io.Reader, stdout, stderr io.Writer, env func(string) string) error {
	if len(args) == 0 {
		return usagef("report draft 需要子命令: show/edit")
	}
	sub, rest := args[0], args[1:]
	fs := newFlagSet("report draft " + sub)
	addCommonFlags(fs, common)
	file := fs.String("file", "", "从文件读取新正文（edit）")
	level := fs.String("level", store.ReportLevelMember, "层级: member|lead")
	pos, err := parseCommand(fs, rest)
	if err != nil {
		return err
	}
	ws, week, personID, err := draftTarget(pos, common, env, "用法: kuibu report draft "+sub+" <week> <person> [--level member|lead]")
	if err != nil {
		return err
	}
	kind := store.NormalizedKind(*level)
	switch sub {
	case "show":
		return showDraft(ws, kind, *level, week, personID, common, stdout)
	case "edit":
		var data []byte
		if *file != "" {
			data, err = os.ReadFile(*file)
		} else {
			data, err = io.ReadAll(stdin)
		}
		if err != nil {
			return fmt.Errorf("读取正文失败: %w", err)
		}
		now, err := nowFrom(env)
		if err != nil {
			return err
		}
		d, err := ws.EditDraftKind(kind, week, personID, string(data), now)
		if err != nil {
			return err
		}
		upsertReportIndex(ws, week, kind, personID, stderr)
		return printDraftResult(ws, d, *level, common, stdout, "已保存修改")
	default:
		return usagef("未知 report draft 子命令 %q", sub)
	}
}

// reportFinalize 实现 report finalize / unfinalize。
func reportFinalize(args []string, finalize bool, common *commonFlags, stdout, stderr io.Writer, env func(string) string) error {
	name := "unfinalize"
	if finalize {
		name = "finalize"
	}
	fs := newFlagSet("report " + name)
	addCommonFlags(fs, common)
	level := fs.String("level", store.ReportLevelMember, "层级: member|lead")
	pos, err := parseCommand(fs, args)
	if err != nil {
		return err
	}
	ws, week, personID, err := draftTarget(pos, common, env, "用法: kuibu report "+name+" <week> <person> [--level member|lead]")
	if err != nil {
		return err
	}
	kind := store.NormalizedKind(*level)
	var d *store.Draft
	verb := "已取消定稿"
	if finalize {
		now, err := nowFrom(env)
		if err != nil {
			return err
		}
		d, err = ws.FinalizeDraftKind(kind, week, personID, now)
		if err != nil {
			return err
		}
		verb = "已定稿"
	} else {
		d, err = ws.UnfinalizeDraftKind(kind, week, personID)
		if err != nil {
			return err
		}
	}
	upsertReportIndex(ws, week, kind, personID, stderr)
	return printDraftResult(ws, d, *level, common, stdout, verb)
}

// draftTarget 解析 <week> <person> 位置参数。
func draftTarget(pos []string, common *commonFlags, env func(string) string, usage string) (*store.Workspace, string, string, error) {
	if err := requireArgs(pos, 2, usage); err != nil {
		return nil, "", "", err
	}
	now, err := nowFrom(env)
	if err != nil {
		return nil, "", "", err
	}
	week, err := store.ParseWeek(pos[0], now)
	if err != nil {
		return nil, "", "", err
	}
	ws, err := resolveWorkspace(common, env)
	if err != nil {
		return nil, "", "", err
	}
	personID := pos[1]
	if p, err := ws.ResolvePerson(pos[1]); err == nil {
		personID = p.ID
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, "", "", err
	}
	return ws, week, personID, nil
}

// showDraft 输出归一稿。
func showDraft(ws *store.Workspace, kind, level, week, personID string, common *commonFlags, stdout io.Writer) error {
	d, err := ws.ReadDraftKind(kind, week, personID)
	if err != nil {
		return err
	}
	report, _ := ws.ReadReport(week, level, personID)
	if common.json {
		return writeJSON(stdout, draftJSONFrom(ws, d, report, true))
	}
	st := store.DraftStateFor(d, report)
	fmt.Fprintf(stdout, "%s  %s  %s  %s\n", d.Week, orDash(d.Name), d.Person, draftStateLabel(st))
	fmt.Fprintf(stdout, "模型: %s  生成: %s", orDash(d.Model), formatLocal(d.GeneratedAt))
	if !d.EditedAt.IsZero() {
		fmt.Fprintf(stdout, "  修改: %s", formatLocal(d.EditedAt))
	}
	if !d.FinalizedAt.IsZero() {
		fmt.Fprintf(stdout, "  定稿: %s", formatLocal(d.FinalizedAt))
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, d.Body)
	return nil
}

// printDraftResult 输出写操作后的归一稿摘要。
func printDraftResult(ws *store.Workspace, d *store.Draft, level string, common *commonFlags, stdout io.Writer, verb string) error {
	report, _ := ws.ReadReport(d.Week, level, d.Person)
	if common.json {
		return writeJSON(stdout, draftJSONFrom(ws, d, report, false))
	}
	fmt.Fprintf(stdout, "%s %s %s（%s）\n", verb, d.Week, orDash(d.Name), draftStateLabel(store.DraftStateFor(d, report)))
	return nil
}

// draftStateLabel 返回归一状态的中文。
func draftStateLabel(st store.DraftState) string {
	label := map[string]string{"none": "未归一", store.DraftStatusDraft: "草稿", store.DraftStatusFinal: "已定稿"}[st.Status]
	if st.FromGroup {
		label = "定稿（组报）"
	}
	if label == "" {
		label = st.Status
	}
	if st.SourceChanged {
		label += "·原文已更新"
	}
	return label
}

// weekOrThis 解析 --week，空值为本周。
func weekOrThis(s string, now time.Time) (string, error) {
	if strings.TrimSpace(s) == "" {
		return store.WeekOf(now), nil
	}
	return store.ParseWeek(s, now)
}
