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
	db           *sql.DB
	closed       bool
	projectCache *sqliteProjectionCache
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
	return &SQLiteStore{
		db:           db,
		projectCache: &sqliteProjectionCache{cache: NewMemoryProjectionCache()},
	}, nil
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

-- v4 §A.4: events 表——Session 状态变更的不可变记录。messages
-- 表保留作为兼容层（v3 投影），但驱动源已切到事件流。
CREATE TABLE IF NOT EXISTS events (
    session_id  TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    seq         INTEGER NOT NULL,
    type        INTEGER NOT NULL,
    ts          INTEGER NOT NULL,
    payload     BLOB NOT NULL,
    actor       TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_events_session ON events(session_id, seq);
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

// AppendEvent 写入事件到 events 表（v4 §A.3）。seq 由存储按会话
// 单调递增分配；返回该值。Payload 为空时存 NULL（sqlite 不支持
// 零长度 BLOB 与 NULL 区分；这里用空字节切片）。
//
// 写入后会让该 sid 的全部投影缓存失效（与 MapStore 一致语义）。
func (s *SQLiteStore) AppendEvent(ctx context.Context, sid string, ev Event) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: append_event begin: %w", err)
	}
	defer tx.Rollback()

	// 校验会话存在。
	var dummy int64
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE id=?`, sid).Scan(&dummy)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("store: append_event lookup: %w", err)
	}

	// 计算下一个 seq。
	var nextSeq int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq), -1) + 1 FROM events WHERE session_id=?`, sid,
	).Scan(&nextSeq); err != nil {
		return 0, fmt.Errorf("store: append_event seq: %w", err)
	}

	// payload 必须非空——使用空字节切片而非 nil 以保证 BLOB 写入。
	pl := ev.Payload
	if pl == nil {
		pl = []byte{}
	}
	ts := ev.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO events(session_id, seq, type, ts, payload, actor)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		sid, nextSeq, int(ev.Type), ts.UnixNano(), pl, ev.Actor,
	); err != nil {
		return 0, fmt.Errorf("store: append_event insert: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: append_event commit: %w", err)
	}
	// 写后失效该 sid 的缓存。下次 Project 触发重放。
	s.projectCache.cache.Invalidate(sid)
	return nextSeq, nil
}

// ReadEvents 返回 sid 从 from 之后的事件，按 seq 升序。
func (s *SQLiteStore) ReadEvents(ctx context.Context, sid string, from int64) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT seq, type, ts, payload, actor
		 FROM events WHERE session_id=? AND seq >= ?
		 ORDER BY seq ASC`, sid, from,
	)
	if err != nil {
		return nil, fmt.Errorf("store: read_events: %w", err)
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var (
			seq    int64
			typ    int
			ts     int64
			actor  string
			payload []byte
		)
		if err := rows.Scan(&seq, &typ, &ts, &payload, &actor); err != nil {
			return nil, fmt.Errorf("store: read_events scan: %w", err)
		}
		out = append(out, Event{
			Sid:       sid,
			Seq:       seq,
			Type:      EventType(typ),
			Timestamp: time.Unix(0, ts),
			Payload:   payload,
			Actor:     actor,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: read_events rows: %w", err)
	}
	return out, nil
}

// GetLastSeq 返回 sid 已分配的最大 seq；sid 不存在返回 ErrNotFound。
// 无事件时返回 -1（与 COALESCE(MAX(seq), -1) 语义一致）。
func (s *SQLiteStore) GetLastSeq(ctx context.Context, sid string) (int64, error) {
	var last int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq), -1) FROM events WHERE session_id=?`, sid,
	).Scan(&last)
	if err != nil {
		return 0, fmt.Errorf("store: get_last_seq: %w", err)
	}
	// 区分 "sid 不存在" 与 "sid 存在但无事件"。
	var exists int
	if err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM sessions WHERE id=?`, sid,
	).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("store: get_last_seq exists: %w", err)
	}
	return last, nil
}

// Project 派生 sid 的指定投影（v4 §A.5）。实现用 MemoryProjectionCache。
// 注意：这是进程级 cache；不同 Store 实例各自独立。
func (s *SQLiteStore) Project(ctx context.Context, sid, name string) (ProjectionState, error) {
	return s.projectCache.getOrCompute(sid, name, func() (ProjectionState, error) {
		// 校验 sid 存在；空 events 不应误判为 unknown session。
		var exists int
		if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE id=?`, sid).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrNotFound
			}
			return nil, fmt.Errorf("store: project lookup: %w", err)
		}
		events, err := s.ReadEvents(ctx, sid, 0)
		if err != nil {
			return nil, err
		}
		set := DefaultProjectorSet()
		// PhaseProjector 单独处理
		if name == "messages" {
			msgs := MessagesState{}
			for _, ev := range events {
				if err := set.Messages.Apply(ev, &msgs); err != nil {
					return nil, err
				}
			}
			return msgs, nil
		}
		if name == "usage" {
			usage := UsageState{}
			for _, ev := range events {
				if err := set.Usage.Apply(ev, &usage); err != nil {
					return nil, err
				}
			}
			return usage, nil
		}
		if name == "phase" {
			phase := PhaseState("")
			for _, ev := range events {
				if err := set.Phase.Apply(ev, &phase); err != nil {
					return nil, err
				}
			}
			return phase, nil
		}
		return nil, fmt.Errorf("store: project: unknown name %q", name)
	})
}

// projectCache 是 SQLiteStore 内部的 ProjectionCache 适配器。
// 避免重复实现 MemoryProjectionCache 的全部方法。
type sqliteProjectionCache struct {
	cache *MemoryProjectionCache
}

func (p *sqliteProjectionCache) getOrCompute(sid, name string, compute func() (ProjectionState, error)) (ProjectionState, error) {
	if s, ok := p.cache.Get(sid, name); ok {
		return s, nil
	}
	s, err := compute()
	if err != nil {
		return nil, err
	}
	p.cache.Put(sid, name, s)
	return s, nil
}
