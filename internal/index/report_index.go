package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/axu4/kuibu/internal/store"
)

// fileKind 判断路径属于哪类索引：note / todo / people / report，无法识别时
// 返回空字符串。周报仅识别 reports/<周>/<层级>/<person>.md 这样的三层路径，
// .history/ 与隐藏目录不参与。
func fileKind(ws *store.Workspace, path string) string {
	path = filepath.Clean(path)
	switch filepath.Dir(path) {
	case filepath.Clean(ws.NotesDir()):
		return "note"
	case filepath.Clean(ws.TodosDir()):
		return "todo"
	case filepath.Clean(ws.PeopleDir()):
		return "people"
	}

	reportsDir := filepath.Clean(ws.ReportsDir())
	if !strings.HasPrefix(path, reportsDir+string(filepath.Separator)) {
		return ""
	}
	rel, err := filepath.Rel(reportsDir, path)
	if err != nil {
		return ""
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) != 3 || !strings.HasSuffix(parts[2], ".md") {
		return ""
	}
	if parts[0] == store.HistoryDirName || parts[1] == store.HistoryDirName {
		return ""
	}
	return "report"
}

// isPeoplePath 判断路径是否位于组员目录下。
func isPeoplePath(ws *store.Workspace, path string) bool {
	return filepath.Dir(filepath.Clean(path)) == filepath.Clean(ws.PeopleDir())
}

// isReportPath 判断路径是否是周报文件。
func isReportPath(ws *store.Workspace, path string) bool {
	return fileKind(ws, path) == "report"
}

// bumpStats 按文件类型累计 Rebuild/Sync 的统计。
func bumpStats(kind string, change Change, stats *Stats) {
	switch kind {
	case "todo":
		switch change.Kind {
		case ChangeAdded:
			stats.TodoAdded++
		case ChangeUpdated:
			stats.TodoUpdated++
		case ChangeRemoved:
			stats.TodoRemoved++
		}
	case "people":
		switch change.Kind {
		case ChangeAdded:
			stats.PeopleAdded++
		case ChangeUpdated:
			stats.PeopleUpdated++
		case ChangeRemoved:
			stats.PeopleRemoved++
		}
	case "report":
		switch change.Kind {
		case ChangeAdded:
			stats.ReportAdded++
		case ChangeUpdated:
			stats.ReportUpdated++
		case ChangeRemoved:
			stats.ReportRemoved++
		}
	default:
		switch change.Kind {
		case ChangeAdded:
			stats.Added++
		case ChangeUpdated:
			stats.Updated++
		case ChangeRemoved:
			stats.Removed++
		}
	}
}

// countPeople 返回索引中的组员总数。
func countPeople(ctx context.Context, tx *sql.Tx) (int, error) {
	var total int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM people").Scan(&total); err != nil {
		return 0, fmt.Errorf("统计索引组员数失败: %w", err)
	}
	return total, nil
}

// countReports 返回索引中的周报总数。
func countReports(ctx context.Context, tx *sql.Tx) (int, error) {
	var total int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM reports").Scan(&total); err != nil {
		return 0, fmt.Errorf("统计索引周报数失败: %w", err)
	}
	return total, nil
}

// reportRID 生成周报在索引里的唯一 ID（week/level/person）。
func reportRID(r *store.Report) string {
	return r.Week + "/" + r.Level + "/" + r.Person
}

