// Package storage 提供 KV 存储抽象（v6 P6-6）。
//
// 3 种实现：
//   - MemoryStorage : 进程内 map，适合单进程 / 测试
//   - FileStorage   : 每个 namespace 一个 JSON 文件（原子写），适合轻量持久化
//   - ChainedStorage: 多 backend 链式查找（写入最后一层，读取从前到后命中）
//
// 数据模型：
//
//	Get(ns, key)   -> (value []byte, found bool, err error)
//	Set(ns, key, value)
//	Delete(ns, key)
//	Has(ns, key)   -> bool
//	List(ns)       -> []string  // 全部 keys
//
// v6 用途：
//   - goal/plan/todo 的工作区缓存；
//   - workflow 变量快照；
//   - jobs / task 元数据索引。
package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// ErrNotFound 在键不存在时返回。
var ErrNotFound = errors.New("storage: not found")

// Storage 是 KV 抽象。
type Storage interface {
	Get(namespace, key string) ([]byte, bool, error)
	Set(namespace, key string, value []byte) error
	Delete(namespace, key string) error
	Has(namespace, key string) (bool, error)
	List(namespace string) ([]string, error)
	Namespaces() []string
}

// MemoryStorage 是进程内实现。
type MemoryStorage struct {
	mu   sync.RWMutex
	data map[string]map[string][]byte // ns -> key -> value
}

// NewMemoryStorage 构造空 MemoryStorage。
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{data: make(map[string]map[string][]byte)}
}

// Get 实现 Storage.Get。
func (m *MemoryStorage) Get(ns, key string) ([]byte, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	bucket, ok := m.data[ns]
	if !ok {
		return nil, false, nil
	}
	v, ok := bucket[key]
	if !ok {
		return nil, false, nil
	}
	out := make([]byte, len(v))
	copy(out, v)
	return out, true, nil
}

// Set 实现 Storage.Set。
func (m *MemoryStorage) Set(ns, key string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data[ns] == nil {
		m.data[ns] = make(map[string][]byte)
	}
	cp := make([]byte, len(value))
	copy(cp, value)
	m.data[ns][key] = cp
	return nil
}

// Delete 实现 Storage.Delete。
func (m *MemoryStorage) Delete(ns, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if bucket, ok := m.data[ns]; ok {
		delete(bucket, key)
		if len(bucket) == 0 {
			delete(m.data, ns)
		}
	}
	return nil
}

// Has 实现 Storage.Has。
func (m *MemoryStorage) Has(ns, key string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	bucket, ok := m.data[ns]
	if !ok {
		return false, nil
	}
	_, ok = bucket[key]
	return ok, nil
}

// List 实现 Storage.List（按字典序）。
func (m *MemoryStorage) List(ns string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	bucket, ok := m.data[ns]
	if !ok {
		return nil, nil
	}
	out := make([]string, 0, len(bucket))
	for k := range bucket {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

// Namespaces 返回全部 namespace 名。
func (m *MemoryStorage) Namespaces() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.data))
	for k := range m.data {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// fileBucket 是 FileStorage 内部 JSON 形态。
type fileBucket struct {
	Values map[string][]byte `json:"values"`
}

// FileStorage 是 JSON 文件实现，每个 namespace 一个文件。
//
// 行为：
//   - 启动时按需懒加载：第一次访问 ns 时读 ns.json；
//   - 写后 flush 整个 bucket 到 disk（先写临时文件再 rename，原子）。
type FileStorage struct {
	mu       sync.RWMutex
	root     string
	cache    map[string]*fileBucket // ns -> bucket
	dirty    map[string]bool
	loaded   map[string]bool
	loadedMu sync.Mutex
}

// NewFileStorage 在 root 下构造（root 不存在则创建）。
func NewFileStorage(root string) (*FileStorage, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &FileStorage{
		root:   root,
		cache:  make(map[string]*fileBucket),
		dirty:  make(map[string]bool),
		loaded: make(map[string]bool),
	}, nil
}

func (f *FileStorage) loadLocked(ns string) (*fileBucket, error) {
	if f.loaded[ns] {
		return f.cache[ns], nil
	}
	path := filepath.Join(f.root, ns+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			bucket := &fileBucket{Values: make(map[string][]byte)}
			f.cache[ns] = bucket
			f.loaded[ns] = true
			return bucket, nil
		}
		return nil, err
	}
	var bucket fileBucket
	if err := json.Unmarshal(b, &bucket); err != nil {
		return nil, err
	}
	if bucket.Values == nil {
		bucket.Values = make(map[string][]byte)
	}
	f.cache[ns] = &bucket
	f.loaded[ns] = true
	return &bucket, nil
}

