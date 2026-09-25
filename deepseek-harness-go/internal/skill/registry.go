// Package skill 实现 DESIGN-v3 §A：Markdown + YAML frontmatter 形式的
// skill 文件，经 mention 或 always-on 触发后注入 system prompt。
//
// 文件格式（§A.1）：`---` 包裹的 YAML frontmatter（name/description/
// trigger/always），frontmatter 之后的正文即 Body。
package skill

import (
	"fmt"
	"sort"
	"sync"
)

// Skill 是一个已解析的 skill 文件。
type Skill struct {
	Name        string `yaml:"name"        json:"name"`
	Description string `yaml:"description" json:"description"`
	Trigger     string `yaml:"trigger"     json:"trigger"` // mention 关键词；空表示仅 always 或显式注册
	Always      bool   `yaml:"always"      json:"always"`
	Body        string `yaml:"body"        json:"body"` // frontmatter body 字段（可选）；文件正文会追加在其后
}

// Registry 是线程安全的 name → Skill 集合。
type Registry struct {
	mu     sync.RWMutex
	skills map[string]Skill
	order  []string
}

// NewRegistry 返回空 Registry。
func NewRegistry() *Registry {
	return &Registry{skills: map[string]Skill{}}
}

// Register 添加 s。同名重复时后者覆盖前者（允许热加载覆盖）。
func (r *Registry) Register(s Skill) error {
	if s.Name == "" {
		return fmt.Errorf("skill: empty name")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.skills[s.Name]; !exists {
		r.order = append(r.order, s.Name)
	}
	r.skills[s.Name] = s
	return nil
}

// Resolve 按名取 skill。
func (r *Registry) Resolve(name string) (Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.skills[name]
	return s, ok
}

// List 按（首次注册的）插入顺序返回全部 skill。
func (r *Registry) List() []Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Skill, 0, len(r.order))
	for _, n := range r.order {
		out = append(out, r.skills[n])
	}
	return out
}

// Names 按字母序返回全部 skill 名，便于 /skills 展示。
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.order))
	for _, n := range r.order {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Len 返回已注册 skill 数量。
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.order)
}
