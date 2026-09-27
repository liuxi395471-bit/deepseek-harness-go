// Package installer 提供插件安装工程化能力（v7 P7-1）。
//
// 职责：
//   - Scanner：扫描 installRoot 下插件（JAR / package.json / plugin.json）；
//   - Manifest：磁盘文件索引 → 内存条目；
//   - Status：5 种状态 + JSON 持久化；
//   - Reconciler：启动时对账（manifest vs 实际加载）；
//   - CLI：dsh plugin {ls|install|uninstall|status}（v7.0 仅实现 ls / status）。
//
// 与 v4 plugin.Inventory / plugin.Host 解耦：
// installer 只管"哪些 plugin 在磁盘上 + 什么状态"，
// 真正的加载仍由 plugin.Host 触发（v4 + v7-2 Node Bridge）。
package installer

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Format 是插件的物理格式。
type Format int

const (
	FormatUnknown Format = iota
	FormatJAR         // Java JAR（与 v4 兼容）
	FormatNode        // package.json（v7-2 Node Bridge）
	FormatNative      // plugin.json（Go 原生 plugin）
)

func (f Format) String() string {
	switch f {
	case FormatJAR:
		return "jar"
	case FormatNode:
		return "node"
	case FormatNative:
		return "native"
	default:
		return "unknown"
	}
}

// Entry 是扫描出的一个插件条目。
type Entry struct {
	Name    string `json:"name"`
	Format  Format `json:"format"`
	Path    string `json:"path"`
	Version string `json:"version,omitempty"`
	Main    string `json:"main,omitempty"`
	// DetectInfo 是扫描时的额外元数据（如 JAR 大小 / package.json scripts）。
	DetectInfo map[string]string `json:"detect_info,omitempty"`
}

// Scanner 扫描 installRoot。
type Scanner struct{}

// NewScanner 构造。
func NewScanner() *Scanner { return &Scanner{} }

// Scan 递归扫描 root 下每个一级子目录；识别 *.jar / package.json / plugin.json。
//
// 行为：
//   - 跳过隐藏目录（"." 开头）；
//   - 每个一级子目录最多返回一个 Entry；
//   - 同一子目录中多种格式共存时按优先级 native > node > jar。
func (s *Scanner) Scan(root string) ([]*Entry, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []*Entry
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		sub := filepath.Join(root, e.Name())
		entry, ok := scanDir(sub)
		if !ok {
			continue
		}
		entry.Name = e.Name()
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func scanDir(dir string) (*Entry, bool) {
	// native 优先：plugin.json
	for _, cand := range []string{"plugin.json"} {
		p := filepath.Join(dir, cand)
		if b, err := os.ReadFile(p); err == nil {
			var meta struct {
				Version string            `json:"version"`
				Main    string            `json:"main"`
				Detect  map[string]string `json:"detect"`
			}
			_ = json.Unmarshal(b, &meta)
			return &Entry{
				Format:     FormatNative,
				Path:       p,
				Version:    meta.Version,
				Main:       meta.Main,
				DetectInfo: meta.Detect,
			}, true
		}
	}
	// node: package.json
	pj := filepath.Join(dir, "package.json")
	if b, err := os.ReadFile(pj); err == nil {
		var pkg struct {
			Version    string            `json:"version"`
			Main       string            `json:"main"`
			DetectInfo map[string]string `json:"-"`
		}
		_ = json.Unmarshal(b, &pkg)
		det := map[string]string{"type": "node"}
		return &Entry{
			Format:     FormatNode,
			Path:       pj,
			Version:    pkg.Version,
			Main:       pkg.Main,
			DetectInfo: det,
		}, true
	}
	// jar: *.jar
	items, err := os.ReadDir(dir)
	if err == nil {
		for _, it := range items {
			if it.IsDir() {
				continue
			}
			if strings.HasSuffix(it.Name(), ".jar") {
				p := filepath.Join(dir, it.Name())
				st, _ := os.Stat(p)
				det := map[string]string{}
				if st != nil {
					det["size"] = itSize(st.Size())
				}
				return &Entry{
					Format:     FormatJAR,
					Path:       p,
					DetectInfo: det,
				}, true
			}
		}
	}
	return nil, false
}

func itSize(n int64) string {
	const k = 1024
	if n < k {
		return "1K"
	}
	if n < k*k {
		return "1M"
	}
	return "1G"
}

// --- Status ---

// State 是插件运行时状态。
type State string

const (
	StateDiscovered  State = "discovered"  // 磁盘上有但未尝试加载
	StateLoaded      State = "loaded"      // 加载成功
	StateFailed      State = "failed"      // 加载失败（panic / version 不匹配）
	StateDisabled    State = "disabled"    // 用户标记为禁用
	StateUninstalled State = "uninstalled" // manifest 标记已卸载（磁盘可保留以便恢复）
)

// Status 是单插件的状态条目。
type Status struct {
	Name    string `json:"name"`
	State   State  `json:"state"`
	Message string `json:"message,omitempty"`
	At      string `json:"at"`
}

// StatusStore 是状态持久化（JSON 文件）。
type StatusStore struct {
	mu      sync.RWMutex
	path    string
	statuses map[string]*Status
}

// NewStatusStore 构造；path 是 plugin_status.json。
func NewStatusStore(path string) (*StatusStore, error) {
	s := &StatusStore{path: path, statuses: make(map[string]*Status)}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *StatusStore) load() error {
	b, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var m map[string]*Status
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	s.statuses = m
	return nil
}

// Get 取一条状态。
func (s *StatusStore) Get(name string) (*Status, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.statuses[name]
	return v, ok
}

// Set 设一条状态并落盘。
func (s *StatusStore) Set(st *Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st.Name == "" {
		return errors.New("installer: status without name")
	}
	cp := *st
	s.statuses[st.Name] = &cp
	return s.flush()
}

func (s *StatusStore) flush() error {
	b, err := json.MarshalIndent(s.statuses, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// All 返回全部状态（按 name 排序）。
func (s *StatusStore) All() []*Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Status, 0, len(s.statuses))
	for _, v := range s.statuses {
		cp := *v
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// --- Reconciler ---

// Report 是对账报告。
type Report struct {
	Orphans []*Entry  // status 有但磁盘无（应标 uninstalled）
	Drift   []*Entry  // 磁盘有但 status 无（应标 discovered）
	Healthy []string  // 双方都有且状态 = loaded
}

// Reconcile 把磁盘 entries vs 持久化 status 对账。
func Reconcile(entries []*Entry, store *StatusStore) *Report {
	r := &Report{}
	seen := make(map[string]bool)
	for _, e := range entries {
		seen[e.Name] = true
		st, ok := store.Get(e.Name)
		if !ok {
			r.Drift = append(r.Drift, e)
			continue
		}
		if st.State == StateLoaded {
			r.Healthy = append(r.Healthy, e.Name)
		}
	}
	// status 有但 disk 无
	for _, st := range store.All() {
		if !seen[st.Name] {
			r.Orphans = append(r.Orphans, &Entry{Name: st.Name})
		}
	}
	sort.Slice(r.Drift, func(i, j int) bool { return r.Drift[i].Name < r.Drift[j].Name })
	return r
}
