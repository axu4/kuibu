package index

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/axu4/kuibu/internal/store"
)

// reportMetaColumns 是 reports 表中查询 ReportMeta 所需的列。
const reportMetaColumns = `id, person, name, week, level, grp, product_lines, source, submitted_at, header_date, path, extra`

// reportDefaultOrder 是 ListReports 的默认排序：先按周倒序，再按层级与人员。
const reportDefaultOrder = `week DESC, level ASC, person ASC`

// ReportMeta 是从索引读出的周报元数据，不含正文。
type ReportMeta struct {
	// ID 是索引里的唯一 ID（week/level/person）。
	ID string `json:"id"`
	// Person 是组员 ID。
	Person string `json:"person"`
	// Name 是提交时姓名快照。
	Name string `json:"name"`
	// Week 是 ISO 周编号。
	Week string `json:"week"`
	// Level 是层级 member / lead。
	Level string `json:"level"`
	// Group 是提交时所属组快照。
	Group string `json:"group"`
	// ProductLines 是提交时产品线快照。
	ProductLines []string `json:"productLines"`
	// Source 是来源 paste / cli。
	Source string `json:"source"`
	// SubmittedAt 是提交时间。
	SubmittedAt time.Time `json:"submittedAt"`
	// HeaderDate 是原文头里的日期，可为空。
	HeaderDate string `json:"headerDate"`
	// Path 是周报文件绝对路径。
	Path string `json:"path"`
	// Extra 是 front matter 中未识别的字段。
	Extra map[string]any `json:"extra,omitempty"`
}

// ReportFilter 是 ListReports 的过滤条件，零值不过滤。
type ReportFilter struct {
	// Week 是 ISO 周编号。
	Week string
	// Level 是层级 member / lead。
	Level string
	// Group 是组名。
	Group string
	// Query 是姓名与正文检索：>=3 字符走 FTS trigram，否则 LIKE。
	Query string
	// Limit 限制返回条数，<=0 表示不限制。
	Limit int
}

