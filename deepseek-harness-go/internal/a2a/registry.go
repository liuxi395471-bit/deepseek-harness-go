// Package a2a 提供 AgentTeam / Agent-to-Agent 编排（v7 P7-6）。
//
// 三层：
//   - registry : 注册 / 注销 agent capability（"code" / "chat" / ...）
//   - orchestrator : 接收复合任务 → 按 capability 路由 → 收集结果
//   - server : 把本地 agent 暴露给其他进程（同进程 v7；跨进程留 v7.1）
//
// 状态共享：跨 agent 通过 storage.Storage（v6 KV）写入 / 读取共享键。
package a2a

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// Agent 是单个 agent 的最小投影（任意能 handle Task 的对象）。
type Agent struct {
	Name       string
	Capability string // e.g. "code" / "chat" / "research"
	Handle     func(ctx context.Context, task Task) (Result, error)
}

// Task 是分配给 agent 的工作单元。
type Task struct {
	ID          string         `json:"id"`
	Capability  string         `json:"capability"`
	Input       string         `json:"input"`
	SharedState map[string]any `json:"shared_state,omitempty"`
}

// Result 是 agent 完成 task 的输出。
type Result struct {
	TaskID  string         `json:"task_id"`
	Agent   string         `json:"agent"`
	Output  string         `json:"output"`
	Updates map[string]any `json:"updates,omitempty"` // 写入 shared state
}

// Registry 持有 agent 注册表。
type Registry struct {
	mu     sync.RWMutex
	agents map[string][]*Agent // capability → agents
}

// NewRegistry 构造。
func NewRegistry() *Registry {
	return &Registry{agents: make(map[string][]*Agent)}
}

// Register 注册一个 agent 到 capability。
func (r *Registry) Register(a *Agent) error {
	if a == nil || a.Name == "" || a.Capability == "" {
		return errors.New("a2a: agent with name/capability required")
	}
	if a.Handle == nil {
		return errors.New("a2a: agent.Handle nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, exist := range r.agents[a.Capability] {
		if exist.Name == a.Name {
			return fmt.Errorf("a2a: %s/%s already registered", a.Capability, a.Name)
		}
	}
	r.agents[a.Capability] = append(r.agents[a.Capability], a)
	return nil
}

// Unregister 移除一个 agent。
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for cap, list := range r.agents {
		for i, a := range list {
			if a.Name == name {
				r.agents[cap] = append(list[:i], list[i+1:]...)
				return
			}
		}
	}
}

// List 返回全部注册表条目（按 capability 排序）。
func (r *Registry) List() []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	caps := make([]string, 0, len(r.agents))
	for k := range r.agents {
		caps = append(caps, k)
	}
	sort.Strings(caps)
	var out []Entry
	for _, c := range caps {
		for _, a := range r.agents[c] {
			out = append(out, Entry{Name: a.Name, Capability: a.Capability})
		}
	}
	return out
}

// Lookup 返回第一个匹配 capability 的 agent。
func (r *Registry) Lookup(capability string) (*Agent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list, ok := r.agents[capability]
	if !ok || len(list) == 0 {
		return nil, false
	}
	return list[0], true
}

// Entry 是 (name, capability) 二元组。
type Entry struct {
	Name       string `json:"name"`
	Capability string `json:"capability"`
}
