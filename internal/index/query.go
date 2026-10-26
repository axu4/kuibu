package index

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Meta 是从索引读出的笔记元数据，不含正文。
//
// 字段与 store.Note 对应，供 App 快速渲染笔记树，无需读取 Markdown 文件。
type Meta struct {
	// ID 是笔记标识。
	ID string `json:"id"`
	// Title 是笔记标题。
	Title string `json:"title"`
	// Type 是笔记类型。
	Type string `json:"type"`
	// Parent 是父笔记 ID，空字符串表示根节点。
	Parent string `json:"parent"`
	// Order 是同级排序序号。
	Order int `json:"order"`
	// Tags 是标签列表。
	Tags []string `json:"tags"`
	// Created 是创建时间，零值表示 front matter 未提供。
	Created time.Time `json:"created"`
	// Updated 是最后更新时间，零值表示 front matter 未提供。
	Updated time.Time `json:"updated"`
	// Path 是笔记文件的绝对路径。
	Path string `json:"path"`
	// MTime 是文件修改时间。
	MTime time.Time `json:"mtime"`
	// Size 是文件大小（字节）。
	Size int64 `json:"size"`
	// Extra 是 front matter 中未识别的字段。
	Extra map[string]any `json:"extra,omitempty"`
}

// Hit 是一条搜索命中。
type Hit struct {
	// ID 是命中的笔记 ID。
	ID string `json:"id"`
	// Title 是命中的笔记标题。
	Title string `json:"title"`
	// Snippet 是带少量上下文的命中片段，命中词用 [] 包裹。
	Snippet string `json:"snippet"`
}

// List 返回索引中的全部笔记元数据。
//
// 排序与 store.Tree 的遍历顺序一致：先按 parent，再按 order，最后按 id，
// 因此同一父节点下的笔记在结果中连续且有序。
func (ix *Index) List() ([]Meta, error) {
	rows, err := ix.db.Query(`SELECT id, title, type, parent, ord, tags, created, updated,
		path, mtime, size, extra
		FROM notes ORDER BY parent, ord, id`)
	if err != nil {
		return nil, fmt.Errorf("查询笔记列表失败: %w", err)
	}
	defer rows.Close()

	var metas []Meta
	for rows.Next() {
		meta, err := scanMeta(rows)
		if err != nil {
			return nil, err
		}
		metas = append(metas, meta)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("查询笔记列表失败: %w", err)
	}
	return metas, nil
}

// Children 返回 parent 下的直接子笔记元数据，按 order 再按 id 排序。
//
// parent 为空字符串表示根节点。
func (ix *Index) Children(parent string) ([]Meta, error) {
	rows, err := ix.db.Query(`SELECT id, title, type, parent, ord, tags, created, updated,
		path, mtime, size, extra
		FROM notes WHERE parent = ? ORDER BY ord, id`, parent)
	if err != nil {
		return nil, fmt.Errorf("查询子笔记失败: %w", err)
	}
	defer rows.Close()

	var metas []Meta
	for rows.Next() {
		meta, err := scanMeta(rows)
		if err != nil {
			return nil, err
		}
		metas = append(metas, meta)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("查询子笔记失败: %w", err)
	}
	return metas, nil
}

// Get 按 ID 返回索引中的笔记元数据。
func (ix *Index) Get(id string) (Meta, error) {
	row := ix.db.QueryRow(`SELECT id, title, type, parent, ord, tags, created, updated,
		path, mtime, size, extra
		FROM notes WHERE id = ?`, id)
	meta, err := scanMeta(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Meta{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err != nil {
		return Meta{}, err
	}
	return meta, nil
}

// Count 返回索引中的笔记总数。
func (ix *Index) Count() (int, error) {
	var total int
	if err := ix.db.QueryRow("SELECT COUNT(*) FROM notes").Scan(&total); err != nil {
		return 0, fmt.Errorf("统计索引笔记数失败: %w", err)
	}
	return total, nil
}

// scanner 是 *sql.Row 与 *sql.Rows 的公共读取接口。
type scanner interface {
	Scan(dest ...any) error
}

// scanMeta 从一行查询结果读取元数据。
func scanMeta(s scanner) (Meta, error) {
	var (
		meta       Meta
		tags       string
		extra      string
		created    string
		updated    string
		mtimeNanos int64
	)
	if err := s.Scan(&meta.ID, &meta.Title, &meta.Type, &meta.Parent, &meta.Order, &tags,
		&created, &updated, &meta.Path, &mtimeNanos, &meta.Size, &extra); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Meta{}, err
		}
		return Meta{}, fmt.Errorf("读取索引项失败: %w", err)
	}

	meta.MTime = time.Unix(0, mtimeNanos)
	meta.Created = parseTime(created)
	meta.Updated = parseTime(updated)
	if tags != "" {
		_ = json.Unmarshal([]byte(tags), &meta.Tags)
	}
	if meta.Tags == nil {
		meta.Tags = []string{}
	}
	if extra != "" && extra != "{}" {
		_ = json.Unmarshal([]byte(extra), &meta.Extra)
	}
	return meta, nil
}