// ListReports 按过滤条件返回周报元数据，排序见 reportDefaultOrder。
func (ix *Index) ListReports(f ReportFilter) ([]ReportMeta, error) {
	query := strings.TrimSpace(f.Query)

	var conds []string
	var args []any
	if f.Week != "" {
		conds = append(conds, "week = ?")
		args = append(args, f.Week)
	}
	if f.Level != "" {
		conds = append(conds, "level = ?")
		args = append(args, f.Level)
	} else {
		// 不指定层级时只列原文，归一稿 / 组报要显式按 level 查。
		conds = append(conds, "level IN (?, ?)")
		args = append(args, store.ReportLevelMember, store.ReportLevelLead)
	}
	if f.Group != "" {
		conds = append(conds, "grp = ?")
		args = append(args, f.Group)
	}
	if query != "" {
		if utf8.RuneCountInString(query) >= 3 {
			phrase := `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
			conds = append(conds, "EXISTS (SELECT 1 FROM reports_fts WHERE reports_fts.id = reports.id AND reports_fts MATCH ?)")
			args = append(args, phrase)
		} else {
			pattern := "%" + escapeLike(query) + "%"
			conds = append(conds, "(name LIKE ? ESCAPE '\\' OR EXISTS (SELECT 1 FROM reports_fts WHERE reports_fts.id = reports.id AND reports_fts.body LIKE ? ESCAPE '\\'))")
			args = append(args, pattern, pattern)
		}
	}

	sqlText := "SELECT " + reportMetaColumns + " FROM reports"
	if len(conds) > 0 {
		sqlText += " WHERE " + strings.Join(conds, " AND ")
	}
	sqlText += " ORDER BY " + reportDefaultOrder

	rows, err := ix.db.Query(sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("查询周报列表失败: %w", err)
	}
	defer rows.Close()

	var metas []ReportMeta
	for rows.Next() {
		meta, err := scanReportMeta(rows)
		if err != nil {
			return nil, err
		}
		metas = append(metas, meta)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("查询周报列表失败: %w", err)
	}
	if f.Limit > 0 && len(metas) > f.Limit {
		metas = metas[:f.Limit]
	}
	return metas, nil
}

// SearchReports 在周报姓名与正文中检索，返回最多 limit 条命中。
//
// 查询长度为 3 个及以上字符时使用 FTS5 trigram 索引，少于 3 个字符时回退
// 为 name/body 的 LIKE 检索。limit <= 0 时使用默认值 20。
func (ix *Index) SearchReports(query string, limit int) ([]Hit, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return []Hit{}, nil
	}
	if limit <= 0 {
		limit = 20
	}

	if utf8.RuneCountInString(q) >= 3 {
		hits, err := ix.searchReportsFTS(q, limit)
		if err == nil {
			return hits, nil
		}
	}
	return ix.searchReportsLike(q, limit)
}

// searchReportsFTS 使用 FTS5 trigram 索引检索周报。
func (ix *Index) searchReportsFTS(q string, limit int) ([]Hit, error) {
	phrase := `"` + strings.ReplaceAll(q, `"`, `""`) + `"`
	rows, err := ix.db.Query(`SELECT r.id, r.name,
			snippet(reports_fts, 1, '[', ']', '…', 12) AS name_snippet,
			snippet(reports_fts, 2, '[', ']', '…', 16) AS body_snippet
		FROM reports_fts
		JOIN reports r ON r.id = reports_fts.id
		WHERE reports_fts MATCH ?
		ORDER BY bm25(reports_fts), r.submitted_at DESC, r.id
		LIMIT ?`, phrase, limit)
	if err != nil {
		return nil, fmt.Errorf("周报全文检索失败: %w", err)
	}
	defer rows.Close()

	hits := []Hit{}
	for rows.Next() {
		var id, name, nameSnippet, bodySnippet string
		if err := rows.Scan(&id, &name, &nameSnippet, &bodySnippet); err != nil {
			return nil, fmt.Errorf("读取周报检索结果失败: %w", err)
		}
		hits = append(hits, Hit{
			ID:      id,
			Title:   name,
			Type:    "report",
			Snippet: pickSnippet(nameSnippet, bodySnippet, q),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("周报全文检索失败: %w", err)
	}
	return hits, nil
}

// searchReportsLike 对短查询使用 LIKE 回退检索周报。
func (ix *Index) searchReportsLike(q string, limit int) ([]Hit, error) {
	pattern := "%" + escapeLike(q) + "%"
	rows, err := ix.db.Query(`SELECT r.id, r.name, f.body
		FROM reports r
		LEFT JOIN reports_fts f ON f.id = r.id
		WHERE r.name LIKE ? ESCAPE '\' OR f.body LIKE ? ESCAPE '\'
		ORDER BY r.submitted_at DESC, r.id
		LIMIT ?`, pattern, pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("周报短词检索失败: %w", err)
	}
	defer rows.Close()

	hits := []Hit{}
	for rows.Next() {
		var id, name string
		var body sql.NullString
		if err := rows.Scan(&id, &name, &body); err != nil {
			return nil, fmt.Errorf("读取周报检索结果失败: %w", err)
		}
		hits = append(hits, Hit{
			ID:      id,
			Title:   name,
			Type:    "report",
			Snippet: likeSnippet(body.String, q),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("周报短词检索失败: %w", err)
	}
	return hits, nil
}

// scanReportMeta 从一行查询结果读取周报元数据。
func scanReportMeta(s scanner) (ReportMeta, error) {
	var (
		meta            ReportMeta
		products, extra string
		submittedAt     string
	)
	if err := s.Scan(&meta.ID, &meta.Person, &meta.Name, &meta.Week, &meta.Level,
		&meta.Group, &products, &meta.Source, &submittedAt, &meta.HeaderDate,
		&meta.Path, &extra); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ReportMeta{}, err
		}
		return ReportMeta{}, fmt.Errorf("读取周报索引项失败: %w", err)
	}

	meta.SubmittedAt = parseTime(submittedAt)
	if products != "" {
		_ = json.Unmarshal([]byte(products), &meta.ProductLines)
	}
	if meta.ProductLines == nil {
		meta.ProductLines = []string{}
	}
	if extra != "" && extra != "{}" {
		_ = json.Unmarshal([]byte(extra), &meta.Extra)
	}
	return meta, nil
}
