package task

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动（与 internal/store 共享）
)

// SQLiteStore 是 Store 接口的 SQLite 实现。
//
// 设计：自管理 schema，独立 db 文件；不与 internal/store 共享 db，
// 避免耦合（task 表与 sessions/events 表生命周期独立）。
//
// 表结构：
//
//	CREATE TABLE tasks (
//	    id           TEXT PRIMARY KEY,
//	    code         TEXT,
//	    title        TEXT NOT NULL,
//	    input        TEXT NOT NULL,
//	    session_id   TEXT,
//	    state        INTEGER NOT NULL DEFAULT 0,
//	    profile      TEXT NOT NULL DEFAULT 'headless',
//	    permission   TEXT,
//	    owner        TEXT,
//	    error        TEXT,
//	    created_at   INTEGER NOT NULL,
//	    updated_at   INTEGER NOT NULL,
//	    started_at   INTEGER,
//	    finished_at  INTEGER,
//	    prompt_tok   INTEGER NOT NULL DEFAULT 0,
//	    comp_tok     INTEGER NOT NULL DEFAULT 0,
//	    total_tok    INTEGER NOT NULL DEFAULT 0
//	)
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore 打开（或创建）path 处数据库并执行 schema 迁移。
// path=":memory:" 用于测试。
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("task: open %q: %w", path, err)
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("task: schema: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("task: pragma wal: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("task: pragma busy_timeout: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

// Close 释放数据库连接。
func (s *SQLiteStore) Close() error { return s.db.Close() }

const schemaSQL = `
CREATE TABLE IF NOT EXISTS tasks (
    id           TEXT PRIMARY KEY,
    code         TEXT,
    title        TEXT NOT NULL,
    input        TEXT NOT NULL,
    session_id   TEXT,
    state        INTEGER NOT NULL DEFAULT 0,
    profile      TEXT NOT NULL DEFAULT 'headless',
    permission   TEXT,
    owner        TEXT,
    error        TEXT,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    started_at   INTEGER,
    finished_at  INTEGER,
    prompt_tok   INTEGER NOT NULL DEFAULT 0,
    comp_tok     INTEGER NOT NULL DEFAULT 0,
    total_tok    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_tasks_state ON tasks(state);
CREATE INDEX IF NOT EXISTS idx_tasks_session ON tasks(session_id);
CREATE INDEX IF NOT EXISTS idx_tasks_updated ON tasks(updated_at DESC);
`

// Insert 插入新 Task 行。ID 必须由调用方生成（Executor 用 newID()）。
func (s *SQLiteStore) Insert(ctx context.Context, t *Task) error {
	if t.ID == "" {
		return errors.New("task: insert: empty id")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tasks(id, code, title, input, session_id, state,
		    profile, permission, owner, error,
		    created_at, updated_at, started_at, finished_at,
		    prompt_tok, comp_tok, total_tok)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Code, t.Title, t.Input, t.SessionID, int(t.State),
		t.Profile, t.Permission, t.Owner, t.Error,
		t.CreatedAt.UnixMilli(), t.UpdatedAt.UnixMilli(),
		optMs(t.StartedAt), optMs(t.FinishedAt),
		t.Usage.PromptTokens, t.Usage.CompletionTokens, t.Usage.TotalTokens,
	)
	if err != nil {
		return fmt.Errorf("task: insert: %w", err)
	}
	return nil
}

// Update 全量覆盖（除 ID / CreatedAt 外）。
func (s *SQLiteStore) Update(ctx context.Context, t *Task) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET
		    code=?, title=?, input=?, session_id=?, state=?,
		    profile=?, permission=?, owner=?, error=?,
		    updated_at=?, started_at=?, finished_at=?,
		    prompt_tok=?, comp_tok=?, total_tok=?
		 WHERE id=?`,
		t.Code, t.Title, t.Input, t.SessionID, int(t.State),
		t.Profile, t.Permission, t.Owner, t.Error,
		t.UpdatedAt.UnixMilli(), optMs(t.StartedAt), optMs(t.FinishedAt),
		t.Usage.PromptTokens, t.Usage.CompletionTokens, t.Usage.TotalTokens,
		t.ID,
	)
	if err != nil {
		return fmt.Errorf("task: update: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Get 返回 id 对应 Task；不存在返回 ErrNotFound。
func (s *SQLiteStore) Get(ctx context.Context, id string) (*Task, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, code, title, input, session_id, state,
		    profile, permission, owner, error,
		    created_at, updated_at, started_at, finished_at,
		    prompt_tok, comp_tok, total_tok
		 FROM tasks WHERE id=?`, id,
	)
	return scanTask(row)
}

