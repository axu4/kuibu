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

// todoMetaColumns 是 todos 表中查询 TodoMeta 所需的列。
const todoMetaColumns = `id, title, status, due_date, due_time, due_at, done_at, parent, ord,
	tags, links, attachments, source, created, updated, path, extra`

// todoDefaultOrder 是 ListTodos 的默认排序：
//
//  1. 未完成（todo/doing）在前，已完成/已取消（done/canceled）在最后；
//  2. 未完成且带截止日期的按 due_at 升序，无截止日期的排后；
//  3. 未完成组内再按 parent、ord、id 升序；
//  4. 已完成/已取消按 done_at 倒序、updated 倒序。
const todoDefaultOrder = `
	CASE WHEN status IN ('todo','doing') THEN 0 ELSE 2 END ASC,
	CASE WHEN status IN ('todo','doing') AND due_at = '' THEN 1 ELSE 0 END ASC,
	CASE WHEN status IN ('todo','doing') THEN due_at ELSE '' END ASC,
	CASE WHEN status IN ('todo','doing') THEN parent ELSE '' END ASC,
	CASE WHEN status IN ('todo','doing') THEN ord ELSE 0 END ASC,
	CASE WHEN status IN ('todo','doing') THEN id ELSE '' END ASC,
	done_at DESC,
	updated DESC`

// TodoMeta 是从索引读出的待办元数据，不含正文。
type TodoMeta struct {
	// ID 是待办标识。
	ID string `json:"id"`
	// Title 是待办标题。
	Title string `json:"title"`
	// Status 是待办状态。
	Status string `json:"status"`
	// Due 是截止日期字符串，写法与 front matter 一致（可空）。
	Due string `json:"due"`
	// DueAt 是截止时刻，无截止日期时为 null。
	DueAt *time.Time `json:"dueAt"`
	// DoneAt 是完成时间，未完成时为 null。
	DoneAt *time.Time `json:"doneAt"`
	// Parent 是父待办 ID，空字符串表示顶层。
	Parent string `json:"parent"`
	// Order 是同级排序序号。
	Order int `json:"order"`
	// Tags 是标签列表。
	Tags []string `json:"tags"`
	// Links 是链接列表。
	Links []store.TodoLink `json:"links"`
	// Attachments 是附件相对路径列表。
	Attachments []string `json:"attachments"`
	// Source 是来源笔记 ID，可空。
	Source string `json:"source"`
	// Created 是创建时间。
	Created time.Time `json:"created"`
	// Updated 是最后更新时间。
	Updated time.Time `json:"updated"`
	// Path 是待办文件的绝对路径。
	Path string `json:"path"`
	// Extra 是 front matter 中未识别的字段。
	Extra map[string]any `json:"extra,omitempty"`
}

// TodoFilter 是 ListTodos 的过滤条件，零值不过滤。
type TodoFilter struct {
	// Statuses 是允许的状态列表，空表示不过滤。
	Statuses []string
	// Tags 是必须全部包含的标签，空表示不过滤。
	Tags []string
	// Parent 过滤父待办：nil 不过滤；指向 "" 表示只要顶层。
	Parent *string
	// DueFrom 与 DueTo 是截止日期闭区间（YYYY-MM-DD），空不限；设置任一时排除无截止日期。
	DueFrom string
	DueTo   string
	// OverdueAt 非零时，只要状态为 todo/doing 且截止时刻早于该时刻的待办。
	OverdueAt time.Time
	// Query 是全文检索，规则同笔记 Search：>=3 字符走 FTS trigram，否则 LIKE。
	Query string
	// Limit 限制返回条数，<=0 表示不限制。
	Limit int
}

