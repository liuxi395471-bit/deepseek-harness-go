package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // 纯 Go 的 SQLite 驱动

	"deepseek-harness-go/internal/llm"
)

// SQLiteStore 通过纯 Go 的 modernc.org/sqlite 驱动将 会话持久化到单个
// SQLite 数据库文件。
//
// 模式（DESIGN-v2 §A.3.3）：
//
//	CREATE TABLE sessions (
//	    id TEXT PRIMARY KEY,
//	    created_at INTEGER, updated_at INTEGER,
//	    preview TEXT, rounds INTEGER DEFAULT 0,
//	    prompt_tokens INTEGER, completion_tokens INTEGER, total_tokens INTEGER
//	)
//	CREATE TABLE messages (
//	    session_id TEXT REFERENCES sessions(id) ON DELETE CASCADE,
//	    seq INTEGER, role TEXT, content TEXT,
//	    tool_call_id TEXT, tool_calls TEXT,
//	    PRIMARY KEY (session_id, seq)
//	)
//
// 所有公开方法都可安全并发使用。
type SQLiteStore struct {
	db     *sql.DB
	closed bool
}

// NewSQLiteStore 打开（或创建）path 处的 SQLite 数据库并执行模式迁移。
// 允许使用路径 ":memory:"（测试用内存库）。
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: open %q: %w", path, err)
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: schema: %w", err)
	}
	// modernc.org/sqlite 默认关闭外键；启用它使 ON DELETE CASCADE 真正生效。
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: pragma: %w", err)
	}
	// WAL 模式为并发 Append 调用方提供更好的写并发。
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: pragma wal: %w", err)
	}
	// busy_timeout 让并发写者最多等待 5 秒而不是立即失败。
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: pragma busy_timeout: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS sessions (
    id          TEXT PRIMARY KEY,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    preview     TEXT NOT NULL DEFAULT '',
    rounds      INTEGER NOT NULL DEFAULT 0,
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens      INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS messages (
    session_id  TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    seq         INTEGER NOT NULL,
    role        TEXT NOT NULL,
    content     TEXT NOT NULL DEFAULT '',
    tool_call_id TEXT NOT NULL DEFAULT '',
    tool_calls  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, seq);
`

// Begin 插入新的会话行并返回它。
func (s *SQLiteStore) Begin(ctx context.Context) (Session, error) {
	id, err := newID()
	if err != nil {
		return Session{}, err
	}
	now := time.Now()
	nowMs := time.Now().UnixMilli()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions(id, created_at, updated_at) VALUES (?, ?, ?)`,
		id, nowMs, nowMs); err != nil {
		return Session{}, fmt.Errorf("store: begin: %w", err)
	}
	return Session{ID: id, CreatedAt: now, UpdatedAt: now}, nil
}

