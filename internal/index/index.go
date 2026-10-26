// Package index 负责 SQLite 索引，可随时删除重建；外部改文件时经 fsnotify 检测并增量重建。
package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/axu4/kuibu/internal/store"

	_ "modernc.org/sqlite"
)

// schemaVersion 是索引数据库结构版本，记录在 PRAGMA user_version 中。
// 版本不匹配时 Open 会删掉旧表并重建。
const schemaVersion = 2

// indexFileName 是索引文件名，位于工作台的 .kuibu/ 目录下。
const indexFileName = "index.db"

// ChangeKind 表示一次单文件同步带来的变化类型。
type ChangeKind string

const (
	// ChangeAdded 表示笔记是新增的。
	ChangeAdded ChangeKind = "added"
	// ChangeUpdated 表示笔记已存在且内容被更新。
	ChangeUpdated ChangeKind = "updated"
	// ChangeRemoved 表示笔记已从索引中删除。
	ChangeRemoved ChangeKind = "removed"
	// ChangeUnchanged 表示文件与索引一致，没有发生变化。
	ChangeUnchanged ChangeKind = "unchanged"
)

// Stats 是一次重建或增量同步的统计结果。
//
// Added/Updated/Removed/Total 只统计笔记；Todo* 字段统计待办，两者互不混入。
type Stats struct {
	// Added 是新增的笔记数量。
	Added int `json:"added"`
	// Updated 是被修改并重新读取的笔记数量。
	Updated int `json:"updated"`
	// Removed 是从索引中删除的笔记数量。
	Removed int `json:"removed"`
	// Total 是同步完成后索引中的笔记总数。
	Total int `json:"total"`

	// TodoAdded 是新增的待办数量。
	TodoAdded int `json:"todoAdded"`
	// TodoUpdated 是被修改并重新读取的待办数量。
	TodoUpdated int `json:"todoUpdated"`
	// TodoRemoved 是从索引中删除的待办数量。
	TodoRemoved int `json:"todoRemoved"`
	// TodoTotal 是同步完成后索引中的待办总数。
	TodoTotal int `json:"todoTotal"`
}

// Change 是单文件同步的结果。
type Change struct {
	// ID 是变化涉及的笔记 ID；没有变化时为空。
	ID string `json:"id"`
	// Kind 是变化类型。
	Kind ChangeKind `json:"kind"`
}

// Index 是工作台的 SQLite 索引。
//
// 索引只是缓存，Markdown 文件才是事实来源；索引文件可以随时删除重建。
// Index 可被多个 goroutine 同时使用，写操作在进程内串行化。
type Index struct {
	// db 是底层数据库连接池。
	db *sql.DB
	// ws 是索引所属的工作台。
	ws *store.Workspace
	// path 是索引文件路径。
	path string
	// mu 串行化进程内的写操作，避免同一进程内出现写冲突。
	mu sync.Mutex
}

// Open 打开工作台的索引，必要时创建索引目录与数据库结构。
//
// 索引文件位于 <工作台>/.kuibu/index.db。数据库使用 WAL 与 busy_timeout，
// 允许 App 与 CLI 同时访问。PRAGMA user_version 与当前结构版本不一致时，
// 会删掉旧表并按最新结构重建。
func Open(ws *store.Workspace) (*Index, error) {
	if ws == nil {
		return nil, fmt.Errorf("%w: 工作台为空", store.ErrInvalid)
	}
	if err := os.MkdirAll(ws.MetaDir(), 0o755); err != nil {
		return nil, fmt.Errorf("创建索引目录失败: %w", err)
	}

	path := filepath.Join(ws.MetaDir(), indexFileName)
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("打开索引数据库失败: %w", err)
	}
	// 单连接即可满足桌面场景，同时避免同进程内多个连接互相抢写锁。
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("连接索引数据库失败: %w", err)
	}

	ix := &Index{db: db, ws: ws, path: path}
	if err := ix.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return ix, nil
}

