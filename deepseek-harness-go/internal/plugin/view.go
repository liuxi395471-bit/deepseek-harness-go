// Package plugin — 库存视图（v4 §C）。
//
// view.go 提供 Inventory 接口聚合本地 Registry 与远端 gRPC 插件
// hosts 的工具清单。Inventory 是 Gateway SSE 暴露 tools.list /
// plugins.list 两个 source 的数据源。
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"deepseek-harness-go/internal/tool"
)

// ToolSpecView 是从 tool.Tool 派生的对外暴露的元数据。
//
// Parameters 是 JSON Schema（tool.Spec 已经输出结构）；调用方应
// 把它视为不透明对象，不假设内部结构。
type ToolSpecView struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Risk        string `json:"risk"` // "low" | "medium" | "high"
	Parameters  any    `json:"parameters"`
}

// PluginEntry 是一个插件（或本地工具组）的对外清单条目。
//
// Kind 区分来源；Source 是原始路径或 gRPC 地址。Healthy 由最近一次
// probe 决定（gRPC plugin 用 dial 验证；本地始终 true）。
type PluginEntry struct {
	Name        string         `json:"name"`
	Kind        string         `json:"kind"`   // "local" | "grpc"
	Source      string         `json:"source"` // 内置路径 / grpc addr
	Version     string         `json:"version,omitempty"`
	Healthy     bool           `json:"healthy"`
	LastError   string         `json:"last_error,omitempty"`
	Permissions []string       `json:"permissions"`
	Tools       []ToolSpecView `json:"tools"`
	ProbedAt    time.Time      `json:"probed_at"`
}

// Inventory 聚合多个 PluginEntry 的可查询视图。
//
// List 在调用时返回快照；调用方并发安全（读侧）。Health 触发实时探
// 测（如 gRPC dial）。
type Inventory interface {
	List(ctx context.Context) ([]PluginEntry, error)
	Get(ctx context.Context, name string) (PluginEntry, bool)
	Health(ctx context.Context, name string) (bool, error)
}

// --- LocalInventory：从 tool.Registry 派生 ---

// LocalInventory 从内部 tool.Registry 派生单个 PluginEntry。
//
// Name 默认 "local"；调用方可修改以反映部署命名。
type LocalInventory struct {
	Name    string
	Version string
	Reg     *tool.Registry
}

// List 返回单个 PluginEntry（kind=local）。LocalRegistry 视为
// 始终 healthy。
func (l *LocalInventory) List(ctx context.Context) ([]PluginEntry, error) {
	if l.Reg == nil {
		return nil, nil
	}
	tools := l.Reg.Specs()
	views := make([]ToolSpecView, 0, len(tools))
	for _, ts := range tools {
		views = append(views, ToolSpecView{
			Name:        ts.Function.Name,
			Description: ts.Function.Description,
			Risk:        riskFor(ts.Function.Name),
			Parameters:  ts.Function.Parameters,
		})
	}
	return []PluginEntry{
		{
			Name:        l.pluginName(),
			Kind:        "local",
			Source:      "internal://registry",
			Version:     l.Version,
			Healthy:     true,
			Permissions: []string{},
			Tools:       views,
			ProbedAt:    time.Now().UTC(),
		},
	}, nil
}

// Get 查找单个工具名对应的 PluginEntry 与 ToolSpecView。
func (l *LocalInventory) Get(ctx context.Context, name string) (PluginEntry, bool) {
	entries, err := l.List(ctx)
	if err != nil || len(entries) == 0 {
		return PluginEntry{}, false
	}
	for _, e := range entries {
		for _, t := range e.Tools {
			if t.Name == name {
				return e, true
			}
		}
	}
	return PluginEntry{}, false
}

// Health 返回 true（本地工具始终可用）；调用方不应依赖此信号。
func (l *LocalInventory) Health(ctx context.Context, name string) (bool, error) {
	if _, ok := l.Get(ctx, name); !ok {
		return false, fmt.Errorf("local inventory: tool %q not found", name)
	}
	return true, nil
}

func (l *LocalInventory) pluginName() string {
	if l.Name == "" {
		return "local"
	}
	return l.Name
}

// riskFor 根据工具名启发式返回风险等级。
//
// v4 §C.5：本地工具可不实现 Risk()；v3 默认按名字判断，已知高危工具
// 显式映射到 "high"。其他工具返回 "low"。
func riskFor(name string) string {
	switch name {
	case "shell", "exec", "bash", "sh":
		return "high"
	case "fs.write", "write_file", "edit_file":
		return "medium"
	case "net.fetch", "http":
		return "medium"
	}
	return "low"
}

// --- Combined：聚合 Local + GRPC ---

// Combined 把多个 Inventory 聚合为一个；同名 plugin 时后者覆盖前者
//（GRPC 通常后注册，因此 GRPC 优先于 Local 同名工具）。
type Combined struct {
	mu        sync.RWMutex
	inventories []Inventory
}

// NewCombined 构造空聚合。
func NewCombined() *Combined {
	return &Combined{}
}

// Add 追加一个 inventory。
func (c *Combined) Add(inv Inventory) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inventories = append(c.inventories, inv)
}

// List 遍历所有 inventory；同名 plugin 时后者覆盖前者。
func (c *Combined) List(ctx context.Context) ([]PluginEntry, error) {
	c.mu.RLock()
	srcs := append([]Inventory(nil), c.inventories...)
	c.mu.RUnlock()

	byName := make(map[string]PluginEntry)
	for _, inv := range srcs {
		entries, err := inv.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("combined inventory: %w", err)
		}
		for _, e := range entries {
			byName[e.Name] = e
		}
	}
	out := make([]PluginEntry, 0, len(byName))
	for _, e := range byName {
		out = append(out, e)
	}
	return out, nil
}

// Get 按 plugin name 查找。
func (c *Combined) Get(ctx context.Context, name string) (PluginEntry, bool) {
	all, err := c.List(ctx)
	if err != nil {
		return PluginEntry{}, false
	}
	for _, e := range all {
		if e.Name == name {
			return e, true
		}
	}
	return PluginEntry{}, false
}

// Health 把请求转发给第一个能识别该 name 的 inventory。
func (c *Combined) Health(ctx context.Context, name string) (bool, error) {
	c.mu.RLock()
	srcs := append([]Inventory(nil), c.inventories...)
	c.mu.RUnlock()
	for _, inv := range srcs {
		if _, ok := inv.Get(ctx, name); ok {
			return inv.Health(ctx, name)
		}
	}
	return false, fmt.Errorf("combined inventory: %q not found", name)
}

// --- grpcClientList 公共 helper ---

// clientList 是把 []*Client 摊平为 ToolSpecView 列表。
func clientList(clients []*Client) []ToolSpecView {
	out := []ToolSpecView{}
	for _, c := range clients {
		for _, spec := range c.Specs() {
			params := json.RawMessage(spec.GetParameters())
			out = append(out, ToolSpecView{
				Name:        spec.GetName(),
				Description: spec.GetDescription(),
				Risk:        "low",
				Parameters:  rawOrEmpty(params),
			})
		}
	}
	return out
}

func rawOrEmpty(b json.RawMessage) any {
	if len(b) == 0 {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return map[string]any{"type": "object", "raw": string(b)}
	}
	return v
}