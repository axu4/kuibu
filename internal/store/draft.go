package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// DraftDirName 是组员周报归一稿目录：reports/<周>/member-normalized/<personId>.md。
//
// 生命周期：原文（只读）→ AI 草稿 → 人工修改 → 定稿。下一级引用与导出只认定稿。
const DraftDirName = "member-normalized"

// 归一稿状态。
const (
	DraftStatusDraft = "draft"
	DraftStatusFinal = "final"
)

// 归一稿相关的哨兵错误。
var (
	// ErrHumanEdits 表示归一稿已被人工修改或已定稿，拒绝被 AI 覆盖（退出码 9）。
	ErrHumanEdits = errors.New("归一稿存在人工修改，拒绝覆盖")
	// ErrFinalized 表示归一稿已定稿，需先取消定稿才能修改。
	ErrFinalized = errors.New("归一稿已定稿，请先取消定稿")
	// ErrNotFinal 表示引用或导出的归一稿尚未定稿（退出码 7）。
	ErrNotFinal = errors.New("归一稿尚未定稿")
)

// Draft 是一份组员周报的归一稿。
type Draft struct {
	// Person 是组员 ID（同时也是文件名）。
	Person string
	// Name 是姓名快照。
	Name string
	// Week 是 ISO 周编号。
	Week string
	// Status 是 draft / final。
	Status string
	// SourceHash 是生成时原文正文的 sha256，原文变了就与当前原文不一致。
	SourceHash string
	// Model 是生成所用的模型名称。
	Model string
	// GeneratedAt 是 AI 生成时间。
	GeneratedAt time.Time
	// EditedAt 是最近一次人工修改时间，未改过为零值。
	EditedAt time.Time
	// FinalizedAt 是定稿时间，未定稿为零值。
	FinalizedAt time.Time
	// HumanEdited 表示是否被人工改过。
	HumanEdited bool
	// Body 是归一后的 Markdown 正文。
	Body string
	// Extra 保存 front matter 中未识别的字段。
	Extra map[string]any

	extraNodes map[string]*yaml.Node
}

// knownDraftKeys 是归一稿认识的 front matter 字段。
var knownDraftKeys = map[string]bool{
	"person": true, "name": true, "week": true, "status": true, "sourceHash": true,
	"model": true, "generatedAt": true, "editedAt": true, "finalizedAt": true, "humanEdited": true,
}

// SourceHash 返回原文正文的 sha256（十六进制）。
func SourceHash(body string) string {
	sum := sha256.Sum256([]byte(trimBlankLines(body)))
	return hex.EncodeToString(sum[:])
}

// draftPath 返回归一稿路径。
func (w *Workspace) draftPath(week, personID string) string {
	return filepath.Join(w.ReportsDir(), week, DraftDirName, personID+noteExt)
}

// ReadDraft 读取归一稿；不存在时返回 ErrNotFound。
func (w *Workspace) ReadDraft(week, personID string) (*Draft, error) {
	return ReadDraftFile(w.draftPath(week, personID))
}

// FinalDraft 返回已定稿的归一稿，未定稿时返回 ErrNotFinal。下一级引用与导出走它。
func (w *Workspace) FinalDraft(week, personID string) (*Draft, error) {
	d, err := w.ReadDraft(week, personID)
	if err != nil {
		return nil, err
	}
	if d.Status != DraftStatusFinal {
		return nil, newReportError(ErrNotFinal, "%s 的 %s 归一稿尚未定稿", d.displayName(), week)
	}
	return d, nil
}

// ListDrafts 返回某周全部归一稿，按人员 ID 排序。
func (w *Workspace) ListDrafts(week string) ([]*Draft, error) {
	dir := filepath.Join(w.ReportsDir(), week, DraftDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []*Draft{}, nil
		}
		return nil, fmt.Errorf("读取归一稿目录失败: %w", err)
	}
	var drafts []*Draft
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !strings.HasSuffix(e.Name(), noteExt) {
			continue
		}
		d, err := ReadDraftFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		drafts = append(drafts, d)
	}
	sort.Slice(drafts, func(i, j int) bool { return drafts[i].Person < drafts[j].Person })
	return drafts, nil
}

// GeneratedDraft 是 AI 生成结果。
type GeneratedDraft struct {
	Person     string
	Name       string
	Week       string
	SourceHash string
	Model      string
	Body       string
	Now        time.Time
}