// dsn 构造 modernc.org/sqlite 的连接串，开启 WAL、busy_timeout 与 NORMAL 同步。
func dsn(path string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(10000)")
	q.Add("_pragma", "synchronous(NORMAL)")
	u.RawQuery = q.Encode()
	return u.String()
}

// initSchema 读取并校验结构版本，不匹配时删表重建。
func (ix *Index) initSchema() error {
	var version int
	if err := ix.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("读取索引版本失败: %w", err)
	}
	if version == schemaVersion {
		return nil
	}

	tx, err := ix.db.Begin()
	if err != nil {
		return fmt.Errorf("开始索引结构重建失败: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// 索引只是缓存，版本不匹配时直接丢弃旧数据。
	for _, stmt := range []string{
		"DROP TABLE IF EXISTS notes_fts",
		"DROP TABLE IF EXISTS notes",
		"DROP TABLE IF EXISTS todos_fts",
		"DROP TABLE IF EXISTS todos",
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("删除旧索引表失败: %w", err)
		}
	}
	for _, stmt := range []string{
		`CREATE TABLE notes (
			id      TEXT PRIMARY KEY,
			title   TEXT NOT NULL DEFAULT '',
			type    TEXT NOT NULL DEFAULT 'note',
			parent  TEXT NOT NULL DEFAULT '',
			ord     INTEGER NOT NULL DEFAULT 0,
			tags    TEXT NOT NULL DEFAULT '[]',
			created TEXT NOT NULL DEFAULT '',
			updated TEXT NOT NULL DEFAULT '',
			path    TEXT NOT NULL,
			mtime   INTEGER NOT NULL DEFAULT 0,
			size    INTEGER NOT NULL DEFAULT 0,
			extra   TEXT NOT NULL DEFAULT '{}'
		)`,
		`CREATE INDEX notes_parent_ord ON notes(parent, ord, id)`,
		`CREATE INDEX notes_path ON notes(path)`,
		// trigram 分词器支持中文子串检索；少于 3 个字符的查询走 LIKE 回退。
		`CREATE VIRTUAL TABLE notes_fts USING fts5(
			id UNINDEXED,
			title,
			body,
			tokenize='trigram'
		)`,
		`CREATE TABLE todos (
			id          TEXT PRIMARY KEY,
			title       TEXT NOT NULL DEFAULT '',
			status      TEXT NOT NULL DEFAULT 'todo',
			due_date    TEXT NOT NULL DEFAULT '',
			due_time    TEXT NOT NULL DEFAULT '',
			due_at      TEXT NOT NULL DEFAULT '',
			done_at     TEXT NOT NULL DEFAULT '',
			parent      TEXT NOT NULL DEFAULT '',
			ord         INTEGER NOT NULL DEFAULT 0,
			tags        TEXT NOT NULL DEFAULT '[]',
			links       TEXT NOT NULL DEFAULT '[]',
			attachments TEXT NOT NULL DEFAULT '[]',
			source      TEXT NOT NULL DEFAULT '',
			created     TEXT NOT NULL DEFAULT '',
			updated     TEXT NOT NULL DEFAULT '',
			path        TEXT NOT NULL,
			mtime       INTEGER NOT NULL DEFAULT 0,
			size        INTEGER NOT NULL DEFAULT 0,
			extra       TEXT NOT NULL DEFAULT '{}'
		)`,
		`CREATE INDEX todos_parent_ord ON todos(parent, ord, id)`,
		`CREATE INDEX todos_status_due ON todos(status, due_date)`,
		`CREATE INDEX todos_path ON todos(path)`,
		`CREATE VIRTUAL TABLE todos_fts USING fts5(
			id UNINDEXED,
			title,
			body,
			tokenize='trigram'
		)`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("创建索引表失败: %w", err)
		}
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return fmt.Errorf("写入索引版本失败: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交索引结构重建失败: %w", err)
	}
	return nil
}