func (f *FileStorage) flush(ns string) error {
	bucket, ok := f.cache[ns]
	if !ok {
		return nil
	}
	path := filepath.Join(f.root, ns+".json")
	tmp := path + ".tmp"
	b, err := json.MarshalIndent(bucket, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Get 实现 Storage.Get。
func (f *FileStorage) Get(ns, key string) ([]byte, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	bucket, err := f.loadLocked(ns)
	if err != nil {
		return nil, false, err
	}
	v, ok := bucket.Values[key]
	if !ok {
		return nil, false, nil
	}
	out := make([]byte, len(v))
	copy(out, v)
	return out, true, nil
}

// Set 实现 Storage.Set；写后刷盘。
func (f *FileStorage) Set(ns, key string, value []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	bucket, err := f.loadLocked(ns)
	if err != nil {
		return err
	}
	cp := make([]byte, len(value))
	copy(cp, value)
	bucket.Values[key] = cp
	f.dirty[ns] = true
	return f.flush(ns)
}

// Delete 实现 Storage.Delete。
func (f *FileStorage) Delete(ns, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	bucket, err := f.loadLocked(ns)
	if err != nil {
		return err
	}
	delete(bucket.Values, key)
	f.dirty[ns] = true
	if len(bucket.Values) == 0 {
		// 删除文件
		path := filepath.Join(f.root, ns+".json")
		_ = os.Remove(path)
	} else {
		return f.flush(ns)
	}
	return nil
}

// Has 实现 Storage.Has。
func (f *FileStorage) Has(ns, key string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	bucket, err := f.loadLocked(ns)
	if err != nil {
		return false, err
	}
	_, ok := bucket.Values[key]
	return ok, nil
}

// List 实现 Storage.List。
func (f *FileStorage) List(ns string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	bucket, err := f.loadLocked(ns)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(bucket.Values))
	for k := range bucket.Values {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

// Namespaces 扫描 root 下所有 *.json 文件。
func (f *FileStorage) Namespaces() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	entries, err := os.ReadDir(f.root)
	if err != nil {
		return nil
	}
	out := make([]string, 0)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if filepath.Ext(name) != ".json" {
			continue
		}
		out = append(out, name[:len(name)-len(".json")])
	}
	sort.Strings(out)
	return out
}

// ChainedStorage 链式查找：写入最后一层；读取从前到后命中。
//
// 用途：把 MemoryStorage 当 L1 缓存 + FileStorage 当 L2 持久层。
type ChainedStorage struct {
	backends []Storage
}

// NewChainedStorage 构造链式存储；backends 按优先级排序（前 = 优先读）。
func NewChainedStorage(backends ...Storage) *ChainedStorage {
	return &ChainedStorage{backends: backends}
}

// Get 实现 Storage.Get。
func (c *ChainedStorage) Get(ns, key string) ([]byte, bool, error) {
	for _, b := range c.backends {
		v, ok, err := b.Get(ns, key)
		if err != nil {
			return nil, false, err
		}
		if ok {
			return v, true, nil
		}
	}
	return nil, false, nil
}

// Set 写到最后一层（其余层缓存由调用方决定是否刷新）。
func (c *ChainedStorage) Set(ns, key string, value []byte) error {
	if len(c.backends) == 0 {
		return errors.New("storage: no backend")
	}
	return c.backends[len(c.backends)-1].Set(ns, key, value)
}

// Delete 从全部层删除。
func (c *ChainedStorage) Delete(ns, key string) error {
	var lastErr error
	for _, b := range c.backends {
		if err := b.Delete(ns, key); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// Has 实现 Storage.Has。
func (c *ChainedStorage) Has(ns, key string) (bool, error) {
	for _, b := range c.backends {
		ok, err := b.Has(ns, key)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

// List 合并所有层的 keys 并去重。
func (c *ChainedStorage) List(ns string) ([]string, error) {
	seen := make(map[string]bool)
	for _, b := range c.backends {
		keys, err := b.List(ns)
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			seen[k] = true
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

// Namespaces 合并所有层的 namespace。
func (c *ChainedStorage) Namespaces() []string {
	seen := make(map[string]bool)
	for _, b := range c.backends {
		for _, n := range b.Namespaces() {
			seen[n] = true
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// JSONGet / JSONSet 便利方法：直接存/取 JSON 类型。
//
// v6 用途：方便快捷地缓存结构化数据。
func JSONSet(s Storage, ns, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.Set(ns, key, b)
}

// JSONGet 把 ns/key 反序列化到 dst；找不到返回 ErrNotFound。
func JSONGet(s Storage, ns, key string, dst any) error {
	b, ok, err := s.Get(ns, key)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return json.Unmarshal(b, dst)
}