// parseTime 解析索引中保存的 RFC3339 时间，失败时返回零值。
func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

// Search 在标题与正文中检索，返回最多 limit 条命中。
//
// 查询长度为 3 个及以上字符时使用 FTS5 trigram 索引（支持中文子串），
// 少于 3 个字符时 trigram 无法匹配，回退为对 title/body 的 LIKE 检索。
// limit <= 0 时使用默认值 20。结果按相关度、更新时间倒序排列。
func (ix *Index) Search(query string, limit int) ([]Hit, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return []Hit{}, nil
	}
	if limit <= 0 {
		limit = 20
	}

	if utf8.RuneCountInString(q) >= 3 {
		hits, err := ix.searchFTS(q, limit)
		if err == nil {
			return hits, nil
		}
		// trigram 对部分含特殊字符的查询会解析失败，退回 LIKE 更稳妥。
	}
	return ix.searchLike(q, limit)
}

// searchFTS 使用 FTS5 trigram 索引检索。
func (ix *Index) searchFTS(q string, limit int) ([]Hit, error) {
	// 把查询当作短语，避免 - : * 等被当作 FTS5 查询语法。
	phrase := `"` + strings.ReplaceAll(q, `"`, `""`) + `"`

	rows, err := ix.db.Query(`SELECT n.id, n.title,
			snippet(notes_fts, 1, '[', ']', '…', 12) AS title_snippet,
			snippet(notes_fts, 2, '[', ']', '…', 16) AS body_snippet
		FROM notes_fts
		JOIN notes n ON n.id = notes_fts.id
		WHERE notes_fts MATCH ?
		ORDER BY bm25(notes_fts), n.updated DESC, n.id
		LIMIT ?`, phrase, limit)
	if err != nil {
		return nil, fmt.Errorf("全文检索失败: %w", err)
	}
	defer rows.Close()

	hits := []Hit{}
	for rows.Next() {
		var id, title, titleSnippet, bodySnippet string
		if err := rows.Scan(&id, &title, &titleSnippet, &bodySnippet); err != nil {
			return nil, fmt.Errorf("读取检索结果失败: %w", err)
		}
		hits = append(hits, Hit{
			ID:      id,
			Title:   title,
			Snippet: pickSnippet(titleSnippet, bodySnippet, q),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("全文检索失败: %w", err)
	}
	return hits, nil
}

// searchLike 对短查询使用 LIKE 回退检索。
func (ix *Index) searchLike(q string, limit int) ([]Hit, error) {
	pattern := "%" + escapeLike(q) + "%"
	rows, err := ix.db.Query(`SELECT n.id, n.title, f.body
		FROM notes n
		LEFT JOIN notes_fts f ON f.id = n.id
		WHERE n.title LIKE ? ESCAPE '\' OR f.body LIKE ? ESCAPE '\'
		ORDER BY n.updated DESC, n.id
		LIMIT ?`, pattern, pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("短词检索失败: %w", err)
	}
	defer rows.Close()

	hits := []Hit{}
	for rows.Next() {
		var id, title string
		var body sql.NullString
		if err := rows.Scan(&id, &title, &body); err != nil {
			return nil, fmt.Errorf("读取检索结果失败: %w", err)
		}
		hits = append(hits, Hit{
			ID:      id,
			Title:   title,
			Snippet: likeSnippet(body.String, q),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("短词检索失败: %w", err)
	}
	return hits, nil
}

// escapeLike 转义 LIKE 模式中的通配符。
func escapeLike(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(s)
}

// pickSnippet 选择正文片段，正文没有命中时退回标题片段。
func pickSnippet(titleSnippet, bodySnippet, query string) string {
	if bodySnippet != "" {
		return bodySnippet
	}
	if titleSnippet != "" {
		return titleSnippet
	}
	return query
}

// likeSnippet 在正文中定位查询词，截取前后少量上下文。
func likeSnippet(body, query string) string {
	if body == "" {
		return ""
	}
	lowerBody := strings.ToLower(body)
	idx := strings.Index(lowerBody, strings.ToLower(query))
	if idx < 0 {
		return head(body, 40)
	}
	start := idx - 12
	if start < 0 {
		start = 0
	}
	end := idx + len(query) + 12
	if end > len(body) {
		end = len(body)
	}
	// 避免把多字节字符截断。
	for start > 0 && !utf8.RuneStart(body[start]) {
		start--
	}
	for end < len(body) && !utf8.RuneStart(body[end]) {
		end++
	}
	snippet := body[start:idx] + "[" + body[idx:idx+len(query)] + "]" + body[end:]
	if start > 0 {
		snippet = "…" + snippet
	}
	if end < len(body) {
		snippet += "…"
	}
	return snippet
}

// head 返回正文开头最多 n 个字符。
func head(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

// ErrNotFound 表示索引中没有对应的笔记。
//
// 索引只是缓存，因此该错误不代表 Markdown 文件一定不存在。
var ErrNotFound = errors.New("索引项不存在")