// Close 关闭索引数据库。
func (ix *Index) Close() error {
	if ix == nil || ix.db == nil {
		return nil
	}
	return ix.db.Close()
}

// Path 返回索引文件的绝对路径。
func (ix *Index) Path() string {
	return ix.path
}

// Rebuild 清空索引，并从全部笔记文件重新构建。
//
// 单个文件读取失败会返回错误；此时索引可能只重建了一部分，再次调用即可修复。
func (ix *Index) Rebuild(ctx context.Context) (Stats, error) {
	ix.mu.Lock()
	defer ix.mu.Unlock()

	tx, err := ix.db.BeginTx(ctx, nil)
	if err != nil {
		return Stats{}, fmt.Errorf("开始重建索引失败: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, stmt := range []string{
		"DELETE FROM notes_fts",
		"DELETE FROM notes",
		"DELETE FROM todos_fts",
		"DELETE FROM todos",
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return Stats{}, fmt.Errorf("清空索引失败: %w", err)
		}
	}

	paths, err := ix.listFiles()
	if err != nil {
		return Stats{}, err
	}
	var stats Stats
	for _, path := range paths {
		change, err := ix.upsert(ctx, tx, path, false)
		if err != nil {
			return Stats{}, err
		}
		todoPath := isTodoPath(ix.ws, path)
		switch change.Kind {
		case ChangeAdded:
			if todoPath {
				stats.TodoAdded++
			} else {
				stats.Added++
			}
		case ChangeUpdated:
			if todoPath {
				stats.TodoUpdated++
			} else {
				stats.Updated++
			}
		}
	}

	total, err := countNotes(ctx, tx)
	if err != nil {
		return Stats{}, err
	}
	stats.Total = total
	todoTotal, err := countTodos(ctx, tx)
	if err != nil {
		return Stats{}, err
	}
	stats.TodoTotal = todoTotal

	if err := tx.Commit(); err != nil {
		return Stats{}, fmt.Errorf("提交索引重建失败: %w", err)
	}
	return stats, nil
}

