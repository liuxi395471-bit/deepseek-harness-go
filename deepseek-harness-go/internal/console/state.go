package console

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// stateStore 是 UI 偏好的 KV 存储。
//
// 落盘格式：{"key1":"v1","key2":"v2"} 的 JSON 文件；写时 atomic rename。
// v8.0 不引入 SQLite，单文件即可。
type stateStore struct {
	mu   sync.RWMutex
	path string
	data map[string]string
}

// newStateStore 构造；path 是 JSON 文件路径，nil 时使用纯内存。
func newStateStore(path string) (*stateStore, error) {
	s := &stateStore{path: path, data: make(map[string]string)}
	if path == "" {
		return s, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// NewStateStore 导出版本，供 cmd/dsh 装配用。
func NewStateStore(path string) (*stateStore, error) { return newStateStore(path) }

func (s *stateStore) load() error {
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(b) == 0 {
		return nil
	}
	return json.Unmarshal(b, &s.data)
}

func (s *stateStore) flush() error {
	if s.path == "" {
		return nil
	}
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Get 返回 key 对应 value 与是否存在标志。
func (s *stateStore) Get(_ context.Context, key string) (string, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[key]
	return v, ok, nil
}

// Set 写入并落盘。
func (s *stateStore) Set(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
	return s.flush()
}

// All 返回 key→value 拷贝（用于调试）。
func (s *stateStore) All() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.data))
	for k, v := range s.data {
		out[k] = v
	}
	return out
}