// CheckRegenerate 判断能否用 AI 结果覆盖现有归一稿：已人工修改或已定稿时，
// 不带 force 返回 ErrHumanEdits。不存在归一稿时返回 nil。
func (w *Workspace) CheckRegenerate(week, personID string, force bool) error {
	old, err := w.ReadDraft(week, personID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if force || (!old.HumanEdited && old.Status != DraftStatusFinal) {
		return nil
	}
	reason := "已人工修改"
	if old.Status == DraftStatusFinal {
		reason = "已定稿"
	}
	return newReportError(ErrHumanEdits, "%s 的归一稿%s，重新生成会覆盖；确认要覆盖请加 --force", old.displayName(), reason)
}

// SaveGeneratedDraft 写入 AI 生成的草稿。已有归一稿被人工修改或已定稿时，
// 不带 force 返回 ErrHumanEdits；覆盖前旧版移入 reports/<周>/.history/member-normalized/。
func (w *Workspace) SaveGeneratedDraft(in GeneratedDraft, force bool) (*Draft, error) {
	if strings.TrimSpace(in.Person) == "" {
		return nil, fmt.Errorf("%w: 归一稿人员 ID 不能为空", ErrInvalid)
	}
	if _, _, err := WeekRange(in.Week); err != nil {
		return nil, err
	}
	if err := w.CheckRegenerate(in.Week, in.Person, force); err != nil {
		return nil, err
	}
	if err := w.archiveDraft(in.Week, in.Person); err != nil {
		return nil, err
	}
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	d := &Draft{
		Person:      in.Person,
		Name:        in.Name,
		Week:        in.Week,
		Status:      DraftStatusDraft,
		SourceHash:  in.SourceHash,
		Model:       in.Model,
		GeneratedAt: now.Truncate(time.Second),
		Body:        trimBlankLines(in.Body),
		Extra:       map[string]any{},
	}
	if err := w.writeDraft(d); err != nil {
		return nil, err
	}
	return d, nil
}

// EditDraft 保存人工修改后的正文。已定稿时返回 ErrFinalized；正文未变时不写盘。
func (w *Workspace) EditDraft(week, personID, body string, now time.Time) (*Draft, error) {
	d, err := w.ReadDraft(week, personID)
	if err != nil {
		return nil, err
	}
	if d.Status == DraftStatusFinal {
		return nil, newReportError(ErrFinalized, "%s 的归一稿已定稿，请先取消定稿再修改", d.displayName())
	}
	body = trimBlankLines(body)
	if body == d.Body {
		return d, nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	d.Body = body
	d.HumanEdited = true
	d.EditedAt = now.Truncate(time.Second)
	if err := w.writeDraft(d); err != nil {
		return nil, err
	}
	return d, nil
}

// FinalizeDraft 定稿。已定稿时原样返回。
func (w *Workspace) FinalizeDraft(week, personID string, now time.Time) (*Draft, error) {
	d, err := w.ReadDraft(week, personID)
	if err != nil {
		return nil, err
	}
	if d.Status == DraftStatusFinal {
		return d, nil
	}
	if strings.TrimSpace(d.Body) == "" {
		return nil, fmt.Errorf("%w: 归一稿正文为空，不能定稿", ErrInvalid)
	}
	if now.IsZero() {
		now = time.Now()
	}
	d.Status = DraftStatusFinal
	d.FinalizedAt = now.Truncate(time.Second)
	if err := w.writeDraft(d); err != nil {
		return nil, err
	}
	return d, nil
}

// UnfinalizeDraft 取消定稿，回到草稿。
func (w *Workspace) UnfinalizeDraft(week, personID string) (*Draft, error) {
	d, err := w.ReadDraft(week, personID)
	if err != nil {
		return nil, err
	}
	if d.Status != DraftStatusFinal {
		return d, nil
	}
	d.Status = DraftStatusDraft
	d.FinalizedAt = time.Time{}
	if err := w.writeDraft(d); err != nil {
		return nil, err
	}
	return d, nil
}

// DraftState 汇总某人某周的归一状态，供列表和状态展示。
type DraftState struct {
	// Status 是 none / draft / final。
	Status string `json:"status"`
	// SourceChanged 表示原文在生成后又被重新提交过。
	SourceChanged bool `json:"sourceChanged"`
	// HumanEdited 表示归一稿被人工改过。
	HumanEdited bool `json:"humanEdited"`
}

// DraftStateFor 返回归一状态；report 为当前原文，可为 nil。
func DraftStateFor(d *Draft, report *Report) DraftState {
	if d == nil {
		return DraftState{Status: "none"}
	}
	st := DraftState{Status: d.Status, HumanEdited: d.HumanEdited}
	if report != nil && d.SourceHash != "" && d.SourceHash != SourceHash(report.Body) {
		st.SourceChanged = true
	}
	return st
}

// displayName 返回用于提示的姓名，缺省用 ID。
func (d *Draft) displayName() string {
	if d.Name != "" {
		return d.Name
	}
	return d.Person
}

// archiveDraft 把现有归一稿移入 .history/member-normalized/，不存在时什么也不做。
func (w *Workspace) archiveDraft(week, personID string) error {
	path := w.draftPath(week, personID)
	old, err := ReadDraftFile(path)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	destDir := filepath.Join(w.ReportsDir(), week, historyDirName, DraftDirName)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("创建归一稿历史目录失败: %w", err)
	}
	stamp := old.EditedAt
	if stamp.IsZero() {
		stamp = old.GeneratedAt
	}
	base := personID + "-" + compactTime(stamp)
	dest := filepath.Join(destDir, base+noteExt)
	for i := 1; ; i++ {
		if _, err := os.Stat(dest); errors.Is(err, os.ErrNotExist) {
			break
		}
		dest = filepath.Join(destDir, fmt.Sprintf("%s-%d%s", base, i, noteExt))
	}
	if err := os.Rename(path, dest); err != nil {
		return fmt.Errorf("归档旧归一稿失败: %w", err)
	}
	return nil
}

// ReadDraftFile 读取任意路径的归一稿文件。
func ReadDraftFile(path string) (*Draft, error) {
	id := IDFromPath(path)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, newReportError(ErrNotFound, "归一稿不存在: %s", id)
		}
		return nil, fmt.Errorf("读取归一稿失败: %w", err)
	}
	d := &Draft{Person: id, Status: DraftStatusDraft, Body: string(data), Extra: map[string]any{}}
	if front, body, ok := splitFrontMatter(data); ok {
		d.Body = trimTrailingNewline(string(body))
		if root, _, ok := frontMatterMapping(front); ok {
			d.extraNodes = map[string]*yaml.Node{}
			for i := 0; i+1 < len(root.Content); i += 2 {
				key, val := root.Content[i], root.Content[i+1]
				switch key.Value {
				case "person":
					_ = val.Decode(&d.Person)
				case "name":
					_ = val.Decode(&d.Name)
				case "week":
					_ = val.Decode(&d.Week)
				case "status":
					_ = val.Decode(&d.Status)
				case "sourceHash":
					_ = val.Decode(&d.SourceHash)
				case "model":
					_ = val.Decode(&d.Model)
				case "generatedAt":
					d.GeneratedAt = decodeTime(val)
				case "editedAt":
					d.EditedAt = decodeTime(val)
				case "finalizedAt":
					d.FinalizedAt = decodeTime(val)
				case "humanEdited":
					_ = val.Decode(&d.HumanEdited)
				default:
					var v any
					if err := val.Decode(&v); err == nil {
						d.Extra[key.Value] = v
					}
					d.extraNodes[key.Value] = val
				}
			}
		}
	}
	if d.Person == "" {
		d.Person = id
	}
	if d.Status != DraftStatusFinal {
		d.Status = DraftStatusDraft
	}
	return d, nil
}

