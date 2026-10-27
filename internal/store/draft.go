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

// GroupDirName 是组报目录：reports/<周>/group/<组长 personId>.md。格式与归一稿相同。
const GroupDirName = "group"

// LeadDraftDirName 是组长周报归一稿目录：reports/<周>/lead-normalized/<personId>.md。
const LeadDraftDirName = "lead-normalized"

// ProductDirName 是 SE 产品 / 项目周报目录：reports/<周>/product/product.md。
const ProductDirName = "product"

// ProductReportID 是产品报的文件名（每周一份）。
const ProductReportID = "product"

// DraftKinds 是所有草稿 / 定稿类文件的目录名，索引据此识别。
var DraftKinds = []string{DraftDirName, LeadDraftDirName, GroupDirName, ProductDirName}

// IsDraftKind 判断目录名是否是草稿 / 定稿类文件。
func IsDraftKind(dir string) bool {
	for _, k := range DraftKinds {
		if k == dir {
			return true
		}
	}
	return false
}

// NormalizedKind 返回某层级原文对应的归一稿目录：member → member-normalized，lead → lead-normalized。
func NormalizedKind(level string) string {
	if level == ReportLevelLead {
		return LeadDraftDirName
	}
	return DraftDirName
}

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
	// Review 是「对照上周计划」的结果，只存在 front matter，不影响定稿状态。
	Review *DraftReview
	// Kind 是所在目录：member-normalized（归一稿）或 group（组报），不写入文件。
	Kind string
	// Group 是组报所属组（仅组报）。
	Group string
	// Sources 是组报纳入的组员 ID（仅组报）。
	Sources []string
	// Missing 是组报生成时未定稿、未纳入的组员姓名（仅组报）。
	Missing []string
	// Extra 保存 front matter 中未识别的字段。
	Extra map[string]any

	extraNodes map[string]*yaml.Node
}

// knownDraftKeys 是归一稿认识的 front matter 字段。
var knownDraftKeys = map[string]bool{
	"person": true, "name": true, "week": true, "status": true, "sourceHash": true,
	"model": true, "generatedAt": true, "editedAt": true, "finalizedAt": true, "humanEdited": true,
	"review": true, "group": true, "sources": true, "missing": true,
}

// 对照状态。
const (
	ReviewDone     = "done"
	ReviewPartial  = "partial"
	ReviewDelayed  = "delayed"
	ReviewCanceled = "canceled"
)

// ValidReviewStatus 判断对照状态是否合法。
func ValidReviewStatus(s string) bool {
	switch s {
	case ReviewDone, ReviewPartial, ReviewDelayed, ReviewCanceled:
		return true
	}
	return false
}

// ReviewItem 是上周一条计划的对照结果。
type ReviewItem struct {
	// Plan 是上周计划条目 id。
	Plan string `yaml:"plan" json:"plan"`
	// Status 是 done / partial / delayed / canceled。
	Status string `yaml:"status" json:"status"`
	// Matches 是对应的本周完成项 id。
	Matches []string `yaml:"matches,omitempty" json:"matches"`
	// Note 是备注。
	Note string `yaml:"note,omitempty" json:"note"`
	// Source 是 ai（AI 建议，未经人工确认）或 human。
	Source string `yaml:"source,omitempty" json:"source"`
}

// DraftReview 是本周归一稿上记录的对照结果。
type DraftReview struct {
	Items []ReviewItem `yaml:"items" json:"items"`
	// Carried 是已顺延到本周「下周计划」的上周计划 id。
	Carried []string `yaml:"carried,omitempty" json:"carried"`
}

// SaveDraftReview 只更新归一稿 front matter 的对照结果，不改正文与定稿状态。
func (w *Workspace) SaveDraftReview(week, personID string, review DraftReview) (*Draft, error) {
	d, err := w.ReadDraft(week, personID)
	if err != nil {
		return nil, err
	}
	d.Review = &review
	if err := w.writeDraft(d); err != nil {
		return nil, err
	}
	return d, nil
}