// List 按 filter 查询；filter.Limit==0 时默认 50。
func (s *SQLiteStore) List(ctx context.Context, filter Filter) ([]*Task, error) {
	q := `SELECT id, code, title, input, session_id, state,
	        profile, permission, owner, error,
	        created_at, updated_at, started_at, finished_at,
	        prompt_tok, comp_tok, total_tok
	      FROM tasks WHERE 1=1`
	args := []any{}

	if len(filter.States) > 0 {
		placeholders := ""
		for i, st := range filter.States {
			if i > 0 {
				placeholders += ","
			}
			placeholders += "?"
			args = append(args, int(st))
		}
		q += " AND state IN (" + placeholders + ")"
	}
	// 注意：单 State 过滤由调用方通过 States=[…] 显式表达，
	// 避免与"零值 Filter{}"歧义（StatePending 是零值）。
	if filter.Owner != "" {
		q += " AND owner=?"
		args = append(args, filter.Owner)
	}
	if filter.SessionID != "" {
		q += " AND session_id=?"
		args = append(args, filter.SessionID)
	}
	if filter.Code != "" {
		q += " AND code=?"
		args = append(args, filter.Code)
	}
	q += " ORDER BY updated_at DESC"

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	q += " LIMIT ? OFFSET ?"
	args = append(args, limit, filter.Offset)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("task: list: %w", err)
	}
	defer rows.Close()

	var out []*Task
	for rows.Next() {
		t, err := scanTaskRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("task: list rows: %w", err)
	}
	return out, nil
}

// scanTask 适配 QueryRow.Scan → *Task。
func scanTask(row *sql.Row) (*Task, error) {
	var (
		id, code, title, input, sid, profile, perm, owner, errMsg string
		st                                                       int
		createdAt, updatedAt                                      int64
		startedAt, finishedAt                                     sql.NullInt64
		pt, ct, tt                                               int
	)
	if err := row.Scan(
		&id, &code, &title, &input, &sid, &st,
		&profile, &perm, &owner, &errMsg,
		&createdAt, &updatedAt, &startedAt, &finishedAt,
		&pt, &ct, &tt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("task: scan: %w", err)
	}
	return hydrate(id, code, title, input, sid, st, profile, perm, owner, errMsg,
		createdAt, updatedAt, startedAt, finishedAt, pt, ct, tt), nil
}

// scanTaskRow 适配 rows.Scan → *Task。
func scanTaskRow(rows *sql.Rows) (*Task, error) {
	var (
		id, code, title, input, sid, profile, perm, owner, errMsg string
		st                                                       int
		createdAt, updatedAt                                      int64
		startedAt, finishedAt                                     sql.NullInt64
		pt, ct, tt                                               int
	)
	if err := rows.Scan(
		&id, &code, &title, &input, &sid, &st,
		&profile, &perm, &owner, &errMsg,
		&createdAt, &updatedAt, &startedAt, &finishedAt,
		&pt, &ct, &tt,
	); err != nil {
		return nil, fmt.Errorf("task: scan row: %w", err)
	}
	return hydrate(id, code, title, input, sid, st, profile, perm, owner, errMsg,
		createdAt, updatedAt, startedAt, finishedAt, pt, ct, tt), nil
}

func hydrate(
	id, code, title, input, sid string, st int,
	profile, perm, owner, errMsg string,
	createdAt, updatedAt int64,
	startedAt, finishedAt sql.NullInt64,
	pt, ct, tt int,
) *Task {
	t := &Task{
		ID:         id,
		Code:       code,
		Title:      title,
		Input:      input,
		SessionID:  sid,
		State:      State(st),
		Profile:    profile,
		Permission: perm,
		Owner:      owner,
		Error:      errMsg,
		CreatedAt:  time.UnixMilli(createdAt),
		UpdatedAt:  time.UnixMilli(updatedAt),
		Usage:      Usage{PromptTokens: pt, CompletionTokens: ct, TotalTokens: tt},
	}
	if startedAt.Valid {
		v := time.UnixMilli(startedAt.Int64)
		t.StartedAt = &v
	}
	if finishedAt.Valid {
		v := time.UnixMilli(finishedAt.Int64)
		t.FinishedAt = &v
	}
	return t
}

func optMs(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UnixMilli()
}
