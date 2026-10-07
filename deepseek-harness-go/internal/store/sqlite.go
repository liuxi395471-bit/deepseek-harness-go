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
	// lease 是 v5 P5-1 引入的会话写租约。Append / AppendEvent 入口
	// 通过它获取单写者语义；nil = NoopLease（兼容 v4 行为）。
	lease Lease
}

// SQLiteOption 配置 NewSQLiteStoreWithOptions 的可选参数。
type SQLiteOption func(*SQLiteStore)

// WithLease 注入自定义 Lease。nil = NoopLease（默认）。
func WithLease(l Lease) SQLiteOption {
	return func(s *SQLiteStore) { s.lease = l }
}

// NewSQLiteStore 打开（或创建）path 处的 SQLite 数据库并执行模式迁移。
// 允许使用路径 ":memory:"（测试用内存库）。v5 起内部默认装配 NoopLease；
// 如需 MemoryLease，请用 NewSQLiteStoreWithOptions。
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	return NewSQLiteStoreWithOptions(path)
}

// NewSQLiteStoreWithOptions 同 NewSQLiteStore，并应用一组选项。
func NewSQLiteStoreWithOptions(path string, opts ...SQLiteOption) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: open %q: %w", path, err)
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: schema: %w", err)
	}
	// v8.1 schema migration：sessions 表的 cache_read_tokens /
	// cache_write_tokens / reasoning_tokens 是在 schemaSQL 中新增的列；
	// CREATE TABLE IF NOT EXISTS 不会 ALTER 旧表 → 必须显式补列。
	if err := migrateSchemaV81(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: migrate v8.1: %w", err)
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
	s := &SQLiteStore{
		db:           db,
		projectCache: &sqliteProjectionCache{cache: NewMemoryProjectionCache()},
		lease:        NoopLease{},
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.lease == nil {
		s.lease = NoopLease{}
	}
	return s, nil
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
    total_tokens      INTEGER NOT NULL DEFAULT 0,
    -- v8.1: cache + reasoning 细分（DeepSeek V4 / OpenAI o1 报告）
    cache_read_tokens  INTEGER NOT NULL DEFAULT 0,
    cache_write_tokens INTEGER NOT NULL DEFAULT 0,
    reasoning_tokens   INTEGER NOT NULL DEFAULT 0
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

-- v8.1 P1：消息评分（dsh 原生 feedback good/bad）
CREATE TABLE IF NOT EXISTS message_ratings (
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    msg_seq    INTEGER NOT NULL,
    rating     INTEGER NOT NULL, -- +1 (good) / -1 (bad) / 0 (clear)
    comment    TEXT NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (session_id, msg_seq)
);
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
//
// v5 P5-1 起：入口处获取 sid 写租约；保证同 sid 的 Append + AppendEvent
// 不出现 seq 抢占错乱。Lease nil = NoopLease，向后兼容。
func (s *SQLiteStore) Append(ctx context.Context, id string, msg llm.Message) error {
	handle, err := s.lease.Acquire(ctx, id, "append-message")
	if err != nil {
		return err
	}
	defer handle.Release()
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
		cr, cw, rsn                     int
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT created_at, updated_at, preview, rounds,
		        prompt_tokens, completion_tokens, total_tokens,
		        cache_read_tokens, cache_write_tokens, reasoning_tokens
		 FROM sessions WHERE id=?`, id,
	).Scan(&createdAt, &updatedAt, &preview, &rounds, &pt, &ct, &tt, &cr, &cw, &rsn)
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
		UsageTotal: llm.Usage{
			PromptTokens:     pt,
			CompletionTokens: ct,
			TotalTokens:      tt,
			CacheReadTokens:  cr,
			CacheWriteTokens: cw,
			ReasoningTokens:  rsn,
		},
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

// UpdateUsage 将 delta 加到 UsageTotal（含 cache / reasoning 细分）。
func (s *SQLiteStore) UpdateUsage(ctx context.Context, id string, delta llm.Usage) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE sessions
		 SET prompt_tokens = prompt_tokens + ?,
		     completion_tokens = completion_tokens + ?,
		     total_tokens = total_tokens + ?,
		     cache_read_tokens = cache_read_tokens + ?,
		     cache_write_tokens = cache_write_tokens + ?,
		     reasoning_tokens = reasoning_tokens + ?,
		     updated_at = ?
		 WHERE id = ?`,
		delta.PromptTokens, delta.CompletionTokens, delta.TotalTokens,
		delta.CacheReadTokens, delta.CacheWriteTokens, delta.ReasoningTokens,
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

// DeleteSession 删除会话及全部 messages/events（依赖外键级联）。
//
// schema 已声明 messages / events 表对 sessions(id) 使用 ON DELETE
// CASCADE；删除 sessions 行即触发级联清理。
func (s *SQLiteStore) DeleteSession(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("store: delete_session: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	s.projectCache.cache.Invalidate(id)
	return nil
}

// EditMessage 把 sid 中 seq=msgSeq 的消息内容替换为 newContent（仅 user/system）。
//
// 校验 role 防止破坏 assistant 工具调用链；用 UPDATE WHERE 命中行数
// 区分 not-found vs not-editable（先 SELECT 拿 role）。
func (s *SQLiteStore) EditMessage(ctx context.Context, sid string, msgSeq int64, newContent string) error {
	handle, err := s.lease.Acquire(ctx, sid, "edit-message")
	if err != nil {
		return err
	}
	defer handle.Release()

	var role string
	err = s.db.QueryRowContext(ctx,
		`SELECT role FROM messages WHERE session_id=? AND seq=?`, sid, msgSeq,
	).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrMessageNotFound
	}
	if err != nil {
		return fmt.Errorf("store: edit_message lookup: %w", err)
	}
	if llm.Role(role) != llm.RoleUser && llm.Role(role) != llm.RoleSystem {
		return ErrMessageNotEditable
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE messages SET content=? WHERE session_id=? AND seq=?`,
		newContent, sid, msgSeq,
	); err != nil {
		return fmt.Errorf("store: edit_message update: %w", err)
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET updated_at=? WHERE id=?`, time.Now().UnixMilli(), sid,
	); err != nil {
		return fmt.Errorf("store: edit_message touch: %w", err)
	}
	s.projectCache.cache.Invalidate(sid)
	return nil
}

// DeleteMessage 删除 sid 中 seq=msgSeq 的消息（seq 保留空位）。
func (s *SQLiteStore) DeleteMessage(ctx context.Context, sid string, msgSeq int64) error {
	handle, err := s.lease.Acquire(ctx, sid, "delete-message")
	if err != nil {
		return err
	}
	defer handle.Release()

	res, err := s.db.ExecContext(ctx,
		`DELETE FROM messages WHERE session_id=? AND seq=?`, sid, msgSeq,
	)
	if err != nil {
		return fmt.Errorf("store: delete_message: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrMessageNotFound
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET updated_at=? WHERE id=?`, time.Now().UnixMilli(), sid,
	); err != nil {
		return fmt.Errorf("store: delete_message touch: %w", err)
	}
	s.projectCache.cache.Invalidate(sid)
	return nil
}

// SetMessageRating 写评分；rating==0 时清除。
func (s *SQLiteStore) SetMessageRating(ctx context.Context, sid string, msgSeq int64, rating int, comment string) error {
	// 验证 session 存在
	var x int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE id=?`, sid).Scan(&x); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("store: rating session check: %w", err)
	}
	// 验证 msg 存在（不能给空位评分）
	var y int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM messages WHERE session_id=? AND seq=?`, sid, msgSeq).Scan(&y); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrMessageNotFound
		}
		return fmt.Errorf("store: rating message check: %w", err)
	}
	if rating == 0 {
		_, err := s.db.ExecContext(ctx, `DELETE FROM message_ratings WHERE session_id=? AND msg_seq=?`, sid, msgSeq)
		if err != nil {
			return fmt.Errorf("store: rating clear: %w", err)
		}
		return nil
	}
	if rating != 1 && rating != -1 {
		return fmt.Errorf("store: invalid rating %d (must be -1, 0, or 1)", rating)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO message_ratings (session_id, msg_seq, rating, comment, updated_at)
         VALUES (?, ?, ?, ?, ?)
         ON CONFLICT (session_id, msg_seq) DO UPDATE SET
           rating=excluded.rating, comment=excluded.comment, updated_at=excluded.updated_at`,
		sid, msgSeq, rating, comment, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("store: rating upsert: %w", err)
	}
	return nil
}

// ListMessageRatings 返回 sid 全部评分（key = msgSeq）。
func (s *SQLiteStore) ListMessageRatings(ctx context.Context, sid string) (map[int64]MessageRating, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT msg_seq, rating, comment FROM message_ratings WHERE session_id=?`, sid)
	if err != nil {
		return nil, fmt.Errorf("store: rating list: %w", err)
	}
	defer rows.Close()
	out := make(map[int64]MessageRating)
	for rows.Next() {
		var seq, rating int64
		var comment string
		if err := rows.Scan(&seq, &rating, &comment); err != nil {
			return nil, fmt.Errorf("store: rating scan: %w", err)
		}
		out[seq] = MessageRating{Seq: seq, Rating: int(rating), Comment: comment}
	}
	return out, rows.Err()
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
//
// v5 P5-1 起：入口处获取 sid 写租约。
func (s *SQLiteStore) AppendEvent(ctx context.Context, sid string, ev Event) (int64, error) {
	handle, err := s.lease.Acquire(ctx, sid, "append-event")
	if err != nil {
		return 0, err
	}
	defer handle.Release()
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

// migrateSchemaV81 给旧的 sessions 表补 cache_read_tokens /
// cache_write_tokens / reasoning_tokens 列。CREATE TABLE IF NOT EXISTS
// 在表已存在时不会 ALTER → 必须手动探测并 ALTER。重复执行是幂等的。
//
// 设计要点：
//   - 用 PRAGMA table_info 探测列存在性；
//   - 缺哪列就 ALTER ADD COLUMN 哪列；default 0 保证旧行合法。
func migrateSchemaV81(db *sql.DB) error {
	rows, err := db.Query("PRAGMA table_info(sessions)")
	if err != nil {
		return fmt.Errorf("pragma table_info(sessions): %w", err)
	}
	defer rows.Close()
	have := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	// v8.1 新增列：cache_read_tokens / cache_write_tokens / reasoning_tokens
	adds := []struct {
		col  string
		def  string
		want bool
	}{
		{"cache_read_tokens", "ALTER TABLE sessions ADD COLUMN cache_read_tokens INTEGER NOT NULL DEFAULT 0", true},
		{"cache_write_tokens", "ALTER TABLE sessions ADD COLUMN cache_write_tokens INTEGER NOT NULL DEFAULT 0", true},
		{"reasoning_tokens", "ALTER TABLE sessions ADD COLUMN reasoning_tokens INTEGER NOT NULL DEFAULT 0", true},
	}
	for _, a := range adds {
		if have[a.col] {
			continue
		}
		if _, err := db.Exec(a.def); err != nil {
			return fmt.Errorf("add column %s: %w", a.col, err)
		}
	}
	return nil
}
