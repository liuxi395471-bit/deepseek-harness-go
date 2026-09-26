// Package tools 包含 dsh 内置的工具；v6 P6-6 提供 KV 工具。
//
//   - kv_set    : 写 KV
//   - kv_get    : 读 KV
//   - kv_delete : 删 KV
//   - kv_list   : 列 namespace 下全部 keys
//
// 工具共享同一个 storage.Storage（main 启动时通过 SetKV 注入）。
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"deepseek-harness-go/internal/storage"
	"deepseek-harness-go/internal/tool"
)

// kvHolder 持有 storage.Storage（v6 P6-6）。
type kvHolder struct {
	mu sync.RWMutex
	s  storage.Storage
}

func (h *kvHolder) Get() storage.Storage {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.s
}

func (h *kvHolder) Set(s storage.Storage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.s = s
}

// SetKV 注入 storage.Storage。
func SetKV(s storage.Storage) {
	globalKV.Set(s)
}

var globalKV = &kvHolder{}

// KVSetTool 写 KV。
type KVSetTool struct{}

func NewKVSetTool() *KVSetTool { return &KVSetTool{} }
func (*KVSetTool) Name() string { return "kv_set" }
func (*KVSetTool) Description() string {
	return "写 KV；value 是字符串（base64 用于二进制，但 v6 简化只支持字符串）。"
}
func (*KVSetTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"namespace": map[string]any{"type": "string"},
			"key":       map[string]any{"type": "string"},
			"value":     map[string]any{"type": "string"},
		},
		"required": []string{"namespace", "key", "value"},
	}
}

func (*KVSetTool) Execute(_ context.Context, raw json.RawMessage) (tool.Result, error) {
	var a struct {
		Namespace string `json:"namespace"`
		Key       string `json:"key"`
		Value     string `json:"value"`
	}
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return tool.Err("invalid args: " + err.Error()), nil
	}
	if a.Namespace == "" || a.Key == "" {
		return tool.Err("namespace and key required"), nil
	}
	s := globalKV.Get()
	if s == nil {
		return tool.Err("kv storage unavailable"), nil
	}
	if err := s.Set(a.Namespace, a.Key, []byte(a.Value)); err != nil {
		return tool.Err(fmt.Sprintf("set: %s", err.Error())), nil
	}
	return tool.Ok(`{"ok":true}`), nil
}

// KVGetTool 读 KV。
type KVGetTool struct{}

func NewKVGetTool() *KVGetTool { return &KVGetTool{} }
func (*KVGetTool) Name() string { return "kv_get" }
func (*KVGetTool) Description() string {
	return "读 KV；找到返回 value，找不到返回 not_found。"
}
func (*KVGetTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"namespace": map[string]any{"type": "string"},
			"key":       map[string]any{"type": "string"},
		},
		"required": []string{"namespace", "key"},
	}
}

func (*KVGetTool) Execute(_ context.Context, raw json.RawMessage) (tool.Result, error) {
	var a struct {
		Namespace string `json:"namespace"`
		Key       string `json:"key"`
	}
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return tool.Err("invalid args: " + err.Error()), nil
	}
	s := globalKV.Get()
	if s == nil {
		return tool.Err("kv storage unavailable"), nil
	}
	v, ok, err := s.Get(a.Namespace, a.Key)
	if err != nil {
		return tool.Err(fmt.Sprintf("get: %s", err.Error())), nil
	}
	if !ok {
		return tool.Ok(`{"found":false}`), nil
	}
	b, _ := json.Marshal(map[string]any{"found": true, "value": string(v)})
	return tool.Ok(string(b)), nil
}

// KVDeleteTool 删 KV。
type KVDeleteTool struct{}

func NewKVDeleteTool() *KVDeleteTool { return &KVDeleteTool{} }
func (*KVDeleteTool) Name() string { return "kv_delete" }
func (*KVDeleteTool) Description() string {
	return "删 KV；幂等。"
}
func (*KVDeleteTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"namespace": map[string]any{"type": "string"},
			"key":       map[string]any{"type": "string"},
		},
		"required": []string{"namespace", "key"},
	}
}

func (*KVDeleteTool) Execute(_ context.Context, raw json.RawMessage) (tool.Result, error) {
	var a struct {
		Namespace string `json:"namespace"`
		Key       string `json:"key"`
	}
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return tool.Err("invalid args: " + err.Error()), nil
	}
	s := globalKV.Get()
	if s == nil {
		return tool.Err("kv storage unavailable"), nil
	}
	if err := s.Delete(a.Namespace, a.Key); err != nil {
		return tool.Err(fmt.Sprintf("delete: %s", err.Error())), nil
	}
	return tool.Ok(`{"ok":true}`), nil
}

// KVListTool 列 namespace 下全部 keys。
type KVListTool struct{}

func NewKVListTool() *KVListTool { return &KVListTool{} }
func (*KVListTool) Name() string { return "kv_list" }
func (*KVListTool) Description() string {
	return "列 namespace 下全部 keys。"
}
func (*KVListTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"namespace": map[string]any{"type": "string"},
		},
		"required": []string{"namespace"},
	}
}

func (*KVListTool) Execute(_ context.Context, raw json.RawMessage) (tool.Result, error) {
	var a struct {
		Namespace string `json:"namespace"`
	}
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return tool.Err("invalid args: " + err.Error()), nil
	}
	s := globalKV.Get()
	if s == nil {
		return tool.Err("kv storage unavailable"), nil
	}
	keys, err := s.List(a.Namespace)
	if err != nil {
		return tool.Err(fmt.Sprintf("list: %s", err.Error())), nil
	}
	if keys == nil {
		keys = []string{}
	}
	b, _ := json.Marshal(map[string]any{"keys": keys, "count": len(keys)})
	return tool.Ok(string(b)), nil
}
