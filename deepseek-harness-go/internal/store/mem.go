package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
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
		id:        id,
		createdAt: now,
		updatedAt: now,
	}
	return Session{
		ID:        id,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
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
