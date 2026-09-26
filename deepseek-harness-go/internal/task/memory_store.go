package task

import (
	"context"
	"sort"
	"sync"
)

// MemoryStore 是 Store 的进程内实现，用于测试。
//
// 线程安全：所有方法都通过 sync.RWMutex 串行化。
type MemoryStore struct {
	mu    sync.RWMutex
	tasks map[string]*Task
}

// NewMemoryStore 构造空内存存储。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{tasks: make(map[string]*Task)}
}

// Insert 写入新 Task。
func (s *MemoryStore) Insert(_ context.Context, t *Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[t.ID]; ok {
		// 不允许覆盖；返回 ErrNotFound 让调用方视作冲突。
		return ErrNotFound
	}
	cp := *t
	s.tasks[t.ID] = &cp
	return nil
}

// Update 覆盖现有 Task。
func (s *MemoryStore) Update(_ context.Context, t *Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[t.ID]; !ok {
		return ErrNotFound
	}
	cp := *t
	s.tasks[t.ID] = &cp
	return nil
}

// Get 返回 task 快照（拷贝）。
func (s *MemoryStore) Get(_ context.Context, id string) (*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *t
	return &cp, nil
}

// List 按 filter 返回 task 列表（拷贝）。
func (s *MemoryStore) List(_ context.Context, filter Filter) ([]*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*Task
	for _, t := range s.tasks {
		if filter.Owner != "" && t.Owner != filter.Owner {
			continue
		}
		if filter.SessionID != "" && t.SessionID != filter.SessionID {
			continue
		}
		if filter.Code != "" && t.Code != filter.Code {
			continue
		}
		if len(filter.States) > 0 {
			hit := false
			for _, st := range filter.States {
				if t.State == st {
					hit = true
					break
				}
			}
			if !hit {
				continue
			}
		}
		cp := *t
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	start := filter.Offset
	if start > len(out) {
		start = len(out)
	}
	end := start + limit
	if end > len(out) {
		end = len(out)
	}
	return out[start:end], nil
}

// Size 返回存储中的 task 数（测试用）。
func (s *MemoryStore) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.tasks)
}

// Reset 清空（测试用）。
func (s *MemoryStore) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = make(map[string]*Task)
}