// upsertPerson 读取组员文件并写入组员索引，返回变化类型。
func (ix *Index) upsertPerson(ctx context.Context, tx *sql.Tx, path string, force bool) (Change, error) {
	path = filepath.Clean(path)

	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		id, removed, err := deletePerson(ctx, tx, path)
		if err != nil {
			return Change{}, err
		}
		if removed {
			return Change{ID: id, Kind: ChangeRemoved}, nil
		}
		return Change{Kind: ChangeUnchanged}, nil
	}
	if err != nil {
		return Change{}, fmt.Errorf("读取组员文件信息失败: %w", err)
	}
	if info.IsDir() {
		return Change{Kind: ChangeUnchanged}, nil
	}

	person, err := store.ReadPersonFile(path)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			id, removed, delErr := deletePerson(ctx, tx, path)
			if delErr != nil {
				return Change{}, delErr
			}
			if removed {
				return Change{ID: id, Kind: ChangeRemoved}, nil
			}
			return Change{Kind: ChangeUnchanged}, nil
		}
		return Change{}, err
	}

	var (
		oldMtime int64
		oldSize  int64
		exists   bool
	)
	err = tx.QueryRowContext(ctx,
		"SELECT mtime, size FROM people WHERE path = ?", path).Scan(&oldMtime, &oldSize)
	switch {
	case err == nil:
		exists = true
	case errors.Is(err, sql.ErrNoRows):
		err = tx.QueryRowContext(ctx,
			"SELECT mtime, size FROM people WHERE id = ?", person.ID).Scan(&oldMtime, &oldSize)
		switch {
		case err == nil:
			exists = true
		case errors.Is(err, sql.ErrNoRows):
			// 新组员。
		default:
			return Change{}, fmt.Errorf("查询组员索引项失败: %w", err)
		}
	default:
		return Change{}, fmt.Errorf("查询组员索引项失败: %w", err)
	}

	if exists && !force && oldMtime == info.ModTime().UnixNano() && oldSize == info.Size() {
		return Change{ID: person.ID, Kind: ChangeUnchanged}, nil
	}

	for _, stmt := range []string{
		"DELETE FROM people_fts WHERE id = ?",
		"DELETE FROM people WHERE path = ? OR id = ?",
	} {
		args := []any{person.ID}
		if strings.HasPrefix(stmt, "DELETE FROM people WHERE") {
			args = []any{path, person.ID}
		}
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return Change{}, fmt.Errorf("清理旧组员索引项失败: %w", err)
		}
	}

	aliases, err := json.Marshal(stringsOrEmpty(person.Aliases))
	if err != nil {
		aliases = []byte("[]")
	}
	products, err := json.Marshal(stringsOrEmpty(person.ProductLines))
	if err != nil {
		products = []byte("[]")
	}
	history, err := json.Marshal(person.GroupHistory)
	if err != nil {
		history = []byte("[]")
	}
	extra := []byte("{}")
	if len(person.Extra) > 0 {
		if data, err := json.Marshal(person.Extra); err == nil {
			extra = data
		}
	}
	active := 0
	if person.Active {
		active = 1
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO people
		(id, name, aliases, grp, product_lines, role, active, group_history,
		 created, updated, path, mtime, size, extra)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		person.ID, person.Name, string(aliases), person.Group, string(products),
		person.Role, active, string(history), formatTime(person.Created),
		formatTime(person.Updated), path, info.ModTime().UnixNano(), info.Size(), string(extra))
	if err != nil {
		return Change{}, fmt.Errorf("写入组员索引项失败: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO people_fts (id, name, body) VALUES (?, ?, ?)",
		person.ID, person.Name, person.Body); err != nil {
		return Change{}, fmt.Errorf("写入组员全文索引失败: %w", err)
	}

	if exists {
		return Change{ID: person.ID, Kind: ChangeUpdated}, nil
	}
	return Change{ID: person.ID, Kind: ChangeAdded}, nil
}

// upsertReport 读取周报文件并写入周报索引，返回变化类型。
func (ix *Index) upsertReport(ctx context.Context, tx *sql.Tx, path string, force bool) (Change, error) {
	path = filepath.Clean(path)

	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		id, removed, err := deleteReport(ctx, tx, path)
		if err != nil {
			return Change{}, err
		}
		if removed {
			return Change{ID: id, Kind: ChangeRemoved}, nil
		}
		return Change{Kind: ChangeUnchanged}, nil
	}
	if err != nil {
		return Change{}, fmt.Errorf("读取周报文件信息失败: %w", err)
	}
	if info.IsDir() {
		return Change{Kind: ChangeUnchanged}, nil
	}

	report, err := store.ReadReportFile(path)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			id, removed, delErr := deleteReport(ctx, tx, path)
			if delErr != nil {
				return Change{}, delErr
			}
			if removed {
				return Change{ID: id, Kind: ChangeRemoved}, nil
			}
			return Change{Kind: ChangeUnchanged}, nil
		}
		return Change{}, err
	}
	rid := reportRID(report)

	var (
		oldMtime int64
		oldSize  int64
		exists   bool
	)
	err = tx.QueryRowContext(ctx,
		"SELECT mtime, size FROM reports WHERE path = ?", path).Scan(&oldMtime, &oldSize)
	switch {
	case err == nil:
		exists = true
	case errors.Is(err, sql.ErrNoRows):
		err = tx.QueryRowContext(ctx,
			"SELECT mtime, size FROM reports WHERE id = ?", rid).Scan(&oldMtime, &oldSize)
		switch {
		case err == nil:
			exists = true
		case errors.Is(err, sql.ErrNoRows):
			// 新周报。
		default:
			return Change{}, fmt.Errorf("查询周报索引项失败: %w", err)
		}
	default:
		return Change{}, fmt.Errorf("查询周报索引项失败: %w", err)
	}

	if exists && !force && oldMtime == info.ModTime().UnixNano() && oldSize == info.Size() {
		return Change{ID: rid, Kind: ChangeUnchanged}, nil
	}

	for _, stmt := range []string{
		"DELETE FROM reports_fts WHERE id = ?",
		"DELETE FROM reports WHERE path = ? OR id = ?",
	} {
		args := []any{rid}
		if strings.HasPrefix(stmt, "DELETE FROM reports WHERE") {
			args = []any{path, rid}
		}
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return Change{}, fmt.Errorf("清理旧周报索引项失败: %w", err)
		}
	}

	products, err := json.Marshal(stringsOrEmpty(report.ProductLines))
	if err != nil {
		products = []byte("[]")
	}
	extra := []byte("{}")
	if len(report.Extra) > 0 {
		if data, err := json.Marshal(report.Extra); err == nil {
			extra = data
		}
	}
	submittedAt := ""
	if !report.SubmittedAt.IsZero() {
		submittedAt = report.SubmittedAt.Format(time.RFC3339)
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO reports
		(id, person, name, week, level, grp, product_lines, source,
		 submitted_at, header_date, path, mtime, size, extra)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rid, report.Person, report.Name, report.Week, report.Level, report.Group,
		string(products), report.Source, submittedAt, report.HeaderDate,
		path, info.ModTime().UnixNano(), info.Size(), string(extra))
	if err != nil {
		return Change{}, fmt.Errorf("写入周报索引项失败: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO reports_fts (id, name, body) VALUES (?, ?, ?)",
		rid, report.Name, report.Body); err != nil {
		return Change{}, fmt.Errorf("写入周报全文索引失败: %w", err)
	}

	if exists {
		return Change{ID: rid, Kind: ChangeUpdated}, nil
	}
	return Change{ID: rid, Kind: ChangeAdded}, nil
}

