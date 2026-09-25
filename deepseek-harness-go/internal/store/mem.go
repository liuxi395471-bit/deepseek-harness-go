package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"deepseek-harness-go/internal/llm"
)

// MapStore 是 Store 的内存实现。适用于测试，以及无需持久化的
// --no-session REPL 模式。
//
// 并发：外层互斥锁保护 sessions map；单个 id 上的所有操作都通过此
// 互斥锁串行化，因此每会话加锁是隐式的。
type MapStore struct {
	mu       sync.RWMutex
	sessions map[string]*mapSession
	closed   bool
}

type mapSession struct {
	id         string
	createdAt  time.Time
	updatedAt  time.Time
	rounds     int
	preview    string
	messages   []llm.Message
	usageTotal llm.Usage
	// v4 §A：events 流。每条事件按 seq 单调递增写入。
	events []Event
	// lastSeq 是 events 流的最后分配 seq（-1 表示无事件）。
	lastSeq int64
	// projectCache 是 session 内的 ProjectionCache 视图。
	projectCache *MemoryProjectionCache
}

// NewMapStore 构造一个空的内存存储。
func NewMapStore() *MapStore {
	return &MapStore{sessions: make(map[string]*mapSession)}
}

// Begin 分配新的 session id 并插入空行。
func (s *MapStore) Begin(ctx context.Context) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Session{}, errors.New("store: closed")
	}
	id, err := newID()
	if err != nil {
		return Session{}, err
	}
	now := time.Now()
	s.sessions[id] = &mapSession{
		id:           id,
		createdAt:    now,
		updatedAt:    now,
		lastSeq:      -1,
		projectCache: NewMemoryProjectionCache(),
	}
	return Session{
		ID:        id,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// AppendEvent 写入事件到内存 session。返回分配的 seq。
func (s *MapStore) AppendEvent(ctx context.Context, sid string, ev Event) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, errors.New("store: closed")
	}
	sess, ok := s.sessions[sid]
	if !ok {
		return 0, ErrNotFound
	}
	sess.lastSeq++
	ev.Sid = sid
	ev.Seq = sess.lastSeq
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now()
	}
	// 复制 payload，避免调用方修改影响历史。
	pl := make([]byte, len(ev.Payload))
	copy(pl, ev.Payload)
	ev.Payload = pl
	sess.events = append(sess.events, ev)
	sess.updatedAt = time.Now()
	// 投影缓存失效：未来 Project 应基于新事件重放。
	if sess.projectCache != nil {
		sess.projectCache.Invalidate(sid)
	}
	return sess.lastSeq, nil
}

// ReadEvents 返回 sid 从 from 之后的事件序列。from=0 表示从头开始。
func (s *MapStore) ReadEvents(ctx context.Context, sid string, from int64) ([]Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("store: closed")
	}
	sess, ok := s.sessions[sid]
	if !ok {
		return nil, ErrNotFound
	}
	out := make([]Event, 0, len(sess.events))
	for _, ev := range sess.events {
		if ev.Seq >= from {
			out = append(out, ev)
		}
	}
	return out, nil
}

// GetLastSeq 返回 sid 已分配的最大 seq；无事件返回 -1；sid 不存在返回 ErrNotFound。
func (s *MapStore) GetLastSeq(ctx context.Context, sid string) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return 0, errors.New("store: closed")
	}
	sess, ok := s.sessions[sid]
	if !ok {
		return 0, ErrNotFound
	}
	return sess.lastSeq, nil
}