// SourceHash 返回原文正文的 sha256（十六进制）。
func SourceHash(body string) string {
	sum := sha256.Sum256([]byte(trimBlankLines(body)))
	return hex.EncodeToString(sum[:])
}

// kindOrDraft 把空 kind 视为归一稿。
func kindOrDraft(kind string) string {
	if kind == "" {
		return DraftDirName
	}
	return kind
}

// kindNoun 返回提示用的名称。
func kindNoun(kind string) string {
	switch kind {
	case GroupDirName:
		return "组报"
	case ProductDirName:
		return "产品报"
	}
	return "归一稿"
}

// ReadDraftKind 读取任意目录的草稿 / 定稿文件。
func (w *Workspace) ReadDraftKind(kind, week, personID string) (*Draft, error) {
	return ReadDraftFile(w.draftPath(kind, week, personID))
}

// draftPath 返回归一稿 / 组报路径。
func (w *Workspace) draftPath(kind, week, personID string) string {
	return filepath.Join(w.ReportsDir(), week, kindOrDraft(kind), personID+noteExt)
}

// ReadDraft 读取归一稿；不存在时返回 ErrNotFound。
func (w *Workspace) ReadDraft(week, personID string) (*Draft, error) {
	return ReadDraftFile(w.draftPath(DraftDirName, week, personID))
}

// ReadGroupReport 读取组报；leadID 是组长的组员 ID。
func (w *Workspace) ReadGroupReport(week, leadID string) (*Draft, error) {
	return ReadDraftFile(w.draftPath(GroupDirName, week, leadID))
}

// FinalDraft 返回已定稿的归一稿，未定稿时返回 ErrNotFinal。下一级引用与导出走它。
func (w *Workspace) FinalDraft(week, personID string) (*Draft, error) {
	return w.FinalDraftKind(DraftDirName, week, personID)
}

// FinalDraftKind 同 FinalDraft，kind 指定目录。
func (w *Workspace) FinalDraftKind(kind, week, personID string) (*Draft, error) {
	d, err := w.ReadDraftKind(kind, week, personID)
	if err != nil {
		return nil, err
	}
	if d.Status != DraftStatusFinal {
		return nil, newReportError(ErrNotFinal, "%s 的 %s %s尚未定稿", d.displayName(), week, kindNoun(kind))
	}
	return d, nil
}

// ListDrafts 返回某周全部组员归一稿，按人员 ID 排序。
func (w *Workspace) ListDrafts(week string) ([]*Draft, error) {
	return w.ListDraftsKind(DraftDirName, week)
}

// ListDraftsKind 同 ListDrafts，kind 指定目录。
func (w *Workspace) ListDraftsKind(kind, week string) ([]*Draft, error) {
	dir := filepath.Join(w.ReportsDir(), week, kind)
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
	// Kind 为空表示归一稿，GroupDirName 表示组报。
	Kind       string
	Group      string
	Sources    []string
	Missing    []string
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
	return w.CheckRegenerateKind(DraftDirName, week, personID, force)
}

// CheckRegenerateKind 同 CheckRegenerate，kind 指定归一稿或组报。
func (w *Workspace) CheckRegenerateKind(kind, week, personID string, force bool) error {
	old, err := ReadDraftFile(w.draftPath(kind, week, personID))
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
	return newReportError(ErrHumanEdits, "%s 的%s%s，重新生成会覆盖；确认要覆盖请加 --force", old.displayName(), kindNoun(kind), reason)
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
	kind := kindOrDraft(in.Kind)
	if err := w.CheckRegenerateKind(kind, in.Week, in.Person, force); err != nil {
		return nil, err
	}
	if err := w.archiveDraft(kind, in.Week, in.Person); err != nil {
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
		Kind:        kind,
		Group:       in.Group,
		Sources:     in.Sources,
		Missing:     in.Missing,
		Extra:       map[string]any{},
	}
	if err := w.writeDraft(d); err != nil {
		return nil, err
	}
	return d, nil
}