// Append 以下一个 seq 值插入新的消息行。
func (s *SQLiteStore) Append(ctx context.Context, id string, msg llm.Message) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: append begin: %w", err)
	}
	defer tx.Rollback()

	// 校验会话存在；更新 updated_at；计算下一个 seq。
	var updatedAt int64
	err = tx.QueryRowContext(ctx, `SELECT updated_at FROM sessions WHERE id=?`, id).Scan(&updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("store: append lookup: %w", err)
	}

	var nextSeq int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq), -1) + 1 FROM messages WHERE session_id=?`, id,
	).Scan(&nextSeq); err != nil {
		return fmt.Errorf("store: append seq: %w", err)
	}

	tcJSON := ""
	if len(msg.ToolCalls) > 0 {
		b, err := json.Marshal(msg.ToolCalls)
		if err != nil {
			return fmt.Errorf("store: marshal tool_calls: %w", err)
		}
		tcJSON = string(b)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO messages(session_id, seq, role, content, tool_call_id, tool_calls)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, nextSeq, string(msg.Role), msg.Content, msg.ToolCallID, tcJSON,
	); err != nil {
		return fmt.Errorf("store: append insert: %w", err)
	}

	// 如果这是第一条用户消息，更新 preview。
	var preview string
	if msg.Role == llm.RoleUser {
		if err := tx.QueryRowContext(ctx,
			`SELECT preview FROM sessions WHERE id=?`, id,
		).Scan(&preview); err != nil {
			return fmt.Errorf("store: append preview: %w", err)
		}
		if preview == "" {
			newPreview := truncatePreview(msg.Content)
			if _, err := tx.ExecContext(ctx,
				`UPDATE sessions SET preview=? WHERE id=?`, newPreview, id,
			); err != nil {
				return fmt.Errorf("store: append preview update: %w", err)
			}
		}
	}

	// assistant 消息使 rounds 递增。
	if msg.Role == llm.RoleAssistant {
		if _, err := tx.ExecContext(ctx,
			`UPDATE sessions SET rounds = rounds + 1, updated_at=? WHERE id=?`,
			time.Now().UnixMilli(), id,
		); err != nil {
			return fmt.Errorf("store: append rounds: %w", err)
		}
	} else {
		if _, err := tx.ExecContext(ctx,
			`UPDATE sessions SET updated_at=? WHERE id=?`, time.Now().Unix(), id,
		); err != nil {
			return fmt.Errorf("store: append touch: %w", err)
		}
	}

	return tx.Commit()
}

// Load 读取全部会话元数据 + 消息。
func (s *SQLiteStore) Load(ctx context.Context, id string) (Session, error) {
	var (
		createdAt, updatedAt            int64
		preview                         string
		rounds, pt, ct, tt              int
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT created_at, updated_at, preview, rounds,
		        prompt_tokens, completion_tokens, total_tokens
		 FROM sessions WHERE id=?`, id,
	).Scan(&createdAt, &updatedAt, &preview, &rounds, &pt, &ct, &tt)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("store: load: %w", err)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT role, content, tool_call_id, tool_calls
		 FROM messages WHERE session_id=? ORDER BY seq ASC`, id,
	)
	if err != nil {
		return Session{}, fmt.Errorf("store: load messages: %w", err)
	}
	defer rows.Close()

	var msgs []llm.Message
	for rows.Next() {
		var (
			role, content, tcid, tcs string
		)
		if err := rows.Scan(&role, &content, &tcid, &tcs); err != nil {
			return Session{}, fmt.Errorf("store: scan message: %w", err)
		}
		m := llm.Message{
			Role:       llm.Role(role),
			Content:    content,
			ToolCallID: tcid,
		}
		if tcs != "" {
			if err := json.Unmarshal([]byte(tcs), &m.ToolCalls); err != nil {
				return Session{}, fmt.Errorf("store: unmarshal tool_calls: %w", err)
			}
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return Session{}, fmt.Errorf("store: rows: %w", err)
	}

	return Session{
		ID:         id,
		CreatedAt:  time.UnixMilli(createdAt),
		UpdatedAt:  time.UnixMilli(updatedAt),
		Rounds:     rounds,
		Preview:    preview,
		Messages:   msgs,
		UsageTotal: llm.Usage{PromptTokens: pt, CompletionTokens: ct, TotalTokens: tt},
	}, nil
}

// List 返回按 UpdatedAt 降序排列的仅含元数据的会话。limit<=0
// 表示"全部"。
func (s *SQLiteStore) List(ctx context.Context, limit, offset int) ([]Session, error) {
	query := `SELECT id, created_at, updated_at, preview, rounds,
	                 prompt_tokens, completion_tokens, total_tokens
	          FROM sessions ORDER BY updated_at DESC`
	args := []any{}
	if limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, limit, offset)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list: %w", err)
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var (
			id                            string
			createdAt, updatedAt          int64
			preview                       string
			rounds, pt, ct, tt            int
		)
		if err := rows.Scan(&id, &createdAt, &updatedAt, &preview, &rounds, &pt, &ct, &tt); err != nil {
			return nil, fmt.Errorf("store: list scan: %w", err)
		}
		out = append(out, Session{
			ID:         id,
			CreatedAt:  time.UnixMilli(createdAt),
			UpdatedAt:  time.UnixMilli(updatedAt),
			Rounds:     rounds,
			Preview:    preview,
			UsageTotal: llm.Usage{PromptTokens: pt, CompletionTokens: ct, TotalTokens: tt},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list rows: %w", err)
	}
	return out, nil
}

// UpdateUsage 将 delta 加到 UsageTotal。若 id 未知则返回 ErrNotFound。
func (s *SQLiteStore) UpdateUsage(ctx context.Context, id string, delta llm.Usage) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE sessions
		 SET prompt_tokens = prompt_tokens + ?,
		     completion_tokens = completion_tokens + ?,
		     total_tokens = total_tokens + ?,
		     updated_at = ?
		 WHERE id = ?`,
		delta.PromptTokens, delta.CompletionTokens, delta.TotalTokens,
		time.Now().UnixMilli(), id,
	)
	if err != nil {
		return fmt.Errorf("store: update_usage: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Close 关闭底层数据库。
func (s *SQLiteStore) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.db.Close()
}