// deletePerson 按路径删除组员索引项，返回被删除的组员 ID 与是否真的删除。
func deletePerson(ctx context.Context, tx *sql.Tx, path string) (string, bool, error) {
	path = filepath.Clean(path)
	var id string
	err := tx.QueryRowContext(ctx, "SELECT id FROM people WHERE path = ?", path).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("查询待删除组员索引项失败: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM people_fts WHERE id = ?", id); err != nil {
		return "", false, fmt.Errorf("删除组员全文索引失败: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM people WHERE id = ?", id); err != nil {
		return "", false, fmt.Errorf("删除组员索引项失败: %w", err)
	}
	return id, true, nil
}

// deleteReport 按路径删除周报索引项，返回被删除的周报 ID 与是否真的删除。
func deleteReport(ctx context.Context, tx *sql.Tx, path string) (string, bool, error) {
	path = filepath.Clean(path)
	var id string
	err := tx.QueryRowContext(ctx, "SELECT id FROM reports WHERE path = ?", path).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("查询待删除周报索引项失败: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM reports_fts WHERE id = ?", id); err != nil {
		return "", false, fmt.Errorf("删除周报全文索引失败: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM reports WHERE id = ?", id); err != nil {
		return "", false, fmt.Errorf("删除周报索引项失败: %w", err)
	}
	return id, true, nil
}

// stringsOrEmpty 返回非 nil 字符串切片，保证 JSON 序列化为 [] 而不是 null。
func stringsOrEmpty(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

// peopleMetaColumns 是 people 表中查询 PeopleMeta 所需的列。
const peopleMetaColumns = `id, name, aliases, grp, product_lines, role, active, group_history, created, updated, path, extra`

// PeopleMeta 是从索引读出的组员元数据，不含正文。
type PeopleMeta struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Aliases      []string            `json:"aliases"`
	Group        string              `json:"group"`
	ProductLines []string            `json:"productLines"`
	Role         string              `json:"role"`
	Active       bool                `json:"active"`
	GroupHistory []store.GroupRecord `json:"groupHistory"`
	Created      time.Time           `json:"created"`
	Updated      time.Time           `json:"updated"`
	Path         string              `json:"path"`
	Extra        map[string]any      `json:"extra,omitempty"`
}

// ListPeople 返回索引中的全部组员元数据，按 ID 排序。
func (ix *Index) ListPeople() ([]PeopleMeta, error) {
	rows, err := ix.db.Query("SELECT " + peopleMetaColumns + " FROM people ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("查询组员列表失败: %w", err)
	}
	defer rows.Close()

	var metas []PeopleMeta
	for rows.Next() {
		meta, err := scanPeopleMeta(rows)
		if err != nil {
			return nil, err
		}
		metas = append(metas, meta)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("查询组员列表失败: %w", err)
	}
	return metas, nil
}

// scanPeopleMeta 从一行查询结果读取组员元数据。
func scanPeopleMeta(s scanner) (PeopleMeta, error) {
	var (
		meta                              PeopleMeta
		aliases, products, history, extra string
		created, updated                  string
		active                            int
	)
	if err := s.Scan(&meta.ID, &meta.Name, &aliases, &meta.Group, &products,
		&meta.Role, &active, &history, &created, &updated, &meta.Path, &extra); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PeopleMeta{}, err
		}
		return PeopleMeta{}, fmt.Errorf("读取组员索引项失败: %w", err)
	}
	meta.Active = active != 0
	meta.Created = parseTime(created)
	meta.Updated = parseTime(updated)
	if aliases != "" {
		_ = json.Unmarshal([]byte(aliases), &meta.Aliases)
	}
	if meta.Aliases == nil {
		meta.Aliases = []string{}
	}
	if products != "" {
		_ = json.Unmarshal([]byte(products), &meta.ProductLines)
	}
	if meta.ProductLines == nil {
		meta.ProductLines = []string{}
	}
	if history != "" {
		_ = json.Unmarshal([]byte(history), &meta.GroupHistory)
	}
	if meta.GroupHistory == nil {
		meta.GroupHistory = []store.GroupRecord{}
	}
	if extra != "" && extra != "{}" {
		_ = json.Unmarshal([]byte(extra), &meta.Extra)
	}
	return meta, nil
}