// Sync 增量同步索引：新增或修改过的文件重新读取，已删除的文件从索引删除。
//
// 是否变化按 path + mtime + size 判断。
func (ix *Index) Sync(ctx context.Context) (Stats, error) {
	ix.mu.Lock()
	defer ix.mu.Unlock()

	tx, err := ix.db.BeginTx(ctx, nil)
	if err != nil {
		return Stats{}, fmt.Errorf("开始同步索引失败: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	existing, err := loadExisting(ctx, tx)
	if err != nil {
		return Stats{}, err
	}

	paths, err := ix.listFiles()
	if err != nil {
		return Stats{}, err
	}

	current := make(map[string]bool, len(paths))
	var stats Stats
	for _, path := range paths {
		current[path] = true
		info, statErr := os.Stat(path)
		if errors.Is(statErr, os.ErrNotExist) {
			// 枚举后被外部删除，按删除处理。
			if _, ok := existing[path]; ok {
				if _, _, err := ix.deleteFile(ctx, tx, path); err != nil {
					return Stats{}, err
				}
				if isTodoPath(ix.ws, path) {
					stats.TodoRemoved++
				} else {
					stats.Removed++
				}
			}
			continue
		}
		if statErr != nil {
			return Stats{}, fmt.Errorf("读取笔记文件信息失败: %w", statErr)
		}

		old, ok := existing[path]
		if ok && old.mtime == info.ModTime().UnixNano() && old.size == info.Size() {
			continue
		}

		change, err := ix.upsert(ctx, tx, path, false)
		if err != nil {
			return Stats{}, err
		}
		todoPath := isTodoPath(ix.ws, path)
		switch change.Kind {
		case ChangeAdded:
			if todoPath {
				stats.TodoAdded++
			} else {
				stats.Added++
			}
		case ChangeUpdated:
			if todoPath {
				stats.TodoUpdated++
			} else {
				stats.Updated++
			}
		case ChangeRemoved:
			if todoPath {
				stats.TodoRemoved++
			} else {
				stats.Removed++
			}
		}
	}

	for path := range existing {
		if current[path] {
			continue
		}
		if _, _, err := ix.deleteFile(ctx, tx, path); err != nil {
			return Stats{}, err
		}
		if isTodoPath(ix.ws, path) {
			stats.TodoRemoved++
		} else {
			stats.Removed++
		}
	}

	total, err := countNotes(ctx, tx)
	if err != nil {
		return Stats{}, err
	}
	stats.Total = total
	todoTotal, err := countTodos(ctx, tx)
	if err != nil {
		return Stats{}, err
	}
	stats.TodoTotal = todoTotal

	if err := tx.Commit(); err != nil {
		return Stats{}, fmt.Errorf("提交索引同步失败: %w", err)
	}
	return stats, nil
}

// UpsertFile 重新读取单个笔记文件并更新索引，供文件监听器使用。
//
// 即使 mtime 与 size 没变也会重读，避免同一秒内的小改动被漏掉。文件不存在
// 时等价于 RemoveFile。
func (ix *Index) UpsertFile(path string) (Change, error) {
	ix.mu.Lock()
	defer ix.mu.Unlock()

	tx, err := ix.db.Begin()
	if err != nil {
		return Change{}, fmt.Errorf("开始更新索引失败: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	change, err := ix.upsert(context.Background(), tx, path, true)
	if err != nil {
		return Change{}, err
	}
	if err := tx.Commit(); err != nil {
		return Change{}, fmt.Errorf("提交索引更新失败: %w", err)
	}
	return change, nil
}

// RemoveFile 从索引中删除指定路径的笔记。
//
// 路径不在索引中时返回 ChangeUnchanged。
func (ix *Index) RemoveFile(path string) (Change, error) {
	ix.mu.Lock()
	defer ix.mu.Unlock()

	tx, err := ix.db.Begin()
	if err != nil {
		return Change{}, fmt.Errorf("开始删除索引项失败: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	id, removed, err := ix.deleteFile(context.Background(), tx, path)
	if err != nil {
		return Change{}, err
	}
	if err := tx.Commit(); err != nil {
		return Change{}, fmt.Errorf("提交删除索引项失败: %w", err)
	}
	if !removed {
		return Change{Kind: ChangeUnchanged}, nil
	}
	return Change{ID: id, Kind: ChangeRemoved}, nil
}

// rowState 是索引中已有的文件状态，用于增量比较。
type rowState struct {
	mtime int64
	size  int64
}

// loadExisting 读取索引中全部 path 及其 mtime/size，笔记与待办合并返回。
func loadExisting(ctx context.Context, tx *sql.Tx) (map[string]rowState, error) {
	existing := map[string]rowState{}
	for _, table := range []string{"notes", "todos"} {
		rows, err := tx.QueryContext(ctx, "SELECT path, mtime, size FROM "+table)
		if err != nil {
			return nil, fmt.Errorf("读取索引状态失败: %w", err)
		}
		for rows.Next() {
			var path string
			var st rowState
			if err := rows.Scan(&path, &st.mtime, &st.size); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("读取索引状态失败: %w", err)
			}
			existing[path] = st
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("读取索引状态失败: %w", err)
		}
		_ = rows.Close()
	}
	return existing, nil
}

// countNotes 返回索引中的笔记总数。
func countNotes(ctx context.Context, tx *sql.Tx) (int, error) {
	var total int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM notes").Scan(&total); err != nil {
		return 0, fmt.Errorf("统计索引笔记数失败: %w", err)
	}
	return total, nil
}

// countTodos 返回索引中的待办总数。
func countTodos(ctx context.Context, tx *sql.Tx) (int, error) {
	var total int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM todos").Scan(&total); err != nil {
		return 0, fmt.Errorf("统计索引待办数失败: %w", err)
	}
	return total, nil
}

// upsert 按路径所在目录分派到笔记或待办索引，返回变化类型。
// force 为 true 时即使 mtime 与 size 未变也重新读取（供 watcher 使用）。
func (ix *Index) upsert(ctx context.Context, tx *sql.Tx, path string, force bool) (Change, error) {
	if isTodoPath(ix.ws, path) {
		return ix.upsertTodo(ctx, tx, path, force)
	}
	return ix.upsertNote(ctx, tx, path, force)
}

// upsertNote 读取笔记文件并写入笔记索引，返回变化类型。
func (ix *Index) upsertNote(ctx context.Context, tx *sql.Tx, path string, force bool) (Change, error) {
	path = filepath.Clean(path)

	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		id, removed, err := deleteNote(ctx, tx, path)
		if err != nil {
			return Change{}, err
		}
		if removed {
			return Change{ID: id, Kind: ChangeRemoved}, nil
		}
		return Change{Kind: ChangeUnchanged}, nil
	}
	if err != nil {
		return Change{}, fmt.Errorf("读取笔记文件信息失败: %w", err)
	}
	if info.IsDir() {
		return Change{Kind: ChangeUnchanged}, nil
	}

	note, err := store.ReadFile(path)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			id, removed, delErr := deleteNote(ctx, tx, path)
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

	// 先按路径找旧记录，找不到再按 ID 找（文件被改名时路径会变）。
	var (
		oldMtime int64
		oldSize  int64
		exists   bool
	)
	err = tx.QueryRowContext(ctx,
		"SELECT mtime, size FROM notes WHERE path = ?", path).Scan(&oldMtime, &oldSize)
	switch {
	case err == nil:
		exists = true
	case errors.Is(err, sql.ErrNoRows):
		err = tx.QueryRowContext(ctx,
			"SELECT mtime, size FROM notes WHERE id = ?", note.ID).Scan(&oldMtime, &oldSize)
		switch {
		case err == nil:
			exists = true
		case errors.Is(err, sql.ErrNoRows):
			// 新笔记。
		default:
			return Change{}, fmt.Errorf("查询索引项失败: %w", err)
		}
	default:
		return Change{}, fmt.Errorf("查询索引项失败: %w", err)
	}

	if exists && !force && oldMtime == info.ModTime().UnixNano() && oldSize == info.Size() {
		return Change{ID: note.ID, Kind: ChangeUnchanged}, nil
	}

	// 同一笔记可能因改名导致旧 path 或旧 id 残留，一并清理后再写入。
	for _, stmt := range []string{
		"DELETE FROM notes_fts WHERE id = ?",
		"DELETE FROM notes WHERE path = ? OR id = ?",
	} {
		args := []any{note.ID}
		if strings.HasPrefix(stmt, "DELETE FROM notes WHERE") {
			args = []any{path, note.ID}
		}
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return Change{}, fmt.Errorf("清理旧索引项失败: %w", err)
		}
	}

	tags, err := json.Marshal(tagsOf(note))
	if err != nil {
		tags = []byte("[]")
	}
	extra := []byte("{}")
	if len(note.Extra) > 0 {
		if data, err := json.Marshal(note.Extra); err == nil {
			extra = data
		}
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO notes
		(id, title, type, parent, ord, tags, created, updated, path, mtime, size, extra)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		note.ID, note.Title, noteType(note), note.Parent, note.Order,
		string(tags), formatTime(note.Created), formatTime(note.Updated),
		path, info.ModTime().UnixNano(), info.Size(), string(extra))
	if err != nil {
		return Change{}, fmt.Errorf("写入索引项失败: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO notes_fts (id, title, body) VALUES (?, ?, ?)",
		note.ID, note.Title, note.Body); err != nil {
		return Change{}, fmt.Errorf("写入全文索引失败: %w", err)
	}

	if exists {
		return Change{ID: note.ID, Kind: ChangeUpdated}, nil
	}
	return Change{ID: note.ID, Kind: ChangeAdded}, nil
}

// upsertTodo 读取待办文件并写入待办索引，返回变化类型。
func (ix *Index) upsertTodo(ctx context.Context, tx *sql.Tx, path string, force bool) (Change, error) {
	path = filepath.Clean(path)

	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		id, removed, err := deleteTodo(ctx, tx, path)
		if err != nil {
			return Change{}, err
		}
		if removed {
			return Change{ID: id, Kind: ChangeRemoved}, nil
		}
		return Change{Kind: ChangeUnchanged}, nil
	}
	if err != nil {
		return Change{}, fmt.Errorf("读取待办文件信息失败: %w", err)
	}
	if info.IsDir() {
		return Change{Kind: ChangeUnchanged}, nil
	}

	todo, err := store.ReadTodoFile(path)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			id, removed, delErr := deleteTodo(ctx, tx, path)
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
		"SELECT mtime, size FROM todos WHERE path = ?", path).Scan(&oldMtime, &oldSize)
	switch {
	case err == nil:
		exists = true
	case errors.Is(err, sql.ErrNoRows):
		err = tx.QueryRowContext(ctx,
			"SELECT mtime, size FROM todos WHERE id = ?", todo.ID).Scan(&oldMtime, &oldSize)
		switch {
		case err == nil:
			exists = true
		case errors.Is(err, sql.ErrNoRows):
			// 新待办。
		default:
			return Change{}, fmt.Errorf("查询待办索引项失败: %w", err)
		}
	default:
		return Change{}, fmt.Errorf("查询待办索引项失败: %w", err)
	}

	if exists && !force && oldMtime == info.ModTime().UnixNano() && oldSize == info.Size() {
		return Change{ID: todo.ID, Kind: ChangeUnchanged}, nil
	}

	// 同一待办可能因改名导致旧 path 或旧 id 残留，一并清理后再写入。
	for _, stmt := range []string{
		"DELETE FROM todos_fts WHERE id = ?",
		"DELETE FROM todos WHERE path = ? OR id = ?",
	} {
		args := []any{todo.ID}
		if strings.HasPrefix(stmt, "DELETE FROM todos WHERE") {
			args = []any{path, todo.ID}
		}
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return Change{}, fmt.Errorf("清理旧待办索引项失败: %w", err)
		}
	}

	tags, err := json.Marshal(tagsOfTodo(todo))
	if err != nil {
		tags = []byte("[]")
	}
	links, err := json.Marshal(linksOfTodo(todo))
	if err != nil {
		links = []byte("[]")
	}
	attachments, err := json.Marshal(attachmentsOfTodo(todo))
	if err != nil {
		attachments = []byte("[]")
	}
	extra := []byte("{}")
	if len(todo.Extra) > 0 {
		if data, err := json.Marshal(todo.Extra); err == nil {
			extra = data
		}
	}

	dueDate, dueTime, dueAt := "", "", ""
	if todo.Due != nil {
		dueDate = todo.Due.Date
		dueTime = todo.Due.Time
		if at := todo.Due.At(store.TodoLocation()); !at.IsZero() {
			dueAt = at.Format(time.RFC3339)
		}
	}
	doneAt := ""
	if todo.DoneAt != nil && !todo.DoneAt.IsZero() {
		doneAt = todo.DoneAt.Format(time.RFC3339)
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO todos
		(id, title, status, due_date, due_time, due_at, done_at, parent, ord,
		 tags, links, attachments, source, created, updated, path, mtime, size, extra)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		todo.ID, todo.Title, todo.Status, dueDate, dueTime, dueAt, doneAt,
		todo.Parent, todo.Order, string(tags), string(links), string(attachments),
		todo.Source, formatTime(todo.Created), formatTime(todo.Updated),
		path, info.ModTime().UnixNano(), info.Size(), string(extra))
	if err != nil {
		return Change{}, fmt.Errorf("写入待办索引项失败: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO todos_fts (id, title, body) VALUES (?, ?, ?)",
		todo.ID, todo.Title, todo.Body); err != nil {
		return Change{}, fmt.Errorf("写入待办全文索引失败: %w", err)
	}

	if exists {
		return Change{ID: todo.ID, Kind: ChangeUpdated}, nil
	}
	return Change{ID: todo.ID, Kind: ChangeAdded}, nil
}

