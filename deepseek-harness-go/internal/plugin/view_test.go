package plugin

import (
	"context"
	"encoding/json"
	"testing"

	"deepseek-harness-go/internal/tool"
)

// stubTool 是用于 Inventory 测试的最小 tool.Tool 实现。
type stubTool struct {
	name, desc string
}

func (s *stubTool) Name() string        { return s.name }
func (s *stubTool) Description() string { return s.desc }
func (s *stubTool) Parameters() any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (s *stubTool) Execute(ctx context.Context, args json.RawMessage) (tool.Result, error) {
	return tool.Result{Content: "ok"}, nil
}

// T3.4.1 LocalInventory.List 含全部已注册工具
func TestLocalInventory_List(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(&stubTool{name: "shell", desc: "shell"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(&stubTool{name: "echo", desc: "echo"}); err != nil {
		t.Fatal(err)
	}
	inv := &LocalInventory{Name: "builtin", Reg: reg}
	entries, err := inv.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.Name != "builtin" {
		t.Errorf("Name = %q", e.Name)
	}
	if e.Kind != "local" {
		t.Errorf("Kind = %q", e.Kind)
	}
	if !e.Healthy {
		t.Error("Healthy should be true")
	}
	names := []string{}
	for _, tt := range e.Tools {
		names = append(names, tt.Name)
	}
	if len(names) != 2 || names[0] != "shell" || names[1] != "echo" {
		t.Errorf("tools = %v, want [shell echo]", names)
	}
}

// T3.4.2 LocalInventory risk 分级
func TestLocalInventory_Risk(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(&stubTool{name: "shell", desc: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(&stubTool{name: "echo", desc: "y"}); err != nil {
		t.Fatal(err)
	}
	inv := &LocalInventory{Reg: reg}
	entries, _ := inv.List(context.Background())
	got := map[string]string{}
	for _, tt := range entries[0].Tools {
		got[tt.Name] = tt.Risk
	}
	if got["shell"] != "high" {
		t.Errorf("shell risk = %q, want high", got["shell"])
	}
	if got["echo"] != "low" {
		t.Errorf("echo risk = %q, want low", got["echo"])
	}
}

// T3.4.3 LocalInventory.Get 按工具名查找
func TestLocalInventory_Get(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(&stubTool{name: "shell", desc: "x"}); err != nil {
		t.Fatal(err)
	}
	inv := &LocalInventory{Reg: reg}
	if _, ok := inv.Get(context.Background(), "shell"); !ok {
		t.Error("Get(shell) should succeed")
	}
	if _, ok := inv.Get(context.Background(), "missing"); ok {
		t.Error("Get(missing) should fail")
	}
}

// T3.4.4 Combined 聚合 + 后注册覆盖
func TestCombined_Dedupe(t *testing.T) {
	reg1 := tool.NewRegistry()
	if err := reg1.Register(&stubTool{name: "shell", desc: "local-shell"}); err != nil {
		t.Fatal(err)
	}
	reg2 := tool.NewRegistry()
	if err := reg2.Register(&stubTool{name: "shell", desc: "remote-shell"}); err != nil {
		t.Fatal(err)
	}
	c := NewCombined()
	c.Add(&LocalInventory{Name: "first", Reg: reg1})
	c.Add(&LocalInventory{Name: "second", Reg: reg2})
	entries, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("got %d entries, want 2", len(entries))
	}
}

// T3.4.5 Combined 去重：同名 plugin 后注册覆盖
func TestCombined_NameOverride(t *testing.T) {
	reg1 := tool.NewRegistry()
	if err := reg1.Register(&stubTool{name: "shell", desc: "v1"}); err != nil {
		t.Fatal(err)
	}
	reg2 := tool.NewRegistry()
	if err := reg2.Register(&stubTool{name: "shell", desc: "v2"}); err != nil {
		t.Fatal(err)
	}
	c := NewCombined()
	c.Add(&LocalInventory{Name: "dup", Version: "v1", Reg: reg1})
	c.Add(&LocalInventory{Name: "dup", Version: "v2", Reg: reg2})
	entries, _ := c.List(context.Background())
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Version != "v2" {
		t.Errorf("Version = %q, want v2", entries[0].Version)
	}
}

// T3.4.6 LocalInventory 空 registry
func TestLocalInventory_Empty(t *testing.T) {
	inv := &LocalInventory{Reg: tool.NewRegistry()}
	entries, _ := inv.List(context.Background())
	if len(entries) != 1 {
		t.Errorf("empty reg should yield 1 entry, got %d", len(entries))
	}
	if len(entries[0].Tools) != 0 {
		t.Errorf("tools = %d, want 0", len(entries[0].Tools))
	}
}

// T3.4.7 LocalInventory nil registry
func TestLocalInventory_NilReg(t *testing.T) {
	inv := &LocalInventory{}
	entries, err := inv.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if entries != nil {
		t.Errorf("nil reg should yield nil entries")
	}
}