// Project 派生 sid 的指定投影。命中 cache 直接返回；未命中从 events 重放。
func (s *MapStore) Project(ctx context.Context, sid, name string) (ProjectionState, error) {
	s.mu.RLock()
	sess, ok := s.sessions[sid]
	s.mu.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}
	if sess.projectCache == nil {
		// 兼容旧 Build（Begin 之前的状态）——补建 cache。
		s.mu.Lock()
		sess.projectCache = NewMemoryProjectionCache()
		s.mu.Unlock()
	}
	if cached, ok := sess.projectCache.Get(sid, name); ok {
		return cached, nil
	}
	events := append([]Event{}, sess.events...)
	set := DefaultProjectorSet()
	var state ProjectionState
	switch name {
	case "messages":
		msgs := MessagesState{}
		for _, ev := range events {
			if err := set.Messages.Apply(ev, &msgs); err != nil {
				return nil, err
			}
		}
		state = msgs
	case "usage":
		u := UsageState{}
		for _, ev := range events {
			if err := set.Usage.Apply(ev, &u); err != nil {
				return nil, err
			}
		}
		state = u
	case "phase":
		p := PhaseState("")
		for _, ev := range events {
			if err := set.Phase.Apply(ev, &p); err != nil {
				return nil, err
			}
		}
		state = p
	default:
		return nil, fmt.Errorf("store: project: unknown name %q", name)
	}
	sess.projectCache.Put(sid, name, state)
	return state, nil
}

// Append 将 msg 添加到会话。如果这是第一条用户消息（role=user 且
// preview 为空），则用 Content 截断到 previewLen 个 rune 填充 preview。
func (s *MapStore) Append(ctx context.Context, id string, msg llm.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("store: closed")
	}
	sess, ok := s.sessions[id]
	if !ok {
		return ErrNotFound
	}
	sess.messages = append(sess.messages, msg)
	if msg.Role == llm.RoleUser && sess.preview == "" {
		sess.preview = truncatePreview(msg.Content)
	}
	if msg.Role == llm.RoleAssistant {
		sess.rounds++
	}
	sess.updatedAt = time.Now()
	return nil
}

// Load 返回完整的会话记录。
func (s *MapStore) Load(ctx context.Context, id string) (Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return Session{}, errors.New("store: closed")
	}
	sess, ok := s.sessions[id]
	if !ok {
		return Session{}, ErrNotFound
	}
	return sessionFromMap(sess), nil
}

// List 返回从 offset 开始的至多 limit 个会话，按 UpdatedAt 降序排列。
// 每个返回的 Session 只带元数据（不含消息——完整历史请用 Load）。
func (s *MapStore) List(ctx context.Context, limit, offset int) ([]Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("store: closed")
	}
	all := make([]*mapSession, 0, len(s.sessions))
	for _, sess := range s.sessions {
		all = append(all, sess)
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].updatedAt.After(all[j].updatedAt)
	})
	if offset > len(all) {
		offset = len(all)
	}
	end := len(all)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	out := make([]Session, 0, end-offset)
	for _, sess := range all[offset:end] {
		out = append(out, sessionFromMap(sess))
	}
	return out, nil
}

// UpdateUsage 将 delta 加到 UsageTotal。
func (s *MapStore) UpdateUsage(ctx context.Context, id string, delta llm.Usage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("store: closed")
	}
	sess, ok := s.sessions[id]
	if !ok {
		return ErrNotFound
	}
	sess.usageTotal.PromptTokens += delta.PromptTokens
	sess.usageTotal.CompletionTokens += delta.CompletionTokens
	sess.usageTotal.TotalTokens += delta.TotalTokens
	sess.updatedAt = time.Now()
	return nil
}

// Close 将存储标记为已关闭。后续操作返回错误。
func (s *MapStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// sessionFromMap 将 mapSession 投影为 Session 值。Messages 切片会被
// 复制，调用方无法改动内部缓冲。
func sessionFromMap(s *mapSession) Session {
	msgs := make([]llm.Message, len(s.messages))
	copy(msgs, s.messages)
	return Session{
		ID:         s.id,
		CreatedAt:  s.createdAt,
		UpdatedAt:  s.updatedAt,
		Rounds:     s.rounds,
		Preview:    s.preview,
		Messages:   msgs,
		UsageTotal: s.usageTotal,
	}
}

// newID 返回 16 字节随机数的十六进制字符串（32 个字符）。
func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