// deleteNote 按路径删除索引项，返回被删除的笔记 ID 与是否真的删除。
func deleteNote(ctx context.Context, tx *sql.Tx, path string) (string, bool, error) {
	path = filepath.Clean(path)
	var id string
	err := tx.QueryRowContext(ctx, "SELECT id FROM notes WHERE path = ?", path).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("查询待删除索引项失败: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM notes_fts WHERE id = ?", id); err != nil {
		return "", false, fmt.Errorf("删除全文索引失败: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM notes WHERE id = ?", id); err != nil {
		return "", false, fmt.Errorf("删除索引项失败: %w", err)
	}
	return id, true, nil
}

// deleteTodo 按路径删除待办索引项，返回被删除的待办 ID 与是否真的删除。
func deleteTodo(ctx context.Context, tx *sql.Tx, path string) (string, bool, error) {
	path = filepath.Clean(path)
	var id string
	err := tx.QueryRowContext(ctx, "SELECT id FROM todos WHERE path = ?", path).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("查询待删除待办索引项失败: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM todos_fts WHERE id = ?", id); err != nil {
		return "", false, fmt.Errorf("删除待办全文索引失败: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM todos WHERE id = ?", id); err != nil {
		return "", false, fmt.Errorf("删除待办索引项失败: %w", err)
	}
	return id, true, nil
}

// deleteFile 按路径所在目录分派删除笔记或待办索引项。
func (ix *Index) deleteFile(ctx context.Context, tx *sql.Tx, path string) (string, bool, error) {
	if isTodoPath(ix.ws, path) {
		return deleteTodo(ctx, tx, path)
	}
	return deleteNote(ctx, tx, path)
}

// listFiles 返回笔记与待办目录下需要索引的 .md 文件绝对路径。
func (ix *Index) listFiles() ([]string, error) {
	// 索引与 store 共用同一套文件枚举规则，避免两处不一致。
	notes, err := ix.ws.ListWorkspaceNotes()
	if err != nil {
		return nil, err
	}
	todos, err := ix.ws.ListWorkspaceTodos()
	if err != nil {
		return nil, err
	}
	return append(notes, todos...), nil
}

// noteType 返回笔记类型，空值统一为 note。
func noteType(n *store.Note) string {
	if n.Type == "" {
		return "note"
	}
	return n.Type
}

// tagsOf 返回非 nil 的标签切片，保证 JSON 序列化为 [] 而不是 null。
func tagsOf(n *store.Note) []string {
	if n.Tags == nil {
		return []string{}
	}
	return n.Tags
}

// tagsOfTodo 返回非 nil 的待办标签切片。
func tagsOfTodo(t *store.Todo) []string {
	if t.Tags == nil {
		return []string{}
	}
	return t.Tags
}

// linksOfTodo 返回非 nil 的待办链接切片。
func linksOfTodo(t *store.Todo) []store.TodoLink {
	if t.Links == nil {
		return []store.TodoLink{}
	}
	return t.Links
}

// attachmentsOfTodo 返回非 nil 的待办附件切片。
func attachmentsOfTodo(t *store.Todo) []string {
	if t.Attachments == nil {
		return []string{}
	}
	return t.Attachments
}

// isTodoPath 判断路径是否位于待办目录下。
func isTodoPath(ws *store.Workspace, path string) bool {
	return filepath.Dir(filepath.Clean(path)) == filepath.Clean(ws.TodosDir())
}

// formatTime 把时间格式化为 RFC3339；零值输出空字符串。
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// isNoteFileName 判断文件名是否是需要索引的普通笔记文件。
func isNoteFileName(name string) bool {
	return !strings.HasPrefix(name, ".") && strings.HasSuffix(name, ".md")
}