// EditDraft 保存人工修改后的正文。已定稿时返回 ErrFinalized；正文未变时不写盘。
func (w *Workspace) EditDraft(week, personID, body string, now time.Time) (*Draft, error) {
	return w.EditDraftKind(DraftDirName, week, personID, body, now)
}

// EditDraftKind 同 EditDraft，kind 指定归一稿或组报。
func (w *Workspace) EditDraftKind(kind, week, personID, body string, now time.Time) (*Draft, error) {
	d, err := ReadDraftFile(w.draftPath(kind, week, personID))
	if err != nil {
		return nil, err
	}
	if d.Status == DraftStatusFinal {
		return nil, newReportError(ErrFinalized, "%s 的%s已定稿，请先取消定稿再修改", d.displayName(), kindNoun(kind))
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
	return w.FinalizeDraftKind(DraftDirName, week, personID, now)
}

// FinalizeDraftKind 同 FinalizeDraft，kind 指定归一稿或组报。
func (w *Workspace) FinalizeDraftKind(kind, week, personID string, now time.Time) (*Draft, error) {
	d, err := ReadDraftFile(w.draftPath(kind, week, personID))
	if err != nil {
		return nil, err
	}
	if d.Status == DraftStatusFinal {
		return d, nil
	}
	if strings.TrimSpace(d.Body) == "" {
		return nil, fmt.Errorf("%w: %s正文为空，不能定稿", ErrInvalid, kindNoun(kind))
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
	return w.UnfinalizeDraftKind(DraftDirName, week, personID)
}

// UnfinalizeDraftKind 同 UnfinalizeDraft，kind 指定归一稿或组报。
func (w *Workspace) UnfinalizeDraftKind(kind, week, personID string) (*Draft, error) {
	d, err := ReadDraftFile(w.draftPath(kind, week, personID))
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
	// FromGroup 表示组长周报由定稿组报带入，直接视为定稿。
	FromGroup bool `json:"fromGroup,omitempty"`
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

// archiveDraft 把现有归一稿 / 组报移入 .history/<kind>/，不存在时什么也不做。
func (w *Workspace) archiveDraft(kind, week, personID string) error {
	path := w.draftPath(kind, week, personID)
	old, err := ReadDraftFile(path)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	destDir := filepath.Join(w.ReportsDir(), week, historyDirName, kindOrDraft(kind))
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
	d := &Draft{Person: id, Status: DraftStatusDraft, Body: string(data), Kind: filepath.Base(filepath.Dir(path)), Extra: map[string]any{}}
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
				case "review":
					var rv DraftReview
					if err := val.Decode(&rv); err == nil {
						d.Review = &rv
					}
				case "group":
					_ = val.Decode(&d.Group)
				case "sources":
					_ = val.Decode(&d.Sources)
				case "missing":
					_ = val.Decode(&d.Missing)
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
	if d.Review != nil && (len(d.Review.Items) > 0 || len(d.Review.Carried) > 0) {
		if err := appendFrontMatterValue(root, "review", d.Review); err != nil {
			return err
		}
	}
	if k := kindOrDraft(d.Kind); k == GroupDirName || k == ProductDirName {
		appendFrontMatterKey(root, "group", stringNode(d.Group))
		if err := appendFrontMatterValue(root, "sources", nonNil(d.Sources)); err != nil {
			return err
		}
		if err := appendFrontMatterValue(root, "missing", nonNil(d.Missing)); err != nil {
			return err
		}
	}
	if err := appendExtraFrontMatter(root, d.Extra, d.extraNodes, knownDraftKeys); err != nil {
		return err
	}
	data, err := marshalFrontMatter(root, ensureTrailingNewline(d.Body))
	if err != nil {
		return err
	}
	path := w.draftPath(d.Kind, d.Week, d.Person)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建归一稿目录失败: %w", err)
	}
	if err := writeFileAtomic(path, data); err != nil {
		return fmt.Errorf("写入归一稿 %s 失败: %w", d.Person, err)
	}
	return nil
}

// nonNil 保证切片序列化为 [] 而不是 null。
func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

// optTime 零值输出空字符串，其余按 RFC3339。
func optTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
