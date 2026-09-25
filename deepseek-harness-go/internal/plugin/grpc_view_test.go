package plugin

import (
	"context"
	"testing"

	pluginpb "deepseek-harness-go/internal/plugin/proto"
)

// T3.4.8 GRPCInventory 空 inventory
func TestGRPCInventory_Empty(t *testing.T) {
	g := NewGRPCInventory()
	entries, err := g.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %d, want 0", len(entries))
	}
}

// T3.4.9 GRPCInventory.Get 不存在的 plugin
func TestGRPCInventory_GetMissing(t *testing.T) {
	g := NewGRPCInventory()
	_, ok := g.Get(context.Background(), "missing")
	if ok {
		t.Error("expected not-found")
	}
}

// T3.4.10 GRPCInventory.Health 不存在的 plugin
func TestGRPCInventory_HealthMissing(t *testing.T) {
	g := NewGRPCInventory()
	_, err := g.Health(context.Background(), "missing")
	if err == nil {
		t.Error("expected error")
	}
}

// T3.4.11 GRPCInventory 加 nil client 时 Health 返回 error
func TestGRPCInventory_NilClientHealth(t *testing.T) {
	g := NewGRPCInventory()
	g.Add("x", nil)
	_, err := g.Health(context.Background(), "x")
	if err == nil {
		t.Error("expected error for nil client")
	}
}

// T3.4.12 GRPCInventory 加一个零 specs 的 client → unhealthy
func TestGRPCInventory_UnhealthyWhenNoSpecs(t *testing.T) {
	c := &Client{addr: "127.0.0.1:0", specs: []*pluginpb.ToolSpec{}}
	g := NewGRPCInventory()
	g.Add("x", c)
	entries, _ := g.List(context.Background())
	if len(entries) != 1 {
		t.Fatalf("entries = %d", len(entries))
	}
	if entries[0].Healthy {
		t.Error("Healthy should be false for empty specs")
	}
	if entries[0].Kind != "grpc" {
		t.Errorf("Kind = %q", entries[0].Kind)
	}
}