// writeDraft 原子写回归一稿。
func (w *Workspace) writeDraft(d *Draft) error {
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	appendFrontMatterKey(root, "person", stringNode(d.Person))
	appendFrontMatterKey(root, "name", stringNode(d.Name))
	appendFrontMatterKey(root, "week", stringNode(d.Week))
	appendFrontMatterKey(root, "status", stringNode(d.Status))
	appendFrontMatterKey(root, "sourceHash", stringNode(d.SourceHash))
	appendFrontMatterKey(root, "model", stringNode(d.Model))
	appendFrontMatterKey(root, "generatedAt", stringNode(optTime(d.GeneratedAt)))
	appendFrontMatterKey(root, "editedAt", stringNode(optTime(d.EditedAt)))
	appendFrontMatterKey(root, "finalizedAt", stringNode(optTime(d.FinalizedAt)))
	if err := appendFrontMatterValue(root, "humanEdited", d.HumanEdited); err != nil {
		return err
	}
	if err := appendExtraFrontMatter(root, d.Extra, d.extraNodes, knownDraftKeys); err != nil {
		return err
	}
	data, err := marshalFrontMatter(root, ensureTrailingNewline(d.Body))
	if err != nil {
		return err
	}
	path := w.draftPath(d.Week, d.Person)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建归一稿目录失败: %w", err)
	}
	if err := writeFileAtomic(path, data); err != nil {
		return fmt.Errorf("写入归一稿 %s 失败: %w", d.Person, err)
	}
	return nil
}

// optTime 零值输出空字符串，其余按 RFC3339。
func optTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