// ListTodos 按过滤条件返回待办元数据，排序规则见 todoDefaultOrder。
func (ix *Index) ListTodos(f TodoFilter) ([]TodoMeta, error) {
	query := strings.TrimSpace(f.Query)

	var conds []string
	var args []any

	if len(f.Statuses) > 0 {
		conds = append(conds, "status IN ("+strings.TrimSuffix(strings.Repeat("?,", len(f.Statuses)), ",")+")")
		for _, s := range f.Statuses {
			args = append(args, s)
		}
	}
	if f.Parent != nil {
		conds = append(conds, "parent = ?")
		args = append(args, *f.Parent)
	}
	if f.DueFrom != "" || f.DueTo != "" {
		// 设置任一时排除无截止日期的待办。
		conds = append(conds, "due_date != ''")
	}
	if f.DueFrom != "" {
		conds = append(conds, "due_date >= ?")
		args = append(args, f.DueFrom)
	}
	if f.DueTo != "" {
		conds = append(conds, "due_date <= ?")
		args = append(args, f.DueTo)
	}
	if !f.OverdueAt.IsZero() {
		conds = append(conds, "status IN ('todo','doing') AND due_at != '' AND due_at < ?")
		args = append(args, f.OverdueAt.In(store.TodoLocation()).Format(time.RFC3339))
	}
	if query != "" {
		if utf8.RuneCountInString(query) >= 3 {
			phrase := `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
			conds = append(conds, "EXISTS (SELECT 1 FROM todos_fts WHERE todos_fts.id = todos.id AND todos_fts MATCH ?)")
			args = append(args, phrase)
		} else {
			pattern := "%" + escapeLike(query) + "%"
			conds = append(conds, "(title LIKE ? ESCAPE '\\' OR EXISTS (SELECT 1 FROM todos_fts WHERE todos_fts.id = todos.id AND todos_fts.body LIKE ? ESCAPE '\\'))")
			args = append(args, pattern, pattern)
		}
	}

	sqlText := "SELECT " + todoMetaColumns + " FROM todos"
	if len(conds) > 0 {
		sqlText += " WHERE " + strings.Join(conds, " AND ")
	}
	sqlText += " ORDER BY " + todoDefaultOrder

	rows, err := ix.db.Query(sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("查询待办列表失败: %w", err)
	}
	defer rows.Close()

	var metas []TodoMeta
	for rows.Next() {
		meta, err := scanTodoMeta(rows)
		if err != nil {
			return nil, err
		}
		metas = append(metas, meta)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("查询待办列表失败: %w", err)
	}

	// 标签为「全部包含」语义，逐个在结果中过滤。
	if len(f.Tags) > 0 {
		filtered := metas[:0]
		for _, m := range metas {
			if containsAllTags(m.Tags, f.Tags) {
				filtered = append(filtered, m)
			}
		}
		metas = filtered
	}
	if f.Limit > 0 && len(metas) > f.Limit {
		metas = metas[:f.Limit]
	}
	return metas, nil
}

// GetTodo 按 ID 返回索引中的待办元数据。
func (ix *Index) GetTodo(id string) (TodoMeta, error) {
	row := ix.db.QueryRow("SELECT "+todoMetaColumns+" FROM todos WHERE id = ?", id)
	meta, err := scanTodoMeta(row)
	if errors.Is(err, sql.ErrNoRows) {
		return TodoMeta{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err != nil {
		return TodoMeta{}, err
	}
	return meta, nil
}

// TodoChildren 返回 parent 下的直接子待办元数据，按 ord 再按 id 排序。
//
// parent 为空字符串表示顶层。
func (ix *Index) TodoChildren(parent string) ([]TodoMeta, error) {
	rows, err := ix.db.Query("SELECT "+todoMetaColumns+" FROM todos WHERE parent = ? ORDER BY ord, id", parent)
	if err != nil {
		return nil, fmt.Errorf("查询子待办失败: %w", err)
	}
	defer rows.Close()

	var metas []TodoMeta
	for rows.Next() {
		meta, err := scanTodoMeta(rows)
		if err != nil {
			return nil, err
		}
		metas = append(metas, meta)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("查询子待办失败: %w", err)
	}
	return metas, nil
}

// SearchTodos 在待办标题与正文中检索，返回最多 limit 条命中。
//
// 查询长度为 3 个及以上字符时使用 FTS5 trigram 索引，少于 3 个字符时回退
// 为 title/body 的 LIKE 检索。limit <= 0 时使用默认值 20。
func (ix *Index) SearchTodos(query string, limit int) ([]Hit, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return []Hit{}, nil
	}
	if limit <= 0 {
		limit = 20
	}

	if utf8.RuneCountInString(q) >= 3 {
		hits, err := ix.searchTodosFTS(q, limit)
		if err == nil {
			return hits, nil
		}
		// trigram 对部分含特殊字符的查询会解析失败，退回 LIKE 更稳妥。
	}
	return ix.searchTodosLike(q, limit)
}

// searchTodosFTS 使用 FTS5 trigram 索引检索待办。
func (ix *Index) searchTodosFTS(q string, limit int) ([]Hit, error) {
	phrase := `"` + strings.ReplaceAll(q, `"`, `""`) + `"`

	rows, err := ix.db.Query(`SELECT t.id, t.title,
			snippet(todos_fts, 1, '[', ']', '…', 12) AS title_snippet,
			snippet(todos_fts, 2, '[', ']', '…', 16) AS body_snippet
		FROM todos_fts
		JOIN todos t ON t.id = todos_fts.id
		WHERE todos_fts MATCH ?
		ORDER BY bm25(todos_fts), t.updated DESC, t.id
		LIMIT ?`, phrase, limit)
	if err != nil {
		return nil, fmt.Errorf("待办全文检索失败: %w", err)
	}
	defer rows.Close()

	hits := []Hit{}
	for rows.Next() {
		var id, title, titleSnippet, bodySnippet string
		if err := rows.Scan(&id, &title, &titleSnippet, &bodySnippet); err != nil {
			return nil, fmt.Errorf("读取待办检索结果失败: %w", err)
		}
		hits = append(hits, Hit{
			ID:      id,
			Title:   title,
			Snippet: pickSnippet(titleSnippet, bodySnippet, q),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("待办全文检索失败: %w", err)
	}
	return hits, nil
}

// searchTodosLike 对短查询使用 LIKE 回退检索待办。
func (ix *Index) searchTodosLike(q string, limit int) ([]Hit, error) {
	pattern := "%" + escapeLike(q) + "%"
	rows, err := ix.db.Query(`SELECT t.id, t.title, f.body
		FROM todos t
		LEFT JOIN todos_fts f ON f.id = t.id
		WHERE t.title LIKE ? ESCAPE '\' OR f.body LIKE ? ESCAPE '\'
		ORDER BY t.updated DESC, t.id
		LIMIT ?`, pattern, pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("待办短词检索失败: %w", err)
	}
	defer rows.Close()

	hits := []Hit{}
	for rows.Next() {
		var id, title string
		var body sql.NullString
		if err := rows.Scan(&id, &title, &body); err != nil {
			return nil, fmt.Errorf("读取待办检索结果失败: %w", err)
		}
		hits = append(hits, Hit{
			ID:      id,
			Title:   title,
			Snippet: likeSnippet(body.String, q),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("待办短词检索失败: %w", err)
	}
	return hits, nil
}

// scanTodoMeta 从一行查询结果读取待办元数据。
func scanTodoMeta(s scanner) (TodoMeta, error) {
	var (
		meta                            TodoMeta
		tags, links, attachments, extra string
		dueDate, dueTime, dueAt, doneAt string
		created, updated                string
	)
	if err := s.Scan(&meta.ID, &meta.Title, &meta.Status, &dueDate, &dueTime, &dueAt, &doneAt,
		&meta.Parent, &meta.Order, &tags, &links, &attachments, &meta.Source,
		&created, &updated, &meta.Path, &extra); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TodoMeta{}, err
		}
		return TodoMeta{}, fmt.Errorf("读取待办索引项失败: %w", err)
	}

	meta.Created = parseTime(created)
	meta.Updated = parseTime(updated)
	meta.Due = dueDate
	if dueTime != "" {
		meta.Due += " " + dueTime
	}
	if dueAt != "" {
		if t := parseTime(dueAt); !t.IsZero() {
			meta.DueAt = &t
		}
	}
	if doneAt != "" {
		if t := parseTime(doneAt); !t.IsZero() {
			meta.DoneAt = &t
		}
	}

	if tags != "" {
		_ = json.Unmarshal([]byte(tags), &meta.Tags)
	}
	if meta.Tags == nil {
		meta.Tags = []string{}
	}
	if links != "" {
		_ = json.Unmarshal([]byte(links), &meta.Links)
	}
	if meta.Links == nil {
		meta.Links = []store.TodoLink{}
	}
	if attachments != "" {
		_ = json.Unmarshal([]byte(attachments), &meta.Attachments)
	}
	if meta.Attachments == nil {
		meta.Attachments = []string{}
	}
	if extra != "" && extra != "{}" {
		_ = json.Unmarshal([]byte(extra), &meta.Extra)
	}
	return meta, nil
}

// containsAllTags 判断标签列表是否包含全部目标标签。
func containsAllTags(tags, want []string) bool {
	for _, w := range want {
		found := false
		for _, t := range tags {
			if t == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
